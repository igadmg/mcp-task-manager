package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"

	"github.com/gpayer/mcp-task-manager/internal/hostclient"
	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// SessionClient is the host-facing surface the dashboard tunnels through
// (design §7): start, list, status, the SSE event stream and the mutations.
// It knows nothing about Claude. The production implementation is
// *hostclient.Client; the interface exists so either side can be faked
// without crossing a process boundary.
type SessionClient interface {
	Health(ctx context.Context) (hostclient.Health, error)
	Create(ctx context.Context, spec sessionapi.Spec) (hostclient.Created, error)
	List(ctx context.Context) ([]hostclient.Meta, error)
	Get(ctx context.Context, id string) (hostclient.View, error)
	Events(ctx context.Context, id, lastID string) (io.ReadCloser, error)
	Send(ctx context.Context, id, text string) error
	Answer(ctx context.Context, id string, a sessionapi.Answer) error
	Stop(ctx context.Context, id string) error
}

// Web-local mutation-guard codes (see csrf.go); every other code is one of
// the sessionapi constants, forwarded from the host as-is.
const (
	errCodeBadCSRF              = "bad_csrf"
	errCodeUnsupportedMediaType = "unsupported_media_type"
)

// hostUnavailableMessage is what a user sees when the session host cannot be
// reached or its token file cannot be read - an explanation, not an error
// string (design §7).
const hostUnavailableMessage = "The session host is unavailable. Check that mcp-session-host is running, then reload this page."

// sessionErrorStatus maps error codes to HTTP statuses for the
// browser-facing session API. It deliberately mirrors the host's own table
// (internal/sessionhost/server.go). The host's unauthorized is a 502 here:
// the web and the host disagreeing about the token is this deployment's
// configuration problem, not the browser's.
var sessionErrorStatus = map[string]int{
	sessionapi.ErrCodeInvalidSpec:              http.StatusBadRequest,
	sessionapi.ErrCodeUnsupportedProvider:      http.StatusBadRequest,
	sessionapi.ErrCodeUnsupportedWorkspaceKind: http.StatusBadRequest,
	sessionapi.ErrCodeWorkspaceNotFound:        http.StatusBadRequest,
	errCodeUnsupportedMediaType:                http.StatusUnsupportedMediaType,
	errCodeBadCSRF:                             http.StatusForbidden,
	sessionapi.ErrCodeUnauthorized:             http.StatusBadGateway,
	sessionapi.ErrCodeNotFound:                 http.StatusNotFound,
	sessionapi.ErrCodeConflict:                 http.StatusConflict,
	sessionapi.ErrCodeTooManySessions:          http.StatusConflict,
	sessionapi.ErrCodeHostUnavailable:          http.StatusServiceUnavailable,
}

// writeSessionError serializes err as {"error":{"code","message"}} with the
// matching HTTP status; unknown errors become a plain 500 so no internal
// detail leaks into the browser.
func writeSessionError(w http.ResponseWriter, err error) {
	var se *sessionapi.Error
	status := http.StatusInternalServerError
	if errors.As(err, &se) {
		if st, ok := sessionErrorStatus[se.Code]; ok {
			status = st
		}
		if se.Code == sessionapi.ErrCodeHostUnavailable {
			se = &sessionapi.Error{Code: se.Code, Message: hostUnavailableMessage}
		}
	} else {
		se = &sessionapi.Error{Code: "internal", Message: "internal error"}
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code":    se.Code,
		"message": se.Message,
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// registerSessionRoutes adds the opt-in session API under the workspace
// token (design §7). Called only when Deps.SessionAPI is set: disabled means
// no routes, not 403s.
func registerSessionRoutes(mux *http.ServeMux, h *handler) {
	mux.HandleFunc("GET /{token}/sessions", h.sessionsPage)
	mux.HandleFunc("POST /{token}/sessions", h.csrf(h.startSession))
	mux.HandleFunc("GET /{token}/sessions/{id}", h.sessionPage)
	mux.HandleFunc("GET /{token}/sessions/{id}/events", h.sessionEvents)
	mux.HandleFunc("POST /{token}/sessions/{id}/messages", h.csrf(h.sessionMessage))
	mux.HandleFunc("POST /{token}/sessions/{id}/answers", h.csrf(h.sessionAnswer))
	mux.HandleFunc("POST /{token}/sessions/{id}/stop", h.csrf(h.sessionStop))
}

// SessionsPageView is one workspace's session list plus the start form.
type SessionsPageView struct {
	Project ProjectView
	Title   string
	CSRF    string
	// StartHref is root-relative; the template prefixes it with nav.
	StartHref string
	// Error is a host-unavailability explanation, empty when the list below
	// is real (design §7: an error message, not a blank screen).
	Error    string
	Sessions []hostclient.Meta
}

// SessionPageView is one session: the status header, the event feed mount,
// the unanswered requests (restored server-side after a reload) and the
// mutation URLs the page script posts to.
type SessionPageView struct {
	Project      ProjectView
	Title        string
	CSRF         string
	ID           string
	Status       sessionapi.Status
	TaskID       string
	CreatedAt    string
	EventsHref   string
	MessagesHref string
	AnswersHref  string
	StopHref     string
	ListHref     string
	Pending      []sessionapi.Event
}

// sessionsPage lists this workspace's sessions and hosts the start form.
func (h *handler) sessionsPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, h.rootTpl.gone, "layout.html")
		return
	}
	h.render(w, http.StatusOK, h.sessionTemplates(sess).sessions, "layout.html",
		h.sessionsPageView(r, sess))
}

func (h *handler) sessionsPageView(r *http.Request, sess *Session) SessionsPageView {
	view := SessionsPageView{
		Project:   h.shellProjectView(sess),
		Title:     "Sessions",
		CSRF:      h.csrfToken,
		StartHref: "/sessions",
	}
	if _, err := h.SessionAPI.Health(r.Context()); err != nil {
		view.Error = hostUnavailableMessage
		return view
	}
	metas, err := h.SessionAPI.List(r.Context())
	if err != nil {
		view.Error = friendlySessionError(err)
		return view
	}
	root := workspaceRoot(sess)
	for _, m := range metas {
		// The workspace token is a namespace, not a boundary: the filter is
		// the session's own project root from the registry, never a path
		// the browser sent (design §7).
		if filepath.Clean(m.WorkspacePath) == filepath.Clean(root) {
			view.Sessions = append(view.Sessions, m)
		}
	}
	return view
}

// startSession begins one AI session in this workspace's project root. The
// spec's workspace.path comes from the server-side workspace registry only -
// the browser body names a prompt and an optional task id, never a path -
// and unknown fields are rejected outright (design §7).
func (h *handler) startSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no such workspace"})
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
		TaskID string `json:"task_id"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec,
			Message: "the start request must be JSON with a prompt and an optional task_id: " + err.Error()})
		return
	}
	spec := sessionapi.Spec{
		Version:   sessionapi.SpecVersion,
		Provider:  sessionapi.ProviderClaude,
		Workspace: sessionapi.Workspace{Kind: sessionapi.WorkspaceKindWorkingDir, Path: workspaceRoot(sess)},
		Prompt:    req.Prompt,
		TaskID:    req.TaskID,
	}
	if err := spec.Validate(); err != nil {
		writeSessionError(w, err)
		return
	}
	created, err := h.SessionAPI.Create(r.Context(), spec)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// sessionPage renders one session of this workspace. Pending requests are
// rendered server-side, so a reload shows what the session is waiting for
// before the event stream catches up.
func (h *handler) sessionPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		h.renderGone(w, r, http.StatusNotFound, h.rootTpl.gone, "layout.html")
		return
	}
	view, ok := h.ownSession(w, r, sess)
	if !ok {
		return
	}
	h.render(w, http.StatusOK, h.sessionTemplates(sess).session, "layout.html",
		h.sessionPageView(sess, view))
}

func (h *handler) sessionPageView(sess *Session, v hostclient.View) SessionPageView {
	id := v.Meta.ID
	return SessionPageView{
		Project:      h.shellProjectView(sess),
		Title:        "Session " + shortID(id),
		CSRF:         h.csrfToken,
		ID:           id,
		Status:       v.Meta.Status,
		TaskID:       v.Meta.TaskID,
		CreatedAt:    v.Meta.CreatedAt.Format(timeFormat),
		EventsHref:   "/sessions/" + id + "/events",
		MessagesHref: "/sessions/" + id + "/messages",
		AnswersHref:  "/sessions/" + id + "/answers",
		StopHref:     "/sessions/" + id + "/stop",
		ListHref:     "/sessions",
		Pending:      v.PendingRequests,
	}
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// sessionEvents tunnels the host's SSE stream to the browser. Last-Event-ID
// is the browser's resume cursor and is forwarded verbatim; the host replays
// its journal from it, so a reconnect loses no events.
func (h *handler) sessionEvents(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, ok := h.ownSession(w, r, sess); !ok {
		return
	}
	body, err := h.SessionAPI.Events(r.Context(), r.PathValue("id"), r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeSessionError(w, err)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// Behind a buffering proxy the feed would arrive in one chunk at the
	// end; this header asks proxies to stream it.
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeSessionError(w, errors.New("streaming unsupported"))
		return
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// The client built the upstream request from this request's context, so
	// a browser that goes away cancels the host stream along with the copy
	// loop.
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

func (h *handler) sessionMessage(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no such workspace"})
		return
	}
	if _, ok := h.ownSession(w, r, sess); !ok {
		return
	}
	var msg sessionapi.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec,
			Message: "the message must be JSON with a text field"})
		return
	}
	if err := h.SessionAPI.Send(r.Context(), r.PathValue("id"), msg.Text); err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) sessionAnswer(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no such workspace"})
		return
	}
	if _, ok := h.ownSession(w, r, sess); !ok {
		return
	}
	var a sessionapi.Answer
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec,
			Message: "the answer must be a JSON question/permission reply"})
		return
	}
	if err := h.SessionAPI.Answer(r.Context(), r.PathValue("id"), a); err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) sessionStop(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(r)
	if !ok {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no such workspace"})
		return
	}
	if _, ok := h.ownSession(w, r, sess); !ok {
		return
	}
	if err := h.SessionAPI.Stop(r.Context(), r.PathValue("id")); err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ownSession loads the host session and verifies it belongs to this
// workspace; ownership is checked per request, not by list filtering, so
// another workspace's session id is a 404 on every endpoint - as is an id
// the host does not know at all.
func (h *handler) ownSession(w http.ResponseWriter, r *http.Request, sess *Session) (hostclient.View, bool) {
	view, err := h.SessionAPI.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeSessionError(w, err)
		return hostclient.View{}, false
	}
	if !sameWorkspace(view.Meta.WorkspacePath, sess) {
		writeSessionError(w, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no such session in this workspace"})
		return hostclient.View{}, false
	}
	return view, true
}

// sameWorkspace compares the host record's workspace path with the session's
// project root. The path travels spec -> meta verbatim (the web fills it
// from the registry), so a cleaned string comparison is exact.
func sameWorkspace(hostPath string, sess *Session) bool {
	root := workspaceRoot(sess)
	if root == "" {
		return false
	}
	return filepath.Clean(hostPath) == filepath.Clean(root)
}

// workspaceRoot is the only directory a session of this workspace may run
// in: the resolved project root from the server-side registry (design §7).
func workspaceRoot(sess *Session) string {
	p := sess.Project()
	if p == nil || p.Resolution() == nil {
		return ""
	}
	return p.Resolution().Root
}

// friendlySessionError renders an error for a human: the host's own message
// when there is one, a generic line when there is not.
func friendlySessionError(err error) string {
	var se *sessionapi.Error
	if errors.As(err, &se) && se.Message != "" {
		return se.Message
	}
	return "internal error"
}
