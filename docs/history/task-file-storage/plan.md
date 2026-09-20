# Plan: task-file-storage

### Planning Summary

Six phases, strictly ordered in two groups. Group A (Phases 1-2) restructures on-disk storage from flat `tasks/{id}.md` files to per-task directories `tasks/{id}/{id}.md`, with automatic migration of the 87 real archived tasks and any active tasks. Group B (Phases 3-5) adds the attached-file capability (`FileStorage` + `Service` methods + MCP tools + CLI subcommands) on top of the new layout. Phase 6 closes out documentation. No code generation exists in this project, so "Regeneration required" is "Not applicable" for every phase.

**One correction to the approved design, required for compilability, not a redesign:** the design proposed calling a package-level function `storage.MigrateFlatLayout(dir string) error` directly from `internal/task/service.go`. This is not implementable — `internal/storage` imports `internal/task` (for the `Task` type), so `internal/task` importing `internal/storage` back would create an import cycle. Phase 2 instead adds `MigrateFlatLayout() error` to the existing `Storage` interface (declared in `task/service.go`, implemented by `MarkdownStorage` using its own `dir` field), matching the codebase's existing dependency-injection convention (`Storage`/`ArchiveStorage`/`Index` are interfaces in `task`, concretely implemented in `storage`). Behavior is unchanged; only the call mechanism differs from the design doc's literal wording.

**One scope correction surfaced by grepping `internal/task/service_test.go`:** the design says `NewService`'s new `fileStorage` parameter needs updating "at both call sites" (`main.go`, `commands.go`). In fact `service_test.go` calls `NewService(...)` directly, positionally, at **~40 additional call sites** (no shared test-construction helper exists except `newServiceWithArchive`, used only 5 times). Phase 3 accounts for this as a distinct, mechanical, batch-editable sub-task rather than a surprise mid-phase.

Both open questions left unresolved in the design doc are decided here so Phase 3/4 implementation is unblocked:
- **Empty list convention**: MCP `list_task_files` returns the text `"No files attached to task N"` when empty (matching `listTasksHandler`'s existing "No tasks found" / "No archived tasks found" convention). CLI `list-task-files` prints nothing on an empty result (idiomatic for a script-facing, `--json`-less, one-name-per-line command, consistent with tools like `ls`).
- **Filename validation**: reject empty/whitespace-only names, any path separator (`/`, and `\` for cross-platform safety), any `..` path segment, and the exact reserved name `{id}.md`. Leading dots (hidden files) and case-only collisions are explicitly out of scope — no existing precedent or acceptance criterion requires them, and adding them would be speculative.

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| P1 | Per-task directory layout in `MarkdownStorage` | `internal/storage/markdown.go`, `internal/storage/storage_test.go` | N/A | none | `go test ./internal/storage/...` |
| P2 | Auto-migrate old flat files into new layout | `internal/storage/migrate.go` (new), `internal/storage/migrate_test.go` (new), `internal/task/service.go`, `internal/task/service_test.go` | N/A | P1 | `go test ./internal/storage/... ./internal/task/...` |
| P3 | `FileStorage` + `Service` attached-file methods | `internal/storage/files.go` (new), `internal/storage/storage_test.go`, `internal/task/service.go`, `internal/task/service_test.go`, `cmd/mcp-task-manager/main.go`, `internal/cli/commands.go` | N/A | P1 | `go test ./internal/storage/... ./internal/task/...`; `go build ./...` |
| P4 | MCP tools `write_task_file`/`read_task_file`/`list_task_files` | `internal/tools/files.go` (new), `internal/tools/tools.go`, `internal/tools/tools_test.go` | N/A | P3 | `go test ./internal/tools/...` |
| P5 | CLI subcommands `write-task-file`/`read-task-file`/`list-task-files` | `internal/cli/cli.go`, `internal/cli/commands.go`, `internal/cli/cli_test.go` | N/A | P3 | `go test ./internal/cli/...` |
| P6 | Documentation sync | `CLAUDE.md`, `AGENTS.md`, `README.md` | N/A | P1-P5 | manual diff review; no build/test impact |

### Phase Details

- `Phase ID:` P1 — storage-path-restructure
- `Goal:` Replace the flat `tasks/{id}.md` / `tasks/archive/{id}.md` layout with per-task directories `tasks/{id}/{id}.md` / `tasks/archive/{id}/{id}.md` inside `MarkdownStorage`, with the `.md` file's own content format completely unchanged.
- `Why this phase exists:` Attached files (P3+) need a per-task directory to live in; this must land first per the design's stated ordering, and is self-contained enough to verify in isolation before anything else builds on it.
- `Files likely to change:`
  - `internal/storage/markdown.go`: replace `taskPath`/`archivePath` with `taskDir(id)`, `taskPath(id)`, `archiveTaskDir(id)`, `archivePath(id)`; `Save` gains `MkdirAll(taskDir(id))` before the existing temp-write+rename; `Delete` becomes `os.RemoveAll(taskDir(id))`; `LoadAll`/`LoadAllArchived` invert the `entry.IsDir()` skip to *require* a directory whose name parses as a positive integer, then read `{name}/{name}.md` from inside it (skip, don't fail, on parse errors or a missing inner `.md`); `Archive` becomes `MkdirAll(tasks/archive) + os.Rename(taskDir(id), archiveTaskDir(id))`; `NextID`/`maxMarkdownTaskID` parse the max ID from directory names (require `entry.IsDir()`, parse the bare name, no `.md` trim).
  - `internal/storage/storage_test.go`: rewrite the 6 hardcoded flat-path assertions (`:38`, `:1511`, `:1642`, `:2097`, `:2107`, `:2112` per research) to the nested shape; add new tests (below).
- `Regeneration required:` Not applicable — no code generation in this project.
- `Dependencies:` None. First phase.
- `Implementation tasks:`
  1. Add `taskDir`/`archiveTaskDir` helpers; keep `%03d` zero-padded ID formatting unchanged (only its use as a directory name is new, not its textual form).
  2. Update `Save`, `Load`, `Delete`, `Archive`, `LoadAll`, `LoadAllArchived`, `NextID`, `maxMarkdownTaskID` per above.
  3. Keep the existing atomic-write pattern (`<path>.tmp` + `os.Rename`) for the `.md` file itself, unchanged — only its parent directory changes.
- `Tests and checks:`
  - Update: the 6 hardcoded-path assertions listed above.
  - New: `TestMarkdownStorage_SaveCreatesTaskDir`, `TestMarkdownStorage_LoadAll_NestedLayout`, `TestMarkdownStorage_LoadAllArchived_NestedLayout`, `TestMarkdownStorage_NextID_NestedLayout`, `TestMarkdownStorage_Archive_MovesWholeDirectory` (write a sibling file into the task dir before archiving, assert it survives at the new archive path — this is the load-bearing test proving the "cascade-free" design claim before any attached-file code exists), `TestMarkdownStorage_Delete_RemovesWholeDirectory`.
  - Run: `go build ./...`, `go vet ./...`, `gofmt -l internal/storage`, `go test ./internal/storage/...`.
- `Commit boundary:` This phase alone. It does not touch the real `tasks/archive/*.md` data (87 files) — those remain flat on disk until P2 lands; all new/updated tests operate on `t.TempDir()` fixtures only, so the commit is safe in isolation even though running the actual CLI/server against the real `tasks/` directory between this commit and P2's would (temporarily) see zero tasks. That gap only matters for real manual use, not for automated verification, and is closed by the very next phase.
- `Definition of done:` `go test ./internal/storage/...` green; `go build ./...` green; no other package touched.
- `Rollback note:` Revert `markdown.go` + `storage_test.go`. No real on-disk data was migrated in this phase, so there is nothing to undo outside the working tree.

---

- `Phase ID:` P2 — flat-layout-migration
- `Goal:` Detect any task still stored in the old flat layout (active or archived) and migrate it in place into the new per-directory layout, automatically, on every server/CLI startup, before the index loads.
- `Why this phase exists:` P1 alone makes the real `tasks/archive/*.md` (87 files) invisible to `LoadAll`/`LoadAllArchived` (they now expect directories). This phase is what actually makes the restructuring safe to run against the real repo data, and is independently testable via pure-filesystem tests before any Service wiring.
- `Files likely to change:`
  - `internal/storage/migrate.go` (new): `func (s *MarkdownStorage) MigrateFlatLayout() error`, using `s.dir` (no need for a separate `dir string` parameter — `MarkdownStorage` already carries it).
  - `internal/task/service.go`: add `MigrateFlatLayout() error` to the `Storage` interface; call `s.storage.MigrateFlatLayout()` at the top of `Initialize()`, before `s.index.Load()`.
  - `internal/storage/migrate_test.go` (new).
  - `internal/task/service_test.go`: add a no-op `MigrateFlatLayout() error { return nil }` to `mockStorage` (and any other `Storage`-implementing test double, e.g. `mockStorageWithEnsureDirTracking` at `:316`) so existing tests keep compiling; add one new tracking test proving `Initialize()` calls migration before `index.Load()`.
- `Regeneration required:` Not applicable.
- `Dependencies:` P1 (needs `taskDir`/`archiveTaskDir` to exist).
- `Implementation tasks:`
  1. `MigrateFlatLayout()` scans `s.dir` and `s.dir/archive` with `os.ReadDir` for **file** entries (not directories) matching `^\d+\.md$` — the exact inverse of the old `LoadAll` skip condition, so it can never match anything already migrated.
  2. For each match with numeric id `N`: `MkdirAll(dir/N)` then `os.Rename(old flat path, dir/N/N.md)`. On a per-ID error, `log.Printf` and continue with the remaining IDs — never abort the whole migration (mirrors `RunAutoArchive`'s existing per-item-tolerant style).
  3. Idempotent by construction: once a flat file is renamed away, its own existence (the sole trigger condition) no longer holds, so a re-run — including one that resumes after a crash between `MkdirAll` and `Rename` — naturally retries only the IDs still in the old shape.
  4. Wire the call into `Service.Initialize()`, strictly before `s.index.Load()` (the single call path reached by both the MCP server and every CLI invocation except `version`).
  5. Add the interface method to `Storage` in `service.go` (this is the design-doc correction from the Planning Summary — a method on the existing `Storage` interface, not a cross-package function call).
- `Tests and checks:`
  - New (`internal/storage/migrate_test.go`): `TestMigrateFlatLayout_ActiveAndArchived`, `TestMigrateFlatLayout_Idempotent` (run twice; second run is a no-op, no error), `TestMigrateFlatLayout_PreservesContent` (byte-for-byte frontmatter+body equality before/after), `TestMigrateFlatLayout_NoFlatFiles_NoOp`, `TestMigrateFlatLayout_SkipsAndLogsOnPerIDCollision` (pre-create a conflicting `tasks/{id}/` directory whose expected `{id}.md` path is itself unwritable/occupied — assert the function still returns `nil` overall and every *other* ID still migrates).
  - New (`internal/task/service_test.go`): a tracking mock (mirroring `mockStorageWithEnsureDirTracking`'s existing pattern at `:316`) asserting `Initialize()` invokes `MigrateFlatLayout()` exactly once, before `index.Load()` is observed to run.
  - Run: `go build ./...`, `go test ./internal/storage/... ./internal/task/...`.
  - Manual validation (not a commit gate, but required before considering the feature done): run the built CLI once against the real `tasks/` directory (already git-tracked, so recoverable) and confirm `list --archived` / `get --archived <id>` still resolve a sample of the 87 real archived tasks post-migration.
- `Commit boundary:` This phase alone; depends on P1 already being committed.
- `Definition of done:` All new migration tests green; existing `service_test.go` suite still compiles and passes with the added no-op mock method; `go build ./...` green.
- `Rollback note:` Revert `migrate.go`, the `Initialize()` call, and the `Storage` interface addition. If migration has already run against the real repo, the moved files are still git-tracked at their new paths — `git mv` history / `git log --follow` recovers origin; nothing is deleted, only renamed.

---

- `Phase ID:` P3 — attached-file-storage-and-service
- `Goal:` Add the `FileStorage` interface and its `MarkdownStorage` implementation, plus `Service.WriteTaskFile`/`ReadTaskFile`/`ListTaskFiles`, so attached files can be written/read/listed inside a task's directory with correct active-vs-archived resolution.
- `Why this phase exists:` This is the actual feature; P1/P2 only prepared the ground for it. Kept as its own phase (separate from MCP tools/CLI) so the storage+service layer can be fully tested via existing mock/real-filesystem conventions before any transport-layer code is added.
- `Files likely to change:`
  - `internal/storage/files.go` (new): `WriteFile`, `ReadFile`, `ListFiles` on `MarkdownStorage`, implementing the new `FileStorage` interface.
  - `internal/storage/storage_test.go`: new tests for the above.
  - `internal/task/service.go`: new `FileStorage` interface; `Service` gains a `fileStorage FileStorage` field; `NewService` gains a new parameter, inserted after `archiveStorage`: `NewService(storage Storage, archiveStorage ArchiveStorage, fileStorage FileStorage, index Index, validTypes []string, cfg *config.Config) *Service`; new methods `WriteTaskFile`, `ReadTaskFile`, `ListTaskFiles`.
  - `internal/task/service_test.go`: add `mockFileStorage` test double (mirroring `mockArchiveStorage`'s shape); **update all ~40 existing `NewService(...)` call sites** to insert the new argument (see Planning Summary — this is a known, mechanical, batch-editable diff, not scope creep); extend `newServiceWithArchive()` (or add a sibling `newServiceWithFiles()`) to also construct a `mockFileStorage`.
  - `cmd/mcp-task-manager/main.go:35`: `svc := task.NewService(mdStorage, mdStorage, mdStorage, index, cfg.TaskTypes, cfg)` (same `MarkdownStorage` value satisfies all three interfaces — no new construction needed, matching the design's "no new type" decision).
  - `internal/cli/commands.go:26`: identical change to the mirrored construction site.
- `Regeneration required:` Not applicable.
- `Dependencies:` P1 (needs `taskDir`/`archiveTaskDir`). Does not strictly depend on P2, but should land after it so the real repo is never left in a state where attached-file writes could target a not-yet-migrated flat task.
- `Implementation tasks:`
  1. Declare `FileStorage` interface in `task/service.go`: `WriteFile(taskID int, filename, content string) error`, `ReadFile(taskID int, filename string) (string, error)`, `ListFiles(taskID int) ([]string, error)`.
  2. Implement on `MarkdownStorage` in `storage/files.go`, reusing `taskDir`/`archiveTaskDir`. Add a shared `validateFilename(name string) error` helper enforcing the Planning Summary's filename rules (non-empty after trim, no `/` or `\`, no `..` path segment, not equal to the task's own `{id}.md` name). Apply it in `WriteFile`; apply the same traversal/empty checks (but not the `{id}.md` reservation, which is a write-only concern) in `ReadFile` since it also joins caller input into a path.
  3. `WriteFile`: atomic temp-write + rename inside `taskDir(id)`, mirroring `Save`'s existing pattern.
  4. `ReadFile`: `os.ReadFile(taskDir(id)/filename)`; distinguish "file not found" from a not-yet-created task directory in the returned error text.
  5. `ListFiles`: `os.ReadDir(taskDir(id))`, return names excluding `{id}.md` itself; empty slice (not an error) when the directory has no other files.
  6. `Service.WriteTaskFile(taskID, filename, content) error`: look up via `s.index.Get(taskID)` (active-only — explicitly *not* `s.Get`, which falls back to archive and would otherwise let a write silently target an archived task). If not found active, check `s.archiveStorage.IsArchived(taskID)`: if true, return a clear "task N is archived; files are read-only" error; if false, "task not found". If found active, delegate to `s.fileStorage.WriteFile`.
  7. `Service.ReadTaskFile(taskID, filename) (string, error)` / `Service.ListTaskFiles(taskID) ([]string, error)`: resolve active-vs-archived the same way (`s.index.Get` then `IsArchived` fallback check) — reads work on both; writes only on active. Apply the Planning Summary's empty-list convention (`"No files attached to task N"`) at the tool layer in P4, not here — `Service.ListTaskFiles` itself just returns `[]string{}` for "no files," keeping the service layer transport-agnostic.
  8. Batch-update the ~40 `NewService(...)` call sites in `service_test.go`: group by existing literal argument pattern (`NewService(newMockStorage(), nil, newMockIndex(), ...)` → insert `nil,` before `newMockIndex()`; `NewService(ms, as, newMockIndex(), ...)` inside `newServiceWithArchive`-style tests → insert the new mock or `nil`; `NewService(nil, nil, nil, nil, cfg)` → insert a fifth `nil`), then verify the total call count is unchanged (`grep -c 'NewService(' internal/task/service_test.go` before/after) and `go vet`/`go build` catch any missed site.
- `Tests and checks:`
  - New (`storage_test.go`): `TestMarkdownStorage_WriteFile_CreatesAndOverwrites`, `TestMarkdownStorage_ReadFile_NotFound`, `TestMarkdownStorage_ReadFile_TaskNotFound`, `TestMarkdownStorage_ListFiles_ExcludesTaskMarkdown`, `TestMarkdownStorage_ListFiles_EmptyWhenNoAttachedFiles`, `TestMarkdownStorage_WriteFile_RejectsTraversalAndReservedName` (table-driven over `../x`, `a/b`, `a\b`, ``, `   `, `{id}.md`).
  - New (`service_test.go`): `TestService_WriteTaskFile_ActiveTask`, `TestService_WriteTaskFile_RejectsArchivedTask`, `TestService_WriteTaskFile_RejectsUnknownTask`, `TestService_ReadTaskFile_ActiveAndArchived`, `TestService_ListTaskFiles_ActiveAndArchived`, `TestService_ListTaskFiles_EmptyReturnsEmptySlice`.
  - Extend one existing `TestService_ArchiveTask_*` and one `TestService_Delete_*` case with a "mockFileStorage recorded zero calls during Archive/Delete" assertion — proves the "cascade-free" design claim actually holds at the service layer, not just at the storage layer (P1 already proved the storage-layer half).
  - Run: `go build ./...`, `go vet ./...`, `go test ./internal/storage/... ./internal/task/...`.
- `Commit boundary:` This phase alone; the ~40-call-site test update is mechanical but still belongs in this same commit since it's a direct, inseparable consequence of the `NewService` signature change in this phase.
- `Definition of done:` All new/updated tests green; `go build ./...` green (proves every `NewService` call site — production and test — was updated); `main.go` and `commands.go` construction sites updated.
- `Rollback note:` Revert `files.go`, the `FileStorage` interface/field/methods in `service.go`, and the `NewService` signature (which forces reverting the ~40 test call-site edits too — treat this phase as one atomic unit for rollback purposes).

---

- `Phase ID:` P4 — mcp-file-tools
- `Goal:` Register `write_task_file`, `read_task_file`, `list_task_files` as MCP tools.
- `Why this phase exists:` Exposes the P3 service methods to MCP clients, following the existing `add_relation`/`remove_relation` template exactly.
- `Files likely to change:`
  - `internal/tools/files.go` (new): `registerFileTools(s *server.MCPServer, svc *task.Service)`, three tool schemas + handlers.
  - `internal/tools/tools.go`: `Register` gains a fourth call, `registerFileTools(s, svc)`.
  - `internal/tools/tools_test.go`: schema-shape assertions.
- `Regeneration required:` Not applicable.
- `Dependencies:` P3.
- `Implementation tasks:`
  1. `write_task_file(task_id number required, filename string required, content string required)` → `svc.WriteTaskFile`; success returns a plain text confirmation; errors via `mcp.NewToolResultError(err.Error())`, never a Go `error` return (existing convention).
  2. `read_task_file(task_id number required, filename string required)` → call `svc.EnsureProjectExists()` first (read-tool guard, matching `getTaskHandler`), then `svc.ReadTaskFile`; return raw file content as the tool's text result.
  3. `list_task_files(task_id number required)` → same `EnsureProjectExists()` guard, then `svc.ListTaskFiles`; on empty result return the text `"No files attached to task N"` (Planning Summary decision); otherwise one name per line (or the existing list-rendering convention used by other list tools — match it, don't invent a new one).
- `Tests and checks:`
  - New in `tools_test.go`, following `TestRegisterDocumentsAllowedTypeValues`'s existing style: assert `write_task_file`/`read_task_file`/`list_task_files` are registered with the expected required-parameter shape (no enums involved, so this is a lighter assertion than the existing type-enum test).
  - Run: `go build ./...`, `go test ./internal/tools/...`.
- `Commit boundary:` This phase alone.
- `Definition of done:` Three tools registered and schema-tested; `go build ./...` green.
- `Rollback note:` Revert `files.go` (new) and the one added line in `tools.go`. No effect on P3's service layer.

---

- `Phase ID:` P5 — cli-file-commands
- `Goal:` Add `write-task-file`, `read-task-file`, `list-task-files` CLI subcommands.
- `Why this phase exists:` CLI mirrors every MCP tool in this project; keeps CLI and MCP surfaces in lockstep, independent of P4 (both build directly on P3's service methods, not on each other).
- `Files likely to change:`
  - `internal/cli/cli.go`: three new `flaggy.NewSubcommand(...)` blocks + `flaggy.AttachSubcommand` + three new dispatch branches in `RunWithArgs`. None register a `--json` flag (deliberate, scoped exception documented in the design — file content and filename lists have no meaningfully distinct JSON shape).
  - `internal/cli/commands.go`: three new `cmdX(stdout, stderr io.Writer, ...) int` functions, structurally like `cmdArchive`/`cmdDelete` but without a `jsonOutput` parameter.
  - `internal/cli/cli_test.go`: new tests following the `t.TempDir()` + `MCP_TASKS_DIR` + `RunWithArgs` pattern (e.g. `TestArchiveCommand` at `:392`).
- `Regeneration required:` Not applicable.
- `Dependencies:` P3.
- `Implementation tasks:`
  1. `write-task-file <task-id> <filename> <content>` (or a `--content`/stdin form if content may be long — decide during implementation by checking how other CLI commands handle multi-word/long string args; default to a positional arg unless flaggy's existing usage elsewhere in `cli.go` shows a stdin convention) → `cmdWriteTaskFile` prints `Wrote file "notes.md" to task #12.` on success.
  2. `read-task-file <task-id> <filename>` → `cmdReadTaskFile` prints the raw file content to stdout, nothing else appended.
  3. `list-task-files <task-id>` → `cmdListTaskFiles` prints one filename per line; empty result prints nothing (Planning Summary decision — deliberately different from the MCP tool's friendlier "No files attached" text, since the CLI is script-facing).
- `Tests and checks:`
  - New: `TestWriteTaskFileCommand`, `TestReadTaskFileCommand`, `TestListTaskFilesCommand`, `TestListTaskFilesCommand_Empty` (asserts empty stdout, not a "no files" message), plus one error-path test per command (unknown task ID, archived task for the write command).
  - Run: `go build ./...`, `go test ./internal/cli/...`.
- `Commit boundary:` This phase alone; may be done in parallel with P4 by a different contributor since both depend only on P3.
- `Definition of done:` Three subcommands working end-to-end against a real temp `tasks/` dir in tests; `go build ./...` green.
- `Rollback note:` Revert the three `cli.go` blocks, the three `commands.go` functions, and the new tests. No effect on P3/P4.

---

- `Phase ID:` P6 — documentation-sync
- `Goal:` Bring `CLAUDE.md`, `AGENTS.md`, `README.md` in line with the new storage layout and the three new tools/subcommands.
- `Why this phase exists:` CLAUDE.md is authoritative project documentation checked into the repo and read by every future agent session (including this one); leaving it describing the old flat layout after P1-P5 land would actively mislead future work.
- `Files likely to change:` `CLAUDE.md`, `AGENTS.md` (edit identically — confirmed byte-identical today), `README.md` (already independently drifted; fix in the same pass rather than compounding the drift).
- `Regeneration required:` Not applicable.
- `Dependencies:` P1-P5 (needs the final tool names/CLI names/layout to be correct, not provisional).
- `Implementation tasks:`
  1. `CLAUDE.md`/`AGENTS.md`: update the ASCII architecture diagram (`./tasks/*.md` → `./tasks/{id}/{id}.md`, `./tasks/archive/*.md` → `./tasks/archive/{id}/{id}.md`), the Storage bullets, the Task Identification filename description, the Archiving → Storage bullet, the `archive_task` tool-table row's description, and add a new "Attached Files" section to the MCP Tools table for `write_task_file`/`read_task_file`/`list_task_files`. Apply every edit to both files identically (diff them after editing to confirm they're still byte-identical).
  2. `README.md`: update the intro sentence and "Markdown-based storage" bullet, the Project Structure ASCII tree, the MCP tool table (add the three new tools), and the CLI command table (add the three new subcommands **and** fix the pre-existing `archive` omission flagged in research, in the same pass rather than leaving it as unrelated drift).
- `Tests and checks:` No automated test; manual read-through diffing old vs. new wording for accuracy against the code actually written in P1-P5. Run `diff CLAUDE.md AGENTS.md` to confirm they remain identical.
- `Commit boundary:` This phase alone — pure documentation, no code.
- `Definition of done:` `diff CLAUDE.md AGENTS.md` empty; every layout/tool/CLI reference in all three files matches the shipped code.
- `Rollback note:` Revert the three markdown files; zero code risk.

### Cross-Phase Risks

- **Import-cycle risk (resolved in-plan, flagged for implementation vigilance):** any temptation during P2/P3 to have `internal/task` import `internal/storage` directly (e.g. for a helper type) will fail to compile. Everything must go through the `Storage`/`ArchiveStorage`/`FileStorage` interfaces declared in `task/service.go`.
- **Real-data window between P1 and P2:** if these two phases are committed with a gap (e.g. P1 lands but P2 is delayed), and someone runs the actual built CLI/server against the real `tasks/` directory in that window, the 87 real archived tasks become temporarily invisible to `LoadAll`/`LoadAllArchived` (though not deleted or corrupted — the flat files are untouched until migration runs). Mitigation: land P1 and P2 back-to-back before any manual/real-data use; automated tests are unaffected since they use `t.TempDir()`.
- **`NewService` signature churn (P3):** touches ~42 call sites total (2 production + ~40 test). A missed test call site is a compile error, not a silent bug, so `go build ./...`/`go vet ./...` after the batch edit is a hard, sufficient gate — but the sheer count makes a partial/rushed edit likely if done by hand one-by-one; do it as a grouped batch edit (see P3 task 8), not 40 individual edits.
- **Filename validation is new, unprecedented territory** (no existing config/validation precedent for file names in this codebase, per research) — P3's `validateFilename` is the single place this logic must live; do not duplicate the rule set in both `storage/files.go` and `tools/files.go` or they will drift.
- **Archived-task write guard is new** (research confirmed no existing code rejects writes to archived tasks — `Update`/`StartTask`/`CompleteTask` don't either). `WriteTaskFile`'s explicit `IsArchived` check in P3 is the first instance of this guard in the codebase; it does not retroactively fix `Update`/`StartTask`/`CompleteTask`'s pre-existing gap, which stays out of scope for this feature.
- **Directory-move failure modes are new** (P1's `Archive` and P3's `Delete`-via-`RemoveAll` are heavier, less atomic operations than the old single-file rename/remove, with no rollback precedent in this codebase). Accepted per the design's own risk analysis; not re-litigated here.

### Execution Order Rationale

P1 → P2 must run in that order and essentially back-to-back (P2 depends on P1's directory helpers; the real repo's 87 archived tasks are only correctly visible again once both have landed). P3 depends only on P1 (not P2) but should follow P2 in practice so no real-data window is left open longer than necessary. P4 and P5 both depend only on P3 and are mutually independent — they can be implemented in either order or in parallel by two different workstreams without conflict (disjoint files: `internal/tools/*` vs `internal/cli/*`). P6 must come last because it documents the final shape of everything else, including tool/CLI names that only exist after P4/P5.

## Global Sections

### Execution order
P1 → P2 → P3 → {P4, P5 in parallel} → P6.

### Parallelization opportunities
P4 (MCP tools) and P5 (CLI subcommands) touch disjoint files (`internal/tools/` vs `internal/cli/`) and depend only on P3's `Service` methods — they can be built simultaneously by two contributors/sessions once P3 is merged.

### Phase-to-commit mapping
One commit per phase (P1, P2, P3, P4, P5, P6) — six commits total, in the order above. P3's mechanical ~40-call-site test update is folded into the P3 commit, not split out, since it's an inseparable consequence of that phase's interface change.

### Documentation update points
Only P6 touches documentation (`CLAUDE.md`/`AGENTS.md`/`README.md`). No earlier phase should touch these files, to keep P1-P5's diffs pure code+tests and P6's diff pure docs, per this repo's design-doc convention of not mixing generated/derived content with logic changes (the same principle applied here to docs vs. code).

### Final verification plan
1. `go build ./...` and `go vet ./...` clean at the repo root after P6.
2. `go test ./...` green.
3. `gofmt -l .` empty (no unformatted files).
4. Manual: run the built binary's CLI against the real `tasks/` directory once, confirm the 87 real archived tasks migrated correctly (`list --archived`, spot-check `get --archived <id>` for a handful of IDs spanning the numbering gap at 071/072), then confirm `tasks/` and `tasks/archive/` now show `{id}/{id}.md` directories with no leftover flat `.md` files at the top level.
5. Manual: exercise `write_task_file`/`read_task_file`/`list_task_files` (via CLI, since it needs no MCP client) against one real active task end-to-end: create a scratch task, write two files, list them, read one back, archive the task, confirm the files moved with it and a further write attempt is now rejected.
6. `diff CLAUDE.md AGENTS.md` empty.
