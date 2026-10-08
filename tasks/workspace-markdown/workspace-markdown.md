---
id: workspace-markdown
parent_id: web-task-workspace
title: Markdown renderer for the file viewer, zero dependencies
status: done
priority: high
type: feature
created_at: "2026-10-07T19:57:41Z"
created_by: igor.cwer
updated_at: "2026-10-07T21:23:54Z"
resolution: completed
closed_at: "2026-10-07T21:23:54Z"
branch: igor.cwer/wip/web-task-workspace--workspace-markdown
base_branch: igor.cwer/wip/web-task-workspace
start_commit: 210eeb2fc11267c25ef21bb50eeda950a7214278
squash_commit: 963f82812a66a6dbe6df56094802b9f07cbcda84
---

A pure, self-contained markdown-to-HTML renderer for the workspace file viewer. No new module: `go.mod` is not touched, so the project's offline guarantee covers the Go build and not only the browser assets (decided 2026-10-07; goldmark was declined).

Independent of every other subtask — it is one function plus its tests and can land before any UI exists.

## Scope

- Subset to support: ATX headings, paragraphs, bullet and ordered lists (nested), GFM tables, fenced code blocks (with the info string kept as a class or data attribute), inline code, emphasis/strong, links, blockquotes, horizontal rules, hard line breaks.
- ```` ```mermaid ```` is an ordinary fenced code block: no diagram rendering (explicitly declined in the task's clarifications).
- Output is a fragment, not a document: no `<html>`, no `<script>`, no `style` attribute, no inline event handlers.

## Acceptance criteria

- Raw HTML in the source is **escaped, never passed through**. The renderer has no "unsafe" mode to turn on.
- Link and image URLs are filtered by scheme: `http`, `https`, `mailto`, relative paths and in-page anchors pass; everything else (`javascript:`, `data:`, `vbscript:`, scheme with embedded whitespace or control characters, case and entity tricks) loses the link and keeps the visible text.
- Every text node, code-block body, table cell and attribute value is escaped; `<`, `>`, `&`, `"` and `'` in prose or code cannot produce markup.
- The function returns a plain `string`. Wrapping it in `template.HTML` happens in exactly one place in `internal/web` (that is the file-column subtask's job, and it is the single, documented exception to the rule in `internal/web/templates.go:16-19` that this package holds no trusted HTML).
- Deterministic and allocation-sane on the real artifacts in `tasks/`: rendering every `*.md` under `tasks/` must not panic and must terminate. A table-driven test does exactly that.
- Unit tests cover, at minimum: each supported construct; nested lists; a table with inline code and a link in a cell; an unterminated fenced block; a fence inside a list; `<script>` in prose, in a heading, in a code block and in a link title; each rejected URL scheme; CRLF input; a file with no trailing newline; empty input.
- No new entry in `go.mod` / `go.sum`.

## Out of scope

- Any HTTP route, template or CSS — the viewer markup and its styling belong to `workspace-file-column`.
- Syntax highlighting inside code blocks.
- Reference-style links, footnotes, task-list checkboxes, inline HTML passthrough, setext headings (add later only if a real artifact in `tasks/` needs them; note which, if any, are missing when the tests run over the corpus).