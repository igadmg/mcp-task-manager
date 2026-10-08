package web

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

type handler struct {
	Deps
}

// board serves "/": the workspace with nothing open, which is the board
// alone. Every page this package serves is the same template fed a
// WorkspaceView, so a URL never switches surface.
func (h *handler) board(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, boardPage, "layout.html", h.bareWorkspaceView())
}

// boardFragment is the htmx poll target: the board replaces itself, so the
// placeholder has to swap in and out through the same route.
func (h *handler) boardFragment(w http.ResponseWriter, r *http.Request) {
	view := h.boardView()
	name := "_board.html"
	if !view.Project.Resolved {
		name = "_unresolved.html"
	}
	h.render(w, http.StatusOK, fragments, name, view)
}

// detail serves /tasks/{id}: the board with that task's panel open. There is
// no standalone detail page any more - the panel is the detail surface, and
// this URL is the state a workspace chain of depth 0 names, so Back out of a
// chain lands here.
//
// An unknown id stays a 404, and so does an unresolved project: the board's
// own routes render a placeholder instead, and TestUnresolvedProjectPlaceholder
// pins that split.
func (h *handler) detail(w http.ResponseWriter, r *http.Request) {
	panel, ok := h.detailView(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	board := h.boardView()
	board.Panel = &panel
	view := WorkspaceView{
		Project:     board.Project,
		Title:       "#" + panel.Card.ID + " " + panel.Card.Title,
		PollSeconds: h.PollSeconds,
		Board:       board,
	}
	h.render(w, http.StatusOK, boardPage, "layout.html", view)
}

// detailPanel answers the card click. A task that vanished between the board
// render and the click is a normal race, not a server error: the panel says so
// and htmx swaps it in like any other fragment.
func (h *handler) detailPanel(w http.ResponseWriter, r *http.Request) {
	view, ok := h.detailView(r.PathValue("id"))
	if !ok {
		view = DetailView{Missing: true}
	}
	h.render(w, http.StatusOK, fragments, "_detail.html", view)
}

// workspace serves a chain state as a whole page: the deep link, the reload,
// and the response htmx fetches when its history cache misses.
func (h *handler) workspace(w http.ResponseWriter, r *http.Request) {
	view, ok := h.workspaceView(r.URL.EscapedPath())
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, boardPage, "layout.html", view)
}

// workspaceFragment is the htmx swap target, paired with workspace the way
// /board is paired with /{$}. Its path is the page path behind the strip
// prefix, so one parser serves both.
func (h *handler) workspaceFragment(w http.ResponseWriter, r *http.Request) {
	page := strings.TrimPrefix(r.URL.EscapedPath(), stripPrefix)
	if page == "" {
		page = "/"
	}
	// The bare board is a chain too: /strip/ swaps the workspace back to
	// the board with no panel.
	if page == "/" {
		h.render(w, http.StatusOK, fragments, "_workspace.html", h.bareWorkspaceView())
		return
	}
	view, ok := h.workspaceView(page)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, fragments, "_workspace.html", view)
}

// workspaceView builds a whole workspace from a page path. Every failure -
// a malformed chain, an unknown kind, a task or file that is not there, an
// unresolved project - is one "not found": the chain lives in the URL, so a
// chain that names nothing is a URL that names nothing.
func (h *handler) workspaceView(escapedPath string) (WorkspaceView, bool) {
	chain, err := ParseChain(escapedPath)
	if err != nil {
		return WorkspaceView{}, false
	}
	resolved, ok := h.Project()
	if !ok {
		return WorkspaceView{}, false
	}

	panel, ok := h.detailView(chain.Root)
	if !ok {
		return WorkspaceView{}, false
	}
	board := h.boardView()
	board.Panel = &panel

	view := WorkspaceView{
		Project:     board.Project,
		Title:       "#" + panel.Card.ID + " " + panel.Card.Title,
		PollSeconds: h.PollSeconds,
		Board:       board,
		Shifted:     chain.Depth() > 0,
	}

	// The root's own column leads the strip whenever anything is open: the
	// board leaves, this takes its leftmost slot, and the chain's columns
	// follow to its right. At = -1 puts its links on the depth-0 chain, so
	// opening one of its files or subtasks appends the first column.
	if chain.Depth() > 0 {
		root, ok := columnKinds[KindTask].Resolve(colCtx{
			Svc:    resolved.Service,
			Cfg:    resolved.Config,
			Now:    h.Now(),
			TaskID: chain.Root,
			Ref:    chain.Root,
			Chain:  chain,
			At:     -1,
		})
		if !ok {
			return WorkspaceView{}, false
		}
		view.Root = &ColumnUnitView{
			Kind:     string(KindTask),
			Class:    columnKinds[KindTask].Class,
			Label:    columnKinds[KindTask].Label,
			Ref:      chain.Root,
			Template: columnKinds[KindTask].Template,
			Href:     chain.TruncateTo(0).Path(),
			Data:     root,
		}
	}

	for i, col := range chain.Columns {
		kind, ok := columnKinds[col.Kind]
		if !ok {
			// ParseChain checked the registry, so this cannot happen
			// unless the registry changed mid-request. Treat it as a
			// URL that names nothing rather than a server error.
			return WorkspaceView{}, false
		}
		data, ok := kind.Resolve(colCtx{
			Svc:    resolved.Service,
			Cfg:    resolved.Config,
			Now:    h.Now(),
			TaskID: chain.TaskAt(i),
			Ref:    col.Ref,
			Chain:  chain,
			At:     i,
		})
		if !ok {
			return WorkspaceView{}, false
		}
		view.Columns = append(view.Columns, ColumnUnitView{
			Kind:     string(col.Kind),
			Class:    kind.Class,
			Label:    kind.Label,
			Ref:      col.Ref,
			Template: kind.Template,
			Working:  i == len(chain.Columns)-1,
			Href:     chain.TruncateTo(i + 1).Path(),
			Data:     data,
		})
	}
	view.Rail = newRailEntries(chain)
	return view, true
}

// bareWorkspaceView is the strip with nothing open: the board alone, no rail.
// It is what /strip/ answers, so a rail entry can step all the way out.
func (h *handler) bareWorkspaceView() WorkspaceView {
	board := h.boardView()
	return WorkspaceView{
		Project:     board.Project,
		PollSeconds: h.PollSeconds,
		Board:       board,
	}
}

// newRailEntries lists the whole chain, board first. Entry i links to the
// chain truncated to i columns, so clicking one is exactly "roll back to
// there". A depth-0 chain gets no rail: the board is already the whole strip.
func newRailEntries(chain Chain) []RailEntryView {
	if chain.Depth() == 0 {
		return nil
	}
	entries := []RailEntryView{{
		Kind:  string(KindTask),
		Label: "task",
		Ref:   chain.Root,
		Href:  chain.TruncateTo(0).Path(),
		HXGet: chain.TruncateTo(0).Fragment(),
	}}
	for i, col := range chain.Columns {
		cut := chain.TruncateTo(i + 1)
		label := string(col.Kind)
		if k, ok := columnKinds[col.Kind]; ok {
			label = k.Label
		}
		entries = append(entries, RailEntryView{
			Kind:    string(col.Kind),
			Label:   label,
			Ref:     col.Ref,
			Href:    cut.Path(),
			HXGet:   cut.Fragment(),
			Current: i == chain.Depth()-1,
		})
	}
	return entries
}

// taskFile serves one attached file as plain text, so the detail view can
// link it into a new tab. Reading is the only thing it can do: the service
// method is a read, and the filename is one path segment that the file
// storage validates.
func (h *handler) taskFile(w http.ResponseWriter, r *http.Request) {
	resolved, ok := h.Project()
	if !ok {
		http.NotFound(w, r)
		return
	}
	content, err := resolved.Service.ReadTaskFile(r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(content))
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (h *handler) boardView() BoardView {
	resolved, ok := h.Project()
	if !ok {
		return unresolvedBoardView(h.PollSeconds)
	}
	snap, err := resolved.Service.BoardSnapshot()
	if err != nil {
		h.Logger.Printf("board snapshot: %v", err)
		return unresolvedBoardView(h.PollSeconds)
	}
	return newBoardView(snap, resolved.Config, h.Now(), h.PollSeconds)
}

func (h *handler) detailView(id string) (DetailView, bool) {
	resolved, ok := h.Project()
	if !ok {
		return DetailView{}, false
	}
	detail, err := resolved.Service.Detail(id)
	if err != nil {
		return DetailView{}, false
	}

	// Relation edges carry ids only; the titles come from the same snapshot
	// the board reads, and only when there is an edge to label.
	var titles map[string]string
	if len(detail.Relations) > 0 {
		titles = h.titles(resolved.Service)
	}
	return newDetailView(detail, resolved.Config, titles, h.Now()), true
}

func (h *handler) titles(svc *task.Service) map[string]string {
	snap, err := svc.BoardSnapshot()
	if err != nil {
		h.Logger.Printf("relation titles: %v", err)
		return nil
	}
	titles := make(map[string]string, len(snap.Tasks))
	for _, t := range snap.Tasks {
		titles[t.ID] = t.Title
	}
	return titles
}

// render buffers the whole response first, so a template error mid-render
// yields a clean 500 instead of a half-written page.
func (h *handler) render(w http.ResponseWriter, status int, tpl *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		h.Logger.Printf("render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
