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
	if err := s.WriteCurrentTask("dev", "1"); err != nil {
		t.Fatalf("WriteCurrentTask() error = %v", err)
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
	if id, ok, err := s.ReadCurrentTask("dev"); err != nil || !ok || id != "1" {
		t.Errorf("after migration ReadCurrentTask() = (%q, %v, %v), want (1, true, nil)", id, ok, err)
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

	if id, ok, err := s.ReadCurrentTask("dev"); err != nil || ok || id != "" {
		t.Errorf("absent pointer: ReadCurrentTask() = (%q, %v, %v), want (\"\", false, nil)", id, ok, err)
	}

	for _, id := range []string{"7", "my-feature"} {
		if err := s.WriteCurrentTask("dev", id); err != nil {
			t.Fatalf("WriteCurrentTask(%s) error = %v", id, err)
		}
		got, ok, err := s.ReadCurrentTask("dev")
		if err != nil || !ok || got != id {
			t.Errorf("ReadCurrentTask() = (%q, %v, %v), want (%q, true, nil)", got, ok, err, id)
		}
	}
	if _, err := os.Stat(s.CurrentTaskPath("dev") + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: stat err = %v", err)
	}

	if err := os.WriteFile(s.CurrentTaskPath("dev"), []byte("  \n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if id, ok, err := s.ReadCurrentTask("dev"); err != nil || ok {
		t.Errorf("empty pointer: ReadCurrentTask() = (%q, %v, %v), want ok=false", id, ok, err)
	}

	for i := 0; i < 2; i++ {
		if err := s.RemoveCurrentTask("dev"); err != nil {
			t.Errorf("RemoveCurrentTask() #%d error = %v", i+1, err)
		}
	}
	if _, ok, _ := s.ReadCurrentTask("dev"); ok {
		t.Error("pointer still readable after RemoveCurrentTask")
	}

	for _, user := range []string{"a/b", `a\b`, "..", "", "  "} {
		if err := s.WriteCurrentTask(user, "1"); err == nil {
			t.Errorf("WriteCurrentTask(%q) = nil, want rejection", user)
		}
		if _, _, err := s.ReadCurrentTask(user); err == nil {
			t.Errorf("ReadCurrentTask(%q) error = nil, want rejection", user)
		}
		if err := s.RemoveCurrentTask(user); err == nil {
			t.Errorf("RemoveCurrentTask(%q) = nil, want rejection", user)
		}
	}
}
