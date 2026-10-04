package task_test

import (
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// The phase flows under git branching. Research, design and planning are
// record-only; implementation reuses start_task's branch flows.

// phaseHistory reads a task's phase records the way get_task does.
func phaseHistory(svc *task.Service, id string) ([]task.PhaseRecord, error) {
	d, err := svc.Detail(id)
	if err != nil {
		return nil, err
	}
	return d.Phases, nil
}

func mustStartPhase(t *testing.T, b *testsupport.GitBacklog, id string, p task.Phase) (*task.Task, *task.PhaseRecord) {
	t.Helper()
	started, rec, err := b.Svc.StartPhase(id, p)
	if err != nil {
		t.Fatalf("StartPhase(%s, %s) error = %v", id, p, err)
	}
	return started, rec
}

func mustFinishPhase(t *testing.T, b *testsupport.GitBacklog, id string, p task.Phase) {
	t.Helper()
	if _, _, err := b.Svc.FinishPhase(id, p, task.PhaseFinish{}); err != nil {
		t.Fatalf("FinishPhase(%s, %s) error = %v", id, p, err)
	}
}

// throughPlanning runs research, design and planning on id.
func throughPlanning(t *testing.T, b *testsupport.GitBacklog, id string) {
	t.Helper()
	for _, p := range []task.Phase{task.PhaseResearch, task.PhaseDesign, task.PhasePlanning} {
		mustStartPhase(t, b, id, p)
		mustFinishPhase(t, b, id, p)
	}
}

func TestStartPhaseNonImplementationNoGitCalls(t *testing.T) {
	b, fg, _ := newFaultBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	refs := b.Git(t, "for-each-ref")

	throughPlanning(t, b, "alpha")

	if fg.calls != 0 {
		t.Errorf("%d mutating git calls (%v), want none", fg.calls, fg.ops)
	}
	if head := b.Head(t); head != "main_patched" {
		t.Errorf("HEAD = %q, want main_patched", head)
	}
	if got := b.Git(t, "for-each-ref"); got != refs {
		t.Errorf("refs changed:\n%s", got)
	}
	got := mustGet(t, b, "alpha")
	if got.Status != task.StatusInProgress || got.Branch != "" {
		t.Errorf("alpha = status %s, branch %q; want in_progress without a branch", got.Status, got.Branch)
	}
	requirePointer(t, b, "alpha")
}

func TestStartPhaseImplementationFresh(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	baseTip := b.Git(t, "rev-parse", "main_patched")
	throughPlanning(t, b, "alpha")

	started, rec := mustStartPhase(t, b, "alpha", task.PhaseImplementation)
	if started.Branch != "dev/wip/alpha" || started.BaseBranch != "main_patched" || started.StartCommit != baseTip {
		t.Errorf("branch fields = %q / %q / %q", started.Branch, started.BaseBranch, started.StartCommit)
	}
	if head := b.Head(t); head != "dev/wip/alpha" {
		t.Errorf("HEAD = %q, want the wip branch", head)
	}
	if len(rec.Runs) != 1 || !rec.Runs[0].Open() {
		t.Errorf("implementation record = %+v, want one open run", rec)
	}
	requirePointer(t, b, "alpha")
	requireNoServerCommitInTasks(t, b)
}

// A todo subtask whose artifacts already prove planning (the migration
// escape) starts implementation directly: its todo parent's wip is cut in
// the same flow, exactly as start_task does.
func TestStartPhaseImplementationSubtaskTodoParent(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "par", "")
	mustCreate(t, b, "sub", "par")
	if err := b.Svc.WriteTaskFile("sub", "plan", "the plan"); err != nil {
		t.Fatalf("WriteTaskFile() error = %v", err)
	}

	started, _ := mustStartPhase(t, b, "sub", task.PhaseImplementation)
	par := mustGet(t, b, "par")
	if par.Status != task.StatusInProgress || par.Branch != "dev/wip/par" {
		t.Errorf("parent = status %s, branch %q", par.Status, par.Branch)
	}
	if started.Branch != "dev/wip/par--sub" || started.BaseBranch != "dev/wip/par" {
		t.Errorf("subtask branch = %q off %q", started.Branch, started.BaseBranch)
	}
	if head := b.Head(t); head != "dev/wip/par--sub" {
		t.Errorf("HEAD = %q, want the sub-wip", head)
	}
	if rec, _ := phaseHistory(b.Svc, "par"); len(rec) != 0 {
		t.Errorf("the parent got phase records: %+v", rec)
	}
}

func TestStartPhaseImplementationRestart(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	throughPlanning(t, b, "alpha")
	mustStartPhase(t, b, "alpha", task.PhaseImplementation)
	tip := commitCode(t, b, "a.txt", "a\n", "wip commit")
	mustFinishPhase(t, b, "alpha", task.PhaseImplementation)
	b.Git(t, "switch", "-q", "main_patched")

	started, rec := mustStartPhase(t, b, "alpha", task.PhaseImplementation)
	if head := b.Head(t); head != "dev/wip/alpha" {
		t.Errorf("HEAD = %q, want the wip branch back", head)
	}
	if got := b.Git(t, "rev-parse", "dev/wip/alpha"); got != tip {
		t.Errorf("wip tip = %s, want %s (nothing to replay)", got, tip)
	}
	if started.Status != task.StatusInProgress || len(rec.Runs) != 2 || !rec.Runs[1].Open() {
		t.Errorf("after restart: status %s, runs %+v", started.Status, rec.Runs)
	}
}

func TestStartPhaseImplementationDeadRefRefused(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	throughPlanning(t, b, "alpha")
	mustStartPhase(t, b, "alpha", task.PhaseImplementation)
	mustFinishPhase(t, b, "alpha", task.PhaseImplementation)
	b.Git(t, "switch", "-q", "main_patched")
	b.Git(t, "branch", "-q", "-D", "dev/wip/alpha")
	before := testsupport.CaptureState(t, b)

	_, _, err := b.Svc.StartPhase("alpha", task.PhaseImplementation)
	wantErr(t, err, "task alpha's branch dev/wip/alpha no longer exists; reopen it to todo and start_phase implementation to cut a new one")
	testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
}

func TestStartPhaseImplementationWithoutBranching(t *testing.T) {
	_, svc, _ := testsupport.NewBacklog(t)
	testsupport.Seed(t, svc, testsupport.TaskSpec{ID: "alpha", Title: "Alpha"})
	for _, p := range []task.Phase{task.PhaseResearch, task.PhaseDesign, task.PhasePlanning} {
		if _, _, err := svc.StartPhase("alpha", p); err != nil {
			t.Fatalf("StartPhase(%s) error = %v", p, err)
		}
		if _, _, err := svc.FinishPhase("alpha", p, task.PhaseFinish{}); err != nil {
			t.Fatalf("FinishPhase(%s) error = %v", p, err)
		}
	}
	started, rec, err := svc.StartPhase("alpha", task.PhaseImplementation)
	if err != nil {
		t.Fatalf("StartPhase(implementation) error = %v", err)
	}
	if started.Branch != "" || started.Status != task.StatusInProgress || len(rec.Runs) != 1 {
		t.Errorf("record-only implementation start = branch %q, status %s, runs %d", started.Branch, started.Status, len(rec.Runs))
	}
}

func TestStartPhaseFaultInjection(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("fresh from a dirty wip", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				mustCreate(t, b, "other", "")
				mustCreate(t, b, "alpha", "")
				throughPlanning(t, b, "alpha")
				mustStart(t, b, "other")
				writeCode(t, b, "other.txt", "other work\n")
			}, func(b *testsupport.GitBacklog) error {
				_, _, err := b.Svc.StartPhase("alpha", task.PhaseImplementation)
				return err
			})
		})

		t.Run("restart onto a moved base", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				testsupport.RequireGitReplay(t)
				mustCreate(t, b, "alpha", "")
				throughPlanning(t, b, "alpha")
				mustStartPhase(t, b, "alpha", task.PhaseImplementation)
				commitCode(t, b, "a.txt", "a\n", "wip commit")
				mustFinishPhase(t, b, "alpha", task.PhaseImplementation)
				b.Git(t, "switch", "-q", "main_patched")
				commitCode(t, b, "m.txt", "base\n", "base moves on")
			}, func(b *testsupport.GitBacklog) error {
				_, _, err := b.Svc.StartPhase("alpha", task.PhaseImplementation)
				return err
			})
		})
	})
}

// TestStartPhaseStoreFault fails the phase-file write of each start flow
// and requires the whole state - refs, HEAD, records, pointer, files - to
// be as it was.
func TestStartPhaseStoreFault(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, b *testsupport.GitBacklog)
		phase task.Phase
	}{
		{"record-only research", func(t *testing.T, b *testsupport.GitBacklog) {
			mustCreate(t, b, "par", "")
			mustCreate(t, b, "alpha", "par")
		}, task.PhaseResearch},
		{"fresh implementation", func(t *testing.T, b *testsupport.GitBacklog) {
			mustCreate(t, b, "alpha", "")
			throughPlanning(t, b, "alpha")
		}, task.PhaseImplementation},
		{"restart implementation", func(t *testing.T, b *testsupport.GitBacklog) {
			mustCreate(t, b, "alpha", "")
			throughPlanning(t, b, "alpha")
			mustStartPhase(t, b, "alpha", task.PhaseImplementation)
			commitCode(t, b, "a.txt", "a\n", "wip commit")
			mustFinishPhase(t, b, "alpha", task.PhaseImplementation)
			b.Git(t, "switch", "-q", "main_patched")
		}, task.PhaseImplementation},
	}
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				b, _, fp := newPhaseFaultBacklog(t, layout)
				c.setup(t, b)
				before := testsupport.CaptureState(t, b)
				fp.armed = true

				_, _, err := b.Svc.StartPhase("alpha", c.phase)
				wantErr(t, err, "SavePhase")
				testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
			})
		}
	})
}

// cutParentSetup puts parent par in its own research phase (in progress,
// no branch) and walks subtask sub to the end of planning.
func cutParentSetup(t *testing.T, b *testsupport.GitBacklog) {
	t.Helper()
	mustCreate(t, b, "par", "")
	mustCreate(t, b, "sub", "par")
	mustStartPhase(t, b, "par", task.PhaseResearch)
	throughPlanning(t, b, "sub")
}

func TestStartPhaseImplementationCutsBranchlessParent(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	cutParentSetup(t, b)
	baseTip := b.Git(t, "rev-parse", "main_patched")
	if par := mustGet(t, b, "par"); par.Status != task.StatusInProgress || par.Branch != "" {
		t.Fatalf("setup: parent = status %s, branch %q", par.Status, par.Branch)
	}

	started, rec := mustStartPhase(t, b, "sub", task.PhaseImplementation)
	par := mustGet(t, b, "par")
	if par.Status != task.StatusInProgress || par.Branch != "dev/wip/par" ||
		par.BaseBranch != "main_patched" || par.StartCommit != baseTip {
		t.Errorf("parent = status %s, branch %q off %q at %s", par.Status, par.Branch, par.BaseBranch, par.StartCommit)
	}
	if got := b.Git(t, "rev-parse", "dev/wip/par"); got != baseTip {
		t.Errorf("parent wip at %s, want the base tip %s", got, baseTip)
	}
	if started.Branch != "dev/wip/par--sub" || started.BaseBranch != "dev/wip/par" || started.StartCommit != baseTip {
		t.Errorf("subtask = %q off %q at %s", started.Branch, started.BaseBranch, started.StartCommit)
	}
	if head := b.Head(t); head != "dev/wip/par--sub" {
		t.Errorf("HEAD = %q, want the sub-wip", head)
	}
	requirePointer(t, b, "sub")
	if len(rec.Runs) != 1 || !rec.Runs[0].Open() {
		t.Errorf("subtask implementation record = %+v", rec)
	}
	// The parent keeps its own open research run and gets no other.
	recs, _ := phaseHistory(b.Svc, "par")
	if len(recs) != 1 || recs[0].Phase != task.PhaseResearch {
		t.Errorf("parent records = %+v, want research only", recs)
	}
}

// The parent's own implementation start later finds the wip a subtask cut
// and restarts on it.
func TestStartPhaseParentLaterRestarts(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	cutParentSetup(t, b)
	mustStartPhase(t, b, "sub", task.PhaseImplementation)
	commitCode(t, b, "s.txt", "s\n", "sub work")
	mustFinishPhase(t, b, "sub", task.PhaseImplementation)
	if _, err := b.Svc.CompleteTask("sub"); err != nil {
		t.Fatalf("CompleteTask(sub) error = %v", err)
	}
	parTip := b.Git(t, "rev-parse", "dev/wip/par")

	mustFinishPhase(t, b, "par", task.PhaseResearch)
	for _, p := range []task.Phase{task.PhaseDesign, task.PhasePlanning} {
		mustStartPhase(t, b, "par", p)
		mustFinishPhase(t, b, "par", p)
	}
	started, _ := mustStartPhase(t, b, "par", task.PhaseImplementation)
	if started.Branch != "dev/wip/par" {
		t.Errorf("parent branch = %q, want the one the subtask cut", started.Branch)
	}
	if head := b.Head(t); head != "dev/wip/par" {
		t.Errorf("HEAD = %q, want the parent wip", head)
	}
	if got := b.Git(t, "rev-parse", "dev/wip/par"); got != parTip {
		t.Errorf("parent wip moved from %s to %s; the restart has nothing to replay", parTip, got)
	}
}

func TestStartTaskInProgressWithoutBranchHint(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	mustStartPhase(t, b, "alpha", task.PhaseResearch)
	_, err := b.Svc.StartTask("alpha")
	wantErr(t, err, "task alpha is in progress without a branch; use start_phase with phase implementation to cut it")
}

func TestStartPhaseCutParentFaultInjection(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
			cutParentSetup(t, b)
			writeCode(t, b, "carried.txt", "carried onto the new wip\n")
		}, func(b *testsupport.GitBacklog) error {
			_, _, err := b.Svc.StartPhase("sub", task.PhaseImplementation)
			return err
		})

		t.Run("phase store", func(t *testing.T) {
			b, _, fp := newPhaseFaultBacklog(t, layout)
			cutParentSetup(t, b)
			before := testsupport.CaptureState(t, b)
			fp.armed = true
			_, _, err := b.Svc.StartPhase("sub", task.PhaseImplementation)
			wantErr(t, err, "SavePhase")
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})
	})
}

// requireNoOpenRun fails if any phase run of id is still open.
func requireNoOpenRun(t *testing.T, b *testsupport.GitBacklog, id string) {
	t.Helper()
	recs, err := phaseHistory(b.Svc, id)
	if err != nil {
		t.Fatalf("PhaseHistory(%s) error = %v", id, err)
	}
	for _, rec := range recs {
		if last := rec.Last(); last != nil && last.Open() {
			t.Errorf("%s run of %s is still open", rec.Phase, id)
		}
	}
}

// implementing walks alpha to an open implementation run with committed
// work on its wip branch.
func implementing(t *testing.T, b *testsupport.GitBacklog) {
	t.Helper()
	mustCreate(t, b, "alpha", "")
	throughPlanning(t, b, "alpha")
	mustStartPhase(t, b, "alpha", task.PhaseImplementation)
	commitCode(t, b, "a.txt", "a\n", "wip commit")
}

func TestCompleteDeliveredClosesImplementationRun(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	implementing(t, b)
	done, err := b.Svc.CompleteTask("alpha")
	if err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if done.FinalBranch != "dev/alpha" || b.Head(t) != "dev/alpha" {
		t.Errorf("final branch %q, HEAD %q", done.FinalBranch, b.Head(t))
	}
	requireNoOpenRun(t, b, "alpha")
}

func TestCompleteSubDeliveredClosesRun(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	cutParentSetup(t, b)
	mustStartPhase(t, b, "sub", task.PhaseImplementation)
	commitCode(t, b, "s.txt", "s\n", "sub work")
	if _, err := b.Svc.CompleteTask("sub"); err != nil {
		t.Fatalf("CompleteTask(sub) error = %v", err)
	}
	requireNoOpenRun(t, b, "sub")
	// The parent is still open, and so is its own research run.
	recs, _ := phaseHistory(b.Svc, "par")
	if len(recs) != 1 || !recs[0].Last().Open() {
		t.Errorf("parent records = %+v, want its research run still open", recs)
	}
}

func TestCompleteAbandonedClosesRun(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	implementing(t, b)
	writeCode(t, b, "left.txt", "uncommitted\n")
	if _, err := b.Svc.CompleteTask("alpha", task.WithResolution(task.ResolutionWontfix)); err != nil {
		t.Fatalf("CompleteTask(wontfix) error = %v", err)
	}
	if head := b.Head(t); head != "main_patched" {
		t.Errorf("HEAD = %q, want back on the base", head)
	}
	requireNoOpenRun(t, b, "alpha")
	rec, _ := phaseHistory(b.Svc, "alpha")
	if note := rec[len(rec)-1].Last().Note; note != "closed by complete_task (wontfix)" {
		t.Errorf("implementation run note = %q", note)
	}
}

func TestCompleteWithOpenRunsFaultInjection(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
			implementing(t, b)
			writeCode(t, b, "left.txt", "uncommitted\n")
		}, func(b *testsupport.GitBacklog) error {
			_, err := b.Svc.CompleteTask("alpha")
			return err
		})

		t.Run("phase store", func(t *testing.T) {
			b, _, fp := newPhaseFaultBacklog(t, layout)
			implementing(t, b)
			writeCode(t, b, "left.txt", "uncommitted\n")
			before := testsupport.CaptureState(t, b)
			fp.armed = true
			_, err := b.Svc.CompleteTask("alpha")
			wantErr(t, err, "SavePhase")
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})
	})
}
