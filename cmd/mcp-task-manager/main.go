package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gpayer/mcp-task-manager/internal/app"
	"github.com/gpayer/mcp-task-manager/internal/cli"
	"github.com/gpayer/mcp-task-manager/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// CLI mode if any arguments provided
	if len(os.Args) > 1 {
		os.Exit(cli.RunWithContext(ctx, os.Args, os.Stdout, os.Stderr))
	}

	// MCP server mode. The config is read here only for the web section; a
	// failure is not fatal, since the project itself is resolved later, from
	// inside the session.
	var web config.WebConfig
	if cfg, err := config.Load(); err == nil {
		web = cfg.Web
	} else {
		log.Printf("task-manager: could not read the config: %v", err)
	}

	if err := app.RunMCP(ctx, app.Options{Web: web, Stderr: os.Stderr}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
