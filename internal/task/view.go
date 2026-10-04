package task

import (
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// This file is the read side of the service: composite queries that answer a
// whole view in one lock acquisition. It exists so that consumers which only
// display tasks - the web dashboard above all - never have to reach into
// internal/storage themselves. internal/task stays the one package that
// performs task operations.

// SubtaskCount is a parent's subtask tally.
type SubtaskCount struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// BoardSnapshot is everything a kanban render needs, taken in one pass under
// one lock: a torn board that shows a task in two columns is impossible.
type BoardSnapshot struct {
	// Tasks holds every active task, top-level and subtask alike, without
	// descriptions (index entries only).
	Tasks []*Task
	// Subtasks groups the subtasks by parent id.
	Subtasks map[string][]*Task
	// Blocked lists the unresolved blockers per task id; absent when none.
	Blocked map[string][]BlockingInfo
	// Counts is the subtask tally per parent id.
	Counts map[string]SubtaskCount
	// Phases is the workflow phase per in-progress task id (subtasks
	// included), from its attached file names. Only in_progress tasks have
	// an entry; a missing entry reads as PhaseResearch (Order 0).
	Phases map[string]Phase
	// TakenAt is when the snapshot was read.
	TakenAt time.Time
}

// TaskDetail is one task plus the derived data a detail view shows.
type TaskDetail struct {
	Task      *Task
	Archived  bool
	Subtasks  []*Task
	Blocked   bool
	Blockers  []BlockingInfo
	Relations []RelationEdge
	Files     []string
}

// BoardSnapshot reads the whole active backlog at once.
func (s *Service) BoardSnapshot() (*BoardSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.boardSnapshot()
}

func (s *Service) boardSnapshot() (*BoardSnapshot, error) {
	all := s.index.All()

	snap := &BoardSnapshot{
		Tasks:    all,
		Subtasks: make(map[string][]*Task),
		Counts:   make(map[string]SubtaskCount),
		Phases:   make(map[string]Phase),
		TakenAt:  time.Now().UTC(),
	}

	ids := make([]string, 0, len(all))
	for _, t := range all {
		ids = append(ids, t.ID)
		if t.Status == StatusInProgress {
			snap.Phases[t.ID] = s.taskPhase(t.ID)
		}
		if t.ParentID == "" {
			continue
		}
		snap.Subtasks[t.ParentID] = append(snap.Subtasks[t.ParentID], t)
		c := snap.Counts[t.ParentID]
		c.Total++
		if t.Status == StatusDone {
			c.Done++
		}
		snap.Counts[t.ParentID] = c
	}

	snap.Blocked = s.blockedMap(ids)
	return snap, nil
}

// taskPhase reads one task's workflow phase from its attached file
// names. Caller holds s.mu. No file store, or a failed listing, reads as
// PhaseResearch: the board never fails over it, and the next poll
// corrects a transient error (the same tolerance as detail()).
func (s *Service) taskPhase(id string) Phase {
	if s.fileStorage == nil {
		return PhaseResearch
	}
	names, err := s.fileStorage.ListFiles(id)
	if err != nil {
		return PhaseResearch
	}
	return phaseFromFiles(names)
}

// Detail returns a task with its subtasks, blockers, relations and attached
// file names. Falls back to the archive, like Get.
func (s *Service) Detail(id string) (*TaskDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.detail(id)
}

func (s *Service) detail(id string) (*TaskDetail, error) {
	t, err := s.get(id)
	if err != nil {
		return nil, err
	}

	d := &TaskDetail{Task: t}
	if s.archiveStorage != nil && s.archiveStorage.IsArchived(id) {
		d.Archived = true
	}

	// An archived task deliberately comes back with no subtasks, blockers or
	// relations: archiving removes it from the index and cleans its edges, so
	// the active index genuinely does not hold them any more. Returning empty
	// slices is the truth, not a lookup that was skipped. Files still come
	// back - they moved into the archive alongside the task.
	if !d.Archived {
		d.Subtasks = s.index.GetSubtasks(id)
		d.Blocked, d.Blockers = s.isBlocked(id)
		d.Relations = s.index.GetRelationsForTask(id)
	}

	if s.fileStorage != nil {
		if files, err := s.fileStorage.ListFiles(id); err == nil {
			d.Files = files
		}
	}
	return d, nil
}

// Relations returns every edge where the task is source or target, including
// the reverse edges the index generates for symmetric relation types.
func (s *Service) Relations(taskID string) []RelationEdge {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index.GetRelationsForTask(taskID)
}

// BlockedMap answers IsBlocked for many tasks in one pass. Semantics are
// identical to calling IsBlocked per id: only blockers that are not done
// count, and an id with no live blockers is absent from the map.
func (s *Service) BlockedMap(ids []string) map[string][]BlockingInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blockedMap(ids)
}

func (s *Service) blockedMap(ids []string) map[string][]BlockingInfo {
	out := make(map[string][]BlockingInfo)
	blockers := s.index.AllBlockers()
	if len(blockers) == 0 {
		return out
	}

	// One board render can name the same blocker many times; loading it once
	// keeps the whole map to a handful of task-file reads.
	seen := make(map[string]*Task)
	lookup := func(id string) (*Task, bool) {
		if t, ok := seen[id]; ok {
			return t, t != nil
		}
		t, ok := s.index.Get(id)
		if !ok {
			seen[id] = nil
			return nil, false
		}
		seen[id] = t
		return t, true
	}

	for _, id := range ids {
		var live []BlockingInfo
		for _, target := range blockers[id] {
			t, ok := lookup(target)
			if !ok || t.Status == StatusDone {
				continue
			}
			live = append(live, BlockingInfo{TaskID: t.ID, Status: t.Status, Title: t.Title})
		}
		if len(live) > 0 {
			out[id] = live
		}
	}
	return out
}

// Config returns the configuration this service was built for.
//
// Deliberately unlocked: the field is write-once, assigned in NewService.
func (s *Service) Config() *config.Config {
	return s.config
}
