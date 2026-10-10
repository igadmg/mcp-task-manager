package task

import (
	"fmt"
	"strings"
)

// completeBranched completes t with git branching on, when t has a branch.
// handled is false when the completion is record-only after all: a task
// closed without delivering, whose branch is not checked out, leaves git
// alone (design 5.8 case A).
func (s *Service) completeBranched(t *Task, subtasks []*Task, resolution Resolution, o updateOpts, opts []UpdateOption) (done *Task, handled bool, err error) {
	if resolution.Delivered() {
		if t.ParentID != "" {
			p, err := s.get(t.ParentID)
			if err != nil {
				return nil, true, fmt.Errorf("parent task not found: %s", t.ParentID)
			}
			if p.Branch != "" {
				done, err := s.completeSubDelivered(t, p, resolution, o, opts)
				return done, true, err
			}
		}
		done, err := s.completeDelivered(t, subtasks, resolution, o, opts)
		return done, true, err
	}

	// Any failure to read HEAD (detached, not a repository) means t's
	// branch is not checked out, and closing the record alone is safe.
	if head, _, err := s.git.Head(); err != nil || head != t.Branch {
		return nil, false, nil
	}
	done, err = s.completeAbandoned(t, subtasks, resolution, opts)
	return done, true, err
}

// preflightWip checks that t's wip branch is checked out and still grows
// from its recorded start commit.
func (s *Service) preflightWip(t *Task) (headState, error) {
	h, err := s.preflightGit()
	if err != nil {
		return headState{}, err
	}
	if h.branch != t.Branch {
		return headState{}, fmt.Errorf("task %s works on branch %s, but HEAD is %s; switch to %s first", t.ID, t.Branch, h.branch, t.Branch)
	}
	if err := s.checkStartCommit(t, h.sha, "restart task "+t.ID+" to rebase it"); err != nil {
		return headState{}, err
	}
	return h, nil
}

// subtaskGate refuses to deliver parent p while a subtask's delivered work
// is missing from p's wip branch at tip (design 5.7). Open subtasks are
// refused before this runs; subtasks closed without delivering, and done
// subtasks that never had a branch, need nothing.
func (s *Service) subtaskGate(p *Task, tip string) error {
	for _, sub := range s.index.GetSubtasks(p.ID) {
		if sub.Status != StatusDone {
			return fmt.Errorf("cannot complete task %s: subtask %s is %s", p.ID, sub.ID, sub.Status)
		}
		if sub.Branch == "" || !sub.EffectiveResolution().Delivered() {
			continue
		}
		if sub.SquashCommit == "" {
			return fmt.Errorf("subtask %s was completed on branch %s but recorded no squash commit; restart %s or merge it back", sub.ID, sub.Branch, sub.ID)
		}
		ok, err := s.git.IsAncestor(sub.SquashCommit, tip)
		if err != nil {
			return fmt.Errorf("checking subtask %s's work (%s) against %s: %w", sub.ID, ShortSHA(sub.SquashCommit), p.Branch, err)
		}
		if !ok {
			return fmt.Errorf("subtask %s was completed but its work (%s) is not in %s; it may have been reset away. Restart %s or merge it back",
				sub.ID, ShortSHA(sub.SquashCommit), p.Branch, sub.ID)
		}
	}
	return nil
}

// ShortSHA abbreviates a commit id for display: its first 12 characters.
func ShortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// commitMessageFor is t's squash commit message, honouring an override
// passed with WithCommitMessage.
func commitMessageFor(t *Task, o updateOpts) string {
	override := ""
	if o.commitMessage != nil {
		override = *o.commitMessage
	}
	return commitMessage(t, override)
}

// completeRecords closes t's records, prunes the pointer list and finishes
// the open phase runs of every task it closed, journaled.
func (s *Service) completeRecords(txn *gitTxn, t *Task, subtasks []*Task, resolution Resolution, opts []UpdateOption) (*Task, error) {
	done, closed, openParent, err := s.completeTaskRecords(txn, t, subtasks, resolution, opts)
	if err != nil {
		return nil, err
	}
	if err := s.prunePointer(txn, openParent, closed...); err != nil {
		return nil, err
	}
	return done, s.closeOpenRuns(txn, closed, fmt.Sprintf("closed by complete_task (%s)", resolution))
}

// completeDelivered squashes t's wip branch into a single commit on top of
// its start commit, on t's final branch, and switches to it (design 5.5).
// The wip branch keeps every commit, plus a snapshot of what was left
// uncommitted.
func (s *Service) completeDelivered(t *Task, subtasks []*Task, resolution Resolution, o updateOpts, opts []UpdateOption) (*Task, error) {
	h, err := s.preflightWip(t)
	if err != nil {
		return nil, err
	}
	final, err := finalBranch(t.Branch)
	if err != nil {
		return nil, err
	}
	oldFinal, finalExists, err := s.git.BranchSHA(final)
	if err != nil {
		return nil, err
	}
	if finalExists && t.FinalBranch != final {
		return nil, fmt.Errorf("branch %s exists and was not created by task %s", final, t.ID)
	}
	if !finalExists {
		if err := s.git.BranchAvailable(final); err != nil {
			return nil, err
		}
	}
	if err := s.subtaskGate(t, h.sha); err != nil {
		return nil, err
	}
	msg := commitMessageFor(t, o)

	var txn gitTxn
	done, err := runFlow(&txn, func() (*Task, error) {
		w, err := s.snapshotHead(&txn, h, completionSnapshotMsg(t.ID))
		if err != nil {
			return nil, err
		}
		tree, err := s.git.TreeOf(w)
		if err != nil {
			return nil, err
		}
		c, err := s.git.CommitTree(tree, t.StartCommit, msg)
		if err != nil {
			return nil, fmt.Errorf("squash commit for task %s: %w", t.ID, err)
		}
		if finalExists {
			if err := s.moveBranch(&txn, final, c, oldFinal); err != nil {
				return nil, err
			}
		} else if err := s.createBranch(&txn, final, c); err != nil {
			return nil, err
		}
		done, err := s.completeRecords(&txn, t, subtasks, resolution,
			append(opts, withBranch(branchInfo{FinalBranch: &final, SquashCommit: &c})))
		if err != nil {
			return nil, err
		}
		return done, s.switchTo(final)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(done), nil
}

// completeSubDelivered squash-merges subtask t's wip branch into its
// parent's as a single commit and switches to the parent's (design 5.6).
// The parent is not auto-completed: it is delivered by its own completion.
func (s *Service) completeSubDelivered(t, p *Task, resolution Resolution, o updateOpts, opts []UpdateOption) (*Task, error) {
	h, err := s.preflightWip(t)
	if err != nil {
		return nil, err
	}
	ptip, err := s.lineTip(p.Branch)
	if err != nil {
		return nil, fmt.Errorf("parent task %s: %w", p.ID, err)
	}
	msg := commitMessageFor(t, o)

	var txn gitTxn
	done, err := runFlow(&txn, func() (*Task, error) {
		w, err := s.snapshotHead(&txn, h, completionSnapshotMsg(t.ID))
		if err != nil {
			return nil, err
		}
		merged, conflicts, err := s.git.MergeTrees(t.StartCommit, ptip, w)
		if err != nil {
			return nil, err
		}
		if len(conflicts) > 0 {
			return nil, fmt.Errorf("squash-merging %s into %s conflicts in: %s; nothing was changed",
				t.Branch, p.Branch, strings.Join(conflicts, ", "))
		}
		c, err := s.git.CommitTree(merged, ptip, msg)
		if err != nil {
			return nil, fmt.Errorf("squash commit for task %s: %w", t.ID, err)
		}
		if err := s.moveBranch(&txn, p.Branch, c, ptip); err != nil {
			return nil, err
		}
		done, err := s.completeRecords(&txn, t, nil, resolution,
			append(opts, withBranch(branchInfo{SquashCommit: &c})))
		if err != nil {
			return nil, err
		}
		return done, s.switchTo(p.Branch)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(done), nil
}

// completeAbandoned closes t without delivering it while its branch is
// checked out (design 5.8 case B): everything left uncommitted is saved in
// a safety commit on the wip branch, and HEAD returns to where t branched
// from.
func (s *Service) completeAbandoned(t *Task, subtasks []*Task, resolution Resolution, opts []UpdateOption) (*Task, error) {
	h, err := s.preflightGit()
	if err != nil {
		return nil, err
	}
	target, err := s.returnTarget(t)
	if err != nil {
		return nil, err
	}

	var txn gitTxn
	done, err := runFlow(&txn, func() (*Task, error) {
		if _, err := s.snapshotHead(&txn, h, closedAsMsg(t.ID, resolution)); err != nil {
			return nil, err
		}
		done, err := s.completeRecords(&txn, t, subtasks, resolution, opts)
		if err != nil {
			return nil, err
		}
		return done, s.switchTo(target)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(done), nil
}

// returnTarget is where HEAD goes when t is closed without delivering: its
// parent's wip branch for a subtask, otherwise the branch t started from,
// or failing that the first existing base branch.
func (s *Service) returnTarget(t *Task) (string, error) {
	if t.ParentID != "" {
		p, err := s.get(t.ParentID)
		if err != nil {
			return "", fmt.Errorf("parent task not found: %s", t.ParentID)
		}
		if p.Branch == "" {
			return "", fmt.Errorf("no branch to return to: parent task %s has no branch", p.ID)
		}
		if _, err := s.lineTip(p.Branch); err != nil {
			return "", fmt.Errorf("no branch to return to: parent task %s: %w", p.ID, err)
		}
		return p.Branch, nil
	}
	base, _, err := s.baseFor(t)
	if err != nil {
		return "", fmt.Errorf("no branch to return to: %w", err)
	}
	return base, nil
}
