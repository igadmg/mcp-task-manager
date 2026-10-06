# Research request: done-stats-bars

**Additional research needed: 40 / 100**

## Already covered by research.md
- Done-column construction in `newBoardView` and the Lanes precedent (`ColumnView.Lanes`, `{{ if .Lanes }}`).
- Templates: the `_*.html` glob, no FuncMap, no `template.HTML`, plain-string view models.
- The board grid and an estimated Done column width (180-275px; arithmetic only, not measured).
- CSS conventions: status and priority colours, `.card`, `.meta`; the dark-only theme; the "Go never computes geometry" precedent.
- The six tests that break when done cards go away; the `assets_test` guards.
- The loss of the final-branch chip on done cards.

## Gaps
1. **How to set segment widths without geometry in Go.** A percentage per value is data, not a fixed set of classes. Check:
   - whether `style="width: N%"` or `style="--w: N"` passes through html/template (CSS context) without `ZgotmplZ`;
   - whether the CSS custom-property approach works with Tailwind v4 (`w-[calc(var(--w)*1%)]`);
   - whether `<progress>`/`<meter>` fits a stacked bar;
   - and agree that rule with the lanes precedent.
2. **Tailwind scan scope.** research.md left the contradiction open: `build-css.sh` scans the cwd, while `input.css:6-8` says only templates. Run the script from the repo root and from `scripts/`, then compare which classes end up in `app.css`. Without this we cannot know whether new classes need to go into `input.css`.
3. **Real Done column width** at 1280 and 1600 px, measured with `/run` or a browser, plus whether the label `7/12 · 5 open · +2` fits in one line.
4. **Colour for "closed in the last 24 h"** inside the emerald done segment, so it stays distinguishable on the dark background. Check which Tailwind colour variables would be newly emitted.
5. **What happens to the existing done-card tests.** For each of the six, decide whether to rewrite, delete or move the check to the To do or In progress columns. `TestBoardShowsBranchChip` and `TestCopyButtonHasNoHtmxAttributes` lose their only done task with a branch.
