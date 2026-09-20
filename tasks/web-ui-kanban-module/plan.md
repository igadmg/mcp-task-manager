# PLAN — Web UI module (htmx/tailwind kanban) alongside MCP

Repo: `/Users/iga/Workspace/mcp-task-manager` (module `github.com/gpayer/mcp-task-manager`). All paths absolute-relative to that root. Every command below is run from the repo root with `GOWORK=off`.

---

## Planning Summary

The accepted design decomposes into **12 commits**. The ordering is driven by four hard constraints:

1. **The lock lands first.** `internal/task` is the most heavily tested package in the repo and the mutex refactor touches 21 internal cross-calls. It must land alone, proven by `go test -race`, before `internal/web`, `internal/app` or the new view API can depend on `Service`.
2. **Bottom-up dependency flow.** `task` (lock) → `task` (view API) → `testsupport` → `config` → `project` → assets → `web` → `tools` → `app` → `cli` → docs.
3. **Two atomicity rules that cannot be violated.** (a) `internal/tools/descriptions/start_web_ui.yaml` must be in the *same commit* as the tool registration or `descriptions.go`'s package-init `panic` fires and **every** `internal/tools` test dies. (b) `cmd/mcp-task-manager/icon.png` + `instructions.md` must move in the *same commit* as the code that embeds them, because `go:embed` cannot reference `../`.
4. **Dogfooding first.** Phase 0 is pure repo config with zero Go impact, and it restores the project's own `task-manager` MCP server (currently `CONNECTION_CLOSED`) for the remaining 11 phases. Highest value-per-risk in the whole plan.

**No code generation exists in this project.** The only build-time input coupling is `go:embed`; `scripts/build-css.sh` is a manual maintainer step never wired into `go build`/`go generate`/`go test`. Phase 6 (vendored assets) is therefore the analogue of a "regeneration input" phase and is deliberately isolated: assets only, no Go code.

**Every phase boundary must be green:** `GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...`.

---

## Plan-time corrections

Four items where the design is under-specified or where a literal reading creates an ordering problem. None is a redesign; each is a mechanical resolution the implementer should adopt.

**C1 — `internal/testsupport` cannot be used by `internal/task` or `internal/project` tests (import cycle).**
Verified: all existing test files are *in-package* (`package task`, `package tools`, `package cli`, `package config`). `testsupport` imports `internal/task` and `internal/project`, so `package task`'s own tests importing it would cycle. **Resolution:** `internal/task/concurrency_test.go` (Phase 1) builds its own temp backlog inline — copy the 10-line construction from `internal/tools/tools_test.go:114-125`. `testsupport` (Phase 3) serves `internal/web`, `internal/app` and `internal/tools` only. This also means Phase 1 must precede Phase 3 rather than depend on it.

**C2 — `tools.Build`'s new `WebStarter` parameter lands one phase before its only real implementation.**
Phase 8 changes the signature while `cmd/mcp-task-manager/main.go` is still the caller; Phase 9 moves that caller into `internal/app` and swaps `nil` for the controller. **Resolution:** Phase 8 passes `nil` explicitly at both `main.go` call sites (`main.go:69` `tools.Register`, `main.go:78` `tools.Build` inside `OnResolve`). `Build` returns the set without `start_web_ui` when `web == nil`, which is exactly the documented contract and is test-covered by `TestBuildOmitsStartWebUIWhenNil`. Phase 9 then flips both to the real controller in one edit. **Do not skip the `OnResolve` closure** — see Risk R2.

**C3 — `cmd/mcp-task-manager/main_test.go` relocation would break the tree mid-move if done naively.**
The file is `package main` and uses `newServer()`, which moves to `internal/app` in Phase 9. **Resolution:** in Phase 9, `git mv cmd/mcp-task-manager/main_test.go internal/app/app_test.go` **in the same commit** as the `newServer` move, change `package main` → `package app`, drop the now-unnecessary `newServer` qualification, and delete nothing else. `isolateEnv` (`main_test.go:32-38`) is *lifted to* `testsupport.IsolateEnv` in Phase 3; in Phase 9 the relocated test calls `testsupport.IsolateEnv(t)` and the local copy is deleted. `rootsHandler`, `textOf`, `taskTypeEnum` (`main_test.go:17-30,149,161`) move verbatim into `internal/app/app_test.go` and stay package-local. Result: `cmd/mcp-task-manager/` ends the phase with `main.go` only, and no phase boundary has an orphaned test.

**C4 — `mcp-tasks.yaml`'s `web:` section is listed under "docs" in the design but must land with the config code.**
Adding `web: {enabled: false, addr: 127.0.0.1:7777}` to the repo's own config before `WebConfig` exists is harmless (unknown YAML keys are ignored by `yaml.Unmarshal` into a struct), but adding it *with* the config phase makes the `applyDefaults` behavior observable in the repo itself. **Resolution:** `mcp-tasks.yaml` changes in Phase 4, not Phase 11. `enabled: false` means this commit opens no port.

---

## Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| P0 | Restore dogfooding; repo hygiene | `.mcp.json`, `plugins/mcp-task-manager/.mcp.json`, `.claude-plugin/*`, `.gitignore`, `.tasks/` | none | — | real `list_tasks` tool call returns `web-ui-kanban-module` |
| P1 | One `sync.Mutex` on `task.Service` + twin discipline | `internal/task/service.go`, `internal/task/concurrency_test.go` | none | — | `go test -race ./internal/task/` |
| P2 | Service view API; close every boundary leak | `internal/task/view.go`, `internal/task/service.go`, `internal/tools/management.go` | none | P1 | `go test ./internal/task/ ./internal/tools/` |
| P3 | Shared test helper package | `internal/testsupport/testsupport.go` | none | P2 | `go build ./... && go test ./...` |
| P4 | `web:` config section + `applyDefaults` | `internal/config/config.go`, `config_test.go`, `mcp-tasks.yaml` | none | — | `go test ./internal/config/` |
| P5 | `project.Build` + `project.Current`; collapse CLI duplicate | `internal/project/resolver.go`, `internal/cli/commands.go` | none | — | `go test ./internal/project/ ./internal/cli/` |
| P6 | Vendor htmx + Tailwind output; CSS refresh script | `internal/web/static/*`, `internal/web/assets/input.css`, `scripts/build-css.sh`, `.gitignore` | manual (never in build) | — | files exist, non-empty, no network refs |
| P7 | `internal/web`: handler, controller, templates, views | `internal/web/**` | none | P2,P3,P4,P5,P6 | `go test ./internal/web/` + `-race` |
| P8 | `WebStarter` + `start_web_ui` tool (+ its YAML) | `internal/tools/{tools,web}.go`, `descriptions/start_web_ui.yaml`, `cmd/.../main.go` | none | P7 | `go test ./internal/tools/` |
| P9 | `internal/app` composition root; cancellable stdio | `internal/app/**`, `cmd/mcp-task-manager/main.go` | none | P7,P8 | `go test -race ./internal/app/` |
| P10 | `serve web` CLI entry point | `internal/cli/{cli,commands}.go`, `cli_test.go` | none | P9 | `go test ./internal/cli/` + manual browser |
| P11 | Docs: CLAUDE.md, README.md | `CLAUDE.md`, `README.md` | none | P10 | full gate + manual validation script |

---

## Phase Details

### P0 — Dogfooding & repo hygiene

- **Goal:** the `task-manager` MCP server starts from a plain `claude` session in this repo, and the repo's stray state is resolved.
- **Why this phase exists / position:** zero Go impact, zero risk to the build, and it restores the project's own task tooling for phases 1–11 (the server is currently `CONNECTION_CLOSED` because `${CLAUDE_PLUGIN_ROOT}` expands to empty for a project-scoped server — measured failure `go: chdir : no such file or directory`). Independent of all Go work; deliberately first.
- **Files likely to change:**
  - `.mcp.json` → project scope only:
    ```json
    { "mcpServers": { "task-manager": {
        "command": "go",
        "args": ["run", "./cmd/mcp-task-manager"],
        "env": { "GOWORK": "off" } } } }
    ```
    No `-C`, no variables (Claude Code launches project servers with the repo as cwd and exports `CLAUDE_PROJECT_DIR`).
  - `plugins/mcp-task-manager/.mcp.json` → `{ "mcpServers": { "task-manager": { "command": "mcp-task-manager" } } }`.
  - **New** `plugins/mcp-task-manager/.claude-plugin/plugin.json` — copy of `.claude-plugin/plugin.json`, version `1.5.0`.
  - `.claude-plugin/marketplace.json` — `"source": "./"` → `"source": "./plugins/mcp-task-manager"`, version `1.5.0`.
  - `.claude-plugin/plugin.json` — version `1.5.0`.
  - `.gitignore` — append `.index.json`, `.current_task`, `/.cache/` (each with the design's one-line comment).
  - `.tasks/task-file-storage/` — **see Open Decision D2.** Default: `git mv .tasks/task-file-storage docs/history/task-file-storage`, then remove the empty `.tasks/`. Do **not** gitignore `.tasks/` (it is the default tasks-dir name for other projects).
- **Regeneration required:** none.
- **Dependencies:** none.
- **Implementation tasks:** edit the five JSON/ignore files; perform the `git mv`; verify `GOWORK=off go run ./cmd/mcp-task-manager version` still prints `tasks=/Users/iga/Workspace/mcp-task-manager/tasks`.
- **Tests and checks:**
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...
  GOWORK=off go run ./cmd/mcp-task-manager version   # expect root=<repo> tasks=<repo>/tasks
  GOWORK=off go run ./cmd/mcp-task-manager list      # expect the web-ui-kanban-module row
  ```
  Then **restart the Claude session and make a real `list_tasks` MCP tool call** — the acceptance criterion says "verified by an actual tool call, not by inspection alone". Expect `web-ui-kanban-module`.
- **Commit boundary:** `fix(dogfood): split project-scoped and plugin MCP configs, tidy repo state`
- **Definition of done:** MCP server connects; `list_tasks` returns the live task; `git status` shows no stray `.index.json` / `.current_task`.
- **Rollback note:** pure `git revert`. Nothing downstream depends on it; reverting only re-breaks the MCP server. Fully reversible.
- **Risk flags:** `marketplace.json`'s `source` change means already-installed copies of the plugin need the version bump to pick it up (hence 1.4.2 → 1.5.0). The `git mv` of `.tasks/` is the only irreversible-feeling step and it is a tracked move, recoverable from git.

---

### P1 — One mutex on `task.Service`

- **Goal:** every task operation is serialized by a single `sync.Mutex`, with no self-deadlock, proven under `-race`.
- **Why this phase exists / position:** **foundational and mandated to land alone.** `internal/task` is the most heavily tested package; the change is mechanical but touches 21 internal cross-call sites. It must be proven in isolation *before* anything new (the view API, the web handlers, `internal/app`) depends on the service. It also fixes a pre-existing latent bug: mcp-go's stdio server dispatches tool calls across **5 workers**, so two concurrent `create_task` calls already race on `Index.entries` and can allocate the same `NextID()`.
- **Files likely to change:**
  - `internal/task/service.go` — add the field + discipline comment; split 7 methods.
  - **New** `internal/task/concurrency_test.go`.
- **Regeneration required:** none.
- **Dependencies:** none (can be done in parallel with P0, P4, P5, P6).
- **Implementation tasks:**
  1. Add to `Service` (verbatim comment from the design):
     ```go
     // mu serializes every task operation. It is the only lock over the
     // index and the markdown storage, both of which are reachable solely
     // through this type.
     //
     // DISCIPLINE: exported methods lock once on entry and delegate to an
     // unexported, unlocked twin. Service methods NEVER call each other's
     // exported forms — Go mutexes are not reentrant. When adding a method,
     // add it in this shape or TestServiceNoSelfDeadlock will fail.
     mu sync.Mutex
     ```
  2. Create unexported unlocked twins for the seven methods with internal callers, and repoint every internal call site:

     | Exported (locks, delegates) | Unlocked twin | Internal call sites in `service.go` |
     |---|---|---|
     | `Get` | `get` | 248, 271, 280, 329, 427, 462, 502, 549, 651, 657, 691 |
     | `Create` | `create` | 227 (`CreateSubtask`) |
     | `Update` | `update` | 528, 535, 586, 606, 624 |
     | `IsBlocked` | `isBlocked` | 512 |
     | `ArchiveTask` | `archiveTask` | 869 |
     | `GetAutoArchiveCandidates` | `autoArchiveCandidates` | 863 |
     | `RunAutoArchive` | `runAutoArchive` | 148 (`Initialize`) |
  3. Add unlocked forms `list`, `getWithSubtasks`, `subtaskCounts` (the exported forms become `mu.Lock(); defer mu.Unlock(); return <twin>(...)`). P2's composites will call these.
  4. Every remaining exported method that touches `index`/`storage` gets the one-line lock prologue.
  5. **Do not lock** `EnsureProjectExists`, `ProjectFound` — they read only `s.config`, which is write-once (assigned only in `NewService`, `service.go:101-109`). Add a comment saying so, so nobody "fixes" it.
  6. `closeSubtasksWith` and `updateAffectedRelationTasks` are already unexported and stay unlocked — no rename.
  7. `Initialize()` locks and calls `runAutoArchive` (unlocked twin).
- **Tests and checks:**
  - **New** `internal/task/concurrency_test.go`:
    - `TestServiceRace` — 8 readers (`List`, `Get`, `IsBlocked`, `GetWithSubtasks`) and 4 writers (`Create`, `Update`, `StartTask`, `CompleteTask`, `AddRelation`) × 200 iterations over a `t.TempDir()` backlog seeded with ~30 tasks, subtasks and `blocked_by` edges. (Reader set is extended to `BoardSnapshot`/`Detail`/`Relations` in P2.) **This must fail loudly on pre-P1 code** — run it once against `HEAD~` to confirm it has teeth.
    - `TestServiceNoSelfDeadlock` — calls every exported method once inside a `time.AfterFunc(10*time.Second, func(){ panic("deadlock") })` watchdog. Permanent cheap guard for the twin discipline.
    - Build the backlog **inline** (see correction C1) — copy the construction from `internal/tools/tools_test.go:114-125`; `internal/testsupport` would be an import cycle here.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./...
  GOWORK=off go test -race -count=3 ./internal/task/
  GOWORK=off go test -race -run 'TestServiceRace|TestServiceNoSelfDeadlock' ./internal/task/
  ```
  Pass = no `WARNING: DATA RACE`, no watchdog panic, all pre-existing `internal/task` tests unchanged and green.
- **Commit boundary:** `fix(task): serialize all task operations behind a single service mutex`
- **Definition of done:** `-race` clean at `-count=3`; the pre-existing `internal/task` suite is untouched and green; no exported signature changed.
- **Rollback note:** `git revert` is clean — it is a single-package, additive-plus-mechanical change with no external API impact. **But** P2, P7 and P9 assume the twins exist; reverting P1 after P2 requires reverting P2 too.
- **Risk flags:** the *only* real risk is a missed internal call site that still targets an exported form → immediate self-deadlock. `TestServiceNoSelfDeadlock` catches the single-method case; for the composite paths, grep-verify after the edit: `grep -n 's\.\(Get\|Create\|Update\|IsBlocked\|ArchiveTask\|RunAutoArchive\|GetAutoArchiveCandidates\)(' internal/task/service.go` must return **zero** hits.

---

### P2 — Service view API (`internal/task/view.go`)

- **Goal:** `*task.Service` exposes everything the board needs, so no other package ever touches `internal/storage`.
- **Why this phase exists / position:** it is the user's binding constraint ("work with tasks lives in one internal package"). It must follow P1 because every new method is a composite built out of the unlocked twins. It must precede P7 — **the web module cannot be built before this API exists.**
- **Files likely to change:**
  - **New** `internal/task/view.go`
  - `internal/task/service.go` — add the `Config()` getter (no lock; write-once field).
  - `internal/tools/management.go` — refactor `list_tasks`' per-task `IsBlocked` loop (`:394-398`) onto `svc.BlockedMap`.
  - `internal/task/concurrency_test.go` — extend `TestServiceRace`'s reader set with `BoardSnapshot`, `Detail`, `Relations`.
- **Regeneration required:** none.
- **Dependencies:** P1.
- **Implementation tasks:**
  1. Types in `view.go`: `SubtaskCount{Total, Done int}`, `BoardSnapshot{Tasks []*Task; Subtasks map[string][]*Task; Blocked map[string][]BlockingInfo; Counts map[string]SubtaskCount; TakenAt time.Time}`, `TaskDetail{Task *Task; Archived bool; Subtasks []*Task; Blocked bool; Blockers []BlockingInfo; Relations []RelationEdge; Files []string}`.
  2. Methods (each: lock once, delegate to an unexported composite that calls only unlocked twins):
     ```go
     func (s *Service) BoardSnapshot() (*BoardSnapshot, error)
     func (s *Service) Detail(id string) (*TaskDetail, error)
     func (s *Service) Relations(taskID string) []RelationEdge
     func (s *Service) BlockedMap(ids []string) map[string][]BlockingInfo
     func (s *Service) Config() *config.Config // no lock: write-once
     ```
  3. `boardSnapshot` = **one** `syncIfStale` and one pass: all active tasks (top-level + subtasks, no descriptions), `Subtasks` grouped by parent, `Counts`, `Blocked` from `blockedMap`. `TakenAt` from `time.Now()`.
  4. `relations` wraps `index.GetRelationsForTask` (`internal/storage/index.go:572-590`) — this is the previously unreachable source∪target edge list.
  5. `blockedMap(ids)` — one sync, one pass over `relationsBySource`. Semantics identical to looping `IsBlocked` (only blockers whose status != `done`).
  6. `detail(id)` — full `Get` (with description, archive fallback); `Archived` from `archiveStorage.IsArchived(id)`; when archived, return **empty** `Subtasks`/`Relations`/`Blockers` **with a doc comment stating this is deliberate** (the active index genuinely does not hold them) while still returning `Files`.
  7. Refactor `internal/tools/management.go:394-398` to a single `svc.BlockedMap(ids)` call. Behavior must be byte-identical in the tool output.
  8. **Explicitly not added:** an `InProgress()` accessor, any watch/notify channel.
- **Tests and checks:**
  - **New** in `internal/task` (in-package): `TestBoardSnapshotShape`, `TestBoardSnapshotSubtaskGrouping`, `TestBlockedMapMatchesIsBlocked` (equivalence against the old per-task loop over a seeded backlog — this is the refactor's safety net), `TestDetailActiveTask`, `TestDetailArchivedTaskHasEmptyDerivedData`, `TestRelationsIncludesIncomingEdges`.
  - Extended `TestServiceRace` reader set.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./...
  GOWORK=off go test -race ./internal/task/
  GOWORK=off go test ./internal/tools/    # existing list_tasks tests must pass UNCHANGED
  ```
- **Commit boundary:** `feat(task): add board/detail view API and batch blocked lookup`
- **Definition of done:** the five methods exist and are locked; `internal/tools`' existing `list_tasks` tests pass without edits; no package outside `internal/task` imports `internal/storage` (verify: `grep -rn 'internal/storage' --include='*.go' internal cmd | grep -v '^internal/storage\|^internal/task\|^internal/project'` → only `internal/cli/commands.go` until P5 removes it).
- **Rollback note:** revertible as one commit, but the `management.go` refactor reverts with it — acceptable since it restores the previous working loop. P7 depends on this irreversibly (the web module has no other data source).
- **Risk flags:** `BlockedMap` must reproduce `IsBlocked`'s exact semantics or `list_tasks` silently changes behavior — `TestBlockedMapMatchesIsBlocked` is the guard and is not optional. The archived-detail empty-data contract is easy to mistake for a bug later; the doc comment and the test name carry the intent.

---

### P3 — `internal/testsupport`

- **Goal:** one shared test-backlog helper so P7/P8/P9 do not copy `newTestService` three times.
- **Why this phase exists / position:** small, additive, and unblocks three later test suites. Placed after P2 so `Seed` can be exercised against the final Service API. **Cannot** be used by `internal/task` or `internal/project` tests (import cycle — correction C1).
- **Files likely to change:** **New** `internal/testsupport/testsupport.go`.
- **Regeneration required:** none.
- **Dependencies:** P2.
- **Implementation tasks:**
  ```go
  package testsupport
  func NewBacklog(t *testing.T) (*project.Resolver, *task.Service, string) // t.TempDir → storage → index → Service → Initialize → project.NewStatic
  type TaskSpec struct{ ID, Title, Status, Priority, Type, ParentID string; BlockedBy []string }
  func Seed(t *testing.T, svc *task.Service, specs ...TaskSpec)
  func IsolateEnv(t *testing.T) // clears MCP_TASKS_DIR/MCP_PROJECT_DIR/CLAUDE_PROJECT_DIR/MCP_ROOT_SOURCE
  ```
  - `NewBacklog` is the generalization of `internal/tools/tools_test.go:114-125`.
  - `IsolateEnv` is lifted from `cmd/mcp-task-manager/main_test.go:32-38` (`t.Setenv(name, "")` then `os.Unsetenv(name)` — keep both lines, the pattern matters).
  - It is a normal (non-`_test.go`) package importing `testing`, the way `net/http/httptest` does. It will be reported as reachable from non-test code by nothing, since no non-test file imports it.
- **Tests and checks:** no tests of its own; it is validated by consumers. To prove it compiles and works now, **refactor `internal/tools/tools_test.go`'s `newTestService` onto `testsupport.NewBacklog` in this same commit** — that is the phase's independent verification.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...
  ```
- **Commit boundary:** `test: add internal/testsupport backlog helpers`
- **Definition of done:** `internal/tools` tests pass using the shared helper; `go vet` clean.
- **Rollback note:** trivially revertible until P7 lands. After P7/P9 it is load-bearing for their test suites.
- **Risk flags:** none material. Watch that `Seed` does not depend on auto-increment id ordering (use explicit custom ids in specs) or tests become order-sensitive.

---

### P4 — `web:` config section and `applyDefaults`

- **Goal:** config carries `Web{Enabled, Addr, WithMCP}` with correct defaults, env overrides, and no partial-section zeroing.
- **Why this phase exists / position:** P7 needs `DefaultWebAddr` and `WebConfig`; P9/P10 need the enable flag. Independent of the `task` work — can be done in parallel with P1/P2.
- **Files likely to change:** `internal/config/config.go`, `internal/config/config_test.go`, `mcp-tasks.yaml` (correction C4).
- **Regeneration required:** none.
- **Dependencies:** none.
- **Implementation tasks:**
  1. Constants: `EnvWebEnabled = "MCP_WEB_ENABLED"`, `EnvWebAddr = "MCP_WEB_ADDR"` (next to the existing four at `config.go:13-26`), `DefaultWebAddr = "127.0.0.1:7777"`.
  2. `type WebConfig struct { Enabled bool \`yaml:"enabled"\`; Addr string \`yaml:"addr"\`; WithMCP bool \`yaml:"with_mcp"\` }`; added to `Config` as a **value** field `Web WebConfig \`yaml:"web"\`` (mirrors `AutoArchive`, `config.go:82-92`).
  3. `DefaultConfig()` gains `Web: WebConfig{Enabled: false, Addr: DefaultWebAddr, WithMCP: false}`.
  4. **New** `func (c *Config) applyDefaults()` called immediately after the `yaml.Unmarshal` in `Resolve` (`config.go:~152`):
     ```go
     d := DefaultConfig()
     if len(c.TaskTypes) == 0        { c.TaskTypes = d.TaskTypes }
     if len(c.RelationTypes) == 0    { c.RelationTypes = d.RelationTypes }
     if c.AutoArchive.AfterDays <= 0 { c.AutoArchive.AfterDays = d.AutoArchive.AfterDays }
     if strings.TrimSpace(c.Web.Addr) == "" { c.Web.Addr = d.Web.Addr }
     ```
     Booleans are intentionally left as-written. This also repairs the pre-existing `auto_archive: {enabled: true}` → `AfterDays: 0` trap.
  5. Env overrides after `applyDefaults`, orthogonal — `MCP_WEB_ADDR` sets the address but **never** enables; `MCP_WEB_ENABLED` is parsed with `strconv.ParseBool` and ignored when unparseable.
  6. `mcp-tasks.yaml` gains `web:\n  enabled: false\n  addr: 127.0.0.1:7777`.
- **Tests and checks:**
  - **New** in `internal/config/config_test.go`: `TestWebDefaults`, `TestPartialWebSectionKeepsDefaultAddr`, `TestPartialAutoArchiveKeepsDefaultAfterDays` (regression for the pre-existing trap), `TestEnvWebOverrides`, `TestEnvWebEnabledInvalidIgnored`.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./internal/config/ && GOWORK=off go test ./...
  ```
- **Commit boundary:** `feat(config): add web section and normalize partially specified sections`
- **Definition of done:** the five new tests pass; existing config/resolve tests pass unchanged; `go run ./cmd/mcp-task-manager version` output is unchanged.
- **Rollback note:** clean revert; P7/P9/P10 depend on it.
- **Risk flags:** `applyDefaults` changes *existing* behavior for partially specified `auto_archive` sections. Any existing test asserting `AfterDays == 0` for such input will now fail — that is the trap being fixed, so update the assertion, do not weaken `applyDefaults`. Also confirm `applyDefaults` runs for **both** `Resolve` and `Load` paths (check `config.go:130-156` for whether `Load` delegates to `Resolve`; if not, call it in both).

---

### P5 — `project.Build` + `project.Current`

- **Goal:** one construction site for a project, and a non-resolving accessor for the HTTP layer.
- **Why this phase exists / position:** P7's `web.Deps.Project` is wired to `Current`; P9/P10's `serve web` needs `Build`. Independent of `task`/`config` work.
- **Files likely to change:** `internal/project/resolver.go`, `internal/cli/commands.go`.
- **Regeneration required:** none.
- **Dependencies:** none (P4 is unrelated; do not couple them).
- **Implementation tasks:**
  1. Extract the body of `Resolver.resolve` (`resolver.go:101-121`) into:
     ```go
     // Build constructs the storage, index and task service for an already
     // loaded config, running Initialize. It is the single construction site
     // for a project: Resolver.resolve and the CLI both go through it.
     func Build(cfg *config.Config) (*Resolved, error)
     ```
     `resolve` becomes `config.Resolve(provider)` + `Build(cfg)`.
  2. Add:
     ```go
     // Current returns the cached resolution without resolving. It is how
     // consumers that must not cause side effects (the HTTP handlers) read
     // the project: resolution runs Initialize(), which migrates the layout
     // and may auto-archive, and must only ever be triggered by an MCP tool
     // call or by an explicit CLI startup.
     func (r *Resolver) Current() (*Resolved, bool)
     ```
     Guarded by the existing `r.mu`; returns `(nil, false)` after `Invalidate()`.
  3. `internal/cli/commands.go::initServiceWithConfig` collapses to a wrapper: `resolved, err := project.Build(cfg); return resolved.Service, err`. New import direction `cli → project`, acyclic (`project` imports only `config`, `storage`, `task`).
- **Tests and checks:**
  - **New** in `internal/project/resolver_test.go`: `TestCurrentReturnsFalseBeforeResolve`, `TestCurrentReturnsResolvedAfterGet`, `TestCurrentFalseAfterInvalidate`, `TestBuildIsEquivalentToResolve`.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./internal/project/ ./internal/cli/ && GOWORK=off go test ./...
  ```
  The entire 732-line `internal/cli` suite must pass **unchanged** — that is the proof the `initServiceWithConfig` collapse is behavior-preserving.
- **Commit boundary:** `refactor(project): export Build and add non-resolving Current accessor`
- **Definition of done:** `internal/cli` suite green unchanged; `internal/cli/commands.go` no longer imports `internal/storage`.
- **Rollback note:** clean revert; P7 and P9/P10 depend on it.
- **Risk flags:** `Current()` must **not** trigger resolution under any code path — a well-meaning "resolve if not yet resolved" would reinstate the exact hazard the design removes (a GET running layout migration and auto-archive). P7's `TestUnresolvedProjectPlaceholder` is the end-to-end guard; add a `Current` doc comment saying so.

---

### P6 — Vendored web assets

- **Goal:** `internal/web/static/{app.css,htmx.min.js}` exist and are committed, so every later `go build` that embeds them succeeds.
- **Why this phase exists / position:** **must precede P7** — `//go:embed static/app.css static/htmx.min.js` is a compile-time requirement; a missing file is a build failure, not a runtime one. Isolated from Go code (the design's analogue of "never mix a generation-input change with logic"). Independent of everything else; can land any time before P7.
- **Files likely to change:**
  - **New** `internal/web/static/htmx.min.js` — htmx **2.0.4** minified, one-time download from `https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js`, committed (~48 KB).
  - **New** `internal/web/static/app.css` — Tailwind build output, committed.
  - **New** `internal/web/assets/input.css` — `@import "tailwindcss"; @source "../templates";` plus a few `@theme` tokens.
  - **New** `scripts/build-css.sh` — downloads the **pinned** Tailwind standalone CLI release into `.cache/` if absent, runs it over `assets/input.css` → `static/app.css`. Never invoked by `go build`/`go generate`/`go test`.
  - `.gitignore` — `/.cache/` (already added in P0; confirm).
- **Regeneration required:** **manual only.** `scripts/build-css.sh` is a maintainer step. A clean checkout builds and tests with zero network access because both outputs are committed.
- **Dependencies:** none. **Open Decision D1 applies here** (exact Tailwind CLI version to pin).
- **Implementation tasks:** download both assets; write `input.css` and the script with a header comment recording the pinned Tailwind version and the htmx version; run the script once to produce the first `app.css`. Templates do not exist yet, so the first `app.css` will be a near-empty Tailwind base — **that is fine**; P7 re-runs the script after writing the templates and commits the regenerated `app.css` as part of P7. Note this explicitly in the script header.
- **Tests and checks:** no Go code changes, so the gate is trivially green. Verify by hand:
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...
  test -s internal/web/static/app.css && test -s internal/web/static/htmx.min.js && echo ok
  grep -c 'https\?://' internal/web/static/app.css   # expect 0 (no @import of remote fonts)
  bash scripts/build-css.sh                          # must succeed and be idempotent
  ```
- **Commit boundary:** `chore(web): vendor htmx 2.0.4 and the Tailwind build pipeline`
- **Definition of done:** both assets committed and non-empty; `build-css.sh` runs offline once `.cache/` is warm; `.gitignore` does **not** exclude `*.css`/`*.js`.
- **Rollback note:** clean revert while P7 has not landed. After P7, reverting breaks `go build` for the whole module — **this is the first irreversible-in-practice coupling**.
- **Risk flags:** a `.gitignore` rule matching `*.css` or `*.js` would silently leave the assets untracked and break CI/fresh clones — explicitly verify with `git check-ignore -v internal/web/static/app.css` (expect no match) before committing. Tailwind v4's standalone CLI download URL and flags differ from v3 — pin and record the exact version.

---

### P7 — `internal/web`

- **Goal:** a complete, tested, read-only kanban handler and its lifecycle controller.
- **Why this phase exists / position:** the largest phase, and the point where the feature becomes real. It depends on **all five** of P2 (view API), P3 (testsupport), P4 (`DefaultWebAddr`), P5 (`Current`), P6 (embedded assets). It deliberately does **not** touch `internal/tools`, `internal/app` or `cmd/` — nothing wires it in yet, so it lands as a self-contained, independently testable package.
- **Files likely to change (all new):**
  - `internal/web/controller.go`, `server.go`, `handlers.go`, `view.go`, `templates.go`, `assets.go`
  - `internal/web/templates/{layout,board,_board,_card,detail,_detail,_unresolved}.html`
  - `internal/web/{handlers,view,controller,race}_test.go`
  - `internal/web/static/app.css` — **regenerated** via `scripts/build-css.sh` once the templates exist, and re-committed in this phase.
- **Regeneration required:** manual `bash scripts/build-css.sh` after the templates are written (Tailwind scans `@source "../templates"`). This is the one place the manual step is mandatory; include the regenerated `app.css` in this commit.
- **Dependencies:** P2, P3, P4, P5, P6.
- **Implementation tasks:**
  1. **Deps & handler:**
     ```go
     type Deps struct {
         Project     func() (*project.Resolved, bool) // non-resolving; wired to resolver.Current
         Logger      *log.Logger                      // default log.New(os.Stderr, "web: ", log.LstdFlags)
         Now         func() time.Time
         PollSeconds int                              // default 5
     }
     func NewHandler(d Deps) http.Handler
     ```
     There is **no context-taking resolution path reachable from a handler** — this is structural, not conventional.
  2. **Routes** (`http.ServeMux`, Go 1.22 method patterns; only `GET` is ever registered, so `ServeMux` returns 405 for everything else by itself):
     | Pattern | Handler | Renders |
     |---|---|---|
     | `GET /` | `board` | `board.html` (full page) |
     | `GET /board` | `boardFragment` | `_board.html` — htmx poll target |
     | `GET /tasks/{id}` | `detail` | `detail.html` (deep-linkable) |
     | `GET /tasks/{id}/panel` | `detailPanel` | `_detail.html` — htmx swap target |
     | `GET /static/` | `http.StripPrefix` + `http.FileServerFS(staticFS)` + `Cache-Control: public, max-age=31536000, immutable` | embedded |
     | `GET /healthz` | `health` | `text/plain; charset=utf-8` → `ok` |
  3. **Unresolved path:** when `Project()` returns `false`, render `_unresolved.html` with **HTTP 200** and the same `hx-get="/board" hx-trigger="every 5s"` poll so it self-heals. Fragment routes return the same fragment (200) so htmx swaps cleanly.
  4. **View models** (`view.go`) exactly as the design specifies — `BoardView`, `ColumnView`, `CardView`, `BlockerView`, `DangerItem`, `ProjectView`, `DetailView`, `RelationView`; **never** `*task.Task` in a template. Two pure mapping functions:
     ```go
     func newBoardView(snap *task.BoardSnapshot, cfg *config.Config, now time.Time, poll int) BoardView
     func newDetailView(d *task.TaskDetail, now time.Time) DetailView
     ```
  5. **Sorting within a column:** `sort.SliceStable` by `task.Priority.Order()` ascending, then `CreatedAt` ascending (older first), then id order. Columns in fixed enum order `todo`, `in_progress`, `done`. (The index sorts by id, *not* priority — the board must sort itself.)
  6. **Subtask placement:** a card goes in the column of **its own** status; it nests inside its parent's card **only when the parent is in the same column**, otherwise it renders standalone with `IsSubtask` and a `↳ #parent` chip linking to the parent detail.
  7. **Danger zone:** derived by filtering the snapshot to `StatusInProgress` (no new Service method). Amber banner above the columns + amber ring on in-progress cards and the column header. Omitted entirely when empty.
  8. **Archived tasks:** absent from the board (active index only); the detail route serves them with an "Archived — read-only; relations and subtasks are not indexed" banner.
  9. **Escaping:** every field is a plain `string` rendered `{{ . }}`. **No** `template.HTML`, no Markdown renderer, no `safeHTML` funcMap entry. Description rendered in `<pre class="whitespace-pre-wrap">`.
  10. **Rendering:** templates parsed once at package init; each response renders into a `bytes.Buffer` first so a mid-render error yields 500 rather than a torn page.
  11. **Missing task between snapshot and detail:** render a "task no longer exists — refresh" panel, not a bare 500. Unknown id on `/tasks/{id}` → **404**.
  12. **Controller:**
      ```go
      func NewController(d Deps, defaultAddr string) *Controller
      func (c *Controller) Start(addr string) (url string, already bool, err error) // satisfies tools.WebStarter
      func (c *Controller) URL() (string, bool)
      func (c *Controller) Shutdown(ctx context.Context) error // idempotent
      ```
      Own `sync.Mutex`; if `c.ln != nil`, `Start` returns `(c.url, true, nil)` without touching the network — never an error, never a double bind. Binds the listener **synchronously** before spawning `Serve`, so a port conflict is a startup error and `:0` yields a real URL. `http.Server.ErrorLog = Deps.Logger` (stderr).
  13. **Logging:** no `fmt.Print*`, no `os.Stdout` anywhere in `internal/web`. Verify: `grep -rn 'fmt.Print\|os.Stdout' internal/web` → zero hits.
  14. **Embeds** (files listed individually so no stray map/cache file ships):
      ```go
      //go:embed templates/*.html
      var templateFS embed.FS
      //go:embed static/app.css static/htmx.min.js
      var staticFS embed.FS
      ```
- **Tests and checks (all new):**
  - `handlers_test.go`: `TestBoardRendersThreeColumns`, `TestBoardCardBadges`, `TestDetailPage`, `TestDetailPanelIsFragment` (asserts no `<html>`), `TestDetailUnknownID404`, `TestDetailArchivedTask`, `TestNoMutatingRoutes` (405 for POST/PUT/PATCH/DELETE on every route **and** a byte-identical tasks directory — recursive listing + mtimes before/after a full GET sweep), `TestEscaping` (`<script>alert(1)</script>` as title, description and attached filename; assert the literal never appears unescaped), `TestUnresolvedProjectPlaceholder` (**resolver whose roots func calls `t.Fatal` if invoked** — proves GETs never resolve), `TestInvalidateShowsPlaceholderAgain`, `TestNoExternalAssetReferences` (fails if any `src=`/`href=` starts with `http://`/`https://`), `TestStaticAssetsServed`.
  - `view_test.go`: priority-then-created-at ordering, subtask nesting rule (same column vs cross column), danger-zone set, archived detail shape.
  - `controller_test.go`: `TestControllerStartIdempotent` (two `Start`s → same URL, `already==true`, one listener), `TestControllerStartPortZero`, `TestControllerShutdown` (dial fails afterwards), `TestShutdownIdempotent`.
  - `race_test.go`: `TestHandlerRaceAgainstWrites` — `httptest.NewServer(web.NewHandler(...))`, 4 HTTP getters on `/` and `/board` while 2 goroutines mutate through the same `*task.Service`.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./internal/web/ && GOWORK=off go test ./...
  GOWORK=off go test -race -count=2 ./internal/web/
  grep -rn 'template.HTML\|fmt.Print\|os.Stdout' internal/web   # expect zero
  ```
- **Commit boundary:** `feat(web): read-only htmx kanban dashboard package`
- **Definition of done:** all web tests green; `-race` clean; no stdout writes; no external asset references; `TestUnresolvedProjectPlaceholder` proves handlers cannot resolve.
- **Rollback note:** revertible as a single additive package while nothing imports it (i.e. before P8/P9). After P9 wires it, a revert must take P8 and P9 with it.
- **Risk flags:**
  - Largest diff in the plan; if it feels unwieldy, the **only** acceptable split is `7a` = `view.go` + `view_test.go` (pure mapping, no HTTP) and `7b` = handlers/controller/templates/assets. Both halves are independently green. Do not split the templates from the handlers.
  - `app.css` must be regenerated **after** the templates exist or Tailwind emits no utility classes and the board renders unstyled.
  - The board poll takes the global service lock; if the tasks directory is stale it triggers a full `Rebuild` under the lock. Keep the poll at **5 s**, not 1 s.

---

### P8 — `WebStarter` interface and the `start_web_ui` tool

- **Goal:** an MCP tool that starts the dashboard on demand, idempotently.
- **Why this phase exists / position:** after P7 so the interface has a real implementation to be checked against (`var _ tools.WebStarter = (*web.Controller)(nil)` — placed in `internal/app` in P9, since `tools` must **not** import `web`). Before P9 so that P9's wiring is a one-line swap.
- **Files likely to change:**
  - `internal/tools/tools.go` — the interface + both signatures.
  - **New** `internal/tools/web.go` — `webTools`, `startWebUIHandler`.
  - **New** `internal/tools/descriptions/start_web_ui.yaml` — **MANDATORY IN THIS COMMIT** (absent ⇒ `textFor` panics at package init and the whole `internal/tools` suite dies).
  - `internal/tools/tools_test.go` — four new tests.
  - `cmd/mcp-task-manager/main.go` — both call sites pass `nil` (correction C2): `main.go:69` (`tools.Register`) and `main.go:78` (`tools.Build` inside `OnResolve`).
- **Regeneration required:** none.
- **Dependencies:** P7.
- **Implementation tasks:**
  1. ```go
     // WebStarter starts the read-only web dashboard on demand. A nil WebStarter
     // omits start_web_ui from the tool set (CLI-built sets and tests pass nil).
     type WebStarter interface {
         Start(addr string) (url string, already bool, err error)
         URL() (url string, running bool)
     }
     func Build(rs *project.Resolver, validTypes, relationTypes []string, web WebStarter) []server.ServerTool
     func Register(s *server.MCPServer, rs *project.Resolver, validTypes, relationTypes []string, web WebStarter)
     ```
     `Build` appends `webTools(rs, web)`, which returns `nil` when `web == nil`.
  2. Handler wraps `withService` **unchanged** — the `svc` argument is deliberately discarded; `withService`'s resolution side effect is the point (it guarantees `Current()` succeeds for the web handlers before the listener accepts anything):
     ```go
     func startWebUIHandler(rs *project.Resolver, web WebStarter) server.ToolHandlerFunc {
         return withService(rs, func(ctx context.Context, _ *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
             url, already, err := web.Start(req.GetString("addr", ""))
             if err != nil { return mcp.NewToolResultError("could not start the web dashboard: " + err.Error()), nil }
             if already   { return mcp.NewToolResultText("Task dashboard already running: " + url), nil }
             return mcp.NewToolResultText("Task dashboard started: " + url + " (read-only)"), nil
         })
     }
     ```
  3. Schema: `mcp.NewTool("start_web_ui", mcp.WithDescription(text.Description), mcp.WithString("addr", mcp.Description(text.param("addr"))))`.
  4. `descriptions/start_web_ui.yaml` — use the design's text verbatim (`description:` + `params.addr:`). Schema is `{description: string, params: map[string]string}`.
- **Tests and checks (all new in `internal/tools/tools_test.go`):**
  - `TestStartWebUIIdempotent` — fake `WebStarter` recording calls; second call reports "already running", one `Start` on the fake returns `already=true` and the same URL.
  - `TestBuildOmitsStartWebUIWhenNil`.
  - `TestSetToolsRepublicationKeepsStartWebUI` — build with a starter, simulate the `OnResolve` re-`Build`, assert `start_web_ui` survives. **This is the guard for the silent-drop hazard.**
  - `TestEveryToolHasDescriptions` — iterate `Build(...)` names and assert a YAML file exists for each, converting the package-init panic trap into a permanent test failure.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./internal/tools/ && GOWORK=off go test ./...
  ls internal/tools/descriptions/start_web_ui.yaml   # must exist in THIS commit
  ```
- **Commit boundary:** `feat(tools): add start_web_ui tool behind a WebStarter interface`
- **Definition of done:** the four tests pass; `internal/tools` does **not** import `internal/web` (`grep -rn 'internal/web' internal/tools` → zero); the tool is absent from the default (`nil`) set.
- **Rollback note:** clean revert; P9 depends on the signature.
- **Risk flags:** **the YAML file.** If it is forgotten or misnamed, `internal/tools` panics at package init and the failure looks unrelated to the change. Second: forgetting the `nil` at `main.go:78` inside `OnResolve` produces a compile error here (good) but in P9 the equivalent mistake is *silent* — see R2.

---

### P9 — `internal/app` composition root

- **Goal:** one process, two transports, one signal context, clean shutdown.
- **Why this phase exists / position:** it is the wiring commit, so it comes after both things it wires (P7, P8). It is also where the `go:embed` assets move — and that move must be atomic with the code that embeds them.
- **Files likely to change:**
  - **New** `internal/app/app.go` — `Options`, `RunMCP`, `RunWeb`, `newServer` (moved verbatim from `main.go:45-90`), `toolSet` (moved from `main.go:92-115`).
  - **New (moved)** `internal/app/icon.png`, `internal/app/instructions.md` — `git mv` from `cmd/mcp-task-manager/`. **Same commit as the embed directives**; `go:embed` cannot cross `../`.
  - **New (moved)** `internal/app/app_test.go` — `git mv cmd/mcp-task-manager/main_test.go`, `package main` → `package app` (correction C3).
  - `cmd/mcp-task-manager/main.go` — shrinks to mode selection + signal context.
- **Regeneration required:** none.
- **Dependencies:** P7, P8. Also P4 (`WebConfig`) and P5 (`Build`, `Current`).
- **Implementation tasks:**
  1. Move `newServer`, `toolSet`, `newToolSet`, `matches`, `set`, and the two `//go:embed` directives + their assets into `internal/app`. `newServer` gains a parameter:
     ```go
     func newServer(web tools.WebStarter) (*server.MCPServer, *project.Resolver)
     ```
     and passes `web` to **both** `tools.Register` and the `tools.Build` call inside the `OnResolve` closure.
  2. ```go
     type Options struct {
         Web    config.WebConfig // effective settings after flag/env overrides
         Stderr io.Writer        // log sink; never Stdout
     }
     func RunMCP(ctx context.Context, opts Options) error
     func RunWeb(ctx context.Context, opts Options) error
     ```
  3. **`RunMCP`:** lazy `project.NewResolver(rootsFunc)` (unchanged behavior). Build `web.NewController(web.Deps{Project: resolver.Current, ...}, opts.Web.Addr)`. If `opts.Web.Enabled`, `Start("")` — **a web start failure is logged to stderr and swallowed**, the dashboard is optional and must not kill the MCP server. Then:
     ```go
     stdio := server.NewStdioServer(srv)
     stdio.SetErrorLogger(log.New(os.Stderr, "", log.LstdFlags))
     err := stdio.Listen(ctx, os.Stdin, os.Stdout)
     ```
     replacing `server.ServeStdio` (whose own hidden `signal.Notify` would compete with ours and whose context is unreachable).
  4. **`RunWeb`:** eager resolution — `cfg, err := config.Load()` → `resolved, err := project.Build(cfg)` → `rs := project.NewStatic(resolved)`. A resolution or bind failure is a **startup error** (non-zero exit), not a runtime surprise. Bind synchronously before spawning `Serve`. When `opts.Web.WithMCP`, additionally attach the stdio transport over the *same* static resolver via `newServer`.
  5. **Coordination:** plain `sync.WaitGroup` + a buffered error channel — **no `golang.org/x/sync`**. Whichever transport returns first cancels a derived context; the other is shut down (`Controller.Shutdown` with a 5 s deadline); `Run*` returns the first non-nil, non-`http.ErrServerClosed` error.
  6. Add the compile-time assertion here (the only place that may import both): `var _ tools.WebStarter = (*web.Controller)(nil)`.
  7. `cmd/mcp-task-manager/main.go` becomes:
     ```go
     ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
     defer stop()
     if len(os.Args) > 1 { cli.RunWithContext(ctx, ...) /* or cli.Run() until P10 */ ; return }
     cfg, _ := config.Load()   // for opts.Web only; failure → zero-value Web, log to stderr
     if err := app.RunMCP(ctx, app.Options{Web: cfg.Web, Stderr: os.Stderr}); err != nil { log.Fatalf(...) }
     ```
     Until P10 lands, keep `cli.Run()` as-is on the CLI branch.
- **Tests and checks:**
  - `internal/app/app_test.go` keeps the relocated `TestServer_ResolvesProjectFromRoots` plus `rootsHandler`, `textOf`, `taskTypeEnum`; `isolateEnv` is replaced by `testsupport.IsolateEnv`.
  - **New:** `TestRunWebStartsAndShutsDown` (ctx cancel → returns nil, port closed), `TestRunMCPStartsWebWhenEnabled`, `TestRunMCPDoesNotStartWebByDefault`, `TestRunMCPWebSharesResolution` (a tool call creates a task → the board fragment shows it — proves the single-service sharing), `TestStdioListenStopsOnContextCancel`.
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./... && GOWORK=off go test -race ./internal/app/ ./internal/web/ ./internal/task/
  ls cmd/mcp-task-manager/   # expect exactly: main.go
  GOWORK=off go run ./cmd/mcp-task-manager version   # unchanged output
  ```
  Manual: `MCP_WEB_ENABLED=1 GOWORK=off go run ./cmd/mcp-task-manager` (stdin a TTY), then `curl -s localhost:7777/healthz` → `ok`, and `curl -s localhost:7777/` → the **placeholder page** (no MCP client has resolved yet). Ctrl-C must exit cleanly.
- **Commit boundary:** `feat(app): add composition root running MCP and web in one process`
- **Definition of done:** `cmd/mcp-task-manager/` contains only `main.go`; the relocated test suite is green; `-race` clean; Ctrl-C shuts both transports down; `start_web_ui` survives a `SetTools` re-publication (P8's test, re-run).
- **Rollback note:** the largest revert surface — it moves binary assets and a test file. `git revert` handles the moves, but **verify `icon.png` and `instructions.md` return to `cmd/mcp-task-manager/` and `go build` passes** after any revert. Treat this as the **point of no return** for the file layout: after it, the three later phases assume `internal/app` exists.
- **Risk flags:**
  - **R2 (the silent one):** if the `OnResolve` closure calls `tools.Build(resolver, cfg.TaskTypes, cfg.RelationTypes, nil)` instead of passing the captured `web`, `start_web_ui` disappears from the tool list the moment the resolved project configures non-default task types — with no error anywhere. `TestSetToolsRepublicationKeepsStartWebUI` must be re-run at this boundary, ideally extended to drive it through `app.newServer`.
  - Replacing `server.ServeStdio` removes mcp-go's built-in SIGINT/SIGTERM handling; if `signal.NotifyContext` is not installed, Ctrl-C regresses silently.
  - `go:embed` cannot cross `../` — if the assets are not moved in this same commit, `go build` fails immediately (loud, therefore low risk).

---

### P10 — `serve web` CLI entry point

- **Goal:** `mcp-task-manager serve web` runs the dashboard in the foreground and shuts down cleanly.
- **Why this phase exists / position:** it is the first way a human can see the board without an MCP client; it needs `app.RunWeb` from P9. Isolated from P9 so the 732-line CLI suite is exercised on its own commit.
- **Files likely to change:** `internal/cli/cli.go`, `internal/cli/commands.go`, `internal/cli/cli_test.go`, `cmd/mcp-task-manager/main.go` (CLI branch → `cli.RunWithContext(ctx, ...)`).
- **Regeneration required:** none.
- **Dependencies:** P9.
- **Implementation tasks:**
  1. `RunWithArgs(args, stdout, stderr) int` **keeps its exact signature** (732 lines of tests depend on it) and becomes `return RunWithContext(context.Background(), args, stdout, stderr)`. `Run()` builds the signal context and calls `RunWithContext`.
  2. Flaggy registration, mirroring `cli.go:44-183`:
     ```go
     serveCmd := flaggy.NewSubcommand("serve")
     serveCmd.Description = "Run a server in the foreground"
     webCmd := flaggy.NewSubcommand("web")
     webCmd.Description = "Serve the read-only task dashboard"
     webCmd.String(&serveAddr, "a", "addr", "Listen address (default from mcp-tasks.yaml, e.g. 127.0.0.1:7777)")
     webCmd.Bool(&serveWithMCP, "", "mcp", "Also serve MCP over stdio in this process")
     serveCmd.AttachSubcommand(webCmd, 1)
     flaggy.AttachSubcommand(serveCmd, 1)
     ```
     Dispatch: `if webCmd.Used { return cmdServeWeb(ctx, stdout, stderr, serveAddr, serveWithMCP) }` — **placed before any `serveCmd.Used` check**.
  3. `cmdServeWeb` loads config, applies the `--addr`/`--mcp` overrides on top of `cfg.Web`, calls `app.RunWeb(ctx, app.Options{Web: ..., Stderr: stderr})`, blocks until the context is cancelled, returns **0** on clean shutdown and **1** on a startup error (bind failure, unresolvable project).
  4. **`--mcp` defaults to `false`** and `web.with_mcp` defaults to `false`: a human running `serve web` in a terminal has a TTY on stdin, and a stdio JSON-RPC reader there would consume keystrokes as garbage frames. The flag exists for a wrapper launching `serve web --mcp` with pipes.
- **Tests and checks:**
  - **New** in `internal/cli/cli_test.go`: `TestServeWebReturnsOnContextCancel` (via `RunWithContext` with an already-cancelled or short-lived context; assert exit code 0), `TestServeWebBadAddrExitsNonZero` (e.g. `--addr 256.256.256.256:1` or an occupied port; assert 1 and a stderr message).
  ```
  GOWORK=off go build ./... && GOWORK=off go vet ./...
  GOWORK=off go test ./internal/cli/ && GOWORK=off go test ./...
  GOWORK=off go test -race ./internal/task/ ./internal/web/ ./internal/app/
  ```
  **Manual (this is the browser milestone):**
  ```
  GOWORK=off go run ./cmd/mcp-task-manager serve web --addr 127.0.0.1:7777
  # browser → http://127.0.0.1:7777/
  ```
  Expect: the single `web-ui-kanban-module` card in *To do*; its detail panel lists attached files `task.md`, `research.md`, `design.md`, `plan.md`; disconnect the network and reload (offline render); in another terminal `GOWORK=off go run ./cmd/mcp-task-manager start web-ui-kanban-module` and watch the 5 s poll move the card into *In progress* and raise the amber danger-zone banner. Ctrl-C exits 0.
- **Commit boundary:** `feat(cli): add serve web entry point`
- **Definition of done:** the existing CLI suite passes **unchanged**; the two new tests pass; the manual walkthrough succeeds end to end.
- **Rollback note:** clean revert; nothing but docs depends on it.
- **Risk flags:** flaggy exits the process itself on an unknown subcommand and prints to **stdout** with exit code 2 (measured); the nested `serve web` form must be attached exactly as above or `serve web` is treated as an unknown positional. Tests must keep `flaggy.PanicInsteadOfExit = true` (`cli_test.go:13-17`). `flaggy.ResetParser()` at the top of every run means the new subcommand must be declared inside `RunWithContext`, not at package level.

---

### P11 — Documentation

- **Goal:** `CLAUDE.md` and `README.md` describe the web module, the new config, the concurrency contract and the dogfooding setup.
- **Why this phase exists / position:** last, because it must describe what actually shipped — in particular the **`CLAUDE.md` "Concurrency" section is now factually wrong** ("MVP assumes single-server, no locking") and must be rewritten.
- **Files likely to change:** `CLAUDE.md`, `README.md`.
- **Regeneration required:** none.
- **Dependencies:** P10 (all behavior final).
- **Implementation tasks:**
  - **CLAUDE.md:** add a `Web UI (kanban)` box to the architecture ASCII as a third peer above `Task Service`; new "### Package boundary" paragraph (*`internal/task` is the only package that performs task operations; `internal/storage` is private to it*); new "### Web UI" section (read-only kanban, one process two transports, `start_web_ui`, embedded htmx/Tailwind, danger-zone indication, archived tasks absent from the board); **replace** the "### Concurrency" bullet with the design's verbatim replacement text about the single `sync.Mutex`, the twin discipline, atomic writes, and cross-process coordination remaining out of scope; add a `start_web_ui` row under "## MCP Tools"; add the `web:` block and the `MCP_WEB_ENABLED`/`MCP_WEB_ADDR` rows under "## Configuration"; update "## Project Structure" (`internal/web/**`, `internal/app/**`, `internal/testsupport/`, `scripts/build-css.sh`; `cmd/mcp-task-manager/` loses `icon.png` and `instructions.md`); note `applyDefaults` under "Validation".
  - **README.md:** "Web dashboard" subsection under `### Running the Server` (:57) with `mcp-task-manager serve web --addr 127.0.0.1:7777`; a `serve web` example in `### CLI Usage` "Other" (:133); document the **project-scoped** `.mcp.json` form and the plugin's `go install` prerequisite under `### Claude Code Integration` (:172); `start_web_ui` under `## MCP Tools` (:261); `web:` under `### Config File` (:299); two rows under `### Environment Variables` (:318); `internal/web`/`internal/app` in `## Project Structure` (:385); a new maintainer subsection "Refreshing the vendored CSS" documenting `scripts/build-css.sh` and the pinned htmx/Tailwind versions. **State plainly** that the HTTP surface is read-only and unauthenticated, and that a non-loopback bind is the operator's explicit choice.
  - Note: `AGENTS.md` is a symlink to `CLAUDE.md` — no separate edit.
- **Tests and checks:** full final gate (below). Re-run the P0 dogfooding verification (a real `list_tasks` tool call) to confirm nothing regressed.
- **Commit boundary:** `docs: document the web dashboard, concurrency contract and dogfooding setup`
- **Definition of done:** the acceptance-criteria checklist below is fully satisfied.
- **Rollback note:** trivially revertible.
- **Risk flags:** the single real risk is leaving the stale "no locking" line in `CLAUDE.md`, which would mislead every future agent into re-adding races.

---

## Cross-Phase Risks

| ID | Risk | Phases | Mitigation |
|---|---|---|---|
| R1 | Twin-method discipline drift — a new exported `Service` method calling another exported one self-deadlocks | P1, P2, and forever after | `mu` doc comment; `TestServiceNoSelfDeadlock`; post-edit grep for `s.Get(`/`s.Update(`/… in `service.go` |
| R2 | `SetTools` re-publication silently drops `start_web_ui` if the `OnResolve` closure forgets the starter | P8, P9 | `TestSetToolsRepublicationKeepsStartWebUI`, re-run through `app.newServer` in P9 |
| R3 | A missing/misnamed `descriptions/*.yaml` panics `internal/tools` at package init | P8 | YAML in the same commit; `TestEveryToolHasDescriptions` as a permanent guard |
| R4 | `go:embed` cannot cross `../` — assets and embedding code must move together | P6, P9 | atomic commits; failure is a loud build error |
| R5 | An HTTP GET triggering resolution would migrate the layout and auto-archive on disk | P5, P7 | `Current()` is the only accessor in `web.Deps`; `TestUnresolvedProjectPlaceholder` uses a roots func that `t.Fatal`s if invoked |
| R6 | A `.gitignore` rule matching `*.css`/`*.js` would leave vendored assets untracked and break fresh clones | P0, P6 | `git check-ignore -v internal/web/static/app.css` must report no match |
| R7 | `applyDefaults` changes existing behavior for partial `auto_archive` sections | P4 | dedicated regression test; update, do not weaken |
| R8 | Board poll takes the global service lock; a stale directory triggers a full rebuild under it | P7 | 5 s poll, archived tasks excluded, no per-card descriptions; debounced/versioned index deferred |
| R9 | Binding a TCP port is new behavior on a read-only, unauthenticated surface | P4, P9, P11 | default `enabled: false`; default addr loopback-only; README states it plainly |
| R10 | `serve web --mcp` on a TTY consumes keystrokes as JSON-RPC frames | P10 | `--mcp` and `web.with_mcp` default to `false` |

**Parallelization opportunities.** P0, P1, P4, P5 and P6 have no mutual dependencies and may be developed concurrently (they must still be committed in an order where every boundary is green — any interleaving works since none of them touch the same files). P2 needs P1. Everything from P7 onward is strictly serial.

---

## Execution Order Summary

| # | Phase | Depends on | Commit subject |
|---|---|---|---|
| 0 | Dogfooding & repo hygiene | — | `fix(dogfood): split project-scoped and plugin MCP configs, tidy repo state` |
| 1 | Service mutex | — | `fix(task): serialize all task operations behind a single service mutex` |
| 2 | Service view API | P1 | `feat(task): add board/detail view API and batch blocked lookup` |
| 3 | `internal/testsupport` | P2 | `test: add internal/testsupport backlog helpers` |
| 4 | `web:` config + `applyDefaults` | — | `feat(config): add web section and normalize partially specified sections` |
| 5 | `project.Build` + `Current` | — | `refactor(project): export Build and add non-resolving Current accessor` |
| 6 | Vendored assets | — | `chore(web): vendor htmx 2.0.4 and the Tailwind build pipeline` |
| 7 | `internal/web` | P2,P3,P4,P5,P6 | `feat(web): read-only htmx kanban dashboard package` |
| 8 | `start_web_ui` tool | P7 | `feat(tools): add start_web_ui tool behind a WebStarter interface` |
| 9 | `internal/app` | P7,P8 | `feat(app): add composition root running MCP and web in one process` |
| 10 | `serve web` CLI | P9 | `feat(cli): add serve web entry point` |
| 11 | Docs | P10 | `docs: document the web dashboard, concurrency contract and dogfooding setup` |

All commit messages end with:
```
Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
```

---

## Milestone View

| Milestone | Phases | What is shippable |
|---|---|---|
| **M1 — Dogfooding restored** | P0 | The repo's own `task-manager` MCP server connects again. Independently valuable; could be released alone. |
| **M2 — Concurrency-safe core** | P1–P2 | A pre-existing latent bug (concurrent `create_task` racing on `Index.entries` and duplicating `NextID()` under mcp-go's 5-worker pool) is fixed, and `list_tasks` stops doing N index scans. Shippable on its own, user-visible as reliability. |
| **M3 — Foundations** | P3–P6 | No user-visible change; the substrate for the web module. Not independently shippable, but every boundary is green. |
| **M4 — Web module exists** | P7–P8 | The package and the tool are complete and tested, but **nothing binds a port yet** — no entry point wires them. Green, inert. |
| **M5 — First browser visibility** | P9 | **The board is first reachable in a browser.** Run `MCP_WEB_ENABLED=1 mcp-task-manager` from an MCP client, or call `start_web_ui`; `curl localhost:7777/healthz` → `ok`. Without a client it shows the placeholder — by design. |
| **M6 — Human entry point** | P10 | **The intended demo:** `mcp-task-manager serve web`, open `http://127.0.0.1:7777/`, see the real backlog with data from the first request. This is the milestone to show the user. |
| **M7 — Complete** | P11 | Acceptance criteria fully met. |

**Earliest full browser experience with real data: end of P10.** P9 gives a board that is live but placeholder-only until an MCP client makes its first tool call.

---

## Global Rollback Notes

- **P0–P6 are individually revertible** with `git revert`, in any order, as long as P7 has not landed (P6's assets become a build requirement the moment P7's `go:embed` directives exist).
- **P1 is the deepest commit to unwind.** Reverting it after P2 requires reverting P2 as well (P2's composites call P1's unlocked twins). If P1 proves unstable, prefer fixing forward: the twin split is mechanical and a missed call site is a deterministic, immediately-reproducible deadlock, not a heisenbug.
- **P9 is the point of no return for the file layout.** It `git mv`s `icon.png`, `instructions.md` and `main_test.go`. Git records the moves, so a revert is mechanically clean, but verify `go build ./...` afterwards — a partially reverted embed is a build break, and a partially reverted `main_test.go` leaves `package main` referencing a `newServer` that no longer exists.
- **Backing out the whole feature** after P11 is `git revert` of P6–P11 (in reverse order) plus a decision on P1/P2, which are independently valuable bug fixes and should normally be **kept**. P0 should always be kept.
- **Nothing in this plan writes to the live `tasks/` directory** except P0's `git mv` of `.tasks/` and whatever the implementer does through the MCP tools. All tests use `t.TempDir()`.

---

## Open Decisions (carried, not invented)

**D1 — Tailwind standalone CLI version to pin (Phase 6).**
The design says: pick the latest v4 release at implementation time and record it in the `scripts/build-css.sh` header. **Not decided here.** The implementer must check the current latest `tailwindlabs/tailwindcss` v4 release, pin the exact tag in the script, and state it in the README's "Refreshing the vendored CSS" subsection. Note that v4's CLI flags and the `@import "tailwindcss"` / `@source` syntax differ from v3 — verify the downloaded binary matches the `input.css` syntax before committing the first `app.css`.

**D2 — Disposition of the orphaned `.tasks/task-file-storage/` (Phase 0).**
The design assumes **preservation** via `git mv .tasks/task-file-storage docs/history/task-file-storage`. Deletion is a one-line change to the plan. The four files (`task.md`, `research.md`, `design.md`, `plan.md`) are tracked workflow artifacts of the **already-shipped** attached-files feature; they have no `{id}.md` record so they are not a task, and no archived task references them. **The maintainer must choose.** Whichever is chosen, `.tasks/` must end up removed and **not** gitignored (it is the default tasks-dir name for other projects; ignoring it here would be misleading).

Both decisions are non-blocking and confined to a single phase each.

---

## Definition of Done — mapped to `task.md`'s acceptance criteria

### Web module
| Criterion | Delivered by | Verified by |
|---|---|---|
| New `internal/web` using `net/http` + `html/template` + `ServeMux` patterns, no framework | P7 | `go.mod` unchanged; route table in `server.go` |
| Kanban at `/` with one column per status | P7 | `TestBoardRendersThreeColumns` |
| Card shows id, title, priority, type; marks blocked and subtasks | P7 | `TestBoardCardBadges`, `view_test.go` nesting cases |
| Task detail with description, relations, subtasks, file names | P2 (`Detail`) + P7 | `TestDetailPage`, `TestDetailArchivedTask` |
| htmx does a real partial update | P7 | `TestDetailPanelIsFragment` (no `<html>`); `/board` poll + panel swap |
| Tailwind + htmx vendored and embedded; renders with no network | P6, P7 | `TestNoExternalAssetReferences`, `TestStaticAssetsServed`, manual offline reload |
| Read-only: no route mutates | P7 | `TestNoMutatingRoutes` (405s + byte-identical tasks dir) |
| All text escaped; no `template.HTML` on task content | P7 | `TestEscaping`; `grep -rn 'template.HTML' internal/web` → zero |

### Process composition
| Criterion | Delivered by | Verified by |
|---|---|---|
| One binary, one process, one `task.Service` | P9 | `TestRunMCPWebSharesResolution` |
| `web:` config section + env override | P4 | `TestWebDefaults`, `TestEnvWebOverrides` |
| MCP mode starts the web server when enabled | P9 | `TestRunMCPStartsWebWhenEnabled`, `TestRunMCPDoesNotStartWebByDefault` |
| `start_web_ui` tool; idempotent, reports URL, no double bind | P8 (+P9 wiring) | `TestStartWebUIIdempotent`, `TestControllerStartIdempotent` |
| CLI entry point `serve web`, MCP in-process per config | P10 | `TestServeWebReturnsOnContextCancel`; manual run |
| Web logging never to stdout | P7, P9 | `grep -rn 'fmt.Print\|os.Stdout' internal/web` → zero; `ErrorLog` → stderr |
| Clean shutdown via `Shutdown`, no leaked goroutine | P9 | `TestRunWebStartsAndShutsDown`, `TestStdioListenStopsOnContextCancel`, `TestControllerShutdown`; `-race` |

### Dogfooding
| Criterion | Delivered by | Verified by |
|---|---|---|
| `.mcp.json` free of `${CLAUDE_PLUGIN_ROOT}`; server starts from a plain session | P0 | session restart + live `list_tasks` call |
| Resolution lands on this repo's `tasks/`, verified by a real tool call | P0 | `list_tasks` returns `web-ui-kanban-module` |
| Stray `tasks/.index.json` removed and/or gitignored | P0 | `.gitignore` entry; `Index.Load` already removes it |
| `CLAUDE.md` / `README.md` document the web module, config, dogfooding | P11 | review against the P11 checklist |

### Quality gates
Final command set, all of which must pass at P11 (and `build`/`vet`/`test ./...` at **every** phase boundary):
```
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test ./...
GOWORK=off go test -race -count=2 ./internal/task/ ./internal/web/ ./internal/app/
git diff --stat HEAD~12 -- go.mod go.sum   # must be EMPTY: no new third-party runtime deps
```
Plus the manual validation walkthrough from P10 (board renders, detail panel lists attached files, offline reload works, starting a task moves the card and raises the danger-zone banner within 5 s, Ctrl-C exits 0).