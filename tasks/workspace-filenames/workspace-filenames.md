---
id: workspace-filenames
parent_id: web-task-workspace
title: Export the attached-filename rules and close the "." hole
status: done
priority: high
type: feature
created_at: "2026-10-07T19:58:01Z"
created_by: igor.cwer
updated_at: "2026-10-07T20:53:03Z"
resolution: completed
closed_at: "2026-10-07T20:53:03Z"
branch: igor.cwer/wip/web-task-workspace--workspace-filenames
base_branch: igor.cwer/wip/web-task-workspace
start_commit: 8b16e42cd939db0eb155b3afed9f6164ceb0b5af
squash_commit: 210eeb2fc11267c25ef21bb50eeda950a7214278
---

The workspace takes a file name from the URL, so the web layer needs to tell "this name is malformed" (400) from "there is no such file" (404) *before* reading. Today it cannot: the rules live in the unexported `validatePathSegment` / `validateFilename` in `internal/storage/files.go:16-46`, and every failure comes back as a bare `fmt.Errorf` string — "task not found", "file not found" and "bad name" are indistinguishable without matching text (`internal/storage/files.go:102`, `internal/storage/files.go:143`).

Decided 2026-10-07: export the rules from the task layer and have the web layer call them before the read; any read failure after that is a 404. No error sentinels.

Independent of every other subtask.

## Scope

- Export a validator from `internal/task` (e.g. `task.ValidateAttachedName(name string) error`) that is the **same code** the storage write/read paths use — not a copy. `internal/storage` already imports `internal/task` (`internal/storage/files.go:9`), so the rules can move down into `internal/task` and be called from storage, keeping one source of truth and the package boundary intact (`internal/storage` stays private to `internal/task`).
- Close the `.` hole: `.` (and `..` variants that normalize to a directory, trailing dots/spaces, names that are only whitespace) currently pass `validatePathSegment` (`internal/storage/files.go:16-26`); `os.ReadFile` on the resulting directory then fails with a non-`IsNotExist` error that `ReadFile` returns raw (`internal/storage/files.go:99-105`) — a handler mapping only "not found" would turn it into a 500.
- Guard `Service.ReadTaskFile` against a nil `fileStorage` (`internal/task/service.go:359` dereferences it unchecked, unlike `internal/task/view.go:136-141`).

## Acceptance criteria

- One exported function in `internal/task` is the only definition of the rules; `internal/storage`'s write and read paths call it. Behaviour for every name that is valid today is unchanged — the existing storage and tool tests pass untouched.
- `.`, `..`, `. `, `..`, names that are only dots and/or spaces, the empty string and whitespace-only names are all rejected by the validator, so no such name can reach `os.ReadFile`.
- `ReadTaskFile` on a service with no file store returns an error instead of panicking; a test covers it.
- The reserved-name rules keep their current asymmetry and it is tested: a **write** refuses `{id}.md` and `*.phase`, a **read** allows them (`internal/storage/files.go:35-45`, `:90`). The decision about whether the *viewer* offers them is the file-column subtask's, not this one's.
- Tests: table-driven over valid and invalid names, including the traversal attempts already covered plus the new dot/space cases; a test asserting storage and the exported validator agree (same input, same verdict).

## Out of scope

- HTTP status mapping — the file-column subtask calls this validator and turns its error into a 400.
- Changing what `ListFiles` returns or its ordering.
- Error sentinels / `errors.Is` plumbing (considered and declined in favour of pre-validation).