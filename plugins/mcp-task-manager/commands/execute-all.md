---
description: Execute all pending tasks using superpowers workflow
---

Use the packaged `superpowers-workflow` skill from the `mcp-task-manager` Codex plugin to execute all pending tasks from the task manager MCP server.

Expected setup:
- Install the plugin from this marketplace with `/plugin install mcp-task-manager@mcp-task-manager`
- Make sure the `mcp-task-manager` binary is installed so the packaged `.mcp.json` can launch the `task-manager` server

The workflow will:
1. Get the next todo task from the task manager
2. If it's a parent task without subtasks: dispatch an ordinary subagent with a complete planner prompt to create implementation subtasks
3. Execute each implementation subtask by dispatching an ordinary subagent with a complete coder prompt
4. Review coding tasks, and other tasks when required, by dispatching an ordinary subagent with a complete reviewer prompt
5. Continue until all tasks are complete

Model and reasoning choices are capability-based recommendations. The workflow uses them only when the active `spawn_agent` schema supports them and user or runtime constraints have not overridden them.
