# Plan: done-stats-bars

### Planning Summary

The work follows `design.md` in three phases. The first makes the Tailwind
build deterministic before any new class goes in. The second adds the view
model, the template and the CSS, and rewrites the tests. The third updates the
docs. Each phase builds and passes the tests on its own.

Under git branching, `complete_task` squashes the work, so the phases are
verification boundaries rather than separate agent commits.

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| P1 | Deterministic Tailwind scope | `assets/input.css`, `scripts/build-css.sh`, `static/app.css` | app.css | – | selector diff = 19 known unused; `go test ./internal/web/...` |
| P2 | Done column renders bars cards | `view.go`, `_board.html`, `_stats_bars.html`, `input.css`, `app.css`, web tests | app.css | P1 | `gofmt`, `go vet`, `go build`, `go test -race ./internal/web/... ./internal/task/...`, `go test ./...` |
| P3 | Docs | `CLAUDE.md`, `README.md` | – | P2 | reread; `go test ./...` |

### Phase Details

- `Phase ID:` P1
- `Goal:` make `app.css` depend only on `input.css` and the templates.
- `Why this phase exists:` P2 must rebuild `app.css`. A rebuild from the repo
  root today also pulls in noise classes from `tasks/*.md`. Isolating the
  scope change keeps P2's CSS diff limited to the stats classes.
- `Files likely to change:`
  - `internal/web/assets/input.css`: `@import "tailwindcss" source(none);` and
    a corrected header comment;
  - `scripts/build-css.sh`: its scan-scope comment;
  - `internal/web/static/app.css`: regenerated.
- `Regeneration required:` yes, run `scripts/build-css.sh`.
- `Dependencies:` none.
- `Implementation tasks:` edit the import and the comments, run the script, and
  diff the selector sets against the previous `app.css`.
- `Tests and checks:`
  - The only removed selectors are the 19 listed in the design, and none is
    added.
  - `go test ./internal/web/...`
- `Commit boundary:` the scope change and its regenerated CSS.
- `Definition of done:` the selector diff is as expected, and the web tests
  pass.
- `Rollback note:` revert both files. Nothing references the dropped classes.

---

- `Phase ID:` P2
- `Goal:` the Done column shows the configured bars cards instead of task cards.
- `Why this phase exists:` it is the feature. The view model, template, CSS and
  tests only verify together: the handler tests need all of them.
- `Files likely to change:`
  - `internal/web/view.go`: `ColumnView.Stats`, `StatsCardView`,
    `StatsBarView` and `newStatsCards`; in `newBoardView`, the Done column gets
    `Stats` and nil `Cards`;
  - `internal/web/templates/_board.html`: the `else if .Stats` branch;
  - `internal/web/templates/_stats_bars.html`: new;
  - `internal/web/assets/input.css`: `.stats-card`, `.stats-row`,
    `.stats-bar`, `.bar-*`, `.stats-recent`;
  - `internal/web/static/app.css`: regenerated;
  - `internal/web/view_test.go`, `handlers_test.go`, `branch_test.go`,
    `assets_test.go`.
- `Regeneration required:` yes, `scripts/build-css.sh` after the template and
  CSS edits.
- `Dependencies:` P1.
- `Implementation tasks:`
  1. Add the view types and the mapper. The mapper keeps only bars cards and
     drops any row with `Total <= 0`. It computes `Open`, `RecentX` and
     `TodoX`.
  2. Make the Done column use Stats instead of Cards.
  3. Add the partial and the board branch.
  4. Add the CSS components and rebuild.
  5. Add tests:
     - `TestDoneColumnHasStatsNotCards`
     - `TestStatsCardsKeepBarsInConfigOrder`
     - `TestStatsBarOffsets`
     - `TestStatsBarSkipsEmptyTotal`
     - `TestBoardDoneColumnRendersStats`
     - `TestBoardDoneColumnEmptyWithoutCards`
     - `TestStatsBarsEscaping`
     - `TestAppCSSDefinesStatsClasses`
  6. Rewrite these tests:
     - `TestBoardRendersThreeColumns`
     - `TestBoardShowsBranchChip`
     - `TestCopyButtonHasNoHtmxAttributes`
     - `TestBoardEmptyInProgressShowsFourStubs`
     - `TestLanesPartitionTheColumn`, which gains a check that the done task
       appears on no card
- `Tests and checks:`
  - `gofmt -l internal/web`
  - `go vet ./...`
  - `go build ./...`
  - `go test -race ./internal/web/... ./internal/task/...`
  - `go test ./...`
- `Commit boundary:` the whole feature slice, CSS included.
- `Definition of done:`
  - All the gates are green.
  - `/board` on the seeded backlog shows three `data-stats-card` sections and
    no "Old chore".
- `Rollback note:` revert the slice. The service data stays unused but harmless.

---

- `Phase ID:` P3
- `Goal:` the docs describe the new Done column.
- `Why this phase exists:` the CLAUDE.md Web UI rules are the project's
  contract for later subtasks (`done-stats-lines`, `done-stats-toggles`).
- `Files likely to change:`
  - `CLAUDE.md`, Web UI section:
    - a Done-column rendering bullet;
    - in the Branch chip bullet, the final branch is visible only in the
      detail view;
    - the build scope in the Assets bullet;
    - "Done statistics (data)" cross-reference.
  - `README.md`: the Web Dashboard bullet.
- `Regeneration required:` no.
- `Dependencies:` P2.
- `Implementation tasks:` edit the docs.
- `Tests and checks:` reread the docs against the code; `go test ./...`
  unchanged.
- `Commit boundary:` docs only.
- `Definition of done:` the docs match the behaviour.
- `Rollback note:` revert the docs.

### Cross-Phase Risks

- A P2 rebuild would run with the template edits from P2 itself. P1 ensures
  that nothing else leaks into that rebuild.
- `app.css` is served as immutable, so a manual check needs a hard reload.

### Execution Order Rationale

P1 → P2 → P3 is strictly sequential:
- P1 makes P2's regenerated CSS reviewable.
- P3 documents what P2 shipped.

There is nothing to parallelize.
