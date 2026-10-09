// Command mcp-task-manager-web serves the read-only kanban dashboard.
//
// It is the second of this repository's two binaries. The first,
// mcp-task-manager, owns writes to the backlog and spawns this one on demand
// (start_web_ui, or web.enabled); this one only ever reads, so the dashboard
// outlives the agent's MCP server and a restart of the agent neither kills it
// nor starts a second one.
//
// Which backlog it serves is resolved the usual way, so MCP_TASKS_DIR -
// which is what the spawner passes - wins outright.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gpayer/mcp-task-manager/internal/webapp"
)

func main() {
	addr := flag.String("addr", "", "Listen address (default from mcp-tasks.yaml, e.g. 127.0.0.1:7777)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := webapp.Run(ctx, webapp.Options{Addr: *addr, Stderr: os.Stderr}); err != nil {
		log.Fatalf("dashboard error: %v", err)
	}
}
