package web

import (
	"slices"
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
		Phases:   map[string]task.Phase{},
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

func lane(v BoardView, phase task.Phase) PhaseLaneView {
	lanes := column(v, "in_progress").Lanes
	if i := slices.IndexFunc(lanes, func(l PhaseLaneView) bool { return l.Phase == string(phase) }); i >= 0 {
		return lanes[i]
	}
	return PhaseLaneView{}
}

func cardIDs(cards []CardView) []string {
	ids := make([]string, 0, len(cards))
	for _, c := range cards {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestInProgressHasFourPhaseLanes(t *testing.T) {
	v := newBoardView(boardOf(), nil, fixedNow, 5)

	var phases []string
	for _, l := range column(v, "in_progress").Lanes {
		phases = append(phases, l.Phase)
		if l.Count != 0 || len(l.Cards) != 0 {
			t.Errorf("lane %s = %d count, %d cards on an empty board", l.Phase, l.Count, len(l.Cards))
		}
	}
	if want := []string{"research", "design", "planning", "implementation"}; !slices.Equal(phases, want) {
		t.Errorf("in_progress lanes = %v, want %v", phases, want)
	}
	for _, status := range []string{"todo", "done"} {
		if n := len(column(v, status).Lanes); n != 0 {
			t.Errorf("%s has %d lanes, want none", status, n)
		}
	}
}

func TestInProgressCardsLandInTheirLane(t *testing.T) {
	snap := boardOf(
		tk("r", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("x", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("q", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("d", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("p", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("i", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
	)
	snap.Phases["x"] = task.PhaseResearch
	snap.Phases["q"] = task.Phase("bogus")
	snap.Phases["d"] = task.PhaseDesign
	snap.Phases["p"] = task.PhasePlanning
	snap.Phases["i"] = task.PhaseImplementation

	v := newBoardView(snap, nil, fixedNow, 5)
	want := map[task.Phase][]string{
		task.PhaseResearch:       {"q", "r", "x"},
		task.PhaseDesign:         {"d"},
		task.PhasePlanning:       {"p"},
		task.PhaseImplementation: {"i"},
	}
	for phase, ids := range want {
		if got := cardIDs(lane(v, phase).Cards); !slices.Equal(got, ids) {
			t.Errorf("lane %s = %v, want %v", phase, got, ids)
		}
	}
}

func TestNilPhasesMapReadsAsResearch(t *testing.T) {
	snap := boardOf(tk("a", "", task.StatusInProgress, task.PriorityHigh, fixedNow))
	snap.Phases = nil

	v := newBoardView(snap, nil, fixedNow, 5)
	if got := cardIDs(lane(v, task.PhaseResearch).Cards); !slices.Equal(got, []string{"a"}) {
		t.Errorf("research lane = %v, want [a]", got)
	}
}

func TestLaneGroupTakesFurthestPhase(t *testing.T) {
	snap := boardOf(
		tk("P", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("S", "P", task.StatusInProgress, task.PriorityHigh, fixedNow),
	)
	snap.Phases["S"] = task.PhasePlanning

	v := newBoardView(snap, nil, fixedNow, 5)
	planning := lane(v, task.PhasePlanning)
	if got := cardIDs(planning.Cards); !slices.Equal(got, []string{"P"}) {
		t.Fatalf("planning lane = %v, want [P]", got)
	}
	if got := cardIDs(planning.Cards[0].Subtasks); !slices.Equal(got, []string{"S"}) {
		t.Errorf("P nests %v, want [S]", got)
	}
	if planning.Count != 2 {
		t.Errorf("planning Count = %d, want 2: nested subtasks count", planning.Count)
	}
	if research := lane(v, task.PhaseResearch); research.Count != 0 || len(research.Cards) != 0 {
		t.Errorf("research lane = %d count, %v cards, want empty", research.Count, cardIDs(research.Cards))
	}
}

func TestStandaloneSubtaskUsesOwnPhase(t *testing.T) {
	snap := boardOf(
		tk("P", "", task.StatusTodo, task.PriorityHigh, fixedNow),
		tk("S", "P", task.StatusInProgress, task.PriorityHigh, fixedNow),
	)
	snap.Phases["S"] = task.PhaseDesign

	v := newBoardView(snap, nil, fixedNow, 5)
	design := lane(v, task.PhaseDesign)
	if len(design.Cards) != 1 || design.Cards[0].ID != "S" || !design.Cards[0].IsSubtask {
		t.Errorf("design lane = %+v, want the standalone subtask S", design.Cards)
	}
	todo := column(v, "todo").Cards
	if len(todo) != 1 || todo[0].ID != "P" || len(todo[0].Subtasks) != 0 {
		t.Errorf("todo = %+v, want P with no nested subtasks", todo)
	}
}

// TestLanesPartitionTheColumn uses a board with nesting, a standalone
// subtask and tasks outside In progress: every In progress card sits in
// exactly one lane, and the lane counts add up to the column count.
func TestLanesPartitionTheColumn(t *testing.T) {
	snap := boardOf(
		tk("a", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("b", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("b1", "b", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("c", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("c1", "c", task.StatusTodo, task.PriorityHigh, fixedNow),
		tk("d", "", task.StatusTodo, task.PriorityHigh, fixedNow),
		tk("d1", "d", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("e", "", task.StatusDone, task.PriorityHigh, fixedNow),
	)
	snap.Phases["b"] = task.PhaseDesign
	snap.Phases["b1"] = task.PhaseImplementation
	snap.Phases["c"] = task.PhasePlanning
	snap.Phases["d1"] = task.PhaseDesign

	col := column(newBoardView(snap, nil, fixedNow, 5), "in_progress")
	var counts []int
	var ids []string
	sum := 0
	for _, l := range col.Lanes {
		counts = append(counts, l.Count)
		ids = append(ids, cardIDs(l.Cards)...)
		sum += l.Count
	}
	if want := []int{1, 1, 1, 2}; !slices.Equal(counts, want) {
		t.Errorf("lane counts = %v, want %v", counts, want)
	}
	if sum != col.Count || col.Count != 5 {
		t.Errorf("lane counts sum to %d, column Count = %d, want both 5", sum, col.Count)
	}
	want := cardIDs(col.Cards)
	slices.Sort(ids)
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("lanes hold %v, column holds %v: each card exactly once", ids, want)
	}
}

func TestLaneSortsByPriorityThenAge(t *testing.T) {
	old := fixedNow.Add(-48 * time.Hour)
	recent := fixedNow.Add(-time.Hour)
	snap := boardOf(
		tk("x", "", task.StatusInProgress, task.PriorityLow, old),
		tk("y", "", task.StatusInProgress, task.PriorityHigh, recent),
		tk("z", "", task.StatusInProgress, task.PriorityHigh, old),
	)
	for _, id := range []string{"x", "y", "z"} {
		snap.Phases[id] = task.PhaseDesign
	}

	got := cardIDs(lane(newBoardView(snap, nil, fixedNow, 5), task.PhaseDesign).Cards)
	if want := []string{"z", "y", "x"}; !slices.Equal(got, want) {
		t.Errorf("design lane = %v, want %v", got, want)
	}
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
