package web

import (
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	seedWorkspace(t, svc)

	for _, path := range []string{"/", "/tasks/1", "/tasks/1/w/f/research.md", "/strip/tasks/1"} {
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
	seedWorkspace(t, svc)

	paths := []string{
		"/", "/board", "/tasks/1", "/tasks/1/panel", "/tasks/1/files/research.md",
		"/tasks/1/w/f/research.md", "/tasks/1/w/t/2", "/strip/tasks/1", "/strip/",
		"/healthz", "/static/app.css",
	}
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

	// The payload also travels in a chain URL: the file column's ref is the
	// attacking file name, escaped on the way out and on the way back.
	chainFile := "/tasks/1/w/f/" + url.PathEscape("x<script>.md")
	for _, path := range []string{
		"/", "/board", "/tasks/1", "/tasks/1/panel",
		chainFile, "/strip" + chainFile, "/strip/tasks/1",
	} {
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
	for _, path := range []string{"/tasks/1", "/tasks/1/w/f/plan.md", "/strip/tasks/1"} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s with no project = %d, want 404", path, rec.Code)
		}
	}
	// The bare strip is the board, so it keeps the placeholder.
	if rec := get(t, h, "/strip/"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "No project resolved yet") {
		t.Errorf("GET /strip/ with no project = %d, want 200 with a placeholder", rec.Code)
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
		i := strings.Index(body, `class="lane" data-phase="`+phase+`"`)
		if i <= last {
			t.Errorf("lane %s at %d, want after %d", phase, i, last)
		}
		last = i
	}

	planning := laneSection(t, body, "planning")
	for _, want := range []string{
		"Fix the index", "chip-live", `hx-get="/tasks/3/panel"`, ">1</span>",
		// A card with no in-progress subtasks occupies its own lane only.
		`class="lane-card lane-from-planning lane-to-planning"`,
		`class="lane-head lane-from-planning lane-to-planning `,
	} {
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

// TestBoardLaneGroupSpansItsSubtasks: parent 10 has no artifact, so it
// reads as research, while its subtask 11 has a plan and reads as
// implementation. The card is drawn in the lane it begins in, spans
// research..implementation, and its nested row is shifted three steps; each
// of the two tasks counts in the lane of its own phase.
func TestBoardLaneGroupSpansItsSubtasks(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "10", Status: "in_progress"},
		testsupport.TaskSpec{ID: "11", ParentID: "10", Status: "in_progress"},
	)
	if err := svc.WriteTaskFile("11", "plan.md", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	body := get(t, h, "/board").Body.String()
	research := laneSection(t, body, "research")
	for _, want := range []string{
		"#10</span>", "#11</span>", ">1</span>",
		`class="lane-card lane-from-research lane-to-implementation"`,
		`class="sub-lane sub-lane-3 `,
	} {
		if !strings.Contains(research, want) {
			t.Errorf("research lane is missing %q:\n%s", want, research)
		}
	}
	impl := laneSection(t, body, "implementation")
	if strings.Contains(impl, "<article") {
		t.Error("implementation lane holds a card, want the spanning card in research")
	}
	if !strings.Contains(impl, ">1</span>") {
		t.Errorf("implementation lane does not count subtask 11:\n%s", impl)
	}
	for _, phase := range []string{"design", "planning"} {
		if s := laneSection(t, body, phase); !strings.Contains(s, ">0</span>") {
			t.Errorf("%s lane does not count 0:\n%s", phase, s)
		}
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

// TestAttachedFileHrefEscaped covers the characters html/template's URL
// normalizer leaves alone: a '#' would start a fragment and a '?' a query, so
// an unescaped href makes the file unreachable. Both names are legal
// attached-file names (task.ValidateAttachedName rejects only empty, a path
// separator and dots-and-spaces-only). Every href is built in Go, which is
// why both the workspace link and the raw route survive them.
func TestAttachedFileHrefEscaped(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	names := []string{"a#b.md", "a?b.md", "my notes.md"}
	for _, name := range names {
		if err := svc.WriteTaskFile("1", name, "content of "+name); err != nil {
			t.Fatalf("WriteTaskFile(%q) error = %v", name, err)
		}
	}

	// The panel is the depth-0 workspace, so its file chips open a column.
	body := get(t, h, "/tasks/1/panel").Body.String()
	for _, want := range []string{
		`href="/tasks/1/w/f/a%23b.md"`,
		`href="/tasks/1/w/f/a%3Fb.md"`,
		`href="/tasks/1/w/f/my%20notes.md"`,
		`hx-get="/strip/tasks/1/w/f/a%23b.md"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel is missing %s", want)
		}
	}

	// Both escaped URLs round-trip: the workspace renders the file, and the
	// raw route serves its bytes.
	for _, name := range names {
		content := "content of " + name
		ws := "/tasks/1/w/f/" + url.PathEscape(name)
		rec := get(t, h, ws)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", ws, rec.Code)
		} else if !strings.Contains(rec.Body.String(), content) {
			t.Errorf("GET %s does not show the file content", ws)
		}

		raw := "/tasks/1/files/" + url.PathEscape(name)
		rec = get(t, h, raw)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", raw, rec.Code)
		} else if got := rec.Body.String(); got != content {
			t.Errorf("GET %s = %q, want %q", raw, got, content)
		}
	}
}

// TestTaskURLIsBoardAndPanel pins the repurposed route: /tasks/{id} is the
// board with that task's panel open, not a surface of its own. It is the
// state a workspace chain of depth 0 names.
func TestTaskURLIsBoardAndPanel(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/tasks/3").Body.String()
	// The board is there: its three column headings and a card of another
	// task.
	for _, want := range []string{"To do", "In progress", "Done", "Ship the board"} {
		if !strings.Contains(body, want) {
			t.Errorf("/tasks/3 is missing the board's %q", want)
		}
	}
	// And so is the panel, inside the #panel aside.
	_, rest, ok := strings.Cut(body, `id="panel"`)
	if !ok {
		t.Fatal("/tasks/3 has no #panel")
	}
	panel, _, ok := strings.Cut(rest, "</aside>")
	if !ok {
		t.Fatal("#panel is never closed")
	}
	if !strings.Contains(panel, "Fix the index") {
		t.Error("#panel does not hold the task's own title")
	}
	if strings.Contains(body, "Pick a card to see its description") {
		t.Error("/tasks/{id} still renders the empty-panel placeholder")
	}
	if !strings.Contains(body, "<title>#3 Fix the index</title>") {
		t.Error("/tasks/{id} does not title the document after the task")
	}

	// The bare board keeps its own title and its placeholder.
	root := get(t, h, "/").Body.String()
	if !strings.Contains(root, "<title>Task board</title>") {
		t.Error("/ lost its title")
	}
	if !strings.Contains(root, "Pick a card to see its description") {
		t.Error("/ lost the empty-panel placeholder")
	}
}

// seedWorkspace gives task 1 a file and task 2 (its subtask) one too, which
// is enough to walk a task-then-file chain.
func seedWorkspace(t *testing.T, svc *task.Service) {
	t.Helper()
	seedBoard(t, svc)
	if err := svc.WriteTaskFile("1", "research.md", "# root findings"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.WriteTaskFile("2", "plan.md", "# subtask plan"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
}

// TestWorkspaceDeepLinks walks every state a chain can be in and asserts each
// one is a whole page that renders its own columns: a deep link and a reload
// are the same request, so this is what "reload restores the layout" means.
func TestWorkspaceDeepLinks(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "a file of the root",
			path: "/tasks/1/w/f/research.md",
			want: []string{`data-column="f"`, `data-ref="research.md"`, "# root findings"},
		},
		{
			name: "a subtask",
			path: "/tasks/1/w/t/2",
			want: []string{`data-column="t"`, `data-ref="2"`, "Write templates"},
		},
		{
			name: "a subtask then its file",
			path: "/tasks/1/w/t/2/f/plan.md",
			want: []string{`data-ref="2"`, `data-ref="plan.md"`, "# subtask plan"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, h, tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.path, rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "<html") {
				t.Error("a chain URL must be a whole document: it is the deep link and the reload")
			}
			// The board is still in the strip at every depth, which is
			// what keeps it polling while off-screen.
			if !strings.Contains(body, `id="board"`) {
				t.Error("the board left the strip")
			}
			if !strings.Contains(body, `id="strip"`) {
				t.Error("no strip to swap")
			}
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("%s is missing %q", tc.path, want)
				}
			}
		})
	}
}

// TestWorkspaceFragmentIsAFragment pairs the fragment route with the page
// route the way /board pairs with /{$}.
func TestWorkspaceFragmentIsAFragment(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	for _, path := range []string{
		"/strip/tasks/1/w/f/research.md",
		"/strip/tasks/1/w/t/2/f/plan.md",
		"/strip/tasks/1",
		"/strip/",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		body := rec.Body.String()
		if strings.Contains(body, "<html") {
			t.Errorf("%s returned a full page; it is swapped into #strip", path)
		}
		if !strings.Contains(body, `id="strip"`) {
			t.Errorf("%s does not carry #strip, so outerHTML would drop the target", path)
		}
	}
}

// TestWorkspaceRailTruncates is the Back mechanism: entry i links to exactly
// the chain cut to i columns, and the last entry is the current one.
func TestWorkspaceRailTruncates(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	body := get(t, h, "/tasks/1/w/t/2/f/plan.md").Body.String()
	rail, _, ok := strings.Cut(body, "</nav>")
	if !ok {
		t.Fatal("the workspace has no rail")
	}
	_, rail, _ = strings.Cut(rail, `class="rail"`)

	for _, want := range []string{
		`href="/tasks/1"`,
		`href="/tasks/1/w/t/2"`,
		`href="/tasks/1/w/t/2/f/plan.md"`,
		`hx-get="/strip/tasks/1"`,
		`hx-target="#strip"`,
		`aria-current="true"`,
	} {
		if !strings.Contains(rail, want) {
			t.Errorf("rail is missing %s:\n%s", want, rail)
		}
	}
	if n := strings.Count(rail, "rail-step"); n != 3 {
		t.Errorf("rail has %d steps, want 3 (root + two columns)", n)
	}
	if n := strings.Count(rail, "rail-current"); n != 1 {
		t.Errorf("rail marks %d current steps, want 1", n)
	}

	// Depth 0 has nothing to step back through, so it has no rail.
	if plain := get(t, h, "/tasks/1").Body.String(); strings.Contains(plain, `class="rail"`) {
		t.Error("/tasks/{id} renders a rail; the board is already the whole strip")
	}
}

// TestWorkspaceColumnLinksAppend covers the other half of the chain: a link
// inside a column opens the next state.
func TestWorkspaceColumnLinksAppend(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// From the root's panel, a file chip appends f/<name>.
	panel := get(t, h, "/tasks/1/panel").Body.String()
	if !strings.Contains(panel, `href="/tasks/1/w/f/research.md"`) {
		t.Error("the panel does not open its file as a column")
	}

	// From a task column, both its files and its subtasks append.
	col := get(t, h, "/tasks/1/w/t/2").Body.String()
	if !strings.Contains(col, `href="/tasks/1/w/t/2/f/plan.md"`) {
		t.Error("a task column does not open its own file")
	}
	// The working column is marked, which is what the layout keys on.
	if !strings.Contains(col, "unit-working") {
		t.Error("no working column is marked")
	}
}

// TestWorkspaceNotFound keeps every bad chain a 404. A chain lives in the URL,
// so a chain that names nothing is a URL that names nothing - never a 500.
func TestWorkspaceNotFound(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	paths := []string{
		"/tasks/nope/w/f/research.md",   // root is gone
		"/tasks/1/w/f/missing.md",       // file is gone
		"/tasks/1/w/t/nope",             // task column is gone
		"/tasks/1/w/z/research.md",      // unknown kind
		"/tasks/1/w/f",                  // malformed pair
		"/tasks/1/w/f/%2E%2E",           // dots-only ref
		"/tasks/1/w/f/a%2Fb.md",         // encoded separator
		"/tasks/1/w/t/2/f/research.md",  // the file belongs to the root, not to task 2
		"/strip/tasks/1/w/f/missing.md", // the fragment route agrees
		"/strip/tasks/nope",             // and on the root
	}
	for _, path := range paths {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}

	// And over the depth cap.
	deep := "/tasks/1/w" + strings.Repeat("/t/2", maxChainDepth+1)
	if rec := get(t, h, deep); rec.Code != http.StatusNotFound {
		t.Errorf("a chain past the cap = %d, want 404", rec.Code)
	}
}

// TestWorkspaceArchivedTask keeps the workspace read-only but reachable for an
// archived task, like the panel already is.
func TestWorkspaceArchivedTask(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "9", Title: "Finished thing", Status: "done"})
	if err := svc.WriteTaskFile("9", "design.md", "# shipped"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.ArchiveTask("9"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	rec := get(t, h, "/tasks/9/w/f/design.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET an archived task's file column = %d, want 200", rec.Code)
	}
	for _, want := range []string{"# shipped", "Archived"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the archived workspace is missing %q", want)
		}
	}
}

// TestWorkspaceRootColumnLeadsTheStrip pins the layout model the browser pass
// settled on: opening anything slides the board off and the root task's own
// column takes its leftmost slot, with the chain's columns to its right. A
// subtask therefore opens as a column beside its parent, not over it.
func TestWorkspaceRootColumnLeadsTheStrip(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// Nothing open: no root column, and the strip is not shifted.
	plain := get(t, h, "/tasks/1").Body.String()
	if strings.Contains(plain, "strip-shifted") {
		t.Error("/tasks/{id} shifts the strip; the board is the whole view until something opens")
	}
	if strings.Contains(plain, `data-column="root"`) {
		t.Error("/tasks/{id} renders a root column next to its own panel")
	}

	// A file of the root: board away, root column first, file column after.
	body := get(t, h, "/tasks/1/w/f/research.md").Body.String()
	if !strings.Contains(body, "strip-shifted") {
		t.Error("the board does not slide off when a column opens")
	}
	rootAt := strings.Index(body, `data-column="root"`)
	fileAt := strings.Index(body, `data-column="f"`)
	if rootAt < 0 {
		t.Fatal("no root task column leads the strip")
	}
	if fileAt < 0 || rootAt > fileAt {
		t.Error("the root column does not come before the opened column")
	}
	// Its links hang off the depth-0 chain, so the root column opens the
	// first column rather than a second one.
	if !strings.Contains(body, `href="/tasks/1/w/t/2"`) {
		t.Error("the root column does not open its subtask as the first column")
	}

	// A subtask: root column, then the subtask's own column beside it.
	sub := get(t, h, "/tasks/1/w/t/2").Body.String()
	rootAt = strings.Index(sub, `data-column="root"`)
	subAt := strings.Index(sub, `data-column="t"`)
	if rootAt < 0 || subAt < 0 || rootAt > subAt {
		t.Error("a subtask does not open to the right of its parent's column")
	}
	// Two columns past the board: the root and the subtask. The board unit
	// carries no data-column, so this counts only the strip's columns.
	if n := strings.Count(sub, `data-column="`); n != 2 {
		t.Errorf("the strip has %d columns, want 2 (root + subtask)", n)
	}

	// A root that is gone takes the whole workspace with it.
	if rec := get(t, h, "/tasks/nope/w/f/x.md"); rec.Code != http.StatusNotFound {
		t.Errorf("a missing root = %d, want 404", rec.Code)
	}
}

// TestTaskColumnIsTheSamePlate pins what a task column looks like: the task's
// detail view, exactly as the side panel renders it. Moving a task to the left
// of the strip must not turn it into a different-looking thing, so the column
// and the panel share one fragment and the column only rebases its links.
func TestTaskColumnIsTheSamePlate(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	panel := get(t, h, "/tasks/1/panel").Body.String()
	column := get(t, h, "/tasks/1/w/f/research.md").Body.String()

	// Everything the panel shows about the task is in the column too.
	for _, want := range []string{
		"Ship the board", // the title
		"Subtasks 0/1",   // the subtask section heading
		"Attached files", // the file section heading
		"research.md",
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("the panel itself is missing %q; fixture is wrong", want)
		}
		if !strings.Contains(column, want) {
			t.Errorf("the task column is missing %q, so it is not the same plate", want)
		}
	}

	// And its links are rebased onto the column's place in the chain, not
	// re-rooted on the task: opening the subtask from the root column
	// appends the first column.
	if !strings.Contains(column, `href="/tasks/1/w/t/2"`) {
		t.Error("the root column does not append its subtask to the chain")
	}

	// A subtask column rebases onto its own position rather than becoming a
	// new root.
	sub := get(t, h, "/tasks/1/w/t/2").Body.String()
	if !strings.Contains(sub, `href="/tasks/1/w/t/2/f/plan.md"`) {
		t.Error("a subtask column re-roots the chain instead of appending to it")
	}
	if strings.Contains(sub, `href="/tasks/2/w/f/plan.md"`) {
		t.Error("a subtask column still links as if it were the chain's root")
	}
}

// TestNoTemplateErrors renders every surface against a task that carries one
// of everything. A template that reads a field its data does not have fails at
// execute time, which render turns into a 500 - so this catches a fragment
// wired to the wrong view model, whatever the fragment is reused by.
func TestNoTemplateErrors(t *testing.T) {
	h, svc, tasksDir := newTestHandler(t)
	seedWorkspace(t, svc)
	// Task 5 is blocked by 3, task 1 has a subtask and files; give 1 a
	// branch, a phase run and a relation so no section is skipped.
	setBranch(t, tasksDir, "1", "dev/wip/1-ship", "")
	if _, _, err := svc.StartPhase("1", task.PhaseResearch); err != nil {
		t.Fatalf("StartPhase() error = %v", err)
	}
	if err := svc.AddRelation("1", "relates_to", "3"); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}

	paths := []string{
		"/", "/board",
		"/tasks/1", "/tasks/1/panel",
		"/tasks/5", "/tasks/5/panel",
		"/tasks/1/w/f/research.md",
		"/tasks/1/w/t/2",
		"/tasks/1/w/t/2/f/plan.md",
		"/strip/", "/strip/tasks/1", "/strip/tasks/1/w/f/research.md",
	}
	for _, path := range paths {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
			continue
		}
		if strings.Contains(rec.Body.String(), "internal error") {
			t.Errorf("GET %s rendered a template error", path)
		}
	}
}

// TestEveryPaneHasAKey ties the markup to app.js: a scroll container without
// a data-pane key is a column whose scroll position is lost on the next board
// poll. Both kinds count - the strip's .pane and a board column's
// .column-body - because app.js keys on the attribute, not on the class.
func TestEveryPaneHasAKey(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	for _, path := range []string{"/", "/tasks/1", "/tasks/1/w/t/2/f/plan.md"} {
		body := get(t, h, path).Body.String()
		panes := strings.Count(body, `class="pane`) +
			strings.Count(body, `class="column-body"`)
		keys := strings.Count(body, "data-pane=")
		if panes == 0 {
			t.Errorf("%s renders no pane", path)
		}
		if panes != keys {
			t.Errorf("%s has %d scroll containers but %d keys", path, panes, keys)
		}
	}

	// The keys are distinct, or two panes would share one offset.
	body := get(t, h, "/tasks/1/w/t/2/f/plan.md").Body.String()
	for _, want := range []string{
		`data-pane="board"`, `data-pane="panel"`, `data-pane="root"`,
		`data-pane="t:2"`, `data-pane="f:plan.md"`,
		`data-pane="column:todo"`, `data-pane="column:in_progress"`,
		`data-pane="column:done"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a deep chain is missing %s", want)
		}
	}
}
