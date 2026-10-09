package web

import (
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// The kind registry is the tiler's extension point. One entry declares
// everything a column type needs - its URL tag, the class that gives it a
// width, its rail label, the fragment that draws it, and how it turns a ref
// into view data - so adding a kind is one entry plus one template and the
// chain, the layout and the routes are not reopened. web-backlog-graph adds a
// "g" entry here and nothing else.
//
// A kind's own data type lives beside it rather than in view.go: that is what
// keeps a new kind additive.

// colCtx is everything a kind needs to resolve one column. TaskID is the task
// the column hangs off (Chain.TaskAt), which for a file column is whose file
// Ref names. Chain and At say where the column sits, which is what a kind
// needs to offer links of its own: appending to Chain.TruncateTo(At+1) is the
// state that opening something from this column leads to.
type colCtx struct {
	Svc    *task.Service
	Cfg    *config.Config
	Now    time.Time
	TaskID string
	Ref    string
	Chain  Chain
	At     int
	// Titles resolves relation targets to their titles. It is memoized by
	// the caller and shared by every column of one render, so a render
	// takes at most one BoardSnapshot for titles however deep the chain
	// is - and none at all when nothing has a relation.
	Titles func() map[string]string
	// Next is the column immediately to the right, or nil at the end of
	// the chain. A column marks whichever of its own items that column
	// shows, so the marking is derived from the URL and nothing else.
	Next *Column
}

// here is the chain up to and including this column: what a link opened from
// it appends to.
func (c colCtx) here() Chain { return c.Chain.TruncateTo(c.At + 1) }

// columnKind describes one column type.
type columnKind struct {
	// Tag is the kind's URL segment.
	Tag ColumnKind
	// Class gives the column its width. It is a component class declared
	// in input.css, not a utility assembled here: Tailwind scans only the
	// templates, so a class named in Go would be dropped from app.css.
	Class string
	// Label prefixes the kind's rail entry.
	Label string
	// Template is the fragment that draws the column body.
	Template string
	// Resolve turns the ref into the template's data, or reports that the
	// column names nothing - which the handler answers with 404.
	Resolve func(colCtx) (any, bool)
}

// columnKinds is immutable after init; nothing writes to it at request time.
var columnKinds = map[ColumnKind]columnKind{
	KindTask: {
		Tag:      KindTask,
		Class:    "kind-task",
		Label:    "task",
		Template: "_col_task.html",
		Resolve:  resolveTaskColumn,
	},
	KindFile: {
		Tag:      KindFile,
		Class:    "kind-file",
		Label:    "file",
		Template: "_col_file.html",
		Resolve:  resolveFileColumn,
	},
	KindDesc: {
		Tag:      KindDesc,
		Class:    "kind-desc",
		Label:    "description",
		Template: "_col_desc.html",
		Resolve:  resolveDescColumn,
	},
	KindGraph: {
		Tag:      KindGraph,
		Class:    "kind-graph",
		Label:    "graph",
		Template: "_col_graph.html",
		Resolve:  resolveGraphColumn,
	},
}

// TaskColumnView is a task column's body. A task column is the same plate the
// side panel shows - the task's detail view - so moving a task to the left of
// the strip does not change how it looks. All this type adds is that its links
// are rebased onto the column's own place in the chain.
type TaskColumnView struct {
	Detail DetailView
}

// FileColumnView is a file column. Body is the file rendered as a document or
// shown as text, decided by its name (body.go).
type FileColumnView struct {
	Name    string
	RawHref string
	Body    bodyView
}

// DescColumnView is a description column: whose description it is, and the
// text as a document. It goes through the same bodyView as a file, so "open
// the description" and "open a file" really are the same working area.
type DescColumnView struct {
	ID    string
	Title string
	Body  bodyView
	// TaskHref opens the task itself as a column, so a reader who came in
	// on a deep link can get at the record the text belongs to.
	TaskHref string
}

// resolveTaskColumn reads the task the column names. Detail falls back to the
// archive (internal/task/service.go), so an archived task has a column too,
// read-only like the rest of the dashboard.
func resolveTaskColumn(c colCtx) (any, bool) {
	detail, err := c.Svc.Detail(c.Ref)
	if err != nil {
		return nil, false
	}
	var titles map[string]string
	if len(detail.Relations) > 0 && c.Titles != nil {
		titles = c.Titles()
	}
	v := TaskColumnView{Detail: newDetailView(detail, c.Cfg, titles, c.Now)}
	// newDetailView links everything into the chain rooted at the task
	// itself, which is right for the side panel - the depth-0 workspace.
	// A column sits further along, so its links append to where it is
	// rather than re-rooting the chain on it, and it marks the item the
	// column to its right shows.
	v.Detail.chainLinks(c.here(), c.Next)
	return v, true
}

// GraphColumnView is a graph column's body: the laid-out graph, and which
// task it was asked to highlight.
type GraphColumnView struct {
	Graph GraphView
}

// resolveGraphColumn reads the whole active backlog as a graph and lays it
// out with this column's ref highlighted. The ref is a task id - the one
// state with nothing to highlight is the graph opened from the board, which
// is a route of its own (handlers.go) because a chain always has a root.
//
// A ref that is not a node is 404, like every other ref that names nothing:
// the highlight is part of the URL, so a URL naming a task that is not in the
// backlog names nothing.
func resolveGraphColumn(c colCtx) (any, bool) {
	g, err := c.Svc.BacklogGraph()
	if err != nil {
		return nil, false
	}
	// An empty ref is the rootless graph, which highlights nothing: a
	// chain column always has a ref (ParseChain refuses an empty segment),
	// so this can only be the board's entry point.
	if c.Ref != "" {
		found := false
		for _, n := range g.Nodes {
			if n.ID == c.Ref {
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return GraphColumnView{Graph: newGraphView(g, c.Cfg, c.Ref)}, true
}

// resolveDescColumn reads the task whose description the column shows. An
// empty description is still a column: the chain names a task, not a non-empty
// text, and the fragment says there is none.
func resolveDescColumn(c colCtx) (any, bool) {
	detail, err := c.Svc.Detail(c.Ref)
	if err != nil {
		return nil, false
	}
	return DescColumnView{
		ID:    detail.Task.ID,
		Title: detail.Task.Title,
		// No file name, so the body renders as a document - which is
		// what a description is.
		Body:     newBodyView("", detail.Task.Description),
		TaskHref: c.here().Append(KindTask, detail.Task.ID).Path(),
	}, true
}

// resolveFileColumn reads one attached file of the column's task. The name was
// already shape-checked by ParseChain; ReadTaskFile reports a missing task or
// a missing file the same way, and both mean the URL names nothing.
func resolveFileColumn(c colCtx) (any, bool) {
	if err := task.ValidateAttachedName(c.Ref); err != nil {
		return nil, false
	}
	// The server's own records are not task artifacts. storage.ReadFile
	// validates a read without the reserved-name rule - that rule is about
	// writes, and read_task_file documents a *.phase file as readable - so
	// without this a phase record, or the task's own {id}.md, would render
	// as a column although no surface links one (ListFiles drops the
	// record, TaskDetail.Files drops the records). A name nothing offers
	// is a URL that names nothing, which is the tiler's one 404.
	if task.IsReservedFileName(c.Ref) || c.Ref == c.TaskID+".md" {
		return nil, false
	}
	content, err := c.Svc.ReadTaskFile(c.TaskID, c.Ref)
	if err != nil {
		return nil, false
	}
	return FileColumnView{
		Name:    c.Ref,
		RawHref: taskFileHref(c.TaskID, c.Ref),
		Body:    newBodyView(c.Ref, content),
	}, true
}
