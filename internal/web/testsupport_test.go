package web

import (
	"io"
	"log"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// adoptBacklog registers an already-built project the way the MCP side does
// and returns its session, so a test can address it by token.
func adoptBacklog(t *testing.T, s *Sessions, rs *project.Resolver) *Session {
	t.Helper()
	resolved, ok := rs.Current()
	if !ok {
		t.Fatal("the test backlog is not resolved")
	}
	sess, err := s.Adopt(resolved)
	if err != nil {
		t.Fatalf("Adopt() error = %v", err)
	}
	return sess
}

// newTestSessions is a registry holding one session over a fresh backlog.
func newTestSessions(t *testing.T, workspaces ...config.Workspace) *Sessions {
	t.Helper()
	return NewSessions(SessionsConfig{Workspaces: workspaces, Logger: discardLogger()})
}

// openWorkspaceForTest is the production opener, so a Pick in a test exercises
// the real read-only build path.
func openWorkspaceForTest(ws config.Workspace) (*project.Resolved, error) {
	cfg, err := config.LoadForRoot(ws.Path, ws.TasksDir)
	if err != nil {
		return nil, err
	}
	return project.BuildReadOnly(cfg)
}

// newBacklogWorkspace is a temp project root holding a backlog, plus the
// workspace entry naming it.
func newBacklogWorkspace(t *testing.T, name string, specs ...testsupport.TaskSpec) config.Workspace {
	t.Helper()
	rs, svc, dir := testsupport.NewBacklog(t)
	_ = rs
	if len(specs) > 0 {
		testsupport.Seed(t, svc, specs...)
	}
	// testsupport.NewBacklog puts the tasks directly in the temp dir, so the
	// project root is its parent and the tasks dir its base name.
	return config.Workspace{Name: name, Path: parentDir(dir), TasksDir: dir}
}

func parentDir(dir string) string {
	for i := len(dir) - 1; i > 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[:i]
		}
	}
	return dir
}
