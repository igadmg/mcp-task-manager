package vcs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirtyPaths lists every changed, staged or untracked path in the worktree
// outside the tasks directory. Empty means the code is clean.
func (r *Repo) DirtyPaths() ([]string, error) {
	if err := r.ensure(); err != nil {
		return nil, err
	}
	args := append([]string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}, r.pathspec()...)
	out, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	var paths []string
	fields := strings.Split(out.stdout, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		xy := entry[:2]
		paths = append(paths, entry[3:])
		// A rename or copy carries its source path as the next field.
		if strings.ContainsAny(xy, "RC") {
			i++
		}
	}
	return paths, nil
}

// IndexTree writes the real index as a tree, capturing it for RestoreIndex.
func (r *Repo) IndexTree() (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	return r.line("write-tree")
}

// RestoreIndex replaces the real index with a tree captured by IndexTree.
func (r *Repo) RestoreIndex(tree string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	_, err := r.git("read-tree", tree)
	return err
}

// Snapshot commits every change outside the tasks directory - staged,
// unstaged and untracked - onto the checked-out branch, without touching
// the worktree. It builds the commit in a temporary index, so whatever the
// user staged under the tasks directory stays staged. committed is false,
// and sha is HEAD, when there was nothing to commit.
func (r *Repo) Snapshot(msg string) (sha string, committed bool, err error) {
	branch, head, err := r.Head()
	if err != nil {
		return "", false, err
	}

	tmp, err := os.MkdirTemp("", "mcp-task-manager-index-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(tmp)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}

	if _, err := run(r.top, "", env, "read-tree", "HEAD"); err != nil {
		return "", false, err
	}
	if err := r.addAllButTasks(env); err != nil {
		return "", false, err
	}
	out, err := run(r.top, "", env, "write-tree")
	if err != nil {
		return "", false, err
	}
	tree := strings.TrimSpace(out.stdout)

	headTree, err := r.TreeOf("HEAD")
	if err != nil {
		return "", false, err
	}
	if tree == headTree {
		return head, false, nil
	}

	sha, err = r.CommitTree(tree, head, msg)
	if err != nil {
		return "", false, err
	}
	if _, err := r.git("update-ref", "-m", reflogPrefix+"snapshot", "refs/heads/"+branch, sha, head); err != nil {
		return "", false, err
	}
	// The real index still holds the old HEAD's entries for code paths;
	// bring them up to the new HEAD, leaving the tasks directory alone.
	if _, err := r.git(append([]string{"reset", "-q"}, r.pathspec()...)...); err != nil {
		return "", false, err
	}
	return sha, true, nil
}

// addAllButTasks stages every change outside the tasks directory into the
// index env points at.
//
// git add refuses an exclude pathspec naming an ignored directory, so an
// ignored tasks directory is not excluded but undone afterwards: add -A
// already skips its untracked files, and resetting it to HEAD drops any
// change to files that were force-added and are tracked despite the ignore.
// check-ignore runs with --no-index because a directory holding a tracked
// file reads as not ignored otherwise, while add still rejects it.
func (r *Repo) addAllButTasks(env []string) error {
	ignored := false
	if r.tasksRel != "" {
		out, err := r.git("check-ignore", "-q", "--no-index", "--", r.tasksRel)
		switch {
		case err == nil:
			ignored = true
		case out.code != 1:
			return err
		}
	}
	if !ignored {
		_, err := run(r.top, "", env, append([]string{"add", "-A"}, r.pathspec()...)...)
		return err
	}
	if _, err := run(r.top, "", env, "add", "-A", "--", "."); err != nil {
		return err
	}
	_, err := run(r.top, "", env, "reset", "-q", "HEAD", "--", ":(top)"+r.tasksRel)
	return err
}

// MergeTrees merges theirs into ours over base without touching the
// worktree or index. A conflicted merge is not an error: it returns the
// conflicting paths.
func (r *Repo) MergeTrees(base, ours, theirs string) (tree string, conflicts []string, err error) {
	if err := r.ensure(); err != nil {
		return "", nil, err
	}
	out, err := r.git("merge-tree", "--write-tree", "--name-only", "--merge-base="+base, ours, theirs)
	if err != nil && out.code != 1 {
		return "", nil, err
	}
	// stdout: the tree, then (on conflict) one path per line up to a blank
	// line, then informational messages.
	lines := strings.Split(out.stdout, "\n")
	tree = strings.TrimSpace(lines[0])
	if out.code == 0 {
		return tree, nil, nil
	}
	seen := make(map[string]bool)
	for _, l := range lines[1:] {
		if l == "" {
			break
		}
		if !seen[l] {
			seen[l] = true
			conflicts = append(conflicts, l)
		}
	}
	return tree, conflicts, nil
}

// CommitTree creates a commit of tree with a single parent.
func (r *Repo) CommitTree(tree, parent, msg string) (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	out, err := run(r.top, msg, nil, "commit-tree", tree, "-p", parent, "-F", "-")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.stdout), nil
}

// Replay computes branch's commits since upstream replayed onto onto and
// returns the new tip. It never moves the branch and never touches the
// worktree or index; the caller moves the ref.
//
// An empty range returns onto itself. A conflict returns the conflicting
// paths with a nil error, as MergeTrees does; when those paths cannot be
// determined it returns ErrReplayConflict.
func (r *Repo) Replay(onto, upstream, branch string) (newTip string, conflicts []string, err error) {
	if err := r.ensure(); err != nil {
		return "", nil, err
	}
	tip, ok, err := r.branchSHA(branch)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("branch %q does not exist", branch)
	}

	ref := "refs/heads/" + branch
	out, err := r.git("replay", "--ref-action=print", "--onto", onto, upstream+".."+ref)
	switch {
	case err == nil:
		for l := range strings.SplitSeq(out.stdout, "\n") {
			if f := strings.Fields(l); len(f) == 4 && f[0] == "update" && f[1] == ref {
				return f[2], nil, nil
			}
		}
		// Nothing to replay: the branch becomes onto.
		sha, err := r.ResolveCommit(onto)
		return sha, nil, err
	case strings.Contains(out.stderr, "merge commits"):
		return "", nil, ErrReplayMerge
	case strings.Contains(out.stderr, "unknown option") || strings.Contains(out.stderr, "is not a git command"):
		// A git without replay, or with a replay that has no print mode.
		return "", nil, ErrReplayUnsupported
	case out.code == 1:
		// git replay reports a conflict with no output at all, so name the
		// paths by merging the whole range at once.
		_, paths, mergeErr := r.MergeTrees(upstream, onto, tip)
		if mergeErr != nil {
			return "", nil, errors.Join(fmt.Errorf("replaying %s..%s onto %s: %w", upstream, branch, onto, ErrReplayConflict), mergeErr)
		}
		if len(paths) == 0 {
			return "", nil, fmt.Errorf("replaying %s..%s onto %s conflicts: %w", upstream, branch, onto, ErrReplayConflict)
		}
		return "", paths, nil
	default:
		return "", nil, err
	}
}
