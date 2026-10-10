package sessionhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func apiNewTestServer(t *testing.T, maxSessions int) (*httptest.Server, *Manager, *fakeBackend) {
	t.Helper()
	backend := newFakeBackend()
	mgr, err := NewManager(filepath.Join(t.TempDir(), "state"), backend, maxSessions)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	srv := NewServer(mgr, testToken)
	srv.heartbeat = 50 * time.Millisecond // fast terminal-close in tests
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("mgr.Close: %v", err)
		}
	})
	return ts, mgr, backend
}

func apiDo(t *testing.T, method, url, token, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func apiErrCode(t *testing.T, data []byte) string {
	t.Helper()
	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, data)
	}
	if out.Error.Code == "" || out.Error.Message == "" {
		t.Fatalf("error body missing code/message: %s", data)
	}
	return out.Error.Code
}

func apiCreateSession(t *testing.T, ts *httptest.Server, ws, prompt string) string {
	t.Helper()
	spec := fmt.Sprintf(`{"version":1,"provider":"claude","workspace":{"kind":"working_dir","path":%q},"prompt":%q}`, ws, prompt)
	code, data := apiDo(t, "POST", ts.URL+"/v1/sessions", testToken, spec)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, data)
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || out.Status == "" {
		t.Fatalf("create response missing id/status: %s", data)
	}
	return out.ID
}

func apiWaitForStatus(t *testing.T, ts *httptest.Server, id string, want sessionapi.Status) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, data := apiDo(t, "GET", ts.URL+"/v1/sessions/"+id, testToken, "")
		if code == http.StatusOK {
			var view struct {
				Meta struct {
					Status sessionapi.Status `json:"status"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(data, &view); err == nil && view.Meta.Status == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s did not reach status %s", id, want)
}

func TestServerHealthzNoToken(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 4)
	code, data := apiDo(t, "GET", ts.URL+"/healthz", "", "")
	if code != http.StatusOK {
		t.Fatalf("healthz: %d %s", code, data)
	}
	if !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("healthz body: %s", data)
	}
}

func TestServerAuth(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 4)
	code, data := apiDo(t, "GET", ts.URL+"/v1/sessions", "", "")
	if code != http.StatusUnauthorized || apiErrCode(t, data) != sessionapi.ErrCodeUnauthorized {
		t.Fatalf("no token: %d %s", code, data)
	}
	code, data = apiDo(t, "GET", ts.URL+"/v1/sessions", "wrong-token", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d %s", code, data)
	}
	code, _ = apiDo(t, "GET", ts.URL+"/v1/sessions", testToken, "")
	if code != http.StatusOK {
		t.Fatalf("valid token: %d", code)
	}
}

func TestServerCreateListGet(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 4)
	ws := t.TempDir()
	id := apiCreateSession(t, ts, ws, "do things")

	code, data := apiDo(t, "GET", ts.URL+"/v1/sessions", testToken, "")
	if code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	if !strings.Contains(string(data), id) {
		t.Fatalf("list does not contain session: %s", data)
	}

	code, data = apiDo(t, "GET", ts.URL+"/v1/sessions/"+id, testToken, "")
	if code != http.StatusOK {
		t.Fatalf("get: %d", code)
	}
	var view SessionView
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Meta.ID != id || view.Meta.WorkspacePath != ws {
		t.Fatalf("view = %+v", view.Meta)
	}

	code, data = apiDo(t, "GET", ts.URL+"/v1/sessions/nope", testToken, "")
	if code != http.StatusNotFound || apiErrCode(t, data) != sessionapi.ErrCodeNotFound {
		t.Fatalf("missing session: %d %s", code, data)
	}
}

func TestServerCreateInvalidSpec(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 4)
	body := `{"version":1,"provider":"codex","workspace":{"kind":"working_dir","path":"C:\\ws"},"prompt":"hi"}`
	code, data := apiDo(t, "POST", ts.URL+"/v1/sessions", testToken, body)
	if code != http.StatusBadRequest || apiErrCode(t, data) != sessionapi.ErrCodeUnsupportedProvider {
		t.Fatalf("invalid provider: %d %s", code, data)
	}
	body = `{"version":1,"provider":"claude","workspace":{"kind":"working_dir","path":"/no/such/dir"},"prompt":"hi"}`
	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions", testToken, body)
	if code != http.StatusBadRequest || apiErrCode(t, data) != sessionapi.ErrCodeWorkspaceNotFound {
		t.Fatalf("missing workspace: %d %s", code, data)
	}
	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions", testToken, "{not json")
	if code != http.StatusBadRequest {
		t.Fatalf("malformed spec: %d %s", code, data)
	}
}

func TestServerTooManySessions(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 1)
	apiCreateSession(t, ts, t.TempDir(), "first")
	code, data := apiDo(t, "POST", ts.URL+"/v1/sessions", testToken,
		fmt.Sprintf(`{"version":1,"provider":"claude","workspace":{"kind":"working_dir","path":%q},"prompt":"second"}`, t.TempDir()))
	if code != http.StatusConflict || apiErrCode(t, data) != sessionapi.ErrCodeTooManySessions {
		t.Fatalf("limit: %d %s", code, data)
	}
}

func TestServerMessagesAndStop(t *testing.T) {
	ts, _, backend := apiNewTestServer(t, 4)
	id := apiCreateSession(t, ts, t.TempDir(), "hello")
	var fp *fakeProcess
	select {
	case fp = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend not started")
	}

	code, data := apiDo(t, "POST", ts.URL+"/v1/sessions/"+id+"/messages", testToken, `{"text":"follow up"}`)
	if code != http.StatusOK {
		t.Fatalf("message: %d %s", code, data)
	}
	select {
	case text := <-fp.sent:
		if text != "follow up" {
			t.Fatalf("sent text = %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not receive message")
	}

	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions/"+id+"/stop", testToken, "")
	if code != http.StatusOK {
		t.Fatalf("stop: %d %s", code, data)
	}
	select {
	case <-fp.stopCalls:
	case <-time.After(5 * time.Second):
		t.Fatal("process not stopped")
	}
	apiWaitForStatus(t, ts, id, sessionapi.StatusStopped)

	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions/"+id+"/messages", testToken, `{"text":"late"}`)
	if code != http.StatusConflict {
		t.Fatalf("message after stop: %d %s", code, data)
	}
}

func TestServerAnswers(t *testing.T) {
	ts, _, backend := apiNewTestServer(t, 4)
	id := apiCreateSession(t, ts, t.TempDir(), "question?")
	var fp *fakeProcess
	select {
	case fp = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend not started")
	}

	fp.eventsCh <- sessionapi.Event{
		Kind: sessionapi.EventQuestion, RequestID: "q1", ToolUseID: "t1",
		Questions: []sessionapi.Question{{Question: "Pick?", Options: []sessionapi.Option{{Label: "A"}}}},
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, data := apiDo(t, "GET", ts.URL+"/v1/sessions/"+id, testToken, "")
		var view SessionView
		if err := json.Unmarshal(data, &view); err == nil && len(view.PendingRequests) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("question not pending: %s", data)
		}
		time.Sleep(20 * time.Millisecond)
	}

	code, data := apiDo(t, "POST", ts.URL+"/v1/sessions/"+id+"/answers", testToken,
		`{"request_id":"q1","behavior":"allow","answers":{"Pick?":"A"}}`)
	if code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, data)
	}
	select {
	case a := <-fp.answers:
		if a.RequestID != "q1" || a.Behavior != "allow" || a.Answers["Pick?"] != "A" {
			t.Fatalf("answer = %+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not receive answer")
	}

	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions/"+id+"/answers", testToken,
		`{"request_id":"nope","behavior":"allow"}`)
	if code != http.StatusNotFound {
		t.Fatalf("unknown request: %d %s", code, data)
	}
	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions/"+id+"/answers", testToken,
		`{"request_id":"q1","behavior":"maybe"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("invalid behavior: %d %s", code, data)
	}
}

func apiReadSSE(t *testing.T, url, token, lastEventID string) []string {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("SSE content type = %q", ct)
	}
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func TestServerSSEAfter(t *testing.T) {
	ts, _, backend := apiNewTestServer(t, 4)
	id := apiCreateSession(t, ts, t.TempDir(), "stream")
	var fp *fakeProcess
	select {
	case fp = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend not started")
	}
	for _, text := range []string{"one", "two", "three"} {
		fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: text}
	}
	close(fp.eventsCh)
	apiWaitForStatus(t, ts, id, sessionapi.StatusFinished)

	lines := apiReadSSE(t, ts.URL+"/v1/sessions/"+id+"/events?after=0", testToken, "")
	var ids, data []string
	for _, l := range lines {
		if strings.HasPrefix(l, "id: ") {
			ids = append(ids, l)
		}
		if strings.HasPrefix(l, "data: ") {
			data = append(data, l)
		}
	}
	if len(ids) != 6 || len(data) != 6 { // starting, running, 3 text events, finished
		t.Fatalf("SSE lines = %v", lines)
	}
	if ids[0] != "id: 1" || ids[5] != "id: 6" {
		t.Fatalf("SSE ids = %v", ids)
	}
	if !strings.Contains(data[2], "one") || !strings.Contains(data[5], `"finished"`) {
		t.Fatalf("SSE data = %v", data)
	}

	// Last-Event-ID replays only the tail.
	lines = apiReadSSE(t, ts.URL+"/v1/sessions/"+id+"/events", testToken, "4")
	ids = nil
	for _, l := range lines {
		if strings.HasPrefix(l, "id: ") {
			ids = append(ids, l)
		}
	}
	if len(ids) != 2 || ids[0] != "id: 5" || ids[1] != "id: 6" {
		t.Fatalf("tail ids = %v", ids)
	}
}

func TestServerSSEAuthAndNotFound(t *testing.T) {
	ts, _, backend := apiNewTestServer(t, 4)
	id := apiCreateSession(t, ts, t.TempDir(), "s")
	var fp *fakeProcess
	select {
	case fp = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend not started")
	}
	defer close(fp.eventsCh)

	code, data := apiDo(t, "GET", ts.URL+"/v1/sessions/"+id+"/events?after=0", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("SSE without token: %d %s", code, data)
	}
	code, data = apiDo(t, "GET", ts.URL+"/v1/sessions/nope/events?after=0", testToken, "")
	if code != http.StatusNotFound {
		t.Fatalf("SSE unknown session: %d %s", code, data)
	}
}

func TestNormalizeListenAddr(t *testing.T) {
	ok := []string{"127.0.0.1:7778", "localhost:7778", "[::1]:7778", "127.0.0.1:1"}
	for _, a := range ok {
		if _, err := NormalizeListenAddr(a); err != nil {
			t.Fatalf("%s should be accepted: %v", a, err)
		}
	}
	bad := []string{"", ":7778", "0.0.0.0:7778", "192.168.1.1:7778", "example.com:7778", "127.0.0.1:99999", "127.0.0.1"}
	for _, a := range bad {
		if _, err := NormalizeListenAddr(a); err == nil {
			t.Fatalf("%s should be rejected", a)
		}
	}
}

func TestServerMethodNotFoundJSON(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 4)
	code, data := apiDo(t, "GET", ts.URL+"/v1/nope", testToken, "")
	if code != http.StatusNotFound {
		t.Fatalf("unknown path: %d %s", code, data)
	}
	if !bytes.Contains(data, []byte(`"error"`)) {
		t.Fatalf("404 body is not JSON: %s", data)
	}
}

// ctxBoundProcess simulates exec.CommandContext: it dies as soon as the
// context handed to Backend.Start is cancelled (review finding 1).
type ctxBoundProcess struct {
	eventsCh chan sessionapi.Event
	sent     chan string
	stopCh   chan struct{}
	dieOnce  sync.Once
}

func newCtxBoundProcess() *ctxBoundProcess {
	return &ctxBoundProcess{
		eventsCh: make(chan sessionapi.Event, 16),
		sent:     make(chan string, 16),
		stopCh:   make(chan struct{}, 1),
	}
}

func (p *ctxBoundProcess) die() { p.dieOnce.Do(func() { close(p.eventsCh) }) }

func (p *ctxBoundProcess) Events() <-chan sessionapi.Event { return p.eventsCh }

func (p *ctxBoundProcess) Send(text string) error { p.sent <- text; return nil }

func (p *ctxBoundProcess) Answer(a sessionapi.Answer) error { return nil }

func (p *ctxBoundProcess) Stop(ctx context.Context) error {
	p.die()
	select {
	case p.stopCh <- struct{}{}:
	default:
	}
	return nil
}

func (p *ctxBoundProcess) Wait() error { return nil }

type ctxBoundBackend struct{ started chan *ctxBoundProcess }

func (b *ctxBoundBackend) Start(ctx context.Context, spec sessionapi.Spec) (Process, error) {
	p := newCtxBoundProcess()
	go func() {
		<-ctx.Done()
		p.die() // exec.CommandContext would kill the process here
	}()
	b.started <- p
	return p, nil
}

func TestServerCreateProcessOutlivesHTTPRequest(t *testing.T) {
	backend := &ctxBoundBackend{started: make(chan *ctxBoundProcess, 4)}
	mgr, err := NewManager(filepath.Join(t.TempDir(), "state"), backend, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Close() })
	ts := httptest.NewServer(NewServer(mgr, testToken))
	t.Cleanup(ts.Close)

	ws := t.TempDir()
	body := fmt.Sprintf(`{"version":1,"provider":"claude","workspace":{"kind":"working_dir","path":%q},"prompt":"hello"}`, ws)
	req, err := http.NewRequest("POST", ts.URL+"/v1/sessions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close() // cancels the request context
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, data)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.ID == "" {
		t.Fatalf("create response: %q (%v)", data, err)
	}

	var fp *ctxBoundProcess
	select {
	case fp = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend not started")
	}

	time.Sleep(200 * time.Millisecond) // let any request-bound kill land

	code, data := apiDo(t, "POST", ts.URL+"/v1/sessions/"+out.ID+"/messages", testToken, `{"text":"follow up"}`)
	if code != http.StatusOK {
		t.Fatalf("follow-up after request end: %d %s (process died with the request)", code, data)
	}
	select {
	case text := <-fp.sent:
		if text != "follow up" {
			t.Fatalf("sent text = %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process is dead: follow-up not delivered")
	}

	code, data = apiDo(t, "POST", ts.URL+"/v1/sessions/"+out.ID+"/stop", testToken, "")
	if code != http.StatusOK {
		t.Fatalf("stop: %d %s", code, data)
	}
	select {
	case <-fp.stopCh:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not reach the process")
	}
	apiWaitForStatus(t, ts, out.ID, sessionapi.StatusStopped)
}

// apiCollectSSE reads an SSE stream until it closes or the timeout passes,
// reporting whether the server closed it.
func apiCollectSSE(t *testing.T, url, token, lastEventID string, timeout time.Duration) ([]string, bool) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", resp.StatusCode)
	}
	lineCh := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lineCh <- sc.Text()
		}
		close(lineCh)
	}()
	var lines []string
	for {
		select {
		case l, ok := <-lineCh:
			if !ok {
				return lines, true
			}
			lines = append(lines, l)
		case <-time.After(timeout):
			return lines, false
		}
	}
}

func TestServerSSETerminalReconnectCloses(t *testing.T) {
	ts, _, backend := apiNewTestServer(t, 4)
	id := apiCreateSession(t, ts, t.TempDir(), "stream")
	var fp *fakeProcess
	select {
	case fp = <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("backend not started")
	}
	close(fp.eventsCh)
	apiWaitForStatus(t, ts, id, sessionapi.StatusFinished)

	_, data := apiDo(t, "GET", ts.URL+"/v1/sessions/"+id, testToken, "")
	var view SessionView
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	last := view.Meta.LastSeq
	if last == 0 {
		t.Fatal("session has no events")
	}

	cases := []struct {
		name   string
		query  string
		lastID string
	}{
		{"Last-Event-ID", "", strconv.FormatInt(last, 10)},
		{"after", "?after=" + strconv.FormatInt(last, 10), ""},
		// The header cursor wins over the query cursor on reconnect.
		{"header precedence", "?after=0", strconv.FormatInt(last, 10)},
	}
	for _, tc := range cases {
		lines, closed := apiCollectSSE(t, ts.URL+"/v1/sessions/"+id+"/events"+tc.query, testToken, tc.lastID, 3*time.Second)
		if !closed {
			t.Fatalf("%s: stream did not close for a terminal session caught up to its cursor", tc.name)
		}
		pings := 0
		for _, l := range lines {
			if strings.HasPrefix(l, "id: ") {
				t.Fatalf("%s: unexpected event after terminal cursor: %v", tc.name, lines)
			}
			if strings.HasPrefix(l, ": ping") {
				pings++
			}
		}
		if pings == 0 {
			t.Fatalf("%s: no heartbeat before close: %v", tc.name, lines)
		}
	}
}

func TestServerSSENegativeAfterRejected(t *testing.T) {
	ts, _, _ := apiNewTestServer(t, 4)
	id := apiCreateSession(t, ts, t.TempDir(), "s")
	code, data := apiDo(t, "GET", ts.URL+"/v1/sessions/"+id+"/events?after=-1", testToken, "")
	if code != http.StatusBadRequest {
		t.Fatalf("negative after: %d %s", code, data)
	}
}
