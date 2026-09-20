# RESEARCH — Web UI module (htmx/tailwind kanban) alongside MCP

All paths are absolute under `/Users/iga/Workspace/mcp-task-manager`; line references use `path:line`.

## Task Slice

- Process composition: how `cmd/mcp-task-manager/main.go` picks CLI vs stdio MCP mode and assembles the MCP server, and where a second (HTTP) transport would attach.
- Lazy project resolution (`internal/project`, `internal/config`) and whether the resolved `*task.Service` is shareable/concurrency-safe with an HTTP goroutine.
- The read-side API of `task.Service` + the task model + the in-memory index, i.e. exactly what a kanban board can obtain today.
- Registration mechanics for MCP tools (`internal/tools`) and CLI subcommands (`internal/cli`), for `start_web_ui` and `serve web`.
- Config schema/env overrides for a new `web:` section; `go:embed` precedent; logging destination.
- Dogfooding state: `.mcp.json`, `.claude-plugin/`, `plugins/`, `tasks/` vs `.tasks/`, `.index.json`, `.gitignore`.

## Confirmed Facts

### 1. Documents loaded

- `CLAUDE.md` (project instructions, 348 lines) — architecture, storage layout, lifecycle, relations, archiving, tool list, config, project structure.
- `README.md` (401 lines) — user-facing install/integration docs; no web/HTTP content exists today (`grep -i web/htmx/serve` returns only "Running the Server" and MCP integration sections, README.md:57, 172–194).
- `_design.md` — rationale for `resolution` / `verified_at` only (headings at `_design.md:1,8,24,45,70,89,100,118,140`). No web/process content.
- `.claude/skills/research/SKILL.md` (the research skill, which dictates this report's format).
- `AGENTS.md` is a symlink to `CLAUDE.md`.
- No `doc/` notes directory exists; `docs/` exists but was not needed (not part of the Go build).

### 2. Current execution path

- Mode selection is by argument count only: `if len(os.Args) > 1 { cli.Run(); return }`, otherwise MCP stdio (`cmd/mcp-task-manager/main.go:27-38`). So **any** argument means CLI mode; a `serve web` subcommand would necessarily route through `internal/cli`.
- MCP mode: `srv, _ := newServer()` then `server.ServeStdio(srv)`; a server error is `log.Fatalf` (`cmd/mcp-task-manager/main.go:34-37`). The second return value (the `*project.Resolver`) is currently discarded in `main` — it exists for tests.
- `newServer()` (`cmd/mcp-task-manager/main.go:45-90`):
  1. Creates a `project.Resolver` whose roots func closes over the not-yet-assigned `srv` (`main.go:47-49`) — deliberate late binding.
  2. `server.NewMCPServer("mcp-task-manager", "0.1.0", WithToolCapabilities(true), WithInstructions(instructionsMD), WithIcons(...))` (`main.go:51-63`).
  3. Registers tools built from `config.DefaultConfig()` types (`main.go:67-69`), remembering the type lists in a mutex-guarded `toolSet` (`main.go:92-115`).
  4. `resolver.OnResolve(...)`: logs the resolution, and if the resolved config's task/relation types differ, calls `srv.SetTools(tools.Build(...)...)` which re-publishes and sends `notifications/tools/list_changed` (`main.go:71-80`; mcp-go `server/server.go:1082-1107`).
  5. Registers a `notifications/roots/list_changed` handler that calls `resolver.Invalidate()` (`main.go:84-87`).
- Lifecycle to a served tool call: process start → `newServer` (touches **no** task files; comment at `main.go:40-44`) → `ServeStdio` on `os.Stdin`/`os.Stdout` (mcp-go `server/stdio.go:837-859`) → client `initialize` → first `tools/call` → handler wrapper `withService` → `resolver.Service(ctx)` → `config.Resolve` + storage/index construction + `svc.Initialize()` (migration, index build, auto-archive) → `OnResolve` callbacks → handler body.
- Nothing cancels/shuts down anything today: `ServeStdio` blocks in `main` and the process exits when stdin closes. There is no signal handling and no `context` plumbed into `main`.

### 3. Project/config resolution

- `project.Resolver` holds `roots RootsFunc`, a `sync.Mutex`, the cached `*Resolved`, and `onChange` callbacks (`internal/project/resolver.go:36-42`). This is the **only** mutex in the whole `internal/` tree (verified: `grep -rn "sync\." internal cmd --include=*.go | grep -v _test` → one hit, `internal/project/resolver.go:39`).
- `Resolved` = `{Config *config.Config; Service *task.Service}` (`resolver.go:24-28`).
- `Get(ctx)` returns the cached resolution or resolves once under the lock; failures are not cached; callbacks fire outside the lock (`resolver.go:68-90`). `Service(ctx)` is the thin shorthand used by every tool handler (`resolver.go:92-99`).
- `resolve()` builds the whole stack: `config.Resolve(provider)` → `storage.NewMarkdownStorage(tasksDir)` → `storage.NewIndex(tasksDir, mdStorage)` → `task.NewService(md, md, md, index, cfg.TaskTypes, cfg)` → `svc.Initialize()` (`resolver.go:101-121`). One storage object serves as Storage, ArchiveStorage and FileStorage.
- `NewStatic(resolved)` pins an already-built project, skipping resolution (`resolver.go:126-128`) — used by tests (`internal/tools/tools_test.go:114-125`) and explicitly documented as "callers that resolved the project themselves". **This is the existing escape hatch for a web-first entry point.**
- `Invalidate()` nils the cache (`resolver.go:60-64`) — meaning a long-lived HTTP handler that cached `*task.Service` in a struct field would go stale after a roots change; the correct pattern is to call `resolver.Service(ctx)` per request, as the tools do.
- Roots path: `project.ServerRoots(srv, DefaultRootsTimeout)` issues `roots/list` with a 5 s timeout (`internal/project/roots.go:16-42`); `PathFromURI` converts `file://` URIs (`roots.go:47-71`).
- Concurrency verdict: `Resolver.Get` is safe. **`*task.Service` is not.** `task.Service` has no mutex (`internal/task/service.go:94-101`), `storage.Index` has none (`internal/storage/index.go:90-100` — plain maps `entries`, `relationsBySource`, `relationsByTarget`), and `MarkdownStorage` has none. Every index read method mutates shared state on the stale path: `syncIfStale()` calls `Rebuild()` → `reset()` which reassigns all three maps (`index.go:119-123, 157-190`), and `builtAt` is written by `Set`/`Delete` (`index.go:258-268`). An HTTP GET concurrent with an MCP `create_task` is a genuine data race on Go maps, not merely a stale read.
- Awkwardness for a web server that starts before any MCP tool call: resolution needs a `ctx` and may do a 5 s `roots/list` round trip which **can only succeed after `initialize`** (`resolver.go:1-7` package doc; `roots.go:14-16`). A web server started at process start therefore either (a) resolves without roots (falling back to env/cwd marker search, `config.Resolve` with a nil provider — see `resolveRoot` attempt "roots unavailable", `internal/config/config.go:211-215`), or (b) defers resolution to the first HTTP request, which then blocks up to 5 s. Note also that the resolver caches the *first* successful resolution, so whichever transport resolves first fixes the project for both (until `Invalidate`).

### 4. Read API surface of `task.Service`

Signatures (all in `internal/task/service.go`):

| Method | Line | Returns |
|---|---|---|
| `List(status *Status, priority *Priority, taskType *string, parentID *string, resolution *Resolution) []*Task` | 490 | index entries, **no Description / ResolutionNote**; sorted by id (numeric-aware, `internal/storage/index.go:14-25, 317-320`). `parentID == nil` → all tasks incl. subtasks; `"0"` → top-level only; other → that parent's subtasks (`index.go:302-314`) |
| `Get(id string) (*Task, error)` | 232 | full task **with description** from disk; falls back to archive (`service.go:230-245`) |
| `GetWithSubtasks(id) (*Task, []*Task, error)` | 247 | task + its subtasks (subtasks without descriptions) |
| `GetSubtaskCounts(taskID) (total, done int)` | 287 | |
| `IsBlocked(taskID) (bool, []BlockingInfo)` | 727 | `BlockingInfo{TaskID, Status, Title}` (`service.go:632-636`); only counts blockers whose status != done |
| `ListTaskFiles(taskID) ([]string, error)` | 279 | works for active and archived |
| `ReadTaskFile(taskID, filename) (string, error)` | 270 | active and archived |
| `ListArchived() ([]*Task, error)` | 877 | linear scan of the archive dir |
| `GetNextTask() *Task` | 496 | |
| `EnsureProjectExists() error` / `ProjectFound() bool` | 117 / 130 | read-op precondition used by every read tool handler |
| `GetAutoArchiveCandidates() []*Task` | 831 | |

Derived data already available: `blocked` + blockers (`IsBlocked`), subtasks (`GetWithSubtasks`, `List` with parentID), subtask counts, `EffectiveResolution()` (`internal/task/task.go:86-94`).

**What the board needs but `Service` does NOT expose:**
- **Reverse / full relation edges.** `Task.Relations` is source-side frontmatter only. `Index.GetRelationsForTask` returns source ∪ target edges (`internal/storage/index.go:572-590`) and `Index.GetBlockers` exists (`index.go:592-603`), but neither is on the `task.Index` interface consumer side… actually `GetRelationsForTask` and `GetBlockers` *are* in the `Index` interface (`service.go:66-70`), yet `Service` exposes **no** public wrapper for `GetRelationsForTask` — only `IsBlocked` uses `GetBlockers` internally. So "what blocks this / what does this block" as a full edge list is unreachable through `*task.Service` today.
- **Bulk blocked-ness.** `list_tasks` computes it per task by calling `svc.IsBlocked(t.ID)` in a loop (`internal/tools/management.go:394-398`); each call runs `syncIfStale` (`index.go:592`). An N-card board would do N such scans. No batch API.
- **Resolved project metadata** (root, tasks dir, source) — `Service.config` is unexported with no getter; the web layer must get it from `Resolved.Config` / `Config.Resolution` (`internal/project/resolver.go:24-33`, `internal/config/config.go:56-80`).
- **Descriptions in list results** — by design (`service.go:489`); a board that wants excerpts must `Get` per task (one file read each).
- **Archived-task detail with subtasks/blocked** — `Get` falls back to archive, but `IsBlocked`/`GetSubtasks` work off the active index only, so archived tasks yield empty derived data.
- **No watch/notify/version counter** on the service or the index, so htmx polling is the only refresh mechanism without new API surface.

### 5. Task model (`internal/task/task.go`)

- `Status` string enum: `todo`, `in_progress`, `done` (`task.go:6-12`); validation `IsValidStatus` (`task.go:97-103`). Exactly three → three kanban columns, no derived "blocked" status.
- `Priority` string enum `critical|high|medium|low` (`task.go:14-22`) with `Order() int` returning 0..3, unknown → 99 (`task.go:25-38`). Note: the index's own sort for lists is **by id**, not priority (`index.go:317-320`); priority order is only applied in `NextTodo` (`index.go:373+`). A board wanting priority-ordered columns must sort itself using `Priority.Order()`.
- `Type` is a free string validated against configured types (`Service.isValidType`, `service.go:885`).
- `Relation{Type, Task string}` with both yaml and json tags (`task.go:41-44`).
- `Task` fields (`task.go:47-76`): `ID`, `ParentID` (`""` = top-level), `Title`, `Description` (`yaml:"-"`, body), `Status`, `Priority`, `Type`, `Relations`, `CreatedAt`, `UpdatedAt`, `Resolution`, `ResolutionNote`, `ClosedAt *time.Time`, `VerifiedAt *time.Time`.
- `Closed()` (`task.go:79-81`), `EffectiveResolution()` (`task.go:86-94`, done-without-resolution reads as `completed`), `Resolution.Delivered()` (`task.go:167-169`), `Resolutions()`/`ResolutionStrings()` (`task.go:144-152, 172-178`).
- Card badge material that already exists: priority, type, resolution, blocked (derived), subtask counts, `verified_at`/`closed_at` presence.
- Precedent for human rendering of all of this: `internal/cli/output.go:20-60` (`FormatTaskDetail` with `TaskDetailOptions{Subtasks, Blocked, Blockers}` at `output.go:13-18`) — a direct model for the web detail view's data shape.

### 6. Storage & index

- `Index` is in-memory only, built from `{id}/{id}.md` files, with no on-disk form (`internal/storage/index.go:88-100`).
- `IndexEntry` holds metadata minus Description and ResolutionNote (`index.go:27-44`), converted by `taskToEntry`/`entryToTask` (`index.go:47-79`).
- `Load()` deletes a leftover legacy `.index.json` then `Rebuild()`s (`index.go:165-174`; path helper `index.go:115-117`). **So any stray `tasks/.index.json` is removed on the first service init** — confirmed empirically: no `.index.json` exists in `tasks/` or `.tasks/` right now.
- Self-healing: `syncIfStale()` on entry to every exported query; rebuild triggered when any `{id}/{id}.md` mtime is after `builtAt`, or the count of task dirs differs from `len(entries)` (`index.go:176-224`). Contract comment forbids nested calls (`index.go:176-181`).
- **No locking anywhere in `internal/storage`** (see §3). `Rebuild` reassigns the three maps; `Set`/`Delete`/`AddRelation`/`RemoveRelation` mutate them and bump `builtAt` (`index.go:258-268, 541-570`). `CLAUDE.md:330-332` states the MVP assumes a single server with no locking — this remains literally true in code, and an HTTP reader goroutine would be the first thing to violate the assumption.
- An HTTP read path would touch: `Index.Filter`/`Get`/`GetSubtasks`/`SubtaskCounts`/`GetBlockers` (each with a possible full directory rescan + per-task file parse via `MarkdownStorage.LoadAll`, `internal/storage/markdown.go:116`), `MarkdownStorage.Load` for descriptions (`markdown.go:100`), `LoadAllArchived` for archive views (`markdown.go:287`), and `ListFiles` for attachment names (`internal/storage/files.go`).
- Writes are atomic temp+rename in `Save` (`markdown.go:45`); `Archive` renames whole directories (`markdown.go:268`).

### 7. Existing CLI structure

- `cli.Run()` → `RunWithArgs(os.Args, os.Stdout, os.Stderr)` → `os.Exit(code)` (`internal/cli/cli.go:18-21`). `RunWithArgs` is the testable seam and returns an int exit code.
- Every invocation starts with `flaggy.ResetParser()` and then loads config just to know the task types for help text (`cli.go:26-35`).
- Subcommands are declared inline, each `flaggy.NewSubcommand(name)` + flags + `flaggy.AttachSubcommand(cmd, 1)` (13 of them, `cli.go:44-180`), then `flaggy.ParseArgs(args[1:])` (`cli.go:183`), then a chain of `if xCmd.Used { return cmdX(...) }` (`cli.go:186-243`), falling through to `return 0`.
- A `serve`/`web` subcommand attaches identically at `cli.go:~180` plus one `if` at `cli.go:~243`, delegating to a `cmdServe` in `commands.go`. Note `RunWithArgs` currently returns an exit code and callers `os.Exit` — a foreground server would block inside that call.
- **Flaggy behavior with an unknown/absent subcommand (measured, built binary):** `mcp-task-manager serve web` and `mcp-task-manager bogus` both print to **stdout** (confirmed: output survives `2>/dev/null`):
  `mcp-task-manager: No subcommand or positional value found at position 1.` followed by `Available subcommands: version list get ...`, and **exit code 2**. Flaggy exits the process itself (tests set `flaggy.PanicInsteadOfExit = true`, `internal/cli/cli_test.go:13-17`). A nested `serve web` form means `serve` is a subcommand with its own attached subcommand or a positional.
- `commands.go` helpers: `loadConfig()` (`commands.go:12-19`), `initServiceWithConfig(cfg)` building storage+index+service+Initialize (`commands.go:21-33`), `initService()` (`commands.go:35-47`), `checkProjectExists(stderr,cfg)` returning an exit code (`commands.go:49-58`). Note the CLI builds its own service **without** `project.Resolver` — a `serve web` entry point that must also run MCP would need the resolver path instead (or wrap what it built in `project.NewStatic`).
- `output.go` (135 lines) holds all formatting: `FormatTaskDetail` (`output.go:20`), plus table/JSON formatters using `text/tabwriter`. Commands take `stdout, stderr io.Writer` — the web module would mirror this injectability for testability.

### 8. Tool registration mechanics

- `tools.Build(rs *project.Resolver, validTypes, relationTypes) []server.ServerTool` concatenates `managementTools`, `workflowTools`, `relationTools`, `fileTools` (`internal/tools/tools.go:18-25`); `Register` = `s.AddTools(Build(...)...)` (`tools.go:28-30`).
- `withService(rs, fn)` wraps a handler: resolves the service and, on failure, returns `mcp.NewToolResultError("could not determine which project to use: "+err)` rather than a Go error (`tools.go:35-43`).
- Per-tool text lives in embedded YAML: `//go:embed descriptions/*.yaml` → `descriptionsFS`, parsed at package init into `toolTexts`; `textFor(name)` and `toolText.param(name)` **panic** if a file or param is missing (`internal/tools/descriptions.go:11-72`). There are 14 files in `internal/tools/descriptions/`. **Adding `start_web_ui` therefore requires `internal/tools/descriptions/start_web_ui.yaml` or the package panics at first use.**
- Example non-trivial tool shape: `workflowTools` builds `mcp.NewTool(name, mcp.WithDescription(...), mcp.WithString("id", mcp.Required(), ...))` and appends `server.ServerTool{Tool, Handler}` (`internal/tools/workflow.go:12-51`); handlers are `xHandler(rs *project.Resolver) server.ToolHandlerFunc` returning `withService(rs, func(ctx, svc, req){...})` (`workflow.go:53-104`).
- `get_next_task` is the closest precedent for a **parameterless** tool (`workflow.go:15-20`).
- Re-publication: `srv.SetTools(tools.Build(...)...)` replaces the whole map and fires `notifications/tools/list_changed` when `listChanged` was declared (mcp-go `server/server.go:1082-1107`); the server declares it via `server.WithToolCapabilities(true)` (`cmd/mcp-task-manager/main.go:56`). **Consequence: any new tool must be produced by `tools.Build`, otherwise the first `SetTools` re-publication would silently drop it.**
- Does `start_web_ui` fit `withService`? Only partially. `withService` hands the handler a `*task.Service` and nothing else; a web-start tool needs (a) the web server handle/state and (b) at minimum the `*config.Config` (for addr/port) and ideally the whole `*Resolved`. `Resolver.Get(ctx)` returns `*Resolved` and is public, so a sibling wrapper (or a closure over a web-controller value passed into `Build`) is needed; `Build`'s signature currently takes only `(rs, validTypes, relationTypes)` (`tools.go:18`) and is called from two places (`tools.go:29` and `cmd/mcp-task-manager/main.go:78`), both of which would need the extra argument.
- Tool tests call handler constructors directly with a `project.NewStatic` resolver (`internal/tools/tools_test.go:114-125`) — the same technique works for a web-start tool.

### 9. Config schema & env overrides

- Env var names are constants: `MCP_TASKS_DIR`, `MCP_PROJECT_DIR`, `CLAUDE_PROJECT_DIR`, `MCP_ROOT_SOURCE` (`internal/config/config.go:13-26`). A `web:` override would idiomatically add e.g. `EnvWebAddr = "MCP_WEB_ADDR"` there.
- Well-known names: `ConfigFileName = "mcp-tasks.yaml"`, `DefaultTasksDirName = ".tasks"`, `LegacyTasksDirName = "tasks"` (`config.go:29-33`).
- **Nested-section precedent** — `AutoArchiveConfig{Enabled bool `yaml:"enabled"`; AfterDays int `yaml:"after_days"`}` (`config.go:82-86`), embedded as a **value** (not pointer) field `AutoArchive AutoArchiveConfig `yaml:"auto_archive"`` (`config.go:92`), with defaults set in `DefaultConfig()` (`config.go:106-116`). A `web:` section would mirror this exactly.
- Full `Config` (`config.go:88-98`): `TaskTypes`, `RelationTypes` (omitempty), `AutoArchive`, `TasksDirName` (`yaml:"tasks_dir,omitempty"`), plus three `yaml:"-"` runtime fields `DataDir`, `ProjectFound`, `Resolution *Resolution`.
- Loading: `Resolve(roots)` starts from `DefaultConfig()`, then `yaml.Unmarshal` of `<root>/mcp-tasks.yaml` **over** it — so a section absent from the file keeps its default, and a present section overwrites field-by-field (a partially specified `auto_archive:` would zero the unspecified fields; e.g. `enabled: true` alone would set `after_days: 0`). See `config.go:130-156`. This repo's own file specifies both (`mcp-tasks.yaml:5-7`).
- **There is no validation step for the config at all** — no function validates task types, relation types or auto_archive values at load time; `IsValidTaskType`/`IsValidRelationType` (`config.go:325-342`) are per-call checks. So a `web:` section would have no existing validation hook to attach to.
- Resolution order implemented in `resolveRoot` (`config.go:184-247`) with `Attempts` recorded for diagnostics; `MCP_ROOT_SOURCE` forces a single source (`config.go:186-187`). `pickTasksDirName` prefers a directory that actually holds tasks over one that merely exists (`config.go:274-313`, helper `holdsTasks` at `config.go:292-313`).

### 10. Logging

- The project uses only the standard `log` package, never `fmt.Print*` to stdout outside the CLI: `internal/storage/migrate.go:51,55`; `internal/task/service.go:150,870`; `cmd/mcp-task-manager/main.go:36,72,79`.
- **Empirically confirmed that `log`'s default output is stderr**: a scratch program `log.Printf("HELLO-LOG")` produced 0 lines on stdout (`2>/dev/null | wc -l` → `0`) and `2026/09/20 22:24:01 HELLO-LOG` on stderr (`2>&1 >/dev/null`). So stdout is already safe for JSON-RPC today, and any web logging that goes through `log` inherits that.
- mcp-go's own stdio server logs via `log.New(os.Stderr, ...)` (`server/stdio.go:369-380`), with `SetErrorLogger` available (`stdio.go:382-385`). `ServeStdio` reads `os.Stdin` and writes `os.Stdout` (`stdio.go:837-859`).
- Risk to watch: `net/http.Server` defaults `ErrorLog` to the standard logger (stderr) — fine — but any handler using `fmt.Println` or a template written to `os.Stdout` would corrupt the JSON-RPC stream. There is no existing guard/lint for this.

### 11. Existing tests & quality constraints

- Test files: `cmd/mcp-task-manager/main_test.go`, `internal/cli/{cli,output}_test.go`, `internal/config/{config,resolve}_test.go`, `internal/project/resolver_test.go`, `internal/storage/{storage,migrate}_test.go`, `internal/task/{service,task,resolution}_test.go`, `internal/tools/tools_test.go`. Sizes: `internal/storage/storage_test.go` 2611 lines, `internal/cli/cli_test.go` 732 lines.
- **Measured quality gates (all clean, `GOWORK=off`):**
  - `go build ./...` → no output, exit 0.
  - `go vet ./...` → no output, exit 0.
  - `go test ./...` → `ok` for all 7 packages (cmd 1.813s, cli 1.509s, config 1.143s, project 2.156s, storage 0.859s, task 2.623s, tools 2.946s).
  - Toolchain: `go version go1.27.1 darwin/arm64`; `go.mod` declares `go 1.25.5` — so `http.ServeMux` method/path patterns (1.22+) are available.
- Idioms: `t.TempDir()` everywhere (dozens of uses in `internal/cli/cli_test.go`), `t.Setenv("MCP_TASKS_DIR", tmpDir)` to point a test at a temp backlog (`cli_test.go:52-53`), table-driven cases (`internal/tools/tools_test.go:53-77`), and buffer-injected `RunWithArgs(args, &stdout, &stderr)` for CLI tests (`cli_test.go:20`).
- **Reusable helper for a web handler test:** `newTestService(t) *project.Resolver` in `internal/tools/tools_test.go:114-125` — temp dir → `MarkdownStorage` → `Index` → `Config{TaskTypes, ProjectFound: true}` → `Service` → `Initialize()` → `project.NewStatic`. It is unexported and package-local to `tools`, so `internal/web` would need its own copy (or an exported test helper). `cmd/mcp-task-manager/main_test.go:32-38` has `isolateEnv(t)` clearing all four env vars, and a `rootsHandler` that answers `roots/list` end-to-end over an in-process client (`main_test.go:17-30, 43+`) — the precedent for testing process composition.
- A handler-level web test fits naturally: build the service as above, construct the web handler over `project.NewStatic(...)`, and drive it with `httptest.NewRequest` + `httptest.NewRecorder`. No such helper exists yet.
- A `PostToolUse` hook runs `.claude/hooks/format-go.sh` on every Edit/Write (`.claude/settings.json:1-14`).
- CI: only `.github/workflows/release.yml` (build/release on GitHub release). **There is no CI test workflow**, so gates are local-only.

### 12. Embedding precedent

- `cmd/mcp-task-manager/main.go:20-24`: `//go:embed icon.png` → `[]byte`, `//go:embed instructions.md` → `string`. Both files sit next to `main.go` (`cmd/mcp-task-manager/` contains `icon.png`, `instructions.md`, `main.go`, `main_test.go`).
- `internal/tools/descriptions.go:11-15`: `//go:embed descriptions/*.yaml` → `embed.FS`, read at package init with panics on error (`descriptions.go:26-50`). This is the direct precedent for a directory embed in `internal/web` (e.g. `//go:embed static/* templates/*` → `embed.FS`, served with `http.FileServerFS` / parsed with `template.ParseFS`).
- Implication: embedded assets must live **inside** the `internal/web` package directory (go:embed cannot reference `../`), and a `.gitignore` that excluded vendored CSS/JS would break the build. Nothing in `.gitignore` currently excludes `.css`/`.js`.

### 13. Dogfooding state

- `.mcp.json` (repo root, tracked) runs `go run -C ${CLAUDE_PLUGIN_ROOT} ./cmd/mcp-task-manager` with `GOWORK=off`.
- `plugins/mcp-task-manager/.mcp.json` (the Codex plugin package) uses `${CLAUDE_PLUGIN_ROOT}/../..` instead.
- `${CLAUDE_PLUGIN_ROOT}` is set by Claude Code only for **plugin-installed** servers (it points at the installed plugin's directory). A project-scoped root `.mcp.json` is loaded directly from the repo and gets no such variable, so it expands to empty. **Measured failure mode:** `go run -C "" ./cmd/mcp-task-manager version` → `go: chdir : no such file or directory`. The server process dies immediately, which matches this session's reported `task-manager (CONNECTION_CLOSED): "Connection closed"`.
- Correct project-scoped forms (not yet applied, stated as fact about the tooling, not a proposal): omit `-C` entirely (Claude Code launches project MCP servers with the project as cwd) or use `${CLAUDE_PROJECT_DIR}`. README documents the plugin-install path only (`README.md:174-188`) and never mentions a project-scoped `.mcp.json`.
- The repo doubles as a plugin marketplace: `.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` both declare version 1.4.2, `"source": "./"`. So the root `.mcp.json` is simultaneously the plugin's bundled `.mcp.json` **and** the repo's project-scoped one — that dual role is the root cause of the broken variable.
- `.claude/` holds `settings.json` (the format hook), `cclsp.json`, `hooks/format-go.sh`, and six skills (`research`, `design`, `begin_task`, `planning`, `workflow`, `implementation`). `commands/` has `begin-task.md`, `execute-all.md`.
- **`tasks/` vs `.tasks/`:**
  - `mcp-tasks.yaml:1-7` pins `tasks_dir: tasks` with the comment "This repository's own backlog predates the `.tasks` default, so pin it", plus `task_types: [feature, bug]` and `auto_archive: {enabled: true, after_days: 30}`.
  - `tasks/` contains `archive/` (87 archived task dirs, `001`–`089` with gaps at 071/072) and one active task `tasks/web-ui-kanban-module/` (holding `web-ui-kanban-module.md` + `task.md`) — **untracked** (`git status --porcelain` → `?? tasks/web-ui-kanban-module/`, `?? .current_task`). `git ls-files tasks | wc -l` → 87.
  - `.tasks/` contains exactly one directory, `.tasks/task-file-storage/`, holding `design.md`, `plan.md`, `research.md`, `task.md` — **but no `task-file-storage.md` record file**, so it is not a valid task under the current layout. `git ls-files .tasks | wc -l` → 4 (tracked).
  - Verified resolution (`GOWORK=off go run ./cmd/mcp-task-manager version`): `root=/Users/iga/Workspace/mcp-task-manager tasks=/Users/iga/Workspace/mcp-task-manager/tasks source=cwd (tried: MCP_PROJECT_DIR unset; CLAUDE_PROJECT_DIR unset; roots unavailable)`. `list` returns exactly one row: `web-ui-kanban-module | Web UI module: htmx/tailwind kanban d... | todo | high | feature`.
  - Conclusion: **`tasks/` is live; `.tasks/` is an orphaned leftover** (artifacts of a previous task's attached files) and is never read while `mcp-tasks.yaml` pins `tasks_dir: tasks`. No conflict at runtime; `pickTasksDirName` would have preferred `.tasks` only if the config left `tasks_dir` unset (`internal/config/config.go:274-287`) — and `holdsTasks(.tasks)` would in fact return **false** (no `.md` at top level, no `archive/`, no `task-file-storage/task-file-storage.md`), so even unpinned the fallback would pick `tasks`.
  - Since Claude Code sets `CLAUDE_PROJECT_DIR`, a working `.mcp.json` would resolve via `source=CLAUDE_PROJECT_DIR` to the same `tasks/` directory.
- **`tasks/.index.json`:** does **not** currently exist (`ls tasks/.index.json` → No such file). `Index.Load` unconditionally `os.Remove`s it on every init (`internal/storage/index.go:165-174`), so it is self-clearing. `.gitignore` (12 lines) contains `/mcp-task-manager`, `coverage.out`, `.idea/`, `.vscode/`, `*.swp`, `*.swo`, `.DS_Store` — **no entry for `.index.json`**, and no entry for `.current_task` either (which is untracked).

### 14. Subsystem dependencies & side effects

- `Service.Initialize()` is not read-only. In order: `storage.MigrateFlatLayout()` (renames `tasks/{id}.md` → `tasks/{id}/{id}.md`, same for archive; per-task failures logged and skipped; `internal/storage/migrate.go:21-58`), then `index.Load()` (which **deletes** `tasks/.index.json` and rebuilds by parsing every task file), then `RunAutoArchive()` if enabled — failures logged, never fatal (`internal/task/service.go:136-154`).
- Auto-archive is **enabled in this repo** (`mcp-tasks.yaml:5-7`). `GetAutoArchiveCandidates` picks done tasks older than `after_days` since `UpdatedAt`, plus any done task whose resolution is not `Delivered()` regardless of age; subtasks only when the parent is also done (`service.go:831-856`). `RunAutoArchive` archives each, skipping failures (`service.go:859-875`). `complete_task` calls it again after every completion (`internal/tools/workflow.go:99-100`).
- **Side effects an HTTP-triggered read could cause:**
  1. If the web server is the first thing to resolve the project, it triggers `Initialize()` → migration + `.index.json` deletion + **auto-archive**, i.e. a plain HTTP GET can move task directories on disk.
  2. Any index query on a stale directory triggers a full `Rebuild()` (re-parse of every task file) inside the request (`internal/storage/index.go:182-190`).
  3. Concurrent with an MCP write, those map mutations are an unsynchronized race (§3, §6).
- The `OnResolve` callback in `main.go:71-80` runs `srv.SetTools(...)` — if the *web* goroutine triggers resolution, that callback fires from the HTTP goroutine and calls into mcp-go's `SetTools`, which does take `s.toolsMu` (mcp-go `server/server.go:1085-1098`) and broadcasts a notification to all clients. Fires before any client may be initialized.

## Evidence Map

| Concern | Current implementation | Evidence |
| --- | --- | --- |
| Mode selection | Any CLI arg → CLI; else stdio MCP | `cmd/mcp-task-manager/main.go:27-38` |
| MCP server assembly | `newServer()` builds resolver + server + tools + callbacks | `cmd/mcp-task-manager/main.go:45-90` |
| Lazy resolution | Cached, mutex-guarded, invalidated on roots change | `internal/project/resolver.go:36-90` |
| Shared service construction | storage + index + service + Initialize | `internal/project/resolver.go:101-121` |
| Pre-resolved project escape hatch | `project.NewStatic` | `internal/project/resolver.go:126-128` |
| Service read API | List/Get/GetWithSubtasks/IsBlocked/ListTaskFiles/ListArchived | `internal/task/service.go:232,247,279,490,727,877` |
| Missing: relation edge list on Service | Only `IsBlocked`; `GetRelationsForTask` unexposed | `internal/task/service.go:66-70,727` vs `internal/storage/index.go:572-590` |
| Kanban columns | 3-value Status enum | `internal/task/task.go:6-12` |
| Card ordering | Lists sorted by id, not priority | `internal/storage/index.go:317-320` vs `internal/task/task.go:25-38` |
| No locking | No `sync` outside the resolver | `grep -rn "sync\." internal cmd` → `internal/project/resolver.go:39` only |
| Index self-heal | mtime/count check then full rebuild | `internal/storage/index.go:176-224` |
| `.index.json` auto-removal | `os.Remove` on every `Load` | `internal/storage/index.go:165-174` |
| CLI subcommand pattern | flaggy declare → Attach → ParseArgs → `if Used` | `internal/cli/cli.go:44-243` |
| Unknown subcommand | stdout message, exit 2, process exits inside flaggy | measured with built binary |
| CLI service construction (no resolver) | `initServiceWithConfig` | `internal/cli/commands.go:21-33` |
| Tool build/register | `Build` concat + `AddTools` | `internal/tools/tools.go:18-30` |
| Handler wrapper | `withService` gives only `*task.Service` | `internal/tools/tools.go:35-43` |
| Tool text must exist or panic | embedded `descriptions/*.yaml` + `textFor` panic | `internal/tools/descriptions.go:11-61` |
| Re-publication drops non-Build tools | `SetTools` replaces the whole map | `cmd/mcp-task-manager/main.go:78`; mcp-go `server/server.go:1082-1107` |
| Nested config section precedent | `AutoArchiveConfig` value field + defaults | `internal/config/config.go:82-116` |
| Env override constants | four `Env*` consts | `internal/config/config.go:13-26` |
| Logging to stderr | std `log` default; mcp-go uses `log.New(os.Stderr,…)` | measured; mcp-go `server/stdio.go:369-380` |
| Embed precedent | file embeds in main, FS embed in tools | `cmd/mcp-task-manager/main.go:20-24`; `internal/tools/descriptions.go:11-15` |
| Broken dogfood config | `${CLAUDE_PLUGIN_ROOT}` empty for project scope | `.mcp.json`; measured `go: chdir : no such file or directory` |
| Live tasks dir | `tasks/`, resolved via cwd marker | `mcp-tasks.yaml:1-2`; `version` output |
| Auto-archive on every init/complete | enabled in this repo | `mcp-tasks.yaml:5-7`; `internal/task/service.go:146-153`; `internal/tools/workflow.go:99-100` |
| Quality gates today | build/vet/test all clean | measured |

## Relevant Files

1. `cmd/mcp-task-manager/main.go` — the single point where a second transport, signal handling and shutdown must be wired; also the `go:embed` precedent.
2. `internal/project/resolver.go` — decides whether a web server can obtain a `*task.Service` before any MCP tool call; `NewStatic` is the pre-resolved path.
3. `internal/task/service.go` — the exact read API a board can use, and the gaps (relations, bulk blocked, project metadata).
4. `internal/storage/index.go` — unsynchronized maps + rebuild-on-read: the concurrency constraint any HTTP reader hits.
5. `internal/config/config.go` — where a `web:` section, its defaults and an env override attach; `AutoArchiveConfig` is the template.
6. `internal/tools/tools.go` — `Build`/`Register`/`withService`; a non-per-task tool needs more than `withService` gives.
7. `internal/tools/descriptions.go` (+ `internal/tools/descriptions/`) — a new tool without a YAML file panics the package.
8. `internal/cli/cli.go` — the flaggy registration point for `serve web` and the `RunWithArgs` exit-code contract.
9. `internal/cli/commands.go` — `initServiceWithConfig`, the resolver-free service construction the CLI uses today.
10. `internal/cli/output.go` — `FormatTaskDetail` shows exactly which fields a detail view is expected to surface.
11. `internal/task/task.go` — column/badge source data.
12. `internal/tools/tools_test.go` (lines 114-125) — the temp-backlog test helper a web test would mirror.
13. `cmd/mcp-task-manager/main_test.go` — the in-process client/roots test harness, the precedent for testing two transports in one process.
14. `.mcp.json` and `.claude-plugin/marketplace.json` — the dual plugin/project role that breaks dogfooding.
15. `mcp-tasks.yaml` — pins `tasks_dir: tasks` and enables auto-archive.

## Inference

- **`.tasks/task-file-storage/` is leftover attached-file output, not a live task.** Inference because nothing documents it; the evidence is that it lacks the mandatory `{id}/{id}.md` record (only `design.md`, `plan.md`, `research.md`, `task.md`), a task id `task-file-storage` appears nowhere in `tasks/`, and `holdsTasks` would return false for `.tasks/` (`internal/config/config.go:292-313`). Whether deleting it is safe is a decision, not a fact.
- **The `CONNECTION_CLOSED` for `task-manager` in this session is caused by the empty `${CLAUDE_PLUGIN_ROOT}`.** Inference: I reproduced the identical failure mode (`go: chdir : no such file or directory`, immediate exit) by running the command with the variable empty, but I did not observe Claude Code's own spawn.
- **The acceptance criterion's "stray untracked `tasks/.index.json`" has already been resolved by `Index.Load`'s `os.Remove`.** Inference from the file being absent now plus the removal code at `internal/storage/index.go:165-174`; it may reappear only if an older binary is run, which `.gitignore` does not currently guard against.
- **Auto-archive would not move anything on the next startup.** Inference: the only active task (`web-ui-kanban-module`) is `todo`, and candidates are drawn from done tasks only (`internal/task/service.go:838`).

## Unknown

- **Where a `serve web` entry point should obtain the project.** `cli` builds a service directly via `config.Load()` (no roots, `internal/cli/commands.go:21-33`) while MCP mode goes through `project.Resolver`. Whether the web entry point reuses `Resolver` (with a nil roots func) or the CLI path — and therefore whether the MCP server it starts shares that resolution — is undetermined by the code and is a design decision.
- **Whether concurrent access will be handled by locking, by serializing through a single owner, or by accepting the race.** The code has no mechanism today and `CLAUDE.md:330-332` explicitly declares "single-server, no locking" — so the existing documented contract does not cover this task and must be revised or worked around. I could not determine which is intended.
- **How `start_web_ui` should receive the addr/port and the web controller.** `Build`'s signature (`internal/tools/tools.go:18`) has no slot for it and `withService` passes only the service; both call sites would change. Undetermined.
- **Whether the root `.mcp.json` can serve both roles** (plugin-bundled via `${CLAUDE_PLUGIN_ROOT}` and project-scoped) with a single command form, or whether the plugin manifest must point elsewhere. The plugin's `source: "./"` (`.claude-plugin/marketplace.json`) makes the plugin root equal to the repo root when installed from source, but I could not verify what `${CLAUDE_PLUGIN_ROOT}` expands to for a marketplace-installed copy.
- **Whether `mcp-go` v1.0.0 exposes a way to serve stdio with a cancellable context** so both transports can shut down together. `ServeStdio(server, opts...)` (`server/stdio.go:837-859`) was not examined for context-bearing options; `Listen(ctx, in, out)` exists at `stdio.go:859` and appears usable, but I did not verify the exported surface around it.
- **No CI test workflow exists** (`.github/workflows/` holds only `release.yml`), so "quality gates" are enforced locally only; whether the task expects CI to be added is undetermined.