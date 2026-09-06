package storage

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
)

var flatTaskFilePattern = regexp.MustCompile(`^(\d+)\.md$`)

// MigrateFlatLayout detects tasks still stored in the old flat file layout
// (tasks/{id}.md, tasks/archive/{id}.md) and moves them into the new
// per-task directory layout (tasks/{id}/{id}.md, tasks/archive/{id}/{id}.md).
//
// It is safe to call on every startup: once a flat file is migrated it no
// longer exists, so the trigger condition (a flat file being present) never
// matches it again, making this idempotent and safe to resume after a crash
// mid-migration. A per-task failure is logged and skipped rather than
// aborting the whole migration, so one bad task can never block startup.
func (s *MarkdownStorage) MigrateFlatLayout() error {
	s.migrateFlatDir(s.dir, s.taskDir, s.taskPath)
	s.migrateFlatDir(filepath.Join(s.dir, "archive"), s.archiveTaskDir, s.archivePath)
	return nil
}

func (s *MarkdownStorage) migrateFlatDir(dir string, dirFor func(string) string, pathFor func(string) string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Missing directory (nothing to migrate yet) or unreadable; either
		// way there is nothing safe to do here, and Save/EnsureDir elsewhere
		// are responsible for ever creating this directory.
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := flatTaskFilePattern.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		id := m[1]
		if id == "0" {
			continue
		}

		oldPath := filepath.Join(dir, entry.Name())
		if err := os.MkdirAll(dirFor(id), 0755); err != nil {
			log.Printf("migrate flat layout: failed to create directory for task %s: %v", id, err)
			continue
		}
		if err := os.Rename(oldPath, pathFor(id)); err != nil {
			log.Printf("migrate flat layout: failed to move task %s into new layout: %v", id, err)
			continue
		}
	}
}
