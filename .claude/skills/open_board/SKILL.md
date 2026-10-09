---
name: open_board
description: Use when the user wants the kanban dashboard of this project's backlog rebuilt from source and opened in a browser. Trigger for requests like "пересобери mcp сервис, установи и открой канбан", "rebuild and open the board", "покажи доску задач", "restart the web UI", or any ask that combines rebuilding/installing the mcp-task-manager binary with showing the current tasks of this project.
---

# Open Board

Rebuild the `mcp-task-manager` binary for mcp and web from the current working tree, install it
on the user's `PATH`, serve the read-only kanban dashboard for **this** project,
and open it in the browser.

Run every step from the repository root (`go env GOMOD` must point at this
repo's `go.mod`). The MCP config launches the server with `GOWORK=off`
(`.mcp.json`), so use the same for build and install — a stray `go.work` would
otherwise resolve different dependencies than the server actually runs with.

## Steps

### 1. Rebuild

```bash
GOWORK=off go build ./...
```

Fix compile errors before going further; never install a tree that does not
build. Run `GOWORK=off go test ./internal/web/... ./internal/task/...` when the
change being verified touches the board, the snapshot or the phase records.

If `internal/web/templates/**` or `internal/web/assets/input.css` changed since
the last commit, run `scripts/build-css.sh` first: the Tailwind output under
`internal/web/static/` is vendored and embedded with `go:embed`, and `go build`
never regenerates it. Skip it otherwise — it is a maintainer step, not part of
the build.

### 2. Install

```bash
GOWORK=off go install ./cmd/mcp-task-manager
```

This writes `mcp-task-manager` into `go env GOBIN` (else `$(go env GOPATH)/bin`).
Confirm the binary that is now first on `PATH` is the one just installed
(`command -v mcp-task-manager`), and say so if `PATH` resolves somewhere else —
the bundled plugin `.mcp.json` launches the binary by name, so a shadowed
`PATH` entry silently keeps the old build.

### 3. Serve the board

Check the port before binding. The default address is `127.0.0.1:7777`
(`web.addr` in `mcp-tasks.yaml`):

```bash
lsof -iTCP:7777 -sTCP:LISTEN -n -P
```

- **Already listening** — do not start a second process and do not kill a
  server the user may have started by hand. Ask whether to restart it, or just
  open the existing URL and say the board is served by an already running
  process (which may be the pre-rebuild binary).
- **Free** — start the freshly installed binary in the background, from the
  project root so project resolution finds this repo:

```bash
mcp-task-manager serve web --addr 127.0.0.1:7777
```

`serve web` resolves the project eagerly, so the board has data from the first
request. Pass `--addr` explicitly on a non-default port; never add `--mcp`
here — it would read JSON-RPC from the terminal's stdin.

### 4. Verify the project, then open

The board must show *this* project's backlog, not whatever directory the
command happened to start in. Confirm the resolution:

```bash
mcp-task-manager version
```

It prints the resolved root, tasks directory and source. If the root is not
this repo, re-run with `MCP_PROJECT_DIR=<repo root>` rather than guessing.

Then open it:

```bash
open http://127.0.0.1:7777/
```

Report the URL in the reply as well, so the user has it if the browser does not
come up.

## Rules

- **The dashboard is read-only.** Only `GET` routes are registered; nothing on
  the board can change a task. Use the MCP tools for mutations, never the UI.
- **Never kill a process the user started**, and never rebind a port that is
  already serving — see step 3.
- **A rebuild does not refresh this session's MCP tools.** `.mcp.json` launches
  the server with `go run ./cmd/mcp-task-manager`, and that process started when
  the session did. The new code is live in the `serve web` process only; the
  `task-manager` tools in the current session still run the old build until the
  MCP client restarts. State this instead of implying the tools were updated.
- The served board and the session's MCP server are **separate processes** with
  separate `task.Service` instances over the same markdown files. The index is
  self-healing (mtime and count checks), so tool writes show up on the board's
  next poll (every 5 s) — but there is no cross-process locking, which is by
  design.
- Report what actually happened: if the CSS was stale, a test failed, the port
  was taken, or `PATH` resolved to another binary, say so plainly with the
  output rather than reporting a clean open.

## Done When

- `go build ./...` passed.
- The installed binary is the current tree's, and first on `PATH`.
- `serve web` is listening and `version` resolves to this repository.
- The browser was opened and the URL is in the reply.
