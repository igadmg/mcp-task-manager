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
}

// TaskColumnView is a task column's body. A task column is the same plate the
// side panel shows - the task's detail view - so moving a task to the left of
// the strip does not change how it looks. All this type adds is that its links
// are rebased onto the column's own place in the chain.
type TaskColumnView struct {
	Detail DetailView
}

// FileColumnView is a file column's body. The content is plain text escaped
// by html/template; workspace-file-column renders it as markdown.
type FileColumnView struct {
	Name    string
	RawHref string
	Content string
}

// resolveTaskColumn reads the task the column names. Detail falls back to the
// archive (internal/task/service.go), so an archived task has a column too,
// read-only like the rest of the dashboard.
func resolveTaskColumn(c colCtx) (any, bool) {
	detail, err := c.Svc.Detail(c.Ref)
	if err != nil {
		return nil, false
	}
	// Relation titles need a second snapshot and the stub shows no
	// relations, so they are not resolved here.
	v := TaskColumnView{Detail: newDetailView(detail, c.Cfg, nil, c.Now)}
	// newDetailView links everything into the chain rooted at the task
	// itself, which is right for the side panel - the depth-0 workspace.
	// A column sits further along, so its links append to where it is
	// rather than re-rooting the chain on it.
	here := c.here()
	for i, f := range v.Detail.Files {
		open := here.Append(KindFile, f.Name)
		v.Detail.Files[i].Href = open.Path()
		v.Detail.Files[i].HXGet = open.Fragment()
	}
	for i, sub := range v.Detail.Card.Subtasks {
		open := here.Append(KindTask, sub.ID)
		v.Detail.Card.Subtasks[i].Href = open.Path()
		v.Detail.Card.Subtasks[i].HXGet = open.Fragment()
	}
	return v, true
}

// resolveFileColumn reads one attached file of the column's task. The name was
// already shape-checked by ParseChain; ReadTaskFile reports a missing task or
// a missing file the same way, and both mean the URL names nothing.
func resolveFileColumn(c colCtx) (any, bool) {
	if err := task.ValidateAttachedName(c.Ref); err != nil {
		return nil, false
	}
	content, err := c.Svc.ReadTaskFile(c.TaskID, c.Ref)
	if err != nil {
		return nil, false
	}
	return FileColumnView{
		Name:    c.Ref,
		RawHref: taskFileHref(c.TaskID, c.Ref),
		Content: content,
	}, true
}
