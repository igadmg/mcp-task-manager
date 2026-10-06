# Implementation: done-stats-cleanup

## Done
1. `task.StatsLine.Cumulative` is set in `statsLines`. `task.StatsCard.Metric` is removed. `newStatsChart` picks the group by `l.Cumulative` and no longer uses `strings.HasSuffix`.
2. `countDays` is the only counting loop; `statsLines` builds every line through a local `line()` helper (zero fill, runningSum, Hidden, Cumulative).
3. `statsFields` (value + order) replaces the two switches. `task.go` gains `Priorities()`, `Statuses()` and `strs[T ~string]`.
   - `IsValidStatus`, `IsValidPriority` and `IsValidResolution` now use `slices.Contains`.
   - `Priority.Order` uses `slices.Index`.
   - `ResolutionStrings` uses `strs`.
4. Web:
   - `dayFormat` constant;
   - the one-day points branch comes first;
   - `.stats-title` in `input.css` and both templates, with the CSS rebuilt (the diff is the one new rule) and `assets_test` updated.
5. `applyDefaults` uses `trimList` for `base_branches`.
6. Tests:
   - `sectionAfter(t, body, marker)` is shared; `laneSection` wraps it;
   - the lines-card test uses it;
   - the tests that used `Metric` now check `Cumulative` and `Field`.
7. CLAUDE.md: the Lines charts and Done statistics bullets now name `Cumulative`, `Priorities()` / `Statuses()` and `statsFields`.

Not done: `humanize` for the split title. It would change the text ("by Created by"), as found in research.

## Verification
- `gofmt -l .` is clean. `go vet ./...`, `go build ./...` and `go test ./...` pass, and `go test -race ./internal/web ./internal/task` passes.
- I compared `/board` HTML from this repo's backlog before and after (`serve web`). The only differences are the h3 class (`stats-title`) and the relative and clock times.
