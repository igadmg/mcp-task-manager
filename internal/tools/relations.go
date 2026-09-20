package tools

import (
	"context"
	"fmt"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func relationTools(rs *project.Resolver, relationTypes []string) []server.ServerTool {
	var tools []server.ServerTool

	// add_relation
	addText := textFor("add_relation")
	addTool := mcp.NewTool("add_relation",
		mcp.WithDescription(addText.Description),
		mcp.WithString("source",
			mcp.Required(),
			mcp.Description(addText.param("source")),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description(allowedValuesDescription(addText.param("type"), relationTypes)),
			mcp.Enum(relationTypes...),
		),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description(addText.param("target")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: addTool, Handler: addRelationHandler(rs)})

	// remove_relation
	removeText := textFor("remove_relation")
	removeTool := mcp.NewTool("remove_relation",
		mcp.WithDescription(removeText.Description),
		mcp.WithString("source",
			mcp.Required(),
			mcp.Description(removeText.param("source")),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description(allowedValuesDescription(removeText.param("type"), relationTypes)),
			mcp.Enum(relationTypes...),
		),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description(removeText.param("target")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: removeTool, Handler: removeRelationHandler(rs)})
	return tools
}

func addRelationHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		source := req.GetString("source", "")
		relationType := req.GetString("type", "")
		target := req.GetString("target", "")

		if err := svc.AddRelation(source, relationType, target); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Added %s relation from task %s to task %s", relationType, source, target)), nil
	})
}

func removeRelationHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		source := req.GetString("source", "")
		relationType := req.GetString("type", "")
		target := req.GetString("target", "")

		if err := svc.RemoveRelation(source, relationType, target); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Removed %s relation from task %s to task %s", relationType, source, target)), nil
	})
}
