package vcs

import (
	"fmt"
	"strings"
)

// reflogPrefix marks every ref update this package makes, so the reflog
// says who moved a branch.
const reflogPrefix = "mcp-task-manager: "

// BranchSHA returns the commit a local branch points at; ok is false when
// the branch does not exist.
func (r *Repo) BranchSHA(name string) (sha string, ok bool, err error) {
	if err := r.ensure(); err != nil {
		return "", false, err
	}
	return r.branchSHA(name)
}

func (r *Repo) branchSHA(name string) (string, bool, error) {
	out, err := r.git("rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	switch {
	case err == nil:
		return strings.TrimSpace(out.stdout), true, nil
	case out.code == 1:
		return "", false, nil
	default:
		return "", false, err
	}
}

// FirstExistingBranch returns the first of names that exists as a local
// branch, with its commit.
func (r *Repo) FirstExistingBranch(names []string) (string, string, error) {
	if err := r.ensure(); err != nil {
		return "", "", err
	}
	for _, name := range names {
		sha, ok, err := r.branchSHA(name)
		if err != nil {
			return "", "", err
		}
		if ok {
			return name, sha, nil
		}
	}
	return "", "", fmt.Errorf("none of the branches %v exist", names)
}

// BranchAvailable reports, as an error naming the obstacle, why a branch
// called name could not be created: an invalid name, an existing branch of
// that name, an existing branch at one of its path prefixes, or existing
// branches below it.
func (r *Repo) BranchAvailable(name string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	if _, err := r.git("check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a valid branch name", name)
	}
	if _, ok, err := r.branchSHA(name); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("branch %q already exists", name)
	}
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		prefix := strings.Join(parts[:i], "/")
		if _, ok, err := r.branchSHA(prefix); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("branch %q exists, so %q cannot be created below it", prefix, name)
		}
	}
	below, err := r.line("for-each-ref", "--count=1", "--format=%(refname:short)", "refs/heads/"+name+"/")
	if err != nil {
		return err
	}
	if below != "" {
		return fmt.Errorf("branch %q exists below %q, so %q cannot be created", below, name, name)
	}
	return nil
}

// CreateBranch creates a branch at sha; it fails if the branch exists.
func (r *Repo) CreateBranch(name, sha string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	_, err := r.git("update-ref", "-m", reflogPrefix+"create", "refs/heads/"+name, sha, "")
	return err
}

// MoveBranch moves a branch from oldSHA to newSHA; it fails, leaving the
// branch alone, if the branch no longer points at oldSHA.
func (r *Repo) MoveBranch(name, newSHA, oldSHA string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	_, err := r.git("update-ref", "-m", reflogPrefix+"move", "refs/heads/"+name, newSHA, oldSHA)
	return err
}

// DeleteBranch deletes a branch; it fails, leaving the branch alone, if the
// branch does not point at expectSHA.
func (r *Repo) DeleteBranch(name, expectSHA string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	_, err := r.git("update-ref", "-m", reflogPrefix+"delete", "-d", "refs/heads/"+name, expectSHA)
	return err
}

// Switch checks out an existing local branch. Git refuses, changing
// nothing, when local changes would be overwritten. --no-guess keeps a
// missing branch from being created off a remote-tracking one.
func (r *Repo) Switch(name string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	_, err := r.git("switch", "--no-guess", name)
	return err
}

// ResetKeep moves the checked-out branch to sha, keeping local changes;
// git refuses, changing nothing, when a local change would conflict.
func (r *Repo) ResetKeep(sha string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	_, err := r.git("reset", "--keep", sha)
	return err
}
