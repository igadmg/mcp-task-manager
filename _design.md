# Resolution and verification: what a closed task means

This document covers two fields on a task — `resolution` and `verified_at` —
and the rules the server enforces around them. For the rest of the server's
design (storage layout, index, relations, archive mechanics) see
[CLAUDE.md](CLAUDE.md).

## The problem

`status` has three values and only one of them is terminal, so `done` has to
carry every way a task can leave the backlog: the work landed, the work is no
longer needed, another task took it over, we decided not to do it. Those are
not the same fact, and squashing them together costs twice.

It costs when closing: a backlog review that finds five tasks whose premise
evaporated has no way to say so. The task gets marked `done` and the truth
goes into the description as prose — `УСТАРЕЛ — решено другим способом` — where
nothing can filter on it.

It costs when reading: `list_tasks(status: done)` returns a wall of rows that
look identical, and the only way to tell delivered work from abandoned work is
to open each one and read it.

## The shape of the fix

**Resolution is an outcome; the archive is a location.** They are orthogonal,
and the mistake worth avoiding is putting the outcome on the archive operation
("reason for archiving"). Completed tasks get archived too, so such a field
would mostly hold noise; and a task that stopped applying is often one you want
to keep visible for a while before it moves. The outcome belongs to the task,
it is set at closing time, and it travels into the archive with it.

**A third storage group would be worse than either.** Path resolution across
this server is binary — the active directory, else `archive/`
([files.go](internal/storage/files.go), [index.go](internal/storage/index.go),
[markdown.go](internal/storage/markdown.go)) — and a third location would make
every tool, the index, the id allocator and auto-archive learn about it, to
express something that is not a location at all.

**The replacement is a link, not a string.** `superseded` and `duplicate` name
a *kind* of closure; which task took over is an edge, recorded with
`add_relation` (`superseded_by`, `duplicate_of`). Copying an id into the
resolution field would give two places to disagree.

## `resolution`

Set when a task becomes `done`, cleared if it is reopened.

| Value | Meaning |
|---|---|
| `completed` | The work landed. The default when a task is closed without naming one. |
| `obsolete` | The task no longer applies — the code, the plan or the surrounding decisions moved out from under it. |
| `superseded` | The need is real, another task covers it now. Pair with a `superseded_by` relation. |
| `duplicate` | Already tracked elsewhere. Pair with a `duplicate_of` relation. |
| `wontfix` | Understood, applies, and deliberately not being done. |

`Resolution.Delivered()` splits these in two: `completed` (and the empty value)
on one side, everything else on the other. That predicate — not the individual
values — is what the rules below key off, so adding a sixth resolution later
does not mean revisiting them.

Companion fields:

- `resolution_note` — one line of why: the commit that landed it, what
  replaced it, why it stopped applying. Only valid on a closed task.
- `closed_at` — when the task became `done`. `updated_at` cannot stand in for
  it: editing a closed task's text moves `updated_at` and would look like a
  second closure.

### Rules

- **A resolution implies closing.** `update_task(id, resolution: obsolete)`
  moves the task to `done` in one call. Passing a resolution together with a
  move to any other status is an error, not a silent reinterpretation.
- **Closing as not-delivered may start from `todo`.** Tasks that stopped
  applying are usually ones nobody ever started, so requiring
  `start_task` first would be pure ceremony. Plain completion is unchanged:
  it still refuses anything that is not `in_progress`.
- **Not-delivered closes cascade to open subtasks**, which are closed with the
  parent's resolution and a note naming it. A branch that no longer applies
  does not stop applying subtask by subtask. Completion is unchanged here too:
  it still refuses a parent with open subtasks.
- **Reopening clears the closure** — resolution, note and `closed_at` all go,
  so nothing describes a closure that no longer holds.
- **A missing resolution on a `done` task reads as `completed`**
  (`Task.EffectiveResolution()`). Every task closed before this field existed
  stays meaningful, and filtering for `completed` finds them.

### Effect on the archive

Archiving is unchanged: any `done` task can be archived, whatever its
resolution, and the resolution stays readable afterwards.

Auto-archive changes in one place. A completed task serves out
`auto_archive.after_days` because there is a window where someone may still
want it in the active list. The window is still counted from `updated_at`,
not `closed_at`, so editing a completed task restarts it. A task closed without its work being done has
nothing to review, so it is eligible on the next pass regardless of age
(`GetAutoArchiveCandidates`, gated as before on `auto_archive.enabled`).

## `verified_at`

A second, unrelated kind of staleness surfaced in the same backlog review: a
task that is still live and still needed, but whose own text has rotted —
descriptions naming symbols that were renamed a year ago, file paths that moved
in a restructure. Nothing about `status` or `resolution` can express that,
because nothing about the task's *lifecycle* is wrong.

`verified_at` is the timestamp of the last time someone checked a task's text
against reality. Set it with `update_task(id, verified: true)`; `verified:
false` clears it. It is deliberately inert: it triggers nothing, blocks
nothing, and says nothing about progress. It exists so a backlog review can
tell "this was confirmed accurate last week" from "nobody has looked at this
since it was filed", and so the next review can start with the latter.

`nil` means never verified since it was filed — which is the honest default, as
being filed is not a check.

## Schema

```yaml
---
id: map-shading-system-research
title: "Research: how map shading works"
status: done
priority: medium
type: feature
created_at: 2026-09-11T00:00:00Z
updated_at: 2026-09-20T17:23:13Z
resolution: obsolete
resolution_note: describes the pre-paged-texture renderer, replaced in f7fe038
closed_at: 2026-09-20T17:23:13Z
verified_at: 2026-09-20T17:20:00Z
---
```

All four keys are `omitempty`: an open task's frontmatter is byte-identical to
what this server wrote before the fields existed, and old files parse
unchanged. No migration runs.

## Tool surface

| Tool | Addition |
|---|---|
| `complete_task` | `resolution`, `resolution_note` |
| `update_task` | `resolution`, `resolution_note`, `verified` |
| `list_tasks` | `resolution` filter (matches the effective resolution) |
| `get_task` | reports `resolution` (effective), `resolution_note`, `closed_at`, `verified_at` |

No new tool was added. `complete_task` already meant "this task is finished
with"; giving it a resolution widens that to "and here is how", which is
cheaper than a second closing verb whose description has to explain when to
prefer it — and tool descriptions are resent on every `tools/list`.

In Go, the fields arrive through variadic `UpdateOption` values
(`WithResolution`, `WithResolutionNote`, `WithVerified`) so that
`Service.Update`'s existing call sites keep compiling.

`resolution` and the two timestamps are carried on `IndexEntry` because
`list_tasks` and auto-archive read them off the in-memory index rather than
loading every file. `resolution_note` stays out, like `Description`: it is body
text, fetched with `get_task`.
