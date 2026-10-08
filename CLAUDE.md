# MCP Task Manager

A Go-based MCP server for task management, designed for Claude and coding agents. Also works as a standalone CLI tool.

**Module path:** `github.com/gpayer/mcp-task-manager`

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│              Entry Point (cmd/mcp-task-manager)               │
│        (CLI if args, MCP server otherwise; internal/app)      │
├──────────────────┬───────────────────┬────────────────────────┤
│   CLI Commands   │ MCP Tool Handlers │   Web UI (kanban)      │
│ (list, get, ...) │ (create_task ...) │ (read-only, htmx+CSS)  │
├──────────────────┴───────────────────┴────────────────────────┤
│                        Task Service                           │
│       (business logic, validation, sorting, one mutex)        │
├───────────────────────────────┬───────────────────────────────┤
│            Storage            │     VCS (git branching)       │
│ (markdown files + mem index)  │ (system git, opt-in, vcs pkg) │
└───────────────────────────────┴───────────────────────────────┘
         │                        │
         ▼                        ▼
    ./tasks/{id}/{id}.md           (source of truth)
    ./tasks/archive/{id}/{id}.md   (archived tasks, not indexed)
```

### Package boundary

`internal/task` is the only package that performs task operations;
`internal/storage` is private to it. A consumer that needs data the service
does not expose gets a new method on `task.Service` (see
`internal/task/view.go`), never its own storage handle.

`internal/vcs` wraps the system `git` binary and imports only the standard
library. `project.Build` is the only place that constructs it; `task.Service`
drives it through the `task.GitRepo` interface, which tests wrap for fault
injection.

## Development Process

### Planning, Code Review & Testing
Use the **task manager MCP tools** to organize work:
- `create_task` - Add new tasks for planned work
- `update_task` - Update task status, priority, or details
- `list_tasks` - Review current task state and priorities
- `delete_task` - Remove obsolete tasks

### Implementation
Use the **task manager MCP tools** to get work assignments:
- `get_next_task` - Get the highest priority todo task
- `get_task` - Get a specific task by ID (when directed)
- `start_task` - Mark a task as in progress before starting work
- `complete_task` - Mark a task as done after finishing

### Code Research & Refactoring
Use the **cclsp MCP tools** (LSP server access) for code navigation:
- `find_definition` - Find where a symbol is defined
- `find_references` - Find all usages of a symbol across the codebase
- `rename_symbol` - Safely rename symbols with LSP support
- `get_diagnostics` - Get language diagnostics (errors, warnings) for a file

## Design Decisions

### Storage
- **Source of truth:** Markdown files with YAML frontmatter, one per-task directory (`./tasks/{id}/{id}.md`)
- **Attached files:** free-form named text files live alongside `{id}.md` in the same `./tasks/{id}/` directory
- **Archive:** `./tasks/archive/{id}/{id}.md` — archived tasks (same format, not indexed); archiving moves the whole per-task directory, carrying attached files with it
- **Index:** in-memory only, rebuilt from the .md files; no cache file is ever written
- **Location:** resolved from the project root, not from the server's working directory. Order: absolute `MCP_TASKS_DIR` → `MCP_PROJECT_DIR` → `CLAUDE_PROJECT_DIR` → MCP `roots/list` → marker search upwards from cwd. Inside the root: relative `MCP_TASKS_DIR` → `tasks_dir` from the config → `.tasks` (legacy `tasks/` when that is the directory actually holding tasks). See `internal/config` and `internal/project`.
- **Resolution timing:** lazy, on the first tool call — MCP roots only exist after `initialize`. The tool schemas are registered on defaults and re-published via `tools/list_changed` if the resolved project configures different task types.
- **Config file:** `mcp-tasks.yaml` in the project root
- **Legacy layout migration:** on startup, any task still found in the old flat layout (`tasks/{id}.md` or `tasks/archive/{id}.md`) is automatically migrated into the per-task directory layout

### Task Schema

```yaml
---
id: 42                # string on the wire (JSON: "42"); a bare YAML scalar like this still parses fine
title: "Task title"
status: todo          # todo | in_progress | done
priority: high        # critical | high | medium | low
type: feature         # configurable, defaults: feature, bug
parent_id: 0          # optional, 0 or omitted = top-level task
orphaned_id: 12       # optional, hand-set: the former parent of a subtask moved to the top level (provenance only)
relations:            # optional, omitted when empty
  - type: blocked_by
    task: 3
  - type: relates_to
    task: 7
created_at: 2025-01-15T10:30:00Z
created_by: igor.cwer  # optional, stamped by create_task from the server identity (git email local part, else OS user)
updated_at: 2025-01-15T10:30:00Z
resolution: obsolete  # optional, done tasks only: completed | obsolete | superseded | duplicate | wontfix
resolution_note: "one line on why"   # optional, done tasks only
closed_at: 2025-01-20T09:00:00Z      # optional, set when the task became done
verified_at: 2025-01-19T12:00:00Z    # optional, last time the task's text was checked against reality
branch: dev/wip/42-add-login         # git branching only: the wip branch (kept across a reopen)
base_branch: main_patched            # git branching only: base branch, or the parent's wip for a subtask
start_commit: 3f2a...                # git branching only: the commit the wip branch grows from
final_branch: dev/42-add-login       # git branching only: set by a delivered top-level completion
squash_commit: 9c1b...               # git branching only: the final commit, or the subtask's merge onto the parent's wip
---

Markdown description here.
```

### Task Identification
- Ids are strings. By default `create_task` still allocates an auto-incrementing numeric-looking id, now unpadded (e.g. `"8"`, not `"008"`).
- Optionally, `create_task` accepts a caller-supplied custom text id via its `id` parameter; it is used verbatim as the id and never advances or collides with the numeric auto-increment counter (a numeric-looking custom id like `"5"` still participates correctly in future auto-increment collision avoidance).
- Custom ids are validated the same way attached filenames are: non-empty, no `/` or `\`, not `..`; additionally `"0"`, `"archive"`, `".index.json"` and `".users"` are reserved (they collide with this package's own on-disk sentinels: the retired index cache filename, and the per-user state directory) and rejected.
- Creating a task with an id that already exists (active or archived) is rejected with a clear error, never silently overwritten or disambiguated.
- Per-task directory is always named after the exact id string used: `7/`, `my-feature/`, etc., each containing `{id}.md` (e.g. `7/7.md`) plus any attached files

### Task Lifecycle
- Simple 3-state workflow: `todo` → `in_progress` → `done`
- Blocked state derived from `blocked_by` relations (see Relations section)
- Completed tasks can be archived (see Archiving section)
- `done` says a task is terminal; `resolution` says what happened (`completed`,
  `obsolete`, `superseded`, `duplicate`, `wontfix`). A resolution implies
  closing, a not-delivered one may close a `todo` task and cascades to its open
  subtasks, and reopening clears it. A `done` task without one reads as
  `completed`. See [_design.md](_design.md)
- `verified_at` is orthogonal to the lifecycle: when the task's own text was
  last checked against reality. Inert — it gates nothing
- With git branching enabled, `start_task`, `start_phase implementation` and
  `complete_task` also move git branches (see Git branching), and
  `update_task` refuses the status moves that would bypass them
- Delivery phases (research, design, planning, implementation) are recorded
  per task in `<phase>.phase` files by `start_phase` / `finish_phase` (see
  Phase records); starting any phase of a `todo` task moves it to
  `in_progress`

### Priority Ordering
- Named levels: `critical` > `high` > `medium` > `low`
- Tiebreaker: creation date (older first)

### Subtasks
Tasks support single-level nesting via the `parent_id` field.

**Constraints:**
- Only one level of nesting allowed (subtasks cannot have subtasks)
- Parent task must exist when creating a subtask
- A subtask moved out to the top level by hand (no tool changes `parent_id`)
  keeps its former parent in `orphaned_id`. Provenance only: no behaviour
  reads it, no tool sets it, `get_task` and the CLI show it

**Automatic Behaviors:**
- **Auto-start parent:** Starting a subtask automatically starts its parent (if parent is `todo`)
- **Block parent completion:** Cannot complete a parent task while it has incomplete subtasks
- **Auto-complete parent:** When the last incomplete subtask is completed, the parent is automatically marked `done` — unless the parent has a git branch: it is then delivered by its own `complete_task`, which squashes its wip branch

**Delete Protection:**
- Deleting a parent with subtasks requires explicit action:
  - Use `delete_subtasks: true` to cascade delete all subtasks
  - Or delete subtasks individually first

**Agent Workflow Integration:**
- `get_next_task` prioritizes subtasks of `in_progress` parents over other todo tasks (ensures focused completion of started work)
- `get_next_task` skips parent tasks that have incomplete subtasks (returns subtasks instead)
- `list_tasks` shows top-level tasks by default; use `parent_id` filter to list subtasks of a specific parent
- `get_task` includes subtasks in the response for parent tasks

### Relations
Tasks support typed relations to other tasks via the `relations` frontmatter field.

**Relation Types:**

| Type | Semantic | Behavioral effect | Symmetric |
|------|----------|-------------------|-----------|
| `blocked_by` | Source can't proceed until target is done | Affects `get_next_task`, `start_task`, `get_task`, `list_tasks` | No |
| `relates_to` | Informational link | None | Yes |
| `duplicate_of` | Source is a duplicate of target | None | No |
| `superseded_by` | Another task took the work over | None | No |

Relation types are configurable via `mcp-tasks.yaml`. Behavioral effects are hardcoded to specific type names (`blocked_by`).

**Storage rules:**
- `blocked_by`: stored only on the blocked task (the source)
- `relates_to`: stored on one side only; the index generates the reverse edge
- `duplicate_of`: stored only on the duplicate (the source)
- `superseded_by`: stored only on the superseded task (the source); it is the
  edge behind the `superseded` resolution, which never stores the target id itself
- Validation: target task must exist, no self-references, no duplicates

**Blocking behavior:**
- `get_next_task` skips tasks with unresolved `blocked_by` relations (target not `done`)
- `start_task` refuses to start a blocked task
- `get_task` includes a derived `blocked` field and blocker details
- `list_tasks` includes a derived `blocked` field per task

**Delete cascade:**
- When deleting a task, all relations referencing it (as source or target) are removed
- Other tasks' frontmatter is updated to remove stale relations

**Interaction with subtasks:**
- Relations and subtasks are orthogonal — a subtask can be blocked by a task outside its parent
- `get_next_task` applies both filters: skip parents with incomplete subtasks AND skip blocked tasks

### Archiving

Completed tasks can be archived to keep the active task list clean and the index small.

**Storage:**
- Archiving renames the whole `tasks/{id}/` directory to `tasks/archive/{id}/` (same markdown format, unchanged); attached files move along with it automatically
- No archive index — queries against archived tasks do a linear scan of `archive/{id}/{id}.md`
- The in-memory index shrinks as tasks are archived

**Archive Rules:**
- Only `done` tasks can be archived
- Archiving a parent requires all subtasks to be `done`; archives the entire tree
- All relations to/from archived tasks are cleaned up (same as delete cascade)
- Archived tasks are read-only: can be listed and viewed, but not updated/started/completed

**Auto-Archive:**
- When enabled in config, done tasks older than `after_days` (since `updated_at`) are automatically archived
- A task closed with a resolution other than `completed` skips the grace period and is eligible immediately: there is no delivered work left to review
- Triggers on startup and after each `complete_task` call

### Attached Files

A task can have zero or more free-form named text files attached to it (e.g. research notes, design docs), read and written incrementally over the task's lifetime.

**Storage:**
- Attached files live inside the task's own `tasks/{id}/` directory, alongside `{id}.md`
- Filenames are chosen freely by the caller at write time — no fixed set of categories
- Because attached files share the task's directory, `archive_task` and `delete_task` already move/remove them as a side effect of moving/removing that directory — no separate cascade step is needed

**Rules:**
- Filenames must be non-empty, must not contain a path separator (`/` or `\`) or a `..` segment, and must not collide with the task's own `{id}.md` record file
- Names ending in `.phase` (case and trailing dots/spaces ignored) are reserved for the server's phase records: `write_task_file` refuses them, `read_task_file` and `list_task_files` show them
- `write_task_file` is rejected for archived tasks (archived tasks are read-only, consistent with the rest of this project's archived-task semantics)
- `read_task_file` and `list_task_files` work for both active and archived tasks

### Web UI

A read-only kanban dashboard, served by `internal/web` (`net/http` +
`html/template` + `http.ServeMux` patterns; no framework).

- **One process, two transports.** MCP (stdio) and HTTP share a single resolved project and a single `task.Service`. `internal/app` is the composition root.
- **Ways to start it:** `web.enabled` in the config (or `MCP_WEB_ENABLED`) brings it up with the MCP server; the `start_web_ui` tool starts it on demand; `mcp-task-manager serve web` runs it in the foreground.
- **Read-only is structural.** Only `GET` patterns are registered, so `ServeMux` answers everything else with 405, and no handler can reach a mutating service method.
- **Handlers never resolve.** They read `Resolver.Current()`, never `Get()`: resolution runs `Service.Initialize()`, which migrates the layout and may auto-archive, and a plain GET must not move files. Before the first tool call the board renders a placeholder that polls itself back to life.
- **Assets are embedded.** Tailwind output and htmx are vendored under `internal/web/static/` and compiled in with `go:embed`; the page renders offline. Regenerate the CSS with `scripts/build-css.sh` (on Windows x64, run it from Git Bash) after editing templates or `input.css` — a maintainer step, never part of `go build`. `input.css` imports Tailwind with `source(none)`, so only `@source "../templates"` is scanned and the output does not depend on the directory the script runs from. Static assets are served as immutable, but the layout links them as `/static/<name>?v=<content hash>` (`asset` template func), so a changed asset is a new URL and no hard reload is needed.
- **Danger zone.** In-progress tasks are highlighted and named in a banner, so a human reading the board knows an agent may be editing those areas. Presentation only; the UI stays read-only.
- **Phase lanes.** The In progress column groups its cards into four virtual, overlapping lanes: Research, Design, Planning and Implementation. Cards stay in one vertical stack, and each later lane is shifted right by half a card: `.lane-<phase>` sets `--lane` and `.lanes` sets the step in `assets/input.css`. Cards keep a minimum width of 11rem, so the step compresses in narrow columns, and the indent collapses below 14rem. A task's lane is the phase of its latest started run in its `<phase>.phase` records (ties go to the later phase; see Phase records). A task without readable records falls back to its attached workflow files (names from the `begin_task` skill): `research` → Design, `design` → Planning, `plan` (or `implementation`) → Implementation, only `task` or nothing → Research. The furthest artifact wins, case is ignored and an optional `.md` suffix counts. A card holding nested in-progress subtasks sits in the furthest phase of its group. `task.Service.BoardSnapshot` computes it (`Phases`, plus `PhaseInfo` for tasks with records; in-progress tasks only, at most four small file reads each per poll; a failed listing reads as Research). The column header counts every in-progress task; each lane counts the tasks on its cards, so the lane counts add up to the total. Empty lanes show a header-only stub. In progress takes half the board from `xl`.
- **Nested subtasks.** A subtask nests inside its parent's card when both sit
  in the same column. One case nests and keeps its own card: a `todo` subtask
  of an `in_progress` parent, so the parent's card lists the work still ahead
  of it while the subtask stays in the To do queue. Column counts are per
  status and lane counts count only in-progress cards, so neither double-counts
  it. Every nested row carries a status dot.
- **Card and detail content.** Every card shows its creator and creation time. An in-progress card with phase records also shows the current phase, who started its latest run and when (or when it finished), and the tokens of all finished runs (`81.2k tok`). Todo and done cards never read phase records. The detail view lists every run of every phase (`.phase-runs` grid) with the total tokens, and leaves `*.phase` files out of "Attached files".
- **Archived tasks are not on the board** (the snapshot is the active index); the detail route still serves them, read-only.
- **Logging goes to stderr.** Nothing in `internal/web` writes to stdout — in stdio mode stdout is the JSON-RPC channel.
- **Branch chip.** A card shows the task's branch (final once delivered, wip before) with a copy button; the detail view has a Git block. Done tasks have no board card, so a delivered task's final branch is visible only in its detail view. Copying is `static/app.js`, one delegated click listener with no request and no htmx attribute.
- **Done column.** Done renders statistics cards, not task cards; its header still counts the done tasks, and `/tasks/{id}` still serves them. `ColumnView.Stats` (set only for Done, like `Lanes` only for In progress) holds the cards of `BoardSnapshot.Stats` in config order (`StatsCardView.Kind` picks `_stats_bars.html` or `_stats_lines.html`; unknown kinds are dropped). `_board.html` branches `.Lanes` → `.Stats` → cards, so a Done column with no drawable cards (`cards: []`) shows the `empty` stub. `_stats_bars.html` draws one row per value: the value, the label `done/total · N open · +M` (`N open` and `+M` left out at 0), and a stacked bar that is an inline SVG with `viewBox="0 0 <total> 1"` and one `<rect>` per non-empty segment, `x`/`width` in task counts (`StatsBarView.RecentX`, `TodoX`). The browser scales it, so there is no geometry in Go and no `style` attribute. Segments reuse the column dot colours (`.bar-done`, `.bar-in_progress`, `.bar-todo`); the last 24 h closures are `.bar-recent` (emerald-200) over the end of done. Each card carries `data-stats-card="<card id>"`, each row `data-value`.
- **Lines charts.** `_stats_lines.html` draws a lines card as an inline SVG whose `viewBox` is the data range, like the bars: day `i` spans x `2i..2i+2` with its points at `2i+1` (`StatsChartView.Width` = 2 × days), y is `Max - count`. Go prints integers only (`newStatsChart`), the browser stretches them (`preserveAspectRatio="none"`), and `vector-effect: non-scaling-stroke` keeps the lines 1.5px. Per-day lines and running totals (`task.StatsLine.Cumulative`: a `*_cumulative` line, or every line of a split card with a cumulative metric) get one nested `<svg>` each with its own `Max`, so neither flattens the other; `Max` is the group's peak over all its lines, hidden ones included (a toggle never rescales), floored at 1, so an all-zero group is a flat baseline. A one-day window draws a flat segment `0,y 2,y` (a one-point polyline paints nothing). Day labels (first, last) and the scales (`5/day · 25 total`) are HTML under the chart, never SVG text. Each day is a transparent `<rect class="stats-day">` with a `<title>` listing every line (`Oct 5 · created 3 · closed 1`); there are no per-point markers. Colours are named classes for both the polyline and its legend swatch: `series-created` (sky-400), `series-closed` (emerald-400), `series-created_cumulative` (violet-400), `series-closed_cumulative` (lime-400), and `series-<i mod 8>` by position for split values, so a key never becomes a class. Hooks for the toggles: the card has `data-stats-card`, every polyline and legend `<button>` has `data-stats-line="<key>"`; a line listed in `hidden` renders with `stats-off` (`display: none`) and its button `aria-pressed="false"` (dimmed, struck through). A card without lines (unknown metric, no split value in the window) says `no data`.
- **Line toggles.** `static/app.js` has a delegated click on `.stats-legend-item[data-stats-line]`: it flips the button's `aria-pressed` and `stats-off` on the card's polylines with the same `data-stats-line`, and records the choice as `"on"`/`"off"` under `mcp-task-manager.stats-line:` + `JSON.stringify([cardId, lineKey])` (never htmx's `htmx-history-cache`). Only clicked lines are stored, so a line without an entry keeps the server's config default and a config change never breaks a key. An in-memory map mirrors every choice and every `localStorage` access is in try/catch, so without storage the toggles still survive polls until a reload. The poll swaps in fresh server markup, so `applyAll()` re-applies the recorded choices to the whole document on start and on `htmx:afterSettle`, `htmx:load` and `htmx:historyRestore`. No request, no `hx-*` (`TestStatsLegendHasNoHtmxAttributes`, `TestAppJSStatsToggles`); like `app.css`, `app.js` is linked by content hash.
- **Done statistics (data).** `BoardSnapshot.Stats` holds one `task.StatsCard` per configured `web.done_stats` card, in config order, computed by `computeStats` (`internal/task/stats.go`) from the snapshot's own `index.All()` read under the same lock, so no second index sync and no archive scan. Bars: per value of `priority`, `type`, `status`, `resolution` or `created_by`, the total, the done / in progress / todo split, and `ClosedRecently` (done tasks whose close time is in the last 24 h, future excluded). The close time is `closed_at`, else `updated_at`. Every task counts once, subtasks included; empty values are dropped; resolution is a done task's `EffectiveResolution`. Values come in domain order (priority `Priorities()`, `task_types` order, status `Statuses()`, `Resolutions()`), then unknown values alphabetically; each field's value and order live in one table, `statsFields`. Lines: one value per calendar day over `days` days, today last, bucketed by civil date in the clock's location (DST-safe). `created` / `closed` count per day; the `_cumulative` variants are running totals that start at 0 at the window start, flagged `Cumulative` so the web layer never parses line names. A split card draws its metric per value, only for values with a count in the window. `Hidden` marks the lines the card lists as hidden. An unknown kind, field, line name or metric yields no data, never an error. The clock is `task.WithClock` (default `time.Now`); its `Location()` sets the day zone, so tests pin both.

### Git branching

Opt-in (`git.branching`, or `MCP_GIT_BRANCHING`). `start_task`,
`start_phase implementation` and `complete_task` then manage per-task
branches in the git repository at the project root, running the system `git`
(`internal/vcs`). The research, design and planning phases never touch git, so
under the phase workflow a branch appears only when implementation starts. The
full rationale, sequences and rejected alternatives are in
`tasks/git-branch-per-task/design`.

- **The tasks directory is never committed by the server.** Server commits are
  built in a temporary index that excludes it (`:(top,exclude)`), wherever it
  lives: tracked, gitignored or nested in the code repo, a separate repo, or
  outside any repo. Records and the pointer are branch-independent files, so
  the index is authoritative on any branch. Users commit `tasks/` themselves.
- **Naming.** `<user>/wip/<name>` for a top-level task, `<parent wip>--<name>`
  for a subtask (a sibling, never a child ref), final branch `<user>/<name>`.
  `<user>` is the sanitized local part of `user.email`, required when
  branching; `<name>` is the id if it is a readable slug, else id + slugified
  title, else `task-<sha1>`. `BranchAvailable` refuses collisions.
- **Base.** A top-level task branches from the first existing entry of
  `git.base_branches` (default `main_patched`, `master_patched`, `main`,
  `master`), wherever HEAD is.
- **Vacate rule.** Before switching away: uncommitted code on a server-owned
  wip branch is checkpointed there; on the chosen base of a fresh start it is
  carried; anywhere else the call refuses (listing up to 20 paths). Tasks-dir
  changes never count. A detached HEAD is refused.
- **Start** creates the wip at the base tip (a subtask: at the parent's wip
  tip, starting a todo parent in the same journal) and switches to it.
  **Restart** (`start_task` on a task whose wip ref exists, in progress or
  reopened to todo) replays the wip onto the current base or parent tip with
  `git replay` (print mode; the ref is moved with compare-and-swap, or with
  `reset --keep` when checked out) and rewrites `start_commit`.
- **Implementation start** (`start_phase implementation`) runs the same
  flows: a restart when a live wip exists; otherwise a fresh or subtask start,
  also for a task already `in_progress` from its earlier phases. A subtask
  whose parent is `in_progress` without a branch cuts the parent's wip first,
  in the same journal (the parent gets no phase run). `start_task` keeps the
  legacy rule instead: under such a parent the subtask starts record-only, and
  on an `in_progress` task without a branch it refuses with a hint to use
  `start_phase implementation`. A dead wip ref on an `in_progress` task is
  refused by both.
- **Complete, delivered.** Snapshot everything onto the wip, then
  `commit-tree` its tree on `start_commit`: the final branch is base + exactly
  one commit (re-completion moves a final branch the task recorded, with
  compare-and-swap). A subtask is squash-merged instead (`merge-tree`) as one
  commit onto the parent's wip. The parent is never auto-completed, and its
  own delivery is refused while a delivered subtask's `squash_commit` is not
  an ancestor of its wip (the parent gate).
- **Complete, not delivered**, branch checked out: a safety snapshot on the
  wip, records closed (with the usual cascade), HEAD back to the base or the
  parent's wip. Branch not checked out: record-only, no git.
- **Rollback.** Every flow is preflight, then journaled ref/index/record/
  pointer mutations (`gitTxn`), then one worktree-changing `switch` or
  `reset --keep`, then an explicit `index.Load()`. Any failure rolls the
  journal back: refs, HEAD, index, worktree, records, pointer and phase
  records end up as they were. Fault-injection tests fail every mutating step
  in turn, and the phase-file write too.
- **`update_task` guard.** Refuses `todo → in_progress`, closing an
  `in_progress` task that has a branch, and `done → in_progress` for one;
  reopening to `todo` is allowed and keeps the branch fields.
- **Legacy tasks.** A task without `branch` (started while branching was off)
  takes the old record-only paths.

### Current-task pointer

`<tasks_dir>/.users/<user>/current_task`, one per user (`<user>` from the git
email, falling back to the OS user). `start_task` writes it; `complete_task`
with any resolution moves it to the still-open parent of a subtask, or removes
it — only when it names a task that call closed. `get_current_task` reads it
(archived tasks included). It is written in both modes and journaled with the
rest of a flow. `.users` is skipped by every scan and reserved as an id; this
repository gitignores `tasks/.users/`. Every successful `start_phase` points
the user at its task too; `finish_phase` leaves the pointer alone.

### Phase records

One server-owned YAML file per phase, `<tasks_dir>/<id>/<phase>.phase`
(`research`, `design`, `planning`, `implementation`), holding every run of
that phase: `started_at`, `started_by`, and once finished `finished_at`,
`finished_by`, `tokens` (caller-reported, optional) and `note`. Written only
through `task.PhaseStore` (`storage/phase.go`); `version: 1`, a newer version
is refused, the file name wins over its `phase` key. Subtasks have their own.

- **`start_phase(phase, id?)`** (id defaults to the current task) appends a
  run, never overwrites. Refused, before anything changes: archived or done
  tasks; any open run of the task (one at a time); a phase whose predecessor
  has no finished run (migration escape: a task with no `.phase` file at all
  may start any phase its artifact names prove); a done parent; a blocked
  `todo` task. A re-run after later phases is allowed and moves the lane back.
- **Status.** On a `todo` task any phase moves it (and a `todo` parent) to
  `in_progress`, record-only and without git; implementation under git
  branching runs the branch flows (see Git branching). The run is appended
  inside the same journaled flow, before the worktree changes.
- **`finish_phase(phase, id?, tokens?, note?)`** stamps the open run; tokens
  must be an integer from 0 to 1e15. No status, pointer or git change; allowed
  on done tasks.
- **`complete_task`** finishes every open run of every task it closes
  (cascaded subtasks and an auto-completed parent included), without tokens
  and with the note `closed by complete_task (<resolution>)`, journaled.
  `update_task` to done leaves runs open.
- **Reading.** `get_task` returns `phases`; `get_current_task` does not. An
  unreadable record is reported by the phase tools ("fix or delete it"),
  skipped by the board and the detail view, and left open by `complete_task`.

## MCP Tools

### Task Management
| Tool | Description |
|------|-------------|
| `create_task` | Create a new task with title, description, priority, type, optional `parent_id` for subtasks, and optional `id` for a caller-supplied custom task id; stamps `created_by` |
| `update_task` | Modify task fields, including `resolution` / `resolution_note` (closes the task) and `verified` (stamps `verified_at`). Under git branching, refuses status moves that belong to `start_task` / `complete_task` |
| `list_tasks` | List tasks with optional filters (status, priority, type, parent_id, resolution, archived); top-level tasks by default |
| `get_task` | Get full details of a task by ID (includes subtasks for parent tasks, `created_by` and the `phases` run history; falls back to archive) |
| `delete_task` | Remove a task; use `delete_subtasks: true` to cascade delete subtasks |
| `archive_task` | Archive a completed task (moves its directory, including attached files, to `tasks/archive/`) |

### Attached Files
| Tool | Description |
|------|-------------|
| `write_task_file` | Create or overwrite a named text file attached to a task (rejected for archived tasks and for reserved `*.phase` names) |
| `read_task_file` | Read the content of a named file attached to a task (works for active and archived tasks) |
| `list_task_files` | List the names of all files attached to a task (works for active and archived tasks) |

### Web UI
| Tool | Description |
|------|-------------|
| `start_web_ui` | Start the read-only kanban dashboard and report its URL; optional `addr`. Idempotent — a second call reports the running URL and never double-binds |

### Relations
| Tool | Description |
|------|-------------|
| `add_relation` | Add a relation (`source`, `type`, `target`) between two tasks |
| `remove_relation` | Remove a relation (`source`, `type`, `target`) between two tasks |

### Agent Workflow
| Tool | Description |
|------|-------------|
| `get_next_task` | Returns highest priority `todo` task (skips parents with incomplete subtasks and blocked tasks) |
| `start_task` | Move task from `todo` to `in_progress` (auto-starts parent if subtask; refuses if blocked) and point the current-task pointer at it; writes no phase records. Under git branching: create and check out its wip branch, or restart (rebase and check out) an existing one; an `in_progress` task without a branch is refused with a hint to use `start_phase implementation` |
| `complete_task` | Close a task: `in_progress` → `done` (auto-completes parent if last subtask; triggers auto-archive if enabled). With a `resolution` other than `completed` it also accepts a `todo` task and closes its open subtasks along with it. Finishes every open phase run of each task it closes. Under git branching: squash onto the final branch (or merge into the parent's wip) with optional `commit_message`, or save a safety commit and return to the base |
| `get_current_task` | Return the task the calling user's current-task pointer names; "No current task" when there is none |
| `start_phase` | Start a run of a delivery phase (`research`, `design`, `planning`, `implementation`) on a task (`id`, default the current task) and record it in `<phase>.phase`; moves a `todo` task to `in_progress` and points the current-task pointer at it. `implementation` under git branching does the `start_task` branch work (cutting a branchless parent's wip for a subtask) |
| `finish_phase` | Finish the open run of a phase with optional `tokens` and `note`; no status or git change |

## Configuration

`mcp-tasks.yaml`, in the project root:
```yaml
tasks_dir: .tasks     # optional, relative to the project root; default: .tasks
task_types:
  - feature
  - bug
relation_types:       # optional, defaults to these four
  - blocked_by
  - relates_to
  - duplicate_of
  - superseded_by
auto_archive:         # optional
  enabled: false      # default: false
  after_days: 30      # default: 30
web:                  # optional
  enabled: false      # start the dashboard with the MCP server; default: false
  addr: 127.0.0.1:7777  # default
  with_mcp: false     # `serve web` also serves MCP over stdio; default: false
  done_stats:         # Done-column statistics cards; default: the four below
    cards:            # board order; `cards: []` = no cards
      - kind: bars    # bars | lines; blank: bars with a field, else lines
        field: priority
      - {kind: bars, field: type}
      - {kind: bars, field: resolution}
      - kind: lines
        id: recent    # optional stable key (line toggles); derived when blank
        title: Last 14 days  # optional, derived when blank
        days: 14      # default 14; <= 0 also means 14
        lines: [created, closed]  # + created_cumulative, closed_cumulative; default created, closed
        hidden: []    # lines (or split values) that start switched off
      - kind: lines
        split_by: priority  # one line per value of this field
        metric: closed      # line name drawn per value; default closed
git:                  # optional
  branching: false    # git branch per task; default: false
  base_branches:      # first existing one is the base; default: these four
    - main_patched
    - master_patched
    - main
    - master
```

A partially written section keeps the defaults for the keys it omits
(`config.applyDefaults`), so `web: {enabled: true}` still listens on the
default address, `auto_archive: {enabled: true}` still waits 30 days, and a
`base_branches` list that is empty after trimming falls back to the defaults.

`web.done_stats.cards` (`internal/config/stats.go`): an absent or null list
means the defaults (bars for priority, type and resolution, plus a 14-day
lines card with created and closed); `cards: []` means no cards. yaml decodes
every card from zero, so per-card defaults are filled after decoding: kind,
`days`, `lines`, `metric`, a title, and an `id` derived from the content
(`bars-<field>`, `lines-<days>d`, `lines-<split_by>-<metric>-<days>d`), made
unique in list order with `-2`, `-3`. Consumers read
`Config.DoneStatsCards()`, which is nil-safe, fills the same defaults for a
config that skipped `applyDefaults`, and returns a copy. A YAML type error
such as `days: abc` fails the load like any other.

An **invalid card is reported, never rejected and never dropped**: a typo in a
dashboard card must not break every MCP tool and the CLI along with the board.
`ValidateStatsCards` (pure) checks the written cards against the three
vocabularies - `statsKinds`, `statsFieldNames`, `statsLineNames`, the latter
two exported as `StatsFields()` / `StatsLineNames()` because `internal/task`
owns the behaviour behind each name and imports this package, so `config`
cannot ask it; `TestStatsFieldsMatchConfig` and `TestStatsLineNamesMatchConfig`
in `internal/task` fail if the copies drift. It flags an unknown kind, field,
`split_by`, `metric` or line name, a bars card with no field, a `hidden` entry
that is not one of a non-split card's lines, and a negative `days`. `days: 0`
is **not** flagged: yaml decodes an absent key and a written zero alike, so
only a negative value proves the key was written. A split card's `hidden`
names field values, which only the backlog knows, so it is not checked.
`applyDefaults` validates before normalizing (normalization repairs some of
what is reported) and keeps the findings on `Web.DoneStats.Problems`;
`Resolve` writes one line per finding to **stderr** (`statsWarnTo`, never
stdout - in stdio mode that is the JSON-RPC channel), each naming the card's
index, its derived id, what is wrong and what follows from it. Consumers that
want to show the findings themselves - a board placeholder, say - read
`Problems` instead of re-deriving them. The rationale and the rejected
alternatives (fail the load; an "invalid card" placeholder on the board) are
in `tasks/done-stats-config-errors/decision.md`.

Environment overrides:

| Variable | Effect |
|----------|--------|
| `MCP_TASKS_DIR` | tasks directory; absolute wins outright, relative is project-root relative |
| `MCP_PROJECT_DIR` | explicit project root |
| `CLAUDE_PROJECT_DIR` | project root, set by Claude Code |
| `MCP_ROOT_SOURCE` | restrict resolution to one source (`roots` to exercise the protocol path) |
| `MCP_WEB_ENABLED` | start the web dashboard with the MCP server (bool; unparseable values ignored) |
| `MCP_WEB_ADDR` | dashboard listen address; never enables it on its own |
| `MCP_GIT_BRANCHING` | git branch per task on/off (bool; unparseable values ignored). No override for the base list |

`mcp-task-manager version` prints the resolved root, tasks directory and source.

## Dependencies

- **MCP SDK:** `github.com/mark3labs/mcp-go` - Third-party Go MCP implementation
- **YAML parsing:** `gopkg.in/yaml.v3` - For frontmatter and config
- **CLI parsing:** `github.com/integrii/flaggy` - Lightweight CLI argument parser

## Project Structure

```
mcp-task-manager/
├── cmd/
│   └── mcp-task-manager/
│       └── main.go              # Entry point: mode selection + signal context
├── internal/
│   ├── app/
│   │   ├── app.go               # Composition root: RunMCP, RunWeb, MCP server assembly
│   │   ├── icon.png             # Embedded server icon
│   │   └── instructions.md      # Embedded server instructions
│   ├── cli/
│   │   ├── cli.go               # CLI entry point and subcommand setup
│   │   ├── cli_test.go          # CLI tests
│   │   ├── commands.go          # Command handlers (list, get, create, etc.)
│   │   ├── output.go            # Output formatters (table, JSON)
│   │   └── output_test.go       # Output formatter tests
│   ├── config/
│   │   ├── config.go            # Project root / tasks dir resolution + config loading
│   │   ├── stats.go             # web.done_stats cards: defaults, per-card defaults, DoneStatsCards, ValidateStatsCards
│   │   └── resolve_test.go      # Resolution order tests
│   ├── project/
│   │   ├── resolver.go          # Lazy, cached project resolution (used by tool handlers)
│   │   └── roots.go             # MCP roots/list client request + file:// URI parsing
│   ├── storage/
│   │   ├── storage.go           # Storage interface
│   │   ├── markdown.go          # Markdown file operations (per-task directory layout)
│   │   ├── migrate.go           # Legacy flat-layout migration
│   │   ├── files.go             # Attached-file read/write/list, reserved names
│   │   ├── phase.go             # <phase>.phase records (task.PhaseStore), YAML codec
│   │   ├── current.go           # Per-user current-task pointer (.users/<user>/current_task)
│   │   └── index.go             # In-memory index over the task directories
│   ├── task/
│   │   ├── task.go              # Task model/types (status, priority, resolution, branch fields)
│   │   ├── service.go           # Business logic (single mutex, twin discipline)
│   │   ├── git.go               # GitRepo, Identity, CurrentTaskStore, ServiceOptions
│   │   ├── branching.go         # Branch naming, commit messages, rollback journal, update guard
│   │   ├── branching_start.go   # Fresh and subtask start flows, vacate rule
│   │   ├── branching_complete.go # Delivered / subtask / abandoned completion, parent gate
│   │   ├── branching_restart.go # Restart: replay the wip onto its parent line
│   │   ├── current.go           # CurrentTask and the pointer rules
│   │   ├── phase.go             # Phase names, order, reserved *.phase names; name-based fallback derivation
│   │   ├── phaserecord.go       # PhaseRun, PhaseRecord, PhaseSummary
│   │   ├── phaseflow.go         # StartPhase, FinishPhase, the one phase loader; open runs closed on completion
│   │   ├── stats.go             # Done-column statistics (bars, per-day lines) for BoardSnapshot
│   │   └── view.go              # Board/detail composites for read-only consumers
│   ├── testsupport/
│   │   ├── testsupport.go       # Shared test backlog helpers (NewBacklog, Seed, IsolateEnv)
│   │   ├── git.go               # RequireGit, NewGitRepo, Git, WriteFile
│   │   └── gitbacklog.go        # NewGitBacklog layouts, CaptureState, RequireStateEqual
│   ├── tools/
│   │   ├── tools.go             # Tool registration + the WebStarter interface
│   │   ├── management.go        # create, update, list, get, delete
│   │   ├── workflow.go          # get_next_task, start, complete
│   │   ├── relations.go         # add_relation, remove_relation
│   │   ├── files.go             # write_task_file, read_task_file, list_task_files
│   │   └── web.go               # start_web_ui
│   ├── vcs/
│   │   ├── vcs.go               # Repo, runner, Check, Head, ResolveIdentity
│   │   ├── refs.go              # Branch queries and compare-and-swap ref updates, switch
│   │   └── tree.go              # Snapshot (tasks dir excluded), merge-tree, commit-tree, replay
│   └── web/
│       ├── server.go            # Route table (GET only)
│       ├── handlers.go          # board, board fragment, detail, panel, health
│       ├── view.go              # View models + pure mapping from the snapshot
│       ├── controller.go        # Listener lifecycle: Start / URL / Shutdown
│       ├── templates.go         # Embedded template sets
│       ├── assets.go            # Embedded static assets
│       ├── templates/           # layout, board, card, detail, placeholder
│       ├── assets/input.css     # Tailwind entry (input to scripts/build-css.sh)
│       └── static/              # app.css + htmx.min.js + app.js, committed and embedded
├── scripts/
│   └── build-css.sh             # Maintainer step: rebuild the vendored Tailwind CSS
├── mcp-tasks.yaml               # Default config (for reference)
├── _design.md                   # Resolution / verified_at: rationale and rules
├── go.mod
├── go.sum
├── CLAUDE.md
└── README.md
```

## Behavior & Error Handling

### get_next_task
- Returns highest priority `todo` task (priority order, then oldest first)
- Skips parent tasks that have incomplete subtasks (returns actionable subtasks instead)
- Skips tasks with unresolved `blocked_by` relations
- If no `todo` tasks exist, returns "no tasks available" message (not an error)

### Index
- In-memory only; it has no on-disk form and nothing is persisted after a write
- Built on server startup by scanning all .md files
- Self-healing: rebuilt whenever a task file's mtime or the task count diverges from what is held in memory, which covers git pulls and hand edits
- Includes relation edges (with auto-generated reverse edges for symmetric types)

### Concurrency
- A single `sync.Mutex` on `task.Service` serializes every task operation. It is the only lock over the index and the markdown storage, both of which are reachable solely through that type.
- **Twin discipline:** exported methods lock once on entry and delegate to an unexported, unlocked twin (`Get`/`get`, `Update`/`update`, …). Service methods never call each other's exported forms — Go mutexes are not reentrant. `TestServiceNoSelfDeadlock` fails if a new method breaks the shape.
- `EnsureProjectExists`, `ProjectFound`, `Config` and `BranchingEnabled` are deliberately unlocked: they read only write-once fields (as is the stats clock `now`, set by `WithClock`).
- Git runs inside `StartTask` / `StartPhase` / `CompleteTask` while the lock is held, so a flow's ref, record, pointer and phase-record changes are atomic to every other call; web reads wait for it (each git command has a 60 s timeout). `StartPhase` and `FinishPhase` follow the twin discipline and are in `TestServiceNoSelfDeadlock` and `TestServiceRace`.
- The lock matters because mcp-go's stdio server dispatches tool calls across a worker pool, and because the web dashboard reads the same service concurrently. `go test -race` covers both (`internal/task/concurrency_test.go`, `internal/web/race_test.go`).
- File writes are atomic (write to temp file, then rename)
- Cross-process coordination (file locks, PID files) stays out of scope: whichever transport the process runs, the other comes up inside it.

### Validation
- Task IDs: strings, format-validated the same way attached filenames are (non-empty, no `/` or `\`, not `..`), plus four reserved names (`"0"`, `"archive"`, `".index.json"` - a retired cache filename - and `".users"`, the per-user state directory) rejected for a caller-supplied custom id
- Status: `todo` | `in_progress` | `done`
- Priority: `critical` | `high` | `medium` | `low`
- Type: must be in configured list (default: `feature`, `bug`)
- Relation type: must be in configured list (default: `blocked_by`, `relates_to`, `duplicate_of`, `superseded_by`)
- Resolution: `completed` | `obsolete` | `superseded` | `duplicate` | `wontfix`; only valid on a `done` task
- Phase: `research` | `design` | `planning` | `implementation`; tokens a non-negative integer (at most 1e15)
- Attached filenames: `{id}.md` and anything ending in `.phase` are reserved for writes
- Config: `applyDefaults` fills in what a partially written YAML section left out, so a half-specified `web:` or `auto_archive:` block cannot silently zero the rest; it also fills each `web.done_stats` card's defaults (nothing in a card is validated yet)

## Future Considerations (Post-MVP)
- Comments/history
- Cycle detection for `blocked_by` chains
- Additional task types (chore, docs, refactor)
- `unarchive` / restore task from archive back to active
- Archive indexing (only needed if archive query performance becomes a problem)
