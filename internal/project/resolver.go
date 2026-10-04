// Package project resolves which project the server is working on, and builds
// the task service for it.
//
// Resolution is lazy: MCP roots are only available after initialize, inside a
// client session, so the tasks directory cannot be known when the server is
// constructed. The first tool call resolves it and the result is cached until
// the client reports that its roots changed.
package project

import (
	"context"
	"sync"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/vcs"
)

// *vcs.Repo is the production task.GitRepo.
var _ task.GitRepo = (*vcs.Repo)(nil)

// RootsFunc asks the client for its roots as filesystem paths. It returns an
// error when the client does not support roots, which is not fatal: the
// resolver simply falls through to the next source.
type RootsFunc func(ctx context.Context) ([]string, error)

// Resolved is a project the server has settled on.
type Resolved struct {
	Config  *config.Config
	Service *task.Service
}

// Resolution describes how this project was found.
func (r *Resolved) Resolution() *config.Resolution {
	return r.Config.Resolution
}

// Resolver resolves a project once and caches it.
type Resolver struct {
	roots RootsFunc

	mu       sync.Mutex
	resolved *Resolved
	onChange []func(*Resolved)
}

// NewResolver returns a resolver. A nil roots func means the roots step is
// skipped, which is the CLI case.
func NewResolver(roots RootsFunc) *Resolver {
	return &Resolver{roots: roots}
}

// OnResolve registers a callback fired after each successful resolution,
// including re-resolution after Invalidate. Callbacks run outside the lock.
func (r *Resolver) OnResolve(fn func(*Resolved)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onChange = append(r.onChange, fn)
}

// Invalidate drops the cached project, so the next call resolves again.
// Called when the client sends notifications/roots/list_changed.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved = nil
}

// Get resolves the project, or returns the cached one. Failed resolutions are
// not cached, so a fixed configuration takes effect without a restart.
func (r *Resolver) Get(ctx context.Context) (*Resolved, error) {
	r.mu.Lock()
	if r.resolved != nil {
		resolved := r.resolved
		r.mu.Unlock()
		return resolved, nil
	}

	resolved, err := r.resolve(ctx)
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	r.resolved = resolved
	callbacks := make([]func(*Resolved), len(r.onChange))
	copy(callbacks, r.onChange)
	r.mu.Unlock()

	for _, fn := range callbacks {
		fn(resolved)
	}
	return resolved, nil
}

// Service is the common shorthand for tool handlers.
func (r *Resolver) Service(ctx context.Context) (*task.Service, error) {
	resolved, err := r.Get(ctx)
	if err != nil {
		return nil, err
	}
	return resolved.Service, nil
}

// Current returns the cached resolution without resolving anything.
//
// It is how consumers that must not cause side effects read the project - the
// HTTP handlers above all. Resolution runs Service.Initialize(), which
// migrates the legacy layout and may auto-archive, so it must only ever be
// triggered by an MCP tool call or an explicit CLI startup, never by a plain
// GET. Do not "helpfully" resolve here when nothing is cached yet.
func (r *Resolver) Current() (*Resolved, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resolved, r.resolved != nil
}

func (r *Resolver) resolve(ctx context.Context) (*Resolved, error) {
	var provider config.RootsProvider
	if r.roots != nil {
		provider = func() ([]string, error) { return r.roots(ctx) }
	}

	cfg, err := config.Resolve(provider)
	if err != nil {
		return nil, err
	}
	return Build(cfg)
}

// Build constructs the storage, index and task service for an already loaded
// config, running Initialize. It is the single construction site for a
// project: Resolver.resolve and the CLI both go through it.
//
// The service always gets the per-user current-task store and the user's
// identity (one `git config` call, falling back to the OS user). Git itself
// is injected only when cfg.Git.Branching is set, and nothing here runs a
// git command against the repository: a project that is not a repository
// fails on its first start_task, not here. extra options apply last, so
// tests can override any of these.
func Build(cfg *config.Config, extra ...task.ServiceOption) (*Resolved, error) {
	tasksDir := cfg.TasksDir()
	mdStorage := storage.NewMarkdownStorage(tasksDir)
	index := storage.NewIndex(tasksDir, mdStorage)

	root := tasksDir
	if cfg.Resolution != nil && cfg.Resolution.Root != "" {
		root = cfg.Resolution.Root
	}
	id := vcs.ResolveIdentity(root)
	opts := []task.ServiceOption{
		task.WithCurrentTaskStore(mdStorage),
		task.WithPhaseStore(mdStorage),
		task.WithIdentity(task.Identity{Name: id.Name, FromGitEmail: id.FromGitEmail}),
	}
	if cfg.Git.Branching {
		opts = append(opts, task.WithGit(vcs.New(root, tasksDir)))
	}
	opts = append(opts, extra...)

	svc := task.NewService(mdStorage, mdStorage, mdStorage, index, cfg.TaskTypes, cfg, opts...)
	if err := svc.Initialize(); err != nil {
		return nil, err
	}

	return &Resolved{Config: cfg, Service: svc}, nil
}

// NewStatic returns a resolver pinned to an already built project, skipping
// resolution entirely. Used by tests and by callers that resolved the project
// themselves.
func NewStatic(resolved *Resolved) *Resolver {
	return &Resolver{resolved: resolved}
}
