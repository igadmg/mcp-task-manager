package app

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/webproc"
	"github.com/mark3labs/mcp-go/server"
)

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

// runMCP starts the server with a pipe on stdin so Listen blocks instead of
// seeing EOF, and returns a function that stops it.
func runMCP(t *testing.T, opts Options) func() {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = r

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = RunMCP(ctx, opts)
	}()

	return func() {
		cancel()
		w.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("RunMCP() did not return after the context was cancelled")
		}
		os.Stdin = oldStdin
		r.Close()
	}
}

// TestRunMCPStartsNoDashboardBeforeResolution: web.enabled spawns a dashboard
// for a *backlog*, and before the first tool call there is none - the MCP
// roots step needs a client session. So nothing is started and no port opens,
// which is also why the spawn hangs off OnResolve rather than off startup.
func TestRunMCPStartsNoDashboardBeforeResolution(t *testing.T) {
	projectWithATask(t)
	addr := freePort(t)

	stop := runMCP(t, Options{Web: config.WebConfig{Enabled: true, Addr: addr}, Stderr: io.Discard})
	defer stop()

	client := http.Client{Timeout: 500 * time.Millisecond}
	time.Sleep(100 * time.Millisecond)
	if _, err := client.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("a port is open although no tool call has resolved a backlog yet")
	}
}

func TestRunMCPDoesNotStartWebByDefault(t *testing.T) {
	projectWithATask(t)
	addr := freePort(t)

	stop := runMCP(t, Options{Web: config.WebConfig{Addr: addr}, Stderr: io.Discard})
	defer stop()

	client := http.Client{Timeout: 500 * time.Millisecond}
	time.Sleep(100 * time.Millisecond)
	if _, err := client.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("a port is open although web.enabled is false")
	}
}

func TestRunMCPStopsOnContextCancel(t *testing.T) {
	projectWithATask(t)
	stop := runMCP(t, Options{Stderr: io.Discard})
	stop() // the assertion lives in runMCP's timeout
}

// TestRunMCPSpawnsRatherThanServing is what replaced
// TestRunMCPWebSharesResolution: the two transports no longer share a service,
// a registry or a process. Resolving a project tells the spawner which backlog
// to serve and nothing else; this process publishes no session and binds no
// port, and the dashboard it starts is another program.
func TestRunMCPSpawnsRatherThanServing(t *testing.T) {
	dir := projectWithATask(t)

	// A stand-in for the dashboard binary: it records that it ran and the
	// environment it was given, then exits. The real one would bind and
	// write a marker.
	ran := filepath.Join(t.TempDir(), "ran")
	fake := writeFakeDashboard(t, ran)
	t.Setenv(webproc.EnvBinary, fake)

	logger := log.New(io.Discard, "", 0)
	var srv *server.MCPServer
	resolver := newLazyResolver(&srv)
	spawner := &webproc.Spawner{Addr: "127.0.0.1:0", Logger: logger}
	srv = newServerFor(resolver, spawner)

	// What RunMCP wires: resolving hands the backlog to the spawner.
	resolver.OnResolve(func(resolved *project.Resolved) {
		spawner.TasksDir = resolved.Config.TasksDir()
		spawner.User = resolved.Service.UserName()
		startDashboard(logger, spawner)
	})

	// Before resolution the spawner has no backlog, so it refuses rather
	// than guessing one.
	if _, _, err := spawner.Start(""); err == nil {
		t.Error("Start() with no resolved backlog did not refuse")
	}

	resolved, err := resolver.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := spawner.TasksDir; got != dir {
		t.Errorf("the spawner serves %q, want the resolved backlog %q", got, dir)
	}
	// The write still happens here, in the process that owns writes.
	if _, err := resolved.Service.Create("Made through the service", "", task.PriorityCritical, "feature", "", "99"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// And the dashboard was started as a separate program.
	if !waitForFile(t, ran) {
		t.Fatal("the dashboard binary was never run")
	}
	out, err := os.ReadFile(ran)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), dir) {
		t.Errorf("the child was not told which backlog to serve: %q", out)
	}
}

// writeFakeDashboard writes a tiny script that records its MCP_TASKS_DIR and
// exits, so a test can see that the spawner ran a program without needing the
// real dashboard built.
func writeFakeDashboard(t *testing.T, record string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake dashboard is a shell script")
	}
	path := filepath.Join(t.TempDir(), "mcp-task-manager-web")
	script := "#!/bin/sh\nprintf '%s' \"$MCP_TASKS_DIR\" > " + record + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForFile(t *testing.T, path string) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
