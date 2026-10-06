# MCP Task Manager

A Go-based MCP (Model Context Protocol) server for task management, designed for Claude and coding agents.

## Overview

MCP Task Manager provides a simple but powerful task management system that integrates with AI coding assistants via the Model Context Protocol. Tasks are stored as human-readable Markdown files with YAML frontmatter in a per-task directory, making them easy to version control and inspect.

### Features

- **Markdown-based storage** - Each task lives in its own directory (`tasks/{id}/{id}.md`) with YAML frontmatter
- **Attached files** - `write_task_file`, `read_task_file`, `list_task_files` let an agent attach free-form notes, research, or design docs to a task; they move and are removed together with the task on archive/delete
- **Priority-based workflow** - Critical > High > Medium > Low, with oldest-first tiebreaker
- **Agent-friendly tools** - `get_next_task`, `start_task`, `complete_task`, `get_current_task` for automated workflows
- **Delivery phase records** - `start_phase` / `finish_phase` record every run of research, design, planning and implementation (who, when, tokens) in server-owned `<phase>.phase` files; tasks record who created them (`created_by`)
- **Git branch per task (opt-in)** - `start_task` (or `start_phase implementation`) creates and checks out a wip branch, `complete_task` squashes it into one commit on a final branch; see [Git Branch-per-Task](#git-branch-per-task)
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
- In progress groups its cards into Research / Design / Planning / Implementation lanes, each shifted right by half a card. A task's lane is the phase of its latest started run in its `<phase>.phase` records; a task without records falls back to the workflow files (`research`, `design`, `plan`) attached to it.
- Done shows statistics instead of task cards: per configured field (by default `priority`, `type`, `resolution`) one stacked bar per value with the done / in progress / todo split, the closures of the last 24 h highlighted, and a label like `7/12 · 5 open · +2`. A done task stays reachable at `/tasks/{id}`. See `web.done_stats` under Configuration.
- Every card shows who created the task and when; in-progress cards with phase records also show the current phase, who started it and when, and the tokens spent so far. The detail view lists every phase run.

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
mcp-task-manager start-phase 1 research              # Start a delivery phase (todo -> in_progress)
mcp-task-manager finish-phase 1 research --tokens 81234 --note "first pass"
mcp-task-manager complete 1        # Complete a task (in_progress -> done)
mcp-task-manager complete 1 -m "feat: login form"   # ...with the squash commit message (git branching)
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
| `get <id>` | Get task details by ID, including `Created by:` and the phase-run history |
| `create <title>` | Create task (defaults: priority=`medium`, type=first configured task type; with default config that is `feature`; allowed task types depend on config and default to `feature`, `bug`); use `--parent` for subtasks, `--id` for a caller-supplied custom task id |
| `update <id>` | Update task fields, including `type` (allowed task types depend on config and default to `feature`, `bug`) |
| `delete <id>` | Delete a task |
| `next` | Get highest priority todo task |
| `start <id>` | Move task to in_progress (under git branching: create or restore its wip branch and check it out) |
| `start-phase <id> <phase>` | Start a run of a delivery phase (`research`, `design`, `planning`, `implementation`); a todo task moves to in_progress, and `implementation` under git branching cuts or restores the wip branch |
| `finish-phase <id> <phase>` | Finish the open run of a phase; `--tokens N` records what it cost, `--note` a one-line note |
| `complete <id>` | Move task to done; `--resolution`/`--note` close it without delivering, `-m`/`--message` sets the squash commit message under git branching |
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

Use this path for Claude Code specifically. The plugin package (`plugins/mcp-task-manager/`) bundles its own `.mcp.json`, so installing the plugin also wires up the `task-manager` MCP server — no separate `claude mcp add` step needed.

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

See [Agent Workflows](#agent-workflows) for what each one does.

### Codex Integration

Use this path for Codex specifically. This repository now acts as a Codex marketplace root: the marketplace catalog lives in `.agents/plugins/marketplace.json`, and the installable Codex plugin package is `plugins/mcp-task-manager/`.

**Prerequisite: the `mcp-task-manager` binary**

The Codex plugin package is the same one Claude Code installs: all the skills listed under [Agent Workflows](#agent-workflows), the `/begin-task` and `/execute-all` commands, and a packaged `.mcp.json`. That `.mcp.json` launches `mcp-task-manager`, so the binary must be on your `PATH` (`go install github.com/gpayer/mcp-task-manager/cmd/mcp-task-manager@latest`).

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

### Agent Workflows

The plugin ships two ways to drive work through the task manager. Both keep
all state in the task manager, so a workflow can be interrupted and resumed in
a new session.

| | `begin-task` | `execute-all` |
|---|---|---|
| Input | One new ticket, feature request or bug report | The existing backlog |
| Driven by | `begin_task` skill | `superpowers-workflow` skill |
| Phases | research → design → planning → implementation | planning → coding → review, per task |
| Human approval | Required between every phase | Only when blocked |
| Artifacts | Files attached to the task: `task`, `research`, `design`, `plan`, `implementation` | Subtasks created by the planner |

**`begin-task`: one ticket, phase by phase**

1. **Intake.** The agent asks only the questions it needs, picks a kebab-case
   id (e.g. `add-tooltip-control`) and creates the task, which stays in
   `todo`. The agent writes the `task` file: the original input,
   clarifications, a one-sentence problem statement and the acceptance
   criteria.
2. **Research.** `start_phase research` moves the task to `in_progress` and
   makes it your current task (`get_current_task`), so later steps never need
   its id. A subagent with the `research` skill maps the current code, with
   file and line references, and the result is saved as `research`. It
   gathers facts only and proposes no changes.
3. **Design.** A subagent with the `design` skill turns the research into an
   architecture proposal, saved as `design`.
4. **Planning.** A subagent with the `planning` skill splits the design into
   commit-sized phases, each one testable on its own. The result is saved as
   `plan`.
5. **Implementation.** `start_phase implementation` creates the task's wip
   branch under git branching. A subagent with the `implementation` skill
   carries out the approved phases one at a time, running the repository's
   quality gates (format, build, tests) after each. A summary is saved as
   `implementation`, and on your "yes" the agent calls `complete_task`.

Every phase is opened with `start_phase` and closed with `finish_phase`,
which records the subagent's token total. After each phase you see a short
summary and must answer "yes" before the next one starts. If you reject an
output, the agent runs the phase again as a new run and revises it; every run
stays in the task's history. Each subagent receives the full files from the
earlier phases, not summaries.
`research`, `design`, `planning` and `implementation` also work on their own,
and the `workflow` skill enforces the same phase order when you ask for "the
whole process" without a ticket. Only `begin_task` records phases; when you
run a phase skill by hand, call `start_phase` / `finish_phase` yourself.

Start it with `/begin-task <ticket text>` from the plugin, or `/begin_task`
inside this repository, where the skills also live under `.claude/skills/`.

**Phase records.** The server keeps every run of a phase in
`<tasks_dir>/<id>/<phase>.phase`: who started it and when, when it finished,
its tokens and an optional note. The dashboard places an in-progress card in
the lane of its latest phase and shows that phase, who started it and the
tokens spent so far; the detail view lists every run. A few rules:

- Only one run per task may be open at a time. If a session dies mid-phase,
  close the run with `finish_phase` (no tokens) before starting again.
- A phase cannot start before its predecessor has a finished run. A task with
  no `.phase` file at all may start any phase its attached files prove (for
  example `implementation` when a `plan` exists), so older tasks can join.
- `*.phase` files belong to the server; `write_task_file` refuses them.
- `complete_task` closes every open run of the tasks it closes, with the note
  `closed by complete_task (<resolution>)`. `update_task` to `done` does not.

**`execute-all`: work through the backlog**

The `superpowers-workflow` skill is a controller that loops until no `todo`
task is left:

1. `get_next_task` picks the next task, by priority and blockers.
2. **A parent task without subtasks** is started, and a *planner* subagent
   breaks it into implementation subtasks.
3. **For each subtask:** the controller starts it, a *coder* subagent
   implements it, and a *reviewer* subagent checks it. Coding tasks are
   always reviewed; documentation tasks only on request. Review findings go
   back to the coder until the review passes. Then the controller completes
   the subtask, passing the coder's commit message.
4. **The controller checks the working tree** before and after every
   subagent, and stops with a report instead of reverting if it finds
   unexpected changes.

Each subagent gets a complete, self-contained role prompt. Subagents never
commit, tag or create branches; only the controller touches git.

**Commits.** With git branching off, `execute-all` commits once per completed
subtask (the code plus `tasks/`) using the coder's message, while `begin-task`
commits nothing unless you ask. With [Git Branch-per-Task](#git-branch-per-task)
on, `start_task` and `complete_task` do the git work instead:

- every started task gets its own wip branch;
- every completed subtask is squash-merged into its parent's branch;
- the agents run no git command for code;
- the controller commits only task records, wherever the tasks directory is
  versioned.

When `execute-all` finishes a parent's subtasks, it leaves the parent
`in_progress`. Complete the parent yourself to get its one-commit final branch.

**Where the skills live.** `plugins/mcp-task-manager/skills/` is the only
copy to edit. See [Editing the Packaged Skills](#editing-the-packaged-skills).

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
| `update_task` | Modify task fields (title, description, status, priority, `type`). Allowed task `type` values come from config and default to `feature`, `bug`. Under git branching, starting a task and closing a branched one are refused here: use `start_task` / `complete_task`. |
| `list_tasks` | List tasks with optional filters (status, priority, `type`); use `parent_id` filter for subtasks. Allowed task `type` values come from config and default to `feature`, `bug`. |
| `get_task` | Get full details of a task by ID (includes subtasks for parent tasks, `created_by`, and `phases`: the run history of every delivery phase) |
| `delete_task` | Remove a task; use `delete_subtasks` to cascade |
| `archive_task` | Archive a completed task (moves its directory, including attached files, to `tasks/archive/`) |

### Attached Files

| Tool | Description |
|------|-------------|
| `write_task_file` | Create or overwrite a named text file attached to a task (rejected for archived tasks, and for names ending in `.phase`, which are the server's phase records) |
| `read_task_file` | Read the content of a named file attached to a task (active or archived) |
| `list_task_files` | List the names of all files attached to a task (active or archived) |

### Agent Workflow

| Tool | Description |
|------|-------------|
| `get_next_task` | Returns highest priority `todo` task |
| `start_task` | Move task from `todo` to `in_progress` and make it your current task; under git branching, create (or restore and rebase) its wip branch and check it out |
| `complete_task` | Move task from `in_progress` to `done` and finish its open phase runs; under git branching, squash its wip branch into one commit (optional `commit_message`) and check out the result |
| `get_current_task` | Return the task your per-user current-task pointer names (`<tasks_dir>/.users/<user>/current_task`), archived tasks included |
| `start_phase` | Start a run of a delivery phase (`research`, `design`, `planning`, `implementation`) on a task (`id`, default your current task): a `todo` task moves to `in_progress` and becomes your current task. Phases run in order, one open run at a time. `implementation` under git branching creates or restores the wip branch, like `start_task`; the other phases never touch git |
| `finish_phase` | Finish the open run of a phase, recording optional `tokens` and `note`; changes no status and no git |

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
  done_stats:             # statistics cards of the Done column, in order
    cards:
      - kind: bars        # one bar per value of a task field
        field: priority
      - kind: lines       # per-day chart over the last `days` days
        title: Last 2 weeks
        days: 14
        lines: [created, closed, created_cumulative, closed_cumulative]
        hidden: [created_cumulative, closed_cumulative]  # off until toggled
      - kind: lines
        days: 30
        split_by: priority  # one line per priority...
        metric: closed      # ...counting closures
git:
  branching: false        # git branch per task, see below
  base_branches: [main_patched, master_patched, main, master]
```

A partially written section keeps the defaults for the keys it does not
mention, so `web: {enabled: true}` still listens on `127.0.0.1:7777`,
`auto_archive: {enabled: true}` still waits 30 days, and
`git: {branching: true}` still uses the default base branches.

Without `done_stats.cards` the Done column shows bars for `priority`, `type`
and `resolution` and a 14-day `lines` card with `created` and `closed`;
`cards: []` shows none. Within a card, everything but the field is optional:
a card with a `field` and no `kind` is `bars`, otherwise `lines`; `days`
defaults to 14, `lines` to `[created, closed]`, `metric` to `closed`, and a
missing `title` is derived. The optional `id` is the key the viewer's line
toggles are stored under; when omitted it is derived from the card's content
(for example `bars-priority`, `lines-14d`), so reordering cards or renaming a
title keeps the toggles.

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
| `MCP_GIT_BRANCHING` | Turns git branch-per-task on or off. Parsed as a bool; an unparseable value is ignored | `false` |

### Git Branch-per-Task

With `git.branching: true` (or `MCP_GIT_BRANCHING=true`), `start_task` (or
`start_phase implementation`) and `complete_task` manage branches in the git
repository at the project root. The server runs the system `git` itself;
agents and skills never commit or switch around these calls. Under the phase
workflow a branch is cut only when implementation starts: research, design and
planning are record-only.

- **Start.** A top-level task gets `<user>/wip/<name>`, cut from the tip of the
  first existing entry of `git.base_branches`, and HEAD moves to it. A subtask
  gets `<parent wip>--<name>`, cut from its parent's wip branch. `<user>` is
  the local part of `git config user.email`; `<name>` is the task id if it is
  readable, otherwise the id plus the slugified title. A subtask's
  `start_phase implementation` under a parent that has no branch yet (it is in
  its own earlier phases) cuts the parent's wip first.
- **Uncommitted changes.** On a task's own wip branch they are saved there in a
  checkpoint commit; on the base branch they are carried to the new branch; on
  any other branch the start is refused. Changes under the tasks directory
  never count.
- **Complete.** Everything on the wip branch, committed or not, becomes exactly
  one commit on top of the task's start commit, on `<user>/<name>`, and HEAD
  moves there. A subtask is instead squash-merged as one commit onto its
  parent's wip branch; the parent is completed on its own and is refused while
  a completed subtask's commit is missing from its branch. The wip branch keeps
  the full history.
- **Close without delivering** (`obsolete`, `wontfix`, ...) while the branch is
  checked out: the leftovers go into a safety commit on the wip branch and HEAD
  returns to the base branch (or the parent's wip).
- **Restart.** `start_task` on a task whose wip branch still exists (in
  progress, or reopened to `todo`) rebases the branch onto the current base
  with `git replay` and checks it out. A conflict changes nothing and lists
  the paths.
- **Nothing is lost.** Every call either completes or leaves refs, HEAD, index,
  worktree, task records and the current-task pointer exactly as they were.

The server never commits task records: where the tasks directory is tracked in
the code repository, commit it yourself. It works the same with the tasks
directory tracked in the code repository, gitignored or nested in it, in a
separate repository, or outside any repository.

Requirements: a `git` whose `git replay` supports `--ref-action=print` (used
for restarts; verified with git 2.54 and 2.55), and `user.email` set - it
names your branches.

For a walkthrough, subtask rules and troubleshooting, see
[docs/git-branching.md](docs/git-branching.md).

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
created_by: jane        # stamped by create_task; omitted on older tasks
updated_at: 2025-01-15T10:30:00Z
---

Detailed description in Markdown format.

- Acceptance criteria
- Implementation notes
- Links and references
```

Ids are strings on the wire (JSON responses and `--json` CLI output render `"id": "1"`, not a bare number); a bare YAML scalar like `id: 1` above still parses fine into that string field. By default `create_task` allocates the next auto-incrementing numeric-looking id, unpadded (`"8"`, not `"008"`). Optionally, pass `id` (MCP) or `--id` (CLI `create`) to use a caller-supplied custom text id instead — it's validated like an attached filename (non-empty, no `/` or `\`, not `..`; `"0"`, `"archive"`, `".index.json"` and `".users"` are reserved - a retired cache filename and the per-user state directory) and rejected if it collides with an existing active or archived task.

The `type` field must be one of the configured `task_types` values. With the default configuration, allowed values are `feature` and `bug`.

Phase records sit next to the task as `tasks/{id}/<phase>.phase` (`research.phase`, `design.phase`, `planning.phase`, `implementation.phase`), written only by `start_phase`, `finish_phase` and `complete_task`. Each keeps every run of its phase:

```yaml
version: 1
phase: design
runs:
  - started_at: "2026-10-04T12:59:10Z"
    started_by: jane
    finished_at: "2026-10-04T13:20:41Z"
    finished_by: jane
    tokens: 81234
  - started_at: "2026-10-04T14:02:00Z"   # a redo after review: still open
    started_by: jane
```

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
- Completing the last subtask auto-completes the parent, unless the parent works on a git branch
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
│   ├── vcs/                 # git wrapper for branch-per-task
│   └── web/                 # Read-only kanban dashboard (htmx + Tailwind)
├── plugins/mcp-task-manager/ # The installable Claude Code / Codex plugin: skills, commands, .mcp.json
├── .claude-plugin/          # Claude Code marketplace catalog (points at plugins/mcp-task-manager)
├── .agents/plugins/         # Codex marketplace catalog (points at plugins/mcp-task-manager)
├── .claude/skills/          # Generated dogfooding copy of the phase skills (scripts/sync-skills.sh)
├── scripts/build-css.sh     # Maintainer step: rebuild the vendored CSS
├── scripts/sync-skills.sh   # Maintainer step: mirror plugin skills into .claude/skills
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

On Windows x64, run the script from Git Bash. Hard-reload the board after
rebuilding: `/static/app.css` is cached as immutable. Tailwind's automatic
source detection scans every non-gitignored file under the directory it runs
from, not only the templates, so build from a clean checkout: untracked notes
or task artifacts would otherwise add stray utility classes.

### Editing the Packaged Skills

`plugins/mcp-task-manager/` is the only source for skills and commands: both
marketplace catalogs install that directory. Edit skills there and nowhere
else.

This repository also uses its own phase skills (`begin_task`, `research`,
`design`, `planning`, `implementation`, `workflow`) through `.claude/skills/`,
without installing the plugin. Installing it would start a second
`task-manager` server next to the project-scoped `go run` one. That directory
is a generated copy, not a symlink, because symlinks break on Windows checkouts.
After editing one of those skills, refresh the copy:

```bash
bash scripts/sync-skills.sh          # rewrite .claude/skills/
bash scripts/sync-skills.sh --check  # fail if the copy has drifted
```

When you change the plugin, bump `version` in
`plugins/mcp-task-manager/.claude-plugin/plugin.json`,
`plugins/mcp-task-manager/.codex-plugin/plugin.json` and
`.claude-plugin/marketplace.json`, so installed copies pick up the change.

## License

MIT License - see LICENSE file for details.
