package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// TaskDetailOptions holds optional display information for FormatTaskDetail
type TaskDetailOptions struct {
	Subtasks []*task.Task
	Blocked  bool
	Blockers []task.BlockingInfo
	// Phases is the task's phase-run history, in workflow order.
	Phases []task.PhaseRecord
}

// FormatTaskDetail formats a single task for human-readable output
func FormatTaskDetail(t *task.Task, opts *TaskDetailOptions) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task #%s\n", t.ID))
	sb.WriteString(fmt.Sprintf("Title:       %s\n", t.Title))
	status := string(t.Status)
	if opts != nil && opts.Blocked {
		status += " [BLOCKED]"
	}
	if res := t.EffectiveResolution(); res != "" {
		status += " (" + string(res) + ")"
	}
	sb.WriteString(fmt.Sprintf("Status:      %s\n", status))
	sb.WriteString(fmt.Sprintf("Priority:    %s\n", t.Priority))
	sb.WriteString(fmt.Sprintf("Type:        %s\n", t.Type))
	if t.ParentID != "" {
		sb.WriteString(fmt.Sprintf("Parent:      #%s\n", t.ParentID))
	}
	sb.WriteString(fmt.Sprintf("Created:     %s\n", t.CreatedAt.Format("2006-01-02 15:04:05")))
	if t.CreatedBy != "" {
		sb.WriteString(fmt.Sprintf("Created by:  %s\n", t.CreatedBy))
	}
	sb.WriteString(fmt.Sprintf("Updated:     %s\n", t.UpdatedAt.Format("2006-01-02 15:04:05")))
	if t.ClosedAt != nil {
		sb.WriteString(fmt.Sprintf("Closed:      %s\n", t.ClosedAt.Format("2006-01-02 15:04:05")))
	}
	if t.VerifiedAt != nil {
		sb.WriteString(fmt.Sprintf("Verified:    %s\n", t.VerifiedAt.Format("2006-01-02 15:04:05")))
	}
	if t.ResolutionNote != "" {
		sb.WriteString(fmt.Sprintf("Why:         %s\n", t.ResolutionNote))
	}
	for _, line := range []struct{ label, value string }{
		{"Branch:      ", t.Branch},
		{"Base branch: ", t.BaseBranch},
		{"Start commit:", t.StartCommit},
		{"Final branch:", t.FinalBranch},
	} {
		if line.value != "" {
			sb.WriteString(fmt.Sprintf("%s %s\n", line.label, line.value))
		}
	}
	if opts != nil && len(opts.Phases) > 0 {
		sb.WriteString("\nPhases:\n")
		for _, rec := range opts.Phases {
			for i, run := range rec.Runs {
				sb.WriteString(formatPhaseRun(rec.Phase, i+1, run))
			}
		}
	}
	if len(t.Relations) > 0 {
		sb.WriteString("\nRelations:\n")
		for _, rel := range t.Relations {
			sb.WriteString(fmt.Sprintf("  %s -> #%s\n", rel.Type, rel.Task))
		}
	}
	if opts != nil && opts.Blocked && len(opts.Blockers) > 0 {
		sb.WriteString("\nBlocked by:\n")
		for _, b := range opts.Blockers {
			sb.WriteString(fmt.Sprintf("  #%s [%s] %s\n", b.TaskID, b.Status, b.Title))
		}
	}
	if t.Description != "" {
		sb.WriteString(fmt.Sprintf("\nDescription:\n%s\n", t.Description))
	}
	if opts != nil && len(opts.Subtasks) > 0 {
		sb.WriteString(fmt.Sprintf("\nSubtasks (%d):\n", len(opts.Subtasks)))
		for _, sub := range opts.Subtasks {
			sb.WriteString(fmt.Sprintf("  #%s [%s] %s\n", sub.ID, sub.Status, sub.Title))
		}
	}
	return sb.String()
}

// formatPhaseRun renders one phase run as a detail line:
// "  design #2  started 2026-10-04 14:02 by igor.cwer  finished 14:30 by igor.cwer  40210 tokens".
// The finish shows only the clock when it is on the start's day.
func formatPhaseRun(p task.Phase, n int, run task.PhaseRun) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %s #%d  started %s", p, n, task.RunStamp(run.StartedAt, run.StartedBy)))
	if run.Open() {
		sb.WriteString("  open")
	} else {
		// On the start's day the finish keeps only its clock.
		finished := task.RunStamp(*run.FinishedAt, run.FinishedBy)
		if day := "2006-01-02 "; run.FinishedAt.Format(day) == run.StartedAt.Format(day) {
			finished = strings.TrimPrefix(finished, run.FinishedAt.Format(day))
		}
		sb.WriteString("  finished " + finished)
	}
	if run.Tokens != nil {
		sb.WriteString(fmt.Sprintf("  %d tokens", *run.Tokens))
	}
	if run.Note != "" {
		sb.WriteString("  (" + run.Note + ")")
	}
	sb.WriteString("\n")
	return sb.String()
}

// SubtaskCounts holds the count of subtasks for a parent task
type SubtaskCounts struct {
	Total int
	Done  int
}

// FormatTaskTable formats a list of tasks as a table
// subtaskCounts is a map of task ID to subtask counts (can be nil)
// blockedTasks is a set of task IDs that are blocked (can be nil)
func FormatTaskTable(tasks []*task.Task, subtaskCounts map[string]SubtaskCounts, blockedTasks map[string]bool) string {
	if len(tasks) == 0 {
		return "No tasks found."
	}

	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tTitle\tStatus\tPriority\tType\tSubtasks")
	for _, t := range tasks {
		title := t.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		statusStr := string(t.Status)
		// A closed task shows how it closed, not just that it did: a table
		// of a dozen "done" rows hides which of them were ever built.
		if res := t.EffectiveResolution(); res != "" && !res.Delivered() {
			statusStr += "/" + string(res)
		}
		if blockedTasks != nil && blockedTasks[t.ID] {
			statusStr += " [BLOCKED]"
		}
		// Show subtask count if this task has subtasks
		subtaskStr := ""
		if subtaskCounts != nil {
			if counts, ok := subtaskCounts[t.ID]; ok && counts.Total > 0 {
				subtaskStr = fmt.Sprintf("[%d/%d]", counts.Done, counts.Total)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, title, statusStr, t.Priority, t.Type, subtaskStr)
	}
	w.Flush()
	return sb.String()
}

// FormatMessage formats a simple message
func FormatMessage(msg string, id string) string {
	return msg
}

// FormatJSON writes a value as JSON to the writer
func FormatJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// FormatJSONMessage writes a message with ID as JSON
func FormatJSONMessage(w io.Writer, msg string, id string) error {
	return FormatJSON(w, map[string]any{
		"message": msg,
		"id":      id,
	})
}
