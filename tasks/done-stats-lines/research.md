# Research: done-stats-lines (gap closure)

The base research is `done-column-stats/research.md`; the widths and colours measured for `done-stats-bars` are in `done-stats-bars/research.md`. This file closes the six gaps in `research_request.md`.

Experiments ran in the scratchpad:
- html/template probes with go1.27.1;
- headless Chrome 154 over CDP, against a small Go harness that serves the vendored `app.css` and `htmx.min.js`, and against `serve web` on a copy of this backlog.

Nothing in the repo changed.

### Task Slice

- Rendering `StatsCard` lines data (`Days`, `Lines`) as an inline server-side SVG inside the Done column.
- How html/template escapes SVG attributes, `<title>` and `data-*` names.
- Where the chart geometry can live, given the "Go never computes geometry" precedent.
- How an SVG behaves at the measured 155–249px card widths: scaling, strokes, text, hit targets.
- A line palette on the dark card, and the edge cases (zeros, one day, long windows).

### Confirmed Facts

**Input from the landed subtasks**
- A lines card has `Days []time.Time`: local midnights in the service clock's zone, oldest first, today last.
- It has `Lines []StatsLine{Key, Values, Hidden}`, with `len(Values) == len(Days)`.
- For a split card, `Field` holds the split field and `Metric` the metric (`internal/task/stats.go:15-54,63-80`).
- Plain cards emit lines in config order. Unknown and duplicate names are skipped (`stats.go:153-175`).
- Split cards emit one line per value that has at least one count in the window, in domain order, unknown values alphabetically (`stats.go:122-151`). An unknown metric gives no lines.
  - **The set of split lines therefore changes from day to day.** A value appears only while it has counts in the window.
- `Key` is a line name (`created`, `closed`, `created_cumulative`, `closed_cumulative`) or a raw field value. Type values are user-defined and case-sensitive, and `created_by` is free text (`stats.go:266-284`). `Hidden` is an exact, case-sensitive match against the card's `hidden` list (`stats.go:119`).
- Card `ID`:
  - a derived ID is slugged to `[a-z0-9-]`;
  - an **explicit `id:` from the config is only trimmed, never slugged**, so it can contain spaces, quotes or `/` (`internal/config/stats.go:107-110,152-154,183-200`).
- `dayWindow` clamps `n < 1` to 1, so a lines card always has at least one day (`stats.go:221-225`). The config turns `days <= 0` into 14 before that (`config/stats.go:131-133`).

**Gap 1: SVG under html/template** (scratch template, actual output)
- These pass through unchanged:
  - `viewBox="0 0 {{.W}} {{.H}}"` → `0 0 100 40`;
  - `points="{{.Points}}"` with the string `0,40 7.69,12.5 100,0`;
  - `d="{{.Path}}"`;
  - `cx="{{.X}}" cy="{{.Y}}"` with floats;
  - `class="s-{{.Idx}}"`;
  - `stroke="var(--c{{.Idx}})"`;
  - `vector-effect`, `preserveAspectRatio` and `role`;
  - `aria-pressed="{{.Bool}}"` → `true`.
- These are escaped as text, with no `ZgotmplZ`:
  - `aria-label` and `<title>{{.}}</title>` inside `<svg>`: `Oct 5: 3 closed <b>` → `Oct 5: 3 closed &lt;b&gt;`;
  - `<text>` content.
- `data-*` names:

  | Attribute | Context | `javascript:alert(1)` | `a b` |
  |---|---|---|---|
  | `data-line`, `data-card`, `data-key`, `data-stats-line`, `data-hidden`, `data-toggle`, `data-hide` | plain text | unchanged | unchanged |
  | `data-url`, `data-uri`, `data-src-key` | **URL** | `#ZgotmplZ` | `a%20b` |
  | `data-on…` (for example `data-onlinekey`) | **JS** | – | JSON-quoted: `"a b"` |

- Arbitrary values in plain-text `data-*` and `class` attributes are entity-escaped (`my card/"x"` → `my card/&#34;x&#34;`). The same value in a `style` attribute becomes `ZgotmplZ`.
- A float printed with `{{.}}` uses Go's shortest repr. `1.0/3` renders as `0.3333333333333333`, so unformatted floats give long `points` strings.

**Gap 2: where the geometry lives**
- The lanes design keeps geometry in CSS for a fixed, four-valued offset. It rejected `style="--lane: N"` for reasons specific to inline styles: the first inline style in the codebase, a future strict CSP, a Go index field, and the chip precedent (`tasks/in-progress-phase-columns/design:288-293,432`).
- A polyline cannot be expressed by a finite set of CSS classes: its points are data.
- **Percent coordinates.** In an SVG without a viewBox, `<circle cx="50%">` and `<line x1="0%" … x2="50%">` resolve against the rendered width (centre at 76px of 151px; line 76px). `<polyline points="0%,100% 100%,0%">` is **invalid**: Chrome parsed 0 points and drew nothing. So percent coordinates work for circles, lines and rects, but not for polyline or path.
- The current view layer already turns data into presentation in Go, for example `humanizeAgo` and `cardBranch` (`internal/web/view.go:337-344,504-516`). It has never computed coordinates; `svg` returns no hits in `internal/web` (`research.md`).

**Gap 3: responsiveness** (measured in Chrome)
- **Card width.** The inner width of a Done `.card` is 155px at 1024, 169 at 1280, 209 at 1440, 249 at 1600 and above, and 193 at 768 (`done-stats-bars/research.md`).
- **`preserveAspectRatio="none"`** with `viewBox="0 0 100 100"` drawn at 151×60px:
  - an `r=3` circle renders as a **9.1×3.6px ellipse**;
  - `<text font-size=10>` is squashed (box 40.2×6.8px);
  - a `stroke-width=2` polyline with `vector-effect="non-scaling-stroke"` keeps a computed stroke width of 2px.
  - So stretched SVG keeps strokes uniform but distorts markers and text.
- **Uniform scaling** (the default `xMidYMid meet`, `viewBox 0 0 100 40` in a 151×60 box) gives an x/y scale ratio of 1.01. `font-size=8` text then renders about 15px tall: SVG text scales with the card width, so its pixel size differs at every breakpoint.
- **Mono label width.** `.meta` (mono, 11px) is about 6.6px per character (119.2px for 18 characters). A 5-character day label (`Oct 6`, `10-06`) is about 33px, so about 4–7 labels fit in 155–249px.
- **Pixels per day** at 169px: 14 days ≈ 12px per day; 30 days ≈ 5.6px; 90 days ≈ 1.9px.

**Gap 4: palette** (WCAG contrast against the card background, about rgb(20,20,20); computed from Tailwind OKLCH values, not from screenshots)

| Colour | Contrast | Colour | Contrast |
|---|---|---|---|
| sky-400 | 8.45 | sky-300 | 11.07 |
| amber-400 | 10.74 | amber-300 | 12.76 |
| rose-400 | 6.45 | rose-300 | 9.60 |
| emerald-400 | 9.54 | emerald-500 | 7.49 |
| teal-400 | 9.89 | violet-400 | 6.47 |
| orange-400 | 7.76 | fuchsia-400 | 7.14 |
| lime-400 | 12.02 | yellow-300 | 13.95 |
| neutral-400 | 7.11 | neutral-500 | 3.90 |

- The existing priority chips use rose (critical), amber (high), sky (medium) and neutral (low) (`internal/web/assets/input.css:24-27`). Status dots use neutral-500, amber-400 and emerald-500 (`input.css:31-34`).
- **Theme variables reach `app.css`** in two ways:
  - Tailwind v4.3.3 emits a `--color-*` variable when an `input.css` component references it through `var()` or `@apply` (scratch build);
  - it also emits it when the build scan finds the utility class string, which depends on the working directory (`done-stats-bars/research.md`, Gap 2).
- **`stroke="var(--c2)"`** as an SVG presentation attribute resolves in Chrome: computed stroke `rgb(56, 189, 248)`.
- The largest split card has 5 lines for resolution, 4 for priority, 3 for status, `len(task_types)` for type, and an unbounded number for `created_by` (`stats.go:287-318`).

**Gap 5: tooltips and hit targets**
- An `r=3` circle in a uniformly scaled SVG at 151px is about 9×9px.
- In the stretched mode it is an ellipse that is 3.6px tall (see above).
- A one-point `<polyline>` has a hit area of nothing: `elementFromPoint` at its coordinate returned the `<svg>`.
- The browser did not show native `<title>` tooltips, because headless Chrome has none. Only the escaping of `<title>` is confirmed (Gap 1).

**Gap 6: edge cases**
- **All zeros.** A y scale of `v * H / max` with `max == 0` is a Go **integer division by zero, which panics**. With floats it is `NaN` or `+Inf`. A `points` string that contains `NaN` is rejected as a whole: Chrome parsed **0 points**, so the line disappeared silently.
- **`days: 1`.** The card has one day. A one-point polyline parses (1 point) but has no painted hit area (Gap 5). Whether a round line cap shows a dot was not checked visually.
- **Large `days`.** Nothing caps `days` (`config/stats.go:131-133`). At 90 days a point is about 1.9px apart at 169px.

**Tests**
- `newBoardView` tests use `boardOf`, which leaves `Stats` nil, and a nil config (`internal/web/view_test.go:25-47`). Lines view-mapping tests can build `task.StatsCard` values by hand.
- Handler tests run the service with the real clock (`NewBacklog` has no `WithClock`, `internal/testsupport/testsupport.go:23-41`). So:
  - day labels in handler output depend on the machine's date and `time.Local`;
  - only "today's bucket holds the seeded tasks" is deterministic.
- `TestNoExternalAssetReferences` scans only `src=` and `href=` for `http(s)://` (`handlers_test.go:221-245`). An inline `<svg xmlns="http://www.w3.org/2000/svg">` attribute is not matched by it.

### Evidence Map

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Lines data | `Days` local midnights; `Lines{Key, Values, Hidden}` | `internal/task/stats.go:15-54,117-176` |
| Split lines vary | Only values with counts in the window | `stats.go:122-151` |
| Card ID | Derived slugged, explicit unslugged | `internal/config/stats.go:107-110,152-154` |
| SVG escaping | Attributes plain; `<title>` escaped; `data-url/uri/src*` URL, `data-on*` JS | scratch probe |
| Percent coords | Work for circle and line; polyline needs numbers | Chrome |
| Stretched SVG | Strokes kept with non-scaling-stroke; circles and text distorted | Chrome |
| Palette contrast | 6.4–14 for the 300/400 shades | computed |
| Zero max | int div panics; NaN empties the polyline | Go semantics; Chrome |
| Widths | 155–249px card inner | `done-stats-bars/research.md` |

### Relevant Files

1. `internal/task/stats.go`: the shape and ordering of `Days` and `Lines`, and the rule that split values appear only when present.
2. `internal/config/stats.go`: card `ID` derivation (slugged) against an explicit `id` (not slugged), and `Hidden`.
3. `internal/web/view.go`: where a `StatsCard` → view-model mapping goes, next to `newPhaseLanes`.
4. `internal/web/assets/input.css`: the colour and component conventions; the place to name series colours so the variables are emitted.
5. `internal/web/templates.go:15-18`: no FuncMap, so formatting of numbers and points has to happen in the view model.
6. `tasks/in-progress-phase-columns/design` (section 6): the scope of the "Go never computes geometry" rule.

### Inference

- **Coordinates in Go.** The no-geometry rule was argued for fixed layout offsets and inline styles. Chart coordinates, which normalize data into a viewBox, are a different kind of thing, and SVG presentation attributes are not inline styles. A pure mapping function in `internal/web` would be the first Go coordinate code, but it would not contradict the stated reasons. This is a reading of the lanes design, not a stated rule.
- **Day labels as HTML.** Because stretched SVG distorts text and uniform scaling makes text size width-dependent, day labels and the y maximum would render predictably as HTML around the SVG (for example `.meta` spans). This follows from the measurements; it was not prototyped.
- **Hover targets.** At 30 or more days, per-point circles overlap (≤ 5.6px apart at 169px). A per-day hover target (one transparent column per day, with a `<title>` covering all lines) would be wider than a circle. Untested.
- **Explicit IDs with unusual characters** are safe in plain `data-*` attributes because they are escaped, but would need escaping wherever they are spliced into a CSS selector (relevant to `done-stats-toggles`).

### Unknown

- Whether native SVG `<title>` tooltips appear on a 9px circle in desktop Chrome, Firefox and Safari. This was not observable headless and needs a manual check.
- Whether the y axis is shared per card or scaled per line. The data allows both, and nothing in `task.md` settles it.
