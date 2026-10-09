package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

func TestParseFieldArgs(t *testing.T) {
	got, err := ParseFieldArgs([]string{"area=web", "complexity=high"})
	if err != nil {
		t.Fatalf("ParseFieldArgs() error = %v", err)
	}
	if want := map[string]any{"area": "web", "complexity": "high"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFieldArgs() = %#v, want %#v", got, want)
	}

	// Only the first "=" separates, so a value may hold one.
	got, _ = ParseFieldArgs([]string{"note=a=b"})
	if got["note"] != "a=b" {
		t.Errorf("value after the first = : %#v", got)
	}

	// An empty value is how the CLI removes a field.
	got, _ = ParseFieldArgs([]string{"area="})
	if got["area"] != "" {
		t.Errorf("ParseFieldArgs(\"area=\") = %#v, want an empty value", got)
	}

	if _, err := ParseFieldArgs([]string{"area"}); err == nil {
		t.Error("ParseFieldArgs() without = returned nil error, want a usage error")
	}
	if got, err := ParseFieldArgs(nil); got != nil || err != nil {
		t.Errorf("ParseFieldArgs(nil) = %#v, %v", got, err)
	}
}

func TestFormatTaskDetailShowsFields(t *testing.T) {
	out := FormatTaskDetail(&task.Task{
		ID: "7", Title: "With fields", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature",
		Fields: task.Fields{"complexity": "high", "area": "web", "count": 3},
	}, nil)

	if !strings.Contains(out, "Fields:") {
		t.Fatalf("detail has no Fields block:\n%s", out)
	}
	// Sorted, one per line, numbers as their digits.
	for _, want := range []string{"  area: web", "  complexity: high", "  count: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail is missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "area:") > strings.Index(out, "complexity:") {
		t.Error("fields are not sorted by key")
	}

	// A task without fields has no block at all.
	plain := FormatTaskDetail(&task.Task{ID: "8", Title: "Plain", Status: task.StatusTodo}, nil)
	if strings.Contains(plain, "Fields:") {
		t.Error("a task with no fields still shows a Fields block")
	}
}

func TestCreateGetAndListWithFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MCP_TASKS_DIR", tmpDir)

	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := RunWithArgs(append([]string{"mcp-task-manager"}, args...), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}

	if _, stderr, code := run("create", "Web work", "--id", "w", "--field", "area=web", "--field", "complexity=high"); code != 0 {
		t.Fatalf("create exited %d: %s", code, stderr)
	}
	if _, stderr, code := run("create", "Cli work", "--id", "c", "--field", "area=cli"); code != 0 {
		t.Fatalf("create exited %d: %s", code, stderr)
	}

	// get shows them.
	stdout, _, code := run("get", "w")
	if code != 0 || !strings.Contains(stdout, "area: web") || !strings.Contains(stdout, "complexity: high") {
		t.Errorf("get = %q (code %d)", stdout, code)
	}

	// list filters on them.
	stdout, _, _ = run("list", "--field", "area=web")
	if !strings.Contains(stdout, "Web work") || strings.Contains(stdout, "Cli work") {
		t.Errorf("list --field area=web = %q", stdout)
	}

	// Two pairs are an AND.
	stdout, _, _ = run("list", "--field", "area=web", "--field", "complexity=low")
	if !strings.Contains(stdout, "No tasks") {
		t.Errorf("a contradictory filter = %q", stdout)
	}

	// update merges, and key= removes.
	if _, stderr, code := run("update", "w", "--field", "complexity=low", "--field", "owner=dev"); code != 0 {
		t.Fatalf("update exited %d: %s", code, stderr)
	}
	stdout, _, _ = run("get", "w")
	if !strings.Contains(stdout, "complexity: low") || !strings.Contains(stdout, "owner: dev") || !strings.Contains(stdout, "area: web") {
		t.Errorf("after an update, get = %q", stdout)
	}
	if _, stderr, code := run("update", "w", "--field", "owner="); code != 0 {
		t.Fatalf("update exited %d: %s", code, stderr)
	}
	stdout, _, _ = run("get", "w")
	if strings.Contains(stdout, "owner") {
		t.Errorf("key= did not remove the field: %q", stdout)
	}

	// A malformed --field and a reserved key are both usage errors.
	if _, stderr, code := run("list", "--field", "area"); code == 0 || !strings.Contains(stderr, "key=value") {
		t.Errorf("list --field area = code %d, stderr %q", code, stderr)
	}
	if _, stderr, code := run("update", "w", "--field", "title=x"); code == 0 || !strings.Contains(stderr, "reserved") {
		t.Errorf("update with a reserved key = code %d, stderr %q", code, stderr)
	}
}
