# Research request: workspace-tiler

**Additional research needed: 70 / 100**

The highest of the six. The parent's `research.md` mapped the *current* route
table, template sets and panel mechanics thoroughly, but every mechanism this
subtask depends on is one the repository has never used: trailing-wildcard
`ServeMux` patterns, `transform` on a strip, a pinned rail, and `hx-push-url`
on anything other than a single panel swap. Nothing here can be settled by
reading the existing code, because the existing code does none of it.

## Already covered by research.md
- The whole current route table and the structural read-only rule
  (`internal/web/server.go:49-64`), and that `ServeMux` answers non-GET with
  405 by itself.
- `Deps.Project` → `Resolver.Current` only, never `Get`
  (`internal/web/server.go:18-23`).
- The exact htmx idiom in use: `hx-get` + `hx-target` + `hx-swap` +
  `hx-push-url` on a card link (`internal/web/templates/_card.html:2-7`), and
  the self-replacing poll on `#board` (`internal/web/templates/_board.html:1`).
- That a pushed `/tasks/{id}` currently reloads into the standalone detail
  page, so no URL describes "board + open panel"
  (`internal/web/handlers.go:30-36`, `internal/web/templates/detail.html:1-7`).
- `_detail.html` is shared by the panel and the page
  (`internal/web/handlers.go:47`, `internal/web/templates/detail.html:5`).
- The template set wiring and the no-`template.HTML` rule
  (`internal/web/templates.go:16-33`).
- The layout shell: `main` is `mx-auto max-w-[1600px] px-4 py-5`
  (`internal/web/templates/layout.html:20`), board grid
  `lg:grid-cols-[minmax(0,1fr)_22rem]` (`board.html:2`).
- The geometry precedent to follow: `--lane` set by a `lane-<phase>` class,
  container queries, `clamp()` step, Go computing nothing
  (`internal/web/assets/input.css:50-73`).
- No `prefers-reduced-motion` rule exists yet in `input.css` or `app.css`.
- htmx 2.0.4 is the vendored version and the bundle *contains* `hx-preserve`,
  `hx-select`, `hx-swap-oob`, `hx-sync`, `hx-history`, `scroll:` / `show:`,
  `focus-scroll` — existence only; no behaviour was verified.
- The test lists that must grow: `TestNoMutatingRoutes`' hardcoded paths
  (`internal/web/handlers_test.go:256`), `TestNoExternalAssetReferences`'
  two paths (`:229`), the race test's fetch list
  (`internal/web/race_test.go:52-56`).

## Gaps
1. **`ServeMux` pattern capability — blocking.** The chain is a variable-length
   tail, so the route needs `GET /tasks/{id}/w/{rest...}` (Go 1.22+ wildcard)
   or an explicit pattern per depth. Verify: that a `{rest...}` wildcard
   matches an empty tail and a deep tail; precedence against the existing
   `/tasks/{id}` and `/tasks/{id}/panel`; and above all **what `PathValue`
   returns for percent-encoded segments** — a file named `my notes.md`,
   `a+b.md`, `100%.md`, `a#b.md`, or a unicode name. Whether the value arrives
   decoded or raw decides the whole encode/decode layer of the chain model.
   Write a throwaway test against `net/http` before designing anything.
2. **Canonical form and redirects.** `ServeMux` cleans paths and redirects
   (`//`, `/./`, trailing slash). Establish what happens to a chain URL with a
   doubled slash or a trailing slash, since a 301 from the mux would break an
   htmx swap. Decide whether the canonical chain ends with a slash.
3. **URL length.** With unbounded depth and text ids like
   `web-task-workspace`, measure a realistic depth-10 chain and check it
   against browser and server limits, and against `hx-push-url` behaviour. If
   there is a practical cap, the rail has to degrade rather than break.
4. **`transform` versus the layout shell.** `main` is a centred
   `max-w-[1600px]` box with `px-4`. A strip that slides needs to escape it or
   the shell has to change. Determine experimentally: whether `translateX` on
   a child of that box produces a horizontal scrollbar (`overflow-x: clip` vs
   `hidden` vs neither), whether the sticky header still works, and how the
   existing container-query lanes behave inside a transformed ancestor —
   container queries and transforms interact in ways worth checking rather
   than assuming.
5. **The width-by-kind rule in Tailwind v4.** Each kind declares a width and
   the tiler lays out right to left, pushing older columns off the left edge.
   Work out which CSS actually expresses that — `flex` with
   `flex: 0 0 var(--w)` plus a negative start offset, or `grid` with named
   areas, or `margin-left` like the lanes — and confirm it survives the
   `source(none)` + `@source "../templates"` scan
   (`internal/web/assets/input.css:13-15`): any class assembled in Go is
   invisible to the scanner, so the kind widths must be named classes or
   custom properties declared in `input.css`.
6. **How the rail stays pinned while the strip slides.** `position: sticky`
   inside a transformed container is a known trap. Verify whether the rail must
   sit outside the transformed element, and how that interacts with the
   below-`lg` stacked layout where nothing slides.
7. **`hx-push-url` + browser Back across a strip.** The repo pushes URLs today
   but only ever swaps `#panel`. Check what htmx's history cache restores when
   the swap target is a whole strip: whether Back replays the fragment or
   refetches, whether `hx-history-elt` is needed, and whether the restored DOM
   keeps the right `translateX` step. `app.js` already listens for
   `htmx:historyRestore` (`internal/web/static/app.js:156-159`), which is a
   hint that restore does fire — confirm what state it hands back.
8. **The `/tasks/{id}` migration, concretely.** List every test that asserts
   the standalone page (`TestDetailPage`, `TestDetailArchivedTask`,
   `TestDetailUnknownID404`, `TestNoExternalAssetReferences`, plus anything in
   `view_test.go` touching `DetailView` as a page) and decide per test:
   rewrite, move, or delete. Also the README line "A done task stays reachable
   at `/tasks/{id}`" (README:99) and `detailPage` in
   `internal/web/templates.go:24`. Done tasks have no board card, so confirm
   that `/tasks/{id}` for a done or archived task still renders its panel on a
   board that does not list it.
9. **404 vs placeholder when nothing is resolved.** `TestUnresolvedProjectPlaceholder`
   expects `/` and `/board` to render a placeholder and `/tasks/1` to 404
   (`internal/web/handlers_test.go:309-329`). Decide what a workspace chain
   does before the first tool call, and keep that test's contract intact.
