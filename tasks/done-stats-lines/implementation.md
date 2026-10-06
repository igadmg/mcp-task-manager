# Implementation: done-stats-lines

## P1 — feature slice (done)

- `internal/web/view.go`: `StatsCardView.Kind`, `.Chart`; `StatsChartView`,
  `StatsLineGroupView` (`Cumulative`, `Width`, `Peak`, `Max`), `StatsSeriesView`,
  `StatsDayView`; `newStatsChart`; `newStatsCards` maps bars and lines cards
  (unknown kinds still dropped).
- `templates/_stats_lines.html` (new), `_board.html` branches on `.Kind`.
- `assets/input.css`: `.stats-chart` (+ `.stats-chart svg {overflow: visible}`),
  `.stats-line`, `.stats-off`, `.stats-day`, `.stats-legend`,
  `.stats-legend-item` (+ `[aria-pressed=false]`), `.stats-swatch`,
  `.series-<name>`, `.series-0..7`. `app.css` regenerated: selector diff is only
  the new classes plus `.justify-between` (used by the date row).

### Deviations from design.md
- `Peak` added next to `Max`: the label shows the real peak (0 for an
  all-zero group), the viewBox uses `Max = max(Peak, 1)`.
- Each group carries `Width` (instead of `$.Chart.Width` in the template).
- Labels: the date row holds first/last day; the scales (`5/day · 25 total`,
  no "peak"/"total" prefix words) are on their own centered line, because at
  1024 px they truncated between the dates.
- The nested svg overflow is a CSS rule, not an `overflow="visible"`
  attribute (the attribute made Tailwind emit a stray `.visible`).
- Legend `<li>` is `flex` so rows are 11px-line tall.

### Verification
- `gofmt -l` clean, `go vet ./...`, `go build ./...`,
  `go test -race ./internal/web/... ./internal/task/...`, `go test ./...`: green.
- New tests: `TestStatsCardsKeepConfigOrder` (replaces the bars-only one),
  `TestStatsChartPoints`, `TestStatsChartGroupsByKind`,
  `TestStatsChartZeroAndSingleDay`, `TestStatsChartColours`,
  `TestStatsChartHiddenAndTitles`, `TestStatsChartNoLines`,
  `TestBoardDoneColumnRendersLines`, `TestBoardHiddenLineRendersOff`,
  `TestStatsLinesEscaping`; `TestAppCSSDefinesStatsClasses` extended.
- Headless Chrome screenshots of `serve web` on a copy of this backlog at
  1024 and 1440 px with four cards (bars, 14-day plain + cumulative with one
  hidden, 30-day split by priority, 1-day): lines, legend (struck-through
  hidden entry), two scales, one-day flat segments all render.
- Not checked: native `<title>` tooltips on hover (headless Chrome has none).

## P2 — docs (done)
- CLAUDE.md Web UI: Done column bullet (both kinds), new "Lines charts" bullet
  with the geometry, scales, colours and toggle hooks.
- README Web Dashboard bullet: line charts.
