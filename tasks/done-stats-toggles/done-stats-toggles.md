---
id: done-stats-toggles
parent_id: done-column-stats
title: 'Web: legend toggles for chart lines, kept in localStorage across htmx polls'
status: done
priority: medium
type: feature
relations:
  - type: blocked_by
    task: done-stats-lines
created_at: "2026-10-06T17:56:10Z"
created_by: igor.cwer
updated_at: "2026-10-06T19:59:50Z"
resolution: completed
closed_at: "2026-10-06T19:59:50Z"
branch: igor.cwer/wip/done-column-stats--done-stats-toggles
base_branch: igor.cwer/wip/done-column-stats
start_commit: 8a0357244164a759b3dab5c14aa5b8f305a8c26c
squash_commit: 4340e21cc84524f836ea2004e2841173ffa5495c
---

In `static/app.js`, add a delegated click on a legend entry that toggles its line.
- The state is kept in localStorage under its own key prefix (not htmx-history-cache), keyed by card + line so a config change does not break it. Every read and write is wrapped in try/catch, and the page works without storage.
- The state is re-applied after every board swap (htmx:afterSettle / htmx:load), because the outerHTML poll and settle reset class and style.
- Send no request and use no htmx attributes, so the board stays read-only.
- Add a test that the legend markup carries no hx-*. Update the README and the CLAUDE.md Web UI section.