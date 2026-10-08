---
id: web-backlog-graph
title: 'Web: whole-backlog relations graph in the workspace viewer'
status: todo
priority: high
type: feature
relations:
  - type: blocked_by
    task: web-task-workspace
created_at: "2026-10-07T19:15:45Z"
created_by: igor.cwer
updated_at: "2026-10-07T19:16:24Z"
---

A graph of the whole active backlog, shown in the large viewer area of the task workspace (`web-task-workspace`). Nodes are every task in the active index, with done tasks dimmed. Edges are parent → subtask plus every configured relation type.

There are two ways to open it. A "Graph" item in a task's workspace column opens it with that task highlighted. A button in the board header opens it with no highlight. Clicking a node opens that task in the workspace. It is read-only, offline and embedded, like the rest of the dashboard.

Full input, clarifications and acceptance criteria are in the attached `task.md`. Blocked by `web-task-workspace`, which provides the viewer area.