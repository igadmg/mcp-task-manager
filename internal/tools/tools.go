package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// WebStarter starts the read-only web dashboard on demand. A nil WebStarter
// omits start_web_ui from the tool set, which is what CLI-built sets and most
// tests want. internal/tools must not import internal/web, so the interface
// lives here and internal/app asserts that *web.Controller satisfies it.
type WebStarter interface {
	Start(addr string) (url string, already bool, err error)
	URL() (url string, running bool)
	// SessionPath is the path, under the listener's base URL, that serves
	// the backlog in tasksDir. Every dashboard URL is token-prefixed, so
	// the base URL alone only reaches the workspace list.
	SessionPath(tasksDir string) (path string, found bool)
}

// Build assembles the full tool set. The task types and relation types are
// baked into the tool schemas, so the set has to be rebuilt (and the client
// notified) if resolving the project yields a different configuration than
// the one the server started with.
//
// Pass the same web starter on every rebuild: dropping it silently removes
// start_web_ui from the published tool list.
func Build(rs *project.Resolver, validTypes []string, relationTypes []string, web WebStarter) []server.ServerTool {
	var tools []server.ServerTool
	tools = append(tools, managementTools(rs, validTypes)...)
	tools = append(tools, workflowTools(rs)...)
	tools = append(tools, relationTools(rs, relationTypes)...)
	tools = append(tools, fileTools(rs)...)
	tools = append(tools, webTools(rs, web)...)
	return tools
}

// Register registers all MCP tools with the server
func Register(s *server.MCPServer, rs *project.Resolver, validTypes []string, relationTypes []string, web WebStarter) {
	s.AddTools(Build(rs, validTypes, relationTypes, web)...)
}

// withService resolves the project before running a handler. Resolution is
// deferred to the first tool call because MCP roots only become available
// after initialize.
func withService(rs *project.Resolver, fn func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		svc, err := rs.Service(ctx)
		if err != nil {
			return mcp.NewToolResultError("could not determine which project to use: " + err.Error()), nil
		}
		return fn(ctx, svc, req)
	}
}

func allowedValuesDescription(label string, values []string) string {
	return fmt.Sprintf("%s Allowed values: %s.", label, strings.Join(values, ", "))
}
