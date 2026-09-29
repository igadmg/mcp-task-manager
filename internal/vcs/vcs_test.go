package vcs_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/vcs"
)

// newRepo returns a fresh repository with one commit on main_patched and a
// Repo over it whose tasks directory is <repo>/.tasks.
func newRepo(t *testing.T) (string, *vcs.Repo) {
	t.Helper()
	testsupport.RequireGit(t)
	dir := testsupport.NewGitRepo(t, filepath.Join(t.TempDir(), "repo"))
	return dir, vcs.New(dir, filepath.Join(dir, ".tasks"))
}

var git = testsupport.Git

// gitMayFail runs git where failure is the point, e.g. a conflicting merge.
func gitMayFail(dir string, args ...string) {
	_ = exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}

// commit writes files (path -> content) on the checked-out branch and
// commits them, returning the new HEAD.
func commit(t *testing.T, dir, msg string, files map[string]string) string {
	t.Helper()
	for p, content := range files {
		testsupport.WriteFile(t, filepath.Join(dir, p), content)
		git(t, dir, "add", "--", p)
	}
	git(t, dir, "commit", "-q", "-m", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func wantErrContaining(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error = %q, want one containing %q", err, substr)
	}
}

// conflictingMerge leaves dir mid-merge with README conflicted.
func conflictingMerge(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "switch", "-q", "-c", "other")
	commit(t, dir, "other", map[string]string{"README": "other\n"})
	git(t, dir, "switch", "-q", "main_patched")
	commit(t, dir, "mine", map[string]string{"README": "mine\n"})
	gitMayFail(dir, "merge", "-q", "other")
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) *vcs.Repo
		want  string // substring of the error; "" means Check passes
	}{
		{"non-repo directory", func(t *testing.T) *vcs.Repo {
			testsupport.RequireGit(t)
			dir := t.TempDir()
			return vcs.New(dir, filepath.Join(dir, ".tasks"))
		}, "not inside a git repository"},
		{"bare repository", func(t *testing.T) *vcs.Repo {
			testsupport.RequireGit(t)
			dir := t.TempDir()
			git(t, dir, "init", "-q", "--bare")
			return vcs.New(dir, filepath.Join(dir, ".tasks"))
		}, "bare"},
		{"merge in progress", func(t *testing.T) *vcs.Repo {
			dir, r := newRepo(t)
			conflictingMerge(t, dir)
			return r
		}, "merge is in progress"},
		{"unmerged paths", func(t *testing.T) *vcs.Repo {
			dir, r := newRepo(t)
			conflictingMerge(t, dir)
			for _, f := range []string{"MERGE_HEAD", "MERGE_MSG", "MERGE_MODE"} {
				os.Remove(filepath.Join(dir, ".git", f))
			}
			return r
		}, "unmerged paths"},
		{"tasks dir is the toplevel", func(t *testing.T) *vcs.Repo {
			dir, _ := newRepo(t)
			return vcs.New(dir, dir)
		}, "contains the whole repository"},
		{"tasks dir above the toplevel", func(t *testing.T) *vcs.Repo {
			dir, _ := newRepo(t)
			return vcs.New(dir, filepath.Dir(dir))
		}, "contains the whole repository"},
		{"clean repository", func(t *testing.T) *vcs.Repo {
			_, r := newRepo(t)
			return r
		}, ""},
		{"linked worktree", func(t *testing.T) *vcs.Repo {
			dir, _ := newRepo(t)
			wt := filepath.Join(filepath.Dir(dir), "wt")
			git(t, dir, "worktree", "add", "-q", "-b", "wt", wt)
			return vcs.New(wt, filepath.Join(wt, ".tasks"))
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.setup(t).Check()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Check() error = %v, want nil", err)
				}
				return
			}
			wantErrContaining(t, err, tc.want)
		})
	}
}

func TestHead(t *testing.T) {
	dir, r := newRepo(t)
	branch, sha, err := r.Head()
	if err != nil {
		t.Fatalf("Head() error = %v", err)
	}
	if branch != "main_patched" || sha != git(t, dir, "rev-parse", "HEAD") {
		t.Errorf("Head() = (%q, %q), want (main_patched, HEAD)", branch, sha)
	}

	git(t, dir, "switch", "-q", "--detach")
	if _, _, err := r.Head(); !errors.Is(err, vcs.ErrDetachedHead) {
		t.Errorf("detached Head() error = %v, want ErrDetachedHead", err)
	}
}

func TestResolveIdentity(t *testing.T) {
	for _, tc := range []struct{ email, want string }{
		{"Igor.Cwer+x@Example.com", "igor.cwer-x"},
		{"a..b.lock@x", "a.b"},
		{"-.x.-@y", "x"},
		{"jürgen@x", "j-rgen"},
	} {
		t.Run(tc.email, func(t *testing.T) {
			dir, _ := newRepo(t)
			git(t, dir, "config", "user.email", tc.email)
			got := vcs.ResolveIdentity(dir)
			if got != (vcs.Identity{Name: tc.want, FromGitEmail: true}) {
				t.Errorf("ResolveIdentity() = %+v, want {%s true}", got, tc.want)
			}
		})
	}

	t.Run("email unset", func(t *testing.T) {
		dir, _ := newRepo(t)
		git(t, dir, "config", "--unset", "user.email")
		got := vcs.ResolveIdentity(dir)
		if got.Name == "" || got.FromGitEmail {
			t.Errorf("ResolveIdentity() = %+v, want a non-empty OS fallback with FromGitEmail=false", got)
		}
	})

	t.Run("root does not exist", func(t *testing.T) {
		testsupport.RequireGit(t)
		got := vcs.ResolveIdentity(filepath.Join(t.TempDir(), "missing"))
		if got.Name == "" || got.FromGitEmail {
			t.Errorf("ResolveIdentity() = %+v, want a non-empty OS fallback with FromGitEmail=false", got)
		}
	})
}

func TestBranchAvailable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing string
		ask      string
		want     string
	}{
		{"free", "", "dev/wip/x", ""},
		{"exists", "dev/wip/x", "dev/wip/x", `branch "dev/wip/x" already exists`},
		{"prefix exists", "dev", "dev/wip/x", `branch "dev" exists`},
		{"child exists", "dev/wip/x/y", "dev/wip/x", `branch "dev/wip/x/y" exists below`},
		{"double dot", "", "a..b", "not a valid branch name"},
		{"space", "", "a b", "not a valid branch name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, r := newRepo(t)
			if tc.existing != "" {
				git(t, dir, "branch", tc.existing)
			}
			err := r.BranchAvailable(tc.ask)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("BranchAvailable(%q) = %v, want nil", tc.ask, err)
				}
				return
			}
			wantErrContaining(t, err, tc.want)
		})
	}
}

func TestFirstExistingBranch(t *testing.T) {
	dir, r := newRepo(t)
	git(t, dir, "branch", "main")
	head := git(t, dir, "rev-parse", "HEAD")

	name, sha, err := r.FirstExistingBranch([]string{"master_patched", "main", "main_patched"})
	if err != nil || name != "main" || sha != head {
		t.Errorf("FirstExistingBranch() = (%q, %q, %v), want (main, %s, nil)", name, sha, err, head)
	}

	if name, sha, err := r.FirstExistingBranch([]string{"nope", "neither"}); name != "" || sha != "" || err != nil {
		t.Errorf("FirstExistingBranch() with no existing branch = (%q, %q, %v), want empty and no error", name, sha, err)
	}
}

func TestCreateMoveDeleteCAS(t *testing.T) {
	dir, r := newRepo(t)
	h0 := git(t, dir, "rev-parse", "HEAD")
	h1 := git(t, dir, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "second")
	ref := func() string {
		out, _ := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "refs/heads/x").Output()
		return strings.TrimSpace(string(out))
	}

	if err := r.CreateBranch("x", h0); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if err := r.CreateBranch("x", h1); err == nil {
		t.Error("CreateBranch() over an existing branch: error = nil")
	}
	if ref() != h0 {
		t.Fatalf("x = %s after refused create, want %s", ref(), h0)
	}

	if err := r.MoveBranch("x", h1, h1); err == nil {
		t.Error("MoveBranch() with a stale old value: error = nil")
	}
	if ref() != h0 {
		t.Fatalf("x = %s after refused move, want %s", ref(), h0)
	}
	if err := r.MoveBranch("x", h1, h0); err != nil {
		t.Fatalf("MoveBranch() error = %v", err)
	}
	if got := git(t, dir, "reflog", "show", "--format=%gs", "x"); got != "mcp-task-manager: move\nmcp-task-manager: create" {
		t.Errorf("reflog of x = %q, want the create and move marked", got)
	}

	if err := r.DeleteBranch("x", h0); err == nil {
		t.Error("DeleteBranch() with the wrong expected sha: error = nil")
	}
	if ref() != h1 {
		t.Fatalf("x = %s after refused delete, want %s", ref(), h1)
	}
	if err := r.DeleteBranch("x", h1); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if ref() != "" {
		t.Errorf("x still exists after DeleteBranch: %s", ref())
	}
}

// snapshotLayout prepares a tasks directory next to or inside dir and
// returns its path and the path of a task record that the test dirties.
type snapshotLayout struct {
	name  string
	setup func(t *testing.T, dir string) (tasksDir string)
	// tracked is set when the task record is committed in the code repo,
	// so staging it is meaningful.
	tracked bool
}

var snapshotLayouts = []snapshotLayout{
	{"inside and tracked", func(t *testing.T, dir string) string {
		commit(t, dir, "tasks", map[string]string{".tasks/1/1.md": "task v1\n"})
		return filepath.Join(dir, ".tasks")
	}, true},
	{"inside and ignored", func(t *testing.T, dir string) string {
		commit(t, dir, "ignore tasks", map[string]string{".gitignore": ".tasks/\n"})
		testsupport.WriteFile(t, filepath.Join(dir, ".tasks", "1", "1.md"), "task v1\n")
		// Tracked despite the ignore: add -A would stage its edits.
		testsupport.WriteFile(t, filepath.Join(dir, ".tasks", "forced.md"), "forced v1\n")
		git(t, dir, "add", "-f", ".tasks/forced.md")
		git(t, dir, "commit", "-q", "-m", "force-add")
		testsupport.WriteFile(t, filepath.Join(dir, ".tasks", "forced.md"), "forced v2\n")
		return filepath.Join(dir, ".tasks")
	}, false},
	{"nested repository", func(t *testing.T, dir string) string {
		tasks := filepath.Join(dir, ".tasks")
		testsupport.WriteFile(t, filepath.Join(tasks, "1", "1.md"), "task v1\n")
		git(t, tasks, "init", "-q")
		git(t, tasks, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "add", ".")
		git(t, tasks, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "commit", "-q", "-m", "tasks")
		return tasks
	}, false},
	{"outside the worktree", func(t *testing.T, dir string) string {
		tasks := filepath.Join(t.TempDir(), "tasks")
		testsupport.WriteFile(t, filepath.Join(tasks, "1", "1.md"), "task v1\n")
		return tasks
	}, false},
}

func TestSnapshotExcludesTasksDir(t *testing.T) {
	for _, layout := range snapshotLayouts {
		t.Run(layout.name, func(t *testing.T) {
			testsupport.RequireGit(t)
			dir := testsupport.NewGitRepo(t, filepath.Join(t.TempDir(), "repo"))
			tasks := layout.setup(t, dir)
			commit(t, dir, "staged file", map[string]string{"staged.txt": "v1\n"})
			head := git(t, dir, "rev-parse", "HEAD")
			r := vcs.New(dir, tasks)

			// Code: an unstaged edit, a staged edit, an untracked file.
			testsupport.WriteFile(t, filepath.Join(dir, "README"), "edited\n")
			testsupport.WriteFile(t, filepath.Join(dir, "staged.txt"), "v2\n")
			git(t, dir, "add", "staged.txt")
			testsupport.WriteFile(t, filepath.Join(dir, "src", "new.go"), "package src\n")
			// Tasks: an edited record, staged where the repo tracks it.
			taskFile := filepath.Join(tasks, "1", "1.md")
			testsupport.WriteFile(t, taskFile, "task v2\n")
			if layout.tracked {
				git(t, dir, "add", ".tasks/1/1.md")
			}

			sha, committed, err := r.Snapshot("snapshot\n")
			if err != nil || !committed {
				t.Fatalf("Snapshot() = (%q, %v, %v), want a commit", sha, committed, err)
			}
			if got := git(t, dir, "rev-parse", "main_patched"); got != sha {
				t.Fatalf("main_patched = %s, want the snapshot %s", got, sha)
			}
			if got := git(t, dir, "rev-parse", sha+"^"); got != head {
				t.Errorf("snapshot parent = %s, want %s", got, head)
			}

			changed := lines(git(t, dir, "diff", "--name-only", head, sha))
			if want := []string{"README", "src/new.go", "staged.txt"}; !reflect.DeepEqual(changed, want) {
				t.Errorf("snapshot changed %q, want %q (and nothing under the tasks dir)", changed, want)
			}
			if tree := git(t, dir, "ls-tree", "-r", sha); strings.Contains(tree, "160000") {
				t.Errorf("snapshot contains a gitlink:\n%s", tree)
			}

			staged := lines(git(t, dir, "diff", "--cached", "--name-only"))
			var wantStaged []string
			if layout.tracked {
				wantStaged = []string{".tasks/1/1.md"}
			}
			if !reflect.DeepEqual(staged, wantStaged) {
				t.Errorf("staged after snapshot = %q, want %q: code must match the new HEAD, task staging must survive", staged, wantStaged)
			}
			if code := git(t, dir, "status", "--porcelain", "--", ".", ":(exclude).tasks"); code != "" {
				t.Errorf("code still dirty after snapshot:\n%s", code)
			}

			if got := readFile(t, filepath.Join(dir, "README")); got != "edited\n" {
				t.Errorf("README = %q, the worktree must not change", got)
			}
			if got := readFile(t, taskFile); got != "task v2\n" {
				t.Errorf("task record = %q, the worktree must not change", got)
			}
		})
	}
}

func TestSnapshotNoopWhenOnlyTasksDirty(t *testing.T) {
	testsupport.RequireGit(t)
	dir := testsupport.NewGitRepo(t, filepath.Join(t.TempDir(), "repo"))
	tasks := snapshotLayouts[0].setup(t, dir)
	head := git(t, dir, "rev-parse", "HEAD")
	testsupport.WriteFile(t, filepath.Join(tasks, "1", "1.md"), "task v2\n")
	testsupport.WriteFile(t, filepath.Join(tasks, "2", "2.md"), "new task\n")

	sha, committed, err := vcs.New(dir, tasks).Snapshot("snapshot\n")
	if err != nil || committed || sha != head {
		t.Fatalf("Snapshot() = (%q, %v, %v), want (%s, false, nil)", sha, committed, err, head)
	}
	if got := git(t, dir, "rev-parse", "main_patched"); got != head {
		t.Errorf("main_patched moved to %s", got)
	}
}

func TestSnapshotDetachedHead(t *testing.T) {
	dir, r := newRepo(t)
	git(t, dir, "switch", "-q", "--detach")
	testsupport.WriteFile(t, filepath.Join(dir, "README"), "edited\n")
	if _, _, err := r.Snapshot("snapshot\n"); !errors.Is(err, vcs.ErrDetachedHead) {
		t.Errorf("Snapshot() error = %v, want ErrDetachedHead", err)
	}
}

func TestDirtyPathsExcludesTasks(t *testing.T) {
	dir, r := newRepo(t)
	commit(t, dir, "files", map[string]string{"a.txt": "a\n", ".tasks/1/1.md": "task\n"})

	if paths, err := r.DirtyPaths(); err != nil || len(paths) != 0 {
		t.Fatalf("clean DirtyPaths() = (%q, %v), want none", paths, err)
	}

	testsupport.WriteFile(t, filepath.Join(dir, "README"), "edited\n")
	testsupport.WriteFile(t, filepath.Join(dir, "new file.txt"), "new\n")
	git(t, dir, "mv", "a.txt", "b.txt")
	testsupport.WriteFile(t, filepath.Join(dir, ".tasks", "1", "1.md"), "edited task\n")
	testsupport.WriteFile(t, filepath.Join(dir, ".tasks", "2", "2.md"), "new task\n")

	paths, err := r.DirtyPaths()
	if err != nil {
		t.Fatalf("DirtyPaths() error = %v", err)
	}
	slices.Sort(paths)
	if want := []string{"README", "b.txt", "new file.txt"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("DirtyPaths() = %q, want %q", paths, want)
	}
}

func TestDirtyPathsAcrossLayouts(t *testing.T) {
	for _, layout := range snapshotLayouts {
		t.Run(layout.name, func(t *testing.T) {
			testsupport.RequireGit(t)
			dir := testsupport.NewGitRepo(t, filepath.Join(t.TempDir(), "repo"))
			tasks := layout.setup(t, dir)
			testsupport.WriteFile(t, filepath.Join(dir, "README"), "edited\n")
			testsupport.WriteFile(t, filepath.Join(tasks, "1", "1.md"), "task v2\n")

			paths, err := vcs.New(dir, tasks).DirtyPaths()
			if err != nil || !reflect.DeepEqual(paths, []string{"README"}) {
				t.Errorf("DirtyPaths() = (%q, %v), want ([README], nil)", paths, err)
			}
		})
	}
}

func TestIndexTreeRestore(t *testing.T) {
	dir, r := newRepo(t)
	testsupport.WriteFile(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")

	captured, err := r.IndexTree()
	if err != nil {
		t.Fatalf("IndexTree() error = %v", err)
	}
	testsupport.WriteFile(t, filepath.Join(dir, "README"), "edited\n")
	git(t, dir, "add", "README")
	git(t, dir, "rm", "-q", "--cached", "a.txt")

	if err := r.RestoreIndex(captured); err != nil {
		t.Fatalf("RestoreIndex() error = %v", err)
	}
	if got := git(t, dir, "write-tree"); got != captured {
		t.Errorf("index tree after restore = %s, want %s", got, captured)
	}
}

func TestMergeTrees(t *testing.T) {
	dir, r := newRepo(t)
	base := commit(t, dir, "base", map[string]string{"f": "base\n", "g": "base\n"})
	git(t, dir, "switch", "-q", "-c", "theirs")
	theirsClean := commit(t, dir, "theirs g", map[string]string{"g": "theirs\n"})
	theirsConflict := commit(t, dir, "theirs f", map[string]string{"f": "theirs\n"})
	git(t, dir, "switch", "-q", "main_patched")
	ours := commit(t, dir, "ours f", map[string]string{"f": "ours\n"})

	tree, conflicts, err := r.MergeTrees(base, ours, theirsClean)
	if err != nil || len(conflicts) != 0 || tree == "" {
		t.Fatalf("clean MergeTrees() = (%q, %q, %v), want a tree and no conflicts", tree, conflicts, err)
	}
	if got := git(t, dir, "show", tree+":g"); got != "theirs" {
		t.Errorf("merged g = %q, want theirs", got)
	}

	_, conflicts, err = r.MergeTrees(base, ours, theirsConflict)
	if err != nil || !reflect.DeepEqual(conflicts, []string{"f"}) {
		t.Errorf("conflicting MergeTrees() = (%q, %v), want ([f], nil)", conflicts, err)
	}
}

func TestCommitTree(t *testing.T) {
	dir, r := newRepo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")

	sha, err := r.CommitTree(tree, head, "feat: squashed\n\nbody line\n")
	if err != nil {
		t.Fatalf("CommitTree() error = %v", err)
	}
	if got := git(t, dir, "rev-parse", sha+"^"); got != head {
		t.Errorf("parent = %s, want %s", got, head)
	}
	if got := git(t, dir, "log", "-1", "--format=%B", sha); got != "feat: squashed\n\nbody line" {
		t.Errorf("message = %q", got)
	}
	if got := git(t, dir, "rev-parse", "main_patched"); got != head {
		t.Errorf("CommitTree moved main_patched to %s", got)
	}
}

func TestReplay(t *testing.T) {
	// setup: main_patched at base; feat = base + one commit; main_patched
	// then advances by one commit of its own.
	setup := func(t *testing.T, featFiles, mainFiles map[string]string) (dir string, r *vcs.Repo, base, featTip, mainTip string) {
		dir, r = newRepo(t)
		testsupport.RequireGitReplay(t)
		base = git(t, dir, "rev-parse", "HEAD")
		git(t, dir, "switch", "-q", "-c", "feat")
		featTip = commit(t, dir, "feat work", featFiles)
		git(t, dir, "switch", "-q", "main_patched")
		mainTip = commit(t, dir, "main work", mainFiles)
		return
	}

	t.Run("clean", func(t *testing.T) {
		dir, r, base, featTip, mainTip := setup(t, map[string]string{"a": "a\n"}, map[string]string{"m": "m\n"})
		newTip, conflicts, err := r.Replay("main_patched", base, "feat")
		if err != nil || len(conflicts) != 0 {
			t.Fatalf("Replay() = (%q, %q, %v)", newTip, conflicts, err)
		}
		if got := git(t, dir, "rev-parse", newTip+"^"); got != mainTip {
			t.Errorf("new tip's parent = %s, want main_patched %s", got, mainTip)
		}
		if got := git(t, dir, "ls-tree", "--name-only", newTip); got != "README\na\nm" {
			t.Errorf("new tip tree = %q, want README, a, m", got)
		}
		if got := git(t, dir, "rev-parse", "feat"); got != featTip {
			t.Errorf("feat moved to %s; Replay must not move the ref", got)
		}
	})

	t.Run("empty range", func(t *testing.T) {
		_, r, _, featTip, mainTip := setup(t, map[string]string{"a": "a\n"}, map[string]string{"m": "m\n"})
		newTip, conflicts, err := r.Replay("main_patched", featTip, "feat")
		if err != nil || len(conflicts) != 0 || newTip != mainTip {
			t.Errorf("Replay() = (%q, %q, %v), want (%s, none, nil)", newTip, conflicts, err, mainTip)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		dir, r, base, featTip, _ := setup(t, map[string]string{"README": "feat\n"}, map[string]string{"README": "main\n"})
		newTip, conflicts, err := r.Replay("main_patched", base, "feat")
		if err != nil || newTip != "" || !reflect.DeepEqual(conflicts, []string{"README"}) {
			t.Fatalf("Replay() = (%q, %q, %v), want conflicts [README]", newTip, conflicts, err)
		}
		if got := git(t, dir, "rev-parse", "feat"); got != featTip {
			t.Errorf("feat moved to %s", got)
		}
		if st := git(t, dir, "status", "--porcelain"); st != "" {
			t.Errorf("worktree changed:\n%s", st)
		}
		if err := r.Check(); err != nil {
			t.Errorf("Check() after conflict = %v, want nothing in progress", err)
		}
	})

	t.Run("merge commit", func(t *testing.T) {
		dir, r, base, _, _ := setup(t, map[string]string{"a": "a\n"}, map[string]string{"m": "m\n"})
		git(t, dir, "switch", "-q", "-c", "side", base)
		commit(t, dir, "side work", map[string]string{"s": "s\n"})
		git(t, dir, "switch", "-q", "feat")
		git(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
		git(t, dir, "switch", "-q", "main_patched")

		if _, _, err := r.Replay("main_patched", base, "feat"); !errors.Is(err, vcs.ErrReplayMerge) {
			t.Errorf("Replay() error = %v, want ErrReplayMerge", err)
		}
	})
}

func TestSwitchRefusesOverwrite(t *testing.T) {
	dir, r := newRepo(t)
	git(t, dir, "switch", "-q", "-c", "other")
	commit(t, dir, "other", map[string]string{"README": "other\n"})
	git(t, dir, "switch", "-q", "main_patched")
	testsupport.WriteFile(t, filepath.Join(dir, "README"), "local\n")

	if err := r.Switch("other"); err == nil {
		t.Fatal("Switch() over a conflicting local change: error = nil")
	}
	if got := git(t, dir, "branch", "--show-current"); got != "main_patched" {
		t.Errorf("HEAD = %s, want main_patched", got)
	}
	if got := readFile(t, filepath.Join(dir, "README")); got != "local\n" {
		t.Errorf("README = %q, want the local change kept", got)
	}

	if err := r.Switch("no-such-branch"); err == nil {
		t.Error("Switch() to a missing branch: error = nil")
	}
}

func TestResetKeepRefusesConflict(t *testing.T) {
	dir, r := newRepo(t)
	c1 := git(t, dir, "rev-parse", "HEAD")
	c2 := commit(t, dir, "v2", map[string]string{"README": "v2\n"})
	testsupport.WriteFile(t, filepath.Join(dir, "README"), "local\n")

	if err := r.ResetKeep(c1); err == nil {
		t.Fatal("ResetKeep() over a conflicting local change: error = nil")
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != c2 {
		t.Errorf("HEAD = %s, want %s", got, c2)
	}
	if got := readFile(t, filepath.Join(dir, "README")); got != "local\n" {
		t.Errorf("README = %q, want the local change kept", got)
	}

	git(t, dir, "checkout", "--", "README")
	testsupport.WriteFile(t, filepath.Join(dir, "other.txt"), "untouched\n")
	if err := r.ResetKeep(c1); err != nil {
		t.Fatalf("ResetKeep() without conflict error = %v", err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != c1 {
		t.Errorf("HEAD = %s, want %s", got, c1)
	}
}

func TestGitNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "")
	if err := vcs.New(dir, filepath.Join(dir, ".tasks")).Check(); !errors.Is(err, vcs.ErrGitNotFound) {
		t.Errorf("Check() error = %v, want ErrGitNotFound", err)
	}
}
