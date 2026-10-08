package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// readOnlyConfig is a project pinned to tasksDir, with no env involvement.
func readOnlyConfig(t *testing.T, root, tasksDir string) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataDir = tasksDir
	cfg.ProjectFound = true
	cfg.Resolution = &config.Resolution{Root: root, TasksDir: tasksDir, Source: config.SourceProjectEnv}
	return cfg
}

func writeTaskRecord(t *testing.T, path, id, status, updated string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	record := "---\nid: " + id + "\ntitle: \"task " + id + "\"\nstatus: " + status +
		"\npriority: medium\ntype: feature\ncreated_at: " + updated +
		"\nupdated_at: " + updated + "\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

// TestBuildReadOnlyDoesNotMigrateTheLegacyLayout is the core of the read-only
// contract: a task still in the old flat layout stays where it is. Build
// would have moved it into tasks/7/7.md.
func TestBuildReadOnlyDoesNotMigrateTheLegacyLayout(t *testing.T) {
	isolateEnv(t)
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	flat := filepath.Join(tasksDir, "7.md")
	writeTaskRecord(t, flat, "7", "todo", "2026-01-01T00:00:00Z")

	if _, err := BuildReadOnly(readOnlyConfig(t, root, tasksDir)); err != nil {
		t.Fatalf("BuildReadOnly() error = %v", err)
	}

	if _, err := os.Stat(flat); err != nil {
		t.Errorf("the flat record was migrated away: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tasksDir, "7", "7.md")); err == nil {
		t.Error("BuildReadOnly migrated the legacy layout, want it left alone")
	}
}

// Build is the writable twin, and migrating is exactly what it is for. The
// two assertions together are what makes the difference a contract rather
// than an accident.
func TestBuildMigratesTheLegacyLayout(t *testing.T) {
	isolateEnv(t)
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	writeTaskRecord(t, filepath.Join(tasksDir, "7.md"), "7", "todo", "2026-01-01T00:00:00Z")

	if _, err := Build(readOnlyConfig(t, root, tasksDir)); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tasksDir, "7", "7.md")); err != nil {
		t.Errorf("Build did not migrate the legacy layout: %v", err)
	}
}

func TestBuildReadOnlyDoesNotAutoArchive(t *testing.T) {
	isolateEnv(t)
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	old := time.Now().AddDate(0, 0, -90).UTC().Format(time.RFC3339)
	writeTaskRecord(t, filepath.Join(tasksDir, "3", "3.md"), "3", "done", old)

	cfg := readOnlyConfig(t, root, tasksDir)
	cfg.AutoArchive = config.AutoArchiveConfig{Enabled: true, AfterDays: 30}

	resolved, err := BuildReadOnly(cfg)
	if err != nil {
		t.Fatalf("BuildReadOnly() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tasksDir, "3", "3.md")); err != nil {
		t.Errorf("BuildReadOnly auto-archived a done task: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tasksDir, "archive")); err == nil {
		t.Error("BuildReadOnly created an archive directory")
	}

	// And it still has to answer: read-only is not inert.
	snap, err := resolved.Service.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	if len(snap.Tasks) != 1 {
		t.Errorf("BoardSnapshot() returned %d tasks, want 1", len(snap.Tasks))
	}
}

// Load removes the retired shared cache file; Rebuild does not. A read-only
// open must not even do that much.
func TestBuildReadOnlyKeepsTheRetiredIndexCacheFile(t *testing.T) {
	isolateEnv(t)
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	writeTaskRecord(t, filepath.Join(tasksDir, "1", "1.md"), "1", "todo", "2026-01-01T00:00:00Z")
	cache := filepath.Join(tasksDir, ".index.json")
	if err := os.WriteFile(cache, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := BuildReadOnly(readOnlyConfig(t, root, tasksDir)); err != nil {
		t.Fatalf("BuildReadOnly() error = %v", err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("BuildReadOnly removed .index.json: %v", err)
	}
}

// Branching is never wired into a read-only project: it would hand a service
// that only reads a handle to the repository.
func TestBuildReadOnlySkipsGitEvenWhenBranchingIsOn(t *testing.T) {
	isolateEnv(t)
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	writeTaskRecord(t, filepath.Join(tasksDir, "1", "1.md"), "1", "todo", "2026-01-01T00:00:00Z")

	cfg := readOnlyConfig(t, root, tasksDir)
	cfg.Git.Branching = true

	resolved, err := BuildReadOnly(cfg)
	if err != nil {
		t.Fatalf("BuildReadOnly() error = %v", err)
	}
	if resolved.Service.BranchingEnabled() {
		t.Error("BranchingEnabled() = true for a read-only project, want false")
	}
}
