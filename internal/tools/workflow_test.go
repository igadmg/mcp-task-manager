package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/mark3labs/mcp-go/mcp"
)

// decodePhaseResult parses a start_phase / finish_phase result.
func decodePhaseResult(t *testing.T, result *mcp.CallToolResult) phaseResponse {
	t.Helper()
	if result.IsError {
		t.Fatalf("phase tool returned an error: %s", resultText(t, result))
	}
	var got phaseResponse
	if err := json.Unmarshal([]byte(resultText(t, result)), &got); err != nil {
		t.Fatalf("response %q is not a phase result: %v", resultText(t, result), err)
	}
	return got
}

func wantToolError(t *testing.T, result *mcp.CallToolResult, substr string) {
	t.Helper()
	if !result.IsError || !strings.Contains(resultText(t, result), substr) {
		t.Errorf("result = %q (error %v), want a tool error containing %q", resultText(t, result), result.IsError, substr)
	}
}

func newPhaseBacklog(t *testing.T) *project.Resolver {
	t.Helper()
	rs, svc, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "work", Title: "Work"})
	return rs
}

func TestStartPhaseHandlerResult(t *testing.T) {
	rs := newPhaseBacklog(t)
	got := decodePhaseResult(t, callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "research"}))
	if got.Task == nil || got.Task.ID != "work" || got.Task.Status != "in_progress" {
		t.Errorf("task = %+v, want work in progress", got.Task)
	}
	if got.Phase != "research" || got.Run != 1 || got.Record == nil || len(got.Record.Runs) != 1 || got.Record.Runs[0].StartedBy != "dev" {
		t.Errorf("phase result = %+v", got)
	}

	finished := decodePhaseResult(t, callTool(t, finishPhaseHandler(rs), map[string]any{"phase": "research"}))
	if finished.Run != 1 || finished.Record.Runs[0].Open() {
		t.Errorf("finish result = %+v", finished)
	}
	rerun := decodePhaseResult(t, callTool(t, startPhaseHandler(rs), map[string]any{"phase": "research"}))
	if rerun.Run != 2 || len(rerun.Record.Runs) != 2 {
		t.Errorf("re-run result = %+v, want run 2", rerun)
	}
}

func TestStartPhaseHandlerUnknownPhase(t *testing.T) {
	rs := newPhaseBacklog(t)
	result := callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "testing"})
	wantToolError(t, result, `unknown phase "testing"`)
	result = callTool(t, finishPhaseHandler(rs), map[string]any{"id": "work", "phase": "plan"})
	wantToolError(t, result, `unknown phase "plan"`)
}

func TestFinishPhaseHandlerTokens(t *testing.T) {
	for _, bad := range []any{1.5, -3.0, "12", -1} {
		rs := newPhaseBacklog(t)
		callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "research"})
		result := callTool(t, finishPhaseHandler(rs), map[string]any{"id": "work", "phase": "research", "tokens": bad})
		wantToolError(t, result, "tokens must be a non-negative integer")
	}

	rs := newPhaseBacklog(t)
	callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "research"})
	got := decodePhaseResult(t, callTool(t, finishPhaseHandler(rs), map[string]any{"id": "work", "phase": "research", "tokens": 0.0, "note": "quick"}))
	if run := got.Record.Runs[0]; run.Tokens == nil || *run.Tokens != 0 || run.Note != "quick" {
		t.Errorf("tokens 0 = %+v, want a recorded 0", run)
	}

	callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "design"})
	got = decodePhaseResult(t, callTool(t, finishPhaseHandler(rs), map[string]any{"id": "work", "phase": "design"}))
	if run := got.Record.Runs[0]; run.Tokens != nil {
		t.Errorf("absent tokens = %d, want none recorded", *run.Tokens)
	}

	callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "planning"})
	got = decodePhaseResult(t, callTool(t, finishPhaseHandler(rs), map[string]any{"id": "work", "phase": "planning", "tokens": 81234.0}))
	if run := got.Record.Runs[0]; run.Tokens == nil || *run.Tokens != 81234 {
		t.Errorf("tokens = %v, want 81234", run.Tokens)
	}
	if !strings.Contains(resultText(t, callTool(t, finishPhaseHandler(rs), map[string]any{"id": "work", "phase": "planning"})), "is not open") {
		t.Error("finishing a finished run again did not fail")
	}
}

func TestPhaseHandlersDefaultToCurrentTask(t *testing.T) {
	rs := newPhaseBacklog(t)
	wantToolError(t, callTool(t, startPhaseHandler(rs), map[string]any{"phase": "research"}),
		"no task id given and no current task; pass id")
	wantToolError(t, callTool(t, finishPhaseHandler(rs), map[string]any{"phase": "research"}),
		"no task id given and no current task; pass id")

	callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "research"})
	got := decodePhaseResult(t, callTool(t, finishPhaseHandler(rs), map[string]any{"phase": "research"}))
	if got.Task.ID != "work" {
		t.Errorf("finish_phase without id finished %q, want the current task", got.Task.ID)
	}
}

func TestGetTaskIncludesPhases(t *testing.T) {
	rs := newPhaseBacklog(t)
	callTool(t, startPhaseHandler(rs), map[string]any{"id": "work", "phase": "research"})
	var got struct {
		CreatedBy string `json:"created_by"`
		Phases    []struct {
			Phase string `json:"phase"`
			Runs  []struct {
				StartedBy string `json:"started_by"`
			} `json:"runs"`
		} `json:"phases"`
	}
	text := resultText(t, callTool(t, getTaskHandler(rs), map[string]any{"id": "work"}))
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("get_task response %q: %v", text, err)
	}
	if got.CreatedBy != "dev" || len(got.Phases) != 1 || got.Phases[0].Phase != "research" ||
		len(got.Phases[0].Runs) != 1 || got.Phases[0].Runs[0].StartedBy != "dev" {
		t.Errorf("get_task = %s", text)
	}
}

func TestPhaseToolsRegistered(t *testing.T) {
	names := toolNames(Build(nil, []string{"feature"}, []string{"blocked_by"}, nil))
	for _, name := range []string{"start_phase", "finish_phase"} {
		if !names[name] {
			t.Errorf("tool %s is not registered", name)
		}
	}
}
