package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// twoSessions serves two independent backlogs from one handler, which is the
// whole point of the token: one process, many workspaces.
func twoSessions(t *testing.T) (http.Handler, *Session, *Session) {
	t.Helper()
	rsA, svcA, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svcA, testsupport.TaskSpec{ID: "1", Title: "Alpha work", Priority: "high", Type: "feature"})
	rsB, svcB, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svcB, testsupport.TaskSpec{ID: "2", Title: "Beta work", Priority: "high", Type: "feature"})

	sessions := newTestSessions(t)
	a := adoptBacklog(t, sessions, rsA)
	b := adoptBacklog(t, sessions, rsB)
	return NewHandler(Deps{Sessions: sessions, Logger: discardLogger()}), a, b
}

// Each token shows its own backlog and nothing of the other's. The two
// services have separate indices and separate locks, so this is isolation by
// construction - the test is here to keep it that way.
func TestTwoSessionsAreIsolated(t *testing.T) {
	h, a, b := twoSessions(t)

	bodyA := getRaw(t, h, a.Base()+"/board").Body.String()
	bodyB := getRaw(t, h, b.Base()+"/board").Body.String()

	if !strings.Contains(bodyA, "Alpha work") || strings.Contains(bodyA, "Beta work") {
		t.Error("session A's board shows the wrong backlog")
	}
	if !strings.Contains(bodyB, "Beta work") || strings.Contains(bodyB, "Alpha work") {
		t.Error("session B's board shows the wrong backlog")
	}

	// A task id that exists in the other backlog is still a 404 here.
	if rec := getRaw(t, h, a.Base()+"/tasks/2"); rec.Code != http.StatusNotFound {
		t.Errorf("GET a task of the other session = %d, want 404", rec.Code)
	}
	if rec := getRaw(t, h, b.Base()+"/tasks/1"); rec.Code != http.StatusNotFound {
		t.Errorf("GET a task of the other session = %d, want 404", rec.Code)
	}
}

// Every link a session's page renders carries that session's token, so a
// click can never cross into another workspace.
func TestSessionLinksCarryTheirOwnToken(t *testing.T) {
	h, a, b := twoSessions(t)

	bodyA := getRaw(t, h, a.Base()+"/board").Body.String()
	if !strings.Contains(bodyA, `hx-get="`+a.Base()+`/board"`) {
		t.Error("the board does not poll its own token")
	}
	if !strings.Contains(bodyA, `href="`+a.Base()+`/tasks/1"`) {
		t.Error("a card does not link its own token")
	}
	if strings.Contains(bodyA, b.Base()+"/") {
		t.Error("session A's page links into session B")
	}
}

// The board at /<token>/ and its htmx fragment have to agree, because the
// fragment replaces the one inside the page.
func TestBoardPageAndFragmentUseTheSamePrefix(t *testing.T) {
	h, a, _ := twoSessions(t)

	page := getRaw(t, h, a.Base()+"/").Body.String()
	fragment := getRaw(t, h, a.Base()+"/board").Body.String()
	for _, want := range []string{`hx-get="` + a.Base() + `/board"`, `href="` + a.Base() + `/tasks/1"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the board page is missing %q", want)
		}
		if !strings.Contains(fragment, want) {
			t.Errorf("the board fragment is missing %q", want)
		}
	}
}

func TestWelcomePageListsWorkspacesAndSessions(t *testing.T) {
	ws := newBacklogWorkspace(t, "alpha")
	other := config.Workspace{Name: "broken", Path: "/no/such/dir", Problem: "path is not a directory"}
	sessions := NewSessions(SessionsConfig{
		Workspaces: []config.Workspace{ws, other},
		ConfigPath: "/home/dev/.config/mcp-task-manager/web.yaml",
		Problems:   []string{"workspace 1 (broken): path is not a directory; listed as unavailable"},
		Open:       openWorkspaceForTest,
		Logger:     discardLogger(),
	})
	h := NewHandler(Deps{Sessions: sessions, Logger: discardLogger()})

	body := getRaw(t, h, "/").Body.String()
	for _, want := range []string{"alpha", "broken", "unavailable", "web.yaml",
		`action="/sessions"`, `value="alpha"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the welcome page is missing %q", want)
		}
	}
	if strings.Contains(body, "Open now") {
		t.Error("the welcome page lists an open session before anything was opened")
	}

	sess, err := sessions.Pick("alpha")
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	body = getRaw(t, h, "/").Body.String()
	if !strings.Contains(body, "Open now") || !strings.Contains(body, `href="`+sess.Base()+`/"`) {
		t.Error("the welcome page does not link the open session")
	}
	if !strings.Contains(body, "open board") {
		t.Error("a live workspace still offers the open form instead of a link")
	}
}

// The session registry is the one POST in this server, and it answers with a
// 303 so the back button does not repost.
func TestCreateSessionRedirectsIntoTheBoard(t *testing.T) {
	ws := newBacklogWorkspace(t, "alpha")
	sessions, _ := newPickableSessions(t, ws)
	h := NewHandler(Deps{Sessions: sessions, Logger: discardLogger()})

	rec := post(t, h, "/sessions", url.Values{"workspace": {"alpha"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /sessions = %d, want 303", rec.Code)
	}
	location := rec.Header().Get("Location")
	sess, ok := sessions.Lookup(strings.Trim(location, "/"))
	if !ok {
		t.Fatalf("Location = %q, which is not a live session", location)
	}
	if location != sess.Base()+"/" {
		t.Errorf("Location = %q, want %q", location, sess.Base()+"/")
	}

	// And the board is actually there.
	if rec := getRaw(t, h, location); rec.Code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", location, rec.Code)
	}
}

func TestCreateSessionRejectsAnUnknownWorkspace(t *testing.T) {
	ws := newBacklogWorkspace(t, "alpha")
	sessions, _ := newPickableSessions(t, ws)
	h := NewHandler(Deps{Sessions: sessions, Logger: discardLogger()})

	for _, name := range []string{"", "nothing-like-this"} {
		rec := post(t, h, "/sessions", url.Values{"workspace": {name}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /sessions workspace=%q = %d, want 400", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Workspaces") {
			t.Errorf("POST /sessions workspace=%q did not re-render the list", name)
		}
	}
	if len(sessions.Live()) != 0 {
		t.Error("a rejected POST registered a session")
	}
}

// The global routes are outside the token space, so one browser cache serves
// every session's assets.
func TestGlobalRoutesNeedNoToken(t *testing.T) {
	h, _, _ := twoSessions(t)

	if rec := getRaw(t, h, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
	rec := getRaw(t, h, "/static/app.css")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /static/app.css = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want the assets immutable", cc)
	}
}

// "static" and "healthz" are reserved against the generator, so a request to
// /static/ cannot be a session - it falls through to the unknown-token page.
func TestReservedSegmentsAreNotSessions(t *testing.T) {
	h, _, _ := twoSessions(t)

	for _, path := range []string{"/healthz/", "/sessions/"} {
		if rec := getRaw(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404: a reserved segment is never a session", path, rec.Code)
		}
	}
	for name := range reservedSegments {
		if _, ok := reservedSegments[name]; !ok {
			t.Errorf("reservedSegments[%q] has no reason", name)
		}
	}
}

// TestTemplatesHaveNoAbsoluteSessionPaths is the mechanical form of "no
// template writes a token by hand". A forgotten nav renders a link that looks
// right and 404s as an unknown token when clicked, which is the worst kind of
// bug to find by hand.
//
// Only / , /sessions and /static/ may be written absolutely: they are the
// routes that live outside the token space.
func TestTemplatesHaveNoAbsoluteSessionPaths(t *testing.T) {
	attrs := regexp.MustCompile(`(href|action|hx-get|hx-post|hx-put|hx-delete|hx-push-url)="(/[^"{}]*)"`)
	allowed := map[string]bool{"/": true, "/sessions": true}

	err := fs.WalkDir(templateFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(templateFS, path)
		if err != nil {
			return err
		}
		for _, m := range attrs.FindAllStringSubmatch(string(body), -1) {
			target := m[2]
			if allowed[target] || strings.HasPrefix(target, "/static/") {
				continue
			}
			t.Errorf("%s writes an absolute session path: %s=%q (use nav)", path, m[1], target)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
}

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
