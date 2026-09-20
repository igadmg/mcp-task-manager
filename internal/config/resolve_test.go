package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// mkdirs creates directories below root and returns root.
func mkdirs(t *testing.T, root string, names ...string) string {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", name, err)
		}
	}
	return root
}

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ConfigFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func staticRoots(paths ...string) RootsProvider {
	return func() ([]string, error) { return paths, nil }
}

func TestResolve_AbsoluteTasksDirEnvWins(t *testing.T) {
	isolateEnv(t)
	other := mkdirs(t, tempDir(t), DefaultTasksDirName)
	tasks := mkdirs(t, tempDir(t), "backlog")

	t.Setenv(EnvClaudeProjectDir, other)
	t.Setenv(EnvTasksDir, filepath.Join(tasks, "backlog"))

	cfg, err := Resolve(staticRoots(other))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := filepath.Join(tasks, "backlog"); cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	if cfg.Resolution.Root != tasks {
		t.Errorf("Root = %q, want %q", cfg.Resolution.Root, tasks)
	}
	if cfg.Resolution.Source != SourceTasksDirEnv {
		t.Errorf("Source = %q, want %q", cfg.Resolution.Source, SourceTasksDirEnv)
	}
}

func TestResolve_RelativeTasksDirEnvIsProjectRelative(t *testing.T) {
	isolateEnv(t)
	root := mkdirs(t, tempDir(t), "backlog")
	// A different working directory: the relative value must not be
	// resolved against it.
	t.Chdir(mkdirs(t, tempDir(t), "elsewhere"))
	t.Setenv(EnvClaudeProjectDir, root)
	t.Setenv(EnvTasksDir, "backlog")

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := filepath.Join(root, "backlog"); cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	if !cfg.ProjectFound {
		t.Error("ProjectFound = false, want true")
	}
}

func TestResolve_ProjectDirEnvBeatsClaudeProjectDir(t *testing.T) {
	isolateEnv(t)
	explicit := mkdirs(t, tempDir(t), DefaultTasksDirName)
	claude := mkdirs(t, tempDir(t), DefaultTasksDirName)

	t.Setenv(EnvProjectDir, explicit)
	t.Setenv(EnvClaudeProjectDir, claude)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Resolution.Root != explicit {
		t.Errorf("Root = %q, want %q", cfg.Resolution.Root, explicit)
	}
	if cfg.Resolution.Source != SourceProjectEnv {
		t.Errorf("Source = %q, want %q", cfg.Resolution.Source, SourceProjectEnv)
	}
}

func TestResolve_ClaudeProjectDirWithConfiguredTasksDir(t *testing.T) {
	isolateEnv(t)
	root := mkdirs(t, tempDir(t), "backlog")
	writeConfig(t, root, "tasks_dir: backlog\ntask_types:\n  - feature\n  - chore\n")
	t.Setenv(EnvClaudeProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := filepath.Join(root, "backlog"); cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	if !cfg.IsValidTaskType("chore") {
		t.Error("config from the project root was not loaded")
	}
	if cfg.Resolution.Source != SourceClaudeEnv {
		t.Errorf("Source = %q, want %q", cfg.Resolution.Source, SourceClaudeEnv)
	}
}

func TestResolve_MissingClaudeProjectDirFallsThroughToRoots(t *testing.T) {
	isolateEnv(t)
	root := mkdirs(t, tempDir(t), DefaultTasksDirName)
	t.Setenv(EnvClaudeProjectDir, filepath.Join(tempDir(t), "gone"))

	cfg, err := Resolve(staticRoots(root))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Resolution.Source != SourceRoots {
		t.Errorf("Source = %q, want %q", cfg.Resolution.Source, SourceRoots)
	}
	if len(cfg.Resolution.Attempts) == 0 {
		t.Error("Attempts is empty, want the rejected CLAUDE_PROJECT_DIR recorded")
	}
}

func TestResolve_RootsPrefersDirectoryHoldingAProject(t *testing.T) {
	isolateEnv(t)
	bare := tempDir(t)
	withProject := mkdirs(t, tempDir(t), DefaultTasksDirName)

	cfg, err := Resolve(staticRoots(bare, withProject))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Resolution.Root != withProject {
		t.Errorf("Root = %q, want %q", cfg.Resolution.Root, withProject)
	}
}

func TestResolve_RootsFailureFallsThroughToCwd(t *testing.T) {
	isolateEnv(t)
	root := mkdirs(t, tempDir(t), DefaultTasksDirName)
	t.Chdir(root)

	cfg, err := Resolve(func() ([]string, error) { return nil, errors.New("no roots capability") })
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Resolution.Source != SourceCwd {
		t.Errorf("Source = %q, want %q", cfg.Resolution.Source, SourceCwd)
	}
	if cfg.Resolution.Root != root {
		t.Errorf("Root = %q, want %q", cfg.Resolution.Root, root)
	}
}

func TestResolve_RootSourceForcesRoots(t *testing.T) {
	isolateEnv(t)
	claude := mkdirs(t, tempDir(t), DefaultTasksDirName)
	fromRoots := mkdirs(t, tempDir(t), DefaultTasksDirName)

	t.Setenv(EnvClaudeProjectDir, claude)
	t.Setenv(EnvRootSource, string(SourceRoots))

	cfg, err := Resolve(staticRoots(fromRoots))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Resolution.Root != fromRoots {
		t.Errorf("Root = %q, want %q", cfg.Resolution.Root, fromRoots)
	}
}

func TestResolve_TasksDirNamePreference(t *testing.T) {
	isolateEnv(t)

	t.Run("dot tasks wins over legacy", func(t *testing.T) {
		root := mkdirs(t, tempDir(t), DefaultTasksDirName, LegacyTasksDirName)
		t.Setenv(EnvClaudeProjectDir, root)

		cfg, err := Resolve(nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := filepath.Join(root, DefaultTasksDirName); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
	})

	t.Run("legacy used when it is the only one", func(t *testing.T) {
		root := mkdirs(t, tempDir(t), LegacyTasksDirName)
		t.Setenv(EnvClaudeProjectDir, root)

		cfg, err := Resolve(nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := filepath.Join(root, LegacyTasksDirName); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
	})
}

func TestResolve_ConfigComesFromProjectRootNotTasksParent(t *testing.T) {
	isolateEnv(t)
	root := mkdirs(t, tempDir(t), filepath.Join("nested", "backlog"))
	writeConfig(t, root, "task_types:\n  - feature\n  - chore\n")
	// A decoy config next to the tasks directory must be ignored.
	writeConfig(t, filepath.Join(root, "nested"), "task_types:\n  - decoy\n")

	t.Setenv(EnvClaudeProjectDir, root)
	t.Setenv(EnvTasksDir, filepath.Join("nested", "backlog"))

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !cfg.IsValidTaskType("chore") {
		t.Errorf("TaskTypes = %v, want the project root config", cfg.TaskTypes)
	}
	if cfg.IsValidTaskType("decoy") {
		t.Errorf("TaskTypes = %v, want the decoy config ignored", cfg.TaskTypes)
	}
}

// writeTask creates a per-task directory with its record file, the shape the
// storage layer uses.
func writeTask(t *testing.T, tasksDir, id string) {
	t.Helper()
	dir := filepath.Join(tasksDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte("---\nid: "+id+"\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestResolve_PrefersTheDirectoryThatHoldsTasks(t *testing.T) {
	isolateEnv(t)

	t.Run("legacy backlog wins over an unrelated .tasks", func(t *testing.T) {
		// This is the plugin's own repository layout: .tasks holds workflow
		// notes, while the real backlog is still tasks/ with an archive.
		root := mkdirs(t, tempDir(t),
			filepath.Join(DefaultTasksDirName, "some-notes"),
			filepath.Join(LegacyTasksDirName, "archive"))
		if err := os.WriteFile(filepath.Join(root, DefaultTasksDirName, "some-notes", "design.md"), []byte("notes\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		t.Setenv(EnvClaudeProjectDir, root)

		cfg, err := Resolve(nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := filepath.Join(root, LegacyTasksDirName); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
	})

	t.Run(".tasks wins when both hold tasks", func(t *testing.T) {
		root := mkdirs(t, tempDir(t), DefaultTasksDirName, LegacyTasksDirName)
		writeTask(t, filepath.Join(root, DefaultTasksDirName), "1")
		writeTask(t, filepath.Join(root, LegacyTasksDirName), "2")
		t.Setenv(EnvClaudeProjectDir, root)

		cfg, err := Resolve(nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := filepath.Join(root, DefaultTasksDirName); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
	})
}
