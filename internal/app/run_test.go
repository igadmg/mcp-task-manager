package app

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/web"
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

func TestRunWebStartsAndShutsDown(t *testing.T) {
	projectWithATask(t)
	addr := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunWeb(ctx, Options{Web: config.WebConfig{Enabled: true, Addr: addr}, Stderr: io.Discard})
	}()

	// The root is the workspace list; RunWeb publishes the project it
	// resolved as a session, so the board is behind that session's token.
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
			t.Errorf("RunWeb() error = %v, want nil on a cancelled context", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunWeb() did not return after the context was cancelled")
	}

	client := http.Client{Timeout: time.Second}
	if _, err := client.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("the port still answers after RunWeb() returned")
	}
}

func TestRunWebBadAddrIsAStartupError(t *testing.T) {
	projectWithATask(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	err = RunWeb(context.Background(), Options{
		Web:    config.WebConfig{Enabled: true, Addr: ln.Addr().String()},
		Stderr: io.Discard,
	})
	if err == nil {
		t.Error("RunWeb() on an occupied port returned no error")
	}
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

func TestRunMCPStartsWebWhenEnabled(t *testing.T) {
	projectWithATask(t)
	addr := freePort(t)

	stop := runMCP(t, Options{Web: config.WebConfig{Enabled: true, Addr: addr}, Stderr: io.Discard})
	defer stop()

	// No client has made a tool call yet, so nothing is resolved by design
	// and the root is the workspace list: there is no session to serve and
	// a GET may not create one.
	body := waitForBoard(t, "http://"+addr+"/")
	if !strings.Contains(body, "No workspaces are configured") {
		t.Errorf("the root is not the empty workspace list before the first tool call:\n%s", body)
	}
	if sessionHref.MatchString(body) {
		t.Error("a session exists before any tool call resolved a project")
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

// TestRunMCPWebSharesResolution is the single-process claim: the dashboard and
// the MCP tools read the same *task.Service, so a task created through a tool
// call shows up on the board without anything being reloaded.
//
// It also pins the two halves of the read-only rule. Before a tool call there
// is no session at all, because resolving is what publishes one and only a
// tool call may resolve; afterwards the board is reachable, under a token.
func TestRunMCPWebSharesResolution(t *testing.T) {
	projectWithATask(t)

	logger := log.New(io.Discard, "", 0)
	var srv *server.MCPServer
	resolver := newLazyResolver(&srv)
	sessions := web.NewSessions(web.SessionsConfig{Logger: logger})
	controller := web.NewController(web.Deps{Sessions: sessions, Logger: logger}, "127.0.0.1:0")
	srv = newServerFor(resolver, controller)

	// What RunMCP wires: resolving publishes the project on the dashboard.
	var published *web.Session
	resolver.OnResolve(func(resolved *project.Resolved) {
		sess, err := sessions.Adopt(resolved)
		if err != nil {
			t.Errorf("Adopt() error = %v", err)
			return
		}
		published = sess
	})

	url, _, err := controller.Start("")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer controller.Shutdown(context.Background())

	body := waitForBoard(t, url+"/")
	if !strings.Contains(body, "No workspaces are configured") {
		t.Fatalf("the root is not the empty workspace list; a GET resolved the project:\n%s", body)
	}
	if len(sessions.Live()) != 0 {
		t.Fatal("a session exists before any tool call resolved the project")
	}

	// What withService does on the first tool call.
	resolved, err := resolver.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if published == nil {
		t.Fatal("resolving did not publish the project on the dashboard")
	}
	if _, err := resolved.Service.Create("Made through the service", "", task.PriorityCritical, "feature", "", "99"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// start_web_ui reports exactly this path.
	path, found := controller.SessionPath(resolved.Config.TasksDir())
	if !found || path != published.Base()+"/" {
		t.Errorf("SessionPath() = %q, %v; want %q", path, found, published.Base()+"/")
	}

	body = waitForBoard(t, url+published.Base()+"/board")
	for _, want := range []string{"Visible on the board", "Made through the service"} {
		if !strings.Contains(body, want) {
			t.Errorf("board is missing %q; the transports are not sharing one service", want)
		}
	}
}

// sessionHref matches the workspace list's link into a live session.
var sessionHref = regexp.MustCompile(`href="(/[A-Za-z0-9_-]{16,}/)"`)
