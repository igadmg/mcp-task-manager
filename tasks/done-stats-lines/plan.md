# Plan: done-stats-lines

### Planning Summary

Two phases following `design.md`: the feature slice (view model, partial,
CSS, regenerated `app.css`, tests), then the docs. Under git branching
`complete_task` squashes the work, so the phases are verification boundaries,
not separate agent commits.

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| P1 | Done column draws lines cards | `view.go`, `_board.html`, `_stats_lines.html`, `input.css`, `app.css`, web tests | app.css | – | `gofmt`, `go vet`, `go build`, `go test -race ./internal/web/... ./internal/task/...`, `go test ./...`, browser check |
| P2 | Docs | `CLAUDE.md`, `README.md` | – | P1 | reread against code |

### Phase Details

- `Phase ID:` P1
- `Goal:` every configured lines card renders as an SVG chart with legend,
  labels, scales and per-day tooltips; hidden lines start off.
- `Why this phase exists:` it is the feature; mapping, template, CSS and
  handler tests only verify together.
- `Files likely to change:`
  - `internal/web/view.go`: `StatsCardView.Kind`, `.Chart`;
    `StatsChartView`, `StatsLineGroupView`, `StatsSeriesView`,
    `StatsDayView`; `newStatsChart`; `newStatsCards` maps both kinds;
  - `internal/web/templates/_board.html`: `eq .Kind "lines"` branch;
  - `internal/web/templates/_stats_lines.html`: new;
  - `internal/web/assets/input.css`: `.stats-chart`, `.stats-line`,
    `.stats-off`, `.stats-day`, `.stats-legend`, `.stats-legend-item`,
    `.stats-swatch`, `.series-*`;
  - `internal/web/static/app.css`: regenerated;
  - `internal/web/view_test.go`, `handlers_test.go`, `assets_test.go`.
- `Regeneration required:` yes, `scripts/build-css.sh`.
- `Dependencies:` none (bars landed).
- `Implementation tasks:`
  1. View types and `newStatsChart` (groups, Max ≥ 1, integer points,
     single-day segment, colours, day titles).
  2. `newStatsCards` maps lines cards; replace
     `TestStatsCardsKeepBarsInConfigOrder` with `TestStatsCardsKeepConfigOrder`.
  3. Partial and board branch.
  4. CSS components; rebuild `app.css`; diff shows only the new rules and
     colour variables.
  5. Tests: `TestStatsChartPoints`, `TestStatsChartGroupsByKind`,
     `TestStatsChartZeroAndSingleDay`, `TestStatsChartColours`,
     `TestStatsChartHiddenAndTitles`, `TestStatsChartNoLines`,
     `TestBoardDoneColumnRendersLines`, `TestBoardHiddenLineRendersOff`,
     `TestStatsLinesEscaping`, `TestAppCSSDefinesStatsClasses` extended.
  6. Browser check of `serve web` against this repo (headless Chrome
     screenshot if available).
- `Tests and checks:` `gofmt -l internal/web`, `go vet ./...`,
  `go build ./...`, `go test -race ./internal/web/... ./internal/task/...`,
  `go test ./...`.
- `Commit boundary:` the whole slice, CSS included.
- `Definition of done:` gates green; `/board` on the seeded backlog shows the
  `lines-14d` chart with two polylines and no `style=`.
- `Rollback note:` revert the slice; lines cards disappear again, bars stay.

---

- `Phase ID:` P2
- `Goal:` docs describe the lines charts.
- `Why this phase exists:` CLAUDE.md Web UI is the contract for
  `done-stats-toggles`, which needs the hooks (`data-stats-line`,
  `stats-off`, `aria-pressed`).
- `Files likely to change:` `CLAUDE.md` (Done column bullet: lines drawn; new
  "Lines charts" bullet), `README.md` (Web Dashboard bullet).
- `Regeneration required:` no.
- `Dependencies:` P1.
- `Implementation tasks:` edit the docs.
- `Tests and checks:` reread against the code.
- `Commit boundary:` docs only.
- `Definition of done:` docs match behaviour.
- `Rollback note:` revert the docs.

### Cross-Phase Risks

- `app.css` is served immutable: a manual check needs a hard reload.
- Handler tests use the real clock; assertions must not depend on dates.

### Execution Order Rationale

P1 → P2: the docs describe what P1 shipped. Nothing to parallelize.
