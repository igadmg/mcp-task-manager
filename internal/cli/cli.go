package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/integrii/flaggy"
)

// Version is set at build time
var Version = "dev"

// Run executes the CLI with os.Args
func Run() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(RunWithContext(ctx, os.Args, os.Stdout, os.Stderr))
}

// RunWithArgs executes the CLI with given arguments (for testing). The
// signature is unchanged on purpose - the whole CLI suite calls it.
func RunWithArgs(args []string, stdout, stderr io.Writer) int {
	return RunWithContext(context.Background(), args, stdout, stderr)
}

// RunWithContext is RunWithArgs plus a cancellation signal, which the
// foreground servers under `serve` need in order to shut down cleanly.
func RunWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// Reset flaggy for fresh parsing
	flaggy.ResetParser()

	taskTypes := []string{"feature", "bug"}
	if cfg, err := loadConfig(); err == nil && len(cfg.TaskTypes) > 0 {
		taskTypes = cfg.TaskTypes
	}
	defaultTaskType := "feature"
	if len(taskTypes) > 0 {
		defaultTaskType = taskTypes[0]
	}

	flaggy.SetName("mcp-task-manager")
	flaggy.SetDescription("Task manager for Claude and coding agents")

	// Disable built-in version flag since we're using a version subcommand
	flaggy.DefaultParser.DisableShowVersionWithVersion()

	// Version subcommand
	versionCmd := flaggy.NewSubcommand("version")
	versionCmd.Description = "Show version information"
	flaggy.AttachSubcommand(versionCmd, 1)

	// List subcommand
	listCmd := flaggy.NewSubcommand("list")
	listCmd.Description = "List tasks with optional filters"
	var listStatus, listPriority, listType string
	var listJSON bool
	var listParent = "0"
	var listArchived bool
	listCmd.String(&listStatus, "s", "status", "Filter by status (todo|in_progress|done)")
	listCmd.String(&listPriority, "p", "priority", "Filter by priority (critical|high|medium|low)")
	listCmd.String(&listType, "t", "type", fmt.Sprintf("Filter by type (%s)", strings.Join(taskTypes, "|")))
	listCmd.Bool(&listJSON, "j", "json", "Output as JSON")
	listCmd.String(&listParent, "", "parent", "List subtasks of parent task ID (default: top-level tasks)")
	listCmd.Bool(&listArchived, "a", "archived", "List archived tasks")
	var listFields []string
	listCmd.StringSlice(&listFields, "", "field", "Keep only tasks with this field value (key=value; repeatable, all must match)")
	flaggy.AttachSubcommand(listCmd, 1)

	// Get subcommand
	getCmd := flaggy.NewSubcommand("get")
	getCmd.Description = "Get task details by ID"
	var getIDStr string
	var getJSON bool
	getCmd.AddPositionalValue(&getIDStr, "id", 1, true, "Task ID")
	getCmd.Bool(&getJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(getCmd, 1)

	// Next subcommand
	nextCmd := flaggy.NewSubcommand("next")
	nextCmd.Description = "Get highest priority todo task"
	var nextJSON bool
	nextCmd.Bool(&nextJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(nextCmd, 1)

	// Create subcommand
	createCmd := flaggy.NewSubcommand("create")
	createCmd.Description = "Create a new task"
	var createTitle string
	var createPriority = "medium"
	var createType = defaultTaskType
	var createDesc string
	var createJSON bool
	var createParent string
	var createID string
	createCmd.AddPositionalValue(&createTitle, "title", 1, true, "Task title")
	createCmd.String(&createPriority, "p", "priority", "Priority (default: medium)")
	createCmd.String(&createType, "t", "type", fmt.Sprintf("Type (%s; default: %s)", strings.Join(taskTypes, "|"), defaultTaskType))
	createCmd.String(&createDesc, "d", "description", "Task description")
	createCmd.Bool(&createJSON, "j", "json", "Output as JSON")
	createCmd.String(&createParent, "", "parent", "Parent task ID (creates a subtask)")
	createCmd.String(&createID, "", "id", "Optional custom task id (used verbatim as id and directory name instead of auto-increment)")
	var createFields []string
	createCmd.StringSlice(&createFields, "", "field", "Free-form field to set (key=value; repeatable)")
	flaggy.AttachSubcommand(createCmd, 1)

	// Update subcommand
	updateCmd := flaggy.NewSubcommand("update")
	updateCmd.Description = "Update an existing task"
	var updateIDStr string
	var updateTitle, updateStatus, updatePriority, updateType, updateDesc string
	var updateResolution, updateNote string
	var updateJSON, updateVerified bool
	updateCmd.AddPositionalValue(&updateIDStr, "id", 1, true, "Task ID")
	updateCmd.String(&updateTitle, "", "title", "New title")
	updateCmd.String(&updateStatus, "s", "status", "New status")
	updateCmd.String(&updatePriority, "p", "priority", "New priority")
	updateCmd.String(&updateType, "t", "type", fmt.Sprintf("New type (%s)", strings.Join(taskTypes, "|")))
	updateCmd.String(&updateDesc, "d", "description", "New description")
	updateCmd.String(&updateResolution, "", "resolution", fmt.Sprintf("Close the task with this resolution (%s)", strings.Join(task.ResolutionStrings(), "|")))
	updateCmd.String(&updateNote, "", "note", "One line on why the task was closed this way")
	updateCmd.Bool(&updateVerified, "", "verified", "Stamp verified_at: this task's text was just checked against reality")
	updateCmd.Bool(&updateJSON, "j", "json", "Output as JSON")
	var updateFields []string
	updateCmd.StringSlice(&updateFields, "", "field", "Free-form field to set, or key= to remove it (key=value; repeatable)")
	flaggy.AttachSubcommand(updateCmd, 1)

	// Delete subcommand
	deleteCmd := flaggy.NewSubcommand("delete")
	deleteCmd.Description = "Delete a task"
	var deleteIDStr string
	var deleteJSON bool
	var deleteForce bool
	deleteCmd.AddPositionalValue(&deleteIDStr, "id", 1, true, "Task ID")
	deleteCmd.Bool(&deleteJSON, "j", "json", "Output as JSON")
	deleteCmd.Bool(&deleteForce, "f", "force", "Force delete (also deletes subtasks)")
	flaggy.AttachSubcommand(deleteCmd, 1)

	// Start subcommand
	startCmd := flaggy.NewSubcommand("start")
	startCmd.Description = "Start a task (todo -> in_progress)"
	var startIDStr string
	var startJSON bool
	startCmd.AddPositionalValue(&startIDStr, "id", 1, true, "Task ID")
	startCmd.Bool(&startJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(startCmd, 1)

	// Complete subcommand
	completeCmd := flaggy.NewSubcommand("complete")
	completeCmd.Description = "Close a task (in_progress -> done, or any status when closed with a non-completed resolution)"
	var completeIDStr string
	var completeResolution, completeNote, completeMessage string
	var completeJSON bool
	completeCmd.AddPositionalValue(&completeIDStr, "id", 1, true, "Task ID")
	completeCmd.String(&completeResolution, "", "resolution", fmt.Sprintf("How the task left the backlog (%s)", strings.Join(task.ResolutionStrings(), "|")))
	completeCmd.String(&completeNote, "", "note", "One line on why")
	completeCmd.String(&completeMessage, "m", "message", "Squash commit message under git branching (default: title, description, Task trailer)")
	completeCmd.Bool(&completeJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(completeCmd, 1)

	// StartPhase subcommand
	startPhaseCmd := flaggy.NewSubcommand("start-phase")
	startPhaseCmd.Description = fmt.Sprintf("Start a delivery phase of a task (%s)", strings.Join(task.PhaseStrings(), "|"))
	var startPhaseIDStr, startPhaseName string
	var startPhaseJSON bool
	startPhaseCmd.AddPositionalValue(&startPhaseIDStr, "id", 1, true, "Task ID")
	startPhaseCmd.AddPositionalValue(&startPhaseName, "phase", 2, true, "Phase")
	startPhaseCmd.Bool(&startPhaseJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(startPhaseCmd, 1)

	// FinishPhase subcommand
	finishPhaseCmd := flaggy.NewSubcommand("finish-phase")
	finishPhaseCmd.Description = "Finish the open run of a task's phase"
	var finishPhaseIDStr, finishPhaseName, finishPhaseTokens, finishPhaseNote string
	var finishPhaseJSON bool
	finishPhaseCmd.AddPositionalValue(&finishPhaseIDStr, "id", 1, true, "Task ID")
	finishPhaseCmd.AddPositionalValue(&finishPhaseName, "phase", 2, true, "Phase")
	finishPhaseCmd.String(&finishPhaseTokens, "", "tokens", "Tokens the phase used (non-negative integer)")
	finishPhaseCmd.String(&finishPhaseNote, "", "note", "One-line note on the run")
	finishPhaseCmd.Bool(&finishPhaseJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(finishPhaseCmd, 1)

	// Archive subcommand
	archiveCmd := flaggy.NewSubcommand("archive")
	archiveCmd.Description = "Archive a completed task"
	var archiveIDStr string
	var archiveJSON bool
	archiveCmd.AddPositionalValue(&archiveIDStr, "id", 1, true, "Task ID")
	archiveCmd.Bool(&archiveJSON, "j", "json", "Output as JSON")
	flaggy.AttachSubcommand(archiveCmd, 1)

	// WriteTaskFile subcommand
	writeTaskFileCmd := flaggy.NewSubcommand("write-task-file")
	writeTaskFileCmd.Description = "Write or overwrite a file attached to a task"
	var writeTaskFileIDStr, writeTaskFileFilename, writeTaskFileContent string
	writeTaskFileCmd.AddPositionalValue(&writeTaskFileIDStr, "task-id", 1, true, "Task ID")
	writeTaskFileCmd.AddPositionalValue(&writeTaskFileFilename, "filename", 2, true, "Attached file name")
	writeTaskFileCmd.AddPositionalValue(&writeTaskFileContent, "content", 3, true, "File content")
	flaggy.AttachSubcommand(writeTaskFileCmd, 1)

	// ReadTaskFile subcommand
	readTaskFileCmd := flaggy.NewSubcommand("read-task-file")
	readTaskFileCmd.Description = "Read a file attached to a task"
	var readTaskFileIDStr, readTaskFileFilename string
	readTaskFileCmd.AddPositionalValue(&readTaskFileIDStr, "task-id", 1, true, "Task ID")
	readTaskFileCmd.AddPositionalValue(&readTaskFileFilename, "filename", 2, true, "Attached file name")
	flaggy.AttachSubcommand(readTaskFileCmd, 1)

	// ListTaskFiles subcommand
	listTaskFilesCmd := flaggy.NewSubcommand("list-task-files")
	listTaskFilesCmd.Description = "List files attached to a task"
	var listTaskFilesIDStr string
	listTaskFilesCmd.AddPositionalValue(&listTaskFilesIDStr, "task-id", 1, true, "Task ID")
	flaggy.AttachSubcommand(listTaskFilesCmd, 1)

	// Serve subcommand: `serve web`
	serveCmd := flaggy.NewSubcommand("serve")
	serveCmd.Description = "Run a server in the foreground"
	webCmd := flaggy.NewSubcommand("web")
	webCmd.Description = "Serve the read-only task dashboard"
	var serveAddr string
	var serveWithMCP bool
	webCmd.String(&serveAddr, "a", "addr", "Listen address (default from mcp-tasks.yaml, e.g. 127.0.0.1:7777)")
	webCmd.Bool(&serveWithMCP, "", "mcp", "Also serve MCP over stdio in this process")
	serveCmd.AttachSubcommand(webCmd, 1)
	flaggy.AttachSubcommand(serveCmd, 1)

	// Parse with custom args
	flaggy.ParseArgs(args[1:])

	// Handle subcommands
	// Checked before serveCmd: flaggy marks both the parent and the child.
	if webCmd.Used {
		return cmdServeWeb(ctx, stderr, serveAddr, serveWithMCP)
	}

	if versionCmd.Used {
		fmt.Fprintf(stdout, "mcp-task-manager %s\n", Version)
		// Print where this invocation would read tasks from: a silently
		// mis-resolved project is otherwise indistinguishable from an
		// empty backlog.
		if cfg, err := config.Load(); err == nil {
			fmt.Fprintf(stdout, "%s\n", cfg.Resolution.Explain())
		}
		return 0
	}

	if listCmd.Used {
		return cmdList(stdout, stderr, listJSON, listStatus, listPriority, listType, listParent, listArchived, listFields)
	}

	if getCmd.Used {
		return cmdGet(stdout, stderr, getJSON, getIDStr)
	}

	if nextCmd.Used {
		return cmdNext(stdout, stderr, nextJSON)
	}

	if createCmd.Used {
		return cmdCreate(stdout, stderr, createJSON, createTitle, createPriority, createType, createDesc, createParent, createID, createFields)
	}

	if updateCmd.Used {
		return cmdUpdate(stdout, stderr, updateJSON, updateIDStr, updateTitle, updateStatus, updatePriority, updateType, updateDesc, updateResolution, updateNote, updateVerified, updateFields)
	}

	if deleteCmd.Used {
		return cmdDelete(stdout, stderr, deleteJSON, deleteIDStr, deleteForce)
	}

	if startCmd.Used {
		return cmdStart(stdout, stderr, startJSON, startIDStr)
	}

	if startPhaseCmd.Used {
		return cmdStartPhase(stdout, stderr, startPhaseJSON, startPhaseIDStr, startPhaseName)
	}

	if finishPhaseCmd.Used {
		return cmdFinishPhase(stdout, stderr, finishPhaseJSON, finishPhaseIDStr, finishPhaseName, finishPhaseTokens, finishPhaseNote)
	}

	if completeCmd.Used {
		return cmdComplete(stdout, stderr, completeJSON, completeIDStr, completeResolution, completeNote, completeMessage)
	}

	if archiveCmd.Used {
		return cmdArchive(stdout, stderr, archiveJSON, archiveIDStr)
	}

	if writeTaskFileCmd.Used {
		return cmdWriteTaskFile(stdout, stderr, writeTaskFileIDStr, writeTaskFileFilename, writeTaskFileContent)
	}

	if readTaskFileCmd.Used {
		return cmdReadTaskFile(stdout, stderr, readTaskFileIDStr, readTaskFileFilename)
	}

	if listTaskFilesCmd.Used {
		return cmdListTaskFiles(stdout, stderr, listTaskFilesIDStr)
	}

	return 0
}
