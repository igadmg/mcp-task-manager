package task

import (
	"fmt"
	"math"
	"regexp"
	"sort"
)

// Fields holds the frontmatter keys the server does not own: free-form
// `key: value` metadata a user or an agent hangs off a task (complexity,
// area, a tracker id). The server gives none of them any meaning - nothing
// sorts, filters a queue or gates a flow on a field value - it only promises
// never to lose one.
//
// The codec lives in internal/storage, which captures these keys with a yaml
// inline map on its one frontmatter struct, so every flow that round-trips a
// record carries them along without knowing they exist.
//
// A value read from a record is kept exactly as yaml decoded it, nested maps
// and block scalars included: being lossless matters more than being tidy. A
// value a caller sets goes through NormalizeFieldValue instead, which allows
// scalars only - that is the shape this feature documents.
type Fields map[string]any

// Clone returns a copy that shares no map with f, so a holder of one cannot
// reach into the other. Nil in, nil out: an empty map and no map read the
// same everywhere, and nil is what omitempty drops.
//
// The copy is shallow. A nested value that came off disk is shared, which is
// safe because nothing in the server ever writes into one.
func (f Fields) Clone() Fields {
	if f == nil {
		return nil
	}
	out := make(Fields, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

// Keys returns f's keys in the order every surface shows them and the storage
// writes them: sorted, which is also what yaml.v3 emits for an inline map.
func (f Fields) Keys() []string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// String renders a field value the way every consumer displays and filters on
// it: the plain scalar, no yaml quoting. It is what WithFieldFilter compares
// against, so `count: 3` is found by "3".
func (f Fields) String(key string) string {
	v, ok := f[key]
	if !ok {
		return ""
	}
	return fieldValueString(v)
}

func fieldValueString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// reservedFieldKeys are the frontmatter keys the server itself owns, with the
// reason each is taken. A caller may not set one as a field: the key would
// collide with a struct field of the storage frontmatter, and yaml.v3 does
// not report that as an error - it panics, and the panic escapes even
// yaml.Marshal's own recover.
//
// internal/storage derives the same set from its frontmatter struct in a test
// (TestReservedFieldKeysMatchFrontmatter), so a new frontmatter key cannot be
// added without this table noticing - the arrangement StatsFields() uses for
// the same reason: the dependency runs storage -> task, so task cannot ask.
var reservedFieldKeys = map[string]string{
	"id":              "the task id",
	"parent_id":       "the parent task",
	"orphaned_id":     "the former parent of a task moved to the top level",
	"title":           "the task title",
	"status":          "the task status",
	"priority":        "the task priority",
	"type":            "the task type",
	"relations":       "the task's typed relations",
	"created_at":      "when the task was created",
	"created_by":      "who created the task",
	"updated_at":      "when the task was last written",
	"resolution":      "how the task left the backlog",
	"resolution_note": "the one line behind the resolution",
	"closed_at":       "when the task became done",
	"verified_at":     "when the task's text was last checked",
	"branch":          "the task's wip branch",
	"base_branch":     "the branch the wip branch was cut from",
	"start_commit":    "the commit the wip branch grows from",
	"final_branch":    "the delivered branch",
	"squash_commit":   "the final or merge commit",
	// description is not a frontmatter key at all - it is the markdown body -
	// which is exactly why it is reserved: a frontmatter "description" would
	// be faithfully preserved as a field while the real one sat below the
	// fence, and that reads as a bug rather than as metadata.
	"description": "the task description, stored in the markdown body",
}

// fieldKeyPattern is the shape of a key a caller may set. Lowercase with a
// leading letter keeps every key unquoted in the frontmatter and short enough
// to read on a board chip.
var fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// maxFieldKeyLen caps a key at a length that still fits a chip.
const maxFieldKeyLen = 64

// ValidateFieldKey reports whether key is one a caller may set as a field.
//
// It deliberately does not reuse ValidateNameSegment: that rule set is about
// path segments and would accept a newline, a colon and 500 characters, none
// of which belong in a yaml key.
//
// The rule applies to caller-supplied keys only. A key already written into a
// record by hand is read and written back whatever it looks like - that is
// the lossless promise - it simply cannot be re-set through a tool without
// being renamed first.
func ValidateFieldKey(key string) error {
	if key == "" {
		return fmt.Errorf("field key cannot be empty")
	}
	if len(key) > maxFieldKeyLen {
		return fmt.Errorf("field key %q is longer than %d characters", key, maxFieldKeyLen)
	}
	if reason, ok := reservedFieldKeys[key]; ok {
		return fmt.Errorf("field key %q is reserved (%s)", key, reason)
	}
	if !fieldKeyPattern.MatchString(key) {
		return fmt.Errorf("field key %q must start with a lowercase letter and hold only lowercase letters, digits, %q and %q", key, "_", "-")
	}
	return nil
}

// ReservedFieldKeys returns the server-owned keys, for an error message or a
// test. The map is copied, so a caller cannot edit the table.
func ReservedFieldKeys() map[string]string {
	out := make(map[string]string, len(reservedFieldKeys))
	for k, v := range reservedFieldKeys {
		out[k] = v
	}
	return out
}

// NormalizeFieldValue prepares one caller-supplied value for storage. keep is
// false when the value means "remove this field": nil, or the empty string.
// The cost of that rule is that a field cannot hold an empty string, which is
// the trade behind "setting a field to empty or null removes it".
//
// Only scalars are accepted. A JSON number arrives as float64 and becomes an
// int when it is integral, so `count: 3` stays 3 in the record rather than
// turning into a float on the next read. int rather than int64 on purpose:
// that is what yaml decodes an integer back into, so a value is the same Go
// type before and after a round trip.
func NormalizeFieldValue(key string, v any) (value any, keep bool, err error) {
	switch tv := v.(type) {
	case nil:
		return nil, false, nil
	case string:
		if tv == "" {
			return nil, false, nil
		}
		return tv, true, nil
	case bool:
		return tv, true, nil
	case int:
		return tv, true, nil
	case int64:
		return int(tv), true, nil
	case float64:
		if tv == math.Trunc(tv) && !math.IsInf(tv, 0) && math.Abs(tv) < 1e15 {
			return int(tv), true, nil
		}
		return tv, true, nil
	case float32:
		return NormalizeFieldValue(key, float64(tv))
	default:
		return nil, false, fmt.Errorf("field %q must be a string, number or boolean, got %T", key, v)
	}
}

// MergeFields applies a caller's changes to a task's fields and returns the
// result as a new map, leaving base untouched. A key in changes is set, a key
// absent from it is left alone - a merge, not a replacement, so two agents
// working one task cannot drop each other's fields. A nil or empty value
// removes its key. An empty result is nil, which is what the storage omits.
func MergeFields(base Fields, changes map[string]any) (Fields, error) {
	out := base.Clone()
	for _, key := range sortedKeys(changes) {
		if err := ValidateFieldKey(key); err != nil {
			return nil, err
		}
		value, keep, err := NormalizeFieldValue(key, changes[key])
		if err != nil {
			return nil, err
		}
		if !keep {
			delete(out, key)
			continue
		}
		if out == nil {
			out = make(Fields, len(changes))
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// sortedKeys orders a change set so that a rejected key yields the same error
// whichever way Go happened to iterate the map.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
