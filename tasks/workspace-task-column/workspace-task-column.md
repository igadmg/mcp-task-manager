---
id: workspace-task-column
parent_id: web-task-workspace
title: 'Task column: openable files, subtasks and related tasks'
status: todo
priority: high
type: feature
relations:
  - type: blocked_by
    task: workspace-tiler
created_at: "2026-10-07T19:58:54Z"
created_by: igor.cwer
updated_at: "2026-10-07T20:00:06Z"
---

The `t` column kind: one task rendered narrow, with everything in it being a link that appends the next column. This is what makes the chain navigable — in the current panel, attached files are inert chips (`internal/web/templates/_detail.html:117-124`) and subtasks, blockers and relation targets are plain full-page links (`:49-62`, `:34-47`, `:64-77`).

Blocked by `workspace-tiler`: it plugs a kind into that registry.

## Scope

- The task column's markup and view model: id, title, status, priority, type, resolution, creator and times, phase runs, git block — the information `_detail.html` already shows, laid out for a narrow column instead of a 22rem aside.
- Clickable items, each appending one column to the chain:
  - the task's **own description** (appends an `f`-style column that shows the description in the working area — the parent's AC calls for the description to be openable like a file);
  - every attached file (`*.phase` excluded, as in the detail view, `internal/task/view.go:173-178`);
  - every subtask;
  - the parent, every blocker and every relation target — uniformly, the same way a subtask opens. There is no separate "cross-task" mode: any task link appends a task column (decided 2026-10-07).
- The selected item is marked, so a column shows which of its children the column to its right is.
- An "Open workspace" control on the board's `#panel` that enters the chain at this task with its description in the working area.
- The board panel's own file and subtask links stop navigating to a separate page and enter the workspace instead. `_detail.html` is shared by the panel and (today) the full page, so this lands together with the tiler's `/tasks/{id}` migration.

## Acceptance criteria

- Clicking a file, a subtask, the parent, a blocker, a relation target or the description appends exactly one column and pushes exactly one URL; the previous column stays visible and marks the clicked item.
- Opening a task column for a task already in the chain still appends (the chain is a history, not a set) — or is explicitly collapsed to that prefix; whichever the design picks, it is tested and the rail stays correct.
- `*.phase` files are not offered anywhere in the column, matching the detail view.
- Files appear in a stable order (`ListFiles` is `os.ReadDir` order, i.e. lexical — `internal/storage/files.go:111-131`).
- An archived task renders its column read-only with the archived banner, and its files are still listed (archiving moves them along, `internal/storage/files.go:136-144`). A task archived or deleted while its column is open shows the archived / "gone" state.
- Every user-controlled string stays escaped: `TestEscaping` (`internal/web/handlers_test.go:277-306`) extended to the new column and still green, payloads in title, description, file name and branch.
- A subtask column and a top-level task column are the same kind with the same markup — nothing special-cases "subtask" (the parent's old two-level model is gone).
- Tests: the column fragment for a parent, for a subtask, for an archived task and for a task with no files; link targets are the appended chain URLs; the selected-item marking; `*.phase` absent; "Open workspace" from the panel.

## Out of scope

- The markdown rendering and the working-area markup (`workspace-file-column`).
- Chain parsing, layout, widths and the rail (`workspace-tiler`).
- Polling (`workspace-live`).