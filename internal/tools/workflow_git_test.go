package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func callTool(t *testing.T, h server.ToolHandlerFunc, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	return result
}

func TestStartCompleteHandlersWithGit(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoIgnored)
	if _, err := b.Svc.Create("Add login", "", "high", "feature", "", "login"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	started := callTool(t, startTaskHandler(b.Resolver), map[string]any{"id": "login"})
	var st struct {
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal([]byte(resultText(t, started)), &st); err != nil || st.Branch != "dev/wip/login" {
		t.Fatalf("start_task = %s, want branch dev/wip/login", resultText(t, started))
	}

	testsupport.WriteFile(t, filepath.Join(b.CodeDir, "login.go"), "package login\n")
	done := callTool(t, completeTaskHandler(b.Resolver), map[string]any{"id": "login", "commit_message": "feat: login form"})
	var dn struct {
		Branch      string `json:"branch"`
		FinalBranch string `json:"final_branch"`
	}
	if err := json.Unmarshal([]byte(resultText(t, done)), &dn); err != nil || dn.FinalBranch != "dev/login" || dn.Branch != "dev/wip/login" {
		t.Fatalf("complete_task = %s, want branch and final_branch", resultText(t, done))
	}
	if got := b.Git(t, "log", "-1", "--format=%B", "dev/login"); got != "feat: login form" {
		t.Errorf("final commit message = %q", got)
	}
}

func TestUpdateTaskRefusedUnderBranching(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksUntracked)
	if _, err := b.Svc.Create("Work", "", "high", "feature", "", "work"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	result := callTool(t, updateTaskHandler(b.Resolver), map[string]any{"id": "work", "status": "in_progress"})
	if !result.IsError || !strings.Contains(resultText(t, result), "git branching is enabled; use start_task") {
		t.Errorf("update_task = %q (error %v), want the start_task refusal", resultText(t, result), result.IsError)
	}
}

func TestCompleteTaskCommitMessageParamDocumented(t *testing.T) {
	s := server.NewMCPServer("test-server", "1.0.0")
	Register(s, nil, []string{"feature", "bug"}, []string{"blocked_by"}, nil)
	props := s.ListTools()["complete_task"].Tool.InputSchema.Properties
	prop, ok := props["commit_message"].(map[string]any)
	if !ok {
		t.Fatalf("complete_task has no commit_message property: %v", props)
	}
	if desc, _ := prop["description"].(string); !strings.Contains(desc, "verbatim") {
		t.Errorf("commit_message description = %q", desc)
	}
}

func TestStartPhaseImplementationHandlerWithGit(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoIgnored)
	if _, err := b.Svc.Create("Add login", "", "high", "feature", "", "login"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, p := range []string{"research", "design", "planning"} {
		callTool(t, startPhaseHandler(b.Resolver), map[string]any{"id": "login", "phase": p})
		if got := b.Head(t); got != "main_patched" {
			t.Fatalf("%s moved HEAD to %s", p, got)
		}
		callTool(t, finishPhaseHandler(b.Resolver), map[string]any{"phase": p, "tokens": 10.0})
	}

	got := decodePhaseResult(t, callTool(t, startPhaseHandler(b.Resolver), map[string]any{"phase": "implementation"}))
	if got.Task.Branch != "dev/wip/login" || got.Phase != "implementation" || got.Run != 1 {
		t.Errorf("start_phase implementation = %+v", got)
	}
	if head := b.Head(t); head != "dev/wip/login" {
		t.Errorf("HEAD = %q, want the wip branch", head)
	}
}
