# web-backlog-graph

## Original input

> давай создадим таск на такую тему
> у нас в UI веба панель с тасками справа самая четыре их на экране
> там выбирается таск а в таске надо сделать списком подтаски
> и когда их выбираешь панели съезжают влево до 4-ой колонки и открывается навоый ворспейс там будут свои панели для таска
> можно будет файлы прикрепленные к таску смотреть, диаграммы и тд

The workspace itself (sliding columns, file viewer) is `web-task-workspace`. This task is the "diagrams" part, which the interview narrowed to a relations graph.

## Clarifications

| Question | Answer |
|----------|--------|
| Which diagram | The **relations graph** of the **whole backlog**, not just the task's neighbours. Rendering the mermaid blocks in files was not requested. |
| Where it opens | **From the task column and from the board.** A "Graph" item in a task's workspace column opens the graph in the large viewer with that task highlighted. A button in the board header opens it with no highlight: the board slides away as for a file, and Back returns. |
| Nodes | **Every task in the active index, done tasks dimmed**: todo, in progress and done, with done visually muted. Archived tasks are not shown; archiving already removes their relations. |
| Edges | parent → subtask, plus every configured relation type (`blocked_by`, `relates_to`, `duplicate_of`, `superseded_by` and custom types from `mcp-tasks.yaml`). |
| Tracker structure | A separate top-level task, `blocked_by` `web-task-workspace`, which provides the viewer area. |
| Priority | high |

## Problem statement

The dashboard has no way to see how tasks connect: the parent/subtask trees and the `blocked_by` / `relates_to` / `duplicate_of` / `superseded_by` edges are visible only one task at a time. Add a read-only graph of the whole active backlog that opens in the workspace viewer, from a task (highlighting it) or from the board.

## Acceptance criteria

### Content
- Nodes: every task in the active index. Each node shows its id and title (truncated), and its status colour reuses the board's dot colours. Done tasks are dimmed. Blocked tasks are marked as on the cards. The highlighted task, when there is one, stands out.
- Edges: parent → subtask, plus one edge per stored relation. Each relation type has a distinct style and a legend. Asymmetric types (`blocked_by`, `duplicate_of`, `superseded_by`) show direction. `relates_to` is drawn once, not twice, even though the index holds a reverse edge. Custom relation types from config get a style too, so none is invisible.
- Isolated tasks (no edges) are still shown, grouped so they don't drown the connected part. The design decides the exact placement.
- The data comes from one new `task.Service` read method that takes the lock once and reads the index once, as `BoardSnapshot` does. It does not scan the archive.

### Entry points and interaction
- A "Graph" item in the workspace task column opens the graph in the viewer with that task highlighted.
- A "Graph" button in the board header opens it full width with no highlight. Back returns to the board.
- Clicking a node opens that task in the workspace (its column, with the graph still in the viewer, highlight moved to it).
- Each graph state has its own URL, so reload, deep link and browser Back work, consistent with `web-task-workspace`.
- The graph refreshes on the same poll as the workspace, and a refresh does not reset what the user is looking at.

### Rendering
- Offline and embedded: no CDN. Either a server-rendered SVG (as the stats charts are, `html/template`, no inline `style`) or a vendored JS layout library under `internal/web/static/`. The design picks one, states the binary-size cost and the largest backlog it targets, and what happens beyond it.
- It stays readable at the backlog sizes the design targets. Pan and zoom, or scrolling within the viewer, are in scope if the layout needs them.
- The read-only rule stays structural: GET only, handlers use `Resolver.Current()`, nothing writes to stdout.

### Tests and docs
- Service test for the graph data: nodes and status, edge types, `relates_to` deduplicated, parent edges, archived tasks absent.
- Handler or fragment tests: the highlighted task is marked, done nodes are dimmed, a node's link points at its workspace URL, and an empty backlog renders a clear empty state.
- `go test -race ./...` passes, and `app.css` is rebuilt if classes change.
- CLAUDE.md Web UI section and project structure are updated, and Dependencies too if a library is vendored.
