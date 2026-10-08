package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// agreementNames covers every shape of attached file name: valid ones,
// empty and whitespace-only, path separators, and the degenerate
// dots-and-spaces names that used to reach os.ReadFile and come back as a
// raw EISDIR error. The expectations are not written down here on purpose -
// the point of the test is that storage and task.ValidateAttachedName judge
// each name the same way, whatever that judgement is.
var agreementNames = []string{
	"notes.md", "research", ".hidden", "..foo", "foo..bar", "notes.",
	" x ", "a b c.md", "Ünïcödé.md",
	"", "   ", "\t", "\n",
	"sub/dir.md", `sub\dir.md`, "../escape.md", "/abs.md",
	".", "..", "...", ". ", " .", " .. ", ". . .",
}

// TestFileNameRulesAgreeWithTaskValidator pins the single source of truth:
// for every name, the exported validator and the storage read and write
// paths reach the same verdict. A valid name that is simply absent comes
// back as "not found", which counts as accepted - the name got past
// validation and reached the filesystem.
func TestFileNameRulesAgreeWithTaskValidator(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	if err := s.Save(makeTestTask(1)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	accepted := map[string]bool{}
	for _, name := range agreementNames {
		if task.ValidateAttachedName(name) == nil {
			accepted[name] = true
		}
		t.Run(name, func(t *testing.T) {
			wantRejected := task.ValidateAttachedName(name) != nil

			if got := rejectedByName(s.WriteFile("1", name, "x")); got != wantRejected {
				t.Errorf("WriteFile(%q) rejected = %v, want %v (validator)", name, got, wantRejected)
			}
			_, readErr := s.ReadFile("1", name)
			if got := rejectedByName(readErr); got != wantRejected {
				t.Errorf("ReadFile(%q) rejected = %v, want %v (validator)", name, got, wantRejected)
			}
		})
	}

	// Nothing a rejected name could have created is on disk: the directory
	// holds the record file and exactly the accepted names.
	names, err := s.ListFiles("1")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	for _, name := range names {
		if !accepted[name] {
			t.Errorf("ListFiles() contains %q, which the validator rejects", name)
		}
	}
	if len(names) != len(accepted) {
		t.Errorf("ListFiles() = %v (%d names), want the %d accepted ones", names, len(names), len(accepted))
	}
}

// rejectedByName reports whether err is a name-shape rejection rather than a
// missing file: a read of a valid name that was never written is "not
// found", and that means the name was accepted.
func rejectedByName(err error) bool {
	return err != nil && !strings.Contains(err.Error(), "not found")
}

// TestReadFile_RejectsDotNames is the "." hole itself: before the rules
// moved into internal/task, "." passed validation, os.ReadFile hit the
// task's own directory and the raw error came straight back out - which a
// handler mapping only "not found" would have turned into a 500.
func TestReadFile_RejectsDotNames(t *testing.T) {
	s := NewMarkdownStorage(t.TempDir())
	if err := s.Save(makeTestTask(1)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, name := range []string{".", "..", "...", ". ", " .. "} {
		_, err := s.ReadFile("1", name)
		if err == nil {
			t.Errorf("ReadFile(%q) = nil error, want a rejection", name)
			continue
		}
		if !strings.Contains(err.Error(), "is not allowed") {
			t.Errorf("ReadFile(%q) error = %q, want the name rejected before the read", name, err)
		}
	}
}

// TestFileNameReservedAsymmetry pins that reserved names are a write-only
// rule: a write refuses the task's own record file and the server-owned
// phase records, a read serves both.
func TestFileNameReservedAsymmetry(t *testing.T) {
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	if err := s.Save(makeTestTask(1)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// Phase records are written by the phase store, not by WriteFile.
	if err := os.WriteFile(filepath.Join(dir, "1", "research.phase"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("seed research.phase: %v", err)
	}

	for _, name := range []string{"1.md", "research.phase", "Research.PHASE", "research.phase."} {
		if err := s.WriteFile("1", name, "x"); err == nil {
			t.Errorf("WriteFile(%q) = nil error, want it refused as reserved", name)
		}
	}

	for _, name := range []string{"1.md", "research.phase"} {
		if _, err := s.ReadFile("1", name); err != nil {
			t.Errorf("ReadFile(%q) error = %v, want it served: a read has no reserved names", name, err)
		}
	}
}
