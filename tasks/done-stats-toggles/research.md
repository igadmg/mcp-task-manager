# Research: done-stats-toggles

## Confirmed facts
- `internal/web/static/app.js` is a single IIFE with one delegated `document` click listener for `[data-copy]`. It has no htmx hooks and no storage use.
- `_stats_lines.html` already renders the toggle hooks:
  - the card `<section class="stats-card" data-stats-card="{{ .ID }}">`;
  - the legend `<button type="button" class="stats-legend-item" data-stats-line="<key>" aria-pressed="true|false">`;
  - each `<polyline class="stats-line <color>[ stats-off]" data-stats-line="<key>">` inside nested `<svg>` groups.
  - Server defaults come from config `hidden`: a hidden line gets `stats-off` and `aria-pressed="false"`.
- The CSS is already in place (`assets/input.css:99-117`, checked by `assets_test.go:59-60`): `.stats-off { display: none }`, `.stats-legend-item[aria-pressed="false"]` dimmed and struck through, `cursor-pointer` on the legend item. **No CSS rebuild is needed.**
- The board polls with `hx-get="/board" hx-trigger="every Ns" hx-swap="outerHTML"` on `#board` (`_board.html:1`). Every poll replaces the legend buttons and polylines with fresh server markup, so they come back in their config-default state.
  - None of these elements has an `id`, so htmx's settle class/style copy does not apply. The reset is the plain replacement.
- htmx 2.0.4 fires `htmx:load` on new content and `htmx:afterSettle` after settle. It also fires `htmx:historyRestore` when it restores a body snapshot from `localStorage["htmx-history-cache"]`.
- Card ids are stable, content-derived strings (`bars-priority`, `lines-14d`, a custom `id`), unique within the list (`config/stats.go`). They are free text when the user sets them.
- Scripts load with `defer` (`layout.html:9`), so `app.js` runs after parsing, when the initial board is already in the DOM.
- `app.js` is embedded and served as immutable (`assets.go`). An open browser needs a hard reload to pick up a new version, the same as for CSS.
- Existing tests:
  - `TestCopyButtonHasNoHtmxAttributes` (`branch_test.go:110`) is the pattern for a "no hx-*" markup test.
  - `TestBoardDoneColumnRendersLines` checks the lines card for `hx-` as a substring.
  - There is no JS test harness in the repo (Go only).
- Docs to update:
  - README Web Dashboard bullet (`README.md:99`) and the `id` paragraph (`README.md:510-513`, which already mentions toggles);
  - CLAUDE.md Web UI "Lines charts" bullet ("Hooks for the toggles…").

## Unknowns / inferred
- Event target of `htmx:afterSettle` for an outerHTML swap (the old element or the new one). This is avoided by re-applying over the whole document, which is cheap: a few cards.
