---
id: done-stats-lines
parent_id: done-column-stats
title: 'Web: lines cards as inline server-side SVG charts'
status: done
priority: medium
type: feature
relations:
  - type: blocked_by
    task: done-stats-bars
created_at: "2026-10-06T17:56:10Z"
created_by: igor.cwer
updated_at: "2026-10-06T19:41:47Z"
resolution: completed
closed_at: "2026-10-06T19:41:47Z"
branch: igor.cwer/wip/done-column-stats--done-stats-lines
base_branch: igor.cwer/wip/done-column-stats
start_commit: 0d11246fd08289af73d3b22db5102260fe694f58
squash_commit: 388ac38d954a876699b903440239a4534cb435ce
---

Add the `_stats_lines.html` partial: an inline SVG line chart rendered on the server, with no JS charting library.
- One polyline per series, plus day labels and `<title>` tooltips per point.
- Add a legend with the line colours. Each series carries stable data attributes (card key + line key) so the next subtask can toggle it; avoid `data-*` names that html/template treats as JS or URL context.
- Lines named in `hidden` render hidden.
- The palette is legible on the dark board.
- Add view-mapping and handler tests.
- Update the docs.