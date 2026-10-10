package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/hostclient"
	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// fakeSessionHost is the web package's own stand-in for the session host's
// HTTP API, answering with exactly the shapes internal/sessionhost writes
// (design §6). internal/web must not import the host package even in tests,
// so the contract is mirrored here instead.
type fakeSessionHost struct {
	mu      sync.Mutex
	token   string
	metas   map[string]hostclient.Meta
	pending map[string][]sessionapi.Event
	lastID  string // Last-Event-ID the events endpoint saw

	lastSpec sessionapi.Spec
	lastMsg  sessionapi.Message
	lastAns  sessionapi.Answer
	stopped  []string
}

func newFakeSessionHost(t *testing.T) *fakeSessionHost {
	t.Helper()
	return &fakeSessionHost{token: "host-token", metas: make(map[string]hostclient.Meta),
		pending: make(map[string][]sessionapi.Event)}
}

// setMeta seeds one session record; workspacePath decides which web
// workspace owns it.
func (f *fakeSessionHost) setMeta(id, workspacePath, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metas[id] = hostclient.Meta{
		ID: id, Title: "session " + id, Status: sessionapi.Status(status),
		WorkspacePath: workspacePath, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func (f *fakeSessionHost) meta(id string) (hostclient.Meta, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.metas[id]
	return m, ok
}

func (f *fakeSessionHost) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "sessions_enabled": true})
		return
	}
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != f.token {
		writeHostErr(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	switch {
	case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
		var spec sessionapi.Spec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeHostErr(w, http.StatusBadRequest, "invalid_spec", "bad spec")
			return
		}
		f.mu.Lock()
		f.lastSpec = spec
		f.mu.Unlock()
		id := "made-" + spec.Prompt
		f.setMeta(id, spec.Workspace.Path, "starting")
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "starting"})
	case r.URL.Path == "/v1/sessions" && r.Method == http.MethodGet:
		f.mu.Lock()
		list := make([]hostclient.Meta, 0, len(f.metas))
		for _, m := range f.metas {
			list = append(list, m)
		}
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
	case strings.HasSuffix(r.URL.Path, "/events"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/events")
		if _, ok := f.meta(id); !ok {
			writeHostErr(w, http.StatusNotFound, "not_found", "no such session: "+id)
			return
		}
		f.mu.Lock()
		f.lastID = r.Header.Get("Last-Event-ID")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 1; i <= 2; i++ {
			ev := sessionapi.Event{Seq: int64(i), Kind: sessionapi.EventAssistantText, Text: "streamed " + id}
			data, _ := json.Marshal(ev)
			_, _ = w.Write([]byte("id: " + strconv.Itoa(i) + "\ndata: " + string(data) + "\n\n"))
		}
	case strings.HasSuffix(r.URL.Path, "/messages"):
		var msg sessionapi.Message
		_ = json.NewDecoder(r.Body).Decode(&msg)
		f.mu.Lock()
		f.lastMsg = msg
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case strings.HasSuffix(r.URL.Path, "/answers"):
		var a sessionapi.Answer
		_ = json.NewDecoder(r.Body).Decode(&a)
		f.mu.Lock()
		f.lastAns = a
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case strings.HasSuffix(r.URL.Path, "/stop"):
		f.mu.Lock()
		f.stopped = append(f.stopped, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/stop"))
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
		m, ok := f.meta(id)
		if !ok {
			writeHostErr(w, http.StatusNotFound, "not_found", "no such session: "+id)
			return
		}
		f.mu.Lock()
		pending := f.pending[id]
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, hostclient.View{Meta: m, PendingRequests: pending})
	}
}

func writeHostErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg}})
}

const testCSRF = "test-csrf-token"

// sessionsTestBed wires one adopted backlog to one fake host through the
// real hostclient, the way webapp.Run does.
func sessionsTestBed(t *testing.T, f *fakeSessionHost) (http.Handler, *Session) {
	t.Helper()
	rs, _, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	tokenPath := filepath.Join(t.TempDir(), "host.token")
	if err := os.WriteFile(tokenPath, []byte(f.token), 0o600); err != nil {
		t.Fatal(err)
	}
	api := hostclient.NewWithClient(srv.URL, tokenPath, srv.Client())

	h := NewHandler(Deps{
		Sessions:   sessions,
		Logger:     discardLogger(),
		SessionAPI: api,
		CSRFToken:  testCSRF,
	})
	return h, sess
}

// postJSON posts one JSON body with the CSRF header, or without it when
// csrf is empty; contentType empty means application/json.
func postJSON(t *testing.T, h http.Handler, path, contentType, csrf, body string) *httptest.ResponseRecorder {
	t.Helper()
	if contentType == "" {
		contentType = "application/json"
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if csrf != "" {
		req.Header.Set("X-Dashboard-CSRF", csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func workspaceRootOf(t *testing.T, sess *Session) string {
	t.Helper()
	root := sess.Project().Resolution().Root
	if root == "" {
		t.Fatal("the test session has no project root")
	}
	return root
}

// The whole session surface is opt-in (design §7): with no host client the
// routes do not exist and no page links to them.
func TestSessionsRoutesHiddenWhenDisabled(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)
	h := NewHandler(Deps{Sessions: sessions, Logger: discardLogger()})

	if rec := getRaw(t, h, sess.Base()+"/sessions"); rec.Code != http.StatusNotFound {
		t.Errorf("GET %s/sessions = %d, want 404 when sessions are disabled", sess.Base(), rec.Code)
	}
	if rec := postJSON(t, h, sess.Base()+"/sessions", "", testCSRF, `{"prompt":"hi"}`); rec.Code != http.StatusNotFound {
		t.Errorf("POST %s/sessions = %d, want 404 when sessions are disabled", sess.Base(), rec.Code)
	}
	if body := getRaw(t, h, sess.Base()+"/").Body.String(); strings.Contains(body, `href="`+sess.Base()+`/sessions"`) {
		t.Error("the board links into the session list although sessions are disabled")
	}
}

func TestSessionsPageListsOnlyOwnWorkspace(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)
	f.setMeta("own-1", root, "running")
	f.setMeta("other-1", "/somewhere/else", "running")

	rec := getRaw(t, h, sess.Base()+"/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s/sessions = %d, want 200", sess.Base(), rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "own-1") || strings.Contains(body, "other-1") {
		t.Errorf("the session list crosses workspaces:\n%s", body)
	}
	if !strings.Contains(body, `data-csrf="`+testCSRF+``) {
		t.Error("the sessions page does not hand out the CSRF token")
	}
}

// A dead host must produce an explanation on the page, not a blank screen
// (design §7).
func TestSessionsPageExplainsAHostFailure(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()
	api := hostclient.NewWithClient(addr, filepath.Join(t.TempDir(), "none"), srv.Client())
	h := NewHandler(Deps{Sessions: sessions, Logger: discardLogger(), SessionAPI: api, CSRFToken: testCSRF})

	rec := getRaw(t, h, sess.Base()+"/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s/sessions = %d, want 200 with an explanation", sess.Base(), rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "unavailable") {
		t.Errorf("the page does not explain the host failure:\n%s", body)
	}
}

// The spec's workspace.path comes from the server-side workspace registry -
// the browser names a prompt and an optional task id, never a path.
func TestStartSessionBuildsSpecFromRegistryRoot(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)

	rec := postJSON(t, h, sess.Base()+"/sessions", "", testCSRF, `{"prompt":"hello claude","task_id":"web-sessions"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s/sessions = %d, want 201: %s", sess.Base(), rec.Code, rec.Body.String())
	}
	var created hostclient.Created
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Errorf("the answer is not {id,status}: %q (%v)", rec.Body.String(), err)
	}
	f.mu.Lock()
	spec := f.lastSpec
	f.mu.Unlock()
	if spec.Workspace.Path != root {
		t.Errorf("spec workspace.path = %q, want the registry root %q - it must not come from the browser", spec.Workspace.Path, root)
	}
	if spec.Workspace.Kind != sessionapi.WorkspaceKindWorkingDir || spec.Provider != sessionapi.ProviderClaude || spec.Version != 1 {
		t.Errorf("the spec is not the v1 working_dir spec: %+v", spec)
	}
	if spec.Prompt != "hello claude" || spec.TaskID != "web-sessions" {
		t.Errorf("the spec lost the user's fields: %+v", spec)
	}
}

// A browser trying to smuggle a workspace path into the start request is
// rejected before the spec is even built.
func TestStartSessionRejectsASmuggledWorkspace(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)

	rec := postJSON(t, h, sess.Base()+"/sessions", "", testCSRF,
		`{"prompt":"hi","workspace":{"kind":"working_dir","path":"/etc"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST with a workspace field = %d, want 400", rec.Code)
	}
	f.mu.Lock()
	got := f.lastSpec.Prompt
	f.mu.Unlock()
	if got != "" {
		t.Errorf("the host was called with a browser-supplied spec (prompt %q)", got)
	}
}

func TestStartSessionValidatesPrompt(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)

	rec := postJSON(t, h, sess.Base()+"/sessions", "", testCSRF, `{"prompt":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST with an empty prompt = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "prompt") {
		t.Errorf("the error does not name the field: %s", rec.Body.String())
	}
}

// Every mutation needs the CSRF header with the page's token (design §7).
func TestSessionsMutationsRequireCSRF(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)

	rec := postJSON(t, h, sess.Base()+"/sessions", "", "", `{"prompt":"hi"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without the CSRF header = %d, want 403", rec.Code)
	}
	rec = postJSON(t, h, sess.Base()+"/sessions", "", "wrong-token", `{"prompt":"hi"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with a wrong CSRF header = %d, want 403", rec.Code)
	}
}

// The mutation bodies are JSON only; a form post cannot ride the routes.
func TestSessionsMutationsRequireJSON(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)

	rec := postJSON(t, h, sess.Base()+"/sessions", "application/x-www-form-urlencoded", testCSRF, "prompt=hi")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("POST with a form content type = %d, want 415", rec.Code)
	}
}

// Ownership is checked per request against the host's record, not by list
// filtering: another workspace's session id is a 404 on every endpoint.
func TestSessionEndpointsEnforceWorkspaceOwnership(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)
	f.setMeta("own-1", root, "running")
	f.setMeta("other-1", "/somewhere/else", "running")

	if rec := getRaw(t, h, sess.Base()+"/sessions/own-1"); rec.Code != http.StatusOK {
		t.Errorf("GET the own session = %d, want 200", rec.Code)
	}
	paths := []string{
		"/sessions/other-1",
		"/sessions/other-1/events",
	}
	for _, p := range paths {
		if rec := getRaw(t, h, sess.Base()+p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404: another workspace's session is not reachable", p, rec.Code)
		}
	}
	posts := []string{
		"/sessions/other-1/messages",
		"/sessions/other-1/answers",
		"/sessions/other-1/stop",
	}
	for _, p := range posts {
		if rec := postJSON(t, h, sess.Base()+p, "", testCSRF, `{"text":"hi"}`); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404: another workspace's session is not reachable", p, rec.Code)
		}
	}

	// And the own session answers on the mutation endpoints.
	if rec := postJSON(t, h, sess.Base()+"/sessions/own-1/messages", "", testCSRF, `{"text":"go on"}`); rec.Code != http.StatusOK {
		t.Errorf("POST the own session's message = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	f.mu.Lock()
	msg := f.lastMsg
	f.mu.Unlock()
	if msg.Text != "go on" {
		t.Errorf("the host received message %+v", msg)
	}
	if rec := postJSON(t, h, sess.Base()+"/sessions/own-1/answers", "", testCSRF,
		`{"request_id":"r1","behavior":"allow","answers":{"q":"a"}}`); rec.Code != http.StatusOK {
		t.Errorf("POST the own session's answer = %d, want 200", rec.Code)
	}
	f.mu.Lock()
	ans := f.lastAns
	f.mu.Unlock()
	if ans.RequestID != "r1" || ans.Behavior != "allow" || ans.Answers["q"] != "a" {
		t.Errorf("the host received answer %+v", ans)
	}
	if rec := postJSON(t, h, sess.Base()+"/sessions/own-1/stop", "", testCSRF, `{}`); rec.Code != http.StatusOK {
		t.Errorf("POST the own session's stop = %d, want 200", rec.Code)
	}
	f.mu.Lock()
	stopped := f.stopped
	f.mu.Unlock()
	if len(stopped) != 1 || stopped[0] != "own-1" {
		t.Errorf("the host saw stops %v", stopped)
	}
}

// The SSE endpoint tunnels the host's stream: media type, anti-buffering
// header, Last-Event-ID forwarding and verbatim id/data frames.
func TestSessionEventsTunnel(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)
	f.setMeta("own-1", root, "running")

	req := httptest.NewRequest(http.MethodGet, sess.Base()+"/sessions/own-1/events", nil)
	req.Header.Set("Last-Event-ID", "5")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the SSE tunnel did not finish")
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if xab := rec.Header().Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", xab)
	}
	f.mu.Lock()
	lastID := f.lastID
	f.mu.Unlock()
	if lastID != "5" {
		t.Errorf("Last-Event-ID forwarded = %q, want 5", lastID)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"text":"streamed own-1"`) || !strings.Contains(body, "id: 1") {
		t.Errorf("the tunnel did not relay the host's frames:\n%s", body)
	}
}

// A host that cannot be reached maps to 503 with an explanation, so the UI
// can say "the session host is unavailable" instead of a bare failure.
func TestSessionMutationsReportAnUnavailableHost(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()
	api := hostclient.NewWithClient(addr, filepath.Join(t.TempDir(), "none"), srv.Client())
	h := NewHandler(Deps{Sessions: sessions, Logger: discardLogger(), SessionAPI: api, CSRFToken: testCSRF})

	rec := postJSON(t, h, sess.Base()+"/sessions", "", testCSRF, `{"prompt":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST with a dead host = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "unavailable") {
		t.Errorf("the error is not user friendly: %s", body)
	}
}
