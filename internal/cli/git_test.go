package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// gitProject is a code repository with git branching enabled, resolved
// through MCP_PROJECT_DIR as a user's shell would.
func gitProject(t *testing.T) string {
	t.Helper()
	testsupport.IsolateEnv(t)
	testsupport.RequireGit(t)
	dir := testsupport.NewGitRepo(t, filepath.Join(t.TempDir(), "code"))
	testsupport.WriteFile(t, filepath.Join(dir, config.ConfigFileName), "git:\n  branching: true\n")
	testsupport.WriteFile(t, filepath.Join(dir, ".gitignore"), ".tasks/\n")
	testsupport.Git(t, dir, "add", ".")
	testsupport.Git(t, dir, "commit", "-q", "-m", "configure")
	t.Setenv(config.EnvProjectDir, dir)
	return dir
}

func run(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := RunWithArgs(append([]string{"mcp-task-manager"}, args...), &stdout, &stderr); code != 0 {
		t.Fatalf("%v: exit %d, stderr: %s", args, code, stderr.String())
	}
	return stdout.String()
}

func TestStartCommandPrintsBranch(t *testing.T) {
	gitProject(t)
	run(t, "create", "Add login", "--id", "login")

	if out := run(t, "start", "login"); !strings.Contains(out, "Task #login started on branch dev/wip/login.") {
		t.Errorf("start output = %q", out)
	}
}

func TestCompleteCommandMessageFlag(t *testing.T) {
	dir := gitProject(t)
	run(t, "create", "Add login", "--id", "login")
	run(t, "start", "login")
	testsupport.WriteFile(t, filepath.Join(dir, "login.go"), "package login\n")

	out := run(t, "complete", "login", "-m", "feat: login form")
	if !strings.Contains(out, "Final branch: dev/login.") {
		t.Errorf("complete output = %q", out)
	}
	if got := testsupport.Git(t, dir, "log", "-1", "--format=%s", "dev/login"); got != "feat: login form" {
		t.Errorf("final commit subject = %q", got)
	}
	if got := testsupport.Git(t, dir, "branch", "--show-current"); got != "dev/login" {
		t.Errorf("HEAD = %q", got)
	}
}
