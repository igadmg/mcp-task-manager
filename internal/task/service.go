package task

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// ErrNoProjectFound is returned when read operations are attempted without an existing project
var ErrNoProjectFound = errors.New("no tasks directory found. Create a task to initialize one here, or set MCP_TASKS_DIR")

// Storage interface for task persistence
type Storage interface {
	Save(t *Task) error
	Load(id string) (*Task, error)
	Delete(id string) error
	EnsureDir() error
	// MigrateFlatLayout migrates any task still stored in the legacy flat
	// file layout into the current per-task directory layout. Called once
	// by Service.Initialize(), before the index loads.
	MigrateFlatLayout() error
	// ValidateID checks a task id is safe to use as a directory/file name
	// and does not collide with a name this package's layout already
	// reserves. It does not check uniqueness against existing tasks - see
	// Exists / ArchiveStorage.IsArchived for that (requires I/O, format
	// doesn't).
	ValidateID(id string) error
	// Exists reports whether an active task directory with this id exists.
	Exists(id string) bool
}

// RelationEdge represents a directed relation between two tasks in the index
type RelationEdge struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// Index interface for task indexing
//
// The index is in-memory only: it is built from the per-task files and has no
// storage of its own, so mutations need no separate persistence step.
//
// Performance note: Methods returning []*Task use lazy loading:
//   - Get() loads the full task with description from disk
//   - Filter(), All(), GetSubtasks(), NextTodo() return tasks without descriptions (from in-memory index)
//
// This design keeps list operations fast while still providing full task data on demand.
type Index interface {
	Load() error
	Get(id string) (*Task, bool) // Loads full task with description from disk
	Set(t *Task)
	Delete(id string)
	All() []*Task                                                                                                  // Returns tasks without descriptions (from index)
	Filter(status *Status, priority *Priority, taskType *string, parentID *string, resolution *Resolution) []*Task // Returns tasks without descriptions
	NextTodo() *Task                                                                                               // Returns task without description (from index)
	NextID() string
	// Subtask methods
	GetSubtasks(parentID string) []*Task // Returns tasks without descriptions (from index)
	HasSubtasks(taskID string) bool
	SubtaskCounts(parentID string) (total int, done int)
	// Relation methods
	AddRelation(edge RelationEdge)
	RemoveRelation(edge RelationEdge)
	GetRelationsForTask(taskID string) []RelationEdge
	GetBlockers(taskID string) []string
	// AllBlockers returns the blocked_by targets of every task that has
	// any, keyed by the blocked task's id. Board-wide blocked lookups go
	// through it so a whole render costs one index pass, not one per card.
	AllBlockers() map[string][]string
	RemoveAllRelationsForTask(taskID string) []RelationEdge
}

// ArchiveStorage extends Storage with archive-specific operations
type ArchiveStorage interface {
	Archive(id string) error
	LoadArchived(id string) (*Task, error)
	LoadAllArchived() ([]*Task, error)
	IsArchived(id string) bool
}

// FileStorage manages free-form named text files attached to a task,
// stored alongside the task's own {id}.md inside its per-task directory.
// Because attached files share that directory, Archive/Delete already
// move or remove them as a side effect of moving/removing the directory -
// no separate cascade logic is needed here.
type FileStorage interface {
	WriteFile(taskID string, filename, content string) error
	ReadFile(taskID string, filename string) (string, error)
	ListFiles(taskID string) ([]string, error)
}

// Service provides task management operations
type Service struct {
	storage        Storage
	archiveStorage ArchiveStorage
	fileStorage    FileStorage
	index          Index
	validTypes     []string
	config         *config.Config

	// git drives the branch-per-task workflow; nil means branching is off.
	// identity names the user branches and the current-task pointer
	// belong to, and current keeps that pointer (nil: none is kept). All
	// three are write-once, set by ServiceOptions in NewService.
	git      GitRepo
	identity Identity
	current  CurrentTaskStore

	// mu serializes every task operation. It is the only lock over the
	// index and the markdown storage, both of which are reachable solely
	// through this type.
	//
	// DISCIPLINE: exported methods lock once on entry and delegate to an
	// unexported, unlocked twin. Service methods NEVER call each other's
	// exported forms - Go mutexes are not reentrant. When adding a method,
	// add it in this shape or TestServiceNoSelfDeadlock will fail.
	mu sync.Mutex
}

// NewService creates a new task service
func NewService(storage Storage, archiveStorage ArchiveStorage, fileStorage FileStorage, index Index, validTypes []string, cfg *config.Config, opts ...ServiceOption) *Service {
	s := &Service{
		storage:        storage,
		archiveStorage: archiveStorage,
		fileStorage:    fileStorage,
		index:          index,
		validTypes:     validTypes,
		config:         cfg,
	}
	for _, apply := range opts {
		apply(s)
	}
	return s
}

// BranchingEnabled reports whether start_task and complete_task drive git
// branches for this project.
//
// Deliberately unlocked: s.git is write-once, assigned in NewService.
func (s *Service) BranchingEnabled() bool {
	return s.git != nil
}

// EnsureProjectExists checks that a project was found during config loading.
// Should be called before read operations.
//
// Deliberately unlocked: it reads only s.config, which is write-once
// (assigned in NewService and never again). Same for ProjectFound, Config and
// BranchingEnabled (s.git).
func (s *Service) EnsureProjectExists() error {
	if s.config == nil || !s.config.ProjectFound {
		// Name the directory that was looked at: an unresolved project
		// otherwise looks exactly like an empty backlog.
		if s.config != nil && s.config.Resolution != nil {
			return fmt.Errorf("%w (%s)", ErrNoProjectFound, s.config.Resolution.Explain())
		}
		return ErrNoProjectFound
	}
	return nil
}

// ProjectFound returns whether an existing project was found
func (s *Service) ProjectFound() bool {
	return s.config != nil && s.config.ProjectFound
}

// Initialize loads the index if directory exists (does not create directory)
// Initialize loads the index if directory exists (does not create directory)
func (s *Service) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Migrate any legacy flat-layout tasks before the index scans the
	// directory, so LoadAll/Rebuild only ever sees the current layout.
	if err := s.storage.MigrateFlatLayout(); err != nil {
		return err
	}
	// Only load index, don't create directory - that happens on first write
	if err := s.index.Load(); err != nil {
		return err
	}
	// Run auto-archive on startup if enabled
	if s.config != nil && s.config.AutoArchive.Enabled {
		if err := s.runAutoArchive(); err != nil {
			// Log but don't fail startup
			log.Printf("auto-archive on startup failed: %v", err)
		}
	}
	return nil
}

// Create creates a new task (optionally as a subtask). If id is non-empty,
// it is validated and used verbatim as the task's id and directory name,
// bypassing (and not advancing) the numeric auto-increment counter. If id
// is empty, the next auto-increment id is allocated as before.
func (s *Service) Create(title, description string, priority Priority, taskType string, parentID string, id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.create(title, description, priority, taskType, parentID, id)
}

func (s *Service) create(title, description string, priority Priority, taskType string, parentID string, id string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if !IsValidPriority(string(priority)) {
		return nil, fmt.Errorf("invalid priority: %s", priority)
	}
	if !s.isValidType(taskType) {
		return nil, fmt.Errorf("invalid task type: %s", taskType)
	}

	// Validate parent if provided
	if parentID != "" {
		parent, ok := s.index.Get(parentID)
		if !ok {
			return nil, fmt.Errorf("parent task not found: %s", parentID)
		}
		if parent.ParentID != "" {
			return nil, fmt.Errorf("cannot create subtask under a subtask (single level only)")
		}
	}

	// Ensure directory exists for write operation
	if err := s.storage.EnsureDir(); err != nil {
		return nil, err
	}

	var taskID string
	if id != "" {
		if err := s.storage.ValidateID(id); err != nil {
			return nil, err
		}
		if s.storage.Exists(id) {
			return nil, fmt.Errorf("task id already exists: %s", id)
		}
		if s.archiveStorage != nil && s.archiveStorage.IsArchived(id) {
			return nil, fmt.Errorf("task id already exists in archive: %s", id)
		}
		taskID = id
	} else {
		taskID = s.index.NextID()
	}

	now := time.Now().UTC()
	t := &Task{
		ID:          taskID,
		ParentID:    parentID,
		Title:       title,
		Description: description,
		Status:      StatusTodo,
		Priority:    priority,
		Type:        taskType,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.storage.Save(t); err != nil {
		return nil, err
	}

	s.index.Set(t)

	return t, nil
}

// CreateSubtask creates a subtask under a parent
func (s *Service) CreateSubtask(title, description string, priority Priority, taskType string, parentID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.create(title, description, priority, taskType, parentID, "")
}

// Get returns a task by ID with full description loaded from disk.
// Falls back to the archive if the task is not found in the active index.
func (s *Service) Get(id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *Service) get(id string) (*Task, error) {
	t, ok := s.index.Get(id)
	if ok {
		return t, nil
	}
	// Fall back to archive
	if s.archiveStorage != nil {
		if archived, err := s.archiveStorage.LoadArchived(id); err == nil {
			return archived, nil
		}
	}
	return nil, fmt.Errorf("task not found: %s", id)
}

// GetWithSubtasks returns a task and its subtasks in one call
func (s *Service) GetWithSubtasks(id string) (*Task, []*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getWithSubtasks(id)
}

func (s *Service) getWithSubtasks(id string) (*Task, []*Task, error) {
	t, err := s.get(id)
	if err != nil {
		return nil, nil, err
	}
	subtasks := s.index.GetSubtasks(id)
	return t, subtasks, nil
}

// WriteTaskFile creates or overwrites a named file attached to the given
// task. Only active tasks accept writes - archived tasks are read-only.
func (s *Service) WriteTaskFile(taskID string, filename, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.index.Get(taskID); !ok {
		if s.archiveStorage != nil && s.archiveStorage.IsArchived(taskID) {
			return fmt.Errorf("task %s is archived; files are read-only", taskID)
		}
		return fmt.Errorf("task not found: %s", taskID)
	}
	return s.fileStorage.WriteFile(taskID, filename, content)
}

// ReadTaskFile returns the content of a named file attached to the given
// task. Works for both active and archived tasks.
func (s *Service) ReadTaskFile(taskID string, filename string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.get(taskID); err != nil {
		return "", err
	}
	return s.fileStorage.ReadFile(taskID, filename)
}

// ListTaskFiles returns the names of all files attached to the given task.
// Works for both active and archived tasks.
func (s *Service) ListTaskFiles(taskID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.get(taskID); err != nil {
		return nil, err
	}
	return s.fileStorage.ListFiles(taskID)
}

// GetSubtaskCounts returns the count of subtasks for a task
func (s *Service) GetSubtaskCounts(taskID string) (total, done int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subtaskCounts(taskID)
}

func (s *Service) subtaskCounts(taskID string) (total, done int) {
	return s.index.SubtaskCounts(taskID)
}

// UpdateOption carries the fields that were added after Update's signature
// settled, so the original callers keep compiling unchanged.
type UpdateOption func(*updateOpts)

type updateOpts struct {
	resolution    *Resolution
	note          *string
	verified      *bool
	branch        *branchInfo
	commitMessage *string
}

// WithResolution closes the task with the given resolution. Naming one implies
// closing: the task is moved to done even if the caller did not say so.
func WithResolution(r Resolution) UpdateOption {
	return func(o *updateOpts) { o.resolution = &r }
}

// WithResolutionNote sets the one-line why behind the resolution. Valid only
// on a task that is closed, or being closed by the same call.
func WithResolutionNote(note string) UpdateOption {
	return func(o *updateOpts) { o.note = &note }
}

// WithVerified stamps (true) or clears (false) VerifiedAt - the moment the
// task's own text was last checked against reality.
func WithVerified(v bool) UpdateOption {
	return func(o *updateOpts) { o.verified = &v }
}

// WithCommitMessage sets the message of the squash commit a completion
// makes under git branching. Update ignores it; CompleteTask reads it.
func WithCommitMessage(msg string) UpdateOption {
	return func(o *updateOpts) { o.commitMessage = &msg }
}

func buildUpdateOpts(opts []UpdateOption) updateOpts {
	var o updateOpts
	for _, apply := range opts {
		apply(&o)
	}
	return o
}

// Update modifies a task
func (s *Service) Update(id string, title, description *string, status *Status, priority *Priority, taskType *string, opts ...UpdateOption) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(id, title, description, status, priority, taskType, opts...)
}

func (s *Service) update(id string, title, description *string, status *Status, priority *Priority, taskType *string, opts ...UpdateOption) (*Task, error) {
	t, err := s.get(id)
	if err != nil {
		return nil, err
	}

	o := buildUpdateOpts(opts)
	if o.resolution != nil {
		if !IsValidResolution(string(*o.resolution)) {
			return nil, fmt.Errorf("invalid resolution: %s (want one of %s)", *o.resolution, strings.Join(ResolutionStrings(), ", "))
		}
		// A resolution only describes a closed task, so it closes the task
		// unless the caller is explicitly moving it somewhere else.
		if status == nil {
			status = &[]Status{StatusDone}[0]
		} else if *status != StatusDone {
			return nil, fmt.Errorf("cannot set resolution %s while moving task %s to %s; a resolution belongs to a done task", *o.resolution, id, *status)
		}
	}

	if title != nil {
		if *title == "" {
			return nil, fmt.Errorf("title cannot be empty")
		}
		t.Title = *title
	}
	if description != nil {
		t.Description = *description
	}
	if status != nil {
		if !IsValidStatus(string(*status)) {
			return nil, fmt.Errorf("invalid status: %s", *status)
		}
		t.Status = *status
	}
	if priority != nil {
		if !IsValidPriority(string(*priority)) {
			return nil, fmt.Errorf("invalid priority: %s", *priority)
		}
		t.Priority = *priority
	}
	if taskType != nil {
		if !s.isValidType(*taskType) {
			return nil, fmt.Errorf("invalid task type: %s", *taskType)
		}
		t.Type = *taskType
	}

	now := time.Now().UTC()

	switch {
	case t.Closed():
		// Closing, or editing an already closed task. An explicit resolution
		// wins; otherwise a task closed without one reads as completed.
		if o.resolution != nil {
			t.Resolution = *o.resolution
		} else if t.Resolution == "" {
			t.Resolution = ResolutionCompleted
		}
		if o.note != nil {
			t.ResolutionNote = *o.note
		}
		if t.ClosedAt == nil {
			closed := now
			t.ClosedAt = &closed
		}
	default:
		// Reopened, or never closed: nothing here may survive, or a stale
		// resolution would outlive the closure it described.
		if o.note != nil && o.resolution == nil {
			return nil, fmt.Errorf("cannot set a resolution note on task %s: it is %s, not done", id, t.Status)
		}
		t.Resolution = ""
		t.ResolutionNote = ""
		t.ClosedAt = nil
	}

	// Branch fields are not touched by the status logic above: they survive
	// a reopen, so a restarted task finds its branch again.
	if o.branch != nil {
		o.branch.apply(t)
	}

	if o.verified != nil {
		if *o.verified {
			verified := now
			t.VerifiedAt = &verified
		} else {
			t.VerifiedAt = nil
		}
	}

	t.UpdatedAt = now

	if err := s.storage.Save(t); err != nil {
		return nil, err
	}

	s.index.Set(t)

	return t, nil
}

// Delete removes a task
func (s *Service) Delete(id string, deleteSubtasks bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.get(id)
	if err != nil {
		return err
	}

	// Check for subtasks
	if s.index.HasSubtasks(id) {
		if !deleteSubtasks {
			total, _ := s.index.SubtaskCounts(id)
			return fmt.Errorf("cannot delete task %s: has %d subtask(s). Use --force to delete this tasks and its subtasks", id, total)
		}

		// Delete all subtasks first
		subtasks := s.index.GetSubtasks(id)
		for _, sub := range subtasks {
			if err := s.storage.Delete(sub.ID); err != nil {
				return fmt.Errorf("failed to delete subtask %s: %w", sub.ID, err)
			}
			s.index.Delete(sub.ID)
		}
	}

	// Remove all relations referencing this task
	removedEdges := s.index.RemoveAllRelationsForTask(id)

	// Update other tasks' frontmatter to remove relations pointing to this task
	affectedTasks := make(map[string]bool)
	for _, edge := range removedEdges {
		// Only update frontmatter for edges where the other task is the source
		// (relations are stored in the source task's frontmatter)
		if edge.Source != id {
			affectedTasks[edge.Source] = true
		}
	}
	for affectedID := range affectedTasks {
		affected, err := s.get(affectedID)
		if err != nil {
			continue
		}
		var newRelations []Relation
		for _, rel := range affected.Relations {
			if rel.Task != id {
				newRelations = append(newRelations, rel)
			}
		}
		affected.Relations = newRelations
		affected.UpdatedAt = time.Now().UTC()
		if err := s.storage.Save(affected); err != nil {
			return fmt.Errorf("failed to update relations in task %s: %w", affectedID, err)
		}
		s.index.Set(affected)
	}

	if err := s.storage.Delete(t.ID); err != nil {
		return err
	}

	s.index.Delete(id)
	return nil
}

// List returns all tasks, optionally filtered
// Note: Tasks returned do not include descriptions for performance (use Get for full task data)
func (s *Service) List(status *Status, priority *Priority, taskType *string, parentID *string, resolution *Resolution) []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(status, priority, taskType, parentID, resolution)
}

func (s *Service) list(status *Status, priority *Priority, taskType *string, parentID *string, resolution *Resolution) []*Task {
	return s.index.Filter(status, priority, taskType, parentID, resolution)
}

// GetNextTask returns the highest priority todo task
// Note: Task returned does not include description (use Get to load full task data)
func (s *Service) GetNextTask() *Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index.NextTodo()
}

// StartTask moves a task from todo to in_progress
func (s *Service) StartTask(id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startTask(id)
}

func (s *Service) startTask(id string) (*Task, error) {
	t, err := s.get(id)
	if err != nil {
		return nil, err
	}

	// A task whose wip branch still exists is restarted on it.
	if s.git != nil && t.Branch != "" && (t.Status == StatusTodo || t.Status == StatusInProgress) {
		if _, ok, err := s.git.BranchSHA(t.Branch); err != nil {
			return nil, err
		} else if ok {
			return nil, fmt.Errorf("task %s: %w", id, errRestartNotImplemented)
		}
	}

	if t.Status != StatusTodo {
		return nil, fmt.Errorf("task %s is not in todo status (current: %s)", id, t.Status)
	}

	// Check if task is blocked
	if blocked, blockers := s.isBlocked(id); blocked {
		var parts []string
		for _, b := range blockers {
			parts = append(parts, fmt.Sprintf("%s (%s)", b.TaskID, b.Status))
		}
		return nil, fmt.Errorf("task %s is blocked by tasks: %s", id, strings.Join(parts, ", "))
	}

	if s.git != nil {
		return s.startBranched(t)
	}
	return s.startPlain(t)
}

// startPlain starts t without git: the records and the pointer.
func (s *Service) startPlain(t *Task) (*Task, error) {
	var txn gitTxn
	return runFlow(&txn, func() (*Task, error) {
		started, err := s.startTaskRecords(&txn, t)
		if err != nil {
			return nil, err
		}
		return started, s.setPointer(&txn, t.ID)
	})
}

// startTaskRecords writes the status changes of a start: the task, and its
// parent when that is still todo. Every record is journaled before it is
// written.
func (s *Service) startTaskRecords(txn *gitTxn, t *Task) (*Task, error) {
	// Auto-start parent if this is a subtask
	if t.ParentID != "" {
		parent, err := s.get(t.ParentID)
		if err != nil {
			return nil, fmt.Errorf("parent task not found: %s", t.ParentID)
		}
		if parent.Status == StatusTodo {
			if err := s.captureTask(txn, t.ParentID); err != nil {
				return nil, err
			}
			status := StatusInProgress
			if _, err := s.update(t.ParentID, nil, nil, &status, nil, nil); err != nil {
				return nil, fmt.Errorf("failed to start parent task: %w", err)
			}
		}
	}

	if err := s.captureTask(txn, t.ID); err != nil {
		return nil, err
	}
	status := StatusInProgress
	return s.update(t.ID, nil, nil, &status, nil, nil)
}

// withRollback reports a failed flow together with anything its rollback
// could not undo. Nothing is ever lost at that point: every commit a flow
// made stays reachable through its branch or the reflog.
func withRollback(err, rollbackErr error) error {
	if rollbackErr == nil {
		return err
	}
	return fmt.Errorf("%w; %w; no content was lost: every commit stays reachable through its branch or the reflog", err, rollbackErr)
}

// CompleteTask closes a task. Without options it is the original
// in_progress -> done completion; with a resolution other than completed it
// is the other way a task leaves the backlog - the work is not going to
// happen - and the rules relax accordingly:
//
//   - the task may be closed straight from todo, because a task that stopped
//     applying is usually one nobody ever started;
//   - open subtasks are closed with the same resolution instead of blocking
//     the parent, since a branch that no longer applies does not apply
//     subtask by subtask either. Completion keeps refusing, unchanged.
func (s *Service) CompleteTask(id string, opts ...UpdateOption) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completeTask(id, opts...)
}

func (s *Service) completeTask(id string, opts ...UpdateOption) (*Task, error) {
	t, err := s.get(id)
	if err != nil {
		return nil, err
	}

	o := buildUpdateOpts(opts)
	resolution := ResolutionCompleted
	if o.resolution != nil {
		resolution = *o.resolution
	}

	if t.Closed() {
		return nil, fmt.Errorf("task %s is already done (resolution: %s)", id, t.EffectiveResolution())
	}
	if resolution.Delivered() && t.Status != StatusInProgress {
		return nil, fmt.Errorf("task %s is not in progress (current: %s)", id, t.Status)
	}

	// Check if this task has incomplete subtasks
	subtasks := s.index.GetSubtasks(id)
	incompleteCount := 0
	for _, sub := range subtasks {
		if sub.Status != StatusDone {
			incompleteCount++
		}
	}
	if incompleteCount > 0 && resolution.Delivered() {
		return nil, fmt.Errorf("cannot complete task %s: has %d incomplete subtask(s)", id, incompleteCount)
	}

	if s.git != nil && t.Branch != "" {
		if done, handled, err := s.completeBranched(t, subtasks, resolution, o, opts); handled {
			return done, err
		}
	}

	var txn gitTxn
	completed, closed, openParent, err := s.completeTaskRecords(&txn, t, subtasks, resolution, opts)
	if err == nil {
		err = s.clearPointerIf(&txn, openParent, closed...)
	}
	if err != nil {
		return nil, withRollback(err, txn.rollback())
	}
	return completed, nil
}

// completeTaskRecords writes the records of a completion: the open
// subtasks a non-delivered close cascades to, the task, and the parent a
// last subtask auto-completes. It returns every task it closed, and the
// parent when that is still open afterwards. Every record is journaled
// before it is written.
func (s *Service) completeTaskRecords(txn *gitTxn, t *Task, subtasks []*Task, resolution Resolution, opts []UpdateOption) (completed *Task, closed []string, openParent string, err error) {
	for _, sub := range subtasks {
		if sub.Status == StatusDone {
			continue
		}
		if err := s.captureTask(txn, sub.ID); err != nil {
			return nil, nil, "", err
		}
		closed = append(closed, sub.ID)
	}
	if err := s.closeSubtasksWith(t.ID, subtasks, resolution); err != nil {
		return nil, nil, "", err
	}

	// Complete this task
	if err := s.captureTask(txn, t.ID); err != nil {
		return nil, nil, "", err
	}
	status := StatusDone
	completed, err = s.update(t.ID, nil, nil, &status, nil, nil, opts...)
	if err != nil {
		return nil, nil, "", err
	}
	closed = append(closed, t.ID)

	if t.ParentID == "" {
		return completed, closed, "", nil
	}

	// If this is a subtask, check if all siblings are done -> auto-complete parent
	siblings := s.index.GetSubtasks(t.ParentID)
	allDone := true
	for _, sib := range siblings {
		if sib.Status != StatusDone {
			allDone = false
			break
		}
	}
	if !allDone {
		return completed, closed, t.ParentID, nil
	}
	// A parent with a branch is delivered by its own completion, which
	// squashes its wip branch into its final one (design 5.6): finishing
	// the last subtask must not close it behind git's back.
	parent, err := s.get(t.ParentID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parent task not found: %s", t.ParentID)
	}
	if parent.Branch != "" {
		return completed, closed, t.ParentID, nil
	}
	// The parent closes as completed even when the last subtask was
	// closed as obsolete: from the parent's side every child has
	// been dealt with. Give the parent its own resolution
	// explicitly when that reading is wrong.
	if err := s.captureTask(txn, t.ParentID); err != nil {
		return nil, nil, "", err
	}
	if _, err := s.update(t.ParentID, nil, nil, &status, nil, nil); err != nil {
		return nil, nil, "", fmt.Errorf("failed to auto-complete parent: %w", err)
	}
	return completed, append(closed, t.ParentID), "", nil
}

// closeSubtasksWith closes every still-open subtask with the parent's
// resolution, recording which parent pulled them along.
func (s *Service) closeSubtasksWith(parentID string, subtasks []*Task, resolution Resolution) error {
	note := fmt.Sprintf("closed as %s together with parent %s", resolution, parentID)
	for _, sub := range subtasks {
		if sub.Status == StatusDone {
			continue
		}
		status := StatusDone
		if _, err := s.update(sub.ID, nil, nil, &status, nil, nil, WithResolution(resolution), WithResolutionNote(note)); err != nil {
			return fmt.Errorf("failed to close subtask %s as %s: %w", sub.ID, resolution, err)
		}
	}
	return nil
}

// BlockingInfo describes a task that is blocking another
type BlockingInfo struct {
	TaskID string `json:"task_id"`
	Status Status `json:"status"`
	Title  string `json:"title"`
}

// AddRelation adds a relation between two tasks
func (s *Service) AddRelation(source string, relationType string, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate no self-reference
	if source == target {
		return fmt.Errorf("cannot create relation: source and target are the same task (%s)", source)
	}

	// Validate relation type
	if s.config != nil && !s.config.IsValidRelationType(relationType) {
		return fmt.Errorf("invalid relation type: %s", relationType)
	}

	// Validate source task exists
	srcTask, err := s.get(source)
	if err != nil {
		return fmt.Errorf("source task not found: %s", source)
	}

	// Validate target task exists
	if _, err := s.get(target); err != nil {
		return fmt.Errorf("target task not found: %s", target)
	}

	// Check for duplicate
	for _, rel := range srcTask.Relations {
		if rel.Type == relationType && rel.Task == target {
			return fmt.Errorf("relation already exists: %s from %s to %s", relationType, source, target)
		}
	}

	// Ensure directory exists for write operation
	if err := s.storage.EnsureDir(); err != nil {
		return err
	}

	// Add relation to source task's frontmatter
	srcTask.Relations = append(srcTask.Relations, Relation{Type: relationType, Task: target})
	srcTask.UpdatedAt = time.Now().UTC()

	if err := s.storage.Save(srcTask); err != nil {
		return err
	}

	s.index.Set(srcTask)

	// Update index
	s.index.AddRelation(RelationEdge{Type: relationType, Source: source, Target: target})

	return nil
}

// RemoveRelation removes a relation between two tasks
func (s *Service) RemoveRelation(source string, relationType string, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	srcTask, err := s.get(source)
	if err != nil {
		return fmt.Errorf("source task not found: %s", source)
	}

	// Find and remove the relation from frontmatter
	found := false
	var newRelations []Relation
	for _, rel := range srcTask.Relations {
		if rel.Type == relationType && rel.Task == target {
			found = true
			continue
		}
		newRelations = append(newRelations, rel)
	}

	if !found {
		return fmt.Errorf("relation not found: %s from %s to %s", relationType, source, target)
	}

	srcTask.Relations = newRelations
	srcTask.UpdatedAt = time.Now().UTC()

	if err := s.storage.Save(srcTask); err != nil {
		return err
	}

	s.index.Set(srcTask)

	// Update index
	s.index.RemoveRelation(RelationEdge{Type: relationType, Source: source, Target: target})

	return nil
}

// IsBlocked checks if a task has unresolved blocked_by relations
func (s *Service) IsBlocked(taskID string) (bool, []BlockingInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isBlocked(taskID)
}

func (s *Service) isBlocked(taskID string) (bool, []BlockingInfo) {
	blockerIDs := s.index.GetBlockers(taskID)
	if len(blockerIDs) == 0 {
		return false, nil
	}

	var blockers []BlockingInfo
	for _, id := range blockerIDs {
		t, ok := s.index.Get(id)
		if !ok {
			continue
		}
		if t.Status != StatusDone {
			blockers = append(blockers, BlockingInfo{
				TaskID: t.ID,
				Status: t.Status,
				Title:  t.Title,
			})
		}
	}

	return len(blockers) > 0, blockers
}

// ArchiveTask moves a done task (and its subtasks) to the archive
func (s *Service) ArchiveTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.archiveTask(id)
}

func (s *Service) archiveTask(id string) error {
	t, ok := s.index.Get(id)
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.Status != StatusDone {
		return fmt.Errorf("task %s is not done (current: %s); only done tasks can be archived", id, t.Status)
	}
	if s.archiveStorage == nil {
		return fmt.Errorf("archive storage not available")
	}

	// If the task has subtasks, verify all are done and archive them first
	if s.index.HasSubtasks(id) {
		subtasks := s.index.GetSubtasks(id)
		for _, sub := range subtasks {
			if sub.Status != StatusDone {
				return fmt.Errorf("cannot archive task %s: subtask %s is not done (current: %s)", id, sub.ID, sub.Status)
			}
		}
		// Archive all subtasks first
		for _, sub := range subtasks {
			// Clean relations for each subtask
			removedEdges := s.index.RemoveAllRelationsForTask(sub.ID)
			if err := s.updateAffectedRelationTasks(sub.ID, removedEdges); err != nil {
				return err
			}
			if err := s.archiveStorage.Archive(sub.ID); err != nil {
				return fmt.Errorf("failed to archive subtask %s: %w", sub.ID, err)
			}
			s.index.Delete(sub.ID)
		}
	}

	// Clean relations for the main task
	removedEdges := s.index.RemoveAllRelationsForTask(id)
	if err := s.updateAffectedRelationTasks(id, removedEdges); err != nil {
		return err
	}

	// Move the file to archive
	if err := s.archiveStorage.Archive(id); err != nil {
		return fmt.Errorf("failed to archive task %s: %w", id, err)
	}

	s.index.Delete(id)
	return nil
}

// updateAffectedRelationTasks updates frontmatter of tasks whose relations pointed to taskID
func (s *Service) updateAffectedRelationTasks(taskID string, removedEdges []RelationEdge) error {
	affectedTasks := make(map[string]bool)
	for _, edge := range removedEdges {
		if edge.Source != taskID {
			affectedTasks[edge.Source] = true
		}
	}
	for affectedID := range affectedTasks {
		affected, ok := s.index.Get(affectedID)
		if !ok {
			continue
		}
		var newRelations []Relation
		for _, rel := range affected.Relations {
			if rel.Task != taskID {
				newRelations = append(newRelations, rel)
			}
		}
		affected.Relations = newRelations
		affected.UpdatedAt = time.Now().UTC()
		if err := s.storage.Save(affected); err != nil {
			return fmt.Errorf("failed to update relations in task %s: %w", affectedID, err)
		}
		s.index.Set(affected)
	}
	return nil
}

// GetAutoArchiveCandidates returns done tasks that are eligible for auto-archiving
func (s *Service) GetAutoArchiveCandidates() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoArchiveCandidates()
}

func (s *Service) autoArchiveCandidates() []*Task {
	if s.config == nil {
		return nil
	}
	threshold := time.Now().UTC().AddDate(0, 0, -s.config.AutoArchive.AfterDays)

	doneTasks := s.index.Filter(&[]Status{StatusDone}[0], nil, nil, nil, nil)
	var candidates []*Task
	for _, t := range doneTasks {
		// A task closed without its work being done has nothing to review
		// later, so it does not serve out the grace period the way a
		// completed one does - it is archived on the next pass.
		if t.EffectiveResolution().Delivered() && t.UpdatedAt.After(threshold) {
			continue
		}
		// Only return top-level tasks or subtasks whose parent is also done
		if t.ParentID != "" {
			parent, ok := s.index.Get(t.ParentID)
			if !ok || parent.Status != StatusDone {
				continue
			}
		}
		candidates = append(candidates, t)
	}
	return candidates
}

// RunAutoArchive archives all eligible candidates; skips individual failures
func (s *Service) RunAutoArchive() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runAutoArchive()
}

func (s *Service) runAutoArchive() error {
	if s.config == nil || !s.config.AutoArchive.Enabled {
		return nil
	}
	candidates := s.autoArchiveCandidates()
	for _, t := range candidates {
		// Skip tasks already archived (may have been archived as subtasks)
		if _, ok := s.index.Get(t.ID); !ok {
			continue
		}
		if err := s.archiveTask(t.ID); err != nil {
			log.Printf("auto-archive: failed to archive task %s: %v", t.ID, err)
		}
	}
	return nil
}

// ListArchived returns all archived tasks (linear scan of archive directory)
func (s *Service) ListArchived() ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.archiveStorage == nil {
		return nil, fmt.Errorf("archive storage not available")
	}
	return s.archiveStorage.LoadAllArchived()
}

// isValidType checks if task type is valid
func (s *Service) isValidType(t string) bool {
	for _, valid := range s.validTypes {
		if t == valid {
			return true
		}
	}
	return false
}
