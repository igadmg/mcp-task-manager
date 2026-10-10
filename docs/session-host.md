# AI session host (opt-in)

The web dashboard can run interactive AI sessions (Claude Code, first
backend) that a user drives from the browser: Claude streams its output,
asks questions and requests permissions, and the user's answers go back
into the running session.

Three processes are involved:

```
browser --HTTP--> mcp-task-manager-web --HTTP+SSE, loopback, bearer--> mcp-session-host --stdin/stdout--> claude -p (stream-json)
```

From the outside there is exactly one thing — the web server. The session
host (`cmd/mcp-session-host`, packages `internal/sessionhost` and
`internal/sessionhost/claude`) is a separate process on the same machine.
The web server tunnels the browser's requests to it through
`internal/hostclient` and knows nothing about Claude; the wire vocabulary
shared by both sides lives in `internal/sessionapi` (standard library only).

## Security model

- **Off by default.** Sessions exist only when `web.sessions.enabled:
  true` is set in `mcp-tasks.yaml`; with it unset or false there are no
  session routes (404) and no UI links.
- **Loopback only.** The host refuses to start on a non-loopback address,
  and the web server's config validation enforces the same rule for
  `web.sessions.host_addr`.
- **Bearer token.** The host creates `<state-dir>/host.token` (random,
  file permissions 0600) on first start and requires
  `Authorization: Bearer` on every route except `/healthz`. The web
  server reads the same file on every request (the host owns it and may
  rotate it); the token never reaches the browser.
- **CSRF.** Every session POST requires the `X-Dashboard-CSRF` header with
  the process-random token the page hands out in a `<meta>` tag, and a
  JSON content type (anything else is 415). The dashboard's read-only
  guarantee for task data is unchanged: session routes never touch the
  task service.
- **No sandbox (v1).** A session runs `claude` directly in the project
  folder of the chosen workspace, with the host process's privileges. That
  means arbitrary code execution and file access in a real working tree.
  This feature is meant for a trusted single-user setup on loopback; do
  not expose the dashboard listener beyond loopback, and treat every
  session as `git status` material — review what Claude changed before
  keeping it. Permission prompts and questions are answered by a human,
  which is the only gate between model output and shell execution.
  Isolated workspaces (containers, clones, worktrees) are an explicit
  non-goal of v1.

## Running the host

```
mcp-session-host [--addr 127.0.0.1:7778] [--state-dir <dir>]
                 [--claude-path <path>] [--max-sessions 4]
```

- `--addr` must be loopback; anything else is a startup error.
- `--state-dir` holds one directory per session (`spec.json`, `meta.json`,
  `events.jsonl`, `raw.jsonl`). Pick a directory outside the task tree.
  The default is under the user config directory.
- `--claude-path` overrides the `PATH` lookup for the `claude`
  executable. A missing binary fails the session visibly (status
  `failed`), not silently.
- The host logs to stderr. Its stdout is not a protocol channel, but it
  is left alone anyway.
- Graceful shutdown (`SIGINT`/`SIGTERM`) stops all live sessions; see
  lifetimes below.

Web side (`mcp-tasks.yaml`):

```yaml
web:
  sessions:
    enabled: true                  # default false
    host_addr: 127.0.0.1:7778
    token_file: <state-dir>/host.token
```

Start the host before (or after) the web server — a missing host shows a
clear "session host is unavailable" message in the UI instead of an empty
page, and recovers on reload once the host is up.

## Session lifetimes

- A session starts from a **spec** (`internal/sessionapi`): `version: 1`,
  `provider: claude`, `workspace: {kind: working_dir, path: <absolute
  dir>}`, a non-empty `prompt` (≤ 64 KiB) and an optional kebab-case
  `task_id`. Only `working_dir` exists in v1; any other workspace kind is
  rejected with a structured error. The web server always fills
  `workspace.path` from its own workspace registry — the browser never
  names a path.
- Claude runs between turns: after a `turn_result` the process waits for
  the next message. Statuses: `starting`, `running`, `waiting_answer`
  (an unanswered question/permission), `idle`, and terminal `finished`,
  `failed`, `stopped`. Terminal sessions stay listable and their events
  replay from the journal.
- Answering, follow-up messages and stop are per-session POSTs tunneled
  to the host. Stopping cancels unanswered requests
  (`request_resolved(outcome=cancelled)`) and kills the whole process
  tree.
- **Host restart** kills the `claude` processes with it: sessions that
  were live become `failed` with `reason=host_restarted`. Finished
  sessions survive and remain browsable. Resuming interrupted sessions is
  a future feature.
- **Web restart** changes nothing: the host owns the sessions, so
  reloading the page or restarting the web server reconnects to the same
  state. The browser's event stream resumes from `Last-Event-ID`.

## Errors and limits

- Errors are structured `{"error": {"code", "message"}}` end to end; the
  UI shows the message. Notable codes: `invalid_spec`,
  `unsupported_provider`, `unsupported_workspace_kind` (the message lists
  what is supported), `workspace_not_found`, `too_many_sessions`,
  `not_found`, `conflict`, `host_unavailable`, `unauthorized`.
- At most `--max-sessions` (default 4) sessions are live at once; further
  starts get `409 too_many_sessions`.
- Slow event consumers do not lose events: the host writes every event to
  its journal and replays from `Last-Event-ID`, so a reconnecting browser
  (or a buffering proxy hiccup) catches up. The web UI shows a visible
  "connection lost, retrying" notice while reconnecting, and a finished
  session's replay closes the stream without it.

## Limitations (v1)

- One workspace kind (`working_dir`), one provider (Claude Code via
  stream-json). Other providers and isolated/committed workspaces are
  planned but not implemented.
- No binding between a session and a task card; no attaching to sessions
  started outside the web UI.
- The host does no git work of its own; branching/committing inside a
  session happens through the MCP task manager like for a local agent.
- Concurrent writers to the same backlog (an agent in a session and
  another process) are not coordinated beyond the existing file-level
  conventions.
