package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func fileTools(rs *project.Resolver) []server.ServerTool {
	var tools []server.ServerTool

	// write_task_file
	writeText := textFor("write_task_file")
	writeTool := mcp.NewTool("write_task_file",
		mcp.WithDescription(writeText.Description),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description(writeText.param("task_id")),
		),
		mcp.WithString("filename",
			mcp.Required(),
			mcp.Description(writeText.param("filename")),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description(writeText.param("content")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: writeTool, Handler: writeTaskFileHandler(rs)})

	// read_task_file
	readText := textFor("read_task_file")
	readTool := mcp.NewTool("read_task_file",
		mcp.WithDescription(readText.Description),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description(readText.param("task_id")),
		),
		mcp.WithString("filename",
			mcp.Required(),
			mcp.Description(readText.param("filename")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: readTool, Handler: readTaskFileHandler(rs)})

	// list_task_files
	listText := textFor("list_task_files")
	listTool := mcp.NewTool("list_task_files",
		mcp.WithDescription(listText.Description),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description(listText.param("task_id")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: listTool, Handler: listTaskFilesHandler(rs)})
	return tools
}

func writeTaskFileHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskID := req.GetString("task_id", "")
		filename := req.GetString("filename", "")
		content := req.GetString("content", "")

		if err := svc.WriteTaskFile(taskID, filename, content); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Wrote file %q to task %s", filename, taskID)), nil
	})
}

func readTaskFileHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		taskID := req.GetString("task_id", "")
		filename := req.GetString("filename", "")

		content, err := svc.ReadTaskFile(taskID, filename)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(content), nil
	})
}

func listTaskFilesHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		taskID := req.GetString("task_id", "")

		names, err := svc.ListTaskFiles(taskID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if len(names) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No files attached to task %s", taskID)), nil
		}

		data, err := json.MarshalIndent(names, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	})
}
