package web

import (
	"bytes"
	"context"
	"io/fs"
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

// testHandler is the route table plus the token of the one session the test
// backlog is served under, so a test can keep writing session paths without
// a prefix.
type testHandler struct {
	http.Handler
	base string
}

func newTestHandler(t *testing.T) (*testHandler, *task.Service, string) {
	t.Helper()
	rs, svc, dir := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)
	h := NewHandler(Deps{
		Sessions: sessions,
		Logger:   discardLogger(),
		Now:      func() time.Time { return fixedNow },
	})
	return &testHandler{Handler: h, base: sess.Base()}, svc, dir
}

// isGlobalPath reports whether a path lives outside the token space. Only
// these two do; everything else is a session route.
func isGlobalPath(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/sessions")
}

// get fetches a session path, prefixing it with the test session's token.
// The global routes are passed through untouched.
func get(t *testing.T, h *testHandler, path string) *httptest.ResponseRecorder {
	t.Helper()
	if !isGlobalPath(path) {
		path = h.base + path
	}
	return getRaw(t, h, path)
}

// getRaw fetches a path verbatim, for the cases that are about the URL space
// itself: the welcome page, an unknown token, a global route.
func getRaw(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
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
		"chip-critical", // priority chip
		"chip-blocked",  // task 5 is blocked by 3
		"chip-live",     // task 3 is in progress
		"0/1 subtasks",  // the subtask tally, from the snapshot
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board fragment is missing %q", want)
		}
	}
}

// TestBoardHasNoDangerBanner: the amber block above the columns is gone. The
// information lives in the page header now, and the board only carries the
// out-of-band copy that keeps it fresh.
func TestBoardHasNoDangerBanner(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc) // task 3 is in progress

	body := get(t, h, "/board").Body.String()
	for _, gone := range []string{"An agent may be working", "Files in these areas can change"} {
		if strings.Contains(body, gone) {
			t.Errorf("the board still carries the banner text %q", gone)
		}
	}
	// What it does carry is the header's indicator, out of band.
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("the board fragment does not carry the header indicator")
	}
	if !strings.Contains(body, "in progress</span>") {
		t.Error("the out-of-band indicator does not name the count")
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
	// Nothing was resolved for this handler, so it claims no backlog rather
	// than claiming an empty one.
	if got := rec.Header().Get(HealthHeader); got != "" {
		t.Errorf("%s = %q, want empty when no backlog was named", HealthHeader, got)
	}
}

// TestHealthzNamesItsBacklog is what makes /healthz usable as a probe now that
// the dashboard is its own process: "ok" says something is listening, the
// header says which backlog it serves. internal/webproc decides whether to
// spawn on exactly this.
func TestHealthzNamesItsBacklog(t *testing.T) {
	sessions := newTestSessions(t)
	h := NewHandler(Deps{
		Sessions:        sessions,
		Logger:          discardLogger(),
		PrimaryTasksDir: "/abs/backlog",
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("GET /healthz = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(HealthHeader); got != "/abs/backlog" {
		t.Errorf("%s = %q, want the backlog this dashboard was started for", HealthHeader, got)
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

	// A file column now renders its artifact, so an ordinary link inside a
	// task note becomes a real anchor. The guarantee is that the PAGE
	// fetches nothing, so the sweep is every src= (an <img> really does
	// fetch, content or not) plus href= on a <link>. An anchor's href is
	// navigation, not a fetch, and a note that cites a URL should link it.
	if err := svc.WriteTaskFile("1", "links.md", "see [the issue](https://example.com/1)"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	for _, path := range []string{
		"/", "/tasks/1", "/tasks/1/w/f/research.md", "/tasks/1/w/f/links.md",
		"/tasks/1/w/d/1", "/strip/tasks/1",
	} {
		body := get(t, h, path).Body.String()
		for _, ref := range externalRefs(body) {
			t.Errorf("%s references the network: %s", path, ref)
		}
	}
}

// externalRefs lists the attribute values on the page that would make the
// browser fetch from the network: any src=, and href= on a <link> element.
func externalRefs(body string) []string {
	var out []string
	for _, attr := range []string{`src="`, `href="`} {
		rest := body
		for {
			i := strings.Index(rest, attr)
			if i < 0 {
				break
			}
			before := rest[:i]
			rest = rest[i+len(attr):]
			value := rest[:strings.Index(rest, `"`)]
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				continue
			}
			// An href only fetches on a <link>: find the tag this
			// attribute belongs to.
			if attr == `href="` {
				open := strings.LastIndex(before, "<")
				if open < 0 || !strings.HasPrefix(strings.ToLower(before[open:]), "<link") {
					continue
				}
			}
			out = append(out, attr+value)
		}
	}
	return out
}

// TestNoMutatingRoutes asserts the read-only guarantee twice over: every
// non-GET method on a route that shows task data is refused by the mux
// itself, and a full GET sweep leaves the tasks directory byte for byte as
// it was.
//
// 405 and not merely "an error": the session mux registers GET patterns
// only, so ServeMux refuses the method before any handler exists to run.
// That is the structural form of the rule, and a 404 here would mean a
// pattern had quietly been registered for another method.
func TestNoMutatingRoutes(t *testing.T) {
	h, svc, dir := newTestHandler(t)
	seedWorkspace(t, svc)

	sessionPaths := []string{
		"/", "/board", "/tasks/1", "/tasks/1/panel", "/tasks/1/files/research.md",
		"/tasks/1/w/f/research.md", "/tasks/1/w/t/2", "/tasks/1/w/d/1",
		"/tasks/1/w/g/1", "/graph",
		"/strip/tasks/1", "/strip/", "/strip/graph",
	}
	for _, path := range sessionPaths {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, h.base+path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, h.base+path, rec.Code)
			}
		}
	}

	// The global routes carry no task data, so all that matters is that
	// they never succeed for a mutating method.
	for _, path := range []string{"/healthz", "/static/app.css"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code >= 200 && rec.Code < 300 {
				t.Errorf("%s %s = %d, want a refusal", method, path, rec.Code)
			}
		}
	}

	before := snapshotDir(t, dir)
	for _, path := range append(sessionPaths, "/healthz", "/static/app.css") {
		get(t, h, path)
	}
	getRaw(t, h, "/")
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

	// The payload also reaches the page header: its in-progress indicator
	// names tasks and puts their titles in a tooltip.
	if _, err := svc.StartTask("1"); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	// The payload also travels in a chain URL: the file column's ref is the
	// attacking file name, escaped on the way out and on the way back.
	chainFile := "/tasks/1/w/f/" + url.PathEscape("x<script>.md")
	for _, path := range []string{
		"/", "/board", "/tasks/1", "/tasks/1/panel",
		chainFile, "/strip" + chainFile, "/strip/tasks/1",
		// The description column renders the task's text as its whole
		// body, so it is the surface with the most payload on it.
		"/tasks/1/w/d/1", "/strip/tasks/1/w/d/1",
		// A node of the graph carries every task's title and id.
		"/graph", "/tasks/1/w/g/1", "/strip/graph",
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

// TestNoRequestEverResolves is the structural rule in its new form: a
// handler reads the registry and nothing else, so no GET - and not even the
// one POST - can be what first resolves a project. Resolution runs
// Initialize(), which migrates the layout and may auto-archive.
func TestNoRequestEverResolves(t *testing.T) {
	testsupport.IsolateEnv(t)
	rs := project.NewResolver(func(context.Context) ([]string, error) {
		t.Fatal("an HTTP request resolved the project")
		return nil, nil
	})
	_ = rs // held only to fail the test if anything reaches for it

	h := NewHandler(Deps{Sessions: newTestSessions(t), Logger: discardLogger()})

	// With nothing registered the root is the workspace list, not a board.
	rec := getRaw(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "No workspaces are configured") {
		t.Errorf("GET / did not render the empty workspace list: %s", body)
	}
}

// An unknown token is the normal case after a restart, so it explains itself
// instead of answering a bare 404.
func TestUnknownTokenExplainsItself(t *testing.T) {
	h := NewHandler(Deps{Sessions: newTestSessions(t), Logger: discardLogger()})

	for _, path := range []string{"/nosuchtoken/", "/nosuchtoken/tasks/1"} {
		rec := getRaw(t, h, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "not open") {
			t.Errorf("GET %s did not explain the missing session: %s", path, body)
		}
		if !strings.Contains(body, `href="/"`) {
			t.Errorf("GET %s did not link back to the workspace list", path)
		}
	}
}

// The htmx targets answer 200 instead: htmx does not swap a 404, so an open
// board would freeze with no explanation. The replacement carries no
// hx-trigger, so it also stops polling.
func TestUnknownTokenSwapsAFragmentAndStopsPolling(t *testing.T) {
	h := NewHandler(Deps{Sessions: newTestSessions(t), Logger: discardLogger()})

	for _, path := range []string{"/nosuchtoken/board", "/nosuchtoken/tasks/1/panel"} {
		rec := getRaw(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 so htmx swaps it in", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "not open") {
			t.Errorf("GET %s did not explain the missing session: %s", path, body)
		}
		if strings.Contains(body, "hx-trigger") {
			t.Errorf("GET %s keeps polling a session that is gone", path)
		}
	}
}

// The plain-text route stays a bare 404: it serves a file into a new tab, so
// an HTML explanation would be the wrong kind of answer.
func TestUnknownTokenOnTheFileRouteIsAPlain404(t *testing.T) {
	h := NewHandler(Deps{Sessions: newTestSessions(t), Logger: discardLogger()})

	rec := getRaw(t, h, "/nosuchtoken/tasks/1/files/research")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET a file of an unknown token = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("the file route answered with a page")
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
		"Fix the index", "chip-live", `hx-get="` + h.base + `/strip/tasks/3"`, ">1</span>",
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
	if n := strings.Count(body, `hx-get="`+h.base+`/strip/tasks/3"`); n != 1 {
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
	if strings.Contains(body, "Old chore") || strings.Contains(body, `hx-get="`+h.base+`/strip/tasks/4"`) {
		t.Error("the Done column renders task 4 as a card")
	}
	if strings.Contains(body, "style=") {
		t.Error("the board carries an inline style")
	}
}

// The bar's colours are explained by the SVG's <title>, which is both the
// hover tooltip and the accessible name, so there is no aria-label.
func TestStatsBarsTooltipNamesTheColours(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/board").Body.String()
	// Task 4 (medium bug) is done and closed just now, task 5 is a medium
	// todo one created just now: 1 done, 1 of them recent, 0 in progress,
	// 1 to do, 1 of them fresh. The title names both windows, which it
	// could not do while one of them was a constant.
	const want = "<title>medium: 1 done (green), 1 of them closed in the last 24 h (light green), " +
		"0 in progress (amber), 1 to do (grey), 1 of them created in the last 24 h (blue)</title>"
	if !strings.Contains(body, want) {
		t.Errorf("the medium bar lacks %s", want)
	}
	if strings.Contains(body, `<svg class="stats-bar" viewBox="0 0 2 1" preserveAspectRatio="none" role="img" aria-label=`) {
		t.Error("the bar still carries an aria-label next to its <title>")
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
	if err := rootTpl.fragments.ExecuteTemplate(&b, "_stats_bars.html", card); err != nil {
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
	if err := rootTpl.fragments.ExecuteTemplate(&b, "_stats_lines.html", card); err != nil {
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

// TestPanelIsTheOnlyScroller pins the panel's scroll model. A long description
// has to scroll inside #panel, not drag the page and the board along. Since
// the workspace the strip introduced, the panel is one of the strip's panes:
// a .pane with a data-pane key, scrolling inside a viewport-tall unit - and
// the description below it is still not a nested scroller of its own.
func TestPanelIsTheOnlyScroller(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := get(t, h, "/").Body.String()
	for _, want := range []string{`id="panel"`, `class="pane"`, `data-pane="panel"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the panel aside lacks %q", want)
		}
	}

	if _, err := svc.Create("Long one", "line\nafter line", "medium", "feature", "", "long"); err != nil {
		t.Fatalf("Create error = %v", err)
	}
	panel := get(t, h, "/tasks/long/panel").Body.String()
	if !strings.Contains(panel, "whitespace-pre-wrap") {
		t.Fatal("the description is not the pre-wrapped block any more")
	}
	for _, bad := range []string{"max-h-96", "overflow-auto"} {
		if strings.Contains(panel, bad) {
			t.Errorf("the description still carries %q: a second scroller nested in #panel", bad)
		}
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
		`href="` + h.base + `/tasks/1/w/f/a%23b.md"`,
		`href="` + h.base + `/tasks/1/w/f/a%3Fb.md"`,
		`href="` + h.base + `/tasks/1/w/f/my%20notes.md"`,
		`hx-get="` + h.base + `/strip/tasks/1/w/f/a%23b.md"`,
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
			want: []string{`data-column="f"`, `data-ref="research.md"`, "<h1>root findings</h1>"},
		},
		{
			name: "a subtask",
			path: "/tasks/1/w/t/2",
			want: []string{`data-column="t"`, `data-ref="2"`, "Write templates"},
		},
		{
			name: "a subtask then its file",
			path: "/tasks/1/w/t/2/f/plan.md",
			want: []string{`data-ref="2"`, `data-ref="plan.md"`, "<h1>subtask plan</h1>"},
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
		`href="` + h.base + `/tasks/1"`,
		`href="` + h.base + `/tasks/1/w/t/2"`,
		`href="` + h.base + `/tasks/1/w/t/2/f/plan.md"`,
		`hx-get="` + h.base + `/strip/tasks/1"`,
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
	if !strings.Contains(panel, `href="`+h.base+`/tasks/1/w/f/research.md"`) {
		t.Error("the panel does not open its file as a column")
	}

	// From a task column, both its files and its subtasks append.
	col := get(t, h, "/tasks/1/w/t/2").Body.String()
	if !strings.Contains(col, `href="`+h.base+`/tasks/1/w/t/2/f/plan.md"`) {
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
	for _, want := range []string{"<h1>shipped</h1>", "Archived"} {
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
	if !strings.Contains(body, `href="`+h.base+`/tasks/1/w/t/2"`) {
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
	if !strings.Contains(column, `href="`+h.base+`/tasks/1/w/t/2"`) {
		t.Error("the root column does not append its subtask to the chain")
	}

	// A subtask column rebases onto its own position rather than becoming a
	// new root.
	sub := get(t, h, "/tasks/1/w/t/2").Body.String()
	if !strings.Contains(sub, `href="`+h.base+`/tasks/1/w/t/2/f/plan.md"`) {
		t.Error("a subtask column re-roots the chain instead of appending to it")
	}
	if strings.Contains(sub, `href="`+h.base+`/tasks/2/w/f/plan.md"`) {
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
		"/tasks/1/w/d/1",
		"/tasks/1/w/t/2/d/2",
		"/tasks/1/w/t/2/t/2",
		"/tasks/1/w/g/1",
		"/tasks/1/w/t/2/g/2",
		"/graph",
		"/strip/", "/strip/tasks/1", "/strip/tasks/1/w/f/research.md",
		"/strip/tasks/1/w/d/1", "/strip/graph",
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
		`data-pane="0:t:2"`, `data-pane="1:f:plan.md"`,
		`data-pane="column:todo"`, `data-pane="column:in_progress"`,
		`data-pane="column:done"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a deep chain is missing %s", want)
		}
	}
}

// columnBody returns one column unit's markup: from its data-column marker to
// the next one, or to the end of the strip. It cannot use sectionAfter - a
// column's body is _detail.html, which opens sections of its own, so the first
// </section> closes "Blocked by", not the column.
func columnBody(t *testing.T, body, marker string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("body has no %s", marker)
	}
	if next := strings.Index(rest, "data-column="); next >= 0 {
		return rest[:next]
	}
	return rest
}

// TestPanelLinksEveryFamilyIntoTheWorkspace is the subtask's point from the
// panel's side: the parent, a blocker, a relation target, a subtask, a file
// and the task's own description all enter the chain, each appending exactly
// one pair. Before this they were plain /tasks/{id} hrefs that left the
// workspace, or inert text.
func TestPanelLinksEveryFamilyIntoTheWorkspace(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	if err := svc.AddRelation("2", "relates_to", "3"); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}

	// Task 2 is a subtask of 1 with a file and now a relation; task 5 is
	// blocked by 3.
	panel := get(t, h, "/tasks/2/panel").Body.String()
	for name, want := range map[string]string{
		"description": "/tasks/2/w/d/2",
		"parent":      "/tasks/2/w/t/1",
		"file":        "/tasks/2/w/f/plan.md",
		"relation":    "/tasks/2/w/t/3",
	} {
		if !strings.Contains(panel, `href="`+h.base+want+`"`) {
			t.Errorf("the panel does not open its %s (%s)", name, want)
		}
		if !strings.Contains(panel, `hx-get="`+h.base+"/strip"+want+`"`) {
			t.Errorf("the panel's %s link has no fragment URL", name)
		}
	}
	if strings.Contains(panel, `href="`+h.base+`/tasks/1"`) {
		t.Error("the panel still links its parent out of the workspace")
	}

	blocked := get(t, h, "/tasks/5/panel").Body.String()
	if !strings.Contains(blocked, `href="`+h.base+`/tasks/5/w/t/3"`) {
		t.Error("the panel does not open its blocker as a column")
	}
}

// TestColumnLinksEveryFamilyRebased is the same families from a column, which
// is the case a per-family rebase gets wrong: the links must append to where
// the column sits, not re-root the chain on its task.
func TestColumnLinksEveryFamilyRebased(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	if err := svc.AddRelation("2", "relates_to", "3"); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}

	body := get(t, h, "/tasks/1/w/t/2").Body.String()
	for name, want := range map[string]string{
		"description": "/tasks/1/w/t/2/d/2",
		"parent":      "/tasks/1/w/t/2/t/1",
		"file":        "/tasks/1/w/t/2/f/plan.md",
		"relation":    "/tasks/1/w/t/2/t/3",
	} {
		if !strings.Contains(body, `href="`+h.base+want+`"`) {
			t.Errorf("the column does not append its %s (%s)", name, want)
		}
	}
	// The root column to its left is rooted at 1 and appends at depth 0.
	if !strings.Contains(body, `href="`+h.base+`/tasks/1/w/d/1"`) {
		t.Error("the root column does not open its own description at depth 0")
	}
	if strings.Contains(body, `href="`+h.base+`/tasks/2/w/`) {
		t.Error("a column re-rooted the chain on its own task")
	}
}

// TestDescriptionColumnShowsTheText pins the d kind end to end, including the
// empty case: the chain names a task, not a non-empty text.
func TestDescriptionColumnShowsTheText(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	desc := "a long description\n\nwith two paragraphs"
	if _, err := svc.Update("1", nil, &desc, nil, nil, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	body := get(t, h, "/tasks/1/w/d/1").Body.String()
	for _, want := range []string{
		`data-column="d"`, `data-ref="1"`, "kind-desc",
		"with two paragraphs",
		// The column links back to the task the text belongs to.
		`href="` + h.base + `/tasks/1/w/d/1/t/1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the description column is missing %q", want)
		}
	}

	// Task 3 has no description; the column still exists and says so.
	empty := get(t, h, "/tasks/3/w/d/3")
	if empty.Code != http.StatusOK {
		t.Fatalf("GET a description column of an empty description = %d, want 200", empty.Code)
	}
	if !strings.Contains(empty.Body.String(), "No description.") {
		t.Error("an empty description column does not say so")
	}

	// A description column of a task that is not there is one 404, like
	// every other chain that names nothing.
	if got := get(t, h, "/tasks/1/w/d/nope").Code; got != http.StatusNotFound {
		t.Errorf("GET a description column of a missing task = %d, want 404", got)
	}
}

// TestColumnMarksTheOpenItem is the "a column shows which of its children the
// column to its right is" criterion, through the rendered markup.
func TestColumnMarksTheOpenItem(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// The root column marks the subtask whose column follows it.
	body := get(t, h, "/tasks/1/w/t/2").Body.String()
	root := columnBody(t, body, `data-column="root"`)
	if !strings.Contains(root, "col-selected") || !strings.Contains(root, `aria-current="true"`) {
		t.Error("the root column does not mark the subtask it opened")
	}

	// The last column marks nothing: there is nothing to its right.
	last := strings.LastIndex(body, `data-column="t"`)
	if last < 0 {
		t.Fatal("no task column in the body")
	}
	if strings.Contains(body[last:], "col-selected") {
		t.Error("the last column marks an item although nothing is open past it")
	}

	// A file column and a description column are marked the same way.
	for _, c := range []struct{ path, marker string }{
		{"/tasks/1/w/f/research.md", "research.md"},
		{"/tasks/1/w/d/1", "Description"},
	} {
		rootCol := columnBody(t, get(t, h, c.path).Body.String(), `data-column="root"`)
		if !strings.Contains(rootCol, "col-selected") {
			t.Errorf("%s: the root column does not mark %s", c.path, c.marker)
		}
	}
}

// TestChainMayRevisitATask is the re-entry decision, recorded in design.md:
// the chain is a history, so opening a task already in it appends. The rail
// stays one rung per column, and the two columns get their own scroll keys -
// app.js keeps one offset per data-pane string, so a shared key would make
// them scroll as one.
func TestChainMayRevisitATask(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	rec := get(t, h, "/tasks/1/w/t/2/t/1/t/2")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET a chain that revisits a task = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if n := strings.Count(body, `data-column="t"`); n != 3 {
		t.Errorf("got %d task columns, want the 3 the chain names", n)
	}
	for _, want := range []string{
		`data-pane="0:t:2"`, `data-pane="1:t:1"`, `data-pane="2:t:2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a revisiting chain is missing %s", want)
		}
	}
	// Four rungs: the root plus one per column, each linking to its prefix.
	for _, want := range []string{
		`href="` + h.base + `/tasks/1"`,
		`href="` + h.base + `/tasks/1/w/t/2"`,
		`href="` + h.base + `/tasks/1/w/t/2/t/1"`,
		`href="` + h.base + `/tasks/1/w/t/2/t/1/t/2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the rail of a revisiting chain is missing %s", want)
		}
	}
}

// TestColumnWithoutFiles keeps the empty cases quiet rather than rendering an
// empty section, and keeps the server's own phase records out of the column -
// they are not attached files a reader opens.
func TestColumnWithoutFiles(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	if _, _, err := svc.StartPhase("3", task.PhaseResearch); err != nil {
		t.Fatalf("StartPhase() error = %v", err)
	}

	body := get(t, h, "/tasks/1/w/t/3").Body.String()
	col := columnBody(t, body, `data-column="t"`)
	if strings.Contains(col, "Attached files") {
		t.Error("a column for a task with no files renders the file section")
	}
	if strings.Contains(col, "research.phase") {
		t.Error("a column offers the server's phase record as an attached file")
	}
	if !strings.Contains(col, "Phases") {
		t.Error("the phase history is missing, so the fixture is wrong")
	}
}

// TestArchivedColumnIsReadOnly keeps an archived task's column honest: the
// banner, its files, and none of the derived data the archive does not hold.
func TestArchivedColumnIsReadOnly(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	if err := svc.WriteTaskFile("4", "notes.md", "# archived notes"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.ArchiveTask("4"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	body := get(t, h, "/tasks/1/w/t/4").Body.String()
	col := columnBody(t, body, `data-column="t"`)
	for _, want := range []string{
		"Archived", "notes.md",
		`href="` + h.base + `/tasks/1/w/t/4/f/notes.md"`,
		`href="` + h.base + `/tasks/1/w/t/4/d/4"`,
	} {
		if !strings.Contains(col, want) {
			t.Errorf("the archived column is missing %q", want)
		}
	}
	if strings.Contains(col, "Subtasks") {
		t.Error("the archived column shows subtasks the archive does not index")
	}
}

// TestFileColumnRendersMarkdown is the parent task's whole point: a task's
// research / design / plan has to be readable in the UI. The constructs are
// the ones this project's own artifacts are made of.
func TestFileColumnRendersMarkdown(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	const src = "# Design\n\n" +
		"| Concern | State |\n| --- | --- |\n| widths | named classes |\n\n" +
		"```go\nfunc main() {}\n```\n\n" +
		"- a list item\n- [ ] a task item\n\n" +
		"> a quote\n\n" +
		"Some `inline code` and **bold**.\n"
	if err := svc.WriteTaskFile("1", "design.md", src); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	body := get(t, h, "/tasks/1/w/f/design.md").Body.String()
	for _, want := range []string{
		`<div class="notes">`,
		"<h1>Design</h1>",
		"<table>", "<thead>", "<th>Concern</th>", "<td>named classes</td>",
		`<pre><code class="language-go">`, "func main() {}",
		"<ul>", "<li>a list item</li>",
		`<li class="task-list-item">`,
		"<blockquote>", "<code>inline code</code>", "<strong>bold</strong>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the rendered file column is missing %q", want)
		}
	}
	// The source markers are gone: this is HTML now, not escaped text.
	if strings.Contains(body, "# Design") {
		t.Error("the file column still shows the markdown source")
	}
}

// TestFileColumnNeverTrustsTheFile is the security half. The renderer's own
// tests cover its output; this asserts it through the route, which is where a
// mistake would actually be served.
func TestFileColumnNeverTrustsTheFile(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	const src = "<script>alert(1)</script>\n\n" +
		"<img src=x onerror=alert(2)>\n\n" +
		"[click me](javascript:alert(3))\n\n" +
		"[relative](design.md)\n"
	if err := svc.WriteTaskFile("1", "evil.md", src); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	// Scope the assertions to the rendered body: the page's own layout
	// legitimately carries a <script src> for the vendored htmx.
	page := get(t, h, "/tasks/1/w/f/evil.md").Body.String()
	_, rest, ok := strings.Cut(page, `<div class="notes">`)
	if !ok {
		t.Fatal("the file column rendered no document at all")
	}
	body, _, ok := strings.Cut(rest, "</div>")
	if !ok {
		t.Fatal("the rendered document is never closed")
	}
	// No tag and no attribute from the file survives as markup. The
	// payloads are still in the body as escaped text, which is the point -
	// a reader sees the source - so these look for live markup, not for
	// the strings.
	// Only live markup counts. An escaped payload still contains
	// "onerror=" as text ("&lt;img src=x onerror=alert(2)&gt;"), which is
	// the point - the reader sees the source - so these are the tag
	// openers and the attribute that would actually navigate.
	for _, bad := range []string{
		"<script", "<img", "javascript:",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the rendered file column passed through %q", bad)
		}
	}
	// The payload is still visible as text, so a reader sees the source.
	for _, want := range []string{"&lt;script&gt;", "&lt;img src=x onerror=alert(2)&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("the raw HTML was dropped instead of escaped: no %q", want)
		}
	}
	// A refused link keeps its text.
	if !strings.Contains(body, "click me") {
		t.Error("a javascript: link lost its text as well as its tag")
	}
}

// TestBodyRenderingByName pins the name rule end to end, including the two
// shapes this backlog actually has: an extensionless artifact and a .md one.
func TestBodyRenderingByName(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	if err := svc.WriteTaskFile("1", "design", "# extensionless"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.WriteTaskFile("1", "notes.txt", "# not a heading"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	doc := get(t, h, "/tasks/1/w/f/design").Body.String()
	if !strings.Contains(doc, "<h1>extensionless</h1>") {
		t.Error("an extensionless artifact is not rendered as a document")
	}

	text := get(t, h, "/tasks/1/w/f/notes.txt").Body.String()
	if !strings.Contains(text, `<pre class="col-file-body">`) ||
		!strings.Contains(text, "# not a heading") {
		t.Error("a .txt file is not shown as preformatted text")
	}
	if strings.Contains(text, `<div class="notes">`) {
		t.Error("a .txt file was rendered as a document")
	}
}

// TestDescriptionColumnRendersAsADocument is the "same working area" claim:
// a description goes through the same bodyView as a file.
func TestDescriptionColumnRendersAsADocument(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	desc := "## Goal\n\n- one\n- two\n\n`code` and <b>raw</b>"
	if _, err := svc.Update("1", nil, &desc, nil, nil, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	body := get(t, h, "/tasks/1/w/d/1").Body.String()
	for _, want := range []string{
		`<div class="notes">`, "<h2>Goal</h2>", "<li>one</li>",
		"<code>code</code>", "&lt;b&gt;raw&lt;/b&gt;",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the description column is missing %q", want)
		}
	}
}

// TestFileColumnRefusesServerOwnedFiles closes a hole the read path leaves
// open: storage validates a read without the reserved-name rule, so before
// this a phase record and the task's own {id}.md rendered as columns although
// no surface links either.
func TestFileColumnRefusesServerOwnedFiles(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)
	if _, _, err := svc.StartPhase("1", task.PhaseResearch); err != nil {
		t.Fatalf("StartPhase() error = %v", err)
	}

	for _, name := range []string{"research.phase", "RESEARCH.PHASE", "1.md"} {
		if got := get(t, h, "/tasks/1/w/f/"+name).Code; got != http.StatusNotFound {
			t.Errorf("GET a column for %q = %d, want 404", name, got)
		}
	}
	// The refusal is about those names only: an ordinary .md still renders,
	// and the raw-text route is left as the escape hatch it already was.
	if got := get(t, h, "/tasks/1/w/f/research.md").Code; got != http.StatusOK {
		t.Errorf("an ordinary file = %d, want 200", got)
	}
	if got := get(t, h, "/tasks/1/files/research.md").Code; got != http.StatusOK {
		t.Errorf("the raw file route = %d, want 200", got)
	}
}

// TestFileColumnOddNamesNeverFail is the "no input produces a 500" criterion.
// "." and ".." never reach a handler at all: ServeMux cleans the path first.
func TestFileColumnOddNamesNeverFail(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// Already-escaped refs are passed through as they are; the rest are
	// escaped here, because httptest.NewRequest panics on a target that is
	// not a valid URL (a bare space is not).
	for _, name := range []string{".", "..", "%2e", "a%23b.md", "%20", "nope.md", "%2Fetc%2Fpasswd"} {
		code := get(t, h, "/tasks/1/w/f/"+name).Code
		if code >= 500 {
			t.Errorf("GET a column for %q = %d, want anything but a server error", name, code)
		}
	}
}

// TestOnlyOneThingPolls is the poll contract. A bare board polls itself; an
// open workspace polls the whole strip instead and its board carries no
// trigger of its own, so there is exactly one request per interval at every
// depth rather than one per column.
func TestOnlyOneThingPolls(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// Nothing open: the board polls, the strip does not.
	for _, path := range []string{"/", "/tasks/1", "/strip/"} {
		body := get(t, h, path).Body.String()
		if n := strings.Count(body, "hx-trigger="); n != 1 {
			t.Errorf("%s has %d hx-trigger attributes, want exactly 1", path, n)
		}
		if !strings.Contains(body, `hx-get="`+h.base+`/board" hx-trigger=`) {
			t.Errorf("%s does not poll the board", path)
		}
	}

	// A chain open: the strip polls its own fragment URL and the board
	// inside it has no trigger.
	for _, c := range []struct{ path, poll string }{
		{"/tasks/1/w/f/research.md", "/strip/tasks/1/w/f/research.md"},
		{"/tasks/1/w/t/2/f/plan.md", "/strip/tasks/1/w/t/2/f/plan.md"},
		{"/strip/tasks/1/w/d/1", "/strip/tasks/1/w/d/1"},
	} {
		body := get(t, h, c.path).Body.String()
		if n := strings.Count(body, "hx-trigger="); n != 1 {
			t.Errorf("%s has %d hx-trigger attributes, want exactly 1", c.path, n)
		}
		if !strings.Contains(body, `hx-get="`+h.base+c.poll+`" hx-trigger=`) {
			t.Errorf("%s does not poll %s", c.path, c.poll)
		}
		if strings.Contains(body, `hx-get="`+h.base+`/board" hx-trigger=`) {
			t.Errorf("%s still polls the board separately", c.path)
		}
	}
}

// TestThePollPushesNoHistory keeps the poll out of the history stack: pushing
// is opt-in per link in this package, and a poll that pushed would fight Back
// five times a minute.
func TestThePollPushesNoHistory(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	body := get(t, h, "/tasks/1/w/f/research.md").Body.String()
	// The polling element is #strip; only its own attributes matter here,
	// since every link inside it pushes on purpose.
	_, strip, ok := strings.Cut(body, `id="strip"`)
	if !ok {
		t.Fatal("the fragment has no #strip")
	}
	strip, _, _ = strings.Cut(strip, ">")
	if !strings.Contains(strip, "hx-trigger=") {
		t.Fatalf("#strip does not poll: %q", strip)
	}
	if strings.Contains(strip, "hx-push-url") {
		t.Errorf("the poll pushes a history entry: %q", strip)
	}
}

// TestWorkspaceIsLive is the acceptance criterion without a browser: the next
// poll is just another GET of the same fragment URL, so a file rewritten
// between two of them comes back with its new content, and a file added to a
// task appears in its column.
func TestWorkspaceIsLive(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	const path = "/strip/tasks/1/w/f/research.md"
	if !strings.Contains(get(t, h, path).Body.String(), "<h1>root findings</h1>") {
		t.Fatal("the fixture is wrong")
	}

	if err := svc.WriteTaskFile("1", "research.md", "# rewritten by an agent"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.WriteTaskFile("1", "late.md", "# added later"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	body := get(t, h, path).Body.String()
	if !strings.Contains(body, "<h1>rewritten by an agent</h1>") {
		t.Error("the next poll did not pick up the rewritten file")
	}
	if strings.Contains(body, "root findings") {
		t.Error("the next poll still shows the old content")
	}
	// The new file is offered by the root column in the same response.
	if !strings.Contains(body, `href="`+h.base+`/tasks/1/w/f/late.md"`) {
		t.Error("a file added while the workspace was open does not appear")
	}
}

// TestWorkspaceGoneAtTheNextPoll covers both ways the chain's root can leave
// while it is open. Neither is an error, and the replacement carries no
// trigger, so a dead workspace stops polling instead of hammering.
func TestWorkspaceGoneAtTheNextPoll(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// Archived: the chain still resolves, read-only, with its banner.
	if err := svc.WriteTaskFile("4", "notes.md", "# archived"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.ArchiveTask("4"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	archived := get(t, h, "/strip/tasks/4/w/f/notes.md")
	if archived.Code != http.StatusOK {
		t.Errorf("an archived root = %d, want 200", archived.Code)
	}
	if !strings.Contains(archived.Body.String(), "Archived") {
		t.Error("an archived root does not say so")
	}

	// Deleted: one 404 on the fragment, like any chain that names nothing.
	if err := svc.Delete("3", false); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := get(t, h, "/strip/tasks/3").Code; got != http.StatusNotFound {
		t.Errorf("a deleted root = %d, want 404", got)
	}

	// A session that ended answers the gone fragment, which has no trigger.
	gone := getRaw(t, h, "/nosuchtoken/strip/tasks/1")
	if gone.Code != http.StatusOK {
		t.Errorf("an unknown token on the strip route = %d, want 200", gone.Code)
	}
	if strings.Contains(gone.Body.String(), "hx-trigger") {
		t.Error("the gone fragment keeps polling")
	}
}

// TestGraphFromTheBoard is the rootless entry point: the board's own button
// opens the whole backlog with nothing highlighted, which is the one workspace
// state a chain cannot name.
func TestGraphFromTheBoard(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// The button is on the board, inside #board so a poll keeps it.
	board := get(t, h, "/").Body.String()
	if !strings.Contains(board, `href="`+h.base+`/graph"`) {
		t.Error("the board has no Graph button")
	}
	if !strings.Contains(board, `hx-get="`+h.base+`/strip/graph"`) {
		t.Error("the board's Graph button has no fragment URL")
	}

	page := get(t, h, "/graph")
	if page.Code != http.StatusOK {
		t.Fatalf("GET /graph = %d, want 200", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{
		`data-column="g"`, "kind-graph", "<svg", "graph-legend",
		// The board is still the strip's first unit, and it is slid away.
		`id="board"`, "strip-shifted",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the graph page is missing %q", want)
		}
	}
	// Nothing is highlighted.
	if strings.Contains(body, "graph-current") {
		t.Error("the rootless graph highlighted a node")
	}
	// The rail steps back to the board.
	if !strings.Contains(body, `href="`+h.base+`/"`) {
		t.Error("the graph's rail does not step back to the board")
	}

	// The fragment form is the same state without the layout shell.
	frag := get(t, h, "/strip/graph")
	if frag.Code != http.StatusOK {
		t.Fatalf("GET /strip/graph = %d, want 200", frag.Code)
	}
	if strings.Contains(frag.Body.String(), "<!doctype html>") {
		t.Error("/strip/graph answered a whole page")
	}
	// One poll, on the strip, at its own URL.
	if n := strings.Count(frag.Body.String(), "hx-trigger="); n != 1 {
		t.Errorf("the graph fragment has %d triggers, want 1", n)
	}
	if !strings.Contains(frag.Body.String(), `hx-get="`+h.base+`/strip/graph" hx-trigger=`) {
		t.Error("the graph does not poll its own URL")
	}
}

// TestGraphFromATask is the other entry point: the same graph with that task
// marked, as a column of the chain.
func TestGraphFromATask(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	// The panel offers it.
	panel := get(t, h, "/tasks/2/panel").Body.String()
	if !strings.Contains(panel, `href="`+h.base+`/tasks/2/w/g/2"`) {
		t.Error("the panel does not open the graph with its task highlighted")
	}

	body := get(t, h, "/tasks/1/w/g/1").Body.String()
	if !strings.Contains(body, "graph-current") {
		t.Error("the graph does not mark the highlighted task")
	}
	if n := strings.Count(body, "graph-current"); n != 1 {
		t.Errorf("%d nodes carry graph-current, want 1", n)
	}
	// The root column to its left marks the graph as the open item.
	root := columnBody(t, body, `data-column="root"`)
	if !strings.Contains(root, "col-selected") {
		t.Error("the task column does not mark the graph it opened")
	}

	// From a task column the graph appends at that column's place.
	deep := get(t, h, "/tasks/1/w/t/2").Body.String()
	if !strings.Contains(deep, `href="`+h.base+`/tasks/1/w/t/2/g/2"`) {
		t.Error("a task column does not append the graph to the chain")
	}

	// A highlight that is not in the backlog is one 404, like every other
	// ref that names nothing.
	if got := get(t, h, "/tasks/1/w/g/nope").Code; got != http.StatusNotFound {
		t.Errorf("GET a graph highlighting a missing task = %d, want 404", got)
	}
}

// TestGraphNodesAreLinksAndCarryState checks what a reader actually sees: a
// node per task, done ones dimmed, blocked ones marked, and a click that opens
// that task's own graph state.
func TestGraphNodesAreLinksAndCarryState(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	body := get(t, h, "/graph").Body.String()
	// seedBoard: 1 parent, 2 subtask, 3 in progress, 4 done, 5 blocked by 3.
	for _, want := range []string{
		`href="` + h.base + `/tasks/1/w/g/1"`, // a node link re-roots on its task
		"graph-done",                          // task 4
		"graph-blocked",                       // task 5
		"graph-in_progress",                   // task 3
		"graph-edge-parent",                   // 1 -> 2
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the graph is missing %q", want)
		}
	}
	// Every task is a node.
	if n := strings.Count(body, `class="graph-node`); n != 5 {
		t.Errorf("%d nodes drawn, want the 5 seeded tasks", n)
	}
}

// TestGraphEmptyBacklog says so rather than drawing an empty frame.
func TestGraphEmptyBacklog(t *testing.T) {
	h, _, _ := newTestHandler(t)

	body := get(t, h, "/graph").Body.String()
	if !strings.Contains(body, "No tasks in this backlog yet.") {
		t.Error("an empty backlog does not say so")
	}
	if strings.Contains(body, "<svg class=\"graph-svg\"") {
		t.Error("an empty backlog drew a graph")
	}
}

// TestGraphUnknownToken follows the rule its route family already has: 404 on
// the page, the trigger-less gone fragment on the strip route.
func TestGraphUnknownToken(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	if got := getRaw(t, h, "/nosuchtoken/graph").Code; got != http.StatusNotFound {
		t.Errorf("GET an unknown token's graph page = %d, want 404", got)
	}
	frag := getRaw(t, h, "/nosuchtoken/strip/graph")
	if frag.Code != http.StatusOK {
		t.Errorf("GET an unknown token's graph fragment = %d, want 200", frag.Code)
	}
	if strings.Contains(frag.Body.String(), "hx-trigger") {
		t.Error("the gone fragment keeps polling")
	}
}

// TestHeaderShowsTheDangerZone is where the banner went: the page shell, on
// every page a session serves, so it is visible from a file column as well as
// from the board.
func TestHeaderShowsTheDangerZone(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc) // task 3 is in progress

	for _, path := range []string{"/", "/tasks/1", "/tasks/1/w/f/research.md", "/graph"} {
		body := get(t, h, path).Body.String()
		header, _, ok := strings.Cut(body, "</header>")
		if !ok {
			t.Fatalf("%s rendered no header", path)
		}
		for _, want := range []string{
			`id="shell-danger"`,
			"1 in progress",
			`href="` + h.base + `/tasks/3"`,
		} {
			if !strings.Contains(header, want) {
				t.Errorf("%s: the header is missing %q", path, want)
			}
		}
	}
}

// TestHeaderIndicatorIsEmptyWhenIdle keeps the wrapper and drops the content:
// an out-of-band swap can only replace an element that is there, so the empty
// wrapper is what lets a later poll clear or fill it.
func TestHeaderIndicatorIsEmptyWhenIdle(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "1", Title: "Quiet"})

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, `id="shell-danger"`) {
		t.Error("the wrapper is gone, so a poll would have nothing to swap into")
	}
	if strings.Contains(body, "in progress</span>") {
		t.Error("the indicator rendered with nothing in progress")
	}
}

// TestHeaderIndicatorCapsAndCounts is the compact form through the route: a
// count chip, two links and a +N whose tooltip names the rest.
func TestHeaderIndicatorCapsAndCounts(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		testsupport.Seed(t, svc, testsupport.TaskSpec{ID: id, Title: "work " + id, Status: "in_progress"})
	}

	header, _, _ := strings.Cut(get(t, h, "/").Body.String(), "</header>")
	if !strings.Contains(header, "4 in progress") {
		t.Error("the chip does not count every in-progress task")
	}
	for _, want := range []string{`/tasks/a"`, `/tasks/b"`} {
		if !strings.Contains(header, want) {
			t.Errorf("the header does not link %s", want)
		}
	}
	for _, unwanted := range []string{`/tasks/c"`, `/tasks/d"`} {
		if strings.Contains(header, unwanted) {
			t.Errorf("the header links %s past the cap", unwanted)
		}
	}
	if !strings.Contains(header, ">+2<") {
		t.Error("no +N chip for the tasks past the cap")
	}
	if !strings.Contains(header, "#c work c, #d work d") {
		t.Error("the +N tooltip does not name the tasks it stands for")
	}
}

// TestPagesWithoutASnapshotStillRender is the regression the data's home
// guards against: the welcome page and an unknown token's page execute the
// same header with models that have no backlog behind them, and a field only
// some models carried would be an execution error, i.e. a 500, on those.
func TestPagesWithoutASnapshotStillRender(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	welcome := getRaw(t, h, "/")
	if welcome.Code != http.StatusOK {
		t.Errorf("GET the welcome page = %d, want 200", welcome.Code)
	}
	gone := getRaw(t, h, "/nosuchtoken/")
	if gone.Code != http.StatusNotFound {
		t.Errorf("GET an unknown token = %d, want 404", gone.Code)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{"welcome": welcome, "gone": gone} {
		body := rec.Body.String()
		if strings.Contains(body, "internal error") {
			t.Errorf("the %s page rendered a template error", name)
		}
		if !strings.Contains(body, `id="shell-danger"`) {
			t.Errorf("the %s page has no indicator wrapper", name)
		}
		if strings.Contains(body, "in progress</span>") {
			t.Errorf("the %s page shows an indicator although it has no snapshot", name)
		}
	}
}

// TestTheOobCopyRidesThePolledFragment pins the freshness mechanism: the
// header is outside both polled nodes, so exactly the fragment that polls
// carries a copy of it - and nothing else does, or two copies would race.
func TestTheOobCopyRidesThePolledFragment(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedWorkspace(t, svc)

	for _, path := range []string{"/board", "/strip/", "/strip/tasks/1", "/strip/graph"} {
		body := get(t, h, path).Body.String()
		if n := strings.Count(body, `hx-swap-oob="true"`); n != 1 {
			t.Errorf("%s carries %d out-of-band elements, want exactly 1", path, n)
		}
		// And the contract it must not disturb.
		if n := strings.Count(body, "hx-trigger="); n != 1 {
			t.Errorf("%s has %d triggers, want still exactly 1", path, n)
		}
	}

	// A whole page needs no copy: the header is in it already, and a
	// second element with the same id would be a duplicate id.
	for _, path := range []string{"/", "/tasks/1", "/graph"} {
		body := get(t, h, path).Body.String()
		if strings.Contains(body, `hx-swap-oob`) {
			t.Errorf("%s is a whole page and should carry no out-of-band copy", path)
		}
		if n := strings.Count(body, `id="shell-danger"`); n != 1 {
			t.Errorf("%s has %d #shell-danger elements, want exactly 1", path, n)
		}
	}

	// The panel swap is neither the poll nor a page, so it carries none and
	// the indicator stays as the last poll left it.
	if body := get(t, h, "/tasks/1/panel").Body.String(); strings.Contains(body, "hx-swap-oob") {
		t.Error("the panel fragment carries an out-of-band header copy")
	}
}

// TestGoneFragmentClearsTheIndicator: a dead session's fragment is the last
// response an open board will ever get, so it has to clear the header rather
// than leave whatever was there when the session died.
func TestGoneFragmentClearsTheIndicator(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	body := getRaw(t, h, "/nosuchtoken/board").Body.String()
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("the gone fragment does not clear the header indicator")
	}
	if strings.Contains(body, "in progress</span>") {
		t.Error("the gone fragment carries an indicator for a session that is gone")
	}
	if strings.Contains(body, "hx-trigger") {
		t.Error("the gone fragment started polling again")
	}
}

// TestStatsBarsDrawTheArrivals is the segment through the route: a bar whose
// todo tasks arrived inside the window carries the fifth colour over the
// start of its todo run, and the row says how many.
func TestStatsBarsDrawTheArrivals(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	// Two fresh todo tasks of one priority, plus one in progress, so the
	// todo run has a start that is not the bar's start.
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "1", Title: "Fresh one", Priority: "high"},
		testsupport.TaskSpec{ID: "2", Title: "Fresh two", Priority: "high"},
		testsupport.TaskSpec{ID: "3", Title: "Moving", Priority: "high", Status: "in_progress"},
	)

	body := get(t, h, "/board").Body.String()
	row := sectionAfter(t, body, `data-value="high"`)

	// 3 total, 1 in progress, 2 todo: the todo run starts at 1 and the
	// arrivals cover all of it.
	for _, want := range []string{
		`<rect class="bar-new" x="1" width="2" height="1"/>`,
		`<span class="stats-new">2 new</span>`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the high row is missing %q\nrow: %s", want, row)
		}
	}
	// The bar's width did not change: the arrivals are inside todo, not a
	// sixth column of the stack.
	if !strings.Contains(row, `viewBox="0 0 3 1"`) {
		t.Errorf("the bar's viewBox is not the total: %s", row)
	}
}

// TestStatsBarsWithoutArrivalsDrawNeither keeps both the rect and the clause
// off a row that has nothing fresh, like every other segment.
func TestStatsBarsWithoutArrivalsDrawNeither(t *testing.T) {
	h, svc, dir := newTestHandler(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "1", Title: "Stale", Priority: "low"})
	// Backdate it past the window, on disk, and let the index notice.
	ageTask(t, dir, "1", 72*time.Hour)

	row := sectionAfter(t, get(t, h, "/board").Body.String(), `data-value="low"`)
	if strings.Contains(row, "bar-new") {
		t.Errorf("a stale row drew the arrivals rect: %s", row)
	}
	if strings.Contains(row, "new</span>") {
		t.Errorf("a stale row drew the arrivals clause: %s", row)
	}
	// And it still draws the todo segment it does have.
	if !strings.Contains(row, "bar-todo") {
		t.Errorf("the todo segment is gone: %s", row)
	}
}

// ageTask rewrites a task's created_at to be ago in the past, the way a
// backlog that has been running for a while looks.
func ageTask(t *testing.T, dir, id string, ago time.Duration) {
	t.Helper()
	path := filepath.Join(dir, id, id+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	stamp := time.Now().Add(-ago).UTC().Format(time.RFC3339)
	out := regexp.MustCompile(`(?m)^created_at:.*$`).ReplaceAll(raw, []byte("created_at: "+stamp))
	if bytes.Equal(raw, out) {
		t.Fatalf("no created_at in %s", path)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

// TestCardClickMovesThePoll pins the fix for a selection the timer kept
// taking back: the page opened at /tasks/3 polls /strip/tasks/3, and a card
// click that swapped only #panel left that poll in place, so five seconds
// later task 3 was back in the panel. A card now swaps the whole strip from
// /strip/tasks/{id}, and that response polls the task it shows.
func TestCardClickMovesThePoll(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	page := get(t, h, "/tasks/3").Body.String()
	link := `hx-get="` + h.base + `/strip/tasks/5"`
	i := strings.Index(page, link)
	if i < 0 {
		t.Fatalf("card 5 does not load %s", link)
	}
	tag := page[i:]
	tag = tag[:strings.Index(tag, ">")]
	for _, want := range []string{`hx-target="#strip"`, `hx-swap="outerHTML"`, `hx-push-url="` + h.base + `/tasks/5"`} {
		if !strings.Contains(tag, want) {
			t.Errorf("card 5's link lacks %s: %s", want, tag)
		}
	}
	if strings.Contains(page, `hx-target="#panel"`) {
		t.Error("a link still swaps #panel alone; the strip's poll would put the old task back")
	}

	frag := get(t, h, "/strip/tasks/5").Body.String()
	for _, want := range []string{
		`hx-get="` + h.base + `/strip/tasks/5" hx-trigger="every`,
		`data-pane="panel" data-task="5"`,
	} {
		if !strings.Contains(frag, want) {
			t.Errorf("/strip/tasks/5 lacks %s", want)
		}
	}
}
