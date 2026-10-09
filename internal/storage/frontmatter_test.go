package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// writeRecord drops a record on disk by hand, the way a user or a git pull
// does, so a test can assert what the server makes of keys it never wrote.
func writeRecord(t *testing.T, dir, id, content string) string {
	t.Helper()
	path := filepath.Join(dir, id, id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const recordWithFields = `---
id: "7"
title: Carry my fields
status: todo
priority: high
type: feature
created_at: 2026-01-15T10:30:00Z
updated_at: 2026-01-15T10:30:00Z
area: web
complexity: high
count: 3
flag: true
nested:
  x: 1
notes: |
  first
  second
quoted: "3"
---

Body text.
`

func TestFrontmatterRoundTripsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	path := writeRecord(t, dir, "7", recordWithFields)

	loaded, err := s.Load("7")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := task.Fields{
		"area":       "web",
		"complexity": "high",
		"count":      3,
		"flag":       true,
		"nested":     map[string]any{"x": 1},
		"notes":      "first\nsecond\n",
		"quoted":     "3",
	}
	if !reflect.DeepEqual(loaded.Fields, want) {
		t.Fatalf("Fields = %#v, want %#v", loaded.Fields, want)
	}
	if loaded.Description != "Body text." {
		t.Fatalf("Description = %q", loaded.Description)
	}

	// A write carries every one of them back out, values and shapes intact.
	if err := s.Save(loaded); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"area: web",
		"complexity: high",
		"count: 3",
		"flag: true",
		"nested:\n  x: 1",
		"notes: |\n  first\n  second",
		`quoted: "3"`,
	} {
		if !strings.Contains(string(written), want) {
			t.Errorf("written record is missing %q:\n%s", want, written)
		}
	}

	// And a second write is a byte-for-byte no-op: the order the server
	// emits is the order it reads back, so a user's tasks/ commit sees no
	// churn from a flow that changed nothing.
	again, err := s.Load("7")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(again); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(written) {
		t.Errorf("second write differs from the first:\n--- first ---\n%s\n--- second ---\n%s", written, second)
	}
}

func TestFrontmatterWritesOwnedKeysThenFieldsSorted(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	writeRecord(t, dir, "7", recordWithFields)

	loaded, err := s.Load("7")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "7", "7.md"))
	if err != nil {
		t.Fatal(err)
	}

	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "---" && len(keys) > 0 {
			break
		}
		if strings.HasPrefix(line, " ") || !strings.Contains(line, ":") {
			continue
		}
		key, _, _ := strings.Cut(line, ":")
		if key == "---" || key == "" {
			continue
		}
		keys = append(keys, key)
	}

	// The owned keys come first, in the frontmatter struct's declaration
	// order, and the fields follow sorted.
	wantOwned := []string{"id", "title", "status", "priority", "type", "created_at", "updated_at"}
	if len(keys) < len(wantOwned) {
		t.Fatalf("keys = %v", keys)
	}
	if !reflect.DeepEqual(keys[:len(wantOwned)], wantOwned) {
		t.Errorf("owned keys = %v, want %v", keys[:len(wantOwned)], wantOwned)
	}
	wantFields := []string{"area", "complexity", "count", "flag", "nested", "notes", "quoted"}
	if !reflect.DeepEqual(keys[len(wantOwned):], wantFields) {
		t.Errorf("field keys = %v, want %v (sorted)", keys[len(wantOwned):], wantFields)
	}
}

// TestSaveRefusesOwnedFieldKey pins the reason checkExtraKeys exists: yaml.v3
// panics on an inline key that collides with a struct field, and the panic
// escapes its own recover. A Save must answer with an error instead.
func TestSaveRefusesOwnedFieldKey(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	defer func() {
		if v := recover(); v != nil {
			t.Fatalf("Save panicked instead of returning an error: %v", v)
		}
	}()

	err := s.Save(&task.Task{
		ID:     "7",
		Title:  "Colliding",
		Fields: task.Fields{"title": "bogus"},
	})
	if err == nil {
		t.Fatal("Save() error = nil, want an error naming the collision")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("Save() error = %v, want it to name the key", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "7", "7.md")); statErr == nil {
		t.Error("a refused Save still wrote the record")
	}
}

// TestReservedFieldKeysMatchFrontmatter holds task's reserved-key table to
// this package's frontmatter struct. The dependency runs storage -> task, so
// task cannot derive the list itself; this test is what keeps the copy honest
// when a frontmatter key is added (the arrangement StatsFields() uses).
func TestReservedFieldKeysMatchFrontmatter(t *testing.T) {
	reserved := task.ReservedFieldKeys()

	for key := range frontmatterKeys {
		if _, ok := reserved[key]; !ok {
			t.Errorf("frontmatter key %q is not in task.ReservedFieldKeys(); a caller could set it as a field and crash the next write", key)
		}
	}

	// The table may reserve more than the struct owns - "description" is
	// the body, not a frontmatter key - but nothing else.
	extra := map[string]bool{"description": true}
	for key := range reserved {
		if _, ok := frontmatterKeys[key]; ok || extra[key] {
			continue
		}
		t.Errorf("task reserves %q, which is neither a frontmatter key nor a documented exception", key)
	}
}

func TestFrontmatterLoadsRecordWithoutFields(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	writeRecord(t, dir, "8", `---
id: "8"
title: Plain
status: todo
priority: low
type: bug
created_at: 2026-01-15T10:30:00Z
updated_at: 2026-01-15T10:30:00Z
---

Plain body.
`)

	loaded, err := s.Load("8")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Fields != nil {
		t.Errorf("Fields = %#v, want nil for a record with no extra keys", loaded.Fields)
	}
}
