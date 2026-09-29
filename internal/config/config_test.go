package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	isolateEnv(t)
	cfg := DefaultConfig()

	if cfg.DataDir != "./tasks" {
		t.Errorf("DefaultConfig().DataDir = %q, want %q", cfg.DataDir, "./tasks")
	}

	if len(cfg.TaskTypes) != 2 {
		t.Errorf("DefaultConfig().TaskTypes length = %d, want 2", len(cfg.TaskTypes))
	}

	expectedTypes := map[string]bool{"feature": true, "bug": true}
	for _, tt := range cfg.TaskTypes {
		if !expectedTypes[tt] {
			t.Errorf("unexpected task type: %q", tt)
		}
	}
}

func TestIsValidTaskType(t *testing.T) {
	isolateEnv(t)
	cfg := &Config{
		TaskTypes: []string{"feature", "bug", "chore"},
	}

	tests := []struct {
		taskType string
		valid    bool
	}{
		{"feature", true},
		{"bug", true},
		{"chore", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.taskType, func(t *testing.T) {
			if got := cfg.IsValidTaskType(tt.taskType); got != tt.valid {
				t.Errorf("IsValidTaskType(%q) = %v, want %v", tt.taskType, got, tt.valid)
			}
		})
	}
}

func TestTasksDir_Absolute(t *testing.T) {
	isolateEnv(t)
	cfg := &Config{DataDir: "/absolute/path"}
	if got := cfg.TasksDir(); got != "/absolute/path" {
		t.Errorf("TasksDir() = %q, want %q", got, "/absolute/path")
	}
}

func TestTasksDir_Relative(t *testing.T) {
	isolateEnv(t)
	cfg := &Config{DataDir: "./tasks"}
	cwd, _ := os.Getwd()
	expected := filepath.Join(cwd, "./tasks")

	if got := cfg.TasksDir(); got != expected {
		t.Errorf("TasksDir() = %q, want %q", got, expected)
	}
}

func TestLoad_WithEnvOverride(t *testing.T) {
	isolateEnv(t)
	// Save and restore env
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)

	os.Setenv("MCP_TASKS_DIR", "/custom/path")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DataDir != "/custom/path" {
		t.Errorf("Load() DataDir = %q, want %q", cfg.DataDir, "/custom/path")
	}
}

func TestFindProjectRoot_ConfigFile(t *testing.T) {
	isolateEnv(t)
	// Create temp directory structure with mcp-tasks.yaml
	tmpDir := tempDir(t)
	subDir := filepath.Join(tmpDir, "sub", "deep")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirs: %v", err)
	}

	// Create config file in tmpDir
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	if err := os.WriteFile(configPath, []byte("task_types:\n  - feature\n"), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	// Change to deep subdirectory
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	root, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}

	if root != tmpDir {
		t.Errorf("FindProjectRoot() = %q, want %q", root, tmpDir)
	}
}

func TestFindProjectRoot_TasksDirectory(t *testing.T) {
	isolateEnv(t)
	// Create temp directory structure with tasks/ directory (no config file)
	tmpDir := tempDir(t)
	subDir := filepath.Join(tmpDir, "sub", "deep")
	tasksDir := filepath.Join(tmpDir, "tasks")

	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirs: %v", err)
	}
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf("failed to create tasks dir: %v", err)
	}

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	// Change to deep subdirectory
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	root, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}

	if root != tmpDir {
		t.Errorf("FindProjectRoot() = %q, want %q", root, tmpDir)
	}
}

func TestFindProjectRoot_ConfigFilePreferredOverTasksDir(t *testing.T) {
	isolateEnv(t)
	// Create two levels: one with tasks/, parent with mcp-tasks.yaml
	// Should find the one with config file first (it's higher priority at same level)
	tmpDir := tempDir(t)
	subDir := filepath.Join(tmpDir, "sub")

	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirs: %v", err)
	}

	// Create both: config file and tasks dir at same level (config should win)
	configPath := filepath.Join(subDir, "mcp-tasks.yaml")
	tasksDir := filepath.Join(subDir, "tasks")

	if err := os.WriteFile(configPath, []byte("task_types:\n  - feature\n"), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf("failed to create tasks dir: %v", err)
	}

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	root, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}

	// Should find subDir (where both exist) - config file is checked first
	if root != subDir {
		t.Errorf("FindProjectRoot() = %q, want %q", root, subDir)
	}
}

func TestFindProjectRoot_NotFound(t *testing.T) {
	isolateEnv(t)
	// Create temp directory with nothing
	tmpDir := tempDir(t)

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	root, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}

	if root != "" {
		t.Errorf("FindProjectRoot() = %q, want empty string", root)
	}
}

func TestFindProjectRoot_InProjectRoot(t *testing.T) {
	isolateEnv(t)
	// When already in project root, should return that directory
	tmpDir := tempDir(t)

	// Create config file in tmpDir
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	if err := os.WriteFile(configPath, []byte("task_types:\n  - feature\n"), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	root, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() error = %v", err)
	}

	if root != tmpDir {
		t.Errorf("FindProjectRoot() = %q, want %q", root, tmpDir)
	}
}

func TestLoad_WithEnvOverride_SetsProjectFound(t *testing.T) {
	isolateEnv(t)
	// Save and restore env
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)

	// ProjectFound now means "the tasks directory exists", so the override
	// has to point at a real directory.
	dir := tempDir(t)
	os.Setenv("MCP_TASKS_DIR", dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.ProjectFound {
		t.Error("Load() ProjectFound = false, want true when env var points at an existing directory")
	}
}

func TestLoad_FindsProjectRoot(t *testing.T) {
	isolateEnv(t)
	// Create temp directory structure with mcp-tasks.yaml in parent
	tmpDir := tempDir(t)
	subDir := filepath.Join(tmpDir, "sub", "deep")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirs: %v", err)
	}

	// Create config file in tmpDir (parent)
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	if err := os.WriteFile(configPath, []byte("task_types:\n  - feature\n  - chore\n"), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Clear env var to ensure we use FindProjectRoot
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)
	os.Unsetenv("MCP_TASKS_DIR")

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	// Change to deep subdirectory
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should find project root and set DataDir. A project root with no
	// tasks directory yet defaults to .tasks.
	expectedDataDir := filepath.Join(tmpDir, DefaultTasksDirName)
	if cfg.DataDir != expectedDataDir {
		t.Errorf("Load() DataDir = %q, want %q", cfg.DataDir, expectedDataDir)
	}

	// The tasks directory itself does not exist yet.
	if cfg.ProjectFound {
		t.Error("Load() ProjectFound = true, want false when the tasks directory is missing")
	}

	// Should also load config from project root
	if !cfg.IsValidTaskType("chore") {
		t.Error("Load() should have loaded config from project root, but 'chore' not in TaskTypes")
	}
}

func TestLoad_NoProjectFound(t *testing.T) {
	isolateEnv(t)
	// Create temp directory with nothing
	tmpDir := tempDir(t)

	// Clear env var
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)
	os.Unsetenv("MCP_TASKS_DIR")

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Falls back to the working directory, and to the .tasks default name.
	if want := filepath.Join(tmpDir, DefaultTasksDirName); cfg.DataDir != want {
		t.Errorf("Load() DataDir = %q, want %q", cfg.DataDir, want)
	}
	if cfg.Resolution.Source != SourceFallback {
		t.Errorf("Load() Source = %q, want %q", cfg.Resolution.Source, SourceFallback)
	}

	if cfg.ProjectFound {
		t.Error("Load() ProjectFound = true, want false when no project found")
	}
}

func TestLoad_LoadsConfigFromProjectRoot(t *testing.T) {
	isolateEnv(t)
	// Create temp directory structure
	tmpDir := tempDir(t)
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirs: %v", err)
	}

	// Create config file in tmpDir with custom task types
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	configContent := "task_types:\n  - feature\n  - bug\n  - docs\n  - refactor\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Clear env var
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)
	os.Unsetenv("MCP_TASKS_DIR")

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should load custom task types from config file in project root
	expectedTypes := []string{"feature", "bug", "docs", "refactor"}
	if len(cfg.TaskTypes) != len(expectedTypes) {
		t.Errorf("Load() TaskTypes length = %d, want %d", len(cfg.TaskTypes), len(expectedTypes))
	}

	for _, tt := range expectedTypes {
		if !cfg.IsValidTaskType(tt) {
			t.Errorf("Load() should have task type %q, but it's not valid", tt)
		}
	}
}

func TestDefaultConfig_AutoArchive(t *testing.T) {
	isolateEnv(t)
	cfg := DefaultConfig()

	if cfg.AutoArchive.Enabled != false {
		t.Errorf("DefaultConfig().AutoArchive.Enabled = %v, want false", cfg.AutoArchive.Enabled)
	}

	if cfg.AutoArchive.AfterDays != 30 {
		t.Errorf("DefaultConfig().AutoArchive.AfterDays = %d, want 30", cfg.AutoArchive.AfterDays)
	}
}

func TestLoad_AutoArchiveFromYAML(t *testing.T) {
	isolateEnv(t)
	// Create temp directory with config file containing auto_archive section
	tmpDir := tempDir(t)

	configContent := `task_types:
  - feature
  - bug
auto_archive:
  enabled: true
  after_days: 60
`
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Clear env var
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)
	os.Unsetenv("MCP_TASKS_DIR")

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.AutoArchive.Enabled {
		t.Errorf("Load() AutoArchive.Enabled = false, want true")
	}

	if cfg.AutoArchive.AfterDays != 60 {
		t.Errorf("Load() AutoArchive.AfterDays = %d, want 60", cfg.AutoArchive.AfterDays)
	}
}

func TestLoad_AutoArchiveDefaults_WhenNotInYAML(t *testing.T) {
	isolateEnv(t)
	// Create temp directory with config file that does NOT contain auto_archive
	tmpDir := tempDir(t)

	configContent := "task_types:\n  - feature\n  - bug\n"
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Clear env var
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)
	os.Unsetenv("MCP_TASKS_DIR")

	// Save and restore cwd
	oldCwd, _ := os.Getwd()
	defer os.Chdir(oldCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AutoArchive.Enabled != false {
		t.Errorf("Load() AutoArchive.Enabled = %v, want false (default)", cfg.AutoArchive.Enabled)
	}

	if cfg.AutoArchive.AfterDays != 30 {
		t.Errorf("Load() AutoArchive.AfterDays = %d, want 30 (default)", cfg.AutoArchive.AfterDays)
	}
}

func TestLoad_EnvVarLoadsConfigFromParentDir(t *testing.T) {
	isolateEnv(t)
	// Create temp directory structure
	tmpDir := tempDir(t)
	tasksDir := filepath.Join(tmpDir, "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf("failed to create tasks dir: %v", err)
	}

	// Create config file in tmpDir (parent of tasks)
	configPath := filepath.Join(tmpDir, "mcp-tasks.yaml")
	configContent := "task_types:\n  - feature\n  - custom\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Set env var to tasks directory
	oldVal := os.Getenv("MCP_TASKS_DIR")
	defer os.Setenv("MCP_TASKS_DIR", oldVal)
	os.Setenv("MCP_TASKS_DIR", tasksDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should load config from parent of env var path
	if !cfg.IsValidTaskType("custom") {
		t.Error("Load() should have loaded config from parent of MCP_TASKS_DIR")
	}
}

// isolateEnv clears every variable that takes part in project resolution, so
// a test never picks up the root of the project it is being run from.
func isolateEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{EnvTasksDir, EnvProjectDir, EnvClaudeProjectDir, EnvRootSource, EnvWebEnabled, EnvWebAddr, EnvGitBranching} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

// tempDir returns a temp directory with symlinks resolved, so it compares
// equal to what os.Getwd reports after chdir (on macOS /var is a symlink).
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	return dir
}
