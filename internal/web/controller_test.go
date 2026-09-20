package web

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

func newTestController(t *testing.T) *Controller {
	t.Helper()
	rs, _, _ := testsupport.NewBacklog(t)
	return NewController(Deps{
		Project: rs.Current,
		Logger:  log.New(io.Discard, "", 0),
	}, "127.0.0.1:0")
}

func TestControllerStartIdempotent(t *testing.T) {
	c := newTestController(t)
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })

	first, already, err := c.Start("")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if already {
		t.Error("first Start() reported already running")
	}

	second, already, err := c.Start("")
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if !already {
		t.Error("second Start() did not report already running")
	}
	if second != first {
		t.Errorf("second Start() = %q, want the running URL %q", second, first)
	}
}

func TestControllerStartPortZero(t *testing.T) {
	c := newTestController(t)
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })

	url, _, err := c.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if strings.HasSuffix(url, ":0") {
		t.Fatalf("Start() = %q; :0 must resolve to the real port before returning", url)
	}

	resp, err := http.Get(url + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("/healthz = %q, want ok", body)
	}
}

func TestControllerStartBadAddrIsAStartupError(t *testing.T) {
	c := newTestController(t)

	// Occupy a port, then ask the controller for the same one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	if _, _, err := c.Start(ln.Addr().String()); err == nil {
		t.Error("Start() on an occupied port returned no error")
	}
	if _, running := c.URL(); running {
		t.Error("a failed Start() left the controller looking like it is running")
	}
}

func TestControllerShutdown(t *testing.T) {
	c := newTestController(t)
	url, _, err := c.Start("")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, running := c.URL(); running {
		t.Error("URL() still reports a running server after Shutdown()")
	}

	client := http.Client{Timeout: time.Second}
	if _, err := client.Get(url + "/healthz"); err == nil {
		t.Error("the port still answers after Shutdown()")
	}
}

func TestShutdownIdempotent(t *testing.T) {
	c := newTestController(t)

	if err := c.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() before Start() error = %v, want nil", err)
	}
	if _, _, err := c.Start(""); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := c.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown() error = %v, want nil", err)
	}
}
