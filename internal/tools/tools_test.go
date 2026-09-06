package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterDocumentsAllowedTypeValues(t *testing.T) {
	validTaskTypes := []string{"bug", "chore"}
	validRelationTypes := []string{"blocks", "duplicates"}

	s := server.NewMCPServer("test-server", "1.0.0")
	Register(s, nil, validTaskTypes, validRelationTypes)

	tools := s.ListTools()

	assertStringProperty(t, tools["create_task"].Tool.InputSchema.Properties, "type",
		"Allowed values: bug, chore.",
		validTaskTypes,
	)
	assertStringProperty(t, tools["update_task"].Tool.InputSchema.Properties, "type",
		"Allowed values: bug, chore.",
		validTaskTypes,
	)
	assertStringProperty(t, tools["list_tasks"].Tool.InputSchema.Properties, "type",
		"Allowed values: bug, chore.",
		validTaskTypes,
	)
	assertStringProperty(t, tools["add_relation"].Tool.InputSchema.Properties, "type",
		"Allowed values: blocks, duplicates.",
		validRelationTypes,
	)
	assertStringProperty(t, tools["remove_relation"].Tool.InputSchema.Properties, "type",
		"Allowed values: blocks, duplicates.",
		validRelationTypes,
	)
}

func TestRegisterDocumentsFileTools(t *testing.T) {
	s := server.NewMCPServer("test-server", "1.0.0")
	Register(s, nil, []string{"feature", "bug"}, []string{"blocked_by", "relates_to", "duplicate_of"})

	tools := s.ListTools()

	cases := []struct {
		name     string
		required []string
	}{
		{"write_task_file", []string{"task_id", "filename", "content"}},
		{"read_task_file", []string{"task_id", "filename"}},
		{"list_task_files", []string{"task_id"}},
	}

	for _, c := range cases {
		tool, ok := tools[c.name]
		if !ok {
			t.Fatalf("tool %q not registered", c.name)
			continue
		}
		for _, name := range c.required {
			if _, ok := tool.Tool.InputSchema.Properties[name]; !ok {
				t.Errorf("tool %q: property %q not found", c.name, name)
			}
		}
		if len(tool.Tool.InputSchema.Required) != len(c.required) {
			t.Errorf("tool %q: required = %v, want %v", c.name, tool.Tool.InputSchema.Required, c.required)
		}
	}
}

func TestRegisterCreateTaskSchemaUsesStringIDs(t *testing.T) {
	s := server.NewMCPServer("test-server", "1.0.0")
	Register(s, nil, []string{"feature", "bug"}, []string{"blocked_by", "relates_to", "duplicate_of"})

	tools := s.ListTools()
	createTool, ok := tools["create_task"]
	if !ok {
		t.Fatal("create_task not registered")
	}

	props := createTool.Tool.InputSchema.Properties

	idProp, ok := props["id"].(map[string]any)
	if !ok {
		t.Fatalf("create_task: property %q has unexpected type %T", "id", props["id"])
	}
	if idProp["type"] != "string" {
		t.Errorf("create_task.id type = %v, want %q", idProp["type"], "string")
	}
	for _, req := range createTool.Tool.InputSchema.Required {
		if req == "id" {
			t.Error("create_task.id must be optional, found in Required")
		}
	}

	parentIDProp, ok := props["parent_id"].(map[string]any)
	if !ok {
		t.Fatalf("create_task: property %q has unexpected type %T", "parent_id", props["parent_id"])
	}
	if parentIDProp["type"] != "string" {
		t.Errorf("create_task.parent_id type = %v, want %q (not number)", parentIDProp["type"], "string")
	}
}

func newTestService(t *testing.T) *task.Service {
	t.Helper()
	dir := t.TempDir()
	st := storage.NewMarkdownStorage(dir)
	idx := storage.NewIndex(dir, st)
	cfg := &config.Config{TaskTypes: []string{"feature", "bug"}, ProjectFound: true}
	svc := task.NewService(st, st, st, idx, cfg.TaskTypes, cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return svc
}

func TestCreateTaskHandler_CustomID_ResponseIDIsJSONString(t *testing.T) {
	svc := newTestService(t)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"title":    "Custom",
		"priority": "high",
		"type":     "feature",
		"id":       "my-feature",
	}

	result, err := createTaskHandler(svc)(context.Background(), req)
	if err != nil {
		t.Fatalf("createTaskHandler() error = %v", err)
	}
	text := resultText(t, result)

	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("response %q is not valid JSON with a string id: %v", text, err)
	}
	if decoded.ID != "my-feature" {
		t.Errorf("response id = %q, want %q", decoded.ID, "my-feature")
	}
}

// TestCreateTaskHandler_LegacyNumericParentID probes mcp-go v1.0.0's
// behavior when a caller sends a raw JSON number for a field that is now
// schema-typed WithString (parent_id), simulating a client written against
// the pre-migration int-typed schema. This resolves an open design question
// empirically rather than assuming a particular failure mode. Whatever the
// observed outcome, this test pins it down as a regression guard.
func TestCreateTaskHandler_LegacyNumericParentID(t *testing.T) {
	svc := newTestService(t)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"title":     "Legacy caller",
		"priority":  "high",
		"type":      "feature",
		"parent_id": 5, // a raw JSON number, not a string - the pre-migration shape
	}

	result, err := createTaskHandler(svc)(context.Background(), req)
	if err != nil {
		t.Fatalf("createTaskHandler() returned a Go error rather than a tool error for a legacy numeric parent_id: %v", err)
	}
	if result == nil {
		t.Fatal("createTaskHandler() returned a nil result for a legacy numeric parent_id")
	}
	// req.GetString silently falls back to its default ("") when the
	// argument isn't a string, so a legacy numeric parent_id is treated as
	// "no parent" rather than erroring - document that behavior here so a
	// change in mcp-go's coercion rules is caught by this test.
	text := resultText(t, result)
	if strings.Contains(text, "parent task not found") {
		t.Errorf("expected the numeric parent_id to be silently treated as absent (top-level task), got error response: %s", text)
	}
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	textContent, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatalf("result content has unexpected type %T", result.Content[0])
	}
	return textContent.Text
}

func assertStringProperty(t *testing.T, properties map[string]any, name, wantDescriptionSuffix string, wantEnum []string) {
	t.Helper()

	raw, ok := properties[name]
	if !ok {
		t.Fatalf("property %q not found", name)
	}

	property, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("property %q has unexpected type %T", name, raw)
	}

	description, ok := property["description"].(string)
	if !ok {
		t.Fatalf("property %q description has unexpected type %T", name, property["description"])
	}
	if !strings.Contains(description, wantDescriptionSuffix) {
		t.Fatalf("property %q description = %q, want substring %q", name, description, wantDescriptionSuffix)
	}

	enumValues, ok := property["enum"].([]string)
	if !ok {
		t.Fatalf("property %q enum has unexpected type %T", name, property["enum"])
	}
	if len(enumValues) != len(wantEnum) {
		t.Fatalf("property %q enum length = %d, want %d", name, len(enumValues), len(wantEnum))
	}
	for i, value := range wantEnum {
		if enumValues[i] != value {
			t.Fatalf("property %q enum[%d] = %q, want %q", name, i, enumValues[i], value)
		}
	}
}
