package web

import (
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

func tk(id, parent string, status task.Status, priority task.Priority, created time.Time) *task.Task {
	return &task.Task{
		ID:        id,
		ParentID:  parent,
		Title:     "task " + id,
		Status:    status,
		Priority:  priority,
		Type:      "feature",
		CreatedAt: created,
		UpdatedAt: created,
	}
}

func boardOf(tasks ...*task.Task) *task.BoardSnapshot {
	snap := &task.BoardSnapshot{
		Tasks:    tasks,
		Subtasks: map[string][]*task.Task{},
		Blocked:  map[string][]task.BlockingInfo{},
		Counts:   map[string]task.SubtaskCount{},
		TakenAt:  fixedNow,
	}
	for _, t := range tasks {
		if t.ParentID == "" {
			continue
		}
		snap.Subtasks[t.ParentID] = append(snap.Subtasks[t.ParentID], t)
		c := snap.Counts[t.ParentID]
		c.Total++
		if t.Status == task.StatusDone {
			c.Done++
		}
		snap.Counts[t.ParentID] = c
	}
	return snap
}

func column(v BoardView, status string) ColumnView {
	for _, c := range v.Columns {
		if c.Status == status {
			return c
		}
	}
	return ColumnView{}
}

func TestColumnsAreInFixedOrder(t *testing.T) {
	v := newBoardView(boardOf(), nil, fixedNow, 5)
	want := []string{"todo", "in_progress", "done"}
	if len(v.Columns) != len(want) {
		t.Fatalf("got %d columns, want %d", len(v.Columns), len(want))
	}
	for i, status := range want {
		if v.Columns[i].Status != status {
			t.Errorf("column %d = %q, want %q", i, v.Columns[i].Status, status)
		}
	}
}

func TestCardsSortByPriorityThenAge(t *testing.T) {
	old := fixedNow.Add(-48 * time.Hour)
	recent := fixedNow.Add(-time.Hour)

	snap := boardOf(
		tk("low", "", task.StatusTodo, task.PriorityLow, old),
		tk("high-new", "", task.StatusTodo, task.PriorityHigh, recent),
		tk("high-old", "", task.StatusTodo, task.PriorityHigh, old),
		tk("critical", "", task.StatusTodo, task.PriorityCritical, recent),
	)

	got := column(newBoardView(snap, nil, fixedNow, 5), "todo").Cards
	want := []string{"critical", "high-old", "high-new", "low"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("card %d = %q, want %q (order: priority, then oldest first)", i, got[i].ID, id)
		}
	}
}

func TestSubtaskNestsWhenParentShareColumn(t *testing.T) {
	snap := boardOf(
		tk("p", "", task.StatusTodo, task.PriorityHigh, fixedNow),
		tk("s", "p", task.StatusTodo, task.PriorityHigh, fixedNow),
	)

	todo := column(newBoardView(snap, nil, fixedNow, 5), "todo")
	if len(todo.Cards) != 1 {
		t.Fatalf("todo has %d top-level cards, want 1: the subtask nests", len(todo.Cards))
	}
	if len(todo.Cards[0].Subtasks) != 1 || todo.Cards[0].Subtasks[0].ID != "s" {
		t.Errorf("subtask did not nest under its parent: %+v", todo.Cards[0].Subtasks)
	}
	if todo.Count != 2 {
		t.Errorf("column count = %d, want 2: the count is tasks, not cards", todo.Count)
	}
}

func TestSubtaskStandsAloneAcrossColumns(t *testing.T) {
	snap := boardOf(
		tk("p", "", task.StatusTodo, task.PriorityHigh, fixedNow),
		tk("s", "p", task.StatusInProgress, task.PriorityHigh, fixedNow),
	)
	v := newBoardView(snap, nil, fixedNow, 5)

	if cards := column(v, "todo").Cards; len(cards) != 1 || len(cards[0].Subtasks) != 0 {
		t.Errorf("parent card nested a subtask from another column: %+v", cards)
	}
	inProgress := column(v, "in_progress").Cards
	if len(inProgress) != 1 || inProgress[0].ID != "s" {
		t.Fatalf("subtask is missing from its own column: %+v", inProgress)
	}
	if !inProgress[0].IsSubtask || inProgress[0].ParentID != "p" {
		t.Error("standalone subtask is not flagged with its parent")
	}
}

func TestDangerZoneIsTheInProgressSet(t *testing.T) {
	snap := boardOf(
		tk("a", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("b", "", task.StatusTodo, task.PriorityHigh, fixedNow),
		tk("c", "", task.StatusInProgress, task.PriorityLow, fixedNow),
	)
	v := newBoardView(snap, nil, fixedNow, 5)

	if len(v.DangerZone) != 2 {
		t.Fatalf("danger zone has %d items, want 2", len(v.DangerZone))
	}
	if v.DangerZone[0].ID != "a" || v.DangerZone[1].ID != "c" {
		t.Errorf("danger zone = %+v, want a and c", v.DangerZone)
	}
}

func TestBlockedCardCarriesBlockers(t *testing.T) {
	snap := boardOf(tk("a", "", task.StatusTodo, task.PriorityHigh, fixedNow))
	snap.Blocked["a"] = []task.BlockingInfo{{TaskID: "b", Status: task.StatusTodo, Title: "blocker"}}

	card := column(newBoardView(snap, nil, fixedNow, 5), "todo").Cards[0]
	if !card.Blocked {
		t.Fatal("card is not marked blocked")
	}
	if len(card.Blockers) != 1 || card.Blockers[0].Title != "blocker" {
		t.Errorf("Blockers = %+v, want the one blocker with its title", card.Blockers)
	}
}

func TestProjectViewComesFromResolution(t *testing.T) {
	cfg := &config.Config{Resolution: &config.Resolution{
		Root:     "/repo",
		TasksDir: "/repo/tasks",
		Source:   config.SourceProjectEnv,
	}}
	v := newBoardView(boardOf(tk("a", "", task.StatusTodo, task.PriorityLow, fixedNow)), cfg, fixedNow, 5)

	if !v.Project.Resolved {
		t.Error("Project.Resolved = false for a resolved config")
	}
	if v.Project.TasksDir != "/repo/tasks" || v.Project.TaskCount != 1 {
		t.Errorf("Project = %+v, want /repo/tasks with 1 task", v.Project)
	}
}

func TestUnresolvedBoardViewKeepsPolling(t *testing.T) {
	v := unresolvedBoardView(7)
	if v.Project.Resolved {
		t.Error("Project.Resolved = true on the placeholder view")
	}
	if v.PollSeconds != 7 {
		t.Errorf("PollSeconds = %d, want 7: the placeholder has to self-heal", v.PollSeconds)
	}
}

func TestDetailViewArchivedShape(t *testing.T) {
	closed := fixedNow.Add(-2 * time.Hour)
	d := &task.TaskDetail{
		Task: &task.Task{
			ID:         "7",
			Title:      "done thing",
			Status:     task.StatusDone,
			Priority:   task.PriorityMedium,
			Type:       "bug",
			CreatedAt:  fixedNow.Add(-72 * time.Hour),
			UpdatedAt:  closed,
			ClosedAt:   &closed,
			Resolution: task.ResolutionObsolete,
		},
		Archived: true,
		Files:    []string{"design.md"},
	}

	v := newDetailView(d, nil, nil, fixedNow)
	if !v.Archived {
		t.Error("Archived = false")
	}
	if len(v.Relations) != 0 || len(v.Card.Subtasks) != 0 || len(v.Card.Blockers) != 0 {
		t.Error("archived detail carries derived data it cannot have")
	}
	if v.Card.Resolution != "obsolete" {
		t.Errorf("Resolution = %q, want obsolete", v.Card.Resolution)
	}
	if v.ClosedAt == "" {
		t.Error("ClosedAt was not rendered")
	}
	if len(v.Files) != 1 {
		t.Errorf("Files = %v, want the one attached file", v.Files)
	}
}

func TestDetailViewRelationDirection(t *testing.T) {
	d := &task.TaskDetail{
		Task: &task.Task{ID: "a", Title: "a", Status: task.StatusTodo, Priority: task.PriorityLow},
		Relations: []task.RelationEdge{
			{Type: "blocked_by", Source: "a", Target: "b"},
			{Type: "relates_to", Source: "c", Target: "a"},
		},
	}

	v := newDetailView(d, nil, map[string]string{"b": "the blocker", "c": "the other"}, fixedNow)
	if len(v.Relations) != 2 {
		t.Fatalf("got %d relations, want 2", len(v.Relations))
	}
	for _, r := range v.Relations {
		switch r.Type {
		case "blocked_by":
			if r.Direction != "outgoing" || r.OtherID != "b" || r.OtherTitle != "the blocker" {
				t.Errorf("blocked_by = %+v, want outgoing to b", r)
			}
		case "relates_to":
			if r.Direction != "incoming" || r.OtherID != "c" || r.OtherTitle != "the other" {
				t.Errorf("relates_to = %+v, want incoming from c", r)
			}
		}
	}
}

func TestHumanizeAgo(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, c := range cases {
		if got := humanizeAgo(fixedNow, fixedNow.Add(-c.ago)); got != c.want {
			t.Errorf("humanizeAgo(-%s) = %q, want %q", c.ago, got, c.want)
		}
	}
}
