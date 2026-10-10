package web

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
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
		".bar-done{", ".bar-done{fill:var(--role-done)}", ".bar-done-recent{fill:var(--role-recent)}",
		".bar-in_progress{fill:var(--role-in_progress)}", ".bar-todo{fill:var(--role-todo)}",
		".bar-todo-new{fill:var(--role-new)}", ".chip-new{",
		".bar-todo-new{",
		".stats-new{", ".bar-done-recent{", ".bar-in_progress{", ".bar-todo{",
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
//     And it must not ride .strip-shifted (or .unit-working): every poll
//     inserts a fresh #strip, so it would replay every five seconds. It rides
//     .strip-enter / .unit-enter, which app.js adds only on a real change.
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
		".strip-enter{",
		".unit-enter{",
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
// a panel whose content is a DIFFERENT task opens at its top. A card click
// swaps the whole strip, so the swap target cannot tell that case from a poll
// of the same task; the panel's data-task does. The reset clears the stored
// offset as well: afterSwap fires before afterSettle, so an offset left
// behind would be restored one event later.
func TestAppJSResetsPanelScroll(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	js := string(data)
	for _, w := range []string{
		`"htmx:afterSwap"`, `getElementById("panel")`, `"data-task"`, "scrollTop = 0",
		`offsets[panel.getAttribute("data-pane")] = 0`,
	} {
		if !strings.Contains(js, w) {
			t.Errorf("app.js lacks %s", w)
		}
	}
}

// TestAppJSAnimatesOnlyOnChange pins the fix for a strip that slid away again
// on every poll and every file click: app.js compares the strip it replaces
// with the new one and adds the animated classes only for a real change.
func TestAppJSAnimatesOnlyOnChange(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	js := string(data)
	for _, w := range []string{
		`target.id !== "strip"`, `".strip-shifted"`, `!was.shifted`,
		`classList.add("strip-enter")`, `".unit-working"`, `classList.add("unit-enter")`,
	} {
		if !strings.Contains(js, w) {
			t.Errorf("app.js lacks %s", w)
		}
	}
}

// TestNoAnimationInMarkup: the server says the state, never the motion. An
// enter class in the markup would replay on every poll of the strip.
func TestNoAnimationInMarkup(t *testing.T) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}
	for _, e := range entries {
		data, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, bad := range []string{"strip-enter", "unit-enter"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s carries %s", e.Name(), bad)
			}
		}
	}
}

// TestAppCSSDefinesDescToggle pins the description block's busy indicator in
// the compiled CSS: without the rebuild the hidden "Loading source" element
// would stay visible for good.
func TestAppCSSDefinesDescToggle(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	css := string(data)
	for _, want := range []string{".desc-busy{display:none}", ".htmx-request .desc-busy{"} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css lacks %q: run scripts/build-css.sh", want)
		}
	}
}

// readInputCSS reads the Tailwind entry. It is not embedded, so the test
// reads it from the package directory, like the build script does.
func readInputCSS(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("assets/input.css")
	if err != nil {
		t.Fatalf("read assets/input.css: %v", err)
	}
	return string(data)
}

// ruleBody returns the text between the braces of the first rule in css whose
// selector is exactly sel. input.css has no nested braces in the rules used.
func ruleBody(t *testing.T, css, sel string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		t.Fatalf("input.css has no rule %q", sel)
	}
	return m[1]
}

// TestColorAllowlistMatchesCSS ties the Go vocabulary of internal/config to
// the stylesheet: the @source inline lines, the compiled role classes and the
// :root defaults. The allowlist lives in both places because Tailwind needs
// the names at build time; this is what keeps them one list.
func TestColorAllowlistMatchesCSS(t *testing.T) {
	input := readInputCSS(t)
	hues := "{" + strings.Join(config.ColorHues, ",") + "}"
	shades := make([]string, len(config.ColorShades))
	for i, sh := range config.ColorShades {
		shades[i] = strconv.Itoa(sh)
	}
	for _, role := range config.ColorRoles() {
		line := `@source inline("role-` + role + `-` + hues + `-{` + strings.Join(shades, ",") + `}");`
		if !strings.Contains(input, line) {
			t.Errorf("input.css lacks %s", line)
		}
		if !strings.Contains(input, "@utility role-"+role+"-* { --role-"+role+": --value(--color-*); }") {
			t.Errorf("input.css lacks the @utility for role %q", role)
		}
		if want := "--role-" + role + ": var(--color-" + config.DefaultColors[role] + ");"; !strings.Contains(input, want) {
			t.Errorf(":root in input.css lacks %q", want)
		}
	}
	// One line per role, plus the Done list's card frames (data-driven class names).
	if n := strings.Count(input, "@source inline("); n != len(config.ColorRoles())+1 {
		t.Errorf("input.css has %d @source inline lines, want %d", n, len(config.ColorRoles())+1)
	}
	if want := `@source inline("card-frame-{todo,in_progress,done,done-recent}");`; !strings.Contains(input, want) {
		t.Errorf("input.css lacks %s", want)
	}

	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	css := string(data)
	for _, name := range config.ColorNames() {
		if !strings.Contains(css, "--color-"+name+":") {
			t.Errorf("app.css lacks --color-%s - rerun scripts/build-css.sh", name)
		}
		for _, role := range config.ColorRoles() {
			if want := ".role-" + role + "-" + name + "{--role-" + role + ":var(--color-" + name + ")}"; !strings.Contains(css, want) {
				t.Errorf("app.css lacks %q - rerun scripts/build-css.sh", want)
			}
		}
	}
}

// TestStatusColoursComeFromTheRoles fails when a status consumer goes back to
// a hard-coded colour: it would no longer follow web.colors.
func TestStatusColoursComeFromTheRoles(t *testing.T) {
	input := readInputCSS(t)
	hard := regexp.MustCompile(`--color-(amber|emerald|sky|neutral)-\d+|\b(bg|text|ring|border|stroke|fill|decoration)-(amber|emerald|sky|neutral)-\d+`)
	for _, sel := range []string{
		".dot-todo", ".dot-in_progress", ".dot-done",
		".bar-done", ".bar-done-recent", ".bar-in_progress", ".bar-todo", ".bar-todo-new",
		".stats-recent", ".stats-new",
		".card-live", ".chip-live", ".chip-new",
		".card-frame-todo", ".card-frame-in_progress", ".card-frame-done", ".card-frame-done-recent",
		".shell-danger-item", ".shell-danger-item:hover",
		".graph-todo        .graph-box", ".graph-in_progress .graph-box", ".graph-done        .graph-box",
	} {
		body := ruleBody(t, input, sel)
		if m := hard.FindString(body); m != "" {
			t.Errorf("%s hard-codes %q", sel, m)
		}
		if !strings.Contains(body, "var(--role-") {
			t.Errorf("%s does not read a role colour: %q", sel, body)
		}
	}
}

// TestAppCSSSizeBound is a tripwire on the allowlist's cost: the role classes
// were measured at about +17 KB over the 48664 B before the palette.
func TestAppCSSSizeBound(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	if n := len(data); n > 70*1024 {
		t.Errorf("app.css is %d B, want under 70 KB - did the allowlist grow?", n)
	}
}
