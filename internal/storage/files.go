package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validatePathSegment holds the three checks common to both attached
// filenames and task ids: non-empty (after trim), no path separator, not
// a ".." traversal segment. label is used only to word the error message
// ("filename" vs "task id").
func validatePathSegment(label, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%s %q must not contain a path separator", label, name)
	}
	if name == ".." {
		return fmt.Errorf("%s %q is not allowed", label, name)
	}
	return nil
}

// validateFilename rejects filenames that are empty, contain a path
// separator or a ".." traversal segment, or (when checkReserved is true)
// collide with the task's own reserved "{id}.md" record file. Attached
// filenames are caller-controlled and joined directly into a filesystem
// path, so this is the one validation this feature needs.
func validateFilename(id string, filename string, checkReserved bool) error {
	if err := validatePathSegment("filename", filename); err != nil {
		return err
	}
	if checkReserved && filename == fmt.Sprintf("%s.md", id) {
		return fmt.Errorf("filename %q is reserved for the task record itself", filename)
	}
	return nil
}

// reservedTaskIDs are literal ids this package's own on-disk layout already
// gives a meaning to, so a custom id must not collide with them.
var reservedTaskIDs = map[string]string{
	"0":           `reserved as the "top-level" filter sentinel`,
	"archive":     "reserved for the archive subdirectory",
	".index.json": "reserved: the filename of the retired index cache file",
}

// ValidateID checks a task id is safe to use as a directory/file name and
// does not collide with a name this package's layout already reserves.
// It does not check uniqueness against existing tasks - see Exists /
// ArchiveStorage.IsArchived for that (requires I/O, format doesn't).
func (s *MarkdownStorage) ValidateID(id string) error {
	if err := validatePathSegment("task id", id); err != nil {
		return err
	}
	if reason, ok := reservedTaskIDs[id]; ok {
		return fmt.Errorf("task id %q is reserved (%s)", id, reason)
	}
	return nil
}

// Exists reports whether an active task directory with this id exists.
func (s *MarkdownStorage) Exists(id string) bool {
	_, err := os.Stat(s.taskPath(id))
	return err == nil
}

// WriteFile creates or overwrites a named file attached to an active task.
func (s *MarkdownStorage) WriteFile(taskID string, filename, content string) error {
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
func (s *MarkdownStorage) ReadFile(taskID string, filename string) (string, error) {
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
			return "", fmt.Errorf("file %q not found on task %s", filename, taskID)
		}
		return "", err
	}
	return string(data), nil
}

// ListFiles returns the names of all files attached to a task, excluding
// the task's own {id}.md record file.
func (s *MarkdownStorage) ListFiles(taskID string) ([]string, error) {
	dir, err := s.resolveTaskDir(taskID)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	recordName := fmt.Sprintf("%s.md", taskID)
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
func (s *MarkdownStorage) resolveTaskDir(taskID string) (string, error) {
	if _, err := os.Stat(s.taskPath(taskID)); err == nil {
		return s.taskDir(taskID), nil
	}
	if _, err := os.Stat(s.archivePath(taskID)); err == nil {
		return s.archiveTaskDir(taskID), nil
	}
	return "", fmt.Errorf("task not found: %s", taskID)
}
