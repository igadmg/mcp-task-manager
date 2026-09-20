package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
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

func TestServeWebServesTheBoardAndReturnsOnCancel(t *testing.T) {
	serveProject(t)
	addr := freeAddr(t)

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	code := make(chan int, 1)
	go func() {
		code <- RunWithContext(ctx, []string{"mcp-task-manager", "serve", "web", "--addr", addr}, &stdout, &stderr)
	}()

	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/")
		if err == nil {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(raw)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
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

func TestServeWebBadAddrExitsNonZero(t *testing.T) {
	serveProject(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	var stdout, stderr bytes.Buffer
	code := RunWithContext(context.Background(),
		[]string{"mcp-task-manager", "serve", "web", "--addr", ln.Addr().String()},
		&stdout, &stderr)

	if code != 1 {
		t.Errorf("serve web on an occupied port exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Errorf("stderr = %q, want an error message", stderr.String())
	}
}
