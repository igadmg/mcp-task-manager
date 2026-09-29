package config

import (
	"reflect"
	"testing"
)

// resolveGit resolves a fresh project whose mcp-tasks.yaml holds body (none
// when body is empty) and returns its git section.
func resolveGit(t *testing.T, body string) GitConfig {
	t.Helper()
	root := tempDir(t)
	if body != "" {
		writeConfig(t, root, body)
	}
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return cfg.Git
}

func TestGitConfigDefaults(t *testing.T) {
	isolateEnv(t)
	git := resolveGit(t, "")

	if git.Branching {
		t.Error("Git.Branching = true, want false by default: branching touches the user's repository only when asked")
	}
	if !reflect.DeepEqual(git.BaseBranches, DefaultBaseBranches) {
		t.Errorf("Git.BaseBranches = %q, want %q", git.BaseBranches, DefaultBaseBranches)
	}
}

func TestGitConfigParse(t *testing.T) {
	isolateEnv(t)
	git := resolveGit(t, "git:\n  branching: true\n  base_branches: [develop, main]\n")

	if !git.Branching {
		t.Error("Git.Branching = false, want true from the file")
	}
	if want := []string{"develop", "main"}; !reflect.DeepEqual(git.BaseBranches, want) {
		t.Errorf("Git.BaseBranches = %q, want %q", git.BaseBranches, want)
	}
}

func TestGitConfigPartialSectionKeepsDefaults(t *testing.T) {
	isolateEnv(t)
	git := resolveGit(t, "git:\n  branching: true\n")

	if !git.Branching {
		t.Error("Git.Branching = false, want true from the file")
	}
	if !reflect.DeepEqual(git.BaseBranches, DefaultBaseBranches) {
		t.Errorf("Git.BaseBranches = %q, want %q: a partial section must not zero the rest", git.BaseBranches, DefaultBaseBranches)
	}
}

func TestGitConfigBaseBranchesTrimmedAndRefilled(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{"blank entries dropped", "git:\n  base_branches: [\"  main \", \"\", \"  \"]\n", []string{"main"}},
		{"only blanks refilled", "git:\n  base_branches: [\"\", \"  \"]\n", DefaultBaseBranches},
		{"empty list refilled", "git:\n  base_branches: []\n", DefaultBaseBranches},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			git := resolveGit(t, tc.body)
			if !reflect.DeepEqual(git.BaseBranches, tc.want) {
				t.Errorf("Git.BaseBranches = %q, want %q", git.BaseBranches, tc.want)
			}
		})
	}
}

func TestGitBranchingEnvOverride(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		env  string
		want bool
	}{
		{"true enables over file false", "git:\n  branching: false\n", "true", true},
		{"false disables over file true", "git:\n  branching: true\n", "false", false},
		{"unparseable is ignored", "git:\n  branching: true\n", "maybe", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv(EnvGitBranching, tc.env)
			if got := resolveGit(t, tc.body).Branching; got != tc.want {
				t.Errorf("Git.Branching = %v, want %v with %s=%q", got, tc.want, EnvGitBranching, tc.env)
			}
		})
	}
}

func TestDefaultBaseBranchesNotAliased(t *testing.T) {
	want := append([]string(nil), DefaultBaseBranches...)

	cfg := DefaultConfig()
	cfg.Git.BaseBranches[0] = "mutated"
	cfg.applyDefaults()
	cfg.Git.BaseBranches[0] = "mutated again"

	if !reflect.DeepEqual(DefaultBaseBranches, want) {
		t.Errorf("DefaultBaseBranches = %q, want %q: a returned config must not alias the package default", DefaultBaseBranches, want)
	}
}
