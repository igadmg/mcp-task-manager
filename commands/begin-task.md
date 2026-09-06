---
description: Formalize a new ticket and drive it through research, design, planning, and implementation
---

Use the packaged `begin_task` skill from the `mcp-task-manager` plugin to turn a feature request, bug report, or ticket description into a task tracked by the task manager MCP server, then drive it through all four delivery phases with an explicit approval gate between each one.

Expected setup:
- Install the plugin from this marketplace with `/plugin install mcp-task-manager@mcp-task-manager`
- Make sure the packaged `.mcp.json` can launch the `task-manager` server

The workflow will:
1. Turn the input into a task on the task manager MCP server and record its id in `.current_task`
2. Dispatch a sub-agent with the `research` skill and save its output to the task
3. Wait for approval, then dispatch a sub-agent with the `design` skill and save its output to the task
4. Wait for approval, then dispatch a sub-agent with the `planning` skill and save its output to the task
5. Wait for approval, then dispatch a sub-agent with the `implementation` skill and report the final status

Each phase requires an explicit "yes" before the next one starts. This is a separate, simpler entry point than `/execute-all`: it drives one ticket through the full pipeline end-to-end, rather than looping over the existing task-manager backlog with planner/coder/reviewer subagents.
