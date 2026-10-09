package webproc

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/web"
)

// This package deliberately imports nothing of internal/web or
// internal/config in its production code - these two constants are duplicated
// so the MCP binary links neither. These tests are where the copies are held
// to their originals.

func TestHealthHeaderMatchesWeb(t *testing.T) {
	if healthHeader != web.HealthHeader {
		t.Errorf("healthHeader = %q, want %q: the probe would stop recognizing a dashboard",
			healthHeader, web.HealthHeader)
	}
}

func TestEnvTasksDirMatchesConfig(t *testing.T) {
	if envTasksDir != config.EnvTasksDir {
		t.Errorf("envTasksDir = %q, want %q: the child would resolve a different backlog",
			envTasksDir, config.EnvTasksDir)
	}
}

func TestUsersDirNameMatchesStorage(t *testing.T) {
	if usersDirName != storage.UsersDirName {
		t.Errorf("usersDirName = %q, want %q: the marker would land where a scan can see it",
			usersDirName, storage.UsersDirName)
	}
}

// dashboardAt is a stand-in dashboard answering /healthz for one backlog.
func dashboardAt(t *testing.T, tasksDir string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if tasksDir != "" {
			w.Header().Set(healthHeader, tasksDir)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestProbeOutcomes(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	if got, _ := Probe(context.Background(), dashboardAt(t, dir), dir); got != ProbeOurs {
		t.Errorf("a dashboard for this backlog = %v, want ProbeOurs", got)
	}
	if got, served := Probe(context.Background(), dashboardAt(t, other), dir); got != ProbeOther || served != other {
		t.Errorf("a dashboard for another backlog = %v/%q, want ProbeOther/%q", got, served, other)
	}
	// 200 "ok" with no header: something is listening, but it has not
	// claimed to be a dashboard, so it is not treated as one.
	if got, _ := Probe(context.Background(), dashboardAt(t, ""), dir); got != ProbeDead {
		t.Errorf("an unidentified service = %v, want ProbeDead", got)
	}
	// Nothing listening at all.
	if got, _ := Probe(context.Background(), "127.0.0.1:1", dir); got != ProbeDead {
		t.Errorf("a closed port = %v, want ProbeDead", got)
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadMarker(dir, "dev"); ok {
		t.Error("an absent marker read as present")
	}

	want := Marker{Pid: 4242, Addr: "127.0.0.1:7777", Base: "/tok"}
	if err := WriteMarker(dir, "dev", want); err != nil {
		t.Fatalf("WriteMarker() error = %v", err)
	}
	got, ok := ReadMarker(dir, "dev")
	if !ok {
		t.Fatal("the marker just written reads as absent")
	}
	if got.Pid != want.Pid || got.Addr != want.Addr || got.Base != want.Base {
		t.Errorf("marker = %+v, want %+v", got, want)
	}
	if got.TasksDir != dir {
		t.Errorf("TasksDir = %q, want the backlog it was written for (%q)", got.TasksDir, dir)
	}
	if got.StartedAt == "" {
		t.Error("StartedAt was not stamped")
	}
	// It lives where no task scan will ever see it.
	if !strings.Contains(MarkerPath(dir, "dev"), filepath.Join(storage.UsersDirName, "dev")) {
		t.Errorf("MarkerPath = %q, want it under the per-user state directory", MarkerPath(dir, "dev"))
	}

	if err := RemoveMarker(dir, "dev"); err != nil {
		t.Fatalf("RemoveMarker() error = %v", err)
	}
	if err := RemoveMarker(dir, "dev"); err != nil {
		t.Errorf("removing an absent marker errored: %v", err)
	}
}

// TestMarkerCorruptReadsAsAbsent: the marker is a hint, and a hint that
// cannot be parsed must not break the tool that reads it.
func TestMarkerCorruptReadsAsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := MarkerPath(dir, "dev")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"", "not json", `{"addr": ""}`, `{"addr":"x","tasks_dir":"/somewhere/else"}`} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := ReadMarker(dir, "dev"); ok {
			t.Errorf("marker %q read as present", content)
		}
	}
}

// TestStartAdoptsARunningDashboard is the idempotency rule: a dashboard that
// is already serving this backlog is reported, not restarted - and nothing is
// spawned, which the test proves by pointing the locator at a binary that
// would fail if it ever ran.
func TestStartAdoptsARunningDashboard(t *testing.T) {
	dir := t.TempDir()
	addr := dashboardAt(t, dir)

	s := &Spawner{
		TasksDir: dir,
		User:     "dev",
		Addr:     addr,
		Logger:   log.New(os.Stderr, "", 0),
		Locate:   func() (string, error) { return "", errSpawned },
	}

	url, already, err := s.Start("")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !already {
		t.Error("already = false for a dashboard that was already serving")
	}
	if url != "http://"+addr {
		t.Errorf("url = %q, want http://%s", url, addr)
	}
	// Adopting records the address, so the next call needs one probe.
	if m, ok := ReadMarker(dir, "dev"); !ok || m.Addr != addr {
		t.Errorf("adopting did not record the address: %+v", m)
	}

	// And URL() finds it without remembering anything, which the old
	// in-process controller could not do across a restart.
	if got, ok := s.URL(); !ok || got != "http://"+addr {
		t.Errorf("URL() = %q, %v; want http://%s", got, ok, addr)
	}
}

// TestStartRefusesAnotherBacklogsDashboard: adopting it would hand the caller
// a link to someone else's board, and binding the port is impossible anyway.
func TestStartRefusesAnotherBacklogsDashboard(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	addr := dashboardAt(t, other)

	s := &Spawner{TasksDir: dir, User: "dev", Addr: addr,
		Locate: func() (string, error) { return "", errSpawned }}

	if _, _, err := s.Start(""); err == nil {
		t.Fatal("Start() adopted a dashboard serving a different backlog")
	} else if !strings.Contains(err.Error(), other) {
		t.Errorf("error = %v, want it to name the backlog actually being served", err)
	}
}

// TestStartWithNoBacklogRefuses: before the first tool call there is nothing
// to serve, and guessing a backlog would be worse than saying so.
func TestStartWithNoBacklogRefuses(t *testing.T) {
	s := &Spawner{User: "dev", Addr: "127.0.0.1:1"}
	if _, _, err := s.Start(""); err == nil {
		t.Error("Start() with no resolved backlog returned no error")
	}
	if _, ok := s.URL(); ok {
		t.Error("URL() claims a dashboard with no backlog resolved")
	}
}

// TestStartOverwritesAStaleMarker covers all three stale shapes at once: the
// marker names an address nothing answers on, whatever the reason.
func TestStartOverwritesAStaleMarker(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMarker(dir, "dev", Marker{Pid: 999999, Addr: "127.0.0.1:1", Base: "/old"}); err != nil {
		t.Fatal(err)
	}
	live := dashboardAt(t, dir)

	s := &Spawner{TasksDir: dir, User: "dev", Addr: live,
		Locate: func() (string, error) { return "", errSpawned }}

	url, already, err := s.Start("")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !already || url != "http://"+live {
		t.Errorf("Start() = %q, %v; want the live dashboard", url, already)
	}
	if m, _ := ReadMarker(dir, "dev"); m.Addr != live {
		t.Errorf("the stale marker was kept: %+v", m)
	}
}

func TestSessionPathComesFromTheMarker(t *testing.T) {
	dir := t.TempDir()
	s := &Spawner{TasksDir: dir, User: "dev"}

	if _, ok := s.SessionPath(dir); ok {
		t.Error("SessionPath() found a path with no marker")
	}
	if err := WriteMarker(dir, "dev", Marker{Addr: "127.0.0.1:7777"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.SessionPath(dir); ok {
		t.Error("SessionPath() returned a path for a marker with no base")
	}
	if err := WriteMarker(dir, "dev", Marker{Addr: "127.0.0.1:7777", Base: "/tok"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.SessionPath(dir); !ok || got != "/tok" {
		t.Errorf("SessionPath() = %q, %v; want /tok", got, ok)
	}
}

func TestLocateOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture names a unix binary")
	}
	// A sibling of the test binary cannot be arranged, so this covers the
	// two lookups a test can control: the override and PATH.
	dir := t.TempDir()
	onPath := filepath.Join(dir, BinaryName)
	if err := os.WriteFile(onPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := Locate()
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}
	if got != onPath {
		t.Errorf("Locate() = %q, want the one on PATH (%q)", got, onPath)
	}

	// The override wins over PATH.
	override := filepath.Join(t.TempDir(), BinaryName)
	if err := os.WriteFile(override, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvBinary, override)
	if got, err := Locate(); err != nil || got != override {
		t.Errorf("Locate() = %q, %v; want the override %q", got, err, override)
	}

	// An override naming nothing falls through rather than failing, and the
	// error names everywhere it looked.
	t.Setenv(EnvBinary, filepath.Join(t.TempDir(), "absent"))
	t.Setenv("PATH", t.TempDir())
	_, err = Locate()
	if err == nil {
		t.Fatal("Locate() found a binary that is not there")
	}
	for _, want := range []string{EnvBinary, "PATH", "go install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q in it", err, want)
		}
	}
}

// errSpawned is returned by a locator that must never be called: if a test
// sees it, the spawner tried to start a dashboard when it should have adopted
// a running one.
var errSpawned = errSentinel("the spawner tried to start a dashboard")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
