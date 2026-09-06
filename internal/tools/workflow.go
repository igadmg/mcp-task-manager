package tools

import (
	"context"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerWorkflowTools(s *server.MCPServer, svc *task.Service) {
	// get_next_task
	nextText := textFor("get_next_task")
	nextTool := mcp.NewTool("get_next_task",
		mcp.WithDescription(nextText.Description),
	)
	s.AddTool(nextTool, getNextTaskHandler(svc))

	// start_task
	startText := textFor("start_task")
	startTool := mcp.NewTool("start_task",
		mcp.WithDescription(startText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(startText.param("id")),
		),
	)
	s.AddTool(startTool, startTaskHandler(svc))

	// complete_task
	completeText := textFor("complete_task")
	completeTool := mcp.NewTool("complete_task",
		mcp.WithDescription(completeText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(completeText.param("id")),
		),
	)
	s.AddTool(completeTool, completeTaskHandler(svc))
}

func getNextTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Check project exists for read operation
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		t := svc.GetNextTask()
		if t == nil {
			return mcp.NewToolResultText("No tasks available"), nil
		}
		return taskResult(t)
	}
}

func startTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")

		t, err := svc.StartTask(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return taskResult(t)
	}
}

func completeTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")

		t, err := svc.CompleteTask(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Trigger auto-archive check if enabled
		_ = svc.RunAutoArchive()

		return taskResult(t)
	}
}
