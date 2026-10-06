---
id: done-stats-bars-legend
orphaned_id: done-column-stats
title: 'Web: tooltip explaining the bar colours on Done statistics cards'
status: todo
priority: low
type: feature
relations:
  - type: relates_to
    task: done-stats-bars
created_at: "2026-10-06T19:25:55Z"
created_by: igor.cwer
updated_at: "2026-10-06T20:18:36Z"
---

Follow-up to `done-stats-bars`. The stacked bar in a bars card (`internal/web/templates/_stats_bars.html`) has four colours, and nothing on the board says what they mean:
- green (`.bar-done`, emerald-500): done;
- light green (`.bar-recent`, emerald-200): closed in the last 24 h. It is part of done, drawn over the end of the done segment, and matches `+M` in the label;
- amber (`.bar-in_progress`, amber-400): in progress;
- grey (`.bar-todo`, neutral-500): todo.

Today the SVG has only an `aria-label` with the counts. That is read by screen readers and gives no visible hover tooltip.

Scope:
- Show a hover tooltip on the bar that explains the colours together with the row's numbers. For example, add an SVG `<title>` child: "medium: 3 done (2 in the last 24 h) · 2 in progress · 2 to do", with each part named by its colour. A per-`<rect>` `<title>` for each segment is an option.
- Optionally add a compact legend in the card header or on the column (four small swatches) that explains the colours once instead of on every row. The design decides between the two.
- Stay within the existing rules: no inline `style`, no JS, plain html/template escaping, and readable on the dark board at the column's narrow width.
- Tests: a handler or fragment test that checks the tooltip text for a known row. If new classes appear, update `assets_test` and rebuild `app.css` with `scripts/build-css.sh`.
- Docs: the Done column bullet in the CLAUDE.md Web UI section.