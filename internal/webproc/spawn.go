package webproc

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// spawnTimeout is how long the parent waits for a freshly started dashboard to
// record its marker. It only bounds the *reporting*: if the marker has not
// appeared by then the dashboard is probably still binding, and the caller
// gets the listener URL without a session prefix rather than an error.
const spawnTimeout = 2 * time.Second

// markerPoll is how often the parent looks for the child's marker while
// waiting. The child writes it right after binding.
const markerPoll = 25 * time.Millisecond

// Spawner starts the dashboard as its own process and reports where it is.
//
// It implements tools.WebStarter, which is the interface start_web_ui has
// always called - the tool does not change at all, only what satisfies it.
// Where the old *web.Controller bound a listener inside this process and
// remembered it in a field, this one runs another program and remembers
// nothing: a dashboard it started yesterday is still found, and a dashboard
// somebody else started by hand is found too.
type Spawner struct {
	// TasksDir is the backlog to serve, absolute. It is passed to the child
	// as MCP_TASKS_DIR, which wins resolution outright - the child cannot
	// repeat this one's resolution, since the MCP roots step needs a client
	// session it does not have.
	TasksDir string
	// User names the per-user state directory the marker lives in.
	User string
	// Addr is the configured default listen address.
	Addr string
	// Logger receives what the parent itself has to say. Never stdout: in
	// stdio mode that is the JSON-RPC channel.
	Logger *log.Logger
	// Now and Locate are injected by tests; both default sensibly.
	Locate func() (string, error)
}

func (s *Spawner) locate() (string, error) {
	if s.Locate != nil {
		return s.Locate()
	}
	return Locate()
}

func (s *Spawner) logf(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, args...)
	}
}

// Start makes sure a dashboard is serving this backlog, and reports its URL.
//
// Probe first, marker second. The marker only supplies an address to try and
// the session prefix to report; the probe is what decides, because it is the
// only check that answers "is a dashboard serving this backlog right now"
// rather than something about a pid.
func (s *Spawner) Start(addr string) (url string, already bool, err error) {
	if s.TasksDir == "" {
		return "", false, fmt.Errorf("no backlog resolved yet, so there is nothing to serve")
	}
	if addr == "" {
		addr = s.Addr
	}
	ctx := context.Background()

	// 1. The address a dashboard last recorded for this backlog.
	if m, ok := ReadMarker(s.TasksDir, s.User); ok {
		switch result, _ := Probe(ctx, m.Addr, s.TasksDir); result {
		case ProbeOurs:
			return "http://" + m.Addr, true, nil
		}
		// Stale in any of its three ways: the process is gone, its pid
		// was reused, or it no longer holds the port. One rule covers
		// all of them - the marker is wrong, so fall through and start.
		s.logf("the recorded dashboard at %s is not answering; starting a new one", m.Addr)
	}

	// 2. The configured address, which a dashboard started by hand or by a
	// supervisor may already be holding.
	switch result, served := Probe(ctx, addr, s.TasksDir); result {
	case ProbeOurs:
		// Adopt it: record what we found so the next call is one probe,
		// and so the reported link has this session's prefix.
		s.adopt(addr)
		return "http://" + addr, true, nil
	case ProbeOther:
		return "", false, fmt.Errorf(
			"a task dashboard for a different backlog is already serving %s (%s); "+
				"stop it or set a different web.addr for this project", addr, served)
	}

	// 3. Nothing is there. Start one.
	return s.spawn(addr)
}

// adopt records an address a dashboard was already serving. The base is left
// empty: this process cannot know the other one's session token, so the link
// degrades to the workspace list, which is the fallback the tool has always
// had for a miss.
func (s *Spawner) adopt(addr string) {
	if err := WriteMarker(s.TasksDir, s.User, Marker{Addr: addr}); err != nil {
		s.logf("could not record the running dashboard: %v", err)
	}
}

func (s *Spawner) spawn(addr string) (string, bool, error) {
	bin, err := s.locate()
	if err != nil {
		return "", false, err
	}

	logPath := LogPath(s.TasksDir, s.User)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", false, fmt.Errorf("prepare the dashboard's state directory: %w", err)
	}
	// Appended, never truncated: a dashboard that failed to bind yesterday
	// should still be explainable today. Nothing rotates it.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", false, fmt.Errorf("open the dashboard log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin, "--addr", addr)
	cmd.Env = append(os.Environ(), envTasksDir+"="+s.TasksDir)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = s.TasksDir
	detach(cmd)

	if err := cmd.Start(); err != nil {
		return "", false, fmt.Errorf("start %s: %w", bin, err)
	}
	// Release instead of Wait: the child is meant to outlive this process,
	// and nothing here will ever reap it.
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		s.logf("could not detach from the dashboard process: %v", err)
	}
	s.logf("dashboard started as pid %d, logging to %s", pid, logPath)

	// Wait for the child to record itself, so the URL we report carries its
	// session prefix and lands on the board rather than the workspace list.
	deadline := time.Now().Add(spawnTimeout)
	for time.Now().Before(deadline) {
		if m, ok := ReadMarker(s.TasksDir, s.User); ok && m.Pid == pid {
			return "http://" + m.Addr, false, nil
		}
		time.Sleep(markerPoll)
	}
	// It may still be coming up. The address is the one we asked for, so
	// the URL is right even if the prefix is not known yet.
	s.logf("the dashboard has not recorded itself within %s; see %s", spawnTimeout, logPath)
	return "http://" + addr, false, nil
}

// URL reports a dashboard serving this backlog, if one is. Unlike the old
// in-process controller it does not remember anything: it reads the marker and
// confirms it with a probe, so a dashboard from a previous run of this process
// is found and a dead one is not reported as alive.
func (s *Spawner) URL() (string, bool) {
	if s.TasksDir == "" {
		return "", false
	}
	m, ok := ReadMarker(s.TasksDir, s.User)
	if !ok {
		return "", false
	}
	if result, _ := Probe(context.Background(), m.Addr, s.TasksDir); result != ProbeOurs {
		return "", false
	}
	return "http://" + m.Addr, true
}

// SessionPath is the path, under the dashboard's base URL, that serves this
// backlog's board. It comes from the marker, because the token lives in the
// other process's memory; an empty or unknown base falls back to "/", the
// workspace list, which is the documented fallback and one click from the
// board.
func (s *Spawner) SessionPath(tasksDir string) (string, bool) {
	m, ok := ReadMarker(tasksDir, s.User)
	if !ok || m.Base == "" {
		return "", false
	}
	return m.Base, true
}

// envTasksDir mirrors config.EnvTasksDir, duplicated so this package stays on
// the standard library. TestEnvTasksDirMatchesConfig pins the two together.
const envTasksDir = "MCP_TASKS_DIR"
