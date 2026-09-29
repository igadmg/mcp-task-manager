package task

import (
	"fmt"
	"slices"
)

// The current-task pointer names the task the calling user is working on:
// start_task writes it; completing or closing that task moves it to the
// still-open parent of a subtask, or removes it. It is kept per user, so
// two people sharing a backlog never overwrite each other's.

// CurrentTask returns the task the calling user's pointer names, archived
// tasks included. ok is false when there is no pointer.
func (s *Service) CurrentTask() (*Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentTask()
}

func (s *Service) currentTask() (*Task, bool, error) {
	if !s.keepsPointer() {
		return nil, false, nil
	}
	id, ok, err := s.current.ReadCurrentTask(s.identity.Name)
	if err != nil || !ok {
		return nil, false, err
	}
	t, err := s.get(id)
	if err != nil {
		return nil, false, fmt.Errorf("current task %s not found", id)
	}
	return t, true, nil
}

// keepsPointer reports whether this service keeps a current-task pointer:
// it needs a store and a user to keep it for.
func (s *Service) keepsPointer() bool {
	return s.current != nil && s.identity.Name != ""
}

// setPointer points the calling user at id, journaling the old value.
func (s *Service) setPointer(txn *gitTxn, id string) error {
	if !s.keepsPointer() {
		return nil
	}
	if err := s.capturePointer(txn, s.identity.Name); err != nil {
		return err
	}
	return s.current.WriteCurrentTask(s.identity.Name, id)
}

// clearPointerIf moves the pointer off tasks that were just closed: when it
// names one of closed, it is set to parentID if that is non-empty, and
// removed otherwise. A pointer naming anything else is left alone.
func (s *Service) clearPointerIf(txn *gitTxn, parentID string, closed ...string) error {
	if !s.keepsPointer() {
		return nil
	}
	id, ok, err := s.current.ReadCurrentTask(s.identity.Name)
	if err != nil {
		return err
	}
	if !ok || !slices.Contains(closed, id) {
		return nil
	}
	if err := s.capturePointer(txn, s.identity.Name); err != nil {
		return err
	}
	if parentID != "" {
		return s.current.WriteCurrentTask(s.identity.Name, parentID)
	}
	return s.current.RemoveCurrentTask(s.identity.Name)
}
