---
id: done-stats-cleanup
parent_id: done-column-stats
title: 'Done stats: remove duplication found in the code review'
status: done
priority: medium
type: feature
created_at: "2026-10-06T20:07:13Z"
created_by: igor.cwer
updated_at: "2026-10-06T20:15:18Z"
resolution: completed
closed_at: "2026-10-06T20:15:18Z"
branch: igor.cwer/wip/done-column-stats--done-stats-cleanup
base_branch: igor.cwer/wip/done-column-stats
start_commit: 85abd9bc1eca785a2126debc3b5c7827649e8bb6
squash_commit: 337273c7165fb31eb6dd37247f0c41f2bfcbf4da
---

Cleanup with no behaviour change, from reviewing the done-column-stats code:
1. Add `task.StatsLine.Cumulative`, set from `statsMetric`. Web reads it instead of checking the `_cumulative` suffix; drop `task.StatsCard.Metric`.
2. One per-day counting helper for the split and plain paths of `statsLines`.
3. One field table (value + domain order) instead of the two switches in `statsValue` / `statsOrder`. Add `Priorities()` and `Statuses()` next to `Resolutions()` and reuse them in `IsValidPriority` / `IsValidStatus`.
4. Web:
   - a `dayFormat` constant;
   - a direct branch for the one-day chart;
   - a `.stats-title` CSS class instead of the duplicated h3 classes (rebuild the CSS).
5. Config:
   - `trimList` for `base_branches` in `applyDefaults`;
   - `humanize` for the split title.
6. Tests: generalize `laneSection` into `sectionAfter` and use it for the lines card.