package tools

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// fieldsOf reads the "fields" map out of a tool result's JSON payload.
func fieldsOf(t *testing.T, payload string) map[string]any {
	t.Helper()
	var got struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, payload)
	}
	return got.Fields
}

func TestCreateAndGetTaskCarryFields(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)

	created := callTool(t, createTaskHandler(rs), map[string]any{
		"title":    "With fields",
		"priority": "high",
		"type":     "feature",
		"id":       "f1",
		"fields":   map[string]any{"complexity": "high", "count": float64(3), "flag": true},
	})
	want := map[string]any{"complexity": "high", "count": float64(3), "flag": true}
	if got := fieldsOf(t, resultText(t, created)); !reflect.DeepEqual(got, want) {
		t.Errorf("create_task fields = %#v, want %#v", got, want)
	}

	got := fieldsOf(t, resultText(t, callTool(t, getTaskHandler(rs), map[string]any{"id": "f1"})))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("get_task fields = %#v, want %#v", got, want)
	}
}

func TestUpdateTaskMergesAndRemovesFields(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	callTool(t, createTaskHandler(rs), map[string]any{
		"title": "T", "priority": "low", "type": "bug", "id": "f1",
		"fields": map[string]any{"area": "web", "complexity": "high"},
	})

	// A key given is set, a key left out is untouched.
	updated := callTool(t, updateTaskHandler(rs), map[string]any{
		"id": "f1", "fields": map[string]any{"complexity": "low"},
	})
	want := map[string]any{"area": "web", "complexity": "low"}
	if got := fieldsOf(t, resultText(t, updated)); !reflect.DeepEqual(got, want) {
		t.Errorf("update_task fields = %#v, want %#v", got, want)
	}

	// An update that does not mention fields leaves them alone.
	title := "Renamed"
	plain := callTool(t, updateTaskHandler(rs), map[string]any{"id": "f1", "title": title})
	if got := fieldsOf(t, resultText(t, plain)); !reflect.DeepEqual(got, want) {
		t.Errorf("an update without fields changed them: %#v", got)
	}

	// null and "" remove.
	cleared := callTool(t, updateTaskHandler(rs), map[string]any{
		"id": "f1", "fields": map[string]any{"area": nil, "complexity": ""},
	})
	if got := fieldsOf(t, resultText(t, cleared)); len(got) != 0 {
		t.Errorf("fields after removal = %#v, want none", got)
	}
}

func TestFieldErrorsAreToolErrorsNotCrashes(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)

	cases := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"a reserved key", map[string]any{"title": "x"}, "reserved"},
		{"an uppercase key", map[string]any{"Area": "x"}, "lowercase"},
		{"a map value", map[string]any{"area": map[string]any{"x": 1}}, "must be a string"},
		{"a list value", map[string]any{"area": []any{1}}, "must be a string"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := callTool(t, createTaskHandler(rs), map[string]any{
				"title": "T", "priority": "low", "type": "bug", "fields": tc.fields,
			})
			if !result.IsError {
				t.Fatalf("create_task = %s, want an error", resultText(t, result))
			}
			if text := resultText(t, result); !strings.Contains(text, tc.want) {
				t.Errorf("error = %q, want it to mention %q", text, tc.want)
			}
		})
	}
}

func TestListTasksFiltersByFields(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	callTool(t, createTaskHandler(rs), map[string]any{
		"title": "Web", "priority": "high", "type": "feature", "id": "w",
		"fields": map[string]any{"area": "web", "count": float64(3)},
	})
	callTool(t, createTaskHandler(rs), map[string]any{
		"title": "Cli", "priority": "high", "type": "feature", "id": "c",
		"fields": map[string]any{"area": "cli"},
	})

	ids := func(payload string) []string {
		var tasks []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(payload), &tasks); err != nil {
			t.Fatalf("payload is not a task list: %v\n%s", err, payload)
		}
		var out []string
		for _, item := range tasks {
			out = append(out, item.ID)
		}
		return out
	}

	all := ids(resultText(t, callTool(t, listTasksHandler(rs), map[string]any{})))
	if len(all) != 2 {
		t.Fatalf("unfiltered list = %v, want both tasks", all)
	}

	web := ids(resultText(t, callTool(t, listTasksHandler(rs), map[string]any{
		"fields": map[string]any{"area": "web"},
	})))
	if !reflect.DeepEqual(web, []string{"w"}) {
		t.Errorf("filtered list = %v, want [w]", web)
	}

	// A numeric filter value matches the stored number either way round.
	for _, value := range []any{float64(3), "3"} {
		got := ids(resultText(t, callTool(t, listTasksHandler(rs), map[string]any{
			"fields": map[string]any{"count": value},
		})))
		if !reflect.DeepEqual(got, []string{"w"}) {
			t.Errorf("filter count=%v (%T) = %v, want [w]", value, value, got)
		}
	}

	none := callTool(t, listTasksHandler(rs), map[string]any{
		"fields": map[string]any{"area": "nowhere"},
	})
	if text := resultText(t, none); !strings.Contains(text, "No tasks found") {
		t.Errorf("a filter matching nothing = %q", text)
	}
}

// TestFieldsSurviveGitFlows covers the flows that rewrite a record inside a
// journaled git transaction: a start cuts a branch and stamps three branch
// fields, a completion squashes and stamps two more, and a rollback restores
// a copy of the record. captureTask's copy is a struct copy, so the fields
// map has to be cloned or an undo would restore a mutated one.
func TestFieldsSurviveGitFlows(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoIgnored)
	if _, err := b.Svc.Create("Add login", "", "high", "feature", "", "login",
		task.WithCreateFields(map[string]any{"area": "web", "complexity": "high"})); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	want := map[string]string{"area": "web", "complexity": "high"}
	check := func(stage string) {
		t.Helper()
		got, err := b.Svc.Get("login")
		if err != nil {
			t.Fatalf("Get() after %s: %v", stage, err)
		}
		for key, value := range want {
			if got.Fields.String(key) != value {
				t.Errorf("after %s, field %s = %q, want %q (all: %#v)", stage, key, got.Fields.String(key), value, got.Fields)
			}
		}
		if len(got.Fields) != len(want) {
			t.Errorf("after %s, fields = %#v, want exactly %v", stage, got.Fields, want)
		}
	}

	if _, err := b.Svc.StartTask("login"); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	check("start_task")

	testsupport.WriteFile(t, filepath.Join(b.CodeDir, "login.go"), "package login\n")
	if _, err := b.Svc.CompleteTask("login"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	check("complete_task")

	// The branch fields the flows stamped are there alongside the free-form
	// ones, which is the point: the two live in the same frontmatter and
	// neither displaces the other.
	done, _ := b.Svc.Get("login")
	if done.FinalBranch == "" || done.SquashCommit == "" {
		t.Errorf("git fields missing after completion: %+v", done)
	}
}
