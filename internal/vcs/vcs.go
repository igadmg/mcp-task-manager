// Package vcs wraps the system git binary for the branch-per-task workflow.
//
// It imports only the standard library and knows nothing about tasks: the
// task service drives it through the task.GitRepo interface. Every command
// runs as `git -C <toplevel> ...` with a per-command timeout, no terminal
// prompts and the C locale, so output is parseable and a hung credential
// helper can never block the server.
package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// commandTimeout bounds every git invocation.
const commandTimeout = 60 * time.Second

var (
	// ErrGitNotFound means no git executable is on PATH.
	ErrGitNotFound = errors.New("git executable not found on PATH")
	// ErrDetachedHead means HEAD is not on a branch.
	ErrDetachedHead = errors.New("HEAD is detached; check out a branch first")
	// ErrReplayConflict means replaying a branch onto a new base conflicts
	// and the conflicting paths could not be determined.
	ErrReplayConflict = errors.New("replay conflicts")
	// ErrReplayMerge means the replayed range contains a merge commit,
	// which git replay cannot replay.
	ErrReplayMerge = errors.New("the branch contains a merge commit, which git replay cannot rebase")
	// ErrReplayUnsupported means the installed git has no print mode for
	// git replay.
	ErrReplayUnsupported = errors.New("git replay with --ref-action=print is required; upgrade git")
)

// Identity is the user name branches are namespaced under.
type Identity struct {
	Name string
	// FromGitEmail is false when the name fell back to the OS user because
	// git has no user.email.
	FromGitEmail bool
}

// Repo is the git repository holding a project. New runs no git; the
// toplevel and the tasks-directory exclusion are resolved on first use.
type Repo struct {
	root     string
	tasksDir string

	mu sync.Mutex
	// top is the worktree toplevel, empty until resolved.
	top string
	// tasksRel is the tasks directory relative to top, slash-separated,
	// empty when it lies outside the worktree.
	tasksRel string
	// exclude is the pathspec keeping the tasks directory out of snapshots,
	// empty when the tasks directory lies outside the worktree.
	exclude string
	// tasksCoversTop is set when the tasks directory is the toplevel or an
	// ancestor of it, where nothing could be excluded safely.
	tasksCoversTop bool
}

// New returns the repository containing root. tasksDir is the backlog's
// directory; when it lies inside the worktree it is excluded from every
// snapshot and dirtiness check.
func New(root, tasksDir string) *Repo {
	return &Repo{root: root, tasksDir: tasksDir}
}

// output is what one git invocation produced.
type output struct {
	stdout string
	stderr string
	code   int
}

// run executes git in dir (no -C when dir is empty). err is nil only on exit
// code 0; on a non-zero exit the code is still reported, so callers can treat
// an expected exit 1 as an answer rather than a failure.
func run(dir, stdin string, env []string, args ...string) (output, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	out := output{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		out.code = -1
		return out, ErrGitNotFound
	}
	out.code = -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.code = exitErr.ExitCode()
	}
	msg := strings.TrimSpace(out.stderr)
	if ctx.Err() != nil {
		msg = fmt.Sprintf("timed out after %s", commandTimeout)
	}
	if msg == "" {
		msg = err.Error()
	}
	return out, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
}

// git runs a command in the resolved toplevel. Callers must have called
// ensure first.
func (r *Repo) git(args ...string) (output, error) {
	return run(r.top, "", nil, args...)
}

// line runs a command in the toplevel and returns its trimmed stdout.
func (r *Repo) line(args ...string) (string, error) {
	out, err := r.git(args...)
	return strings.TrimSpace(out.stdout), err
}

// ensure resolves and caches the toplevel and the tasks exclusion. A failure
// is not cached, so a later call can succeed once the repository exists.
func (r *Repo) ensure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.top != "" {
		return nil
	}

	bare, err := run(r.root, "", nil, "rev-parse", "--is-bare-repository")
	if err != nil {
		if errors.Is(err, ErrGitNotFound) {
			return err
		}
		return fmt.Errorf("%s is not inside a git repository: %w", r.root, err)
	}
	if strings.TrimSpace(bare.stdout) == "true" {
		return fmt.Errorf("%s is a bare git repository; branching needs a worktree", r.root)
	}
	out, err := run(r.root, "", nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("%s is not inside a git worktree: %w", r.root, err)
	}

	top := realPath(strings.TrimSpace(out.stdout))
	tasks := realPath(r.tasksDir)
	tasksRel, exclude, covers := "", "", false
	if rel, err := filepath.Rel(top, tasks); err == nil {
		switch {
		case rel == ".":
			covers = true
		case !isOutside(rel):
			tasksRel = filepath.ToSlash(rel)
			exclude = ":(top,exclude)" + tasksRel
		}
	}
	if rel, err := filepath.Rel(tasks, top); err == nil && !isOutside(rel) {
		covers = true
	}
	r.top, r.tasksRel, r.exclude, r.tasksCoversTop = top, tasksRel, exclude, covers
	return nil
}

// isOutside reports whether a filepath.Rel result climbs out of its base.
func isOutside(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// realPath resolves symlinks in p, including in a tail that does not exist
// yet, so /var and /private/var on macOS compare equal.
func realPath(p string) string {
	p = filepath.Clean(p)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(realPath(parent), filepath.Base(p))
}

// pathspec is the whole worktree minus the tasks directory.
func (r *Repo) pathspec() []string {
	if r.exclude == "" {
		return []string{"--", "."}
	}
	return []string{"--", ".", r.exclude}
}

// inProgressMarkers are the git-dir entries that mean an operation is half
// done; branching must not start in the middle of one.
var inProgressMarkers = []struct{ file, op string }{
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
	{"rebase-merge", "rebase"},
	{"rebase-apply", "rebase or am"},
	{"BISECT_LOG", "bisect"},
}

// Check verifies the repository can take branch operations: git exists, the
// root is in a non-bare worktree, no operation is in progress, the index has
// no unmerged paths, git has an author identity, and the tasks directory is
// neither the toplevel nor above it.
func (r *Repo) Check() error {
	if err := r.ensure(); err != nil {
		return err
	}
	if r.tasksCoversTop {
		return fmt.Errorf("the tasks directory %s contains the whole repository %s; branching needs it inside or outside the worktree", r.tasksDir, r.top)
	}
	for _, m := range inProgressMarkers {
		p, err := r.line("rev-parse", "--git-path", m.file)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.top, p)
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("a %s is in progress in %s (%s exists); finish or abort it first", m.op, r.top, m.file)
		}
	}
	unmerged, err := r.line("ls-files", "-u")
	if err != nil {
		return err
	}
	if unmerged != "" {
		return fmt.Errorf("the index of %s has unmerged paths; resolve them first", r.top)
	}
	if _, err := r.git("var", "GIT_AUTHOR_IDENT"); err != nil {
		return fmt.Errorf("git has no author identity; set user.name and user.email: %w", err)
	}
	return nil
}

// Head returns the checked-out branch and its commit. A detached HEAD is
// ErrDetachedHead.
func (r *Repo) Head() (branch, sha string, err error) {
	if err := r.ensure(); err != nil {
		return "", "", err
	}
	out, err := r.git("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if out.code == 1 {
			return "", "", ErrDetachedHead
		}
		return "", "", err
	}
	sha, err = r.line("rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(out.stdout), sha, nil
}

// ResolveCommit returns the commit rev names.
func (r *Repo) ResolveCommit(rev string) (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	sha, err := r.line("rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q does not name a commit: %w", rev, err)
	}
	return sha, nil
}

// IsAncestor reports whether commit a is an ancestor of (or equal to) b.
func (r *Repo) IsAncestor(a, b string) (bool, error) {
	if err := r.ensure(); err != nil {
		return false, err
	}
	out, err := r.git("merge-base", "--is-ancestor", a, b)
	switch {
	case err == nil:
		return true, nil
	case out.code == 1:
		return false, nil
	default:
		return false, err
	}
}

// TreeOf returns the tree of rev.
func (r *Repo) TreeOf(rev string) (string, error) {
	if err := r.ensure(); err != nil {
		return "", err
	}
	return r.line("rev-parse", rev+"^{tree}")
}

// ResolveIdentity picks the user name branches are namespaced under: the
// sanitized local part of git's user.email, else the OS user, else
// "default". It works outside a repository, because git config also reads
// the global configuration.
func ResolveIdentity(root string) Identity {
	dir := root
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		dir = ""
	}
	if out, err := run(dir, "", nil, "config", "--get", "user.email"); err == nil {
		local := strings.TrimSpace(out.stdout)
		if i := strings.IndexByte(local, '@'); i >= 0 {
			local = local[:i]
		}
		if name := sanitizeUser(local); name != "" {
			return Identity{Name: name, FromGitEmail: true}
		}
	}

	var candidates []string
	if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndexByte(name, '\\'); i >= 0 {
			name = name[i+1:]
		}
		candidates = append(candidates, name)
	}
	candidates = append(candidates, os.Getenv("USER"), os.Getenv("USERNAME"))
	for _, c := range candidates {
		if name := sanitizeUser(c); name != "" {
			return Identity{Name: name}
		}
	}
	return Identity{Name: "default"}
}

// sanitizeUser turns a raw user name into one safe as a branch path
// component: lowercase, runes outside [a-z0-9._-] become "-", runs of "-"
// collapse, leading and trailing "." and "-" go, ".." becomes ".", and a
// trailing ".lock" is stripped. The steps repeat until nothing changes, since
// stripping ".lock" can expose a new trailing "." or "-".
func sanitizeUser(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	for {
		prev := s
		for strings.Contains(s, "--") {
			s = strings.ReplaceAll(s, "--", "-")
		}
		s = strings.Trim(s, ".-")
		for strings.Contains(s, "..") {
			s = strings.ReplaceAll(s, "..", ".")
		}
		s = strings.TrimSuffix(s, ".lock")
		if s == prev {
			return s
		}
	}
}
