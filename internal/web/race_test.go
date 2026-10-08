package web

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// TestHandlerRaceAgainstWrites is the end-to-end version of the concurrency
// contract: HTTP readers and MCP-style writers share one *task.Service, which
// is exactly what happens when the dashboard runs inside the MCP server.
func TestHandlerRaceAgainstWrites(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	seedBoard(t, svc)
	rs2, svc2, _ := testsupport.NewBacklog(t)
	seedBoard(t, svc2)

	sessions := newTestSessions(t)
	one := adoptBacklog(t, sessions, rs)
	two := adoptBacklog(t, sessions, rs2)

	srv := httptest.NewServer(NewHandler(Deps{
		Sessions: sessions,
		Logger:   log.New(io.Discard, "", 0),
	}))
	defer srv.Close()

	const iterations = 40
	var wg sync.WaitGroup

	run := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				fn(i)
			}
		}()
	}

	fetch := func(path string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Errorf("GET %s error = %v", path, err)
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// Both sessions are read concurrently, and the welcome page reads the
	// registry while a session is being adopted.
	for _, s := range []*Session{one, two} {
		base := s.Base()
		run(func(int) { fetch(base + "/") })
		run(func(int) { fetch(base + "/board") })
		run(func(int) { fetch(base + "/tasks/1/panel") })
		run(func(int) { fetch(base + "/tasks/5") })
		run(func(int) { fetch(base + "/tasks/3") })
		// A workspace chain reads the board, a task and a file in one
		// request, while the writers below are changing all three.
		run(func(int) { fetch(base + "/tasks/3/w/f/research") })
		run(func(int) { fetch(base + "/strip/tasks/3/w/t/2") })
	}
	run(func(int) { fetch("/") })
	run(func(int) { fetch("/nosuchtoken/board") })

	for i, s := range []*task.Service{svc, svc2} {
		s := s
		suffix := i
		run(func(i int) {
			s.Create(fmt.Sprintf("created %d-%d", suffix, i), "", task.PriorityMedium, "feature", "", "")
		})
		run(func(i int) {
			title := fmt.Sprintf("renamed %d-%d", suffix, i)
			s.Update("1", &title, nil, nil, nil, nil)
		})
		// Task 3 is in progress, so /board lists its files while they change.
		names := []string{"research", "design", "plan"}
		run(func(i int) { _ = s.WriteTaskFile("3", names[i%len(names)], "x") })
		// Phase records are read for in-progress cards and the detail view.
		run(func(int) { _, _, _ = s.StartPhase("3", task.PhaseResearch) })
		run(func(int) { _, _, _ = s.FinishPhase("3", task.PhaseResearch, task.PhaseFinish{}) })
	}

	// Re-adopting is what the resolver does when its roots change, and it
	// swaps a live session's project under the readers above.
	run(func(int) { adoptAgain(t, sessions, rs) })

	wg.Wait()
}

// adoptAgain re-registers an already-registered project, the way
// Resolver.OnResolve does after an Invalidate.
func adoptAgain(t *testing.T, s *Sessions, rs *project.Resolver) {
	resolved, ok := rs.Current()
	if !ok {
		return
	}
	if _, err := s.Adopt(resolved); err != nil {
		t.Errorf("Adopt() error = %v", err)
	}
}
