package hostclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// fakeHost is the contract-exact stand-in for the session host's HTTP API
// (design §6). It answers from the same shapes internal/sessionhost writes,
// so the client tests pin the real wire format without importing the host.
type fakeHost struct {
	t *testing.T

	mu        chan struct{} // serializes handler assertions; capacity 1
	token     string
	metas     map[string]Meta
	lastSpec  sessionapi.Spec
	lastMsg   sessionapi.Message
	lastAns   sessionapi.Answer
	lastIDHdr string
	stopped   []string
	served    int
}

func newFakeHost(t *testing.T, token string) *fakeHost {
	return &fakeHost{t: t, token: token, metas: make(map[string]Meta)}
}

func (f *fakeHost) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test", "sessions_enabled": true})
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got != f.token {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	switch {
	case r.URL.Path == "/v1/sessions" && r.Method == http.MethodPost:
		var spec sessionapi.Spec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_spec", "bad spec")
			return
		}
		f.lastSpec = spec
		id := "sess-" + spec.Prompt
		f.metas[id] = Meta{ID: id, Title: spec.Prompt, Status: sessionapi.StatusStarting,
			WorkspacePath: spec.Workspace.Path, TaskID: spec.TaskID, CreatedAt: time.Now()}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "starting"})
	case r.URL.Path == "/v1/sessions" && r.Method == http.MethodGet:
		list := make([]Meta, 0, len(f.metas))
		for _, m := range f.metas {
			list = append(list, m)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
	case strings.HasPrefix(r.URL.Path, "/v1/sessions/") && strings.HasSuffix(r.URL.Path, "/events"):
		f.lastIDHdr = r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		f.served++
		after := int64(0)
		if f.lastIDHdr != "" {
			var err error
			after, err = parseInt(f.lastIDHdr)
			if err != nil {
				return
			}
		}
		for i := after + 1; i <= after+2; i++ {
			ev := sessionapi.Event{Seq: i, Kind: sessionapi.EventAssistantText, Text: "line"}
			data, _ := json.Marshal(ev)
			_, _ = w.Write([]byte("id: " + itoa(i) + "\ndata: " + string(data) + "\n\n"))
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
		}
	case strings.HasSuffix(r.URL.Path, "/messages"):
		var msg sessionapi.Message
		_ = json.NewDecoder(r.Body).Decode(&msg)
		f.lastMsg = msg
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case strings.HasSuffix(r.URL.Path, "/answers"):
		var a sessionapi.Answer
		_ = json.NewDecoder(r.Body).Decode(&a)
		f.lastAns = a
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case strings.HasSuffix(r.URL.Path, "/stop"):
		f.stopped = append(f.stopped, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/stop"))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
		m, ok := f.metas[id]
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", "no such session: "+id)
			return
		}
		writeJSON(w, http.StatusOK, View{Meta: m, PendingRequests: []sessionapi.Event{{
			Kind: sessionapi.EventQuestion, RequestID: "req-1",
			Questions: []sessionapi.Question{{Question: "Pick", Options: []sessionapi.Option{{Label: "a"}}}},
		}}})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg}})
}

func parseInt(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}

// tokenFile writes a token file and returns its path.
func tokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host.token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestClient(t *testing.T, f *fakeHost) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return NewWithClient(srv.URL, tokenFile(t, f.token), srv.Client())
}

func TestHealthNeedsNoToken(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)

	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if !h.OK || h.Version != "test" || !h.SessionsEnabled {
		t.Errorf("Health() = %+v, want ok/test/enabled", h)
	}
}

func TestCreateSendsSpecAndParsesIDStatus(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)

	spec := sessionapi.Spec{Version: 1, Provider: "claude",
		Workspace: sessionapi.Workspace{Kind: "working_dir", Path: "/work/proj"},
		Prompt:    "do it", TaskID: "web-sessions"}
	created, err := c.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID != "sess-do it" || created.Status != sessionapi.StatusStarting {
		t.Errorf("Create() = %+v, want {id,status} from the host", created)
	}
	if f.lastSpec.Workspace.Path != "/work/proj" || f.lastSpec.Prompt != "do it" || f.lastSpec.TaskID != "web-sessions" {
		t.Errorf("the host received the wrong spec: %+v", f.lastSpec)
	}
}

func TestListFiltersNothingClientSide(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)
	_, _ = c.Create(context.Background(), sessionapi.Spec{Version: 1, Provider: "claude",
		Workspace: sessionapi.Workspace{Kind: "working_dir", Path: "/work/a"}, Prompt: "a"})

	metas, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(metas) != 1 || metas[0].WorkspacePath != "/work/a" {
		t.Errorf("List() = %+v", metas)
	}
}

func TestGetReturnsPendingRequests(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)
	_, _ = c.Create(context.Background(), sessionapi.Spec{Version: 1, Provider: "claude",
		Workspace: sessionapi.Workspace{Kind: "working_dir", Path: "/work/a"}, Prompt: "a"})

	view, err := c.Get(context.Background(), "sess-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if view.Meta.ID != "sess-a" {
		t.Errorf("Get() meta id = %q", view.Meta.ID)
	}
	if len(view.PendingRequests) != 1 || view.PendingRequests[0].RequestID != "req-1" {
		t.Errorf("Get() pending = %+v", view.PendingRequests)
	}
}

// Events must forward Last-Event-ID so a reconnecting browser resumes, and
// expose the raw SSE stream for the tunnel to copy verbatim.
func TestEventsForwardsLastEventID(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)
	_, _ = c.Create(context.Background(), sessionapi.Spec{Version: 1, Provider: "claude",
		Workspace: sessionapi.Workspace{Kind: "working_dir", Path: "/work/a"}, Prompt: "a"})

	body, err := c.Events(context.Background(), "sess-a", "2")
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read the event stream: %v", err)
	}
	if f.lastIDHdr != "2" {
		t.Errorf("Last-Event-ID forwarded = %q, want 2", f.lastIDHdr)
	}
	if !strings.Contains(string(data), "id: 3") || !strings.Contains(string(data), "id: 4") {
		t.Errorf("the stream does not carry events after the cursor:\n%s", data)
	}
}

func TestSendAnswerStopBodies(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)
	ctx := context.Background()

	if err := c.Send(ctx, "sess-a", "hello"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if f.lastMsg.Text != "hello" {
		t.Errorf("the host received message %+v", f.lastMsg)
	}

	ans := sessionapi.Answer{RequestID: "r", Behavior: sessionapi.BehaviorDeny, Message: "no"}
	if err := c.Answer(ctx, "sess-a", ans); err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if f.lastAns.RequestID != ans.RequestID || f.lastAns.Behavior != ans.Behavior || f.lastAns.Message != ans.Message {
		t.Errorf("the host received answer %+v", f.lastAns)
	}

	if err := c.Stop(ctx, "sess-a"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if len(f.stopped) != 1 || f.stopped[0] != "sess-a" {
		t.Errorf("the host saw stops %v", f.stopped)
	}
}

func TestHostErrorsBecomeSessionAPIErrors(t *testing.T) {
	f := newFakeHost(t, "tok")
	c := newTestClient(t, f)

	// The fake host 404s unknown ids with the host's error envelope.
	_, err := c.Get(context.Background(), "nope")
	var se *sessionapi.Error
	if !errors.As(err, &se) {
		t.Fatalf("Get() error = %T %v, want *sessionapi.Error", err, err)
	}
	if se.Code != sessionapi.ErrCodeNotFound {
		t.Errorf("error code = %q, want not_found", se.Code)
	}
}

func TestWrongTokenYieldsUnauthorized(t *testing.T) {
	f := newFakeHost(t, "tok")
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	c := NewWithClient(srv.URL, tokenFile(t, "wrong"), srv.Client())

	_, err := c.List(context.Background())
	var se *sessionapi.Error
	if !errors.As(err, &se) || se.Code != sessionapi.ErrCodeUnauthorized {
		t.Fatalf("List() error = %v, want unauthorized", err)
	}
}

func TestUnreadableTokenFileIsHostUnavailable(t *testing.T) {
	f := newFakeHost(t, "tok")
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	c := NewWithClient(srv.URL, filepath.Join(t.TempDir(), "missing.token"), srv.Client())

	_, err := c.List(context.Background())
	var se *sessionapi.Error
	if !errors.As(err, &se) || se.Code != sessionapi.ErrCodeHostUnavailable {
		t.Fatalf("List() error = %v, want host_unavailable", err)
	}
}

func TestUnreachableHostIsHostUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close() // nothing listens there any more
	c := NewWithClient(addr, tokenFile(t, "tok"), srv.Client())

	_, err := c.Health(context.Background())
	var se *sessionapi.Error
	if !errors.As(err, &se) || se.Code != sessionapi.ErrCodeHostUnavailable {
		t.Fatalf("Health() error = %v, want host_unavailable", err)
	}
}
