package webapp_test

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/webproc"
)

// TestTwoProcessesShareTheBacklog is the claim the whole split rests on.
//
// The dashboard runs as its own program with its own task.Service and its own
// in-memory index; the process that owns writes is a different one. Nothing
// coordinates them - no lock, no IPC, no shared memory. What makes it work is
// that records are written atomically (temp file plus rename) and that the
// index rebuilds itself when a task file's mtime moves past the moment it was
// built, or when the task count diverges. So a write here must show up there
// within a poll, and this test is what says so.
//
// The writer is this test process, which is genuinely a different process
// from the child - exactly the arrangement the MCP server and the dashboard
// are in.
func TestTwoProcessesShareTheBacklog(t *testing.T) {
	bin := testsupport.BuildDashboard(t)

	testsupport.IsolateEnv(t)
	_, svc, dir := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "1", Title: "Seeded before the dashboard started"})

	addr := freeAddr(t)
	child := exec.Command(bin, "--addr", addr)
	child.Env = append(os.Environ(), config.EnvTasksDir+"="+dir)
	child.Stdout, child.Stderr = io.Discard, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start the dashboard: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Signal(os.Interrupt)
		_, _ = child.Process.Wait()
	})

	// It comes up, records itself, and serves the backlog it was told to.
	base := "http://" + addr
	waitFor(t, base+"/healthz", "ok")

	// The marker is under the CHILD's user directory, which is the real
	// identity of whoever runs the test - not this service's test identity,
	// so it is found by scanning rather than by assuming a name.
	var marker webproc.Marker
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m, ok := anyMarker(dir); ok {
			marker = m
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if marker.Base == "" {
		t.Fatal("the dashboard did not record its session base")
	}
	if marker.Pid == os.Getpid() {
		t.Fatal("the dashboard is running in this process; the point is that it is not")
	}

	board := base + marker.Base + "/board"
	if body := waitFor(t, board, "Seeded before the dashboard started"); body == "" {
		t.Fatal("the dashboard does not serve the seeded backlog")
	}

	// Now write from this process, the way the MCP server does.
	if _, err := svc.Create("Written by another process", "", task.PriorityCritical, "feature", "", "42"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// And the dashboard sees it, with nothing told to reload: the index
	// notices the new task file by itself on the next read.
	if body := waitFor(t, board, "Written by another process"); body == "" {
		t.Error("a task written by another process never appeared on the board")
	}
}

// anyMarker returns the one marker under dir's per-user state directory,
// whoever wrote it.
func anyMarker(dir string) (webproc.Marker, bool) {
	users, err := os.ReadDir(filepath.Join(dir, ".users"))
	if err != nil {
		return webproc.Marker{}, false
	}
	for _, u := range users {
		if !u.IsDir() {
			continue
		}
		if m, ok := webproc.ReadMarker(dir, u.Name()); ok {
			return m, true
		}
	}
	return webproc.Marker{}, false
}

// waitFor polls url until the body contains want, and returns the body - or
// "" if it never did.
func waitFor(t *testing.T, url, want string) string {
	t.Helper()
	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if body := string(raw); strings.Contains(body, want) {
				return body
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ""
}

// freeAddr returns an address that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}
