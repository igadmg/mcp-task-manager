# Design: done-stats-cleanup

This is a refactoring only. The rendered HTML (apart from the h3 class), the statistics and the config behaviour do not change.

1. **`task.StatsLine.Cumulative bool`** says the values are a running total.
   - `statsLines` sets it from `statsMetric`.
   - `task.StatsCard.Metric` is removed. `Field` stays: it is how web tells a split card apart, which decides the colours.
   - In web, `newStatsChart` picks the group as `l.Cumulative`, with no `strings.HasSuffix`.
2. **Counting.** `countDays(all, base, n, bucket, keyOf func(*Task) (string, bool)) map[string][]int` counts per key, per day.
   - A split card calls it with `keyOf = statsValue(…, SplitBy)`.
   - A plain line calls it with the constant key `name`, and gets a zero slice when nothing was counted.
   - `statsLine(key, values, cumulative, hidden)` builds the line and applies `runningSum`.
3. **Fields.** `statsFields map[string]statsField{value, order}` holds the five fields.
   - `statsValue` and `statsOrder` read from it.
   - `task.go` gets `Priorities()` and `Statuses()` (in the style of `Resolutions()`) and a generic `strs[T ~string]([]T) []string`. `ResolutionStrings` is built on `strs`.
   - `IsValidStatus` / `IsValidPriority` become `slices.Contains`, and `Priority.Order` becomes `slices.Index` (99 for an unknown value).
4. **Web.**
   - `const dayFormat = "Jan 2"`.
   - In `newStatsChart`, points are built either for n == 1 or for the general case, never both.
   - `.stats-title` in `input.css` and both templates; rebuild with `scripts/build-css.sh`, and add the class to `TestAppCSSDefinesStatsClasses`.
5. **Config.** `applyDefaults` gets `bases := trimList(c.Git.BaseBranches)`.
6. **Tests.**
   - `laneSection` → `sectionAfter(t, body, marker)`. It is used by the lanes and by `TestBoardDoneColumnRendersLines`.
   - Tests that use `card.Metric` check `Field` and `Lines[i].Cumulative` instead.

Rejected: `humanize` in the split title, because it changes the text (see research).
