package web

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/gpayer/mcp-task-manager/internal/project"
)

type handler struct {
	Deps
}

// welcome is the page at /: it lists what this server may open and what it
// already has open. It replaces "the board at /", because with many
// workspaces there is no single board to put there.
func (h *handler) welcome(w http.ResponseWriter, r *http.Request) {
	h.renderWelcome(w, http.StatusOK, "")
}

// createSession is the only POST in this server. It turns a chosen workspace
// into a session and sends the browser into it.
//
// It touches no task data: Sessions.Pick opens the backlog through
// project.BuildReadOnly, which never migrates the layout and never
// auto-archives. 303 rather than 302, so the back button does not repost.
func (h *handler) createSession(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderWelcome(w, http.StatusBadRequest, "could not read the form")
		return
	}
	sess, err := h.Sessions.Pick(r.PostForm.Get("workspace"))
	if err != nil {
		h.Logger.Printf("pick workspace: %v", err)
		h.renderWelcome(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, sess.Base()+"/", http.StatusSeeOther)
}

func (h *handler) board(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, rootTpl.gone, "layout.html")
		return
	}
	h.render(w, http.StatusOK, sess.tpl.board, "layout.html", h.boardView(sess))
}

// boardFragment is the htmx poll target: the board replaces itself, so a
// session that ended has to be able to swap in through the same route.
//
// It answers 200 even for an unknown token. htmx does not swap a 404 by
// default, so a 404 here would freeze an open board with no explanation;
// instead the gone fragment swaps in, says what happened, and carries no
// hx-trigger, so the dead board stops polling.
func (h *handler) boardFragment(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusOK, rootTpl.fragments, "_gone.html")
		return
	}
	h.render(w, http.StatusOK, sess.tpl.fragments, "_board.html", h.boardView(sess))
}

func (h *handler) detail(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, rootTpl.gone, "layout.html")
		return
	}
	view, ok := h.detailView(sess, r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, sess.tpl.detail, "layout.html", view)
}

// detailPanel answers the card click. A task that vanished between the board
// render and the click is a normal race, not a server error: the panel says so
// and htmx swaps it in like any other fragment.
func (h *handler) detailPanel(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusOK, rootTpl.fragments, "_gone.html")
		return
	}
	view, ok := h.detailView(sess, r.PathValue("id"))
	if !ok {
		view = DetailView{Missing: true}
	}
	h.render(w, http.StatusOK, sess.tpl.fragments, "_detail.html", view)
}

// taskFile serves one attached file as plain text, so the detail view can
// link it into a new tab. Reading is the only thing it can do: the service
// method is a read, and the filename is one path segment that the file
// storage validates.
//
// Unlike the page routes this answers a bare 404 for an unknown token: it
// serves text/plain into a new tab, so an HTML explanation would be the
// wrong kind of answer.
func (h *handler) taskFile(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	resolved := sess.Project()
	if resolved == nil {
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

// session resolves the token in the URL. It is a map read: a handler never
// resolves, opens or initializes anything.
func (h *handler) session(r *http.Request) (*Session, bool) {
	return h.Sessions.Lookup(r.PathValue("token"))
}

func (h *handler) renderWelcome(w http.ResponseWriter, status int, errMsg string) {
	view := newWelcomeView(h.Sessions.Workspaces(), h.Sessions.Live(),
		h.Sessions.ConfigPath(), h.Sessions.Problems(), errMsg)
	h.render(w, status, rootTpl.welcome, "layout.html", view)
}

func (h *handler) renderGone(w http.ResponseWriter, r *http.Request, status int, tpl *template.Template, name string) {
	h.render(w, status, tpl, name, GoneView{Token: r.PathValue("token")})
}

func (h *handler) boardView(sess *Session) BoardView {
	resolved := sess.Project()
	snap, err := resolved.Service.BoardSnapshot()
	if err != nil {
		h.Logger.Printf("board snapshot for %s: %v", sess.TasksDir, err)
		return BoardView{Project: newProjectView(resolved.Config, 0), PollSeconds: h.PollSeconds}
	}
	return newBoardView(snap, resolved.Config, h.Now(), h.PollSeconds)
}

func (h *handler) detailView(sess *Session, id string) (DetailView, bool) {
	resolved := sess.Project()
	detail, err := resolved.Service.Detail(id)
	if err != nil {
		return DetailView{}, false
	}

	// Relation edges carry ids only; the titles come from the same snapshot
	// the board reads, and only when there is an edge to label.
	var titles map[string]string
	if len(detail.Relations) > 0 {
		titles = h.titles(resolved)
	}
	return newDetailView(detail, resolved.Config, titles, h.Now()), true
}

func (h *handler) titles(resolved *project.Resolved) map[string]string {
	snap, err := resolved.Service.BoardSnapshot()
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
