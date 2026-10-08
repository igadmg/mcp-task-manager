---
id: workspace-file-column
parent_id: web-task-workspace
title: 'File column: rendered markdown in the wide working area'
status: todo
priority: high
type: feature
relations:
  - type: blocked_by
    task: workspace-tiler
  - type: blocked_by
    task: workspace-markdown
  - type: blocked_by
    task: workspace-filenames
created_at: "2026-10-07T19:59:14Z"
created_by: igor.cwer
updated_at: "2026-10-07T20:00:14Z"
---

The `f` column kind: the wide working area that shows one attached file (or a task's description), rendered as markdown on the server. This is the point of the whole parent task — a task's `research` / `design` / `plan` cannot be read in the UI at all today.

Blocked by `workspace-markdown` (the renderer), `workspace-filenames` (the exported validator) and `workspace-tiler` (the chain and the kind registry).

## Scope

- The file column: a route that takes the task id and the file name out of the chain, reads the file through `task.Service.ReadTaskFile` (`internal/task/service.go:352-360`, archive fallback included) and renders it.
- Rendering rules:
  - `*.md` and files with **no extension** (older tasks carry `design`, `research`, `plan`) go through the markdown renderer;
  - everything else is shown as preformatted text;
  - the rendered fragment is the single place in `internal/web` where a `template.HTML` wrap happens. It gets a comment saying so and why, because `internal/web/templates.go:16-19` currently states the package has none — that rule is being narrowed on purpose, not forgotten.
- The description column: a task's own description rendered the same way, so "open the description" and "open a file" are the same working area.
- Status mapping: the exported validator rejects the name → **400**; the read fails for any reason → **404** with the "gone"-style state (`internal/web/templates/_detail.html:1-5` is the precedent); nothing produces a 500.
- The working area scrolls independently of the page; the columns do not move with it.
- Styling for rendered markdown: a prose scope in `internal/web/assets/input.css` covering headings, lists, tables, code blocks, inline code, links, blockquotes and rules, consistent with the dashboard's dark palette.

## Acceptance criteria

- A heading, a GFM table and a fenced code block from a real artifact all render as HTML.
- Raw HTML in a file is escaped and never executed; a `javascript:` link is neutralized and keeps its text. Both asserted through the HTTP route, not only in the renderer's own tests.
- An extensionless file (`design`) renders as markdown; a `.txt` or `.yaml` file renders as preformatted text.
- A malformed file name → 400. A missing file, a missing task, and a task deleted while the column was open → 404 with the "gone" state. No input produces a 500 (including `.`, the hole `workspace-filenames` closes).
- A `*.phase` name reaching this route is refused rather than rendered — the phase records are server-owned and the detail view leaves them out (`internal/task/view.go:173-178`).
- An archived task's file renders read-only.
- Nothing is fetched from the network: `TestNoExternalAssetReferences` (`internal/web/handlers_test.go:226-250`) is extended to the workspace routes and stays green. No new JS is needed for rendering; any that appears is vendored under `internal/web/static/` and embedded.
- New classes exist in the compiled `app.css` — an `assets_test.go`-style check so a forgotten `scripts/build-css.sh` run fails loudly.
- Tests, per the parent's AC list: markdown → HTML (heading, table, code block); raw HTML and `javascript:` neutralized; extensionless as markdown and `.txt` as plain text; bad name 400; missing file 404; archived task; the description column.

## Out of scope

- The markdown subset itself and its unit tests (`workspace-markdown`).
- Keeping scroll position across a poll (`workspace-live`).
- Listing the files — the list lives in the task column (`workspace-task-column`); this subtask only renders the one the chain names and marks nothing.