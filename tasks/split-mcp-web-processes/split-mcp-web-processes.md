---
id: split-mcp-web-processes
title: Split MCP and web into two binaries, run the dashboard as its own process
status: todo
priority: medium
type: feature
created_at: "2026-10-07T19:53:39Z"
created_by: igor.cwer
updated_at: "2026-10-07T19:53:39Z"
---

Today one process serves both transports: `internal/app` is the composition root, MCP (stdio) and HTTP share a single resolved project and a single `task.Service`, and `start_web_ui` binds a listener inside the MCP process. The dashboard therefore lives and dies with the agent's MCP server.

Goal: two applications in one codebase — the web UI runs as a separate process of the same repo, decoupled from the MCP server's lifetime.

## Decisions (from the intake interview)

**Data access — its own `task.Service` over the same directory.**
The web binary resolves the project itself and reads `./tasks` through its own `task.Service` and its own in-memory index. No MCP client, no new IPC contract. The dashboard is already read-only, and the index is already self-healing on mtime / task-count divergence, so a write by the MCP process becomes visible to the web process on the next poll.

**Write ownership — only MCP writes.**
The web process gets a structurally read-only service: no legacy-layout migration, no auto-archive, no index persistence (there is none on disk anyway). This needs an explicit resolve mode that skips `Service.Initialize()`, which currently migrates and may auto-archive — a plain GET must not move files, and now neither may a whole process. If the tasks directory is still in the legacy flat layout, the web process renders the existing placeholder/banner instead of migrating it; the MCP process does the migration when it starts.

**Launch — `start_web_ui` spawns the process.**
The tool stays, but instead of an in-process listener it starts the second binary (`os/exec`) and reports its URL. `web.enabled` / `MCP_WEB_ENABLED` does the same at MCP startup. `mcp-task-manager-web` can also be run by hand or by a supervisor.

**Lifecycle — the child outlives MCP, one instance per project.**
The spawned process is detached: restarting the agent neither kills the dashboard nor starts a second one. Idempotency therefore needs an on-disk marker of the running instance (PID + address under `.users/`, or a probe of the configured address) rather than the current in-memory "already started" check. Decide and document what happens when the marker is stale (dead PID, port taken by something else).

**Repo structure — two `cmd/`, shared `internal`.**
`cmd/mcp-task-manager` (MCP + CLI) and `cmd/mcp-task-manager-web`. `internal/app` splits into two composition points; `internal/task`, `storage`, `config`, `project`, `vcs`, `web` stay shared. Two build artifacts.

## Scope notes

- `web.with_mcp` (`serve web` also serving MCP over stdio) has to be reconsidered: it exists only because the two transports share a process.
- `internal/web` handlers already read `Resolver.Current()` and never `Get()`; check whether that distinction still earns its keep once the web process owns its resolution.
- The `sync.Mutex` on `task.Service` no longer coordinates web reads against MCP writes — they are in different processes. CLAUDE.md states cross-process coordination is out of scope; this task does not change that, it relies on atomic writes (temp file + rename) plus index self-healing, and that reasoning belongs in the design doc.

## Definition of done

- The dashboard behaves as it does today: same board, same detail view, same polling, same read-only guarantees.
- `internal/app` split into two composition roots; both binaries build.
- Existing tests green, `internal/web/race_test.go` included, adapted to the new wiring.
- An integration test: the MCP process writes a task, a web process in a separate process sees it through index self-healing.
- Docs: CLAUDE.md architecture section and README updated — how to run the two processes, what changed in the config (`web.enabled`, `web.with_mcp`, the instance marker), and the write-ownership rule.
