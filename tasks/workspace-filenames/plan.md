# Plan: workspace-filenames

One commit; the three steps are too small and too coupled (step 2 does not
compile without step 1) to split. Order matters only between 1 and 2.

## Step 1 — `internal/task/name.go` (new)

- `ValidateNameSegment(label, name string) error`: the three rules from
  design §1, messages copied verbatim from the current
  `storage.validatePathSegment`.
- `ValidateAttachedName(name string) error` → `ValidateNameSegment("filename", name)`.
- No new imports beyond `fmt`, `strings`.

Verify: `go build ./...`.

## Step 2 — `internal/storage` calls it

- `files.go`: delete `validatePathSegment`; `validateFilename` starts with
  `task.ValidateAttachedName(filename)`; `ValidateID` starts with
  `task.ValidateNameSegment("task id", id)`. Reserved-id table and the
  `checkReserved` asymmetry untouched.
- `phase.go:61`, `:74`: `task.ValidateNameSegment("task id", taskID)`.
- `current.go:26`, `:43`, `:52`: `task.ValidateNameSegment("user", user)`
  (add the `task` import if the file lacks it).

Verify: `go build ./... && go test ./internal/storage/...` — expected green
without touching any existing test (research §1: only `phase_test.go:164`
pins text, and that message is unchanged).

## Step 3 — `Service.files()` nil guard

- `internal/task/service.go`: add the unexported `files()` helper; use it in
  `WriteTaskFile`, `ReadTaskFile`, `ListTaskFiles`. Lock discipline is
  unchanged — the helper only reads a write-once field, like `view.go`'s
  `listFiles`.

Verify: `go test ./internal/task/...`, including
`TestServiceNoSelfDeadlock` and `TestServiceRace`.

## Step 4 — tests

- `internal/task/name_test.go`: the valid/invalid table from design §"Testing
  strategy", plus the nil-file-store cases for the three service methods
  (a service built with no `FileStorage`; follow how `service_test.go`
  constructs a bare service).
- `internal/storage/files_agreement_test.go` (or appended to
  `storage_test.go`, matching the file layout already used): for each row of a
  shared table, assert `task.ValidateAttachedName` and the storage
  write/read paths agree — storage rejects exactly the names the validator
  rejects. A valid name that is simply absent returns "file not found", which
  counts as accepted.
- Explicit asymmetry test if not already present: `WriteFile` refuses
  `{id}.md` and `research.phase`, `ReadFile` accepts both
  (`storage_test.go:2560` and `phase_test.go:164` cover most of it; add the
  read side).

## Final verification

```
gofmt -l internal
go vet ./...
go test -race ./...
```

Also confirm by grep that `validatePathSegment` no longer exists and that
`internal/task` is the only definition of the rules.

## Rollback

Single commit, additive plus six call-site edits; `git revert` restores the
previous behaviour. No data, config or on-disk format changes, so nothing to
migrate back.

## Risks

- **A name that is valid today starts failing.** Mitigated by keeping rule 3
  to dots-and-spaces-only names and by the research sweep of every id and
  attached name on disk (none affected).
- **A sibling subtask touching `internal/storage/files.go`.** This task is
  marked independent; conflicts would be textual only.

## Docs

`CLAUDE.md` mentions the validation rules in two places ("Validation" and
"Attached Files"). Add that names made only of dots and spaces are rejected
and that the rules now live in `internal/task`, plus `internal/task/name.go`
in the project structure tree.
