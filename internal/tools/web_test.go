package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// fakeStarter records what the tool asked for and behaves like the real
// controller: the first Start binds, every later one reports the same URL.
type fakeStarter struct {
	mu      sync.Mutex
	url     string
	addrs   []string
	running bool
}

func (f *fakeStarter) Start(addr string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addrs = append(f.addrs, addr)
	if f.running {
		return f.url, true, nil
	}
	f.running = true
	f.url = "http://127.0.0.1:7777"
	return f.url, false, nil
}

func (f *fakeStarter) URL() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.url, f.running
}

func toolNames(tools []server.ServerTool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, t := range tools {
		names[t.Tool.Name] = true
	}
	return names
}

func TestStartWebUIIdempotent(t *testing.T) {
	rs := newTestService(t)
	web := &fakeStarter{}
	handler := startWebUIHandler(rs, web)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"addr": "127.0.0.1:7777"}

	first, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("start_web_ui error = %v", err)
	}
	if got := textOf(t, first); !strings.Contains(got, "started") || !strings.Contains(got, "http://") {
		t.Errorf("first call = %q, want a started message with the URL", got)
	}

	second, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("second start_web_ui error = %v", err)
	}
	if got := textOf(t, second); !strings.Contains(got, "already running") {
		t.Errorf("second call = %q, want an already-running message", got)
	}

	if len(web.addrs) != 2 {
		t.Fatalf("starter saw %d calls, want 2", len(web.addrs))
	}
	if web.addrs[0] != "127.0.0.1:7777" {
		t.Errorf("addr passed through = %q, want 127.0.0.1:7777", web.addrs[0])
	}
}

func TestStartWebUIReportsStartFailure(t *testing.T) {
	rs := newTestService(t)
	handler := startWebUIHandler(rs, failingStarter{})

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("start_web_ui error = %v", err)
	}
	if !result.IsError {
		t.Error("a bind failure did not come back as a tool error")
	}
}

type failingStarter struct{}

func (failingStarter) Start(string) (string, bool, error) {
	return "", false, errors.New("address already in use")
}
func (failingStarter) URL() (string, bool) { return "", false }

func TestBuildOmitsStartWebUIWhenNil(t *testing.T) {
	names := toolNames(Build(nil, []string{"feature"}, []string{"blocked_by"}, nil))
	if names["start_web_ui"] {
		t.Error("start_web_ui is in the default tool set; it must only appear with a WebStarter")
	}
	if !names["create_task"] {
		t.Error("the rest of the tool set went missing")
	}
}

// TestSetToolsRepublicationKeepsStartWebUI guards the silent hazard: the
// server rebuilds its tool set when the resolved project configures different
// task types, and a rebuild that forgets the starter drops the tool with no
// error anywhere.
func TestSetToolsRepublicationKeepsStartWebUI(t *testing.T) {
	web := &fakeStarter{}

	initial := toolNames(Build(nil, []string{"feature", "bug"}, []string{"blocked_by"}, web))
	if !initial["start_web_ui"] {
		t.Fatal("start_web_ui is missing from the initial tool set")
	}

	republished := toolNames(Build(nil, []string{"chore", "docs"}, []string{"blocked_by"}, web))
	if !republished["start_web_ui"] {
		t.Error("start_web_ui vanished when the tool schemas were republished")
	}
}

// TestEveryToolHasDescriptions turns descriptions.go's package-init panic into
// a permanent test failure: a tool registered without its YAML kills the whole
// package at load time, and the failure looks unrelated to the change.
func TestEveryToolHasDescriptions(t *testing.T) {
	for _, tool := range Build(nil, []string{"feature"}, []string{"blocked_by"}, &fakeStarter{}) {
		if _, ok := toolTexts[tool.Tool.Name]; !ok {
			t.Errorf("tool %q has no descriptions/%s.yaml", tool.Tool.Name, tool.Tool.Name)
		}
	}
}

func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("tool returned no content")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("tool returned %T, want text content", result.Content[0])
	}
	return text.Text
}
