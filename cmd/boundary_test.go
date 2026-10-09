// Package cmd_test holds the one invariant that keeps the MCP/web split
// structural rather than conventional.
package cmd_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestMCPBinaryDoesNotLinkTheDashboard is what stops this split from rotting
// back into one process.
//
// "Two binaries" can be satisfied while both still link the web server, and
// then the next convenience - "let serve web just run it in-process" - puts
// the listener back inside the MCP binary and nothing fails. So the boundary
// is stated as an import rule and checked here: cmd/mcp-task-manager reaches
// the dashboard only through os/exec, an HTTP probe and a file on disk
// (internal/webproc), and never through internal/web.
func TestMCPBinaryDoesNotLinkTheDashboard(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain to walk the import graph with")
	}

	out, err := exec.Command(goBin, "list", "-deps", "./mcp-task-manager").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}

	const forbidden = "github.com/gpayer/mcp-task-manager/internal/web"
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch strings.TrimSpace(line) {
		case forbidden:
			t.Errorf("cmd/mcp-task-manager imports %s; the dashboard must stay in its own process "+
				"(reach it through internal/webproc instead)", forbidden)
		case forbidden + "app", forbidden + "proc":
			// internal/webapp and internal/webproc: the first must not
			// be here either, the second is exactly how this binary is
			// allowed to know about the dashboard.
			if strings.TrimSpace(line) == forbidden+"app" {
				t.Errorf("cmd/mcp-task-manager imports %sapp, the dashboard's composition root", forbidden)
			}
		}
	}
}

// TestDashboardBinaryDoesNotLinkMCP is the other half: the dashboard serves
// HTTP and nothing else, so it has no business carrying the MCP server or the
// tool set.
func TestDashboardBinaryDoesNotLinkMCP(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain to walk the import graph with")
	}

	out, err := exec.Command(goBin, "list", "-deps", "./mcp-task-manager-web").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}

	for _, forbidden := range []string{
		"github.com/gpayer/mcp-task-manager/internal/app",
		"github.com/gpayer/mcp-task-manager/internal/tools",
		"github.com/mark3labs/mcp-go/server",
	} {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.TrimSpace(line) == forbidden {
				t.Errorf("cmd/mcp-task-manager-web imports %s; the dashboard is HTTP only", forbidden)
			}
		}
	}
}
