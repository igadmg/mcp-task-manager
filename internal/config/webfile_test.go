package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateWebEnv clears everything WebFilePath consults, so a developer's own
// ~/.config file cannot leak into the test.
func isolateWebEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{EnvWebConfig, EnvXDGConfigHome} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	// HOME is what os.UserHomeDir reads; point it at an empty directory so
	// the ~/.config candidate exists but holds nothing.
	t.Setenv("HOME", t.TempDir())
}

func writeWebFile(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, WebFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func TestWebFilePathPrefersTheExplicitEnvVar(t *testing.T) {
	isolateWebEnv(t)
	dir := t.TempDir()
	path := writeWebFile(t, dir, "version: 1\n")
	t.Setenv(EnvWebConfig, path)

	got, found, err := WebFilePath()
	if err != nil || !found {
		t.Fatalf("WebFilePath() = %q, %v, %v; want the env path", got, found, err)
	}
	if got != path {
		t.Errorf("WebFilePath() = %q, want %q", got, path)
	}
}

// A path the operator named explicitly must not degrade into silence.
func TestWebFilePathErrorsOnAMissingExplicitFile(t *testing.T) {
	isolateWebEnv(t)
	t.Setenv(EnvWebConfig, filepath.Join(t.TempDir(), "nope.yaml"))

	if _, found, err := WebFilePath(); err == nil {
		t.Errorf("WebFilePath() found = %v, err = nil; want an error", found)
	}
}

func TestWebFilePathFallsBackToXDGThenHome(t *testing.T) {
	isolateWebEnv(t)
	xdg := t.TempDir()
	dir := filepath.Join(xdg, "mcp-task-manager")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	want := writeWebFile(t, dir, "version: 1\n")
	t.Setenv(EnvXDGConfigHome, xdg)

	got, found, err := WebFilePath()
	if err != nil || !found || got != want {
		t.Fatalf("WebFilePath() = %q, %v, %v; want %q", got, found, err, want)
	}

	// With XDG_CONFIG_HOME unset, ~/.config is the candidate.
	os.Unsetenv(EnvXDGConfigHome)
	home := t.TempDir()
	t.Setenv("HOME", home)
	hdir := filepath.Join(home, ".config", "mcp-task-manager")
	if err := os.MkdirAll(hdir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	want = writeWebFile(t, hdir, "version: 1\n")

	got, found, err = WebFilePath()
	if err != nil || !found || got != want {
		t.Fatalf("WebFilePath() = %q, %v, %v; want %q", got, found, err, want)
	}
}

// No file anywhere is the normal state for an embedded dashboard, not an
// error.
func TestLoadWebFileWithoutAFileIsNotAnError(t *testing.T) {
	isolateWebEnv(t)

	wf, err := LoadWebFile()
	if err != nil {
		t.Fatalf("LoadWebFile() error = %v", err)
	}
	if len(wf.Workspaces) != 0 || len(wf.Problems) != 0 || wf.Path != "" {
		t.Errorf("LoadWebFile() = %+v, want an empty file", wf)
	}
}

func TestLoadWebFileExpandsHomeAndDefaultsTheName(t *testing.T) {
	isolateWebEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Work", "alpha"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Setenv(EnvWebConfig, writeWebFile(t, t.TempDir(), "version: 1\nworkspaces:\n  - path: ~/Work/alpha\n"))

	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)

	wf, err := LoadWebFile()
	if err != nil {
		t.Fatalf("LoadWebFile() error = %v", err)
	}
	if len(wf.Workspaces) != 1 {
		t.Fatalf("LoadWebFile() loaded %d workspaces, want 1", len(wf.Workspaces))
	}
	w := wf.Workspaces[0]
	if w.Path != filepath.Join(home, "Work", "alpha") {
		t.Errorf("Path = %q, want ~ expanded", w.Path)
	}
	if w.Name != "alpha" {
		t.Errorf("Name = %q, want the path's base name", w.Name)
	}
	if w.Problem != "" {
		t.Errorf("Problem = %q, want none", w.Problem)
	}
	if buf.Len() != 0 {
		t.Errorf("a valid file reported %q", buf.String())
	}
}

// Every finding is reported, and the entry is kept and marked - a mistyped
// path explains itself on the page instead of vanishing, and one bad entry
// cannot hide the others.
func TestLoadWebFileReportsAndKeepsInvalidEntries(t *testing.T) {
	isolateWebEnv(t)
	good := t.TempDir()
	dupPath := t.TempDir()

	body := "version: 2\n" +
		"workspaces:\n" +
		"  - {name: ok, path: " + good + "}\n" +
		"  - {name: nopath}\n" +
		"  - {name: relative, path: relative/dir}\n" +
		"  - {name: missing, path: " + filepath.Join(good, "absent") + "}\n" +
		"  - {name: ok, path: " + dupPath + "}\n" +
		"  - {name: twice, path: " + good + "}\n"
	t.Setenv(EnvWebConfig, writeWebFile(t, t.TempDir(), body))

	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)

	wf, err := LoadWebFile()
	if err != nil {
		t.Fatalf("LoadWebFile() error = %v", err)
	}
	if len(wf.Workspaces) != 6 {
		t.Fatalf("LoadWebFile() kept %d workspaces, want all 6", len(wf.Workspaces))
	}

	wantProblem := []string{"", "no path", "path is not absolute", "path is not a directory",
		"name already used by workspace 0", "same backlog as workspace 0"}
	for i, want := range wantProblem {
		if got := wf.Workspaces[i].Problem; got != want {
			t.Errorf("workspaces[%d].Problem = %q, want %q", i, got, want)
		}
	}

	// The unsupported version is a finding too, not a refusal.
	if len(wf.Problems) != 6 {
		t.Errorf("Problems = %d lines, want 6 (5 entries + the version)", len(wf.Problems))
	}
	if !strings.Contains(wf.Problems[0], "version 2") {
		t.Errorf("Problems[0] = %q, want the version finding first", wf.Problems[0])
	}
	for _, p := range wf.Problems {
		if !strings.Contains(buf.String(), p) {
			t.Errorf("problem %q was not reported to the sink", p)
		}
	}

	if sel := wf.Selectable(); len(sel) != 1 || sel[0].Name != "ok" {
		t.Errorf("Selectable() = %+v, want only the one good entry", sel)
	}
}

func TestLoadWebFileFailsOnAYAMLTypeError(t *testing.T) {
	isolateWebEnv(t)
	t.Setenv(EnvWebConfig, writeWebFile(t, t.TempDir(), "version: abc\n"))

	if _, err := LoadWebFile(); err == nil {
		t.Error("LoadWebFile() error = nil, want a parse error")
	}
}

func TestLoadForRootPicksTheTasksDir(t *testing.T) {
	// MCP_TASKS_DIR must not reach a workspace entry: it describes the
	// process's own project.
	t.Setenv(EnvTasksDir, filepath.Join(t.TempDir(), "elsewhere"))

	t.Run("the project's own tasks_dir", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ConfigFileName), []byte("tasks_dir: backlog\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		cfg, err := LoadForRoot(root, "")
		if err != nil {
			t.Fatalf("LoadForRoot() error = %v", err)
		}
		if want := filepath.Join(root, "backlog"); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
		if cfg.Resolution.Source != SourceWorkspace {
			t.Errorf("Source = %q, want %q", cfg.Resolution.Source, SourceWorkspace)
		}
	})

	t.Run("the override wins", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ConfigFileName), []byte("tasks_dir: backlog\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		cfg, err := LoadForRoot(root, ".tasks")
		if err != nil {
			t.Fatalf("LoadForRoot() error = %v", err)
		}
		if want := filepath.Join(root, ".tasks"); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
	})

	t.Run("a legacy tasks directory is picked over the default", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, LegacyTasksDirName, "1"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, LegacyTasksDirName, "1", "1.md"), []byte("---\nid: 1\n---\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		cfg, err := LoadForRoot(root, "")
		if err != nil {
			t.Fatalf("LoadForRoot() error = %v", err)
		}
		if want := filepath.Join(root, LegacyTasksDirName); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
		if !cfg.ProjectFound {
			t.Error("ProjectFound = false for a directory that holds tasks")
		}
	})

	t.Run("nothing there yet", func(t *testing.T) {
		root := t.TempDir()
		cfg, err := LoadForRoot(root, "")
		if err != nil {
			t.Fatalf("LoadForRoot() error = %v", err)
		}
		if want := filepath.Join(root, DefaultTasksDirName); cfg.DataDir != want {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
		}
		if cfg.ProjectFound {
			t.Error("ProjectFound = true for a root with no tasks directory")
		}
	})
}
