package task_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/vcs"
)

func forEachLayout(t *testing.T, fn func(t *testing.T, layout testsupport.Layout)) {
	for _, layout := range testsupport.AllLayouts {
		t.Run(string(layout), func(t *testing.T) { fn(t, layout) })
	}
}

func TestStartFresh(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		b := testsupport.NewGitBacklog(t, layout)
		mustCreate(t, b, "alpha", "")
		baseTip := b.Git(t, "rev-parse", "main_patched")

		started := mustStart(t, b, "alpha")

		if got := b.Git(t, "rev-parse", "dev/wip/alpha"); got != baseTip {
			t.Errorf("dev/wip/alpha = %s, want the base tip %s", got, baseTip)
		}
		if head := b.Head(t); head != "dev/wip/alpha" {
			t.Errorf("HEAD = %s, want dev/wip/alpha", head)
		}
		if started.Status != task.StatusInProgress || started.Branch != "dev/wip/alpha" ||
			started.BaseBranch != "main_patched" || started.StartCommit != baseTip {
			t.Errorf("started = status %s, branch %q, base %q, start %q", started.Status, started.Branch, started.BaseBranch, started.StartCommit)
		}
		if got := mustGet(t, b, "alpha"); got.Branch != "dev/wip/alpha" {
			t.Errorf("stored branch = %q", got.Branch)
		}
		data, err := os.ReadFile(b.PointerPath())
		if err != nil || string(data) != "alpha\n" {
			t.Errorf("pointer %s = (%q, %v), want alpha", b.PointerPath(), data, err)
		}
		requireNoServerCommitInTasks(t, b)
	})
}

func TestStartBaseSelection(t *testing.T) {
	layout := testsupport.TasksUntracked

	t.Run("main_patched over main", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		b.Git(t, "branch", "main")
		writeCode(t, b, "later.txt", "later\n")
		b.Git(t, "add", "later.txt")
		b.Git(t, "commit", "-q", "-m", "later")
		mustCreate(t, b, "alpha", "")

		started := mustStart(t, b, "alpha")
		if started.BaseBranch != "main_patched" || started.StartCommit != b.Git(t, "rev-parse", "main_patched") {
			t.Errorf("base = %s at %s, want main_patched's tip", started.BaseBranch, started.StartCommit)
		}
	})

	t.Run("master_patched over master", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		b.Git(t, "branch", "-m", "main_patched", "master")
		b.Git(t, "branch", "master_patched")
		writeCode(t, b, "later.txt", "later\n")
		b.Git(t, "add", "later.txt")
		b.Git(t, "commit", "-q", "-m", "later on master")
		mustCreate(t, b, "alpha", "")

		started := mustStart(t, b, "alpha")
		if started.BaseBranch != "master_patched" || started.StartCommit != b.Git(t, "rev-parse", "master_patched") {
			t.Errorf("base = %s at %s, want master_patched's tip", started.BaseBranch, started.StartCommit)
		}
	})

	t.Run("no base branch", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		b.Git(t, "branch", "-m", "main_patched", "trunk")
		mustCreate(t, b, "alpha", "")
		before := testsupport.CaptureState(t, b)

		_, err := b.Svc.StartTask("alpha")
		wantErr(t, err, "none of the base branches", "set git.base_branches")
		testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
	})

	t.Run("custom list naming missing branches", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		b.Svc.Config().Git.BaseBranches = []string{"develop", "trunk"}
		mustCreate(t, b, "alpha", "")
		before := testsupport.CaptureState(t, b)

		_, err := b.Svc.StartTask("alpha")
		wantErr(t, err, "[develop trunk]")
		testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
	})

	t.Run("HEAD on a clean non-base branch", func(t *testing.T) {
		b := testsupport.NewGitBacklog(t, layout)
		b.Git(t, "switch", "-q", "-c", "feature")
		writeCode(t, b, "feature.txt", "feature\n")
		b.Git(t, "add", "feature.txt")
		b.Git(t, "commit", "-q", "-m", "feature work")
		mustCreate(t, b, "alpha", "")

		mustStart(t, b, "alpha")
		if got, want := b.Git(t, "rev-parse", "dev/wip/alpha"), b.Git(t, "rev-parse", "main_patched"); got != want {
			t.Errorf("dev/wip/alpha = %s, want main_patched's tip %s, not the feature branch", got, want)
		}
		if head := b.Head(t); head != "dev/wip/alpha" {
			t.Errorf("HEAD = %s", head)
		}
	})
}

func TestStartDirtyRules(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("dirty on the base is carried", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "alpha", "")
			baseTip := b.Git(t, "rev-parse", "main_patched")
			writeCode(t, b, "README", "edited on main\n")

			mustStart(t, b, "alpha")
			if head := b.Head(t); head != "dev/wip/alpha" {
				t.Fatalf("HEAD = %s", head)
			}
			if got := b.Git(t, "rev-parse", "main_patched"); got != baseTip {
				t.Errorf("main_patched moved to %s; the server must not commit on the base", got)
			}
			if got := b.Git(t, "status", "--porcelain", "--", "README"); got != "M README" {
				t.Errorf("README status = %q, want the edit carried uncommitted", got)
			}
		})

		t.Run("dirty on another task's wip is checkpointed", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "alpha", "")
			mustCreate(t, b, "beta", "")
			mustStart(t, b, "alpha")
			alphaTip := b.Git(t, "rev-parse", "dev/wip/alpha")
			writeCode(t, b, "alpha.txt", "alpha work\n")

			mustStart(t, b, "beta")
			if got := b.Git(t, "log", "-1", "--format=%s", "dev/wip/alpha"); got != "wip(alpha): checkpoint before start beta" {
				t.Errorf("alpha's wip tip subject = %q", got)
			}
			if got := b.Git(t, "rev-parse", "dev/wip/alpha^"); got != alphaTip {
				t.Errorf("checkpoint parent = %s, want %s", got, alphaTip)
			}
			if got := b.Git(t, "show", "dev/wip/alpha:alpha.txt"); got != "alpha work" {
				t.Errorf("checkpointed alpha.txt = %q", got)
			}
			if got, want := b.Git(t, "rev-parse", "dev/wip/beta"), b.Git(t, "rev-parse", "main_patched"); got != want {
				t.Errorf("dev/wip/beta = %s, want the base tip %s", got, want)
			}
			if _, err := os.Stat(filepath.Join(b.CodeDir, "alpha.txt")); !os.IsNotExist(err) {
				t.Errorf("alpha.txt still in the worktree on beta's branch: %v", err)
			}
			requireNoServerCommitInTasks(t, b)
		})

		t.Run("dirty on a foreign branch is refused", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			b.Git(t, "switch", "-q", "-c", "feature")
			mustCreate(t, b, "alpha", "")
			writeCode(t, b, "README", "mine\n")
			before := testsupport.CaptureState(t, b)

			_, err := b.Svc.StartTask("alpha")
			wantErr(t, err, "uncommitted changes on feature", "README", "before starting alpha")
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})

		t.Run("only the tasks dir dirty never blocks", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			b.Git(t, "switch", "-q", "-c", "feature")
			mustCreate(t, b, "alpha", "")
			mustCreate(t, b, "beta", "")

			mustStart(t, b, "alpha")
			if head := b.Head(t); head != "dev/wip/alpha" {
				t.Errorf("HEAD = %s", head)
			}
		})
	})
}

func TestStartRefusals(t *testing.T) {
	layout := testsupport.TasksInRepoTracked
	for _, tc := range []struct {
		name  string
		opts  func(t *testing.T) []task.ServiceOption
		setup func(t *testing.T, b *testsupport.GitBacklog)
		want  []string
	}{
		{"detached HEAD", nil, func(t *testing.T, b *testsupport.GitBacklog) {
			b.Git(t, "switch", "-q", "--detach")
		}, []string{"HEAD is detached"}},
		{"foreign final branch", nil, func(t *testing.T, b *testsupport.GitBacklog) {
			b.Git(t, "branch", "dev/alpha")
		}, []string{"branch dev/alpha exists and was not created by task alpha"}},
		{"prefix collision", nil, func(t *testing.T, b *testsupport.GitBacklog) {
			b.Git(t, "branch", "dev")
		}, []string{`branch "dev" exists`}},
		{"no email identity", func(t *testing.T) []task.ServiceOption {
			return []task.ServiceOption{task.WithIdentity(task.Identity{Name: "x"})}
		}, nil, []string{"user.email is not set"}},
		{"not a repository", func(t *testing.T) []task.ServiceOption {
			dir := t.TempDir()
			return []task.ServiceOption{task.WithGit(vcs.New(dir, filepath.Join(dir, "tasks")))}
		}, nil, []string{"not inside a git repository"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts []task.ServiceOption
			if tc.opts != nil {
				testsupport.RequireGit(t)
				opts = tc.opts(t)
			}
			b := testsupport.NewGitBacklog(t, layout, opts...)
			mustCreate(t, b, "alpha", "")
			if tc.setup != nil {
				tc.setup(t, b)
			}
			before := testsupport.CaptureState(t, b)

			_, err := b.Svc.StartTask("alpha")
			wantErr(t, err, tc.want...)
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})
	}
}

func TestStartSubtask(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("from the parent's dirty wip", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "par", "")
			mustCreate(t, b, "sub", "par")
			mustStart(t, b, "par")
			writeCode(t, b, "par.txt", "parent work\n")

			started := mustStart(t, b, "sub")
			parTip := b.Git(t, "rev-parse", "dev/wip/par")
			if got := b.Git(t, "log", "-1", "--format=%s", parTip); got != "wip(par): checkpoint before start sub" {
				t.Errorf("parent wip tip subject = %q", got)
			}
			if got := b.Git(t, "rev-parse", "dev/wip/par--sub"); got != parTip {
				t.Errorf("dev/wip/par--sub = %s, want the parent's new tip %s", got, parTip)
			}
			if head := b.Head(t); head != "dev/wip/par--sub" {
				t.Errorf("HEAD = %s", head)
			}
			if started.Branch != "dev/wip/par--sub" || started.BaseBranch != "dev/wip/par" || started.StartCommit != parTip {
				t.Errorf("subtask branch fields = (%q, %q, %q)", started.Branch, started.BaseBranch, started.StartCommit)
			}
			if got := b.Git(t, "show", "HEAD:par.txt"); got != "parent work" {
				t.Errorf("subtask branch par.txt = %q, want the parent's work", got)
			}
			if data, _ := os.ReadFile(b.PointerPath()); string(data) != "par\nsub\n" {
				t.Errorf("pointer = %q, want par then sub", data)
			}
			requireNoServerCommitInTasks(t, b)
		})

		t.Run("todo parent is started with its own wip", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "par", "")
			mustCreate(t, b, "sub", "par")
			baseTip := b.Git(t, "rev-parse", "main_patched")

			mustStart(t, b, "sub")
			par := mustGet(t, b, "par")
			if par.Status != task.StatusInProgress || par.Branch != "dev/wip/par" || par.StartCommit != baseTip {
				t.Errorf("parent = status %s, branch %q, start %q", par.Status, par.Branch, par.StartCommit)
			}
			for _, ref := range []string{"dev/wip/par", "dev/wip/par--sub"} {
				if got := b.Git(t, "rev-parse", ref); got != baseTip {
					t.Errorf("%s = %s, want the base tip", ref, got)
				}
			}
			if head := b.Head(t); head != "dev/wip/par--sub" {
				t.Errorf("HEAD = %s", head)
			}
			if data, _ := os.ReadFile(b.PointerPath()); string(data) != "par\nsub\n" {
				t.Errorf("pointer = %q, want par then sub", data)
			}
		})

		t.Run("parent started without branching", func(t *testing.T) {
			b := testsupport.NewGitBacklog(t, layout)
			mustCreate(t, b, "par", "")
			mustCreate(t, b, "sub", "par")
			// A parent started while branching was off: in progress, no branch.
			st := storage.NewMarkdownStorage(b.TasksDir)
			par, err := st.Load("par")
			if err != nil {
				t.Fatalf("Load(par) error = %v", err)
			}
			par.Status = task.StatusInProgress
			if err := st.Save(par); err != nil {
				t.Fatalf("Save(par) error = %v", err)
			}
			refs := b.Git(t, "for-each-ref")

			started := mustStart(t, b, "sub")
			if started.Status != task.StatusInProgress || started.Branch != "" {
				t.Errorf("subtask = status %s, branch %q; want started without git", started.Status, started.Branch)
			}
			if got := b.Git(t, "for-each-ref"); got != refs {
				t.Errorf("refs changed:\n%s", got)
			}
			if head := b.Head(t); head != "main_patched" {
				t.Errorf("HEAD = %s", head)
			}
		})
	})
}

func TestStartFaultInjection(t *testing.T) {
	forEachLayout(t, func(t *testing.T, layout testsupport.Layout) {
		t.Run("fresh from a dirty wip", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				mustCreate(t, b, "alpha", "")
				mustCreate(t, b, "beta", "")
				mustStart(t, b, "alpha")
				writeCode(t, b, "alpha.txt", "alpha work\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.StartTask("beta")
				return err
			})
		})

		t.Run("subtask of a todo parent from a dirty wip", func(t *testing.T) {
			runFaults(t, layout, func(t *testing.T, b *testsupport.GitBacklog) {
				mustCreate(t, b, "alpha", "")
				mustCreate(t, b, "par", "")
				mustCreate(t, b, "sub", "par")
				mustStart(t, b, "alpha")
				writeCode(t, b, "alpha.txt", "alpha work\n")
			}, func(b *testsupport.GitBacklog) error {
				_, err := b.Svc.StartTask("sub")
				return err
			})
		})

		t.Run("pointer write failure", func(t *testing.T) {
			b, _, fs := newFaultBacklog(t, layout)
			mustCreate(t, b, "alpha", "")
			mustCreate(t, b, "beta", "")
			mustStart(t, b, "alpha")
			writeCode(t, b, "alpha.txt", "alpha work\n")
			before := testsupport.CaptureState(t, b)
			fs.fail = true

			_, err := b.Svc.StartTask("beta")
			wantErr(t, err, "WriteCurrentTask")
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})
	})
}

func TestStartBranchingOffUnchanged(t *testing.T) {
	testsupport.RequireGit(t)
	_, svc, _ := testsupport.NewBacklog(t)
	if svc.BranchingEnabled() {
		t.Fatal("NewBacklog has branching on")
	}
	if _, err := svc.Create("Work", "", task.PriorityHigh, "feature", "", "work"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	started, err := svc.StartTask("work")
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if started.Status != task.StatusInProgress || started.Branch != "" {
		t.Errorf("started = status %s, branch %q", started.Status, started.Branch)
	}
}
