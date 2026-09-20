# MCP Task Manager

A Go-based MCP (Model Context Protocol) server for task management, designed for Claude and coding agents.

## Overview

MCP Task Manager provides a simple but powerful task management system that integrates with AI coding assistants via the Model Context Protocol. Tasks are stored as human-readable Markdown files with YAML frontmatter in a per-task directory, making them easy to version control and inspect.

### Features

- **Markdown-based storage** - Each task lives in its own directory (`tasks/{id}/{id}.md`) with YAML frontmatter
- **Attached files** - `write_task_file`, `read_task_file`, `list_task_files` let an agent attach free-form notes, research, or design docs to a task; they move and are removed together with the task on archive/delete
- **Priority-based workflow** - Critical > High > Medium > Low, with oldest-first tiebreaker
- **Agent-friendly tools** - `get_next_task`, `start_task`, `complete_task` for automated workflows
- **Self-healing index** - in-memory index rebuilds automatically from the task files
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

Task storage is resolved from the **project root**, not from the server's working directory. The root is looked up in this order:

1. `MCP_TASKS_DIR`, if it holds an absolute path — that path is the tasks directory, full stop.
2. `MCP_PROJECT_DIR` — an explicit project root.
3. `CLAUDE_PROJECT_DIR` — exported by Claude Code into every MCP server it spawns.
4. **MCP roots** (`roots/list`) — the protocol's own mechanism; works with any client that declares the `roots` capability. With several roots, one that already holds a project (an `mcp-tasks.yaml`, `.tasks/` or `tasks/`) wins over one that merely exists.
5. A search upwards from the working directory for `mcp-tasks.yaml`, `.tasks/` or `tasks/`. This is what CLI invocations use.

Inside the root, the tasks directory is `MCP_TASKS_DIR` (relative values are resolved against the root), else `tasks_dir` from `mcp-tasks.yaml`, else `.tasks` — falling back to a legacy `tasks/` directory when that is the one actually holding tasks.

Because roots only become available after `initialize`, the project is resolved on the first tool call rather than at startup. `mcp-task-manager version` prints the resolution, which is the quickest way to check where a given invocation would read and write.

Note that a client which launches the server with a working directory *other* than the project (for example `go run -C <dir>`) is fine: steps 2–4 do not depend on the working directory at all.

#### Web Dashboard

The same binary also serves a read-only kanban dashboard of the backlog:

```bash
mcp-task-manager serve web --addr 127.0.0.1:7777
# then open http://127.0.0.1:7777/
```

One process, two transports: the dashboard and the MCP tools share a single
resolved project and a single task service, so a task created through a tool
call shows up on the board on the next refresh (every 5 seconds).

- From an MCP client, set `web.enabled` in `mcp-tasks.yaml` (or `MCP_WEB_ENABLED=1`) to bring the board up with the server, or call the `start_web_ui` tool to start it on demand.
- `serve web` resolves the project eagerly, so the board has data from the very first request. Started from inside the MCP server it comes up before the client has named a project and shows a placeholder until the first tool call.
- `serve web --mcp` additionally serves MCP over stdio in the same process. It is off by default: a terminal has a TTY on stdin, and a JSON-RPC reader there would eat your keystrokes.
- Tailwind CSS and htmx are compiled into the binary, so the page renders with no network access.

**The HTTP surface is read-only and unauthenticated.** No route mutates a task,
but anyone who can reach the port can read the whole backlog. The default
address is loopback-only; binding anywhere else is your explicit choice.

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

# Web dashboard (foreground, Ctrl-C to stop)
mcp-task-manager serve web --addr 127.0.0.1:7777

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

Use this path for Claude Code specifically. The Claude plugin now bundles its own `.mcp.json` (repo root), so installing the plugin also wires up the `task-manager` MCP server — no separate `claude mcp add` step needed.

**Setup:**

1. Install the Claude Code plugin:

```bash
# Add the marketplace
/plugin marketplace add gpayer/mcp-task-manager

# Install the plugin
/plugin install mcp-task-manager@mcp-task-manager
```

That's it — the bundled `plugins/mcp-task-manager/.mcp.json` launches `mcp-task-manager`, so the plugin needs the binary on your `PATH` (`go install github.com/gpayer/mcp-task-manager/cmd/mcp-task-manager@latest`). An installed plugin directory is not a Go module, so `go run` cannot be used there.

**Working on this repository itself** is the other case, and it needs a different command. The repo root carries a *project-scoped* `.mcp.json`, which Claude Code launches with the repository as the working directory:

```json
{
  "mcpServers": {
    "task-manager": {
      "command": "go",
      "args": ["run", "./cmd/mcp-task-manager"],
      "env": { "GOWORK": "off" }
    }
  }
}
```

This always runs the checked-out source, with no build step between debug runs. Do not use `${CLAUDE_PLUGIN_ROOT}` there: it expands to nothing for a project-scoped server.

**Usage:**

- Use `/mcp-task-manager:begin-task` (backed by the packaged `begin_task` skill) to turn a ticket into a task-manager task and drive it through research → design → planning → implementation, with an approval gate between each phase.
- Use `/mcp-task-manager:execute-all` (backed by the packaged `superpowers-workflow` skill) to instead loop over the existing task-manager backlog, spawning planner/coder/reviewer subagents per task.

### Codex Integration

Use this path for Codex specifically. This repository now acts as a Codex marketplace root: the marketplace catalog lives in `.agents/plugins/marketplace.json`, and the installable Codex plugin package is `plugins/mcp-task-manager/`.

**Prerequisite: the Go toolchain**

The Codex plugin package includes `begin_task`, `superpowers-workflow`, the `/begin-task` and `/execute-all` commands, and a packaged `.mcp.json`. The `.mcp.json` launches the server with `go run -C ${CLAUDE_PLUGIN_ROOT}/../.. ./cmd/mcp-task-manager`, so it runs the checked-out source directly — you need the Go toolchain available, but not a pre-installed `mcp-task-manager` binary.

**Add this marketplace and install the plugin**

```bash
codex plugin marketplace add https://github.com/gpayer/mcp-task-manager
```

Inside Codex, install the packaged plugin from that marketplace:

```text
/plugin install mcp-task-manager@mcp-task-manager
```

The plugin package wires in the MCP server definition from `plugins/mcp-task-manager/.mcp.json`, so you do not need a separate `codex mcp add` step.

**Usage**

- Use the Codex skill `$begin_task` or the packaged command `/begin-task` to turn a ticket into a task-manager task and drive it through research → design → planning → implementation, with an approval gate between each phase.
- Use the Codex skill `$superpowers-workflow` or the packaged command `/execute-all` to instead loop over the existing task-manager backlog, spawning planner/coder/reviewer subagents per task.

The model/reasoning settings for `superpowers-workflow` are capability-based recommendations. The workflow applies them only when the active subagent tool supports those controls and they are not overridden by user choice, model availability, policy, cost/latency constraints, or task-specific needs.

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

### Web Dashboard

| Tool | Description |
|------|-------------|
| `start_web_ui` | Start the read-only kanban dashboard and return its URL. Idempotent: a second call reports the running URL instead of binding another port. Optional `addr` (`host:port`) |

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
web:
  enabled: false          # start the dashboard alongside the MCP server
  addr: 127.0.0.1:7777    # listen address
  with_mcp: false         # `serve web` also serves MCP over stdio
```

A partially written section keeps the defaults for the keys it does not
mention, so `web: {enabled: true}` still listens on `127.0.0.1:7777` and
`auto_archive: {enabled: true}` still waits 30 days.

The `task_types` list defines the allowed values for every task `type` field in the CLI, MCP tools, and task frontmatter. If omitted, the default allowed values are `feature` and `bug`.
The `relation_types` list defines the allowed values for every relation `type` field in MCP tools and task metadata. If omitted, the default allowed values are `blocked_by`, `relates_to`, and `duplicate_of`.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_TASKS_DIR` | Tasks directory. Absolute paths are used as-is; relative ones are resolved against the project root | `.tasks` under the project root |
| `MCP_PROJECT_DIR` | Explicit project root, overriding every other source | unset |
| `CLAUDE_PROJECT_DIR` | Project root; set by Claude Code itself | unset |
| `MCP_ROOT_SOURCE` | Restricts resolution to a single source: `MCP_PROJECT_DIR`, `CLAUDE_PROJECT_DIR`, `roots` or `cwd`. Useful for testing the roots path, which the environment variables would otherwise always win | unset |
| `MCP_WEB_ENABLED` | Starts the web dashboard with the MCP server. Parsed as a bool; an unparseable value is ignored | `false` |
| `MCP_WEB_ADDR` | Dashboard listen address, `host:port`. Never enables the dashboard on its own | `127.0.0.1:7777` |

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

Ids are strings on the wire (JSON responses and `--json` CLI output render `"id": "1"`, not a bare number); a bare YAML scalar like `id: 1` above still parses fine into that string field. By default `create_task` allocates the next auto-incrementing numeric-looking id, unpadded (`"8"`, not `"008"`). Optionally, pass `id` (MCP) or `--id` (CLI `create`) to use a caller-supplied custom text id instead — it's validated like an attached filename (non-empty, no `/` or `\`, not `..`; `"0"`, `"archive"`, and `".index.json"` are reserved, the last being a retired cache filename) and rejected if it collides with an existing active or archived task.

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
├── cmd/mcp-task-manager/    # Entry point: mode selection and signal handling
├── internal/
│   ├── app/                 # Composition root: MCP + web in one process
│   ├── cli/                 # CLI command handlers
│   ├── config/              # Configuration loading
│   ├── project/             # Project resolution and construction
│   ├── storage/             # Markdown storage + in-memory index
│   ├── task/                # Task model, service and view API
│   ├── testsupport/         # Shared test backlog helpers
│   ├── tools/               # MCP tool handlers
│   └── web/                 # Read-only kanban dashboard (htmx + Tailwind)
├── scripts/build-css.sh     # Maintainer step: rebuild the vendored CSS
├── tasks/                   # Task storage (created at runtime); tasks/{id}/{id}.md plus attached files
├── mcp-tasks.yaml           # Configuration file
└── CLAUDE.md                # AI assistant instructions
```

### Refreshing the Vendored CSS

`internal/web/static/app.css` and `internal/web/static/htmx.min.js` are
committed and compiled into the binary, so a clean checkout builds and tests
with no network access. Only a maintainer touching
`internal/web/templates/` needs to regenerate the CSS:

```bash
bash scripts/build-css.sh
```

The script downloads the pinned Tailwind standalone CLI (**v4.3.3**, no Node
required) into the gitignored `.cache/` directory and runs it over
`internal/web/assets/input.css`. It is never invoked by `go build`,
`go generate` or `go test`. htmx is pinned at **2.0.4**.

## License

MIT License - see LICENSE file for details.
