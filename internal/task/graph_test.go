package task

import (
	"testing"
)

// graphOf is a service over the mock index with a small backlog in it.
func graphOf(t *testing.T) (*Service, *mockStorage, *mockArchiveStorage) {
	t.Helper()
	ms := newMockStorage()
	as := newMockArchiveStorage(ms)
	svc := NewService(ms, as, nil, newMockIndex(), []string{"feature", "bug"}, nil)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return svc, ms, as
}

func nodeByID(g *BacklogGraph, id string) (GraphNode, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return GraphNode{}, false
}

func hasEdge(g *BacklogGraph, typ, source, target string) bool {
	for _, e := range g.Edges {
		if e.Type == typ && e.Source == source && e.Target == target {
			return true
		}
	}
	return false
}

func TestBacklogGraphNodes(t *testing.T) {
	svc, _, _ := graphOf(t)
	parent, _ := svc.Create("Parent", "d", PriorityHigh, "feature", "", "")
	sub, _ := svc.Create("Sub", "d", PriorityLow, "bug", parent.ID, "")
	blocked, _ := svc.Create("Blocked", "d", PriorityMedium, "feature", "", "")
	if err := svc.AddRelation(blocked.ID, "blocked_by", parent.ID); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}

	g, err := svc.BacklogGraph()
	if err != nil {
		t.Fatalf("BacklogGraph() error = %v", err)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3: %+v", len(g.Nodes), g.Nodes)
	}
	if g.TakenAt.IsZero() {
		t.Error("TakenAt was not set")
	}

	p, ok := nodeByID(g, parent.ID)
	if !ok {
		t.Fatal("the parent is not a node")
	}
	if p.Title != "Parent" || p.Status != StatusTodo || p.Priority != PriorityHigh || p.Type != "feature" {
		t.Errorf("parent node = %+v", p)
	}
	if p.ParentID != "" {
		t.Errorf("a top-level task has ParentID %q", p.ParentID)
	}

	s, _ := nodeByID(g, sub.ID)
	if s.ParentID != parent.ID {
		t.Errorf("subtask ParentID = %q, want %q", s.ParentID, parent.ID)
	}

	// Blocked is the card's own meaning: a blocker that is not done.
	b, _ := nodeByID(g, blocked.ID)
	if !b.Blocked {
		t.Error("a task with an open blocker is not marked blocked")
	}
	if p.Blocked {
		t.Error("the blocker itself is marked blocked")
	}
}

func TestBacklogGraphParentEdges(t *testing.T) {
	svc, _, _ := graphOf(t)
	parent, _ := svc.Create("Parent", "", PriorityMedium, "feature", "", "")
	sub, _ := svc.Create("Sub", "", PriorityMedium, "feature", parent.ID, "")

	g, _ := svc.BacklogGraph()
	if !hasEdge(g, GraphEdgeTypeParent, parent.ID, sub.ID) {
		t.Errorf("no parent edge %s -> %s in %+v", parent.ID, sub.ID, g.Edges)
	}
	if len(g.Edges) != 1 {
		t.Errorf("got %d edges, want only the parent edge: %+v", len(g.Edges), g.Edges)
	}
}

// TestBacklogGraphSkipsOrphanedParentEdge: a subtask whose parent was removed
// by hand keeps its parent_id (orphaned_id is provenance only), and an edge to
// a task that is not a node would be an edge to nothing.
func TestBacklogGraphSkipsOrphanedParentEdge(t *testing.T) {
	svc, _, _ := graphOf(t)
	orphan, err := svc.Create("Orphan", "", PriorityMedium, "feature", "", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// A parent_id naming a task that is not there, which is what a deleted
	// parent or a hand-edited record leaves behind.
	loaded, _ := svc.Get(orphan.ID)
	loaded.ParentID = "ghost"
	svc.index.Set(loaded)

	g, _ := svc.BacklogGraph()
	if len(g.Edges) != 0 {
		t.Errorf("an edge names a task that is not a node: %+v", g.Edges)
	}
	// The node keeps its parent_id: the record is the truth, the edge is
	// the thing that cannot be drawn.
	if n, ok := nodeByID(g, orphan.ID); !ok || n.ParentID != "ghost" {
		t.Errorf("node = %+v, want it to keep parent_id ghost", n)
	}
}

func TestBacklogGraphRelationEdges(t *testing.T) {
	svc, _, _ := graphOf(t)
	a, _ := svc.Create("A", "", PriorityMedium, "feature", "", "")
	b, _ := svc.Create("B", "", PriorityMedium, "feature", "", "")
	if err := svc.AddRelation(a.ID, "relates_to", b.ID); err != nil {
		t.Fatalf("AddRelation(relates_to) error = %v", err)
	}
	if err := svc.AddRelation(a.ID, "blocked_by", b.ID); err != nil {
		t.Fatalf("AddRelation(blocked_by) error = %v", err)
	}

	g, _ := svc.BacklogGraph()
	// relates_to is stored in both directions and must appear once.
	var relates, blocks int
	for _, e := range g.Edges {
		switch e.Type {
		case "relates_to":
			relates++
			if !e.Symmetric {
				t.Error("relates_to is not flagged symmetric")
			}
		case "blocked_by":
			blocks++
			if e.Symmetric {
				t.Error("blocked_by is flagged symmetric")
			}
			if e.Source != a.ID || e.Target != b.ID {
				t.Errorf("blocked_by %s -> %s, want %s -> %s", e.Source, e.Target, a.ID, b.ID)
			}
		}
	}
	if relates != 1 {
		t.Errorf("got %d relates_to edges, want 1", relates)
	}
	if blocks != 1 {
		t.Errorf("got %d blocked_by edges, want 1", blocks)
	}
}

// TestBacklogGraphExcludesArchived: the graph is the active index, and
// archiving already cleaned the task's relations.
func TestBacklogGraphExcludesArchived(t *testing.T) {
	svc, _, _ := graphOf(t)
	keep, _ := svc.Create("Keep", "", PriorityMedium, "feature", "", "")
	gone, _ := svc.Create("Gone", "", PriorityMedium, "feature", "", "")
	if err := svc.AddRelation(keep.ID, "relates_to", gone.ID); err != nil {
		t.Fatalf("AddRelation() error = %v", err)
	}
	if _, err := svc.StartTask(gone.ID); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if _, err := svc.CompleteTask(gone.ID); err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if err := svc.ArchiveTask(gone.ID); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}

	g, _ := svc.BacklogGraph()
	if _, ok := nodeByID(g, gone.ID); ok {
		t.Error("an archived task is a node")
	}
	for _, e := range g.Edges {
		if e.Source == gone.ID || e.Target == gone.ID {
			t.Errorf("an edge still names the archived task: %+v", e)
		}
	}
}

func TestBacklogGraphEmpty(t *testing.T) {
	svc, _, _ := graphOf(t)
	g, err := svc.BacklogGraph()
	if err != nil {
		t.Fatalf("BacklogGraph() error = %v", err)
	}
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Errorf("an empty backlog yielded %+v", g)
	}
}
