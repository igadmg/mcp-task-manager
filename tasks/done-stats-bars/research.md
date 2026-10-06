# Research: done-stats-bars (gap closure)

The base research is `done-column-stats/research.md`. This file closes the five gaps in `research_request.md`. The config (`cf1bf77`) and the service (`b484801`) have landed, so this file also records what the web layer now receives.

Experiments ran in the scratchpad, not in the repo:
- html/template probes with go1.27.1;
- Tailwind v4.3.3 builds from the cached `.cache/tailwindcss-macos-arm64-v4.3.3`, written to scratch outputs;
- headless Chrome 154 driven over CDP against `mcp-task-manager serve web` on a **copy** of this repo's backlog.

The repo is unchanged: `git status` is clean for `internal/`, and the md5 of `app.css` is the same before and after.

### Task Slice

- Mapping `snap.Stats` bars cards into the Done column's view model, and replacing the done task cards.
- How a stacked bar gets per-value widths under html/template without "Go computing geometry".
- Which classes and colour variables the Tailwind build really emits, depending on where the script runs.
- The measured Done-column width and the measured label widths.
- Colour contrast for the done, recent, in-progress and todo segments on the dark card.
- The existing web tests that depend on done task cards.

### Confirmed Facts

**Input from the landed subtasks**
- `BoardSnapshot.Stats []StatsCard` is filled in `boardSnapshot` from the same `All()` read: `computeStats(all, s.config.DoneStatsCards(), s.validTypes, now)` (`internal/task/view.go:41-43,79-88`).
- `StatsCard{ID, Kind, Title, Field, Metric, Bars, Days, Lines}` (`internal/task/stats.go:15-31`).
- `StatsBar{Value, Total, Done, InProgress, Todo, ClosedRecently}`. ClosedRecently is part of Done, and the window is `(now-24h, now]` (`stats.go:33-44,56-57,99`).
- Rows come in domain order; unknown values follow, sorted alphabetically (`stats.go:287-318`).
- An unknown field gives a card with `Bars == nil`. The card still keeps ID, Kind and Title (`stats.go:63-80`).
- `ColumnView` has `Status, Title, Count, Cards, Lanes` and no stats field (`internal/web/view.go:38-46`). Only In progress gets `Lanes` (`view.go:234-236`).
  - `Count` counts `snap.Tasks` by status, independently of `Cards` (`view.go:222-227`), so the header count does not depend on cards being rendered.
- Web view tests build snapshots with `boardOf`, which sets no `Stats` (`internal/web/view_test.go:25-47`), and pass a nil config.

**Gap 1: segment widths without Go geometry**
- html/template, `style` attribute (CSS context):

  | Template | Output |
  |---|---|
  | `style="width: {{.Pct}}%"` (int 42) | `width: 42%` |
  | `style="--w: {{.}}"` (int, float 33.333, string `"42.5"`) | passes unchanged |
  | `style="flex-grow: {{.Int}}"` | `flex-grow: 7` |
  | `style="--w: {{.}}"` with a string holding a quote, or `javascript:alert(1)` | `ZgotmplZ` |

- `<meter value max>` and `<progress value max>` render with plain integer attributes. Each represents a single value, so neither can show three or four stacked segments. That is from the HTML element definition; it was not tried as a stack.
- Three stacked-bar variants, rendered at 1280px in the real Done column (card inner width 169px), with done 7 (2 of them recent), in progress 3 and todo 2:
  - **A**, flex items with `flex: N 1 0`, raw counts, the recent part nested inside done: segments 70.4 / 28.2 / 42.3 / 28.2px. A zero-count segment measures 0px.
  - **B**, `<svg viewBox="0 0 12 1" preserveAspectRatio="none">` with `<rect x width>` in raw counts: in-progress segment 42.3px.
  - **C**, a grid with `grid-template-columns: 5fr 2fr 3fr 2fr 0fr`: 70.4 / 42.3px.
  - All three equal the exact proportion (169 × 3/12 = 42.3). None needs percentages computed in Go. A and C need raw counts in a `style` attribute; B needs raw counts plus x offsets, which are cumulative sums, in SVG attributes.
- Tailwind v4.3.3 compiles the arbitrary class `w-[calc(var(--w)*1%)]` to `width:calc(var(--w) * 1%)`. It is emitted only when the scanner sees the string (see Gap 2).
- The lanes design rejected inline `style="--lane: N"` for four reasons:
  - it would be the first inline style in the codebase;
  - it would break under any future strict CSP;
  - it needs an index field in Go;
  - it departs from the `chip-{{ .Priority }}` precedent.

  It also noted that an integer passes the CSS filter (`tasks/in-progress-phase-columns/design:288-293`). There is still no `style=` in `internal/web/templates` (grep).
- No handler sets a Content-Security-Policy. The only headers set are Cache-Control on static files and Content-Type (`internal/web/assets.go:24`, `handlers.go:51,109`).

**Gap 2: Tailwind scan scope**
- I built the unchanged `input.css` from four working directories and compared the selector sets with the committed `app.css`:

  | Built from | Selectors | Same as committed? |
  |---|---|---|
  | committed `app.css` (28 774 B) | 163 | – |
  | repo root | 172 | no: +`.bg-amber-400 .bg-emerald-500 .bg-neutral-500 .container .grid-cols-3 .p-2 .pb-1 .shrink .xl:grid-cols-[...]` |
  | `scripts/` | 144 | no: −`.absolute .collapse .contents .filter .fixed .grow .hidden .inline .invert .invisible .isolate .lowercase .relative .resize .ring .static .table .transition .visible` |
  | `internal/web/` | 150 | no |

- **Probe.** I added a temporary Go file in `internal/web` holding the string `bg-fuchsia-700 w-[calc(var(--w)*1%)]` and deleted it afterwards.
  - A build from the root emitted both classes.
  - A build from `scripts/` emitted neither.
  - So from the repo root Tailwind scans Go files and markdown, as `scripts/build-css.sh:8-10` says. The narrower claim in `input.css:6-8` ("the scanner only reads ../templates") holds only when the script runs from a directory outside the repo tree.
- The extra classes in today's root build come from task markdown that mentions class names. Grep finds them in `tasks/in-progress-phase-columns/{research,design,plan}` and `tasks/done-column-stats/research.md`. A rebuild from the root therefore picks up whatever `tasks/` currently contains.
- The committed `app.css` contains `.hidden{display:none}` and `.invisible{visibility:hidden}`. A `scripts/` build lacks them, and no template uses them (grep). They exist only because of the wider scan.
- A colour referenced only through `var()` in an `input.css` component (`.probe-var{background:var(--color-lime-300)}`) makes Tailwind emit `--color-lime-300` (scratch build with `source(none)`). The same holds for `@apply bg-teal-300`.
- The colour variables emitted today are: red-300/500, amber-100…500, emerald-500, sky-300/500, rose-300/500, neutral-100/200/300/400/500/600/900/950. No other emerald shade and no neutral-700/800.
- `assets_test.go` guards compiled classes by substring: `.lanes{`, `.lane-<phase>{` and `.phase-runs{` (`internal/web/assets_test.go:15-45`).

**Gap 3: measured Done-column width** (headless Chrome, height 900, scale factor 1, today's board with the detail aside)

| Viewport | Columns (px) | Done content | `.card` inner |
|---|---|---|---|
| 768 | 237 / 237 / 237 | 219 | 193 |
| 1024 | 199 / 199 / 199 | 181 | **155** |
| 1280 | 213 / 426 / 213 | 195 | 169 |
| 1440 | 253 / 506 / 253 | 235 | 209 |
| 1600 | 293 / 586 / 293 | 275 | 249 |
| 1920 | 293 / 586 / 293 | 275 | 249 (capped by `max-w-[1600px]`) |

- The narrowest width is at 1024, where the `lg` grid adds the 22rem `#panel` aside (`board.html:2`). Below 768 the board is a single column.
- Measured label widths (max-content, nowrap):
  - `7/12 · 5 open · +2` is 119.2px in `.meta` (mono, 11px) and 97.4px in `text-xs` (sans).
  - `128/1024 · 896 open · +12` is 165.6px in `.meta`.
  - The value `superseded` is 66.2px in `.meta`.
  - Value and label on one line, `superseded 7/12 · 5 open · +2`, is 192.1px in `.meta`.
- **On one line:** the typical label fits every measured card width. The long label does not fit at 1024 (165.6 > 155). Value plus label fits only at 768 (barely: 192.1 < 193) and at 1440 and above.
- The earlier arithmetic estimate in `research.md` (about 195px at 1280, about 275 at 1600 and above) matches the measured Done content widths.

**Gap 4: colours** (WCAG contrast ratios computed from Tailwind's OKLCH values with the OKLab → sRGB matrices in node, not read from a screenshot)
- The card background is `bg-neutral-900/60` over `bg-white/[0.02]` over neutral-950, which is about rgb(20,20,20).
- Against the card:

  | Colour | Contrast |
  |---|---|
  | emerald-500 (done dot today) | 7.49 |
  | emerald-300 | 12.16 |
  | emerald-200 | 14.41 |
  | emerald-700 | 3.44 |
  | amber-400 (in progress dot) | 10.74 |
  | neutral-500 (todo dot) | 3.90 |
  | neutral-600 | 2.37 |
  | neutral-700 | 1.78 |

- Candidates for the recent part, against emerald-500:

  | Colour | Contrast |
  |---|---|
  | emerald-200 | 1.92 |
  | emerald-300 | 1.62 |
  | emerald-400 | 1.27 |
  | emerald-700 | 2.18 |
  | lime-300 | 1.89 |
  | yellow-300 | 1.86 |
  | amber-300 | 1.70 |

- Pairs:
  - emerald-600 against emerald-300: 2.42.
  - Adjacent done (emerald-500) against in progress (amber-400): 1.44. These two differ mostly in hue, not lightness.
- Every shade other than emerald-500 and the neutrals listed above would be a new variable in `app.css`.

**Gap 5: tests that rely on done task cards**

| Test | What it relies on | Evidence |
|---|---|---|
| `TestBoardRendersThreeColumns` | The board body contains card title "Old chore", which is task 4, done in `seedBoard` | `handlers_test.go:44-53,69-73` |
| `TestBoardShowsBranchChip` | On `/board`: `data-copy="dev/4-old-chore"` (done task 4's final branch) and exactly 2 `copy-btn`, from in-progress task 3 and task 4. The detail assertions on `/tasks/4` do not involve the board | `branch_test.go:69-109` |
| `TestCopyButtonHasNoHtmxAttributes` | A copy button on `/` and `/board`. Only task 4 gets a branch, so after the change those pages have none and the test fails on "has no copy button". `setBranch` works on any record, task 3 (in progress) included | `branch_test.go:20-36,111-128` |
| `TestBoardEmptyInProgressShowsFourStubs` | Exactly one `>empty</p>`, the empty Done column (task 1 is todo) | `handlers_test.go:467-483`, `_board.html:39-41` |
| `TestInProgressHasFourPhaseLanes` | Done has 0 `Lanes`. Unaffected while stats live in a separate field | `view_test.go:74-92` |
| `TestLanesPartitionTheColumn` | Contains done task `e` but asserts only the in_progress column | `view_test.go:177-213` |
| `TestDetailArchivedTask` | Seeds a done task and reads its detail; no board assertion | `handlers_test.go:171-189` |

- `TestHandlerRaceAgainstWrites` uses `seedBoard` and fetches pages concurrently, so the stats path is covered under `-race` (`race_test.go:19-73`).
- Handler tests get a service with the real clock:
  - `testsupport.NewBacklog` takes no options, so there is no `WithClock` (`internal/testsupport/testsupport.go:23-41`);
  - `Deps.Now` is pinned to January 2026 (`handlers_test.go:23-35`);
  - seeded done tasks are closed at the real "now", so they count as `ClosedRecently`;
  - the web layer's `fixedNow` plays no part in the stats.
- Once done cards are gone, the board renders `_branch.html` (`copy-btn`, `data-copy`) only for To do and In progress cards. `_detail.html` keeps the Git block for done tasks (`branch_test.go:94-104` asserts it).

### Evidence Map

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Bars data | `StatsBar` per value, domain order | `internal/task/stats.go:33-44,83-112,287-318` |
| Where stats arrive | `BoardSnapshot.Stats`, same read and lock | `internal/task/view.go:41-43,79-88` |
| Column model | No stats field; `Lanes` precedent | `internal/web/view.go:38-46,234-236` |
| Inline style under html/template | Numbers pass; unsafe strings → `ZgotmplZ` | scratch probe |
| Width variants | flex-grow, SVG rect and grid fr all proportional to raw counts | Chrome measurement |
| No-inline-style rationale | Four reasons in the lanes design | `tasks/in-progress-phase-columns/design:288-293` |
| CSP | None set | `assets.go:24`, `handlers.go:51,109` |
| Tailwind scope | cwd-dependent; root scans Go and `tasks/*.md` | four builds plus probe; `build-css.sh:8-10` vs `input.css:6-8` |
| Var emission | `var(--color-x)` in a component emits it | scratch build |
| Done width | 155–249px card inner | Chrome measurement |
| Tests at risk | 4 fail, 2 unaffected | `handlers_test.go`, `branch_test.go`, `view_test.go` |

### Relevant Files

1. `internal/task/stats.go`: the `StatsBar` semantics, especially that ClosedRecently is inside Done and that the resolution card counts done tasks only.
2. `internal/web/view.go`: `ColumnView`, `newBoardView` and the Lanes precedent that the stats mapping mirrors.
3. `internal/web/templates/_board.html`: the `{{ if .Lanes }}` branch and the `empty` fallback that the Done branch replaces.
4. `internal/web/assets/input.css`: the component-class pattern and the lanes geometry comment; the place for new bar and colour classes.
5. `scripts/build-css.sh`: where the build runs from decides which classes are emitted.
6. `internal/web/branch_test.go` and `handlers_test.go`: the tests listed above.
7. `tasks/in-progress-phase-columns/design` (section 6): the reasoning behind the no-inline-style rule.

### Inference

- **The committed `app.css` was built from the repo root,** or another directory whose scan sees Go and markdown files. It holds generic words such as `.table` and `.hidden` that only appear outside templates, and a `scripts/` build lacks them. Its exact source tree is not reproducible today because `tasks/` has changed since.
- **CSP and SVG attributes.** A strict `style-src` CSP governs `style` attributes, not SVG presentation attributes such as `x` and `width`, so variant B would survive such a CSP and variants A and C would not. That is from the CSP model; it was not tested, because the server sets no CSP.
- **The bar label fits on one line,** but the value name in the same row does not below 1440px, so value and label probably need separate lines or a smaller font. The measurements above support this; the layout itself was not prototyped.
- **Colour pairs.** Low-contrast neighbours (recent against done at 1.3–2.2) would be told apart mostly by hue and lightness steps, not by the WCAG 3:1 non-text threshold. No pair from the emerald family reaches 3:1 against emerald-500.

### Unknown

- None blocking. The choice between the width variants, the class names, the colours and the test rewrites is design work.
