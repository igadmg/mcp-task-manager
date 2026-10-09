// Package app is the MCP server's composition root.
//
// It is one of this repository's two applications; internal/webapp is the
// other. The split is structural, not conventional: this package - and so
// cmd/mcp-task-manager - has no path to internal/web at all, and
// cmd/import_test.go fails if one appears. Its only view of the dashboard is
// internal/webproc, which runs the other binary, probes it over HTTP and
// reads the marker it leaves on disk.
//
// This process owns writes to the backlog. The dashboard only reads, in a
// process of its own, which is why it outlives the agent.
package app

import (
	"context"
	_ "embed"
	"encoding/base64"
	"io"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/tools"
	"github.com/gpayer/mcp-task-manager/internal/webproc"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//go:embed icon.png
var iconPNG []byte

//go:embed instructions.md
var instructionsMD string

// The interface and its one real implementation, checked against each other
// exactly once. Since the split that implementation runs another program
// rather than binding a listener here.
var _ tools.WebStarter = (*webproc.Spawner)(nil)

// Options are the effective settings a run was started with.
type Options struct {
	// Web is the web section after config, flags and env were merged.
	Web config.WebConfig
	// Stderr is the log sink. Never Stdout: in MCP stdio mode stdout is the
	// JSON-RPC channel and a stray line there corrupts the session.
	Stderr io.Writer
}

func (o Options) logger() *log.Logger {
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	return log.New(o.Stderr, "web: ", log.LstdFlags)
}

// RunMCP serves MCP over stdio, optionally bringing the dashboard up with it.
//
// The project is resolved lazily, on the first tool call: MCP roots only exist
// once a client session does. Until then the board serves its placeholder.
func RunMCP(ctx context.Context, opts Options) error {
	logger := opts.logger()

	// The spawner has to exist before the tool set is built - a tool set
	// built around a nil one would still register start_web_ui, and calling
	// it would dereference nothing. It is cheap and stateless: until a
	// project is resolved it has no backlog to serve and says so.
	var srv *server.MCPServer
	resolver := newLazyResolver(&srv)
	spawner := &webproc.Spawner{Addr: opts.Web.Addr, Logger: logger}
	srv = newServerFor(resolver, spawner)

	// Resolving the project is what tells the spawner which backlog to
	// serve. Nothing is published into a registry any more: the dashboard
	// owns its own, in its own process.
	resolver.OnResolve(func(resolved *project.Resolved) {
		spawner.TasksDir = resolved.Config.TasksDir()
		spawner.User = resolved.Service.UserName()
		if url, running := spawner.URL(); running {
			path, _ := spawner.SessionPath(spawner.TasksDir)
			logger.Printf("dashboard: %s%s/", url, path)
		}
		if opts.Web.Enabled {
			startDashboard(logger, spawner)
		}
	})

	// Nothing is deferred: the dashboard is a separate process and outlives
	// this one on purpose. Restarting the agent must neither kill the board
	// nor start a second one.

	// Replacing server.ServeStdio: its own signal handling would compete with
	// the caller's context, and its context is unreachable from here.
	stdio := server.NewStdioServer(srv)
	stdio.SetErrorLogger(log.New(opts.stderr(), "", log.LstdFlags))
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

// startDashboard brings the dashboard up for a resolved project. The
// dashboard is an extra, not a dependency: anything that goes wrong is logged
// and MCP keeps serving tools.
//
// It runs on resolution rather than at startup because the child has to be
// told which backlog to serve, and before resolution there is none - the MCP
// roots step needs a client session.
func startDashboard(logger *log.Logger, spawner *webproc.Spawner) {
	url, already, err := spawner.Start("")
	if err != nil {
		logger.Printf("could not start the dashboard: %v", err)
		return
	}
	path, _ := spawner.SessionPath(spawner.TasksDir)
	if already {
		logger.Printf("dashboard already running: %s%s/", url, path)
		return
	}
	logger.Printf("dashboard: %s%s/", url, path)
}

func (o Options) stderr() io.Writer {
	if o.Stderr == nil {
		return os.Stderr
	}
	return o.Stderr
}

// The roots/list round trip lives here rather than in internal/project: it
// is the MCP transport asking a client a question, and keeping it out of
// internal/project is what stops mcp-go being linked into the dashboard
// binary, which imports that package for BuildReadOnly.
// defaultRootsTimeout bounds the roots/list round trip. Without it a client
// that never answers would hang the first tool call indefinitely.
const defaultRootsTimeout = 5 * time.Second

// serverRoots returns a project.RootsFunc backed by the protocol's roots/list request.
// Clients that did not declare the roots capability make this fail, which the
// resolver treats as "try the next source".
func serverRoots(s *server.MCPServer, timeout time.Duration) project.RootsFunc {
	if timeout <= 0 {
		timeout = defaultRootsTimeout
	}
	return func(ctx context.Context) ([]string, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		result, err := s.RequestRoots(ctx, mcp.ListRootsRequest{})
		if err != nil {
			return nil, err
		}

		var paths []string
		for _, root := range result.Roots {
			if p := project.PathFromURI(root.URI); p != "" {
				paths = append(paths, p)
			}
		}
		return paths, nil
	}
}

// newLazyResolver returns a resolver whose roots step asks the client through
// the server that will be stored in *srv. The indirection is what lets the web
// controller be built from resolver.Current before the server exists.
func newLazyResolver(srv **server.MCPServer) *project.Resolver {
	return project.NewResolver(func(ctx context.Context) ([]string, error) {
		return serverRoots(*srv, defaultRootsTimeout)(ctx)
	})
}

// newServer assembles the MCP server over a lazily resolving project.
//
// The project is resolved on the first tool call: MCP roots are only available
// once a client session exists, and the environment may not identify a project
// at all. Nothing here touches the task files.
func newServer(web tools.WebStarter) (*server.MCPServer, *project.Resolver) {
	var srv *server.MCPServer
	resolver := newLazyResolver(&srv)
	srv = newServerFor(resolver, web)
	return srv, resolver
}

// newServerFor builds the MCP server around an existing resolver, so the web
// entry point can share the project it already resolved.
func newServerFor(resolver *project.Resolver, web tools.WebStarter) *server.MCPServer {
	srv := server.NewMCPServer(
		"mcp-task-manager",
		"0.1.0",
		// listChanged is required: the tool schemas carry the configured
		// task types, which are unknown until the project is resolved.
		server.WithToolCapabilities(true),
		server.WithInstructions(instructionsMD),
		server.WithIcons(mcp.Icon{
			Src:      "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconPNG),
			MIMEType: "image/png",
			Sizes:    []string{"128x128"},
		}),
	)

	// Register on defaults, then re-register if the resolved project turns
	// out to configure different types.
	defaults := config.DefaultConfig()
	registered := newToolSet(defaults.TaskTypes, defaults.RelationTypes)
	tools.Register(srv, resolver, defaults.TaskTypes, defaults.RelationTypes, web)

	resolver.OnResolve(func(resolved *project.Resolved) {
		log.Printf("task-manager: %s", resolved.Resolution().Explain())
		cfg := resolved.Config
		if registered.matches(cfg.TaskTypes, cfg.RelationTypes) {
			return
		}
		registered.set(cfg.TaskTypes, cfg.RelationTypes)
		// Pass web again: dropping it here would silently remove
		// start_web_ui from the republished tool list.
		srv.SetTools(tools.Build(resolver, cfg.TaskTypes, cfg.RelationTypes, web)...)
		log.Printf("task-manager: tool schemas updated for task types %v", cfg.TaskTypes)
	})

	// The client tells us when its roots change; the next tool call then
	// resolves the project again.
	srv.AddNotificationHandler(mcp.MethodNotificationRootsListChanged,
		func(ctx context.Context, _ mcp.JSONRPCNotification) {
			resolver.Invalidate()
		})

	return srv
}

// toolSet remembers which type lists the currently registered tool schemas
// were built from.
type toolSet struct {
	mu            sync.Mutex
	taskTypes     []string
	relationTypes []string
}

func newToolSet(taskTypes, relationTypes []string) *toolSet {
	return &toolSet{taskTypes: slices.Clone(taskTypes), relationTypes: slices.Clone(relationTypes)}
}

func (t *toolSet) matches(taskTypes, relationTypes []string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Equal(t.taskTypes, taskTypes) && slices.Equal(t.relationTypes, relationTypes)
}

func (t *toolSet) set(taskTypes, relationTypes []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.taskTypes = slices.Clone(taskTypes)
	t.relationTypes = slices.Clone(relationTypes)
}
