package task

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// phaseFixture is an in-memory service with every collaborator the phase
// flows use: archive, files, pointer and phase stores, identity "dev".
type phaseFixture struct {
	svc     *Service
	archive *mockArchiveStorage
	files   *mockFileStorage
	pointer *mockCurrentStore
	phases  *mockPhaseStore
}

func newPhaseFixture(t *testing.T) *phaseFixture {
	t.Helper()
	ms := newMockStorage()
	f := &phaseFixture{
		archive: newMockArchiveStorage(ms),
		files:   newMockFileStorage(),
		pointer: newMockCurrentStore(),
		phases:  newMockPhaseStore(),
	}
	cfg := &config.Config{TaskTypes: []string{"feature", "bug"}, RelationTypes: config.DefaultRelationTypes}
	f.svc = NewService(ms, f.archive, f.files, newMockIndex(), cfg.TaskTypes, cfg,
		WithCurrentTaskStore(f.pointer), WithPhaseStore(f.phases), WithIdentity(Identity{Name: "dev"}))
	// List the phase records next to the files, as the real store does.
	f.svc.fileStorage = phaseListingFiles{f.files, f.svc}
	if err := f.svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return f
}

// phaseHistory reads a task's phase records the way get_task does.
func phaseHistory(svc *Service, id string) ([]PhaseRecord, error) {
	d, err := svc.Detail(id)
	if err != nil {
		return nil, err
	}
	return d.Phases, nil
}

func (f *phaseFixture) create(t *testing.T, id, parentID string) *Task {
	t.Helper()
	created, err := f.svc.Create("Task "+id, "", PriorityHigh, "feature", parentID, id)
	if err != nil {
		t.Fatalf("Create(%s) error = %v", id, err)
	}
	return created
}

func (f *phaseFixture) start(t *testing.T, id string, p Phase) *PhaseRecord {
	t.Helper()
	_, rec, err := f.svc.StartPhase(id, p)
	if err != nil {
		t.Fatalf("StartPhase(%s, %s) error = %v", id, p, err)
	}
	return rec
}

func (f *phaseFixture) finish(t *testing.T, id string, p Phase) *PhaseRecord {
	t.Helper()
	_, rec, err := f.svc.FinishPhase(id, p, PhaseFinish{})
	if err != nil {
		t.Fatalf("FinishPhase(%s, %s) error = %v", id, p, err)
	}
	return rec
}

// runPhases starts and finishes each phase in turn.
func (f *phaseFixture) runPhases(t *testing.T, id string, phases ...Phase) {
	t.Helper()
	for _, p := range phases {
		f.start(t, id, p)
		f.finish(t, id, p)
	}
}

func (f *phaseFixture) status(t *testing.T, id string) Status {
	t.Helper()
	got, err := f.svc.Get(id)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", id, err)
	}
	return got.Status
}

func wantErrText(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("error = %v, want one containing %q", err, substr)
	}
}

func TestStartPhaseRules(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *phaseFixture) (id string, p Phase)
		want  string
	}{
		{"unknown phase", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			return "a", "testing"
		}, `unknown phase "testing" (want research, design, planning, implementation)`},
		{"not found", func(t *testing.T, f *phaseFixture) (string, Phase) {
			return "nope", PhaseResearch
		}, "task not found: nope"},
		{"archived", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			if _, err := f.svc.CompleteTask("a", WithResolution(ResolutionObsolete)); err != nil {
				t.Fatalf("CompleteTask() error = %v", err)
			}
			if err := f.svc.ArchiveTask("a"); err != nil {
				t.Fatalf("ArchiveTask() error = %v", err)
			}
			return "a", PhaseResearch
		}, "task a is archived; phases are read-only"},
		{"done", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			if _, err := f.svc.CompleteTask("a", WithResolution(ResolutionWontfix)); err != nil {
				t.Fatalf("CompleteTask() error = %v", err)
			}
			return "a", PhaseResearch
		}, "task a is done (resolution: wontfix); reopen it to todo before starting a phase"},
		{"open run of another phase", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			f.start(t, "a", PhaseResearch)
			return "a", PhaseDesign
		}, "phase research of task a is still open (started "},
		{"open run of the same phase", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			f.start(t, "a", PhaseResearch)
			return "a", PhaseResearch
		}, "by dev); finish_phase it first"},
		{"order", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			f.runPhases(t, "a", PhaseResearch)
			return "a", PhasePlanning
		}, "cannot start planning on task a: phase design has no finished run"},
		{"order on a fresh task", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			return "a", PhaseDesign
		}, "cannot start design on task a: phase research has no finished run"},
		{"parent done", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "p", "")
			f.create(t, "s", "p")
			if _, err := f.svc.CompleteTask("p", WithResolution(ResolutionObsolete)); err != nil {
				t.Fatalf("CompleteTask(p) error = %v", err)
			}
			// The cascade closed s too; reopen it alone.
			todo := StatusTodo
			if _, err := f.svc.Update("s", nil, nil, &todo, nil, nil); err != nil {
				t.Fatalf("Update(s) error = %v", err)
			}
			return "s", PhaseResearch
		}, "parent task p is done; reopen it before starting a phase of s"},
		{"blocked todo", func(t *testing.T, f *phaseFixture) (string, Phase) {
			f.create(t, "a", "")
			f.create(t, "b", "")
			if err := f.svc.AddRelation("a", "blocked_by", "b"); err != nil {
				t.Fatalf("AddRelation() error = %v", err)
			}
			return "a", PhaseResearch
		}, "task a is blocked by tasks: b (todo)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPhaseFixture(t)
			id, p := tt.setup(t, f)
			before := len(f.phases.recs)
			_, _, err := f.svc.StartPhase(id, p)
			wantErrText(t, err, tt.want)
			if len(f.phases.recs) != before {
				t.Errorf("a refused start changed the phase records")
			}
		})
	}
}

func TestStartPhaseBlockedInProgressAllowed(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.create(t, "b", "")
	f.runPhases(t, "a", PhaseResearch)
	if err := f.svc.AddRelation("a", "blocked_by", "b"); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}
	// Already in progress: the start does not leave todo, so blockers do
	// not stop it.
	f.start(t, "a", PhaseDesign)
}

func TestStartPhaseReRunAppends(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.runPhases(t, "a", PhaseResearch, PhaseDesign, PhasePlanning)
	rec := f.start(t, "a", PhaseDesign) // a re-run after a later phase ran
	if len(rec.Runs) != 2 || !rec.Runs[1].Open() || rec.Runs[0].Open() {
		t.Fatalf("design runs = %+v, want one finished and one open", rec.Runs)
	}
	if got, _ := f.phases.LoadPhase("a", PhasePlanning); len(got.Runs) != 1 || got.Runs[0].Open() {
		t.Errorf("a re-run touched a later phase: %+v", got)
	}
}

func TestStartPhaseMigrationEscape(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	_ = f.files.WriteFile("a", "task", "x")
	_ = f.files.WriteFile("a", "research", "x")
	// No .phase file yet, and the research artifact proves design.
	f.start(t, "a", PhaseDesign)
	f.finish(t, "a", PhaseDesign)

	// Now a record exists: ordering is strict again.
	_ = f.files.WriteFile("a", "plan", "x")
	_, _, err := f.svc.StartPhase("a", PhaseImplementation)
	wantErrText(t, err, "phase planning has no finished run")

	// The escape never reaches beyond what the artifacts prove.
	f.create(t, "b", "")
	_ = f.files.WriteFile("b", "task", "x")
	_, _, err = f.svc.StartPhase("b", PhaseDesign)
	wantErrText(t, err, "phase research has no finished run")
}

func TestStartPhaseResearchMovesTodoToInProgress(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	started, rec, err := f.svc.StartPhase("a", PhaseResearch)
	if err != nil {
		t.Fatalf("StartPhase() error = %v", err)
	}
	if started.Status != StatusInProgress {
		t.Errorf("status = %s, want in_progress", started.Status)
	}
	if id := f.pointer.ids["dev"]; id != "a" {
		t.Errorf("pointer = %q, want a", id)
	}
	if len(rec.Runs) != 1 || rec.Runs[0].StartedBy != "dev" || !rec.Runs[0].Open() || rec.Runs[0].StartedAt.IsZero() {
		t.Errorf("record = %+v, want one open run started by dev", rec)
	}
}

func TestStartPhaseSubtaskAutoStartsTodoParent(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "p", "")
	f.create(t, "s", "p")
	f.start(t, "s", PhaseResearch)
	if got := f.status(t, "p"); got != StatusInProgress {
		t.Errorf("parent status = %s, want in_progress", got)
	}
	if got := f.status(t, "s"); got != StatusInProgress {
		t.Errorf("subtask status = %s, want in_progress", got)
	}
	if rec, _ := f.phases.LoadPhase("p", PhaseResearch); rec != nil {
		t.Errorf("the parent got a phase run: %+v", rec)
	}
	if id := f.pointer.ids["dev"]; id != "s" {
		t.Errorf("pointer = %q, want s", id)
	}
}

func TestStartPhaseInProgressNoStatusChange(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.create(t, "b", "")
	f.runPhases(t, "a", PhaseResearch)
	f.start(t, "b", PhaseResearch) // moves the pointer away
	before, _ := f.svc.Get("a")

	f.start(t, "a", PhaseDesign)
	after, _ := f.svc.Get("a")
	if after.Status != StatusInProgress || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("in-progress task changed: status %s, updated %v -> %v", after.Status, before.UpdatedAt, after.UpdatedAt)
	}
	if id := f.pointer.ids["dev"]; id != "a" {
		t.Errorf("pointer = %q, want a", id)
	}
}

func TestStartPhaseDefaultsToCurrentTask(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	_, _, err := f.svc.StartPhase("", PhaseResearch)
	wantErrText(t, err, "no task id given and no current task; pass id")
	_, _, err = f.svc.FinishPhase("", PhaseResearch, PhaseFinish{})
	wantErrText(t, err, "no task id given and no current task; pass id")

	f.start(t, "a", PhaseResearch)
	if _, rec, err := f.svc.FinishPhase("", PhaseResearch, PhaseFinish{}); err != nil || rec.Runs[0].Open() {
		t.Fatalf("FinishPhase(current) = %+v, %v", rec, err)
	}
	got, _, err := f.svc.StartPhase("", PhaseDesign)
	if err != nil || got.ID != "a" {
		t.Fatalf("StartPhase(current) = %v, %v; want task a", got, err)
	}
}

func TestFinishPhaseRules(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	_, _, err := f.svc.FinishPhase("a", PhaseResearch, PhaseFinish{})
	wantErrText(t, err, "phase research of task a is not open (never started)")

	f.start(t, "a", PhaseResearch)
	for _, n := range []int64{-1, maxPhaseTokens + 1} {
		_, _, err = f.svc.FinishPhase("a", PhaseResearch, PhaseFinish{Tokens: &n})
		wantErrText(t, err, "tokens must be a non-negative integer")
	}
	zero := int64(0)
	_, rec, err := f.svc.FinishPhase("a", PhaseResearch, PhaseFinish{Tokens: &zero, Note: "quick"})
	if err != nil {
		t.Fatalf("FinishPhase() error = %v", err)
	}
	run := rec.Runs[0]
	if run.Open() || run.FinishedBy != "dev" || run.Tokens == nil || *run.Tokens != 0 || run.Note != "quick" {
		t.Errorf("finished run = %+v", run)
	}

	_, _, err = f.svc.FinishPhase("a", PhaseResearch, PhaseFinish{})
	wantErrText(t, err, "phase research of task a is not open (last run finished ")

	_, _, err = f.svc.FinishPhase("a", "bogus", PhaseFinish{})
	wantErrText(t, err, `unknown phase "bogus"`)
}

func TestFinishPhaseOnDoneTaskAndArchivedRefused(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	// update_task to done leaves the run open; finish_phase still closes it.
	done := StatusDone
	if _, err := f.svc.Update("a", nil, nil, &done, nil, nil); err != nil {
		t.Fatalf("Update(done) error = %v", err)
	}
	f.finish(t, "a", PhaseResearch)

	if err := f.svc.ArchiveTask("a"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	_, _, err := f.svc.FinishPhase("a", PhaseResearch, PhaseFinish{})
	wantErrText(t, err, "task a is archived; phases are read-only")
}

func TestFinishPhaseKeepsStatusAndPointer(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.create(t, "b", "")
	f.start(t, "a", PhaseResearch)
	f.start(t, "b", PhaseResearch) // pointer now on b
	before, _ := f.svc.Get("a")
	f.finish(t, "a", PhaseResearch)
	after, _ := f.svc.Get("a")
	if after.Status != StatusInProgress || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("FinishPhase changed the task: %+v", after)
	}
	if id := f.pointer.ids["dev"]; id != "b" {
		t.Errorf("pointer = %q, want b (left alone)", id)
	}
}

func TestPhaseHistoryOrderAndArchived(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.runPhases(t, "a", PhaseResearch, PhaseDesign)
	f.start(t, "a", PhasePlanning)

	recs, err := phaseHistory(f.svc, "a")
	if err != nil {
		t.Fatalf("PhaseHistory() error = %v", err)
	}
	var got []Phase
	for _, r := range recs {
		got = append(got, r.Phase)
	}
	if len(got) != 3 || got[0] != PhaseResearch || got[1] != PhaseDesign || got[2] != PhasePlanning {
		t.Errorf("phases = %v, want research, design, planning", got)
	}

	f.finish(t, "a", PhasePlanning)
	if _, err := f.svc.CompleteTask("a", WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if err := f.svc.ArchiveTask("a"); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	if recs, err := phaseHistory(f.svc, "a"); err != nil || len(recs) != 3 {
		t.Errorf("PhaseHistory(archived) = %d records, %v; want 3", len(recs), err)
	}
	if _, err := phaseHistory(f.svc, "nope"); err == nil {
		t.Error("PhaseHistory(unknown) succeeded")
	}
}

func TestPhaseToolsWithoutStore(t *testing.T) {
	svc, _ := newPointerService()
	mustCreate(t, svc, "A", "")
	_, _, err := svc.StartPhase("1", PhaseResearch)
	wantErrText(t, err, "phase tracking is not available for this project")
	_, _, err = svc.FinishPhase("1", PhaseResearch, PhaseFinish{})
	wantErrText(t, err, "phase tracking is not available for this project")
	if recs, err := phaseHistory(svc, "1"); recs != nil || err != nil {
		t.Errorf("PhaseHistory() without a store = %v, %v; want nil, nil", recs, err)
	}
}

func TestStartPhaseStoreFailureRollsBack(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "p", "")
	f.create(t, "s", "p")
	f.phases.failSave = true
	_, _, err := f.svc.StartPhase("s", PhaseResearch)
	wantErrText(t, err, "disk full")
	for _, id := range []string{"p", "s"} {
		if got := f.status(t, id); got != StatusTodo {
			t.Errorf("status of %s = %s after a failed start, want todo", id, got)
		}
	}
	if _, ok := f.pointer.ids["dev"]; ok {
		t.Errorf("pointer = %q after a failed start, want none", f.pointer.ids["dev"])
	}
	if len(f.phases.recs) != 0 {
		t.Errorf("phase records after a failed start: %v", f.phases.recs)
	}
}

func TestStartPhaseUnreadableRecordRefused(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	broken := &brokenPhaseStore{mockPhaseStore: f.phases, broken: PhaseDesign}
	f.svc.phases = broken
	_, _, err := f.svc.StartPhase("a", PhaseResearch)
	wantErrText(t, err, "cannot read design.phase of task a")
	wantErrText(t, err, "fix or delete it")
}

// brokenPhaseStore fails to read one phase's record, like a hand-mangled file.
type brokenPhaseStore struct {
	*mockPhaseStore
	broken Phase
}

func (b *brokenPhaseStore) LoadPhase(id string, p Phase) (*PhaseRecord, error) {
	if p == b.broken {
		return nil, errBrokenRecord
	}
	return b.mockPhaseStore.LoadPhase(id, p)
}

var errBrokenRecord = errors.New("yaml: cannot parse")

// requireRunsClosed fails unless every run of every record of id is
// finished.
func requireRunsClosed(t *testing.T, f *phaseFixture, id string) {
	t.Helper()
	recs, err := phaseHistory(f.svc, id)
	if err != nil {
		t.Fatalf("PhaseHistory(%s) error = %v", id, err)
	}
	if len(recs) == 0 {
		t.Fatalf("task %s has no phase records", id)
	}
	for _, rec := range recs {
		for i, run := range rec.Runs {
			if run.Open() {
				t.Errorf("%s run %d of %s is still open", rec.Phase, i+1, id)
			}
		}
	}
}

func TestCompleteClosesOpenRuns(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	if _, err := f.svc.CompleteTask("a"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	requireRunsClosed(t, f, "a")
	rec, _ := f.phases.LoadPhase("a", PhaseResearch)
	run := rec.Runs[0]
	if run.FinishedBy != "dev" || run.Tokens != nil || run.Note != "closed by complete_task (completed)" {
		t.Errorf("closed run = %+v", run)
	}
}

func TestCompleteClosesRunsOfCascadedSubtasks(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "p", "")
	f.create(t, "s", "p")
	f.start(t, "s", PhaseResearch)
	f.finish(t, "s", PhaseResearch)
	f.start(t, "s", PhaseDesign)
	if _, err := f.svc.CompleteTask("p", WithResolution(ResolutionObsolete)); err != nil {
		t.Fatalf("CompleteTask(p, obsolete) error = %v", err)
	}
	requireRunsClosed(t, f, "s")
	rec, _ := f.phases.LoadPhase("s", PhaseDesign)
	if note := rec.Runs[0].Note; note != "closed by complete_task (obsolete)" {
		t.Errorf("cascaded run note = %q", note)
	}
}

func TestCompleteClosesRunsOfAutoCompletedParent(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "p", "")
	f.create(t, "s", "p")
	f.start(t, "p", PhaseResearch)
	f.start(t, "s", PhaseResearch)
	if _, err := f.svc.CompleteTask("s"); err != nil {
		t.Fatalf("CompleteTask(s) error = %v", err)
	}
	if got := f.status(t, "p"); got != StatusDone {
		t.Fatalf("parent status = %s, want auto-completed", got)
	}
	requireRunsClosed(t, f, "p")
	requireRunsClosed(t, f, "s")
}

func TestCompleteLeavesFinishedRunsUntouched(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	n := int64(1234)
	f.start(t, "a", PhaseResearch)
	if _, _, err := f.svc.FinishPhase("a", PhaseResearch, PhaseFinish{Tokens: &n, Note: "mine"}); err != nil {
		t.Fatalf("FinishPhase() error = %v", err)
	}
	before, _ := f.phases.LoadPhase("a", PhaseResearch)
	if _, err := f.svc.CompleteTask("a"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	after, _ := f.phases.LoadPhase("a", PhaseResearch)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("a finished run changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestUpdateToDoneLeavesRunsOpen(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	done := StatusDone
	if _, err := f.svc.Update("a", nil, nil, &done, nil, nil); err != nil {
		t.Fatalf("Update(done) error = %v", err)
	}
	rec, _ := f.phases.LoadPhase("a", PhaseResearch)
	if !rec.Runs[0].Open() {
		t.Fatalf("update_task closed a run: %+v", rec.Runs[0])
	}
	f.finish(t, "a", PhaseResearch)
}

func TestCompleteStoreFailureRollsBack(t *testing.T) {
	f := newPhaseFixture(t)
	f.create(t, "a", "")
	f.start(t, "a", PhaseResearch)
	f.phases.failSave = true
	_, err := f.svc.CompleteTask("a")
	wantErrText(t, err, "disk full")
	f.phases.failSave = false
	if got := f.status(t, "a"); got != StatusInProgress {
		t.Errorf("status = %s after a failed completion, want in_progress", got)
	}
	if id := f.pointer.ids["dev"]; id != "a" {
		t.Errorf("pointer = %q after a failed completion, want a", id)
	}
	if rec, _ := f.phases.LoadPhase("a", PhaseResearch); !rec.Runs[0].Open() {
		t.Errorf("run closed by a failed completion: %+v", rec.Runs[0])
	}
}
