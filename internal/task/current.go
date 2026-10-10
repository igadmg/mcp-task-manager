package task

import (
	"slices"
)

// The current-task list names the tasks the calling user is working on,
// in the order they were started: the last entry is the most recent one.
// start_task and start_phase append to it; complete_task removes what the
// call closed. It is kept per user, so two people sharing a backlog never
// overwrite each other's.

// CurrentTasks returns the calling user's effective current-task list: the
// stored ids that still resolve to an in_progress task, in the order they
// were added. The last element is the most recently started task. Entries
// naming an unknown or no-longer-in-progress task are skipped silently, and
// reading never rewrites the file.
func (s *Service) CurrentTasks() ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentTasks()
}

func (s *Service) currentTasks() ([]*Task, error) {
	if !s.keepsPointer() {
		return nil, nil
	}
	ids, err := s.current.ReadCurrentTasks(s.identity.Name)
	if err != nil {
		return nil, err
	}
	return s.inProgressTasks(ids), nil
}

// inProgressTasks resolves ids, keeping only those naming a task that is
// still in progress; entries naming anything else are dropped. Caller
// holds s.mu.
func (s *Service) inProgressTasks(ids []string) []*Task {
	var tasks []*Task
	for _, id := range ids {
		t, err := s.get(id)
		if err != nil || t.Status != StatusInProgress {
			continue
		}
		tasks = append(tasks, t)
	}
	return tasks
}

// keepsPointer reports whether this service keeps a current-task list: it
// needs a store and a user to keep it for.
func (s *Service) keepsPointer() bool {
	return s.current != nil && s.identity.Name != ""
}

// addPointer appends ids to the calling user's current-task list, moving an
// id that is already listed to the end instead of duplicating it. Entries
// that no longer resolve to an in_progress task fall out of the file. The
// old list is journaled before it is written.
func (s *Service) addPointer(txn *gitTxn, ids ...string) error {
	if !s.keepsPointer() {
		return nil
	}
	stored, err := s.current.ReadCurrentTasks(s.identity.Name)
	if err != nil {
		return err
	}
	list := taskIDs(s.inProgressTasks(stored))
	for _, id := range ids {
		list = slices.DeleteFunc(list, func(x string) bool { return x == id })
		list = append(list, id)
	}
	if err := s.capturePointer(txn, s.identity.Name); err != nil {
		return err
	}
	return s.current.WriteCurrentTasks(s.identity.Name, list)
}

// prunePointer removes the just-closed ids from the calling user's
// current-task list, dropping stale entries the same way addPointer does.
// When the closure closed the list's last entry and an open parent of the
// closed task remains, the parent is listed last instead. A list naming
// none of the closed ids is left alone: no write, no journal entry, so
// closing someone else's task never touches the caller's list.
func (s *Service) prunePointer(txn *gitTxn, parentID string, closed ...string) error {
	if !s.keepsPointer() {
		return nil
	}
	stored, err := s.current.ReadCurrentTasks(s.identity.Name)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(stored, func(id string) bool { return slices.Contains(closed, id) }) {
		return nil
	}
	lastClosed := len(stored) > 0 && slices.Contains(closed, stored[len(stored)-1])
	list := slices.DeleteFunc(taskIDs(s.inProgressTasks(stored)), func(id string) bool { return slices.Contains(closed, id) })
	if parentID != "" && lastClosed {
		list = slices.DeleteFunc(list, func(id string) bool { return id == parentID })
		list = append(list, parentID)
	}
	if err := s.capturePointer(txn, s.identity.Name); err != nil {
		return err
	}
	return s.current.WriteCurrentTasks(s.identity.Name, list)
}

// taskIDs is the ids of tasks in their list order.
func taskIDs(tasks []*Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}
