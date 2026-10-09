package web

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// The backlog graph's layout. Go computes integer coordinates in viewBox
// units and the browser scales them, exactly as the statistics charts do -
// there is no geometry in the template, no inline style, and no client-side
// layout pass. The whole picture is a pure function of the graph data, the
// configured relation types and the highlighted task, so two renders of an
// unchanged backlog are byte-identical and the five-second poll does not
// reshuffle what the reader is looking at.
//
// Shape of the layout: connected components (undirected), each laid out
// top-down in layers, the layer of a node being the longest path to it. Tasks
// with no edges at all are not components worth a column each - they go in a
// trailing block under a divider, wrapped to the layout width, so a backlog
// of mostly unrelated tasks does not stretch the connected part flat.

// Graph geometry, in viewBox units. A unit is a pixel at 1:1, which is what
// the numbers are chosen to look right at; the viewBox scales from there.
const (
	graphNodeW   = 180
	graphNodeH   = 44
	graphGapX    = 20
	graphGapY    = 72
	graphPad     = 12
	graphCols    = 5 // isolated block: nodes per row
	graphLabelCh = 24
	graphIDCh    = 18
)

// graphEdgeClasses is how many positional classes exist for relation types.
// A type's class is its index in the configured list modulo this, so a custom
// relation type is never invisible and a type name never becomes a class -
// the same rule the statistics charts use for a split card's values.
const graphEdgeClasses = 6

// GraphView is the whole drawn graph: a viewBox, the nodes and edges in it,
// and the legend. Width and Height are viewBox units.
type GraphView struct {
	Width  int
	Height int
	Nodes  []GraphNodeView
	Edges  []GraphEdgeView
	Legend []GraphLegendView
	// Isolated is the y of the divider above the unconnected tasks, and
	// IsolatedLabel its caption; both are zero when every task has an edge.
	Isolated      int
	IsolatedLabel string
	// Empty is an empty backlog, which draws no svg at all.
	Empty bool
	// Highlight is the id the view was asked to mark, kept so a template
	// can say whose graph this is.
	Highlight string
}

// GraphNodeView is one node box.
type GraphNodeView struct {
	ID     string
	X, Y   int
	W, H   int
	Label  string // the title, truncated to fit the box
	IDText string // the id, truncated
	Title  string // the full "id - title", the tooltip and accessible name
	Class  string // status, dimming, blocked and highlight, all named classes
	Href   string
	HXGet  string
}

// GraphEdgeView is one edge line.
type GraphEdgeView struct {
	X1, Y1, X2, Y2 int
	Class          string
	// Marker is "url(#graph-arrow)" for a directed edge, empty for a
	// symmetric one. The template cannot build it: an attribute that is
	// sometimes absent is a branch, and this keeps it to one.
	Marker string
	Title  string
}

// GraphLegendView is one entry of the legend: the edge kind and its class.
type GraphLegendView struct {
	Label string
	Class string
}

// newGraphView lays out a backlog graph. highlight may be empty, which is the
// graph opened from the board.
//
// A node's link does not append to the chain the graph sits in, the way every
// other link in this package does: it is the depth-0 chain rooted at that
// node plus its own graph column. The graph is a view of the whole backlog,
// so clicking a node is jumping to an unrelated task - the acceptance
// criterion is "opens that task in the workspace, highlight moved to it" -
// and appending would leave the previous task's column to the left of a graph
// that no longer marks it, growing the rail by one stale rung per click.
// Re-rooting also means the state a node click produces is the very state
// that task's own panel link produces: one state, one URL.
func newGraphView(g *task.BacklogGraph, cfg *config.Config, highlight string) GraphView {
	v := GraphView{Highlight: highlight, Legend: graphLegend(cfg)}
	if g == nil || len(g.Nodes) == 0 {
		v.Empty = true
		return v
	}

	nodes := make(map[string]*task.GraphNode, len(g.Nodes))
	order := make([]string, 0, len(g.Nodes))
	for i := range g.Nodes {
		nodes[g.Nodes[i].ID] = &g.Nodes[i]
		order = append(order, g.Nodes[i].ID)
	}

	degree := map[string]int{}
	for _, e := range g.Edges {
		degree[e.Source]++
		degree[e.Target]++
	}

	// Connected tasks are laid out in components; the rest go in the
	// trailing block.
	var connected, isolated []string
	for _, id := range order {
		if degree[id] > 0 {
			connected = append(connected, id)
		} else {
			isolated = append(isolated, id)
		}
	}

	pos := map[string]struct{ x, y int }{}
	y := graphPad

	comps := components(connected, g.Edges)
	layers := layerMap(g.Edges, order)
	for _, comp := range comps {
		byLayer := map[int][]string{}
		depth := 0
		for _, id := range comp {
			l := layers[id]
			byLayer[l] = append(byLayer[l], id)
			if l > depth {
				depth = l
			}
		}
		for l := 0; l <= depth; l++ {
			row := byLayer[l]
			if len(row) == 0 {
				continue
			}
			sortNodes(row, nodes)
			x := graphPad
			for _, id := range row {
				pos[id] = struct{ x, y int }{x, y}
				x += graphNodeW + graphGapX
			}
			y += graphNodeH + graphGapY
		}
		// One blank layer between components, so two trees do not read
		// as one.
		y += graphGapY / 2
	}

	if len(isolated) > 0 {
		if len(connected) > 0 {
			v.Isolated = y
			y += graphGapY / 2
		}
		v.IsolatedLabel = strconv.Itoa(len(isolated)) + " with no relations"
		sortNodes(isolated, nodes)
		for i, id := range isolated {
			col, row := i%graphCols, i/graphCols
			x := graphPad + col*(graphNodeW+graphGapX)
			pos[id] = struct{ x, y int }{x, y + row*(graphNodeH+graphGapY/3)}
		}
	}

	// The viewBox comes from where the nodes actually ended up, not from
	// the cursor the loops left behind: the connected part advances by one
	// gap per layer while the isolated block wraps on a smaller one, so a
	// cursor-derived height clips the last row.
	v.Width, v.Height = graphNodeW+2*graphPad, graphNodeH+2*graphPad
	for _, p := range pos {
		if right := p.x + graphNodeW + graphPad; right > v.Width {
			v.Width = right
		}
		if bottom := p.y + graphNodeH + graphPad; bottom > v.Height {
			v.Height = bottom
		}
	}

	for _, id := range order {
		n := nodes[id]
		p := pos[id]
		open := Chain{Root: id}.Append(KindGraph, id)
		v.Nodes = append(v.Nodes, GraphNodeView{
			ID:     id,
			X:      p.x,
			Y:      p.y,
			W:      graphNodeW,
			H:      graphNodeH,
			Label:  truncateRunes(n.Title, graphLabelCh),
			IDText: truncateRunes("#"+id, graphIDCh),
			Title:  graphNodeTitle(n),
			Class:  graphNodeClass(n, highlight),
			Href:   open.Path(),
			HXGet:  open.Fragment(),
		})
	}

	relClass := relationClasses(cfg)
	for _, e := range g.Edges {
		from, okFrom := pos[e.Source]
		to, okTo := pos[e.Target]
		if !okFrom || !okTo {
			continue
		}
		ev := GraphEdgeView{
			X1:    from.x + graphNodeW/2,
			Y1:    from.y + graphNodeH,
			X2:    to.x + graphNodeW/2,
			Y2:    to.y,
			Class: edgeClass(e, relClass),
			Title: e.Source + " " + e.Type + " " + e.Target,
		}
		if !e.Symmetric {
			ev.Marker = "url(#graph-arrow)"
		}
		v.Edges = append(v.Edges, ev)
	}
	return v
}

// components groups ids into connected components, treating every edge as
// undirected. Components come back largest first, ties broken by their
// smallest member, and each component's ids in the order they were given -
// so the result is a total order and a render is stable.
func components(ids []string, edges []task.GraphEdge) [][]string {
	parent := make(map[string]string, len(ids))
	for _, id := range ids {
		parent[id] = id
	}
	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b string) {
		if _, ok := parent[a]; !ok {
			return
		}
		if _, ok := parent[b]; !ok {
			return
		}
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for _, e := range edges {
		union(e.Source, e.Target)
	}

	groups := map[string][]string{}
	for _, id := range ids {
		root := find(id)
		groups[root] = append(groups[root], id)
	}
	out := make([][]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i][0] < out[j][0]
	})
	return out
}

// layerMap is each node's layer: the length of the longest directed path
// ending at it. It is a memoized DFS with a visiting set, so an edge that
// closes a cycle contributes nothing and the layout terminates - a blocked_by
// cycle is possible (cycle detection is not implemented) and must not hang a
// render.
//
// order is why this takes the node list and not only the edges: inside a
// cycle every node's depth depends on where the walk started, so walking from
// a map's iteration order would move the picture between two renders of the
// same backlog. Seeding it from the node order makes a cycle lay out the same
// way every time.
func layerMap(edges []task.GraphEdge, order []string) map[string]int {
	preds := map[string][]string{}
	for _, e := range edges {
		// A symmetric edge has no direction, so it cannot order two
		// nodes into layers; it only joins their component.
		if e.Symmetric {
			continue
		}
		preds[e.Target] = append(preds[e.Target], e.Source)
	}
	depth := map[string]int{}
	visiting := map[string]bool{}
	var walk func(string) int
	walk = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		if visiting[id] {
			return 0
		}
		visiting[id] = true
		best := 0
		for _, p := range preds[id] {
			if d := walk(p) + 1; d > best {
				best = d
			}
		}
		visiting[id] = false
		depth[id] = best
		return best
	}
	for _, id := range order {
		walk(id)
	}
	return depth
}

// sortNodes orders a row the way the board orders a column: priority first,
// then the older task, then the id - so the graph and the board agree about
// what comes first.
func sortNodes(ids []string, nodes map[string]*task.GraphNode) {
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := nodes[ids[i]], nodes[ids[j]]
		if a == nil || b == nil {
			return ids[i] < ids[j]
		}
		if a.Priority.Order() != b.Priority.Order() {
			return a.Priority.Order() < b.Priority.Order()
		}
		return ids[i] < ids[j]
	})
}

// graphNodeClass names a node's status, its dimming, its blocked mark and the
// highlight. Every one is a declared class: nothing about the picture is
// assembled as a style.
func graphNodeClass(n *task.GraphNode, highlight string) string {
	// The status class is also what dims a done node - graph-done is the
	// status, so there is nothing extra to add for it.
	classes := []string{"graph-node", "graph-" + string(n.Status)}
	if n.Blocked {
		classes = append(classes, "graph-blocked")
	}
	if highlight != "" && n.ID == highlight {
		classes = append(classes, "graph-current")
	}
	return strings.Join(classes, " ")
}

// graphNodeTitle is the tooltip and the accessible name: everything the box
// had to truncate, spelled out.
func graphNodeTitle(n *task.GraphNode) string {
	out := "#" + n.ID + " " + n.Title + " (" + string(n.Status)
	if n.Blocked {
		out += ", blocked"
	}
	if n.Resolution != "" && n.Status == task.StatusDone {
		out += ", " + string(n.Resolution)
	}
	return out + ")"
}

// relationClasses maps a configured relation type to its positional class.
func relationClasses(cfg *config.Config) map[string]string {
	out := map[string]string{}
	for i, name := range relationTypes(cfg) {
		out[name] = "graph-rel-" + strconv.Itoa(i%graphEdgeClasses)
	}
	return out
}

// edgeClass is an edge's class: the parent tree has its own, a relation type
// gets its positional one, and a type that is not configured at all - a
// record written before the config changed - falls back to the last class
// rather than rendering invisibly.
func edgeClass(e task.GraphEdge, relClass map[string]string) string {
	if e.Type == task.GraphEdgeTypeParent {
		return "graph-edge graph-edge-parent"
	}
	class, ok := relClass[e.Type]
	if !ok {
		class = "graph-rel-" + strconv.Itoa(graphEdgeClasses-1)
	}
	return "graph-edge " + class
}

// graphLegend lists the edge kinds in a fixed order: the parent tree, then
// the relation types as configured.
func graphLegend(cfg *config.Config) []GraphLegendView {
	out := []GraphLegendView{{Label: "parent", Class: "graph-edge-parent"}}
	for i, name := range relationTypes(cfg) {
		out = append(out, GraphLegendView{
			Label: name,
			Class: "graph-rel-" + strconv.Itoa(i%graphEdgeClasses),
		})
	}
	return out
}

// relationTypes is the configured list, defaulting the way the rest of the
// package does for a nil config (the board view does the same).
func relationTypes(cfg *config.Config) []string {
	if cfg == nil || len(cfg.RelationTypes) == 0 {
		return config.DefaultRelationTypes
	}
	return cfg.RelationTypes
}

// truncateRunes cuts a label to n runes with an ellipsis, because SVG has no
// text-overflow: a <text> element draws whatever it is given, past the end of
// its box. The full string is always in the node's <title>.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	if n <= 1 {
		return string(runes[:n])
	}
	return strings.TrimRight(string(runes[:n-1]), " ") + "…"
}
