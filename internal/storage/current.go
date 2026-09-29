package storage

import (
	"os"
	"path/filepath"
	"strings"
)

// Per-user state lives under the tasks directory, outside any task.
const (
	// UsersDirName holds one directory per user. Scans skip it explicitly,
	// and it is a reserved task id, so it can never be read as a task.
	UsersDirName = ".users"
	// CurrentTaskFileName is the per-user current-task pointer.
	CurrentTaskFileName = "current_task"
)

// CurrentTaskPath returns where user's current-task pointer is kept.
func (s *MarkdownStorage) CurrentTaskPath(user string) string {
	return filepath.Join(s.dir, UsersDirName, user, CurrentTaskFileName)
}

// ReadCurrentTask returns the id held in user's pointer: its trimmed first
// line. A missing or empty pointer reads as ok=false with no error.
func (s *MarkdownStorage) ReadCurrentTask(user string) (id string, ok bool, err error) {
	if err := validatePathSegment("user", user); err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(s.CurrentTaskPath(user))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	first, _, _ := strings.Cut(string(data), "\n")
	id = strings.TrimSpace(first)
	return id, id != "", nil
}

// WriteCurrentTask points user's pointer at id, atomically.
func (s *MarkdownStorage) WriteCurrentTask(user, id string) error {
	if err := validatePathSegment("user", user); err != nil {
		return err
	}
	return writeFileAtomic(s.CurrentTaskPath(user), []byte(id+"\n"))
}

// RemoveCurrentTask clears user's pointer. Removing an absent pointer is not
// an error.
func (s *MarkdownStorage) RemoveCurrentTask(user string) error {
	if err := validatePathSegment("user", user); err != nil {
		return err
	}
	if err := os.Remove(s.CurrentTaskPath(user)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
