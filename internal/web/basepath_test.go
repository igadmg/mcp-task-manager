package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

const testWebBase = "/board"

func basePathDeps(s *Sessions, base string) Deps {
	return Deps{Sessions: s, Logger: discardLogger(), BasePath: base}
}

func assertPrefixedURLs(t *testing.T, body string) {
	t.Helper()
	attrs := regexp.MustCompile(`(?:href|src|action|hx-get|hx-post|hx-push-url)="(/[^\"]*)"`)
	matches := attrs.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatal("no URLs rendered")
	}
	for _, m := range matches {
		if !strings.HasPrefix(m[1], testWebBase+"/") {
			t.Errorf("URL escaped base path: %s", m[1])
		}
	}
}

func TestWebBasePathSessionPages(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "1", Title: "Base path work", Status: "in_progress", Priority: "high", Type: "feature", BlockedBy: []string{"3"}},
		testsupport.TaskSpec{ID: "2", Title: "Nested work", ParentID: "1"},
		testsupport.TaskSpec{ID: "3", Title: "Related work"},
	)
	if err := svc.WriteTaskFile("1", "research.md", "real attached content"); err != nil {
		t.Fatal(err)
	}
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)
	h := NewHandler(basePathDeps(sessions, testWebBase))
	for _, path := range []string{"/", sess.Base() + "/", sess.Base() + "/board", sess.Base() + "/tasks/1", sess.Base() + "/tasks/1/panel", sess.Base() + "/tasks/1/w/f/research.md", sess.Base() + "/strip/tasks/1/w/f/research.md", "/expired/", "/expired/board", "/expired/tasks/1/panel", "/expired/strip/"} {
		t.Run(path, func(t *testing.T) {
			rec := getRaw(t, h, path)
			if rec.Code != http.StatusOK && !(path == "/expired/" && rec.Code == http.StatusNotFound) {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			assertPrefixedURLs(t, rec.Body.String())
			if strings.HasPrefix(path, sess.Base()) && !strings.Contains(rec.Body.String(), testWebBase+sess.Base()+"/") {
				t.Error("session prefix missing")
			}
		})
	}
	// The upstream ServeMux adds a trailing slash to a session root. That
	// redirect must also stay inside the externally published mount.
	redirect := getRaw(t, h, sess.Base())
	if redirect.Code < 300 || redirect.Code >= 400 || redirect.Header().Get("Location") != testWebBase+sess.Base()+"/" {
		t.Errorf("session slash redirect = %d %q", redirect.Code, redirect.Header().Get("Location"))
	}
	// Real external deep link with the same prefix stripping as nginx.
	proxy := httptest.NewServer(http.StripPrefix(testWebBase, h))
	defer proxy.Close()
	resp, err := proxy.Client().Get(proxy.URL + testWebBase + sess.Base() + "/tasks/1/w/f/research.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "real attached content") {
		t.Fatalf("proxy deep link = %d", resp.StatusCode)
	}
	assertPrefixedURLs(t, string(body))
	respAsset, err := proxy.Client().Get(proxy.URL + testWebBase + "/static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer respAsset.Body.Close()
	if respAsset.StatusCode != 200 {
		t.Errorf("asset status=%d", respAsset.StatusCode)
	}
}

func TestWebBasePathSessionPost(t *testing.T) {
	ws := newBacklogWorkspace(t, "alpha")
	sessions, _ := newPickableSessions(t, ws)
	h := NewHandler(basePathDeps(sessions, testWebBase))
	assertPrefixedURLs(t, getRaw(t, h, "/").Body.String())
	rec := post(t, h, "/sessions", url.Values{"workspace": {"alpha"}})
	if rec.Code != 303 {
		t.Fatalf("status=%d", rec.Code)
	}
	sess, err := sessions.Pick("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rec.Header().Get("Location"), testWebBase+sess.Base()+"/"; got != want {
		t.Errorf("Location=%q want %q", got, want)
	}
	assertPrefixedURLs(t, getRaw(t, h, "/").Body.String())
	assertPrefixedURLs(t, post(t, h, "/sessions", url.Values{"workspace": {"unknown"}}).Body.String())
}

func TestWebBasePathIgnoresForwardedPrefix(t *testing.T) {
	for _, base := range []string{"", testWebBase} {
		h := NewHandler(basePathDeps(newTestSessions(t), base))
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-Prefix", "/attacker")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "/attacker") {
			t.Fatal("untrusted forwarded prefix used")
		}
		if !strings.Contains(rec.Body.String(), `href="`+base+`/"`) {
			t.Error("trusted base missing")
		}
	}
}

func TestWebBasePathControllerSessionPath(t *testing.T) {
	rs, _, dir := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)
	c := NewController(basePathDeps(sessions, testWebBase), "127.0.0.1:0")
	got, ok := c.SessionPath(dir)
	if !ok || got != testWebBase+sess.Base()+"/" {
		t.Errorf("SessionPath=%q,%v", got, ok)
	}
}

func TestAppJSHasNoHardcodedRootURLs(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`["'\x60]/(?:[^/]|$)`).Match(data) {
		t.Fatal("app.js contains a root URL; use server-rendered URLs")
	}
}
