package sessionhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

const titleMaxRunes = 80

// Session is one session as the manager sees it: meta, journal, the
// provider process (nil for sessions restored from disk), and the pending
// question/permission requests awaiting an answer.
type Session struct {
	dir string

	mu            sync.Mutex
	meta          Meta
	journal       *Journal
	proc          Process
	pending       map[string]sessionapi.Event
	stopRequested bool
}

// SessionView is the manager's read model for one session.
type SessionView struct {
	Meta            Meta               `json:"meta"`
	PendingRequests []sessionapi.Event `json:"pending_requests"`
}

func newSession(root string, spec sessionapi.Spec, now time.Time) *Session {
	id := newSessionID()
	return &Session{
		dir: filepath.Join(root, id),
		meta: Meta{
			ID:            id,
			Title:         titleOf(spec.Prompt),
			Status:        sessionapi.StatusStarting,
			WorkspacePath: spec.Workspace.Path,
			TaskID:        spec.TaskID,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		pending: make(map[string]sessionapi.Event),
	}
}

func newSessionID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func titleOf(prompt string) string {
	runes := []rune(strings.TrimSpace(prompt))
	if len(runes) > titleMaxRunes {
		runes = runes[:titleMaxRunes]
	}
	return string(runes)
}

// restoreSession loads a finished session from disk without a process.
func restoreSession(dir string, meta Meta) *Session {
	return &Session{dir: dir, meta: meta, pending: make(map[string]sessionapi.Event)}
}

// journalOf lazily opens the journal (restored sessions only get one when
// someone subscribes or appends).
func (s *Session) journalOf() (*Journal, error) {
	if s.journal != nil {
		return s.journal, nil
	}
	j, err := OpenJournal(s.dir)
	if err != nil {
		return nil, err
	}
	s.journal = j
	return j, nil
}

// appendEvent writes the event to the journal and syncs meta's seq stamps.
// The event's own seq/ts are set by the journal.
func (s *Session) appendEvent(ev sessionapi.Event) (sessionapi.Event, error) {
	j, err := s.journalOf()
	if err != nil {
		return ev, err
	}
	ev, err = j.Append(ev)
	if err != nil {
		return ev, err
	}
	s.meta.LastSeq = ev.Seq
	s.meta.UpdatedAt = ev.Ts
	return ev, nil
}

// setStatus transitions and records a status event; meta is persisted.
func (s *Session) setStatus(status sessionapi.Status, reason string) error {
	if s.meta.Status == status {
		return nil
	}
	s.meta.Status = status
	s.meta.UpdatedAt = time.Now().UTC()
	if _, err := s.appendEvent(sessionapi.Event{
		Kind:   sessionapi.EventStatus,
		Status: status,
		Reason: reason,
	}); err != nil {
		return err
	}
	return WriteMeta(s.dir, s.meta)
}

// applyNormalizedEvent updates pending requests and the status machine for
// one backend event; the event itself is already journaled by the caller.
func (s *Session) applyNormalizedEvent(ev sessionapi.Event) {
	switch ev.Kind {
	case sessionapi.EventQuestion, sessionapi.EventPermission:
		s.pending[ev.RequestID] = ev
		_ = s.setStatus(sessionapi.StatusWaitingAnswer, "")
	case sessionapi.EventRequestResolved:
		delete(s.pending, ev.RequestID)
		if len(s.pending) == 0 && s.meta.Status == sessionapi.StatusWaitingAnswer {
			_ = s.setStatus(sessionapi.StatusRunning, "")
		}
	case sessionapi.EventTurnResult:
		if len(s.pending) == 0 && !s.meta.Status.IsTerminal() {
			_ = s.setStatus(sessionapi.StatusIdle, "")
		}
	}
}

// recoverAsFailed marks a session restored after a host restart as failed:
// the provider process is gone, and v1 does not resume sessions.
func (s *Session) recoverAsFailed() error {
	return s.setStatus(sessionapi.StatusFailed, "host_restarted")
}

// fail records an error event and moves the session to failed.
func (s *Session) fail(code, message string) {
	_, _ = s.appendEvent(sessionapi.Event{Kind: sessionapi.EventError, Code: code, Message: message})
	_ = s.setStatus(sessionapi.StatusFailed, message)
}

// pump reads backend events until the process ends, then finalizes the
// session status (running | waiting_answer | idle) -> terminal.
func (s *Session) pump() {
	for ev := range s.proc.Events() {
		s.mu.Lock()
		if _, err := s.appendEvent(ev); err == nil {
			s.applyNormalizedEvent(ev)
			WriteMeta(s.dir, s.meta)
		}
		s.mu.Unlock()
	}
	err := s.proc.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.stopRequested || s.meta.Status == sessionapi.StatusStopped:
		_ = s.setStatus(sessionapi.StatusStopped, "")
	case s.meta.Status.IsTerminal():
		// Already failed via an error event.
	case err != nil:
		s.fail("process_exited", err.Error())
	default:
		_ = s.setStatus(sessionapi.StatusFinished, "")
	}
}

func (s *Session) send(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.meta.Status {
	case sessionapi.StatusIdle, sessionapi.StatusRunning:
	default:
		return &sessionapi.Error{Code: sessionapi.ErrCodeConflict,
			Message: "session is " + string(s.meta.Status) + ", cannot send messages"}
	}
	if err := s.proc.Send(text); err != nil {
		return err
	}
	if _, err := s.appendEvent(sessionapi.Event{Kind: sessionapi.EventUserMessage, Text: text}); err != nil {
		return err
	}
	return s.setStatus(sessionapi.StatusRunning, "")
}

func (s *Session) answer(a sessionapi.Answer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pending[a.RequestID]; !ok {
		return &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no pending request " + a.RequestID}
	}
	if err := s.proc.Answer(a); err != nil {
		return &sessionapi.Error{Code: sessionapi.ErrCodeConflict, Message: err.Error()}
	}
	// The backend emits request_resolved once the answer is processed;
	// pending state is cleared there.
	return nil
}

func (s *Session) stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meta.Status.IsTerminal() {
		return &sessionapi.Error{Code: sessionapi.ErrCodeConflict,
			Message: "session is already " + string(s.meta.Status)}
	}
	s.stopRequested = true
	proc := s.proc
	// pump needs s.mu to journal the process's last events, so a Stop
	// that waits for the event channel to drain would deadlock under it.
	s.mu.Unlock()
	err := proc.Stop(ctx)
	s.mu.Lock()
	// Cancel every unanswered request so the UI can stop waiting.
	for reqID := range s.pending {
		outcome := sessionapi.Event{
			Kind:      sessionapi.EventRequestResolved,
			RequestID: reqID,
			Outcome:   "cancelled",
		}
		if _, aerr := s.appendEvent(outcome); aerr == nil {
			delete(s.pending, reqID)
		}
	}
	if serr := s.setStatus(sessionapi.StatusStopped, ""); serr != nil && err == nil {
		err = serr
	}
	return err
}

// view snapshots the session for Get/List.
func (s *Session) view() SessionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := make([]sessionapi.Event, 0, len(s.pending))
	for _, ev := range s.pending {
		pending = append(pending, ev)
	}
	return SessionView{Meta: s.meta, PendingRequests: pending}
}
