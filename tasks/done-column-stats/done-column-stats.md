---
id: done-column-stats
title: 'Done column: config-defined stat cards (per-field progress bars, toggleable line charts)'
status: in_progress
priority: medium
type: feature
created_at: "2026-10-06T17:21:28Z"
created_by: igor.cwer
updated_at: "2026-10-06T18:07:12Z"
branch: igor.cwer/wip/done-column-stats
base_branch: main_patched
start_commit: 800eea5c01c78e65d8eeaa0544eeeab5d7963fc9
---

Replace the task cards in the web board's Done column with a config-defined list of statistics cards. Bars cards (default priority, type, resolution) show done / in progress / todo per value, with the last 24 h's closures highlighted. Line-chart cards show per-day created / closed counts, cumulative or split by a field's values; the viewer toggles lines from the legend, and the choice is kept in localStorage. See the `task` file.