---
id: workspace-live
parent_id: web-task-workspace
title: Live update, scroll preservation, CSS rebuild and docs
status: todo
priority: medium
type: feature
relations:
  - type: blocked_by
    task: workspace-task-column
  - type: blocked_by
    task: workspace-file-column
created_at: "2026-10-07T20:00:03Z"
created_by: igor.cwer
updated_at: "2026-10-07T20:00:19Z"
---

Makes the workspace live — a file an agent is writing updates on screen — and closes the task: CSS rebuild and documentation.

Blocked by `workspace-task-column` and `workspace-file-column`: there must be columns to refresh.

## Scope

- Polling: every column in the chain and the working area refresh on `PollSeconds`, the way `#board` does today (`hx-get` + `hx-trigger="every Ns"` + `hx-swap="outerHTML"`, `internal/web/templates/_board.html:1`). Note that today **nothing but `#board` polls** — the panel sits outside it (`internal/web/templates/board.html:7-11`) and is never refreshed, so this is new behaviour, not a copy.
- The board keeps polling while off-screen: the strip keeps it in the DOM (decided 2026-10-07), so this subtask only has to confirm it and measure the cost — each poll takes the service lock and `Index.All` may rebuild the whole index (`internal/storage/index.go:202-249`, `:290`).
- Scroll preservation: the working area keeps its scroll position across a refresh, and the open state (which column is selected, which file is open) survives the swap. htmx 2.0.4 already ships `hx-preserve` and the `scroll:` / `show:` swap modifiers — neither is used anywhere in this repo today, so whichever is chosen is new ground. Any script goes in `internal/web/static/app.js`, delegated and request-free, like the existing two IIFEs; `TestAppJSStatsToggles` (`internal/web/assets_test.go:74-93`) bans `fetch(`, `XMLHttpRequest`, `htmx.ajax` and `hx-` in that file and must stay green.
- A task archived or deleted while open shows its archived / "gone" state at the next poll rather than erroring.
- `scripts/build-css.sh` run and the regenerated `app.css` committed; the `assets_test.go` checks for every new class added.
- Docs: CLAUDE.md's Web UI section (the workspace, the chain URL scheme, the tiler and its width rule, the rail, the column kinds, polling), the project-structure tree, and the Behavior notes. README's Web Dashboard section, including the `/tasks/{id}` change. **The Dependencies list is deliberately not touched** — the markdown renderer is in-house (goldmark was declined), which is itself worth a line.

## Acceptance criteria

- With the workspace open, editing a file on disk makes the working area show the new content within one poll; adding a file makes it appear in its column.
- The working area's scroll position is unchanged across a poll, and no column loses its selected marking.
- Deleting the task the chain is rooted at shows the "gone" state at the next poll; archiving it shows the archived state. Neither logs an error or returns a 500.
- A poll never mutates anything: the new routes are in `TestNoMutatingRoutes`' sweep and the tasks directory stays byte-identical (`internal/web/handlers_test.go:252-275`).
- `TestHandlerRaceAgainstWrites` (`internal/web/race_test.go`) exercises the workspace routes against concurrent `WriteTaskFile` / `StartPhase` / `Update`, and `go test -race ./...` passes.
- Nothing in `internal/web` writes to stdout.
- `app.css` is rebuilt and committed; every class the workspace templates introduce is asserted present.
- CLAUDE.md and README describe the workspace as built, including the superseded single-level model being gone.

## Out of scope

- A configurable poll interval. `PollSeconds` is still never set by either call site (`internal/app/app.go:70`, `:110`) and stays the constant 5; adding a config key is a separate task if it is wanted.
- Pausing the board poll off-screen (considered and declined: the strip keeps it live).