package task

import (
	"strings"
	"testing"
)

// nameCases is the shared table of name shapes: the storage agreement test
// in internal/storage runs the same list against the real file paths.
var nameCases = []struct {
	name    string
	wantErr bool
}{
	// Valid: anything that is a single, non-degenerate path segment.
	{"notes.md", false},
	{"research", false},
	{".hidden", false},
	{"..foo", false},
	{"foo..bar", false},
	{"notes.", false},
	{" x ", false},
	{"research.phase", false}, // shape is fine; reserved is a separate, write-only rule
	{"1.md", false},           // ditto: collides with a record file only for a write
	{"a b c.md", false},
	{"Ünïcödé.md", false},

	// Empty and whitespace-only.
	{"", true},
	{"   ", true},
	{"\t", true},
	{"\n", true},

	// Path separators.
	{"sub/dir.md", true},
	{`sub\dir.md`, true},
	{"../escape.md", true},
	{"/abs.md", true},

	// Only dots and spaces: resolves to a directory, or to nothing at all.
	{".", true},
	{"..", true},
	{"...", true},
	{". ", true},
	{" .", true},
	{" .. ", true},
	{". . .", true},
}

func TestValidateAttachedName(t *testing.T) {
	for _, tt := range nameCases {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAttachedName(tt.name)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateAttachedName(%q) = nil, want error", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateAttachedName(%q) = %v, want nil", tt.name, err)
			}
			if err != nil && !strings.Contains(err.Error(), "filename") {
				t.Errorf("ValidateAttachedName(%q) error = %q, want it to name the subject", tt.name, err)
			}
		})
	}
}

// TestValidateNameSegmentLabel pins that the label words the message: the id
// and user paths in internal/storage rely on it for their own wording.
func TestValidateNameSegmentLabel(t *testing.T) {
	for _, label := range []string{"filename", "task id", "user"} {
		for _, name := range []string{"", "a/b", ".."} {
			err := ValidateNameSegment(label, name)
			if err == nil {
				t.Fatalf("ValidateNameSegment(%q, %q) = nil, want error", label, name)
			}
			if !strings.HasPrefix(err.Error(), label) {
				t.Errorf("ValidateNameSegment(%q, %q) error = %q, want it to start with the label", label, name, err)
			}
		}
	}
}

// TestNoFileStorage covers a service built without a file store: the three
// attached-file methods report it instead of dereferencing nil.
func TestNoFileStorage(t *testing.T) {
	svc := NewService(newMockStorage(), nil, nil, newMockIndex(), []string{"feature", "bug"}, nil)

	if _, err := svc.ReadTaskFile("1", "notes.md"); err == nil {
		t.Error("ReadTaskFile() = nil error, want one: no file storage")
	}
	if _, err := svc.ListTaskFiles("1"); err == nil {
		t.Error("ListTaskFiles() = nil error, want one: no file storage")
	}
	if err := svc.WriteTaskFile("1", "notes.md", "x"); err == nil {
		t.Error("WriteTaskFile() = nil error, want one: no file storage")
	}
}
