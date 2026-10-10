package sessionhost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

func newTestManager(t *testing.T, max int) (*Manager, *fakeBackend, string) {
	t.Helper()
	dir := t.TempDir()
	b := newFakeBackend()
	m, err := NewManager(dir, b, max)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m, b, dir
}

func createSession(t *testing.T, m *Manager, b *fakeBackend) (Meta, *fakeProcess) {
	t.Helper()
	meta, err := m.Create(context.Background(), testSpec(t.TempDir(), "do the thing"))
	if err != nil {
		t.Fatal(err)
	}
	var fp *fakeProcess
	select {
	case fp = <-b.started:
	case <-time.After(2 * time.Second):
		t.Fatal("backend not started")
	}
	return meta, fp
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}

func getStatus(t *testing.T, m *Manager, id string) sessionapi.Status {
	t.Helper()
	view, err := m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return view.Meta.Status
}

func drainEvents(t *testing.T, m *Manager, id string, after int64) []sessionapi.Event {
	t.Helper()
	sub, err := m.Subscribe(id, after)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	time.Sleep(100 * time.Millisecond) // let replay arrive
	var events []sessionapi.Event
	for {
		select {
		case ev, ok := <-sub.C:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-time.After(150 * time.Millisecond):
			return events
		}
	}
}

func errCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var e *sessionapi.Error
	if !errors.As(err, &e) {
		t.Fatalf("not a sessionapi.Error: %v", err)
	}
	return e.Code
}

func TestManager_Lifecycle(t *testing.T) {
	m, b, _ := newTestManager(t, 4)
	meta, fp := createSession(t, m, b)

	fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "working on it"}
	fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventTurnResult, OK: true}
	close(fp.eventsCh)

	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusFinished }, "finished")

	events := drainEvents(t, m, meta.ID, 0)
	if len(events) < 6 {
		t.Fatalf("too few events: %d", len(events))
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Fatalf("seq gap at %d: %+v", i, ev)
		}
		if ev.Ts.IsZero() {
			t.Fatalf("zero ts at seq %d", ev.Seq)
		}
	}
	kinds := make([]sessionapi.EventKind, 0, len(events))
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	assertOrder(t, kinds, sessionapi.EventStatus, sessionapi.EventStatus, sessionapi.EventAssistantText,
		sessionapi.EventTurnResult, sessionapi.EventStatus, sessionapi.EventStatus)
	if events[len(events)-1].Status != sessionapi.StatusFinished {
		t.Fatalf("last event: %+v", events[len(events)-1])
	}
}

func assertOrder(t *testing.T, kinds []sessionapi.EventKind, want ...sessionapi.EventKind) {
	t.Helper()
	filtered := make([]sessionapi.EventKind, 0, len(want))
	for _, k := range kinds {
		if k == sessionapi.EventStatus {
			filtered = append(filtered, k)
		}
	}
	// compare full sequence if lengths match, else status subsequence
	if len(kinds) == len(want) {
		for i := range want {
			if kinds[i] != want[i] {
				t.Fatalf("kinds[%d]=%s, want %s (all: %v)", i, kinds[i], want[i], kinds)
			}
		}
		return
	}
	if len(filtered) != len(want) {
		t.Fatalf("status events %v, want %d", filtered, len(want))
	}
}

func TestManager_QuestionAnswer(t *testing.T) {
	m, b, _ := newTestManager(t, 4)
	meta, fp := createSession(t, m, b)

	fp.eventsCh <- sessionapi.Event{
		Kind:      sessionapi.EventQuestion,
		RequestID: "r1",
		ToolUseID: "tu1",
		Questions: []sessionapi.Question{{
			Question: "pick",
			Options:  []sessionapi.Option{{Label: "a"}, {Label: "b"}},
		}},
	}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusWaitingAnswer }, "waiting_answer")

	view, err := m.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.PendingRequests) != 1 || view.PendingRequests[0].RequestID != "r1" {
		t.Fatalf("pending: %+v", view.PendingRequests)
	}

	answer := sessionapi.Answer{RequestID: "r1", Behavior: sessionapi.BehaviorAllow, Answers: map[string]string{"pick": "a"}}
	if err := m.Answer(meta.ID, answer); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-fp.answers:
		if got.RequestID != "r1" || got.Answers["pick"] != "a" {
			t.Fatalf("answer delivered: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("answer not delivered to process")
	}

	fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventRequestResolved, RequestID: "r1", Outcome: "answered"}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusRunning }, "running after answer")

	view, _ = m.Get(meta.ID)
	if len(view.PendingRequests) != 0 {
		t.Fatalf("pending after answer: %+v", view.PendingRequests)
	}
}

func TestManager_MaxSessions(t *testing.T) {
	m, b, _ := newTestManager(t, 1)
	meta, _ := createSession(t, m, b)
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusRunning }, "first running")

	_, err := m.Create(context.Background(), testSpec(t.TempDir(), "second"))
	if code := errCode(t, err); code != sessionapi.ErrCodeTooManySessions {
		t.Fatalf("code: %s", code)
	}
}

func TestManager_Send(t *testing.T) {
	m, b, _ := newTestManager(t, 4)
	meta, fp := createSession(t, m, b)

	fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventTurnResult, OK: true}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusIdle }, "idle")

	if err := m.Send(meta.ID, "follow up"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-fp.sent:
		if text != "follow up" {
			t.Fatalf("sent: %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message not delivered")
	}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusRunning }, "running after send")

	close(fp.eventsCh)
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusFinished }, "finished")
	if code := errCode(t, m.Send(meta.ID, "too late")); code != sessionapi.ErrCodeConflict {
		t.Fatalf("send to finished: %s", code)
	}
}

func TestManager_Stop(t *testing.T) {
	m, b, _ := newTestManager(t, 4)
	meta, fp := createSession(t, m, b)

	fp.eventsCh <- sessionapi.Event{
		Kind:      sessionapi.EventPermission,
		RequestID: "r9",
		ToolUseID: "tu9",
		ToolName:  "Bash",
	}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusWaitingAnswer }, "waiting_answer")

	if err := m.Stop(meta.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-fp.stopCalls:
		if !ok {
			break
		}
		t.Fatal("stopCalls should be closed, not sent to")
	case <-time.After(2 * time.Second):
		t.Fatal("process not stopped")
	}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusStopped }, "stopped")

	events := drainEvents(t, m, meta.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == sessionapi.EventRequestResolved && ev.RequestID == "r9" && ev.Outcome == "cancelled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no cancellation event for r9: %+v", events)
	}
	close(fp.eventsCh)
}

func TestManager_StartErrorMarksFailed(t *testing.T) {
	dir := t.TempDir()
	b := newFakeBackend()
	b.startErr = errors.New("claude binary not found")
	m, err := NewManager(dir, b, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	meta, err := m.Create(context.Background(), testSpec(t.TempDir(), "hello"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusFailed }, "failed")

	events := drainEvents(t, m, meta.ID, 0)
	var errEv *sessionapi.Event
	for i := range events {
		if events[i].Kind == sessionapi.EventError {
			errEv = &events[i]
		}
	}
	if errEv == nil || errEv.Message == "" {
		t.Fatalf("no error event: %+v", events)
	}
}

func TestManager_CreateValidates(t *testing.T) {
	m, _, _ := newTestManager(t, 4)

	bad := testSpec(t.TempDir(), "x")
	bad.Provider = "gemini"
	_, err := m.Create(context.Background(), bad)
	if code := errCode(t, err); code != sessionapi.ErrCodeUnsupportedProvider {
		t.Fatalf("provider: %s", code)
	}

	missing := testSpec(t.TempDir(), "x")
	missing.Workspace.Path += "/no-such-dir"
	_, err = m.Create(context.Background(), missing)
	if code := errCode(t, err); code != sessionapi.ErrCodeWorkspaceNotFound {
		t.Fatalf("workspace: %s", code)
	}
}

func TestManager_RestartRecovery(t *testing.T) {
	m1, b, dir := newTestManager(t, 4)
	meta, fp := createSession(t, m1, b)
	fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "mid-work"}
	waitFor(t, func() bool { return getStatus(t, m1, meta.ID) == sessionapi.StatusRunning }, "running")

	m2, err := NewManager(dir, b, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.Close() })
	view, err := m2.Get(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Meta.Status != sessionapi.StatusFailed {
		t.Fatalf("status after restart: %s", view.Meta.Status)
	}

	events := drainEvents(t, m2, meta.ID, 0)
	last := events[len(events)-1]
	if last.Kind != sessionapi.EventStatus || last.Status != sessionapi.StatusFailed || last.Reason != "host_restarted" {
		t.Fatalf("last event: %+v", last)
	}
	close(fp.eventsCh)
}

func TestManager_FinishedSurvivesRestart(t *testing.T) {
	m1, b, dir := newTestManager(t, 4)
	meta, fp := createSession(t, m1, b)
	close(fp.eventsCh)
	waitFor(t, func() bool { return getStatus(t, m1, meta.ID) == sessionapi.StatusFinished }, "finished")

	m2, err := NewManager(dir, b, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m2.Close() })
	list, err := m2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != sessionapi.StatusFinished {
		t.Fatalf("list: %+v", list)
	}
	if _, err := m2.Get(meta.ID); err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, m2, meta.ID, 0)
	if len(events) == 0 {
		t.Fatal("no events after restart")
	}
}

func TestManager_SlowSubscriberReadsFromJournal(t *testing.T) {
	m, b, _ := newTestManager(t, 4)
	meta, fp := createSession(t, m, b)
	for i := 0; i < 50; i++ {
		fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "line"}
	}
	fp.eventsCh <- sessionapi.Event{Kind: sessionapi.EventTurnResult, OK: true}
	close(fp.eventsCh)
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusFinished }, "finished")

	// Subscriber connects only after everything happened; journal must replay all.
	events := drainEvents(t, m, meta.ID, 0)
	if len(events) != 55 { // starting, running, 50 text, turn_result, idle, finished
		t.Fatalf("replayed %d events", len(events))
	}
}
