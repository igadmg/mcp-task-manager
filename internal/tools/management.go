package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func managementTools(rs *project.Resolver, validTypes []string) []server.ServerTool {
	var tools []server.ServerTool

	// create_task
	createText := textFor("create_task")
	createTool := mcp.NewTool("create_task",
		mcp.WithDescription(createText.Description),
		mcp.WithString("title",
			mcp.Required(),
			mcp.Description(createText.param("title")),
		),
		mcp.WithString("description",
			mcp.Description(createText.param("description")),
		),
		mcp.WithString("priority",
			mcp.Required(),
			mcp.Description(createText.param("priority")),
			mcp.Enum("critical", "high", "medium", "low"),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description(allowedValuesDescription(createText.param("type"), validTypes)),
			mcp.Enum(validTypes...),
		),
		mcp.WithString("parent_id",
			mcp.Description(createText.param("parent_id")),
		),
		mcp.WithString("id",
			mcp.Description(createText.param("id")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: createTool, Handler: createTaskHandler(rs)})

	// get_task
	getText := textFor("get_task")
	getTool := mcp.NewTool("get_task",
		mcp.WithDescription(getText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(getText.param("id")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: getTool, Handler: getTaskHandler(rs)})

	// update_task
	updateText := textFor("update_task")
	updateTool := mcp.NewTool("update_task",
		mcp.WithDescription(updateText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(updateText.param("id")),
		),
		mcp.WithString("title",
			mcp.Description(updateText.param("title")),
		),
		mcp.WithString("description",
			mcp.Description(updateText.param("description")),
		),
		mcp.WithString("status",
			mcp.Description(updateText.param("status")),
			mcp.Enum("todo", "in_progress", "done"),
		),
		mcp.WithString("priority",
			mcp.Description(updateText.param("priority")),
			mcp.Enum("critical", "high", "medium", "low"),
		),
		mcp.WithString("type",
			mcp.Description(allowedValuesDescription(updateText.param("type"), validTypes)),
			mcp.Enum(validTypes...),
		),
		mcp.WithString("resolution",
			mcp.Description(updateText.param("resolution")),
			mcp.Enum(task.ResolutionStrings()...),
		),
		mcp.WithString("resolution_note",
			mcp.Description(updateText.param("resolution_note")),
		),
		mcp.WithBoolean("verified",
			mcp.Description(updateText.param("verified")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: updateTool, Handler: updateTaskHandler(rs)})

	// delete_task
	deleteText := textFor("delete_task")
	deleteTool := mcp.NewTool("delete_task",
		mcp.WithDescription(deleteText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(deleteText.param("id")),
		),
		mcp.WithBoolean("delete_subtasks",
			mcp.Description(deleteText.param("delete_subtasks")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: deleteTool, Handler: deleteTaskHandler(rs)})

	// list_tasks
	listText := textFor("list_tasks")
	listTool := mcp.NewTool("list_tasks",
		mcp.WithDescription(listText.Description),
		mcp.WithString("status",
			mcp.Description(listText.param("status")),
			mcp.Enum("todo", "in_progress", "done"),
		),
		mcp.WithString("priority",
			mcp.Description(listText.param("priority")),
			mcp.Enum("critical", "high", "medium", "low"),
		),
		mcp.WithString("type",
			mcp.Description(allowedValuesDescription(listText.param("type"), validTypes)),
			mcp.Enum(validTypes...),
		),
		mcp.WithString("parent_id",
			mcp.Description(listText.param("parent_id")),
		),
		mcp.WithString("resolution",
			mcp.Description(listText.param("resolution")),
			mcp.Enum(task.ResolutionStrings()...),
		),
		mcp.WithBoolean("archived",
			mcp.Description(listText.param("archived")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: listTool, Handler: listTasksHandler(rs)})

	// archive_task
	archiveText := textFor("archive_task")
	archiveTool := mcp.NewTool("archive_task",
		mcp.WithDescription(archiveText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(archiveText.param("id")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: archiveTool, Handler: archiveTaskHandler(rs)})
	return tools
}

func createTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title := req.GetString("title", "")
		description := req.GetString("description", "")
		priority := task.Priority(req.GetString("priority", ""))
		taskType := req.GetString("type", "")
		parentID := req.GetString("parent_id", "")
		customID := req.GetString("id", "")

		t, err := svc.Create(title, description, priority, taskType, parentID, customID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(t)
	})
}

// taskWithSubtasksResponse is the response structure for get_task
type taskWithSubtasksResponse struct {
	ID          string              `json:"id"`
	ParentID    string              `json:"parent_id,omitempty"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Status      task.Status         `json:"status"`
	Priority    task.Priority       `json:"priority"`
	Type        string              `json:"type"`
	Relations   []task.Relation     `json:"relations,omitempty"`
	Blocked     bool                `json:"blocked"`
	BlockedBy   []task.BlockingInfo `json:"blocked_by,omitempty"`
	CreatedAt   string              `json:"created_at"`
	CreatedBy   string              `json:"created_by,omitempty"`
	UpdatedAt   string              `json:"updated_at"`
	// Resolution is the effective one: a task closed before the field
	// existed reports "completed" rather than an empty string.
	Resolution     task.Resolution `json:"resolution,omitempty"`
	ResolutionNote string          `json:"resolution_note,omitempty"`
	ClosedAt       string          `json:"closed_at,omitempty"`
	VerifiedAt     string          `json:"verified_at,omitempty"`
	Subtasks       []*task.Task    `json:"subtasks,omitempty"`
	// Phases is the task's phase-run history, in workflow order.
	Phases []task.PhaseRecord `json:"phases,omitempty"`
}

// formatOptionalTime renders a nullable timestamp for a tool response.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02T15:04:05Z")
}

func getTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Check project exists for read operation
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		id := req.GetString("id", "")

		d, err := svc.Detail(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		t := d.Task

		response := taskWithSubtasksResponse{
			ID:          t.ID,
			ParentID:    t.ParentID,
			Title:       t.Title,
			Description: t.Description,
			Status:      t.Status,
			Priority:    t.Priority,
			Type:        t.Type,
			Relations:   t.Relations,
			Blocked:     d.Blocked,
			BlockedBy:   d.Blockers,
			CreatedAt:   t.CreatedAt.Format("2006-01-02T15:04:05Z"),
			CreatedBy:   t.CreatedBy,
			UpdatedAt:   t.UpdatedAt.Format("2006-01-02T15:04:05Z"),

			Resolution:     t.EffectiveResolution(),
			ResolutionNote: t.ResolutionNote,
			ClosedAt:       formatOptionalTime(t.ClosedAt),
			VerifiedAt:     formatOptionalTime(t.VerifiedAt),
			Phases:         d.Phases,
		}

		// Only include subtasks if task has them (top-level task with children)
		if len(d.Subtasks) > 0 {
			response.Subtasks = d.Subtasks
		}

		return jsonResult(response)
	})
}

func updateTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")

		var title, description, taskType *string
		var status *task.Status
		var priority *task.Priority

		args := req.GetArguments()
		if _, ok := args["title"]; ok {
			v := req.GetString("title", "")
			title = &v
		}
		if _, ok := args["description"]; ok {
			v := req.GetString("description", "")
			description = &v
		}
		if _, ok := args["status"]; ok {
			s := task.Status(req.GetString("status", ""))
			status = &s
		}
		if _, ok := args["priority"]; ok {
			p := task.Priority(req.GetString("priority", ""))
			priority = &p
		}
		if _, ok := args["type"]; ok {
			v := req.GetString("type", "")
			taskType = &v
		}

		var opts []task.UpdateOption
		if _, ok := args["resolution"]; ok {
			opts = append(opts, task.WithResolution(task.Resolution(req.GetString("resolution", ""))))
		}
		if _, ok := args["resolution_note"]; ok {
			opts = append(opts, task.WithResolutionNote(req.GetString("resolution_note", "")))
		}
		if _, ok := args["verified"]; ok {
			opts = append(opts, task.WithVerified(req.GetBool("verified", false)))
		}

		t, err := svc.Update(id, title, description, status, priority, taskType, opts...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(t)
	})
}

func deleteTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")
		deleteSubtasks := req.GetBool("delete_subtasks", false)

		if err := svc.Delete(id, deleteSubtasks); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Task %s deleted", id)), nil
	})
}

func archiveTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")
		if err := svc.ArchiveTask(id); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Task %s archived", id)), nil
	})
}

func listTasksHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Check project exists for read operation
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// If archived flag is set, return archived tasks
		if req.GetBool("archived", false) {
			tasks, err := svc.ListArchived()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(tasks) == 0 {
				return mcp.NewToolResultText("No archived tasks found"), nil
			}
			return jsonResult(tasks)
		}

		var status *task.Status
		var priority *task.Priority
		var taskType *string

		args := req.GetArguments()
		if _, ok := args["status"]; ok {
			s := task.Status(req.GetString("status", ""))
			status = &s
		}
		if _, ok := args["priority"]; ok {
			p := task.Priority(req.GetString("priority", ""))
			priority = &p
		}
		if _, ok := args["type"]; ok {
			v := req.GetString("type", "")
			taskType = &v
		}

		var resolution *task.Resolution
		if _, ok := args["resolution"]; ok {
			r := task.Resolution(req.GetString("resolution", ""))
			resolution = &r
		}

		// Default to showing top-level tasks (parentID = "0")
		// If parent_id is explicitly provided, use that value
		defaultParentID := "0"
		parentID := &defaultParentID
		if _, ok := args["parent_id"]; ok {
			id := req.GetString("parent_id", "")
			parentID = &id
		}

		tasks := svc.List(status, priority, taskType, parentID, resolution)

		if len(tasks) == 0 {
			return mcp.NewToolResultText("No tasks found"), nil
		}

		// Enrich tasks with blocked field
		type taskWithBlocked struct {
			*task.Task
			Blocked bool `json:"blocked"`
		}
		ids := make([]string, len(tasks))
		for i, t := range tasks {
			ids[i] = t.ID
		}
		blocked := svc.BlockedMap(ids)

		enriched := make([]taskWithBlocked, len(tasks))
		for i, t := range tasks {
			enriched[i] = taskWithBlocked{Task: t, Blocked: len(blocked[t.ID]) > 0}
		}

		return jsonResult(enriched)
	})
}

// jsonResult renders v as an indented JSON tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
