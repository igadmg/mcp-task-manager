package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// WebFileName is the web server's own configuration file. It is a
// machine/user-level file, not a per-project one: the workspace list is
// cross-project and must not depend on which project the process happened to
// be started from.
const WebFileName = "web.yaml"

// webFileDirName is the directory WebFileName lives in, under the user's
// config root.
const webFileDirName = "mcp-task-manager"

// WebFileVersion is the only format version this build understands. A higher
// one in the file is reported, not refused: an older binary reading a newer
// file should say so and keep serving.
const WebFileVersion = 1

// Workspace is one selectable backlog on the dashboard's welcome page.
type Workspace struct {
	// Name is how the workspace is listed and what the session POST names.
	// Defaults to the base name of Path.
	Name string `yaml:"name,omitempty"`
	// Path is the project root, not the tasks directory: the tasks
	// directory under it is resolved by the rules that already exist, so a
	// workspace entry does not have to restate what the project's own
	// mcp-tasks.yaml already says. A leading ~ is expanded.
	Path string `yaml:"path"`
	// TasksDir overrides the project's own tasks_dir for this entry.
	TasksDir string `yaml:"tasks_dir,omitempty"`
	// Problem is why this entry cannot be opened, empty when it can. An
	// invalid entry is kept and listed as unavailable rather than dropped,
	// so a mistyped path explains itself instead of vanishing.
	Problem string `yaml:"-"`
}

// Selectable reports whether this entry can be opened.
func (w Workspace) Selectable() bool { return w.Problem == "" }

// WebFile is the loaded web.yaml.
type WebFile struct {
	Version    int         `yaml:"version,omitempty"`
	Workspaces []Workspace `yaml:"workspaces,omitempty"`

	// Path is the file this was read from, empty when no file was found.
	Path string `yaml:"-"`
	// Problems are the findings of ValidateWorkspaces, in file order. A
	// consumer that wants to show them itself reads this instead of
	// re-deriving them.
	Problems []string `yaml:"-"`
}

// EnvWebConfig names the web server's own config file explicitly. Unlike the
// well-known locations, a path given here that does not exist is an error:
// the operator named it, so a typo must not degrade into silence.
const EnvWebConfig = "MCP_WEB_CONFIG"

// EnvXDGConfigHome is the standard config root override.
const EnvXDGConfigHome = "XDG_CONFIG_HOME"

// WebFilePath reports where the web config file is, if anywhere. In order:
// MCP_WEB_CONFIG, $XDG_CONFIG_HOME/mcp-task-manager/web.yaml,
// ~/.config/mcp-task-manager/web.yaml.
//
// ~/.config is used literally on every platform rather than
// os.UserConfigDir(), which on macOS yields ~/Library/Application Support:
// this is a file a developer edits by hand.
func WebFilePath() (path string, found bool, err error) {
	if env := strings.TrimSpace(os.Getenv(EnvWebConfig)); env != "" {
		if _, err := os.Stat(env); err != nil {
			return "", false, fmt.Errorf("%s=%s: %w", EnvWebConfig, env, err)
		}
		return filepath.Clean(env), true, nil
	}

	var candidates []string
	if xdg := strings.TrimSpace(os.Getenv(EnvXDGConfigHome)); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, webFileDirName, WebFileName))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".config", webFileDirName, WebFileName))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true, nil
		}
	}
	return "", false, nil
}

// LoadWebFile reads the web config file, fills each entry's defaults and
// reports what is wrong with it.
//
// A missing file at a well-known location is not an error: there are then no
// selectable workspaces, the welcome page says so, and a dashboard embedded
// in an MCP server still serves the project that server resolved. That is
// the normal state.
//
// A YAML type error fails the load, like mcp-tasks.yaml. An invalid
// *workspace* does not: it is reported and kept (see Workspace.Problem).
func LoadWebFile() (*WebFile, error) {
	path, found, err := WebFilePath()
	if err != nil {
		return nil, err
	}
	wf := &WebFile{Path: path}
	if !found {
		return wf, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, wf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	wf.Path = path

	wf.normalize()
	wf.Problems = ValidateWorkspaces(wf)
	wf.reportProblems()
	return wf, nil
}

// normalize expands ~ and fills each entry's name from its path. It runs
// before validation, because validation checks the expanded path.
func (wf *WebFile) normalize() {
	for i := range wf.Workspaces {
		w := &wf.Workspaces[i]
		w.Path = expandHome(strings.TrimSpace(w.Path))
		w.TasksDir = strings.TrimSpace(w.TasksDir)
		w.Name = strings.TrimSpace(w.Name)
		if w.Name == "" && w.Path != "" {
			w.Name = filepath.Base(w.Path)
		}
	}
}

// ValidateWorkspaces checks the written entries and marks the ones that
// cannot be opened. It is pure except for the stat calls it needs, and it
// never removes an entry: the welcome page lists an unavailable workspace
// with its reason, so an operator who mistyped a path sees why instead of
// wondering where it went, and one bad entry cannot hide the others.
func ValidateWorkspaces(wf *WebFile) []string {
	var problems []string

	if wf.Version != 0 && wf.Version != WebFileVersion {
		problems = append(problems, fmt.Sprintf(
			"version %d is not supported (this build understands version %d); reading it anyway",
			wf.Version, WebFileVersion))
	}

	seenName := make(map[string]int, len(wf.Workspaces))
	seenPath := make(map[string]int, len(wf.Workspaces))

	for i := range wf.Workspaces {
		w := &wf.Workspaces[i]
		where := fmt.Sprintf("workspace %d", i)
		if w.Name != "" {
			where = fmt.Sprintf("workspace %d (%s)", i, w.Name)
		}

		switch {
		case w.Path == "":
			w.Problem = "no path"
		case !filepath.IsAbs(w.Path):
			w.Problem = "path is not absolute"
		case !isDir(w.Path):
			w.Problem = "path is not a directory"
		}

		if w.Problem == "" {
			if prev, dup := seenName[w.Name]; dup {
				w.Problem = fmt.Sprintf("name already used by workspace %d", prev)
			} else {
				seenName[w.Name] = i
			}
		}
		if w.Problem == "" {
			key := filepath.Clean(w.Path) + "\x00" + w.TasksDir
			if prev, dup := seenPath[key]; dup {
				w.Problem = fmt.Sprintf("same backlog as workspace %d", prev)
			} else {
				seenPath[key] = i
			}
		}

		if w.Problem != "" {
			problems = append(problems, fmt.Sprintf("%s: %s; listed as unavailable", where, w.Problem))
		}
	}

	return problems
}

// reportProblems writes one line per finding to the same sink the Done-column
// card diagnostics use - stderr, never stdout: in stdio mode that is the
// JSON-RPC channel.
func (wf *WebFile) reportProblems() {
	name := WebFileName
	if wf.Path != "" {
		name = wf.Path
	}
	for _, p := range wf.Problems {
		fmt.Fprintf(statsWarnTo, "%s: %s\n", name, p)
	}
}

// Selectable returns the entries that can be opened, in file order.
func (wf *WebFile) Selectable() []Workspace {
	if wf == nil {
		return nil
	}
	out := make([]Workspace, 0, len(wf.Workspaces))
	for _, w := range wf.Workspaces {
		if w.Selectable() {
			out = append(out, w)
		}
	}
	return out
}

// expandHome expands a leading ~ to the user's home directory. Nothing else
// is expanded: no $VAR, no globs.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}
