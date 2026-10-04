package storage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"gopkg.in/yaml.v3"
)

// phaseFileVersion is the only <phase>.phase format this code reads.
const phaseFileVersion = 1

// phaseFile is the on-disk form of a task.PhaseRecord. Times are strings
// in the frontmatter's format; the optional keys of an open run are
// omitted rather than written empty.
type phaseFile struct {
	Version int            `yaml:"version"`
	Phase   string         `yaml:"phase"`
	Runs    []phaseFileRun `yaml:"runs"`
}

type phaseFileRun struct {
	StartedAt  string `yaml:"started_at"`
	StartedBy  string `yaml:"started_by"`
	FinishedAt string `yaml:"finished_at,omitempty"`
	FinishedBy string `yaml:"finished_by,omitempty"`
	Tokens     *int64 `yaml:"tokens,omitempty"`
	Note       string `yaml:"note,omitempty"`
}

func (s *MarkdownStorage) phasePath(dir string, p task.Phase) string {
	return filepath.Join(dir, task.PhaseFileName(p))
}

// LoadPhase reads the task's record for p from its active directory, else
// its archived one. A task without the file reads as nil, nil.
func (s *MarkdownStorage) LoadPhase(taskID string, p task.Phase) (*task.PhaseRecord, error) {
	dir, err := s.resolveTaskDir(taskID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.phasePath(dir, p))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	rec, err := decodePhase(data, p)
	if err != nil {
		return nil, fmt.Errorf("%s of task %s: %w", task.PhaseFileName(p), taskID, err)
	}
	return rec, nil
}

// SavePhase writes rec into the active task's directory, atomically.
func (s *MarkdownStorage) SavePhase(taskID string, rec *task.PhaseRecord) error {
	if err := validatePathSegment("task id", taskID); err != nil {
		return err
	}
	data, err := encodePhase(rec)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.phasePath(s.taskDir(taskID), rec.Phase), data)
}

// RemovePhase deletes the active task's record for p. Removing a missing
// record is not an error.
func (s *MarkdownStorage) RemovePhase(taskID string, p task.Phase) error {
	if err := validatePathSegment("task id", taskID); err != nil {
		return err
	}
	if err := os.Remove(s.phasePath(s.taskDir(taskID), p)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func encodePhase(rec *task.PhaseRecord) ([]byte, error) {
	f := phaseFile{Version: phaseFileVersion, Phase: string(rec.Phase), Runs: []phaseFileRun{}}
	for _, run := range rec.Runs {
		f.Runs = append(f.Runs, phaseFileRun{
			StartedAt:  run.StartedAt.Format(timeLayout),
			StartedBy:  run.StartedBy,
			FinishedAt: formatTime(run.FinishedAt),
			FinishedBy: run.FinishedBy,
			Tokens:     run.Tokens,
			Note:       run.Note,
		})
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodePhase parses a record. A missing version reads as 1, a newer one
// is refused, and the phase is the one the file is named after, whatever
// its phase key says.
func decodePhase(data []byte, p task.Phase) (*task.PhaseRecord, error) {
	var f phaseFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Version > phaseFileVersion {
		return nil, fmt.Errorf("unsupported phase file version %d", f.Version)
	}
	rec := &task.PhaseRecord{Phase: p, Runs: make([]task.PhaseRun, 0, len(f.Runs))}
	for i, r := range f.Runs {
		started, err := parseTime(r.StartedAt)
		if err != nil {
			return nil, fmt.Errorf("run %d: started_at %q: %w", i+1, r.StartedAt, err)
		}
		run := task.PhaseRun{
			StartedAt:  started,
			StartedBy:  r.StartedBy,
			FinishedBy: r.FinishedBy,
			Tokens:     r.Tokens,
			Note:       r.Note,
		}
		if r.FinishedAt != "" {
			finished, err := parseTime(r.FinishedAt)
			if err != nil {
				return nil, fmt.Errorf("run %d: finished_at %q: %w", i+1, r.FinishedAt, err)
			}
			run.FinishedAt = &finished
		}
		rec.Runs = append(rec.Runs, run)
	}
	return rec, nil
}
