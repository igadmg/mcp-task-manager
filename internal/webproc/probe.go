package webproc

import (
	"context"
	"net/http"
	"time"
)

// probeTimeout bounds the liveness check. It is on the path of start_web_ui,
// which is a tool call a human is waiting on, and the dashboard it asks about
// is on loopback.
const probeTimeout = 500 * time.Millisecond

// ProbeResult is what asking an address tells us.
type ProbeResult int

const (
	// ProbeDead is nothing listening, or something that is not a dashboard.
	ProbeDead ProbeResult = iota
	// ProbeOurs is a dashboard serving the backlog we asked about.
	ProbeOurs
	// ProbeOther is a dashboard serving a *different* backlog. It holds the
	// port, so we cannot bind it - and adopting it would hand the caller a
	// link to someone else's board, so it is reported rather than reused.
	ProbeOther
)

// Probe asks whether a dashboard for tasksDir is serving on addr.
//
// This - not the marker, and not a pid - is what decides whether to spawn.
// A marker can be stale three ways (the process is gone, its pid was reused,
// or it is alive but no longer holds the port) and none of them answers the
// question actually being asked.
func Probe(ctx context.Context, addr, tasksDir string) (ProbeResult, string) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return ProbeDead, ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ProbeDead, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProbeDead, ""
	}
	// The header is what makes this identifying: a bare 200 on /healthz
	// could be anything, including another program entirely.
	served := resp.Header.Get(healthHeader)
	if served == "" {
		return ProbeDead, ""
	}
	if served != tasksDir {
		return ProbeOther, served
	}
	return ProbeOurs, served
}

// healthHeader mirrors web.HealthHeader, duplicated so this package imports
// nothing of internal/web. TestHealthHeaderMatchesWeb pins the two together.
const healthHeader = "X-Task-Dashboard"
