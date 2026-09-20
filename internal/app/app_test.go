package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// rootsHandler answers roots/list with a fixed set of directories, the way a
// client that declares the roots capability does.
type rootsHandler struct {
	paths []string
	calls int
}

func (h *rootsHandler) ListRoots(context.Context, mcp.ListRootsRequest) (*mcp.ListRootsResult, error) {
	h.calls++
	roots := make([]mcp.Root, 0, len(h.paths))
	for _, p := range h.paths {
		roots = append(roots, mcp.Root{URI: "file://" + p, Name: filepath.Base(p)})
	}
	return &mcp.ListRootsResult{Roots: roots}, nil
}

// TestServer_ResolvesProjectFromRoots drives the whole loop the way a client
// does: initialize, then a tool call that has to discover the project through
// roots/list, with the tool schemas following the project's configuration.
func TestServer_ResolvesProjectFromRoots(t *testing.T) {
	testsupport.IsolateEnv(t)
	// Force the roots step: the environment variables would otherwise win and
	// this path would never be exercised.
	t.Setenv(config.EnvRootSource, string(config.SourceRoots))

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, config.DefaultTasksDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, config.ConfigFileName),
		[]byte("tasks_dir: .tasks\ntask_types:\n  - feature\n  - chore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	srv, resolver := newServer(nil)
	handler := &rootsHandler{paths: []string{root}}
	c := client.NewClient(transport.NewInProcessTransportWithOptions(srv,
		transport.WithRootsHandler(handler)))

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	initReq.Params.Capabilities.Roots = &struct {
		ListChanged bool `json:"listChanged,omitempty"`
	}{ListChanged: true}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Nothing has been resolved yet: no tool call has happened.
	if handler.calls != 0 {
		t.Errorf("roots/list calls before the first tool call = %d, want 0", handler.calls)
	}

	// A task of a type that only this project's config allows proves both the
	// resolution and that validation uses the project config.
	createReq := mcp.CallToolRequest{}
	createReq.Params.Name = "create_task"
	createReq.Params.Arguments = map[string]any{
		"title":    "From roots",
		"priority": "high",
		"type":     "chore",
	}
	result, err := c.CallTool(ctx, createReq)
	if err != nil {
		t.Fatalf("CallTool(create_task) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("create_task failed: %s", textOf(t, result))
	}
	if handler.calls != 1 {
		t.Errorf("roots/list calls = %d, want 1", handler.calls)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(textOf(t, result)), &created); err != nil {
		t.Fatalf("unmarshal create_task result: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create_task returned no id")
	}

	// The task must land in the project the client advertised.
	record := filepath.Join(root, config.DefaultTasksDirName, created.ID, created.ID+".md")
	if _, err := os.Stat(record); err != nil {
		t.Errorf("task record %s: %v", record, err)
	}

	// The schemas registered at startup knew only the default types; after
	// resolution they must carry the project's.
	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if got := taskTypeEnum(t, tools, "create_task"); !slices.Equal(got, []string{"feature", "chore"}) {
		t.Errorf("create_task type enum = %v, want [feature chore]", got)
	}

	// A roots change must be picked up without a restart.
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	resolver.Invalidate()
	listReq := mcp.CallToolRequest{}
	listReq.Params.Name = "list_tasks"
	if _, err := c.CallTool(ctx, listReq); err != nil {
		t.Fatalf("CallTool(list_tasks) error = %v", err)
	}
	if handler.calls != 2 {
		t.Errorf("roots/list calls after invalidation = %d, want 2", handler.calls)
	}
}

func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("tool result content type = %T, want TextContent", result.Content[0])
	}
	return text.Text
}

func taskTypeEnum(t *testing.T, tools *mcp.ListToolsResult, toolName string) []string {
	t.Helper()
	for _, tool := range tools.Tools {
		if tool.Name != toolName {
			continue
		}
		prop, ok := tool.InputSchema.Properties["type"].(map[string]any)
		if !ok {
			t.Fatalf("%s: type property = %T, want an object", toolName, tool.InputSchema.Properties["type"])
		}
		values, ok := prop["enum"].([]any)
		if !ok {
			t.Fatalf("%s: type enum = %T, want a list", toolName, prop["enum"])
		}
		out := make([]string, 0, len(values))
		for _, v := range values {
			out = append(out, v.(string))
		}
		return out
	}
	t.Fatalf("tool %q not found", toolName)
	return nil
}
