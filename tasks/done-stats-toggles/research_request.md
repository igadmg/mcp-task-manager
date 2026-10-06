# Research request: done-stats-toggles

**Additional research needed: 70 / 100**

## Already covered by research.md
- The poll replaces `#board` with `hx-swap="outerHTML"` every 5 s, with no `hx-select`.
- htmx 2.0.4 settle (read from the minified code): `attributesToSettle` class/style/width/height, 20 ms, and the server value wins on elements that have an `id`.
- The `htmx:load`, `htmx:afterSwap` and `htmx:afterSettle` events and `hx-preserve` exist.
- htmx uses `localStorage` for `htmx-history-cache` (10 body snapshots).
- `app.js`: one IIFE with a delegated listener for `[data-copy]`; static files are served immutable (a hard reload is needed after an update).
- A localStorage origin depends on host and port, and the port can be random (`127.0.0.1:0`).

## Gaps (largest)
1. **Check htmx behaviour in a real browser, not from minified code.**
   - Does a hidden line flash for one frame after a poll if it is re-applied on `htmx:afterSettle` or `htmx:load`?
   - Which event fires earliest without flashing: `htmx:beforeSwap` or `htmx:oobBeforeSwap`? Or edit the HTML in `htmx:beforeSwap` (`evt.detail.serverResponse`)?
   - Does `hx-preserve` work on a legend or chart node whose data has to refresh? (Probably not.)
   - Use `/run` plus the browser skill or Chrome.
2. **Alternatives to re-applying through JS:**
   - a class on `#board` or `<body>` plus CSS selectors such as `[data-hide~="card:line"]` (survives the swap if it sits outside `#board`);
   - a `<style>` element maintained by JS outside the swap zone. This would remove the flash completely; check how it fits with settle.
3. **History cache.** When going back (`hx-push-url` on card clicks), a body snapshot with old toggles is restored. Is that a conflict, or does it just need a re-apply on `htmx:historyRestore`?
4. **Stability of localStorage keys** across port changes (a random port means a new origin and lost state; is that acceptable?) and across config changes (agree with the card key from done-stats-config).
5. **Testability of JS.** There are no JS tests in the project. Decide what can be checked from Go (markup, attributes, no `hx-*`) and what only by hand in a browser, and record that.
6. **Keyboard and accessibility:** whether the legend should be `<button aria-pressed>` instead of a span, and focus behaviour after the swap (the poll replaces the focused element).
