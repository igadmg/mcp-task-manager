package task

import (
	"fmt"
	"strings"
)

// restartTarget is the line a restarted wip branch is replayed onto.
type restartTarget struct {
	onto    string
	ontoTip string
}

// restartBranched starts t again on its existing wip branch (design 5.4):
// the branch is replayed onto the current tip of its parent line - the
// base branch, or the parent's wip for a subtask - and checked out.
// Nothing is lost: uncommitted work on t's own wip is checkpointed first,
// and a conflicting replay changes nothing at all. extra runs inside the
// flow, after the pointer moves and before the worktree does.
func (s *Service) restartBranched(t *Task, extra flowStep) (*Task, error) {
	if t.Status == StatusTodo {
		if err := s.blockedError(t.ID); err != nil {
			return nil, err
		}
	}
	h, err := s.preflightGit()
	if err != nil {
		return nil, err
	}
	oldTip, err := s.wipTip(t)
	if err != nil {
		return nil, err
	}

	// A subtask's target is only known once a todo parent has moved.
	var target restartTarget
	var line parentLine
	if t.ParentID == "" {
		target.onto, target.ontoTip, err = s.baseFor(t)
	} else {
		line, err = s.parentLineFor(t, h, false)
		target.onto = line.branch
		if err == nil && line.step == nil {
			target.ontoTip, err = s.lineTip(line.branch)
		}
	}
	if err != nil {
		return nil, err
	}

	owner, err := s.classifyVacate(h, "", t.ID, t.ID)
	if err != nil {
		return nil, err
	}
	onWip := h.branch == t.Branch
	// Already on its own branch with nothing to replay: the worktree stays
	// as it is, so there is nothing to checkpoint either.
	if onWip && line.step == nil && target.ontoTip == t.StartCommit {
		owner = ""
	}

	var txn gitTxn
	started, err := runFlow(&txn, func() (*Task, error) {
		w, err := s.checkpoint(&txn, h, owner, "restart "+t.ID)
		if err != nil {
			return nil, err
		}
		if onWip {
			oldTip = w
		}
		if line.step != nil {
			if err := line.step(&txn); err != nil {
				return nil, err
			}
		}
		if t.ParentID != "" {
			// A checkpoint on the parent's wip, or the parent's own
			// start, may just have moved it.
			if target.ontoTip, err = s.lineTip(line.branch); err != nil {
				return nil, err
			}
		}
		newTip, started, err := s.restartRecords(&txn, t, target, oldTip, onWip)
		if err != nil {
			return nil, err
		}
		if err := s.pointAndRun(&txn, t.ID, extra); err != nil {
			return nil, err
		}
		if !onWip {
			return started, s.switchTo(t.Branch)
		}
		if newTip == oldTip {
			return started, nil
		}
		// The rebased branch is the checked-out one: move it with the
		// worktree, never behind git's back.
		if err := s.git.ResetKeep(newTip); err != nil {
			return nil, fmt.Errorf("move %s to %s: %w", t.Branch, ShortSHA(newTip), err)
		}
		return started, nil
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(started), nil
}

// blockedError refuses to start a task with unresolved blockers.
func (s *Service) blockedError(id string) error {
	blocked, blockers := s.isBlocked(id)
	if !blocked {
		return nil
	}
	var parts []string
	for _, b := range blockers {
		parts = append(parts, fmt.Sprintf("%s (%s)", b.TaskID, b.Status))
	}
	return fmt.Errorf("task %s is blocked by tasks: %s", id, strings.Join(parts, ", "))
}

// wipTip returns the tip of t's wip branch after checking it still grows
// from t's recorded start commit.
func (s *Service) wipTip(t *Task) (string, error) {
	tip, err := s.lineTip(t.Branch)
	if err != nil {
		return "", fmt.Errorf("task %s: %w", t.ID, err)
	}
	if err := s.checkStartCommit(t, tip, "rebase it by hand or delete it to start task "+t.ID+" afresh"); err != nil {
		return "", err
	}
	return tip, nil
}

// checkStartCommit refuses when t's recorded start commit does not resolve
// or is no longer in the history of tip, the tip of t's wip branch. hint
// says what to do about the latter.
func (s *Service) checkStartCommit(t *Task, tip, hint string) error {
	if _, err := s.git.ResolveCommit(t.StartCommit); err != nil {
		return fmt.Errorf("the recorded start commit %q of task %s does not resolve: %w", t.StartCommit, t.ID, err)
	}
	ok, err := s.git.IsAncestor(t.StartCommit, tip)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the history of %s no longer contains its recorded start %s; %s", t.Branch, ShortSHA(t.StartCommit), hint)
	}
	return nil
}

// parentLine is the parent wip branch a subtask starts or restarts on.
type parentLine struct {
	// branch is the parent's wip branch, known before anything changes.
	branch string
	// step, when set, starts or restarts a todo parent inside the journal,
	// without switching to it.
	step func(txn *gitTxn) error
	// carryFrom is the branch whose uncommitted changes may be carried:
	// a todo parent starting fresh lands on the base tip, exactly where
	// its own fresh start would.
	carryFrom string
}

// parentLineFor resolves subtask t's parent line. A parent in progress
// must have a live wip branch - or, with cutParent, none at all, and then
// its wip is cut in the journal like a todo parent's; a todo parent is
// started fresh, or restarted in place when its branch still exists.
func (s *Service) parentLineFor(t *Task, h headState, cutParent bool) (parentLine, error) {
	p, err := s.get(t.ParentID)
	if err != nil {
		return parentLine{}, fmt.Errorf("parent task not found: %s", t.ParentID)
	}
	switch p.Status {
	case StatusDone:
		return parentLine{}, fmt.Errorf("parent task %s is done; reopen it before starting subtask %s", p.ID, t.ID)
	case StatusInProgress:
		if p.Branch == "" && cutParent {
			return s.freshParentLine(p)
		}
		if p.Branch == "" {
			return parentLine{}, fmt.Errorf("parent %s has no branch; start it first", p.ID)
		}
		if _, err := s.lineTip(p.Branch); err != nil {
			return parentLine{}, fmt.Errorf("parent task %s: %w", p.ID, err)
		}
		return parentLine{branch: p.Branch}, nil
	}

	if p.Branch != "" {
		if _, ok, err := s.git.BranchSHA(p.Branch); err != nil {
			return parentLine{}, err
		} else if ok {
			if h.branch == p.Branch {
				return parentLine{}, fmt.Errorf("parent task %s's branch %s is checked out; start %s first", p.ID, p.Branch, p.ID)
			}
			pOld, err := s.wipTip(p)
			if err != nil {
				return parentLine{}, err
			}
			var pTarget restartTarget
			pTarget.onto, pTarget.ontoTip, err = s.baseFor(p)
			if err != nil {
				return parentLine{}, err
			}
			return parentLine{branch: p.Branch, step: func(txn *gitTxn) error {
				if _, _, err := s.restartRecords(txn, p, pTarget, pOld, false); err != nil {
					return fmt.Errorf("restarting parent task %s: %w", p.ID, err)
				}
				return nil
			}}, nil
		}
	}

	return s.freshParentLine(p)
}

// freshParentLine cuts parent p's wip branch at the base tip inside the
// journal and records it on p, which leaves - or moves - p in progress.
// p gets no phase run.
func (s *Service) freshParentLine(p *Task) (parentLine, error) {
	plan, err := s.planFresh(p)
	if err != nil {
		return parentLine{}, fmt.Errorf("starting parent task %s: %w", p.ID, err)
	}
	return parentLine{branch: plan.wip, carryFrom: plan.base, step: func(txn *gitTxn) error {
		if _, err := s.startOnNewBranch(txn, p, plan.wip, plan.base, plan.baseTip); err != nil {
			return fmt.Errorf("starting parent task %s: %w", p.ID, err)
		}
		return nil
	}}, nil
}

// lineTip reads a branch's current tip inside a flow, after a checkpoint
// or a parent step may have moved it.
func (s *Service) lineTip(branch string) (string, error) {
	tip, ok, err := s.git.BranchSHA(branch)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("branch %s no longer exists", branch)
	}
	return tip, nil
}

// restartRecords replays t's wip branch (at oldTip) onto target, moves the
// branch unless it is checked out - the caller then moves it with the
// worktree - and records the new start. It returns the branch's new tip.
func (s *Service) restartRecords(txn *gitTxn, t *Task, target restartTarget, oldTip string, checkedOut bool) (string, *Task, error) {
	newTip := oldTip
	if target.ontoTip != t.StartCommit {
		tip, conflicts, err := s.git.Replay(target.ontoTip, t.StartCommit, t.Branch)
		switch {
		case err != nil:
			return "", nil, fmt.Errorf("rebasing %s onto %s: %w; nothing was changed", t.Branch, target.onto, err)
		case len(conflicts) > 0:
			return "", nil, fmt.Errorf("rebasing %s onto %s conflicts in: %s; nothing was changed",
				t.Branch, target.onto, strings.Join(conflicts, ", "))
		}
		newTip = tip
	}
	if !checkedOut && newTip != oldTip {
		if err := s.moveBranch(txn, t.Branch, newTip, oldTip); err != nil {
			return "", nil, err
		}
	}
	started, err := s.updateStatus(txn, t.ID, StatusInProgress, withBranch(branchInfo{
		BaseBranch:  &target.onto,
		StartCommit: &target.ontoTip,
	}))
	if err != nil {
		return "", nil, err
	}
	return newTip, started, nil
}
