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
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

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
	for _, want := range []string{"Ship the board", "Fix the index", "Old chore", "Blocked work"} {
		if !strings.Contains(body, want) {
			t.Errorf("board is missing card %q", want)
		}
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

// laneSection returns the markup of one In progress phase lane: from its
// data-phase attribute to the first closing section tag after it. Lanes
// hold only card articles, so that tag closes the lane itself.
func laneSection(t *testing.T, body, phase string) string {
	t.Helper()
	i := strings.Index(body, `data-phase="`+phase+`"`)
	if i < 0 {
		t.Fatalf("board has no %s lane", phase)
	}
	j := strings.Index(body[i:], "</section>")
	if j < 0 {
		t.Fatalf("the %s lane is never closed", phase)
	}
	return body[i : i+j]
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
	if n := strings.Count(body, ">empty</p>"); n != 1 {
		t.Errorf("board shows %d empty placeholders, want 1 (Done only)", n)
	}
}
