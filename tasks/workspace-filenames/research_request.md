# Research request: workspace-filenames

**Additional research needed: 15 / 100**

The parent's `research.md` already answers almost everything this subtask
touches, down to line numbers. What is left is a caller sweep, which is
mechanical.

## Already covered by research.md
- The exact rules and where they live: `validatePathSegment` and
  `validateFilename` in `internal/storage/files.go:16-46`, with
  `checkReserved` splitting write from read.
- The asymmetry that must be preserved: a write refuses `{id}.md` and
  `*.phase`, a read allows them (`internal/storage/files.go:35-45`, `:90`).
- `task.IsReservedFileName` normalizes case and trailing dots/spaces first
  (`internal/task/phase.go:77-82`).
- The `.` hole and why it ends as a 500: `.` passes the segment check,
  `os.ReadFile` on a directory fails with a non-`IsNotExist` error, and
  `ReadFile` returns it raw (`internal/storage/files.go:16-26`, `:99-105`).
- `ReadTaskFile` dereferences `s.fileStorage` unguarded
  (`internal/task/service.go:359`), unlike `internal/task/view.go:136-141`.
- The import direction that makes the move possible: `internal/storage`
  already imports `internal/task` (`internal/storage/files.go:9`), so the
  rules can live in `internal/task` with no cycle.
- Error values are plain `fmt.Errorf` strings; the only sentinels are
  `ErrNoProjectFound` and `ErrInvalidTokens`.

## Gaps (small)
1. **Caller sweep.** Enumerate every caller of `validateFilename` /
   `validatePathSegment` / `ValidateID`, in `internal/storage`,
   `internal/task`, `internal/tools` and `internal/cli`, and list the tests
   that assert their current error *text*. Moving the definition must not
   change any existing message that a test or a tool response pins.
2. **`ValidateID` shares `validatePathSegment`** (`internal/storage/files.go:61-69`)
   and adds the four reserved ids. Decide whether the id path moves too or
   keeps calling the shared helper from where it is — and whether tightening
   the segment check (the dot/space cases) would start rejecting a **task id**
   that is legal today. Check the ids actually present in `tasks/` and
   `tasks/archive/` before tightening anything shared.
3. **What "whitespace-only" already means.** `validatePathSegment` trims for
   the empty check but validates the untrimmed name
   (`internal/storage/files.go:17-19`), so `" x "` is accepted and written
   verbatim. Confirm whether any task on disk has such a file before deciding
   whether to reject or normalize.
4. **Windows-shaped names.** `IsReservedFileName` already guards against
   `research.PHASE` and `research.phase.`; check whether the same
   normalization belongs in the general validator (trailing dot or space makes
   a different file on POSIX but the same one on Windows), and whether the
   project claims Windows support for the server (`scripts/build-css.sh:12-13`
   mentions Windows only for the CSS step).
