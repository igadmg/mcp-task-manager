package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"log"
	"os"
	"slices"
	"sync"

	"github.com/gpayer/mcp-task-manager/internal/cli"
	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//go:embed icon.png
var iconPNG []byte

//go:embed instructions.md
var instructionsMD string

func main() {
	// CLI mode if any arguments provided
	if len(os.Args) > 1 {
		cli.Run()
		return
	}

	// MCP server mode
	srv, _ := newServer()
	if err := server.ServeStdio(srv); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// newServer assembles the MCP server.
//
// The project is resolved lazily, on the first tool call: MCP roots are only
// available once a client session exists, and the environment may not identify
// a project at all. Nothing here touches the task files.
func newServer() (*server.MCPServer, *project.Resolver) {
	var srv *server.MCPServer
	resolver := project.NewResolver(func(ctx context.Context) ([]string, error) {
		return project.ServerRoots(srv, project.DefaultRootsTimeout)(ctx)
	})

	srv = server.NewMCPServer(
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
	tools.Register(srv, resolver, defaults.TaskTypes, defaults.RelationTypes)

	resolver.OnResolve(func(resolved *project.Resolved) {
		log.Printf("task-manager: %s", resolved.Resolution().Explain())
		cfg := resolved.Config
		if registered.matches(cfg.TaskTypes, cfg.RelationTypes) {
			return
		}
		registered.set(cfg.TaskTypes, cfg.RelationTypes)
		srv.SetTools(tools.Build(resolver, cfg.TaskTypes, cfg.RelationTypes)...)
		log.Printf("task-manager: tool schemas updated for task types %v", cfg.TaskTypes)
	})

	// The client tells us when its roots change; the next tool call then
	// resolves the project again.
	srv.AddNotificationHandler(mcp.MethodNotificationRootsListChanged,
		func(ctx context.Context, _ mcp.JSONRPCNotification) {
			resolver.Invalidate()
		})

	return srv, resolver
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
