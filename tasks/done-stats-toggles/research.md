# Research: done-stats-toggles (gap closure)

The base research is `done-column-stats/research.md`; the SVG and escaping facts are in `done-stats-lines/research.md`. This file closes the six gaps in `research_request.md`.

**How it was tested.** I wrote a scratchpad harness: a Go server that serves the repo's vendored `htmx.min.js` (2.0.4) and `app.css`, with a `#board` that polls `/board` every 1 s using `hx-swap="outerHTML"`, like `_board.html:1`. Each response holds a legend, an SVG with three `data-stats-line` polylines whose data changes on every request, and a card link with `hx-push-url`.
- I drove it with headless Chrome 154 over CDP.
- A `requestAnimationFrame` loop counted every frame in which the line meant to be hidden (`closed`) was connected and had a computed `display` other than `none`. Each run lasted 6 s, about 5 swaps and 366 frames.
- Nothing in the repo changed.

### Task Slice

- Keeping a viewer's per-line visibility across the 5 s `outerHTML` poll of `#board`.
- Which htmx 2.0.4 events and mechanisms can re-apply that state, and whether a hidden line flashes.
- How the state interacts with htmx's history cache, focus and `hx-preserve`.
- localStorage scope and key stability (origin and port, card ID, line key).
- What is testable from Go and what needs a browser.

### Confirmed Facts

**Gap 1 and 2: re-applying state across the poll** (measured)

| Strategy | Element ids | Flash frames / total (5 swaps) | Final state |
|---|---|---|---|
| none (baseline) | – | 366 / 366 | visible |
| set `style.display=none` on `htmx:afterSettle` | no | **6** / 367 | hidden |
| same on `htmx:load` | no | **6** / 367 | hidden |
| same on `htmx:afterSwap` | no | **0** / 367 | hidden |
| same on `htmx:afterSwap` | **yes** | **298** / 367 | **visible** (settle reverted it) |
| `afterSwap` **and** `afterSettle` | yes | 0 / 367 | hidden |
| `<style>` element in `<head>`, kept by JS: `[data-stats-card="…"] [data-stats-line="closed"]{display:none}` | no / yes | **0** / 366, 0 / 367 | hidden |
| rewrite `evt.detail.serverResponse` in `htmx:beforeSwap` to add `style="display:none"` | no | **0** / 367 | hidden |
| `hx-preserve` + `id` on the `<svg>` | yes | – | the chart's data stayed at its first render (`points` still had `Y=5` after about 56 server renders) |

- **Event order per poll,** observed: `beforeSwap` → `afterSwap` (target `board`) → `load` (on the new `board`) → `afterSettle`.
- **Settle and ids.** On elements with an `id`, settle reverts JS-set `style` and `class` to the server's values after the settle delay. That confirms the reading of the minified code in `research.md`. Re-applying only on `afterSwap` then leaves the line visible until the next swap. On elements without an `id`, settle does not touch them.
- **`afterSettle` and `load`** fire after the browser has painted the new content once: about 1 flash frame per swap.
- **`htmx:beforeSwap` `serverResponse` is writable** in 2.0.4: the modified string is what gets swapped.
- **`hx-preserve`** keeps the old element as it is, so the chart never refreshes. It is unsuitable for anything carrying data.
- **A `<style>` element outside `#board`** is never swapped, so it hides lines from the moment they are inserted. It does not update attributes inside the board: `aria-pressed` on the legend stayed at the server's `true`.

**Gap 3: history cache** (measured)
- After clicking the `hx-push-url` link, localStorage holds `htmx-history-cache`. sessionStorage is empty.
- On `history.back()`, `htmx:historyRestore` fires and the body snapshot comes back, inline styles included: the restored `closed` polyline had `style="display: none;"`, the value at snapshot time.
- Polling resumed after the restore; the `gen` counter kept rising.
- With the `<style>`-in-head strategy, the restored line was hidden by the head rule (no inline style). htmx snapshots the history element, which is `body`, so `head` is not part of the snapshot.
- In the production board, card links push URLs (`internal/web/templates/_card.html:2-7`).

**Gap 4: localStorage scope and keys**
- localStorage is per origin (scheme, host, port), and the listen address decides the port:
  - The default address is `127.0.0.1:7777` (`DefaultWebAddr`, `internal/config/config.go:39`).
  - It becomes a random port (`127.0.0.1:0`) only when `config.Load()` fails at startup in MCP mode (`cmd/mcp-task-manager/main.go:27-32`, `internal/web/controller.go:30-33`), or when `start_web_ui` is called with another `addr` (`internal/tools/web.go:20-33`).
  - `127.0.0.1` and `localhost` are different origins.
- **Card key.** `StatsCard.ID`:
  - when derived it is content-based, unique and slugged: `bars-<field>`, `lines-<days>d`, `lines-<split>-<metric>-<days>d`, then `-2`, `-3`;
  - an explicit `id:` is kept as written, only trimmed, so it may contain spaces, quotes or `/` (`internal/config/stats.go:107-110,152-154,183-200`);
  - reordering cards or editing a title keeps the ID; changing a lines card's days, split or metric changes a derived ID (`tasks/done-stats-config/design.md:157-172`).
- **Line key.** `StatsLine.Key`:
  - a line name, or a raw field value; type values are user-defined and case-sensitive, and `created_by` is free text (`internal/task/stats.go:46-54,266-284`);
  - split lines exist only for values with counts in the window, so a key can vanish from the markup and reappear on a later day (`stats.go:122-151`).
- **Server defaults.** `StatsLine.Hidden` is the config's initial state: an exact match against `hidden` (`stats.go:119`). The server renders it on every poll.
- **Escaping.** html/template escapes arbitrary IDs and keys safely in plain `data-*` attributes (`my card/"x"` → `my card/&#34;x&#34;`). Names under `data-url`, `data-uri`, `data-src…` and `data-on…` switch to URL or JS context (`done-stats-lines/research.md`, Gap 1).
- **Existing storage use.** The only other localStorage user is htmx's `htmx-history-cache` (`research.md`). `app.js` uses no storage today (`internal/web/static/app.js:1-54`).

**Gap 5: testability**
- There are no JS tests and no JS tooling in the repo. The only workflow is `release.yml` (`research.md`).
- Go-side guards exist for the markup:
  - `TestCopyButtonHasNoHtmxAttributes` regex-scans `<button …copy-btn…>` tags for `hx-` (`internal/web/branch_test.go:111-128`);
  - `TestNoMutatingRoutes` (`handlers_test.go:247-270`) and `TestNoExternalAssetReferences` (`:221-245`).
- `app.js` is embedded (`internal/web/assets.go:12`) and served immutable (`assets.go:23-26`). A Go test can read it through `staticFS`, as `assets_test.go` does for `app.css` (`assets_test.go:15-45`).
- **What only a browser shows:** the flash, the settle revert, the history restore, focus. Headless Chrome over CDP works on this machine (Chrome 154, node 26 with a built-in WebSocket), but it is not a repo dependency.

**Gap 6: keyboard and focus** (measured)
- I focused a legend `<button>` and waited one poll:
  - **without an `id`**, `document.activeElement` became `BODY`, so focus was lost;
  - **with an `id`**, focus moved to the new button with the same id (a different node). htmx restores focus by id.
- `<button type="button" aria-pressed="{{.Bool}}">` renders as `aria-pressed="true"` under html/template (`done-stats-lines/research.md`).
- The existing delegated listener matches `[data-copy]` and calls `preventDefault` and `stopPropagation` (`app.js:37-46`). A legend button inside a card would sit inside no `<a>`: Done stats cards are not task links. **Inference**, since that markup does not exist yet.

### Evidence Map

| Concern | Current implementation / observation | Evidence |
| ------- | ------------------------------------ | -------- |
| Poll | `outerHTML` swap of `#board` every 5 s | `internal/web/templates/_board.html:1` |
| Flash on afterSettle/load | About 1 frame per swap | harness, Chrome |
| No flash | `afterSwap` (no ids); `<style>` in head; `beforeSwap` rewrite | harness, Chrome |
| Settle revert | Id'd elements lose JS-set style after settle | harness (298/367 frames) |
| hx-preserve | Freezes data | harness |
| History | Snapshot restores inline state; `historyRestore` fires; head not snapshotted | harness |
| Focus | Kept only with a stable id | harness |
| Card key | Derived slugged, explicit raw | `internal/config/stats.go:107-110,152-154` |
| Line key | Line name or raw value; split keys come and go | `internal/task/stats.go:122-151,266-284` |
| Origin | Fixed 7777 by default; random only on config failure or explicit addr | `main.go:27-32`, `controller.go:30-33`, `tools/web.go:20-33` |
| JS tests | None; `staticFS` readable from Go | `assets.go:12`, `assets_test.go` |

### Relevant Files

1. `internal/web/static/app.js`: the one IIFE and delegated listener that the toggle code extends.
2. `internal/web/templates/_board.html`: the poll element (`#board`, `outerHTML`) whose lifecycle drives re-application.
3. `internal/web/templates/layout.html`: `<head>` (outside the swap) and the `defer` script order (htmx before app.js).
4. `internal/task/stats.go` and `internal/config/stats.go`: where card IDs and line keys come from, and their stability.
5. `internal/web/branch_test.go:111-128` and `assets_test.go`: the patterns for "no hx-*" and embedded-asset guards.
6. `done-stats-lines/research.md`: the data-attribute naming and escaping facts the markup must follow.

### Inference

- **Flash-free options.** Measured, the flash-free choices are: re-apply on `afterSwap` without ids on the toggled elements (or on `afterSwap` plus `afterSettle` with ids); a head `<style>` rule set; or rewriting the response in `beforeSwap`. A head rule needs any card ID or line key that goes into a selector to be escaped (for example `CSS.escape`), because explicit IDs and field values are arbitrary strings. This is inferred from the escaping facts; the escaping was not exercised in the browser.
- **Stale history snapshots.** If a viewer toggles after a snapshot was taken, a back navigation restores the older inline state. Strategies that write into the board DOM would then need a re-apply on `htmx:historyRestore`. The head-rule strategy is unaffected, because it lives outside the snapshot. The staleness case itself was not reproduced; the snapshot keeping inline state was.
- **Split keys that vanish.** A stored "hidden" entry for a split value that disappears from the window simply matches nothing until the value reappears. Stored keys can therefore accumulate.
- **Port-change loss.** Toggle state is lost only on an origin change (random port after a config error, a different `addr`, or `localhost` versus `127.0.0.1`). With the default config the port is stable.

### Unknown

- Whether the htmx history restore runs in exactly the same order on every browser engine: only Chrome was tested, not Firefox or Safari.
- The keyboard and ARIA expectations for the legend (button with `aria-pressed` versus a checkbox) are not specified in `task.md`.
