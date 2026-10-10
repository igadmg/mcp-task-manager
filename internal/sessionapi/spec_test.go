package sessionapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func validSpecJSON() string {
	return `{
		"version": 1,
		"provider": "claude",
		"workspace": {"kind": "working_dir", "path": "D:/work/proj"},
		"prompt": "do the thing",
		"task_id": "my-task-1"
	}`
}

func mustParse(t *testing.T, data string) Spec {
	t.Helper()
	spec, err := ParseSpec([]byte(data))
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	return spec
}

func TestParseSpec_RoundTrip(t *testing.T) {
	spec := mustParse(t, validSpecJSON())
	if spec.Version != 1 || spec.Provider != "claude" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if spec.Workspace.Kind != "working_dir" || spec.Workspace.Path != "D:/work/proj" {
		t.Fatalf("unexpected workspace: %+v", spec.Workspace)
	}
	if spec.Prompt != "do the thing" || spec.TaskID != "my-task-1" {
		t.Fatalf("unexpected fields: %+v", spec)
	}
}

func TestParseSpec_RejectsUnknownFields(t *testing.T) {
	_, err := ParseSpec([]byte(`{"version":1,"provider":"claude","workspace":{"kind":"working_dir","path":"D:/w"},"prompt":"x","typo_field":true}`))
	var e *Error
	if !asError(err, &e) || e.Code != ErrCodeInvalidSpec {
		t.Fatalf("want invalid_spec, got %v", err)
	}
}

func TestParseSpec_RejectsMalformedJSON(t *testing.T) {
	for _, data := range []string{`{`, `[]`, `"str"`, validSpecJSON() + ` {"version":1}`} {
		if _, err := ParseSpec([]byte(data)); err == nil {
			t.Fatalf("want error for %q", data)
		}
	}
}

func TestSpec_Validate_Branches(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*Spec)
		wantCode string
	}{
		{"ok", func(*Spec) {}, ""},
		{"version 0", func(s *Spec) { s.Version = 0 }, ErrCodeInvalidSpec},
		{"version 2", func(s *Spec) { s.Version = 2 }, ErrCodeInvalidSpec},
		{"provider", func(s *Spec) { s.Provider = "gemini" }, ErrCodeUnsupportedProvider},
		{"workspace kind", func(s *Spec) { s.Workspace.Kind = "git_clone" }, ErrCodeUnsupportedWorkspaceKind},
		{"relative path", func(s *Spec) { s.Workspace.Path = "work/proj" }, ErrCodeInvalidSpec},
		{"empty path", func(s *Spec) { s.Workspace.Path = "" }, ErrCodeInvalidSpec},
		{"empty prompt", func(s *Spec) { s.Prompt = "  " }, ErrCodeInvalidSpec},
		{"prompt too big", func(s *Spec) { s.Prompt = strings.Repeat("x", MaxPromptBytes+1) }, ErrCodeInvalidSpec},
		{"task id uppercase", func(s *Spec) { s.TaskID = "MyTask" }, ErrCodeInvalidSpec},
		{"task id double dash", func(s *Spec) { s.TaskID = "a--b" }, ErrCodeInvalidSpec},
		{"task id trailing dash", func(s *Spec) { s.TaskID = "a-" }, ErrCodeInvalidSpec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := mustParse(t, validSpecJSON())
			tc.mutate(&spec)
			err := spec.Validate()
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			var e *Error
			if !asError(err, &e) || e.Code != tc.wantCode {
				t.Fatalf("want code %q, got %v", tc.wantCode, err)
			}
		})
	}
}

func TestSpec_Validate_PromptAtLimit(t *testing.T) {
	spec := mustParse(t, validSpecJSON())
	spec.Prompt = strings.Repeat("x", MaxPromptBytes)
	if err := spec.Validate(); err != nil {
		t.Fatalf("prompt at limit must pass: %v", err)
	}
}

func TestSpec_Validate_TaskIDVariants(t *testing.T) {
	for _, id := range []string{"", "a", "a-b-c", "123", "task-2-do"} {
		spec := mustParse(t, validSpecJSON())
		spec.TaskID = id
		if err := spec.Validate(); err != nil {
			t.Fatalf("task_id %q must pass: %v", id, err)
		}
	}
}

func TestSpec_JSONShape(t *testing.T) {
	spec := mustParse(t, validSpecJSON())
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["workspace"].(map[string]any)["kind"]; !ok {
		t.Fatalf("workspace.kind missing: %s", data)
	}
}
