package webapp

import (
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
	"github.com/gpayer/mcp-task-manager/internal/web"
	"github.com/gpayer/mcp-task-manager/internal/webproc"
)

// These four moved here from internal/app when the dashboard became its own
// process: internal/app no longer has a web entry point, and this package is
// the one that serves.

// projectWithATask writes a minimal backlog and points resolution at it.
func projectWithATask(t *testing.T) string {
	t.Helper()
	testsupport.IsolateEnv(t)

	_, svc, dir := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "1", Title: "Visible on the board", Priority: "high"})

	t.Setenv(config.EnvTasksDir, dir)
	return dir
}

// freePort returns a port that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitForBoard(t *testing.T, url string) string {
	t.Helper()
	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return string(body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no answer from %s", url)
	return ""
}

// sessionHref matches the workspace list's link into a live session.
var sessionHref = regexp.MustCompile(`href="(/[A-Za-z0-9_-]{16,}/)"`)

func TestRunStartsAndShutsDown(t *testing.T) {
	projectWithATask(t)
	addr := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Addr: addr, Stderr: io.Discard})
	}()

	// The root is the workspace list; Run publishes the project it resolved
	// as a session, so the board is behind that session's token.
	welcome := waitForBoard(t, "http://"+addr+"/")
	m := sessionHref.FindStringSubmatch(welcome)
	if m == nil {
		t.Fatalf("the workspace list does not link the resolved project:\n%s", welcome)
	}
	if body := waitForBoard(t, "http://"+addr+m[1]); !strings.Contains(body, "Visible on the board") {
		t.Errorf("board did not render the seeded task:\n%s", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil on a cancelled context", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after the context was cancelled")
	}

	client := http.Client{Timeout: time.Second}
	if _, err := client.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("the port still answers after Run() returned")
	}
}

func TestRunBadAddrIsAStartupError(t *testing.T) {
	projectWithATask(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	if err := Run(context.Background(), Options{Addr: ln.Addr().String(), Stderr: io.Discard}); err == nil {
		t.Error("Run() on an occupied port returned no error")
	}
}

// TestRunRecordsAndClearsItsMarker is how the MCP process finds a dashboard it
// did not start and does not share memory with. The marker's whole lifetime
// belongs to this process: written after binding, removed on clean shutdown.
func TestRunRecordsAndClearsItsMarker(t *testing.T) {
	dir := projectWithATask(t)
	addr := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Addr: addr, Stderr: io.Discard})
	}()
	waitForBoard(t, "http://"+addr+"/healthz")

	// The user directory is the service's own; find the one marker there.
	marker, user := findMarker(t, dir)
	if marker.Addr != addr {
		t.Errorf("marker Addr = %q, want %q", marker.Addr, addr)
	}
	if marker.Pid != os.Getpid() {
		t.Errorf("marker Pid = %d, want this process %d", marker.Pid, os.Getpid())
	}
	if marker.Base == "" {
		t.Error("marker has no session base, so a spawner could only link the workspace list")
	}
	if marker.TasksDir != dir {
		t.Errorf("marker TasksDir = %q, want %q", marker.TasksDir, dir)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, ok := webproc.ReadMarker(dir, user); ok {
		t.Error("the marker outlived the dashboard that wrote it")
	}
}

// TestRunIdentifiesItsBacklogOnHealthz: the header is what lets a would-be
// spawner tell this dashboard from anything else holding the port.
func TestRunIdentifiesItsBacklogOnHealthz(t *testing.T) {
	dir := projectWithATask(t)
	addr := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, Options{Addr: addr, Stderr: io.Discard}) }()
	waitForBoard(t, "http://"+addr+"/healthz")

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get(web.HealthHeader); got != dir {
		t.Errorf("%s = %q, want the backlog it serves (%q)", web.HealthHeader, got, dir)
	}
}

// TestRunDoesNotMigrateALegacyLayout is the write-ownership rule where it is
// easiest to break: resolution used to run Initialize, which migrates the flat
// layout and may auto-archive. A whole process that only reads must not move a
// file on disk - the MCP process does the migration when it next starts.
func TestRunDoesNotMigrateALegacyLayout(t *testing.T) {
	testsupport.IsolateEnv(t)
	dir := t.TempDir()
	legacy := filepath.Join(dir, "7.md")
	const record = `---
id: "7"
title: Written in the old flat layout
status: todo
priority: medium
type: feature
created_at: 2026-01-15T10:30:00Z
updated_at: 2026-01-15T10:30:00Z
---

Body.
`
	if err := os.WriteFile(legacy, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvTasksDir, dir)

	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, Options{Addr: addr, Stderr: io.Discard}) }()

	// It serves, which is the other half of the rule: a legacy layout is
	// rendered as the read-only service sees it, not refused.
	if body := waitForBoard(t, "http://"+addr+"/"); !strings.Contains(body, "workspace") {
		t.Errorf("the dashboard did not render against a legacy layout:\n%s", body)
	}

	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("the flat record was moved: %v - the web process migrated the layout", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "7", "7.md")); err == nil {
		t.Error("the web process migrated the record into the per-task layout")
	}
}

// findMarker returns the one marker under dir's .users, and the user it
// belongs to.
func findMarker(t *testing.T, dir string) (webproc.Marker, string) {
	t.Helper()
	users, err := os.ReadDir(filepath.Join(dir, ".users"))
	if err != nil {
		t.Fatalf("no per-user state directory: %v", err)
	}
	for _, u := range users {
		if !u.IsDir() {
			continue
		}
		if m, ok := webproc.ReadMarker(dir, u.Name()); ok {
			return m, u.Name()
		}
	}
	t.Fatal("no marker was written")
	return webproc.Marker{}, ""
}
