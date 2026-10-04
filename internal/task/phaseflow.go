package task

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"time"
)

// Phase records: each delivery phase of a task keeps the history of its
// runs in <phase>.phase, written only by start_phase, finish_phase and
// complete_task. Starting a phase also moves the task: research, design
// and planning start a todo task record-only, and implementation does
// what start_task does under git branching - it cuts or restarts the wip
// branch. Every start is one journaled flow, the phase file included.

var errNoPhaseStore = errors.New("phase tracking is not available for this project")

// ErrInvalidTokens is the one refusal of a reported token count, shared by
// the callers that convert it and the service that bounds it.
var ErrInvalidTokens = errors.New("tokens must be a non-negative integer")

// maxPhaseTokens bounds a reported token count: anything larger is a bug
// in the caller, not a cost.
const maxPhaseTokens = 1_000_000_000_000_000

// PhaseTimeFormat renders run times in messages and the CLI.
const PhaseTimeFormat = "2006-01-02 15:04"

// StartPhase starts a new run of phase p on task id, or on the calling
// user's current task when id is empty, and makes the task current.
func (s *Service) StartPhase(id string, p Phase) (*Task, *PhaseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startPhase(id, p)
}

// FinishPhase finishes the open run of phase p, recording f. It changes no
// status, pointer or branch.
func (s *Service) FinishPhase(id string, p Phase, f PhaseFinish) (*Task, *PhaseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finishPhase(id, p, f)
}

func (s *Service) startPhase(id string, p Phase) (*Task, *PhaseRecord, error) {
	if s.phases == nil {
		return nil, nil, errNoPhaseStore
	}
	if err := p.check(); err != nil {
		return nil, nil, err
	}
	t, err := s.phaseTask(id)
	if err != nil {
		return nil, nil, err
	}
	rec, err := s.checkPhaseStart(t, p)
	if err != nil {
		return nil, nil, err
	}

	var saved *PhaseRecord
	extra := func(txn *gitTxn) error {
		var err error
		saved, err = s.appendRun(txn, t.ID, p, rec)
		return err
	}
	var started *Task
	if p == PhaseImplementation {
		started, err = s.startWith(t, extra)
	} else {
		started, err = s.startPhaseRecords(t, extra)
	}
	if err != nil {
		return nil, nil, err
	}
	return started, saved, nil
}

// phaseTask resolves the task a phase call names: id, or the calling
// user's current task. Archived tasks are refused: their phases are
// history.
func (s *Service) phaseTask(id string) (*Task, error) {
	if id == "" {
		cur, ok, err := s.currentTask()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("no task id given and no current task; pass id")
		}
		id = cur.ID
	}
	return s.activeTask(id, "phases")
}

// checkPhaseStart enforces the start rules, in order, before anything
// changes: not done, no open run, the previous phase finished, a parent
// that is not done, and no blockers on a todo task. It returns t's record
// for p as loaded, nil when there is none. The git preconditions of an
// implementation start are checked by its flow.
func (s *Service) checkPhaseStart(t *Task, p Phase) (*PhaseRecord, error) {
	if t.Status == StatusDone {
		return nil, fmt.Errorf("task %s is done (resolution: %s); reopen it to todo before starting a phase", t.ID, t.EffectiveResolution())
	}
	names, err := s.listFiles(t.ID)
	if err != nil {
		return nil, err
	}
	recs, err := s.loadPhases(t.ID, names)
	if err != nil {
		return nil, err
	}
	if op, run := openRun(recs); run != nil {
		return nil, fmt.Errorf("phase %s of task %s is still open (started %s); finish_phase it first", op, t.ID, RunStamp(run.StartedAt, run.StartedBy))
	}
	if prev, ok := p.Prev(); ok && !hasFinishedRun(recordOf(recs, prev)) {
		// Migration escape: a task that never had a phase record may start
		// any phase its workflow artifacts prove it has reached.
		if len(recs) > 0 || phaseFromFiles(names).Order() < p.Order() {
			return nil, fmt.Errorf("cannot start %s on task %s: phase %s has no finished run", p, t.ID, prev)
		}
	}
	if t.ParentID != "" {
		parent, err := s.get(t.ParentID)
		if err != nil {
			return nil, fmt.Errorf("parent task not found: %s", t.ParentID)
		}
		if parent.Status == StatusDone {
			return nil, fmt.Errorf("parent task %s is done; reopen it before starting a phase of %s", parent.ID, t.ID)
		}
	}
	if t.Status == StatusTodo {
		if err := s.blockedError(t.ID); err != nil {
			return nil, err
		}
	}
	return recordOf(recs, p), nil
}

// startPhaseRecords is the record-only start of a phase: a todo task (and
// a todo parent) moves to in_progress, the user is pointed at the task, and
// extra appends the run. No git, even with branching on.
func (s *Service) startPhaseRecords(t *Task, extra flowStep) (*Task, error) {
	if t.Status == StatusTodo {
		return s.startPlain(t, extra)
	}
	var txn gitTxn
	return runFlow(&txn, func() (*Task, error) {
		return t, s.pointAndRun(&txn, t.ID, extra)
	})
}

// closeOpenRuns finishes every open phase run of the tasks ids, journaled:
// stamped now by the calling user, with note why and no tokens - the
// server cannot know them. A record that cannot be read is left as it is
// rather than failing the completion. A no-op without a phase store.
func (s *Service) closeOpenRuns(txn *gitTxn, ids []string, why string) error {
	if s.phases == nil {
		return nil
	}
	for _, id := range ids {
		recs, err := s.loadPhases(id, s.attachedFiles(id))
		if err != nil {
			log.Printf("warning: leaving a phase run open: %v", err)
		}
		for i := range recs {
			rec := &recs[i]
			last := rec.Last()
			if last == nil || !last.Open() {
				continue
			}
			s.capturePhase(txn, id, rec.Phase, rec)
			last.finish(s.identity.Name, why, nil)
			if err := s.phases.SavePhase(id, rec); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendRun appends a new run of p, started now by the calling user, to
// rec - task id's record as loaded, nil when it has none - journaled, and
// returns the record it saved.
func (s *Service) appendRun(txn *gitTxn, id string, p Phase, rec *PhaseRecord) (*PhaseRecord, error) {
	s.capturePhase(txn, id, p, rec)
	if rec == nil {
		rec = &PhaseRecord{Phase: p}
	}
	rec.Runs = append(rec.Runs, PhaseRun{StartedAt: phaseNow(), StartedBy: s.identity.Name})
	if err := s.phases.SavePhase(id, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *Service) finishPhase(id string, p Phase, f PhaseFinish) (*Task, *PhaseRecord, error) {
	if s.phases == nil {
		return nil, nil, errNoPhaseStore
	}
	if err := p.check(); err != nil {
		return nil, nil, err
	}
	t, err := s.phaseTask(id)
	if err != nil {
		return nil, nil, err
	}
	rec, err := s.loadPhase(t.ID, p)
	if err != nil {
		return nil, nil, err
	}
	last := rec.Last()
	switch {
	case last == nil:
		return nil, nil, fmt.Errorf("phase %s of task %s is not open (never started)", p, t.ID)
	case !last.Open():
		return nil, nil, fmt.Errorf("phase %s of task %s is not open (last run finished %s)", p, t.ID, last.FinishedAt.Format(PhaseTimeFormat))
	}
	if f.Tokens != nil && (*f.Tokens < 0 || *f.Tokens > maxPhaseTokens) {
		return nil, nil, ErrInvalidTokens
	}

	last.finish(s.identity.Name, f.Note, f.Tokens)
	if err := s.phases.SavePhase(t.ID, rec); err != nil {
		return nil, nil, err
	}
	return t, rec, nil
}

// loadPhases is the one loader of a task's phase records: it reads the
// <phase>.phase files among names - task id's attached file names, listed
// once by the caller - in workflow order. A record that cannot be read is
// left out and reported in the joined error, next to the records that
// could; each caller picks its own policy. Nil without a phase store.
// Caller holds s.mu.
func (s *Service) loadPhases(id string, names []string) ([]PhaseRecord, error) {
	if s.phases == nil {
		return nil, nil
	}
	var recs []PhaseRecord
	var errs []error
	for _, p := range Phases() {
		if !slices.Contains(names, PhaseFileName(p)) {
			continue
		}
		rec, err := s.loadPhase(id, p)
		switch {
		case err != nil:
			errs = append(errs, err)
		case rec != nil:
			recs = append(recs, *rec)
		}
	}
	return recs, errors.Join(errs...)
}

// loadPhase reads one record, wording a read failure so the caller knows
// the tools will not overwrite the file.
func (s *Service) loadPhase(id string, p Phase) (*PhaseRecord, error) {
	rec, err := s.phases.LoadPhase(id, p)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s of task %s: %w; fix or delete it", PhaseFileName(p), id, err)
	}
	return rec, nil
}

// recordOf returns p's record among recs, nil when there is none.
func recordOf(recs []PhaseRecord, p Phase) *PhaseRecord {
	for i := range recs {
		if recs[i].Phase == p {
			return &recs[i]
		}
	}
	return nil
}

// openRun returns the open run among recs and its phase, if any.
func openRun(recs []PhaseRecord) (Phase, *PhaseRun) {
	for i := range recs {
		if last := recs[i].Last(); last != nil && last.Open() {
			return recs[i].Phase, last
		}
	}
	return "", nil
}

// hasFinishedRun reports whether rec has at least one finished run.
func hasFinishedRun(rec *PhaseRecord) bool {
	if rec == nil {
		return false
	}
	for _, run := range rec.Runs {
		if !run.Open() {
			return true
		}
	}
	return false
}

// RunStamp renders "<time> by <user>" in PhaseTimeFormat, leaving the user
// out when unknown.
func RunStamp(at time.Time, by string) string {
	if by == "" {
		return at.Format(PhaseTimeFormat)
	}
	return at.Format(PhaseTimeFormat) + " by " + by
}
