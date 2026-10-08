# Design: workspace-filenames

Three changes, all small, all inside `internal/task` and `internal/storage`.
No behaviour visible to a tool or the CLI changes except that a handful of
names that are only dots and spaces are now refused.

## 1. The rules move down into `internal/task`

New file `internal/task/name.go`, next to `phase.go` (which already owns the
reserved-name rule).

```go
// ValidateNameSegment holds the rules every caller-supplied path segment of
// a task's on-disk layout obeys: non-empty, no path separator, and not a
// name made only of dots and spaces (".", "..", ". " resolve to a directory
// or, on Windows, to nothing). label words the message ("filename",
// "task id", "user").
func ValidateNameSegment(label, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%s %q must not contain a path separator", label, name)
	}
	if strings.Trim(strings.TrimSpace(name), ".") == "" {
		return fmt.Errorf("%s %q is not allowed", label, name)
	}
	return nil
}

// ValidateAttachedName reports whether name is usable as the name of a file
// attached to a task. It is the rule set the storage read and write paths
// apply, exported so a consumer can tell a malformed name from a missing
// file before it tries to read one.
func ValidateAttachedName(name string) error {
	return ValidateNameSegment("filename", name)
}
```

Why this shape:

- **`internal/storage` already imports `internal/task`**
  (`internal/storage/files.go:9`), so moving the rules down adds no import
  edge and no cycle; `internal/storage` stays private to `internal/task`.
- **One definition, three labels.** The label parameter is what lets the id
  and user paths keep their exact wording while sharing the one
  implementation. `ValidateAttachedName` is the named entry point the web
  layer will call — a consumer never has to pass a label.
- **Three rules, not more.** Rule 3 is deliberately narrower than
  `IsReservedFileName`'s Windows normalization: it rejects a name that is
  *entirely* dots and spaces, so `" x "` and `"notes."` — legal today —
  stay legal. See research §3, §4.

Rule 3 covers every case the acceptance criteria list: `.`, `..`, `...`,
`". "`, `" .. "`, dots-and-spaces in any mix. The empty and whitespace-only
cases are rule 1, as today.

### What stays in `internal/storage`

- The **reserved-id table** (`reservedTaskIDs`, `files.go:49-54`): it is about
  this package's own on-disk sentinels (`archive/`, `.users`, `.index.json`),
  not about name shape.
- The **reserved-filename asymmetry** in `validateFilename`: `{id}.md` needs
  the task id, and the `*.phase` rule is already `task.IsReservedFileName`.
  `checkReserved` keeps splitting write from read exactly as now.

`validatePathSegment` is deleted; its six call sites call
`task.ValidateNameSegment` directly (`files.go` ×2 via the two wrappers,
`phase.go:61`, `:74`, `current.go:26`, `:43`, `:52`). A thin local alias was
the alternative; calling the exported function at each site makes the single
source of truth visible where it is used, and the diff is the same size.

All six error strings are unchanged, so `internal/storage/phase_test.go:164`
— the only test that pins text — and every `err != nil` assertion keep
passing (research §1).

## 2. The `.` hole is closed before `os.ReadFile`

With rule 3 in place, `ReadFile` returns `filename "." is not allowed`
instead of letting `os.ReadFile` hit a directory and return a raw
`EISDIR`-shaped error (`internal/storage/files.go:99-105`). The handler the
file-column subtask writes can therefore pre-validate with
`task.ValidateAttachedName` (→ 400) and treat every later read failure as
404, which is the decision this task is implementing.

## 3. Nil file store

`Service.ReadTaskFile` (`service.go:359`), `ListTaskFiles` (`:372`) and
`WriteTaskFile` (`:334`) all dereference `s.fileStorage` unguarded. One
unexported helper, used by all three:

```go
// files returns the attached-file store, or an error when the service was
// built without one. The read-only view path tolerates a missing store
// (view.go: no store lists no files); the tool paths report it instead.
func (s *Service) files() (FileStorage, error) {
	if s.fileStorage == nil {
		return nil, fmt.Errorf("attached files are not available: no file storage configured")
	}
	return s.fileStorage, nil
}
```

Guarding all three rather than only `ReadTaskFile` is the same one-line fix
for the same bug class; leaving two of three panicking would be arbitrary.
The existing tolerant behaviour of `view.go:136-141` is untouched — a board
render still shows a task with no files rather than failing.

## Alternatives rejected

- **Error sentinels + `errors.Is`.** Already declined in the task
  description, in favour of pre-validation. Nothing here needs it.
- **A copy of the rules in `internal/task`.** Explicitly ruled out: two
  definitions drift.
- **Inheriting `IsReservedFileName`'s full Windows normalization** in the
  general validator. It would reject `" x "` and `"notes."`, breaking
  "valid today stays valid", and the server claims no Windows support
  (research §4).
- **Normalizing (trimming) names instead of rejecting.** Silent rewriting of
  a caller's filename would make `write` then `read` of the same string
  disagree.

## Testing strategy

- `internal/task/name_test.go`: table over valid and invalid names —
  `""`, `"   "`, `"\t"`, `"."`, `".."`, `"..."`, `". "`, `" .. "`,
  `"a/b"`, `` `a\b` ``, versus `"notes.md"`, `".hidden"`, `"..foo"`,
  `" x "`, `"notes."`, `"research.phase"` (shape is fine; reserved is a
  separate rule).
- An agreement test: for every row, `task.ValidateAttachedName` and
  `storage.MarkdownStorage.ReadFile`/`WriteFile` reach the same verdict on
  the name — the storage path either returns a validation error or gets past
  validation (a "file not found" from a valid name counts as accepted).
- The asymmetry: `WriteFile` refuses `{id}.md` and `research.phase`,
  `ReadFile` accepts both.
- `ReadTaskFile` / `ListTaskFiles` / `WriteTaskFile` on a service with no
  file store return an error and do not panic.
- Existing suites run untouched: `./internal/...` with `-race`.
