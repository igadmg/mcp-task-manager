---
id: workspace-tiler
parent_id: web-task-workspace
title: 'Tiler: URL column chain, strip layout, nesting rail, routes'
status: in_progress
priority: critical
type: feature
created_at: "2026-10-07T19:58:32Z"
created_by: igor.cwer
updated_at: "2026-10-08T06:37:22Z"
branch: igor.cwer/wip/web-task-workspace--workspace-tiler
base_branch: igor.cwer/wip/web-task-workspace
start_commit: 8dee9de6965c93ada4958154a9db50f86341faa0
---

The load-bearing subtask: the column chain as a first-class model, the layout engine that places and shifts columns, the left nesting rail, and the route family they hang off. Every other UI subtask plugs a column type into this.

Decided 2026-10-07 (supersedes the single-level rule in the parent's `task.md`; see its Revision section):

- **Chain is unbounded and heterogeneous.** Opening anything from a column appends a column on the right and the strip shifts left. A column is whatever its URL segments name.
- **URL is `type/ref` pairs in the path**: `/tasks/42/w/t/43/f/plan.md`. The tiler parses the path into a list of column descriptors; truncating the path is how the chain rolls back. A future `g` (graph, `web-backlog-graph`) is one more tag.
- **Whole strip stays in the DOM** and a state is a `translateX` step, so the slide is real CSS and the board keeps polling off-screen.
- **Width is declared by column type** (task narrow, file wide, graph wider). The tiler lays out right to left and pushes older columns off the left edge; what fell off stays reachable through the rail.
- **`/tasks/{id}` is repurposed**: it renders the board with that task's panel open — the state the Back chain ends at. The standalone `max-w-3xl` detail page stops being a separate surface.

## Scope

- A column-chain model in `internal/web`: parse a request path into an ordered list of descriptors (kind + reference), render a canonical path back, append and truncate. Unknown kind, malformed pair, unparseable tail → a clean 404, never a 500.
- A registry of column kinds: each declares its tag, its width class and how it renders. This subtask ships only the kinds it needs to prove the mechanism (the task column's markup is `workspace-task-column`, the file column `workspace-file-column`); it must be possible to add a kind without touching the tiler.
- Layout: a flex strip, `transform: translateX(...)` driven by a CSS custom property the server sets per state, widths from the kind registry — the geometry rule stays in CSS, following the existing `.lanes` / `--lane` precedent (`internal/web/assets/input.css:50-73`). Discrete steps only.
- The nesting rail: a pinned narrow column on the far left listing the whole chain, each entry a link to the truncated path. It is the primary Back; browser Back does the same thing because every state is a real URL.
- Routes: the `/tasks/{id}/w/...` family plus the repurposed `/tasks/{id}`. Page route for a deep link / reload, fragment route for the htmx swap, as `/board` and `/tasks/{id}/panel` already pair.
- Migrate `/tasks/{id}`: `detail.html` as a standalone page goes away or becomes the board page with the panel pre-rendered. `TestDetailPage`, `TestDetailArchivedTask`, `TestDetailUnknownID404`, `TestNoExternalAssetReferences` and the README line about `/tasks/{id}` all need to follow.

## Acceptance criteria

- Chain round-trips: parse → render → parse is the identity for every valid chain, including file names with dots, spaces, unicode and `+`, and text task ids like `web-task-workspace`.
- Appending a column produces the URL the link carries; truncating at rail entry *i* produces exactly the prefix chain. A chain of depth 10 works.
- Every state is reachable by deep link and by reload and renders the same layout it had, including which column is the working one.
- `/tasks/{id}` renders the board with the panel open. `/` is unchanged. Both still render the placeholder when no project is resolved (`internal/web/handlers_test.go:309-329` keeps passing).
- The strip cannot be scrolled horizontally with a mouse or trackpad, and the page has no horizontal scrollbar at any state or viewport. Below `lg` nothing slides: columns and the working area stack vertically.
- The slide is a CSS transition and is instant under `prefers-reduced-motion` (there is no such rule in `input.css` today — this adds the first one).
- Handlers stay read-only and structural: GET patterns only, `Resolver.Current()` only, no stdout. The new paths are added to `TestNoMutatingRoutes`' path list (`internal/web/handlers_test.go:256`) and to `TestHandlerRaceAgainstWrites` (`internal/web/race_test.go:52-56`); the GET sweep still leaves the tasks directory byte-identical.
- An unknown or malformed chain returns 404 with the "gone"-style page, not a 500. A chain naming a task that was deleted meanwhile does the same.
- Tests: chain parse/render table; append/truncate; rail entries and their hrefs; deep-link render per state; `/tasks/{id}` is board+panel; no horizontal overflow markers; reduced-motion rule present in the compiled CSS (`assets_test.go` style).

## Out of scope

- What a task column or a file column actually shows (`workspace-task-column`, `workspace-file-column`).
- Polling and scroll preservation (`workspace-live`).
- The graph column (`web-backlog-graph`) — only its kind tag must fit without reopening the tiler.