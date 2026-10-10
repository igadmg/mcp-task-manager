package claude

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
	"github.com/gpayer/mcp-task-manager/internal/sessionhost"
)

// TestManagerQuestionAnswerFullFlow drives a real session through the
// manager with the re-exec fake backend: question -> answer -> running ->
// idle -> follow-up message, with exactly one request_resolved (review
// finding 2). The manager must receive request_resolved from the backend;
// without it the session would hang in waiting_answer.
func TestManagerQuestionAnswerFullFlow(t *testing.T) {
	dir := t.TempDir()
	backend := fakeBackend(t, "askstay")
	mgr, err := sessionhost.NewManager(filepath.Join(dir, "state"), backend, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Close() })

	spec := sessionapi.Spec{
		Version:   sessionapi.SpecVersion,
		Provider:  sessionapi.ProviderClaude,
		Workspace: sessionapi.Workspace{Kind: sessionapi.WorkspaceKindWorkingDir, Path: t.TempDir()},
		Prompt:    "ask me",
	}
	meta, err := mgr.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var reqID string
	for time.Now().Before(deadline) {
		view, err := mgr.Get(meta.ID)
		if err == nil && len(view.PendingRequests) == 1 {
			reqID = view.PendingRequests[0].RequestID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("question never became pending")
	}

	answer := sessionapi.Answer{RequestID: reqID, Behavior: sessionapi.BehaviorAllow,
		Answers: map[string]string{"Pick one?": "B"}}
	if err := mgr.Answer(meta.ID, answer); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	// The journal must carry exactly one request_resolved, followed by a
	// return to running and then idle after the turn ends.
	sub, err := mgr.Subscribe(meta.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	resolved := 0
	runningAfterResolved := false
	var saw []sessionapi.Event
	quiet := time.Now().Add(2 * time.Second)
	for time.Now().Before(quiet) {
		select {
		case ev, ok := <-sub.C:
			if !ok {
				goto drained
			}
			saw = append(saw, ev)
			switch ev.Kind {
			case sessionapi.EventRequestResolved:
				resolved++
				if ev.RequestID != reqID || ev.Outcome != "answered" {
					t.Fatalf("request_resolved = %+v", ev)
				}
			case sessionapi.EventStatus:
				if resolved > 0 && ev.Status == sessionapi.StatusRunning {
					runningAfterResolved = true
				}
			}
			quiet = time.Now().Add(300 * time.Millisecond)
		case <-time.After(50 * time.Millisecond):
		}
	}
drained:
	if resolved != 1 {
		t.Fatalf("request_resolved count = %d, want 1; events: %+v", resolved, saw)
	}
	if !runningAfterResolved {
		t.Fatalf("session did not return to running after the answer: %+v", saw)
	}

	if err := mgr.Answer(meta.ID, answer); err == nil {
		t.Fatal("duplicate answer accepted")
	}

	waitStatus(t, mgr, meta.ID, sessionapi.StatusIdle)

	// The session is alive between turns: a follow-up message gets a reply.
	if err := mgr.Send(meta.ID, "continue"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitStatus(t, mgr, meta.ID, sessionapi.StatusIdle)

	if err := mgr.Stop(meta.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitStatus(t, mgr, meta.ID, sessionapi.StatusStopped)
}

func waitStatus(t *testing.T, mgr *sessionhost.Manager, id string, want sessionapi.Status) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last sessionapi.Status
	for time.Now().Before(deadline) {
		view, err := mgr.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		last = view.Meta.Status
		if last == want {
			return
		}
		if len(view.PendingRequests) != 0 && want != sessionapi.StatusWaitingAnswer {
			t.Fatalf("pending requests while waiting for %s: %+v", want, view.PendingRequests)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status = %s, want %s", last, want)
}
