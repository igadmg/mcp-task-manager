package tools

import (
	"context"
	"math"

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
		mcp.WithString("commit_message",
			mcp.Description(completeText.param("commit_message")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: completeTool, Handler: completeTaskHandler(rs)})

	// get_current_task
	currentText := textFor("get_current_task")
	currentTool := mcp.NewTool("get_current_task",
		mcp.WithDescription(currentText.Description),
	)
	tools = append(tools, server.ServerTool{Tool: currentTool, Handler: getCurrentTaskHandler(rs)})

	// start_phase
	startPhaseText := textFor("start_phase")
	startPhaseTool := mcp.NewTool("start_phase",
		mcp.WithDescription(startPhaseText.Description),
		mcp.WithString("phase",
			mcp.Required(),
			mcp.Description(startPhaseText.param("phase")),
			mcp.Enum(task.PhaseStrings()...),
		),
		mcp.WithString("id",
			mcp.Description(startPhaseText.param("id")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: startPhaseTool, Handler: startPhaseHandler(rs)})

	// finish_phase
	finishPhaseText := textFor("finish_phase")
	finishPhaseTool := mcp.NewTool("finish_phase",
		mcp.WithDescription(finishPhaseText.Description),
		mcp.WithString("phase",
			mcp.Required(),
			mcp.Description(finishPhaseText.param("phase")),
			mcp.Enum(task.PhaseStrings()...),
		),
		mcp.WithString("id",
			mcp.Description(finishPhaseText.param("id")),
		),
		mcp.WithNumber("tokens",
			mcp.Description(finishPhaseText.param("tokens")),
		),
		mcp.WithString("note",
			mcp.Description(finishPhaseText.param("note")),
		),
	)
	tools = append(tools, server.ServerTool{Tool: finishPhaseTool, Handler: finishPhaseHandler(rs)})
	return tools
}

func startPhaseHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := task.ParsePhase(req.GetString("phase", ""))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		t, rec, err := svc.StartPhase(req.GetString("id", ""), p)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return phaseResult(t, rec)
	})
}

func finishPhaseHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := task.ParsePhase(req.GetString("phase", ""))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		f := task.PhaseFinish{Note: req.GetString("note", "")}
		if raw, ok := req.GetArguments()["tokens"]; ok {
			if f.Tokens, err = parseTokens(raw); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}

		t, rec, err := svc.FinishPhase(req.GetString("id", ""), p, f)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return phaseResult(t, rec)
	})
}

// parseTokens converts the tokens argument, a JSON number, to an integer.
// JSON-RPC delivers every number as a float64; it must be integral and fit
// an int64. The service checks the range.
func parseTokens(raw any) (*int64, error) {
	v, ok := raw.(float64)
	if !ok || v != math.Trunc(v) || math.Abs(v) >= math.MaxInt64 {
		return nil, task.ErrInvalidTokens
	}
	n := int64(v)
	return &n, nil
}

// phaseResponse is the result of start_phase and finish_phase: the task,
// and the phase record with the 1-based number of the run just started or
// finished.
type phaseResponse struct {
	Task   *task.Task        `json:"task"`
	Phase  task.Phase        `json:"phase"`
	Run    int               `json:"run"`
	Record *task.PhaseRecord `json:"record"`
}

func phaseResult(t *task.Task, rec *task.PhaseRecord) (*mcp.CallToolResult, error) {
	return jsonResult(phaseResponse{Task: t, Phase: rec.Phase, Run: len(rec.Runs), Record: rec})
}

func getCurrentTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := svc.EnsureProjectExists(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		tasks, err := svc.CurrentTasks()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(tasks) == 0 {
			return mcp.NewToolResultText("No current task"), nil
		}
		return jsonResult(currentTasksResult{Current: tasks[len(tasks)-1].ID, Tasks: tasks})
	})
}

// currentTasksResult is the get_current_task answer: the id of the most
// recently started task and the full records of every in-progress task, in
// the order they were started.
type currentTasksResult struct {
	Current string       `json:"current"`
	Tasks   []*task.Task `json:"tasks"`
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
		return jsonResult(t)
	})
}

func startTaskHandler(rs *project.Resolver) server.ToolHandlerFunc {
	return withService(rs, func(ctx context.Context, svc *task.Service, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := req.GetString("id", "")

		t, err := svc.StartTask(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(t)
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
		if msg := req.GetString("commit_message", ""); msg != "" {
			opts = append(opts, task.WithCommitMessage(msg))
		}

		t, err := svc.CompleteTask(id, opts...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Trigger auto-archive check if enabled
		_ = svc.RunAutoArchive()

		return jsonResult(t)
	})
}
