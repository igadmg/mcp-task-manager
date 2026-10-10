package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

func branchedTask(id string) *task.Task {
	tk := makeTestTask(0)
	tk.ID = id
	tk.Title = "Task " + id
	tk.Status = task.StatusInProgress
	tk.Branch = "dev/wip/" + id
	tk.BaseBranch = "main_patched"
	tk.StartCommit = "1111111111111111111111111111111111111111"
	tk.FinalBranch = "dev/" + id
	tk.SquashCommit = "2222222222222222222222222222222222222222"
	return tk
}

func TestSaveParseBranchFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	want := branchedTask("7")
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load("7")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, f := range []struct{ name, got, want string }{
		{"Branch", got.Branch, want.Branch},
		{"BaseBranch", got.BaseBranch, want.BaseBranch},
		{"StartCommit", got.StartCommit, want.StartCommit},
		{"FinalBranch", got.FinalBranch, want.FinalBranch},
		{"SquashCommit", got.SquashCommit, want.SquashCommit},
	} {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, f.got, f.want)
		}
	}
}

func TestSaveOmitsEmptyBranchFields(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	if err := s.Save(makeTestTask(3)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "3", "3.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, key := range []string{"branch:", "base_branch:", "start_commit:", "final_branch:", "squash_commit:"} {
		if strings.Contains(string(data), key) {
			t.Errorf("record without branch data contains %q; existing files must stay byte-stable:\n%s", key, data)
		}
	}
}

func TestIndexEntryCarriesBranchFields(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	parent := makeTestTask(1)
	child := branchedTask("2")
	child.ParentID = parent.ID
	for _, tk := range []*task.Task{parent, child} {
		if err := s.Save(tk); err != nil {
			t.Fatalf("Save(%s) error = %v", tk.ID, err)
		}
	}
	idx := NewIndex(dir, s)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var fromAll *task.Task
	for _, tk := range idx.All() {
		if tk.ID == child.ID {
			fromAll = tk
		}
	}
	subtasks := idx.GetSubtasks(parent.ID)
	if fromAll == nil || len(subtasks) != 1 {
		t.Fatalf("All() found child = %v, GetSubtasks() = %d entries; want the child in both", fromAll != nil, len(subtasks))
	}

	for name, got := range map[string]*task.Task{"All": fromAll, "GetSubtasks": subtasks[0]} {
		if got.Branch != child.Branch || got.FinalBranch != child.FinalBranch || got.SquashCommit != child.SquashCommit {
			t.Errorf("%s: branch fields = (%q, %q, %q), want (%q, %q, %q)", name,
				got.Branch, got.FinalBranch, got.SquashCommit,
				child.Branch, child.FinalBranch, child.SquashCommit)
		}
		if got.BaseBranch != "" || got.StartCommit != "" {
			t.Errorf("%s: BaseBranch = %q, StartCommit = %q; want both empty, they stay out of the index", name, got.BaseBranch, got.StartCommit)
		}
	}
}

func TestUsersDirIgnoredByScans(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	if err := s.Save(makeTestTask(1)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := s.WriteCurrentTasks("dev", []string{"1"}); err != nil {
		t.Fatalf("WriteCurrentTasks() error = %v", err)
	}
	// A crafted record exactly where a task scan would look for task ".users".
	rec := "---\nid: .users\ntitle: crafted\nstatus: todo\npriority: high\ntype: feature\n---\n"
	if err := os.WriteFile(filepath.Join(dir, UsersDirName, UsersDirName+".md"), []byte(rec), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tasks, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "1" {
		ids := make([]string, 0, len(tasks))
		for _, tk := range tasks {
			ids = append(ids, tk.ID)
		}
		t.Errorf("LoadAll() ids = %v, want [1]", ids)
	}

	idx := NewIndex(dir, s)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	stale, err := idx.isStaleOnDisk()
	if err != nil {
		t.Fatalf("isStaleOnDisk() error = %v", err)
	}
	if stale {
		t.Error("isStaleOnDisk() = true after Load; .users must not count as a task directory, or every query rebuilds")
	}
	if got := idx.NextID(); got != "2" {
		t.Errorf("NextID() = %s, want 2", got)
	}

	if err := s.MigrateFlatLayout(); err != nil {
		t.Fatalf("MigrateFlatLayout() error = %v", err)
	}
	if ids, err := s.ReadCurrentTasks("dev"); err != nil || len(ids) != 1 || ids[0] != "1" {
		t.Errorf("after migration ReadCurrentTasks() = (%q, %v), want ([1], nil)", ids, err)
	}
	if _, err := os.Stat(filepath.Join(dir, UsersDirName, UsersDirName+".md")); err != nil {
		t.Errorf("migration touched .users: %v", err)
	}
}

func TestValidateIDReservesUsers(t *testing.T) {
	s := NewMarkdownStorage(t.TempDir())
	if err := s.ValidateID(UsersDirName); err == nil {
		t.Errorf("ValidateID(%q) = nil, want reserved error", UsersDirName)
	}
	if err := s.ValidateID(".current_task"); err != nil {
		t.Errorf("ValidateID(.current_task) = %v, want nil: only .users is reserved", err)
	}
}

func TestCurrentTaskPointer(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)

	if want := filepath.Join(dir, ".users", "dev", "current_task"); s.CurrentTaskPath("dev") != want {
		t.Errorf("CurrentTaskPath(dev) = %q, want %q", s.CurrentTaskPath("dev"), want)
	}

	if ids, err := s.ReadCurrentTasks("dev"); err != nil || ids != nil {
		t.Errorf("absent pointer: ReadCurrentTasks() = (%q, %v), want (nil, nil)", ids, err)
	}

	// A legacy pointer file holding a single id reads as a one-element list.
	if err := os.MkdirAll(filepath.Join(dir, ".users", "dev"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(s.CurrentTaskPath("dev"), []byte("7\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if ids, err := s.ReadCurrentTasks("dev"); err != nil || len(ids) != 1 || ids[0] != "7" {
		t.Errorf("legacy pointer: ReadCurrentTasks() = (%q, %v), want ([7], nil)", ids, err)
	}

	// Garbage tolerance: blanks, surrounding whitespace and \r are dropped,
	// duplicates collapse to their first occurrence.
	if err := os.WriteFile(s.CurrentTaskPath("dev"), []byte(" 7 \r\n\nmy-feature\r\n7\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if ids, err := s.ReadCurrentTasks("dev"); err != nil || len(ids) != 2 || ids[0] != "7" || ids[1] != "my-feature" {
		t.Errorf("messy pointer: ReadCurrentTasks() = (%q, %v), want ([7 my-feature], nil)", ids, err)
	}

	if err := s.WriteCurrentTasks("dev", []string{"7", "my-feature"}); err != nil {
		t.Fatalf("WriteCurrentTasks() error = %v", err)
	}
	if ids, err := s.ReadCurrentTasks("dev"); err != nil || len(ids) != 2 || ids[0] != "7" || ids[1] != "my-feature" {
		t.Errorf("ReadCurrentTasks() = (%q, %v), want ([7 my-feature], nil)", ids, err)
	}
	if _, err := os.Stat(s.CurrentTaskPath("dev") + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: stat err = %v", err)
	}

	// An empty list removes the file; removing an absent one is not an error.
	for i := 0; i < 2; i++ {
		if err := s.WriteCurrentTasks("dev", nil); err != nil {
			t.Errorf("WriteCurrentTasks(nil) #%d error = %v", i+1, err)
		}
	}
	if _, err := os.Stat(s.CurrentTaskPath("dev")); !os.IsNotExist(err) {
		t.Errorf("pointer file still there after an empty write: stat err = %v", err)
	}
	if ids, err := s.ReadCurrentTasks("dev"); err != nil || ids != nil {
		t.Errorf("removed pointer: ReadCurrentTasks() = (%q, %v), want (nil, nil)", ids, err)
	}

	for _, user := range []string{"a/b", `a\b`, "..", "", "  "} {
		if err := s.WriteCurrentTasks(user, []string{"1"}); err == nil {
			t.Errorf("WriteCurrentTasks(%q) = nil, want rejection", user)
		}
		if _, err := s.ReadCurrentTasks(user); err == nil {
			t.Errorf("ReadCurrentTasks(%q) error = nil, want rejection", user)
		}
	}
}
