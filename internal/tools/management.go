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
		mcp.WithObject("fields",
			mcp.Description(createText.param("fields")),
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
		mcp.WithObject("fields",
			mcp.Description(updateText.param("fields")),
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
		mcp.WithObject("fields",
			mcp.Description(listText.param("fields")),
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

		var opts []task.CreateOption
		if fields, ok := fieldsArg(req, "fields"); ok {
			opts = append(opts, task.WithCreateFields(fields))
		}

		t, err := svc.Create(title, description, priority, taskType, parentID, customID, opts...)
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
	OrphanedID  string              `json:"orphaned_id,omitempty"`
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
	// Fields is the task's free-form key/value metadata, empty when it has
	// none.
	Fields   task.Fields  `json:"fields,omitempty"`
	Subtasks []*task.Task `json:"subtasks,omitempty"`
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
			OrphanedID:  t.OrphanedID,
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
			Fields:         t.Fields,
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
		if fields, ok := fieldsArg(req, "fields"); ok {
			opts = append(opts, task.WithFields(fields))
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

		var listOpts []task.ListOption
		if fields, ok := fieldsArg(req, "fields"); ok {
			listOpts = append(listOpts, task.WithFieldFilter(fieldFilter(fields)))
		}

		tasks := svc.List(status, priority, taskType, parentID, resolution, listOpts...)

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

// fieldsArg reads an object argument as the free-form field map. ok is false
// when the caller did not pass the argument at all, which is what tells
// "leave the fields alone" from "change these fields" - the same distinction
// the rest of this file draws with the args lookup.
//
// A non-object value is reported as an empty map rather than an error: the
// service validates the pairs and answers with a message that names the key,
// which is more useful than a type complaint here.
func fieldsArg(req mcp.CallToolRequest, name string) (map[string]any, bool) {
	raw, ok := req.GetArguments()[name]
	if !ok {
		return nil, false
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}, true
	}
	return fields, true
}

// fieldFilter renders a filter object's values the way Fields.String renders a
// stored one, so {"count": 3} and {"count": "3"} both match `count: 3`.
func fieldFilter(fields map[string]any) map[string]string {
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		if value == nil {
			out[key] = ""
			continue
		}
		out[key] = fmt.Sprint(value)
	}
	return out
}
