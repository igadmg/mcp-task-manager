# Research: done-stats-cleanup

The source is the code review of done-column-stats: commits cf1bf77..4340e21. The evidence for each point is below.

1. **Metric/cumulative.** `task.StatsCard.Metric` is set only for split cards (`task/stats.go:74`) and read only by `web/view.go:422`. There it is used together with `strings.HasSuffix(l.Key, "_cumulative")` to choose the y-scale group. `statsMetric` (`task/stats.go:182`) already returns `cumulative`.
   - Tests that use `card.Metric`: `task/stats_test.go:249-288` and `web/view_test.go:715,752`.
2. **Counting loops.** `statsLines` (`task/stats.go:121-177`) repeats metricTime → bucket → count → runningSum in its split and plain branches.
3. **Field switches.** `statsValue` and `statsOrder` (`task/stats.go:266-300`) switch on the same five field names. The priority and status orders are written out by hand. `IsValidStatus` / `IsValidPriority` (`task.go:116-131`) list the same constants. `Resolutions()` already shows the "list function" pattern.
4. **Web.**
   - `"Jan 2"` appears three times in `newStatsChart`.
   - For n == 1 the points are computed and then replaced.
   - The h3 classes are identical in `_stats_bars.html:2` and `_stats_lines.html:2`.
5. **Config.**
   - The `base_branches` trim loop in `applyDefaults` (`config.go:264-269`) is exactly `trimList`.
   - **`humanize` for the split title is NOT equivalent**: it capitalizes the first letter, which would turn "Closed cumulative by created by" (`config/stats_test.go:89`) into "…by Created by". **Dropped.**
6. **Tests.** `laneSection` (`handlers_test.go:397`) and the manual cut of the lines card (`handlers_test.go:628-636`) do the same thing.
