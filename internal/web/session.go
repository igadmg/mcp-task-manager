package web

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
)

// Source records who opened a workspace. It is provenance, not a permission:
// nothing in the route table or the handlers branches on it.
type Source string

const (
	// SourceMCP is the MCP server's own live project. Its *task.Service is
	// the one MCP writes through, so this board sees a write the moment the
	// tool call that made it releases the service lock.
	SourceMCP Source = "mcp"
	// SourceWeb is a workspace opened read-only from the configured list.
	// It sees another process's writes when the index notices that the task
	// count or an mtime diverged.
	SourceWeb Source = "web"
)

// reservedSegments are the first path segments the token generator can never
// emit, because the route table already means something by them. Shaped like
// storage.reservedTaskIDs - name to reason - so a diagnostic can quote the
// reason.
//
// The asymmetry with task ids is deliberate: an id is caller-supplied, so a
// reserved id is a validation error. A token is server-generated, so a
// reserved token is a generator postcondition - see newToken.
var reservedSegments = map[string]string{
	"static":   "reserved for the global asset routes",
	"healthz":  "reserved for the health check",
	"sessions": "reserved for the session registry",
}

// Session is one workspace the server is serving, under one token.
//
// Everything on it but the resolved project is immutable once Sessions hands
// it out; the project is swapped when MCP resolves (or re-resolves) the same
// backlog, under the registry's write lock.
type Session struct {
	// Token is the first path segment of every URL of this session.
	Token string
	// Name is how the session is labelled on the welcome page.
	Name string
	// TasksDir is the cleaned absolute tasks directory. It is the
	// registry's key.
	TasksDir string

	tpl sessionTemplates

	mu       sync.RWMutex
	source   Source
	resolved *project.Resolved
}

// Base is the URL prefix every route of this session hangs off, without a
// trailing slash: nav("/") yields "/<token>/".
func (s *Session) Base() string { return "/" + s.Token }

// Source reports who opened this session's project.
func (s *Session) Source() Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.source
}

// Project is the backlog this session serves. A handler reads it once, at
// the top of the request, and uses that value for the whole render: an
// Adopt concurrent with a request must not make one page read two projects.
func (s *Session) Project() *project.Resolved {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolved
}

func (s *Session) adopt(resolved *project.Resolved, source Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved, s.source = resolved, source
}

// OpenFunc builds a read-only project for a configured workspace. It is
// injected rather than called directly so internal/web never constructs a
// project itself - internal/app owns that, as it owns every other wiring
// decision between project and web.
type OpenFunc func(config.Workspace) (*project.Resolved, error)

// SessionsConfig is what a registry needs to exist.
type SessionsConfig struct {
	// Workspaces is the selectable list, as written in web.yaml - invalid
	// entries included, so the welcome page can list them as unavailable.
	Workspaces []config.Workspace
	// ConfigPath is the file Workspaces came from, shown on the welcome
	// page so an operator knows which file to edit.
	ConfigPath string
	// Problems are that file's findings. The welcome page shows them, so
	// the page explains itself and not only the server log.
	Problems []string
	// Open builds a workspace's project. A nil Open means no workspace can
	// be picked; an MCP-registered session still works.
	Open OpenFunc
	// NewToken is injectable so a test can drive the reserved-segment and
	// collision paths of the generator.
	NewToken func() (string, error)
	Logger   *log.Logger
}

// Sessions is the in-memory, process-lifetime session registry.
//
// # Tokens are not authentication
//
// A token is a namespace in the URL that picks which backlog is shown.
// Tokens are random, so a URL is not guessable from a path, but this is not
// an access-control boundary and nothing should ever be built as if it were:
// a token appears in the server's log, in the browser's history, in the
// Referer header of any outbound link on the page, and in anything that
// proxies it. The listener is loopback-only by default
// (config.DefaultWebAddr), and that - not the token - is the only thing here
// that resembles a boundary.
//
// # Lifetime
//
// Memory only, for the life of the process. A restart starts from scratch
// and every old link stops resolving, which is why an unknown token renders
// an explanation rather than a bare 404. There is no on-disk token state, no
// TTL and no eviction: the key is the tasks directory, so a repeated pick of
// the same workspace reuses its token and the registry cannot grow past one
// session per configured workspace, plus whatever MCP resolved.
type Sessions struct {
	workspaces []config.Workspace
	configPath string
	problems   []string
	open       OpenFunc
	newToken   func() (string, error)
	logger     *log.Logger

	mu      sync.RWMutex
	byToken map[string]*Session
	byDir   map[string]*Session
}

// NewSessions returns an empty registry.
func NewSessions(c SessionsConfig) *Sessions {
	s := &Sessions{
		workspaces: c.Workspaces,
		configPath: c.ConfigPath,
		problems:   c.Problems,
		open:       c.Open,
		newToken:   c.NewToken,
		logger:     c.Logger,
		byToken:    make(map[string]*Session),
		byDir:      make(map[string]*Session),
	}
	if s.newToken == nil {
		s.newToken = randomToken
	}
	if s.logger == nil {
		s.logger = log.New(os.Stderr, "web: ", log.LstdFlags)
	}
	return s
}

// Lookup resolves a token. It is on every request, so it is one map read
// under a read lock and nothing else.
func (s *Sessions) Lookup(token string) (*Session, bool) {
	if token == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byToken[token]
	return sess, ok
}

// Workspaces returns the configured list in file order, each with the token
// of its live session when it has one.
func (s *Sessions) Workspaces() []WorkspaceStatus {
	out := make([]WorkspaceStatus, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		st := WorkspaceStatus{Workspace: w}
		if w.Selectable() {
			if dir, err := s.workspaceDir(w); err == nil {
				s.mu.RLock()
				if sess, ok := s.byDir[dir]; ok {
					st.Token, st.Live = sess.Token, true
				}
				s.mu.RUnlock()
			}
		}
		out = append(out, st)
	}
	return out
}

// Live returns every registered session, newest last is not promised - the
// welcome page sorts what it shows.
func (s *Sessions) Live() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.byToken))
	for _, sess := range s.byToken {
		out = append(out, sess)
	}
	return out
}

// ConfigPath is the file the workspace list was read from, empty when none
// was found.
func (s *Sessions) ConfigPath() string { return s.configPath }

// Problems are the workspace file's findings, for a consumer that shows them
// itself rather than leaving them in the log.
func (s *Sessions) Problems() []string { return s.problems }

// WorkspaceStatus is a configured workspace plus whether it is already open.
type WorkspaceStatus struct {
	config.Workspace
	Token string
	Live  bool
}

// Pick registers (or re-finds) the session for a configured workspace. It is
// what the one POST in this server does.
//
// The project is built outside the registry lock: it reads a config file and
// scans a whole backlog, and a slow scan must not block Lookup for every
// other open board. The insert then re-checks, so two concurrent picks of
// one workspace still yield one session.
func (s *Sessions) Pick(name string) (*Session, error) {
	ws, err := s.workspace(name)
	if err != nil {
		return nil, err
	}
	dir, err := s.workspaceDir(ws)
	if err != nil {
		return nil, err
	}

	if sess, ok := s.byDirLocked(dir); ok {
		return sess, nil
	}
	if s.open == nil {
		return nil, fmt.Errorf("this server cannot open workspaces")
	}

	resolved, err := s.open(ws)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", ws.Name, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.byDir[dir]; ok {
		// Another pick won the race; its session is as good as ours and
		// already published, so ours is dropped.
		return sess, nil
	}
	return s.insertLocked(dir, ws.Name, SourceWeb, resolved)
}

// Adopt registers the MCP server's own resolved project, or hands an
// existing session over to it.
//
// Called from the resolver's OnResolve, which also fires on re-resolution,
// so "already adopted" is the normal case and simply swaps the project. A
// session a human opened from the welcome page for the same backlog is
// adopted in place: same token, so an open browser tab keeps its URL and
// starts seeing MCP's writes through the shared service lock.
func (s *Sessions) Adopt(resolved *project.Resolved) (*Session, error) {
	if resolved == nil || resolved.Config == nil {
		return nil, fmt.Errorf("adopt: no project")
	}
	dir := cleanDir(resolved.Config.TasksDir())

	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.byDir[dir]; ok {
		sess.adopt(resolved, SourceMCP)
		return sess, nil
	}
	return s.insertLocked(dir, filepath.Base(filepath.Dir(dir)), SourceMCP, resolved)
}

// PathFor reports the URL path of the session serving tasksDir, if any. It
// is how start_web_ui turns a listener address into a usable link.
func (s *Sessions) PathFor(tasksDir string) (string, bool) {
	dir := cleanDir(tasksDir)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.byDir[dir]; ok {
		return sess.Base() + "/", true
	}
	return "", false
}

// insertLocked mints a token and publishes a fully built session. The caller
// holds the write lock, so nothing can observe a half-built one.
func (s *Sessions) insertLocked(dir, name string, source Source, resolved *project.Resolved) (*Session, error) {
	token, err := s.mintLocked()
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = dir
	}
	sess := &Session{
		Token:    token,
		Name:     name,
		TasksDir: dir,
		source:   source,
		resolved: resolved,
	}
	sess.tpl = newSessionTemplates(sess.Base())

	s.byToken[token] = sess
	s.byDir[dir] = sess
	s.logger.Printf("workspace %s (%s) served at %s/", name, source, sess.Base())
	return sess, nil
}

// maxTokenDraws bounds the generator's retry loop. A reserved or colliding
// draw is astronomically unlikely in production; the loop exists so an
// injected generator that keeps returning one value errors instead of
// spinning forever.
const maxTokenDraws = 100

func (s *Sessions) mintLocked() (string, error) {
	for i := 0; i < maxTokenDraws; i++ {
		token, err := s.newToken()
		if err != nil {
			return "", fmt.Errorf("generate a session token: %w", err)
		}
		if reason, bad := reservedSegments[token]; bad {
			s.logger.Printf("discarded a session token %q (%s)", token, reason)
			continue
		}
		if token == "" {
			continue
		}
		if _, taken := s.byToken[token]; taken {
			continue
		}
		return token, nil
	}
	return "", fmt.Errorf("could not generate a session token in %d draws", maxTokenDraws)
}

func (s *Sessions) byDirLocked(dir string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.byDir[dir]
	return sess, ok
}

func (s *Sessions) workspace(name string) (config.Workspace, error) {
	for _, w := range s.workspaces {
		if w.Name != name {
			continue
		}
		if !w.Selectable() {
			return config.Workspace{}, fmt.Errorf("workspace %q is unavailable: %s", name, w.Problem)
		}
		return w, nil
	}
	return config.Workspace{}, fmt.Errorf("no workspace named %q", name)
}

// workspaceDir is the registry key a workspace entry maps to. It resolves
// the project's config, which is the only way to know which directory under
// the root actually holds the tasks.
func (s *Sessions) workspaceDir(w config.Workspace) (string, error) {
	cfg, err := config.LoadForRoot(w.Path, w.TasksDir)
	if err != nil {
		return "", err
	}
	return cleanDir(cfg.TasksDir()), nil
}

// cleanDir is the key normalization: one backlog is one key however it was
// spelled. Symlinks are resolved when they can be, so /tmp and /private/tmp
// on macOS do not register twice.
func cleanDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// randomToken is 16 crypto/rand bytes as 22 URL-safe characters.
func randomToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
