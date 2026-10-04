package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/gpayer/mcp-task-manager/internal/app"
	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// loadConfig loads configuration only (does not create directories)
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// initServiceWithConfig initializes the task service with an already loaded
// config. Construction lives in internal/project so the CLI, the MCP server
// and the web entry point all build a project the same way.
func initServiceWithConfig(cfg *config.Config) (*task.Service, error) {
	resolved, err := project.Build(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize: %w", err)
	}
	return resolved.Service, nil
}

// initService initializes the task service (loads config and initializes)
func initService() (*task.Service, *config.Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}

	svc, err := initServiceWithConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	return svc, cfg, nil
}

// checkProjectExists verifies a project was found, returns exit code
func checkProjectExists(stderr io.Writer, cfg *config.Config) int {
	if !cfg.ProjectFound {
		fmt.Fprintln(stderr, "Error: no tasks directory found.")
		fmt.Fprintf(stderr, "Looked for: %s\n", cfg.Resolution.Explain())
		fmt.Fprintln(stderr, "Create a task to initialize one here, or set MCP_TASKS_DIR.")
		return 1
	}
	return 0
}

// cmdList handles the list command
func cmdList(stdout, stderr io.Writer, jsonOutput bool, status, priority, taskType string, parentID string, archived bool) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Check project exists for read operation
	if code := checkProjectExists(stderr, cfg); code != 0 {
		return code
	}

	svc, err := initServiceWithConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if archived {
		tasks, err := svc.ListArchived()
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if jsonOutput {
			if tasks == nil {
				tasks = []*task.Task{}
			}
			if err := FormatJSON(stdout, tasks); err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
		} else {
			fmt.Fprint(stdout, FormatTaskTable(tasks, nil, nil))
		}
		return 0
	}

	var statusPtr *task.Status
	var priorityPtr *task.Priority
	var typePtr *string

	if status != "" {
		s := task.Status(status)
		statusPtr = &s
	}
	if priority != "" {
		p := task.Priority(priority)
		priorityPtr = &p
	}
	if taskType != "" {
		typePtr = &taskType
	}

	// parentID semantics:
	// - Default ("0"): show top-level tasks only (parentID = "0")
	// - Specified N: show subtasks of task N (parentID = N)
	parentPtr := &parentID
	tasks := svc.List(statusPtr, priorityPtr, typePtr, parentPtr, nil)

	// Build subtask counts for each task
	subtaskCounts := make(map[string]SubtaskCounts)
	for _, t := range tasks {
		total, done := svc.GetSubtaskCounts(t.ID)
		if total > 0 {
			subtaskCounts[t.ID] = SubtaskCounts{Total: total, Done: done}
		}
	}

	// Build blocked status for each task
	blockedTasks := make(map[string]bool)
	for _, t := range tasks {
		if blocked, _ := svc.IsBlocked(t.ID); blocked {
			blockedTasks[t.ID] = true
		}
	}

	if jsonOutput {
		// Ensure we always output a JSON array, even if empty
		if tasks == nil {
			tasks = []*task.Task{}
		}
		if err := FormatJSON(stdout, tasks); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprint(stdout, FormatTaskTable(tasks, subtaskCounts, blockedTasks))
	}

	return 0
}

// cmdGet handles the get command
func cmdGet(stdout, stderr io.Writer, jsonOutput bool, id string) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Check project exists for read operation
	if code := checkProjectExists(stderr, cfg); code != 0 {
		return code
	}

	svc, err := initServiceWithConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	d, err := svc.Detail(id)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if jsonOutput {
		// Include subtasks and the phase history in JSON output
		type taskWithSubtasks struct {
			*task.Task
			Subtasks []*task.Task       `json:"subtasks,omitempty"`
			Phases   []task.PhaseRecord `json:"phases,omitempty"`
		}
		output := taskWithSubtasks{Task: d.Task, Phases: d.Phases}
		if len(d.Subtasks) > 0 {
			output.Subtasks = d.Subtasks
		}
		if err := FormatJSON(stdout, output); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		opts := &TaskDetailOptions{
			Subtasks: d.Subtasks,
			Blocked:  d.Blocked,
			Blockers: d.Blockers,
			Phases:   d.Phases,
		}
		fmt.Fprint(stdout, FormatTaskDetail(d.Task, opts))
	}

	return 0
}

// cmdNext handles the next command
func cmdNext(stdout, stderr io.Writer, jsonOutput bool) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Check project exists for read operation
	if code := checkProjectExists(stderr, cfg); code != 0 {
		return code
	}

	svc, err := initServiceWithConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	t := svc.GetNextTask()
	if t == nil {
		if jsonOutput {
			FormatJSON(stdout, map[string]string{"message": "No tasks available"})
		} else {
			fmt.Fprintln(stdout, "No tasks available.")
		}
		return 0
	}

	if jsonOutput {
		if err := FormatJSON(stdout, t); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprint(stdout, FormatTaskDetail(t, nil))
	}

	return 0
}

// cmdCreate handles the create command
func cmdCreate(stdout, stderr io.Writer, jsonOutput bool, title, priority, taskType, description string, parentID string, id string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	t, err := svc.Create(title, description, task.Priority(priority), taskType, parentID, id)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if jsonOutput {
		if err := FormatJSON(stdout, t); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprint(stdout, FormatTaskDetail(t, nil))
	}

	return 0
}

// cmdUpdate handles the update command
func cmdUpdate(stdout, stderr io.Writer, jsonOutput bool, id string, title, status, priority, taskType, description, resolution, resolutionNote string, verified bool) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	var titlePtr, descPtr, typePtr *string
	var statusPtr *task.Status
	var priorityPtr *task.Priority

	if title != "" {
		titlePtr = &title
	}
	if description != "" {
		descPtr = &description
	}
	if status != "" {
		s := task.Status(status)
		statusPtr = &s
	}
	if priority != "" {
		p := task.Priority(priority)
		priorityPtr = &p
	}
	if taskType != "" {
		typePtr = &taskType
	}

	var opts []task.UpdateOption
	if resolution != "" {
		opts = append(opts, task.WithResolution(task.Resolution(resolution)))
	}
	if resolutionNote != "" {
		opts = append(opts, task.WithResolutionNote(resolutionNote))
	}
	if verified {
		opts = append(opts, task.WithVerified(true))
	}

	t, err := svc.Update(id, titlePtr, descPtr, statusPtr, priorityPtr, typePtr, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if jsonOutput {
		if err := FormatJSON(stdout, t); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprint(stdout, FormatTaskDetail(t, nil))
	}

	return 0
}

// cmdDelete handles the delete command
func cmdDelete(stdout, stderr io.Writer, jsonOutput bool, id string, force bool) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if err := svc.Delete(id, force); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	msg := fmt.Sprintf("Task #%s deleted.", id)
	if jsonOutput {
		if err := FormatJSONMessage(stdout, msg, id); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintln(stdout, msg)
	}

	return 0
}

// cmdStart handles the start command
func cmdStart(stdout, stderr io.Writer, jsonOutput bool, id string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	started, err := svc.StartTask(id)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	msg := fmt.Sprintf("Task #%s started.", id)
	if started.Branch != "" {
		msg = fmt.Sprintf("Task #%s started on branch %s.", id, started.Branch)
	}
	if jsonOutput {
		if err := FormatJSONMessage(stdout, msg, id); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintln(stdout, msg)
	}

	return 0
}

// cmdStartPhase handles the start-phase command
func cmdStartPhase(stdout, stderr io.Writer, jsonOutput bool, id, phase string) int {
	p, err := task.ParsePhase(phase)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	started, rec, err := svc.StartPhase(id, p)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	run := len(rec.Runs)
	msg := fmt.Sprintf("Started phase %s of task %s (run %d)", p, id, run)
	if started.Branch != "" {
		msg += " on branch " + started.Branch
	}
	return printPhaseMessage(stdout, stderr, jsonOutput, msg+".", id, p, run)
}

// cmdFinishPhase handles the finish-phase command
func cmdFinishPhase(stdout, stderr io.Writer, jsonOutput bool, id, phase, tokens, note string) int {
	p, err := task.ParsePhase(phase)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	f := task.PhaseFinish{Note: note}
	if tokens != "" {
		// The service checks the range.
		n, err := strconv.ParseInt(tokens, 10, 64)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", task.ErrInvalidTokens)
			return 1
		}
		f.Tokens = &n
	}
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	_, rec, err := svc.FinishPhase(id, p, f)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	run := len(rec.Runs)
	msg := fmt.Sprintf("Finished phase %s of task %s (run %d", p, id, run)
	if f.Tokens != nil {
		msg += fmt.Sprintf(", %d tokens", *f.Tokens)
	}
	return printPhaseMessage(stdout, stderr, jsonOutput, msg+").", id, p, run)
}

// printPhaseMessage prints a phase command's result: the message, or with
// --json {message, id, phase, run}.
func printPhaseMessage(stdout, stderr io.Writer, jsonOutput bool, msg, id string, p task.Phase, run int) int {
	if !jsonOutput {
		fmt.Fprintln(stdout, msg)
		return 0
	}
	if err := FormatJSON(stdout, map[string]any{"message": msg, "id": id, "phase": p, "run": run}); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// cmdArchive handles the archive command
func cmdArchive(stdout, stderr io.Writer, jsonOutput bool, id string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if err := svc.ArchiveTask(id); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	msg := fmt.Sprintf("Task #%s archived.", id)
	if jsonOutput {
		if err := FormatJSONMessage(stdout, msg, id); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintln(stdout, msg)
	}

	return 0
}

// cmdWriteTaskFile handles the write-task-file command
func cmdWriteTaskFile(stdout, stderr io.Writer, id string, filename, content string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if err := svc.WriteTaskFile(id, filename, content); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Wrote file %q to task #%s.\n", filename, id)
	return 0
}

// cmdReadTaskFile handles the read-task-file command
func cmdReadTaskFile(stdout, stderr io.Writer, id string, filename string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	content, err := svc.ReadTaskFile(id, filename)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Fprint(stdout, content)
	return 0
}

// cmdListTaskFiles handles the list-task-files command
func cmdListTaskFiles(stdout, stderr io.Writer, id string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	names, err := svc.ListTaskFiles(id)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	for _, name := range names {
		fmt.Fprintln(stdout, name)
	}
	return 0
}

// cmdComplete handles the complete command
func cmdComplete(stdout, stderr io.Writer, jsonOutput bool, id, resolution, resolutionNote, commitMessage string) int {
	svc, _, err := initService()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	var opts []task.UpdateOption
	if resolution != "" {
		opts = append(opts, task.WithResolution(task.Resolution(resolution)))
	}
	if resolutionNote != "" {
		opts = append(opts, task.WithResolutionNote(resolutionNote))
	}
	if commitMessage != "" {
		opts = append(opts, task.WithCommitMessage(commitMessage))
	}

	done, err := svc.CompleteTask(id, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	msg := fmt.Sprintf("Task #%s completed.", id)
	if resolution != "" && resolution != string(task.ResolutionCompleted) {
		msg = fmt.Sprintf("Task #%s closed as %s.", id, resolution)
	}
	// A delivered completion under git branching ends on a known branch:
	// the final one, or the parent's wip a subtask was merged into.
	if svc.BranchingEnabled() && done.Branch != "" && done.EffectiveResolution().Delivered() {
		if done.FinalBranch != "" {
			msg += fmt.Sprintf(" Final branch: %s.", done.FinalBranch)
		} else if done.ParentID != "" {
			msg += fmt.Sprintf(" Merged into %s.", done.BaseBranch)
		}
	}
	if jsonOutput {
		if err := FormatJSONMessage(stdout, msg, id); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintln(stdout, msg)
	}

	return 0
}

// cmdServeWeb runs the dashboard in the foreground until the context is
// cancelled. Exit 0 on a clean shutdown, 1 on a startup failure - an occupied
// port or an unresolvable project must not look like success.
func cmdServeWeb(ctx context.Context, stderr io.Writer, addr string, withMCP bool) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	webCfg := cfg.Web
	webCfg.Enabled = true
	if addr != "" {
		webCfg.Addr = addr
	}
	if withMCP {
		webCfg.WithMCP = true
	}

	if err := app.RunWeb(ctx, app.Options{Web: webCfg, Stderr: stderr}); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
