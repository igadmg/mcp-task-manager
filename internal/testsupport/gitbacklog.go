package testsupport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// Layout is where a git backlog's tasks directory lives relative to the
// code repository.
type Layout string

const (
	// TasksInRepoTracked: <code>/.tasks, committed in the code repository.
	TasksInRepoTracked Layout = "tracked"
	// TasksInRepoIgnored: <code>/.tasks, listed in the code repo's .gitignore.
	TasksInRepoIgnored Layout = "ignored"
	// TasksNestedRepo: <code>/.tasks is a git repository of its own.
	TasksNestedRepo Layout = "nested"
	// TasksSeparateRepo: a git repository outside the code repository.
	TasksSeparateRepo Layout = "separate"
	// TasksUntracked: a plain directory outside the code repository.
	TasksUntracked Layout = "untracked"
)

// AllLayouts lists every Layout, for table-driven tests.
var AllLayouts = []Layout{TasksInRepoTracked, TasksInRepoIgnored, TasksNestedRepo, TasksSeparateRepo, TasksUntracked}

// GitBacklog is a project with git branching on: a code repository on
// main_patched and a tasks directory laid out per Layout.
type GitBacklog struct {
	Svc      *task.Service
	Resolver *project.Resolver
	CodeDir  string
	TasksDir string
	// TasksRepo is the tasks directory's own repository (nested and
	// separate layouts), empty otherwise.
	TasksRepo string
	Layout    Layout
}

// NewGitBacklog builds a git backlog with branching enabled through
// project.Build, as production does; opts apply last, so a test can wrap
// the repository (task.WithGit) or replace the identity or pointer store.
// The identity resolves to "dev" from the repository's user.email.
//
// Seed it with Svc.Create only: status moves through Update are refused
// for branched projects, and every start must go through StartTask.
// It calls RequireGit, so callers must not use t.Parallel.
func NewGitBacklog(t *testing.T, layout Layout, opts ...task.ServiceOption) *GitBacklog {
	t.Helper()
	RequireGit(t)
	base := realDir(t, t.TempDir())
	code := NewGitRepo(t, filepath.Join(base, "code"))

	b := &GitBacklog{CodeDir: code, Layout: layout}
	switch layout {
	case TasksInRepoTracked:
		b.TasksDir = filepath.Join(code, ".tasks")
		WriteFile(t, filepath.Join(b.TasksDir, "README.md"), "tasks\n")
		Git(t, code, "add", ".tasks")
		Git(t, code, "commit", "-q", "-m", "track tasks")
	case TasksInRepoIgnored:
		b.TasksDir = filepath.Join(code, ".tasks")
		WriteFile(t, filepath.Join(code, ".gitignore"), ".tasks/\n")
		Git(t, code, "add", ".gitignore")
		Git(t, code, "commit", "-q", "-m", "ignore tasks")
		mkdir(t, b.TasksDir)
	case TasksNestedRepo:
		b.TasksDir = NewGitRepo(t, filepath.Join(code, ".tasks"))
		b.TasksRepo = b.TasksDir
	case TasksSeparateRepo:
		b.TasksDir = NewGitRepo(t, filepath.Join(base, "tasks"))
		b.TasksRepo = b.TasksDir
	case TasksUntracked:
		b.TasksDir = filepath.Join(base, "tasks")
		mkdir(t, b.TasksDir)
	default:
		t.Fatalf("unknown layout %q", layout)
	}

	cfg := &config.Config{
		TaskTypes:     []string{"feature", "bug"},
		RelationTypes: config.DefaultRelationTypes,
		DataDir:       b.TasksDir,
		ProjectFound:  true,
		Resolution:    &config.Resolution{Root: code, TasksDir: b.TasksDir, Source: config.SourceProjectEnv},
		Web:           config.DefaultConfig().Web,
		Git: config.GitConfig{
			Branching:    true,
			BaseBranches: append([]string(nil), config.DefaultBaseBranches...),
		},
	}
	resolved, err := project.Build(cfg, opts...)
	if err != nil {
		t.Fatalf("project.Build() error = %v", err)
	}
	b.Svc = resolved.Service
	b.Resolver = project.NewStatic(resolved)
	return b
}

func realDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s) error = %v", dir, err)
	}
	return real
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", dir, err)
	}
}

// Git runs git in the code repository.
func (b *GitBacklog) Git(t *testing.T, args ...string) string {
	t.Helper()
	return Git(t, b.CodeDir, args...)
}

// Head is the checked-out branch of the code repository.
func (b *GitBacklog) Head(t *testing.T) string {
	t.Helper()
	return b.Git(t, "branch", "--show-current")
}

// PointerPath is where user dev's current-task pointer is kept.
func (b *GitBacklog) PointerPath() string {
	return filepath.Join(b.TasksDir, storage.UsersDirName, "dev", storage.CurrentTaskFileName)
}

// State is everything a git flow may change, captured to prove that a
// refused or failed call changed none of it.
type State struct {
	Refs    string
	Head    string
	Index   string
	Status  string
	Files   map[string]string // code-repo dirty path -> content hash, outside the tasks dir
	Records map[string]task.Task
	Pointer string // "<absent>" or the file's content
	// TasksRepo is the tasks repository's HEAD, log and index tree.
	TasksRepo string
}

// CaptureState records b's current State.
func CaptureState(t *testing.T, b *GitBacklog) State {
	t.Helper()
	s := State{
		Refs:   b.Git(t, "for-each-ref", "--format=%(refname) %(objectname)"),
		Head:   headOf(t, b.CodeDir),
		Index:  b.Git(t, "write-tree"),
		Status: b.Git(t, "status", "--porcelain=v1", "-z", "--untracked-files=all"),
		Files:  map[string]string{},
	}

	tasksRel := ""
	if rel, err := filepath.Rel(b.CodeDir, b.TasksDir); err == nil && !strings.HasPrefix(rel, "..") {
		tasksRel = filepath.ToSlash(rel) + "/"
	}
	for _, p := range statusPaths(s.Status) {
		if tasksRel != "" && strings.HasPrefix(p, tasksRel) {
			continue // records are compared semantically below
		}
		data, err := os.ReadFile(filepath.Join(b.CodeDir, p))
		switch {
		case err == nil:
			sum := sha256.Sum256(data)
			s.Files[p] = hex.EncodeToString(sum[:])
		case errors.Is(err, os.ErrNotExist):
			s.Files[p] = "<absent>"
		default:
			s.Files[p] = "<unreadable: " + err.Error() + ">"
		}
	}

	st := storage.NewMarkdownStorage(b.TasksDir)
	s.Records = map[string]task.Task{}
	for _, load := range []func() ([]*task.Task, error){st.LoadAll, st.LoadAllArchived} {
		tasks, err := load()
		if err != nil {
			t.Fatalf("loading records: %v", err)
		}
		for _, tk := range tasks {
			s.Records[tk.ID] = *tk
		}
	}

	s.Pointer = "<absent>"
	if data, err := os.ReadFile(b.PointerPath()); err == nil {
		s.Pointer = string(data)
	}

	if b.TasksRepo != "" {
		s.TasksRepo = strings.Join([]string{
			headOf(t, b.TasksRepo),
			Git(t, b.TasksRepo, "log", "--oneline", "--all"),
			Git(t, b.TasksRepo, "write-tree"),
		}, "\n")
	}
	return s
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	if branch := Git(t, dir, "branch", "--show-current"); branch != "" {
		return "ref: " + branch
	}
	return Git(t, dir, "rev-parse", "HEAD")
}

// statusPaths parses `status --porcelain=v1 -z` output into paths.
func statusPaths(status string) []string {
	var paths []string
	fields := strings.Split(status, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		if strings.ContainsAny(entry[:2], "RC") {
			i++
		}
	}
	sort.Strings(paths)
	return paths
}

// RequireStateEqual fails the test for every part of the state that
// differs between before and after.
func RequireStateEqual(t *testing.T, before, after State) {
	t.Helper()
	for _, c := range []struct {
		name          string
		before, after any
	}{
		{"refs", before.Refs, after.Refs},
		{"HEAD", before.Head, after.Head},
		{"index tree", before.Index, after.Index},
		{"worktree status", before.Status, after.Status},
		{"worktree files", before.Files, after.Files},
		{"task records", before.Records, after.Records},
		{"current-task pointer", before.Pointer, after.Pointer},
		{"tasks repository", before.TasksRepo, after.TasksRepo},
	} {
		if !reflect.DeepEqual(c.before, c.after) {
			t.Errorf("%s changed:\nbefore: %v\nafter:  %v", c.name, c.before, c.after)
		}
	}
}
