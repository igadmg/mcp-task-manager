# MCP Task Manager

A Go-based MCP (Model Context Protocol) server for task management, designed for Claude and coding agents.

## Overview

MCP Task Manager provides a simple but powerful task management system that integrates with AI coding assistants via the Model Context Protocol. Tasks are stored as human-readable Markdown files with YAML frontmatter in a per-task directory, making them easy to version control and inspect.

### Features

- **Markdown-based storage** - Each task lives in its own directory (`tasks/{id}/{id}.md`) with YAML frontmatter
- **Attached files** - `write_task_file`, `read_task_file`, `list_task_files` let an agent attach free-form notes, research, or design docs to a task; they move and are removed together with the task on archive/delete
- **Priority-based workflow** - Critical > High > Medium > Low, with oldest-first tiebreaker
- **Agent-friendly tools** - `get_next_task`, `start_task`, `complete_task` for automated workflows
- **Self-healing index** - JSON index cache rebuilds automatically from source files
- **Configurable task types** - Default: `feature`, `bug`; extensible via config

## Installation

### Prerequisites

- Go 1.21 or later

### Install with Go

```bash
go install github.com/gpayer/mcp-task-manager/cmd/mcp-task-manager@latest
```

### Download Pre-built Binaries

Download the latest release for your platform from the [Releases page](https://github.com/gpayer/mcp-task-manager/releases):

- `mcp-task-manager-linux-amd64` - Linux (x86_64)
- `mcp-task-manager-linux-arm64` - Linux (ARM64)
- `mcp-task-manager-windows-amd64.exe` - Windows (x86_64)

After downloading, rename and make executable (Linux/macOS):

```bash
mv mcp-task-manager-linux-amd64 mcp-task-manager
chmod +x mcp-task-manager
# Optionally move to a directory in your PATH:
sudo mv mcp-task-manager /usr/local/bin/
```

### Build from Source

```bash
git clone https://github.com/gpayer/mcp-task-manager.git
cd mcp-task-manager
go build -o mcp-task-manager ./cmd/mcp-task-manager
```

## Usage

### Running the Server

The MCP server communicates via stdio:

```bash
mcp-task-manager
```

Task storage is resolved relative to the process's current working directory (`./tasks`, or the path in `MCP_TASKS_DIR` — see [Environment Variables](#environment-variables)). An MCP client normally launches the server with its working directory set to the project it's operating on, so each project gets its own `tasks/` directory automatically — no per-project server instance or config needed.

#### Running a Locally Built Binary Against a Project

To try a locally built binary against a real project before installing it system-wide:

```bash
# Build once, from the mcp-task-manager repo
go build -o mcp-task-manager ./cmd/mcp-task-manager

# Run it from inside the target project (stdio mode, for an MCP client)
cd /path/to/your-project
/path/to/mcp-task-manager/mcp-task-manager

# Or exercise it directly via the CLI first
cd /path/to/your-project
/path/to/mcp-task-manager/mcp-task-manager create "First task"
/path/to/mcp-task-manager/mcp-task-manager list
```

This creates `tasks/` inside `your-project`. Point your MCP client (see the integration sections below, including [VS Code Integration](#vs-code-integration)) at the absolute path of the binary you just built instead of a system-installed one to test local changes.

### CLI Usage

The same binary also works as a standalone CLI tool when called with arguments:

```bash
# List all tasks
mcp-task-manager list
mcp-task-manager list --status=todo --priority=high
mcp-task-manager list --json

# Get task details
mcp-task-manager get 1
mcp-task-manager get 1 --json

# Create a task
mcp-task-manager create "Fix login bug" -p high -t bug -d "Users can't log in"

# Update a task
mcp-task-manager update 1 --title "New title" -s in_progress

# Delete a task
mcp-task-manager delete 1

# Workflow commands
mcp-task-manager next              # Get highest priority todo task
mcp-task-manager start 1           # Start a task (todo -> in_progress)
mcp-task-manager complete 1        # Complete a task (in_progress -> done)
mcp-task-manager archive 1         # Archive a completed task

# Attached files
mcp-task-manager write-task-file 1 notes.md "some research notes"
mcp-task-manager read-task-file 1 notes.md
mcp-task-manager list-task-files 1

# Other
mcp-task-manager version
mcp-task-manager --help
```

#### CLI Commands

| Command | Description |
|---------|-------------|
| `list` | List tasks with optional filters (`-s status`, `-p priority`, `-t type`, where allowed task types depend on config and default to `feature`, `bug`) |
| `get <id>` | Get task details by ID |
| `create <title>` | Create task (defaults: priority=`medium`, type=first configured task type; with default config that is `feature`; allowed task types depend on config and default to `feature`, `bug`); use `--parent` for subtasks, `--id` for a caller-supplied custom task id |
| `update <id>` | Update task fields, including `type` (allowed task types depend on config and default to `feature`, `bug`) |
| `delete <id>` | Delete a task |
| `next` | Get highest priority todo task |
| `start <id>` | Move task to in_progress |
| `complete <id>` | Move task to done |
| `archive <id>` | Archive a completed task (moves its directory, including attached files, to `tasks/archive/`) |
| `write-task-file <task-id> <filename> <content>` | Create or overwrite a file attached to a task (rejected for archived tasks) |
| `read-task-file <task-id> <filename>` | Print the content of a file attached to a task |
| `list-task-files <task-id>` | Print the names of all files attached to a task, one per line |
| `version` | Show version |

All commands except `write-task-file`/`read-task-file`/`list-task-files` support `--json` / `-j` for JSON output (file content and filename lists have no distinct JSON shape worth adding).

### Claude Desktop Integration

Add to your Claude Desktop configuration (`~/.config/claude/claude_desktop_config.json` on Linux, `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "task-manager": {
      "command": "/path/to/mcp-task-manager"
    }
  }
}
```

### Claude Code Integration

Use this path for Claude Code specifically. The Claude plugin and the Codex plugin are packaged differently, so the Claude flow still uses an explicit `claude mcp add` step.

**Setup:**

1. Install the `mcp-task-manager` binary so Claude can launch it:

```bash
go install github.com/gpayer/mcp-task-manager/cmd/mcp-task-manager@latest
```

2. Add the MCP server in Claude Code:

```bash
claude mcp add --transport stdio task-manager -- mcp-task-manager
```

3. Install the Claude Code plugin:

```bash
# Add the marketplace
/plugin marketplace add gpayer/mcp-task-manager

# Install the plugin
/plugin install mcp-task-manager@mcp-task-manager
```

**Usage:**

Use the Claude Code command `/mcp-task-manager:superpowers-workflow` to automatically execute pending tasks. The workflow spawns ordinary subagents and includes the complete planner, coder, and reviewer role contracts in the relevant subagent initial prompts.

### Codex Integration

Use this path for Codex specifically. This repository now acts as a Codex marketplace root: the marketplace catalog lives in `.agents/plugins/marketplace.json`, and the installable Codex plugin package is `plugins/mcp-task-manager/`.

**Prerequisite: install the MCP server binary first**

The Codex plugin package includes `superpowers-workflow`, `/execute-all`, and a packaged `.mcp.json`, but it still expects the `mcp-task-manager` executable to already be available on your `PATH`:

```bash
go install github.com/gpayer/mcp-task-manager/cmd/mcp-task-manager@latest
```

**Add this marketplace and install the plugin**

```bash
codex plugin marketplace add https://github.com/gpayer/mcp-task-manager
```

Inside Codex, install the packaged plugin from that marketplace:

```text
/plugin install mcp-task-manager@mcp-task-manager
```

The plugin package wires in the MCP server definition from `plugins/mcp-task-manager/.mcp.json`, so you do not need a separate `codex mcp add` step as long as `mcp-task-manager` is already installed and resolvable by name.

**Usage**

Use the Codex skill `$superpowers-workflow` or the packaged command `/execute-all` to automatically execute pending tasks. The workflow spawns ordinary subagents and includes the complete planner, coder, and reviewer role contracts in the relevant subagent initial prompts.

The model/reasoning settings are capability-based recommendations. The workflow applies them only when the active subagent tool supports those controls and they are not overridden by user choice, model availability, policy, cost/latency constraints, or task-specific needs.

### VS Code Integration

VS Code (with GitHub Copilot Chat's agent mode) can launch MCP servers per-workspace, which is a natural fit for project-local task storage: each project gets its own `tasks/` directory automatically, without editing global config.

**Setup:**

1. Install the binary, or build it locally (see [Build from Source](#build-from-source)):

```bash
go install github.com/gpayer/mcp-task-manager/cmd/mcp-task-manager@latest
```

2. In your project's root, create `.vscode/mcp.json`:

```json
{
  "servers": {
    "task-manager": {
      "type": "stdio",
      "command": "mcp-task-manager"
    }
  }
}
```

Use an absolute path in `command` if the binary isn't on your `PATH` (e.g. a locally built one — see [Running a Locally Built Binary Against a Project](#running-a-locally-built-binary-against-a-project)).

Alternatively, run **MCP: Add Server** from the Command Palette (`Cmd/Ctrl+Shift+P`), choose **Command (stdio)**, point it at the `mcp-task-manager` binary, and select **Workspace Settings** to have VS Code write this file for you.

3. Reload the window, or run **MCP: List Servers** from the Command Palette and start `task-manager` from there.

**Usage:**

VS Code launches the server with the workspace root as its working directory, so tasks are stored in `<workspace>/tasks`. Open Copilot Chat, switch to **Agent** mode, and the task-manager tools (`create_task`, `get_next_task`, etc.) become available to the agent — check the tools list (🛠️) in the chat input to confirm they're enabled.

To make the server available across every workspace instead of configuring it per project, use **MCP: Add Server** and choose **User Settings** (previously **Global**) instead of **Workspace Settings**. VS Code still launches the server with the current workspace as its working directory, so task storage stays project-local unless you pin it elsewhere with `MCP_TASKS_DIR`.

## MCP Tools

### Task Management

| Tool | Description |
|------|-------------|
| `create_task` | Create a new task with title, description, priority, `type`, optional `parent_id` for subtasks, and optional `id` for a caller-supplied custom task id (used verbatim as the id and storage directory name instead of the next auto-increment id). Allowed task `type` values come from config and default to `feature`, `bug`. |
| `update_task` | Modify task fields (title, description, status, priority, `type`). Allowed task `type` values come from config and default to `feature`, `bug`. |
| `list_tasks` | List tasks with optional filters (status, priority, `type`); use `parent_id` filter for subtasks. Allowed task `type` values come from config and default to `feature`, `bug`. |
| `get_task` | Get full details of a task by ID (includes subtasks for parent tasks) |
| `delete_task` | Remove a task; use `delete_subtasks` to cascade |
| `archive_task` | Archive a completed task (moves its directory, including attached files, to `tasks/archive/`) |

### Attached Files

| Tool | Description |
|------|-------------|
| `write_task_file` | Create or overwrite a named text file attached to a task (rejected for archived tasks) |
| `read_task_file` | Read the content of a named file attached to a task (active or archived) |
| `list_task_files` | List the names of all files attached to a task (active or archived) |

### Agent Workflow

| Tool | Description |
|------|-------------|
| `get_next_task` | Returns highest priority `todo` task |
| `start_task` | Move task from `todo` to `in_progress` |
| `complete_task` | Move task from `in_progress` to `done` |

### Relations

| Tool | Description |
|------|-------------|
| `add_relation` | Add a relation between two tasks. Allowed relation `type` values come from config and default to `blocked_by`, `relates_to`, `duplicate_of`. |
| `remove_relation` | Remove a relation between two tasks. Allowed relation `type` values come from config and default to `blocked_by`, `relates_to`, `duplicate_of`. |

## Configuration

### Config File

Create `mcp-tasks.yaml` in the working directory:

```yaml
task_types:
  - feature
  - bug
  - chore
  - docs
relation_types:
  - blocked_by
  - relates_to
  - duplicate_of
```

The `task_types` list defines the allowed values for every task `type` field in the CLI, MCP tools, and task frontmatter. If omitted, the default allowed values are `feature` and `bug`.
The `relation_types` list defines the allowed values for every relation `type` field in MCP tools and task metadata. If omitted, the default allowed values are `blocked_by`, `relates_to`, and `duplicate_of`.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_TASKS_DIR` | Directory for task storage | `./tasks` |

## Task Format

Each task is stored at `tasks/{id}/{id}.md` (e.g. `tasks/7/7.md`, unpadded) as a Markdown file with YAML frontmatter. Any files attached via `write_task_file` / `write-task-file` live alongside it in the same `tasks/{id}/` directory.

```yaml
---
id: 1
title: "Implement user authentication"
status: todo
priority: high
type: feature
created_at: 2025-01-15T10:30:00Z
updated_at: 2025-01-15T10:30:00Z
---

Detailed description in Markdown format.

- Acceptance criteria
- Implementation notes
- Links and references
```

Ids are strings on the wire (JSON responses and `--json` CLI output render `"id": "1"`, not a bare number); a bare YAML scalar like `id: 1` above still parses fine into that string field. By default `create_task` allocates the next auto-incrementing numeric-looking id, unpadded (`"8"`, not `"008"`). Optionally, pass `id` (MCP) or `--id` (CLI `create`) to use a caller-supplied custom text id instead — it's validated like an attached filename (non-empty, no `/` or `\`, not `..`; `"0"`, `"archive"`, and `".index.json"` are reserved) and rejected if it collides with an existing active or archived task.

The `type` field must be one of the configured `task_types` values. With the default configuration, allowed values are `feature` and `bug`.

### Status Values

- `todo` - Task is pending
- `in_progress` - Task is actively being worked on
- `done` - Task is completed

### Priority Levels

- `critical` - Highest priority
- `high` - Important tasks
- `medium` - Normal priority (default)
- `low` - Can wait

### Subtasks

Tasks support single-level nesting via the `parent_id` field.

**Creating subtasks:**
```bash
# CLI
mcp-task-manager create "Implement login form" -p high --parent 1

# MCP tool
create_task with parent_id parameter
```

**Automatic behaviors:**
- Starting a subtask auto-starts its parent task
- Completing the last subtask auto-completes the parent
- Parent tasks cannot be completed while subtasks remain incomplete
- `get_next_task` returns subtasks instead of parents with incomplete subtasks

## Project Structure

```
mcp-task-manager/
├── cmd/mcp-task-manager/    # Entry point (MCP server + CLI)
├── internal/
│   ├── cli/                 # CLI command handlers
│   ├── config/              # Configuration loading
│   ├── storage/             # Markdown + index storage
│   ├── task/                # Task model and service
│   └── tools/               # MCP tool handlers
├── tasks/                   # Task storage (created at runtime); tasks/{id}/{id}.md plus attached files
├── mcp-tasks.yaml           # Configuration file
└── CLAUDE.md                # AI assistant instructions
```

## License

MIT License - see LICENSE file for details.
