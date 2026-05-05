---
name: install-agents
description: Use when installing or upgrading the mcp-task-manager Codex plugin and the planner, coder, or reviewer agents must be made globally visible to Codex sessions.
---

# Install Agents

Codex discovers reusable subagents from `~/.codex/agents/` or project-local `.codex/agents/`. The `mcp-task-manager` plugin packages `planner`, `coder`, and `reviewer` definitions, but installing the plugin does not automatically register them globally.

Run the installer script from the newest installed plugin version:

```bash
plugin_root=$(ls -dt ~/.codex/plugins/cache/mcp-task-manager/mcp-task-manager/* | head -n 1)
bash "$plugin_root/scripts/install-codex-agents.sh"
```

After the script finishes, restart Codex so the global agents are discovered in every session.
