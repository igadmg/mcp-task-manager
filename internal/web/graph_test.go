package web

import (
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

func gn(id string, status task.Status, priority task.Priority) task.GraphNode {
	return task.GraphNode{ID: id, Title: "task " + id, Type: "feature", Status: status, Priority: priority}
}

func graphData(nodes []task.GraphNode, edges ...task.GraphEdge) *task.BacklogGraph {
	return &task.BacklogGraph{Nodes: nodes, Edges: edges}
}

func nodeView(v GraphView, id string) (GraphNodeView, bool) {
	for _, n := range v.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return GraphNodeView{}, false
}

func TestGraphViewEmptyBacklog(t *testing.T) {
	v := newGraphView(graphData(nil), nil, "")
	if !v.Empty {
		t.Error("an empty backlog is not marked empty")
	}
	if len(v.Nodes) != 0 || v.Width != 0 {
		t.Errorf("an empty graph drew something: %+v", v)
	}
	// The legend is still there: it describes the edge kinds, not the data.
	if len(v.Legend) == 0 {
		t.Error("no legend")
	}
	if v := newGraphView(nil, nil, ""); !v.Empty {
		t.Error("a nil graph is not marked empty")
	}
}

// TestGraphViewLayersByLongestPath is the layout's core: a chain of edges
// becomes a chain of layers, and a node with two predecessors sits below the
// deeper one.
func TestGraphViewLayersByLongestPath(t *testing.T) {
	v := newGraphView(graphData(
		[]task.GraphNode{
			gn("a", task.StatusTodo, task.PriorityMedium),
			gn("b", task.StatusTodo, task.PriorityMedium),
			gn("c", task.StatusTodo, task.PriorityMedium),
		},
		task.GraphEdge{Type: "parent", Source: "a", Target: "b"},
		task.GraphEdge{Type: "parent", Source: "b", Target: "c"},
		task.GraphEdge{Type: "blocked_by", Source: "a", Target: "c"},
	), nil, "")

	a, _ := nodeView(v, "a")
	b, _ := nodeView(v, "b")
	c, _ := nodeView(v, "c")
	if !(a.Y < b.Y && b.Y < c.Y) {
		t.Errorf("layers are not top-down: a=%d b=%d c=%d", a.Y, b.Y, c.Y)
	}
	// One node per layer here, so each starts at the same x.
	if a.X != b.X || b.X != c.X {
		t.Errorf("single-node layers are not aligned: %d %d %d", a.X, b.X, c.X)
	}
	// c has two predecessors (b at layer 1, a at layer 0): the longest
	// path wins, so it is two layers down and not one.
	if c.Y-b.Y != b.Y-a.Y {
		t.Errorf("c did not take the longest path: a=%d b=%d c=%d", a.Y, b.Y, c.Y)
	}
	if v.Height < c.Y+c.H {
		t.Errorf("height %d clips the last layer, which ends at %d", v.Height, c.Y+c.H)
	}
}

// TestGraphViewCycleTerminates: a blocked_by cycle is possible today (cycle
// detection is a future consideration), so the layering must not hang or
// recurse forever.
func TestGraphViewCycleTerminates(t *testing.T) {
	done := make(chan GraphView, 1)
	go func() {
		done <- newGraphView(graphData(
			[]task.GraphNode{
				gn("1", task.StatusTodo, task.PriorityMedium),
				gn("2", task.StatusTodo, task.PriorityMedium),
				gn("3", task.StatusTodo, task.PriorityMedium),
			},
			task.GraphEdge{Type: "blocked_by", Source: "1", Target: "2"},
			task.GraphEdge{Type: "blocked_by", Source: "2", Target: "3"},
			task.GraphEdge{Type: "blocked_by", Source: "3", Target: "1"},
		), nil, "")
	}()
	v := <-done
	if len(v.Nodes) != 3 {
		t.Fatalf("a cyclic graph laid out %d nodes, want 3", len(v.Nodes))
	}
	if len(v.Edges) != 3 {
		t.Errorf("a cyclic graph drew %d edges, want 3", len(v.Edges))
	}
	// Inside a cycle every depth depends on where the walk started, so a
	// render must not depend on a map's iteration order either.
	nodes := []task.GraphNode{
		gn("1", task.StatusTodo, task.PriorityMedium),
		gn("2", task.StatusTodo, task.PriorityMedium),
		gn("3", task.StatusTodo, task.PriorityMedium),
	}
	edges := []task.GraphEdge{
		{Type: "blocked_by", Source: "1", Target: "2"},
		{Type: "blocked_by", Source: "2", Target: "3"},
		{Type: "blocked_by", Source: "3", Target: "1"},
	}
	first := newGraphView(graphData(nodes, edges...), nil, "")
	for i := 0; i < 20; i++ {
		again := newGraphView(graphData(nodes, edges...), nil, "")
		for j := range first.Nodes {
			if again.Nodes[j] != first.Nodes[j] {
				t.Fatalf("run %d: a cycle laid out differently: %+v vs %+v",
					i, again.Nodes[j], first.Nodes[j])
			}
		}
	}
}

// TestGraphViewIsolatedBlock: tasks with no edges at all would stretch the
// connected part flat if they each took a layer, so they go in a trailing
// grid under a divider.
func TestGraphViewIsolatedBlock(t *testing.T) {
	nodes := []task.GraphNode{
		gn("a", task.StatusTodo, task.PriorityMedium),
		gn("b", task.StatusTodo, task.PriorityMedium),
	}
	for _, id := range []string{"i1", "i2", "i3", "i4", "i5", "i6"} {
		nodes = append(nodes, gn(id, task.StatusTodo, task.PriorityMedium))
	}
	v := newGraphView(graphData(nodes,
		task.GraphEdge{Type: "parent", Source: "a", Target: "b"},
	), nil, "")

	if v.Isolated == 0 {
		t.Error("no divider above the unconnected tasks")
	}
	if !strings.Contains(v.IsolatedLabel, "6") {
		t.Errorf("IsolatedLabel = %q, want it to count the six", v.IsolatedLabel)
	}
	b, _ := nodeView(v, "b")
	i1, _ := nodeView(v, "i1")
	if i1.Y <= b.Y {
		t.Errorf("an unconnected task sits above the connected part: %d <= %d", i1.Y, b.Y)
	}
	// Six of them wrap, so the seventh column does not exist.
	i6, _ := nodeView(v, "i6")
	if i6.Y == i1.Y {
		t.Error("the isolated block did not wrap")
	}
	// The whole box has to fit, not just its top edge: the connected part
	// advances by a full gap per layer while this block wraps on a smaller
	// one, which is exactly how a cursor-derived height clips the last row.
	if v.Height < i6.Y+i6.H {
		t.Errorf("height %d clips the last isolated row, which ends at %d", v.Height, i6.Y+i6.H)
	}
	for _, n := range v.Nodes {
		if n.Y+n.H > v.Height || n.X+n.W > v.Width {
			t.Errorf("node %s (%d,%d %dx%d) falls outside the %dx%d viewBox",
				n.ID, n.X, n.Y, n.W, n.H, v.Width, v.Height)
		}
	}
}

// TestGraphViewIsolatedOnlyHasNoDivider: a divider under nothing would be a
// line across an otherwise empty picture.
func TestGraphViewIsolatedOnlyHasNoDivider(t *testing.T) {
	v := newGraphView(graphData([]task.GraphNode{
		gn("a", task.StatusTodo, task.PriorityMedium),
		gn("b", task.StatusTodo, task.PriorityMedium),
	}), nil, "")
	if v.Isolated != 0 {
		t.Errorf("Isolated = %d, want no divider when nothing is connected", v.Isolated)
	}
	if len(v.Nodes) != 2 {
		t.Errorf("got %d nodes", len(v.Nodes))
	}
}

func TestGraphViewNodeClasses(t *testing.T) {
	v := newGraphView(graphData([]task.GraphNode{
		{ID: "1", Title: "todo", Status: task.StatusTodo, Priority: task.PriorityMedium},
		{ID: "2", Title: "live", Status: task.StatusInProgress, Priority: task.PriorityMedium},
		{ID: "3", Title: "shipped", Status: task.StatusDone, Priority: task.PriorityMedium, Resolution: task.ResolutionCompleted},
		{ID: "4", Title: "stuck", Status: task.StatusTodo, Priority: task.PriorityMedium, Blocked: true},
	}), nil, "2")

	for _, c := range []struct{ id, want string }{
		{"1", "graph-todo"},
		{"2", "graph-in_progress"},
		{"3", "graph-done"},
		{"4", "graph-blocked"},
		{"2", "graph-current"},
	} {
		n, ok := nodeView(v, c.id)
		if !ok {
			t.Fatalf("node %s missing", c.id)
		}
		if !strings.Contains(n.Class, c.want) {
			t.Errorf("node %s class = %q, want %q in it", c.id, n.Class, c.want)
		}
	}
	// Exactly one node is the highlight.
	var current int
	for _, n := range v.Nodes {
		if strings.Contains(n.Class, "graph-current") {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d nodes are highlighted, want 1", current)
	}
	// And none when there is no highlight.
	v = newGraphView(graphData([]task.GraphNode{gn("1", task.StatusTodo, task.PriorityMedium)}), nil, "")
	if n, _ := nodeView(v, "1"); strings.Contains(n.Class, "graph-current") {
		t.Error("a graph with no highlight marked a node anyway")
	}
}

// TestGraphViewEdgeClassesFollowConfig: a relation type's style is its
// position in the configured list, so a custom type is never invisible and a
// type name never becomes a class.
func TestGraphViewEdgeClassesFollowConfig(t *testing.T) {
	cfg := &config.Config{RelationTypes: []string{"blocked_by", "relates_to", "spike_of"}}
	v := newGraphView(graphData(
		[]task.GraphNode{
			gn("1", task.StatusTodo, task.PriorityMedium),
			gn("2", task.StatusTodo, task.PriorityMedium),
			gn("3", task.StatusTodo, task.PriorityMedium),
			gn("4", task.StatusTodo, task.PriorityMedium),
		},
		task.GraphEdge{Type: "parent", Source: "1", Target: "2"},
		task.GraphEdge{Type: "blocked_by", Source: "2", Target: "3"},
		task.GraphEdge{Type: "spike_of", Source: "3", Target: "4"},
		task.GraphEdge{Type: "relates_to", Source: "1", Target: "4", Symmetric: true},
	), cfg, "")

	want := map[string]string{
		"1 parent 2":     "graph-edge-parent",
		"2 blocked_by 3": "graph-rel-0",
		"3 spike_of 4":   "graph-rel-2",
		"1 relates_to 4": "graph-rel-1",
	}
	for _, e := range v.Edges {
		if class, ok := want[e.Title]; ok && !strings.Contains(e.Class, class) {
			t.Errorf("edge %q class = %q, want %q", e.Title, e.Class, class)
		}
		// Only an asymmetric edge shows direction.
		if strings.Contains(e.Title, "relates_to") && e.Marker != "" {
			t.Errorf("a symmetric edge has an arrowhead: %+v", e)
		}
		if strings.Contains(e.Title, "blocked_by") && e.Marker == "" {
			t.Errorf("an asymmetric edge has no arrowhead: %+v", e)
		}
	}

	// The legend lists the parent tree and every configured type, in order.
	if len(v.Legend) != 4 {
		t.Fatalf("legend = %+v, want parent plus three types", v.Legend)
	}
	for i, label := range []string{"parent", "blocked_by", "relates_to", "spike_of"} {
		if v.Legend[i].Label != label {
			t.Errorf("legend[%d] = %q, want %q", i, v.Legend[i].Label, label)
		}
	}
	// An unconfigured type still gets a class rather than rendering blank.
	v = newGraphView(graphData(
		[]task.GraphNode{gn("1", task.StatusTodo, task.PriorityMedium), gn("2", task.StatusTodo, task.PriorityMedium)},
		task.GraphEdge{Type: "retired_type", Source: "1", Target: "2"},
	), cfg, "")
	if len(v.Edges) != 1 || !strings.Contains(v.Edges[0].Class, "graph-rel-") {
		t.Errorf("an unconfigured relation type lost its style: %+v", v.Edges)
	}
}

// TestGraphViewLabelsTruncateButKeepTheTitle: SVG has no text-overflow, so a
// long title has to be cut in Go - and the whole of it has to stay reachable.
func TestGraphViewLabelsTruncateButKeepTheTitle(t *testing.T) {
	long := "a title far longer than any node box could ever hope to show"
	v := newGraphView(graphData([]task.GraphNode{
		{ID: "long-custom-id-for-the-overflow-check", Title: long, Status: task.StatusTodo, Priority: task.PriorityMedium},
	}), nil, "")

	n := v.Nodes[0]
	if n.Label == long {
		t.Error("the label was not truncated")
	}
	if !strings.HasSuffix(n.Label, "…") {
		t.Errorf("Label = %q, want an ellipsis", n.Label)
	}
	if !strings.Contains(n.Title, long) {
		t.Errorf("Title = %q, want the whole title in it", n.Title)
	}
	if !strings.Contains(n.Title, "todo") {
		t.Errorf("Title = %q, want the status in it", n.Title)
	}
	if n.IDText == "#"+n.ID {
		t.Error("a long id was not truncated")
	}
}

// TestGraphViewNodeLinksReRoot: a node click jumps to that task, so its link
// is the depth-0 chain rooted there plus its own graph column - the very state
// that task's panel link produces - and not an append onto wherever the graph
// happened to be opened from.
func TestGraphViewNodeLinksReRoot(t *testing.T) {
	v := newGraphView(graphData([]task.GraphNode{gn("7", task.StatusTodo, task.PriorityMedium)}), nil, "")
	n := v.Nodes[0]
	if n.Href != "/tasks/7/w/g/7" {
		t.Errorf("Href = %q, want /tasks/7/w/g/7", n.Href)
	}
	if n.HXGet != "/strip/tasks/7/w/g/7" {
		t.Errorf("HXGet = %q", n.HXGet)
	}
}

// TestGraphViewIsDeterministic: the graph is re-rendered every five seconds.
func TestGraphViewIsDeterministic(t *testing.T) {
	nodes := []task.GraphNode{
		gn("1", task.StatusTodo, task.PriorityLow),
		gn("2", task.StatusTodo, task.PriorityCritical),
		gn("3", task.StatusInProgress, task.PriorityHigh),
		gn("4", task.StatusDone, task.PriorityMedium),
		gn("5", task.StatusTodo, task.PriorityMedium),
	}
	edges := []task.GraphEdge{
		{Type: "parent", Source: "1", Target: "2"},
		{Type: "parent", Source: "1", Target: "3"},
		{Type: "relates_to", Source: "4", Target: "5", Symmetric: true},
	}
	first := newGraphView(graphData(nodes, edges...), nil, "3")
	for i := 0; i < 5; i++ {
		again := newGraphView(graphData(nodes, edges...), nil, "3")
		if again.Width != first.Width || again.Height != first.Height {
			t.Fatalf("run %d: size changed", i)
		}
		for j := range first.Nodes {
			if again.Nodes[j] != first.Nodes[j] {
				t.Fatalf("run %d: node %d moved: %+v vs %+v", i, j, again.Nodes[j], first.Nodes[j])
			}
		}
	}
	// Two siblings of one parent share a layer, ordered by priority.
	two, _ := nodeView(first, "2")
	three, _ := nodeView(first, "3")
	if two.Y != three.Y {
		t.Errorf("siblings are on different layers: %d vs %d", two.Y, three.Y)
	}
	if !(two.X < three.X) {
		t.Errorf("critical should come before high: %d, %d", two.X, three.X)
	}
}
