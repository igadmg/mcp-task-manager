package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

func isolateEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{config.EnvTasksDir, config.EnvProjectDir, config.EnvClaudeProjectDir, config.EnvRootSource} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func projectDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, config.DefaultTasksDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	return dir
}

func TestResolver_CachesUntilInvalidated(t *testing.T) {
	isolateEnv(t)
	root := projectDir(t)
	// Force the roots step so every resolution is observable.
	t.Setenv(config.EnvRootSource, string(config.SourceRoots))

	var calls atomic.Int32
	r := NewResolver(func(context.Context) ([]string, error) {
		calls.Add(1)
		return []string{root}, nil
	})

	first, err := r.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	second, err := r.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if first != second {
		t.Error("Get() returned a different project on the second call, want the cached one")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("roots/list calls = %d, want 1", got)
	}
	if first.Resolution().Root != root {
		t.Errorf("Root = %q, want %q", first.Resolution().Root, root)
	}

	r.Invalidate()
	if _, err := r.Get(context.Background()); err != nil {
		t.Fatalf("Get() after Invalidate() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("roots/list calls after Invalidate = %d, want 2", got)
	}
}

func TestResolver_FailureIsNotCached(t *testing.T) {
	isolateEnv(t)
	root := projectDir(t)
	t.Setenv(config.EnvProjectDir, root)

	configPath := filepath.Join(root, config.ConfigFileName)
	if err := os.WriteFile(configPath, []byte("task_types: [unterminated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	r := NewResolver(nil)
	if _, err := r.Get(context.Background()); err == nil {
		t.Fatal("Get() error = nil, want a parse error for the broken config")
	}

	// Fixing the config must take effect without recreating the resolver.
	if err := os.WriteFile(configPath, []byte("task_types:\n  - feature\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	resolved, err := r.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !resolved.Config.IsValidTaskType("feature") {
		t.Error("config was not reloaded after the failure")
	}
}

func TestResolver_OnResolveFiresPerResolution(t *testing.T) {
	isolateEnv(t)
	t.Setenv(config.EnvProjectDir, projectDir(t))

	var fired atomic.Int32
	r := NewResolver(nil)
	r.OnResolve(func(*Resolved) { fired.Add(1) })

	for range 2 {
		if _, err := r.Get(context.Background()); err != nil {
			t.Fatalf("Get() error = %v", err)
		}
	}
	if got := fired.Load(); got != 1 {
		t.Errorf("OnResolve calls = %d, want 1 (cached second Get)", got)
	}

	r.Invalidate()
	if _, err := r.Get(context.Background()); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := fired.Load(); got != 2 {
		t.Errorf("OnResolve calls after Invalidate = %d, want 2", got)
	}
}

func TestResolver_RootsErrorIsNotFatal(t *testing.T) {
	isolateEnv(t)
	root := projectDir(t)
	t.Chdir(root)

	r := NewResolver(func(context.Context) ([]string, error) {
		return nil, errors.New("client does not support roots")
	})

	resolved, err := r.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resolved.Resolution().Source != config.SourceCwd {
		t.Errorf("Source = %q, want %q", resolved.Resolution().Source, config.SourceCwd)
	}
}

func TestPathFromURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"file:///Users/me/project", "/Users/me/project"},
		{"file://localhost/Users/me/project", "/Users/me/project"},
		{"file:///Users/me/project/", "/Users/me/project"},
		{"/Users/me/project", "/Users/me/project"},
		{"file://remotehost/Users/me/project", ""},
		{"https://example.com/repo", ""},
		{"relative/path", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := PathFromURI(tc.uri); got != tc.want {
			t.Errorf("PathFromURI(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}
