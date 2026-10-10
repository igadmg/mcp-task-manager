package task

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// mockCurrentStore is an in-memory CurrentTaskStore; failWrite makes every
// write fail, to exercise rollback.
type mockCurrentStore struct {
	lists     map[string][]string
	failWrite bool
}

func newMockCurrentStore() *mockCurrentStore {
	return &mockCurrentStore{lists: make(map[string][]string)}
}

func (m *mockCurrentStore) ReadCurrentTasks(user string) ([]string, error) {
	return append([]string(nil), m.lists[user]...), nil
}

func (m *mockCurrentStore) WriteCurrentTasks(user string, ids []string) error {
	if m.failWrite {
		return errors.New("disk full")
	}
	if len(ids) == 0 {
		delete(m.lists, user)
	} else {
		m.lists[user] = append([]string(nil), ids...)
	}
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

func wantPointers(t *testing.T, store *mockCurrentStore, want ...string) {
	t.Helper()
	got := store.lists["dev"]
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !slices.Equal(got, want) {
		t.Errorf("pointer list = %v, want %v", got, want)
	}
}

func mustStartIDs(t *testing.T, svc *Service, ids ...string) {
	t.Helper()
	tasks, err := svc.CurrentTasks()
	if err != nil {
		t.Fatalf("CurrentTasks() error = %v", err)
	}
	got := taskIDs(tasks)
	if !slices.Equal(got, ids) {
		t.Fatalf("CurrentTasks() = %v, want %v", got, ids)
	}
}

func TestStartTaskWritesPointer(t *testing.T) {
	svc, store := newPointerService()
	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)
	wantPointers(t, store, task.ID)
}

func TestStartSubtaskListsParentThenSubtask(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	sub := mustCreate(t, svc, "Sub", parent.ID)
	mustStart(t, svc, sub.ID)
	wantPointers(t, store, parent.ID, sub.ID)
}

func TestStartTwoTasksKeepsBothInOrder(t *testing.T) {
	svc, store := newPointerService()
	a := mustCreate(t, svc, "A", "")
	b := mustCreate(t, svc, "B", "")
	mustStart(t, svc, a.ID)
	mustStart(t, svc, b.ID)
	wantPointers(t, store, a.ID, b.ID)
	mustStartIDs(t, svc, a.ID, b.ID)
}

func TestCompletePrunesPointerOnlyIfNamed(t *testing.T) {
	svc, store := newPointerService()
	a := mustCreate(t, svc, "A", "")
	b := mustCreate(t, svc, "B", "")
	mustStart(t, svc, a.ID)
	mustStart(t, svc, b.ID)
	wantPointers(t, store, a.ID, b.ID)

	if _, err := svc.CompleteTask(a.ID); err != nil {
		t.Fatalf("CompleteTask(a) error = %v", err)
	}
	wantPointers(t, store, b.ID)

	// Closing a task that is not on the list leaves the list alone.
	c := mustCreate(t, svc, "C", "")
	if _, err := svc.CompleteTask(c.ID, WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(c) error = %v", err)
	}
	wantPointers(t, store, b.ID)

	if _, err := svc.CompleteTask(b.ID); err != nil {
		t.Fatalf("CompleteTask(b) error = %v", err)
	}
	wantPointers(t, store)
}

func TestCompleteSubtaskLeavesOpenParentLast(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	s1 := mustCreate(t, svc, "S1", parent.ID)
	s2 := mustCreate(t, svc, "S2", parent.ID)
	s3 := mustCreate(t, svc, "S3", parent.ID)
	mustStart(t, svc, s1.ID)
	mustStart(t, svc, s2.ID)
	mustStart(t, svc, s3.ID)
	wantPointers(t, store, parent.ID, s1.ID, s2.ID, s3.ID)

	// The last entry is closed: the open parent is listed last.
	if _, err := svc.CompleteTask(s3.ID, WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(s3) error = %v", err)
	}
	wantPointers(t, store, s1.ID, s2.ID, parent.ID)

	// Closing a middle entry leaves the order of the rest alone.
	if _, err := svc.CompleteTask(s2.ID, WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(s2) error = %v", err)
	}
	wantPointers(t, store, s1.ID, parent.ID)

	// The last open subtask auto-completes the parent; both leave the list.
	if _, err := svc.CompleteTask(s1.ID, WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(s1) error = %v", err)
	}
	if p, _ := svc.Get(parent.ID); p.Status != StatusDone {
		t.Fatalf("parent status = %s, want auto-completed", p.Status)
	}
	wantPointers(t, store)
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
	wantPointers(t, store)
}

func TestNonDeliveredCloseClearsPointer(t *testing.T) {
	svc, store := newPointerService()
	a := mustCreate(t, svc, "A", "")
	b := mustCreate(t, svc, "B", "")
	mustStart(t, svc, a.ID)
	mustStart(t, svc, b.ID)

	if _, err := svc.CompleteTask(a.ID, WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(obsolete) error = %v", err)
	}
	wantPointers(t, store, b.ID)
}

// A parent closed as obsolete takes its open subtasks along; both must
// leave the list.
func TestNonDeliveredCascadeClearsPointerOnSubtask(t *testing.T) {
	svc, store := newPointerService()
	parent := mustCreate(t, svc, "Parent", "")
	sub := mustCreate(t, svc, "Sub", parent.ID)
	mustStart(t, svc, sub.ID)

	if _, err := svc.CompleteTask(parent.ID, WithResolution(ResolutionWontfix)); err != nil {
		t.Fatalf("CompleteTask(parent, wontfix) error = %v", err)
	}
	wantPointers(t, store)
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
	wantPointers(t, store)
}

func TestPointerWriteFailureRestoresPreviousList(t *testing.T) {
	svc, store := newPointerService()
	a := mustCreate(t, svc, "A", "")
	b := mustCreate(t, svc, "B", "")
	mustStart(t, svc, a.ID)
	mustStart(t, svc, b.ID)
	store.failWrite = true

	c := mustCreate(t, svc, "C", "")
	_, err := svc.StartTask(c.ID)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("StartTask() error = %v, want the pointer write failure", err)
	}
	if got, _ := svc.Get(c.ID); got.Status != StatusTodo {
		t.Errorf("task c status = %s after a failed start, want todo", got.Status)
	}
	wantPointers(t, store, a.ID, b.ID)
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
	wantPointers(t, store, parent.ID, s1.ID)
}

func TestCurrentTasksAbsentUnknownAndStale(t *testing.T) {
	svc, store := newPointerService()
	if got, err := svc.CurrentTasks(); got != nil || err != nil {
		t.Errorf("CurrentTasks() without a pointer = (%v, %v), want (nil, nil)", got, err)
	}

	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)
	mustStartIDs(t, svc, task.ID)

	// An unknown id reads as an empty list, not an error; a legacy file
	// holding one id reads as a list of one.
	store.lists["dev"] = []string{"no-such-task"}
	mustStartIDs(t, svc)
	store.lists["dev"] = []string{task.ID}
	mustStartIDs(t, svc, task.ID)

	// A task that left in_progress outside the flows drops out of the
	// effective list, and the next write drops it from the file.
	todo := StatusTodo
	if _, err := svc.Update(task.ID, nil, nil, &todo, nil, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	mustStartIDs(t, svc)
	other := mustCreate(t, svc, "Other", "")
	mustStart(t, svc, other.ID)
	wantPointers(t, store, other.ID)
}

func TestNilStoreIsNoop(t *testing.T) {
	svc := newResolutionService()
	task := mustCreate(t, svc, "Work", "")
	mustStart(t, svc, task.ID)
	if _, err := svc.CompleteTask(task.ID); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if got, err := svc.CurrentTasks(); got != nil || err != nil {
		t.Errorf("CurrentTasks() without a store = (%v, %v), want (nil, nil)", got, err)
	}
}
