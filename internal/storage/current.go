package storage

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// Per-user state lives under the tasks directory, outside any task.
const (
	// UsersDirName holds one directory per user. Scans skip it explicitly,
	// and it is a reserved task id, so it can never be read as a task.
	UsersDirName = ".users"
	// CurrentTaskFileName is the per-user current-task list, one task id
	// per line, in the order the tasks were started.
	CurrentTaskFileName = "current_task"
)

// CurrentTaskPath returns where user's current-task list is kept.
func (s *MarkdownStorage) CurrentTaskPath(user string) string {
	return filepath.Join(s.dir, UsersDirName, user, CurrentTaskFileName)
}

// ReadCurrentTasks returns the ids held in user's current-task list. A
// missing file reads as nil, nil. Reading is tolerant of garbage: blank
// lines, surrounding whitespace and \r are dropped, and duplicates collapse
// to their first occurrence, so a legacy single-id file reads as a
// one-element list.
func (s *MarkdownStorage) ReadCurrentTasks(user string) ([]string, error) {
	if err := task.ValidateNameSegment("user", user); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.CurrentTaskPath(user))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(line)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// WriteCurrentTasks stores user's current-task list, one id per line,
// atomically. An empty list removes the file, which is not an error.
func (s *MarkdownStorage) WriteCurrentTasks(user string, ids []string) error {
	if err := task.ValidateNameSegment("user", user); err != nil {
		return err
	}
	if len(ids) == 0 {
		if err := os.Remove(s.CurrentTaskPath(user)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id)
		b.WriteByte('\n')
	}
	return writeFileAtomic(s.CurrentTaskPath(user), []byte(b.String()))
}
