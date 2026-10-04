package task

import (
	"errors"
	"testing"
)

// newViewService builds a service with a real (mock-backed) archive and file
// storage so Detail can exercise every branch.
func newViewService(t *testing.T) *Service {
	t.Helper()
	return newConcurrentService()
}

func TestBoardSnapshotShape(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}

	if got, want := len(snap.Tasks), 30; got != want {
		t.Errorf("len(Tasks) = %d, want %d (10 parents + 20 subtasks)", got, want)
	}
	if snap.TakenAt.IsZero() {
		t.Error("TakenAt is zero")
	}
}

func TestBoardSnapshotSubtaskGrouping(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	if _, err := svc.StartTask("p0-s0"); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if _, err := svc.CompleteTask("p0-s0"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}

	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}

	subs := snap.Subtasks["p0"]
	if len(subs) != 2 {
		t.Fatalf("len(Subtasks[p0]) = %d, want 2", len(subs))
	}
	for _, s := range subs {
		if s.ParentID != "p0" {
			t.Errorf("subtask %s grouped under p0 but has parent %q", s.ID, s.ParentID)
		}
	}

	count := snap.Counts["p0"]
	if count.Total != 2 {
		t.Errorf("Counts[p0].Total = %d, want 2", count.Total)
	}
	if count.Done != 1 {
		t.Errorf("Counts[p0].Done = %d, want 1", count.Done)
	}
	if _, ok := snap.Counts["p0-s0"]; ok {
		t.Error("a subtask must not appear in Counts; only parents do")
	}
}

func TestBoardSnapshotPhases(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	if _, err := svc.StartTask("p0"); err != nil {
		t.Fatalf("StartTask(p0) error = %v", err)
	}
	if err := svc.WriteTaskFile("p0", "research", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	// Starting the subtask auto-starts p2, which has no files of its own.
	if _, err := svc.StartTask("p2-s0"); err != nil {
		t.Fatalf("StartTask(p2-s0) error = %v", err)
	}
	if err := svc.WriteTaskFile("p2-s0", "PLAN.md", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	// A todo task with a plan still gets no entry.
	if err := svc.WriteTaskFile("p3", "plan", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}

	want := map[string]Phase{
		"p0":    PhaseDesign,
		"p2":    PhaseResearch,
		"p2-s0": PhaseImplementation,
	}
	for id, p := range want {
		if got, ok := snap.Phases[id]; !ok || got != p {
			t.Errorf("Phases[%s] = %q (present %v), want %q", id, got, ok, p)
		}
	}
	if _, ok := snap.Phases["p3"]; ok {
		t.Error("Phases has an entry for todo task p3")
	}
	if len(snap.Phases) != len(want) {
		t.Errorf("len(Phases) = %d, want %d: %v", len(snap.Phases), len(want), snap.Phases)
	}
}

func TestBoardSnapshotPhasesEmptyWhenNothingInProgress(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	if snap.Phases == nil {
		t.Fatal("Phases is nil, want an empty map")
	}
	if len(snap.Phases) != 0 {
		t.Errorf("Phases = %v, want empty", snap.Phases)
	}
}

func TestBoardSnapshotPhasesWithoutFileStorage(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)
	svc.fileStorage = nil // the untyped nil: a typed nil pointer would pass the guard

	if _, err := svc.StartTask("p0"); err != nil {
		t.Fatalf("StartTask(p0) error = %v", err)
	}
	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	if got := snap.Phases["p0"]; got != PhaseResearch {
		t.Errorf("Phases[p0] = %q, want %q", got, PhaseResearch)
	}
}

// failingListFiles is a file store whose listing always fails.
type failingListFiles struct{ *mockFileStorage }

func (failingListFiles) ListFiles(string) ([]string, error) {
	return nil, errors.New("listing failed")
}

func TestBoardSnapshotPhaseListingErrorDegrades(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)
	if err := svc.WriteTaskFile("p0", "plan", "x"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	svc.fileStorage = failingListFiles{svc.fileStorage.(*mockFileStorage)}

	if _, err := svc.StartTask("p0"); err != nil {
		t.Fatalf("StartTask(p0) error = %v", err)
	}
	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	if got := snap.Phases["p0"]; got != PhaseResearch {
		t.Errorf("Phases[p0] = %q, want %q despite the plan file", got, PhaseResearch)
	}
}

// TestBlockedMapMatchesIsBlocked is the safety net for the list_tasks
// refactor: the batch lookup has to answer exactly what the per-task loop did.
func TestBlockedMapMatchesIsBlocked(t *testing.T) {
	svc := newViewService(t)
	ids := seedBacklog(t, svc)

	// Close one blocker so the "target is done stops blocking" branch is live.
	if _, err := svc.StartTask("p0"); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	batch := svc.BlockedMap(ids)
	for _, id := range ids {
		wantBlocked, wantBlockers := svc.IsBlocked(id)
		gotBlockers, ok := batch[id]
		if ok != wantBlocked {
			t.Errorf("BlockedMap[%s] present = %v, IsBlocked = %v", id, ok, wantBlocked)
			continue
		}
		if len(gotBlockers) != len(wantBlockers) {
			t.Errorf("BlockedMap[%s] = %d blockers, IsBlocked = %d", id, len(gotBlockers), len(wantBlockers))
			continue
		}
		for i := range gotBlockers {
			if gotBlockers[i] != wantBlockers[i] {
				t.Errorf("BlockedMap[%s][%d] = %+v, want %+v", id, i, gotBlockers[i], wantBlockers[i])
			}
		}
	}
}

func TestDetailActiveTask(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	if err := svc.WriteTaskFile("p1", "research.md", "notes"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	d, err := svc.Detail("p1")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}

	if d.Archived {
		t.Error("Archived = true for an active task")
	}
	if d.Task.ID != "p1" {
		t.Errorf("Task.ID = %q, want p1", d.Task.ID)
	}
	if len(d.Subtasks) != 2 {
		t.Errorf("len(Subtasks) = %d, want 2", len(d.Subtasks))
	}
	if !d.Blocked || len(d.Blockers) != 1 {
		t.Errorf("Blocked = %v with %d blockers, want true with 1", d.Blocked, len(d.Blockers))
	}
	if len(d.Relations) == 0 {
		t.Error("Relations is empty, want the blocked_by edge")
	}
	if len(d.Files) != 1 || d.Files[0] != "research.md" {
		t.Errorf("Files = %v, want [research.md]", d.Files)
	}
}

func TestDetailArchivedTaskHasEmptyDerivedData(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	// Close the whole p3 tree, then archive it.
	for _, id := range []string{"p3-s0", "p3-s1"} {
		if _, err := svc.StartTask(id); err != nil {
			t.Fatalf("StartTask(%s) error = %v", id, err)
		}
		if _, err := svc.CompleteTask(id); err != nil {
			t.Fatalf("CompleteTask(%s) error = %v", id, err)
		}
	}
	if err := svc.WriteTaskFile("p3", "design.md", "kept"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}
	if err := svc.ArchiveTask("p3"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	d, err := svc.Detail("p3")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}

	if !d.Archived {
		t.Fatal("Archived = false for an archived task")
	}
	if len(d.Subtasks) != 0 || len(d.Relations) != 0 || len(d.Blockers) != 0 || d.Blocked {
		t.Errorf("archived detail carries derived data: subtasks=%d relations=%d blockers=%d blocked=%v",
			len(d.Subtasks), len(d.Relations), len(d.Blockers), d.Blocked)
	}
	if len(d.Files) != 1 || d.Files[0] != "design.md" {
		t.Errorf("Files = %v, want [design.md]: attached files travel with the task", d.Files)
	}
}

func TestRelationsIncludesIncomingEdges(t *testing.T) {
	svc := newViewService(t)
	seedBacklog(t, svc)

	// p1 is blocked_by p0, so the edge is stored on p1 and p0 only ever sees
	// it as an incoming one.
	incoming := svc.Relations("p0")
	if len(incoming) == 0 {
		t.Fatal("Relations(p0) is empty, want the incoming blocked_by edge")
	}
	found := false
	for _, e := range incoming {
		if e.Type == "blocked_by" && e.Source == "p1" && e.Target == "p0" {
			found = true
		}
	}
	if !found {
		t.Errorf("Relations(p0) = %+v, want an edge p1 -blocked_by-> p0", incoming)
	}
}

func TestConfigIsTheServiceConfig(t *testing.T) {
	svc := newViewService(t)
	cfg := svc.Config()
	if cfg == nil {
		t.Fatal("Config() = nil")
	}
	if len(cfg.TaskTypes) == 0 {
		t.Error("Config().TaskTypes is empty")
	}
}
