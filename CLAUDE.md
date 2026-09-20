# MCP Task Manager

A Go-based MCP server for task management, designed for Claude and coding agents. Also works as a standalone CLI tool.

**Module path:** `github.com/gpayer/mcp-task-manager`

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│              Entry Point (cmd/mcp-task-manager)               │
│        (CLI if args, MCP server otherwise; internal/app)      │
├──────────────────┬───────────────────┬────────────────────────┤
│   CLI Commands   │ MCP Tool Handlers │   Web UI (kanban)      │
│ (list, get, ...) │ (create_task ...) │ (read-only, htmx+CSS)  │
├──────────────────┴───────────────────┴────────────────────────┤
│                        Task Service                           │
│       (business logic, validation, sorting, one mutex)        │
├───────────────────────────────────────────────────────────────┤
│                           Storage                             │
│              (markdown files + in-memory index)               │
└───────────────────────────────────────────────────────────────┘
         │                        │
         ▼                        ▼
    ./tasks/{id}/{id}.md           (source of truth)
    ./tasks/archive/{id}/{id}.md   (archived tasks, not indexed)
```

### Package boundary

`internal/task` is the only package that performs task operations;
`internal/storage` is private to it. A consumer that needs data the service
does not expose gets a new method on `task.Service` (see
`internal/task/view.go`), never its own storage handle.

## Development Process

### Planning, Code Review & Testing
Use the **task manager MCP tools** to organize work:
- `create_task` - Add new tasks for planned work
- `update_task` - Update task status, priority, or details
- `list_tasks` - Review current task state and priorities
- `delete_task` - Remove obsolete tasks

### Implementation
Use the **task manager MCP tools** to get work assignments:
- `get_next_task` - Get the highest priority todo task
- `get_task` - Get a specific task by ID (when directed)
- `start_task` - Mark a task as in progress before starting work
- `complete_task` - Mark a task as done after finishing

### Code Research & Refactoring
Use the **cclsp MCP tools** (LSP server access) for code navigation:
- `find_definition` - Find where a symbol is defined
- `find_references` - Find all usages of a symbol across the codebase
- `rename_symbol` - Safely rename symbols with LSP support
- `get_diagnostics` - Get language diagnostics (errors, warnings) for a file

## Design Decisions

### Storage
- **Source of truth:** Markdown files with YAML frontmatter, one per-task directory (`./tasks/{id}/{id}.md`)
- **Attached files:** free-form named text files live alongside `{id}.md` in the same `./tasks/{id}/` directory
- **Archive:** `./tasks/archive/{id}/{id}.md` — archived tasks (same format, not indexed); archiving moves the whole per-task directory, carrying attached files with it
- **Index:** in-memory only, rebuilt from the .md files; no cache file is ever written
- **Location:** resolved from the project root, not from the server's working directory. Order: absolute `MCP_TASKS_DIR` → `MCP_PROJECT_DIR` → `CLAUDE_PROJECT_DIR` → MCP `roots/list` → marker search upwards from cwd. Inside the root: relative `MCP_TASKS_DIR` → `tasks_dir` from the config → `.tasks` (legacy `tasks/` when that is the directory actually holding tasks). See `internal/config` and `internal/project`.
- **Resolution timing:** lazy, on the first tool call — MCP roots only exist after `initialize`. The tool schemas are registered on defaults and re-published via `tools/list_changed` if the resolved project configures different task types.
- **Config file:** `mcp-tasks.yaml` in the project root
- **Legacy layout migration:** on startup, any task still found in the old flat layout (`tasks/{id}.md` or `tasks/archive/{id}.md`) is automatically migrated into the per-task directory layout

### Task Schema

```yaml
---
id: 42                # string on the wire (JSON: "42"); a bare YAML scalar like this still parses fine
title: "Task title"
status: todo          # todo | in_progress | done
priority: high        # critical | high | medium | low
type: feature         # configurable, defaults: feature, bug
parent_id: 0          # optional, 0 or omitted = top-level task
relations:            # optional, omitted when empty
  - type: blocked_by
    task: 3
  - type: relates_to
    task: 7
created_at: 2025-01-15T10:30:00Z
updated_at: 2025-01-15T10:30:00Z
resolution: obsolete  # optional, done tasks only: completed | obsolete | superseded | duplicate | wontfix
resolution_note: "one line on why"   # optional, done tasks only
closed_at: 2025-01-20T09:00:00Z      # optional, set when the task became done
verified_at: 2025-01-19T12:00:00Z    # optional, last time the task's text was checked against reality
---

Markdown description here.
```

### Task Identification
- Ids are strings. By default `create_task` still allocates an auto-incrementing numeric-looking id, now unpadded (e.g. `"8"`, not `"008"`).
- Optionally, `create_task` accepts a caller-supplied custom text id via its `id` parameter; it is used verbatim as the id and never advances or collides with the numeric auto-increment counter (a numeric-looking custom id like `"5"` still participates correctly in future auto-increment collision avoidance).
- Custom ids are validated the same way attached filenames are: non-empty, no `/` or `\`, not `..`; additionally `"0"`, `"archive"`, and `".index.json"` are reserved (they collide with this package's own on-disk sentinels, the last being the retired index cache filename) and rejected.
- Creating a task with an id that already exists (active or archived) is rejected with a clear error, never silently overwritten or disambiguated.
- Per-task directory is always named after the exact id string used: `7/`, `my-feature/`, etc., each containing `{id}.md` (e.g. `7/7.md`) plus any attached files

### Task Lifecycle
- Simple 3-state workflow: `todo` → `in_progress` → `done`
- Blocked state derived from `blocked_by` relations (see Relations section)
- Completed tasks can be archived (see Archiving section)
- `done` says a task is terminal; `resolution` says what happened (`completed`,
  `obsolete`, `superseded`, `duplicate`, `wontfix`). A resolution implies
  closing, a not-delivered one may close a `todo` task and cascades to its open
  subtasks, and reopening clears it. A `done` task without one reads as
  `completed`. See [_design.md](_design.md)
- `verified_at` is orthogonal to the lifecycle: when the task's own text was
  last checked against reality. Inert — it gates nothing

### Priority Ordering
- Named levels: `critical` > `high` > `medium` > `low`
- Tiebreaker: creation date (older first)

### Subtasks
Tasks support single-level nesting via the `parent_id` field.

**Constraints:**
- Only one level of nesting allowed (subtasks cannot have subtasks)
- Parent task must exist when creating a subtask

**Automatic Behaviors:**
- **Auto-start parent:** Starting a subtask automatically starts its parent (if parent is `todo`)
- **Block parent completion:** Cannot complete a parent task while it has incomplete subtasks
- **Auto-complete parent:** When the last incomplete subtask is completed, the parent is automatically marked `done`

**Delete Protection:**
- Deleting a parent with subtasks requires explicit action:
  - Use `delete_subtasks: true` to cascade delete all subtasks
  - Or delete subtasks individually first

**Agent Workflow Integration:**
- `get_next_task` prioritizes subtasks of `in_progress` parents over other todo tasks (ensures focused completion of started work)
- `get_next_task` skips parent tasks that have incomplete subtasks (returns subtasks instead)
- `list_tasks` shows top-level tasks by default; use `parent_id` filter to list subtasks of a specific parent
- `get_task` includes subtasks in the response for parent tasks

### Relations
Tasks support typed relations to other tasks via the `relations` frontmatter field.

**Relation Types:**

| Type | Semantic | Behavioral effect | Symmetric |
|------|----------|-------------------|-----------|
| `blocked_by` | Source can't proceed until target is done | Affects `get_next_task`, `start_task`, `get_task`, `list_tasks` | No |
| `relates_to` | Informational link | None | Yes |
| `duplicate_of` | Source is a duplicate of target | None | No |
| `superseded_by` | Another task took the work over | None | No |

Relation types are configurable via `mcp-tasks.yaml`. Behavioral effects are hardcoded to specific type names (`blocked_by`).

**Storage rules:**
- `blocked_by`: stored only on the blocked task (the source)
- `relates_to`: stored on one side only; the index generates the reverse edge
- `duplicate_of`: stored only on the duplicate (the source)
- `superseded_by`: stored only on the superseded task (the source); it is the
  edge behind the `superseded` resolution, which never stores the target id itself
- Validation: target task must exist, no self-references, no duplicates

**Blocking behavior:**
- `get_next_task` skips tasks with unresolved `blocked_by` relations (target not `done`)
- `start_task` refuses to start a blocked task
- `get_task` includes a derived `blocked` field and blocker details
- `list_tasks` includes a derived `blocked` field per task

**Delete cascade:**
- When deleting a task, all relations referencing it (as source or target) are removed
- Other tasks' frontmatter is updated to remove stale relations

**Interaction with subtasks:**
- Relations and subtasks are orthogonal — a subtask can be blocked by a task outside its parent
- `get_next_task` applies both filters: skip parents with incomplete subtasks AND skip blocked tasks

### Archiving

Completed tasks can be archived to keep the active task list clean and the index small.

**Storage:**
- Archiving renames the whole `tasks/{id}/` directory to `tasks/archive/{id}/` (same markdown format, unchanged); attached files move along with it automatically
- No archive index — queries against archived tasks do a linear scan of `archive/{id}/{id}.md`
- The in-memory index shrinks as tasks are archived

**Archive Rules:**
- Only `done` tasks can be archived
- Archiving a parent requires all subtasks to be `done`; archives the entire tree
- All relations to/from archived tasks are cleaned up (same as delete cascade)
- Archived tasks are read-only: can be listed and viewed, but not updated/started/completed

**Auto-Archive:**
- When enabled in config, done tasks older than `after_days` (since `updated_at`) are automatically archived
- A task closed with a resolution other than `completed` skips the grace period and is eligible immediately: there is no delivered work left to review
- Triggers on startup and after each `complete_task` call

### Attached Files

A task can have zero or more free-form named text files attached to it (e.g. research notes, design docs), read and written incrementally over the task's lifetime.

**Storage:**
- Attached files live inside the task's own `tasks/{id}/` directory, alongside `{id}.md`
- Filenames are chosen freely by the caller at write time — no fixed set of categories
- Because attached files share the task's directory, `archive_task` and `delete_task` already move/remove them as a side effect of moving/removing that directory — no separate cascade step is needed

**Rules:**
- Filenames must be non-empty, must not contain a path separator (`/` or `\`) or a `..` segment, and must not collide with the task's own `{id}.md` record file
- `write_task_file` is rejected for archived tasks (archived tasks are read-only, consistent with the rest of this project's archived-task semantics)
- `read_task_file` and `list_task_files` work for both active and archived tasks

### Web UI

A read-only kanban dashboard, served by `internal/web` (`net/http` +
`html/template` + `http.ServeMux` patterns; no framework).

- **One process, two transports.** MCP (stdio) and HTTP share a single resolved project and a single `task.Service`. `internal/app` is the composition root.
- **Ways to start it:** `web.enabled` in the config (or `MCP_WEB_ENABLED`) brings it up with the MCP server; the `start_web_ui` tool starts it on demand; `mcp-task-manager serve web` runs it in the foreground.
- **Read-only is structural.** Only `GET` patterns are registered, so `ServeMux` answers everything else with 405, and no handler can reach a mutating service method.
- **Handlers never resolve.** They read `Resolver.Current()`, never `Get()`: resolution runs `Service.Initialize()`, which migrates the layout and may auto-archive, and a plain GET must not move files. Before the first tool call the board renders a placeholder that polls itself back to life.
- **Assets are embedded.** Tailwind output and htmx are vendored under `internal/web/static/` and compiled in with `go:embed`; the page renders offline. Regenerate the CSS with `scripts/build-css.sh` after editing templates — a maintainer step, never part of `go build`.
- **Danger zone.** In-progress tasks are highlighted and named in a banner, so a human reading the board knows an agent may be editing those areas. Presentation only; the UI stays read-only.
- **Archived tasks are not on the board** (the snapshot is the active index); the detail route still serves them, read-only.
- **Logging goes to stderr.** Nothing in `internal/web` writes to stdout — in stdio mode stdout is the JSON-RPC channel.

## MCP Tools

### Task Management
| Tool | Description |
|------|-------------|
| `create_task` | Create a new task with title, description, priority, type, optional `parent_id` for subtasks, and optional `id` for a caller-supplied custom task id |
| `update_task` | Modify task fields, including `resolution` / `resolution_note` (closes the task) and `verified` (stamps `verified_at`) |
| `list_tasks` | List tasks with optional filters (status, priority, type, parent_id, resolution, archived); top-level tasks by default |
| `get_task` | Get full details of a task by ID (includes subtasks for parent tasks; falls back to archive) |
| `delete_task` | Remove a task; use `delete_subtasks: true` to cascade delete subtasks |
| `archive_task` | Archive a completed task (moves its directory, including attached files, to `tasks/archive/`) |

### Attached Files
| Tool | Description |
|------|-------------|
| `write_task_file` | Create or overwrite a named text file attached to a task (rejected for archived tasks) |
| `read_task_file` | Read the content of a named file attached to a task (works for active and archived tasks) |
| `list_task_files` | List the names of all files attached to a task (works for active and archived tasks) |

### Web UI
| Tool | Description |
|------|-------------|
| `start_web_ui` | Start the read-only kanban dashboard and report its URL; optional `addr`. Idempotent — a second call reports the running URL and never double-binds |

### Relations
| Tool | Description |
|------|-------------|
| `add_relation` | Add a relation (`source`, `type`, `target`) between two tasks |
| `remove_relation` | Remove a relation (`source`, `type`, `target`) between two tasks |

### Agent Workflow
| Tool | Description |
|------|-------------|
| `get_next_task` | Returns highest priority `todo` task (skips parents with incomplete subtasks and blocked tasks) |
| `start_task` | Move task from `todo` to `in_progress` (auto-starts parent if subtask; refuses if blocked) |
| `complete_task` | Close a task: `in_progress` → `done` (auto-completes parent if last subtask; triggers auto-archive if enabled). With a `resolution` other than `completed` it also accepts a `todo` task and closes its open subtasks along with it |

## Configuration

`mcp-tasks.yaml`, in the project root:
```yaml
tasks_dir: .tasks     # optional, relative to the project root; default: .tasks
task_types:
  - feature
  - bug
relation_types:       # optional, defaults to these four
  - blocked_by
  - relates_to
  - duplicate_of
  - superseded_by
auto_archive:         # optional
  enabled: false      # default: false
  after_days: 30      # default: 30
web:                  # optional
  enabled: false      # start the dashboard with the MCP server; default: false
  addr: 127.0.0.1:7777  # default
  with_mcp: false     # `serve web` also serves MCP over stdio; default: false
```

A partially written section keeps the defaults for the keys it omits
(`config.applyDefaults`), so `web: {enabled: true}` still listens on the
default address and `auto_archive: {enabled: true}` still waits 30 days.

Environment overrides:

| Variable | Effect |
|----------|--------|
| `MCP_TASKS_DIR` | tasks directory; absolute wins outright, relative is project-root relative |
| `MCP_PROJECT_DIR` | explicit project root |
| `CLAUDE_PROJECT_DIR` | project root, set by Claude Code |
| `MCP_ROOT_SOURCE` | restrict resolution to one source (`roots` to exercise the protocol path) |
| `MCP_WEB_ENABLED` | start the web dashboard with the MCP server (bool; unparseable values ignored) |
| `MCP_WEB_ADDR` | dashboard listen address; never enables it on its own |

`mcp-task-manager version` prints the resolved root, tasks directory and source.

## Dependencies

- **MCP SDK:** `github.com/mark3labs/mcp-go` - Third-party Go MCP implementation
- **YAML parsing:** `gopkg.in/yaml.v3` - For frontmatter and config
- **CLI parsing:** `github.com/integrii/flaggy` - Lightweight CLI argument parser

## Project Structure

```
mcp-task-manager/
├── cmd/
│   └── mcp-task-manager/
│       └── main.go              # Entry point: mode selection + signal context
├── internal/
│   ├── app/
│   │   ├── app.go               # Composition root: RunMCP, RunWeb, MCP server assembly
│   │   ├── icon.png             # Embedded server icon
│   │   └── instructions.md      # Embedded server instructions
│   ├── cli/
│   │   ├── cli.go               # CLI entry point and subcommand setup
│   │   ├── cli_test.go          # CLI tests
│   │   ├── commands.go          # Command handlers (list, get, create, etc.)
│   │   ├── output.go            # Output formatters (table, JSON)
│   │   └── output_test.go       # Output formatter tests
│   ├── config/
│   │   ├── config.go            # Project root / tasks dir resolution + config loading
│   │   └── resolve_test.go      # Resolution order tests
│   ├── project/
│   │   ├── resolver.go          # Lazy, cached project resolution (used by tool handlers)
│   │   └── roots.go             # MCP roots/list client request + file:// URI parsing
│   ├── storage/
│   │   ├── storage.go           # Storage interface
│   │   ├── markdown.go          # Markdown file operations (per-task directory layout)
│   │   ├── migrate.go           # Legacy flat-layout migration
│   │   ├── files.go             # Attached-file read/write/list
│   │   └── index.go             # In-memory index over the task directories
│   ├── task/
│   │   ├── task.go              # Task model/types (status, priority, resolution)
│   │   ├── service.go           # Business logic (single mutex, twin discipline)
│   │   └── view.go              # Board/detail composites for read-only consumers
│   ├── testsupport/
│   │   └── testsupport.go       # Shared test backlog helpers (NewBacklog, Seed, IsolateEnv)
│   ├── tools/
│   │   ├── tools.go             # Tool registration + the WebStarter interface
│   │   ├── management.go        # create, update, list, get, delete
│   │   ├── workflow.go          # get_next_task, start, complete
│   │   ├── relations.go         # add_relation, remove_relation
│   │   ├── files.go             # write_task_file, read_task_file, list_task_files
│   │   └── web.go               # start_web_ui
│   └── web/
│       ├── server.go            # Route table (GET only)
│       ├── handlers.go          # board, board fragment, detail, panel, health
│       ├── view.go              # View models + pure mapping from the snapshot
│       ├── controller.go        # Listener lifecycle: Start / URL / Shutdown
│       ├── templates.go         # Embedded template sets
│       ├── assets.go            # Embedded static assets
│       ├── templates/           # layout, board, card, detail, placeholder
│       ├── assets/input.css     # Tailwind entry (input to scripts/build-css.sh)
│       └── static/              # app.css + htmx.min.js, committed and embedded
├── scripts/
│   └── build-css.sh             # Maintainer step: rebuild the vendored Tailwind CSS
├── mcp-tasks.yaml               # Default config (for reference)
├── _design.md                   # Resolution / verified_at: rationale and rules
├── go.mod
├── go.sum
├── CLAUDE.md
└── README.md
```

## Behavior & Error Handling

### get_next_task
- Returns highest priority `todo` task (priority order, then oldest first)
- Skips parent tasks that have incomplete subtasks (returns actionable subtasks instead)
- Skips tasks with unresolved `blocked_by` relations
- If no `todo` tasks exist, returns "no tasks available" message (not an error)

### Index
- In-memory only; it has no on-disk form and nothing is persisted after a write
- Built on server startup by scanning all .md files
- Self-healing: rebuilt whenever a task file's mtime or the task count diverges from what is held in memory, which covers git pulls and hand edits
- Includes relation edges (with auto-generated reverse edges for symmetric types)

### Concurrency
- A single `sync.Mutex` on `task.Service` serializes every task operation. It is the only lock over the index and the markdown storage, both of which are reachable solely through that type.
- **Twin discipline:** exported methods lock once on entry and delegate to an unexported, unlocked twin (`Get`/`get`, `Update`/`update`, …). Service methods never call each other's exported forms — Go mutexes are not reentrant. `TestServiceNoSelfDeadlock` fails if a new method breaks the shape.
- `EnsureProjectExists`, `ProjectFound` and `Config` are deliberately unlocked: they read only the write-once config field.
- The lock matters because mcp-go's stdio server dispatches tool calls across a worker pool, and because the web dashboard reads the same service concurrently. `go test -race` covers both (`internal/task/concurrency_test.go`, `internal/web/race_test.go`).
- File writes are atomic (write to temp file, then rename)
- Cross-process coordination (file locks, PID files) stays out of scope: whichever transport the process runs, the other comes up inside it.

### Validation
- Task IDs: strings, format-validated the same way attached filenames are (non-empty, no `/` or `\`, not `..`), plus three reserved names (`"0"`, `"archive"`, `".index.json"` - the last a retired cache filename) rejected for a caller-supplied custom id
- Status: `todo` | `in_progress` | `done`
- Priority: `critical` | `high` | `medium` | `low`
- Type: must be in configured list (default: `feature`, `bug`)
- Relation type: must be in configured list (default: `blocked_by`, `relates_to`, `duplicate_of`, `superseded_by`)
- Resolution: `completed` | `obsolete` | `superseded` | `duplicate` | `wontfix`; only valid on a `done` task
- Config: `applyDefaults` fills in what a partially written YAML section left out, so a half-specified `web:` or `auto_archive:` block cannot silently zero the rest

## Future Considerations (Post-MVP)
- Comments/history
- Cycle detection for `blocked_by` chains
- Additional task types (chore, docs, refactor)
- `unarchive` / restore task from archive back to active
- Archive indexing (only needed if archive query performance becomes a problem)
