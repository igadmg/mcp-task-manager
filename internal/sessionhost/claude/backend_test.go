package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// TestFakeClaudeMain is the re-exec entry point: when FAKE_CLAUDE_SCENARIO is
// set, the test binary acts as a fake claude process speaking stream-json.
func TestFakeClaudeMain(t *testing.T) {
	scenario := os.Getenv("FAKE_CLAUDE_SCENARIO")
	if scenario == "" {
		return
	}
	runFakeClaude(scenario)
	os.Exit(0)
}

func fakeSay(w *bufio.Writer, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintln(w, string(b))
	w.Flush()
}

func fakeRead(stdin *bufio.Reader) map[string]any {
	line, err := stdin.ReadBytes('\n')
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(line, &m)
	return m
}

func fakeWaitForResponse(stdin *bufio.Reader, requestID string) map[string]any {
	for {
		m := fakeRead(stdin)
		if m == nil {
			return nil
		}
		if m["type"] == "control_response" {
			resp, _ := m["response"].(map[string]any)
			if resp != nil && resp["request_id"] == requestID {
				return m
			}
		}
	}
}

func runFakeClaude(scenario string) {
	stdin := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	systemInit := map[string]any{"type": "system", "subtype": "init", "session_id": "fake-session"}
	userToolResult := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "file.txt", "is_error": false},
	}}}
	resultEvent := map[string]any{"type": "result", "subtype": "success", "session_id": "fake-session",
		"total_cost_usd": 0.01, "duration_ms": 1500}

	switch scenario {
	case "turn":
		fakeSay(out, systemInit)
		if m := fakeRead(stdin); m == nil {
			return
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "Hello!"},
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]any{"command": "ls"}},
		}}})
		fakeSay(out, map[string]any{"type": "control_request", "request_id": "req-1", "request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "ls"},
			"tool_use_id": "toolu_1", "description": "Run ls"}})
		if fakeWaitForResponse(stdin, "req-1") == nil {
			return
		}
		fakeSay(out, userToolResult)
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "Done."},
		}}})
		fakeSay(out, resultEvent)

	case "interactive":
		fakeSay(out, systemInit)
		n := 0
		for {
			m := fakeRead(stdin)
			if m == nil {
				return
			}
			if m["type"] != "user" {
				continue
			}
			n++
			fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": fmt.Sprintf("Reply %d", n)},
			}}})
			fakeSay(out, resultEvent)
		}

	case "ask":
		fakeSay(out, systemInit)
		if m := fakeRead(stdin); m == nil {
			return
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_2", "name": "AskUserQuestion", "input": map[string]any{}},
		}}})
		fakeSay(out, map[string]any{"type": "control_request", "request_id": "req-q", "request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "requires_user_interaction": true,
			"tool_use_id": "toolu_2", "description": "Ask",
			"input": map[string]any{"questions": []any{
				map[string]any{"question": "Pick one?", "header": "Choice", "multiSelect": false,
					"options": []any{map[string]any{"label": "A", "description": "first"}, map[string]any{"label": "B"}}},
				map[string]any{"question": "Many?", "multiSelect": true,
					"options": []any{map[string]any{"label": "X"}, map[string]any{"label": "Y"}}},
			}}}})
		resp := fakeWaitForResponse(stdin, "req-q")
		if resp == nil {
			return
		}
		// Echo the answers received so the test can verify updatedInput.
		answers := ""
		if r, _ := resp["response"].(map[string]any); r != nil {
			if inner, _ := r["response"].(map[string]any); inner != nil {
				if ui, _ := inner["updatedInput"].(map[string]any); ui != nil {
					if a, _ := ui["answers"].(map[string]any); a != nil {
						b, _ := json.Marshal(a)
						answers = string(b)
					}
				}
			}
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "answers=" + answers},
		}}})
		fakeSay(out, resultEvent)

	case "askstay":
		// Like "ask", but the process stays alive between turns (as the
		// real interactive claude does), so the session must settle in
		// idle, not finished.
		fakeSay(out, systemInit)
		if m := fakeRead(stdin); m == nil {
			return
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_2", "name": "AskUserQuestion", "input": map[string]any{}},
		}}})
		fakeSay(out, map[string]any{"type": "control_request", "request_id": "req-q", "request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "requires_user_interaction": true,
			"tool_use_id": "toolu_2", "description": "Ask",
			"input": map[string]any{"questions": []any{
				map[string]any{"question": "Pick one?", "header": "Choice", "multiSelect": false,
					"options": []any{map[string]any{"label": "A", "description": "first"}, map[string]any{"label": "B"}}},
			}}}})
		if fakeWaitForResponse(stdin, "req-q") == nil {
			return
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "answered"},
		}}})
		fakeSay(out, resultEvent)
		for {
			m := fakeRead(stdin)
			if m == nil {
				return
			}
			if m["type"] != "user" {
				continue
			}
			fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "follow-up reply"},
			}}})
			fakeSay(out, resultEvent)
		}

	case "deny":
		fakeSay(out, systemInit)
		if m := fakeRead(stdin); m == nil {
			return
		}
		fakeSay(out, map[string]any{"type": "control_request", "request_id": "req-d", "request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "rm"},
			"tool_use_id": "toolu_9", "description": "Run rm"}})
		resp := fakeWaitForResponse(stdin, "req-d")
		behavior, message := "", ""
		if r, _ := resp["response"].(map[string]any); r != nil {
			if inner, _ := r["response"].(map[string]any); inner != nil {
				behavior, _ = inner["behavior"].(string)
				message, _ = inner["message"].(string)
			}
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": fmt.Sprintf("behavior=%s message=%s", behavior, message)},
		}}})
		fakeSay(out, resultEvent)

	case "fail":
		if m := fakeRead(stdin); m == nil {
			return
		}
		fmt.Fprintln(os.Stderr, "fake claude: boom")
		os.Exit(1)

	case "hang":
		// Never reads stdin: Stop must kill the whole process tree.
		if m := fakeRead(stdin); m == nil {
			return
		}
		select {}

	case "quiet":
		// Silences until stdin closes, then exits cleanly.
		_, _ = io.Copy(io.Discard, os.Stdin)

	case "bigline":
		if m := fakeRead(stdin); m == nil {
			return
		}
		fmt.Fprint(os.Stdout, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"`+
			strings.Repeat("a", 9<<20)+`"}]}}`+"\n")
		_, _ = io.Copy(io.Discard, os.Stdin)

	case "bignoline":
		if m := fakeRead(stdin); m == nil {
			return
		}
		// 9 MiB without a trailing newline: the reader must error out on
		// the size limit instead of waiting for the line to end.
		fmt.Fprint(os.Stdout, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"`+
			strings.Repeat("a", 9<<20))
		_, _ = io.Copy(io.Discard, os.Stdin)

	case "env":
		fakeSay(out, systemInit)
		if m := fakeRead(stdin); m == nil {
			return
		}
		fakeSay(out, map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "task_id=" + os.Getenv("MCP_TASK_ID")},
		}}})
		fakeSay(out, resultEvent)
	}
}

// fakeBackend re-execs the test binary as the claude process.
func fakeBackend(t *testing.T, scenario string) *Backend {
	t.Helper()
	b := New("fake-claude")
	b.command = func(ctx context.Context, path string, args []string) *exec.Cmd {
		full := append([]string{"-test.run=TestFakeClaudeMain", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], full...)
		cmd.Env = append(os.Environ(), "FAKE_CLAUDE_SCENARIO="+scenario)
		return cmd
	}
	return b
}

func fakeSpec(t *testing.T, prompt string) sessionapi.Spec {
	t.Helper()
	return sessionapi.Spec{
		Version:   sessionapi.SpecVersion,
		Provider:  sessionapi.ProviderClaude,
		Workspace: sessionapi.Workspace{Kind: sessionapi.WorkspaceKindWorkingDir, Path: t.TempDir()},
		Prompt:    prompt,
	}
}

func nextEvent(t *testing.T, p *proc, timeout time.Duration) sessionapi.Event {
	t.Helper()
	select {
	case ev, ok := <-p.Events():
		if !ok {
			t.Fatal("events channel closed unexpectedly")
		}
		return ev
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
		return sessionapi.Event{}
	}
}

func nextEventKind(t *testing.T, p *proc, kind sessionapi.EventKind, timeout time.Duration) sessionapi.Event {
	t.Helper()
	ev := nextEvent(t, p, timeout)
	if ev.Kind != kind {
		t.Fatalf("event kind = %q, want %q (event %+v)", ev.Kind, kind, ev)
	}
	return ev
}

func startProc(t *testing.T, scenario, prompt string, taskID string) (*Backend, *proc) {
	t.Helper()
	b := fakeBackend(t, scenario)
	spec := fakeSpec(t, prompt)
	spec.TaskID = taskID
	pr, err := b.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return b, pr.(*proc)
}

func TestBackendTurnLifecycle(t *testing.T) {
	_, p := startProc(t, "turn", "list files", "")
	defer p.Stop(context.Background())

	ev := nextEventKind(t, p, sessionapi.EventAssistantText, 10*time.Second)
	if ev.Text != "Hello!" {
		t.Fatalf("text = %q", ev.Text)
	}
	ev = nextEventKind(t, p, sessionapi.EventToolUse, 5*time.Second)
	if ev.Name != "Bash" || ev.ToolUseID != "toolu_1" {
		t.Fatalf("tool_use = %+v", ev)
	}
	ev = nextEventKind(t, p, sessionapi.EventPermission, 5*time.Second)
	if ev.RequestID != "req-1" || ev.ToolName != "Bash" || ev.Description != "Run ls" {
		t.Fatalf("permission = %+v", ev)
	}
	if err := p.Answer(sessionapi.Answer{RequestID: "req-1", Behavior: sessionapi.BehaviorAllow}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	ev = nextEventKind(t, p, sessionapi.EventRequestResolved, 5*time.Second)
	if ev.RequestID != "req-1" || ev.Outcome != "answered" {
		t.Fatalf("request_resolved = %+v", ev)
	}
	if err := p.Answer(sessionapi.Answer{RequestID: "req-1", Behavior: sessionapi.BehaviorAllow}); err == nil {
		t.Fatal("repeat answer accepted after resolution")
	}
	ev = nextEventKind(t, p, sessionapi.EventToolResult, 5*time.Second)
	if ev.Text != "file.txt" || ev.IsError {
		t.Fatalf("tool_result = %+v", ev)
	}
	ev = nextEventKind(t, p, sessionapi.EventAssistantText, 5*time.Second)
	if ev.Text != "Done." {
		t.Fatalf("text = %q", ev.Text)
	}
	ev = nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
	if !ev.OK || ev.CostUSD == nil || *ev.CostUSD != 0.01 || ev.DurationMs == nil || *ev.DurationMs != 1500 {
		t.Fatalf("turn_result = %+v", ev)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestBackendQuestionAnswer(t *testing.T) {
	_, p := startProc(t, "ask", "ask me", "")
	defer p.Stop(context.Background())

	nextEventKind(t, p, sessionapi.EventToolUse, 10*time.Second)
	ev := nextEventKind(t, p, sessionapi.EventQuestion, 5*time.Second)
	if ev.RequestID != "req-q" || len(ev.Questions) != 2 {
		t.Fatalf("question = %+v", ev)
	}
	if ev.Questions[0].Question != "Pick one?" || ev.Questions[1].Question != "Many?" {
		t.Fatalf("questions = %+v", ev.Questions)
	}
	err := p.Answer(sessionapi.Answer{
		RequestID: "req-q",
		Behavior:  sessionapi.BehaviorAllow,
		Answers:   map[string]string{"Pick one?": "B", "Many?": "X, Y"},
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	ev = nextEventKind(t, p, sessionapi.EventRequestResolved, 5*time.Second)
	if ev.RequestID != "req-q" || ev.Outcome != "answered" {
		t.Fatalf("request_resolved = %+v", ev)
	}
	ev = nextEventKind(t, p, sessionapi.EventAssistantText, 5*time.Second)
	// The fake echoes the answers map it received in updatedInput.
	want := `{"Many?":"X, Y","Pick one?":"B"}`
	if !strings.Contains(ev.Text, want) {
		t.Fatalf("fake did not receive merged answers: %q", ev.Text)
	}
	nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestBackendPermissionDeny(t *testing.T) {
	_, p := startProc(t, "deny", "remove", "")
	defer p.Stop(context.Background())

	ev := nextEventKind(t, p, sessionapi.EventPermission, 10*time.Second)
	if ev.RequestID != "req-d" {
		t.Fatalf("permission = %+v", ev)
	}
	if err := p.Answer(sessionapi.Answer{RequestID: "req-d", Behavior: sessionapi.BehaviorDeny, Message: "not allowed"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	ev = nextEventKind(t, p, sessionapi.EventRequestResolved, 5*time.Second)
	if ev.RequestID != "req-d" || ev.Outcome != "denied" {
		t.Fatalf("request_resolved = %+v", ev)
	}
	ev = nextEventKind(t, p, sessionapi.EventAssistantText, 5*time.Second)
	if !strings.Contains(ev.Text, "behavior=deny") || !strings.Contains(ev.Text, "message=not allowed") {
		t.Fatalf("deny not delivered: %q", ev.Text)
	}
	nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
}

func TestBackendSendFollowUp(t *testing.T) {
	_, p := startProc(t, "interactive", "first", "")
	defer p.Stop(context.Background())

	nextEventKind(t, p, sessionapi.EventAssistantText, 10*time.Second) // "Reply 1"
	nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
	if err := p.Send("second"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	ev := nextEventKind(t, p, sessionapi.EventAssistantText, 5*time.Second)
	if ev.Text != "Reply 2" {
		t.Fatalf("text = %q", ev.Text)
	}
	nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
}

func TestBackendProcessFailureIncludesStderr(t *testing.T) {
	_, p := startProc(t, "fail", "go", "")
	err := p.Wait()
	if err == nil {
		t.Fatal("Wait should report the failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("stderr tail missing from Wait error: %v", err)
	}
}

func TestBackendStopKillsHungProcess(t *testing.T) {
	_, p := startProc(t, "hang", "wait", "")
	time.Sleep(200 * time.Millisecond)
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Wait(); err == nil {
		t.Fatal("Wait should report the killed process")
	}
}

func TestBackendOversizeLine(t *testing.T) {
	_, p := startProc(t, "bigline", "big", "")
	defer p.Stop(context.Background())
	ev := nextEvent(t, p, 30*time.Second)
	if ev.Kind != sessionapi.EventError {
		t.Fatalf("event = %+v, want error", ev)
	}
}

func TestBackendOversizeLineWithoutNewline(t *testing.T) {
	_, p := startProc(t, "bignoline", "big", "")
	defer p.Stop(context.Background())
	ev := nextEvent(t, p, 10*time.Second)
	if ev.Kind != sessionapi.EventError || ev.Code != "line_too_large" {
		t.Fatalf("event = %+v, want line_too_large error", ev)
	}
	if err := p.Wait(); err == nil {
		t.Fatal("Wait should report the killed process")
	}
}

// closeTrackingWriter records Close so tests can assert the raw log is
// released when start fails halfway.
type closeTrackingWriter struct{ closed chan struct{} }

func (w *closeTrackingWriter) Write(b []byte) (int, error) { return len(b), nil }

func (w *closeTrackingWriter) Close() error { close(w.closed); return nil }

func TestBackendStartSendFailureCleansUp(t *testing.T) {
	b := New("fake-claude")
	b.command = func(ctx context.Context, path string, args []string) *exec.Cmd {
		full := append([]string{"-test.run=TestFakeClaudeMain", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], full...)
		cmd.Env = append(os.Environ(), "FAKE_CLAUDE_SCENARIO=quiet")
		return cmd
	}
	b.sendPrompt = func(p *proc, prompt string) error { return errors.New("stdin write failed") }
	var cmd *exec.Cmd
	orig := b.command
	b.command = func(ctx context.Context, path string, args []string) *exec.Cmd {
		cmd = orig(ctx, path, args)
		return cmd
	}

	raw := &closeTrackingWriter{closed: make(chan struct{})}
	start := time.Now()
	_, err := b.StartRaw(context.Background(), fakeSpec(t, "hi"), raw)
	if err == nil {
		t.Fatal("expected start error for failed prompt delivery")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cleanup took %s: the half-started process was not stopped promptly", elapsed)
	}
	select {
	case <-raw.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("raw log not closed after failed start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for cmd.ProcessState == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if cmd.ProcessState == nil {
		t.Fatal("half-started process was not reaped")
	}
}

func TestBackendConcurrentAnswersResolveOnce(t *testing.T) {
	_, p := startProc(t, "ask", "ask me", "")
	defer p.Stop(context.Background())

	nextEventKind(t, p, sessionapi.EventToolUse, 10*time.Second)
	question := nextEventKind(t, p, sessionapi.EventQuestion, 5*time.Second)

	const senders = 8
	results := make(chan error, senders)
	for i := 0; i < senders; i++ {
		go func() {
			results <- p.Answer(sessionapi.Answer{
				RequestID: question.RequestID,
				Behavior:  sessionapi.BehaviorAllow,
				Answers:   map[string]string{"Pick one?": "A", "Many?": "X"},
			})
		}()
	}
	accepted := 0
	for i := 0; i < senders; i++ {
		if err := <-results; err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted answers = %d, want exactly 1", accepted)
	}
	if err := p.Answer(sessionapi.Answer{RequestID: question.RequestID, Behavior: sessionapi.BehaviorAllow}); err == nil {
		t.Fatal("repeat answer accepted after resolution")
	}

	resolved := 0
	for {
		ev := nextEvent(t, p, 5*time.Second)
		if ev.Kind == sessionapi.EventRequestResolved {
			resolved++
			if ev.RequestID != question.RequestID || ev.Outcome != "answered" {
				t.Fatalf("resolved = %+v", ev)
			}
		}
		if ev.Kind == sessionapi.EventTurnResult {
			break
		}
	}
	if resolved != 1 {
		t.Fatalf("request_resolved events = %d, want 1", resolved)
	}
}

func TestBackendStartNotFound(t *testing.T) {
	b := New(filepath.Join(t.TempDir(), "no-such-claude"))
	_, err := b.Start(context.Background(), fakeSpec(t, "hi"))
	if err == nil {
		t.Fatal("expected start error for missing binary")
	}
}

func TestBackendTaskIDEnv(t *testing.T) {
	_, p := startProc(t, "env", "show env", "my-task-1")
	defer p.Stop(context.Background())
	ev := nextEventKind(t, p, sessionapi.EventAssistantText, 10*time.Second)
	if ev.Text != "task_id=my-task-1" {
		t.Fatalf("MCP_TASK_ID not passed: %q", ev.Text)
	}
}

func TestBackendRawLog(t *testing.T) {
	rawPath := filepath.Join(t.TempDir(), "raw.jsonl")
	b := fakeBackend(t, "turn")
	spec := fakeSpec(t, "list")
	raw, err := os.Create(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	pi, err := b.StartRaw(context.Background(), spec, raw)
	if err != nil {
		t.Fatalf("StartRaw: %v", err)
	}
	p := pi.(*proc)
	defer p.Stop(context.Background())
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				t.Fatal("stream ended before turn_result")
			}
			if ev.Kind == sessionapi.EventPermission {
				_ = p.Answer(sessionapi.Answer{RequestID: ev.RequestID, Behavior: sessionapi.BehaviorAllow})
			}
			if ev.Kind == sessionapi.EventTurnResult {
				goto drained
			}
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for turn_result")
		}
	}
drained:
	p.Wait()
	data, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"type":"result"`) || !strings.Contains(s, `"type":"control_request"`) {
		t.Fatalf("raw.jsonl incomplete:\n%s", s)
	}
}

func TestBuildArgvWrapsCmdShims(t *testing.T) {
	cases := []struct {
		path string
		want string // argv0
	}{
		{path: "/usr/bin/claude", want: "/usr/bin/claude"},
		{path: `C:\nvm4w\nodejs\claude.cmd`, want: "cmd"},
		{path: `C:\tools\claude.bat`, want: "cmd"},
	}
	for _, tc := range cases {
		argv0, args := buildArgv(tc.path, []string{"-p"})
		if argv0 != tc.want {
			t.Fatalf("buildArgv(%q) argv0 = %q, want %q", tc.path, argv0, tc.want)
		}
		if tc.want == "cmd" && (len(args) != 3 || args[0] != "/c" || args[1] != tc.path || args[2] != "-p") {
			t.Fatalf("buildArgv(%q) args = %v", tc.path, args)
		}
	}
}

func TestBackendConcurrency(t *testing.T) {
	// -count=5 catches races in stdin/answer handling without the race detector.
	_, p := startProc(t, "interactive", "go", "")
	defer p.Stop(context.Background())
	nextEventKind(t, p, sessionapi.EventAssistantText, 10*time.Second)
	nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
	done := make(chan error, 2)
	go func() { done <- p.Send("a") }()
	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- p.Send("b")
	}()
	if err := <-done; err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Send: %v", err)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		ev := nextEventKind(t, p, sessionapi.EventAssistantText, 5*time.Second)
		seen[ev.Text] = true
		nextEventKind(t, p, sessionapi.EventTurnResult, 5*time.Second)
	}
	if !seen["Reply 2"] || !seen["Reply 3"] {
		t.Fatalf("replies = %v", seen)
	}
}

// failWriter makes every stdin write fail; Close is a no-op.
type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }
func (w failWriter) Close() error              { return nil }

// recordingWriter captures stdin writes; Close is a no-op.
type recordingWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(b)
}

func (w *recordingWriter) Close() error { return nil }

func (w *recordingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestBackendAnswerAfterProcessExit(t *testing.T) {
	_, p := startProc(t, "ask", "ask me", "")
	nextEventKind(t, p, sessionapi.EventToolUse, 10*time.Second)
	question := nextEventKind(t, p, sessionapi.EventQuestion, 5*time.Second)

	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	// Late answers (the UI clicking after the process died) must fail
	// cleanly, stay retriable, and never panic on the closed events
	// channel (review findings 1 and 3).
	for i := 0; i < 3; i++ {
		err := p.Answer(sessionapi.Answer{RequestID: question.RequestID, Behavior: sessionapi.BehaviorAllow})
		if err == nil {
			t.Fatal("late answer accepted after process exit")
		}
		var se *sessionapi.Error
		if errors.As(err, &se) && se.Code == sessionapi.ErrCodeNotFound {
			t.Fatalf("late answer %d lost the pending request: %v", i, err)
		}
	}

	// A resolved event racing process exit is dropped, not a panic.
	for i := 0; i < 20; i++ {
		p.emit(sessionapi.Event{Kind: sessionapi.EventRequestResolved, RequestID: question.RequestID, Outcome: "answered"})
	}
}

func TestBackendAnswerWriteFailureRetainsPending(t *testing.T) {
	p := newProc(nil, failWriter{err: errors.New("stdin broken")}, nil)
	p.pendingMu.Lock()
	p.pending["req-x"] = json.RawMessage(`{"question":"pick?"}`)
	p.pendingMu.Unlock()

	// A failed stdin write must not consume the request: the UI is still
	// waiting_answer, so the answer stays retriable (review finding 3).
	for i := 0; i < 2; i++ {
		err := p.Answer(sessionapi.Answer{RequestID: "req-x", Behavior: sessionapi.BehaviorAllow})
		if err == nil {
			t.Fatalf("answer %d: expected write error", i)
		}
		var se *sessionapi.Error
		if errors.As(err, &se) && se.Code == sessionapi.ErrCodeNotFound {
			t.Fatalf("answer %d: pending lost after write failure: %v", i, err)
		}
	}

	// Once stdin works again the retry succeeds and resolves exactly once.
	rec := &recordingWriter{}
	p.mu.Lock()
	p.stdin = rec
	p.mu.Unlock()
	if err := p.Answer(sessionapi.Answer{RequestID: "req-x", Behavior: sessionapi.BehaviorAllow}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if s := rec.String(); !strings.Contains(s, `"control_response"`) || !strings.Contains(s, `"req-x"`) {
		t.Fatalf("control_response not written to stdin: %q", s)
	}
	ev := nextEventKind(t, p, sessionapi.EventRequestResolved, 2*time.Second)
	if ev.RequestID != "req-x" || ev.Outcome != "answered" {
		t.Fatalf("resolved = %+v", ev)
	}
	if err := p.Answer(sessionapi.Answer{RequestID: "req-x", Behavior: sessionapi.BehaviorAllow}); err == nil {
		t.Fatal("repeat answer accepted after resolution")
	}
}

func TestBackendAnswerWriteFailureConcurrentRetry(t *testing.T) {
	p := newProc(nil, failWriter{err: errors.New("stdin broken")}, nil)
	p.pendingMu.Lock()
	p.pending["req-x"] = json.RawMessage(`{"question":"pick?"}`)
	p.pendingMu.Unlock()

	// Concurrent answers on a failing writer: no answer may succeed (the
	// control_response never reached claude), and every caller is told
	// either the write failed or the request was already taken. A failed
	// write restores the pending entry, so more than one goroutine may
	// sequentially reach the write — the invariant is no duplicate
	// *successful* answers.
	const senders = 8
	results := make(chan error, senders)
	for i := 0; i < senders; i++ {
		go func() {
			results <- p.Answer(sessionapi.Answer{RequestID: "req-x", Behavior: sessionapi.BehaviorAllow})
		}()
	}
	writeErrors, notFound := 0, 0
	for i := 0; i < senders; i++ {
		err := <-results
		var se *sessionapi.Error
		switch {
		case err == nil:
			t.Fatal("answer accepted despite broken stdin")
		case errors.As(err, &se) && se.Code == sessionapi.ErrCodeNotFound:
			notFound++
		default:
			writeErrors++
		}
	}
	if writeErrors == 0 {
		t.Fatalf("no goroutine reached the write (not-found %d)", notFound)
	}
	if writeErrors+notFound != senders {
		t.Fatalf("write errors %d + not-found %d != %d", writeErrors, notFound, senders)
	}

	rec := &recordingWriter{}
	p.mu.Lock()
	p.stdin = rec
	p.mu.Unlock()
	if err := p.Answer(sessionapi.Answer{RequestID: "req-x", Behavior: sessionapi.BehaviorAllow}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	ev := nextEventKind(t, p, sessionapi.EventRequestResolved, 2*time.Second)
	if ev.RequestID != "req-x" || ev.Outcome != "answered" {
		t.Fatalf("resolved = %+v", ev)
	}
	if err := p.Answer(sessionapi.Answer{RequestID: "req-x", Behavior: sessionapi.BehaviorAllow}); err == nil {
		t.Fatal("repeat answer accepted after successful retry")
	}
}
