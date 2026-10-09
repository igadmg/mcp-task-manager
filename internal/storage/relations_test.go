package storage

import (
	"fmt"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// relRecord is a task record with a relations block, written straight to disk
// so the index builds from frontmatter the way a git pull or a hand edit
// leaves it - which is the path that can produce a dangling edge.
func relRecord(id, title string, relations ...task.Relation) string {
	out := fmt.Sprintf("---\nid: %q\ntitle: %s\nstatus: todo\npriority: medium\ntype: feature\n", id, title)
	if len(relations) > 0 {
		out += "relations:\n"
		for _, r := range relations {
			out += fmt.Sprintf("  - type: %s\n    task: %q\n", r.Type, r.Task)
		}
	}
	out += "created_at: 2026-01-15T10:30:00Z\nupdated_at: 2026-01-15T10:30:00Z\n---\n\nBody.\n"
	return out
}

func indexWith(t *testing.T, records map[string]string) *Index {
	t.Helper()
	dir := t.TempDir()
	st := NewMarkdownStorage(dir)
	for id, content := range records {
		writeRecord(t, dir, id, content)
	}
	idx := NewIndex(dir, st)
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	return idx
}

// TestAllRelationsCollapsesSymmetricEdges is the reason this method exists at
// the storage boundary: relates_to is stored in both directions on purpose, so
// a whole-graph read would otherwise draw every one of them twice.
func TestAllRelationsCollapsesSymmetricEdges(t *testing.T) {
	idx := indexWith(t, map[string]string{
		"1": relRecord("1", "one", task.Relation{Type: "relates_to", Task: "2"}),
		"2": relRecord("2", "two"),
		"3": relRecord("3", "three", task.Relation{Type: "blocked_by", Task: "1"}),
	})

	got := idx.AllRelations()
	want := []task.GraphEdge{
		{Type: "relates_to", Source: "1", Target: "2", Symmetric: true},
		{Type: "blocked_by", Source: "3", Target: "1"},
	}
	if len(got) != len(want) {
		t.Fatalf("AllRelations() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("edge %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The index really does hold both directions, so the collapse is doing
	// work rather than describing an index that never duplicated: task 2
	// stored no relation at all, yet it is the source of one edge and the
	// target of the other.
	var asSource, asTarget int
	for _, e := range idx.GetRelationsForTask("2") {
		if e.Source == "2" {
			asSource++
		}
		if e.Target == "2" {
			asTarget++
		}
	}
	if asSource != 1 || asTarget != 1 {
		t.Errorf("task 2 is source of %d and target of %d edges, want 1 and 1", asSource, asTarget)
	}
}

// TestAllRelationsKeepsBothAsymmetricDirections: two tasks blocking each other
// is a cycle, not a duplicate, and both edges are real.
func TestAllRelationsKeepsBothAsymmetricDirections(t *testing.T) {
	idx := indexWith(t, map[string]string{
		"1": relRecord("1", "one", task.Relation{Type: "blocked_by", Task: "2"}),
		"2": relRecord("2", "two", task.Relation{Type: "blocked_by", Task: "1"}),
	})

	got := idx.AllRelations()
	if len(got) != 2 {
		t.Fatalf("AllRelations() = %+v, want both directions", got)
	}
	for _, e := range got {
		if e.Symmetric {
			t.Errorf("blocked_by %s->%s is marked symmetric", e.Source, e.Target)
		}
	}
}

// TestAllRelationsDropsDanglingEdges: AddRelation validates its target, but a
// rebuild takes frontmatter as it is - and the generated reverse of a dangling
// relates_to has a source that does not exist either.
func TestAllRelationsDropsDanglingEdges(t *testing.T) {
	idx := indexWith(t, map[string]string{
		"1": relRecord("1", "one",
			task.Relation{Type: "blocked_by", Task: "nope"},
			task.Relation{Type: "relates_to", Task: "gone"}),
	})

	if got := idx.AllRelations(); len(got) != 0 {
		t.Errorf("AllRelations() = %+v, want nothing: both targets are missing", got)
	}
	// The edges are in the index; it is this method that refuses to hand
	// out an edge to a node that is not there.
	if n := len(idx.GetRelationsForTask("1")); n == 0 {
		t.Error("the fixture wrote no edges at all")
	}
}

// TestAllRelationsIsDeterministic: the graph is re-rendered every five
// seconds, so an unstable order would reshuffle the picture under the reader.
func TestAllRelationsIsDeterministic(t *testing.T) {
	records := map[string]string{
		"1":  relRecord("1", "one", task.Relation{Type: "relates_to", Task: "10"}),
		"2":  relRecord("2", "two", task.Relation{Type: "blocked_by", Task: "1"}),
		"10": relRecord("10", "ten", task.Relation{Type: "blocked_by", Task: "2"}),
		"b":  relRecord("b", "text id", task.Relation{Type: "duplicate_of", Task: "1"}),
	}
	idx := indexWith(t, records)

	first := idx.AllRelations()
	for i := 0; i < 5; i++ {
		if err := idx.Rebuild(); err != nil {
			t.Fatal(err)
		}
		again := idx.AllRelations()
		if len(again) != len(first) {
			t.Fatalf("rebuild %d: %d edges, want %d", i, len(again), len(first))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("rebuild %d: edge %d = %+v, want %+v", i, j, again[j], first[j])
			}
		}
	}
	// Numeric ids sort numerically, as everywhere else in this package.
	if first[0].Source != "1" || first[1].Source != "2" || first[2].Source != "10" {
		t.Errorf("order = %+v, want sources 1, 2, 10, b", first)
	}
}
