package claude

import (
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		kinds []sessionapi.EventKind
		check func(t *testing.T, evs []sessionapi.Event)
	}{
		{
			name:  "system init ignored",
			line:  `{"type":"system","subtype":"init","session_id":"s-1","cwd":"D:\\ws"}`,
			kinds: nil,
		},
		{
			name:  "assistant text and tool_use, thinking dropped",
			line:  `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi there"},{"type":"thinking","thinking":"secret"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la"}}]},"session_id":"s-1"}`,
			kinds: []sessionapi.EventKind{sessionapi.EventAssistantText, sessionapi.EventToolUse},
			check: func(t *testing.T, evs []sessionapi.Event) {
				if evs[0].Text != "Hi there" {
					t.Fatalf("text = %q", evs[0].Text)
				}
				tu := evs[1]
				if tu.ToolUseID != "toolu_1" || tu.Name != "Bash" || string(tu.Input) != `{"command":"ls -la"}` {
					t.Fatalf("tool_use = %+v", tu)
				}
			},
		},
		{
			name:  "user tool_result string content",
			line:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"file.txt","is_error":false}]},"session_id":"s-1"}`,
			kinds: []sessionapi.EventKind{sessionapi.EventToolResult},
			check: func(t *testing.T, evs []sessionapi.Event) {
				tr := evs[0]
				if tr.ToolUseID != "toolu_1" || tr.Text != "file.txt" || tr.IsError {
					t.Fatalf("tool_result = %+v", tr)
				}
			},
		},
		{
			name:  "user tool_result list content and error flag",
			line:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"boom"}],"is_error":true}]},"session_id":"s-1"}`,
			kinds: []sessionapi.EventKind{sessionapi.EventToolResult},
			check: func(t *testing.T, evs []sessionapi.Event) {
				tr := evs[0]
				if tr.Text != "boom" || !tr.IsError {
					t.Fatalf("tool_result = %+v", tr)
				}
			},
		},
		{
			name:  "tool_result truncated to 8 KiB",
			line:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_3","content":"` + strings.Repeat("x", 9<<10) + `"}]}}`,
			kinds: []sessionapi.EventKind{sessionapi.EventToolResult},
			check: func(t *testing.T, evs []sessionapi.Event) {
				if len(evs[0].Text) > toolResultMaxBytes {
					t.Fatalf("tool_result not truncated: %d bytes", len(evs[0].Text))
				}
			},
		},
		{
			name:  "permission control_request",
			line:  `{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"},"tool_use_id":"toolu_1","description":"Run ls"}}`,
			kinds: []sessionapi.EventKind{sessionapi.EventPermission},
			check: func(t *testing.T, evs []sessionapi.Event) {
				p := evs[0]
				if p.RequestID != "req-1" || p.ToolName != "Bash" || p.ToolUseID != "toolu_1" ||
					p.Description != "Run ls" || string(p.Input) != `{"command":"ls"}` {
					t.Fatalf("permission = %+v", p)
				}
			},
		},
		{
			name:  "AskUserQuestion becomes question",
			line:  `{"type":"control_request","request_id":"req-2","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","requires_user_interaction":true,"input":{"questions":[{"question":"Pick?","header":"Choice","multiSelect":true,"options":[{"label":"A","description":"first"},{"label":"B"}]}]},"tool_use_id":"toolu_2","description":"Ask"}}`,
			kinds: []sessionapi.EventKind{sessionapi.EventQuestion},
			check: func(t *testing.T, evs []sessionapi.Event) {
				q := evs[0]
				if q.RequestID != "req-2" || q.ToolUseID != "toolu_2" || len(q.Questions) != 1 {
					t.Fatalf("question = %+v", q)
				}
				q0 := q.Questions[0]
				if q0.Question != "Pick?" || q0.Header != "Choice" || !q0.MultiSelect || len(q0.Options) != 2 {
					t.Fatalf("question[0] = %+v", q0)
				}
				if q0.Options[0].Label != "A" || q0.Options[0].Description != "first" || q0.Options[1].Label != "B" {
					t.Fatalf("options = %+v", q0.Options)
				}
			},
		},
		{
			name:  "result becomes turn_result",
			line:  `{"type":"result","subtype":"success","session_id":"s-1","total_cost_usd":0.0123,"duration_ms":1500}`,
			kinds: []sessionapi.EventKind{sessionapi.EventTurnResult},
			check: func(t *testing.T, evs []sessionapi.Event) {
				r := evs[0]
				if !r.OK || r.CostUSD == nil || *r.CostUSD != 0.0123 || r.DurationMs == nil || *r.DurationMs != 1500 {
					t.Fatalf("turn_result = %+v", r)
				}
			},
		},
		{
			name:  "rate_limit_event ignored",
			line:  `{"type":"rate_limit_event","message":"slow down"}`,
			kinds: nil,
		},
		{
			name:  "control_response ignored",
			line:  `{"type":"control_response","response":{"subtype":"success","request_id":"req-1","response":{"behavior":"allow"}}}`,
			kinds: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evs, err := parseLine([]byte(tc.line))
			if err != nil {
				t.Fatalf("parseLine: %v", err)
			}
			if len(evs) != len(tc.kinds) {
				t.Fatalf("got %d events %+v, want kinds %v", len(evs), evs, tc.kinds)
			}
			for i, k := range tc.kinds {
				if evs[i].Kind != k {
					t.Fatalf("event %d kind = %q, want %q", i, evs[i].Kind, k)
				}
			}
			if tc.check != nil {
				tc.check(t, evs)
			}
		})
	}
}

func TestParseLineInvalidJSON(t *testing.T) {
	if _, err := parseLine([]byte(`{"type":"assistant","broken`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
