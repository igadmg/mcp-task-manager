package tools

import (
	"context"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func webTools(rs *project.Resolver, web WebStarter) []server.ServerTool {
	if web == nil {
		return nil
	}

	text := textFor("start_web_ui")
	tool := mcp.NewTool("start_web_ui",
		mcp.WithDescription(text.Description),
		mcp.WithString("addr",
			mcp.Description(text.param("addr")),
		),
	)
	return []server.ServerTool{{Tool: tool, Handler: startWebUIHandler(rs, web)}}
}

// startWebUIHandler wraps withService although it never uses the service: the
// resolution side effect is the point. It guarantees the project is resolved
// before the listener accepts anything - and resolving is also what registers
// the project as a dashboard session, so the URL reported below has a token
// in it and lands on a board instead of the workspace list.
func startWebUIHandler(rs *project.Resolver, web WebStarter) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, _ *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, already, err := web.Start(req.GetString("addr", ""))
		if err != nil {
			return mcp.NewToolResultError("could not start the web dashboard: " + err.Error()), nil
		}
		url += sessionPath(rs, web)
		if already {
			return mcp.NewToolResultText("Task dashboard already running: " + url), nil
		}
		return mcp.NewToolResultText("Task dashboard started: " + url + " (read-only)"), nil
	})
}

// sessionPath is the resolved project's path on the dashboard. A miss falls
// back to the server root, which serves the workspace list: the tool must not
// fail over a link detail, and the list is one click from the board.
func sessionPath(rs *project.Resolver, web WebStarter) string {
	resolved, ok := rs.Current()
	if !ok || resolved.Config == nil {
		return "/"
	}
	if path, found := web.SessionPath(resolved.Config.TasksDir()); found {
		return path
	}
	return "/"
}
