package task_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
	"github.com/gpayer/mcp-task-manager/internal/vcs"
)

var errInjected = errors.New("injected failure")

// failingGit wraps a GitRepo and fails its failAt-th mutating call (1-based;
// 0 disables it). After that one failure every call passes through, so the
// rollback's own undo calls never fail.
type failingGit struct {
	task.GitRepo
	failAt    int
	mutations int
	failed    bool
}

func (f *failingGit) step(op string) error {
	if f.failAt == 0 || f.failed {
		return nil
	}
	f.mutations++
	if f.mutations == f.failAt {
		f.failed = true
		return fmt.Errorf("%s: %w", op, errInjected)
	}
	return nil
}

// arm makes the n-th mutating call from now on fail.
func (f *failingGit) arm(n int) {
	f.failAt, f.mutations, f.failed = n, 0, false
}

func (f *failingGit) Snapshot(msg string) (string, bool, error) {
	if err := f.step("Snapshot"); err != nil {
		return "", false, err
	}
	return f.GitRepo.Snapshot(msg)
}

func (f *failingGit) CreateBranch(name, sha string) error {
	if err := f.step("CreateBranch"); err != nil {
		return err
	}
	return f.GitRepo.CreateBranch(name, sha)
}

func (f *failingGit) MoveBranch(name, newSHA, oldSHA string) error {
	if err := f.step("MoveBranch"); err != nil {
		return err
	}
	return f.GitRepo.MoveBranch(name, newSHA, oldSHA)
}

func (f *failingGit) DeleteBranch(name, expectSHA string) error {
	if err := f.step("DeleteBranch"); err != nil {
		return err
	}
	return f.GitRepo.DeleteBranch(name, expectSHA)
}

func (f *failingGit) Switch(name string) error {
	if err := f.step("Switch"); err != nil {
		return err
	}
	return f.GitRepo.Switch(name)
}

func (f *failingGit) ResetKeep(sha string) error {
	if err := f.step("ResetKeep"); err != nil {
		return err
	}
	return f.GitRepo.ResetKeep(sha)
}

func (f *failingGit) RestoreIndex(tree string) error {
	if err := f.step("RestoreIndex"); err != nil {
		return err
	}
	return f.GitRepo.RestoreIndex(tree)
}

func (f *failingGit) CommitTree(tree, parent, msg string) (string, error) {
	if err := f.step("CommitTree"); err != nil {
		return "", err
	}
	return f.GitRepo.CommitTree(tree, parent, msg)
}

func (f *failingGit) MergeTrees(base, ours, theirs string) (string, []string, error) {
	if err := f.step("MergeTrees"); err != nil {
		return "", nil, err
	}
	return f.GitRepo.MergeTrees(base, ours, theirs)
}

func (f *failingGit) Replay(onto, upstream, branch string) (string, []string, error) {
	if err := f.step("Replay"); err != nil {
		return "", nil, err
	}
	return f.GitRepo.Replay(onto, upstream, branch)
}

// failingStore wraps a CurrentTaskStore whose writes fail while fail is set.
type failingStore struct {
	task.CurrentTaskStore
	fail bool
}

func (f *failingStore) WriteCurrentTask(user, id string) error {
	if f.fail {
		return fmt.Errorf("WriteCurrentTask: %w", errInjected)
	}
	return f.CurrentTaskStore.WriteCurrentTask(user, id)
}

// newFaultBacklog is a git backlog whose repository and pointer store are
// wrapped for fault injection, both disarmed.
func newFaultBacklog(t *testing.T, layout testsupport.Layout) (*testsupport.GitBacklog, *failingGit, *failingStore) {
	t.Helper()
	fg, fs := &failingGit{}, &failingStore{}
	b := testsupport.NewGitBacklog(t, layout, task.WithGit(fg), task.WithCurrentTaskStore(fs))
	// Build runs no git, so the wrapped values can be filled in afterwards.
	fg.GitRepo = vcs.New(b.CodeDir, b.TasksDir)
	fs.CurrentTaskStore = storage.NewMarkdownStorage(b.TasksDir)
	return b, fg, fs
}

// runFaults runs op against a fresh backlog prepared by setup, failing the
// 1st, 2nd, ... mutating git call until op succeeds. Every failed run must
// report the injected failure and leave the whole state unchanged.
func runFaults(t *testing.T, layout testsupport.Layout, setup func(t *testing.T, b *testsupport.GitBacklog), op func(b *testsupport.GitBacklog) error) {
	t.Helper()
	for n := 1; ; n++ {
		if n > 40 {
			t.Fatal("op still failing after 40 injected faults")
		}
		b, fg, _ := newFaultBacklog(t, layout)
		setup(t, b)
		before := testsupport.CaptureState(t, b)
		fg.arm(n)
		err := op(b)
		if !fg.failed {
			if err != nil {
				t.Fatalf("op with no fault left to inject: %v", err)
			}
			if n == 1 {
				t.Fatal("op made no mutating git call")
			}
			return
		}
		if !errors.Is(err, errInjected) {
			t.Fatalf("fault %d: error = %v, want the injected failure", n, err)
		}
		t.Run(fmt.Sprintf("fault %d", n), func(t *testing.T) {
			testsupport.RequireStateEqual(t, before, testsupport.CaptureState(t, b))
		})
	}
}

// Helpers shared by the branching tests.

func mustCreate(t *testing.T, b *testsupport.GitBacklog, id, parentID string) *task.Task {
	t.Helper()
	created, err := b.Svc.Create("Task "+id, "", task.PriorityHigh, "feature", parentID, id)
	if err != nil {
		t.Fatalf("Create(%s) error = %v", id, err)
	}
	return created
}

func mustStart(t *testing.T, b *testsupport.GitBacklog, id string) *task.Task {
	t.Helper()
	started, err := b.Svc.StartTask(id)
	if err != nil {
		t.Fatalf("StartTask(%s) error = %v", id, err)
	}
	return started
}

func mustGet(t *testing.T, b *testsupport.GitBacklog, id string) *task.Task {
	t.Helper()
	got, err := b.Svc.Get(id)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", id, err)
	}
	return got
}

func writeCode(t *testing.T, b *testsupport.GitBacklog, name, content string) {
	t.Helper()
	testsupport.WriteFile(t, filepath.Join(b.CodeDir, name), content)
}

func wantErr(t *testing.T, err error, substrs ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", substrs)
	}
	for _, s := range substrs {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error = %q, want it to contain %q", err, s)
		}
	}
}

// requireNoServerCommitInTasks fails if any commit in the code repository
// other than the layout's own setup commit touches the tasks directory.
func requireNoServerCommitInTasks(t *testing.T, b *testsupport.GitBacklog) {
	t.Helper()
	rel, err := filepath.Rel(b.CodeDir, b.TasksDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	got := b.Git(t, "log", "--all", "--format=%s", "--", rel)
	want := ""
	if b.Layout == testsupport.TasksInRepoTracked {
		want = "track tasks"
	}
	if got != want {
		t.Errorf("commits touching %s: %q, want %q", rel, got, want)
	}
}
