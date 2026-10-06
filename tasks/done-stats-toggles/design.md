# Design: done-stats-toggles

## Scope
Client-side legend toggles for lines cards, in `internal/web/static/app.js`. There are no server, template or CSS changes: the hooks and the `stats-off` / `aria-pressed` styling already exist.

## Behaviour
- **Click.** A second delegated `document` click listener matches `.stats-legend-item[data-stats-line]` inside `[data-stats-card]`.
  - It flips the button's `aria-pressed`.
  - It toggles `stats-off` on every `polyline[data-stats-line="<key>"]` of the same card.
  - It records the choice.
  - It does not call `preventDefault` or `stopPropagation`: a button of type `button` has no default action, and the stats cards sit outside any `hx-get` card.
- **State.** The key is `mcp-task-manager.stats-line:` + `JSON.stringify([cardId, lineKey])`. The JSON array makes the key unambiguous whatever characters a custom card id holds.
  - The value is `"on"` or `"off"`.
  - Only lines the viewer clicked are stored. A line with no entry keeps whatever the server rendered (the config `hidden` default).
  - Keying by card id and line key means that reordering or retitling cards, or adding lines, leaves existing choices valid. A stale key, for a removed card or line, is simply never read.
  - The prefix is our own and never touches `htmx-history-cache`.
- **Storage fallback.** An in-memory map mirrors every choice. Reads check memory first, then `localStorage`. Every `localStorage` access, including the `window.localStorage` getter, which throws when storage is blocked, is wrapped in try/catch. Without storage, toggles still survive polls for the life of the page, and only a reload drops them back to the config defaults.
- **Re-apply.** `applyAll()` walks every `[data-stats-card]` legend button in the document and applies any recorded choice to the button and its polylines. It is idempotent: it sets state explicitly and never flips.
  - It runs once at script start (the script is deferred, so the initial board is already parsed).
  - It runs on `htmx:afterSettle`, `htmx:load` and `htmx:historyRestore`, all on `document`.
  - Scanning the whole document, rather than `event.target`, avoids depending on which element htmx targets for an outerHTML swap; the cost is a handful of elements.
- **Read-only.** No request API (`fetch`, `XMLHttpRequest`, `htmx.ajax`) and no `hx-*` attribute is added.

## Rejected alternatives
- *One JSON blob per card*: needs read-modify-write and parse-error handling, for no gain.
- *Storing the full visible set*: a config change that adds a line would then hide it. Storing only clicked lines keeps new lines at their config default.
- *`hx-preserve` on the legend and chart*: the server data, the polyline points, would then stop refreshing.
- *A server round-trip or query parameter*: it breaks the read-only and no-request rule.

## Tests (Go)
- `TestStatsLegendHasNoHtmxAttributes`: on `/` and `/board`, every `stats-legend-item` button and `stats-line` polyline tag carries no `hx-`, and the legend exists.
- `TestAppJSStatsToggles`: the embedded `app.js` contains the storage prefix and the three htmx event names, and none of `fetch(`, `XMLHttpRequest` or `htmx.ajax`. This guards the read-only rule without a JS runtime.

## Docs
- README Web Dashboard: clicking a legend entry toggles the line; the choice is kept per browser in localStorage.
- CLAUDE.md Web UI: a "Line toggles" bullet covering the key format, the memory fallback, the re-apply events, and the fact that no request is sent.

## Risks
- `app.js` is cached as immutable, so open browsers need a hard reload. This is already documented for CSS; mention it for app.js as well.
