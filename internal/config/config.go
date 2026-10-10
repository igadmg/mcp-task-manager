package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Environment variables understood when resolving the project root.
const (
	// EnvTasksDir points directly at the tasks directory. An absolute value
	// is used verbatim; a relative one is resolved against the project root.
	EnvTasksDir = "MCP_TASKS_DIR"
	// EnvProjectDir is an explicit project root override.
	EnvProjectDir = "MCP_PROJECT_DIR"
	// EnvClaudeProjectDir is exported by Claude Code into the environment of
	// every MCP server it spawns.
	EnvClaudeProjectDir = "CLAUDE_PROJECT_DIR"
	// EnvRootSource forces a single resolution source, skipping the others.
	// Mainly a testing lever: in Claude Code the CLAUDE_PROJECT_DIR step
	// always wins, so the roots step would otherwise never execute.
	EnvRootSource = "MCP_ROOT_SOURCE"
	// EnvWebEnabled turns the web dashboard on or off, parsed as a bool.
	EnvWebEnabled = "MCP_WEB_ENABLED"
	// EnvWebAddr sets the dashboard listen address. It never enables the
	// dashboard on its own - the two levers stay orthogonal.
	EnvWebAddr = "MCP_WEB_ADDR"
	// EnvGitBranching turns the git branch-per-task workflow on or off,
	// parsed as a bool. There is no override for the base branch list.
	EnvGitBranching = "MCP_GIT_BRANCHING"
)

// DefaultWebAddr is loopback-only on purpose: the dashboard is unauthenticated,
// so reaching it from another machine has to be the operator's explicit choice.
const DefaultWebAddr = "127.0.0.1:7777"

// DefaultSessionHostAddr is where the session host listens by default
// (design §6); it is loopback-only by the same reasoning as DefaultWebAddr.
const DefaultSessionHostAddr = "127.0.0.1:7778"

// Well-known names inside a project root.
const (
	ConfigFileName      = "mcp-tasks.yaml"
	DefaultTasksDirName = ".tasks"
	LegacyTasksDirName  = "tasks"
)

// Source identifies where the project root was resolved from.
type Source string

const (
	SourceTasksDirEnv Source = "MCP_TASKS_DIR"
	SourceProjectEnv  Source = "MCP_PROJECT_DIR"
	SourceClaudeEnv   Source = "CLAUDE_PROJECT_DIR"
	SourceRoots       Source = "roots"
	SourceCwd         Source = "cwd"
	SourceFallback    Source = "cwd-fallback"
	// SourceWorkspace is a project named by the web server's own workspace
	// list (web.yaml), not found by any of the root-resolution steps above.
	// It is a different axis from the web session's own source: this says
	// how the tasks directory was arrived at, that says who opened it.
	SourceWorkspace Source = "workspace"
)

// RootsProvider returns filesystem paths advertised by the MCP client through
// the protocol's roots/list request. It is nil whenever roots are unavailable
// (CLI mode, or a client that does not declare the roots capability).
type RootsProvider func() ([]string, error)

// Resolution records how the project root and tasks directory were picked.
// Attempts holds the sources that were tried and rejected, in order, so a
// failed or surprising resolution can be explained instead of silently
// degrading to an empty backlog.
type Resolution struct {
	Root     string
	TasksDir string
	Source   Source
	Attempts []string
}

// String renders the resolution for diagnostics.
func (r *Resolution) String() string {
	if r == nil {
		return "unresolved"
	}
	return fmt.Sprintf("root=%s tasks=%s source=%s", r.Root, r.TasksDir, r.Source)
}

// Explain renders the resolution together with the rejected sources.
func (r *Resolution) Explain() string {
	if r == nil {
		return "unresolved"
	}
	if len(r.Attempts) == 0 {
		return r.String()
	}
	return r.String() + " (tried: " + strings.Join(r.Attempts, "; ") + ")"
}

// AutoArchiveConfig holds configuration for automatic task archiving
type AutoArchiveConfig struct {
	Enabled   bool `yaml:"enabled"`
	AfterDays int  `yaml:"after_days"`
}

// WebSessionsConfig holds the opt-in interactive AI session settings
// (design §7). Everything is off unless explicitly enabled.
type WebSessionsConfig struct {
	// Enabled registers the session routes and shows the UI links.
	Enabled bool `yaml:"enabled"`
	// HostAddr is the session host's loopback address, host:port.
	HostAddr string `yaml:"host_addr"`
	// TokenFile is the host.token file the host wrote; the web server reads
	// the bearer token from it. The token never reaches the browser.
	TokenFile string `yaml:"token_file"`
}

// Validate enforces the loopback-only rule for the session host address. It
// runs only when sessions are enabled: a disabled section's address is never
// used, so it is not validated either.
func (s WebSessionsConfig) Validate() error {
	if !s.Enabled {
		return nil
	}
	host, _, err := net.SplitHostPort(s.HostAddr)
	if err != nil {
		return fmt.Errorf("web.sessions.host_addr %q: %w", s.HostAddr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("web.sessions.host_addr %q: the session host must be reached over loopback only", s.HostAddr)
	}
	return nil
}

// WebConfig holds the read-only web dashboard settings.
type WebConfig struct {
	// Enabled starts the dashboard alongside the MCP server.
	Enabled bool `yaml:"enabled"`
	// Addr is the listen address, host:port.
	Addr string `yaml:"addr"`
	// Sessions is the opt-in interactive AI session surface.
	Sessions WebSessionsConfig `yaml:"sessions"`
	// DoneStats defines the statistics cards of the board's Done column.
	DoneStats DoneStatsConfig `yaml:"done_stats"`
	// Colors is the status palette shared by cards, bars and the graph.
	Colors ColorsConfig `yaml:"colors"`
	// NewHours is the board's "new" window: a todo task created inside it
	// carries the new marker on its card, and a bars card that names no
	// new_hours of its own highlights the same arrivals.
	NewHours int `yaml:"new_hours"`
}

// GitConfig holds the opt-in git branch-per-task workflow.
type GitConfig struct {
	// Branching makes start_task/complete_task create, squash, rebase and
	// switch per-task branches in the code repository. Off by default.
	Branching bool `yaml:"branching"`
	// BaseBranches is the priority list of mainline branches; a top-level
	// task branches from the first one that exists. _patched variants rank
	// above plain ones.
	BaseBranches []string `yaml:"base_branches"`
}

// Config holds application configuration
type Config struct {
	TaskTypes     []string          `yaml:"task_types"`
	RelationTypes []string          `yaml:"relation_types,omitempty"`
	AutoArchive   AutoArchiveConfig `yaml:"auto_archive"`
	Web           WebConfig         `yaml:"web"`
	Git           GitConfig         `yaml:"git"`
	// TasksDirName is the tasks directory, relative to the project root.
	TasksDirName string      `yaml:"tasks_dir,omitempty"`
	DataDir      string      `yaml:"-"` // Resolved tasks directory
	ProjectFound bool        `yaml:"-"` // Whether an existing tasks directory was found
	Resolution   *Resolution `yaml:"-"` // How DataDir was arrived at
}

// DefaultRelationTypes returns the default relation types
// superseded_by is the edge behind the "superseded" resolution: the task that
// took over the work, kept as a link rather than a copy of an id in a field.
var DefaultRelationTypes = []string{"blocked_by", "relates_to", "duplicate_of", "superseded_by"}

// DefaultBaseBranches is the base branch priority list used when the config
// names none.
var DefaultBaseBranches = []string{"main_patched", "master_patched", "main", "master"}

// DefaultConfig returns configuration with defaults
func DefaultConfig() *Config {
	return &Config{
		TaskTypes:     []string{"feature", "bug"},
		RelationTypes: DefaultRelationTypes,
		DataDir:       "./tasks",
		AutoArchive: AutoArchiveConfig{
			Enabled:   false,
			AfterDays: 30,
		},
		Web: WebConfig{
			Enabled:  false,
			Addr:     DefaultWebAddr,
			NewHours: DefaultStatsHours,
			Sessions: WebSessionsConfig{
				HostAddr: DefaultSessionHostAddr,
			},
			// A fresh list, like BaseBranches below.
			DoneStats: DoneStatsConfig{Cards: DefaultStatsCards()},
		},
		Git: GitConfig{
			Branching: false,
			// A copy, so a caller mutating its config cannot change the
			// package default.
			BaseBranches: append([]string(nil), DefaultBaseBranches...),
		},
	}
}

// Load resolves configuration without access to MCP roots. It is the entry
// point for CLI mode; the server uses Resolve so that roots can participate.
func Load() (*Config, error) {
	return Resolve(nil)
}

// Resolve determines the project root and tasks directory, then loads
// mcp-tasks.yaml from the project root.
//
// Project root, in order: MCP_PROJECT_DIR, CLAUDE_PROJECT_DIR, MCP roots,
// a marker search upwards from the working directory. An absolute
// MCP_TASKS_DIR short-circuits the whole search.
func Resolve(roots RootsProvider) (*Config, error) {
	cfg := DefaultConfig()
	res := &Resolution{}

	var relTasksDir string
	if env := strings.TrimSpace(os.Getenv(EnvTasksDir)); env != "" {
		if filepath.IsAbs(env) {
			res.TasksDir = filepath.Clean(env)
			res.Root = filepath.Dir(res.TasksDir)
			res.Source = SourceTasksDirEnv
		} else {
			relTasksDir = env
		}
	}

	if res.Root == "" {
		root, source, attempts := resolveRoot(roots)
		res.Root, res.Source, res.Attempts = root, source, attempts
	}

	// The project config lives in the project root, not next to the tasks
	// directory.
	if data, err := os.ReadFile(filepath.Join(res.Root, ConfigFileName)); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s in %s: %w", ConfigFileName, res.Root, err)
		}
	}
	cfg.applyDefaults()
	cfg.applyEnvOverrides()
	reportStatsProblems(cfg)
	reportColorProblems(cfg)

	if res.TasksDir == "" {
		name := relTasksDir
		if name == "" {
			name = strings.TrimSpace(cfg.TasksDirName)
		}
		if name == "" {
			name = pickTasksDirName(res.Root)
		}
		if filepath.IsAbs(name) {
			res.TasksDir = filepath.Clean(name)
		} else {
			res.TasksDir = filepath.Join(res.Root, name)
		}
		if relTasksDir != "" {
			res.Source = SourceTasksDirEnv
		}
	}

	cfg.DataDir = res.TasksDir
	cfg.ProjectFound = isDir(res.TasksDir)
	cfg.Resolution = res
	return cfg, nil
}

// LoadForRoot loads the project rooted at root, without consulting the
// environment or the working directory.
//
// It is how the web server opens a workspace from its own list: the
// environment overrides (MCP_TASKS_DIR and friends) describe the process's
// own project, not an arbitrary directory an operator listed, so applying
// them here would make one workspace entry silently shadow another.
//
// tasksDirOverride wins over the project's own tasks_dir; when both are
// empty the tasks directory is picked exactly as Resolve picks it.
func LoadForRoot(root, tasksDirOverride string) (*Config, error) {
	root = absPath(filepath.Clean(root))
	cfg := DefaultConfig()

	if data, err := os.ReadFile(filepath.Join(root, ConfigFileName)); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s in %s: %w", ConfigFileName, root, err)
		}
	}
	cfg.applyDefaults()
	reportStatsProblems(cfg)
	reportColorProblems(cfg)

	name := strings.TrimSpace(tasksDirOverride)
	if name == "" {
		name = strings.TrimSpace(cfg.TasksDirName)
	}
	if name == "" {
		name = pickTasksDirName(root)
	}
	tasksDir := name
	if !filepath.IsAbs(tasksDir) {
		tasksDir = filepath.Join(root, name)
	}
	tasksDir = filepath.Clean(tasksDir)

	cfg.DataDir = tasksDir
	cfg.ProjectFound = isDir(tasksDir)
	cfg.Resolution = &Resolution{Root: root, TasksDir: tasksDir, Source: SourceWorkspace}
	return cfg, nil
}

// applyDefaults fills in what a partially written section left out.
// Unmarshalling into an already-defaulted struct only protects keys the file
// does not mention at all: writing `auto_archive: {enabled: true}` zeroes
// after_days, and `web: {enabled: true}` zeroes the address.
//
// Booleans are deliberately left as written - false is a real value there.
// The Done-column cards are refilled when `cards:` reset them to nil, and
// each written card gets its per-card defaults (see normalizeStatsCards). An
// invalid card is kept as written and reported, never rejected.
func (c *Config) applyDefaults() {
	d := DefaultConfig()
	if len(c.TaskTypes) == 0 {
		c.TaskTypes = d.TaskTypes
	}
	if len(c.RelationTypes) == 0 {
		c.RelationTypes = d.RelationTypes
	}
	if c.AutoArchive.AfterDays <= 0 {
		c.AutoArchive.AfterDays = d.AutoArchive.AfterDays
	}
	if strings.TrimSpace(c.Web.Addr) == "" {
		c.Web.Addr = d.Web.Addr
	}
	if strings.TrimSpace(c.Web.Sessions.HostAddr) == "" {
		c.Web.Sessions.HostAddr = d.Web.Sessions.HostAddr
	}
	// Validate before normalizing: normalization repairs some of what the
	// diagnostics report. Problems are never fatal and never change Cards.
	c.Web.DoneStats.Problems = ValidateStatsCards(c.Web.DoneStats.Cards)
	c.Web.Colors.Problems = ValidateColors(c.Web.Colors.Roles, c.Web.NewHours)
	if c.Web.NewHours <= 0 {
		c.Web.NewHours = DefaultStatsHours
	}
	cards := c.Web.DoneStats.Cards
	if reflect.DeepEqual(cards, DefaultStatsCards()) {
		// Untouched defaults (or cards written to be identical, which is
		// indistinguishable): rebuild them on the board window, so a
		// web.new_hours reaches the default cards too.
		cards = nil
	}
	c.Web.DoneStats.Cards = normalizeStatsCards(cards, c.Web.NewHours)
	bases := trimList(c.Git.BaseBranches)
	if len(bases) == 0 {
		bases = d.Git.BaseBranches
	}
	c.Git.BaseBranches = bases
}

// applyEnvOverrides lets the environment override the web section and the
// git branching switch. The two web variables are orthogonal: an address
// never enables the dashboard. An unparseable MCP_WEB_ENABLED or
// MCP_GIT_BRANCHING is ignored rather than guessed at.
func (c *Config) applyEnvOverrides() {
	if addr := strings.TrimSpace(os.Getenv(EnvWebAddr)); addr != "" {
		c.Web.Addr = addr
	}
	envBool(EnvWebEnabled, &c.Web.Enabled)
	envBool(EnvGitBranching, &c.Git.Branching)
}

// envBool sets *dst from the bool environment variable name, leaving it
// alone when the variable is unset or unparseable.
func envBool(name string, dst *bool) {
	if v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(name))); err == nil {
		*dst = v
	}
}

// resolveRoot walks the resolution sources in priority order. It always
// returns a usable directory: the working directory is the last resort.
func resolveRoot(roots RootsProvider) (string, Source, []string) {
	var attempts []string
	forced := Source(strings.TrimSpace(os.Getenv(EnvRootSource)))
	enabled := func(s Source) bool { return forced == "" || forced == s }

	for _, env := range []struct {
		name   string
		source Source
	}{
		{EnvProjectDir, SourceProjectEnv},
		{EnvClaudeProjectDir, SourceClaudeEnv},
	} {
		if !enabled(env.source) {
			continue
		}
		dir := strings.TrimSpace(os.Getenv(env.name))
		if dir == "" {
			attempts = append(attempts, env.name+" unset")
			continue
		}
		if !isDir(dir) {
			attempts = append(attempts, fmt.Sprintf("%s=%q is not a directory", env.name, dir))
			continue
		}
		return absPath(dir), env.source, attempts
	}

	if enabled(SourceRoots) {
		switch {
		case roots == nil:
			attempts = append(attempts, "roots unavailable")
		default:
			paths, err := roots()
			switch {
			case err != nil:
				attempts = append(attempts, "roots/list failed: "+err.Error())
			case len(paths) == 0:
				attempts = append(attempts, "roots/list returned no usable paths")
			default:
				if root := pickRoot(paths); root != "" {
					return absPath(root), SourceRoots, attempts
				}
				attempts = append(attempts, "roots/list returned no existing directory")
			}
		}
	}

	if enabled(SourceCwd) {
		root, err := FindProjectRoot()
		if err != nil {
			attempts = append(attempts, "working directory unavailable: "+err.Error())
		} else if root != "" {
			return root, SourceCwd, attempts
		} else {
			attempts = append(attempts, "no project marker found above the working directory")
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return cwd, SourceFallback, attempts
}

// pickRoot chooses between several client-advertised roots: one that already
// holds a project wins over one that merely exists.
func pickRoot(paths []string) string {
	for _, marker := range []string{ConfigFileName, DefaultTasksDirName, LegacyTasksDirName} {
		for _, p := range paths {
			if !isDir(p) {
				continue
			}
			if _, err := os.Stat(filepath.Join(p, marker)); err == nil {
				return p
			}
		}
	}
	for _, p := range paths {
		if isDir(p) {
			return p
		}
	}
	return ""
}

// pickTasksDirName returns the tasks directory name to use when nothing is
// configured. A directory that actually holds tasks wins over one that merely
// exists: a project can have a .tasks directory used for something else
// entirely while its real backlog still sits in the legacy tasks/.
func pickTasksDirName(root string) string {
	candidates := []string{DefaultTasksDirName, LegacyTasksDirName}
	for _, name := range candidates {
		if holdsTasks(filepath.Join(root, name)) {
			return name
		}
	}
	for _, name := range candidates {
		if isDir(filepath.Join(root, name)) {
			return name
		}
	}
	return DefaultTasksDirName
}

// holdsTasks reports whether dir looks like a backlog: an archive directory,
// a per-task directory holding its own {id}.md record, or a legacy flat
// {id}.md file awaiting migration.
func holdsTasks(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			if strings.HasSuffix(name, ".md") {
				return true
			}
			continue
		}
		if name == "archive" {
			return true
		}
		if _, err := os.Stat(filepath.Join(dir, name, name+".md")); err == nil {
			return true
		}
	}
	return false
}

// TasksDir returns the full path to the tasks directory
func (c *Config) TasksDir() string {
	if filepath.IsAbs(c.DataDir) {
		return c.DataDir
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, c.DataDir)
}

// IsValidTaskType checks if task type is in configured list
func (c *Config) IsValidTaskType(t string) bool {
	for _, valid := range c.TaskTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// IsValidRelationType checks if relation type is in configured list
func (c *Config) IsValidRelationType(t string) bool {
	for _, valid := range c.RelationTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// FindProjectRoot searches for an existing project root by looking for
// mcp-tasks.yaml, a .tasks directory or a legacy tasks directory, starting
// from cwd and moving up. Returns the directory containing the marker, or an
// empty string if none was found.
func FindProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		for _, marker := range []string{ConfigFileName, DefaultTasksDirName, LegacyTasksDirName} {
			path := filepath.Join(dir, marker)
			if marker == ConfigFileName {
				if _, err := os.Stat(path); err == nil {
					return dir, nil
				}
				continue
			}
			if isDir(path) {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", nil
		}
		dir = parent
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
