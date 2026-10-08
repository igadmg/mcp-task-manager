---
id: task-extra-fields
title: 'Flexible task fields: arbitrary key/value frontmatter, preserved and exposed everywhere'
status: todo
priority: medium
type: feature
relations:
  - type: relates_to
    task: web-phase-grid-spans
created_at: "2026-10-07T21:25:00Z"
created_by: igor.cwer
updated_at: "2026-10-07T21:25:03Z"
---

Give a task arbitrary `key: value` frontmatter fields (think `complexity: high`, `area: web`), never lose the ones the server does not know, and surface them through the tools, the board and the CLI.

## Why

The frontmatter is read and written through two closed structs — [markdown.go:47](internal/storage/markdown.go#L47) (write) and [markdown.go:185](internal/storage/markdown.go#L185) (read) — so any key outside that list is silently dropped the next time the server writes the record. Today that means a hand-added `complexity: high` survives exactly until the first `update_task`. The immediate trigger: `web-phase-grid-spans` had to carry its complexity as a line of markdown in the description because the schema has no place for it.

## Wanted behaviour

- **Shape: arbitrary scalar `key: value` pairs** at the frontmatter top level, not a `tags:` list. `complexity: high`, `area: web`. Decide and document: which value types are allowed (string / number / bool scalars at least), how keys are validated (shape rules reuse, reserved = every key the server owns), and what happens on a collision with a known key.
- **Lossless round-trip (the core).** Any unknown key read from a record is carried back out unchanged on every write — update, start, complete, archive, branch flows. Key order and formatting should stay stable enough that a server write produces no spurious diff in a user's `tasks/` commit. This is the one part that must be right even if the rest is staged.
- **MCP tools.** `get_task` and `list_tasks` return the fields; `create_task` and `update_task` can set and clear them. Setting a field to empty/null removes it. Keep the wire shape one obvious map.
- **Web board.** Extra fields render as chips on the card (alongside the existing `chip-{{ .Priority }}` / `chip-muted` chips in [_card.html:11-19](internal/web/templates/_card.html#L11-L19)) and as a block in the detail view. Decide what is chip-worthy on a card vs detail-only — a card with ten chips is noise. Presentation stays read-only.
- **Filters and CLI.** `list_tasks` can filter by a field value; the CLI shows the fields in `get` and can filter in `list`.

## Scope notes

- `internal/storage` is private to `internal/task`, so the map travels on `task.Task` and the storage layer owns the merge. A `yaml.Node` or `map[string]any` catch-all on the parse side plus a merge on the write side is the likely mechanism; keep it out of the per-flow code so no new flow can forget it.
- `*.phase` records and the config are separate formats — out of scope.
- Does **not** make any behaviour depend on a field value: no sorting, no `get_next_task` effect. Flexible metadata only. A real first-class `complexity` (ordering, stats card) would be a follow-up on top of this.
- Tests: round-trip through every mutating service flow (unknown keys survive), tool payloads, chip markup, filter behaviour, and the collision/validation rules.

Follow-up on: `web-phase-grid-spans`, whose `Complexity: high` marker lives in its description for want of this.