package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/webproc"
)

// serveProject points resolution at a temp backlog holding one task.
func serveProject(t *testing.T) {
	t.Helper()
	for _, name := range []string{config.EnvTasksDir, config.EnvProjectDir, config.EnvClaudeProjectDir, config.EnvRootSource, config.EnvWebEnabled, config.EnvWebAddr} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}

	dir := t.TempDir()
	taskDir := filepath.Join(dir, "7")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	body := "---\nid: 7\ntitle: Served task\nstatus: todo\npriority: high\ntype: feature\n" +
		"created_at: 2026-01-01T00:00:00Z\nupdated_at: 2026-01-01T00:00:00Z\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(taskDir, "7.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(config.EnvTasksDir, dir)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// TestServeWebServesTheBoardAndReturnsOnCancel now covers the forwarder:
// since the split, `serve web` locates mcp-task-manager-web and runs it in
// the foreground, so this exercises the whole chain - the CLI, the locator,
// and the dashboard binary itself.
func TestServeWebServesTheBoardAndReturnsOnCancel(t *testing.T) {
	serveProject(t)
	t.Setenv(webproc.EnvBinary, testsupport.BuildDashboard(t))
	addr := freeAddr(t)

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	code := make(chan int, 1)
	go func() {
		code <- RunWithContext(ctx, []string{"mcp-task-manager", "serve", "web", "--addr", addr}, &stdout, &stderr)
	}()

	client := http.Client{Timeout: time.Second}
	base := "http://" + addr

	// The root is the workspace list now; serve web publishes the project
	// it resolved as a session, so the board lives behind that session's
	// token. Reading the token off the page keeps the test out of the
	// stderr buffer the server goroutine is still writing to.
	welcome := fetchUntil(t, client, base+"/", "Open now")
	m := sessionHref.FindStringSubmatch(welcome)
	if m == nil {
		t.Fatalf("the workspace list does not link a session:\n%s", welcome)
	}

	body := fetchUntil(t, client, base+m[1], "Served task")
	if !strings.Contains(body, "Served task") {
		t.Errorf("board did not render the backlog:\n%s", body)
	}

	cancel()
	select {
	case got := <-code:
		if got != 0 {
			t.Errorf("serve web exit code = %d, want 0 on a clean shutdown (stderr: %s)", got, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve web did not return after the context was cancelled")
	}

	if stdout.Len() != 0 {
		t.Errorf("serve web wrote to stdout: %q; stdout is the JSON-RPC channel", stdout.String())
	}
}

// TestServeWebBadAddrExitsNonZero: the forwarder passes the child's exit code
// through, and the child's stderr is the terminal's, so an occupied port is
// still a visible, non-zero failure.
func TestServeWebBadAddrExitsNonZero(t *testing.T) {
	serveProject(t)
	t.Setenv(webproc.EnvBinary, testsupport.BuildDashboard(t))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	var stdout, stderr bytes.Buffer
	code := RunWithContext(context.Background(),
		[]string{"mcp-task-manager", "serve", "web", "--addr", ln.Addr().String()},
		&stdout, &stderr)

	if code == 0 {
		t.Errorf("serve web on an occupied port exit code = 0, want non-zero (stderr: %q)", stderr.String())
	}
	if !strings.Contains(stderr.String(), "start dashboard") {
		t.Errorf("stderr = %q, want the child's bind failure", stderr.String())
	}
}

// TestServeWebWithoutTheBinarySaysWhereItLooked: the dashboard is a second
// artefact now, so the most likely failure is simply not having installed it.
func TestServeWebWithoutTheBinarySaysWhereItLooked(t *testing.T) {
	serveProject(t)
	// An override pointing at nothing, and a PATH with no dashboard on it.
	t.Setenv(webproc.EnvBinary, filepath.Join(t.TempDir(), "absent"))
	t.Setenv("PATH", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := RunWithContext(context.Background(),
		[]string{"mcp-task-manager", "serve", "web"}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	for _, want := range []string{"mcp-task-manager-web", webproc.EnvBinary, "go install"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want %q in it", stderr.String(), want)
		}
	}
}

// sessionHref matches the workspace list's link into a live session.
var sessionHref = regexp.MustCompile(`href="(/[A-Za-z0-9_-]{16,}/)"`)

// fetchUntil polls url until the body contains want, or the deadline passes.
// The server is still starting up when the test begins, so a miss is a
// retry, not a failure.
func fetchUntil(t *testing.T, client http.Client, url, want string) string {
	t.Helper()
	var body string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(raw)
			if strings.Contains(body, want) {
				return body
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return body
}
