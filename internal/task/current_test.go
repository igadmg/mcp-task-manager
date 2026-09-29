package task

import (
	"errors"
	"strings"
	"testing"
)

// mockCurrentStore is an in-memory CurrentTaskStore; failWrite makes every
// write fail, to exercise rollback.
type mockCurrentStore struct {
	ids       map[string]string
	failWrite bool
}

func newMockCurrentStore() *mockCurrentStore {
	return &mockCurrentStore{ids: make(map[string]string)}
}

func (m *mockCurrentStore) ReadCurrentTask(user string) (string, bool, error) {
	id, ok := m.ids[user]
	return id, ok, nil
}

func (m *mockCurrentStore) WriteCurrentTask(user, id string) error {
	if m.failWrite {
		return errors.New("disk full")
	}
	m.ids[user] = id
	return nil
}

func (m *mockCurrentStore) RemoveCurrentTask(user string) error {
	delete(m.ids, user)
	return nil
}

func newPointerService() (*Service, *mockCurrentStore) {
	store := newMockCurrentStore()
	svc := NewService(newMockStorage(), nil, nil, newMockIndex(), []string{"feature", "bug"}, nil,
		WithCurrentTaskStore(store), WithIdentity(Identity{Name: "dev"}))
	svc.Initialize()
	return svc, store
}

func mustCreate(t *testing.T, svc *Service, title, parentID string) *Task {
	t.Helper()
	created, err := svc.Create(title, "", PriorityHigh, "feature", parentID, "")
	if err != nil {
		t.Fatalf("Create(%q) error = %v", title, err)
	}
	return created
}

func mustStart(t *testing.T, svc *Service, id string) {
	t.Helper()
	if _, err := svc.StartTask(id); err != nil {
		t.Fatalf("StartTask(%s) error = %v", id, err)
	}
}

func wantPointer(t *testing.T, store *mockCurrentStore, want string) {
	t.Helper()
	got, ok := store.ids["dev"]
	switch {
	case want == "" && ok:
		t.Errorf("pointer = %q, want none", got)
	case want != "" && got != want:
		t.Errorf("pointer = %q (set: %v), want %q", got, ok, want)
	}
}

func TestStartTaskWritesPointer(t *testing.T) {
	svc, store := newPointerService()
	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)
	wantPointer(t, store, task.ID)
}

func TestStartSubtaskPointsAtSubtask(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	sub := mustCreate(t, svc, "Sub", parent.ID)
	mustStart(t, svc, sub.ID)
	wantPointer(t, store, sub.ID)
}

func TestCompleteClearsPointerOnlyIfNamed(t *testing.T) {
	svc, store := newPointerService()
	a := mustCreate(t, svc, "A", "")
	b := mustCreate(t, svc, "B", "")
	mustStart(t, svc, a.ID)
	mustStart(t, svc, b.ID)
	wantPointer(t, store, b.ID)

	if _, err := svc.CompleteTask(a.ID); err != nil {
		t.Fatalf("CompleteTask(a) error = %v", err)
	}
	wantPointer(t, store, b.ID)

	if _, err := svc.CompleteTask(b.ID); err != nil {
		t.Fatalf("CompleteTask(b) error = %v", err)
	}
	wantPointer(t, store, "")
}

func TestCompleteSubtaskMovesPointerToOpenParent(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	s1 := mustCreate(t, svc, "S1", parent.ID)
	mustCreate(t, svc, "S2", parent.ID)
	mustStart(t, svc, s1.ID)

	if _, err := svc.CompleteTask(s1.ID); err != nil {
		t.Fatalf("CompleteTask(s1) error = %v", err)
	}
	wantPointer(t, store, parent.ID)
}

func TestCompleteLastSubtaskAutoCompletesParentRemovesPointer(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	sub := mustCreate(t, svc, "Only", parent.ID)
	mustStart(t, svc, sub.ID)

	if _, err := svc.CompleteTask(sub.ID); err != nil {
		t.Fatalf("CompleteTask(sub) error = %v", err)
	}
	if p, _ := svc.Get(parent.ID); p.Status != StatusDone {
		t.Fatalf("parent status = %s, want auto-completed", p.Status)
	}
	wantPointer(t, store, "")
}

func TestNonDeliveredCloseClearsPointer(t *testing.T) {
	svc, store := newPointerService()
	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)

	if _, err := svc.CompleteTask(task.ID, WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(obsolete) error = %v", err)
	}
	wantPointer(t, store, "")
}

// A parent closed as obsolete takes its open subtasks along; a pointer on
// one of them must not be left naming a task that was just closed.
func TestNonDeliveredCascadeClearsPointerOnSubtask(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	sub := mustCreate(t, svc, "Sub", parent.ID)
	mustStart(t, svc, sub.ID)

	if _, err := svc.CompleteTask(parent.ID, WithResolution(ResolutionWontfix)); err != nil {
		t.Fatalf("CompleteTask(parent, wontfix) error = %v", err)
	}
	wantPointer(t, store, "")
}

func TestPointerWriteFailureRollsBackStart(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	sub := mustCreate(t, svc, "Sub", parent.ID)
	store.failWrite = true

	_, err := svc.StartTask(sub.ID)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("StartTask() error = %v, want the pointer write failure", err)
	}
	for _, id := range []string{parent.ID, sub.ID} {
		got, err := svc.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", id, err)
		}
		if got.Status != StatusTodo {
			t.Errorf("task %s status = %s after a failed start, want todo", id, got.Status)
		}
	}
	wantPointer(t, store, "")
}

func TestPointerWriteFailureRollsBackComplete(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	s1 := mustCreate(t, svc, "S1", parent.ID)
	mustCreate(t, svc, "S2", parent.ID)
	mustStart(t, svc, s1.ID)
	store.failWrite = true

	if _, err := svc.CompleteTask(s1.ID); err == nil {
		t.Fatal("CompleteTask() error = nil, want the pointer write failure")
	}
	if got, _ := svc.Get(s1.ID); got.Status != StatusInProgress || got.ClosedAt != nil {
		t.Errorf("s1 = %s (closed_at %v) after a failed completion, want in_progress", got.Status, got.ClosedAt)
	}
	wantPointer(t, store, s1.ID)
}

func TestCurrentTaskAbsentAndUnknown(t *testing.T) {
	svc, store := newPointerService()
	if got, ok, err := svc.CurrentTask(); got != nil || ok || err != nil {
		t.Errorf("CurrentTask() without a pointer = (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}

	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)
	got, ok, err := svc.CurrentTask()
	if err != nil || !ok || got.ID != task.ID {
		t.Errorf("CurrentTask() = (%v, %v, %v), want task %s", got, ok, err, task.ID)
	}

	store.ids["dev"] = "no-such-task"
	if _, _, err := svc.CurrentTask(); err == nil || !strings.Contains(err.Error(), "current task no-such-task not found") {
		t.Errorf("CurrentTask() with an unknown id: error = %v", err)
	}
}

func TestNilStoreIsNoop(t *testing.T) {
	svc := newResolutionService()
	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)
	if _, err := svc.CompleteTask(task.ID); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if got, ok, err := svc.CurrentTask(); got != nil || ok || err != nil {
		t.Errorf("CurrentTask() without a store = (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
}
