package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeFlatTaskFile writes a task directly in the legacy flat file layout
// (dir/NNN.md), bypassing MarkdownStorage.Save (which only ever writes the
// current nested layout), so migration has something old-format to act on.
func writeFlatTaskFile(t *testing.T, dir string, id int, body string) string {
	t.Helper()
	content := fmt.Sprintf(
		"---\nid: %d\ntitle: Flat Task %d\nstatus: todo\npriority: medium\ntype: feature\ncreated_at: 2026-01-01T00:00:00Z\nupdated_at: 2026-01-01T00:00:00Z\n---\n\n%s",
		id, id, body,
	)
	path := filepath.Join(dir, fmt.Sprintf("%03d.md", id))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write flat task file %s: %v", path, err)
	}
	return path
}

func TestMigrateFlatLayout_ActiveAndArchived(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatalf("failed to create archive dir: %v", err)
	}

	writeFlatTaskFile(t, dir, 1, "active body")
	writeFlatTaskFile(t, archiveDir, 2, "archived body")

	s := NewMarkdownStorage(dir)
	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("MigrateFlatLayout() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "001.md")); !os.IsNotExist(err) {
		t.Error("expected flat tasks/001.md to no longer exist after migration")
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "002.md")); !os.IsNotExist(err) {
		t.Error("expected flat tasks/archive/002.md to no longer exist after migration")
	}

	active, err := s.Load(1)
	if err != nil {
		t.Fatalf("Load(1) after migration error = %v", err)
	}
	if active.Description != "active body" {
		t.Errorf("Load(1).Description = %q, want %q", active.Description, "active body")
	}

	archived, err := s.LoadArchived(2)
	if err != nil {
		t.Fatalf("LoadArchived(2) after migration error = %v", err)
	}
	if archived.Description != "archived body" {
		t.Errorf("LoadArchived(2).Description = %q, want %q", archived.Description, "archived body")
	}
}

func TestMigrateFlatLayout_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeFlatTaskFile(t, dir, 1, "active body")

	s := NewMarkdownStorage(dir)
	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("first MigrateFlatLayout() error = %v", err)
	}
	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("second MigrateFlatLayout() error = %v", err)
	}

	tk, err := s.Load(1)
	if err != nil {
		t.Fatalf("Load(1) after repeated migration error = %v", err)
	}
	if tk.Description != "active body" {
		t.Errorf("Description = %q, want %q", tk.Description, "active body")
	}
}

func TestMigrateFlatLayout_PreservesContent(t *testing.T) {
	dir := t.TempDir()
	flatPath := writeFlatTaskFile(t, dir, 1, "line one\n\nline two")
	original, err := os.ReadFile(flatPath)
	if err != nil {
		t.Fatalf("failed to read original flat file: %v", err)
	}

	s := NewMarkdownStorage(dir)
	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("MigrateFlatLayout() error = %v", err)
	}

	migrated, err := os.ReadFile(filepath.Join(dir, "001", "001.md"))
	if err != nil {
		t.Fatalf("failed to read migrated file: %v", err)
	}
	if string(migrated) != string(original) {
		t.Errorf("migrated content = %q, want byte-identical to original %q", migrated, original)
	}
}

func TestMigrateFlatLayout_NoFlatFiles_NoOp(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("MigrateFlatLayout() error = %v", err)
	}

	loaded, err := s.Load(1)
	if err != nil {
		t.Fatalf("Load(1) after no-op migration error = %v", err)
	}
	if loaded.ID != 1 {
		t.Errorf("Load(1).ID = %d, want 1", loaded.ID)
	}
}

func TestMigrateFlatLayout_SkipsAndLogsOnPerIDCollision(t *testing.T) {
	dir := t.TempDir()

	// Task 1's migration target directory path is occupied by a plain file,
	// so MkdirAll(dir/001) will fail for it - migration must skip it, not abort.
	if err := os.WriteFile(filepath.Join(dir, "001"), []byte("collision"), 0644); err != nil {
		t.Fatalf("failed to create colliding file: %v", err)
	}
	writeFlatTaskFile(t, dir, 1, "should stay flat")
	writeFlatTaskFile(t, dir, 2, "should migrate fine")

	s := NewMarkdownStorage(dir)
	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("MigrateFlatLayout() error = %v, want nil (per-ID failures must not abort migration)", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "001.md")); err != nil {
		t.Error("expected colliding task 001.md to remain in place after a failed per-ID migration")
	}

	tk, err := s.Load(2)
	if err != nil {
		t.Fatalf("Load(2) error = %v, want task 2 to have migrated despite task 1's collision", err)
	}
	if tk.Description != "should migrate fine" {
		t.Errorf("Load(2).Description = %q, want %q", tk.Description, "should migrate fine")
	}
}
