package web

import (
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// fixedNow keeps "x ago" strings deterministic.
var fixedNow = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

func newTestHandler(t *testing.T) (http.Handler, *task.Service, string) {
	t.Helper()
	rs, svc, dir := testsupport.NewBacklog(t)
	h := NewHandler(Deps{
		Project: rs.Current,
		Logger:  log.New(io.Discard, "", 0),
		Now:     func() time.Time { return fixedNow },
	})
	return h, svc, dir
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func seedBoard(t *testing.T, svc *task.Service) {
	t.Helper()
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "1", Title: "Ship the board", Priority: "high", Type: "feature"},
		testsupport.TaskSpec{ID: "2", Title: "Write templates", Priority: "critical", Type: "feature", ParentID: "1"},
		testsupport.TaskSpec{ID: "3", Title: "Fix the index", Priority: "low", Type: "bug", Status: "in_progress"},
		testsupport.TaskSpec{ID: "4", Title: "Old chore", Priority: "medium", Type: "bug", Status: "done"},
		testsupport.TaskSpec{ID: "5", Title: "Blocked work", Priority: "medium", Type: "feature", BlockedBy: []string{"3"}},
	)
}

func TestBoardRendersThreeColumns(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{"To do", "In progress", "Done"} {
		if !strings.Contains(body, want) {
			t.Errorf("board is missing the %q column", want)
		}
	}
	for _, want := range []string{"Ship the board", "Fix the index", "Blocked work"} {
		if !strings.Contains(body, want) {
			t.Errorf("board is missing card %q", want)
		}
	}
	if strings.Contains(body, "Old chore") {
		t.Error("the Done column renders a task card, want statistics only")
	}
	if !strings.Contains(body, "<html") {
		t.Error("GET / did not return a full page")
	}
}

func TestBoardCardBadges(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/board").Body.String()

	if strings.Contains(body, "<html") {
		t.Error("/board returned a full page; it is the htmx fragment target")
	}
	for _, want := range []string{
		"chip-critical",                     // priority chip
		"chip-blocked",                      // task 5 is blocked by 3
		"chip-live",                         // task 3 is in progress
		"An agent may be working right now", // danger zone banner
		"0/1 subtasks",                      // the subtask tally, from the snapshot
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board fragment is missing %q", want)
		}
	}
}

func TestBoardWithoutInProgressHidesDangerZone(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "1", Title: "Quiet"})

	if body := get(t, h, "/board").Body.String(); strings.Contains(body, "An agent may be working") {
		t.Error("danger-zone banner rendered with nothing in progress")
	}
}

func TestDetailPage(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	if err := svc.WriteTaskFile("1", "research.md", "findings"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	rec := get(t, h, "/tasks/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "<html") {
		t.Error("/tasks/{id} is the deep-linkable page and must be a full document")
	}
	for _, want := range []string{"Ship the board", "Write templates", "research.md"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page is missing %q", want)
		}
	}
}

func TestDetailPanelIsFragment(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/tasks/5/panel").Body.String()
	if strings.Contains(body, "<html") {
		t.Error("/tasks/{id}/panel returned a full page; it is swapped into #panel")
	}
	if !strings.Contains(body, "Blocked by") {
		t.Error("panel does not name the blocker")
	}
}

func TestDetailUnknownID404(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	if rec := get(t, h, "/tasks/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /tasks/nope = %d, want 404", rec.Code)
	}
}

// TestDetailPanelForMissingTask covers the ordinary race: the board was drawn,
// then the task went away before the click landed.
func TestDetailPanelForMissingTask(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	rec := get(t, h, "/tasks/nope/panel")
	if rec.Code != http.StatusOK {
		t.Errorf("panel for a missing task = %d, want 200 so htmx swaps it", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gone") {
		t.Error("panel for a missing task does not say the task is gone")
	}
}

func TestDetailArchivedTask(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "9", Title: "Finished thing", Status: "done"})
	if err := svc.ArchiveTask("9"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	if body := get(t, h, "/board").Body.String(); strings.Contains(body, "Finished thing") {
		t.Error("archived task appears on the board; the board is the active index only")
	}

	rec := get(t, h, "/tasks/9")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks/9 = %d, want 200: the detail route falls back to the archive", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Archived") {
		t.Error("archived detail page has no archived banner")
	}
}

func TestHealthz(t *testing.T) {
	h, _, _ := newTestHandler(t)
	rec := get(t, h, "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("GET /healthz = %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}
}

func TestStaticAssetsServed(t *testing.T) {
	h, _, _ := newTestHandler(t)
	for _, path := range []string{"/static/app.css", "/static/htmx.min.js", "/static/app.js"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		if strings.HasSuffix(path, ".js") && !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
			t.Errorf("GET %s Content-Type = %q, want JavaScript", path, rec.Header().Get("Content-Type"))
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s served an empty file", path)
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("GET %s Cache-Control = %q, want an immutable policy", path, cc)
		}
	}
}

// TestNoExternalAssetReferences is the offline guarantee: everything the page
// loads has to come out of the binary.
func TestNoExternalAssetReferences(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	for _, path := range []string{"/", "/tasks/1"} {
		body := get(t, h, path).Body.String()
		for _, attr := range []string{`src="`, `href="`} {
			rest := body
			for {
				i := strings.Index(rest, attr)
				if i < 0 {
					break
				}
				rest = rest[i+len(attr):]
				value := rest[:strings.Index(rest, `"`)]
				if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
					t.Errorf("%s references the network: %s%s", path, attr, value)
				}
			}
		}
	}
}

// TestNoMutatingRoutes asserts the read-only guarantee twice over: every
// non-GET method is refused, and a full GET sweep leaves the tasks directory
// byte for byte as it was.
func TestNoMutatingRoutes(t *testing.T) {
	h, svc, dir := newTestHandler(t)
	seedBoard(t, svc)

	paths := []string{"/", "/board", "/tasks/1", "/tasks/1/panel", "/healthz", "/static/app.css"}
	for _, path := range paths {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rec.Code)
			}
		}
	}

	before := snapshotDir(t, dir)
	for _, path := range paths {
		get(t, h, path)
	}
	if after := snapshotDir(t, dir); after != before {
		t.Errorf("a GET sweep changed the tasks directory:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestEscaping feeds markup through every user-controlled field.
func TestEscaping(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	const payload = `<script>alert(1)</script>`
	if _, err := svc.Create(payload, payload, task.PriorityHigh, "feature", "", "1"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// The filename skips the closing tag: a "/" is rejected as a path separator.
	if err := svc.WriteTaskFile("1", "x<script>.md", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	// A branch name is text too, and it also lands in a data-copy attribute.
	// Git would never create this one; a hand-edited record could hold it.
	const branchPayload = `dev/wip/"><script>alert(2)</script>`
	setBranch(t, dir(t, svc), "1", branchPayload, "")

	for _, path := range []string{"/", "/board", "/tasks/1", "/tasks/1/panel"} {
		body := get(t, h, path).Body.String()
		if strings.Contains(body, payload) || strings.Contains(body, branchPayload) {
			t.Errorf("%s rendered a payload unescaped", path)
		}
		if !strings.Contains(body, "&lt;script&gt;") {
			t.Errorf("%s did not render the escaped payload at all", path)
		}
		if !strings.Contains(body, `data-copy="dev/wip/&#34;&gt;&lt;script&gt;alert(2)&lt;/script&gt;"`) {
			t.Errorf("%s did not escape the branch inside data-copy", path)
		}
	}
}

// TestUnresolvedProjectPlaceholder proves the structural rule: a GET can never
// be what first resolves the project, because resolution runs Initialize(),
// which migrates the layout and may auto-archive.
func TestUnresolvedProjectPlaceholder(t *testing.T) {
	testsupport.IsolateEnv(t)
	rs := project.NewResolver(func(context.Context) ([]string, error) {
		t.Fatal("an HTTP request resolved the project")
		return nil, nil
	})
	h := NewHandler(Deps{Project: rs.Current, Logger: log.New(io.Discard, "", 0)})

	for _, path := range []string{"/", "/board"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 with a placeholder", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "No project resolved yet") {
			t.Errorf("GET %s did not render the placeholder", path)
		}
	}
	if rec := get(t, h, "/tasks/1"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /tasks/1 with no project = %d, want 404", rec.Code)
	}
}

func TestInvalidateShowsPlaceholderAgain(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	seedBoard(t, svc)
	h := NewHandler(Deps{Project: rs.Current, Logger: log.New(io.Discard, "", 0)})

	if body := get(t, h, "/board").Body.String(); !strings.Contains(body, "Ship the board") {
		t.Fatal("board did not render the seeded backlog")
	}

	rs.Invalidate()
	if body := get(t, h, "/board").Body.String(); !strings.Contains(body, "No project resolved yet") {
		t.Error("board still renders data after the resolution was invalidated")
	}
}

// snapshotDir renders a recursive listing with sizes and mtimes.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		lines = append(lines, rel+" "+info.ModTime().UTC().Format(time.RFC3339Nano)+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("WalkDir() error = %v", err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestCardIDTruncates guards narrow cards: an id that does not fit shrinks
// with an ellipsis and keeps its full text as a tooltip, and a long title
// word wraps instead of pushing the card wider.
func TestCardIDTruncates(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	const long = "long-custom-id-for-the-overflow-check"
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: long, Title: "Parent with a long id"},
		testsupport.TaskSpec{ID: long + "-nested", ParentID: long},
		testsupport.TaskSpec{ID: long + "-live", ParentID: long, Status: "in_progress"},
	)

	body := get(t, h, "/board").Body.String()
	for _, want := range []string{
		`<span class="meta truncate" title="` + long + `">#` + long + `</span>`,
		`<span class="meta truncate" title="` + long + `-nested">#` + long + `-nested</span>`,
		`<span class="meta truncate" title="subtask of ` + long + `">`,
		`break-words`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board is missing %q", want)
		}
	}
}

// sectionAfter returns the body from marker to the first closing section
// tag after it. Lanes hold only card articles and statistics cards hold no
// sections, so for either that tag closes the section the marker opens.
func sectionAfter(t *testing.T, body, marker string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("body has no %s", marker)
	}
	section, _, ok := strings.Cut(rest, "</section>")
	if !ok {
		t.Fatalf("%s is never closed", marker)
	}
	return section
}

// laneSection returns the In progress lane of phase.
func laneSection(t *testing.T, body, phase string) string {
	t.Helper()
	return sectionAfter(t, body, `data-phase="`+phase+`"`)
}

func TestBoardRendersPhaseLanes(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	if err := svc.WriteTaskFile("3", "design", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	body := get(t, h, "/board").Body.String()
	if n := strings.Count(body, `class="lanes"`); n != 1 {
		t.Errorf("board has %d lane stacks, want 1", n)
	}
	last := -1
	for _, phase := range []string{"research", "design", "planning", "implementation"} {
		i := strings.Index(body, `class="lane lane-`+phase+`" data-phase="`+phase+`"`)
		if i <= last {
			t.Errorf("lane %s at %d, want after %d", phase, i, last)
		}
		last = i
	}

	planning := laneSection(t, body, "planning")
	for _, want := range []string{"Fix the index", "chip-live", `hx-get="/tasks/3/panel"`, ">1</span>"} {
		if !strings.Contains(planning, want) {
			t.Errorf("planning lane is missing %q", want)
		}
	}
	for _, phase := range []string{"research", "design", "implementation"} {
		s := laneSection(t, body, phase)
		if strings.Contains(s, "<article") {
			t.Errorf("%s lane holds a card, want none", phase)
		}
		if !strings.Contains(s, ">0</span>") {
			t.Errorf("%s lane does not count 0", phase)
		}
	}
	if n := strings.Count(body, `hx-get="/tasks/3/panel"`); n != 1 {
		t.Errorf("task 3 renders %d times, want once", n)
	}
}

func TestBoardLaneGroupsByFurthestPhase(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "10", Status: "in_progress"},
		testsupport.TaskSpec{ID: "11", ParentID: "10", Status: "in_progress"},
	)
	if err := svc.WriteTaskFile("11", "plan.md", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	body := get(t, h, "/board").Body.String()
	impl := laneSection(t, body, "implementation")
	for _, want := range []string{"#10</span>", "#11</span>", ">2</span>"} {
		if !strings.Contains(impl, want) {
			t.Errorf("implementation lane is missing %q", want)
		}
	}
	if strings.Contains(laneSection(t, body, "research"), "<article") {
		t.Error("research lane holds a card, want none")
	}
}

func TestBoardEmptyInProgressShowsFourStubs(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "1"})

	body := get(t, h, "/board").Body.String()
	if n := strings.Count(body, `data-phase="`); n != 4 {
		t.Errorf("board has %d lanes, want 4", n)
	}
	for _, phase := range []string{"research", "design", "planning", "implementation"} {
		if strings.Contains(laneSection(t, body, phase), "<article") {
			t.Errorf("%s lane holds a card, want none", phase)
		}
	}
	if n := strings.Count(body, ">empty</p>"); n != 0 {
		t.Errorf("board shows %d empty placeholders, want 0 (Done holds the statistics cards)", n)
	}
	if !strings.Contains(body, `data-stats-card="`) {
		t.Error("the Done column shows no statistics card")
	}
}

func TestBoardShowsPhaseFromRecords(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	// Task 3 is in progress with a design artifact, so the names alone
	// would put it in Planning; its records put it in Design.
	if err := svc.WriteTaskFile("3", "design", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if _, _, err := svc.StartPhase("3", task.PhaseResearch); err != nil {
		t.Fatalf("StartPhase(research) error = %v", err)
	}
	tokens := int64(81234)
	if _, _, err := svc.FinishPhase("3", task.PhaseResearch, task.PhaseFinish{Tokens: &tokens}); err != nil {
		t.Fatalf("FinishPhase(research) error = %v", err)
	}
	if _, _, err := svc.StartPhase("3", task.PhaseDesign); err != nil {
		t.Fatalf("StartPhase(design) error = %v", err)
	}

	body := get(t, h, "/board").Body.String()
	design := laneSection(t, body, "design")
	for _, want := range []string{"Fix the index", "design &middot; started ", " by dev</span>", ">81.2k tok</span>", `title="81234 tokens"`, "created "} {
		if !strings.Contains(design, want) {
			t.Errorf("design lane is missing %q:\n%s", want, design)
		}
	}
	if strings.Contains(laneSection(t, body, "planning"), "Fix the index") {
		t.Error("the file names still decide the lane")
	}

	detail := get(t, h, "/tasks/3").Body.String()
	for _, want := range []string{`class="phase-runs`, "research #1", "design #1", "81.2k tokens", `<span class="chip chip-live">open</span>`, "created by"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail is missing %q", want)
		}
	}
	if strings.Contains(detail, `<li class="chip chip-muted">research.phase</li>`) {
		t.Error("detail lists a phase record as an attached file")
	}
}

func TestPhaseNoteEscaped(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	if _, _, err := svc.StartPhase("3", task.PhaseResearch); err != nil {
		t.Fatalf("StartPhase() error = %v", err)
	}
	if _, _, err := svc.FinishPhase("3", task.PhaseResearch, task.PhaseFinish{Note: "<script>alert(1)</script>"}); err != nil {
		t.Fatalf("FinishPhase() error = %v", err)
	}
	body := get(t, h, "/tasks/3").Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("a phase note is rendered unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the escaped note is missing")
	}
}

func TestBoardDoneColumnRendersStats(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/board").Body.String()
	for _, id := range []string{"bars-priority", "bars-type", "bars-resolution"} {
		if !strings.Contains(body, `data-stats-card="`+id+`"`) {
			t.Errorf("Done column lacks the %s card", id)
		}
	}
	if a, b := strings.Index(body, `data-stats-card="bars-priority"`), strings.Index(body, `data-stats-card="bars-type"`); a > b {
		t.Error("stats cards are not in config order")
	}
	// Task 4 (medium bug) is the only done task, closed just now; task 5
	// is the other medium one, still todo.
	for _, want := range []string{
		`data-value="medium"`,
		`1/2 &middot; 1 open &middot; <span class="stats-recent">+1</span>`,
		`viewBox="0 0 2 1"`,
		`<rect class="bar-done" x="0" width="1" height="1"/>`,
		`<rect class="bar-recent" x="0" width="1" height="1"/>`,
		`<rect class="bar-todo" x="1" width="1" height="1"/>`,
		`data-value="completed"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Done column lacks %s", want)
		}
	}
	if strings.Contains(body, "Old chore") || strings.Contains(body, `hx-get="/tasks/4/panel"`) {
		t.Error("the Done column renders task 4 as a card")
	}
	if strings.Contains(body, "style=") {
		t.Error("the board carries an inline style")
	}
}

func TestBoardDoneColumnEmptyWithoutCards(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	svc.Config().Web.DoneStats.Cards = []config.StatsCard{}

	body := get(t, h, "/board").Body.String()
	if strings.Contains(body, "data-stats-card") {
		t.Error("cards: [] still renders statistics cards")
	}
	if n := strings.Count(body, ">empty</p>"); n != 1 {
		t.Errorf("board shows %d empty placeholders, want 1 (Done)", n)
	}
	if strings.Contains(body, "Old chore") {
		t.Error("an empty Done column falls back to task cards")
	}
}

func TestStatsBarsEscaping(t *testing.T) {
	const payload = `<script>alert(1)</script>`
	card := StatsCardView{ID: `x" onmouseover="y`, Title: payload,
		Bars: []StatsBarView{{Value: payload, Total: 1, Done: 1}}}

	var b strings.Builder
	if err := fragments.ExecuteTemplate(&b, "_stats_bars.html", card); err != nil {
		t.Fatalf("execute _stats_bars.html: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "<script>") || strings.Contains(out, `" onmouseover="`) {
		t.Errorf("stats card output is not escaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("escaped payload missing:\n%s", out)
	}
}

func TestBoardDoneColumnRendersLines(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/board").Body.String()
	if strings.Index(body, `data-stats-card="lines-14d"`) < strings.Index(body, `data-stats-card="bars-resolution"`) {
		t.Error("the lines card is not after the bars cards (config order)")
	}
	card := sectionAfter(t, body, `data-stats-card="lines-14d"`)
	// All five seeded tasks were created today and task 4 closed today, so
	// the per-day scale is 5 and today's points are created 5, closed 1,
	// whatever the date.
	for _, want := range []string{
		`viewBox="0 0 28 1"`,
		`viewBox="0 0 28 5"`,
		`<polyline class="stats-line series-created" data-stats-line="created" points="1,5 `,
		` 27,0"/>`,
		`<polyline class="stats-line series-closed" data-stats-line="closed" points="1,5 `,
		` 27,4"/>`,
		`<button type="button" class="stats-legend-item" data-stats-line="created" aria-pressed="true"`,
		`<button type="button" class="stats-legend-item" data-stats-line="closed" aria-pressed="true"`,
		`>5/day<`,
		` · created 5 · closed 1</title>`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("lines card lacks %s", want)
		}
	}
	if n := strings.Count(card, `<rect class="stats-day"`); n != 14 {
		t.Errorf("lines card has %d day columns, want 14", n)
	}
	for _, bad := range []string{"style=", "hx-", "stats-off"} {
		if strings.Contains(card, bad) {
			t.Errorf("lines card carries %q", bad)
		}
	}
}

func TestBoardHiddenLineRendersOff(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	svc.Config().Web.DoneStats.Cards = []config.StatsCard{{
		ID: "recent", Kind: config.StatsKindLines, Title: "Recent", Days: 7,
		Lines: []string{"created", "closed_cumulative"}, Hidden: []string{"closed_cumulative"},
	}}

	body := get(t, h, "/board").Body.String()
	for _, want := range []string{
		`<polyline class="stats-line series-created" data-stats-line="created"`,
		`<polyline class="stats-line series-closed_cumulative stats-off" data-stats-line="closed_cumulative"`,
		`data-stats-line="closed_cumulative" aria-pressed="false"`,
		`data-stats-line="created" aria-pressed="true"`,
		`5/day &middot; 1 total`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board lacks %s", want)
		}
	}
}

// TestStatsLegendHasNoHtmxAttributes keeps the line toggles client-side: the
// legend entries and the lines they switch must never be wired to a request.
func TestStatsLegendHasNoHtmxAttributes(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	tag := regexp.MustCompile(`<(?:button[^>]*stats-legend-item|polyline)[^>]*>`)
	for _, path := range []string{"/", "/board"} {
		tags := tag.FindAllString(get(t, h, path).Body.String(), -1)
		if len(tags) == 0 {
			t.Errorf("%s has no legend entries or lines", path)
		}
		for _, tag := range tags {
			if strings.Contains(tag, "hx-") {
				t.Errorf("%s: legend or line carries an htmx attribute: %s", path, tag)
			}
		}
	}
}

func TestStatsLinesEscaping(t *testing.T) {
	const payload = `<script>alert(1)</script>`
	line := StatsSeriesView{Key: `x" onmouseover="y` + payload, Color: "series-0", Points: "1,0"}
	card := StatsCardView{ID: `x" onmouseover="y`, Kind: config.StatsKindLines, Title: payload,
		Chart: &StatsChartView{Width: 2, First: payload, Last: payload,
			Groups: []StatsLineGroupView{{Width: 2, Max: 1, Lines: []StatsSeriesView{line}}},
			Legend: []StatsSeriesView{line},
			Days:   []StatsDayView{{X: 0, Title: payload}}}}

	var b strings.Builder
	if err := fragments.ExecuteTemplate(&b, "_stats_lines.html", card); err != nil {
		t.Fatalf("execute _stats_lines.html: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "<script>") || strings.Contains(out, `" onmouseover="`) || strings.Contains(out, "ZgotmplZ") {
		t.Errorf("lines card output is not escaped:\n%s", out)
	}
	if !strings.Contains(out, "<title>&lt;script&gt;") {
		t.Errorf("escaped day title missing:\n%s", out)
	}
}
