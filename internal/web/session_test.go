package web

import (
	"errors"
	"sync"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// countingOpener wraps the real opener and records how often it ran, which is
// how "a repeat pick reuses the session" is proved rather than assumed.
type countingOpener struct {
	mu    sync.Mutex
	calls int
}

func (c *countingOpener) open(ws config.Workspace) (*project.Resolved, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return openWorkspaceForTest(ws)
}

func (c *countingOpener) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func newPickableSessions(t *testing.T, workspaces ...config.Workspace) (*Sessions, *countingOpener) {
	t.Helper()
	opener := &countingOpener{}
	return NewSessions(SessionsConfig{
		Workspaces: workspaces,
		Open:       opener.open,
		Logger:     discardLogger(),
	}), opener
}

// The registry is keyed on the tasks directory, so picking the same workspace
// twice is one session with one token: URLs stay stable across picks and the
// registry cannot grow from clicking.
func TestPickReusesTheSessionForOneWorkspace(t *testing.T) {
	ws := newBacklogWorkspace(t, "alpha")
	sessions, opener := newPickableSessions(t, ws)

	first, err := sessions.Pick("alpha")
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	second, err := sessions.Pick("alpha")
	if err != nil {
		t.Fatalf("second Pick() error = %v", err)
	}

	if first.Token != second.Token {
		t.Errorf("Pick() minted a second token %q for %q", second.Token, first.Token)
	}
	if opener.count() != 1 {
		t.Errorf("the workspace was opened %d times, want once", opener.count())
	}
	if first.Source() != SourceWeb {
		t.Errorf("Source() = %q, want %q", first.Source(), SourceWeb)
	}
}

func TestPickRefusesWhatItCannotOpen(t *testing.T) {
	broken := config.Workspace{Name: "broken", Path: "/no/such/dir", Problem: "path is not a directory"}
	sessions, opener := newPickableSessions(t, broken)

	if _, err := sessions.Pick("broken"); err == nil {
		t.Error("Pick() on an entry with a problem error = nil, want a refusal")
	}
	if _, err := sessions.Pick("nothing-like-this"); err == nil {
		t.Error("Pick() on an unknown name error = nil, want a refusal")
	}
	if opener.count() != 0 {
		t.Errorf("a refused pick still opened %d workspaces", opener.count())
	}
	if len(sessions.Live()) != 0 {
		t.Error("a refused pick registered a session")
	}
}

// MCP resolving a backlog a human already opened hands the session over in
// place: same token, so an open tab keeps its URL and starts seeing MCP's
// writes through the shared service lock.
func TestAdoptTakesOverAWebSessionKeepingItsToken(t *testing.T) {
	rs, _, dir := testsupport.NewBacklog(t)
	ws := config.Workspace{Name: "alpha", Path: parentDir(dir), TasksDir: dir}
	sessions, opener := newPickableSessions(t, ws)

	picked, err := sessions.Pick("alpha")
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if opener.count() != 1 {
		t.Fatalf("the workspace was opened %d times, want once", opener.count())
	}

	resolved, _ := rs.Current()
	adopted, err := sessions.Adopt(resolved)
	if err != nil {
		t.Fatalf("Adopt() error = %v", err)
	}

	if adopted.Token != picked.Token {
		t.Errorf("Adopt() minted a new token %q, want %q kept", adopted.Token, picked.Token)
	}
	if adopted.Source() != SourceMCP {
		t.Errorf("Source() = %q, want %q after an adopt", adopted.Source(), SourceMCP)
	}
	if adopted.Project() != resolved {
		t.Error("Adopt() did not swap in the live project")
	}
	if n := len(sessions.Live()); n != 1 {
		t.Errorf("Live() = %d sessions, want 1", n)
	}
}

// And the other way round: a workspace MCP already serves is not opened a
// second time, so one backlog is one service and one index.
func TestPickDoesNotReopenAnMCPBacklog(t *testing.T) {
	rs, _, dir := testsupport.NewBacklog(t)
	ws := config.Workspace{Name: "alpha", Path: parentDir(dir), TasksDir: dir}
	sessions, opener := newPickableSessions(t, ws)

	resolved, _ := rs.Current()
	adopted, err := sessions.Adopt(resolved)
	if err != nil {
		t.Fatalf("Adopt() error = %v", err)
	}

	picked, err := sessions.Pick("alpha")
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if picked.Token != adopted.Token {
		t.Errorf("Pick() = %q, want the live MCP session %q", picked.Token, adopted.Token)
	}
	if opener.count() != 0 {
		t.Errorf("Pick() opened the backlog %d times although MCP already serves it", opener.count())
	}
	if picked.Project() != resolved {
		t.Error("Pick() returned a session that is not on MCP's project")
	}
}

// Re-resolution is the normal case: the resolver rebuilds the project after
// its roots change and adopts it again.
func TestAdoptSwapsTheProjectOnReresolution(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)

	first := adoptBacklog(t, sessions, rs)
	second := adoptBacklog(t, sessions, rs)

	if first.Token != second.Token {
		t.Errorf("a re-adopt minted a new token %q, want %q", second.Token, first.Token)
	}
	if n := len(sessions.Live()); n != 1 {
		t.Errorf("Live() = %d sessions, want 1", n)
	}
}

// A token that is a reserved route segment is discarded at generation: the
// route table already means something by it. Task ids are validated because
// a caller supplies them; a token is generated, so this is a postcondition
// of the generator and not an error anybody sees.
func TestTokenGeneratorSkipsReservedSegments(t *testing.T) {
	draws := []string{"static", "healthz", "sessions", "good-token"}
	i := 0
	sessions := NewSessions(SessionsConfig{
		Logger: discardLogger(),
		NewToken: func() (string, error) {
			token := draws[i]
			i++
			return token, nil
		},
	})

	rs, _, _ := testsupport.NewBacklog(t)
	sess := adoptBacklog(t, sessions, rs)

	if sess.Token != "good-token" {
		t.Errorf("Token = %q, want the first non-reserved draw", sess.Token)
	}
	if i != len(draws) {
		t.Errorf("the generator was drawn %d times, want %d", i, len(draws))
	}
}

// A generator that can only produce a reserved segment fails the
// registration instead of spinning forever or publishing /static/.
func TestTokenGeneratorGivesUpOnOnlyReservedDraws(t *testing.T) {
	sessions := NewSessions(SessionsConfig{
		Logger:   discardLogger(),
		NewToken: func() (string, error) { return "static", nil },
	})

	rs, _, _ := testsupport.NewBacklog(t)
	resolved, _ := rs.Current()
	if _, err := sessions.Adopt(resolved); err == nil {
		t.Error("Adopt() error = nil, want a token-generation failure")
	}
	if len(sessions.Live()) != 0 {
		t.Error("a failed registration left a session behind")
	}
}

func TestTokenGeneratorErrorIsReported(t *testing.T) {
	sessions := NewSessions(SessionsConfig{
		Logger:   discardLogger(),
		NewToken: func() (string, error) { return "", errors.New("no entropy") },
	})

	rs, _, _ := testsupport.NewBacklog(t)
	resolved, _ := rs.Current()
	if _, err := sessions.Adopt(resolved); err == nil {
		t.Error("Adopt() error = nil, want the generator's error")
	}
}

func TestRandomTokensAreDistinctAndURLSafe(t *testing.T) {
	seen := make(map[string]bool, 64)
	for i := 0; i < 64; i++ {
		token, err := randomToken()
		if err != nil {
			t.Fatalf("randomToken() error = %v", err)
		}
		if seen[token] {
			t.Fatalf("randomToken() repeated %q", token)
		}
		seen[token] = true
		for _, r := range token {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				t.Fatalf("randomToken() = %q, which is not URL-safe", token)
			}
		}
	}
}

func TestLookupOfAnUnknownTokenMisses(t *testing.T) {
	sessions := newTestSessions(t)
	if _, ok := sessions.Lookup("nope"); ok {
		t.Error("Lookup() found an unregistered token")
	}
	if _, ok := sessions.Lookup(""); ok {
		t.Error("Lookup(\"\") found something")
	}
}

func TestPathForNamesTheLiveSession(t *testing.T) {
	rs, _, dir := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)

	path, found := sessions.PathFor(dir)
	if !found {
		t.Fatalf("PathFor(%q) found = false", dir)
	}
	if want := sess.Base() + "/"; path != want {
		t.Errorf("PathFor() = %q, want %q", path, want)
	}
	if _, found := sessions.PathFor(t.TempDir()); found {
		t.Error("PathFor() found a directory nobody registered")
	}
}

// Workspaces reports the configured list with the live session's token, so
// the welcome page can link straight into a board instead of re-picking it.
func TestWorkspacesReportsLiveSessions(t *testing.T) {
	rs, _, dir := testsupport.NewBacklog(t)
	ws := config.Workspace{Name: "alpha", Path: parentDir(dir), TasksDir: dir}
	broken := config.Workspace{Name: "broken", Path: "/no/such/dir", Problem: "path is not a directory"}
	sessions := NewSessions(SessionsConfig{
		Workspaces: []config.Workspace{ws, broken},
		Open:       openWorkspaceForTest,
		Logger:     discardLogger(),
	})

	statuses := sessions.Workspaces()
	if len(statuses) != 2 {
		t.Fatalf("Workspaces() = %d entries, want both", len(statuses))
	}
	if statuses[0].Live {
		t.Error("the workspace is live before anything opened it")
	}

	sess := adoptBacklog(t, sessions, rs)
	statuses = sessions.Workspaces()
	if !statuses[0].Live || statuses[0].Token != sess.Token {
		t.Errorf("Workspaces()[0] = %+v, want live with token %q", statuses[0], sess.Token)
	}
	if statuses[1].Live {
		t.Error("an unavailable entry reported itself live")
	}
}

// Concurrent picks of one workspace still yield one session: the project is
// built outside the registry lock, so the insert has to re-check.
func TestConcurrentPicksYieldOneSession(t *testing.T) {
	ws := newBacklogWorkspace(t, "alpha")
	sessions, _ := newPickableSessions(t, ws)

	const n = 8
	tokens := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess, err := sessions.Pick("alpha")
			if err != nil {
				t.Errorf("Pick() error = %v", err)
				return
			}
			tokens[i] = sess.Token
		}(i)
	}
	wg.Wait()

	for i, token := range tokens {
		if token != tokens[0] {
			t.Fatalf("tokens[%d] = %q, want %q: concurrent picks split the session", i, token, tokens[0])
		}
	}
	if n := len(sessions.Live()); n != 1 {
		t.Errorf("Live() = %d sessions, want 1", n)
	}
}
