package tools

import (
	"context"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func workflowTools(rs *project.Resolver) []server.ServerTool {
	var tools []server.ServerTool

	// get_next_task
	nextText := textFor("get_next_task")
	nextTool := mcp.NewTool("get_next_task",
		mcp.WithDescription(nextText.Description),
	)
	tools = append(tools, server.ServerTool{Tool: nextTool, Handler: getNextTaskHandler(rs)})

	// start_task
	startText := textFor("start_task")
	startTool := mcp.NewTool("start_task",
		mcp.WithDescription(startText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(startText.param("id")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: startTool, Handler: startTaskHandler(rs)})

	// complete_task
	completeText := textFor("complete_task")
	completeTool := mcp.NewTool("complete_task",
		mcp.WithDescription(completeText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(completeText.param("id")),
		),
		mcp.WithString("resolution",
			mcp.Description(completeText.param("resolution")),
			mcp.Enum(task.ResolutionStrings()...),
		),
		mcp.WithString("resolution_note",
			mcp.Description(completeText.param("resolution_note")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: completeTool, Handler: completeTaskHandler(rs)})
	return tools
}

func getNextTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Check project exists for read operation
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		t := svc.GetNextTask()
		if t == nil {
			return mcp.NewToolResultText("No tasks available"), nil
		}
		return taskResult(t)
	})
}

func startTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")

		t, err := svc.StartTask(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return taskResult(t)
	})
}

func completeTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")

		args := req.GetArguments()
		var opts []task.UpdateOption
		if _, ok := args["resolution"]; ok {
			opts = append(opts, task.WithResolution(task.Resolution(req.GetString("resolution", ""))))
		}
		if _, ok := args["resolution_note"]; ok {
			opts = append(opts, task.WithResolutionNote(req.GetString("resolution_note", "")))
		}

		t, err := svc.CompleteTask(id, opts...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Trigger auto-archive check if enabled
		_ = svc.RunAutoArchive()

		return taskResult(t)
	})
}
