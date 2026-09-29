// Package testsupport builds throwaway backlogs for tests in the packages
// above internal/task: internal/web, internal/app and internal/tools.
//
// It is a normal package that imports testing, the way net/http/httptest is.
// It deliberately cannot be used from internal/task's or internal/project's
// own tests - those are in-package and importing this would cycle.
package testsupport

import (
	"os"
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
	svc := task.NewService(st, st, st, idx, cfg.TaskTypes, cfg)
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
	} {
		// t.Setenv registers the restore; Unsetenv then makes it actually
		// absent rather than set-to-empty, which resolution treats alike but
		// os.LookupEnv does not.
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}
