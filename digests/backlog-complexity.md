# Backlog digest — todo tasks by complexity

Snapshot: 2026-10-08. Source: `tasks/*/*.md` (active index, archive excluded).
11 tasks in `todo`; 14 `done`, 1 `in_progress` (`web-phase-grid-spans`).

Complexity here is implementation cost and risk, not value: number of packages
touched, how much of it is new ground, and how much is still undecided.

## Scale

| | Meaning |
|---|---|
| **XL** | new architecture: process/URL model changes, many packages, new config surface |
| **L** | one new subsystem plus its wiring across 3+ packages |
| **M** | a new column/segment on existing rails; all the mechanisms already exist |
| **S** | localized change in one or two files |

## Ranked, hardest first

### 1. `web-token-workspaces` — XL
*Token-prefixed web URLs: one server, many task workspaces per session token.* `medium`

Rewrites the whole URL space of `internal/web` (`/<token>/...`), adds an
in-memory session registry, a per-descriptor `task.Service`, a welcome page
with the process's only `POST`, and a machine-level config file
(`~/.config/mcp-task-manager/web.yaml`). ~15 absolute paths in the templates
must go through a new base-prefix template func. `Deps.Project` stops being a
function and becomes a registry lookup, so the "handlers never resolve" rule
has to be re-established rather than kept.

Open at the start: token reuse on re-registration, per-descriptor index
build timing, poll cost with N boards. Hardest part is that nothing in the
package is left untouched.

### 2. `split-mcp-web-processes` — XL / L
*Split MCP and web into two binaries, run the dashboard as its own process.* `medium`

Decisions are made (own `task.Service`, MCP-only writes, `start_web_ui`
spawns a detached child, two `cmd/`, shared `internal`). The cost is in the
parts that have no precedent in the repo: a structurally read-only resolve
mode that skips `Service.Initialize()`, an on-disk instance marker with
stale-PID/port-taken handling, a detached child's lifecycle, and a
cross-process integration test. `web.with_mcp` has to be reconsidered, and
`internal/web/race_test.go` rewired.

Lower than #1 only because the decisions are already settled.

### 3. `task-extra-fields` — L
*Flexible task fields: arbitrary key/value frontmatter, preserved and exposed everywhere.* `medium`

The load-bearing part is small but unforgiving: a catch-all on the parse side
of `internal/storage/markdown.go` and a merge on the write side, such that
unknown keys survive **every** mutating flow (update, start, complete,
archive, all branch flows) with a stable enough key order to produce no
spurious diff in a user's `tasks/` commit. Then it fans out: MCP tool
payloads, `list_tasks` filtering, card chips, the detail block, the CLI.
Validation and key-collision rules are still undecided.

Risk: a single flow that forgets the merge silently eats user data — which is
exactly today's bug, in a new place.

### 4. `web-backlog-graph` — L
*Whole-backlog relations graph in the workspace viewer.* `high` · blocked

The only task with a genuinely unsolved rendering question: a graph layout
that is offline and embedded — either a server-rendered SVG like the stats
charts, or a vendored JS layout library (binary-size cost, target backlog
size, behaviour beyond it, all to be decided in design). Plus a new
`task.Service` read method, a `g` column kind, per-state URLs, two entry
points, edge styles per configured relation type with `relates_to`
deduplicated.

### 5. `web-task-workspace` — L (mostly delegated)
*Task workspace — board slides away, task column + large file viewer.* `high` · parent

As a unit this is the largest UI feature in the backlog, but three of six
subtasks are shipped (`workspace-tiler` critical, `workspace-markdown`,
`workspace-filenames`) and the strip, chain, rail, kind registry and routes
are in `main`. What is left on the parent itself is closure: it carries the
wip branch, so it is delivered by its own `complete_task` and is gated on
its delivered subtasks' `squash_commit` being an ancestor of its wip.

### 6. `workspace-file-column` — M
*File column: rendered markdown in the wide working area.* `high` · **unblocked**

All three blockers are `done`. What remains: the `f` column's route and
fragment, the rendering rules (`*.md` and extensionless → markdown,
everything else preformatted), the single intentional `template.HTML` wrap in
the package with the comment explaining why, a prose scope in `input.css`,
and the status-code discipline (400 bad name / 404 missing / never 500).
Mechanisms exist; the work is breadth of cases, not depth.

### 7. `workspace-live` — M
*Live update, scroll preservation, CSS rebuild and docs.* `medium` · blocked

New ground in two places: nothing but `#board` polls today, and neither
`hx-preserve` nor the `scroll:`/`show:` swap modifiers are used anywhere in
the repo. Add the archived/"gone" states at poll time, the race test against
concurrent writes, the CSS rebuild, and the documentation of the whole
workspace — this is where CLAUDE.md and README get their workspace sections.
The docs load is a real part of the cost.

### 8. `workspace-task-column` — M
*Task column: openable files, subtasks and related tasks.* `high` · **unblocked**

Partly landed by the tiler: `_col_task.html` already renders `_detail.html`
at the same 22rem, and `resolveTaskColumn` already rebases file and subtask
links onto the column's own place. Remaining: blockers, relation targets and
the parent still emit plain `/tasks/{id}` links; the description as an
openable item; the selected-item marking; the "Open workspace" control on the
panel; extending `TestEscaping`.

### 9. `stats-bars-new-todo` — S/M
*Done-stats bars: segment for freshly created todo tasks.* `medium`

One count in `statsBars`, one view-model field, one `<rect>` over the start
of the todo run, one `.bar-new` class, one clause in the `aria-label`. The
M-ish part is config: a new per-card `new_hours` key, plus the open question
of whether `bar-recent`'s 24 h becomes `recent_hours` for symmetry — and the
constraint that a card's derived `id` must not change, or stored line-toggle
keys break.

### 10. `done-stats-bars-legend` — S
*Tooltip explaining the bar colours on Done statistics cards.* `low`

An SVG `<title>` per row or per `<rect>`, or a four-swatch legend in the card
header; design picks. No Go logic, no new data. Only constraint is the
existing one: no inline `style`, no JS, readable at the column's width.

### 11. `done-stats-config-errors` — S
*`done_stats` config: handling invalid card entries.* `low`

Smallest change, but it is a **decision task**: skip the card and log to
stderr, render an "invalid card" placeholder, or fail the config load — and a
config load error breaks every MCP tool and the CLI, not only the board. Once
the option is picked, the code is a handful of lines in
`internal/config/stats.go`.

## Totals

| Bucket | Tasks |
|---|---|
| XL | `web-token-workspaces`, `split-mcp-web-processes` |
| L | `task-extra-fields`, `web-backlog-graph`, `web-task-workspace` (delegated) |
| M | `workspace-file-column`, `workspace-live`, `workspace-task-column` |
| S | `stats-bars-new-todo`, `done-stats-bars-legend`, `done-stats-config-errors` |

Two observations worth acting on:

- The three **S** tasks all touch the same two files (`_stats_bars.html`,
  `internal/config/stats.go`). Done separately they conflict in the bar
  markup and the `aria-label`; done as one batch they are one small change.
- The two **XL** tasks need the *same* missing primitive — an explicit
  read-only resolve mode that skips `Service.Initialize()`. Whichever runs
  first should ship it as its own commit.
