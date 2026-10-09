package task

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateFieldKey(t *testing.T) {
	tests := []struct {
		key     string
		wantErr string // a substring, "" = accepted
	}{
		{"complexity", ""},
		{"area", ""},
		{"tracker_id", ""},
		{"jira-key", ""},
		{"a", ""},
		{"a1", ""},
		{"", "empty"},
		{"Area", "lowercase"},
		{"1area", "lowercase"},
		{"_area", "lowercase"},
		{"-area", "lowercase"},
		{"my area", "lowercase"},
		{"my.area", "lowercase"},
		{"area:web", "lowercase"},
		{"area\n", "lowercase"},
		{strings.Repeat("a", 65), "longer than"},
		{strings.Repeat("a", 64), ""},
		// Every server-owned key is refused, with its reason.
		{"title", "reserved"},
		{"status", "reserved"},
		{"squash_commit", "reserved"},
		{"description", "reserved"},
	}

	for _, tc := range tests {
		err := ValidateFieldKey(tc.key)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("ValidateFieldKey(%q) = %v, want nil", tc.key, err)
		case tc.wantErr != "" && err == nil:
			t.Errorf("ValidateFieldKey(%q) = nil, want an error about %q", tc.key, tc.wantErr)
		case tc.wantErr != "" && err != nil && !strings.Contains(err.Error(), tc.wantErr):
			t.Errorf("ValidateFieldKey(%q) = %v, want it to mention %q", tc.key, err, tc.wantErr)
		}
	}
}

// TestValidateFieldKeyCoversEveryReservedKey makes sure the table is actually
// consulted for all of its entries, not just the ones spot-checked above.
func TestValidateFieldKeyCoversEveryReservedKey(t *testing.T) {
	for key := range ReservedFieldKeys() {
		err := ValidateFieldKey(key)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("ValidateFieldKey(%q) = %v, want a reserved error", key, err)
		}
	}
}

func TestNormalizeFieldValue(t *testing.T) {
	tests := []struct {
		name      string
		in        any
		want      any
		wantKeep  bool
		wantError bool
	}{
		{"string", "high", "high", true, false},
		{"bool", true, true, true, false},
		{"int", 3, 3, true, false},
		{"integral float becomes an int", 3.0, 3, true, false},
		{"fractional float stays a float", 1.5, 1.5, true, false},
		{"nil removes", nil, nil, false, false},
		{"empty string removes", "", nil, false, false},
		{"a map is refused", map[string]any{"x": 1}, nil, false, true},
		{"a list is refused", []any{1, 2}, nil, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, keep, err := NormalizeFieldValue("k", tc.in)
			if tc.wantError {
				if err == nil {
					t.Fatalf("NormalizeFieldValue() error = nil, want one")
				}
				if !strings.Contains(err.Error(), `"k"`) {
					t.Errorf("error = %v, want it to name the key", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeFieldValue() error = %v", err)
			}
			if keep != tc.wantKeep {
				t.Errorf("keep = %v, want %v", keep, tc.wantKeep)
			}
			if keep && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("value = %#v (%T), want %#v", got, got, tc.want)
			}
		})
	}
}

func TestMergeFields(t *testing.T) {
	base := Fields{"area": "web", "complexity": "high"}

	// A merge, not a replacement: an absent key is left alone.
	got, err := MergeFields(base, map[string]any{"complexity": "low", "count": 3})
	if err != nil {
		t.Fatalf("MergeFields() error = %v", err)
	}
	want := Fields{"area": "web", "complexity": "low", "count": 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %#v, want %#v", got, want)
	}
	// The base is untouched.
	if !reflect.DeepEqual(base, Fields{"area": "web", "complexity": "high"}) {
		t.Errorf("base was mutated: %#v", base)
	}

	// Null and the empty string remove.
	got, err = MergeFields(base, map[string]any{"area": nil, "complexity": ""})
	if err != nil {
		t.Fatalf("MergeFields() error = %v", err)
	}
	if got != nil {
		t.Errorf("merged = %#v, want nil once every field is removed", got)
	}

	// An empty change set is a no-op, not "clear all".
	got, err = MergeFields(base, map[string]any{})
	if err != nil || !reflect.DeepEqual(got, base) {
		t.Errorf("MergeFields(base, {}) = %#v, %v, want the base unchanged", got, err)
	}

	// A bad key or value fails the whole merge.
	if _, err := MergeFields(base, map[string]any{"title": "x"}); err == nil {
		t.Error("MergeFields() with a reserved key = nil error, want one")
	}
	if _, err := MergeFields(base, map[string]any{"ok": map[string]any{"x": 1}}); err == nil {
		t.Error("MergeFields() with a map value = nil error, want one")
	}

	// Removing a key that was never there is not an error.
	if got, err := MergeFields(nil, map[string]any{"gone": nil}); err != nil || got != nil {
		t.Errorf("MergeFields(nil, remove) = %#v, %v", got, err)
	}
}

func TestFieldsClone(t *testing.T) {
	if Fields(nil).Clone() != nil {
		t.Error("Clone() of a nil Fields should be nil, so omitempty still drops it")
	}

	original := Fields{"area": "web"}
	clone := original.Clone()
	clone["area"] = "cli"
	clone["extra"] = 1
	if original["area"] != "web" || len(original) != 1 {
		t.Errorf("Clone() aliased the original: %#v", original)
	}
}

func TestFieldsKeysAndString(t *testing.T) {
	f := Fields{"complexity": "high", "area": "web", "count": 3, "flag": true}
	if want := []string{"area", "complexity", "count", "flag"}; !reflect.DeepEqual(f.Keys(), want) {
		t.Errorf("Keys() = %v, want %v", f.Keys(), want)
	}
	// String is what a filter compares against, so a number reads as its
	// plain digits rather than as a yaml scalar.
	for key, want := range map[string]string{
		"area":    "web",
		"count":   "3",
		"flag":    "true",
		"missing": "",
	} {
		if got := f.String(key); got != want {
			t.Errorf("String(%q) = %q, want %q", key, got, want)
		}
	}
}
