// Package app is the composition root: it wires the MCP transport and the web
// dashboard onto one resolved project and one *task.Service, inside one
// process. Nothing else in the tree knows about both transports.
package app

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/tools"
	"github.com/gpayer/mcp-task-manager/internal/web"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//go:embed icon.png
var iconPNG []byte

//go:embed instructions.md
var instructionsMD string

// The only place allowed to know both packages, so the interface and its one
// real implementation are checked against each other exactly once.
var _ tools.WebStarter = (*web.Controller)(nil)

// shutdownGrace is how long the HTTP listener gets to drain once the process
// is stopping.
const shutdownGrace = 5 * time.Second

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

	// The controller has to exist before the tool set is built - a tool set
	// built around a nil controller would still register start_web_ui, and
	// calling it would dereference nothing.
	var srv *server.MCPServer
	resolver := newLazyResolver(&srv)
	controller := web.NewController(web.Deps{Project: resolver.Current, Logger: logger}, opts.Web.Addr)
	srv = newServerFor(resolver, controller)

	if opts.Web.Enabled {
		// The dashboard is an extra, not a dependency: if the port is taken,
		// say so and keep serving MCP.
		if url, _, err := controller.Start(""); err != nil {
			logger.Printf("could not start the dashboard: %v", err)
		} else {
			logger.Printf("dashboard: %s", url)
		}
	}
	defer shutdown(controller)

	// Replacing server.ServeStdio: its own signal handling would compete with
	// the caller's context, and its context is unreachable from here.
	stdio := server.NewStdioServer(srv)
	stdio.SetErrorLogger(log.New(opts.stderr(), "", log.LstdFlags))
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

// RunWeb serves the dashboard in the foreground, and MCP alongside it when
// the config asks for that.
//
// Unlike RunMCP it resolves the project eagerly: an unresolvable project or an
// occupied port is a startup error the operator sees immediately, not a
// surprise on the first request.
func RunWeb(ctx context.Context, opts Options) error {
	logger := opts.logger()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	resolved, err := project.Build(cfg)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	resolver := project.NewStatic(resolved)

	controller := web.NewController(web.Deps{Project: resolver.Current, Logger: logger}, opts.Web.Addr)
	url, _, err := controller.Start(opts.Web.Addr)
	if err != nil {
		return fmt.Errorf("start dashboard: %w", err)
	}
	logger.Printf("%s", resolved.Resolution().Explain())
	logger.Printf("dashboard: %s", url)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	if opts.Web.WithMCP {
		stdio := server.NewStdioServer(newServerFor(resolver, controller))
		stdio.SetErrorLogger(log.New(opts.stderr(), "", log.LstdFlags))
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel() // stdin closing ends the run, like any other transport
			errs <- stdio.Listen(ctx, os.Stdin, os.Stdout)
		}()
	}

	<-ctx.Done()
	shutdown(controller)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, io.EOF) {
			return err
		}
	}
	return nil
}

func (o Options) stderr() io.Writer {
	if o.Stderr == nil {
		return os.Stderr
	}
	return o.Stderr
}

func shutdown(c *web.Controller) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	_ = c.Shutdown(ctx)
}

// newLazyResolver returns a resolver whose roots step asks the client through
// the server that will be stored in *srv. The indirection is what lets the web
// controller be built from resolver.Current before the server exists.
func newLazyResolver(srv **server.MCPServer) *project.Resolver {
	return project.NewResolver(func(ctx context.Context) ([]string, error) {
		return project.ServerRoots(*srv, project.DefaultRootsTimeout)(ctx)
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
