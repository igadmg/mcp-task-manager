package task_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// replayBacklog is a git backlog for restart tests, which need git replay.
func replayBacklog(t *testing.T, layout testsupport.Layout) *testsupport.GitBacklog {
	t.Helper()
	b := testsupport.NewGitBacklog(t, layout)
	testsupport.RequireGitReplay(t)
	return b
}

// advanceBase commits a file on main_patched and returns to where HEAD was.
func advanceBase(t *testing.T, b *testsupport.GitBacklog, name, content string) string {
	t.Helper()
	head := b.Head(t)
	b.Git(t, "switch", "-q", "main_patched")
	tip := commitCode(t, b, name, content, "base moves on")
	if head != "main_patched" {
		b.Git(t, "switch", "-q", head)
	}
	return tip
}

func reopen(t *testing.T, b *testsupport.GitBacklog, id string) {
	t.Helper()
	todo := task.StatusTodo
	if _, err := b.Svc.Update(id, nil, nil, &todo, nil, nil); err != nil {
		t.Fatalf("Update(%s, todo) error = %v", id, err)
	}
}

// codeStatus is `git status` of the code repository outside the tasks dir.
func codeStatus(t *testing.T, b *testsupport.GitBacklog) string {
	t.Helper()
	var out []string
	prefix := ""
	if rel, err := filepath.Rel(b.CodeDir, b.TasksDir); err == nil && !strings.HasPrefix(rel, "..") {
		prefix = filepath.ToSlash(rel) + "/"
	}
	for _, l := range strings.Split(b.Git(t, "status", "--porcelain", "--untracked-files=all"), "\n") {
		if l == "" || (prefix != "" && strings.HasPrefix(l[3:], prefix)) {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

var restartLayouts = []testsupport.Layout{testsupport.TasksInRepoTracked, testsupport.TasksSeparateRepo}

func forRestartLayouts(t *testing.T, fn func(t *testing.T, layout testsupport.Layout)) {
	for _, layout := range restartLayouts {
		t.Run(string(layout), func(t *testing.T) { fn(t, layout) })
	}
}

func TestRestartAfterBaseAdvanced(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		b := replayBacklog(t, layout)
		mustCreate(t, b, "alpha", "")
		mustStart(t, b, "alpha")
		commitCode(t, b, "a.txt", "a\n", "wip commit")
		if _, err := b.Svc.CompleteTask("alpha"); err != nil {
			t.Fatalf("CompleteTask() error = %v", err)
		}
		oldFinal := b.Git(t, "rev-parse", "dev/alpha")
		reopen(t, b, "alpha")
		newBase := advanceBase(t, b, "m.txt", "base\n")

		started := mustStart(t, b, "alpha")
		if got := b.Git(t, "rev-parse", "dev/wip/alpha^"); got != newBase {
			t.Errorf("replayed wip parent = %s, want the new base tip %s", got, newBase)
		}
		if got := b.Git(t, "log", "-1", "--format=%s", "dev/wip/alpha"); got != "wip commit" {
			t.Errorf("replayed wip tip subject = %q", got)
		}
		if started.StartCommit != newBase || started.BaseBranch != "main_patched" || started.Status != task.StatusInProgress {
			t.Errorf("restarted = start %s, base %s, status %s", started.StartCommit, started.BaseBranch, started.Status)
		}
		if started.FinalBranch != "dev/alpha" {
			t.Errorf("final_branch = %q, want it kept as history", started.FinalBranch)
		}
		requireHead(t, b, "dev/wip/alpha")
		requirePointer(t, b, "alpha")

		if _, err := b.Svc.CompleteTask("alpha"); err != nil {
			t.Fatalf("second CompleteTask() error = %v", err)
		}
		if got := b.Git(t, "rev-parse", "dev/alpha^"); got != newBase {
			t.Errorf("dev/alpha^ = %s, want the new base %s", got, newBase)
		}
		if got := b.Git(t, "rev-list", "--count", newBase+"..dev/alpha"); got != "1" {
			t.Errorf("dev/alpha is %s commits over the base, want 1", got)
		}
		if reflog := b.Git(t, "reflog", "show", "--format=%H %gs", "dev/alpha"); !strings.Contains(reflog, oldFinal+" mcp-task-manager: create") ||
			!strings.Contains(reflog, "mcp-task-manager: move") {
			t.Errorf("dev/alpha reflog = %q, want the create at %s and a compare-and-swap move", reflog, oldFinal)
		}
		requireNoServerCommitInTasks(t, b)
	})
}

func TestRestartInProgressFromOtherBranch(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		b := replayBacklog(t, layout)
		mustCreate(t, b, "alpha", "")
		mustStart(t, b, "alpha")
		commitCode(t, b, "a.txt", "a\n", "wip commit")
		b.Git(t, "switch", "-q", "main_patched")
		newBase := commitCode(t, b, "m.txt", "base\n", "base moves on")

		mustStart(t, b, "alpha")
		requireHead(t, b, "dev/wip/alpha")
		if got := b.Git(t, "rev-parse", "HEAD^"); got != newBase {
			t.Errorf("HEAD^ = %s, want the new base %s", got, newBase)
		}
		for _, f := range []string{"a.txt", "m.txt"} {
			if _, err := os.Stat(filepath.Join(b.CodeDir, f)); err != nil {
				t.Errorf("%s missing from the restarted worktree: %v", f, err)
			}
		}
	})
}

func TestRestartWhileOnWipDirty(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		b := replayBacklog(t, layout)
		mustCreate(t, b, "alpha", "")
		mustStart(t, b, "alpha")
		commitCode(t, b, "a.txt", "a\n", "wip commit")
		newBase := advanceBase(t, b, "m.txt", "base\n")
		writeCode(t, b, "a.txt", "a edited\n")
		writeCode(t, b, "d.txt", "new\n")

		mustStart(t, b, "alpha")
		requireHead(t, b, "dev/wip/alpha")
		if got := b.Git(t, "log", "--format=%s", newBase+"..dev/wip/alpha"); got != "wip(alpha): checkpoint before restart alpha\nwip commit" {
			t.Errorf("replayed history = %q, want the commit plus the checkpoint", got)
		}
		if got := b.Git(t, "rev-parse", "dev/wip/alpha~2"); got != newBase {
			t.Errorf("dev/wip/alpha~2 = %s, want the new base %s", got, newBase)
		}
		if got := codeStatus(t, b); got != "" {
			t.Errorf("code worktree differs from the new tip:\n%s", got)
		}
		for name, want := range map[string]string{"a.txt": "a edited\n", "d.txt": "new\n", "m.txt": "base\n"} {
			if data, err := os.ReadFile(filepath.Join(b.CodeDir, name)); err != nil || string(data) != want {
				t.Errorf("%s = (%q, %v), want %q", name, data, err, want)
			}
		}
	})
}

func TestRestartNoBaseMovementNoReplay(t *testing.T) {
	b, fg, _ := newFaultBacklog(t, testsupport.TasksInRepoTracked)
	testsupport.RequireGitReplay(t)
	mustCreate(t, b, "alpha", "")
	mustStart(t, b, "alpha")
	commitCode(t, b, "a.txt", "a\n", "wip commit")
	tip := b.Git(t, "rev-parse", "HEAD")
	b.Git(t, "switch", "-q", "main_patched")
	fg.ops = nil

	mustStart(t, b, "alpha")
	requireHead(t, b, "dev/wip/alpha")
	if fg.ops["Replay"] != 0 {
		t.Errorf("%d Replay calls, want none when the base has not moved", fg.ops["Replay"])
	}
	if got := b.Git(t, "rev-parse", "HEAD"); got != tip {
		t.Errorf("wip moved to %s", got)
	}
}

func TestRestartSubtaskOntoParentTip(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		b := replayBacklog(t, layout)
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		mustStart(t, b, "sub")
		commitCode(t, b, "s.txt", "s\n", "sub commit")
		b.Git(t, "switch", "-q", "dev/wip/par")
		parTip := commitCode(t, b, "p.txt", "p\n", "parent commit")

		started := mustStart(t, b, "sub")
		if got := b.Git(t, "rev-parse", "dev/wip/par--sub^"); got != parTip {
			t.Errorf("replayed subtask parent = %s, want the parent's tip %s", got, parTip)
		}
		if started.StartCommit != parTip || started.BaseBranch != "dev/wip/par" {
			t.Errorf("restarted subtask = start %s, base %s", started.StartCommit, started.BaseBranch)
		}
		requireHead(t, b, "dev/wip/par--sub")
		if got := b.Git(t, "show", "HEAD:p.txt"); got != "p" {
			t.Errorf("HEAD:p.txt = %q", got)
		}
	})
}

func TestRestartSubtaskOfReopenedParent(t *testing.T) {
	b := replayBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "par", "")
	mustCreate(t, b, "sub", "par")
	mustStart(t, b, "sub")
	commitCode(t, b, "s.txt", "s\n", "sub commit")
	reopen(t, b, "par")
	b.Git(t, "switch", "-q", "main_patched")
	newBase := commitCode(t, b, "m.txt", "base\n", "base moves on")

	mustStart(t, b, "sub")
	par := mustGet(t, b, "par")
	if par.Status != task.StatusInProgress || par.StartCommit != newBase {
		t.Errorf("parent = status %s, start %s; want restarted onto %s", par.Status, par.StartCommit, newBase)
	}
	if got := b.Git(t, "rev-parse", "dev/wip/par--sub^"); got != b.Git(t, "rev-parse", "dev/wip/par") {
		t.Errorf("subtask not replayed onto the restarted parent")
	}
	requireHead(t, b, "dev/wip/par--sub")
}

func TestRestartConflict(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		b := replayBacklog(t, layout)
		mustCreate(t, b, "alpha", "")
		mustStart(t, b, "alpha")
		commitCode(t, b, "README", "wip\n", "wip edit")
		advanceBase(t, b, "README", "base\n")
		writeCode(t, b, "x.txt", "uncommitted\n")
		before := testsupport.CaptureState(t, b)

		_, err := b.Svc.StartTask("alpha")
		wantErr(t, err, "rebasing dev/wip/alpha onto main_patched conflicts in: README", "nothing was changed")
		testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
	})
}

func TestRestartMergeCommitRefused(t *testing.T) {
	b := replayBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	mustStart(t, b, "alpha")
	b.Git(t, "switch", "-q", "-c", "side")
	commitCode(t, b, "side.txt", "side\n", "side work")
	b.Git(t, "switch", "-q", "dev/wip/alpha")
	b.Git(t, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	b.Git(t, "switch", "-q", "main_patched")
	commitCode(t, b, "m.txt", "base\n", "base moves on")
	before := testsupport.CaptureState(t, b)

	_, err := b.Svc.StartTask("alpha")
	wantErr(t, err, "merge commit", "nothing was changed")
	testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
}

func TestRestartMissingWipTakesFreshPath(t *testing.T) {
	b := replayBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	mustStart(t, b, "alpha")
	b.Git(t, "switch", "-q", "main_patched")
	b.Git(t, "branch", "-q", "-D", "dev/wip/alpha")
	reopen(t, b, "alpha")
	newBase := commitCode(t, b, "m.txt", "base\n", "base moves on")

	started := mustStart(t, b, "alpha")
	if got := b.Git(t, "rev-parse", "dev/wip/alpha"); got != newBase || started.StartCommit != newBase {
		t.Errorf("fresh wip = %s (start %s), want the base tip %s", got, started.StartCommit, newBase)
	}
	requireHead(t, b, "dev/wip/alpha")
}

func TestRestartStartCommitNotAncestor(t *testing.T) {
	b := replayBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	mustStart(t, b, "alpha")
	commitCode(t, b, "a.txt", "a\n", "wip commit")
	b.Git(t, "switch", "-q", "main_patched")
	orphan := b.Git(t, "commit-tree", "dev/wip/alpha^{tree}", "-m", "rewritten")
	b.Git(t, "update-ref", "refs/heads/dev/wip/alpha", orphan)
	before := testsupport.CaptureState(t, b)

	_, err := b.Svc.StartTask("alpha")
	wantErr(t, err, "no longer contains its recorded start")
	testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
}

func TestRestartFaultInjection(t *testing.T) {
	forRestartLayouts(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("switch", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				testsupport.RequireGitReplay(t)
				mustCreate(t, b, "alpha", "")
				mustStart(t, b, "alpha")
				commitCode(t, b, "a.txt", "a\n", "wip commit")
				b.Git(t, "switch", "-q", "main_patched")
				commitCode(t, b, "m.txt", "base\n", "base moves on")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.StartTask("alpha")
				return err
			})
		})

		t.Run("reset keep", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				testsupport.RequireGitReplay(t)
				mustCreate(t, b, "alpha", "")
				mustStart(t, b, "alpha")
				commitCode(t, b, "a.txt", "a\n", "wip commit")
				advanceBase(t, b, "m.txt", "base\n")
				writeCode(t, b, "d.txt", "dirty\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.StartTask("alpha")
				return err
			})
		})
	})
}

// Reopening to todo is the one status move update_task allows on a branched
// task; the branch is kept, so the next start resumes on it.
func TestReopenThenStartRestarts(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	mustStart(t, b, "alpha")
	tip := commitCode(t, b, "a.txt", "a\n", "wip commit")
	b.Git(t, "switch", "-q", "main_patched")

	reopen(t, b, "alpha")
	if got := mustGet(t, b, "alpha"); got.Status != task.StatusTodo || got.Branch != "dev/wip/alpha" {
		t.Fatalf("reopened = status %s, branch %q; want todo with the branch kept", got.Status, got.Branch)
	}
	started := mustStart(t, b, "alpha")
	if started.Status != task.StatusInProgress {
		t.Errorf("status = %s", started.Status)
	}
	requireHead(t, b, "dev/wip/alpha")
	if got := b.Git(t, "rev-parse", "HEAD"); got != tip {
		t.Errorf("HEAD = %s, want the wip work %s back", got, tip)
	}
}
