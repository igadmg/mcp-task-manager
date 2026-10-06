---
id: done-stats-config-errors
title: 'done_stats config: handling invalid card entries'
status: todo
priority: low
type: feature
relations:
  - type: relates_to
    task: done-column-stats
created_at: "2026-10-06T17:56:10Z"
created_by: igor.cwer
updated_at: "2026-10-06T17:56:18Z"
---

Decide and implement what happens to an invalid entry in `web.done_stats.cards`: an unknown kind, field or line name, or a non-positive days.

Background, from the research of done-column-stats:
- A config load error breaks every MCP tool, the CLI and `serve web`, not only the board.
- applyDefaults and the env overrides silently fix bad values today.
- Unknown YAML keys are ignored.
- A YAML type error such as `days: abc` is already a hard Resolve error.

Options:
- skip the card and write a line to stderr;
- show an "invalid card: …" placeholder on the board;
- fail the config load.

Postponed: done-column-stats ships without this.