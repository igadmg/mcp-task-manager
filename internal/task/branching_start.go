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

// firstBase returns the first configured base branch that exists, with its
// tip.
func (s *Service) firstBase() (name, tip string, err error) {
	bases := s.baseBranches()
	name, tip, err = s.git.FirstExistingBranch(bases)
	if err != nil {
		return "", "", err
	}
	if name == "" {
		return "", "", fmt.Errorf("none of the base branches %v exist; set git.base_branches", bases)
	}
	return name, tip, nil
}

// baseFor is the base branch top-level task t grows from now: the one it
// started from if that still exists, otherwise the first existing one.
func (s *Service) baseFor(t *Task) (name, tip string, err error) {
	if t.BaseBranch != "" {
		if tip, ok, err := s.git.BranchSHA(t.BaseBranch); err != nil {
			return "", "", err
		} else if ok {
			return t.BaseBranch, tip, nil
		}
	}
	return s.firstBase()
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
	base, tip, err := s.firstBase()
	if err != nil {
		return freshPlan{}, err
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
	return s.snapshotHead(txn, h, checkpointMsg(owner, op))
}

// snapshotHead commits every uncommitted change outside the tasks directory
// onto HEAD's branch, journaling the ref move and the index. It returns
// HEAD's commit afterwards, unchanged when there was nothing to commit.
func (s *Service) snapshotHead(txn *gitTxn, h headState, msg string) (string, error) {
	w, committed, err := s.git.Snapshot(msg)
	if err != nil {
		return "", fmt.Errorf("snapshot on %s: %w", h.branch, err)
	}
	if !committed {
		return h.sha, nil
	}
	txn.add(fmt.Sprintf("snapshot %s on %s (was %s)", w, h.branch, h.sha), func() error {
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

// moveBranch moves a branch with compare-and-swap, journaling the move back.
func (s *Service) moveBranch(txn *gitTxn, name, newSHA, oldSHA string) error {
	if err := s.git.MoveBranch(name, newSHA, oldSHA); err != nil {
		return fmt.Errorf("move branch %s: %w", name, err)
	}
	txn.add(fmt.Sprintf("branch %s moved %s -> %s", name, oldSHA, newSHA), func() error {
		return s.git.MoveBranch(name, oldSHA, newSHA)
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
// wip branch, a top-level task off the first existing base branch. extra
// runs inside the flow (see flowStep).
func (s *Service) startBranched(t *Task, extra flowStep) (*Task, error) {
	if t.ParentID != "" {
		return s.startSub(t, extra)
	}
	return s.startFresh(t, extra)
}

// startFresh creates t's wip branch at the tip of the base branch and
// switches to it (design 5.2).
func (s *Service) startFresh(t *Task, extra flowStep) (*Task, error) {
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
		started, err := s.startOnNewBranch(&txn, t, plan.wip, plan.base, plan.baseTip)
		if err != nil {
			return nil, err
		}
		if err := s.pointAndRun(&txn, t.ID, extra); err != nil {
			return nil, err
		}
		return started, s.switchTo(plan.wip)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(started), nil
}

// startOnNewBranch creates t's wip branch at tip (the tip of base) and
// starts t on it, without switching or moving the pointer: the caller does
// both, and a subtask start reuses it to start a todo parent.
func (s *Service) startOnNewBranch(txn *gitTxn, t *Task, wip, base, tip string) (*Task, error) {
	if err := s.createBranch(txn, wip, tip); err != nil {
		return nil, err
	}
	started, err := s.updateStatus(txn, t.ID, StatusInProgress, withBranch(branchInfo{
		Branch:      &wip,
		BaseBranch:  &base,
		StartCommit: &tip,
	}))
	if err != nil {
		return nil, err
	}
	return started, nil
}

// startSub creates subtask t's wip branch at the tip of its parent's and
// switches to it (design 5.3). A parent that is still todo is started - or
// restarted, when its branch still exists - in the same journal. A parent
// in progress without a branch keeps the subtask out of git too, unless
// extra is set: then it is an implementation start under a parent still in
// its own earlier phases, and the parent's wip is cut first.
func (s *Service) startSub(t *Task, extra flowStep) (*Task, error) {
	p, err := s.get(t.ParentID)
	if err != nil {
		return nil, fmt.Errorf("parent task not found: %s", t.ParentID)
	}
	cutParent := extra != nil
	if p.Status == StatusInProgress && p.Branch == "" && !cutParent {
		return s.startPlain(t, extra)
	}

	h, err := s.preflightGit()
	if err != nil {
		return nil, err
	}
	line, err := s.parentLineFor(t, h, cutParent)
	if err != nil {
		return nil, err
	}

	subWip := subWipBranch(line.branch, shortName(t))
	if err := s.git.BranchAvailable(subWip); err != nil {
		return nil, err
	}
	op := "start " + t.ID
	owner, err := s.classifyVacate(h, line.carryFrom, "", t.ID)
	if err != nil {
		return nil, err
	}
	parentBranch := line.branch

	var txn gitTxn
	started, err := runFlow(&txn, func() (*Task, error) {
		if _, err := s.checkpoint(&txn, h, owner, op); err != nil {
			return nil, err
		}
		if line.step != nil {
			if err := line.step(&txn); err != nil {
				return nil, err
			}
		}
		// Read the parent's tip now: a checkpoint or the parent's own
		// start may just have moved it.
		ptip, err := s.lineTip(parentBranch)
		if err != nil {
			return nil, err
		}
		started, err := s.startOnNewBranch(&txn, t, subWip, parentBranch, ptip)
		if err != nil {
			return nil, err
		}
		if err := s.pointAndRun(&txn, t.ID, extra); err != nil {
			return nil, err
		}
		return started, s.switchTo(subWip)
	})
	if err != nil {
		return nil, err
	}
	return s.afterSwitch(started), nil
}
