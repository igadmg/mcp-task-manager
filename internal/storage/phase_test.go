package storage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// newPhaseStorage returns a storage holding one active task "t1".
func newPhaseStorage(t *testing.T) (*MarkdownStorage, string) {
	t.Helper()
	dir := t.TempDir()
	s := NewMarkdownStorage(dir)
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := s.Save(&task.Task{ID: "t1", Title: "T", Status: task.StatusInProgress, Priority: task.PriorityLow,
		Type: "feature", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return s, dir
}

func int64p(n int64) *int64 { return &n }

func TestPhaseFileRoundTrip(t *testing.T) {
	s, dir := newPhaseStorage(t)
	started := time.Date(2026, 10, 4, 12, 59, 10, 0, time.UTC)
	finished := time.Date(2026, 10, 4, 13, 20, 41, 0, time.UTC)
	rec := &task.PhaseRecord{Phase: task.PhaseDesign, Runs: []task.PhaseRun{
		{StartedAt: started, StartedBy: "igor.cwer", FinishedAt: &finished, FinishedBy: "igor.cwer", Tokens: int64p(81234)},
		{StartedAt: started, StartedBy: "igor.cwer", FinishedAt: &finished, FinishedBy: "igor.cwer", Tokens: int64p(0), Note: "revised"},
		{StartedAt: finished, StartedBy: "igor.cwer"},
	}}
	if err := s.SavePhase("t1", rec); err != nil {
		t.Fatalf("SavePhase() error = %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "t1", "design.phase"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := `version: 1
phase: design
runs:
  - started_at: "2026-10-04T12:59:10Z"
    started_by: igor.cwer
    finished_at: "2026-10-04T13:20:41Z"
    finished_by: igor.cwer
    tokens: 81234
  - started_at: "2026-10-04T12:59:10Z"
    started_by: igor.cwer
    finished_at: "2026-10-04T13:20:41Z"
    finished_by: igor.cwer
    tokens: 0
    note: revised
  - started_at: "2026-10-04T13:20:41Z"
    started_by: igor.cwer
`
	if string(raw) != want {
		t.Errorf("design.phase =\n%s\nwant\n%s", raw, want)
	}

	loaded, err := s.LoadPhase("t1", task.PhaseDesign)
	if err != nil {
		t.Fatalf("LoadPhase() error = %v", err)
	}
	if loaded.Phase != task.PhaseDesign || len(loaded.Runs) != 3 {
		t.Fatalf("LoadPhase() = %+v, want 3 design runs", loaded)
	}
	first, open := loaded.Runs[0], loaded.Runs[2]
	if !first.StartedAt.Equal(started) || first.FinishedAt == nil || !first.FinishedAt.Equal(finished) ||
		first.Tokens == nil || *first.Tokens != 81234 || first.FinishedBy != "igor.cwer" {
		t.Errorf("first run = %+v", first)
	}
	if loaded.Runs[1].Tokens == nil || *loaded.Runs[1].Tokens != 0 || loaded.Runs[1].Note != "revised" {
		t.Errorf("a reported 0 or the note was lost: %+v", loaded.Runs[1])
	}
	if !open.Open() || open.Tokens != nil || open.FinishedBy != "" {
		t.Errorf("open run = %+v, want no finish data", open)
	}
}

func TestLoadPhaseAbsent(t *testing.T) {
	s, _ := newPhaseStorage(t)
	rec, err := s.LoadPhase("t1", task.PhaseResearch)
	if rec != nil || err != nil {
		t.Errorf("LoadPhase() = %v, %v; want nil, nil", rec, err)
	}
}

func TestLoadPhaseArchived(t *testing.T) {
	s, _ := newPhaseStorage(t)
	rec := &task.PhaseRecord{Phase: task.PhaseResearch, Runs: []task.PhaseRun{{StartedAt: time.Now().UTC(), StartedBy: "dev"}}}
	if err := s.SavePhase("t1", rec); err != nil {
		t.Fatalf("SavePhase() error = %v", err)
	}
	if err := s.Archive("t1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	loaded, err := s.LoadPhase("t1", task.PhaseResearch)
	if err != nil || loaded == nil || len(loaded.Runs) != 1 {
		t.Errorf("LoadPhase(archived) = %+v, %v; want one run", loaded, err)
	}
}

func TestRemovePhaseMissingIsNoop(t *testing.T) {
	s, dir := newPhaseStorage(t)
	if err := s.RemovePhase("t1", task.PhasePlanning); err != nil {
		t.Errorf("RemovePhase(missing) error = %v", err)
	}
	rec := &task.PhaseRecord{Phase: task.PhasePlanning, Runs: []task.PhaseRun{{StartedAt: time.Now().UTC()}}}
	if err := s.SavePhase("t1", rec); err != nil {
		t.Fatalf("SavePhase() error = %v", err)
	}
	if err := s.RemovePhase("t1", task.PhasePlanning); err != nil {
		t.Fatalf("RemovePhase() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "t1", "planning.phase")); !os.IsNotExist(err) {
		t.Errorf("planning.phase still exists after RemovePhase (stat error %v)", err)
	}
}

func TestLoadPhaseUnsupportedVersion(t *testing.T) {
	s, dir := newPhaseStorage(t)
	path := filepath.Join(dir, "t1", "research.phase")
	if err := os.WriteFile(path, []byte("version: 2\nphase: research\nruns: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := s.LoadPhase("t1", task.PhaseResearch)
	if err == nil || !strings.Contains(err.Error(), "unsupported phase file version 2") {
		t.Errorf("LoadPhase(version 2) error = %v, want unsupported version", err)
	}

	// A file without a version reads as version 1.
	if err := os.WriteFile(path, []byte("phase: research\nruns:\n  - started_at: 2026-10-04T12:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec, err := s.LoadPhase("t1", task.PhaseResearch)
	if err != nil || len(rec.Runs) != 1 {
		t.Errorf("LoadPhase(no version) = %+v, %v; want one run", rec, err)
	}
}

func TestLoadPhaseNameMismatch(t *testing.T) {
	s, dir := newPhaseStorage(t)
	path := filepath.Join(dir, "t1", "design.phase")
	if err := os.WriteFile(path, []byte("version: 1\nphase: research\nruns: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec, err := s.LoadPhase("t1", task.PhaseDesign)
	if err != nil || rec.Phase != task.PhaseDesign {
		t.Errorf("LoadPhase() = %+v, %v; want the file name's phase (design)", rec, err)
	}
}

func TestWriteFileRefusesPhaseNames(t *testing.T) {
	s, _ := newPhaseStorage(t)
	for _, name := range []string{"research.phase", "Design.PHASE", "x.phase", "research.phase."} {
		err := s.WriteFile("t1", name, "x")
		if err == nil || !strings.Contains(err.Error(), "is reserved: phase files are written only by start_phase/finish_phase") {
			t.Errorf("WriteFile(%q) error = %v, want the reserved-name error", name, err)
		}
	}
	for _, name := range []string{"phase.md", "research"} {
		if err := s.WriteFile("t1", name, "x"); err != nil {
			t.Errorf("WriteFile(%q) error = %v, want it allowed", name, err)
		}
	}

	rec := &task.PhaseRecord{Phase: task.PhaseResearch, Runs: []task.PhaseRun{{StartedAt: time.Now().UTC(), StartedBy: "dev"}}}
	if err := s.SavePhase("t1", rec); err != nil {
		t.Fatalf("SavePhase() error = %v", err)
	}
	names, err := s.ListFiles("t1")
	if err != nil || !slices.Contains(names, "research.phase") {
		t.Errorf("ListFiles() = %v, %v; want research.phase listed", names, err)
	}
	if content, err := s.ReadFile("t1", "research.phase"); err != nil || !strings.Contains(content, "phase: research") {
		t.Errorf("ReadFile(research.phase) = %q, %v", content, err)
	}
}
