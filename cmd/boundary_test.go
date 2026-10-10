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

// depsOf runs `go list -deps` for a package and returns the dependency paths.
func depsOf(t *testing.T, pkg string) []string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain to walk the import graph with")
	}
	out, err := exec.Command(goBin, "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	var deps []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if dep := strings.TrimSpace(line); dep != "" {
			deps = append(deps, dep)
		}
	}
	return deps
}

func depsContain(deps []string, forbidden string) bool {
	for _, dep := range deps {
		if dep == forbidden {
			return true
		}
	}
	return false
}

// TestSessionHostBinaryKeepsTheBoundary is the session-host twin of the two
// tests above (design §1): the host runs AI sessions in a project folder, so
// it must never link the task service, the tool set, the MCP app or the
// dashboard - it talks to the world only through its HTTP API, and an agent
// inside a session reaches the backlog through MCP like any local agent.
func TestSessionHostBinaryKeepsTheBoundary(t *testing.T) {
	deps := depsOf(t, "./mcp-session-host")
	const base = "github.com/gpayer/mcp-task-manager/internal/"
	for _, forbidden := range []string{
		base + "task",
		base + "tools",
		base + "app",
		base + "web",
		base + "webapp",
		"github.com/mark3labs/mcp-go/server",
	} {
		if depsContain(deps, forbidden) {
			t.Errorf("cmd/mcp-session-host imports %s; the host owns sessions, never task data or the dashboard", forbidden)
		}
	}
}

// TestDashboardAndMCPBinariesDoNotLinkTheSessionHost: the session host is a
// third process reached over loopback HTTP (internal/hostclient), so neither
// the dashboard nor the MCP binary may link it - otherwise "separate process"
// degrades into "conveniently imported package".
func TestDashboardAndMCPBinariesDoNotLinkTheSessionHost(t *testing.T) {
	const forbidden = "github.com/gpayer/mcp-task-manager/internal/sessionhost"
	for _, binary := range []string{"./mcp-task-manager-web", "./mcp-task-manager"} {
		deps := depsOf(t, binary)
		if depsContain(deps, forbidden) {
			t.Errorf("%s imports %s; reach the host through internal/hostclient instead", binary, forbidden)
		}
		// claude backend is a subpackage: assert it by prefix.
		for _, dep := range deps {
			if strings.HasPrefix(dep, forbidden+"/") {
				t.Errorf("%s imports %s; reach the host through internal/hostclient instead", binary, dep)
			}
		}
	}
}

// TestSessionAPIIsStdlibOnly: internal/sessionapi is the shared vocabulary of
// web and host (Spec, Event, Status, Answer), so it may depend on nothing
// outside the standard library - any third-party import would be linked into
// both processes at once.
func TestSessionAPIIsStdlibOnly(t *testing.T) {
	deps := depsOf(t, "github.com/gpayer/mcp-task-manager/internal/sessionapi")
	const ownModule = "github.com/gpayer/mcp-task-manager"
	for _, dep := range deps {
		if strings.HasPrefix(dep, ownModule) {
			continue // the package itself
		}
		if first, _, _ := strings.Cut(dep, "/"); strings.Contains(first, ".") {
			t.Errorf("internal/sessionapi depends on non-stdlib package %s", dep)
		}
	}
}
