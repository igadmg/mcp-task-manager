package storage

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// compareTaskIDs reports whether a sorts before b: numerically when both
// are pure non-negative integers (preserving today's 1,2,...,9,10,11
// ordering even though ids are now variable-width strings), falling back
// to a plain string compare otherwise (a stable, deterministic ordering
// for custom ids or a numeric-vs-custom pairing).
func compareTaskIDs(a, b string) bool {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return an < bn
	}
	return a < b
}

// IndexEntry contains task metadata without description. It is an in-memory
// projection of a task's {id}/{id}.md frontmatter and is never serialized.
type IndexEntry struct {
	ID        string
	ParentID  string
	Title     string
	Status    task.Status
	Priority  task.Priority
	Type      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// taskToEntry converts a Task to an IndexEntry
func taskToEntry(t *task.Task) *IndexEntry {
	return &IndexEntry{
		ID:        t.ID,
		ParentID:  t.ParentID,
		Title:     t.Title,
		Status:    t.Status,
		Priority:  t.Priority,
		Type:      t.Type,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// entryToTask converts an IndexEntry back to a Task (without description)
func entryToTask(e *IndexEntry) *task.Task {
	return &task.Task{
		ID:        e.ID,
		ParentID:  e.ParentID,
		Title:     e.Title,
		Status:    e.Status,
		Priority:  e.Priority,
		Type:      e.Type,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
		// Description intentionally empty
	}
}

// SymmetricRelationType is a relation type where reverse edges are auto-generated
const SymmetricRelationType = "relates_to"

// BlockingRelationType is the relation type that affects task execution order
const BlockingRelationType = "blocked_by"

// Index is an in-memory cache of all active tasks, built entirely from the
// per-task {id}/{id}.md files. It has no on-disk form of its own.
type Index struct {
	entries           map[string]*IndexEntry
	relationsBySource map[string][]task.RelationEdge
	relationsByTarget map[string][]task.RelationEdge
	dir               string
	storage           *MarkdownStorage
	// builtAt is the moment the in-memory state last agreed with disk,
	// refreshed both by a rebuild and by our own writes. It is the baseline
	// isStaleOnDisk compares task file mtimes against.
	builtAt time.Time
}

// NewIndex creates a new index for the given directory
func NewIndex(dir string, storage *MarkdownStorage) *Index {
	return &Index{
		entries:           make(map[string]*IndexEntry),
		relationsBySource: make(map[string][]task.RelationEdge),
		relationsByTarget: make(map[string][]task.RelationEdge),
		dir:               dir,
		storage:           storage,
	}
}

// legacyIndexPath is the shared metadata cache this package used to write.
// It is only referenced to delete a leftover copy - see Load.
func (idx *Index) legacyIndexPath() string {
	return filepath.Join(idx.dir, ".index.json")
}

func (idx *Index) reset() {
	idx.entries = make(map[string]*IndexEntry)
	idx.relationsBySource = make(map[string][]task.RelationEdge)
	idx.relationsByTarget = make(map[string][]task.RelationEdge)
}

func (idx *Index) rebuildFromTasks(tasks []*task.Task) error {
	idx.reset()

	for _, t := range tasks {
		idx.entries[t.ID] = taskToEntry(t)
	}

	// Build relation edges from task frontmatter.
	for _, t := range tasks {
		for _, rel := range t.Relations {
			edge := task.RelationEdge{
				Type:   rel.Type,
				Source: t.ID,
				Target: rel.Task,
			}
			idx.addEdge(edge)
			// Symmetric types generate a reverse edge.
			if rel.Type == SymmetricRelationType {
				reverse := task.RelationEdge{
					Type:   rel.Type,
					Source: rel.Task,
					Target: t.ID,
				}
				idx.addEdge(reverse)
			}
		}
	}

	idx.builtAt = time.Now()
	return nil
}

// Rebuild scans all markdown files and rebuilds the index
func (idx *Index) Rebuild() error {
	tasks, err := idx.storage.LoadAll()
	if err != nil {
		return err
	}

	return idx.rebuildFromTasks(tasks)
}

// Load populates the index from the per-task directories. It also removes
// the obsolete shared cache file this package used to write, so a repository
// carrying one from an older build does not keep it around indefinitely;
// failing to remove it is harmless (nothing reads it) and never fails startup.
func (idx *Index) Load() error {
	_ = os.Remove(idx.legacyIndexPath())
	return idx.Rebuild()
}

// syncIfStale rebuilds the index when the task directories have changed
// behind our back (a git pull, a hand-edited file, another process).
//
// Contract: exported query methods call this exactly once, on entry;
// helpers reachable only from inside such a method must not call it, or a
// single query costs one directory scan per task instead of one in total.
func (idx *Index) syncIfStale() {
	stale, err := idx.isStaleOnDisk()
	if err != nil || !stale {
		return
	}
	_ = idx.Rebuild()
}

// isStaleOnDisk reports whether any task file has been written since the
// in-memory state was last known to agree with disk, or whether the number
// of task directories has diverged from the number of entries we hold.
func (idx *Index) isStaleOnDisk() (bool, error) {
	entries, err := os.ReadDir(idx.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	taskCount := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "archive" {
			continue
		}

		info, err := os.Stat(filepath.Join(idx.dir, entry.Name(), entry.Name()+".md"))
		if err != nil {
			continue
		}

		taskCount++

		if info.ModTime().After(idx.builtAt) {
			return true, nil
		}
	}

	if taskCount == 0 {
		return false, nil
	}

	if taskCount != len(idx.entries) {
		return true, nil
	}

	return false, nil
}

// GetEntry returns an entry by ID (metadata only, no description)
func (idx *Index) GetEntry(id string) (*IndexEntry, bool) {
	idx.syncIfStale()
	return idx.getEntry(id)
}

// getEntry is the non-syncing form of GetEntry, for callers that have
// already synced (see syncIfStale's contract).
func (idx *Index) getEntry(id string) (*IndexEntry, bool) {
	e, ok := idx.entries[id]
	return e, ok
}

// Get returns a full task by ID (loads description from disk)
func (idx *Index) Get(id string) (*task.Task, bool) {
	idx.syncIfStale()
	if _, ok := idx.entries[id]; !ok {
		return nil, false
	}
	t, err := idx.storage.Load(id)
	if err != nil {
		return nil, false
	}
	return t, true
}

// Set adds or updates a task in the index
func (idx *Index) Set(t *task.Task) {
	idx.entries[t.ID] = taskToEntry(t)
	idx.builtAt = time.Now()
}

// Delete removes a task from the index
func (idx *Index) Delete(id string) {
	delete(idx.entries, id)
	idx.builtAt = time.Now()
}

// All returns all tasks sorted by ID
func (idx *Index) All() []*task.Task {
	idx.syncIfStale()
	tasks := make([]*task.Task, 0, len(idx.entries))
	for _, e := range idx.entries {
		tasks = append(tasks, entryToTask(e))
	}
	sort.Slice(tasks, func(i, j int) bool {
		return compareTaskIDs(tasks[i].ID, tasks[j].ID)
	})
	return tasks
}

// Filter returns tasks matching the given criteria
// parentID: nil = all tasks, "0" = top-level only, otherwise = subtasks of that parent
func (idx *Index) Filter(status *task.Status, priority *task.Priority, taskType *string, parentID *string) []*task.Task {
	idx.syncIfStale()
	var result []*task.Task
	for _, e := range idx.entries {
		if status != nil && e.Status != *status {
			continue
		}
		if priority != nil && e.Priority != *priority {
			continue
		}
		if taskType != nil && e.Type != *taskType {
			continue
		}
		if parentID != nil {
			if *parentID == "0" {
				// Top-level only
				if e.ParentID != "" {
					continue
				}
			} else {
				// Subtasks of specific parent
				if e.ParentID != *parentID {
					continue
				}
			}
		}
		result = append(result, entryToTask(e))
	}
	sort.Slice(result, func(i, j int) bool {
		return compareTaskIDs(result[i].ID, result[j].ID)
	})
	return result
}

type nextTodoGroupKey struct {
	priorityOrder    int
	createdAt        time.Time
	id               string
	inProgressParent bool
}

type nextTodoGroup struct {
	key   nextTodoGroupKey
	tasks []*task.Task
}

func (idx *Index) isActionableForNextTodo(e *IndexEntry) bool {
	if e.Status != task.StatusTodo && e.Status != task.StatusInProgress {
		return false
	}
	if idx.hasSubtasks(e.ID) {
		return false
	}
	return !idx.isBlocked(e.ID)
}

func (idx *Index) nextTodoGroupForEntry(e *IndexEntry) (string, nextTodoGroupKey) {
	groupID := e.ID
	key := nextTodoGroupKey{
		priorityOrder:    e.Priority.Order(),
		createdAt:        e.CreatedAt,
		id:               e.ID,
		inProgressParent: e.Status == task.StatusInProgress,
	}

	if e.ParentID != "" {
		if parent, ok := idx.getEntry(e.ParentID); ok {
			groupID = parent.ID
			key = nextTodoGroupKey{
				priorityOrder:    parent.Priority.Order(),
				createdAt:        parent.CreatedAt,
				id:               parent.ID,
				inProgressParent: parent.Status == task.StatusInProgress,
			}
		}
	}

	return groupID, key
}

// NextTodo returns the highest priority actionable todo task.
// Parent tasks with subtasks are skipped. Subtasks inherit their parent's
// priority, creation date, and ID for group selection, then compete within the
// winning group by their own priority, creation date, and ID.
func (idx *Index) NextTodo() *task.Task {
	idx.syncIfStale()
	groups := make(map[string]*nextTodoGroup)

	for _, e := range idx.entries {
		if !idx.isActionableForNextTodo(e) {
			continue
		}

		candidate := entryToTask(e)
		groupID, key := idx.nextTodoGroupForEntry(e)

		group, ok := groups[groupID]
		if !ok {
			group = &nextTodoGroup{key: key}
			groups[groupID] = group
		}
		group.tasks = append(group.tasks, candidate)
	}

	if len(groups) == 0 {
		return nil
	}

	groupList := make([]*nextTodoGroup, 0, len(groups))
	for _, group := range groups {
		groupList = append(groupList, group)
	}

	sort.Slice(groupList, func(i, j int) bool {
		left := groupList[i].key
		right := groupList[j].key
		if left.inProgressParent != right.inProgressParent {
			return left.inProgressParent
		}
		if left.priorityOrder != right.priorityOrder {
			return left.priorityOrder < right.priorityOrder
		}
		if !left.createdAt.Equal(right.createdAt) {
			return left.createdAt.Before(right.createdAt)
		}
		return compareTaskIDs(left.id, right.id)
	})

	winningGroup := groupList[0].tasks
	sort.Slice(winningGroup, func(i, j int) bool {
		if winningGroup[i].Status != winningGroup[j].Status {
			return winningGroup[i].Status == task.StatusInProgress
		}
		if winningGroup[i].Priority.Order() != winningGroup[j].Priority.Order() {
			return winningGroup[i].Priority.Order() < winningGroup[j].Priority.Order()
		}
		if !winningGroup[i].CreatedAt.Equal(winningGroup[j].CreatedAt) {
			return winningGroup[i].CreatedAt.Before(winningGroup[j].CreatedAt)
		}
		return compareTaskIDs(winningGroup[i].ID, winningGroup[j].ID)
	})

	return winningGroup[0]
}

// NextID returns the next available task ID
func (idx *Index) NextID() string {
	idx.syncIfStale()
	maxID := 0
	for id := range idx.entries {
		if n, err := strconv.Atoi(id); err == nil && n > maxID {
			maxID = n
		}
	}
	activeNextID := maxID + 1

	storageNextID, err := idx.storage.NextID()
	if err != nil {
		return strconv.Itoa(activeNextID)
	}
	if storageNextID > activeNextID {
		activeNextID = storageNextID
	}
	return strconv.Itoa(activeNextID)
}

// GetSubtasks returns all subtasks of a parent task
func (idx *Index) GetSubtasks(parentID string) []*task.Task {
	idx.syncIfStale()
	var result []*task.Task
	for _, e := range idx.entries {
		if e.ParentID == parentID {
			result = append(result, entryToTask(e))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return compareTaskIDs(result[i].ID, result[j].ID)
	})
	return result
}

// HasSubtasks returns true if the task has any subtasks
func (idx *Index) HasSubtasks(taskID string) bool {
	idx.syncIfStale()
	return idx.hasSubtasks(taskID)
}

// hasSubtasks is the non-syncing form of HasSubtasks, for callers that have
// already synced (see syncIfStale's contract).
func (idx *Index) hasSubtasks(taskID string) bool {
	for _, e := range idx.entries {
		if e.ParentID == taskID {
			return true
		}
	}
	return false
}

// SubtaskCounts returns (total, done) counts for a parent task
func (idx *Index) SubtaskCounts(parentID string) (total int, done int) {
	idx.syncIfStale()
	for _, e := range idx.entries {
		if e.ParentID == parentID {
			total++
			if e.Status == task.StatusDone {
				done++
			}
		}
	}
	return
}

// isBlocked checks if a task has any unresolved blocked_by relations.
// Non-syncing: reachable only from NextTodo, which syncs first.
func (idx *Index) isBlocked(taskID string) bool {
	for _, e := range idx.relationsBySource[taskID] {
		if e.Type == BlockingRelationType {
			if target, ok := idx.entries[e.Target]; ok && target.Status != task.StatusDone {
				return true
			}
		}
	}
	return false
}

// addEdge adds an edge to both lookup maps (internal helper, no persistence)
func (idx *Index) addEdge(edge task.RelationEdge) {
	idx.relationsBySource[edge.Source] = append(idx.relationsBySource[edge.Source], edge)
	idx.relationsByTarget[edge.Target] = append(idx.relationsByTarget[edge.Target], edge)
}

// removeEdge removes an edge from both lookup maps (internal helper, no persistence)
func (idx *Index) removeEdge(edge task.RelationEdge) {
	// Remove from source map
	src := idx.relationsBySource[edge.Source]
	for i, e := range src {
		if e.Type == edge.Type && e.Source == edge.Source && e.Target == edge.Target {
			idx.relationsBySource[edge.Source] = append(src[:i], src[i+1:]...)
			break
		}
	}
	// Remove from target map
	tgt := idx.relationsByTarget[edge.Target]
	for i, e := range tgt {
		if e.Type == edge.Type && e.Source == edge.Source && e.Target == edge.Target {
			idx.relationsByTarget[edge.Target] = append(tgt[:i], tgt[i+1:]...)
			break
		}
	}
}

// AddRelation adds a relation edge to the index
func (idx *Index) AddRelation(edge task.RelationEdge) {
	idx.addEdge(edge)
	// Symmetric types generate a reverse edge
	if edge.Type == SymmetricRelationType {
		reverse := task.RelationEdge{
			Type:   edge.Type,
			Source: edge.Target,
			Target: edge.Source,
		}
		idx.addEdge(reverse)
	}
	idx.builtAt = time.Now()
}

// RemoveRelation removes a relation edge from the index
func (idx *Index) RemoveRelation(edge task.RelationEdge) {
	idx.removeEdge(edge)
	// Symmetric types also remove the reverse edge
	if edge.Type == SymmetricRelationType {
		reverse := task.RelationEdge{
			Type:   edge.Type,
			Source: edge.Target,
			Target: edge.Source,
		}
		idx.removeEdge(reverse)
	}
	idx.builtAt = time.Now()
}

// GetRelationsForTask returns all edges where task is source OR target
func (idx *Index) GetRelationsForTask(taskID string) []task.RelationEdge {
	idx.syncIfStale()
	seen := make(map[task.RelationEdge]bool)
	var result []task.RelationEdge

	for _, e := range idx.relationsBySource[taskID] {
		if !seen[e] {
			seen[e] = true
			result = append(result, e)
		}
	}
	for _, e := range idx.relationsByTarget[taskID] {
		if !seen[e] {
			seen[e] = true
			result = append(result, e)
		}
	}
	return result
}

// GetBlockers returns target IDs from blocked_by edges where source == taskID
func (idx *Index) GetBlockers(taskID string) []string {
	idx.syncIfStale()
	var blockers []string
	for _, e := range idx.relationsBySource[taskID] {
		if e.Type == BlockingRelationType {
			blockers = append(blockers, e.Target)
		}
	}
	return blockers
}

// RemoveAllRelationsForTask removes all relations where task appears as source or target
// Returns the removed edges so the service knows which other task files to update
func (idx *Index) RemoveAllRelationsForTask(taskID string) []task.RelationEdge {
	var removed []task.RelationEdge

	// Remove edges where task is source
	for _, e := range idx.relationsBySource[taskID] {
		removed = append(removed, e)
		// Remove from target's map
		tgt := idx.relationsByTarget[e.Target]
		for i, te := range tgt {
			if te.Type == e.Type && te.Source == e.Source && te.Target == e.Target {
				idx.relationsByTarget[e.Target] = append(tgt[:i], tgt[i+1:]...)
				break
			}
		}
	}
	delete(idx.relationsBySource, taskID)

	// Remove edges where task is target
	for _, e := range idx.relationsByTarget[taskID] {
		removed = append(removed, e)
		// Remove from source's map
		src := idx.relationsBySource[e.Source]
		for i, se := range src {
			if se.Type == e.Type && se.Source == e.Source && se.Target == e.Target {
				idx.relationsBySource[e.Source] = append(src[:i], src[i+1:]...)
				break
			}
		}
	}
	delete(idx.relationsByTarget, taskID)

	idx.builtAt = time.Now()
	return removed
}
