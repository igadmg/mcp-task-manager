---
id: git-branch-per-task
title: 'Git branch per task: wip branch on start, squashed branch on complete'
status: in_progress
priority: high
type: feature
created_at: "2026-09-29T18:30:18Z"
updated_at: "2026-09-29T18:30:37Z"
---

start_task creates `<git.user>/wip/<short-name>` and switches to it; complete_task squashes the wip work (soft reset to the branch start, keeping all changes) into a single commit on `<git.user>/<short-name>`. All git logic lives in MCP server functions, not in skills. No data loss. See attached `task` file.