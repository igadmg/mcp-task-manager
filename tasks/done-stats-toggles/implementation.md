# Implementation: done-stats-toggles

## Changes
- `internal/web/static/app.js`: a second IIFE adds the legend toggles.
  - A delegated click flips `aria-pressed` and `stats-off` on the card's matching polylines.
  - Each choice is stored as `"on"`/`"off"` under `mcp-task-manager.stats-line:["<card>","<line>"]`, with an in-memory mirror. Every `localStorage` access, including the getter, is in try/catch.
  - `applyAll()` re-applies the stored choices on start and on `htmx:afterSettle`, `htmx:load` and `htmx:historyRestore`.
- `internal/web/handlers_test.go`: `TestStatsLegendHasNoHtmxAttributes` checks that no legend button or polyline on `/` or `/board` carries `hx-`.
- `internal/web/assets_test.go`: `TestAppJSStatsToggles` checks that the embedded app.js has the prefix and the three htmx events, and does not use `fetch(`, `XMLHttpRequest`, `htmx.ajax`, `hx-` or `htmx-history-cache`.
- README Web Dashboard bullet; CLAUDE.md Web UI "Line toggles" bullet.
- No template, CSS or server changes: the hooks and styles from `done-stats-lines` were already in place.

## Verification
- `gofmt -l .` is clean. `go vet ./...`, `go build ./...` and `go test ./...` pass, and `go test -race ./internal/web` passes.
- `node --check` on app.js passes.
- A node fake-DOM smoke test (scratchpad, not committed) covers:
  - a click toggles the button and line;
  - a fresh board after `htmx:afterSettle` gets the choices re-applied;
  - a reload restores the choices from storage;
  - with a throwing `localStorage` getter, or no storage at all, the page still works and the memory map carries the state across swaps.
- `serve web` on a scratch port serves the new app.js, and `/board` renders the legend.
- It was not exercised in a real browser.
