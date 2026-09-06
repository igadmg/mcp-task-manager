package task

import (
	"errors"
	"fmt"
	"log"
	"strings"
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
// Performance note: Methods returning []*Task use lazy loading:
//   - Get() loads the full task with description from disk
//   - Filter(), All(), GetSubtasks(), NextTodo() return tasks without descriptions (from in-memory index)
//
// This design keeps list operations fast while still providing full task data on demand.
type Index interface {
	Load() error
	Save() error
	Get(id string) (*Task, bool) // Loads full task with description from disk
	Set(t *Task)
	Delete(id string)
	All() []*Task                                                                          // Returns tasks without descriptions (from index)
	Filter(status *Status, priority *Priority, taskType *string, parentID *string) []*Task // Returns tasks without descriptions
	NextTodo() *Task                                                                       // Returns task without description (from index)
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
}

// NewService creates a new task service
func NewService(storage Storage, archiveStorage ArchiveStorage, fileStorage FileStorage, index Index, validTypes []string, cfg *config.Config) *Service {
	return &Service{
		storage:        storage,
		archiveStorage: archiveStorage,
		fileStorage:    fileStorage,
		index:          index,
		validTypes:     validTypes,
		config:         cfg,
	}
}

// EnsureProjectExists checks that a project was found during config loading.
// Should be called before read operations.
func (s *Service) EnsureProjectExists() error {
	if s.config == nil || !s.config.ProjectFound {
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
		if err := s.RunAutoArchive(); err != nil {
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
	if err := s.index.Save(); err != nil {
		return nil, err
	}

	return t, nil
}

// CreateSubtask creates a subtask under a parent
func (s *Service) CreateSubtask(title, description string, priority Priority, taskType string, parentID string) (*Task, error) {
	return s.Create(title, description, priority, taskType, parentID, "")
}

// Get returns a task by ID with full description loaded from disk.
// Falls back to the archive if the task is not found in the active index.
func (s *Service) Get(id string) (*Task, error) {
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
	t, err := s.Get(id)
	if err != nil {
		return nil, nil, err
	}
	subtasks := s.index.GetSubtasks(id)
	return t, subtasks, nil
}

// WriteTaskFile creates or overwrites a named file attached to the given
// task. Only active tasks accept writes - archived tasks are read-only.
func (s *Service) WriteTaskFile(taskID string, filename, content string) error {
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
	if _, err := s.Get(taskID); err != nil {
		return "", err
	}
	return s.fileStorage.ReadFile(taskID, filename)
}

// ListTaskFiles returns the names of all files attached to the given task.
// Works for both active and archived tasks.
func (s *Service) ListTaskFiles(taskID string) ([]string, error) {
	if _, err := s.Get(taskID); err != nil {
		return nil, err
	}
	return s.fileStorage.ListFiles(taskID)
}

// GetSubtaskCounts returns the count of subtasks for a task
func (s *Service) GetSubtaskCounts(taskID string) (total, done int) {
	return s.index.SubtaskCounts(taskID)
}

// Update modifies a task
func (s *Service) Update(id string, title, description *string, status *Status, priority *Priority, taskType *string) (*Task, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
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

	t.UpdatedAt = time.Now().UTC()

	if err := s.storage.Save(t); err != nil {
		return nil, err
	}

	s.index.Set(t)
	if err := s.index.Save(); err != nil {
		return nil, err
	}

	return t, nil
}

// Delete removes a task
func (s *Service) Delete(id string, deleteSubtasks bool) error {
	t, err := s.Get(id)
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
		affected, err := s.Get(affectedID)
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
	return s.index.Save()
}

// List returns all tasks, optionally filtered
// Note: Tasks returned do not include descriptions for performance (use Get for full task data)
func (s *Service) List(status *Status, priority *Priority, taskType *string, parentID *string) []*Task {
	return s.index.Filter(status, priority, taskType, parentID)
}

// GetNextTask returns the highest priority todo task
// Note: Task returned does not include description (use Get to load full task data)
func (s *Service) GetNextTask() *Task {
	return s.index.NextTodo()
}

// StartTask moves a task from todo to in_progress
func (s *Service) StartTask(id string) (*Task, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	if t.Status != StatusTodo {
		return nil, fmt.Errorf("task %s is not in todo status (current: %s)", id, t.Status)
	}

	// Check if task is blocked
	if blocked, blockers := s.IsBlocked(id); blocked {
		var parts []string
		for _, b := range blockers {
			parts = append(parts, fmt.Sprintf("%s (%s)", b.TaskID, b.Status))
		}
		return nil, fmt.Errorf("task %s is blocked by tasks: %s", id, strings.Join(parts, ", "))
	}

	// Auto-start parent if this is a subtask
	if t.ParentID != "" {
		parent, err := s.Get(t.ParentID)
		if err != nil {
			return nil, fmt.Errorf("parent task not found: %s", t.ParentID)
		}
		if parent.Status == StatusTodo {
			status := StatusInProgress
			if _, err := s.Update(t.ParentID, nil, nil, &status, nil, nil); err != nil {
				return nil, fmt.Errorf("failed to start parent task: %w", err)
			}
		}
	}

	status := StatusInProgress
	return s.Update(id, nil, nil, &status, nil, nil)
}

// CompleteTask moves a task from in_progress to done
func (s *Service) CompleteTask(id string) (*Task, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	if t.Status != StatusInProgress {
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
	if incompleteCount > 0 {
		return nil, fmt.Errorf("cannot complete task %s: has %d incomplete subtask(s)", id, incompleteCount)
	}

	// Complete this task
	status := StatusDone
	completed, err := s.Update(id, nil, nil, &status, nil, nil)
	if err != nil {
		return nil, err
	}

	// If this is a subtask, check if all siblings are done -> auto-complete parent
	if t.ParentID != "" {
		siblings := s.index.GetSubtasks(t.ParentID)
		allDone := true
		for _, sib := range siblings {
			if sib.Status != StatusDone {
				allDone = false
				break
			}
		}
		if allDone {
			if _, err := s.Update(t.ParentID, nil, nil, &status, nil, nil); err != nil {
				return nil, fmt.Errorf("failed to auto-complete parent: %w", err)
			}
		}
	}

	return completed, nil
}

// BlockingInfo describes a task that is blocking another
type BlockingInfo struct {
	TaskID string `json:"task_id"`
	Status Status `json:"status"`
	Title  string `json:"title"`
}

// AddRelation adds a relation between two tasks
func (s *Service) AddRelation(source string, relationType string, target string) error {
	// Validate no self-reference
	if source == target {
		return fmt.Errorf("cannot create relation: source and target are the same task (%s)", source)
	}

	// Validate relation type
	if s.config != nil && !s.config.IsValidRelationType(relationType) {
		return fmt.Errorf("invalid relation type: %s", relationType)
	}

	// Validate source task exists
	srcTask, err := s.Get(source)
	if err != nil {
		return fmt.Errorf("source task not found: %s", source)
	}

	// Validate target task exists
	if _, err := s.Get(target); err != nil {
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

	return s.index.Save()
}

// RemoveRelation removes a relation between two tasks
func (s *Service) RemoveRelation(source string, relationType string, target string) error {
	srcTask, err := s.Get(source)
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

	return s.index.Save()
}

// IsBlocked checks if a task has unresolved blocked_by relations
func (s *Service) IsBlocked(taskID string) (bool, []BlockingInfo) {
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
	return s.index.Save()
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
	if s.config == nil {
		return nil
	}
	threshold := time.Now().UTC().AddDate(0, 0, -s.config.AutoArchive.AfterDays)

	doneTasks := s.index.Filter(&[]Status{StatusDone}[0], nil, nil, nil)
	var candidates []*Task
	for _, t := range doneTasks {
		if t.UpdatedAt.After(threshold) {
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
	if s.config == nil || !s.config.AutoArchive.Enabled {
		return nil
	}
	candidates := s.GetAutoArchiveCandidates()
	for _, t := range candidates {
		// Skip tasks already archived (may have been archived as subtasks)
		if _, ok := s.index.Get(t.ID); !ok {
			continue
		}
		if err := s.ArchiveTask(t.ID); err != nil {
			log.Printf("auto-archive: failed to archive task %s: %v", t.ID, err)
		}
	}
	return nil
}

// ListArchived returns all archived tasks (linear scan of archive directory)
func (s *Service) ListArchived() ([]*Task, error) {
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
