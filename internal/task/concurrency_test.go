package task

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// newConcurrentService builds a service over this package's in-package mocks.
// The mocks are plain maps with no locking of their own, which is the point:
// anything the service mutex fails to cover shows up under -race.
//
// internal/testsupport cannot be used here - it imports this package.
func newConcurrentService() *Service {
	st := newMockStorage()
	cfg := &config.Config{
		TaskTypes:     []string{"feature", "bug"},
		RelationTypes: config.DefaultRelationTypes,
		ProjectFound:  true,
		AutoArchive:   config.AutoArchiveConfig{AfterDays: 30},
	}
	return NewService(st, newMockArchiveStorage(st), newMockFileStorage(), newMockIndex(), cfg.TaskTypes, cfg)
}

// seedBacklog fills the service with parents, subtasks and blocked_by edges.
func seedBacklog(t *testing.T, svc *Service) []string {
	t.Helper()
	priorities := []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}

	var ids []string
	for i := 0; i < 10; i++ {
		parent, err := svc.Create(fmt.Sprintf("parent %d", i), "body", priorities[i%len(priorities)], "feature", "", fmt.Sprintf("p%d", i))
		if err != nil {
			t.Fatalf("Create(parent) error = %v", err)
		}
		ids = append(ids, parent.ID)
		for j := 0; j < 2; j++ {
			sub, err := svc.Create(fmt.Sprintf("sub %d.%d", i, j), "body", priorities[j%len(priorities)], "bug", parent.ID, fmt.Sprintf("p%d-s%d", i, j))
			if err != nil {
				t.Fatalf("Create(subtask) error = %v", err)
			}
			ids = append(ids, sub.ID)
		}
	}
	for i := 1; i < 10; i += 3 {
		if err := svc.AddRelation(fmt.Sprintf("p%d", i), "blocked_by", fmt.Sprintf("p%d", i-1)); err != nil {
			t.Fatalf("AddRelation() error = %v", err)
		}
	}
	return ids
}

// TestServiceRace hammers the service from many goroutines at once, the way
// mcp-go's stdio server does: it dispatches tool calls across a worker pool,
// so two create_task calls really can land in Create at the same moment.
func TestServiceRace(t *testing.T) {
	svc := newConcurrentService()
	ids := seedBacklog(t, svc)

	const iterations = 200
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

	// Readers.
	run(func(i int) { svc.List(nil, nil, nil, nil, nil) })
	run(func(i int) { svc.List(ptrTo(StatusTodo), nil, nil, nil, nil) })
	run(func(i int) { svc.Get(ids[i%len(ids)]) })
	run(func(i int) { svc.GetWithSubtasks(ids[i%len(ids)]) })
	run(func(i int) { svc.IsBlocked(ids[i%len(ids)]) })
	run(func(i int) { svc.GetNextTask() })
	run(func(i int) { svc.GetSubtaskCounts(fmt.Sprintf("p%d", i%10)) })
	run(func(i int) { svc.ListTaskFiles(ids[i%len(ids)]) })
	run(func(i int) { svc.BoardSnapshot() })
	run(func(i int) { svc.Detail(ids[i%len(ids)]) })
	run(func(i int) { svc.Relations(ids[i%len(ids)]) })

	// Writers.
	run(func(i int) { svc.Create(fmt.Sprintf("created %d", i), "", PriorityMedium, "feature", "", "") })
	run(func(i int) {
		title := fmt.Sprintf("renamed %d", i)
		svc.Update(fmt.Sprintf("p%d", i%10), &title, nil, nil, nil, nil)
	})
	run(func(i int) { svc.StartTask(fmt.Sprintf("p%d-s%d", i%10, i%2)) })
	run(func(i int) { svc.CompleteTask(fmt.Sprintf("p%d-s%d", i%10, i%2)) })
	run(func(i int) { svc.WriteTaskFile(ids[i%len(ids)], "notes.md", "content") })

	wg.Wait()
}

// TestServiceNoSelfDeadlock is the permanent guard for the twin discipline:
// an exported method that calls another exported one deadlocks on the spot,
// because Go mutexes are not reentrant.
func TestServiceNoSelfDeadlock(t *testing.T) {
	svc := newConcurrentService()
	seedBacklog(t, svc)

	watchdog := time.AfterFunc(10*time.Second, func() {
		panic("deadlock: a Service method called another exported Service method")
	})
	defer watchdog.Stop()

	title := "edited"
	status := StatusInProgress

	svc.EnsureProjectExists()
	svc.ProjectFound()
	svc.Config()
	svc.BranchingEnabled()
	svc.CurrentTask()
	svc.Initialize()
	svc.Create("fresh", "", PriorityLow, "feature", "", "fresh")
	svc.CreateSubtask("fresh sub", "", PriorityLow, "bug", "fresh")
	svc.Get("fresh")
	svc.GetWithSubtasks("fresh")
	svc.Update("fresh", &title, nil, nil, nil, nil)
	svc.List(nil, nil, nil, nil, nil)
	svc.GetNextTask()
	svc.GetSubtaskCounts("fresh")
	svc.WriteTaskFile("fresh", "notes.md", "hello")
	svc.ReadTaskFile("fresh", "notes.md")
	svc.ListTaskFiles("fresh")
	svc.AddRelation("fresh", "relates_to", "p0")
	svc.Relations("fresh")
	svc.IsBlocked("fresh")
	svc.BlockedMap([]string{"fresh", "p0"})
	svc.BoardSnapshot()
	svc.Detail("fresh")
	svc.RemoveRelation("fresh", "relates_to", "p0")
	svc.StartTask("p0")
	svc.Update("p0", nil, nil, &status, nil, nil)
	svc.CompleteTask("p0-s0")
	svc.GetAutoArchiveCandidates()
	svc.RunAutoArchive()
	svc.ArchiveTask("fresh")
	svc.ListArchived()
	svc.Delete("fresh", true)
}

func ptrTo[T any](v T) *T { return &v }
