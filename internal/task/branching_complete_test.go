package task_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// commitCode commits one file on the checked-out branch.
func commitCode(t *testing.T, b *testsupport.GitBacklog, name, content, msg string) string {
	t.Helper()
	writeCode(t, b, name, content)
	b.Git(t, "add", "--", name)
	b.Git(t, "commit", "-q", "-m", msg)
	return b.Git(t, "rev-parse", "HEAD")
}

func requirePointer(t *testing.T, b *testsupport.GitBacklog, want string) {
	t.Helper()
	data, err := os.ReadFile(b.PointerPath())
	switch {
	case want == "" && !os.IsNotExist(err):
		t.Errorf("pointer = (%q, %v), want none", data, err)
	case want != "" && string(data) != want+"\n":
		t.Errorf("pointer = (%q, %v), want %s", data, err, want)
	}
}

func requireHead(t *testing.T, b *testsupport.GitBacklog, want string) {
	t.Helper()
	if head := b.Head(t); head != want {
		t.Errorf("HEAD = %q, want %q", head, want)
	}
}

// setRecord rewrites a task record behind the service, the way a task
// handled while branching was off (or by hand) looks.
func setRecord(t *testing.T, b *testsupport.GitBacklog, id string, edit func(*task.Task)) {
	t.Helper()
	st := storage.NewMarkdownStorage(b.TasksDir)
	rec, err := st.Load(id)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", id, err)
	}
	edit(rec)
	if err := st.Save(rec); err != nil {
		t.Fatalf("Save(%s) error = %v", id, err)
	}
}

func TestCompleteDelivered(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "alpha", "")
		start := mustStart(t, b, "alpha").StartCommit
		commitCode(t, b, "a.txt", "committed\n", "wip commit")
		writeCode(t, b, "README", "modified\n")
		writeCode(t, b, "c.txt", "untracked\n")

		done, err := b.Svc.CompleteTask("alpha", task.WithCommitMessage("feat: x"))
		if err != nil {
			t.Fatalf("CompleteTask() error = %v", err)
		}

		if got := b.Git(t, "rev-parse", "dev/alpha^"); got != start {
			t.Errorf("dev/alpha^ = %s, want the start commit %s", got, start)
		}
		if got := b.Git(t, "rev-list", "--count", start+"..dev/alpha"); got != "1" {
			t.Errorf("dev/alpha is %s commits over its start, want exactly 1", got)
		}
		if got := b.Git(t, "diff", "--name-only", start, "dev/alpha"); got != "README\na.txt\nc.txt" {
			t.Errorf("final changes %q, want README, a.txt, c.txt and nothing under the tasks dir", got)
		}
		if got := b.Git(t, "show", "dev/alpha:README"); got != "modified" {
			t.Errorf("final README = %q", got)
		}
		if got := b.Git(t, "log", "--format=%s", start+"..dev/wip/alpha"); got != "wip(alpha): snapshot before completion\nwip commit" {
			t.Errorf("wip history = %q, want the commit plus the snapshot", got)
		}
		if got := b.Git(t, "log", "-1", "--format=%B", "dev/alpha"); got != "feat: x" {
			t.Errorf("final message = %q, want feat: x", got)
		}
		requireHead(t, b, "dev/alpha")
		if done.Status != task.StatusDone || done.FinalBranch != "dev/alpha" || done.SquashCommit != b.Git(t, "rev-parse", "dev/alpha") {
			t.Errorf("done = status %s, final %q, squash %q", done.Status, done.FinalBranch, done.SquashCommit)
		}
		if got := mustGet(t, b, "alpha"); got.FinalBranch != "dev/alpha" || got.Branch != "dev/wip/alpha" {
			t.Errorf("stored branch fields = (%q, %q)", got.Branch, got.FinalBranch)
		}
		requirePointer(t, b, "")
		requireNoServerCommitInTasks(t, b)
	})
}

func TestCompleteDeliveredDefaultMessage(t *testing.T) {
	b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoIgnored)
	if _, err := b.Svc.Create("Add login", "The form.\n", task.PriorityHigh, "feature", "", "login"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	mustStart(t, b, "login")
	writeCode(t, b, "login.go", "package login\n")

	if _, err := b.Svc.CompleteTask("login"); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if got := b.Git(t, "log", "-1", "--format=%B", "dev/login"); got != "Add login\n\nThe form.\n\nTask: login" {
		t.Errorf("final message = %q", got)
	}
}

func TestCompleteDeliveredRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, b *testsupport.GitBacklog)
		want  []string
	}{
		{"HEAD is not the wip", func(t *testing.T, b *testsupport.GitBacklog) {
			b.Git(t, "switch", "-q", "main_patched")
		}, []string{"switch to dev/wip/alpha first"}},
		{"start commit no longer in history", func(t *testing.T, b *testsupport.GitBacklog) {
			commitCode(t, b, "a.txt", "a\n", "wip commit")
			orphan := b.Git(t, "commit-tree", "HEAD^{tree}", "-m", "rewritten")
			b.Git(t, "reset", "-q", "--hard", orphan)
		}, []string{"no longer contains its recorded start"}},
		{"foreign final branch", func(t *testing.T, b *testsupport.GitBacklog) {
			b.Git(t, "branch", "dev/alpha", "main_patched")
		}, []string{"branch dev/alpha exists and was not created by task alpha"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, testsupport.TasksInRepoTracked)
			mustCreate(t, b, "alpha", "")
			mustStart(t, b, "alpha")
			writeCode(t, b, "dirty.txt", "dirty\n")
			tc.setup(t, b)
			before := testsupport.CaptureState(t, b)

			_, err := b.Svc.CompleteTask("alpha")
			wantErr(t, err, tc.want...)
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})
	}
}

func TestCompleteNonDeliveredCheckedOut(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("top-level", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "alpha", "")
			mustStart(t, b, "alpha")
			writeCode(t, b, "b.txt", "unfinished\n")

			done, err := b.Svc.CompleteTask("alpha", task.WithResolution(task.ResolutionObsolete))
			if err != nil {
				t.Fatalf("CompleteTask(obsolete) error = %v", err)
			}
			if got := b.Git(t, "log", "-1", "--format=%s", "dev/wip/alpha"); got != "wip(alpha): closed as obsolete" {
				t.Errorf("wip tip subject = %q", got)
			}
			if got := b.Git(t, "show", "dev/wip/alpha:b.txt"); got != "unfinished" {
				t.Errorf("safety commit b.txt = %q", got)
			}
			requireHead(t, b, "main_patched")
			if _, err := os.Stat(filepath.Join(b.CodeDir, "b.txt")); !os.IsNotExist(err) {
				t.Errorf("b.txt still in the worktree: %v", err)
			}
			if done.Status != task.StatusDone || done.Resolution != task.ResolutionObsolete {
				t.Errorf("done = %s/%s", done.Status, done.Resolution)
			}
			requirePointer(t, b, "")
			requireNoServerCommitInTasks(t, b)
		})

		t.Run("subtask", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "par", "")
			mustCreate(t, b, "sub", "par")
			mustStart(t, b, "sub")
			writeCode(t, b, "b.txt", "unfinished\n")

			if _, err := b.Svc.CompleteTask("sub", task.WithResolution(task.ResolutionWontfix)); err != nil {
				t.Fatalf("CompleteTask(wontfix) error = %v", err)
			}
			if got := b.Git(t, "show", "dev/wip/par--sub:b.txt"); got != "unfinished" {
				t.Errorf("safety commit b.txt = %q", got)
			}
			requireHead(t, b, "dev/wip/par")
			requirePointer(t, b, "par")
			if par := mustGet(t, b, "par"); par.Status != task.StatusInProgress {
				t.Errorf("parent status = %s, want in_progress", par.Status)
			}
		})
	})
}

func TestCompleteNonDeliveredNotCheckedOut(t *testing.T) {
	b, fg, _ := newFaultBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "alpha", "")
	mustStart(t, b, "alpha")
	b.Git(t, "switch", "-q", "main_patched")
	refs := b.Git(t, "for-each-ref")
	fg.calls = 0

	done, err := b.Svc.CompleteTask("alpha", task.WithResolution(task.ResolutionDuplicate))
	if err != nil {
		t.Fatalf("CompleteTask(duplicate) error = %v", err)
	}
	if done.Status != task.StatusDone {
		t.Errorf("status = %s", done.Status)
	}
	if fg.calls != 0 {
		t.Errorf("%d mutating git calls, want none", fg.calls)
	}
	if got := b.Git(t, "for-each-ref"); got != refs {
		t.Errorf("refs changed:\n%s", got)
	}
	requireHead(t, b, "main_patched")
	requirePointer(t, b, "")
}

func TestCompleteNonDeliveredCascade(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		for _, onParent := range []bool{false, true} {
			name := "HEAD on a subtask wip"
			if onParent {
				name = "HEAD on the parent wip"
			}
			t.Run(name, func(t *testing.T) {
				b := testsupport.NewGitBacklog(t, layout)
				mustCreate(t, b, "par", "")
				mustCreate(t, b, "sub1", "par")
				mustCreate(t, b, "sub2", "par")
				mustStart(t, b, "sub1")
				subTip := b.Git(t, "rev-parse", "dev/wip/par--sub1")
				if onParent {
					b.Git(t, "switch", "-q", "dev/wip/par")
				}
				head := b.Head(t)

				if _, err := b.Svc.CompleteTask("par", task.WithResolution(task.ResolutionObsolete)); err != nil {
					t.Fatalf("CompleteTask(par, obsolete) error = %v", err)
				}
				for _, id := range []string{"par", "sub1", "sub2"} {
					if got := mustGet(t, b, id); got.Status != task.StatusDone || got.Resolution != task.ResolutionObsolete {
						t.Errorf("%s = %s/%s, want done/obsolete", id, got.Status, got.Resolution)
					}
				}
				if got := b.Git(t, "rev-parse", "dev/wip/par--sub1"); got != subTip {
					t.Errorf("subtask wip moved to %s", got)
				}
				if onParent {
					requireHead(t, b, "main_patched")
				} else {
					requireHead(t, b, head)
				}
				requirePointer(t, b, "")
			})
		}
	})
}

func TestCompleteSubtaskDelivered(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		mustStart(t, b, "sub")
		commitCode(t, b, "s.txt", "sub work\n", "sub commit")
		writeCode(t, b, "u.txt", "uncommitted\n")
		ptip := b.Git(t, "rev-parse", "dev/wip/par")

		done, err := b.Svc.CompleteTask("sub", task.WithCommitMessage("feat(sub): done"))
		if err != nil {
			t.Fatalf("CompleteTask(sub) error = %v", err)
		}
		newTip := b.Git(t, "rev-parse", "dev/wip/par")
		if got := b.Git(t, "rev-parse", newTip+"^"); got != ptip {
			t.Errorf("merge commit parent = %s, want the old parent tip %s", got, ptip)
		}
		if got := b.Git(t, "rev-list", "--count", ptip+"..dev/wip/par"); got != "1" {
			t.Errorf("%s new commits on the parent wip, want 1", got)
		}
		if got := b.Git(t, "diff", "--name-only", ptip, newTip); got != "s.txt\nu.txt" {
			t.Errorf("merge changes %q", got)
		}
		if got := b.Git(t, "log", "-1", "--format=%B", newTip); got != "feat(sub): done" {
			t.Errorf("merge message = %q", got)
		}
		if done.SquashCommit != newTip {
			t.Errorf("squash_commit = %q, want %s", done.SquashCommit, newTip)
		}
		requireHead(t, b, "dev/wip/par")
		requirePointer(t, b, "par")
		if par := mustGet(t, b, "par"); par.Status != task.StatusInProgress {
			t.Errorf("parent status = %s; a branched parent is never auto-completed", par.Status)
		}
		requireNoServerCommitInTasks(t, b)
	})
}

func TestParentGate(t *testing.T) {
	layout := testsupport.TasksInRepoIgnored
	deliverSub := func(t *testing.T, b *testsupport.GitBacklog) {
		t.Helper()
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		mustStart(t, b, "sub")
		commitCode(t, b, "s.txt", "sub work\n", "sub commit")
		if _, err := b.Svc.CompleteTask("sub"); err != nil {
			t.Fatalf("CompleteTask(sub) error = %v", err)
		}
	}

	t.Run("passes after the subtask merge", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		deliverSub(t, b)
		start := mustGet(t, b, "par").StartCommit

		if _, err := b.Svc.CompleteTask("par"); err != nil {
			t.Fatalf("CompleteTask(par) error = %v", err)
		}
		if got := b.Git(t, "rev-list", "--count", start+"..dev/par"); got != "1" {
			t.Errorf("dev/par is %s commits over its start, want 1", got)
		}
		if got := b.Git(t, "show", "dev/par:s.txt"); got != "sub work" {
			t.Errorf("dev/par s.txt = %q", got)
		}
		requireHead(t, b, "dev/par")
	})

	t.Run("refuses while a subtask is in progress", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		mustStart(t, b, "sub")
		b.Git(t, "switch", "-q", "dev/wip/par")
		before := testsupport.CaptureState(t, b)

		_, err := b.Svc.CompleteTask("par")
		wantErr(t, err, "incomplete subtask")
		testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
	})

	t.Run("refuses when the merge was reset away", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		deliverSub(t, b)
		squash := mustGet(t, b, "sub").SquashCommit
		b.Git(t, "reset", "-q", "--hard", squash+"^")
		before := testsupport.CaptureState(t, b)

		_, err := b.Svc.CompleteTask("par")
		wantErr(t, err, "subtask sub was completed but its work ("+squash[:12]+") is not in dev/wip/par")
		testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
	})

	t.Run("accepts a subtask closed without delivering", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		mustStart(t, b, "sub")
		if _, err := b.Svc.CompleteTask("sub", task.WithResolution(task.ResolutionObsolete)); err != nil {
			t.Fatalf("CompleteTask(sub, obsolete) error = %v", err)
		}
		if _, err := b.Svc.CompleteTask("par"); err != nil {
			t.Fatalf("CompleteTask(par) error = %v", err)
		}
	})

	t.Run("accepts a done subtask without a branch", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		setRecord(t, b, "sub", func(rec *task.Task) {
			rec.Status = task.StatusDone
			rec.Resolution = task.ResolutionCompleted
		})
		mustStart(t, b, "par")
		if _, err := b.Svc.CompleteTask("par"); err != nil {
			t.Fatalf("CompleteTask(par) error = %v", err)
		}
	})
}

func TestSubtaskMergeConflict(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "par", "")
		mustCreate(t, b, "sub", "par")
		mustStart(t, b, "sub")
		commitCode(t, b, "README", "sub\n", "sub edit")
		b.Git(t, "switch", "-q", "dev/wip/par")
		commitCode(t, b, "README", "par\n", "par edit")
		b.Git(t, "switch", "-q", "dev/wip/par--sub")
		writeCode(t, b, "u.txt", "uncommitted\n")
		before := testsupport.CaptureState(t, b)

		_, err := b.Svc.CompleteTask("sub")
		wantErr(t, err, "squash-merging dev/wip/par--sub into dev/wip/par conflicts in: README", "nothing was changed")
		testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
	})
}

func TestCompleteFaultInjection(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("delivered top-level", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				mustCreate(t, b, "alpha", "")
				mustStart(t, b, "alpha")
				commitCode(t, b, "a.txt", "a\n", "wip commit")
				writeCode(t, b, "b.txt", "b\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.CompleteTask("alpha")
				return err
			})
		})

		t.Run("delivered subtask", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				mustCreate(t, b, "par", "")
				mustCreate(t, b, "sub", "par")
				mustStart(t, b, "sub")
				commitCode(t, b, "s.txt", "s\n", "sub commit")
				writeCode(t, b, "u.txt", "u\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.CompleteTask("sub")
				return err
			})
		})

		t.Run("closed while checked out", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				mustCreate(t, b, "alpha", "")
				mustStart(t, b, "alpha")
				writeCode(t, b, "b.txt", "b\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.CompleteTask("alpha", task.WithResolution(task.ResolutionObsolete))
				return err
			})
		})
	})
}

func TestLegacyInProgressTaskNoGitCalls(t *testing.T) {
	b, fg, _ := newFaultBacklog(t, testsupport.TasksInRepoTracked)
	mustCreate(t, b, "old", "")
	// Started while branching was off: in progress, no branch.
	setRecord(t, b, "old", func(rec *task.Task) { rec.Status = task.StatusInProgress })
	refs := b.Git(t, "for-each-ref")

	done, err := b.Svc.CompleteTask("old")
	if err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if done.Status != task.StatusDone || done.FinalBranch != "" {
		t.Errorf("done = status %s, final %q", done.Status, done.FinalBranch)
	}
	if fg.calls != 0 {
		t.Errorf("%d mutating git calls, want none", fg.calls)
	}
	if got := b.Git(t, "for-each-ref"); got != refs {
		t.Errorf("refs changed:\n%s", got)
	}
}
