package webapp

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunBasePathFromEnvironment(t *testing.T) {
	projectWithATask(t)
	t.Setenv("MCP_WEB_BASE_PATH", "/board/")
	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var logs bytes.Buffer
	go func() { done <- Run(ctx, Options{Addr: addr, Stderr: &logs}) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
			if !strings.Contains(logs.String(), "workspaces: http://"+addr+"/board/") ||
				!strings.Contains(logs.String(), "dashboard: http://"+addr+"/board/") {
				t.Errorf("logged URLs omit mount: %s", logs.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("shutdown timed out")
		}
	}()
	body := waitForBoard(t, "http://"+addr+"/")
	if !strings.Contains(body, `href="/board/"`) || !strings.Contains(body, `href="/board/static/app.css?v=`) {
		t.Errorf("trusted env mount not rendered: %s", body)
	}
}

func TestRunRejectsAnInvalidBasePath(t *testing.T) {
	projectWithATask(t)
	t.Setenv("MCP_WEB_BASE_PATH", "no-leading-slash/../x")
	if err := Run(context.Background(), Options{Addr: freePort(t), Stderr: &bytes.Buffer{}}); err == nil {
		t.Fatal("Run() accepted an invalid MCP_WEB_BASE_PATH")
	}
}
