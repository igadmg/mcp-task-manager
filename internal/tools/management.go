package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerManagementTools(s *server.MCPServer, svc *task.Service, validTypes []string) {
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
	s.AddTool(createTool, createTaskHandler(svc))

	// get_task
	getText := textFor("get_task")
	getTool := mcp.NewTool("get_task",
		mcp.WithDescription(getText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(getText.param("id")),
		),
	)
	s.AddTool(getTool, getTaskHandler(svc))

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
	)
	s.AddTool(updateTool, updateTaskHandler(svc))

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
	s.AddTool(deleteTool, deleteTaskHandler(svc))

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
		mcp.WithBoolean("archived",
			mcp.Description(listText.param("archived")),
		),
	)
	s.AddTool(listTool, listTasksHandler(svc))

	// archive_task
	archiveText := textFor("archive_task")
	archiveTool := mcp.NewTool("archive_task",
		mcp.WithDescription(archiveText.Description),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description(archiveText.param("id")),
		),
	)
	s.AddTool(archiveTool, archiveTaskHandler(svc))
}

func createTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		return taskResult(t)
	}
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
	UpdatedAt   string              `json:"updated_at"`
	Subtasks    []*task.Task        `json:"subtasks,omitempty"`
}

func getTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Check project exists for read operation
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		id := req.GetString("id", "")

		t, subtasks, err := svc.GetWithSubtasks(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		blocked, blockers := svc.IsBlocked(id)

		response := taskWithSubtasksResponse{
			ID:          t.ID,
			ParentID:    t.ParentID,
			Title:       t.Title,
			Description: t.Description,
			Status:      t.Status,
			Priority:    t.Priority,
			Type:        t.Type,
			Relations:   t.Relations,
			Blocked:     blocked,
			BlockedBy:   blockers,
			CreatedAt:   t.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:   t.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}

		// Only include subtasks if task has them (top-level task with children)
		if len(subtasks) > 0 {
			response.Subtasks = subtasks
		}

		data, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

func updateTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		t, err := svc.Update(id, title, description, status, priority, taskType)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return taskResult(t)
	}
}

func deleteTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")
		deleteSubtasks := req.GetBool("delete_subtasks", false)

		if err := svc.Delete(id, deleteSubtasks); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Task %s deleted", id)), nil
	}
}

func archiveTaskHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")
		if err := svc.ArchiveTask(id); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Task %s archived", id)), nil
	}
}

func listTasksHandler(svc *task.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			data, err := json.MarshalIndent(tasks, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(data)), nil
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

		// Default to showing top-level tasks (parentID = "0")
		// If parent_id is explicitly provided, use that value
		defaultParentID := "0"
		parentID := &defaultParentID
		if _, ok := args["parent_id"]; ok {
			id := req.GetString("parent_id", "")
			parentID = &id
		}

		tasks := svc.List(status, priority, taskType, parentID)

		if len(tasks) == 0 {
			return mcp.NewToolResultText("No tasks found"), nil
		}

		// Enrich tasks with blocked field
		type taskWithBlocked struct {
			*task.Task
			Blocked bool `json:"blocked"`
		}
		enriched := make([]taskWithBlocked, len(tasks))
		for i, t := range tasks {
			blocked, _ := svc.IsBlocked(t.ID)
			enriched[i] = taskWithBlocked{Task: t, Blocked: blocked}
		}

		data, err := json.MarshalIndent(enriched, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

func taskResult(t *task.Task) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
