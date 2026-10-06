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
	// included): the phase of its latest started run when it has phase
	// records, else derived from its attached file names. Only in_progress
	// tasks have an entry; a missing entry reads as PhaseResearch (Order 0).
	Phases map[string]Phase
	// PhaseInfo summarizes the phase records of the in-progress tasks that
	// have readable ones. Todo and done tasks are never read.
	PhaseInfo map[string]PhaseSummary
	// Stats holds the Done-column statistics cards, in config order,
	// computed from Tasks (see stats.go).
	Stats []StatsCard
	// TakenAt is when the snapshot was read, in UTC; Stats counts up to it.
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
	// Files are the attached file names, without the phase records that
	// Phases holds.
	Files []string
	// Phases holds the task's phase records in workflow order; unreadable
	// ones are left out.
	Phases []PhaseRecord
}

// TotalTokens sums the tokens of the finished runs across d.Phases; ok is
// false when none reported any.
func (d *TaskDetail) TotalTokens() (total int64, ok bool) {
	return TotalTokens(d.Phases)
}

// BoardSnapshot reads the whole active backlog at once.
func (s *Service) BoardSnapshot() (*BoardSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.boardSnapshot()
}

func (s *Service) boardSnapshot() (*BoardSnapshot, error) {
	all := s.index.All()
	now := s.now()

	snap := &BoardSnapshot{
		Tasks:     all,
		Subtasks:  make(map[string][]*Task),
		Counts:    make(map[string]SubtaskCount),
		Phases:    make(map[string]Phase),
		PhaseInfo: make(map[string]PhaseSummary),
		Stats:     computeStats(all, s.config.DoneStatsCards(), s.validTypes, now),
		TakenAt:   now.UTC(),
	}

	ids := make([]string, 0, len(all))
	for _, t := range all {
		ids = append(ids, t.ID)
		if t.Status == StatusInProgress {
			// An unreadable record only leaves itself out; with no run
			// left, the names decide.
			names := s.attachedFiles(t.ID)
			recs, _ := s.loadPhases(t.ID, names)
			if sum := summarizePhases(recs); sum.Current != "" {
				snap.PhaseInfo[t.ID] = sum
				snap.Phases[t.ID] = sum.Current
			} else {
				snap.Phases[t.ID] = phaseFromFiles(names)
			}
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

// attachedFiles lists a task's attached file names for a read-only view.
// Caller holds s.mu. No file store, or a failed listing, reads as no files:
// a view never fails over them, and the next read corrects a transient
// error. ListTaskFiles, the tool path, reports errors instead.
func (s *Service) attachedFiles(id string) []string {
	files, err := s.listFiles(id)
	if err != nil {
		return nil
	}
	return files
}

// listFiles lists a task's attached file names, phase records included.
// Caller holds s.mu. No file store lists none.
func (s *Service) listFiles(id string) ([]string, error) {
	if s.fileStorage == nil {
		return nil, nil
	}
	return s.fileStorage.ListFiles(id)
}

// Detail returns a task with its subtasks, blockers, relations, attached
// file names and phase records. Falls back to the archive, like Get.
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

	names := s.attachedFiles(id)
	for _, name := range names {
		if !IsReservedFileName(name) {
			d.Files = append(d.Files, name)
		}
	}
	// An unreadable record only leaves itself out of the history.
	d.Phases, _ = s.loadPhases(id, names)
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
