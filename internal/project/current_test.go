package project

import (
	"context"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

func TestCurrentReturnsFalseBeforeResolve(t *testing.T) {
	isolateEnv(t)
	r := NewResolver(nil)

	if resolved, ok := r.Current(); ok || resolved != nil {
		t.Errorf("Current() = %v, %v before any resolution; want nil, false", resolved, ok)
	}
}

func TestCurrentReturnsResolvedAfterGet(t *testing.T) {
	isolateEnv(t)
	root := projectDir(t)
	t.Setenv(config.EnvProjectDir, root)

	r := NewResolver(nil)
	want, err := r.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	got, ok := r.Current()
	if !ok {
		t.Fatal("Current() reported nothing resolved after a successful Get()")
	}
	if got != want {
		t.Error("Current() returned a different project than Get()")
	}
}

func TestCurrentFalseAfterInvalidate(t *testing.T) {
	isolateEnv(t)
	root := projectDir(t)
	t.Setenv(config.EnvProjectDir, root)

	r := NewResolver(nil)
	if _, err := r.Get(context.Background()); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	r.Invalidate()

	if _, ok := r.Current(); ok {
		t.Error("Current() still reports a project after Invalidate()")
	}
}

// TestCurrentNeverResolves is the structural guard behind the read-only web
// handlers: resolution runs Initialize(), which migrates the layout and may
// auto-archive, so a plain GET must not be able to trigger it.
func TestCurrentNeverResolves(t *testing.T) {
	isolateEnv(t)
	t.Setenv(config.EnvRootSource, string(config.SourceRoots))

	r := NewResolver(func(context.Context) ([]string, error) {
		t.Fatal("Current() resolved the project; it must never do I/O")
		return nil, nil
	})

	for i := 0; i < 3; i++ {
		if _, ok := r.Current(); ok {
			t.Fatal("Current() = true without a resolution")
		}
	}
}

func TestBuildIsEquivalentToResolve(t *testing.T) {
	isolateEnv(t)
	root := projectDir(t)
	t.Setenv(config.EnvProjectDir, root)

	viaResolver, err := NewResolver(nil).Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	viaBuild, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if viaBuild.Config.TasksDir() != viaResolver.Config.TasksDir() {
		t.Errorf("Build() tasks dir = %q, Resolver = %q", viaBuild.Config.TasksDir(), viaResolver.Config.TasksDir())
	}
	if viaBuild.Service == nil {
		t.Error("Build() returned a nil service")
	}
}
