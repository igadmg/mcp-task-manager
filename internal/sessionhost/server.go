package sessionhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// Version is reported by /healthz; overridable via ldflags.
var Version = "dev"

const defaultHeartbeat = 15 * time.Second

// Server is the host's HTTP API (design §6): loopback-only callers holding
// the bearer token manage sessions.
type Server struct {
	mgr       *Manager
	token     string
	mux       *http.ServeMux
	heartbeat time.Duration
}

// NewServer builds the API handler; the manager and token are captured at
// construction.
func NewServer(mgr *Manager, token string) *Server {
	s := &Server{mgr: mgr, token: token, mux: http.NewServeMux(), heartbeat: defaultHeartbeat}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /v1/sessions", s.handleCreate)
	s.mux.HandleFunc("GET /v1/sessions", s.handleList)
	s.mux.HandleFunc("GET /v1/sessions/{id}", s.handleGet)
	s.mux.HandleFunc("GET /v1/sessions/{id}/events", s.handleEvents)
	s.mux.HandleFunc("POST /v1/sessions/{id}/messages", s.handleMessages)
	s.mux.HandleFunc("POST /v1/sessions/{id}/answers", s.handleAnswers)
	s.mux.HandleFunc("POST /v1/sessions/{id}/stop", s.handleStop)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound, Message: "no such route: " + r.Method + " " + r.URL.Path})
	})
	return s
}

// ServeHTTP gates everything except /healthz behind the bearer token.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		s.mux.ServeHTTP(w, r)
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == r.Header.Get("Authorization") || !CheckToken(got, s.token) {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeUnauthorized,
			Message: "missing or invalid bearer token"})
		return
	}
	s.mux.ServeHTTP(w, r)
}

var errorStatus = map[string]int{
	sessionapi.ErrCodeInvalidSpec:              http.StatusBadRequest,
	sessionapi.ErrCodeUnsupportedProvider:      http.StatusBadRequest,
	sessionapi.ErrCodeUnsupportedWorkspaceKind: http.StatusBadRequest,
	sessionapi.ErrCodeWorkspaceNotFound:        http.StatusBadRequest,
	sessionapi.ErrCodeUnauthorized:             http.StatusUnauthorized,
	sessionapi.ErrCodeNotFound:                 http.StatusNotFound,
	sessionapi.ErrCodeConflict:                 http.StatusConflict,
	sessionapi.ErrCodeTooManySessions:          http.StatusConflict,
}

// writeError serializes err as {"error":{"code","message"}} with the matching
// HTTP status; unknown errors become 500.
func writeError(w http.ResponseWriter, err error) {
	var se *sessionapi.Error
	status := http.StatusInternalServerError
	if errors.As(err, &se) {
		if st, ok := errorStatus[se.Code]; ok {
			status = st
		}
	} else {
		se = &sessionapi.Error{Code: "internal", Message: err.Error()}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"code":    se.Code,
		"message": se.Message,
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"version":          Version,
		"sessions_enabled": true,
	})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, sessionapi.MaxPromptBytes*2+4096))
	if err != nil {
		writeError(w, err)
		return
	}
	spec, err := sessionapi.ParseSpec(body)
	if err != nil {
		writeError(w, err)
		return
	}
	meta, err := s.mgr.Create(context.Background(), spec)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": meta.ID, "status": meta.Status})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	metas, err := s.mgr.List()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": metas})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	view, err := s.mgr.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleEvents streams the journal as SSE. The client resumes with either
// ?after=<seq> or the Last-Event-ID header; a finished session delivers its
// tail and the stream closes (design §6).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	after, err := parseAfter(r)
	if err != nil {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec, Message: err.Error()})
		return
	}
	sub, err := s.mgr.Subscribe(id, after)
	if err != nil {
		writeError(w, err)
		return
	}
	defer sub.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, errors.New("streaming unsupported"))
		return
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// The cursor starts at the resume point: a client that reconnects with
	// the last seq of a finished session is already caught up, so the
	// heartbeat check below can close the stream (review finding 5).
	lastSeq := after
	ticker := time.NewTicker(s.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			lastSeq = ev.Seq
			if !writeSSE(w, flusher, ev) {
				return
			}
		case <-ticker.C:
			writeComment(w, flusher)
			if s.terminalAndCaughtUp(id, lastSeq) {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// terminalAndCaughtUp reports whether the session ended and everything the
// journal holds has been delivered, so the SSE stream can close.
func (s *Server) terminalAndCaughtUp(id string, lastSeq int64) bool {
	view, err := s.mgr.Get(id)
	if err != nil {
		return false
	}
	return view.Meta.Status.IsTerminal() && lastSeq >= view.Meta.LastSeq
}

func parseAfter(r *http.Request) (int64, error) {
	// Last-Event-ID is the reconnect cursor and wins over the query param.
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("after")
	}
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid event cursor %q", raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("event cursor must be >= 0, got %d", n)
	}
	return n, nil
}

func writeSSE(w io.Writer, f http.Flusher, ev sessionapi.Event) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, data); err != nil {
		return false
	}
	f.Flush()
	return true
}

func writeComment(w io.Writer, f http.Flusher) {
	_, _ = io.WriteString(w, ": ping\n\n")
	f.Flush()
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var msg sessionapi.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec, Message: "invalid message body: " + err.Error()})
		return
	}
	if err := s.mgr.Send(r.PathValue("id"), msg.Text); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAnswers(w http.ResponseWriter, r *http.Request) {
	var a sessionapi.Answer
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec, Message: "invalid answer body: " + err.Error()})
		return
	}
	if a.RequestID == "" {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec, Message: "request_id is required"})
		return
	}
	if a.Behavior != sessionapi.BehaviorAllow && a.Behavior != sessionapi.BehaviorDeny {
		writeError(w, &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec, Message: "behavior must be allow or deny"})
		return
	}
	if err := s.mgr.Answer(r.PathValue("id"), a); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := s.mgr.Stop(r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// NormalizeListenAddr rejects addresses that would bind beyond loopback
// (design §6) and returns the address unchanged otherwise.
func NormalizeListenAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid listen port in %q", addr)
	}
	if host == "localhost" {
		return addr, nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("refusing to listen on non-loopback address %q", addr)
	}
	return addr, nil
}
