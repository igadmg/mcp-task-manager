package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// fakeHost is the composition-level stand-in for the session host: it
// answers /healthz and an empty session list with exactly the shapes
// internal/sessionhost writes (design §6).
type fakeHost struct {
	token string
}

func (f *fakeHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "test", "sessions_enabled": true})
		return
	}
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != f.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/v1/sessions" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []any{}})
		return
	}
	http.NotFound(w, r)
}

// TestRunServesSessionsWhenEnabled is the composition-root half of the
// opt-in: web.sessions.enabled in the project config makes Run build a
// hostclient and register the session routes, without importing the host
// process's packages.
func TestRunServesSessionsWhenEnabled(t *testing.T) {
	testsupport.IsolateEnv(t)
	_, _, dir := testsupport.NewBacklog(t)
	root := filepath.Dir(dir)

	host := &fakeHost{token: "host-token"}
	srv := httptest.NewServer(host)
	t.Cleanup(srv.Close)
	tokenPath := filepath.Join(t.TempDir(), "host.token")
	if err := os.WriteFile(tokenPath, []byte(host.token), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgYAML := fmt.Sprintf("web:\n  sessions:\n    enabled: true\n    host_addr: %s\n    token_file: %s\n",
		listenAddr(srv.URL), filepath.ToSlash(tokenPath))
	if err := os.WriteFile(filepath.Join(root, config.ConfigFileName), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvTasksDir, dir)

	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, Options{Addr: addr, Stderr: io.Discard}) }()

	welcome := waitForBoard(t, "http://"+addr+"/")
	m := sessionHref.FindStringSubmatch(welcome)
	if m == nil {
		t.Fatalf("the workspace list does not link the resolved project:\n%s", welcome)
	}
	body := waitForBoard(t, "http://"+addr+m[1]+"sessions")
	if !strings.Contains(body, "Sessions") {
		t.Errorf("the session list page did not render:\n%s", body)
	}
}

// A non-loopback session host address is a startup error: the web server
// must never tunnel session traffic anywhere else.
func TestRunRejectsANonLoopbackSessionHost(t *testing.T) {
	testsupport.IsolateEnv(t)
	_, _, dir := testsupport.NewBacklog(t)
	root := filepath.Dir(dir)

	cfgYAML := "web:\n  sessions:\n    enabled: true\n    host_addr: example.com:7778\n"
	if err := os.WriteFile(filepath.Join(root, config.ConfigFileName), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvTasksDir, dir)

	err := Run(context.Background(), Options{Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Errorf("Run() error = %v, want a loopback refusal", err)
	}
}
