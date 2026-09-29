package task

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// Every git flow has the same shape: preflight checks that change nothing,
// then ref, index and record/pointer mutations - each journaled with its
// undo - and last a single worktree-changing switch. A failure anywhere
// rolls the journal back, so a refused or failed call leaves refs, HEAD,
// index, worktree, records and pointer as they were.

// errNoEmail refuses branching without a git identity: the email names
// the branches, and a fallback name would scatter one user's work.
var errNoEmail = errors.New("git config user.email is not set; it names your branches")

// errRestartNotImplemented marks the restart flow, which arrives with
// branch rebasing.
var errRestartNotImplemented = errors.New("restart of a branched task is not implemented")

// maxListedPaths bounds the paths a refusal lists.
const maxListedPaths = 20

// headState is HEAD as a flow found it, for undoing a checkpoint.
type headState struct {
	branch string
	sha    string
	index  string // tree of the real index
}

// preflightGit runs the checks every git flow starts with and captures
// HEAD. A detached HEAD is refused.
func (s *Service) preflightGit() (headState, error) {
	if err := s.git.Check(); err != nil {
		return headState{}, err
	}
	if !s.identity.FromGitEmail {
		return headState{}, errNoEmail
	}
	branch, sha, err := s.git.Head()
	if err != nil {
		return headState{}, err
	}
	index, err := s.git.IndexTree()
	if err != nil {
		return headState{}, err
	}
	return headState{branch: branch, sha: sha, index: index}, nil
}

// baseBranches is the configured base branch priority list.
func (s *Service) baseBranches() []string {
	if s.config == nil {
		return config.DefaultBaseBranches
	}
	return s.config.Git.BaseBranches
}

// freshPlan is where a fresh top-level start branches from and what it
// creates.
type freshPlan struct {
	base    string
	baseTip string
	wip     string
	final   string
}

// planFresh picks the base and names t's branches, refusing names that are
// taken. The final branch may already exist only as t's own, left by an
// earlier completion of t.
func (s *Service) planFresh(t *Task) (freshPlan, error) {
	bases := s.baseBranches()
	base, tip, err := s.git.FirstExistingBranch(bases)
	if err != nil {
		return freshPlan{}, err
	}
	if base == "" {
		return freshPlan{}, fmt.Errorf("none of the base branches %v exist; set git.base_branches", bases)
	}
	wip := wipBranch(s.identity.Name, shortName(t))
	final, err := finalBranch(wip)
	if err != nil {
		return freshPlan{}, err
	}
	if err := s.git.BranchAvailable(wip); err != nil {
		return freshPlan{}, err
	}
	if final != t.FinalBranch {
		if _, exists, err := s.git.BranchSHA(final); err != nil {
			return freshPlan{}, err
		} else if exists {
			return freshPlan{}, fmt.Errorf("branch %s exists and was not created by task %s", final, t.ID)
		}
		if err := s.git.BranchAvailable(final); err != nil {
			return freshPlan{}, err
		}
	}
	return freshPlan{base: base, baseTip: tip, wip: wip, final: final}, nil
}

// wipOwner returns the task whose wip branch is branch and which the
// server may checkpoint onto: one in progress, or self.
func (s *Service) wipOwner(branch, self string) string {
	if branch == "" {
		return ""
	}
	for _, t := range s.index.All() {
		if t.Branch == branch && (t.Status == StatusInProgress || t.ID == self) {
			return t.ID
		}
	}
	return ""
}

// classifyVacate decides how HEAD's uncommitted changes (outside the tasks
// directory) are dealt with before switching away, without changing
// anything. It returns the task to checkpoint onto, or "" when there is
// nothing to commit or the changes are carried because HEAD is carryFrom.
// Changes on any other branch are the user's own, and the server never
// commits or guesses where they belong: it refuses.
func (s *Service) classifyVacate(h headState, carryFrom, self, starting string) (string, error) {
	dirty, err := s.git.DirtyPaths()
	if err != nil {
		return "", err
	}
	if len(dirty) == 0 {
		return "", nil
	}
	if owner := s.wipOwner(h.branch, self); owner != "" {
		return owner, nil
	}
	if carryFrom != "" && h.branch == carryFrom {
		return "", nil
	}
	listed := dirty
	more := ""
	if len(listed) > maxListedPaths {
		more = fmt.Sprintf(", and %d more", len(listed)-maxListedPaths)
		listed = listed[:maxListedPaths]
	}
	return "", fmt.Errorf("uncommitted changes on %s (%s%s); commit or stash them before starting %s",
		h.branch, strings.Join(listed, ", "), more, starting)
}

// checkpoint commits HEAD's uncommitted changes onto owner's wip branch,
// which HEAD is. It returns HEAD's commit afterwards.
func (s *Service) checkpoint(txn *gitTxn, h headState, owner, op string) (string, error) {
	if owner == "" {
		return h.sha, nil
	}
	w, committed, err := s.git.Snapshot(checkpointMsg(owner, op))
	if err != nil {
		return "", fmt.Errorf("checkpoint on %s: %w", h.branch, err)
	}
	if !committed {
		return h.sha, nil
	}
	txn.add(fmt.Sprintf("checkpoint %s on %s (was %s)", w, h.branch, h.sha), func() error {
		if err := s.git.MoveBranch(h.branch, h.sha, w); err != nil {
			return err
		}
		return s.git.RestoreIndex(h.index)
	})
	return w, nil
}

// createBranch creates a branch, journaling its deletion.
func (s *Service) createBranch(txn *gitTxn, name, sha string) error {
	if err := s.git.CreateBranch(name, sha); err != nil {
		return fmt.Errorf("create branch %s: %w", name, err)
	}
	txn.add(fmt.Sprintf("branch %s at %s", name, sha), func() error {
		return s.git.DeleteBranch(name, sha)
	})
	return nil
}

// switchTo is a flow's last step. Git refuses atomically when the switch
// would overwrite local changes, so a failure here changes nothing and the
// journal can still be rolled back.
func (s *Service) switchTo(branch string) error {
	if err := s.git.Switch(branch); err != nil {
		return fmt.Errorf("switch to %s: %w", branch, err)
	}
	return nil
}

// afterSwitch reloads the index: a switch can change task files when the
// tasks directory is tracked in the code repository, and mtimes alone may
// not show it. Git state is final by now, so a reload failure is logged
// rather than reported as a failed call; the index heals on the next scan.
func (s *Service) afterSwitch(t *Task) *Task {
	if err := s.index.Load(); err != nil {
		log.Printf("warning: reloading the task index after switching branches: %v", err)
	}
	return t
}

// runFlow runs the mutating part of a flow and rolls the journal back when
// it fails.
func runFlow(txn *gitTxn, mutate func() (*Task, error)) (*Task, error) {
	t, err := mutate()
	if err != nil {
		return nil, withRollback(err, txn.rollback())
	}
	return t, nil
}

// startBranched starts t with git branching on: a subtask off its parent's
// wip branch, a top-level task off the first existing base branch.
func (s *Service) startBranched(t *Task) (*Task, error) {
	if t.ParentID != "" {
		return s.startSub(t)
	}
	return s.startFresh(t)
}

// startFresh creates t's wip branch at the tip of the base branch and
// switches to it (design 5.2).
func (s *Service) startFresh(t *Task) (*Task, error) {
	h, err := s.preflightGit()
	if err != nil {
		return nil, err
	}
	plan, err := s.planFresh(t)
	if err != nil {
		return nil, err
	}
	op := "start " + t.ID
	owner, err := s.classifyVacate(h, plan.base, "", t.ID)
	if err != nil {
		return nil, err
	}

	var txn gitTxn
	started, err := runFlow(&txn, func() (*Task, error) {
		if _, err := s.checkpoint(&txn, h, owner, op); err != nil {
			return nil, err
		}
		started, err := s.startFreshRecords(&txn, t, plan)
		if err != nil {
			return nil, err
		}
		return started, s.switchTo(plan.wip)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(started), nil
}

// startFreshRecords creates t's wip branch and records it, without
// switching: a subtask start reuses it to start a todo parent.
func (s *Service) startFreshRecords(txn *gitTxn, t *Task, plan freshPlan) (*Task, error) {
	if err := s.createBranch(txn, plan.wip, plan.baseTip); err != nil {
		return nil, err
	}
	if err := s.captureTask(txn, t.ID); err != nil {
		return nil, err
	}
	status := StatusInProgress
	started, err := s.update(t.ID, nil, nil, &status, nil, nil, withBranch(branchInfo{
		Branch:      &plan.wip,
		BaseBranch:  &plan.base,
		StartCommit: &plan.baseTip,
	}))
	if err != nil {
		return nil, err
	}
	if err := s.setPointer(txn, t.ID); err != nil {
		return nil, err
	}
	return started, nil
}

// startSub creates subtask t's wip branch at the tip of its parent's and
// switches to it (design 5.3). A parent that is still todo is started in
// the same journal; a parent started without branching keeps the subtask
// out of git too.
func (s *Service) startSub(t *Task) (*Task, error) {
	p, err := s.get(t.ParentID)
	if err != nil {
		return nil, fmt.Errorf("parent task not found: %s", t.ParentID)
	}
	switch {
	case p.Status == StatusInProgress && p.Branch == "":
		return s.startPlain(t)
	case p.Status == StatusDone:
		return nil, fmt.Errorf("parent task %s is done; reopen it before starting subtask %s", p.ID, t.ID)
	}

	h, err := s.preflightGit()
	if err != nil {
		return nil, err
	}

	var parentPlan *freshPlan
	parentBranch, carryFrom := p.Branch, ""
	if p.Status == StatusTodo {
		if p.Branch != "" {
			if _, ok, err := s.git.BranchSHA(p.Branch); err != nil {
				return nil, err
			} else if ok {
				return nil, fmt.Errorf("parent task %s: %w", p.ID, errRestartNotImplemented)
			}
		}
		plan, err := s.planFresh(p)
		if err != nil {
			return nil, fmt.Errorf("starting parent task %s: %w", p.ID, err)
		}
		parentPlan, parentBranch = &plan, plan.wip
		// The subtask lands on the base tip, exactly where a fresh start
		// of the parent would, so changes made on the base are carried.
		carryFrom = plan.base
	} else if _, ok, err := s.git.BranchSHA(p.Branch); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("the branch %s of parent task %s no longer exists", p.Branch, p.ID)
	}

	subWip := subWipBranch(parentBranch, shortName(t))
	if err := s.git.BranchAvailable(subWip); err != nil {
		return nil, err
	}
	op := "start " + t.ID
	owner, err := s.classifyVacate(h, carryFrom, "", t.ID)
	if err != nil {
		return nil, err
	}

	var txn gitTxn
	started, err := runFlow(&txn, func() (*Task, error) {
		if _, err := s.checkpoint(&txn, h, owner, op); err != nil {
			return nil, err
		}
		if parentPlan != nil {
			if _, err := s.startFreshRecords(&txn, p, *parentPlan); err != nil {
				return nil, fmt.Errorf("starting parent task %s: %w", p.ID, err)
			}
		}
		// Re-read the parent's tip: a checkpoint may just have moved it.
		ptip, ok, err := s.git.BranchSHA(parentBranch)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("the branch %s of parent task %s no longer exists", parentBranch, p.ID)
		}
		if err := s.createBranch(&txn, subWip, ptip); err != nil {
			return nil, err
		}
		if err := s.captureTask(&txn, t.ID); err != nil {
			return nil, err
		}
		status := StatusInProgress
		started, err := s.update(t.ID, nil, nil, &status, nil, nil, withBranch(branchInfo{
			Branch:      &subWip,
			BaseBranch:  &parentBranch,
			StartCommit: &ptip,
		}))
		if err != nil {
			return nil, err
		}
		if err := s.setPointer(&txn, t.ID); err != nil {
			return nil, err
		}
		return started, s.switchTo(subWip)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(started), nil
}
