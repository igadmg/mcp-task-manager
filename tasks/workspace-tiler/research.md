# Research — workspace-tiler

Phase: research, run 1. Evidence-only, no design proposed. Builds on the
parent's `tasks/web-task-workspace/research.md` and does not repeat it; this
document answers the nine gaps in `research_request.md`, in order, plus what
changed in the tree since the parent's run.

Every mechanism this subtask depends on is one the repository has never used,
so most gaps could not be settled by reading code. They were settled by
**throwaway probes** written for this run: a `net/http` program against
`httptest` (ServeMux patterns, decoding, path cleaning, chain length,
round-trip), an `html/template` program (URL escaping in `href`), and a real
run of the pinned Tailwind CLI over a candidate `input.css`. The probes live
in the session scratchpad only; nothing was added to the repository, and
`git status` under `internal/` is clean.

Caveat on the probes: the local toolchain is go1.27.1 while `go.mod` declares
`go 1.25.5` (`go.mod:3`). `ServeMux` wildcards are a 1.22 feature and nothing
in the probe results depends on a post-1.25 change, but the numbers below were
observed on 1.27.1, not on 1.25.5.

### Task Slice

- The route table as it stands now and what `ServeMux` does with a
  trailing-wildcard pattern added to it: matching, precedence, registration
  conflicts, path cleaning and redirects.
- The encode/decode layer: what `PathValue` hands a handler, what survives a
  render → click → parse round trip, and which component must do the escaping.
- The strip/rail/width geometry as expressible in this repo's CSS pipeline,
  verified by compiling candidate rules with the pinned Tailwind CLI.
- htmx 2.0.4's history mechanics read from the vendored bundle: what is
  cached, what a restore replaces, and what a cache miss fetches.
- The `/tasks/{id}` migration, test by test, including the two negative
  assertions that a board+panel render could break.
- Out of slice: what a task or file column shows, polling and scroll
  preservation, the graph column, the markdown renderer's internals.

### Confirmed Facts

**What changed since the parent's research**

- The route table has **seven** patterns, not six: `GET /tasks/{id}/files/{name}`
  was added by `8b16e42` and serves one attached file as `text/plain` with
  `X-Content-Type-Options: nosniff` (`internal/web/server.go:58-64`,
  `internal/web/handlers.go:54-68`). The parent's research predates it.
- `_detail.html` now links attached files instead of showing inert chips:
  `href="/tasks/{{ $id }}/files/{{ . }}"` with `target="_blank"`
  (`internal/web/templates/_detail.html:122`).
- The two finished sibling subtasks delivered what this one consumes:
  `task.ValidateAttachedName` / `task.ValidateNameSegment` are exported and
  reject empty, separator-bearing and dots-and-spaces-only names
  (`internal/task/name.go:17-38`), and `markdown.Render` is the single
  exported symbol of a stdlib-only renderer with no unsafe mode
  (`internal/markdown/markdown.go:1-37`).
- Baseline is green: `go test ./internal/web/ ./internal/task/ ./internal/markdown/`
  passes on the current tree (89 s, dominated by `internal/task`).

**Gap 1 — `ServeMux` pattern capability (was blocking)**

- `GET /tasks/{id}/w/{rest...}` registers without conflict next to all seven
  existing patterns; the probe registered the real set plus the wildcard and
  no registration panicked.
- Registering **both** `GET /tasks/{id}/w/` and `GET /tasks/{id}/w/{rest...}`
  panics: *"GET /tasks/{id}/w/ matches the same requests as
  GET /tasks/{id}/w/{rest...}"*. The family is exactly one pattern.
- Precedence is correct and needs no ordering care: `/tasks/42` →
  `GET /tasks/{id}`, `/tasks/42/panel` → `GET /tasks/{id}/panel`,
  `/tasks/42/files/plan.md` → `GET /tasks/{id}/files/{name}`,
  `/tasks/42/w/f/plan.md` → the wildcard.
- The wildcard **matches an empty tail**, but only with the trailing slash:
  `/tasks/42/w/` → `rest=""`. `/tasks/42/w` (no slash) is answered by
  `ServeMux` itself with **307 Temporary Redirect** to `/tasks/42/w/`.
- **`PathValue` returns decoded values, and that is the central fact of the
  encode/decode layer.** `%2F` decodes to a real slash: `/tasks/42/w/f/a%2Fb.md`
  yields `rest="f/a/b.md"`, and `/tasks/a%2Fb/w/f/x.md` yields `id="a/b"`.
  A tail split on `/` **after** decoding is therefore wrong — one `%2F` in a
  file name injects a segment boundary that was never in the URL.
- The raw form is available: `r.URL.EscapedPath()` preserves `%2F`, `%2E%2E`
  and the original case of the escapes. Splitting `EscapedPath()` on `/` and
  `url.PathUnescape`-ing each segment separately is the only split that keeps
  segment identity.
- Decoded `..` reaches the handler when it arrives percent-encoded:
  `/tasks/42/w/f/%2E%2E` → `rest="f/.."` with no redirect, and
  `/tasks/42/w/f/%2e%2e/%2e%2e/etc/passwd` → `rest="f/../../etc/passwd"`.
  `task.ValidateNameSegment` rejects both shapes (`..` is dots-only; a decoded
  `/` is a separator), so validation — not the mux — is what closes this
  (`internal/task/name.go:21-26`).
- **Extracting the tail by searching for `"/w/"` is a real correctness trap.**
  For a task whose id is literally `w`, `/tasks/w/w/f/x.md` gives
  `strings.Index(esc, "/w/")` the *first* `/w/`, yielding `rest="w/f/x.md"`
  instead of `"f/x.md"`. Splitting `EscapedPath()` into segments and taking
  `segs[4:]` is correct for every case probed, including `id="w"` and
  `id="w%2Fw"`.

**Gap 2 — canonical form and redirects**

- `ServeMux` cleans and redirects with **307**, not 301: `/tasks/42//w/f/x.md`,
  `/tasks/42/w/./f/x.md` and `/tasks/42/w/../x.md` each produce a 307 to the
  cleaned path. A literal (unescaped) `.` or `..` segment is also redirected
  (`/tasks/42/w/f/.` → `/tasks/42/w/f`); the percent-encoded forms are not.
- A **trailing slash is not cleaned away and changes the parse**:
  `/tasks/42/w/f/x.md/` reaches the handler with `rest="f/x.md/"`, whose
  segment slice ends in an empty string. So the empty chain `/tasks/{id}/w/`
  needs the trailing slash and a non-empty chain must not have one — two
  different canonical rules, and a stray trailing slash is a malformed chain
  rather than a redirect.
- `?q=1` is stripped from `rest` as expected; the query never reaches the
  chain parse.

**Gap 3 — URL length (not a constraint)**

- Measured against this repository's real corpus (26 active task directories,
  66 attached files; longest id `in-progress-phase-columns` at 25 chars,
  longest attached name `research_request.md` at 19 chars), a worst-case
  depth-10 chain of alternating task and file columns is **270 characters**
  and is matched and served normally.
- That is three orders of magnitude below any browser or server limit, so the
  rail has nothing to degrade for. Go's own limit on the request line is
  `MaxHeaderBytes`, 1 MB by default, which no realistic chain approaches.

**Gap 4 / 5 / 6 — the CSS, compiled rather than reasoned about**

- The pinned Tailwind CLI is **already cached locally** at
  `.cache/tailwindcss-macos-arm64-v4.3.3` (80 MB), so `scripts/build-css.sh`
  runs offline on this machine; it only downloads when the cache is absent
  (`scripts/build-css.sh:38-45`).
- A candidate `input.css` compiled cleanly with that exact CLI, which settles
  what the pipeline can express:
  - A component class driving geometry from a custom property set by a named
    class — the `.lane` / `--lane` precedent (`internal/web/assets/input.css:50-73`)
    — compiles verbatim:
    `.strip{…--col:0;--col-w:22rem;transform:translateX(calc(-1 * var(--col) * (var(--col-w) + 1.25rem)));transition:transform .24s ease-out;display:flex}`.
  - `@media (prefers-reduced-motion: reduce)` **inside `@layer components`**
    survives the build: `@media (prefers-reduced-motion:reduce){.strip{transition:none}}`.
    The `motion-reduce:` variant used from a template works too
    (`@media (prefers-reduced-motion:reduce){.motion-reduce\:transition-none{…}}`).
    Both routes are available; neither exists in the tree today.
  - `overflow-x-clip` is a real utility in v4.3.3 → `.overflow-x-clip{overflow-x:clip}`.
  - An arbitrary-property variant such as `lg:[--col-w:26rem]` compiles, so a
    per-breakpoint width is expressible without Go computing anything.
  - Per-kind width classes (`.kind-task`, `.kind-file`, `.kind-graph`) as
    `flex: 0 0 <w>` component classes compile and, being declared in
    `input.css`, are immune to the `source(none)` + `@source "../templates"`
    scan that would otherwise miss a class assembled in Go
    (`internal/web/assets/input.css:13-15`).
- The compiled `app.css` today contains **no** `prefers-reduced-motion` rule,
  no `overflow-x:clip`, no `translate-x-*` utility and no
  `transition-property:transform` — every one of these is new output that a
  `build-css.sh` run must produce, which is exactly what an `assets_test.go`
  assertion can pin (`internal/web/assets_test.go:15-69` is the pattern).
- One collision to know about: Tailwind v4's own `-translate-x-[22rem]`
  utility compiles to the **`translate:`** property
  (`--tw-translate-x:calc(22rem * -1);translate:var(--tw-translate-x) var(--tw-translate-y)`),
  while a hand-written component rule uses **`transform:`**. They are two
  different properties and both apply, so mixing the utility and the component
  class on one element composes two translations rather than overriding one.
- The shell the strip must live inside is unchanged: `main` is
  `mx-auto max-w-[1600px] px-4 py-5` (`internal/web/templates/layout.html:20`),
  the header above it is `sticky top-0 z-10` and outside `main`
  (`internal/web/templates/layout.html:12`), and the board is
  `lg:grid-cols-[minmax(0,1fr)_22rem]` with `#panel` as an `<aside>` carrying
  `lg:sticky lg:top-20 lg:self-start` (`internal/web/templates/board.html:2-11`).

**Gap 7 — htmx history, read from the vendored bundle**

- The history element is `[hx-history-elt]` if present, else
  `document.body` — `function Ut(){…querySelector("[hx-history-elt],[data-hx-history-elt]");return e||ne().body}`
  (`internal/web/static/htmx.min.js`). The repo sets the attribute nowhere.
- What is cached is the history element's **`innerHTML`** (a clone with
  request classes stripped), plus `document.title` and `window.scrollY`:
  `function _t(e){…return n.innerHTML}` and `jt(t,e)`, stored in
  `localStorage` under `htmx-history-cache`. Defaults:
  `historyEnabled:true`, `historyCacheSize:10`, `refreshOnHistoryMiss:false`.
- On `popstate` with an htmx state, `Wt()` saves the current page, looks the
  target path up in the cache and, **on a hit, replaces the whole history
  element's content** from the cached HTML, restores the title, scrolls to the
  cached `scrollY` and fires `htmx:historyRestore`.
- **On a cache miss it issues an ordinary `GET` of the same URL** with
  `HX-Request: true` and `HX-History-Restore-Request: true`, then takes
  `[hx-history-elt]` out of the response *or the whole response document* and
  swaps it into the history element (`function Gt(o)`).
- Two consequences follow directly from that code, and both are facts about
  htmx rather than design choices: the server needs a correct **full-page**
  response for every chain URL (the cache-miss path uses it, as do reload and
  deep link), and any state that must survive Back has to be in the
  server-rendered markup that the snapshot captures — a restore replays HTML,
  not JavaScript state.
- `app.js` already re-applies its own state on `htmx:afterSettle`,
  `htmx:load` and `htmx:historyRestore` (`internal/web/static/app.js:141-159`),
  so the restore event is already observed in this codebase.
- `TestAppJSStatsToggles` fails if `app.js` ever contains `fetch(`,
  `XMLHttpRequest`, `htmx.ajax`, `hx-` or the literal `htmx-history-cache`
  (`internal/web/assets_test.go:74-93`) — a constraint on any rail or Back
  script.

**Gap 8 — the `/tasks/{id}` migration, test by test**

The migration is much smaller than `research_request.md` assumed: **no test
asserts the standalone page's wrapper, its `max-w-3xl` class or its "back to
the board" link.** Per test:

- `TestDetailPage` (`internal/web/handlers_test.go:116-137`) asserts 200, that
  the body contains `<html` ("the deep-linkable page must be a full
  document"), and the strings `Ship the board`, `Write templates`,
  `research.md`. A board+panel render satisfies all four; no rewrite needed.
- `TestDetailUnknownID404` (`:152-159`) pins `/tasks/nope` → 404. Keeping the
  404 keeps the test.
- `TestDetailArchivedTask` (`:176-194`) asserts the **board fragment** does
  not name the archived task and that `/tasks/9` returns 200 containing
  `Archived`. Both hold for board+panel: the board lists the active index only
  and the panel carries the banner (`internal/web/templates/_detail.html:8-10`).
- `TestBoardShowsPhaseFromRecords` (`:499-537`) uses `/tasks/3` as the detail
  surface and wants `class="phase-runs`, `research #1`, `design #1`,
  `81.2k tokens`, the `open` chip and `created by`; it also asserts the page
  does **not** contain `<li class="chip chip-muted">research.phase</li>`.
  All come from `_detail.html` and survive.
- `TestPhaseNoteEscaped` (`:540-556`) reads `/tasks/3` for the escaped note —
  survives.
- `TestNoExternalAssetReferences` (`:226-250`) sweeps `/` and `/tasks/1` for
  `http://` / `https://` in `src=`/`href=` — survives, and is the list to
  extend with chain paths.
- `TestNoMutatingRoutes` (`:252-275`) is the hardcoded path list plus a GET
  sweep that must leave the tasks directory byte-identical. `snapshotDir`
  compares path + **mtime** + size only (`:347-367`), so reads cannot trip it.
- `TestUnresolvedProjectPlaceholder` (`:309-329`) — see gap 9.
- `branch_test.go` holds the two **negative** assertions that a board+panel
  render could plausibly break, and neither does: `/tasks/1` must not contain
  `">Git<"` (`internal/web/branch_test.go:105`), and `">Git<"` appears only in
  `_detail.html:81`, never in `_branch.html`, whose chip is a `<code>` plus a
  copy button (`internal/web/templates/_branch.html:1-5`). And
  `TestCopyButtonHasNoHtmxAttributes` (`:112-130`) requires at least one copy
  button on `/tasks/4` — the board part supplies one via task 3's wip branch.
- The surfaces that actually go away are code, not tests: `detail.html` (its
  `{{ define "title" }}` gives the page a task-specific `<title>`, which the
  board page has no equivalent for — `internal/web/templates/detail.html:1-6`)
  and the `detailPage` template set (`internal/web/templates.go:24`).
- The README needs **no** correction for the repurposing: line 99 says only
  "A done task stays reachable at `/tasks/{id}`", which stays true, and line
  100's "The detail view lists every phase run" stays true of the panel.
- A consequence worth recording, because it weakens future tests rather than
  current ones: once `/tasks/{id}` contains the whole board, an assertion of
  the form "X must not appear on `/tasks/{id}`" no longer isolates the detail
  surface. The two existing negative assertions happen to use detail-only
  strings.

**Gap 9 — 404 versus placeholder before resolution**

- `TestUnresolvedProjectPlaceholder` pins the split exactly: `/` and `/board`
  must answer **200 with "No project resolved yet"**, while `/tasks/1` must
  answer **404** (`internal/web/handlers_test.go:309-329`). The test also
  fails outright if an HTTP request resolves the project, by making the roots
  callback `t.Fatal` (`:311-314`).
- The mechanism behind that split is `h.detailView`, which returns
  `(DetailView{}, false)` the moment `h.Project()` reports unresolved
  (`internal/web/handlers.go:88-92`), and `detail` turning `!ok` into
  `http.NotFound` (`internal/web/handlers.go:30-36`); the board handler
  instead falls back to `unresolvedBoardView` (`internal/web/handlers.go:75-79`).
- `taskFile` does the same, 404 on unresolved (`internal/web/handlers.go:55-58`).

**One defect found in shipped code, relevant because the chain inherits it**

- `html/template`'s URL normalizer in an `href` **does not escape `#` or `?`**.
  Probed against the exact idiom at `_detail.html:122`: a file named
  `a#b.md` renders as `href="/tasks/42/files/a#b.md"`, where `#b.md` is a
  fragment, and `a?b.md` renders as `…/files/a?b.md`, where `?b.md` is a
  query. Both files are unreachable through the link the detail view draws
  today. Spaces (`%20`), `%` (`%25`) and unicode (`%d0%b8…`) *are* escaped,
  and `&`/`+` come out HTML-escaped (`&amp;`, `&#43;`), which the browser
  decodes correctly.
- Both names are legal attached file names: `ValidateNameSegment` rejects only
  empty, `/`, `\` and dots-and-spaces-only (`internal/task/name.go:17-28`).
- Escaping in Go first is exact. `url.PathEscape` → split
  `EscapedPath()` → `url.PathUnescape` round-trips **every** name probed:
  `my notes.md`, `a+b.md`, `100%.md`, `исслед.md`, `a#b.md`, `a?b.md`,
  `a&b=c.md`, `" x "`, `plan.md`. The acceptance criterion's round-trip list
  ("dots, spaces, unicode and `+`") should gain `#` and `?`, which are the two
  that actually break.

### Evidence Map

| Concern | Current state / probe result | Evidence |
| ------- | ---------------------------- | -------- |
| Route table | seven GET patterns; `files/{name}` is new since the parent's research | `internal/web/server.go:58-64` |
| Wildcard registration | `/tasks/{id}/w/{rest...}` conflicts with nothing in that set | probe |
| Duplicate pattern | `/tasks/{id}/w/` + `{rest...}` together panic at registration | probe |
| Empty tail | `/tasks/42/w/` → `rest=""`; `/tasks/42/w` → 307 to the slash form | probe |
| `PathValue` decoding | decoded, so `%2F` becomes a separator in `rest` and in `id` | probe |
| Raw path | `r.URL.EscapedPath()` keeps `%2F`, `%2E%2E` and escape case | probe |
| Tail extraction | `strings.Index(esc,"/w/")` is wrong for `id == "w"`; `segs[4:]` is right | probe |
| Path cleaning | `//`, `/./`, `/../`, literal `.`/`..` → **307**, not 301 | probe |
| Trailing slash | not cleaned; yields a trailing empty segment in the tail | probe |
| Traversal | `%2e%2e` reaches the handler; `ValidateNameSegment` is what rejects it | probe, `internal/task/name.go:21-26` |
| URL length | worst-case realistic depth-10 chain = 270 chars | probe over `tasks/` |
| Tailwind CLI | v4.3.3 cached at `.cache/`, so the rebuild is offline here | `scripts/build-css.sh:38-45` |
| Custom-property geometry | `transform: translateX(calc(var(--col)…))` compiles | CSS probe |
| Reduced motion | compiles both as a `@media` in `@layer components` and as `motion-reduce:` | CSS probe |
| `overflow-x: clip` | real utility in v4.3.3 | CSS probe |
| Per-breakpoint width | `lg:[--col-w:26rem]` compiles | CSS probe |
| Utility vs component transform | v4 utilities set `translate:`, hand-written rules set `transform:` | CSS probe |
| Current compiled CSS | no reduced-motion, no `overflow-x:clip`, no `translate-x-*`, no transform transition | grep `internal/web/static/app.css` |
| htmx history element | `[hx-history-elt]` else `body`; attribute unused in this repo | `internal/web/static/htmx.min.js` |
| htmx snapshot | history element's `innerHTML` + title + `scrollY`, 10 entries, `localStorage` | `internal/web/static/htmx.min.js` |
| htmx restore | cache hit replaces the history element's content; miss GETs the URL | `internal/web/static/htmx.min.js` |
| Restore already observed | `app.js` listens for `htmx:historyRestore` | `internal/web/static/app.js:141-159` |
| `app.js` constraints | no `fetch`, `hx-`, or `htmx-history-cache` allowed | `internal/web/assets_test.go:74-93` |
| Detail-page tests | none assert the wrapper; all survive board+panel | `internal/web/handlers_test.go:116-194`, `:499-556` |
| Negative assertions at risk | `">Git<"` is detail-only; copy button satisfied by the board | `internal/web/branch_test.go:105,112-130`, `internal/web/templates/_branch.html:1-5` |
| README | line 99 stays true under the repurposing | `README.md:99-100` |
| Unresolved split | `/`, `/board` → 200 placeholder; `/tasks/{id}` → 404 | `internal/web/handlers_test.go:309-329`, `internal/web/handlers.go:30-36,88-92` |
| `href` escaping gap | `html/template` leaves `#` and `?` unescaped in a path | probe, `internal/web/templates/_detail.html:122` |
| Go-side escaping | `PathEscape` → raw split → `PathUnescape` round-trips every probed name | probe |
| Validation available | `ValidateAttachedName` / `ValidateNameSegment` exported | `internal/task/name.go:17-38` |
| Renderer available | `markdown.Render`, stdlib only, no unsafe mode | `internal/markdown/markdown.go:1-37` |
| Baseline | `internal/web`, `internal/task`, `internal/markdown` all pass | test run |

### Relevant Files

1. `internal/web/server.go:54-65` — the one place the wildcard pattern is
   registered, and the only pattern of its family that may exist.
2. `internal/web/handlers.go:30-68,88-105` — the 404-vs-`Missing` split, the
   unresolved→404 path, and `detailView`'s second snapshot call for relation
   titles.
3. `internal/web/templates/board.html:2-11` — the grid and the sticky `#panel`
   the strip replaces or wraps.
4. `internal/web/templates/layout.html:12,20` — the `max-w-[1600px]` shell and
   the sticky header the strip has to live with.
5. `internal/web/assets/input.css:50-73` — the `.lane` / `--lane` precedent,
   the only existing example of geometry named by a class and computed in CSS.
6. `internal/web/templates/_detail.html:49-62,119-124,81` — the subtask list,
   the attached-file links (with the `#`/`?` escaping defect) and the Git
   block whose `">Git<"` string two tests depend on.
7. `internal/web/templates.go:20-33` — the template sets, including the
   `detailPage` set that the migration removes, and the no-`template.HTML`
   rule a rendered-markdown column must reckon with.
8. `internal/web/handlers_test.go:116-194,226-275,309-329` — the tests the
   migration touches and the two path lists to extend.
9. `internal/web/branch_test.go:105,112-130` — the two negative assertions
   most exposed to `/tasks/{id}` becoming the whole board.
10. `internal/task/name.go:17-38` — the validation the chain parse must call
    before any read.
11. `internal/web/static/htmx.min.js` — `Ut`/`jt`/`_t`/`Wt`/`Gt`, the history
    functions quoted above.
12. `internal/web/assets_test.go:15-93` — how a compiled-CSS assertion and the
    `app.js` no-request assertion are written.
13. `scripts/build-css.sh:21-48` — the maintainer step and its local cache.

### Inference

- **The rail cannot sit inside the transformed strip.** This is an inference
  from the CSS specs, not an observation: a transformed element establishes a
  containing block for fixed-position descendants and shifts the coordinate
  space its `position: sticky` descendants are offset in, so a rail inside the
  strip would translate with it. Nothing in this repository exercises
  `sticky` inside a `transform`, and no browser was driven in this run.
- **`overflow-x: clip` and `overflow-x: hidden` are not interchangeable
  here.** Inference from CSS Overflow 3: `hidden` makes the element a scroll
  container and forces the other axis's used value away from `visible`, which
  would clip or scroll `#panel`'s `lg:sticky lg:top-20`
  (`internal/web/templates/board.html:7`), whereas `clip` paired with
  `overflow-y: visible` is explicitly allowed. The compile probe confirms the
  utility exists; the layout consequence was reasoned, not seen.
- **Container queries inside a transformed ancestor should be unaffected.**
  Inference: `transform` does not change an element's border-box size and
  `container-type: inline-size` queries that size, so the phase lanes
  (`internal/web/assets/input.css:56-73`) should behave the same off-screen.
  Untested in a browser.
- **A `translateX` strip will create scrollable overflow unless clipped.**
  Inference from the spec (a transform does not affect layout but does affect
  the ancestor's scrollable overflow area); not observed here.

### Unknown

- **No browser was driven in this run.** Every geometry claim above is either
  a compile result from the real Tailwind CLI (what CSS is produced) or a
  spec-level inference (how a browser lays it out). Whether the strip, the
  rail and the lanes behave as reasoned at `lg` and below, and whether any
  state produces a horizontal scrollbar, remains unverified by observation —
  and the repository has no browser-driven test to verify it with: every
  existing assertion is over response bytes or compiled CSS text
  (`internal/web/handlers_test.go`, `internal/web/assets_test.go`). This is
  the one gap the design phase cannot close by reading anything.
- **htmx's history behaviour was read, not run.** The functions quoted from
  the minified bundle say what is cached and what a restore replaces; that a
  restored strip ends up on the right step was not observed in a browser.
