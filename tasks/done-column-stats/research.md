# Research: done-column-stats (Done column → configurable statistics cards)

### Task Slice

- **Task type:** web UI and view-layer feature. It touches the board render path (`internal/web`), the read side of `task.Service` (`internal/task/view.go`), the config (`internal/config`), static assets (htmx, `app.js`, Tailwind CSS) and the docs.
- **Covered:** the board render path from the route to the template, including how the Done column is built today and the phase-lane precedent for a column with its own layout.
- **Covered:** the task timestamps (`created_at`, `updated_at`, `closed_at`) and every path that sets or clears them, plus the ordering helpers for the fields cards could group on.
- **Covered:** how the config reaches the service and the web layer, how `applyDefaults` works, how config errors surface, and how yaml.v3 decodes partial sections and lists.
- **Covered:** clock and time-zone handling, and how htmx swaps and settles the board, including what that means for client-side toggle state.
- **Covered:** existing tests and helpers (which tests break when done cards go away, how to seed timestamps), CSS and colour conventions, and the docs that will need updating.

### Confirmed Facts

**Documents loaded**
- I read `CLAUDE.md` in full. The parts that matter here are Web UI (lines 232-247), Configuration (394-440), Concurrency (542-549), Validation (551-560) and Project Structure (448-526).
- I read `README.md`: Web Dashboard (81-103), Configuration (461-493) and Refreshing the Vendored CSS (655-676).
- I read the closure rules in `_design.md` (60-100).
- I checked earlier task artifacts for precedent: `tasks/in-progress-phase-columns/{design,plan}` and `tasks/web-ui-kanban-module/design.md`.
- No file under `docs/` and nothing in `plugins/` mentions the dashboard or the `web:` config, so neither needs updating.

**Execution path: request → snapshot → view model → template**
- **Routes.** Only GET patterns are registered: `GET /{$}` → `board`, `GET /board` → `boardFragment` (the htmx poll target), `/tasks/{id}`, `/tasks/{id}/panel`, `/static/`, `/healthz` (`internal/web/server.go:54-65`). The poll interval is `DefaultPollSeconds = 5` (`server.go:15`).
- **Deps.** `Deps` carries `Project func() (*project.Resolved, bool)` (wired to `Resolver.Current`), `Logger` (stderr), `Now func() time.Time` ("injectable so tests get deterministic 'x ago' strings") and `PollSeconds` (`server.go:18-31`). `withDefaults` sets `Now = time.Now`, i.e. local time (`server.go:40-42`).
  - Production never sets `Now`: `internal/app/app.go:70,110` pass only `Project` and `Logger`.
- **Handlers.** `board` renders `layout.html` with `boardPage` (`handlers.go:15-17`). `boardFragment` renders `_board.html`, or `_unresolved.html` when no project is resolved (`handlers.go:21-28`).
  - `boardView()` calls `resolved.Service.BoardSnapshot()` and then `newBoardView(snap, resolved.Config, h.Now(), h.PollSeconds)` (`handlers.go:55-66`).
  - So the web layer already receives the full resolved `*config.Config`.
  - `render` buffers the whole page and returns a clean 500 on a template error (`handlers.go:102-112`).
- **Service read side.** `Service.BoardSnapshot()` locks `s.mu` and delegates to `boardSnapshot()` (`internal/task/view.go:67-72`).
  - `boardSnapshot` reads `s.index.All()`: every active task, subtasks included, no descriptions, never the archive (`view.go:75`, struct comment `view.go:24-26`).
  - It builds `Subtasks`, `Counts`, `Blocked`, and for in-progress tasks only `Phases` and `PhaseInfo` (at most a few file reads each) (`view.go:86-114`).
  - It sets `TakenAt: time.Now().UTC()` (`view.go:83`). The web layer never reads `TakenAt`; the only references are in `view.go`.
- **The index.** `Index.All()` calls `syncIfStale()`, which rebuilds when any task file's mtime is after `builtAt` or the task count differs, then returns the entries sorted by `compareTaskIDs` (`internal/storage/index.go:202-248`, `290-303`).
  - `IndexEntry` already carries everything the stats need: Status, Priority, Type, ParentID, CreatedAt, UpdatedAt, CreatedBy, Resolution and ClosedAt (`index.go:29-54`, `taskToEntry` 57-76, `entryToTask` 79-100).
  - The archive is never scanned on a board poll.
- **View mapping.** `newBoardView` is pure (`internal/web/view.go:188-254`):
  - It builds a `CardView` for every task, done tasks included (`view.go:195-199`).
  - It nests a subtask under its parent only when both are in the same column (`view.go:204-217`).
  - It loops over the fixed `columns` list — todo "To do", in_progress "In progress", done "Done" (`view.go:175-182`).
  - Each column's `Count` is every task in the snapshot with that status, subtasks included (`view.go:222-227`).
  - `Cards` holds the sorted root cards (`view.go:228-233`).
  - **The Done column has no special handling today: it is built exactly like To do.**
- **The phase-lane precedent.** `ColumnView.Lanes []PhaseLaneView` is set only for in_progress (`view.go:43-46`, `234-236`). `newPhaseLanes` fills it (`view.go:261-275`). The template branches on `{{ if .Lanes }}` (`_board.html:21-43`).
  - Every other column renders `{{ range .Cards }}{{ template "_card.html" . }}{{ else }}<p ...>empty</p>{{ end }}` (`_board.html:38-42`).
  - Lane tests check both the view model and the rendered HTML (see Tests).
- **Templates.**
  - Fragments are parsed with the glob `templates/_*.html`, and both pages are layout + page + `_*.html` (`internal/web/templates.go:19-31`). A new `_something.html` partial is therefore picked up automatically by both template sets.
  - No `FuncMap` is registered, and the comment states there is deliberately no `template.HTML` or safeHTML helper (`templates.go:15-18`).
  - The view models are plain strings and ints; no `*task.Task` reaches a template (`view.go:14-17`).
- **Board grid.**
  - The page is `lg:grid-cols-[minmax(0,1fr)_22rem]`: the board plus a 22rem `#panel` aside (`board.html:2`, `:7`).
  - The board is `md:grid-cols-3 xl:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)]`, so Done gets 1fr at xl (`_board.html:13`).
  - A column is `.column` with `p-2` (`input.css:43-45`). A column header is a status dot, an uppercase title and a count chip (`_board.html:16-20`).
- **Cards.** `_card.html` wraps the content in an `<a>` that does `hx-get="/tasks/{id}/panel"`, `hx-target="#panel"` and `hx-push-url` (`_card.html:2-7`). A done task stays reachable at `/tasks/{id}` (`handlers.go:30-37`). The detail view lists subtasks with their status (`_detail.html:49-60`).

**What the Done column shows today, and what removing it takes away**
- Done cards show priority, type, resolution chip, subtask tally, "updated x ago", creator, and the final-branch chip with a copy button (`_card.html:9-35`, `view.go:337-344` `cardBranch`).
- CLAUDE.md:247 documents the branch chip. Once done cards are gone, a delivered task's final branch is visible only in the detail view's Git block. **Inference:** this follows from the template structure.

**htmx swap and client-side state**
- The vendored htmx is version 2.0.4 (`internal/web/static/htmx.min.js`, string `version:"2.0.4"`; also `scripts/build-css.sh:17-18`).
- `#board` polls itself: `hx-get="/board" hx-trigger="every {{ .PollSeconds }}s" hx-swap="outerHTML"`, with no `hx-select` (`_board.html:1`, same in `_unresolved.html:1`). Every poll therefore replaces the whole board element, including any SVG, with fresh server HTML.
- htmx settle behaviour, from strings in `htmx.min.js`:
  - `attributesToSettle:["class","style","width","height"]` and `defaultSettleDelay:20`.
  - For each new element whose `id` matches an old one, htmx first copies the old element's class, style, width and height onto the new one, then restores the new values after settle (functions `Oe` and the `c.tasks.push(function(){Oe(t,s)})` call).
  - So a class or style set by JS on an id'd element survives about 20 ms, then the server's value wins.
  - `htmx:load` fires per new element after settle; `htmx:afterSwap` and `htmx:afterSettle` events exist; `hx-preserve` is supported.
- htmx already uses the page's `localStorage`: the key `htmx-history-cache` holds up to 10 body snapshots for `hx-push-url` history (`historyCacheSize:10`, `localStorage.getItem("htmx-history-cache")`). Card clicks push URLs (`_card.html:6`).
- **`app.js` today** is one IIFE with one delegated `document` click listener for `[data-copy]`. It calls `preventDefault` and `stopPropagation`, sends no request and has no htmx hooks (`internal/web/static/app.js:1-54`).
- Scripts load with `defer` from `/static/` (`layout.html:8-9`). Every static file, `app.js` included, is served `Cache-Control: public, max-age=31536000, immutable` (`internal/web/assets.go:23-26`).
- The embedded asset list is explicit: `//go:embed static/app.css static/htmx.min.js static/app.js` (`assets.go:12`). A new static file would have to be added there.
- `localStorage` is per origin (scheme, host, port). The listen address can change:
  - In MCP mode, if `config.Load()` fails at startup, `main.go` passes a zero `WebConfig` (`cmd/mcp-task-manager/main.go:27-32`).
  - `NewController` then defaults to `127.0.0.1:0`, a random port (`internal/web/controller.go:30-33`).
  - `start_web_ui` accepts an optional `addr` (`internal/tools/web.go:20-33`).

**CSS, colours and theme**
- **The page is dark-only.**
  - `<html class="h-full bg-neutral-950">` and `<body class="... bg-neutral-950 ... text-neutral-200">` are hard-coded (`layout.html:2`, `:11`).
  - There is no `dark:` variant, no `prefers-color-scheme` and no `color-scheme` anywhere in the templates, `input.css` or `app.css`. The only `@media` rules in `app.css` are `(hover:hover)` and the min-width breakpoints 48, 64 and 80rem.
- **There is no SVG or chart rendering anywhere.** A grep for `svg` across `internal/web` templates, Go files and assets returns nothing. There are no inline `style=` attributes either.
- **Tailwind setup.**
  - Tailwind v4.3.3 standalone builds `assets/input.css` into the committed `static/app.css` (`scripts/build-css.sh:21-25`, `:47`).
  - `input.css` declares `@source "../templates"` (`:12`) and a `@theme` that sets only fonts (`:14-17`).
  - Component classes exist "so Go can name a style without Tailwind having to find that string in a template" (`input.css:6-8`).
- **Colour conventions in use.**
  - Status dots: todo `bg-neutral-500`, in_progress `bg-amber-400`, done `bg-emerald-500` (`input.css:31-34`).
  - Priority chips: critical rose, high amber, medium sky, low neutral (`input.css:24-27`).
  - Blocked is red; live is amber (`input.css:28-29`).
  - Card: `.card` = `rounded-lg border border-white/10 bg-neutral-900/60 p-3` (`input.css:36-41`). Small text: `.meta` = `font-mono text-[11px] text-neutral-500` (`input.css:77`).
- **Colour variables emitted in `app.css`** (Tailwind v4 emits only the ones used): amber-100/200/300/400/500, emerald-500, neutral-100/200/300/400/500/600/900/950, red-300/500, rose-300/500, sky-300/500, white and black.
- **Geometry precedent.** The lane CSS comment says `--lane` comes from a `.lane-<phase>` class "so Go never computes geometry" (`input.css:47-52`).
- **Build-guard tests.** `TestAppCSSDefinesLaneClasses` and `TestAppCSSDefinesPhaseRuns` fail when `app.css` is not rebuilt (`internal/web/assets_test.go:15-45`).

**Service locking (the twin discipline)**
- `Service.mu` is the one lock. Exported methods lock and delegate to an unexported twin and never call each other's exported forms (`internal/task/service.go:117-125`).
- `Config()` is deliberately unlocked because it is write-once (`task/view.go:235-240`).
- **Registration is manual.** `TestServiceNoSelfDeadlock` is a hand-written list of calls (`internal/task/concurrency_test.go:110-157`, with `BoardSnapshot` at :143 and :149). `TestServiceRace` is a hand-written reader and writer list (`concurrency_test.go:60-105`, `BoardSnapshot` at :86). A new exported method has to be added to both lists by hand; no reflection check exists.
- The web-side race test is `TestHandlerRaceAgainstWrites` (`internal/web/race_test.go:19-73`).
- `Service` holds `validTypes []string` (= `cfg.TaskTypes`) and `config *config.Config` (`service.go:104-105`, set in `NewService` 129-141). The service already reads `s.config.AutoArchive` (`service.go:1093-1099`, `1127-1130`).

**Task timestamps and when they change**
- **Fields** (`internal/task/task.go:47-96`):
  - `CreatedAt time.Time` (:56).
  - `CreatedBy string`, empty on tasks that predate the field (:57-60).
  - `UpdatedAt time.Time` (:61).
  - `Resolution` (:63-67).
  - `ClosedAt *time.Time`, documented as "when the task became done; UpdatedAt cannot stand in for it" (:71-74).
  - `VerifiedAt *time.Time` (:75-79).
- **Create** sets `CreatedAt = UpdatedAt = time.Now().UTC()` and stamps `CreatedBy` (`service.go:253-265`).
- **Every `update()` call** (`service.go:440-541`) does the following:
  - A resolution implies `done` (:447-458).
  - It sets `now := time.Now().UTC()` (:488).
  - If the task is closed after the edit, `Resolution` defaults to `completed` when empty (:494-498), and **`ClosedAt` is stamped with `now` if it is nil** (:502-505).
  - Otherwise it is a reopen or an open task: `Resolution`, `ResolutionNote` and `ClosedAt` are all cleared (:506-515).
  - `UpdatedAt = now` always (:532).
  - Consequence: any later `update()` of a legacy done task without `closed_at` (a title edit, a `verified` stamp, a priority change) stamps `ClosedAt` as the time of that edit.
- **Paths that bypass `update()`** bump `UpdatedAt` without touching `ClosedAt`:
  - `AddRelation` (`service.go:916`) and `RemoveRelation` (`:956`).
  - The delete cascade on related tasks (`:582-598`).
  - The archive cleanup on related tasks, via `updateAffectedRelationTasks` (`:1058-1083`).
  - For a legacy done task without `closed_at`, these move the `updated_at` fallback value.
- **`update_task` → done versus `complete_task`.** Both close through `update()`, so `ClosedAt` and `Resolution` are set the same way. `complete_task` (`service.go:742-790`) additionally:
  - refuses unless `in_progress` (for delivered resolutions);
  - cascades a non-delivered resolution to open subtasks, each closed via `update()` with the parent's resolution and a note (`closeSubtasksWith`, `:855-866`), so they get `ClosedAt = now`;
  - auto-completes a branchless parent when the last subtask closes, also via `update()` (`:832-851`);
  - closes open phase runs;
  - is followed by `RunAutoArchive()` in the tool handler (`internal/tools/workflow.go:224-231`).
  - Status changes in the git flows also go through `updateStatus` → `update` (`branching.go:249-254`).
  - On rollback, records are restored as captured (`branching.go:186-200`).
- **Storage format.** Timestamps are written as `2006-01-02T15:04:05Z07:00`, second precision, in the time's own offset (`internal/storage/markdown.go:247`, Save at `:44-89`).
  - On load, `parseTime` tries RFC3339, `...Z` and `2006-01-02`. An unparsable or missing `created_at`/`updated_at` reads as the zero time, and an unparsable optional `closed_at` reads as nil (`markdown.go:218-219`, `261-286`).
  - Times loaded from disk therefore carry UTC or a fixed offset, never `time.Local`.
- **Phase operations.** `start_phase` on an already in-progress task, and `finish_phase`, write only `.phase` files, not the task record (`internal/task/phaseflow.go:145-153`, `187-197`). A phase start on a todo task goes through `startPlain` → `update()`.
- **Auto-archive removes done tasks from the index**, and the stats count the index only:
  - Delivered tasks are archived once `UpdatedAt` is older than `after_days` (default 30).
  - **Non-delivered resolutions** (obsolete, superseded, duplicate, wontfix) **are eligible on the next pass regardless of age** (`service.go:1093-1118`, especially :1105; `_design.md:93-100`).
  - Passes run on `Initialize` (`service.go:190-196`) and after each `complete_task` tool call (`workflow.go:230`).
  - This repository's own config enables auto-archive with `after_days: 30` (`mcp-tasks.yaml:6-8`).
- **Current data in this repo:** 5 active tasks (4 done, all with `closed_at`, 1 in progress) and 87 archived.

**Ordering helpers and enums for groupable fields**
- **Priority.** `Priority.Order()` gives critical 0, high 1, medium 2, low 3, and 99 for anything unknown (`task.go:25-38`). There is no `Priorities()` list function; `IsValidPriority` hard-codes the four values (`task.go:126-132`). The MCP schemas repeat them as string enums (`internal/tools/management.go:32,79,122`).
- **Type.** The order comes from config `task_types` (`config.go:126`, default `["feature","bug"]` at :150). The helpers are `Config.IsValidTaskType` (`config.go:432-439`) and `Service.isValidType` (`service.go:1156-1163`).
  - Types are validated only on create and update (`service.go:216-218`, `485-490`). Loading from disk does not validate, so hand-edited or old types outside the list can appear in the index.
- **Resolution.** `Resolutions()` returns completed, obsolete, superseded, duplicate, wontfix (`task.go:164-172`); `ResolutionStrings()` is at `task.go:192-198`.
  - `Task.EffectiveResolution()` returns "" for an open task and `completed` for a done task with an empty resolution (`task.go:106-114`).
  - `Resolution.Delivered()` is true for "" or completed (`task.go:187-189`).
  - The web layer already uses `EffectiveResolution` (`web/view.go:306`).
- **Status.** The constants todo, in_progress and done are at `task.go:8-12`; `IsValidStatus` is at :117-123. There is no `Statuses()` list function; the only ordered list is the web `columns` var (`web/view.go:175-182`).
- **Phase precedent for an ordered enum.** `task.Phases()` and `Phase.Order()` (`internal/task/phase.go:23`, `:87`).
- **`created_by`** is free text and may be empty (`task.go:57-60`).

**Config**
- **Structs.** `Config{TaskTypes, RelationTypes, AutoArchive, Web, Git, TasksDirName, DataDir, ProjectFound, Resolution}` (`internal/config/config.go:124-136`). `WebConfig{Enabled, Addr, WithMCP}` (`config.go:101-111`). `DefaultConfig()` is at `config.go:147-169`.
- **Import direction.** `config` imports only the standard library and yaml.v3; `task`, `web` and `project` import `config` (`go list` output). Config therefore cannot use the `task` enums such as `Resolutions()` or `Priority`.
- **Load.** `Resolve` starts from `DefaultConfig()`, calls `yaml.Unmarshal(data, cfg)` (not a `Decoder` with `KnownFields`), then `applyDefaults()`, then `applyEnvOverrides()` (`config.go:183-235`).
  - A YAML syntax or type error makes `Resolve` return `parse mcp-tasks.yaml in <root>: ...` (`config.go:205-209`).
- **`applyDefaults`** (`config.go:237-267`):
  - It refills empty `TaskTypes` and `RelationTypes`, non-positive `AfterDays` and a blank `Web.Addr`.
  - It trims `Git.BaseBranches` and falls back to the defaults when the list is empty after trimming.
  - Booleans are left as written.
  - Semantic problems are fixed silently; there is no logging in `config`.
  - `applyEnvOverrides` likewise ignores unparseable bools silently (`config.go:269-287`).
- **Where a config load error lands:**
  - **MCP mode:** `main.go` logs "could not read the config" and continues with a zero web section (`main.go:24-32`). The project is resolved lazily, and every tool call then fails with "could not determine which project to use: ..." (`internal/tools/tools.go:48-55`, `project/resolver.go:117-127`). The board stays on the "No project resolved yet" placeholder, because `Current()` never has a cached project.
  - **`serve web`:** exits 1 with "load config" (`internal/cli/commands.go:602-607`, `internal/app/app.go:100-103`).
  - **CLI commands:** fail through `loadConfig` (`commands.go:16-22`).
  - Other places that log to stderr: `log.Printf` in `OnResolve` (`app.go:205-216`), auto-archive failures (`service.go:1137`), and phase warnings (`phaseflow.go:166`).
- **How config reaches consumers.** `project.Build(cfg)` → `task.NewService(..., cfg.TaskTypes, cfg, ...)` → `Resolved{Config: cfg, Service: svc}` (`internal/project/resolver.go:141-170`). The web layer gets `resolved.Config` per request (`handlers.go:65`).
- **The resolution is cached.** It is re-resolved only after `Invalidate()`, which the roots/list_changed notification triggers (`resolver.go:62-94`, `app.go:218-223`). An edit to `mcp-tasks.yaml` therefore takes effect only after a restart or a roots change.
- **The listener address** comes from a separate, startup-time `config.Load()` (`main.go:27-32`, `app.go:70`), not from the resolved project.
- **yaml.v3 behaviour.** I checked this in a throwaway scratchpad program, not in the repo, against `gopkg.in/yaml.v3 v3.0.1` (`go.mod`):
  - A partial nested mapping (`web: {enabled: true}`, or block style) leaves the other pre-filled fields untouched (Addr kept).
  - A null section (`web:`) also keeps them.
  - A sequence always replaces the slice: `reflect.MakeSlice` in `decode.go:735`. Each element starts from its zero value, so per-entry defaults must be filled after decoding.
  - `cards: []` gives a non-nil empty slice. `cards:` (null) gives a nil slice. A null list item `-` is dropped.
  - Unknown keys such as `bogus:` are ignored silently.
  - A type mismatch such as `days: abc` is a hard unmarshal error, which means a `Resolve` error.
- **Docs vs observed behaviour.** The `applyDefaults` comment says `web: {enabled: true}` "zeroes the address" (`config.go:238-240`, and CLAUDE.md:560 says similar). The experiment above did not reproduce that; I treat the observed behaviour as correct. `applyDefaults` is harmless either way.
- **Config tests** use the helpers `writeConfig` (`resolve_test.go:21-26`), `tempDir` and `isolateEnv` (`config_test.go:537-555`), and `resolveGit`, which shows the per-section helper pattern (`git_test.go:8-23`).
  - Partial-section tests: `TestPartialWebSectionKeepsDefaultAddr` (`web_test.go:25-41`), `TestPartialAutoArchiveKeepsDefaultAfterDays` (`web_test.go:46-62`), `TestGitConfigPartialSectionKeepsDefaults` and `TestGitConfigBaseBranchesTrimmedAndRefilled` (`git_test.go:49-79`).
  - `TestDefaultBaseBranchesNotAliased` (`git_test.go:102`) guards against slice aliasing of package defaults; `DefaultConfig` copies `BaseBranches` (`config.go:164-166`).
- **Reference file.** `mcp-tasks.yaml` currently holds `tasks_dir`, `task_types`, `auto_archive`, `web: {enabled, addr}` and `git` (`mcp-tasks.yaml:1-17`).

**Time and time zones**
- **`task.Service` has no clock injection.** All timestamps use `time.Now().UTC()` (`service.go:253,488,594,916,956,1077,1097`, `view.go:83`, `phaserecord.go:33-35`).
- **No production code converts to local time.** A grep finds no `.Local()`, `.In(` or `time.Local` in non-test code.
  - Cards format times in the time's own location, effectively UTC (`web/view.go:311`, `timeFormat = "2006-01-02 15:04"` at :184).
  - The board footer's `Generated` time uses `now.Format("15:04:05")`, where `now` comes from `Deps.Now` (local by default) (`view.go:192`).
- **`humanizeAgo`** returns "just now" for any negative difference, i.e. a future timestamp (`view.go:504-516`).
- **Web tests** pin `fixedNow = 2026-01-02 15:04:05 UTC` (`handlers_test.go:24`). Tasks seeded through the service get the real `time.Now()`, so in handler tests every seeded timestamp is in the future relative to `Deps.Now`.
- **Task tests** use `at(h)` = 2026-10-04 h:00 UTC for phase records (`task/view_test.go:335`).
- No test sets `TZ` or `time.Local`.

**Tests and helpers**
- **`testsupport.NewBacklog`** builds a temp-dir project with a `config.Config` literal: `Web: config.DefaultConfig().Web`, no `applyDefaults` (`internal/testsupport/testsupport.go:21-42`). It returns a static resolver, the service and the tasks dir.
- **`testsupport.Seed`** creates tasks through `svc.Create` and sets status through `svc.Update` (todo → in_progress → done) (`testsupport.go:59-106`). It cannot set `created_at` or `closed_at`.
  - Specific timestamps in web tests need a direct record rewrite, as `setBranch` does: `storage.NewMarkdownStorage(dir).Load`, edit, `Save`, then the index resyncs by mtime (`internal/web/branch_test.go:20-36`).
- **Task-package tests** can put tasks into the mock maps directly (`ms.tasks[id] = &Task{...}`, `service_test.go:1810`; backdating at `:1698`).
  - The mocks come from `newConcurrentService()` (`concurrency_test.go:17-27`), whose config has no `Web` section.
  - `mockIndex.All()` returns tasks in map order, i.e. unsorted (`service_test.go:185-191`).
- **Web view tests** call `newBoardView(boardOf(...), nil, fixedNow, 5)` with a **nil config** (`internal/web/view_test.go:25-47`, `74-90` and throughout). `newProjectView` already handles a nil cfg (`view.go:283-294`).
- **Existing tests that assume done cards are rendered on the board:**
  - `TestBoardRendersThreeColumns` expects card "Old chore", task 4, which `seedBoard` makes done (`handlers_test.go:44-53`, `55-78`).
  - `TestBoardShowsBranchChip` expects `data-copy="dev/4-old-chore"` on `/board` and exactly 2 `copy-btn`, one of them on done task 4 (`branch_test.go:69-90`).
  - `TestCopyButtonHasNoHtmxAttributes` requires a copy button on `/` and `/board` from done task 4 (`branch_test.go:111-128`).
  - `TestBoardEmptyInProgressShowsFourStubs` expects exactly one `>empty</p>`, "Done only" (`handlers_test.go:467-483`).
  - `TestInProgressHasFourPhaseLanes` asserts Done has no `Lanes` (`view_test.go:87-91`).
  - `TestLanesPartitionTheColumn` includes a done task (`view_test.go:177-213`).
- **Read-only and offline guards:**
  - `TestNoMutatingRoutes` sends non-GET requests (405) and checks a GET sweep leaves the tasks dir byte-identical (`handlers_test.go:247-270`).
  - `TestNoExternalAssetReferences` scans only `src="` and `href="` values for `http(s)://` (`handlers_test.go:221-245`).
  - `TestEscaping` (`handlers_test.go:272-302`) and `TestUnresolvedProjectPlaceholder` (`:304-324`).
- **Baseline on this machine (macOS, go1.27.1; `go.mod` says `go 1.25.5`):** `go vet ./...` is clean, `gofmt -l` is clean, and `go test ./...` passes in every package. `go test -race` on `internal/web` and `internal/config`, and on `TestServiceRace|TestServiceNoSelfDeadlock|TestBoard` in `internal/task`, also passes.
  - The only GitHub workflow is `release.yml`; no CI runs tests.
  - The previous web task recorded the two branch-chip tests as flaky on Windows (`tasks/in-progress-phase-columns/plan:85`).

**Docs that describe the affected behaviour**
- **CLAUDE.md:** Web UI bullets (232-247: Phase lanes 243, Card and detail content 244 including "Todo and done cards never read phase records", Archived 245, Branch chip 247); Configuration YAML block and partial-section paragraph (394-440); Validation "Config:" line (560); Project Structure web and task entries (448-526); Concurrency (542-549).
- **README:** Web Dashboard bullets (81-103); Config File YAML and partial-section paragraph (461-490); Refreshing the Vendored CSS (655-676).
  - Doc drift: `README.md:492` lists the `relation_types` default without `superseded_by`; the code default has four entries (`config.go:141`).

### Evidence Map

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Routes / read-only | GET-only `ServeMux` | `internal/web/server.go:54-65` |
| Board data source | `BoardSnapshot` over `index.All()`, one lock | `internal/task/view.go:67-115` |
| Done column build | Same as To do: cards plus count; no special case | `internal/web/view.go:219-238` |
| Custom column layout precedent | `ColumnView.Lanes` for in_progress plus `{{ if .Lanes }}` | `view.go:43-46,234-236,261-275`; `_board.html:21-43` |
| Column count | Every task with that status, subtasks included | `view.go:222-227` |
| Poll / swap | `every 5s`, `hx-swap="outerHTML"` on `#board`, no `hx-select` | `_board.html:1`; `server.go:15` |
| Settle resets JS-set attributes on id'd elements | `attributesToSettle` class/style/width/height, 20 ms | `static/htmx.min.js` (2.0.4) |
| Existing localStorage use | htmx history cache key `htmx-history-cache` | `static/htmx.min.js` |
| Client JS | One delegated click listener, no htmx hooks | `static/app.js:37-53` |
| Static caching | Immutable, explicit embed list | `assets.go:12,24` |
| Theme | Dark only, no light mode | `layout.html:2,11`; `input.css` |
| SVG / charts | None | grep, no hits |
| Template escaping | No FuncMap, no `template.HTML` | `templates.go:15-18` |
| `closed_at` set/cleared | In `update()`: stamped if nil when closed, cleared on reopen | `service.go:490-515` |
| `updated_at` bumps outside `update()` | Relation add/remove, delete and archive cascades | `service.go:594,916,956,1077` |
| Resolution default | `EffectiveResolution()` → completed | `task.go:106-114` |
| Priority order | `Priority.Order()` (unknown = 99) | `task.go:25-38` |
| Resolution order | `Resolutions()` | `task.go:164-172` |
| Type order | `cfg.TaskTypes` | `config.go:126,150` |
| Auto-archive shrinks index | Delivered after `after_days`; non-delivered immediately | `service.go:1093-1118` |
| Config defaults | `applyDefaults` after Unmarshal | `config.go:205-267` |
| Config error | Hard `Resolve` error, which breaks all tools and the board | `config.go:206-208`; `tools.go:48-55` |
| Config reaches web | `resolved.Config` → `newBoardView` | `handlers.go:65`; `resolver.go:141-170` |
| Clock | Service: `time.Now().UTC()`; web: `Deps.Now` | `service.go:488`; `server.go:27-28,40-42` |
| Twin discipline registration | Manual lists in two tests | `concurrency_test.go:60-105,110-157` |
| Test backlog | `NewBacklog` config literal, `Seed` via service | `testsupport.go:21-106` |
| Direct record edit in tests | `setBranch` pattern | `branch_test.go:20-36` |

### Relevant Files

1. `internal/web/view.go`: holds the view models and `newBoardView`; Done-column handling, the `Lanes` precedent and the nil-config handling all live here.
2. `internal/web/templates/_board.html`: the column loop, the lane branch, the `empty` fallback and the htmx poll attributes.
3. `internal/task/view.go`: `BoardSnapshot` is where a new or extended service view method belongs, under the one lock.
4. `internal/task/task.go`: timestamp fields, `EffectiveResolution`, `Priority.Order`, `Resolutions()`.
5. `internal/task/service.go` (440-541, 742-866, 1093-1142): when `closed_at` and `updated_at` move; cascades; auto-archive.
6. `internal/config/config.go`: `WebConfig`, `DefaultConfig`, `Resolve`, `applyDefaults`; error surfacing.
7. `internal/web/static/app.js` and `internal/web/assets/input.css`: the delegated-listener pattern and the CSS/colour conventions; the dark-only theme.
8. `internal/web/handlers_test.go`, `view_test.go`, `branch_test.go`, `assets_test.go`: the tests that will break and the patterns to follow.
9. `internal/task/concurrency_test.go`: where a new exported service method has to be registered.
10. `internal/testsupport/testsupport.go`: the backlog and seeding helpers and their limits (no timestamps).
11. `internal/config/web_test.go`, `git_test.go`: the patterns for config tests.
12. `CLAUDE.md` (232-247, 394-440, 560) and `README.md` (81-103, 461-492): the docs to update.

### Inference

- **Done column width.** From the grid classes (`board.html:2`, `_board.html:13`, `input.css:43-45`, `layout.html:13,20`), I estimate the usable width inside the Done column at roughly 180-220px at md/lg, about 195px at a 1280px viewport and about 275px at 1600px and above; below md it spans the full width. These are arithmetic estimates, not measurements.
- **Re-applying toggles after a poll.** Because the poll replaces `#board` wholesale and htmx settle reverts class and style on id'd elements, toggle state applied before the swap is lost or reverted. It has to be re-applied on an htmx event that fires after settle (`htmx:afterSettle` or `htmx:load`) or rendered so that settle does not touch it. The detail-panel swap (`#panel`) fires the same events. This is inferred from the minified htmx code, not observed in a browser.
- **History restore.** The htmx history cache stores body HTML snapshots, toggled DOM state included, in `localStorage`; a back-navigation restore would bring back the DOM as it was at snapshot time. Inferred from the htmx code and not tested.
- **Tailwind scan scope.** `build-css.sh:8-10` and README 671-676 say Tailwind scans every non-gitignored file under the directory it runs from, in addition to `@source "../templates"`. When run from the repo root, class strings in Go files would probably be picked up too, which contradicts the narrower claim at `input.css:6-8`. The script does not `cd`, so the result depends on the caller's working directory.
- **SVG in `html/template`.** Inline `<svg>` should escape cleanly: unknown attributes such as `points`, `d` and `viewBox` are plain-text context (Go stdlib `html/template/attr.go` `attrType`).
  - A `data-*` attribute whose remainder starts with `on`, or contains `src`, `uri` or `url`, is treated as JS or URL context.
  - `style` attributes are CSS context.
  - `<title>` is treated as RCDATA everywhere, inside SVG too (`html/template/transition.go:680`), so text tooltips there are escaped.
  - From reading the standard library; not exercised in this repo.
- **Branch-chip test flakiness.** The flaky branch-chip tests on Windows probably come from the mtime-based staleness check after a direct record write (`index.go:213-248` plus `setBranch`). Tests that rewrite timestamps the same way could inherit it.

### Unknown

- **Light mode.** The acceptance criteria require the charts to be "legible in light and dark mode", but the dashboard has no light theme at all (`layout.html:2,11`; no `prefers-color-scheme` anywhere). The design has to decide whether that means "on the existing dark board" or adding a light theme.
- **Which "now" and which time zone.** The service has no clock and stamps UTC. The web layer has an injectable `Deps.Now` that defaults to local time, and web tests use `fixedNow` in January 2026 while seeded tasks carry real October timestamps. Where the "last 24 h" reference point comes from, how "server local time" day buckets are made deterministic in tests (no TZ is pinned), and whether the service gains a clock are all open.
- **Values outside the domain enums.** Hand edits or config changes can leave types outside `task_types`, unknown priority strings (`Order()` = 99) or unknown resolution strings in the index, since loading does not validate (`markdown.go:221-245`). Their placement in a bars card is unspecified.
- **Auto-archive interaction.** With `auto_archive.enabled`, non-delivered resolutions leave the index on the next pass, and delivered tasks leave after `after_days` counted from `updated_at`. A resolution card would then rarely show non-completed values, and a `lines` window longer than `after_days` would undercount closures. The task's "active index only" rule accepts this, but nothing says whether to document it or warn about it.
- **Legacy `closed_at`.** For done tasks without `closed_at`, any later `update()` stamps `closed_at` at the time of the edit (`service.go:502-505`), and relation changes bump `updated_at` (the fallback). Either can make an old task look "closed today". How much this matters depends on the data; this repo currently has no done task without `closed_at`.
- **Invalid config entries.** A hard error (YAML type errors already behave this way) blocks every MCP tool call, the CLI and `serve web`, not just the board. A skip-and-log approach has no precedent beyond silent fallbacks (`applyDefaults`, env bools) and `log.Printf` in `OnResolve` and auto-archive. Unknown YAML keys are ignored silently today. The design has to choose.
- **`cards: []`.** It decodes to a non-nil empty slice and `cards:` to nil, so the config can tell "no cards" apart from "absent" only by checking for nil, unlike the `len(...) == 0` checks `applyDefaults` uses now.
- **Config reload.** Card config changes take effect only after a restart or a roots change, because the resolution is cached (`resolver.go:62-94`). Whether that is acceptable for "easy to define in the config" is not stated.
