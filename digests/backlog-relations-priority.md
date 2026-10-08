# Backlog digest — relations and execution order

Snapshot: 2026-10-08. Companion to [backlog-complexity.md](backlog-complexity.md).
11 `todo` tasks, 1 `in_progress`, 14 `done`.

This digest answers two questions: how the todo tasks hang together, and what
to do next.

## 1. The dependency picture

### Recorded edges

```
web-task-workspace (todo, high, on wip branch igor.cwer/wip/web-task-workspace)
├── workspace-tiler        done (critical)  ── the load-bearing subtask, shipped
├── workspace-markdown     done (high)
├── workspace-filenames    done (high)
├── workspace-task-column  todo (high)   blocked_by: workspace-tiler            → SATISFIED
├── workspace-file-column  todo (high)   blocked_by: tiler, markdown, filenames → SATISFIED
└── workspace-live         todo (medium) blocked_by: task-column, file-column    → OPEN

web-backlog-graph    todo (high)   blocked_by: web-task-workspace → OPEN

web-token-workspaces todo (medium) relates_to: split-mcp-web-processes
task-extra-fields    todo (medium) relates_to: web-phase-grid-spans (in_progress)
done-stats-bars-legend   todo (low) relates_to: done-stats-bars (done)
done-stats-config-errors todo (low) relates_to: done-column-stats (done)
stats-bars-new-todo      todo (medium) — no recorded relations
```

**Both unblocked-but-not-started tasks are the same two:**
`workspace-task-column` and `workspace-file-column`. Their blockers all went
`done` with the tiler on 2026-10-08, and nothing in the backlog records that
they are now the front of the queue — `get_next_task` will find them, but a
human reading the board will not see why they matter.

### Unrecorded couplings — the real structure

The frontmatter misses four links that will decide the order:

1. **`split-mcp-web-processes` ↔ `web-token-workspaces`** — recorded as a
   mere `relates_to`, but they overlap hard. Both need a *structurally
   read-only resolve mode* that skips `Service.Initialize()` (no migration, no
   auto-archive); `web-token-workspaces` says so itself ("coordinate"). Both
   rewrite how `internal/web` gets its project: one makes it process-owned,
   the other makes it a per-token registry lookup. Doing them in either order
   makes the second one cheap; doing them in parallel means one of the two
   rewrites is thrown away.
   *Recommendation: make this `blocked_by`.* `split-mcp-web-processes` first —
   it is the one with settled decisions, and a one-project read-only process
   is the honest base for an N-project one.

2. **`task-extra-fields` ← `web-phase-grid-spans` (in progress)** — the
   trigger is concrete: `web-phase-grid-spans` had to carry `Complexity: high`
   as a line of markdown because the frontmatter has no place for it. The
   relation direction in the records (`relates_to`) understates it: the
   in-progress task is the *motivation*, not a sibling.

3. **The three Done-stats tasks are one work item in three records.**
   `stats-bars-new-todo`, `done-stats-bars-legend`,
   `done-stats-config-errors` all land in `internal/web/templates/_stats_bars.html`
   and `internal/config/stats.go`. The first two both edit the bar's `<rect>`
   set and its `aria-label`; the first and third both add/validate per-card
   config keys. Nothing records that. Sequenced apart they produce three CSS
   rebuilds and three merge conflicts in the same template.
   *Recommendation: `blocked_by` chain `new-todo → legend`, or fold all three
   into one parent.*

4. **`web-backlog-graph` is effectively blocked by three tasks, not one.**
   It is `blocked_by web-task-workspace`, and that parent cannot be delivered
   while `workspace-task-column`, `workspace-file-column` and
   `workspace-live` are open (and, under git branching, while a delivered
   subtask's `squash_commit` is not an ancestor of the parent's wip — the
   parent gate). The graph also needs a `g` column kind, which is exactly the
   extension point `workspace-tiler` shipped — so the mechanism is there, but
   the viewer area it draws into is `workspace-file-column`'s wide pane.

### The critical path

```
workspace-task-column ─┐
                       ├─→ workspace-live ─→ web-task-workspace (deliver) ─→ web-backlog-graph
workspace-file-column ─┘
```

Four of the five `high`-priority todo tasks are on this one chain. Everything
else in the backlog is independent of it.

## 2. Ranked by importance

Importance = recorded priority, weighted by how many other tasks it unblocks
and whether work is already in flight on it (an open wip branch is a cost that
grows while it waits).

### Tier 1 — do now, in this order

**1. `workspace-file-column`** · `high` · unblocked · M
> *"This is the point of the whole parent task — a task's `research` / `design`
> / `plan` cannot be read in the UI at all today."*

Highest user-visible value per unit of work in the backlog, and the one
deliverable that justifies everything the tiler built. All three blockers are
`done`. It also builds the wide working area that `web-backlog-graph` will
draw into.

**2. `workspace-task-column`** · `high` · unblocked · M
Makes the chain actually navigable — today blockers, relation targets and the
parent still emit plain `/tasks/{id}` links that re-root the workspace instead
of extending it, which is a visible inconsistency in shipped code. Partly
landed already, so the remainder is small. Can run in parallel with #1: one
owns `_col_task.html`, the other `_col_file.html`.

**3. `workspace-live`** · `medium` · blocked by #1 and #2 · M
Recorded `medium`, but it is the **closing** subtask: polling, scroll
preservation, the CSS rebuild and the whole CLAUDE.md/README workspace
documentation. Until it lands, `web-task-workspace` cannot be delivered and
the feature is undocumented. Treat it as `high` in practice.

**4. `web-task-workspace` (deliver)** · `high` · parent
Not new work — the squash of a live wip branch that has been open since
2026-10-07. Delivering it releases `web-backlog-graph` and closes the longest
open branch in the repo.

### Tier 2 — the next real feature

**5. `web-backlog-graph`** · `high` · blocked by #4 · L
The only task in the backlog that answers a question the dashboard cannot
answer at all: how tasks connect. Needs a design decision (server-rendered SVG
vs. a vendored layout library) before implementation; that design can be
written while Tier 1 runs, since the `g` kind contract is already fixed by the
tiler's registry.

### Tier 3 — infrastructure, sequence matters more than speed

**6. `split-mcp-web-processes`** · `medium` · XL
Real pain it removes: the dashboard dies with the agent's MCP server. Should
run before #7 and should ship the read-only resolve mode as a reusable commit.

**7. `web-token-workspaces`** · `medium` · XL
Depends in practice on #6. Do not start both.

**8. `task-extra-fields`** · `medium` · L
Independent of everything above and fixes an active data-loss bug: a
hand-added `complexity: high` survives until the first `update_task`. The
lossless round-trip alone is worth shipping even if the tools/web/CLI fan-out
is staged — and it is the prerequisite for ever ordering this backlog by a
real `complexity` field instead of by a digest like this one.

*Note the irony worth recording: this very digest exists because the schema
has nowhere to put a complexity value.*

### Tier 4 — batch them into one change

**9–11. `stats-bars-new-todo` (medium) + `done-stats-bars-legend` (low) + `done-stats-config-errors` (low)**
One sitting, one CSS rebuild, one CLAUDE.md edit. Order inside the batch:
`config-errors` (decide the policy) → `stats-bars-new-todo` (adds the fifth
segment and the per-card window key) → `done-stats-bars-legend` (explains the
now-five colours). Doing the legend before the new segment means writing the
tooltip twice.

## 3. Recommended record changes

If the backlog should encode what this digest found:

| Change | Why |
|---|---|
| `add_relation web-token-workspaces blocked_by split-mcp-web-processes` | shared read-only resolve mode; parallel work is wasted work |
| `add_relation done-stats-bars-legend blocked_by stats-bars-new-todo` | the legend must explain five colours, not four |
| `update_task workspace-live priority=high` | it is the gate on the parent's delivery and holds all the docs |
| fold the three Done-stats tasks under one parent | they are one change to two files |

Nothing here argues for re-prioritising Tier 1: the recorded priorities
already point at the critical path. What the records miss is only that two of
those tasks became actionable today.
