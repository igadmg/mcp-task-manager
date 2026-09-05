package tools

import (
	"strings"
	"testing"

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
