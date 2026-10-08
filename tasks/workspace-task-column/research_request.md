# Research request: workspace-task-column

**Additional research needed: 35 / 100**

The data side is fully mapped — this column shows what `_detail.html` already
shows, from a service composite that already exists. What is unresearched is
the fit: the same content in a narrow column, and the behavioural question the
unbounded chain introduced.

## Already covered by research.md
- `Service.Detail` returns task, archived flag, subtasks, blocked + blockers,
  relations, filtered file names and phase records in one lock acquisition
  (`internal/task/view.go:145-182`).
- `newDetailView` maps all of it to plain strings, including the subtask list,
  blockers, relations with titles, git fields and phase runs
  (`internal/web/view.go:548-633`).
- Relation titles cost a second full `BoardSnapshot` call, and only when the
  task has edges (`internal/web/handlers.go:78-98`) — with one column per
  chain entry this multiplies.
- Current markup for every item this column makes clickable: inert file chips
  (`internal/web/templates/_detail.html:117-124`), plain-href subtasks
  (`:49-62`), blockers (`:34-47`), relations (`:64-77`), the `max-h-96`
  description `<pre>` (`:28-32`).
- `*.phase` is filtered out in the service, not the template
  (`internal/task/view.go:173-178`).
- File order is `os.ReadDir`, i.e. lexical (`internal/storage/files.go:111-131`).
- Archived tasks deliberately come back with no subtasks, blockers or
  relations, and that is the truth rather than a skipped lookup
  (`internal/task/view.go:162-171`) — so an archived column is genuinely
  thinner, by design.
- The htmx click idiom and the escaping guarantees, including the branch
  payload inside `data-copy` (`internal/web/handlers_test.go:277-306`).

## Gaps
1. **Re-entering a task already in the chain.** The task description leaves
   this open on purpose: append again (chain as history) or collapse to that
   prefix. Settle it with the rail in mind — a chain that can contain
   `42 → 43 → 42 → 43` makes the rail ambiguous, while collapsing loses the
   working area the user came from. Check what the rail would look like in
   both cases before choosing, and whether a cycle can grow without bound.
2. **Content budget for a narrow column.** `_detail.html` is built for a
   22rem aside on a `max-w-3xl` page and already has a `phase-runs` four-column
   grid (`internal/web/assets/input.css:76-79`) and a two-column `dl`
   (`_detail.html:126-132`). Decide what a narrow column keeps, what collapses
   and what moves to a tooltip — measured, not guessed, including a task with
   many phase runs (this repo has real examples) and a long text id, which
   already needed `truncate` plus `title` on the board
   (`internal/web/handlers_test.go:372-395`).
3. **Cost of N columns per render.** Each column is one `Detail` call, and any
   column whose task has relations triggers a whole `BoardSnapshot`. For a
   depth-10 chain that is up to 10 locks plus 10 snapshots, every poll. Measure
   it on this repo's backlog and decide whether the tiler needs one composite
   read for a chain (a new method in `internal/task/view.go`, which is where
   the project says such composites belong) instead of a call per column.
4. **"Open workspace" placement.** The control lives on the board panel, which
   is `_detail.html` — shared with the page that `workspace-tiler` is
   repurposing. Confirm the ordering: whether this subtask edits a template the
   tiler has already rewritten, or whether both touch it and need sequencing.
5. **Where the description column comes from.** The parent's AC wants the
   description openable like a file. Decide whether the chain encodes it as a
   distinct kind or as a reserved file reference, and check it cannot collide
   with a real attached file name (a task could legitimately have a file called
   `description.md`).
6. **Selected-item marking across a poll.** The marking is server-rendered
   from the chain, so it should survive a swap for free — verify that holds
   once `workspace-live` replaces column markup, rather than assuming it.
