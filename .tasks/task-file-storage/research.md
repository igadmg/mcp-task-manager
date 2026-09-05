# Research: task-file-storage

### Task Slice

- How tasks are persisted today (`internal/storage/markdown.go`, `internal/storage/index.go`) — on-disk layout, atomic-write pattern, archive move mechanics.
- The `Task` data model and `Service` business logic (`internal/task/task.go`, `internal/task/service.go`), specifically the `Delete` and `ArchiveTask` cascade paths (relation cleanup, subtask handling).
- The MCP tool registration pattern (`internal/tools/*.go`) and the CLI subcommand pattern (`internal/cli/*.go`) that any new `read_task_file`/`write_task_file`/`list_task_files` tool would need to follow.
- Config surface (`internal/config/config.go`) for anything resembling file-name/size limits.
- Existing test conventions for storage, service, tools, and CLI layers.
- Documentation locations that describe the tool table and storage design (CLAUDE.md, AGENTS.md, README.md, `docs/plans/*.md`), to know what would need updating.

### Confirmed Facts

**Storage layout — strictly flat, one file per task, no per-task directory or auxiliary files exist today.**
- Task files live at `<tasksDir>/NNN.md`, built via `fmt.Sprintf("%03d.md", id)` — `internal/storage/markdown.go:33-35`.
- Archived files live at `<tasksDir>/archive/NNN.md`, same naming — `internal/storage/markdown.go:208-210`.
- `LoadAll()` scans the tasks dir for `*.md` (skipping `.index.json`) — `internal/storage/markdown.go:95-127`. There is no directory-per-task or sidecar-file concept anywhere in the codebase; a grep for `attach|task_file|\.files` across all non-test `.go` files returned zero relevant hits (only unrelated `flaggy.AttachSubcommand` matches).
- Confirmed on the real repo state: `tasks/` contains only `archive/*.md` (89 files) and `.index.json`; zero active `.md` files exist at `tasks/` top level right now (`ls tasks/*.md` → 0 matches).
- A **separate, unrelated** directory `.tasks/` (with a leading dot) exists at the repo root and holds workflow/skill artifacts (e.g. `.tasks/task-file-storage/task.md`, the very task being researched) — this is not `MCP_TASKS_DIR` and not part of the storage subsystem; naming is easy to confuse with `tasks/` and worth flagging for the design phase.

**Markdown file format and atomicity.**
- `Save(t *task.Task)` builds YAML frontmatter (id, parent_id, title, status, priority, type, relations, created_at, updated_at) then writes `---\n` + YAML + `---\n\n` + `t.Description` as the body — `internal/storage/markdown.go:38-70`.
- Atomic write pattern: write to `<path>.tmp` via `os.WriteFile`, then `os.Rename(tmpPath, finalPath)` — `internal/storage/markdown.go:72-77`. The exact same pattern is reused for the index file — `internal/storage/index.go:225-231`. This is the pattern CLAUDE.md refers to as "File writes are atomic (write to temp file, then rename)."
- `parse()` requires an opening `---` line, reads frontmatter until a closing `---`, then treats everything after as the body/description — `internal/storage/markdown.go:130-190`.

**Archive mechanics (file move).**
- `Archive(id int) error` = `os.MkdirAll(archiveDir)` then `os.Rename(s.taskPath(id), s.archivePath(id))` — `internal/storage/markdown.go:212-219`. It is a plain rename of the single `.md` file; there is no other artifact moved.
- `LoadArchived`, `LoadAllArchived`, `IsArchived` all operate by path only (no index) — `internal/storage/markdown.go:221-266`.
- `NextID()` takes `max(activeMaxID, archiveMaxID) + 1`, scanning both directories for numeric `.md` filenames — `internal/storage/markdown.go:268-307`.

**Archive cascade (service layer) — the pattern to replicate for attached files.**
- `Service.ArchiveTask(id int) error` — `internal/task/service.go:540-587`:
  - Requires status `done` (line 545-547).
  - If task has subtasks, requires **all** subtasks `done` (551-559), then for each subtask: `idx.RemoveAllRelationsForTask(sub.ID)` → `updateAffectedRelationTasks(sub.ID, removedEdges)` → `archiveStorage.Archive(sub.ID)` → `idx.Delete(sub.ID)` (560-572).
  - Then for the parent itself: same relation cleanup → `archiveStorage.Archive(id)` → `idx.Delete(id)` → `idx.Save()` (574-587).
  - `updateAffectedRelationTasks` (589-616) rewrites the frontmatter of any *other* task whose `relations` pointed at the archived task, via `storage.Save(affected)` + `idx.Set(affected)`.
- This is exactly the point in the code where a future "move attached files to `tasks/archive/<id>/`" call would need to be inserted, once per archived task ID (both subtasks and the parent).

**Delete cascade (service layer).**
- `Service.Delete(id int, deleteSubtasks bool) error` — `internal/task/service.go:257-317`:
  - If `HasSubtasks(id)` and `!deleteSubtasks`, returns an error naming the subtask count (264-267, mentions `--force`, a CLI-flag reference leaking into a service error message).
  - Otherwise deletes each subtask via `storage.Delete(sub.ID)` + `idx.Delete(sub.ID)` (270-277) — subtask files removed **before** relation cleanup or parent deletion.
  - Cleans relations for the parent task via `idx.RemoveAllRelationsForTask(id)` and updates affected tasks' frontmatter inline (280-309) — this cleanup logic is **duplicated** between `Delete` (280-309) and `ArchiveTask`'s `updateAffectedRelationTasks` helper (589-616); `Delete` does not call the shared helper, it has its own copy.
  - Finally `storage.Delete(t.ID)` + `idx.Delete(id)` + `idx.Save()` (311-316).
  - This is the analogous point where deleting attached files (both for subtasks and the parent) would need to be inserted.

**Task model.**
- `Task` struct fields: `ID int`, `ParentID *int`, `Title string`, `Description string` (yaml:`-`, stored in markdown body only), `Status`, `Priority`, `Type string`, `Relations []Relation`, `CreatedAt`, `UpdatedAt` — `internal/task/task.go:47-58`. There is no field today for a list of attached file names; adding one would require deciding whether it's persisted in frontmatter (like `Relations`) or derived by listing a directory at read time (more consistent with "index caches metadata, storage is source of truth" but would require either a new storage method or an index rebuild step).
- `Relation` struct: `Type string`, `Task int` (41-44) — the precedent for a small, frontmatter-embeddable value type, useful only as a style reference, not directly reusable for file lists (files are content-bearing, not just references).

**Storage/Index interfaces used by `Service` (the extension points).**
- `Storage` interface (what `MarkdownStorage` implements for the service): `Save`, `Load`, `Delete`, `EnsureDir` — `internal/task/service.go:17-22`.
- `ArchiveStorage` interface: `Archive`, `LoadArchived`, `LoadAllArchived`, `IsArchived` — `internal/task/service.go:61-66`. Note: there is no `internal/storage/storage.go` file; the `Storage`/`Index`/`ArchiveStorage` interfaces are declared directly in `internal/task/service.go`, not in the storage package (the task prompt's assumption of a `storage.go` file is incorrect — no such file exists; confirmed via `find internal -name '*.go'`).
- `Index` interface (in-memory cache contract): `Load/Save/Get/Set/Delete/All/Filter/NextTodo/NextID` plus subtask methods (`GetSubtasks/HasSubtasks/SubtaskCounts`) and relation methods (`AddRelation/RemoveRelation/GetRelationsForTask/GetBlockers/RemoveAllRelationsForTask`) — `internal/task/service.go:38-58`. Any new file-storage capability would most likely be injected via a new interface/dependency on `Service` (mirroring how `archiveStorage` was added alongside `storage`), rather than folded into `Storage`/`Index`, since attached files are not part of the index's job (metadata-only cache) — this is an **inference** the design phase must confirm, not a fact.

**MCP tool registration pattern (concrete template).**
- Tools are grouped by file and registered from `tools.Register` — `internal/tools/tools.go:11-16`: calls `registerManagementTools`, `registerWorkflowTools`, `registerRelationTools`. A fourth call (e.g. `registerFileTools`) would be the natural extension point, in a new file (e.g. `internal/tools/files.go`) mirroring `relations.go`.
- Concrete end-to-end template — `add_relation` in `internal/tools/relations.go:12-30` (schema) and `:52-64` (handler):
  ```go
  addTool := mcp.NewTool("add_relation",
      mcp.WithDescription("Add a relation between two tasks"),
      mcp.WithNumber("source", mcp.Required(), mcp.Description("Source task ID")),
      mcp.WithString("type", mcp.Required(), mcp.Description(...), mcp.Enum(relationTypes...)),
      mcp.WithNumber("target", mcp.Required(), mcp.Description("Target task ID")),
  )
  s.AddTool(addTool, addRelationHandler(svc))
  ```
  Handler signature is `func(svc *task.Service) server.ToolHandlerFunc`, returning `func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)`. Params are pulled via `req.GetInt`/`req.GetString`/`req.GetBool`/`req.GetArguments()` (for optional-field presence checks, e.g. `internal/tools/management.go:134-138`). Errors are converted with `mcp.NewToolResultError(err.Error()), nil` (never a Go `error` return) — see every handler, e.g. `internal/tools/relations.go:58-60`.
  Read-only tools call `svc.EnsureProjectExists()` first and short-circuit with `mcp.NewToolResultError` — e.g. `getTaskHandler` at `internal/tools/management.go:169-171` and `getNextTaskHandler` at `internal/tools/workflow.go:42-44`. This is the pattern a `read_task_file`/`list_task_files` handler should follow (a write tool, `write_task_file`, would instead need the "task not archived" check, analogous to how `archive_task`/mutating tools implicitly rely on `Service.Get`/`Update` failing or need a new explicit guard — no existing tool currently blocks writes solely because a task is archived, since `Update`/`StartTask`/`CompleteTask` all operate on `s.index.Get` which only finds active tasks; archived tasks are simply invisible to those paths rather than explicitly rejected. This is a **gap to note**: there's no existing "reject write on archived task" pattern to copy for a hypothetical `write_task_file` call on an archived task's ID — it would need new logic, likely checking `archiveStorage.IsArchived(id)` explicitly since `Get()` falls back to archive and would otherwise succeed silently).
- `archive_task` tool: `internal/tools/management.go:116-123` (schema), `:263-271` (handler) — simplest possible template (single required `id` numeric param, calls `svc.ArchiveTask(id)`, returns a plain text success message).
- `delete_task` tool: `internal/tools/management.go:79-89` (schema, includes optional `delete_subtasks` boolean), `:250-261` (handler).

**CLI layer mirrors every MCP tool as a subcommand, with a consistent structural pattern.**
- `internal/cli/cli.go` defines one `flaggy.NewSubcommand(...)` block per verb (list, get, next, create, update, delete, start, complete, archive — `internal/cli/cli.go:47-146`), each attached via `flaggy.AttachSubcommand(cmd, 1)`, then dispatched via `if xCmd.Used { return cmdX(...) }` in `RunWithArgs` (`internal/cli/cli.go:157-221`).
- `internal/cli/commands.go` has one `cmdX(stdout, stderr io.Writer, jsonOutput bool, ...) int` function per subcommand, each independently calling `loadConfig()`/`initService()` (or `initServiceWithConfig`), doing the service call, and branching on `jsonOutput` between `FormatJSON(stdout, ...)` and a human-readable formatter (`FormatTaskDetail`/`FormatTaskTable`/plain `fmt.Fprintln`) — e.g. `cmdArchive` at `internal/cli/commands.go:380-403`, `cmdDelete` at `:328-351`. New `read-task-file`/`write-task-file`/`list-task-files` CLI subcommands would need to follow this same three-part pattern (flaggy definition in `cli.go`, `cmdX` in `commands.go`, output formatting in `output.go`).
- README.md's CLI table (`README.md:97-111`) lists 9 CLI commands but **omits `archive`** even though it exists in code and CLAUDE.md's MCP tool table includes `archive_task` — confirms doc drift already exists before this feature is added.

**Config has no file-related settings; it is strictly task/relation/archive scoped today.**
- `Config` struct: `TaskTypes []string`, `RelationTypes []string`, `AutoArchive AutoArchiveConfig{Enabled, AfterDays}`, `DataDir`, `ProjectFound` — `internal/config/config.go:17-23`. No fields for max file size, filename pattern/allowlist, or per-task file count limits exist anywhere in config, storage, or task packages. `mcp-tasks.yaml` (repo root, real file) only sets `task_types` and `auto_archive` (`mcp-tasks.yaml` at repo root, confirmed by direct read) — confirms config is validation-only for type enums, not a place with precedent for size/name limits.

**Tests: strong coverage of storage/service cascades; weaker coverage of MCP tool handlers themselves.**
- `internal/storage/storage_test.go` (2250 lines) covers `Save/Load/Delete/LoadAll` (`:16-127`), `NextID` across active+archive (`:123-234`), `Archive/LoadArchived/LoadAllArchived/IsArchived` (`:2087-2249`), and extensive `Index` behavior (`Filter`, `NextTodo` ordering/subtask/in-progress rules, relations, git-commit staleness). All tests use `t.TempDir()` and construct `NewMarkdownStorage(dir)` / `NewIndex(dir, storage)` directly — no fixtures directory, everything built inline per test.
- `internal/task/service_test.go` (2250+ lines) uses in-memory `mockStorage`/`mockArchiveStorage`/`mockIndex` (defined at top of file, `:11-150+`) rather than real files, and covers: `TestService_Delete*` (`:406, 648, 662, 684, 1266`), `TestService_ArchiveTask_*` (`:1307-1434`, covering basic archive, not-done rejection, in-progress rejection, not-found, subtasks-all-done, subtasks-some-not-done, relation cleanup), `TestService_Get_FallbackToArchive` (`:1436`), `TestService_ListArchived`, auto-archive candidate/run tests (`:1482-1583`). This mock-based service test suite is the natural place to add tests for a new "attach files" service capability, following the existing `newServiceWithArchive()` helper pattern (`:1298-1305`).
- `internal/tools/tools_test.go` contains exactly **one** test, `TestRegisterDocumentsAllowedTypeValues` (`:10-39`), which only asserts on `mcp.NewTool` schema properties (enum values, description text) for `create_task`/`update_task`/`list_tasks`/`add_relation`/`remove_relation`. **No test in this file exercises any tool handler's actual behavior** (no calls through `s.CallTool` or handler functions directly) — handler logic is validated only indirectly via `service_test.go`. This means a new file-tools test file would either need to introduce the first handler-level MCP test in this repo, or (following existing convention) put the real behavioral coverage in `internal/task/service_test.go` against a new service method, and only add schema-shape assertions in `tools_test.go`.
- `internal/cli/cli_test.go` tests use `t.TempDir()` + `t.Setenv("MCP_TASKS_DIR", tmpDir)` + `RunWithArgs([]string{...}, &stdout, &stderr)` with buffered `bytes.Buffer` for stdout/stderr (`:49-64` for the pattern; `TestArchiveCommand` at `:392`, `TestArchiveCommandJSON` at `:415`, `TestArchiveCommandNotDone` at `:439` show the archive-specific CLI test shape). `flaggy.PanicInsteadOfExit = true` is set in `init()` (`:13-16`) for testability.

**Documentation: the MCP tool table exists in three separate files, byte-identical in two of them.**
- `CLAUDE.md` and `AGENTS.md` are byte-for-byte identical (`diff` returned no output) — both contain the full tool table, storage design, archiving rules, etc. Any documentation update for the new tools must be applied to **both** files identically.
- `README.md:191-217` has its own, shorter MCP tool table (Task Management / Agent Workflow / Relations sections) that already **omits `archive_task`** — a pre-existing gap, independent of this feature, that the new tools table update will need to reconcile rather than copy verbatim from CLAUDE.md.
- `docs/plans/2026-03-13-task-archiving-design.md` is the historical design doc for the archiving feature and documents the "Changes by Area" format this repo's own design docs use (`config/config.go`, `storage/markdown.go`, `storage/index.go`, `task/service.go`, `tools/management.go`, `tools/workflow.go`, `cli/cli.go`, `cli/commands.go` — full file at that path) — useful precedent for how a design doc for this new feature would likely be structured, but it is not itself a rule source for the new feature.

**Wiring / entry point.**
- `cmd/mcp-task-manager/main.go:14-49` constructs `MarkdownStorage`, `Index`, `Service`, then `tools.Register(s, svc, cfg.TaskTypes, cfg.RelationTypes)`. Any new storage dependency (e.g. a file-attachment storage) would need to be constructed here and threaded into `task.NewService(...)` (or a new constructor), and also duplicated in `internal/cli/commands.go:22-33` (`initServiceWithConfig`), since CLI and MCP server each build their own `Service` instance independently — there is no shared service-construction helper beyond that file.

### Evidence Map

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Task file naming | `NNN.md`, 3-digit zero-padded ID | `internal/storage/markdown.go:33-35` |
| Archive file naming | `archive/NNN.md`, same padding | `internal/storage/markdown.go:208-210` |
| Per-task auxiliary files | None exist; strictly one `.md` file per task | grep across repo returned no hits; `internal/storage/markdown.go` has no directory-per-task concept |
| Atomic write | temp file + `os.Rename` | `internal/storage/markdown.go:72-77`; same pattern in index `internal/storage/index.go:225-231` |
| Archive = file move | `os.Rename` after `MkdirAll` | `internal/storage/markdown.go:212-219` |
| Archive cascade (subtasks + relations) | Loop over subtasks: clean relations, `Archive`, `idx.Delete`; then same for parent | `internal/task/service.go:540-587` |
| Delete cascade (subtasks + relations) | Loop over subtasks: `storage.Delete` + `idx.Delete`; separate inline relation cleanup (not shared helper) | `internal/task/service.go:257-317` |
| Relation-cleanup code duplication | `Delete` has inline copy (280-309); `ArchiveTask` calls shared `updateAffectedRelationTasks` (589-616) | `internal/task/service.go:280-309` vs `:589-616` |
| Storage/Index/ArchiveStorage interfaces | Declared in the `task` package, not a `storage.go` file | `internal/task/service.go:17-66` |
| MCP tool registration entry point | `tools.Register` dispatches to per-concern registration functions | `internal/tools/tools.go:11-16` |
| MCP tool template (schema+handler) | `add_relation` | `internal/tools/relations.go:12-30` (schema), `:52-64` (handler) |
| Read-tool "project exists" guard | `svc.EnsureProjectExists()` before read ops | `internal/tools/management.go:169-171`; `internal/tools/workflow.go:42-44` |
| Archived-task write rejection | **No explicit guard exists**; mutating ops use `s.index.Get` which simply can't see archived tasks | `internal/task/service.go:178-190` (`Get` falls back to archive only for reads used by `Get`/`GetWithSubtasks`), contrasted with `Update`/`StartTask`/`CompleteTask` which call `s.Get` too (so they'd actually succeed against an archived task's stale in-memory data if `Get` returns it via fallback — worth double-checking in design, since `Update` at `internal/task/service.go:208-254` calls `s.Get(id)` which *does* fall back to archive, meaning today `update_task` might not actually error out cleanly on an archived task; this is a **plausible existing gap**, not confirmed by a passing/failing test) |
| CLI subcommand pattern | flaggy subcommand def in `cli.go` + `cmdX` in `commands.go` + formatter in `output.go` | `internal/cli/cli.go:139-146` + `internal/cli/commands.go:380-403` |
| Config limits | Only `task_types`, `relation_types`, `auto_archive`; no file name/size config | `internal/config/config.go:17-23` |
| MCP tool handler test coverage | Only schema/enum assertions, no handler execution tests | `internal/tools/tools_test.go:10-39` |
| Service-level test coverage for cascades | Extensive, via mocks | `internal/task/service_test.go:406-1583` |
| CLI test convention | `t.TempDir()` + `t.Setenv("MCP_TASKS_DIR", ...)` + `RunWithArgs` with buffers | `internal/cli/cli_test.go:49-64` |
| Doc duplication | CLAUDE.md == AGENTS.md (identical); README.md has a separate, already-incomplete table | `diff CLAUDE.md AGENTS.md` (empty); `README.md:191-217` |

### Relevant Files

1. `/Users/iga/Workspace/mcp-task-manager/internal/task/service.go` — the central place where `ArchiveTask`/`Delete` cascades live; any new file-attachment lifecycle hook goes here, and this is where the `Storage`/`ArchiveStorage`/`Index` interfaces are actually declared (not in `internal/storage/`).
2. `/Users/iga/Workspace/mcp-task-manager/internal/storage/markdown.go` — the only place that touches the filesystem for tasks; shows the exact atomic-write and archive-move primitives to reuse/mirror for attached files.
3. `/Users/iga/Workspace/mcp-task-manager/internal/tools/relations.go` — best concrete, complete template (schema + handler + error convention) for a brand-new small MCP tool family, more directly analogous in scope to `read/write/list_task_file` than the larger `management.go`.
4. `/Users/iga/Workspace/mcp-task-manager/internal/tools/management.go` — shows the `archive_task`/`delete_task` tools and the `EnsureProjectExists` read-guard pattern; also where `delete_subtasks` cascade option is exposed.
5. `/Users/iga/Workspace/mcp-task-manager/internal/cli/cli.go` and `internal/cli/commands.go` — CLI mirrors every MCP tool; new tools imply new subcommands following this exact split.
6. `/Users/iga/Workspace/mcp-task-manager/internal/task/service_test.go` — mock-based test conventions (`newServiceWithArchive`, `mockStorage`, `mockArchiveStorage`, `mockIndex`) that a new service-level file-attachment feature should extend.
7. `/Users/iga/Workspace/mcp-task-manager/internal/storage/storage_test.go` — real-filesystem test conventions (`t.TempDir()`, `makeTestTask` helper referenced near line 2080) for storage-level file tests.
8. `/Users/iga/Workspace/mcp-task-manager/CLAUDE.md`, `/Users/iga/Workspace/mcp-task-manager/AGENTS.md`, `/Users/iga/Workspace/mcp-task-manager/README.md` — three documentation locations needing updates (first two identical, README already diverged).
9. `/Users/iga/Workspace/mcp-task-manager/cmd/mcp-task-manager/main.go` — the MCP-server wiring point; any new storage/service dependency must be constructed here in parallel with `internal/cli/commands.go`'s `initServiceWithConfig`.
10. `/Users/iga/Workspace/mcp-task-manager/docs/plans/2026-03-13-task-archiving-design.md` — precedent design-doc structure ("Changes by Area") for the archiving feature this new feature must stay consistent with.

### Inference

- Whether a new "attached files" capability should be modeled as a new interface on `Service` (parallel to `archiveStorage`) versus folded into the existing `Storage` interface is **not decided by any existing code** — it's inferred from the fact that `archiveStorage` was added as a *separate* interface alongside `storage` rather than extending `Storage` itself (`internal/task/service.go:17-66`), suggesting the codebase's convention is one interface per storage *concern*.
- Whether `update_task`/`start_task`/`complete_task` actually succeed or fail against an archived task ID today is inferred from reading `Get()`'s archive-fallback logic (`internal/task/service.go:178-190`) combined with `Update()` calling `s.Get(id)` (`:209`) — no test in `service_test.go` exercises "call `Update` on an archived task ID," so this is a plausible but unverified gap, not a confirmed fact.

### Unknown

- Whether calling `update_task`/`start_task`/`complete_task` on an archived task ID currently succeeds, fails, or corrupts state — no test covers this path, and it directly affects how a `write_task_file` archived-task guard should be implemented (should it reuse existing archived-write logic, or is there none to reuse).
- Nothing in the codebase indicates where a design would place attached files on disk (no precedent for per-task directories); this is squarely a design-phase decision, not a research gap, but is flagged because the acceptance criteria mention "e.g. under a per-task directory" as only an example.

---

## Addendum: Storage Restructuring (per-task directories + migration)

*This addendum was added after the task was expanded with a new coupled requirement: restructure on-disk task storage from the flat layout to a per-task directory layout (`tasks/{id}/{id}.md`, `tasks/archive/{id}/{id}.md`), with automatic migration of existing flat-file tasks on startup. It assumes the reader has already read the sections above in full and does not repeat them.*

### Task Slice (addendum scope)

- Full blast radius of the flat-path → per-task-directory change: every construction site of `tasks/{id}.md` / `tasks/archive/{id}.md`.
- Exact current bodies of `LoadAll`, `LoadAllArchived`, `NextID` (and their shared helper `maxMarkdownTaskID`) and what directory-vs-file enumeration assumptions they make.
- Whether any migration/format-upgrade precedent exists anywhere in the codebase.
- The single shared startup hook used by both the MCP server process and every one-shot CLI invocation.
- The index self-heal/rebuild code path and its relationship to a filesystem-layout migration step.
- The only existing "create dir then move file" precedent (`Archive`), for its atomicity/error-handling shape.
- Real on-disk archive contents (count, naming, anomalies) and git tracking status of `tasks/`.

### Confirmed Facts

**Every call site that depends on the flat path shape — confirmed exhaustive by repo-wide grep.**
- Repo-wide grep for `taskPath|archivePath`, `%03d`, and `.md"` across all non-test `.go` files returns hits **only** inside `internal/storage/markdown.go` and `internal/storage/index.go`. No other package (`internal/task`, `internal/tools`, `internal/cli`, `internal/config`, `cmd/`) constructs a task-ID-based path, calls `os.ReadDir`/`os.Rename`/`os.MkdirAll` on the tasks tree, or assumes `NNN.md` naming — confirmed by `grep -rln "os.ReadDir\|os.Rename\|os.MkdirAll" --include="*.go" .` returning only `internal/config/config_test.go` and `internal/cli/cli_test.go` (both unrelated: config-discovery test scaffolding, not task file paths). This means the entire flat→directory restructuring is contained to `internal/storage/markdown.go` (plus its test file); no other production code needs to change to accommodate the new path shape itself.
- Exact construction sites, all in `internal/storage/markdown.go`:
  - `taskPath(id)` → `filepath.Join(s.dir, fmt.Sprintf("%03d.md", id))` — `internal/storage/markdown.go:33-35`, used at `:73`, `:77`, `:82`, `:91`, `:218`.
  - `archivePath(id)` → `filepath.Join(s.dir, "archive", fmt.Sprintf("%03d.md", id))` — `internal/storage/markdown.go:208-210`, used at `:218`, `:223`, `:264`.
- `internal/config/config.go` only resolves the **root** tasks directory (`TasksDir()` at `:84-90`) and checks that a `tasks` directory exists for project discovery (`config.go:130`, `os.Stat(filepath.Join(dir, "tasks"))` checking `info.IsDir()`) — this check is unaffected by the per-task-directory change since it only tests that `tasks/` itself is a directory, not its internal shape.

**`LoadAll`, `LoadAllArchived`, `NextID` — exact current bodies (`internal/storage/markdown.go`).**
```go
// LoadAll reads all tasks from the directory
func (s *MarkdownStorage) LoadAll() ([]*task.Task, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tasks []*task.Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == ".index.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		t, err := s.parse(data)
		if err != nil {
			continue
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
```
— `internal/storage/markdown.go:95-127`. **Critically, `entry.IsDir()` is a skip condition** (`:106`): today any directory entry inside `s.dir` (e.g. `archive/`) is explicitly ignored by `LoadAll`. Under the new per-task-directory layout, every task lives *inside* a directory (`tasks/{id}/`), so this exact filter — "skip anything that `IsDir()`, keep only flat `*.md` names" — is the precise piece of logic that must change or be replaced for `LoadAll` to see any task at all post-migration. `LoadAllArchived` has the identical structure and the identical `entry.IsDir()` skip:
```go
func (s *MarkdownStorage) LoadAllArchived() ([]*task.Task, error) {
	archiveDir := filepath.Join(s.dir, "archive")
	entries, err := os.ReadDir(archiveDir)
	...
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(archiveDir, entry.Name()))
		...
	}
	...
}
```
— `internal/storage/markdown.go:231-260` (skip condition at `:243`).

`NextID` and its helper:
```go
func (s *MarkdownStorage) NextID() (int, error) {
	activeMax, err := maxMarkdownTaskID(s.dir)
	if err != nil { return 0, err }
	archiveMax, err := maxMarkdownTaskID(filepath.Join(s.dir, "archive"))
	if err != nil { return 0, err }
	if archiveMax > activeMax { return archiveMax + 1, nil }
	return activeMax + 1, nil
}

func maxMarkdownTaskID(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) { return 0, nil }
		return 0, err
	}
	maxID := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if id, err := strconv.Atoi(name); err == nil && id > maxID {
			maxID = id
		}
	}
	return maxID, nil
}
```
— `internal/storage/markdown.go:268-307`. Same pattern: `maxMarkdownTaskID` derives the numeric ID by **stripping `.md` off the filename itself** (`:300`) and skips directories (`:297`). Under the new layout the numeric ID would instead need to come from the *directory name* (`tasks/{id}/`), not a file name — this is a second, independent place (distinct from `LoadAll`/`LoadAllArchived`) where the "skip dirs, parse filename" logic is baked in.
- All three functions use `os.ReadDir` (single-level, non-recursive) — confirmed no `filepath.Walk`/`filepath.WalkDir`/glob usage anywhere in `internal/storage/markdown.go` (only `os.ReadDir` appears, per the grep above and direct reading of the file).

**No existing migration/schema-upgrade precedent for filesystem layout — confirmed absent; a related but distinct precedent exists for the *index format*.**
- Repo-wide case-insensitive grep for `migrat|upgrade|legacy` across `*.go` files returns exactly two hits, both about the **index JSON format**, not the markdown file layout: a comment `// Empty commit in file with non-empty current = stale (migration case)` at `internal/storage/index.go:257`, and a test named `TestIndex_Load_MigratesOldFormat` at `internal/storage/storage_test.go:1549-1579`.
- That test's full mechanism: it writes an "old format" index file as a raw JSON array (`[{"id":1,"title":"Stale",...}]`, no `git_commit` wrapper) directly via `os.WriteFile` (`storage_test.go:1559-1560`), then calls `idx.Load()`. `Load()`'s actual code path (`internal/storage/index.go:237-276`) does `json.Unmarshal(data, &indexFile)` where `indexFile` is the current `IndexFile` struct (`GitCommit`/`Tasks`/`Relations`); unmarshaling a bare array into that struct fails, so `Load()` falls into `return idx.Rebuild()` (`index.go:247-249`), which calls `idx.storage.LoadAll()` and reconstructs the index from the real `.md` files, discarding the stale/old-format JSON entirely. **This is the only "migration" that exists in the codebase today, and it is push-button/implicit**: there is no explicit versioned-migration function, no `v1`/`v2` markers, no schema-version field anywhere (`grep -rniE "schemaversion|\"v1\"|\"v2\"|indexversion"` across all `.go` files returns zero hits) — it works purely because "index unparseable as current struct" is used as the rebuild trigger, and rebuilding is non-destructive to the real source of truth (the `.md` files). This establishes the codebase's *only* precedent style for "old thing found → silently regenerate from source of truth," but it operates purely on a JSON blob it fully controls and discards, not on renaming/moving real user-authored files on disk — the filesystem-layout migration this task requires (moving 87+ real files into new directories) is a **genuinely new category of risk** with no code precedent to mirror mechanically, only a philosophical one (documented in CLAUDE.md's "self-healing" language, itself only describing the index-rebuild case above).
- `docs/plans/2026-01-23-index-improvements-design.md:115` and `:137` document this same index-format migration-by-rebuild design ("When loading an old-format index... JSON unmarshal... will fail... triggers a rebuild automatically - no explicit migration code needed") — confirms this was a deliberate, documented design choice for the index, not an accident, and reinforces that it's the philosophical (not mechanical) precedent for the new filesystem migration.
- All other `migrat` hits in the repo are unrelated historical content: archived task bodies in `tasks/archive/*.md` (e.g. `079.md`, `081-086.md`) describing an unrelated Codex-plugin "implicit subagent roles migration," and `docs/plans/2026-07-10-implicit-subagent-roles-migration.md` (a different, unrelated migration effort for agent-role packaging, not storage).

**Single shared startup/init hook — confirmed identical for MCP server and every CLI invocation except `version`.**
- `Service.Initialize()` — `internal/task/service.go:102-116`:
  ```go
  // Initialize loads the index if directory exists (does not create directory)
  func (s *Service) Initialize() error {
  	// Only load index, don't create directory - that happens on first write
  	if err := s.index.Load(); err != nil {
  		return err
  	}
  	// Run auto-archive on startup if enabled
  	if s.config != nil && s.config.AutoArchive.Enabled {
  		if err := s.RunAutoArchive(); err != nil {
  			// Log but don't fail startup
  			log.Printf("auto-archive on startup failed: %v", err)
  		}
  	}
  	return nil
  }
  ```
  This is called from exactly two places in the whole codebase: `cmd/mcp-task-manager/main.go:36` (once, at MCP server process start, before `server.ServeStdio`), and `internal/cli/commands.go:28`, inside `initServiceWithConfig(cfg)` (`commands.go:21-33`).
- `initServiceWithConfig` is in turn called by **every** CLI subcommand handler, either directly (e.g. `cmdDelete` at `commands.go:329`, `cmdArchive` at `commands.go:381`, both via the `initService()` wrapper at `commands.go:35-48`) or via `loadConfig()` + `initServiceWithConfig(cfg)` directly (e.g. `cmdList` at `commands.go:62-73`, confirmed by direct read: `cfg, err := loadConfig()` → `checkProjectExists` → `svc, err := initServiceWithConfig(cfg)`). Every dispatch branch in `RunWithArgs` (`internal/cli/cli.go:157-221`: `list`, `get`, `next`, `create`, `update`, `delete`, `start`, `complete`, `archive`) routes to a `cmdX` function that calls `initServiceWithConfig`/`initService` and therefore `Initialize()`. The **only** exception is the `version` subcommand (`cli.go:152-155`), which returns before any service/config code runs at all.
- Conclusion (fact, not inference): `Service.Initialize()` is a single, reliable, already-existing injection point reached by both the long-running MCP server process (once, at startup) and by every one-shot CLI invocation (once per process, at the very top of every subcommand's work, before any read/write of tasks) — with the sole carve-out being the `version` subcommand which touches no task data anyway.

**Index rebuild path — cleanly separable from a filesystem migration, confirmed by reading the full logic.**
- `Index.Load()` — `internal/storage/index.go:237-276` — reads `.index.json`; on missing file or JSON-unmarshal failure, calls `idx.Rebuild()` (`:241`, `:248`); also treats a stale git commit or an empty-`GitCommit`-with-nonempty-current-commit as stale and rebuilds (`:251-260`).
- `Index.Rebuild()` — `internal/storage/index.go:171-179` — `tasks, err := idx.storage.LoadAll()` then `idx.rebuildFromTasks(tasks)`.
- `rebuildFromTasks` — `internal/storage/index.go:135-169` — resets in-memory state, populates `idx.entries` from the given `[]*task.Task`, rebuilds relation edges (including symmetric-type reverse-edge generation, `:151-159`), then calls `idx.Save()` (unless `len(tasks) == 0`).
- Because `Rebuild()`'s only filesystem dependency is `idx.storage.LoadAll()` (a single call, `index.go:173`), the index-rebuild logic itself requires **zero changes** to support the new layout, *provided* `MarkdownStorage.LoadAll()` (and `LoadAllArchived()`, used by `Service.ListArchived`/auto-archive-candidate paths, not shown in `Index` but present in `internal/task/service.go` per the original research) is updated to enumerate the new per-task-directory layout and return the same `[]*task.Task` shape it does today. This confirms the two concerns are cleanly separable **as long as migration runs and completes before any call to `LoadAll`/`Rebuild`** — i.e., migration must happen inside or immediately preceding `Service.Initialize()`, before `s.index.Load()` is invoked (`service.go:105`), otherwise a rebuild triggered mid-migration (or with a stale/missing index in a partially-migrated tree) would run `LoadAll()` against a filesystem in an inconsistent state (some tasks in old flat files, some already in new directories) — whether `LoadAll` post-migration-design must transparently handle *both* shapes simultaneously (defensive) or whether migration is guaranteed atomic-enough that `LoadAll` only ever sees one shape is unresolved (see Open Questions).

**Concurrent/partial-migration safety — closest existing precedent is `Archive()`, confirmed, with visible untreated edge cases.**
- `Archive(id int) error` — `internal/storage/markdown.go:212-219`:
  ```go
  func (s *MarkdownStorage) Archive(id int) error {
  	archiveDir := filepath.Join(s.dir, "archive")
  	if err := os.MkdirAll(archiveDir, 0755); err != nil {
  		return err
  	}
  	return os.Rename(s.taskPath(id), s.archivePath(id))
  }
  ```
  This is confirmed the closest — and only — existing "create directory, then move a file into it" precedent anywhere in the codebase (no other `MkdirAll` + `Rename` pairing exists in any `.go` file per the earlier repo-wide grep). Its error-handling style is minimal and has visible gaps relevant to a per-task migration:
  - If `MkdirAll` succeeds but the subsequent `os.Rename` fails (e.g. source file went missing between calls, permissions, cross-device link on some filesystems), `Archive` simply returns that error with **no rollback** — the now-existing (possibly newly created, possibly already-existing) `archiveDir` is left in place. This is harmless for `Archive()` today because `archiveDir` (`tasks/archive/`) is a single shared directory reused by every task, so "leaving it behind" is a no-op, not partial state. Under the new per-task-directory scheme, the equivalent operation would be `os.MkdirAll(tasks/{id})` + `os.Rename(old, tasks/{id}/{id}.md)` where the directory is now **task-specific**, so a `MkdirAll`-succeeds/`Rename`-fails outcome would leave an empty (or partially populated, if attached files were already moved) `tasks/{id}/` directory sitting where `LoadAll`'s "skip if not a recognized task dir" logic would need to explicitly tolerate or clean up — there is no code anywhere today that handles "directory exists but is empty/incomplete."
  - No `.tmp`-suffix or lock-file convention is used for the `Archive`/directory-move operation at all — that convention (`<path>.tmp` + `os.Rename`, per `Save()` at `markdown.go:72-77` and `Index.Save()` at `index.go:225-231`) exists only for single-file *content* writes, never for directory moves. There is therefore no existing pattern in this codebase for "detect and recover from a `.tmp` or half-moved artifact left behind by a crash," which a directory-per-task migration (crash between `MkdirAll` and `Rename`, or between renaming the `.md` file and renaming/copying attached files) would need to define from scratch.

**Test fixtures with hardcoded flat-path expectations — confirmed to exist exclusively in one file.**
- Repo-wide grep for `.md"` inside every `*_test.go` file in the repo returns hits only in `internal/storage/storage_test.go`, at exactly six lines (confirmed no `_test.go` file outside `internal/storage` references a task markdown filename at all — `internal/cli/cli_test.go`'s `os.MkdirAll` calls at lines 310/373/530 are unrelated project-discovery scaffolding, not task-file assertions):
  1. `internal/storage/storage_test.go:38` — inside `TestMarkdownStorage_SaveAndLoad` (func starts `:16`): `if _, err := os.Stat(filepath.Join(dir, "001.md")); os.IsNotExist(err) { ... }`.
  2. `internal/storage/storage_test.go:1511` — inside `TestIndex_Load_RebuildOnGitChange` (func starts `:1494`): `if _, err := os.Stat(filepath.Join(dir, "001.md")); err != nil { ... }`.
  3. `internal/storage/storage_test.go:1642` — inside `TestMarkdownStorage_SaveLoad_WithoutRelations` (func starts `:1622`): `data, err := os.ReadFile(filepath.Join(dir, "001.md"))`.
  4. `internal/storage/storage_test.go:2097` — inside `TestMarkdownStorage_Archive` (func starts `:2087`): `if _, err := os.Stat(filepath.Join(dir, "001.md")); os.IsNotExist(err) { t.Fatal("expected 001.md to exist before archiving") }`.
  5. `internal/storage/storage_test.go:2107` — same test: `if _, err := os.Stat(filepath.Join(dir, "001.md")); !os.IsNotExist(err) { t.Error("expected 001.md to be removed after archiving") }`.
  6. `internal/storage/storage_test.go:2112` — same test: `if _, err := os.Stat(filepath.Join(dir, "archive", "001.md")); os.IsNotExist(err) { t.Error("expected archive/001.md to exist after archiving") }`.
- All six are direct filesystem assertions in storage-layer tests using real `t.TempDir()` fixtures (no golden files/fixture directories exist in the repo — confirmed no `testdata/` directory anywhere via the project structure). Every one of these six assertions encodes the flat-path assumption directly and will need rewriting to assert against `tasks/{id}/{id}.md` / `tasks/archive/{id}/{id}.md` shapes instead. No test outside this one file is affected by the path-shape change itself (mock-based tests in `internal/task/service_test.go` and `internal/cli/cli_test.go` operate above the storage abstraction and don't encode path literals).

**Real on-disk archive directory — inspected directly; confirmed shape and one anomaly worth flagging.**
- `ls /Users/iga/Workspace/mcp-task-manager/tasks/` → contains only `.index.json` and `archive/` (no active `tasks/{id}.md` files exist right now, consistent with prior research).
- `ls /Users/iga/Workspace/mcp-task-manager/tasks/archive/` → **87 files** (not 89 — recount as of today differs from the number quoted in the pre-expansion research above, which said "89 files"; current exact count confirmed twice, via `ls | wc -l` and independently via `git ls-files tasks/ | wc -l`, both = 87). All 87 filenames match `^[0-9]{3}\.md$` exactly (confirmed via `ls tasks/archive/ | grep -vE '^[0-9]{3}\.md$'` returning zero anomalous names — no `.gitkeep`, no stray `.tmp`, no non-numeric names).
- **Numbering gap confirmed**: filenames run `001.md`...`070.md` (70 files) then jump to `073.md`...`089.md` (17 files) — **`071.md` and `072.md` are missing** from the archive (and are not present as active tasks either, per the empty active-`.md` scan). This is consistent with those two IDs having been deleted (not archived) at some point — `NextID()`'s `maxMarkdownTaskID` logic tolerates gaps fine since it only tracks the maximum seen ID, but the design/migration phase should not assume archive IDs are contiguous or assume "89 = count," only "089 = max ID, 87 = actual file count."
- No leftover `.tmp` files, no empty directories, no partial-write artifacts of any kind currently present in `tasks/` or `tasks/archive/` — the real data migration will start from a clean, fully-flat, fully-consistent state.

**Git tracking status of `tasks/` — confirmed: archive is tracked, index cache is gitignored.**
- `.gitignore` (repo root) contains exactly one `tasks/`-related line: `tasks/.index.json` (gitignoring only the index cache, not the directory itself or its contents).
- `git ls-files tasks/` lists all 87 `tasks/archive/NNN.md` files as tracked — confirmed by exact count match (87) between `ls tasks/archive/ | wc -l` and `git ls-files tasks/ | wc -l`.
- `git status --short` on the current working tree shows only `?? .tasks/` (the unrelated dotted workflow-artifacts directory) as untracked — the real `tasks/archive/*.md` files are all clean/committed, confirmed no pending local modifications to any archived task file.
- **Practical consequence for migration risk**: every one of the 87 real archived task files migration must touch is already committed to git — if a migration bug corrupts or loses content during the flat→directory move, the original content is recoverable via `git checkout`/`git log` on the old paths, materially lowering the real-world risk of the "renames 87+ files automatically on startup" requirement compared to if this were gitignored, uncommitted local data.

**Exact current doc wording describing the flat layout — confirmed line numbers (CLAUDE.md and AGENTS.md remain byte-identical; `diff` returns empty).**
- ASCII architecture diagram: `./tasks/*.md           ./tasks/.index.json` and `./tasks/archive/*.md   (archived tasks, no index)` — `CLAUDE.md:25-26` (identical in `AGENTS.md:25-26`).
- Storage bullets: `- **Source of truth:** Markdown files with YAML frontmatter (`./tasks/*.md`)` and `- **Archive:** `./tasks/archive/*.md` — archived tasks (same format, no index)` — `CLAUDE.md:55-56` (identical `AGENTS.md:55-56`).
- Task Identification: `- Filenames: `001.md`, `002.md`, etc.` — `CLAUDE.md:85` (identical `AGENTS.md:85`).
- Archiving → Storage: `- Archived files move to `tasks/archive/` (same markdown format, unchanged)` — `CLAUDE.md:157` (identical `AGENTS.md:157`).
- MCP tool table: `| `archive_task` | Archive a completed task (moves to `tasks/archive/`) |` — `CLAUDE.md:181` (identical `AGENTS.md:181`).
- README.md's separate (already-diverged, per original research) mentions: `- **Markdown-based storage** - Tasks stored as `.md` files with YAML frontmatter` (`README.md:11`), the intro sentence "Tasks are stored as human-readable Markdown files with YAML frontmatter, making them easy to version control and inspect" (`README.md:7`), and the Project Structure ASCII tree line `├── tasks/                   # Task storage (created at runtime)` (`README.md:312`). README's "Task Format" section (`README.md:245-267`) describes only the YAML/markdown *content* shape, not file paths, so it needs no changes for this restructuring.
- `docs/plans/2026-03-13-task-archiving-design.md` and `docs/plans/2026-03-26-lazy-index-creation.md` also reference `tasks/*.md`/`archive/*.md` paths as historical design-doc context (not confirmed as requiring updates — they're point-in-time design records, per the existing repo convention of not retroactively editing historical `docs/plans/*.md` files, unlike CLAUDE.md/AGENTS.md/README.md which describe *current* behavior).

### Evidence Map (addendum)

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Full blast radius of path-shape change | Confined to `internal/storage/markdown.go` (+ its test file); no other package constructs task-ID paths | repo-wide grep for `taskPath\|archivePath\|%03d\|\.md"` and for `os.ReadDir\|os.Rename\|os.MkdirAll` |
| `LoadAll` directory-skip logic | `entry.IsDir()` explicitly skipped — must change for per-task dirs to be seen | `internal/storage/markdown.go:106` |
| `LoadAllArchived` directory-skip logic | Same `entry.IsDir()` skip | `internal/storage/markdown.go:243` |
| `NextID` ID derivation | Strips `.md` off filename via `strings.TrimSuffix`, skips dirs | `internal/storage/markdown.go:296-303` |
| Existing filesystem-migration precedent | None; only precedent is index-format-rebuild-on-unmarshal-failure (JSON blob only, not real files) | `internal/storage/index.go:237-276`; `storage_test.go:1549-1579`; `docs/plans/2026-01-23-index-improvements-design.md:115,137` |
| Shared startup hook (both MCP + CLI) | `Service.Initialize()`, called once by server main and once per CLI invocation (except `version`) | `internal/task/service.go:102-116`; `cmd/mcp-task-manager/main.go:36`; `internal/cli/commands.go:21-33`; dispatch at `internal/cli/cli.go:152-221` |
| Index rebuild vs. migration separability | `Rebuild()`'s only FS dependency is `storage.LoadAll()`; cleanly separable **if** migration completes before `index.Load()` runs | `internal/storage/index.go:171-179`, `:237-276` |
| Closest "mkdir then move" precedent | `Archive()`: `MkdirAll` + `Rename`, no rollback on partial failure, no `.tmp` convention for directory moves | `internal/storage/markdown.go:212-219` |
| Hardcoded flat-path test assertions | 6 lines, all in one file | `internal/storage/storage_test.go:38,1511,1642,2097,2107,2112` |
| Real archive directory shape | 87 files, all `NNN.md`, gap at 071/072, no anomalies | `ls tasks/archive/`; `git ls-files tasks/` |
| Git tracking of `tasks/` | `archive/*.md` tracked/committed; only `.index.json` gitignored | `.gitignore`; `git ls-files tasks/`; `git status --short` |
| Doc lines needing rewording | 5 lines in CLAUDE.md/AGENTS.md (identical), 3 in README.md | `CLAUDE.md:25-26,55-56,85,157,181` = `AGENTS.md` same; `README.md:7,11,312` |

### Relevant Files (addendum)

1. `/Users/iga/Workspace/mcp-task-manager/internal/storage/markdown.go` — sole location of all flat-path construction (`taskPath`, `archivePath`, `maxMarkdownTaskID`) and all directory-skip enumeration logic (`LoadAll`, `LoadAllArchived`); the entire restructuring is contained here at the storage layer.
2. `/Users/iga/Workspace/mcp-task-manager/internal/storage/storage_test.go` — the only test file with hardcoded flat-path literals (6 lines, 4 test functions); must be updated in lockstep with `markdown.go`.
3. `/Users/iga/Workspace/mcp-task-manager/internal/storage/index.go` — `Load`/`Rebuild`/`rebuildFromTasks` (`:135-276`); needs no direct changes for the new layout itself but is the code that must run *after* migration completes, making `Service.Initialize()`'s call order the critical sequencing point.
4. `/Users/iga/Workspace/mcp-task-manager/internal/task/service.go:102-116` — `Initialize()`, the single confirmed injection point reachable from both the MCP server and every CLI invocation; a migration step would need to run here, before `s.index.Load()`.
5. `/Users/iga/Workspace/mcp-task-manager/cmd/mcp-task-manager/main.go:29-38` and `/Users/iga/Workspace/mcp-task-manager/internal/cli/commands.go:21-48` — the two (parallel, duplicated) construction sites for `MarkdownStorage`/`Index`/`Service` that both call `Initialize()`.
6. `/Users/iga/Workspace/mcp-task-manager/tasks/archive/` — the real 87-file dataset the migration must handle correctly on this very machine; already inspected directly (see Confirmed Facts).
7. `/Users/iga/Workspace/mcp-task-manager/CLAUDE.md` and `/Users/iga/Workspace/mcp-task-manager/AGENTS.md` (identical) — 5 line locations needing literal rewording; `/Users/iga/Workspace/mcp-task-manager/README.md` — 3 line locations.

### Inference (addendum)

- Whether `LoadAll`/`LoadAllArchived` should be designed to tolerate a **mixed** filesystem state (some tasks still flat, some already migrated) as an ongoing defensive posture, versus assuming migration always fully completes before any load — is not settled by any existing code; it's inferred as an open concern from the fact that `Index.Load()` can trigger `Rebuild()`→`LoadAll()` independently of any explicit migration call (e.g. via the git-commit-staleness check at `index.go:251-260`, or a corrupt-index fallback at `:247-249`), so if a migration step were ever skipped, deferred, or run non-atomically relative to `Initialize()`'s call order, `LoadAll` could be invoked against a partially-migrated tree with no existing code path anticipating that state.

### Unknown (addendum)

- **What should happen if a directory named after a task ID already unexpectedly exists** at migration time (e.g. `tasks/42/` exists as a stray/leftover directory when task `42.md` is still flat and needs migrating into `tasks/42/42.md`) — no code or test anywhere addresses a naming collision between a to-be-created migration target directory and pre-existing content.
- **What happens if migration is interrupted mid-way and rerun** — e.g. process killed after `MkdirAll(tasks/42/)` but before `Rename(tasks/42.md → tasks/42/42.md)` (leaving both an empty `tasks/42/` dir and the original flat `tasks/42.md` present simultaneously), or killed after the `.md` rename but before any attached files would be moved (N/A yet since attached files don't exist pre-feature, but relevant once `write_task_file` exists) — is migration idempotent/re-runnable safely today? No, because no migration code exists yet; this is a genuine design-phase decision with zero code precedent to fall back on (per the "No existing migration/schema-upgrade precedent" finding above).
- **Whether `.index.json` needs a format change, or just a full rebuild after migration** — evidence suggests a full rebuild is sufficient and requires no `IndexFile`/`IndexEntry` schema change, since `IndexEntry` (`index.go:49-58`) stores no path information at all (only `ID`/`ParentID`/`Title`/`Status`/`Priority`/`Type`/timestamps) — paths are always re-derived from `MarkdownStorage.taskPath`/`archivePath` at read time, never cached in the index. This is a strong signal but not a certainty until the design phase confirms no new index field (e.g., "has attached files") is needed.
- **Whether concurrent CLI-invocation-during-server-migration is a realistic concern** — CLAUDE.md explicitly documents "MVP assumes single-server, no locking" (already noted in the original research's scope, not re-verified with new evidence here), and no file-locking primitive exists anywhere in the codebase (confirmed: no `flock`/`syscall.Flock`/lockfile library in `go.mod` or imports). Whether "migrate on startup" needs any interlock against a second process (e.g., a concurrently-invoked CLI command) reading/writing the same `tasks/` directory mid-migration is unresolved and inherits the pre-existing no-locking limitation rather than introducing a new one — but migration is a heavier, multi-file operation than any existing single-file atomic write, so the design phase should explicitly decide whether this raises the risk profile enough to warrant a guard (e.g., a migration marker/lock file) that has no precedent in this codebase today.
- **Exact trigger condition for "is this task still in old flat layout"** — is it purely "a `tasks/{id}.md` file exists" (simple existence check), or does it need to also positively identify `tasks/{id}/{id}.md` as *not yet* existing to avoid double-migrating, and how should it behave if *both* the old flat file and a new-layout directory exist for the same ID simultaneously (e.g., a prior interrupted migration)? No code exists to answer this; flagged as blocking for the design phase's migration-function contract.
