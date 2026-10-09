package storage

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// frontmatter is the yaml head of a task record, and the one place its key
// list exists. Save encodes it and parse decodes it: a single struct for both
// directions, so a new key cannot be added to one and forgotten in the other
// (it used to be two anonymous structs that had to agree by hand).
//
// Every timestamp is a string here because the record's time format is this
// package's business, not yaml's - see timeLayout, formatTime and parseTime.
//
// Extra is the inline catch-all that makes a record lossless: yaml hands a
// named key to its field and everything else to this map, and encodes the
// named fields in declaration order followed by Extra's keys sorted. A
// record the server rewrites therefore produces no diff beyond what actually
// changed, and a hand-written key the server knows nothing about survives
// every flow.
type frontmatter struct {
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

	Extra task.Fields `yaml:",inline"`
}

// newFrontmatter renders a task's head for writing.
func newFrontmatter(t *task.Task) frontmatter {
	return frontmatter{
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
		Extra:          t.Fields.Clone(),
	}
}

// toTask builds the task a decoded frontmatter and a markdown body describe.
func (fm frontmatter) toTask(body string) *task.Task {
	createdAt, _ := parseTime(fm.CreatedAt)
	updatedAt, _ := parseTime(fm.UpdatedAt)

	return &task.Task{
		ID:             fm.ID,
		ParentID:       fm.ParentID,
		OrphanedID:     fm.OrphanedID,
		Title:          fm.Title,
		Description:    body,
		Status:         fm.Status,
		Priority:       fm.Priority,
		Type:           fm.Type,
		Relations:      fm.Relations,
		CreatedAt:      createdAt,
		CreatedBy:      fm.CreatedBy,
		UpdatedAt:      updatedAt,
		Resolution:     fm.Resolution,
		ResolutionNote: fm.ResolutionNote,
		ClosedAt:       parseOptionalTime(fm.ClosedAt),
		VerifiedAt:     parseOptionalTime(fm.VerifiedAt),
		Branch:         fm.Branch,
		BaseBranch:     fm.BaseBranch,
		StartCommit:    fm.StartCommit,
		FinalBranch:    fm.FinalBranch,
		SquashCommit:   fm.SquashCommit,
		Fields:         fm.Extra,
	}
}

// frontmatterKeys is the set of keys the frontmatter struct itself owns,
// derived from its yaml tags once. It backs checkExtraKeys and the test that
// holds task.ReservedFieldKeys() to it.
var frontmatterKeys = deriveFrontmatterKeys()

func deriveFrontmatterKeys() map[string]struct{} {
	keys := make(map[string]struct{})
	rt := reflect.TypeOf(frontmatter{})
	for i := range rt.NumField() {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		keys[tag] = struct{}{}
	}
	return keys
}

// checkExtraKeys refuses a field whose key the frontmatter struct already
// owns. The guard exists because yaml.v3 does not report such a key as an
// error: it panics ("cannot have key %q in inlined map"), and the panic
// escapes even yaml.Marshal's own recover, so a single bad field would take
// the whole server down on the next write. task.ValidateFieldKey keeps one
// from ever getting this far; this turns a crash into a failed call if it
// somehow does.
func checkExtraKeys(f task.Fields) error {
	for _, key := range f.Keys() {
		if _, owned := frontmatterKeys[key]; owned {
			return fmt.Errorf("field %q collides with the frontmatter key the server owns", key)
		}
	}
	return nil
}
