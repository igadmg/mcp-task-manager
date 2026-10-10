package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func callCurrentTask(t *testing.T, rs *project.Resolver) *mcp.CallToolResult {
	t.Helper()
	result, err := getCurrentTaskHandler(rs)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("getCurrentTaskHandler() error = %v", err)
	}
	return result
}

// currentTasks decodes a get_current_task JSON answer into the id of the
// most recently started task and the ids in the order they were started.
func currentTasks(t *testing.T, result *mcp.CallToolResult) (string, []string) {
	t.Helper()
	if result.IsError {
		t.Fatalf("get_current_task returned an error: %s", resultText(t, result))
	}
	var decoded struct {
		Current string `json:"current"`
		Tasks   []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(resultText(t, result)), &decoded); err != nil {
		t.Fatalf("response %q is not current-tasks JSON: %v", resultText(t, result), err)
	}
	ids := make([]string, len(decoded.Tasks))
	for i, task := range decoded.Tasks {
		ids[i] = task.ID
	}
	return decoded.Current, ids
}

func TestGetCurrentTask_None(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	result := callCurrentTask(t, rs)
	if result.IsError || resultText(t, result) != "No current task" {
		t.Errorf("get_current_task = %q (error %v), want \"No current task\"", resultText(t, result), result.IsError)
	}
}

func TestGetCurrentTask_AfterStart(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "work", Title: "Work"})
	if _, err := svc.StartTask("work"); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	current, ids := currentTasks(t, callCurrentTask(t, rs))
	if current != "work" || len(ids) != 1 || ids[0] != "work" {
		t.Errorf("get_current_task = (current %q, tasks %v), want (work, [work])", current, ids)
	}
}

func TestGetCurrentTask_Two(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "first", Title: "First"},
		testsupport.TaskSpec{ID: "second", Title: "Second"},
	)
	if _, err := svc.StartTask("first"); err != nil {
		t.Fatalf("StartTask(first) error = %v", err)
	}
	if _, err := svc.StartTask("second"); err != nil {
		t.Fatalf("StartTask(second) error = %v", err)
	}
	current, ids := currentTasks(t, callCurrentTask(t, rs))
	if current != "second" || len(ids) != 2 || ids[0] != "first" || ids[1] != "second" {
		t.Errorf("get_current_task = (current %q, tasks %v), want (second, [first second])", current, ids)
	}
}

// TestGetCurrentTask_ClosedArchivedUnknown: entries naming a closed,
// archived or unknown task are skipped, without an error.
func TestGetCurrentTask_ClosedArchivedUnknown(t *testing.T) {
	newBacklog := func(t *testing.T) (*project.Resolver, *storage.MarkdownStorage) {
		rs, _, dir := testsupport.NewBacklog(t)
		return rs, storage.NewMarkdownStorage(dir)
	}
	write := func(t *testing.T, store *storage.MarkdownStorage, ids ...string) {
		t.Helper()
		if err := store.WriteCurrentTasks("dev", ids); err != nil {
			t.Fatalf("WriteCurrentTasks() error = %v", err)
		}
	}
	expectNone := func(t *testing.T, rs *project.Resolver) {
		t.Helper()
		if result := callCurrentTask(t, rs); result.IsError || resultText(t, result) != "No current task" {
			t.Errorf("get_current_task = %q (error %v), want \"No current task\"", resultText(t, result), result.IsError)
		}
	}

	t.Run("closed", func(t *testing.T) {
		rs, svc, dir := testsupport.NewBacklog(t)
		testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "work", Title: "Work"})
		if _, err := svc.StartTask("work"); err != nil {
			t.Fatalf("StartTask() error = %v", err)
		}
		if _, err := svc.CompleteTask("work"); err != nil {
			t.Fatalf("CompleteTask() error = %v", err)
		}
		write(t, storage.NewMarkdownStorage(dir), "work")
		expectNone(t, rs)
	})

	t.Run("archived", func(t *testing.T) {
		rs, svc, dir := testsupport.NewBacklog(t)
		testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "work", Title: "Work"})
		if _, err := svc.StartTask("work"); err != nil {
			t.Fatalf("StartTask() error = %v", err)
		}
		if _, err := svc.CompleteTask("work"); err != nil {
			t.Fatalf("CompleteTask() error = %v", err)
		}
		// Completion removed the pointer; point it back by hand, then archive.
		write(t, storage.NewMarkdownStorage(dir), "work")
		if err := svc.ArchiveTask("work"); err != nil {
			t.Fatalf("ArchiveTask() error = %v", err)
		}
		expectNone(t, rs)
	})

	t.Run("unknown", func(t *testing.T) {
		rs, store := newBacklog(t)
		write(t, store, "gone")
		expectNone(t, rs)
	})

	t.Run("skipped among open", func(t *testing.T) {
		rs, svc, dir := testsupport.NewBacklog(t)
		testsupport.Seed(t, svc,
			testsupport.TaskSpec{ID: "open-one", Title: "Open one"},
			testsupport.TaskSpec{ID: "open-two", Title: "Open two"},
		)
		if _, err := svc.StartTask("open-one"); err != nil {
			t.Fatalf("StartTask(open-one) error = %v", err)
		}
		if _, err := svc.StartTask("open-two"); err != nil {
			t.Fatalf("StartTask(open-two) error = %v", err)
		}
		write(t, storage.NewMarkdownStorage(dir), "open-one", "gone", "open-two")
		current, ids := currentTasks(t, callCurrentTask(t, rs))
		if current != "open-two" || len(ids) != 2 || ids[0] != "open-one" || ids[1] != "open-two" {
			t.Errorf("get_current_task = (current %q, tasks %v), want (open-two, [open-one open-two])", current, ids)
		}
	})
}

// TestToolDescriptionsComplete builds every tool, which panics on a tool or
// parameter without descriptions/<tool>.yaml text.
func TestToolDescriptionsComplete(t *testing.T) {
	s := server.NewMCPServer("test-server", "1.0.0")
	Register(s, nil, []string{"feature", "bug"}, []string{"blocked_by"}, nil)
	if _, ok := s.ListTools()["get_current_task"]; !ok {
		t.Error("get_current_task not registered")
	}
}
