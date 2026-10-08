# Research — web-task-workspace

Phase: research, run 1. Evidence-only: the dashboard as it exists at
`79f070d` plus the uncommitted asset-hash change. No design proposed.

### Task Slice

- `internal/web`: the route table, the four handlers, the view models, the
  templates and the vendored assets — everything the workspace layout, the
  file viewer and its polling would have to grow out of.
- The read side of `internal/task` (`view.go`, `Service.ReadTaskFile`,
  `Service.ListTaskFiles`) and the attached-file rules in
  `internal/storage/files.go`: what a viewer can read and how a bad name fails.
- The current panel/card click and URL mechanics (htmx attributes, `#panel`,
  `hx-push-url`) and what a reload of a pushed URL renders today.
- Escaping and offline guarantees as they are currently stated and tested,
  including the package's explicit "no `template.HTML`" rule.
- The existing tests and maintainer steps a new route set would have to fit
  (`TestNoMutatingRoutes`, `TestNoExternalAssetReferences`, `assets_test.go`,
  `scripts/build-css.sh`).
- Out of slice: MCP tools, git branching, CLI, stats computation.

Documents loaded: `CLAUDE.md` (Web UI, Attached Files, Phase records,
Concurrency, Validation sections), `README.md` (Web Dashboard §81-104,
Refreshing the Vendored CSS §678-700), `tasks/web-task-workspace/task.md`,
`tasks/web-backlog-graph/web-backlog-graph.md`. `_design.md` covers
resolution/`verified_at` only and is not relevant here. There is no `doc/`
directory.

### Confirmed Facts

**Route table and handlers**

- Six GET patterns exist and nothing else: `/{$}`, `/board`, `/tasks/{id}`,
  `/tasks/{id}/panel`, `/static/`, `/healthz`
  (`internal/web/server.go:57-64`). The read-only guarantee is stated as
  structural — only GET patterns are registered, so `ServeMux` answers every
  other method with 405 (`internal/web/server.go:49-53`).
- `Deps.Project` is documented as "reads the current project WITHOUT
  resolving it. Wire this to `Resolver.Current`, never to `Resolver.Get`"
  (`internal/web/server.go:18-23`); both handler entry points go through it
  (`internal/web/handlers.go:56`, `internal/web/handlers.go:69`).
- `DefaultPollSeconds = 5` (`internal/web/server.go:15`) and
  `withDefaults` applies it whenever `PollSeconds <= 0`
  (`internal/web/server.go:43-45`). Neither production call site ever sets
  `PollSeconds` — both pass only `Project` and `Logger`
  (`internal/app/app.go:70`, `internal/app/app.go:110`), so the poll interval
  is effectively the constant 5 and there is no config key for it.
- `render` buffers the whole template output before writing, so a mid-render
  error yields a clean 500 (`internal/web/handlers.go:100-112`). Every
  response is `text/html; charset=utf-8` (`internal/web/handlers.go:109`).
- `/tasks/{id}` 404s for an unknown id (`internal/web/handlers.go:30-36`),
  while `/tasks/{id}/panel` always answers 200 and renders a "gone" state via
  `DetailView{Missing: true}` (`internal/web/handlers.go:42-48`,
  `internal/web/templates/_detail.html:1-5`).
- `detailView` makes a second, full `BoardSnapshot()` call just to label
  relation targets, and only when the task has relation edges
  (`internal/web/handlers.go:78-98`). So one panel render can take the service
  lock twice.

**Layout, panel and URL mechanics as they are now**

- The page shell is `main class="mx-auto max-w-[1600px] px-4 py-5"`
  (`internal/web/templates/layout.html:20`); the board page inside it is a
  two-column grid, content plus a fixed `22rem` aside
  (`internal/web/templates/board.html:2`).
- `#panel` is an `<aside>` **outside** the polled `#board` element
  (`internal/web/templates/board.html:7-11`), and only `#board` polls itself:
  `hx-get="/board" hx-trigger="every {{ .PollSeconds }}s"
  hx-swap="outerHTML"` (`internal/web/templates/_board.html:1`, same on the
  placeholder, `internal/web/templates/_unresolved.html:1`). An open panel
  therefore survives board polls and is itself never refreshed.
- A card click is an `<a href="/tasks/{id}">` carrying
  `hx-get="/tasks/{id}/panel" hx-target="#panel" hx-swap="innerHTML"
  hx-push-url="/tasks/{id}"` (`internal/web/templates/_card.html:2-7`); the
  nested subtask rows repeat the same four attributes
  (`internal/web/templates/_card.html:40-44`).
- Because the pushed URL is `/tasks/{id}`, a reload or deep link of a pushed
  state renders the standalone detail **page** (`max-w-3xl`, its own "back to
  the board" link), not the board with that panel open
  (`internal/web/templates/detail.html:1-7`, `internal/web/handlers.go:30-36`).
  No URL exists today for "board plus open panel".
- `_detail.html` is one fragment used by two surfaces: the htmx panel
  (`internal/web/handlers.go:47`) and the full page
  (`internal/web/templates/detail.html:5`). Any change to its links lands in
  both contexts at once.
- Inside `_detail.html` today: attached files are inert `<li class="chip
  chip-muted">` text, not links (`internal/web/templates/_detail.html:117-124`);
  subtasks, blockers and relation targets are plain full-page `href="/tasks/{id}"`
  links with no htmx attributes (`:49-62`, `:34-47`, `:64-77`); the
  description is a `<pre class="... max-h-96 overflow-auto whitespace-pre-wrap
  ...">` (`:28-32`). There is no "Open workspace" control and no back control
  other than the detail page's own (`internal/web/templates/detail.html:4`).

**Templates and the escaping rule**

- Three template sets: `fragments` (`templates/_*.html`), `boardPage` and
  `detailPage`, each page parsed with the layout plus every fragment
  (`internal/web/templates.go:20-33`).
- The package states a hard rule in its own comment: "There is deliberately no
  safeHTML helper and no template.HTML anywhere in this package — task titles
  and descriptions are user-controlled"
  (`internal/web/templates.go:16-19`). The only `FuncMap` entry is `asset`
  (`internal/web/templates.go:21`, uncommitted change). The task's acceptance
  criteria ask for exactly one pre-trusted HTML fragment (rendered markdown),
  which this comment currently forbids; code and the task document disagree,
  and the code's rule is what is in force today.
- `TestEscaping` feeds `<script>alert(1)</script>` through title, description,
  filename and branch across `/`, `/board`, `/tasks/1`, `/tasks/1/panel` and
  asserts the raw payload never appears
  (`internal/web/handlers_test.go:277-306`).

**Reading files and listing them**

- `Service.ReadTaskFile` locks, requires the task to exist, then delegates
  (`internal/task/service.go:352-360`). `s.get` falls back to the archive
  (`internal/task/service.go:295-307`), so an archived task's files are
  readable — matching the task document's assumption.
- `ReadTaskFile` dereferences `s.fileStorage` with no nil guard
  (`internal/task/service.go:359`), unlike the read-side helper `listFiles`,
  which treats a nil store as "no files" (`internal/task/view.go:136-141`).
- `storage.ReadFile` validates with `checkReserved=false`
  (`internal/storage/files.go:90`): empty/whitespace names, `/` or `\`, and
  exactly `..` are rejected (`internal/storage/files.go:16-26`), so path
  traversal is impossible — but `{id}.md` and `*.phase` **are** readable
  through this path (`internal/storage/files.go:35-45`). A name of `.`
  passes validation and makes `os.ReadFile` fail with a non-`IsNotExist`
  error ("is a directory"), which is not the not-found branch
  (`internal/storage/files.go:99-105`).
- `resolveTaskDir` checks the active directory, then the archive, then errors
  `task not found: <id>` (`internal/storage/files.go:136-144`).
- `ListFiles` excludes only `{id}.md` and skips subdirectories, returning
  `os.ReadDir` order, i.e. lexically sorted
  (`internal/storage/files.go:111-131`). `TaskDetail.Files` additionally
  filters `*.phase` through `task.IsReservedFileName`
  (`internal/task/view.go:173-178`), which normalizes case and trailing dots
  and spaces first (`internal/task/phase.go:77-82`).
- Error values are plain `fmt.Errorf` strings. The only sentinels in the task
  layer are `ErrNoProjectFound` (`internal/task/service.go:15`) and
  `ErrInvalidTokens` (`internal/task/phaseflow.go:22`); nothing distinguishes
  "task missing" from "file missing" from "invalid name" without string
  matching (`internal/storage/files.go:102`,
  `internal/storage/files.go:143`). The 400-vs-404 criterion has no existing
  API to lean on.
- Nothing caps the size of a read: `ReadFile` returns the whole file as a
  string (`internal/storage/files.go:99-106`).
- No composite read method returns "task plus one file's content" in one lock
  acquisition; `view.go`'s composites are `BoardSnapshot` and `Detail`
  (`internal/task/view.go:71-75`, `internal/task/view.go:145-149`).

**Concurrency and index behaviour under polling**

- Every exported query takes the single service mutex and delegates to an
  unlocked twin (`internal/task/view.go:71-75`, `:145-149`,
  `internal/task/service.go:352-355`).
- `Index.Get` and `Index.All` call `syncIfStale` on entry
  (`internal/storage/index.go:265-266`, `internal/storage/index.go:290-291`),
  which rebuilds the whole index when any task file's mtime is newer than
  `builtAt` or the directory count diverges
  (`internal/storage/index.go:202-249`). A poll can therefore trigger a full
  rescan — the stated reason the poll is not one second
  (`internal/web/server.go:12-15`).
- `TestHandlerRaceAgainstWrites` runs `/`, `/board`, `/tasks/1/panel`,
  `/tasks/5`, `/tasks/3` against concurrent `Create`, `Update`,
  `WriteTaskFile`, `StartPhase`, `FinishPhase`
  (`internal/web/race_test.go:52-71`).

**Assets, CSS and JS**

- `staticFS` embeds exactly three files by name, "so a stray source map or
  editor backup can never be shipped"
  (`internal/web/assets.go:11-15`); they are served with
  `Cache-Control: public, max-age=31536000, immutable`
  (`internal/web/assets.go:25-28`) and linked by content hash through the
  `asset` func (`internal/web/assets.go:34-41`,
  `internal/web/templates/layout.html:7-9`) — both uncommitted.
- `app.js` is two IIFEs with document-delegated click listeners and no
  request API; the stats block re-applies its state on `htmx:afterSettle`,
  `htmx:load` and `htmx:historyRestore`
  (`internal/web/static/app.js:37-53`, `:141-159`).
- htmx is pinned at 2.0.4 (`scripts/build-css.sh:17-18`) and the vendored
  bundle contains `hx-preserve`, `hx-select`, `hx-select-oob`, `hx-swap-oob`,
  `hx-push-url`, `hx-sync`, `hx-history`, the `scroll:`/`show:` swap
  modifiers and `focus-scroll` (grep over
  `internal/web/static/htmx.min.js`).
- `input.css` contains **no** `prefers-reduced-motion` rule, and the compiled
  `app.css` has none either (grep). Container queries are already in use for
  the phase lanes (`internal/web/assets/input.css:56-73`), and `--lane` is
  set by a `lane-<phase>` class so Go never computes geometry
  (`internal/web/assets/input.css:50-55`).
- CSS is a maintainer step: `scripts/build-css.sh` runs the pinned Tailwind
  v4.3.3 CLI over `input.css` (`scripts/build-css.sh:19-48`), and `input.css`
  scans only `../templates` via `source(none)` + `@source`
  (`internal/web/assets/input.css:13-15`). Any class assembled in Go must have
  a rule in `input.css` to survive the scanner
  (`internal/web/assets/input.css:5-11`).
- `assets_test.go` pins compiled output: lane classes
  (`internal/web/assets_test.go:15-30`), `.phase-runs`
  (`:35-45`), the stats classes (`:50-69`), and `TestAppJSStatsToggles`,
  which fails if `app.js` ever contains `fetch(`, `XMLHttpRequest`,
  `htmx.ajax`, `hx-` or `htmx-history-cache` (`:74-93`).

**Dependencies and offline posture**

- `go.mod` has three direct dependencies — flaggy, mcp-go, yaml.v3 — on Go
  1.25.5 (`go.mod:1-11`). No markdown or HTML-sanitizer module is present
  anywhere in `go.mod`/`go.sum` (grep for goldmark, blackfriday, gomarkdown,
  bluemonday: no hits), and there is no `vendor/` directory, so a new module
  is fetched from the proxy/module cache rather than committed.
- `TestNoExternalAssetReferences` scans every `src="` and `href="` of `/`
  and `/tasks/1` for `http://`/`https://`
  (`internal/web/handlers_test.go:226-250`) — it does not yet sweep any other
  route.
- `TestNoMutatingRoutes` holds a hardcoded path list (`/`, `/board`,
  `/tasks/1`, `/tasks/1/panel`, `/healthz`, `/static/app.css`), asserts 405
  for POST/PUT/PATCH/DELETE on each, and asserts a full GET sweep leaves the
  tasks directory byte-identical (`internal/web/handlers_test.go:252-275`,
  with `snapshotDir` at `:347-370`). A new route is not covered until it is
  added to that list.
- Test helpers available: `newTestHandler` (handler + service + dir),
  `get`, `seedBoard` (`internal/web/handlers_test.go:28-55`),
  `sectionAfter`/`laneSection` for slicing rendered HTML (`:397-414`),
  `testsupport.NewBacklog`/`Seed`
  (`internal/testsupport/testsupport.go:21-41`, `:58-80`).
- `go test ./internal/web/ ./internal/task/` passes on the current tree.

**Sibling task**

- `web-backlog-graph` is `todo`, `blocked_by: web-task-workspace`, and
  expects to render into "the large viewer area of the task workspace", opened
  both from a task's workspace column and from the board header
  (`tasks/web-backlog-graph/web-backlog-graph.md:1-19`). The viewer area has
  a second consumer before it ships.

### Evidence Map

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Route surface | 6 GET patterns, 405 for everything else by `ServeMux` | `internal/web/server.go:57-64`, `:49-53` |
| Project access | `Deps.Project` → `Resolver.Current`, never `Get` | `internal/web/server.go:18-23`, `internal/web/handlers.go:56,69` |
| Poll interval | constant 5 s; never set by any call site | `internal/web/server.go:15,43-45`, `internal/app/app.go:70,110` |
| What polls | only `#board`, `outerHTML` on itself; `#panel` is outside it | `internal/web/templates/_board.html:1`, `internal/web/templates/board.html:7-11` |
| Card click | `href` + `hx-get .../panel`, `hx-target="#panel"`, `hx-push-url="/tasks/{id}"` | `internal/web/templates/_card.html:2-7,40-44` |
| Pushed URL on reload | renders the standalone detail page, not board+panel | `internal/web/templates/detail.html:1-7`, `internal/web/handlers.go:30-36` |
| Panel/page sharing | one `_detail.html` serves both | `internal/web/handlers.go:47`, `internal/web/templates/detail.html:5` |
| Files in the panel | inert chips; subtasks/relations are full-page links | `internal/web/templates/_detail.html:117-124,49-62,64-77` |
| Trusted HTML | explicitly none; only `asset` in the FuncMap | `internal/web/templates.go:16-21` |
| Escaping coverage | payload swept across 4 routes | `internal/web/handlers_test.go:277-306` |
| File read | locks, task must exist (archive fallback), whole file as string | `internal/task/service.go:352-360,295-307`, `internal/storage/files.go:89-107` |
| Name validation | no empty / `/` / `\` / `..`; `{id}.md` and `*.phase` readable | `internal/storage/files.go:16-26,35-45,90` |
| Error typing | plain strings, no sentinels for missing file vs missing task | `internal/storage/files.go:102,143`, `internal/task/service.go:15` |
| File list order | `os.ReadDir` (lexical), `{id}.md` out, `*.phase` filtered in the view layer | `internal/storage/files.go:111-131`, `internal/task/view.go:173-178` |
| Lock discipline | one mutex, exported→unexported twins, composites per view | `internal/task/view.go:71-75,145-149` |
| Index staleness | `syncIfStale` on `Get`/`All`, full rebuild on mtime/count divergence | `internal/storage/index.go:202-249,265,290` |
| Assets | three files embedded by name, immutable + content-hash URL | `internal/web/assets.go:11-15,25-28,34-41` |
| htmx features present | `hx-preserve`, `hx-select(-oob)`, `hx-swap-oob`, `hx-sync`, `scroll:`/`show:` | grep `internal/web/static/htmx.min.js`; version at `scripts/build-css.sh:17-18` |
| Reduced motion | no rule in `input.css` or `app.css` | grep both files |
| CSS pipeline | maintainer-only script, scans only `../templates` | `scripts/build-css.sh:19-48`, `internal/web/assets/input.css:13-15` |
| CSS/JS pinned by test | class strings and JS contract asserted against compiled output | `internal/web/assets_test.go:15-93` |
| Read-only proof | hardcoded path list + byte-identical directory sweep | `internal/web/handlers_test.go:252-275,347-370` |
| Dependencies | 3 direct modules, no markdown lib, no `vendor/` | `go.mod:1-11`, grep |

### Relevant Files

1. `internal/web/server.go` — the route table and `Deps`; every new route and
   any poll-interval knob lands here.
2. `internal/web/templates/board.html` — the only place the board grid and the
   `22rem` `#panel` live; the strip layout starts from this file.
3. `internal/web/templates/_detail.html` — the panel's markup, shared with the
   full page; the subtask list, file chips and description are here.
4. `internal/web/templates/_card.html` — the established htmx click idiom
   (`hx-get`/`hx-target`/`hx-swap`/`hx-push-url`) the workspace links would
   mirror.
5. `internal/web/handlers.go` — handler shape, the buffered `render`, the
   404-vs-Missing split, and `titles`' second snapshot call.
6. `internal/web/view.go` — the "plain strings only, no `*task.Task` in a
   template" mapping layer a viewer view model must join.
7. `internal/web/templates.go` — the no-`template.HTML` rule and the template
   set wiring any new fragment must be parsed into.
8. `internal/task/service.go:352-372` — `ReadTaskFile` / `ListTaskFiles`: the
   only existing read path for file content, lock behaviour and archive
   fallback.
9. `internal/storage/files.go` — the filename rules that decide what a bad
   name does, and the active/archive resolution.
10. `internal/task/view.go` — where a new composite read method belongs per the
    package-boundary rule.
11. `internal/web/handlers_test.go` — `TestNoMutatingRoutes` and
    `TestNoExternalAssetReferences` path lists, plus the helpers new tests
    would reuse.
12. `internal/web/assets/input.css` + `internal/web/assets_test.go` — where
    new classes must be declared and where a forgotten `build-css.sh` run is
    caught.
13. `internal/web/static/app.js` — the delegated-listener, no-request pattern
    any scroll-preservation or back-control script would have to follow.

### Inference

- **A workspace state cannot be restored from a pushed URL today.** Inference,
  not a tested fact: `hx-push-url="/tasks/{id}"`
  (`internal/web/templates/_card.html:6`) pushes a URL whose GET handler
  renders the standalone detail page (`internal/web/handlers.go:30-36`), and
  no route renders "board + open panel". No test asserts reload behaviour, so
  this is read off the two code paths rather than observed.
- **Preserving viewer scroll across a poll has at least two in-tree
  mechanisms available** — `hx-preserve` and the `scroll:`/`show:` swap
  modifiers are both present in the vendored htmx 2.0.4 bundle (grep). That
  they would behave as needed is an inference from htmx's documented feature
  set, not from any use in this repo: neither attribute appears in any
  template today.
- **A viewer poll would take the service lock at least twice** (once for the
  column's `Detail`, once for `ReadTaskFile`) unless a new composite is added;
  inferred from the absence of any method returning both
  (`internal/task/view.go:71-149`, `internal/task/service.go:352-360`) plus
  the one-lock-per-exported-method discipline.
- **`.` as a filename is a 500 risk.** Inferred from the code path, not
  observed: `validatePathSegment` permits `.`
  (`internal/storage/files.go:16-26`), `os.ReadFile` on a directory fails with
  a non-`IsNotExist` error, and `ReadFile` returns that raw error
  (`internal/storage/files.go:99-105`), which a handler mapping only
  "not found" would turn into a 500. No test covers it.

### Unknown

- Whether adding a module dependency is acceptable in this environment in
  practice: the repo has no `vendor/` directory and `go.mod` has only three
  direct requirements (`go.mod:1-11`), so a markdown library has to be
  resolved through the module proxy at least once. The task document approves
  the dependency in principle but the offline guarantee is currently stated
  only for browser assets (`scripts/build-css.sh:4-7`), not for the Go build.
