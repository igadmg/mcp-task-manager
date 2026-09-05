package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateFilename rejects filenames that are empty, contain a path
// separator or a ".." traversal segment, or (when checkReserved is true)
// collide with the task's own reserved "{id}.md" record file. Attached
// filenames are caller-controlled and joined directly into a filesystem
// path, so this is the one validation this feature needs.
func validateFilename(id int, filename string, checkReserved bool) error {
	trimmed := strings.TrimSpace(filename)
	if trimmed == "" {
		return fmt.Errorf("filename cannot be empty")
	}
	if strings.ContainsAny(filename, "/\\") {
		return fmt.Errorf("filename %q must not contain a path separator", filename)
	}
	if filename == ".." {
		return fmt.Errorf("filename %q is not allowed", filename)
	}
	if checkReserved && filename == fmt.Sprintf("%03d.md", id) {
		return fmt.Errorf("filename %q is reserved for the task record itself", filename)
	}
	return nil
}

// WriteFile creates or overwrites a named file attached to an active task.
func (s *MarkdownStorage) WriteFile(taskID int, filename, content string) error {
	if err := validateFilename(taskID, filename, true); err != nil {
		return err
	}

	dir := s.taskDir(taskID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, filename)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ReadFile returns the content of a named file attached to a task,
// resolving whether the task is active or archived by checking which
// directory actually holds it.
func (s *MarkdownStorage) ReadFile(taskID int, filename string) (string, error) {
	if err := validateFilename(taskID, filename, false); err != nil {
		return "", err
	}

	dir, err := s.resolveTaskDir(taskID)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file %q not found on task %d", filename, taskID)
		}
		return "", err
	}
	return string(data), nil
}

// ListFiles returns the names of all files attached to a task, excluding
// the task's own {id}.md record file.
func (s *MarkdownStorage) ListFiles(taskID int) ([]string, error) {
	dir, err := s.resolveTaskDir(taskID)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	recordName := fmt.Sprintf("%03d.md", taskID)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == recordName {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// resolveTaskDir returns the task's active directory if it exists there,
// falling back to the archive directory, or an error if the task's record
// file is not found in either location.
func (s *MarkdownStorage) resolveTaskDir(taskID int) (string, error) {
	if _, err := os.Stat(s.taskPath(taskID)); err == nil {
		return s.taskDir(taskID), nil
	}
	if _, err := os.Stat(s.archivePath(taskID)); err == nil {
		return s.archiveTaskDir(taskID), nil
	}
	return "", fmt.Errorf("task not found: %d", taskID)
}
