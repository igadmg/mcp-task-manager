package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

func TestMarkdownStorage_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC().Truncate(time.Second)
	original := &task.Task{
		ID:          "1",
		Title:       "Test Task",
		Description: "This is a test description.\n\nWith multiple paragraphs.",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "feature",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save
	if err := storage.Save(original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(dir, "1", "1.md")); os.IsNotExist(err) {
		t.Fatal("expected 1/1.md to exist")
	}

	// Load
	loaded, err := storage.Load("1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Compare fields
	if loaded.ID != original.ID {
		t.Errorf("ID = %s, want %s", loaded.ID, original.ID)
	}
	if loaded.Title != original.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, original.Title)
	}
	if loaded.Description != original.Description {
		t.Errorf("Description = %q, want %q", loaded.Description, original.Description)
	}
	if loaded.Status != original.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, original.Status)
	}
	if loaded.Priority != original.Priority {
		t.Errorf("Priority = %q, want %q", loaded.Priority, original.Priority)
	}
	if loaded.Type != original.Type {
		t.Errorf("Type = %q, want %q", loaded.Type, original.Type)
	}
}

func TestMarkdownStorage_SaveCreatesTaskDir(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC()
	tk := &task.Task{ID: "7", Title: "Nested", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now}
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	wantPath := filepath.Join(dir, "7", "7.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected %s to exist, got error: %v", wantPath, err)
	}
}

func TestMarkdownStorage_Delete(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	task := &task.Task{
		ID:        "1",
		Title:     "To Delete",
		Status:    task.StatusTodo,
		Priority:  task.PriorityMedium,
		Type:      "bug",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := storage.Save(task); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := storage.Delete("1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := storage.Load("1"); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted, got error: %v", err)
	}
}

func TestMarkdownStorage_Delete_RemovesWholeDirectory(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	tk := makeTestTask(1)
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	attached := filepath.Join(dir, "1", "notes.md")
	if err := os.WriteFile(attached, []byte("attached content"), 0644); err != nil {
		t.Fatalf("failed to write attached file: %v", err)
	}

	if err := storage.Delete("1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "1")); !os.IsNotExist(err) {
		t.Error("expected task directory (including attached files) to be removed after Delete()")
	}
}

func TestMarkdownStorage_LoadAll(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC()
	tasks := []*task.Task{
		{ID: "1", Title: "Task 1", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now},
		{ID: "2", Title: "Task 2", Status: task.StatusDone, Priority: task.PriorityLow, Type: "bug", CreatedAt: now, UpdatedAt: now},
		{ID: "3", Title: "Task 3", Status: task.StatusInProgress, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now, UpdatedAt: now},
	}

	for _, tk := range tasks {
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	loaded, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	if len(loaded) != 3 {
		t.Errorf("LoadAll() returned %d tasks, want 3", len(loaded))
	}
}

func TestMarkdownStorage_LoadAll_MixedCustomAndNumericIDs(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC()
	tasks := []*task.Task{
		{ID: "1", Title: "Numeric", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now},
		{ID: "foo", Title: "Custom", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	loaded, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("LoadAll() returned %d tasks, want 2", len(loaded))
	}

	ids := map[string]bool{}
	for _, tk := range loaded {
		ids[tk.ID] = true
	}
	if !ids["1"] || !ids["foo"] {
		t.Errorf("LoadAll() = %v, want both a numeric and a custom id present", ids)
	}
}

func TestMarkdownStorage_LoadAll_SkipsArchiveDirectory(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC()
	tk := &task.Task{ID: "1", Title: "Active", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now}
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	archived := &task.Task{ID: "2", Title: "Archived", Status: task.StatusDone, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now}
	if err := storage.Save(archived); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := storage.Archive("2"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	loaded, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "1" {
		t.Errorf("LoadAll() = %v, want only the active task (archive/ subdirectory must be skipped)", loaded)
	}
}

func TestMarkdownStorage_NextID(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		storage := NewMarkdownStorage(dir)

		id, err := storage.NextID()
		if err != nil {
			t.Fatalf("NextID() error = %v", err)
		}
		if id != 1 {
			t.Errorf("NextID() on empty dir = %d, want 1", id)
		}
	})

	t.Run("active tasks only", func(t *testing.T) {
		dir := t.TempDir()
		storage := NewMarkdownStorage(dir)

		now := time.Now().UTC()
		for _, i := range []int{1, 5, 3} {
			tk := &task.Task{ID: strconv.Itoa(i), Title: "Task", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now, UpdatedAt: now}
			if err := storage.Save(tk); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
		}

		id, err := storage.NextID()
		if err != nil {
			t.Fatalf("NextID() error = %v", err)
		}
		if id != 6 {
			t.Errorf("NextID() = %d, want 6", id)
		}
	})

	t.Run("archived tasks only", func(t *testing.T) {
		dir := t.TempDir()
		storage := NewMarkdownStorage(dir)

		for _, i := range []int{2, 8, 4} {
			tk := makeTestTask(i)
			if err := storage.Save(tk); err != nil {
				t.Fatalf("Save(%d) error = %v", i, err)
			}
			if err := storage.Archive(strconv.Itoa(i)); err != nil {
				t.Fatalf("Archive(%d) error = %v", i, err)
			}
		}

		id, err := storage.NextID()
		if err != nil {
			t.Fatalf("NextID() error = %v", err)
		}
		if id != 9 {
			t.Errorf("NextID() = %d, want 9", id)
		}
	})

	t.Run("archived task has highest id", func(t *testing.T) {
		dir := t.TempDir()
		storage := NewMarkdownStorage(dir)

		for _, i := range []int{1, 3} {
			tk := makeTestTask(i)
			if err := storage.Save(tk); err != nil {
				t.Fatalf("Save(%d) error = %v", i, err)
			}
		}
		tk := makeTestTask(10)
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save(10) error = %v", err)
		}
		if err := storage.Archive("10"); err != nil {
			t.Fatalf("Archive(10) error = %v", err)
		}

		id, err := storage.NextID()
		if err != nil {
			t.Fatalf("NextID() error = %v", err)
		}
		if id != 11 {
			t.Errorf("NextID() = %d, want 11", id)
		}
	})

	t.Run("active task has highest id", func(t *testing.T) {
		dir := t.TempDir()
		storage := NewMarkdownStorage(dir)

		for _, i := range []int{2, 5} {
			tk := makeTestTask(i)
			if err := storage.Save(tk); err != nil {
				t.Fatalf("Save(%d) error = %v", i, err)
			}
			if err := storage.Archive(strconv.Itoa(i)); err != nil {
				t.Fatalf("Archive(%d) error = %v", i, err)
			}
		}
		tk := makeTestTask(9)
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save(9) error = %v", err)
		}

		id, err := storage.NextID()
		if err != nil {
			t.Fatalf("NextID() error = %v", err)
		}
		if id != 10 {
			t.Errorf("NextID() = %d, want 10", id)
		}
	})
}

func TestIndex_SetGetDelete(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	tk := &task.Task{
		ID:        "1",
		Title:     "Test",
		Status:    task.StatusTodo,
		Priority:  task.PriorityHigh,
		Type:      "feature",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Save to disk first (required for Get to work)
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Set
	idx.Set(tk)

	// Get
	got, ok := idx.Get("1")
	if !ok {
		t.Fatal("Get() returned false for existing task")
	}
	if got.Title != tk.Title {
		t.Errorf("Get() title = %q, want %q", got.Title, tk.Title)
	}

	// Delete. The index mirrors the task directories, so the file has to go
	// too - dropping only the entry leaves a divergence that the next query
	// detects and heals by rebuilding from disk.
	if err := storage.Delete("1"); err != nil {
		t.Fatalf("storage.Delete() error = %v", err)
	}
	idx.Delete("1")
	_, ok = idx.Get("1")
	if ok {
		t.Error("Get() returned true for deleted task")
	}
}

func TestIndex_Filter(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	tasks := []*task.Task{
		{ID: "1", Title: "Task 1", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now},
		{ID: "2", Title: "Task 2", Status: task.StatusDone, Priority: task.PriorityLow, Type: "bug", CreatedAt: now, UpdatedAt: now},
		{ID: "3", Title: "Task 3", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		idx.Set(tk)
	}

	// Filter by status
	todoStatus := task.StatusTodo
	filtered := idx.Filter(&todoStatus, nil, nil, nil)
	if len(filtered) != 2 {
		t.Errorf("Filter by todo status returned %d tasks, want 2", len(filtered))
	}

	// Filter by priority
	highPriority := task.PriorityHigh
	filtered = idx.Filter(nil, &highPriority, nil, nil)
	if len(filtered) != 1 {
		t.Errorf("Filter by high priority returned %d tasks, want 1", len(filtered))
	}

	// Filter by type
	featureType := "feature"
	filtered = idx.Filter(nil, nil, &featureType, nil)
	if len(filtered) != 2 {
		t.Errorf("Filter by feature type returned %d tasks, want 2", len(filtered))
	}

	// Combined filter
	filtered = idx.Filter(&todoStatus, nil, &featureType, nil)
	if len(filtered) != 2 {
		t.Errorf("Combined filter returned %d tasks, want 2", len(filtered))
	}
}

func TestIndex_NextTodo(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Empty index
	if got := idx.NextTodo(); got != nil {
		t.Errorf("NextTodo() on empty index = %v, want nil", got)
	}

	now := time.Now().UTC()
	tasks := []*task.Task{
		{ID: "1", Title: "Low", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "feature", CreatedAt: now, UpdatedAt: now},
		{ID: "2", Title: "Critical", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "bug", CreatedAt: now.Add(time.Hour), UpdatedAt: now},
		{ID: "3", Title: "Critical Older", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "feature", CreatedAt: now, UpdatedAt: now},
		{ID: "4", Title: "Done", Status: task.StatusDone, Priority: task.PriorityCritical, Type: "bug", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		idx.Set(tk)
	}

	// Should return Critical Older (highest priority, oldest)
	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "3" {
		t.Errorf("NextTodo() ID = %s, want 3 (Critical Older)", next.ID)
	}
}

func TestIndex_NextID(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Empty
	if got := idx.NextID(); got != "1" {
		t.Errorf("NextID() on empty = %s, want 1", got)
	}

	// Add tasks
	now := time.Now().UTC()
	idx.Set(&task.Task{ID: "5", Title: "Task", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now, UpdatedAt: now})
	idx.Set(&task.Task{ID: "3", Title: "Task", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now, UpdatedAt: now})

	if got := idx.NextID(); got != "6" {
		t.Errorf("NextID() = %s, want 6", got)
	}
}

func TestIndex_NextIDIncludesArchivedTasks(t *testing.T) {
	tests := []struct {
		name     string
		activeID int
		want     string
	}{
		{name: "no active entries", want: "79"},
		{name: "archived task has highest id", activeID: 12, want: "79"},
		{name: "active task has highest id", activeID: 90, want: "91"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			storage := NewMarkdownStorage(dir)
			idx := NewIndex(dir, storage)

			if err := storage.Save(makeTestTask(78)); err != nil {
				t.Fatalf("Save(78) error = %v", err)
			}
			if err := storage.Archive("78"); err != nil {
				t.Fatalf("Archive(78) error = %v", err)
			}

			if tt.activeID != 0 {
				idx.Set(makeTestTask(tt.activeID))
			}

			if got := idx.NextID(); got != tt.want {
				t.Errorf("NextID() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestIndex_NextID_IgnoresNonNumericCustomIDs(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	for _, id := range []string{"foo", "1", "2", "3"} {
		idx.Set(&task.Task{ID: id, Title: "T", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})
	}

	if got := idx.NextID(); got != "4" {
		t.Errorf("NextID() = %s, want 4 (custom id %q must not affect the numeric max)", got, "foo")
	}
}

func TestIndex_NextID_AccountsForNumericLookingCustomID(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	idx.Set(&task.Task{ID: "5", Title: "Custom numeric-looking", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})

	if got := idx.NextID(); got != "6" {
		t.Errorf(`NextID() after a numeric-looking custom id "5" = %s, want 6`, got)
	}
}

func TestIndex_All_OrdersIDsNumerically(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	for _, id := range []string{"10", "2", "1"} {
		idx.Set(&task.Task{ID: id, Title: "T", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})
	}

	all := idx.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d tasks, want 3", len(all))
	}
	got := []string{all[0].ID, all[1].ID, all[2].ID}
	want := []string{"1", "2", "10"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("All() order = %v, want %v (numeric, not lexicographic)", got, want)
		}
	}
}

func TestService_CreateUsesArchivedTaskIDs(t *testing.T) {
	tests := []struct {
		name     string
		activeID int
	}{
		{name: "archived task has highest id"},
		{name: "mixed active and archived tasks", activeID: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasksDir := t.TempDir()
			storageBackend := NewMarkdownStorage(tasksDir)

			if tt.activeID != 0 {
				active := makeTestTask(tt.activeID)
				active.Status = task.StatusTodo
				active.Title = "Active Task"
				if err := storageBackend.Save(active); err != nil {
					t.Fatalf("Save(%d) error = %v", tt.activeID, err)
				}
			}

			archived := makeTestTask(78)
			archived.Title = "Archived Task"
			if err := storageBackend.Save(archived); err != nil {
				t.Fatalf("Save(78) error = %v", err)
			}
			if err := storageBackend.Archive("78"); err != nil {
				t.Fatalf("Archive(78) error = %v", err)
			}

			idx := NewIndex(tasksDir, storageBackend)
			cfg := &config.Config{TaskTypes: []string{"feature", "bug"}, ProjectFound: true}
			svc := task.NewService(storageBackend, storageBackend, storageBackend, idx, cfg.TaskTypes, cfg)
			if err := svc.Initialize(); err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}

			created, err := svc.Create("New Task", "Description", task.PriorityHigh, "bug", "", "")
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if created.ID != "79" {
				t.Fatalf("Create() ID = %s, want 79", created.ID)
			}

			gotArchived, err := svc.Get("78")
			if err != nil {
				t.Fatalf("Get(78) error = %v", err)
			}
			if gotArchived.Title != "Archived Task" {
				t.Fatalf("Get(78) title = %q, want %q", gotArchived.Title, "Archived Task")
			}

			gotCreated, err := svc.Get("79")
			if err != nil {
				t.Fatalf("Get(79) error = %v", err)
			}
			if gotCreated.Title != "New Task" {
				t.Fatalf("Get(79) title = %q, want %q", gotCreated.Title, "New Task")
			}
			if gotCreated.Description != "Description" {
				t.Fatalf("Get(79) description = %q, want %q", gotCreated.Description, "Description")
			}
		})
	}
}

func TestIndex_NextTodoBreaksTiesByLowerID(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	idx.Set(&task.Task{ID: "5", Title: "Later ID", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})
	idx.Set(&task.Task{ID: "3", Title: "Earlier ID", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "3" {
		t.Errorf("NextTodo() ID = %s, want 3 (lower ID wins when priority and CreatedAt match)", next.ID)
	}
}

func TestIndex_NextTodo_UsesParentOrderingForTodoParentSubtasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	base := time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC)
	parentID := "1"
	tasks := []*task.Task{
		{
			ID:        "1",
			Title:     "Older Parent",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base,
			UpdatedAt: base,
		},
		{
			ID:        "2",
			ParentID:  parentID,
			Title:     "Child Of Older Parent",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base.Add(4 * time.Hour),
			UpdatedAt: base.Add(4 * time.Hour),
		},
		{
			ID:        "3",
			Title:     "Newer Standalone",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base.Add(2 * time.Hour),
			UpdatedAt: base.Add(2 * time.Hour),
		},
	}

	for _, tk := range tasks {
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (child of older equal-priority parent)", next.ID)
	}
}

func TestIndex_NextTodo_UsesParentOrderingBeforeChildPriority(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	base := time.Date(2025, time.January, 2, 9, 0, 0, 0, time.UTC)
	parentID := "1"
	tasks := []*task.Task{
		{
			ID:        "1",
			Title:     "Critical Parent",
			Status:    task.StatusTodo,
			Priority:  task.PriorityCritical,
			Type:      "feature",
			CreatedAt: base,
			UpdatedAt: base,
		},
		{
			ID:        "2",
			ParentID:  parentID,
			Title:     "Low Priority Child",
			Status:    task.StatusTodo,
			Priority:  task.PriorityLow,
			Type:      "feature",
			CreatedAt: base.Add(6 * time.Hour),
			UpdatedAt: base.Add(6 * time.Hour),
		},
		{
			ID:        "3",
			Title:     "High Priority Standalone",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base.Add(3 * time.Hour),
			UpdatedAt: base.Add(3 * time.Hour),
		},
	}

	for _, tk := range tasks {
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (child of higher-priority parent wins before child priority)", next.ID)
	}
}

func TestIndex_NextTodo_SortsWinningParentChildrenAfterParentSelection(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	base := time.Date(2025, time.January, 3, 9, 0, 0, 0, time.UTC)
	parentID := "1"
	tasks := []*task.Task{
		{
			ID:        "1",
			Title:     "Winning Parent",
			Status:    task.StatusTodo,
			Priority:  task.PriorityCritical,
			Type:      "feature",
			CreatedAt: base,
			UpdatedAt: base,
		},
		{
			ID:        "2",
			ParentID:  parentID,
			Title:     "Medium Child",
			Status:    task.StatusTodo,
			Priority:  task.PriorityMedium,
			Type:      "feature",
			CreatedAt: base.Add(1 * time.Hour),
			UpdatedAt: base.Add(1 * time.Hour),
		},
		{
			ID:        "6",
			ParentID:  parentID,
			Title:     "High Child Later",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base.Add(4 * time.Hour),
			UpdatedAt: base.Add(4 * time.Hour),
		},
		{
			ID:        "5",
			ParentID:  parentID,
			Title:     "High Child Same Time Higher ID",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base.Add(2 * time.Hour),
			UpdatedAt: base.Add(2 * time.Hour),
		},
		{
			ID:        "4",
			ParentID:  parentID,
			Title:     "High Child Same Time Lower ID",
			Status:    task.StatusTodo,
			Priority:  task.PriorityHigh,
			Type:      "feature",
			CreatedAt: base.Add(2 * time.Hour),
			UpdatedAt: base.Add(2 * time.Hour),
		},
		{
			ID:        "7",
			Title:     "Competing Standalone",
			Status:    task.StatusTodo,
			Priority:  task.PriorityCritical,
			Type:      "feature",
			CreatedAt: base.Add(8 * time.Hour),
			UpdatedAt: base.Add(8 * time.Hour),
		},
	}

	for _, tk := range tasks {
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "4" {
		t.Errorf("NextTodo() ID = %s, want 4 (winning parent's children sorted by priority, CreatedAt, then ID)", next.ID)
	}
}

func TestIndex_FreshIndexSeesTasksOnDisk(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	tk := &task.Task{
		ID:        "1",
		Title:     "Persisted",
		Status:    task.StatusTodo,
		Priority:  task.PriorityHigh,
		Type:      "feature",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	idx.Set(tk)

	// A second index over the same directory must reconstruct the same state
	// from the task folders alone - there is no shared file between them.
	idx2 := NewIndex(dir, storage)
	if err := idx2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, ok := idx2.Get("1")
	if !ok {
		t.Fatal("Get() after Load() returned false")
	}
	if got.Title != tk.Title {
		t.Errorf("Title after Load() = %q, want %q", got.Title, tk.Title)
	}
}

func TestService_Initialize_EmptyProjectDoesNotCreateTasksDirOrIndex(t *testing.T) {
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")

	storageBackend := NewMarkdownStorage(tasksDir)
	idx := NewIndex(tasksDir, storageBackend)
	cfg := &config.Config{
		TaskTypes:    []string{"feature", "bug"},
		ProjectFound: true,
	}
	svc := task.NewService(storageBackend, nil, storageBackend, idx, cfg.TaskTypes, cfg)

	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if _, err := os.Stat(tasksDir); !os.IsNotExist(err) {
		t.Fatalf("expected tasks directory to remain absent, got err = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tasksDir, ".index.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .index.json file, got err = %v", err)
	}
}

func TestIndex_Load_MissingTasksDirDoesNotCreateIndexFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks")
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := idx.All(); len(got) != 0 {
		t.Fatalf("All() returned %d tasks, want 0", len(got))
	}

	if _, err := os.Stat(filepath.Join(dir, ".index.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .index.json file, got err = %v", err)
	}
}

func TestIndex_Load_EmptyTasksDirDoesNotCreateIndexFile(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := idx.All(); len(got) != 0 {
		t.Fatalf("All() returned %d tasks, want 0", len(got))
	}

	if _, err := os.Stat(filepath.Join(dir, ".index.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .index.json file, got err = %v", err)
	}
}

func TestIndex_Load_RemovesLegacyIndexFile(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC().Truncate(time.Second)
	if err := storage.Save(&task.Task{ID: "1", Title: "Real Task", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// A cache left behind by an older build, claiming something else entirely.
	legacy := `{"git_commit":"","tasks":[{"id":"99","title":"Stale","status":"done","priority":"low","type":"bug"}]}`
	legacyPath := filepath.Join(dir, ".index.json")
	if err := os.WriteFile(legacyPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("failed to write legacy index: %v", err)
	}

	idx := NewIndex(dir, storage)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("expected legacy .index.json to be removed, got err = %v", err)
	}

	entry, ok := idx.GetEntry("1")
	if !ok {
		t.Fatal("GetEntry(1) not found; index must come from the task folders")
	}
	if entry.Title != "Real Task" {
		t.Errorf("GetEntry(1).Title = %q, want %q", entry.Title, "Real Task")
	}
	if _, ok := idx.GetEntry("99"); ok {
		t.Error("task 99 exists only in the legacy cache and must not be loaded")
	}
}

func TestIndex_PicksUpExternalTaskFileEdit(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC().Truncate(time.Second)
	tk := &task.Task{ID: "1", Title: "Before", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now}
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	idx := NewIndex(dir, storage)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Rewrite the task behind the index's back, as a git pull or a hand edit
	// would. The mtime bump is what the staleness check keys on.
	tk.Title = "After"
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "1", "1.md"), future, future); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	entry, ok := idx.GetEntry("1")
	if !ok {
		t.Fatal("GetEntry(1) returned false")
	}
	if entry.Title != "After" {
		t.Errorf("GetEntry(1).Title = %q, want %q (external edit must be picked up)", entry.Title, "After")
	}
}

func TestService_WritePathNeverCreatesIndexFile(t *testing.T) {
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")

	storageBackend := NewMarkdownStorage(tasksDir)
	idx := NewIndex(tasksDir, storageBackend)
	cfg := &config.Config{
		TaskTypes:     []string{"feature", "bug"},
		RelationTypes: config.DefaultRelationTypes,
		ProjectFound:  true,
	}
	svc := task.NewService(storageBackend, storageBackend, storageBackend, idx, cfg.TaskTypes, cfg)

	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	first, err := svc.Create("First", "body", task.PriorityHigh, "feature", "", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second, err := svc.Create("Second", "body", task.PriorityLow, "bug", "", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := svc.AddRelation(second.ID, "blocked_by", first.ID); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}
	title := "Renamed"
	if _, err := svc.Update(first.ID, &title, nil, nil, nil, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := svc.Delete(second.ID, false); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tasksDir, ".index.json")); !os.IsNotExist(err) {
		t.Fatalf("no write path may create .index.json, got err = %v", err)
	}

	got, err := svc.Get(first.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != title {
		t.Errorf("Get().Title = %q, want %q", got.Title, title)
	}
}

func TestMarkdownStorage_SaveLoad_WithParentID(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	parentID := "1"
	tsk := &task.Task{
		ID:        "2",
		ParentID:  parentID,
		Title:     "Subtask",
		Status:    task.StatusTodo,
		Priority:  task.PriorityHigh,
		Type:      "feature",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := storage.Save(tsk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := storage.Load("2")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.ParentID == "" {
		t.Fatal("ParentID should not be empty")
	}
	if loaded.ParentID != "1" {
		t.Errorf("ParentID = %s, want 1", loaded.ParentID)
	}
}

func TestMarkdownStorage_SaveLoad_WithoutParentID(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	tsk := &task.Task{
		ID:        "1",
		ParentID:  "",
		Title:     "Top-level task",
		Status:    task.StatusTodo,
		Priority:  task.PriorityHigh,
		Type:      "feature",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := storage.Save(tsk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := storage.Load("1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.ParentID != "" {
		t.Error("ParentID should be empty for top-level task")
	}
}

func TestIndex_RebuildFromFiles(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	// Save tasks directly to storage
	now := time.Now().UTC()
	tasks := []*task.Task{
		{ID: "1", Title: "Task 1", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now},
		{ID: "2", Title: "Task 2", Status: task.StatusDone, Priority: task.PriorityLow, Type: "bug", CreatedAt: now, UpdatedAt: now},
	}
	for _, tk := range tasks {
		if err := storage.Save(tk); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	// Create index without existing index file (triggers rebuild)
	idx := NewIndex(dir, storage)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify tasks were loaded
	all := idx.All()
	if len(all) != 2 {
		t.Errorf("All() after rebuild returned %d tasks, want 2", len(all))
	}
}

func TestIndex_GetSubtasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Create parent task
	parent := &task.Task{ID: "1", Title: "Parent", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	storage.Save(parent)

	// Create subtasks
	parentID := "1"
	sub1 := &task.Task{ID: "2", ParentID: parentID, Title: "Sub 1", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	sub2 := &task.Task{ID: "3", ParentID: parentID, Title: "Sub 2", Status: task.StatusDone, Priority: task.PriorityMedium, Type: "feature"}
	storage.Save(sub1)
	storage.Save(sub2)

	idx.Load()

	subtasks := idx.GetSubtasks("1")
	if len(subtasks) != 2 {
		t.Errorf("GetSubtasks(1) = %d, want 2", len(subtasks))
	}

	// Non-existent parent
	subtasks = idx.GetSubtasks("99")
	if len(subtasks) != 0 {
		t.Errorf("GetSubtasks(99) = %d, want 0", len(subtasks))
	}
}

func TestIndex_HasSubtasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	parent := &task.Task{ID: "1", Title: "Parent", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	storage.Save(parent)

	parentID := "1"
	sub := &task.Task{ID: "2", ParentID: parentID, Title: "Sub", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	storage.Save(sub)

	idx.Load()

	if !idx.HasSubtasks("1") {
		t.Error("HasSubtasks(1) = false, want true")
	}
	if idx.HasSubtasks("2") {
		t.Error("HasSubtasks(2) = true, want false")
	}
}

func TestIndex_SubtaskCounts(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	parent := &task.Task{ID: "1", Title: "Parent", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	storage.Save(parent)

	parentID := "1"
	sub1 := &task.Task{ID: "2", ParentID: parentID, Title: "Sub 1", Status: task.StatusDone, Priority: task.PriorityHigh, Type: "feature"}
	sub2 := &task.Task{ID: "3", ParentID: parentID, Title: "Sub 2", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature"}
	sub3 := &task.Task{ID: "4", ParentID: parentID, Title: "Sub 3", Status: task.StatusDone, Priority: task.PriorityLow, Type: "feature"}
	storage.Save(sub1)
	storage.Save(sub2)
	storage.Save(sub3)

	idx.Load()

	total, done := idx.SubtaskCounts("1")
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if done != 2 {
		t.Errorf("done = %d, want 2", done)
	}
}

func TestIndex_NextTodo_SkipsParentsWithSubtasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Create parent with subtask
	parent := &task.Task{ID: "1", Title: "Parent", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "feature", CreatedAt: time.Now()}
	storage.Save(parent)

	parentID := "1"
	sub := &task.Task{ID: "2", ParentID: parentID, Title: "Subtask", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: time.Now()}
	storage.Save(sub)

	// Create standalone task (lower priority)
	standalone := &task.Task{ID: "3", Title: "Standalone", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "feature", CreatedAt: time.Now()}
	storage.Save(standalone)

	idx.Load()

	// Should return subtask (skipping parent even though it's higher priority)
	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (subtask)", next.ID)
	}
}

func TestIndex_NextTodo_ReturnsParentWithoutSubtasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Create parent without subtasks
	parent := &task.Task{ID: "1", Title: "Parent", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "feature", CreatedAt: time.Now()}
	storage.Save(parent)

	idx.Load()

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "1" {
		t.Errorf("NextTodo() ID = %s, want 1", next.ID)
	}
}

func TestIndex_NextTodo_PrioritizesInProgressParentSubtasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()

	// Create in_progress parent with todo subtasks
	inProgressParent := &task.Task{ID: "1", Title: "In Progress Parent", Status: task.StatusInProgress, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now}
	storage.Save(inProgressParent)

	parentID1 := "1"
	subLowPriority := &task.Task{ID: "2", ParentID: parentID1, Title: "Low Priority Subtask", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "feature", CreatedAt: now}
	subHighPriority := &task.Task{ID: "3", ParentID: parentID1, Title: "High Priority Subtask", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now}
	storage.Save(subLowPriority)
	storage.Save(subHighPriority)

	// Create standalone critical task (highest priority but should be deprioritized)
	criticalStandalone := &task.Task{ID: "4", Title: "Critical Standalone", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "feature", CreatedAt: now}
	storage.Save(criticalStandalone)

	idx.Load()

	// Should return high priority subtask of in_progress parent, not the critical standalone
	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "3" {
		t.Errorf("NextTodo() ID = %s, want 3 (high priority subtask of in_progress parent)", next.ID)
	}

	// Complete the high priority subtask
	entry3, _ := idx.GetEntry("3")
	completedTask3 := entryToTask(entry3)
	completedTask3.Status = task.StatusDone
	idx.Set(completedTask3)

	// Now should return low priority subtask of in_progress parent
	next = idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil after completing high priority subtask")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (low priority subtask of in_progress parent)", next.ID)
	}

	// Complete the low priority subtask too
	entry2, _ := idx.GetEntry("2")
	completedTask2 := entryToTask(entry2)
	completedTask2.Status = task.StatusDone
	idx.Set(completedTask2)

	// Now should return the critical standalone
	next = idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil after completing all subtasks")
	}
	if next.ID != "4" {
		t.Errorf("NextTodo() ID = %s, want 4 (critical standalone)", next.ID)
	}
}

func TestIndex_NextTodo_PrioritizesInProgressStandaloneTask(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()

	inProgress := &task.Task{
		ID:        "1",
		Title:     "Resume me",
		Status:    task.StatusInProgress,
		Priority:  task.PriorityLow,
		Type:      "feature",
		CreatedAt: now.Add(2 * time.Hour),
	}
	competingTodo := &task.Task{
		ID:        "2",
		Title:     "Fresh todo",
		Status:    task.StatusTodo,
		Priority:  task.PriorityCritical,
		Type:      "feature",
		CreatedAt: now,
	}

	if err := storage.Save(inProgress); err != nil {
		t.Fatalf("Save(inProgress) error = %v", err)
	}
	if err := storage.Save(competingTodo); err != nil {
		t.Fatalf("Save(competingTodo) error = %v", err)
	}
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "1" {
		t.Errorf("NextTodo() ID = %s, want 1 (in-progress standalone task)", next.ID)
	}
}

func TestIndex_NextTodo_PrioritizesInProgressSubtaskOverTodoWork(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()

	inProgressParent := &task.Task{
		ID:        "1",
		Title:     "Started parent",
		Status:    task.StatusInProgress,
		Priority:  task.PriorityMedium,
		Type:      "feature",
		CreatedAt: now.Add(2 * time.Hour),
	}
	parentID := "1"
	inProgressSubtask := &task.Task{
		ID:        "2",
		ParentID:  parentID,
		Title:     "Resume started subtask",
		Status:    task.StatusInProgress,
		Priority:  task.PriorityLow,
		Type:      "feature",
		CreatedAt: now.Add(3 * time.Hour),
	}
	competingTodoSibling := &task.Task{
		ID:        "3",
		ParentID:  parentID,
		Title:     "Not started sibling",
		Status:    task.StatusTodo,
		Priority:  task.PriorityHigh,
		Type:      "feature",
		CreatedAt: now.Add(4 * time.Hour),
	}
	competingStandalone := &task.Task{
		ID:        "4",
		Title:     "Critical todo",
		Status:    task.StatusTodo,
		Priority:  task.PriorityCritical,
		Type:      "feature",
		CreatedAt: now,
	}

	if err := storage.Save(inProgressParent); err != nil {
		t.Fatalf("Save(inProgressParent) error = %v", err)
	}
	if err := storage.Save(inProgressSubtask); err != nil {
		t.Fatalf("Save(inProgressSubtask) error = %v", err)
	}
	if err := storage.Save(competingTodoSibling); err != nil {
		t.Fatalf("Save(competingTodoSibling) error = %v", err)
	}
	if err := storage.Save(competingStandalone); err != nil {
		t.Fatalf("Save(competingStandalone) error = %v", err)
	}
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (in-progress subtask)", next.ID)
	}
}

func TestIndex_NextTodo_TodoParentSubtasksNotPrioritized(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()

	// Create todo parent with subtasks (parent not started yet)
	todoParent := &task.Task{ID: "1", Title: "Todo Parent", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "feature", CreatedAt: now}
	storage.Save(todoParent)

	parentID1 := "1"
	subHighPriority := &task.Task{ID: "2", ParentID: parentID1, Title: "High Priority Subtask", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now}
	storage.Save(subHighPriority)

	// Create standalone medium task
	mediumStandalone := &task.Task{ID: "3", Title: "Medium Standalone", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now}
	storage.Save(mediumStandalone)

	idx.Load()

	// Should return high priority subtask (normal priority sorting, not boosted)
	// because parent is still in todo state, not in_progress
	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (high priority subtask, normal sort order)", next.ID)
	}
}

func TestIndex_Filter_ByParentID(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	parent := &task.Task{ID: "1", Title: "Parent", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	storage.Save(parent)

	parentID := "1"
	sub1 := &task.Task{ID: "2", ParentID: parentID, Title: "Sub 1", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature"}
	sub2 := &task.Task{ID: "3", ParentID: parentID, Title: "Sub 2", Status: task.StatusDone, Priority: task.PriorityMedium, Type: "feature"}
	storage.Save(sub1)
	storage.Save(sub2)

	standalone := &task.Task{ID: "4", Title: "Standalone", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "feature"}
	storage.Save(standalone)

	idx.Load()

	// Filter subtasks of parent
	result := idx.Filter(nil, nil, nil, &parentID)
	if len(result) != 2 {
		t.Errorf("Filter(parent_id=1) = %d, want 2", len(result))
	}

	// Filter top-level only (parent_id = 0 means top-level)
	topLevel := "0"
	result = idx.Filter(nil, nil, nil, &topLevel)
	if len(result) != 2 {
		t.Errorf("Filter(parent_id=0) = %d, want 2 (parent + standalone)", len(result))
	}

	// No filter - returns all tasks
	result = idx.Filter(nil, nil, nil, nil)
	if len(result) != 4 {
		t.Errorf("Filter(parent_id=nil) = %d, want 4", len(result))
	}
}

// Test that GetEntry returns metadata without loading from disk
func TestIndex_GetEntry_ReturnsMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	tk := &task.Task{
		ID:          "1",
		Title:       "Test Task",
		Description: "This description should not be in the entry",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "feature",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Set the task in the index
	idx.Set(tk)

	// GetEntry should return entry with metadata but no description
	entry, ok := idx.GetEntry("1")
	if !ok {
		t.Fatal("GetEntry() returned false for existing task")
	}
	if entry.ID != "1" {
		t.Errorf("entry.ID = %s, want 1", entry.ID)
	}
	if entry.Title != "Test Task" {
		t.Errorf("entry.Title = %q, want %q", entry.Title, "Test Task")
	}
	if entry.Status != task.StatusTodo {
		t.Errorf("entry.Status = %v, want %v", entry.Status, task.StatusTodo)
	}

	// GetEntry should return false for non-existent task
	_, ok = idx.GetEntry("999")
	if ok {
		t.Error("GetEntry() returned true for non-existent task")
	}
}

// Test that Get loads full task from disk (with description)
func TestIndex_Get_LoadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	tk := &task.Task{
		ID:          "1",
		Title:       "Test Task",
		Description: "Full description from disk",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "feature",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save to disk
	if err := storage.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Set in index (this should store entry, not full task)
	idx.Set(tk)

	// Get should load from disk and include description
	loaded, ok := idx.Get("1")
	if !ok {
		t.Fatal("Get() returned false for existing task")
	}
	if loaded.Description != "Full description from disk" {
		t.Errorf("Get() description = %q, want %q", loaded.Description, "Full description from disk")
	}
	if loaded.Title != "Test Task" {
		t.Errorf("Get() title = %q, want %q", loaded.Title, "Test Task")
	}
}

// Test that All returns tasks without descriptions
func TestIndex_All_ReturnsTasksWithoutDescriptions(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()
	tk1 := &task.Task{
		ID:          "1",
		Title:       "Task 1",
		Description: "Description 1",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "feature",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tk2 := &task.Task{
		ID:          "2",
		Title:       "Task 2",
		Description: "Description 2",
		Status:      task.StatusDone,
		Priority:    task.PriorityLow,
		Type:        "bug",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	idx.Set(tk1)
	idx.Set(tk2)

	all := idx.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d tasks, want 2", len(all))
	}

	// Descriptions should be empty
	for _, tk := range all {
		if tk.Description != "" {
			t.Errorf("All() task %s has description %q, want empty", tk.ID, tk.Description)
		}
	}

	// Other fields should be populated
	if all[0].Title != "Task 1" {
		t.Errorf("All() task 1 title = %q, want %q", all[0].Title, "Task 1")
	}
	if all[1].Title != "Task 2" {
		t.Errorf("All() task 2 title = %q, want %q", all[1].Title, "Task 2")
	}
}

// === Relation Tests ===

func TestMarkdownStorage_SaveLoad_WithRelations(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC().Truncate(time.Second)
	tsk := &task.Task{
		ID:       "5",
		Title:    "Blocked task",
		Status:   task.StatusTodo,
		Priority: task.PriorityHigh,
		Type:     "feature",
		Relations: []task.Relation{
			{Type: "blocked_by", Task: "3"},
			{Type: "relates_to", Task: "7"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := storage.Save(tsk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := storage.Load("5")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.Relations) != 2 {
		t.Fatalf("Relations count = %d, want 2", len(loaded.Relations))
	}
	if loaded.Relations[0].Type != "blocked_by" || loaded.Relations[0].Task != "3" {
		t.Errorf("Relations[0] = %v, want {blocked_by, 3}", loaded.Relations[0])
	}
	if loaded.Relations[1].Type != "relates_to" || loaded.Relations[1].Task != "7" {
		t.Errorf("Relations[1] = %v, want {relates_to, 7}", loaded.Relations[1])
	}
}

func TestMarkdownStorage_SaveLoad_WithoutRelations(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC().Truncate(time.Second)
	tsk := &task.Task{
		ID:        "1",
		Title:     "Plain task",
		Status:    task.StatusTodo,
		Priority:  task.PriorityHigh,
		Type:      "feature",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := storage.Save(tsk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify the file doesn't contain "relations" field
	data, err := os.ReadFile(filepath.Join(dir, "1", "1.md"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if strings.Contains(string(data), "relations:") {
		t.Errorf("File should not contain 'relations:' field when empty, got:\n%s", string(data))
	}

	loaded, err := storage.Load("1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Relations) != 0 {
		t.Errorf("Relations should be empty, got %v", loaded.Relations)
	}
}

func TestIndex_AddRemoveRelation(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Add a relation
	edge := task.RelationEdge{Type: "blocked_by", Source: "5", Target: "3"}
	idx.AddRelation(edge)

	// Verify it's in the index
	relations := idx.GetRelationsForTask("5")
	if len(relations) != 1 {
		t.Fatalf("GetRelationsForTask(5) = %d, want 1", len(relations))
	}
	if relations[0].Type != "blocked_by" || relations[0].Target != "3" {
		t.Errorf("relation = %v, want {blocked_by, 5, 3}", relations[0])
	}

	// Target task should also see the relation
	relations = idx.GetRelationsForTask("3")
	if len(relations) != 1 {
		t.Fatalf("GetRelationsForTask(3) = %d, want 1", len(relations))
	}

	// Remove the relation
	idx.RemoveRelation(edge)
	relations = idx.GetRelationsForTask("5")
	if len(relations) != 0 {
		t.Errorf("GetRelationsForTask(5) after remove = %d, want 0", len(relations))
	}
}

func TestIndex_SymmetricRelation(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Add a relates_to relation (symmetric)
	edge := task.RelationEdge{Type: "relates_to", Source: "5", Target: "7"}
	idx.AddRelation(edge)

	// Both tasks should see the relation
	rel5 := idx.GetRelationsForTask("5")
	rel7 := idx.GetRelationsForTask("7")

	if len(rel5) != 2 { // original + reverse
		t.Errorf("GetRelationsForTask(5) = %d, want 2 (original + reverse)", len(rel5))
	}
	if len(rel7) != 2 { // original + reverse
		t.Errorf("GetRelationsForTask(7) = %d, want 2 (original + reverse)", len(rel7))
	}

	// Remove the relation - should remove both edges
	idx.RemoveRelation(edge)
	rel5 = idx.GetRelationsForTask("5")
	rel7 = idx.GetRelationsForTask("7")
	if len(rel5) != 0 {
		t.Errorf("GetRelationsForTask(5) after remove = %d, want 0", len(rel5))
	}
	if len(rel7) != 0 {
		t.Errorf("GetRelationsForTask(7) after remove = %d, want 0", len(rel7))
	}
}

func TestIndex_GetBlockers(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Add blocked_by relations
	idx.AddRelation(task.RelationEdge{Type: "blocked_by", Source: "5", Target: "3"})
	idx.AddRelation(task.RelationEdge{Type: "blocked_by", Source: "5", Target: "8"})
	idx.AddRelation(task.RelationEdge{Type: "relates_to", Source: "5", Target: "10"})

	blockers := idx.GetBlockers("5")
	if len(blockers) != 2 {
		t.Fatalf("GetBlockers(5) = %d, want 2", len(blockers))
	}

	// Task 3 should not have any blockers
	blockers = idx.GetBlockers("3")
	if len(blockers) != 0 {
		t.Errorf("GetBlockers(3) = %d, want 0", len(blockers))
	}
}

func TestIndex_RemoveAllRelationsForTask(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	// Add multiple relations involving task 5
	idx.AddRelation(task.RelationEdge{Type: "blocked_by", Source: "5", Target: "3"})
	idx.AddRelation(task.RelationEdge{Type: "relates_to", Source: "5", Target: "7"})
	idx.AddRelation(task.RelationEdge{Type: "blocked_by", Source: "10", Target: "5"})

	removed := idx.RemoveAllRelationsForTask("5")
	if len(removed) < 3 { // at least 3 edges: blocked_by 5->3, relates_to 5->7, relates_to 7->5 (reverse), blocked_by 10->5
		t.Logf("removed %d edges", len(removed))
	}

	// Task 5 should have no relations
	rel5 := idx.GetRelationsForTask("5")
	if len(rel5) != 0 {
		t.Errorf("GetRelationsForTask(5) after remove all = %d, want 0", len(rel5))
	}

	// Task 3 should have no relations pointing at 5
	rel3 := idx.GetRelationsForTask("3")
	if len(rel3) != 0 {
		t.Errorf("GetRelationsForTask(3) after remove all = %d, want 0", len(rel3))
	}
}

func TestIndex_NextTodo_SkipsBlockedTasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()

	// Create tasks: task 1 (blocker, todo), task 2 (blocked by 1, high priority), task 3 (low priority)
	task1 := &task.Task{ID: "1", Title: "Blocker", Status: task.StatusTodo, Priority: task.PriorityMedium, Type: "feature", CreatedAt: now, UpdatedAt: now}
	task2 := &task.Task{ID: "2", Title: "Blocked", Status: task.StatusTodo, Priority: task.PriorityCritical, Type: "feature", CreatedAt: now, UpdatedAt: now}
	task3 := &task.Task{ID: "3", Title: "Unblocked", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "feature", CreatedAt: now, UpdatedAt: now}

	storage.Save(task1)
	storage.Save(task2)
	storage.Save(task3)

	idx.Set(task1)
	idx.Set(task2)
	idx.Set(task3)

	// Block task 2 by task 1
	idx.AddRelation(task.RelationEdge{Type: "blocked_by", Source: "2", Target: "1"})

	// NextTodo should skip task 2 (blocked) and return task 1 (medium priority, higher than task 3)
	next := idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil")
	}
	if next.ID != "1" {
		t.Errorf("NextTodo() ID = %s, want 1 (blocker task, medium priority)", next.ID)
	}

	// Mark task 1 as done - now task 2 should be unblocked
	task1.Status = task.StatusDone
	idx.Set(task1)

	next = idx.NextTodo()
	if next == nil {
		t.Fatal("NextTodo() returned nil after unblocking")
	}
	if next.ID != "2" {
		t.Errorf("NextTodo() ID = %s, want 2 (now unblocked, critical priority)", next.ID)
	}
}

func TestIndex_RebuildWithRelations(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC()

	// Create tasks with relations in frontmatter
	task1 := &task.Task{ID: "1", Title: "Blocker", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now}
	task2 := &task.Task{
		ID: "2", Title: "Blocked", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature",
		Relations: []task.Relation{{Type: "blocked_by", Task: "1"}},
		CreatedAt: now, UpdatedAt: now,
	}
	task3 := &task.Task{
		ID: "3", Title: "Related", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature",
		Relations: []task.Relation{{Type: "relates_to", Task: "1"}},
		CreatedAt: now, UpdatedAt: now,
	}

	storage.Save(task1)
	storage.Save(task2)
	storage.Save(task3)

	// Create new index and rebuild from files
	idx := NewIndex(dir, storage)
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	// Verify blocked_by edge
	blockers := idx.GetBlockers("2")
	if len(blockers) != 1 || blockers[0] != "1" {
		t.Errorf("GetBlockers(2) = %v, want [1]", blockers)
	}

	// Verify relates_to generates symmetric edges
	rel1 := idx.GetRelationsForTask("1")
	hasRelatesToFrom1 := false
	hasRelatesToFrom3 := false
	for _, r := range rel1 {
		if r.Type == "relates_to" && r.Source == "1" && r.Target == "3" {
			hasRelatesToFrom1 = true
		}
		if r.Type == "relates_to" && r.Source == "3" && r.Target == "1" {
			hasRelatesToFrom3 = true
		}
	}
	if !hasRelatesToFrom1 {
		t.Error("expected reverse relates_to edge (1->3) in task 1 relations")
	}
	if !hasRelatesToFrom3 {
		t.Error("expected original relates_to edge (3->1) in task 1 relations")
	}
}

func TestIndex_FreshIndexRebuildsRelationsFromTaskFiles(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)

	now := time.Now().UTC()
	storage.Save(&task.Task{ID: "1", Title: "Blocker", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})
	storage.Save(&task.Task{ID: "3", Title: "Related", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature", CreatedAt: now, UpdatedAt: now})
	storage.Save(&task.Task{
		ID: "2", Title: "Blocked", Status: task.StatusTodo, Priority: task.PriorityHigh, Type: "feature",
		Relations: []task.Relation{
			{Type: "blocked_by", Task: "1"},
			{Type: "relates_to", Task: "3"},
		},
		CreatedAt: now, UpdatedAt: now,
	})

	idx := NewIndex(dir, storage)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	blockers := idx.GetBlockers("2")
	if len(blockers) != 1 || blockers[0] != "1" {
		t.Errorf("GetBlockers(2) = %v, want [1]", blockers)
	}

	// The reverse edge of a symmetric relation is derived, not stored.
	var hasReverse bool
	for _, e := range idx.GetRelationsForTask("3") {
		if e.Type == "relates_to" && e.Source == "3" && e.Target == "2" {
			hasReverse = true
		}
	}
	if !hasReverse {
		t.Error("expected derived reverse relates_to edge (3->2)")
	}
}

func TestIndex_Integration_FullFlow(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	now := time.Now().UTC()

	// Create tasks with descriptions
	task1 := &task.Task{
		ID:          "1",
		Title:       "Task 1",
		Description: "Long description for task 1 that should NOT be in index",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "feature",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	task2 := &task.Task{
		ID:          "2",
		Title:       "Task 2",
		Description: "Another long description",
		Status:      task.StatusDone,
		Priority:    task.PriorityLow,
		Type:        "bug",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save to both storage and index
	if err := storage.Save(task1); err != nil {
		t.Fatalf("storage.Save() error = %v", err)
	}
	if err := storage.Save(task2); err != nil {
		t.Fatalf("storage.Save() error = %v", err)
	}
	idx.Set(task1)
	idx.Set(task2)

	// Load fresh index
	idx2 := NewIndex(dir, storage)
	if err := idx2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// All() should return tasks without descriptions
	all := idx2.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d tasks, want 2", len(all))
	}
	for _, tk := range all {
		if tk.Description != "" {
			t.Errorf("All() task %s has description %q, want empty", tk.ID, tk.Description)
		}
	}

	// Get() should return full task with description
	full, ok := idx2.Get("1")
	if !ok {
		t.Fatal("Get(1) returned false")
	}
	if full.Description != "Long description for task 1 that should NOT be in index" {
		t.Errorf("Get(1) description = %q, want full description", full.Description)
	}

	// Filter() should return tasks without descriptions
	todoStatus := task.StatusTodo
	filtered := idx2.Filter(&todoStatus, nil, nil, nil)
	if len(filtered) != 1 {
		t.Fatalf("Filter() returned %d tasks, want 1", len(filtered))
	}
	if filtered[0].Description != "" {
		t.Errorf("Filter() task has description %q, want empty", filtered[0].Description)
	}
}

func TestIndex_AutoRebuildsWhenDiskHasNewTasks(t *testing.T) {
	dir := t.TempDir()
	storage := NewMarkdownStorage(dir)
	idx := NewIndex(dir, storage)

	parent := &task.Task{
		ID:          "67",
		Title:       "Parent",
		Description: "parent description",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "bug",
		CreatedAt:   time.Date(2026, 4, 13, 19, 2, 52, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 13, 19, 2, 52, 0, time.UTC),
	}
	if err := storage.Save(parent); err != nil {
		t.Fatalf("storage.Save(parent) error = %v", err)
	}
	idx.Set(parent)

	subtask := &task.Task{
		ID:          "68",
		ParentID:    parent.ID,
		Title:       "Child",
		Description: "child description",
		Status:      task.StatusTodo,
		Priority:    task.PriorityHigh,
		Type:        "bug",
		CreatedAt:   time.Date(2026, 4, 13, 19, 7, 5, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 13, 19, 7, 5, 0, time.UTC),
	}
	if err := storage.Save(subtask); err != nil {
		t.Fatalf("storage.Save(subtask) error = %v", err)
	}

	got, ok := idx.Get(subtask.ID)
	if !ok {
		t.Fatalf("Get(%s) returned false, want auto-rebuilt task", subtask.ID)
	}
	if got.ParentID != parent.ID {
		t.Fatalf("Get(%s) parent_id = %v, want %s", subtask.ID, got.ParentID, parent.ID)
	}

	filtered := idx.Filter(nil, nil, nil, &parent.ID)
	if len(filtered) != 1 {
		t.Fatalf("Filter(parent_id=%s) returned %d tasks, want 1", parent.ID, len(filtered))
	}
	if filtered[0].ID != subtask.ID {
		t.Fatalf("Filter(parent_id=%s) returned task %s, want %s", parent.ID, filtered[0].ID, subtask.ID)
	}

	if nextID := idx.NextID(); nextID != "69" {
		t.Fatalf("NextID() = %s, want 69 after auto-rebuild", nextID)
	}
}

func makeTestTask(id int) *task.Task {
	now := time.Now().UTC().Truncate(time.Second)
	return &task.Task{
		ID:          strconv.Itoa(id),
		Title:       fmt.Sprintf("Task %d", id),
		Description: fmt.Sprintf("Description for task %d", id),
		Status:      task.StatusDone,
		Priority:    task.PriorityMedium,
		Type:        "feature",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestMarkdownStorage_Archive(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify directory exists before archiving
	if _, err := os.Stat(filepath.Join(dir, "1", "1.md")); os.IsNotExist(err) {
		t.Fatal("expected 1/1.md to exist before archiving")
	}

	// Archive the task
	if err := s.Archive("1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Verify original directory is gone
	if _, err := os.Stat(filepath.Join(dir, "1")); !os.IsNotExist(err) {
		t.Error("expected 1/ to be removed after archiving")
	}

	// Verify directory exists in archive dir
	if _, err := os.Stat(filepath.Join(dir, "archive", "1", "1.md")); os.IsNotExist(err) {
		t.Error("expected archive/1/1.md to exist after archiving")
	}
}

func TestMarkdownStorage_Archive_MovesWholeDirectory(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	attached := filepath.Join(dir, "1", "notes.md")
	if err := os.WriteFile(attached, []byte("attached content"), 0644); err != nil {
		t.Fatalf("failed to write attached file: %v", err)
	}

	if err := s.Archive("1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	archivedAttached := filepath.Join(dir, "archive", "1", "notes.md")
	data, err := os.ReadFile(archivedAttached)
	if err != nil {
		t.Fatalf("expected attached file to survive archiving at %s, got error: %v", archivedAttached, err)
	}
	if string(data) != "attached content" {
		t.Errorf("attached file content = %q, want %q", string(data), "attached content")
	}
}

func TestMarkdownStorage_Archive_NonExistent(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	err := s.Archive("99")
	if err == nil {
		t.Error("Archive() of non-existent task should return error")
	}
}

func TestMarkdownStorage_LoadArchived(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	tk := makeTestTask(2)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := s.Archive("2"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	loaded, err := s.LoadArchived("2")
	if err != nil {
		t.Fatalf("LoadArchived() error = %v", err)
	}

	if loaded.ID != tk.ID {
		t.Errorf("ID = %s, want %s", loaded.ID, tk.ID)
	}
	if loaded.Title != tk.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, tk.Title)
	}
	if loaded.Description != tk.Description {
		t.Errorf("Description = %q, want %q", loaded.Description, tk.Description)
	}
}

func TestMarkdownStorage_LoadArchived_NonExistent(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	_, err := s.LoadArchived("99")
	if err == nil {
		t.Error("LoadArchived() of non-existent archived task should return error")
	}
}

func TestMarkdownStorage_LoadAllArchived(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	// Save and archive three tasks
	for i := 1; i <= 3; i++ {
		tk := makeTestTask(i)
		if err := s.Save(tk); err != nil {
			t.Fatalf("Save(%d) error = %v", i, err)
		}
		if err := s.Archive(strconv.Itoa(i)); err != nil {
			t.Fatalf("Archive(%d) error = %v", i, err)
		}
	}

	// Save a non-archived task
	tk4 := makeTestTask(4)
	if err := s.Save(tk4); err != nil {
		t.Fatalf("Save(4) error = %v", err)
	}

	archived, err := s.LoadAllArchived()
	if err != nil {
		t.Fatalf("LoadAllArchived() error = %v", err)
	}

	if len(archived) != 3 {
		t.Errorf("LoadAllArchived() returned %d tasks, want 3", len(archived))
	}

	ids := make(map[string]bool)
	for _, at := range archived {
		ids[at.ID] = true
	}
	for i := 1; i <= 3; i++ {
		if !ids[strconv.Itoa(i)] {
			t.Errorf("expected archived task %d to be present", i)
		}
	}
	if ids["4"] {
		t.Error("non-archived task 4 should not be in LoadAllArchived() result")
	}
}

func TestMarkdownStorage_LoadAllArchived_Empty(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	archived, err := s.LoadAllArchived()
	if err != nil {
		t.Fatalf("LoadAllArchived() on empty archive error = %v", err)
	}
	if len(archived) != 0 {
		t.Errorf("LoadAllArchived() returned %d tasks, want 0", len(archived))
	}
}

func TestMarkdownStorage_IsArchived(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	tk := makeTestTask(5)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Before archiving
	if s.IsArchived("5") {
		t.Error("IsArchived(5) = true before archiving, want false")
	}

	if err := s.Archive("5"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// After archiving
	if !s.IsArchived("5") {
		t.Error("IsArchived(5) = false after archiving, want true")
	}

	// Non-existent ID
	if s.IsArchived("999") {
		t.Error("IsArchived(999) = true for non-existent task, want false")
	}
}

func TestMarkdownStorage_Exists(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	if s.Exists("5") {
		t.Error("Exists(5) = true before the task is saved, want false")
	}

	tk := makeTestTask(5)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !s.Exists("5") {
		t.Error("Exists(5) = false after Save(), want true")
	}

	if err := s.Archive("5"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if s.Exists("5") {
		t.Error("Exists(5) = true for an archived-only task, want false (that is IsArchived's job)")
	}
}

func TestMarkdownStorage_ValidateID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"forward slash", "foo/bar", true},
		{"backslash", "foo\\bar", true},
		{"dotdot", "..", true},
		{"reserved zero", "0", true},
		{"reserved archive", "archive", true},
		{"reserved index file", ".index.json", true},
		{"normal custom id", "my-feature", false},
		{"numeric-looking id", "42", false},
	}

	s := NewMarkdownStorage(t.TempDir())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.ValidateID(tt.id)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateID(%q) = nil, want error", tt.id)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateID(%q) = %v, want nil", tt.id, err)
			}
		})
	}
}

func TestMarkdownStorage_WriteFile_CreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := s.WriteFile("1", "notes.md", "first"); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := s.ReadFile("1", "notes.md")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got != "first" {
		t.Errorf("ReadFile() = %q, want %q", got, "first")
	}

	if err := s.WriteFile("1", "notes.md", "second"); err != nil {
		t.Fatalf("WriteFile() overwrite error = %v", err)
	}
	got, err = s.ReadFile("1", "notes.md")
	if err != nil {
		t.Fatalf("ReadFile() after overwrite error = %v", err)
	}
	if got != "second" {
		t.Errorf("ReadFile() after overwrite = %q, want %q", got, "second")
	}
}

func TestMarkdownStorage_ReadFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := s.ReadFile("1", "missing.md"); err == nil {
		t.Fatal("expected ReadFile() to error for a file that was never written")
	}
}

func TestMarkdownStorage_ReadFile_TaskNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	if _, err := s.ReadFile("999", "notes.md"); err == nil {
		t.Fatal("expected ReadFile() to error for a task that does not exist")
	}
}

func TestMarkdownStorage_ListFiles_ExcludesTaskMarkdown(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := s.WriteFile("1", "notes.md", "a"); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := s.WriteFile("1", "design.md", "b"); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	names, err := s.ListFiles("1")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("ListFiles() returned %d names, want 2: %v", len(names), names)
	}
	for _, n := range names {
		if n == "001.md" {
			t.Error("ListFiles() should exclude the task's own record file")
		}
	}
}

func TestMarkdownStorage_ListFiles_EmptyWhenNoAttachedFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	names, err := s.ListFiles("1")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(names) != 0 {
		t.Errorf("ListFiles() = %v, want empty", names)
	}
}

func TestMarkdownStorage_ReadListFiles_WorkOnArchivedTask(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := s.WriteFile("1", "notes.md", "content"); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := s.Archive("1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	got, err := s.ReadFile("1", "notes.md")
	if err != nil {
		t.Fatalf("ReadFile() on archived task error = %v", err)
	}
	if got != "content" {
		t.Errorf("ReadFile() on archived task = %q, want %q", got, "content")
	}

	names, err := s.ListFiles("1")
	if err != nil {
		t.Fatalf("ListFiles() on archived task error = %v", err)
	}
	if len(names) != 1 || names[0] != "notes.md" {
		t.Errorf("ListFiles() on archived task = %v, want [notes.md]", names)
	}
}

func TestMarkdownStorage_WriteFile_RejectsInvalidFilenames(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	tk := makeTestTask(1)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	invalid := []string{"", "   ", "../escape.md", "sub/dir.md", "sub\\dir.md", "..", "1.md"}
	for _, name := range invalid {
		if err := s.WriteFile("1", name, "x"); err == nil {
			t.Errorf("WriteFile(%q) expected error, got nil", name)
		}
	}
}
