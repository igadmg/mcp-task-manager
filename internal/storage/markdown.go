package storage

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"gopkg.in/yaml.v3"
)

// MarkdownStorage handles reading/writing task markdown files
type MarkdownStorage struct {
	dir string
}

// NewMarkdownStorage creates a new markdown storage
func NewMarkdownStorage(dir string) *MarkdownStorage {
	return &MarkdownStorage{dir: dir}
}

// EnsureDir creates the tasks directory if it doesn't exist
func (s *MarkdownStorage) EnsureDir() error {
	return os.MkdirAll(s.dir, 0755)
}

// taskPath returns the file path for a task ID
// taskDir returns the per-task directory path for a task ID
func (s *MarkdownStorage) taskDir(id string) string {
	return filepath.Join(s.dir, id)
}

// taskPath returns the file path for a task ID
func (s *MarkdownStorage) taskPath(id string) string {
	return filepath.Join(s.taskDir(id), fmt.Sprintf("%s.md", id))
}

// Save writes a task to a markdown file
// Save writes a task to a markdown file
func (s *MarkdownStorage) Save(t *task.Task) error {
	// Build frontmatter
	frontmatter := struct {
		ID             string          `yaml:"id"`
		ParentID       string          `yaml:"parent_id,omitempty"`
		OrphanedID     string          `yaml:"orphaned_id,omitempty"`
		Title          string          `yaml:"title"`
		Status         task.Status     `yaml:"status"`
		Priority       task.Priority   `yaml:"priority"`
		Type           string          `yaml:"type"`
		Relations      []task.Relation `yaml:"relations,omitempty"`
		CreatedAt      string          `yaml:"created_at"`
		CreatedBy      string          `yaml:"created_by,omitempty"`
		UpdatedAt      string          `yaml:"updated_at"`
		Resolution     task.Resolution `yaml:"resolution,omitempty"`
		ResolutionNote string          `yaml:"resolution_note,omitempty"`
		ClosedAt       string          `yaml:"closed_at,omitempty"`
		VerifiedAt     string          `yaml:"verified_at,omitempty"`
		Branch         string          `yaml:"branch,omitempty"`
		BaseBranch     string          `yaml:"base_branch,omitempty"`
		StartCommit    string          `yaml:"start_commit,omitempty"`
		FinalBranch    string          `yaml:"final_branch,omitempty"`
		SquashCommit   string          `yaml:"squash_commit,omitempty"`
	}{
		ID:             t.ID,
		ParentID:       t.ParentID,
		OrphanedID:     t.OrphanedID,
		Title:          t.Title,
		Status:         t.Status,
		Priority:       t.Priority,
		Type:           t.Type,
		Relations:      t.Relations,
		CreatedAt:      t.CreatedAt.Format(timeLayout),
		CreatedBy:      t.CreatedBy,
		UpdatedAt:      t.UpdatedAt.Format(timeLayout),
		Resolution:     t.Resolution,
		ResolutionNote: t.ResolutionNote,
		ClosedAt:       formatTime(t.ClosedAt),
		VerifiedAt:     formatTime(t.VerifiedAt),
		Branch:         t.Branch,
		BaseBranch:     t.BaseBranch,
		StartCommit:    t.StartCommit,
		FinalBranch:    t.FinalBranch,
		SquashCommit:   t.SquashCommit,
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(frontmatter); err != nil {
		return err
	}
	buf.WriteString("---\n\n")
	buf.WriteString(t.Description)

	return writeFileAtomic(s.taskPath(t.ID), buf.Bytes())
}

// writeFileAtomic writes data to path through a temp file and a rename, so a
// reader never sees a half-written file. It creates path's directory.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Load reads a task from a markdown file
func (s *MarkdownStorage) Load(id string) (*task.Task, error) {
	data, err := os.ReadFile(s.taskPath(id))
	if err != nil {
		return nil, err
	}
	return s.parse(data)
}

// Delete removes a task file
// Delete removes a task's directory (the task's .md file and any attached files)
func (s *MarkdownStorage) Delete(id string) error {
	return os.RemoveAll(s.taskDir(id))
}

// LoadAll reads all tasks from the directory
// LoadAll reads all tasks from the directory
func (s *MarkdownStorage) LoadAll() ([]*task.Task, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tasks []*task.Task
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "archive" || entry.Name() == UsersDirName {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name(), entry.Name()+".md"))
		if err != nil {
			continue
		}

		t, err := s.parse(data)
		if err != nil {
			continue
		}
		tasks = append(tasks, t)
	}

	return tasks, nil
}

// parse extracts task from markdown with frontmatter
func (s *MarkdownStorage) parse(data []byte) (*task.Task, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	// Find opening ---
	if !scanner.Scan() || scanner.Text() != "---" {
		return nil, fmt.Errorf("invalid frontmatter: missing opening ---")
	}

	// Collect frontmatter
	var frontmatterBuf bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			break
		}
		frontmatterBuf.WriteString(line)
		frontmatterBuf.WriteString("\n")
	}

	// Parse frontmatter
	var fm struct {
		ID             string          `yaml:"id"`
		ParentID       string          `yaml:"parent_id"`
		OrphanedID     string          `yaml:"orphaned_id"`
		Title          string          `yaml:"title"`
		Status         string          `yaml:"status"`
		Priority       string          `yaml:"priority"`
		Type           string          `yaml:"type"`
		Relations      []task.Relation `yaml:"relations"`
		CreatedAt      string          `yaml:"created_at"`
		CreatedBy      string          `yaml:"created_by"`
		UpdatedAt      string          `yaml:"updated_at"`
		Resolution     string          `yaml:"resolution"`
		ResolutionNote string          `yaml:"resolution_note"`
		ClosedAt       string          `yaml:"closed_at"`
		VerifiedAt     string          `yaml:"verified_at"`
		Branch         string          `yaml:"branch"`
		BaseBranch     string          `yaml:"base_branch"`
		StartCommit    string          `yaml:"start_commit"`
		FinalBranch    string          `yaml:"final_branch"`
		SquashCommit   string          `yaml:"squash_commit"`
	}
	if err := yaml.Unmarshal(frontmatterBuf.Bytes(), &fm); err != nil {
		return nil, err
	}

	// Collect body (description)
	var bodyBuf bytes.Buffer
	for scanner.Scan() {
		if bodyBuf.Len() > 0 {
			bodyBuf.WriteString("\n")
		}
		bodyBuf.WriteString(scanner.Text())
	}

	// Parse timestamps
	createdAt, _ := parseTime(fm.CreatedAt)
	updatedAt, _ := parseTime(fm.UpdatedAt)

	return &task.Task{
		ID:             fm.ID,
		ParentID:       fm.ParentID,
		OrphanedID:     fm.OrphanedID,
		Title:          fm.Title,
		Description:    strings.TrimSpace(bodyBuf.String()),
		Status:         task.Status(fm.Status),
		Priority:       task.Priority(fm.Priority),
		Type:           fm.Type,
		Relations:      fm.Relations,
		CreatedAt:      createdAt,
		CreatedBy:      fm.CreatedBy,
		UpdatedAt:      updatedAt,
		Resolution:     task.Resolution(fm.Resolution),
		ResolutionNote: fm.ResolutionNote,
		ClosedAt:       parseOptionalTime(fm.ClosedAt),
		VerifiedAt:     parseOptionalTime(fm.VerifiedAt),
		Branch:         fm.Branch,
		BaseBranch:     fm.BaseBranch,
		StartCommit:    fm.StartCommit,
		FinalBranch:    fm.FinalBranch,
		SquashCommit:   fm.SquashCommit,
	}, nil
}

// timeLayout is how every timestamp the storage writes is rendered: the
// task frontmatter and the phase records alike.
const timeLayout = "2006-01-02T15:04:05Z07:00"

// formatTime renders an optional timestamp for the frontmatter, yielding ""
// for a missing one so the yaml omitempty tag drops the key entirely.
func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(timeLayout)
}

// parseOptionalTime is parseTime for a key that may be absent: an empty or
// unparsable value reads back as "no timestamp" rather than the zero time,
// which would otherwise be indistinguishable from a real one.
func parseOptionalTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := parseTime(s)
	if err != nil {
		return nil
	}
	return &t
}

// parseTime tries multiple time formats
func parseTime(s string) (t time.Time, err error) {
	formats := []string{
		timeLayout,
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err = time.Parse(f, s); err == nil {
			return
		}
	}
	return
}

// archivePath returns the file path for an archived task ID
// archiveTaskDir returns the per-task archive directory path for a task ID
func (s *MarkdownStorage) archiveTaskDir(id string) string {
	return filepath.Join(s.dir, "archive", id)
}

// archivePath returns the file path for an archived task ID
func (s *MarkdownStorage) archivePath(id string) string {
	return filepath.Join(s.archiveTaskDir(id), fmt.Sprintf("%s.md", id))
}

// Archive moves a task file from the tasks directory to the archive subdirectory
// Archive moves a task's directory from the tasks directory to the archive subdirectory
func (s *MarkdownStorage) Archive(id string) error {
	archiveDir := filepath.Join(s.dir, "archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}
	return os.Rename(s.taskDir(id), s.archiveTaskDir(id))
}

// LoadArchived reads an archived task from the archive directory
func (s *MarkdownStorage) LoadArchived(id string) (*task.Task, error) {
	data, err := os.ReadFile(s.archivePath(id))
	if err != nil {
		return nil, err
	}
	return s.parse(data)
}

// LoadAllArchived reads all archived tasks from the archive subdirectory
// LoadAllArchived reads all archived tasks from the archive subdirectory
func (s *MarkdownStorage) LoadAllArchived() ([]*task.Task, error) {
	archiveDir := filepath.Join(s.dir, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tasks []*task.Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(archiveDir, entry.Name(), entry.Name()+".md"))
		if err != nil {
			continue
		}

		t, err := s.parse(data)
		if err != nil {
			continue
		}
		tasks = append(tasks, t)
	}

	return tasks, nil
}

// IsArchived checks whether a task exists in the archive directory
func (s *MarkdownStorage) IsArchived(id string) bool {
	_, err := os.Stat(s.archivePath(id))
	return err == nil
}

// NextID returns the next available task ID
func (s *MarkdownStorage) NextID() (int, error) {
	activeMax, err := maxMarkdownTaskID(s.dir)
	if err != nil {
		return 0, err
	}

	archiveMax, err := maxMarkdownTaskID(filepath.Join(s.dir, "archive"))
	if err != nil {
		return 0, err
	}

	if archiveMax > activeMax {
		return archiveMax + 1, nil
	}
	return activeMax + 1, nil
}

func maxMarkdownTaskID(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	maxID := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if id, err := strconv.Atoi(entry.Name()); err == nil && id > maxID {
			maxID = id
		}
	}

	return maxID, nil
}
