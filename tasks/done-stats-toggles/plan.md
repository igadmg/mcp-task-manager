# Plan: done-stats-toggles

This is a single phase, delivered as one squash commit on the parent's wip branch through `complete_task`.

1. **app.js.** Add a second IIFE section for the stats toggles: storage helpers (memory map plus a try/catch-wrapped `localStorage`), `setLine`, `applyAll`, a delegated click handler, and listeners for `htmx:afterSettle`, `htmx:load` and `htmx:historyRestore`, plus an initial `applyAll()`. Update the file header comment.
2. **Tests** in `internal/web`:
   - `TestStatsLegendHasNoHtmxAttributes` in `handlers_test.go`, next to the lines tests;
   - `TestAppJSStatsToggles` in `assets_test.go`.
3. **Docs.** README Web Dashboard bullet; CLAUDE.md Web UI "Line toggles" bullet, with an adjusted "Lines charts" hook sentence.
4. **Gates.** `gofmt -l`, `go vet ./...`, `go build ./...`, `go test ./internal/web/... -race`, and the full `go test ./...`. Syntax-check app.js with `node --check` if node is available. Then a manual smoke test: run `serve web`, then curl `/board` and `/static/app.js`.

Rollback: revert the single commit. Nothing server-side changes.
