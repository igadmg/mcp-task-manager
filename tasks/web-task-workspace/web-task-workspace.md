---
id: web-task-workspace
title: 'Web: task workspace — board slides away, task column + large file viewer'
status: in_progress
priority: high
type: feature
created_at: "2026-10-07T19:15:45Z"
created_by: igor.cwer
updated_at: "2026-10-07T20:45:42Z"
branch: igor.cwer/wip/web-task-workspace
base_branch: main_patched
start_commit: 8b16e42cd939db0eb155b3afed9f6164ceb0b5af
---

Today the dashboard shows a task only in the 22rem `#panel` on the right of the board (`internal/web/templates/board.html`). Attached files appear there as name chips that cannot be opened, and subtasks are plain links to the full `/tasks/{id}` page. A task's `task` / `research` / `design` / `plan` files cannot be read in the UI at all.

Add a task workspace. Opening a file or a subtask from the task panel slides the board columns out to the left. The task column takes the leftmost slot, and the rest of the width becomes a large viewer that shows the file rendered as markdown. A subtask opens as a second narrow column next to its parent. A Back control steps back to the previous layout. The strip moves only in discrete steps, never by free scrolling. The open column(s) and file refresh by polling, like the board.

Full input, clarifications and acceptance criteria are in the attached `task.md`. The backlog relations graph that will also open in the viewer is a separate task, `web-backlog-graph`.