package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// RequireGit skips the test when git is not installed, and otherwise pins
// git's environment so the developer's own configuration cannot leak in:
// no global or system config, fixed commit dates, and no identity or
// repository location from the environment.
//
// It uses t.Setenv, so callers must not use t.Parallel.
func RequireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
	for _, name := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
	} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

// RequireGitReplay skips the test unless the installed git has
// `git replay --ref-action`. Call it after RequireGit.
func RequireGitReplay(t *testing.T) {
	t.Helper()
	out, _ := exec.Command("git", "replay", "-h").CombinedOutput()
	if !strings.Contains(string(out), "--ref-action") {
		t.Skip("git replay --ref-action not available")
	}
}

// NewGitRepo initializes a repository in dir on branch main_patched, with a
// local identity (dev@example.com) and one commit of README. It returns dir
// with symlinks resolved, so it compares equal to what git reports.
func NewGitRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", dir, err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s) error = %v", dir, err)
	}
	Git(t, real, "init", "-q", "-b", "main_patched")
	Git(t, real, "config", "user.email", "dev@example.com")
	Git(t, real, "config", "user.name", "Dev")
	WriteFile(t, filepath.Join(real, "README"), "readme\n")
	Git(t, real, "add", "README")
	Git(t, real, "commit", "-q", "-m", "initial")
	return real
}

// Git runs git in dir, fails the test on error, and returns trimmed stdout.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// WriteFile writes content to path, creating parent directories.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
