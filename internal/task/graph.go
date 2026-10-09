package task

import "time"

// GraphEdgeTypeParent is the edge type of the parent -> subtask relationship.
// It is not a relation type and never appears in a record's frontmatter: the
// edge is derived from ParentID. The name is a constant so no consumer spells
// it, and so it cannot collide with a configured relation type - a relation
// type called "parent" would be a configuration mistake, and this constant is
// where that would show up.
const GraphEdgeTypeParent = "parent"

// GraphNode is one task in the backlog graph: what a node has to draw itself
// and to say which task it is. It is an index projection, so there is no
// description here.
type GraphNode struct {
	ID       string
	Title    string
	Type     string
	Status   Status
	Priority Priority
	// ParentID is kept even though the parent edge is in Edges: a renderer
	// may want to group a tree without walking the edge list.
	ParentID string
	// Blocked is the card's own meaning: at least one blocker that is not
	// done.
	Blocked bool
	// Resolution is a done task's effective resolution, empty otherwise.
	Resolution Resolution
}

// GraphEdge is one edge of the backlog graph.
//
// Symmetric says the edge has no direction, which is what a renderer needs to
// decide whether to draw an arrowhead. It is set by the index rather than
// derived here, because the index owns the rule: a symmetric relation type is
// stored in both directions, and the index is what generates the reverse edge
// and what drops it again when a whole-graph read asks for each edge once.
type GraphEdge struct {
	Type      string
	Source    string
	Target    string
	Symmetric bool
}

// BacklogGraph is the active backlog as a graph: every indexed task as a
// node, the parent edges, and every stored relation once.
//
// It is a read composite in the shape of BoardSnapshot: one lock, one index
// pass, and no archive scan - an archived task is out of the index and its
// relations were cleaned when it was archived, so there is nothing of it to
// draw.
type BacklogGraph struct {
	Nodes   []GraphNode
	Edges   []GraphEdge
	TakenAt time.Time
}

// BacklogGraph reads the whole active backlog as a graph.
func (s *Service) BacklogGraph() (*BacklogGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backlogGraph()
}

func (s *Service) backlogGraph() (*BacklogGraph, error) {
	tasks := s.index.All()

	known := make(map[string]bool, len(tasks))
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		known[t.ID] = true
		ids = append(ids, t.ID)
	}
	// The same map the board marks its cards from: only blockers that are
	// not done count.
	blocked := s.blockedMap(ids)

	g := &BacklogGraph{
		Nodes:   make([]GraphNode, 0, len(tasks)),
		TakenAt: s.now().UTC(),
	}
	for _, t := range tasks {
		g.Nodes = append(g.Nodes, GraphNode{
			ID:         t.ID,
			Title:      t.Title,
			Type:       t.Type,
			Status:     t.Status,
			Priority:   t.Priority,
			ParentID:   t.ParentID,
			Blocked:    len(blocked[t.ID]) > 0,
			Resolution: t.EffectiveResolution(),
		})
		// A parent edge only exists when the parent is a node too. A
		// subtask whose parent was deleted by hand keeps its parent_id
		// (orphaned_id provenance is hand-set), and an edge to a task
		// that is not there would be an edge to nothing.
		if t.ParentID != "" && known[t.ParentID] {
			g.Edges = append(g.Edges, GraphEdge{
				Type:   GraphEdgeTypeParent,
				Source: t.ParentID,
				Target: t.ID,
			})
		}
	}
	// The index returns each relation once, flags the symmetric ones and
	// has already dropped the edges whose endpoints are not tasks.
	g.Edges = append(g.Edges, s.index.AllRelations()...)
	return g, nil
}
