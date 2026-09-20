# Task: Web UI module (htmx/tailwind kanban) alongside MCP

## Original input

> так смотри какая задача
> нам надо в мсп сервер добавть модуль веб сервера на htmx/tailwind и масимально стандартном го ланг стеке. этот веб сервер должен быть равноправным модулем с мсп всё для работы с тасками
> когда мы щапускаем мсп сервер - стартует веб сервер (возможно по мсп запросу)
> когда мы запускаем веб сервер - стартуем мсп сервер (возможно по конфигу)
> веб сервер нам будет показывать канбан дэшборд показывающий таски как карточки в колоках по статусу
> и ещё надо обновить сам проект чтобы он использовал свой мсп сервер во время разработки
> начинаем такой таск

## Clarifications

**Q: Scope of the web dashboard in v1?**
A: **Read-only.** Kanban board, cards, task detail. No mutations from the web UI in this task.

**Q: How are MCP and web startup linked technically?**
A: **One process, two transports.** A single binary runs the MCP server over stdio and the HTTP server in a goroutine, both over one shared `task.Service`. The web module is enabled by config/flag, and an MCP tool can start it on demand. No child processes, no cross-process write races.

**Q: How is Tailwind delivered?**
A: **Vendored, pre-built CSS embedded into the binary** via `go:embed`. No Node in the build, works offline, single binary. CSS refresh is a manual step.

## Problem statement

Add a web server module to `mcp-task-manager` that serves a read-only htmx/Tailwind kanban dashboard of the project's tasks from the same process and the same `task.Service` that backs the MCP tools — startable both from MCP-server mode (via config and via an MCP tool) and as its own entry point (which also brings up the MCP server per config) — and repair the repository's own MCP configuration so the project dogfoods its task manager during development.

## Acceptance criteria

### Web module
- [ ] New `internal/web` package: `net/http` + `html/template`, standard library routing (`http.ServeMux` with Go 1.22+ method/path patterns). No web framework dependency.
- [ ] Serves a kanban board at `/`: one column per status (`todo`, `in_progress`, `done`), each task rendered as a card.
- [ ] Card shows at minimum: id, title, priority, type; visually marks blocked tasks and subtasks (or nests subtasks under their parent card).
- [ ] Task detail view (own page or htmx-swapped panel) showing full description, relations, subtasks, attached file names.
- [ ] htmx is used for at least one real partial update (e.g. board refresh / opening a card) — templates render fragments, not just whole pages.
- [ ] Tailwind CSS and htmx JS are vendored in-repo and embedded with `go:embed`; the page renders correctly with no network access.
- [ ] Read-only: no route mutates task state. Server-side handlers only read through `task.Service`.
- [ ] All task text rendered through `html/template` escaping; no `template.HTML` on user-controlled task content.

### Process composition
- [ ] One binary, one process: MCP (stdio) and HTTP share a single resolved project and a single `task.Service` instance.
- [ ] Config gains a `web:` section (at least `enabled`, `addr`/`port`) in `mcp-tasks.yaml`; env override available in the same style as the existing overrides.
- [ ] MCP-server mode starts the web server when config enables it.
- [ ] An MCP tool (e.g. `start_web_ui`) starts the web server on demand when it is not already running, and reports the URL; calling it twice is not an error and does not double-bind the port.
- [ ] A CLI entry point (e.g. `mcp-task-manager serve web` / `--web`) starts the web server in the foreground; per config it also serves MCP in the same process.
- [ ] Web server logging never writes to stdout in MCP stdio mode (stdout is the JSON-RPC channel); logs go to stderr.
- [ ] Clean shutdown: the HTTP listener is stopped via `Shutdown` on signal/context cancellation and the process exits without a leaked goroutine.

### Dogfooding
- [ ] `.mcp.json` no longer depends on the unset `${CLAUDE_PLUGIN_ROOT}`; the `task-manager` server starts successfully from a plain `claude` session opened in this repository.
- [ ] Resolution lands on this repo's own `tasks/` directory (per `mcp-tasks.yaml`), verified by an actual tool call, not by inspection alone.
- [ ] The stray untracked `tasks/.index.json` (a retired cache file) is resolved — removed and/or gitignored.
- [ ] `CLAUDE.md` / `README.md` document the web module, its config, and the dogfooding setup.

### Quality gates
- [ ] `go build ./...` and `go vet ./...` clean.
- [ ] `go test ./...` passes, including new tests for `internal/web` (handler-level tests over a temp task directory: board renders expected columns/cards, detail view, 404 for unknown id, no-mutation guarantee).
- [ ] No new third-party runtime dependencies beyond what is already in `go.mod` (vendored CSS/JS assets are static files, not Go modules).

## Design direction (from the user, after the research phase)

Verbatim:

> давай это выделим работу с тасками в отдельный внутренний пэкедж если оно ещё не так и сделаем чтобы работа с тасками была защищена от гонок. ну например мы можем так же показывать в веб ui какой таск сейчас в работе и немного оповещать пользовател что он в опасной зоне.
> учитывае то что если запущен мсп сервер мы не будем руками запускать веб, и наоборот. и не мудри там с защитами
> переходи к дизайну

Binding constraints this adds:

- **Task operations live in one internal package.** `internal/task` already is that package; confirm it and, where the boundary leaks (e.g. handlers reaching into `storage.Index` directly, or the web layer needing data the service does not expose), close the leak by widening `task.Service`'s API rather than by letting a second consumer touch storage.
- **Race protection is required, and must stay simple.** The explicit instruction is "не мудри там с защитами" — no per-entry locking, no lock-free schemes, no actor/ownership machinery. A single straightforward mutex (or RWMutex) guarding the one concurrency-unsafe unit is the intended shape. Correctness under `go test -race` matters; cleverness does not.
- **Single process, single launcher.** MCP and web are never started by hand against the same directory at the same time — whichever transport the process runs, the other comes up inside it. Cross-process coordination (file locks, PID files, port handshakes) is explicitly out of scope.
- **The board should surface the in-flight task.** Show which task is currently `in_progress` and give the viewer a light-touch warning that an agent may be working in that area right now ("danger zone"). This is presentation, not enforcement — the UI stays read-only.

## Clarification after the design phase (from the user)

> http слой если запускается с параметром путь к проекту он вполне его резолвит - если ты про это
> но в общем да давай переходи к планированию

Confirmed reading, and it matches the accepted design — the two cases must not be conflated:

- **The `serve web` entry point DOES resolve the project**, eagerly at startup, from the config / project path / env, and then shares that resolution with the MCP transport via `project.NewStatic`. Passing an explicit project path to the web entry point is supported and resolves normally.
- **Individual HTTP request handlers do NOT resolve.** A single GET must never be the thing that first resolves the project, because resolution runs `Service.Initialize()` — layout migration and auto-archive — which would let a plain read move directories on disk. Handlers use `Resolver.Current()` and render a placeholder if nothing is resolved yet.

The placeholder state is therefore reachable only in MCP-server mode, where the web listener comes up before the client has supplied roots. Started via `serve web`, the board has data from the first request.