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

func currentTaskID(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result.IsError {
		t.Fatalf("get_current_task returned an error: %s", resultText(t, result))
	}
	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(resultText(t, result)), &decoded); err != nil {
		t.Fatalf("response %q is not task JSON: %v", resultText(t, result), err)
	}
	return decoded.ID
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
	if id := currentTaskID(t, callCurrentTask(t, rs)); id != "work" {
		t.Errorf("current task id = %q, want work", id)
	}
}

func TestGetCurrentTask_Archived(t *testing.T) {
	rs, svc, dir := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "work", Title: "Work"})
	if _, err := svc.StartTask("work"); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if _, err := svc.CompleteTask("work"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	// Completion removed the pointer; point it back by hand, then archive.
	if err := storage.NewMarkdownStorage(dir).WriteCurrentTask("dev", "work"); err != nil {
		t.Fatalf("WriteCurrentTask() error = %v", err)
	}
	if err := svc.ArchiveTask("work"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	if id := currentTaskID(t, callCurrentTask(t, rs)); id != "work" {
		t.Errorf("current task id = %q, want the archived task", id)
	}
}

func TestGetCurrentTask_UnknownID(t *testing.T) {
	rs, _, dir := testsupport.NewBacklog(t)
	if err := storage.NewMarkdownStorage(dir).WriteCurrentTask("dev", "gone"); err != nil {
		t.Fatalf("WriteCurrentTask() error = %v", err)
	}
	if result := callCurrentTask(t, rs); !result.IsError {
		t.Errorf("get_current_task with an unknown id = %q, want a tool error", resultText(t, result))
	}
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
