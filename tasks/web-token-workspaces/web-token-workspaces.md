---
id: web-token-workspaces
title: 'Token-prefixed web URLs: one server, many task workspaces per session token'
status: todo
priority: medium
type: feature
relations:
  - type: relates_to
    task: split-mcp-web-processes
created_at: "2026-10-07T20:29:45Z"
created_by: igor.cwer
updated_at: "2026-10-07T20:29:48Z"
---

Today the web dashboard serves exactly one project: `internal/web` reads a single resolved project through `Deps.Project`, and the route table is flat (`GET /{$}`, `/board`, `/tasks/{id}`, `/tasks/{id}/panel`, `/static/`, `/healthz` — `internal/web/server.go:57-64`). One process, one board, one tasks directory.

Goal: every API/page URL of the web server starts with an access token. A token stands for a workspace session — a descriptor built from the path to a tasks directory plus the source that opened it (MCP, or the web UI itself) — and the server serves many of them at once.

## Decisions (from the intake interview)

**Purpose — multi-project / multi-session, not authentication.**
The token is a namespace in the URL that picks which tasks are shown. Tokens are random, so they are not guessable from the path, but the design makes no access-control promise: no auth, no authorization, nothing that should be mistaken for a security boundary. Document it that way so nobody later relies on it.

**Token → descriptor.**
A descriptor is `{tasks dir path, source}`, where source is `mcp` (an MCP server registered its resolved project) or `web` (a workspace chosen on the welcome page). The token itself is random, generated at registration time, and is just the key for that descriptor.

**Registry — in memory, process-lifetime.**
The registry lives only in the web server's memory. A restart starts from scratch; old links stop resolving. No on-disk token state, no TTL, no eviction, nothing to clean up.

**URL shape — `/<token>/...`, with reserved segments.**
`/<token>/{$}`, `/<token>/board`, `/<token>/tasks/{id}`, `/<token>/tasks/{id}/panel`. `/static/` and `/healthz` stay global: the embedded assets are identical for every session, so one URL space and one browser cache for them. `static` and `healthz` become reserved segments the token generator can never emit — the same pattern as the reserved task ids in `internal/storage`.

**Root `/` — a welcome page.**
`GET /` renders a welcome page listing the workspaces the server is allowed to open; picking one creates a token and takes the browser into `/<token>/`. It replaces today's "the board at `/`".

**Read-only stays structural, re-stated.**
Under `/<token>/` only GET patterns are registered, so no handler there can reach a mutating `task.Service` method and `ServeMux` still answers everything else with 405. The one POST in the process is the session registry at the root (create a token from a chosen workspace). The rule becomes: *task data is reachable by GET only; POST exists only on the session registry.*

**Workspace list — the web server's own config.**
A machine/user-level file (e.g. `~/.config/mcp-task-manager/web.yaml`) holds the selectable workspaces and the server's own settings. Not `web.workspaces` in a project's `mcp-tasks.yaml`: the list is cross-project and must not depend on which project the process was started from. Free-form path entry and directory scanning are out — the welcome page offers exactly what the config lists.

**Tokens always — no untokenized mode.**
One code path. The dashboard embedded in the MCP process also registers its resolved project as a descriptor (`source: mcp`) and `start_web_ui` returns the URL with the token in it. No legacy flat routes, no redirect from `/board` to a single session.

## Scope notes

- Every link and htmx attribute in the templates is an absolute path today (`href="/"`, `hx-get="/board"`, `href="/tasks/{{ .ID }}"`, `hx-get="/tasks/{{ .ID }}/panel"`, `hx-push-url=…` — ~15 places in `templates/layout.html`, `_board.html`, `_unresolved.html`, `_card.html`, `_detail.html`, `detail.html`). All of them need the session base prefix, best through one template func alongside the existing `asset` one, so no template ever writes a token by hand.
- Per-descriptor `task.Service` + in-memory index: decide when it is built (lazily on the first request for that token), and what resolve mode it gets. The read-only rule says a GET must never run `Service.Initialize()` (it migrates the layout and may auto-archive) — with N workspaces that rule gets sharper, not softer. `split-mcp-web-processes` wants the same explicit read-only resolve mode; coordinate.
- Poll cost: N open boards poll every `DefaultPollSeconds` and each poll may rebuild an index. Say something about the bound on memory and work.
- Re-registering a path: does a second welcome-page pick of the same workspace reuse the existing descriptor's token or mint a new one? Same question across sources (`mcp` and `web` on one path). Not decided — decide in design.
- `Deps.Project func() (*project.Resolved, bool)` no longer describes the dependency; it becomes a registry lookup by token. The "handlers never resolve" rule has to survive the rewrite.
- Tokens land in logs, `Referer` headers and the browser history. Fine given the no-security stance, but write it down.
- 404 for an unknown or expired token should explain itself and link to `/`, not be a bare 404 — restarts make this the normal case.

## Definition of done

- `/<token>/…` serves board, board fragment, detail and detail panel for the descriptor the token names; `/static/` and `/healthz` global; `static` / `healthz` unusable as tokens.
- Welcome page at `/` lists the configured workspaces and creates a session (the only POST).
- The MCP-embedded dashboard and `start_web_ui` report tokenized URLs; no untokenized route remains.
- The web server's own config file: format, lookup order and defaults documented.
- Templates carry no absolute path to a session route any more.
- Tests: routing with two live descriptors isolated from each other, an unknown token, a reserved-segment token rejected at generation, the existing `internal/web/race_test.go` adapted to several concurrent sessions.
- Docs: CLAUDE.md "Web UI" section and README — URL shape, the token-is-not-auth statement, the new config file, the restated read-only rule.