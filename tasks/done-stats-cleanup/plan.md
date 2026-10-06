# Plan: done-stats-cleanup

This is one commit, the squash `complete_task` makes onto the parent's wip branch. The steps follow the layers; the tests stay green after each one.

1. **task.go:** `Priorities`, `Statuses`, `strs`; `IsValid*` and `Order` built on them. Gate: `go test ./internal/task`.
2. **task/stats.go:** `statsFields`, `countDays`, `statsLine`, `StatsLine.Cumulative`, and remove `StatsCard.Metric`; update `stats_test.go`. Gate: `go test ./internal/task`.
3. **web/view.go:** `Cumulative`, `dayFormat`, the n == 1 branch; update `view_test.go`.
4. **CSS:** `.stats-title` in `input.css` and both templates, `scripts/build-css.sh`, `assets_test`.
5. **config.go:** `trimList`.
6. **handlers_test:** `sectionAfter`.
7. **Docs:** in CLAUDE.md, mention `Cumulative` where the lines groups are described, if that text names the suffix.
8. **Gates:** `gofmt`, `go vet ./...`, `go build ./...`, `go test ./...`, `go test -race ./internal/web ./internal/task`. Compare the `/board` HTML before and after; the only expected difference is the h3 class.
