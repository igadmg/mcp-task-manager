# Research request: done-stats-lines

**Additional research needed: 65 / 100**

## Already covered by research.md
- The project has no SVG or charts at all (grep finds nothing).
- An inference, not checked: html/template treats `points`, `d` and `viewBox` as plain text, `<title>` as RCDATA (escaped), and some `data-*` names (on…, src, uri, url) as JS or URL context.
- The dark-only theme; the Tailwind colour variables that are emitted today.
- The precedent "Go never computes geometry" (`input.css:47-52`).

## Gaps (largest)
1. **Check SVG escaping in html/template in practice.** Write a scratchpad template with `<svg viewBox>`, `<polyline points="{{.}}">`, `<title>{{.}}</title>`, `<circle cx cy>`, `stroke="var(--…)"` and `data-line="…"`, and confirm the output has no `ZgotmplZ` and no unexpected escaping.
2. **Where the geometry lives.** A line chart's points have to be computed somewhere. That conflicts with the "Go never computes geometry" precedent. Either agree on a scoped exception (a pure function in `web/view.go` that normalizes into a 0..100 viewBox), or find an alternative such as CSS-only or vector-effect. Read the lanes design (`tasks/in-progress-phase-columns/design`) for why that rule exists.
3. **Responsiveness.** At 180-275px wide: `preserveAspectRatio="none"` plus `vector-effect: non-scaling-stroke`, how text scales inside the SVG, and whether day labels go in the SVG or in HTML under it.
4. **Palette.** Up to about 6 lines (split_by priority × 4, resolution × 5), distinguishable on neutral-950, consistent with the existing priority chips (rose/amber/sky/neutral). Decide where colours are set: CSS classes per series index or per field value. Tailwind v4 emits only the variables it uses, so check how to guarantee they appear in `app.css`.
5. **Tooltips and accessibility.** Native `<title>` on a point needs a target big enough to hover in a narrow column. Consider an invisible enlarged circle and an `aria-label` summary.
6. **Edge cases:** all zeros (a flat line, so the y scale has to avoid dividing by zero); `days` = 1; a large `days` (90+) and thinning out the labels.
