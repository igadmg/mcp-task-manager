package task

import "time"

// Status represents the current state of a task
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// Priority represents task priority level
type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

// PriorityOrder returns numeric order for sorting (lower = higher priority)
func (p Priority) Order() int {
	switch p {
	case PriorityCritical:
		return 0
	case PriorityHigh:
		return 1
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 3
	default:
		return 99
	}
}

// Relation represents a link between tasks
type Relation struct {
	Type string `yaml:"type" json:"type"`
	Task string `yaml:"task" json:"task"`
}

// Task represents a single task
type Task struct {
	ID          string     `yaml:"id" json:"id"`
	ParentID    string     `yaml:"parent_id,omitempty" json:"parent_id,omitempty"` // "" = no parent
	Title       string     `yaml:"title" json:"title"`
	Description string     `yaml:"-" json:"description"` // Stored in markdown body
	Status      Status     `yaml:"status" json:"status"`
	Priority    Priority   `yaml:"priority" json:"priority"`
	Type        string     `yaml:"type" json:"type"`
	Relations   []Relation `yaml:"relations,omitempty" json:"relations,omitempty"`
	CreatedAt   time.Time  `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `yaml:"updated_at" json:"updated_at"`

	// Resolution is how the task left the active flow, set when it becomes
	// done and cleared if it is reopened. Empty on any task that is still
	// open, and on tasks closed before this field existed - a done task
	// without one reads as ResolutionCompleted.
	Resolution Resolution `yaml:"resolution,omitempty" json:"resolution,omitempty"`
	// ResolutionNote is the one-line why behind the resolution: which commit
	// landed the work, what replaced the task, why it stopped applying.
	ResolutionNote string `yaml:"resolution_note,omitempty" json:"resolution_note,omitempty"`
	// ClosedAt is when the task became done. UpdatedAt cannot stand in for
	// it: editing a closed task's text moves UpdatedAt and would otherwise
	// look like it was closed again.
	ClosedAt *time.Time `yaml:"closed_at,omitempty" json:"closed_at,omitempty"`
	// VerifiedAt is when a human or an agent last checked this task's text
	// against reality. It says nothing about whether the task is done - an
	// open task whose description still names renamed symbols is stale in a
	// way status cannot express. Nil means never checked since it was filed.
	VerifiedAt *time.Time `yaml:"verified_at,omitempty" json:"verified_at,omitempty"`
}

// Closed reports whether the task has left the active flow.
func (t *Task) Closed() bool {
	return t.Status == StatusDone
}

// EffectiveResolution is the resolution to display or filter on, resolving a
// closed task that predates the field to ResolutionCompleted and leaving an
// open task's resolution empty.
func (t *Task) EffectiveResolution() Resolution {
	if !t.Closed() {
		return ""
	}
	if t.Resolution == "" {
		return ResolutionCompleted
	}
	return t.Resolution
}

// IsValidStatus checks if status is valid
func IsValidStatus(s string) bool {
	switch Status(s) {
	case StatusTodo, StatusInProgress, StatusDone:
		return true
	}
	return false
}

// IsValidPriority checks if priority is valid
func IsValidPriority(p string) bool {
	switch Priority(p) {
	case PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

// Resolution records how a task left the active flow. Status says a task is
// terminal, Resolution says what actually happened: a task closed because the
// work landed and a task closed because the work stopped being needed are both
// StatusDone, and without this field the difference can only be written into
// the description in prose.
//
// The target of a supersede or a duplicate is not stored here: that is an edge
// between tasks, expressed with the existing relations (superseded_by,
// duplicate_of), so that it cannot drift from a copy kept in this field.
type Resolution string

const (
	// ResolutionCompleted - the work was done. The default when a task is
	// closed without naming a resolution.
	ResolutionCompleted Resolution = "completed"
	// ResolutionObsolete - the task no longer applies; the code, the plan or
	// the surrounding decisions moved out from under it.
	ResolutionObsolete Resolution = "obsolete"
	// ResolutionSuperseded - the need is still real but another task covers
	// it now. Pair with a superseded_by relation.
	ResolutionSuperseded Resolution = "superseded"
	// ResolutionDuplicate - the same work is already tracked elsewhere.
	// Pair with a duplicate_of relation.
	ResolutionDuplicate Resolution = "duplicate"
	// ResolutionWontfix - the task applies and is understood, and we have
	// decided not to do it.
	ResolutionWontfix Resolution = "wontfix"
)

// Resolutions lists every valid resolution, in the order tools present them.
func Resolutions() []Resolution {
	return []Resolution{
		ResolutionCompleted,
		ResolutionObsolete,
		ResolutionSuperseded,
		ResolutionDuplicate,
		ResolutionWontfix,
	}
}

// IsValidResolution checks if resolution is valid
func IsValidResolution(r string) bool {
	for _, valid := range Resolutions() {
		if Resolution(r) == valid {
			return true
		}
	}
	return false
}

// Delivered reports whether the resolution means the work actually landed.
// Everything else is a task that closed without its work being done, which is
// what backlog reviews and the archive rules key off.
func (r Resolution) Delivered() bool {
	return r == "" || r == ResolutionCompleted
}

// ResolutionStrings renders the valid resolutions for enums and error text.
func ResolutionStrings() []string {
	out := make([]string, 0, len(Resolutions()))
	for _, r := range Resolutions() {
		out = append(out, string(r))
	}
	return out
}
