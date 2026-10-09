// Package testsupport builds throwaway backlogs for tests in the packages
// above internal/task: internal/web, internal/app and internal/tools.
//
// It is a normal package that imports testing, the way net/http/httptest is.
// It deliberately cannot be used from internal/task's or internal/project's
// own tests - those are in-package and importing this would cycle.
package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// NewBacklog creates an empty project in a temp directory and returns a
// resolver pinned to it, the service behind it, and the tasks directory.
func NewBacklog(t *testing.T) (*project.Resolver, *task.Service, string) {
	t.Helper()

	dir := t.TempDir()
	st := storage.NewMarkdownStorage(dir)
	idx := storage.NewIndex(dir, st)
	cfg := &config.Config{
		TaskTypes:     []string{"feature", "bug"},
		RelationTypes: config.DefaultRelationTypes,
		DataDir:       dir,
		ProjectFound:  true,
		Resolution:    &config.Resolution{Root: dir, TasksDir: dir, Source: config.SourceProjectEnv},
		Web:           config.DefaultConfig().Web,
	}
	svc := task.NewService(st, st, st, idx, cfg.TaskTypes, cfg,
		task.WithCurrentTaskStore(st), task.WithPhaseStore(st), task.WithIdentity(task.Identity{Name: "dev"}))
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	return project.NewStatic(&project.Resolved{Config: cfg, Service: svc}), svc, dir
}

// TaskSpec describes one task to seed. ID is always explicit so tests never
// depend on auto-increment ordering.
type TaskSpec struct {
	ID        string
	Title     string
	Status    string
	Priority  string
	Type      string
	ParentID  string
	BlockedBy []string
}

// Seed creates the given tasks in order, then wires their blocked_by edges.
// Parents must be listed before their subtasks, and blockers may appear in
// any position: relations are applied after every task exists.
func Seed(t *testing.T, svc *task.Service, specs ...TaskSpec) {
	t.Helper()

	for _, spec := range specs {
		title := spec.Title
		if title == "" {
			title = "task " + spec.ID
		}
		priority := spec.Priority
		if priority == "" {
			priority = string(task.PriorityMedium)
		}
		taskType := spec.Type
		if taskType == "" {
			taskType = "feature"
		}

		if _, err := svc.Create(title, "", task.Priority(priority), taskType, spec.ParentID, spec.ID); err != nil {
			t.Fatalf("Create(%s) error = %v", spec.ID, err)
		}
	}

	for _, spec := range specs {
		for _, blocker := range spec.BlockedBy {
			if err := svc.AddRelation(spec.ID, "blocked_by", blocker); err != nil {
				t.Fatalf("AddRelation(%s blocked_by %s) error = %v", spec.ID, blocker, err)
			}
		}
	}

	// Statuses come last: starting a subtask auto-starts its parent, so
	// applying them during creation would fight the explicit values.
	for _, spec := range specs {
		status := task.Status(spec.Status)
		if status == "" || status == task.StatusTodo {
			continue
		}
		inProgress := task.StatusInProgress
		if _, err := svc.Update(spec.ID, nil, nil, &inProgress, nil, nil); err != nil {
			t.Fatalf("Update(%s -> in_progress) error = %v", spec.ID, err)
		}
		if status == task.StatusDone {
			if _, err := svc.Update(spec.ID, nil, nil, &status, nil, nil); err != nil {
				t.Fatalf("Update(%s -> done) error = %v", spec.ID, err)
			}
		}
	}
}

// IsolateEnv clears every environment variable project resolution reads, so a
// test never picks up the developer's own project.
func IsolateEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		config.EnvTasksDir,
		config.EnvProjectDir,
		config.EnvClaudeProjectDir,
		config.EnvRootSource,
		config.EnvWebEnabled,
		config.EnvWebAddr,
		config.EnvGitBranching,
		config.EnvWebConfig,
		config.EnvXDGConfigHome,
	} {
		// t.Setenv registers the restore; Unsetenv then makes it actually
		// absent rather than set-to-empty, which resolution treats alike but
		// os.LookupEnv does not.
		t.Setenv(name, "")
		os.Unsetenv(name)
	}

	// The web server's own workspace file is looked up under the user's
	// config directory, so a test must not be able to read - let alone open
	// a backlog from - the developer's real ~/.config.
	t.Setenv("HOME", t.TempDir())
}

// BuildDashboard builds cmd/mcp-task-manager-web into a temporary directory
// and returns the binary's path.
//
// It exists because the dashboard became its own program: `serve web` runs it,
// start_web_ui spawns it, and a test of either has to have one to run. Built
// once per test rather than relying on an installed copy, so a test never
// silently exercises a stale binary from GOBIN.
//
// Skips the test when `go` is unavailable or under -short: building takes a
// second or two, and nothing else here needs a toolchain.
func BuildDashboard(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("building the dashboard binary takes a moment; skipped under -short")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain to build the dashboard with")
	}

	root := moduleRoot(t)
	out := filepath.Join(t.TempDir(), "mcp-task-manager-web")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command(goBin, "build", "-o", out, "./cmd/mcp-task-manager-web")
	cmd.Dir = root
	// GOWORK=off for the same reason the MCP config and the open_board skill
	// use it: a stray go.work would resolve different dependencies than the
	// module's own.
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the dashboard: %v\n%s", err, out)
	}
	return out
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod, which is where a `go build ./cmd/...` has to run.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
