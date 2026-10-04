package web

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// TestHandlerRaceAgainstWrites is the end-to-end version of the concurrency
// contract: HTTP readers and MCP-style writers share one *task.Service, which
// is exactly what happens when the dashboard runs inside the MCP server.
func TestHandlerRaceAgainstWrites(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	seedBoard(t, svc)

	srv := httptest.NewServer(NewHandler(Deps{
		Project: rs.Current,
		Logger:  log.New(io.Discard, "", 0),
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

	run(func(int) { fetch("/") })
	run(func(int) { fetch("/board") })
	run(func(int) { fetch("/tasks/1/panel") })
	run(func(int) { fetch("/tasks/5") })

	run(func(i int) {
		svc.Create(fmt.Sprintf("created %d", i), "", task.PriorityMedium, "feature", "", "")
	})
	run(func(i int) {
		title := fmt.Sprintf("renamed %d", i)
		svc.Update("1", &title, nil, nil, nil, nil)
	})
	// Task 3 is in progress, so /board lists its files while they change.
	names := []string{"research", "design", "plan"}
	run(func(i int) { _ = svc.WriteTaskFile("3", names[i%len(names)], "x") })
	// Phase records are read for in-progress cards and the detail view.
	run(func(int) { _, _, _ = svc.StartPhase("3", task.PhaseResearch) })
	run(func(int) { _, _, _ = svc.FinishPhase("3", task.PhaseResearch, task.PhaseFinish{}) })
	run(func(int) { fetch("/tasks/3") })

	wg.Wait()
}
