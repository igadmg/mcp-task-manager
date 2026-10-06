# Implementation: done-stats-bars

### Phase Summary

All three phases of `plan.md` were executed in order:
- **P1:** a deterministic Tailwind scope;
- **P2:** the Done column renders bars cards;
- **P3:** the docs.

All are complete and verified.

### Lead Checklist

- [x] Followed `design.md`: count-scaled inline SVG, `ColumnView.Stats` for
  Done only, nil `Cards` for Done, bars cards only, and `source(none)`.
- [x] Regenerated `app.css` with `scripts/build-css.sh` after each CSS or
  template change; never edited it by hand.
- [x] Every gate was run, and every gate was green.

### Coder Report

- **P1**
  - `input.css` imports Tailwind with `source(none)`, and its comment says why.
  - The `build-css.sh` comment now says that only the templates are scanned.
  - `app.css` was rebuilt. The selector diff against the previous build is
    exactly 19 removed and 0 added: `.absolute .collapse .contents .filter
    .fixed .grow .hidden .inline .invert .invisible .isolate .lowercase
    .relative .resize .ring .static .table .transition .visible`. None of them
    is used. The design said 20; that was a miscount, now fixed.
- **P2: view model** (`view.go`)
  - It adds `ColumnView.Stats`, `StatsCardView`, `StatsBarView` and
    `newStatsCards`.
  - `newStatsCards` keeps only bars cards and drops rows with `Total <= 0`. It
    computes `Open`, `RecentX` and `TodoX`.
  - The Done column gets `Stats` and nil `Cards`; its `Count` is unchanged.
- **P2: templates and CSS**
  - `_board.html` has the new `else if .Stats` branch.
  - `_stats_bars.html` is new.
  - `input.css` gains `.stats-card`, `.stats-row`, `.stats-bar`,
    `.stats-recent` and `.bar-{done,recent,in_progress,todo}`.
  - `app.css` was rebuilt. It adds those classes, plus `.gap-x-2` from the
    partial and `--color-emerald-200` / `-300`.
- **P2: tests**
  - Rewritten:
    - `TestBoardRendersThreeColumns`: "Old chore" must be absent;
    - `TestBoardShowsBranchChip`: only task 3's chip and 1 copy button on the
      board, with task 4's branches in its detail view only;
    - `TestCopyButtonHasNoHtmxAttributes`: the branch is now on task 3;
    - `TestBoardEmptyInProgressShowsFourStubs`: 0 `empty` stubs, plus a stats
      card;
    - `TestLanesPartitionTheColumn`: done task `e` has no card in any column.
  - New in `view_test.go`:
    - `TestDoneColumnHasStatsNotCards`
    - `TestStatsCardsKeepBarsInConfigOrder`
    - `TestStatsBarOffsets`
    - `TestStatsBarSkipsEmptyTotal`
  - New in `handlers_test.go`:
    - `TestBoardDoneColumnRendersStats`: it also asserts that the board has no
      `style=`;
    - `TestBoardDoneColumnEmptyWithoutCards`
    - `TestStatsBarsEscaping`
  - New in `assets_test.go`: `TestAppCSSDefinesStatsClasses`.
- **P3**
  - CLAUDE.md, Web UI section:
    - a new "Done column" bullet;
    - in the Branch chip bullet, the final branch is now visible only in the
      detail view;
    - in the Assets bullet, the build scope.
  - README: a Web Dashboard bullet.

### Reviewer Findings

No findings:
- no architecture drift;
- `internal/task` and `internal/config` are untouched;
- the handlers are still GET-only and never resolve;
- there is no `template.HTML` and no inline style;
- the only new template data is plain strings and ints.

### Tester Report

- `gofmt -l ./internal ./cmd`: clean.
- `go vet ./...`, `go build ./...`: ok.
- `go test -race ./internal/web/... ./internal/task/...`: ok.
- `go test ./...`: ok, all packages.
- **Manual check.** I ran `serve web` on a copy of this backlog and took
  headless Chrome screenshots at 1024 and 1440 px.
  - The three bars cards appear in config order, and the header count is 6.
  - At 1024 px the label wraps under the value; at 1440 px it fits on one
    line.
  - The segment colours read well on the dark board, and the +2 highlight is
    visible inside done.

### Quality Gate Result

PASS.

### Files Changed

- `internal/web/view.go`
- `internal/web/templates/_board.html`
- `internal/web/templates/_stats_bars.html` (new)
- `internal/web/assets/input.css`
- `internal/web/static/app.css` (regenerated)
- `scripts/build-css.sh`
- `internal/web/view_test.go`, `handlers_test.go`, `branch_test.go`,
  `assets_test.go`
- `CLAUDE.md`, `README.md`

### Deviations From Plan

- The design's count of selectors dropped by `source(none)` was 20; it is 19.
  The design and the plan are corrected.
- `TestStatsBarsEscaping` renders the partial directly instead of seeding a
  hostile `created_by`, as the revised design describes.

### Next Phase Handoff

`done-stats-lines` can add its partial behind a kind switch in the Done
branch of `_board.html`, and extend `newStatsCards`, which today keeps only
`config.StatsKindBars`.
