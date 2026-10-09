package web

import (
	"bytes"
	"html/template"
	"net/http"
	"strconv"
	"strings"

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

// board serves /<token>/: that session's workspace with nothing open, which
// is the board alone. Every page this package serves a session is the same
// template fed a WorkspaceView, so a URL never switches surface.
func (h *handler) board(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, rootTpl.gone, "layout.html")
		return
	}
	h.render(w, http.StatusOK, sess.tpl.board, "layout.html", h.bareWorkspaceView(sess))
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
	board := h.boardView(sess)
	board.Fragment = true
	h.render(w, http.StatusOK, sess.tpl.fragments, "_board.html", board)
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
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, rootTpl.gone, "layout.html")
		return
	}
	panel, ok := h.detailView(sess, r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	view := h.bareWorkspaceView(sess)
	view.Board.Panel = &panel
	view.Title = "#" + panel.Card.ID + " " + panel.Card.Title
	h.render(w, http.StatusOK, sess.tpl.board, "layout.html", view)
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

// descriptionFragment answers the Source / Rendered toggle of a description
// block. It answers 200 even when there is nothing to show - htmx does not
// swap a 404 - but never with _gone.html: that carries a #board and an
// out-of-band header copy, which swapped into a .desc would duplicate the
// board's id inside the panel. The next strip or board poll delivers the real
// gone fragment through its own path.
func (h *handler) descriptionFragment(w http.ResponseWriter, r *http.Request) {
	view := r.PathValue("view")
	if view != "raw" && view != "rendered" {
		http.NotFound(w, r)
		return
	}
	sess, ok := h.session(r)
	if !ok {
		h.render(w, http.StatusOK, rootTpl.fragments, "_desc.html",
			descNotice("This workspace session is not open any more. The board will say so at its next refresh."))
		return
	}
	detail, err := sess.Project().Service.Detail(r.PathValue("id"))
	if err != nil {
		h.render(w, http.StatusOK, sess.tpl.fragments, "_desc.html", descNotice("This task is gone."))
		return
	}
	t := detail.Task
	dv := newDescView(t.ID, t.Description)
	if view == "raw" {
		dv = newRawDescView(t.ID, t.Description)
	}
	h.render(w, http.StatusOK, sess.tpl.fragments, "_desc.html", dv)
}

// workspace serves a chain state as a whole page: the deep link, the reload,
// and the response htmx fetches when its history cache misses.
func (h *handler) workspace(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, rootTpl.gone, "layout.html")
		return
	}
	view, ok := h.workspaceView(sess, sessionPath(sess, r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, sess.tpl.board, "layout.html", view)
}

// graph serves /<token>/graph: the backlog graph with nothing highlighted.
// It is the one workspace state outside the chain model, because a chain
// always has a root task and the board has none to offer - see KindGraph.
func (h *handler) graph(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, rootTpl.gone, "layout.html")
		return
	}
	view, ok := h.graphWorkspaceView(sess)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, sess.tpl.board, "layout.html", view)
}

// workspaceFragment is the htmx swap target, paired with workspace the way
// /board is paired with /{$}. Its path is the page path behind the strip
// prefix, so one parser serves both.
func (h *handler) workspaceFragment(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusOK, rootTpl.fragments, "_gone.html")
		return
	}
	page := strings.TrimPrefix(sessionPath(sess, r), stripPrefix)
	if page == "" {
		page = "/"
	}
	// The bare board is a chain too: /<token>/strip/ swaps the workspace
	// back to the board with no panel.
	if page == "/" {
		view := h.bareWorkspaceView(sess)
		view.Fragment = true
		h.render(w, http.StatusOK, sess.tpl.fragments, "_workspace.html", view)
		return
	}
	// And the rootless graph is the second state that is not a chain.
	if page == graphPath {
		view, ok := h.graphWorkspaceView(sess)
		if !ok {
			http.NotFound(w, r)
			return
		}
		view.Fragment = true
		h.render(w, http.StatusOK, sess.tpl.fragments, "_workspace.html", view)
		return
	}
	view, ok := h.workspaceView(sess, page)
	if !ok {
		http.NotFound(w, r)
		return
	}
	view.Fragment = true
	h.render(w, http.StatusOK, sess.tpl.fragments, "_workspace.html", view)
}

// sessionPath is the request path with the session prefix taken off, which is
// the root-relative path every chain in this package is written in. Chain
// paths are produced and parsed without a token - the token is the session's
// business, and templates put it back through nav.
func sessionPath(sess *Session, r *http.Request) string {
	return strings.TrimPrefix(r.URL.EscapedPath(), sess.Base())
}

// workspaceView builds a whole workspace from a page path. Every failure -
// a malformed chain, an unknown kind, a task or a file that is not there -
// is one "not found": the chain lives in the URL, so a chain that names
// nothing is a URL that names nothing. A session always has a project, so
// that is no longer among the failures.
func (h *handler) workspaceView(sess *Session, escapedPath string) (WorkspaceView, bool) {
	chain, err := ParseChain(escapedPath)
	if err != nil {
		return WorkspaceView{}, false
	}
	resolved := sess.Project()

	// One memo for the whole render: the panel and every task column read
	// relation titles through it, so the second BoardSnapshot is taken at
	// most once however deep the chain is, and not at all when nothing in
	// it has a relation.
	titles := relationTitles(h, resolved)

	panel, ok := h.detailViewWith(resolved, chain.Root, titles)
	if !ok {
		return WorkspaceView{}, false
	}
	board := h.boardView(sess)
	board.Panel = &panel

	// The strip refreshes itself from the URL it is already at, and the
	// board inside it stops polling separately.
	board.Polled = true
	view := WorkspaceView{
		Project:     board.Project,
		Title:       "#" + panel.Card.ID + " " + panel.Card.Title,
		PollSeconds: h.PollSeconds,
		PollHref:    chain.Fragment(),
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
			Titles: titles,
			Next:   &chain.Columns[0],
		})
		if !ok {
			return WorkspaceView{}, false
		}
		view.Root = &ColumnUnitView{
			Kind:     string(KindTask),
			Class:    columnKinds[KindTask].Class,
			Label:    columnKinds[KindTask].Label,
			Ref:      chain.Root,
			Key:      "root",
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
		var next *Column
		if i+1 < len(chain.Columns) {
			next = &chain.Columns[i+1]
		}
		data, ok := kind.Resolve(colCtx{
			Svc:    resolved.Service,
			Cfg:    resolved.Config,
			Now:    h.Now(),
			TaskID: chain.TaskAt(i),
			Ref:    col.Ref,
			Chain:  chain,
			At:     i,
			Titles: titles,
			Next:   next,
		})
		if !ok {
			return WorkspaceView{}, false
		}
		view.Columns = append(view.Columns, ColumnUnitView{
			Kind:     string(col.Kind),
			Class:    kind.Class,
			Label:    kind.Label,
			Ref:      col.Ref,
			Key:      paneKey(i, col),
			Template: kind.Template,
			Working:  i == len(chain.Columns)-1,
			Href:     chain.TruncateTo(i + 1).Path(),
			Data:     data,
		})
	}
	view.Rail = newRailEntries(chain)
	return view, true
}

// graphPath is the rootless graph's page path, root-relative like every chain
// path: the token goes back on through nav, and the fragment is the strip
// prefix plus this.
const graphPath = "/graph"

// graphWorkspaceView is the strip with the backlog graph open and no task
// highlighted. It has no root column - there is no root task - so the graph is
// the only column, and the rail's one rung steps back to the board.
func (h *handler) graphWorkspaceView(sess *Session) (WorkspaceView, bool) {
	resolved := sess.Project()
	// No ref: nothing is highlighted. The rest of colCtx is what a chain
	// column needs to place its links, and a graph node's link re-roots
	// rather than appending, so there is nothing here to place it against.
	data, ok := columnKinds[KindGraph].Resolve(colCtx{
		Svc: resolved.Service,
		Cfg: resolved.Config,
		Now: h.Now(),
	})
	if !ok {
		return WorkspaceView{}, false
	}
	board := h.boardView(sess)
	board.Polled = true
	view := WorkspaceView{
		Project:     board.Project,
		Title:       "Backlog graph",
		PollSeconds: h.PollSeconds,
		PollHref:    stripPrefix + graphPath,
		Board:       board,
		Shifted:     true,
		Columns: []ColumnUnitView{{
			Kind:     string(KindGraph),
			Class:    columnKinds[KindGraph].Class,
			Label:    columnKinds[KindGraph].Label,
			Key:      "0:g:",
			Template: columnKinds[KindGraph].Template,
			Working:  true,
			Href:     graphPath,
			Data:     data,
		}},
		Rail: []RailEntryView{
			{Kind: string(KindTask), Label: "board", Href: "/", HXGet: stripPrefix + "/"},
			{Kind: string(KindGraph), Label: "graph", Ref: "backlog", Href: graphPath, HXGet: stripPrefix + graphPath, Current: true},
		},
	}
	return view, true
}

// bareWorkspaceView is the strip with nothing open: the board alone, no rail.
// It is what /<token>/strip/ answers, so a rail entry can step all the way
// out.
func (h *handler) bareWorkspaceView(sess *Session) WorkspaceView {
	board := h.boardView(sess)
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

// paneKey is a column's scroll-memory key. The position leads it because a
// chain is a history and may name the same task twice: app.js keeps one
// offset per data-pane string (internal/web/static/app.js), so two columns
// that shared a key would scroll as one.
func paneKey(i int, col Column) string {
	return strconv.Itoa(i) + ":" + string(col.Kind) + ":" + col.Ref
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

// health is the liveness route, and since the dashboard became its own
// process it is also its identity: the header names the backlog this process
// was started for, so a would-be spawner can tell a running dashboard of ours
// from an unrelated service on the same port - and a dashboard serving a
// *different* backlog from one serving this one.
func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if dir := h.PrimaryTasksDir; dir != "" {
		w.Header().Set(HealthHeader, dir)
	}
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
	view := newBoardView(snap, resolved.Config, h.Now(), h.PollSeconds)
	view.GraphHref, view.GraphHXGet = graphPath, stripPrefix+graphPath
	return view
}

func (h *handler) detailView(sess *Session, id string) (DetailView, bool) {
	resolved := sess.Project()
	return h.detailViewWith(resolved, id, relationTitles(h, resolved))
}

// detailViewWith is the panel's view with the title lookup passed in, so a
// workspace render can share one memo with every column instead of each
// surface taking its own snapshot.
func (h *handler) detailViewWith(resolved *project.Resolved, id string, titles func() map[string]string) (DetailView, bool) {
	detail, err := resolved.Service.Detail(id)
	if err != nil {
		return DetailView{}, false
	}

	// Relation edges carry ids only; the titles come from the same snapshot
	// the board reads, and only when there is an edge to label.
	var labels map[string]string
	if len(detail.Relations) > 0 && titles != nil {
		labels = titles()
	}
	return newDetailView(detail, resolved.Config, labels, h.Now()), true
}

// relationTitles returns a lookup that takes its BoardSnapshot at most once,
// on the first caller that has an edge to label. A nil map is cached too: a
// failed snapshot is logged once and every later caller renders bare ids
// rather than queueing another scan.
func relationTitles(h *handler, resolved *project.Resolved) func() map[string]string {
	var (
		titles map[string]string
		done   bool
	)
	return func() map[string]string {
		if !done {
			titles, done = h.titles(resolved), true
		}
		return titles
	}
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
