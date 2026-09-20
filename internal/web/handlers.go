package web

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

type handler struct {
	Deps
}

func (h *handler) board(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, boardPage, "layout.html", h.boardView())
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

func (h *handler) detail(w http.ResponseWriter, r *http.Request) {
	view, ok := h.detailView(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, detailPage, "layout.html", view)
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
