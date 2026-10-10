package sessionhost

import (
	"context"
	"errors"
	"sync"
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

// blockingStartBackend stalls Start until released, to exercise Stop while
// the session is still starting and has no process handle yet.
type blockingStartBackend struct {
	entered chan struct{}
	release chan struct{}
	proc    *fakeProcess
}

func newBlockingStartBackend() *blockingStartBackend {
	return &blockingStartBackend{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		proc:    newFakeProcess(),
	}
}

func (b *blockingStartBackend) Start(ctx context.Context, spec sessionapi.Spec) (Process, error) {
	close(b.entered)
	<-b.release
	return b.proc, nil
}

func TestManager_StopDuringStart(t *testing.T) {
	dir := t.TempDir()
	b := newBlockingStartBackend()
	m, err := NewManager(dir, b, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	created := make(chan Meta, 1)
	go func() {
		meta, err := m.Create(context.Background(), testSpec(t.TempDir(), "stop me"))
		if err != nil {
			t.Errorf("Create: %v", err)
			return
		}
		created <- meta
	}()
	<-b.entered

	var id string
	waitFor(t, func() bool {
		list, err := m.List()
		if err != nil || len(list) != 1 {
			return false
		}
		id = list[0].ID
		return list[0].Status == sessionapi.StatusStarting
	}, "session visible as starting")

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.Stop(id) }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop during start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung while Start was blocked")
	}

	close(b.release)
	meta := <-created
	if meta.ID != id {
		t.Fatalf("Create returned %s, want %s", meta.ID, id)
	}
	waitFor(t, func() bool { return getStatus(t, m, id) == sessionapi.StatusStopped }, "stopped")
	select {
	case _, ok := <-b.proc.stopCalls:
		if !ok {
			break
		}
		t.Fatal("stopCalls should be closed, not sent to")
	default:
		t.Fatal("process was never stopped")
	}
	close(b.proc.eventsCh) // release the pump
}

// fullEmittingProcess behaves like the real backend: Answer publishes the
// request_resolved event into the events channel, which has a tiny buffer so
// a flood saturates it exactly like >64 queued events do for the real
// process. A Session.answer that emits while holding Session.mu blocks
// forever here once the buffer is full.
type fullEmittingProcess struct {
	eventsCh chan sessionapi.Event
	answers  chan sessionapi.Answer
}

func newFullEmittingProcess() *fullEmittingProcess {
	return &fullEmittingProcess{
		eventsCh: make(chan sessionapi.Event, 1),
		answers:  make(chan sessionapi.Answer, 16),
	}
}

func (p *fullEmittingProcess) Events() <-chan sessionapi.Event { return p.eventsCh }
func (p *fullEmittingProcess) Send(text string) error          { return nil }
func (p *fullEmittingProcess) Answer(a sessionapi.Answer) error {
	p.answers <- a
	p.eventsCh <- sessionapi.Event{Kind: sessionapi.EventRequestResolved, RequestID: a.RequestID, Outcome: "answered"}
	return nil
}
func (p *fullEmittingProcess) Stop(ctx context.Context) error { return nil }
func (p *fullEmittingProcess) Wait() error                    { return nil }

type staticBackend struct{ proc Process }

func (b *staticBackend) Start(ctx context.Context, spec sessionapi.Spec) (Process, error) {
	return b.proc, nil
}

func TestManager_AnswerDoesNotDeadlockOnFullEventBuffer(t *testing.T) {
	dir := t.TempDir()
	proc := newFullEmittingProcess()
	m, err := NewManager(dir, &staticBackend{proc}, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })

	meta, err := m.Create(context.Background(), testSpec(t.TempDir(), "hi"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusRunning }, "running")

	// Keep the events buffer full from the producer side, so an Answer
	// that emits while holding Session.mu blocks forever with the pump
	// unable to drain (review finding 2).
	stopFlood := make(chan struct{})
	var floodWG sync.WaitGroup
	floodWG.Add(1)
	go func() {
		defer floodWG.Done()
		for {
			select {
			case <-stopFlood:
				return
			case proc.eventsCh <- sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "flood"}:
			}
		}
	}()
	t.Cleanup(func() {
		close(stopFlood)
		floodWG.Wait()
		close(proc.eventsCh) // release the pump
	})

	proc.eventsCh <- sessionapi.Event{Kind: sessionapi.EventQuestion, RequestID: "r1"}
	waitFor(t, func() bool { return getStatus(t, m, meta.ID) == sessionapi.StatusWaitingAnswer }, "waiting_answer")
	waitFor(t, func() bool { return len(proc.eventsCh) == cap(proc.eventsCh) }, "events buffer full")

	const senders = 4
	results := make(chan error, senders)
	for i := 0; i < senders; i++ {
		go func() {
			results <- m.Answer(meta.ID, sessionapi.Answer{RequestID: "r1", Behavior: sessionapi.BehaviorAllow})
		}()
	}
	timeout := time.After(3 * time.Second)
	succeeded := 0
	for i := 0; i < senders; i++ {
		select {
		case err := <-results:
			// Racing duplicates are told the request is gone once the
			// first answer resolves it; the regression here is the
			// bounded completion, not duplicate rejection.
			if err == nil {
				succeeded++
			}
		case <-timeout:
			t.Fatal("Answer deadlocked on a full events buffer")
		}
	}
	if succeeded == 0 {
		t.Fatal("no concurrent answer succeeded")
	}
	select {
	case a := <-proc.answers:
		if a.RequestID != "r1" {
			t.Fatalf("answer = %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("answer never reached the process")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.Stop(meta.ID) }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop deadlocked after answers")
	}
}
