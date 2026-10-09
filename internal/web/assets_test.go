package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// TestAppCSSDefinesLaneClasses ties the phase list to the compiled CSS: a
// forgotten scripts/build-css.sh run, or a phase without its placement
// rules, fails here first. The strings are the minified forms Tailwind
// v4.3.3 emits; it rewrites the 14rem collapse rule's "width < 14rem" as
// "not (min-width:14rem)".
func TestAppCSSDefinesLaneClasses(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	css := string(data)
	want := []string{
		".lanes{", "container:lanes/inline-size", "grid-template-columns:",
		".lane{", "lanes not (min-width:14rem)", ".lane-head{", ".lane-card{",
		".sub-lane{",
	}
	for i, p := range task.Phases() {
		// One class per side of a card's span, plus a nested row's indent
		// in steps right of the card's first lane.
		want = append(want,
			".lane-from-"+string(p)+"{",
			".lane-to-"+string(p)+"{",
			".sub-lane-"+strconv.Itoa(i)+"{",
		)
	}
	for _, w := range want {
		if !strings.Contains(css, w) {
			t.Errorf("app.css lacks %q - rerun scripts/build-css.sh", w)
		}
	}
}

// TestAppCSSDefinesPhaseRuns ties the detail view's phase history grid to
// the compiled CSS: a forgotten scripts/build-css.sh run after the phase
// views landed fails here.
func TestAppCSSDefinesPhaseRuns(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	for _, w := range []string{".phase-runs{", ".phase-runs .phase-note{"} {
		if !strings.Contains(string(data), w) {
			t.Errorf("app.css lacks %q - rerun scripts/build-css.sh", w)
		}
	}
}

// TestAppCSSDefinesStatsClasses ties the Done column's statistics markup to
// the compiled CSS: the bar segment classes, the chart and legend classes,
// and the colours Tailwind emits only because a component references them.
func TestAppCSSDefinesStatsClasses(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	for _, w := range []string{
		".stats-card{", ".stats-title{", ".stats-row{", ".stats-bar{", ".stats-recent{",
		".bar-done{",
		".bar-new{",
		".stats-new{", ".bar-recent{", ".bar-in_progress{", ".bar-todo{",
		"--color-emerald-200:",
		".stats-chart{", ".stats-chart svg{", ".stats-line{", ".stats-off{", ".stats-day{",
		".stats-legend{", ".stats-legend-item{", ".stats-legend-item[aria-pressed=false]{", ".stats-swatch{",
		".series-created{", ".series-closed{", ".series-created_cumulative{", ".series-closed_cumulative{",
		".series-0{", ".series-7{",
		"--color-sky-400:", "--color-violet-400:", "--color-lime-400:", "--color-fuchsia-400:",
	} {
		if !strings.Contains(string(data), w) {
			t.Errorf("app.css lacks %q - rerun scripts/build-css.sh", w)
		}
	}
}

// TestAppJSStatsToggles pins the legend toggles' contract without a JS
// runtime: their own storage prefix, re-applied after every htmx swap, and no
// request API, so the board stays read-only.
func TestAppJSStatsToggles(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	js := string(data)
	for _, w := range []string{
		`"mcp-task-manager.stats-line:"`, "localStorage",
		`"htmx:afterSettle"`, `"htmx:load"`, `"htmx:historyRestore"`,
		"data-stats-card", "data-stats-line", "stats-off", "aria-pressed",
	} {
		if !strings.Contains(js, w) {
			t.Errorf("app.js lacks %s", w)
		}
	}
	for _, bad := range []string{"fetch(", "XMLHttpRequest", "htmx.ajax", "hx-", "htmx-history-cache"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js uses %s", bad)
		}
	}
}

// TestAppCSSDefinesWorkspaceClasses ties the task workspace to the compiled
// CSS. Three of these strings carry a design decision rather than a style, so
// a well-meant edit that breaks them should fail here:
//
//   - "overflow:clip visible" on .stage. Tailwind's minifier folds the two
//     axes into the shorthand. It must stay clip-plus-visible: "hidden" would
//     make the stage a scroll container, drag the vertical axis away from
//     visible and break the panel's lg:sticky - and it would give the strip
//     something to pan, which clip structurally denies.
//   - the strip-slide keyframes. The slide must be an animation, not a
//     transition: htmx replaces the whole #strip, and a freshly inserted
//     element has no previous value to transition from.
//   - the viewport-height workspace. Every column is one screen tall and
//     scrolls inside its own .pane, so the page itself never scrolls.
//   - the prefers-reduced-motion block. It is the only motion the workspace
//     adds and the first such rule in this project.
func TestAppCSSDefinesWorkspaceClasses(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	css := string(data)

	want := []string{
		".workspace{",
		".stage{",
		"overflow:clip visible",
		".strip{",
		".strip-shifted{",
		"@keyframes strip-slide",
		".unit{",
		".unit-working{",
		".pane{",
		// The board is not its own scroll container: its pane clips and each
		// status column scrolls inside .column-body, so a long To do queue
		// cannot push In progress off the screen.
		".pane-board{overflow:hidden}",
		".board{",
		".board-grid{",
		".column-body{",
		"@keyframes unit-in",
		"height:calc(100dvh - var(--shell-chrome))",
		".rail{",
		".rail-step{",
		".rail-current{",
		".rail-label{",
		".rail-ref{",
		".col-head{",
		".col-section{",
		".col-link{",
		".col-file-body{",
		// The rendered-artifact scope. Element selectors inside .notes, so
		// a missing rebuild shows a task's design as unstyled HTML.
		// The backlog graph: the node box, the two text styles, the
		// positional edge colours and the legend swatch.
		// The page shell's in-progress indicator.
		".shell-danger{",
		".shell-danger-item{",
		".graph-svg{",
		".graph-box{",
		".graph-label{",
		".graph-edge{",
		".graph-edge-parent{",
		".graph-rel-0{",
		".graph-rel-5{",
		".graph-legend{",
		".graph-divider{",
		".notes{",
		".notes h1{",
		".notes h3,",
		".notes pre{",
		".notes table{",
		// Tailwind rewrites "width < 64rem" as a negated min-width.
		"@media not all and (min-width:64rem)",
		"@media (prefers-reduced-motion:reduce)",
	}
	// Every registered kind must have its width class compiled, or a column
	// of that kind would lay out with no width at all. Two kinds of the same
	// width are merged into one selector list by Tailwind (.kind-file and
	// .kind-desc are both 44rem), so the class may be followed by a comma
	// rather than by the brace.
	defines := func(class string) bool {
		return strings.Contains(css, "."+class+"{") ||
			strings.Contains(css, "."+class+",")
	}
	for tag, k := range columnKinds {
		if !defines(k.Class) {
			t.Errorf("app.css lacks a width rule for kind %q (.%s)", tag, k.Class)
		}
	}
	if !defines("kind-board") {
		t.Error("app.css lacks a width rule for the board unit (.kind-board)")
	}
	want = append(want, ".col-selected{")

	for _, w := range want {
		if !strings.Contains(css, w) {
			t.Errorf("app.css lacks %q - rerun scripts/build-css.sh", w)
		}
	}
}

// TestAppJSPreservesPaneScroll pins the scroll-keeping block. Columns scroll
// inside themselves, so a poll replaces markup inside scroll containers and
// the browser clamps scrollTop to 0 while the old node is gone: without this
// every column jumps to the top every five seconds. The panel is in the map
// too, since an open workspace polls the whole strip and replaces it with the
// same task's content. The bans are the same as the stats toggles' - this
// stays a client-side listener with no request and no htmx attribute in the
// markup.
func TestAppJSPreservesPaneScroll(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	js := string(data)
	for _, w := range []string{
		"data-pane", "scrollTop",
		`"htmx:beforeSwap"`, `"htmx:afterSwap"`, `"htmx:afterSettle"`,
	} {
		if !strings.Contains(js, w) {
			t.Errorf("app.js lacks %s", w)
		}
	}
	// The panel is no longer excluded: a strip poll replaces it with the
	// same task's content, so it keeps its place like any other pane.
	if strings.Contains(js, ":not(#panel)") {
		t.Error("app.js still leaves the panel out of the offset map")
	}
	for _, bad := range []string{"fetch(", "XMLHttpRequest", "htmx.ajax", "hx-", "htmx-history-cache"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js uses %s", bad)
		}
	}
}

// TestAppJSResetsPanelScroll pins the other half of the panel's scroll model:
// #panel is a scroll container too, and an innerHTML swap keeps its
// scrollTop, so a panel whose content is a DIFFERENT task has to be put back
// to its top explicitly. That case is the swap target being #panel - a card
// click - which is why the rule is keyed on the target rather than on leaving
// the panel out of the map, and why the reset clears the stored offset as
// well: afterSwap fires before afterSettle, so an offset left behind would be
// restored one event later.
func TestAppJSResetsPanelScroll(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	js := string(data)
	for _, w := range []string{
		`"htmx:afterSwap"`, `"panel"`, "scrollTop = 0",
		`offsets[target.getAttribute("data-pane")] = 0`,
	} {
		if !strings.Contains(js, w) {
			t.Errorf("app.js lacks %s", w)
		}
	}
}
