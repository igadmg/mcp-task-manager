// Package webproc is the MCP process's view of the dashboard, which since the
// split is a separate program. It never imports internal/web: its only ways
// of knowing about a dashboard are os/exec, an HTTP probe and a file on disk.
// That is what keeps the split structural - cmd/mcp-task-manager has no path
// to the web server at all, and cmd/import_test.go fails if one appears.
package webproc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// MarkerName is the instance marker's file name. It lives inside the tasks
// directory's per-user state directory - .users/<user>/ - because that
// directory is already skipped by every task scan and is already a reserved
// task id, so the marker needs no new reserved name and can never be read as
// a task.
//
// Per user rather than per backlog, matching the current-task pointer beside
// it. Two users sharing one backlog therefore keep separate markers, which is
// harmless: the probe is what actually decides whether a dashboard is
// serving, and it finds the other user's dashboard on the address just the
// same.
const MarkerName = "web.json"

// LogName is where a spawned dashboard's output goes. The parent's own stderr
// is deliberately not inherited: the child outlives the parent, and writing
// to a closed pipe would kill it.
const LogName = "web.log"

// Marker is what a running dashboard records about itself.
//
// Addr and Base are the useful parts: Addr is what the probe asks, and Base
// is the session prefix that turns the listener's URL into a link to this
// backlog's board - the one thing the MCP side can no longer read out of
// shared memory.
//
// Pid is recorded for an operator reading the file by hand and is deliberately
// never used for a decision: a pid can be gone, reused by something
// unrelated, or alive while no longer holding the port, and none of those is
// the question "is a dashboard serving this backlog right now".
type Marker struct {
	Pid       int    `json:"pid"`
	Addr      string `json:"addr"`
	Base      string `json:"base"`
	TasksDir  string `json:"tasks_dir"`
	StartedAt string `json:"started_at"`
}

// MarkerPath is where user's marker for the backlog in tasksDir lives.
func MarkerPath(tasksDir, user string) string {
	return filepath.Join(tasksDir, usersDirName, user, MarkerName)
}

// LogPath is where a spawned dashboard for that backlog writes its output.
func LogPath(tasksDir, user string) string {
	return filepath.Join(tasksDir, usersDirName, user, LogName)
}

// usersDirName mirrors storage.UsersDirName. It is duplicated rather than
// imported so this package depends on nothing but the standard library: the
// name is part of the on-disk layout and is pinned by TestUsersDirNameMatchesStorage.
const usersDirName = ".users"

// ReadMarker reads user's marker. A missing, unreadable or corrupt marker is
// reported as absent, not as an error: the marker is a hint, and the probe is
// what decides. A marker naming another backlog is also absent - it is not
// about this one.
func ReadMarker(tasksDir, user string) (Marker, bool) {
	data, err := os.ReadFile(MarkerPath(tasksDir, user))
	if err != nil {
		return Marker{}, false
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, false
	}
	if m.Addr == "" {
		return Marker{}, false
	}
	if m.TasksDir != "" && m.TasksDir != tasksDir {
		return Marker{}, false
	}
	return m, true
}

// WriteMarker records a running dashboard. It is written by whoever knows the
// facts: the dashboard itself on startup, or the spawner when it adopts an
// address a dashboard was already serving.
func WriteMarker(tasksDir, user string, m Marker) error {
	if m.StartedAt == "" {
		m.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	m.TasksDir = tasksDir
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := MarkerPath(tasksDir, user)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// RemoveMarker clears user's marker. Removing an absent one is not an error.
// Only the process that wrote a marker removes it; nobody deletes another
// process's.
func RemoveMarker(tasksDir, user string) error {
	if err := os.Remove(MarkerPath(tasksDir, user)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeFileAtomic mirrors internal/storage's writer: a reader in another
// process sees either the old file or the new one, never a half-written one.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".web-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
