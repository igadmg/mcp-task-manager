package task_test

import (
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// TestFieldFaultInjection runs the branch flows against a task that carries
// free-form fields, failing every mutating git call in turn. CaptureState
// compares records semantically, so a rollback that lost or mangled a field
// fails here.
//
// It does not prove captureTask's clone is needed: no flow edits a fields map
// in place today, so the test passes without it. The clone is there for the
// first flow that does, and this test is what would then notice.
func TestFieldFaultInjection(t *testing.T) {
	withFields := func(t *testing.T, b *testsupport.GitBacklog, id string) {
		t.Helper()
		if _, err := b.Svc.Create("Task "+id, "", task.PriorityHigh, "feature", "", id,
			task.WithCreateFields(map[string]any{"area": "web", "complexity": "high", "count": 3})); err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
	}

	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("start", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				withFields(t, b, "alpha")
				withFields(t, b, "beta")
				mustStart(t, b, "alpha")
				writeCode(t, b, "alpha.txt", "alpha work\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.StartTask("beta")
				return err
			})
		})

		t.Run("complete", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				withFields(t, b, "alpha")
				mustStart(t, b, "alpha")
				writeCode(t, b, "alpha.txt", "alpha work\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.CompleteTask("alpha")
				return err
			})
		})
	})
}

// TestFieldsSurviveTheWholeGitLifecycle walks one task through start,
// complete and a reopen-and-restart, checking the fields after each step.
// The branch fields the flows stamp and the free-form ones share a
// frontmatter, and neither may displace the other.
func TestFieldsSurviveTheWholeGitLifecycle(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoIgnored)
	if _, err := b.Svc.Create("Add login", "", task.PriorityHigh, "feature", "", "login",
		task.WithCreateFields(map[string]any{"area": "web", "complexity": "high", "count": 3})); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	check := func(stage string) *task.Task {
		t.Helper()
		got, err := b.Svc.Get("login")
		if err != nil {
			t.Fatalf("Get() after %s: %v", stage, err)
		}
		for key, want := range map[string]string{"area": "web", "complexity": "high", "count": "3"} {
			if got.Fields.String(key) != want {
				t.Errorf("after %s, %s = %q, want %q", stage, key, got.Fields.String(key), want)
			}
		}
		if len(got.Fields) != 3 {
			t.Errorf("after %s, fields = %#v, want exactly three", stage, got.Fields)
		}
		return got
	}

	mustStart(t, b, "login")
	check("start")

	writeCode(t, b, "login.txt", "work\n")
	if _, err := b.Svc.CompleteTask("login"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	delivered := check("complete")
	if delivered.FinalBranch == "" || delivered.SquashCommit == "" {
		t.Errorf("the branch fields are missing: %+v", delivered)
	}

	// A reopen keeps both sets, and a restart rewrites only the branch ones.
	todo := task.StatusTodo
	if _, err := b.Svc.Update("login", nil, nil, &todo, nil, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	check("reopen")
	mustStart(t, b, "login")
	check("restart")
}
