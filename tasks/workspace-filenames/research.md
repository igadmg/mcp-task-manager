# Research: workspace-filenames

Closes the four gaps in `research_request.md`. Everything else this task
touches is already answered by the parent's `research.md`; nothing here
contradicts it.

## 1. Caller sweep

`validatePathSegment` (`internal/storage/files.go:16`) has six call sites, all
inside `internal/storage`, with three different labels:

| Call site | Label | Subject |
|---|---|---|
| `files.go:36` (`validateFilename`) | `filename` | attached file name |
| `files.go:62` (`ValidateID`) | `task id` | task id |
| `phase.go:61`, `phase.go:74` | `task id` | task id for a phase record |
| `current.go:26`, `:43`, `:52` | `user` | pointer user directory |

`validateFilename` has exactly two call sites: `WriteFile`
(`files.go:79`, `checkReserved=true`) and `ReadFile` (`files.go:90`,
`checkReserved=false`) — the asymmetry the acceptance criteria pin.

`ValidateID` is exported and reached from outside the package through the
`task.Storage` interface (`internal/task/service.go:27-32`), called once, in
`createTask` for a caller-supplied custom id (`service.go:243`).

Nothing in `internal/tools`, `internal/cli` or `internal/web` calls any of
them directly; they all go through `Service.WriteTaskFile` /
`ReadTaskFile` / `ListTaskFiles` (`internal/task/service.go:327`, `:352`,
`:365`).

### Tests that pin the error *text*

Only one, and it is a reserved-name message, not a segment message:

- `internal/storage/phase_test.go:164` — `strings.Contains(err.Error(),
  "is reserved: phase files are written only by start_phase/finish_phase")`.

Everything else asserts only `err != nil` / `err == nil`:
`internal/storage/storage_test.go:2391` (`ValidateID` table),
`:2560` (invalid write names `"", "   ", "../escape.md", "sub/dir.md",
"sub\\dir.md", "..", "1.md"`), `internal/storage/current_test.go:160-166`
and `:211` (`"a/b"`, `` `a\b` ``, `".."`, `""`, `"  "`).

`internal/task/service_test.go:96-106` is a *mock* `ValidateID` that
re-implements the three messages verbatim. It is a test double, so it does
not constrain the real implementation, but it is the one place a copy of the
wording lives.

Conclusion: all six existing messages can be kept word for word, and the
move is mechanical.

## 2. `ValidateID` and the shared helper

`ValidateID` = `validatePathSegment("task id", id)` + the four reserved ids
(`files.go:49-69`). The reserved-id table is about *this package's on-disk
layout* (`archive/`, `.users`, the retired `.index.json`), so it stays in
`internal/storage`. Only the shared segment rules move.

Tightening the shared check is safe for ids: every id on disk is a slug or a
number — `done-column-stats`, `git-branch-per-task`, `web-task-workspace`,
`001`…`010`, etc. (`tasks/`, `tasks/archive/`). None is only dots and/or
spaces, none has a leading or trailing dot or space. The same holds for the
`user` label: a sanitized git-email local part cannot be dots-only.

So the proposed rule — reject a name that consists only of dots and spaces —
rejects nothing that exists today and nothing a user could reasonably want,
for any of the three labels.

## 3. What "whitespace-only" means today

`validatePathSegment` trims only for the *empty* check and then validates the
untrimmed name (`files.go:17-19`), so `" x "` is accepted and written
verbatim; `"   "` is rejected as empty (and `storage_test.go:2560` pins that
`"   "` is invalid).

No file on disk has a name with a leading/trailing space or one made of dots
and spaces (checked across `tasks/` and `tasks/archive/`), so nothing has to
be migrated — but nothing requires *normalizing* `" x "` either, and the
acceptance criteria say every name valid today stays valid. Decision for the
design: keep accepting `" x "`, reject only names that are *entirely* dots
and/or whitespace.

## 4. Windows-shaped names

`task.IsReservedFileName` (`internal/task/phase.go:76-82`) already normalizes
the Windows way — `strings.TrimRight(name, ". ")` plus `ToLower` — so
`research.PHASE` and `research.phase.` are both refused for a write. That
normalization is specifically about *not letting a phase record be
overwritten*, and it belongs where it is.

The general validator should **not** inherit it wholesale: rejecting every
trailing dot or space would invalidate `" x "`, which is legal today. What it
should inherit is the narrower consequence: a name that is nothing but dots
and spaces resolves to a directory (`.`, `..`) or to an empty name on
Windows, and must be refused.

The project claims no Windows support for the server itself; the only Windows
mention is the CSS build step (`scripts/build-css.sh:12-13`). So Windows
shapes are a hardening nicety, not a requirement — which is another reason to
keep the new rule minimal.

## Extra finding (not in the request)

`Service.ListTaskFiles` (`internal/task/service.go:372`) dereferences
`s.fileStorage` unguarded too, exactly like `ReadTaskFile` (`:359`). Same bug
class, same one-line fix; `WriteTaskFile` (`:334`) as well. The read-only
view path is already guarded (`internal/task/view.go:136-141`).
