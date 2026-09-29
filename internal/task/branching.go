package task

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Branch naming. Wip branches sit at depth 3 (<user>/wip/<name>), final
// branches at depth 2 (<user>/<name>, never "wip"), and a subtask's wip is a
// sibling of its parent's (<parent wip>--<sub>), so no branch this workflow
// creates can sit inside another. Pre-existing branches that would collide
// are caught by GitRepo.BranchAvailable.

// slugRe matches an id usable verbatim as a branch name component.
var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const (
	// maxIDSlug bounds an id used verbatim as a short name.
	maxIDSlug = 48
	// maxSlug bounds each slugified part of a derived short name.
	maxSlug = 40
	// subSeparator joins a parent's wip branch and a subtask's short name.
	// Slugs collapse repeated hyphens, so it never occurs inside one.
	subSeparator = "--"
)

// slugify lowercases s, keeps ASCII letters and digits, turns every other
// run of runes into one "-", and cuts the result to at most maxSlug
// characters, at a hyphen where there is one.
func slugify(s string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			hyphen = false
			continue
		}
		if !hyphen {
			b.WriteByte('-')
			hyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) <= maxSlug {
		return slug
	}
	cut := slug[:maxSlug]
	if slug[maxSlug] != '-' {
		if i := strings.LastIndexByte(cut, '-'); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.Trim(cut, "-")
}

// shortName is the name a task's branches are built from: a readable id as
// is, otherwise the slugified id and title, otherwise a stable hash.
func shortName(t *Task) string {
	if len(t.ID) <= maxIDSlug && slugRe.MatchString(t.ID) && strings.ContainsAny(t.ID, "abcdefghijklmnopqrstuvwxyz") && t.ID != "wip" {
		return t.ID
	}
	var parts []string
	for _, p := range []string{slugify(t.ID), slugify(t.Title)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	name := strings.Join(parts, "-")
	if name == "" || name == "wip" {
		sum := sha1.Sum([]byte(t.ID))
		return "task-" + hex.EncodeToString(sum[:])[:8]
	}
	return name
}

// wipBranch is a top-level task's working branch.
func wipBranch(user, name string) string {
	return user + "/wip/" + name
}

// subWipBranch is a subtask's working branch, a sibling of its parent's.
func subWipBranch(parentBranch, sub string) string {
	return parentBranch + subSeparator + sub
}

// finalBranch derives the squashed branch from a wip branch by dropping the
// "wip" segment. It works off the stored branch, so a later change of the
// user's email cannot desynchronize the two.
func finalBranch(wip string) (string, error) {
	i := strings.Index(wip, "/wip/")
	if i < 0 {
		return "", fmt.Errorf("branch %q is not a wip branch (no /wip/ segment)", wip)
	}
	return wip[:i] + "/" + wip[i+len("/wip/"):], nil
}

// commitMessage is the message of a squash commit: override verbatim when
// it is not blank, otherwise the title, the description if any, and a Task
// trailer.
func commitMessage(t *Task, override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(t.Title))
	b.WriteString("\n\n")
	if desc := strings.TrimSpace(t.Description); desc != "" {
		b.WriteString(desc)
		b.WriteString("\n\n")
	}
	b.WriteString("Task: ")
	b.WriteString(t.ID)
	b.WriteString("\n")
	return b.String()
}

// Bookkeeping commits use fixed subjects, so they are recognizable in a
// wip branch's history and never mistaken for delivered work.
const (
	checkpointFormat         = "wip(%s): checkpoint before %s\n"
	completionSnapshotFormat = "wip(%s): snapshot before completion\n"
	closedAsFormat           = "wip(%s): closed as %s\n"
)

// checkpointMsg is the message of the commit that saves uncommitted work on
// a wip branch before op moves away from it.
func checkpointMsg(id, op string) string {
	return fmt.Sprintf(checkpointFormat, id, op)
}

// completionSnapshotMsg is the message of the commit that saves uncommitted
// work before a delivered completion squashes the branch.
func completionSnapshotMsg(id string) string {
	return fmt.Sprintf(completionSnapshotFormat, id)
}

// closedAsMsg is the message of the commit that saves uncommitted work on a
// task closed without delivering it.
func closedAsMsg(id string, r Resolution) string {
	return fmt.Sprintf(closedAsFormat, id, r)
}

// gitTxn is a rollback journal for a flow that mixes ref moves, worktree
// changes and record writes: every applied step registers its undo, and a
// failure later in the flow rolls back everything already done.
type gitTxn struct {
	undo  []func() error
	names []string
}

// add registers the undo of a step that has just been applied.
func (x *gitTxn) add(name string, f func() error) {
	x.undo = append(x.undo, f)
	x.names = append(x.names, name)
}

// rollback runs every undo in reverse order, including after one fails, and
// reports every step it could not undo.
func (x *gitTxn) rollback() error {
	var errs []error
	for i := len(x.undo) - 1; i >= 0; i-- {
		if err := x.undo[i](); err != nil {
			errs = append(errs, fmt.Errorf("undo %s: %w", x.names[i], err))
		}
	}
	total := len(x.undo)
	x.undo, x.names = nil, nil
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("rollback incomplete, %d of %d steps could not be undone: %w", len(errs), total, errors.Join(errs...))
}

// captureTask records task id as it is now, registering an undo that
// writes it back to storage and the index.
func (s *Service) captureTask(txn *gitTxn, id string) error {
	t, err := s.storage.Load(id)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("task not found: %s", id)
	}
	saved := *t
	txn.add("record of task "+id, func() error {
		restored := saved
		if err := s.storage.Save(&restored); err != nil {
			return err
		}
		s.index.Set(&restored)
		return nil
	})
	return nil
}

// capturePointer records user's current-task pointer as it is now,
// registering an undo that rewrites or removes it. A no-op without a
// pointer store.
func (s *Service) capturePointer(txn *gitTxn, user string) error {
	if s.current == nil {
		return nil
	}
	id, ok, err := s.current.ReadCurrentTask(user)
	if err != nil {
		return err
	}
	txn.add("current-task pointer of "+user, func() error {
		if ok {
			return s.current.WriteCurrentTask(user, id)
		}
		return s.current.RemoveCurrentTask(user)
	})
	return nil
}

// branchingGuard refuses the update_task status moves that would bypass the
// git flows (design 5.10): starting, closing a task that works on a branch,
// and resuming a done one. Reopening to todo stays allowed - the branch
// fields are kept, so the next start_task restarts on the branch - and so
// does every edit that leaves the status alone.
func branchingGuard(t *Task, status *Status, o updateOpts) error {
	moving := func(to Status) bool { return status != nil && *status == to && t.Status != to }
	closing := o.resolution != nil || moving(StatusDone)
	switch {
	case t.Status == StatusTodo && moving(StatusInProgress):
		return fmt.Errorf("git branching is enabled; use start_task to start task %s", t.ID)
	case t.Status == StatusInProgress && t.Branch != "" && closing:
		return fmt.Errorf("git branching is enabled; use complete_task to close task %s, which works on branch %s", t.ID, t.Branch)
	case t.Status == StatusDone && t.Branch != "" && moving(StatusInProgress):
		return fmt.Errorf("git branching is enabled; reopen task %s to todo and use start_task to resume it on %s", t.ID, t.Branch)
	}
	return nil
}

// branchInfo carries branch field changes into update. A nil field is left
// as it is, so a restart can move StartCommit without clearing FinalBranch.
type branchInfo struct {
	Branch       *string
	BaseBranch   *string
	StartCommit  *string
	FinalBranch  *string
	SquashCommit *string
}

// withBranch sets branch fields. Unexported: only the git flows record
// branches, and they do it through update, the single persistence path.
func withBranch(b branchInfo) UpdateOption {
	return func(o *updateOpts) { o.branch = &b }
}

// apply writes the non-nil fields onto t.
func (b branchInfo) apply(t *Task) {
	for _, f := range []struct {
		src *string
		dst *string
	}{
		{b.Branch, &t.Branch},
		{b.BaseBranch, &t.BaseBranch},
		{b.StartCommit, &t.StartCommit},
		{b.FinalBranch, &t.FinalBranch},
		{b.SquashCommit, &t.SquashCommit},
	} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
}
