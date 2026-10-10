package sessionhost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

const defaultMaxSessions = 4

// Manager owns the session state directory and the live sessions.
type Manager struct {
	stateDir    string
	backend     Backend
	maxSessions int

	// baseCtx bounds the provider processes: they belong to the host's
	// lifetime, never to an individual API request (review finding 1).
	baseCtx context.Context
	cancel  context.CancelFunc

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager opens stateDir, recovers sessions from meta.json, and marks
// every unfinished session as failed(host_restarted): provider processes
// die with the host, and v1 does not resume them (design §5).
func NewManager(stateDir string, backend Backend, maxSessions int) (*Manager, error) {
	if maxSessions <= 0 {
		maxSessions = defaultMaxSessions
	}
	m := &Manager{
		stateDir:    stateDir,
		backend:     backend,
		maxSessions: maxSessions,
		sessions:    make(map[string]*Session),
	}
	m.baseCtx, m.cancel = context.WithCancel(context.Background())
	if err := m.recover(); err != nil {
		m.cancel()
		return nil, err
	}
	return m, nil
}

func (m *Manager) sessionsRoot() string {
	return filepath.Join(m.stateDir, "sessions")
}

func (m *Manager) recover() error {
	entries, err := os.ReadDir(m.sessionsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(m.sessionsRoot(), e.Name())
		meta, err := ReadMeta(dir)
		if err != nil {
			continue // corrupt or partial session directory; skip
		}
		s := restoreSession(dir, meta)
		if !meta.Status.IsTerminal() {
			if jerr := s.recoverAsFailed(); jerr != nil {
				continue
			}
		}
		m.sessions[meta.ID] = s
	}
	return nil
}

// Create validates the spec, enforces the live-session limit, opens the
// session directory, and starts the backend. A backend start failure does
// not fail the call: the session is created in failed state so the user
// sees the error (design §4). The ctx argument does not bound the provider
// process: process lifetime belongs to the manager (review finding 1), so a
// cancelled request context cannot kill a freshly started session.
func (m *Manager) Create(ctx context.Context, spec sessionapi.Spec) (Meta, error) {
	if err := spec.Validate(); err != nil {
		return Meta{}, err
	}
	info, err := os.Stat(spec.Workspace.Path)
	if err != nil || !info.IsDir() {
		return Meta{}, &sessionapi.Error{Code: sessionapi.ErrCodeWorkspaceNotFound,
			Message: "workspace directory not found: " + spec.Workspace.Path}
	}

	m.mu.Lock()
	live := 0
	for _, s := range m.sessions {
		if !s.view().Meta.Status.IsTerminal() {
			live++
		}
	}
	if live >= m.maxSessions {
		m.mu.Unlock()
		return Meta{}, &sessionapi.Error{Code: sessionapi.ErrCodeTooManySessions,
			Message: "maximum number of live sessions reached"}
	}

	sess := newSession(m.sessionsRoot(), spec, time.Now().UTC())
	if err := os.MkdirAll(sess.dir, 0o755); err != nil {
		m.mu.Unlock()
		return Meta{}, err
	}
	m.sessions[sess.meta.ID] = sess
	m.mu.Unlock()

	if err := WriteMeta(sess.dir, sess.meta); err != nil {
		return Meta{}, err
	}
	if err := writeSpec(sess.dir, spec); err != nil {
		return Meta{}, err
	}
	if _, err := sess.appendEvent(sessionapi.Event{Kind: sessionapi.EventStatus, Status: sessionapi.StatusStarting}); err != nil {
		return Meta{}, err
	}

	raw, err := os.OpenFile(filepath.Join(sess.dir, "raw.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Meta{}, err
	}
	proc, err := startProcess(m.baseCtx, m.backend, spec, raw)
	if err != nil {
		sess.mu.Lock()
		sess.fail("start_failed", err.Error())
		sess.mu.Unlock()
		return sess.view().Meta, nil
	}

	sess.mu.Lock()
	sess.proc = proc
	stopped := sess.stopRequested
	if !stopped {
		_ = sess.setStatus(sessionapi.StatusRunning, "")
	}
	sess.mu.Unlock()

	go sess.pump()

	if stopped {
		// Stop arrived while the backend was starting; finish the
		// termination the early stop could not perform without a process
		// handle (review finding 6).
		_ = sess.stop(context.Background())
	}
	return sess.view().Meta, nil
}

func writeSpec(dir string, spec sessionapi.Spec) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "spec.json"), data, 0o644)
}

// List returns all sessions, most recently updated first.
func (m *Manager) List() ([]Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	metas := make([]Meta, 0, len(m.sessions))
	for _, s := range m.sessions {
		metas = append(metas, s.view().Meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].UpdatedAt.After(metas[j].UpdatedAt) })
	return metas, nil
}

// Get returns the session view (meta + unanswered requests).
func (m *Manager) Get(id string) (SessionView, error) {
	s, err := m.find(id)
	if err != nil {
		return SessionView{}, err
	}
	return s.view(), nil
}

// Subscribe streams events after the given seq; replay comes from the
// journal, so late subscribers lose nothing.
func (m *Manager) Subscribe(id string, after int64) (*Subscription, error) {
	s, err := m.find(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.journalOf()
	if err != nil {
		return nil, err
	}
	return j.Subscribe(after)
}

// Send delivers a user message into a live session.
func (m *Manager) Send(id, text string) error {
	s, err := m.find(id)
	if err != nil {
		return err
	}
	if text == "" {
		return &sessionapi.Error{Code: sessionapi.ErrCodeInvalidSpec, Message: "message text must not be empty"}
	}
	return s.send(text)
}

// Answer responds to a pending question/permission request.
func (m *Manager) Answer(id string, a sessionapi.Answer) error {
	s, err := m.find(id)
	if err != nil {
		return err
	}
	return s.answer(a)
}

// Stop terminates a session and cancels its unanswered requests.
func (m *Manager) Stop(id string) error {
	s, err := m.find(id)
	if err != nil {
		return err
	}
	return s.stop(context.Background())
}

func (m *Manager) find(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, &sessionapi.Error{Code: sessionapi.ErrCodeNotFound, Message: "no such session: " + id}
	}
	return s, nil
}

// Close releases all open journals. It does not change session statuses;
// recovery after a real host restart happens in NewManager.
func (m *Manager) Close() error {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for _, s := range m.sessions {
		s.mu.Lock()
		s.closed = true
		if s.journal != nil {
			if err := s.journal.Close(); err != nil && first == nil {
				first = err
			}
			s.journal = nil
		}
		s.mu.Unlock()
	}
	return first
}
