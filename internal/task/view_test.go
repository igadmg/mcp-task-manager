package task

import (
	"errors"
	"slices"
	"testing"
	"time"
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

// phaseListingFiles lists a task's phase records next to its other files,
// as the real file store does: the service only reads the records it sees.
// It asks svc's current phase store, so a swapped-in broken one lists too.
type phaseListingFiles struct {
	*mockFileStorage
	svc *Service
}

func (f phaseListingFiles) ListFiles(id string) ([]string, error) {
	names, _ := f.mockFileStorage.ListFiles(id)
	for _, p := range Phases() {
		if rec, err := f.svc.phases.LoadPhase(id, p); err != nil || rec != nil {
			names = append(names, PhaseFileName(p))
		}
	}
	return names, nil
}

// newBoardFixture is the phase fixture, whose file listing shows the records.
func newBoardFixture(t *testing.T) *phaseFixture {
	t.Helper()
	return newPhaseFixture(t)
}

// at is 2026-10-04 at hour h, UTC.
func at(h int) time.Time { return time.Date(2026, 10, 4, h, 0, 0, 0, time.UTC) }

// putRuns writes p's record directly; each run starts at the given hour,
// and all but an open last one (open=true) are finished an hour later.
func (f *phaseFixture) putRuns(t *testing.T, id string, p Phase, open bool, tokens *int64, hours ...int) {
	t.Helper()
	rec := &PhaseRecord{Phase: p}
	for i, h := range hours {
		run := PhaseRun{StartedAt: at(h), StartedBy: "dev"}
		if !open || i < len(hours)-1 {
			done := at(h + 1)
			run.FinishedAt, run.FinishedBy, run.Tokens = &done, "dev", tokens
		}
		rec.Runs = append(rec.Runs, run)
	}
	if err := f.phases.SavePhase(id, rec); err != nil {
		t.Fatalf("SavePhase() error = %v", err)
	}
}

func (f *phaseFixture) snapshot(t *testing.T) *BoardSnapshot {
	t.Helper()
	snap, err := f.svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	return snap
}

func TestBoardLaneFromLatestRun(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	f.putRuns(t, "a", PhaseResearch, false, nil, 10)
	f.putRuns(t, "a", PhaseDesign, true, nil, 11)

	snap := f.snapshot(t)
	if got := snap.Phases["a"]; got != PhaseDesign {
		t.Errorf("Phases[a] = %q, want design", got)
	}
	info, ok := snap.PhaseInfo["a"]
	if !ok || info.Current != PhaseDesign || !info.Run.Open() || !info.Run.StartedAt.Equal(at(11)) || info.Run.StartedBy != "dev" {
		t.Errorf("PhaseInfo[a] = %+v, %v", info, ok)
	}

	// Same start second: the later phase wins.
	f.putRuns(t, "a", PhasePlanning, true, nil, 11)
	if got := f.snapshot(t).Phases["a"]; got != PhasePlanning {
		t.Errorf("tie: Phases[a] = %q, want planning", got)
	}
}

func TestBoardReRunMovesLaneBack(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	f.putRuns(t, "a", PhaseResearch, false, nil, 10)
	f.putRuns(t, "a", PhaseDesign, true, nil, 11, 13)
	f.putRuns(t, "a", PhasePlanning, false, nil, 12)
	if got := f.snapshot(t).Phases["a"]; got != PhaseDesign {
		t.Errorf("Phases[a] = %q, want design after its re-run", got)
	}
}

func TestBoardFallbackWithoutRecords(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	inProgress := StatusInProgress
	if _, err := f.svc.Update("a", nil, nil, &inProgress, nil, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	_ = f.files.WriteFile("a", "research", "x")
	snap := f.snapshot(t)
	if got := snap.Phases["a"]; got != PhaseDesign {
		t.Errorf("Phases[a] = %q, want design from the artifact name", got)
	}
	if _, ok := snap.PhaseInfo["a"]; ok {
		t.Errorf("PhaseInfo[a] set without records")
	}
}

func TestBoardUnparsableRecordFallsBack(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	f.finish(t, "a", PhaseResearch)
	f.start(t, "a", PhaseDesign)
	_ = f.files.WriteFile("a", "design", "x") // the names prove planning
	broken := &brokenPhaseStore{mockPhaseStore: f.phases, broken: PhaseDesign}
	f.svc.phases = broken

	// Research still parses: it is the lane, not the broken design.
	snap := f.snapshot(t)
	if got := snap.Phases["a"]; got != PhaseResearch {
		t.Errorf("Phases[a] = %q, want research from the readable record", got)
	}

	// With no readable record left, the names decide.
	_ = f.phases.RemovePhase("a", PhaseResearch)
	snap = f.snapshot(t)
	if got := snap.Phases["a"]; got != PhasePlanning {
		t.Errorf("Phases[a] = %q, want planning from the names", got)
	}
	if _, ok := snap.PhaseInfo["a"]; ok {
		t.Errorf("PhaseInfo[a] set from an unreadable record")
	}
}

func TestBoardTokensSum(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	hundred, fifty := int64(100), int64(50)
	f.putRuns(t, "a", PhaseResearch, false, &hundred, 10)
	f.putRuns(t, "a", PhaseDesign, true, &fifty, 11, 12, 14) // two finished, one open
	info := f.snapshot(t).PhaseInfo["a"]
	if info.Tokens != 200 || !info.HasTokens {
		t.Errorf("tokens = %d (%v), want 200", info.Tokens, info.HasTokens)
	}

	f.create(t, "b", "")
	f.start(t, "b", PhaseResearch)
	if info := f.snapshot(t).PhaseInfo["b"]; info.HasTokens {
		t.Errorf("b reports tokens without any: %+v", info)
	}
}

func TestBoardPhaseInfoInProgressOnly(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "todo", "")
	f.putRuns(t, "todo", PhaseResearch, true, nil, 10)
	f.create(t, "done", "")
	f.start(t, "done", PhaseResearch)
	if _, err := f.svc.CompleteTask("done"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	snap := f.snapshot(t)
	if len(snap.PhaseInfo) != 0 || len(snap.Phases) != 0 {
		t.Errorf("PhaseInfo = %v, Phases = %v; want neither for todo and done tasks", snap.PhaseInfo, snap.Phases)
	}
}

func TestDetailPhasesIncludingArchived(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	f.finish(t, "a", PhaseResearch)
	f.start(t, "a", PhaseDesign)

	d, err := f.svc.Detail("a")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if len(d.Phases) != 2 || d.Phases[0].Phase != PhaseResearch || d.Phases[1].Phase != PhaseDesign {
		t.Fatalf("Detail().Phases = %+v, want research, design", d.Phases)
	}

	if _, err := f.svc.CompleteTask("a"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if err := f.svc.ArchiveTask("a"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	d, err = f.svc.Detail("a")
	if err != nil || !d.Archived || len(d.Phases) != 2 {
		t.Errorf("archived Detail() = %+v, %v; want both records", d, err)
	}
}

func TestDetailFilesHidePhaseFiles(t *testing.T) {
	f := newBoardFixture(t)
	f.create(t, "a", "")
	_ = f.files.WriteFile("a", "notes.md", "x")
	f.start(t, "a", PhaseResearch)
	d, err := f.svc.Detail("a")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if !slices.Equal(d.Files, []string{"notes.md"}) || len(d.Phases) != 1 {
		t.Errorf("Files = %v, Phases = %d; want the phase record only in Phases", d.Files, len(d.Phases))
	}
}
