package web

import (
	"slices"
	"strconv"
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

// TestLaneGroupSpansItsSubtaskPhases pins the spanning rule: the parent P
// is in research with a planning subtask, so its card is listed in the lane
// it begins in and spans research..planning, while each task counts in the
// lane of its own phase.
func TestLaneGroupSpansItsSubtaskPhases(t *testing.T) {
	snap := boardOf(
		tk("P", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("S", "P", task.StatusInProgress, task.PriorityHigh, fixedNow),
	)
	snap.Phases["S"] = task.PhasePlanning

	v := newBoardView(snap, nil, fixedNow, 5)
	research := lane(v, task.PhaseResearch)
	if got := cardIDs(research.Cards); !slices.Equal(got, []string{"P"}) {
		t.Fatalf("research lane = %v, want [P]: a card is listed where it begins", got)
	}
	card := research.Cards[0]
	if card.LaneFrom != "research" || card.LaneTo != "planning" {
		t.Errorf("P spans %s..%s, want research..planning", card.LaneFrom, card.LaneTo)
	}
	if got := cardIDs(card.Subtasks); !slices.Equal(got, []string{"S"}) {
		t.Errorf("P nests %v, want [S]", got)
	}
	if got := card.Subtasks[0].LaneOffset; got != 2 {
		t.Errorf("S LaneOffset = %d, want 2 steps right of research", got)
	}
	if research.Count != 1 {
		t.Errorf("research Count = %d, want 1: only P is in research", research.Count)
	}
	if planning := lane(v, task.PhasePlanning); planning.Count != 1 || len(planning.Cards) != 0 {
		t.Errorf("planning lane = %d count, %v cards, want 1 count and no card of its own",
			planning.Count, cardIDs(planning.Cards))
	}
	for _, phase := range []task.Phase{task.PhaseDesign, task.PhaseImplementation} {
		if l := lane(v, phase); l.Count != 0 || len(l.Cards) != 0 {
			t.Errorf("%s lane = %d count, %v cards, want empty", phase, l.Count, cardIDs(l.Cards))
		}
	}
}

// TestSingleLaneCardSpansOneLane: without in-progress subtasks a card still
// occupies exactly its own phase, and a nested todo subtask - which has no
// phase of its own - neither widens the span nor shifts its row.
func TestSingleLaneCardSpansOneLane(t *testing.T) {
	snap := boardOf(
		tk("P", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("S", "P", task.StatusTodo, task.PriorityHigh, fixedNow),
	)
	snap.Phases["P"] = task.PhaseDesign

	v := newBoardView(snap, nil, fixedNow, 5)
	design := lane(v, task.PhaseDesign)
	if len(design.Cards) != 1 || design.Count != 1 {
		t.Fatalf("design lane = %d count, %v cards, want 1 and [P]", design.Count, cardIDs(design.Cards))
	}
	card := design.Cards[0]
	if card.LaneFrom != "design" || card.LaneTo != "design" {
		t.Errorf("P spans %s..%s, want design..design", card.LaneFrom, card.LaneTo)
	}
	if got := card.Subtasks[0].LaneOffset; got != 0 {
		t.Errorf("todo subtask LaneOffset = %d, want 0", got)
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
	// a has no phase, so it reads as research; b spans design..implementation
	// and is listed in design; c's nested subtask is todo, so c stays in
	// planning; d1 is a standalone subtask in design.

	col := column(newBoardView(snap, nil, fixedNow, 5), "in_progress")
	var counts []int
	var ids []string
	sum := 0
	for _, l := range col.Lanes {
		counts = append(counts, l.Count)
		ids = append(ids, cardIDs(l.Cards)...)
		sum += l.Count
	}
	if want := []int{1, 2, 1, 1}; !slices.Equal(counts, want) {
		t.Errorf("lane counts = %v, want %v: every task counts in its own phase", counts, want)
	}
	if got := cardIDs(col.Lanes[1].Cards); !slices.Equal(got, []string{"b", "d1"}) {
		t.Errorf("design lane holds %v, want [b d1]", got)
	}
	if got := col.Lanes[3]; len(got.Cards) != 0 || got.Count != 1 {
		t.Errorf("implementation lane = %d count, %v cards, want 1 count from b1 and no card",
			got.Count, cardIDs(got.Cards))
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
	for _, c := range newBoardView(snap, nil, fixedNow, 5).Columns {
		if slices.Contains(cardIDs(c.Cards), "e") {
			t.Errorf("done task e has a card in the %s column", c.Status)
		}
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

	if v.Project.Root != "/repo" || v.Project.Source != string(config.SourceProjectEnv) {
		t.Errorf("Project = %+v, want the resolution's root and source", v.Project)
	}
	if v.Project.TasksDir != "/repo/tasks" || v.Project.TaskCount != 1 {
		t.Errorf("Project = %+v, want /repo/tasks with 1 task", v.Project)
	}
}

// The welcome page renders through the same layout as a board, so it has to
// survive a ProjectView with nothing in it: there is no backlog behind it.
func TestWelcomeViewHasNoProject(t *testing.T) {
	v := newWelcomeView(nil, nil, "", nil, "")
	if v.Project.TasksDir != "" || v.Project.TaskCount != 0 {
		t.Errorf("Project = %+v, want an empty one", v.Project)
	}
	if len(v.Workspaces) != 0 || len(v.Sessions) != 0 {
		t.Errorf("newWelcomeView(nil, nil, ...) = %+v, want nothing listed", v)
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
	} else if f := v.Files[0]; f.Name != "design.md" ||
		f.Href != "/tasks/7/w/f/design.md" ||
		f.HXGet != "/strip/tasks/7/w/f/design.md" ||
		f.RawHref != "/tasks/7/files/design.md" {
		t.Errorf("Files[0] = %+v, want design.md with its workspace and raw URLs", f)
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

func findCard(t *testing.T, v BoardView, id string) CardView {
	t.Helper()
	for _, col := range v.Columns {
		for _, c := range col.Cards {
			if c.ID == id {
				return c
			}
		}
	}
	t.Fatalf("no card %s on the board", id)
	return CardView{}
}

func TestCardCreatedBy(t *testing.T) {
	owned := tk("a", "", task.StatusTodo, task.PriorityHigh, fixedNow.Add(-3*time.Hour))
	owned.CreatedBy = "igor.cwer"
	legacy := tk("b", "", task.StatusTodo, task.PriorityHigh, fixedNow.Add(-2*24*time.Hour))
	v := newBoardView(boardOf(owned, legacy), nil, fixedNow, 5)

	a := findCard(t, v, "a")
	if a.CreatedBy != "igor.cwer" || a.CreatedAgo != "3h ago" || a.CreatedAt != fixedNow.Add(-3*time.Hour).Format(timeFormat) {
		t.Errorf("card a = by %q, %q, %q", a.CreatedBy, a.CreatedAgo, a.CreatedAt)
	}
	if b := findCard(t, v, "b"); b.CreatedBy != "" || b.CreatedAgo != "2d ago" {
		t.Errorf("legacy card = by %q, %q", b.CreatedBy, b.CreatedAgo)
	}
}

func TestCardPhaseOpenVsFinished(t *testing.T) {
	open := tk("open", "", task.StatusInProgress, task.PriorityHigh, fixedNow)
	finished := tk("fin", "", task.StatusInProgress, task.PriorityHigh, fixedNow)
	snap := boardOf(open, finished)
	done := fixedNow.Add(-10 * time.Minute)
	snap.Phases["open"], snap.Phases["fin"] = task.PhaseDesign, task.PhasePlanning
	snap.PhaseInfo = map[string]task.PhaseSummary{
		"open": {Current: task.PhaseDesign, Run: task.PhaseRun{StartedAt: fixedNow.Add(-2 * time.Hour), StartedBy: "dev"}},
		"fin": {Current: task.PhasePlanning, Run: task.PhaseRun{StartedAt: fixedNow.Add(-5 * time.Hour), StartedBy: "ann",
			FinishedAt: &done, FinishedBy: "bob"}},
	}
	v := newBoardView(snap, nil, fixedNow, 5)

	if c := findCard(t, v, "open"); c.Phase != "design" || !c.PhaseOpen || c.PhaseAgo != "2h ago" || c.PhaseBy != "dev" {
		t.Errorf("open card phase = %q open %v %q by %q", c.Phase, c.PhaseOpen, c.PhaseAgo, c.PhaseBy)
	}
	if c := findCard(t, v, "fin"); c.Phase != "planning" || c.PhaseOpen || c.PhaseAgo != "10m ago" || c.PhaseBy != "ann" {
		t.Errorf("finished card phase = %q open %v %q by %q", c.Phase, c.PhaseOpen, c.PhaseAgo, c.PhaseBy)
	}
}

func TestCardTokensHumanized(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"}, {950, "950"}, {999, "999"}, {1000, "1k"}, {81234, "81.2k"},
		{999_949, "999.9k"}, {999_950, "1M"}, {1_400_000, "1.4M"}, {12_345_678, "12.3M"},
	}
	for _, tt := range tests {
		if got := humanizeTokens(tt.n); got != tt.want {
			t.Errorf("humanizeTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}

	a := tk("a", "", task.StatusInProgress, task.PriorityHigh, fixedNow)
	snap := boardOf(a)
	snap.PhaseInfo = map[string]task.PhaseSummary{"a": {Current: task.PhaseResearch,
		Run: task.PhaseRun{StartedAt: fixedNow}, Tokens: 81234, HasTokens: true}}
	if c := findCard(t, newBoardView(snap, nil, fixedNow, 5), "a"); c.Tokens != "81.2k" || c.TokensExact != "81234" {
		t.Errorf("card tokens = %q (%q)", c.Tokens, c.TokensExact)
	}
	snap.PhaseInfo["a"] = task.PhaseSummary{Current: task.PhaseResearch, Run: task.PhaseRun{StartedAt: fixedNow}}
	if c := findCard(t, newBoardView(snap, nil, fixedNow, 5), "a"); c.Tokens != "" {
		t.Errorf("card without reported tokens shows %q", c.Tokens)
	}
}

func TestCardNoPhaseWithoutPhaseInfo(t *testing.T) {
	a := tk("a", "", task.StatusInProgress, task.PriorityHigh, fixedNow)
	snap := boardOf(a)
	snap.Phases["a"] = task.PhaseDesign // from the file names
	c := findCard(t, newBoardView(snap, nil, fixedNow, 5), "a")
	if c.Phase != "" || c.PhaseBy != "" || c.PhaseAgo != "" || c.Tokens != "" {
		t.Errorf("card without phase records shows phase data: %+v", c)
	}
}

func TestLaneFromRecordsViaSnapshot(t *testing.T) {
	a := tk("a", "", task.StatusInProgress, task.PriorityHigh, fixedNow)
	snap := boardOf(a)
	snap.Phases["a"] = task.PhasePlanning
	snap.PhaseInfo = map[string]task.PhaseSummary{"a": {Current: task.PhasePlanning, Run: task.PhaseRun{StartedAt: fixedNow}}}
	v := newBoardView(snap, nil, fixedNow, 5)
	if ids := cardIDs(lane(v, task.PhasePlanning).Cards); !slices.Equal(ids, []string{"a"}) {
		t.Errorf("planning lane = %v, want [a]", ids)
	}
}

func TestDetailPhasesMapping(t *testing.T) {
	started := fixedNow.Add(-3 * time.Hour)
	finished := fixedNow.Add(-2 * time.Hour)
	n1, n2 := int64(40210), int64(1000)
	d := &task.TaskDetail{
		Task: &task.Task{ID: "a", Title: "A", Status: task.StatusInProgress, CreatedBy: "igor.cwer", CreatedAt: started, UpdatedAt: started},
		Phases: []task.PhaseRecord{
			{Phase: task.PhaseResearch, Runs: []task.PhaseRun{
				{StartedAt: started, StartedBy: "dev", FinishedAt: &finished, FinishedBy: "ann", Tokens: &n1, Note: "first"},
			}},
			{Phase: task.PhaseDesign, Runs: []task.PhaseRun{
				{StartedAt: started, StartedBy: "dev", FinishedAt: &finished, FinishedBy: "dev", Tokens: &n2},
				{StartedAt: finished, StartedBy: "dev"},
			}},
		},
	}
	v := newDetailView(d, nil, nil, fixedNow)
	if v.Card.CreatedBy != "igor.cwer" {
		t.Errorf("CreatedBy = %q", v.Card.CreatedBy)
	}
	if v.TotalTokens != "41.2k" {
		t.Errorf("TotalTokens = %q, want 41.2k", v.TotalTokens)
	}
	if len(v.Phases) != 2 || v.Phases[0].Phase != "research" || len(v.Phases[1].Runs) != 2 {
		t.Fatalf("Phases = %+v", v.Phases)
	}
	r := v.Phases[0].Runs[0]
	if r.N != 1 || r.Started != started.Format(timeFormat) || r.StartedBy != "dev" || r.Finished != finished.Format(timeFormat) ||
		r.FinishedBy != "ann" || r.Tokens != "40.2k" || r.TokensExact != "40210" || r.Note != "first" {
		t.Errorf("research run = %+v", r)
	}
	if open := v.Phases[1].Runs[1]; open.N != 2 || open.Finished != "" || open.Tokens != "" {
		t.Errorf("open design run = %+v", open)
	}

	d.Phases = nil
	if v := newDetailView(d, nil, nil, fixedNow); v.Phases != nil || v.TotalTokens != "" {
		t.Errorf("no records: Phases %v, TotalTokens %q", v.Phases, v.TotalTokens)
	}
}

func TestDoneColumnHasStatsNotCards(t *testing.T) {
	snap := boardOf(
		tk("a", "", task.StatusDone, task.PriorityHigh, fixedNow),
		tk("b", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("c", "", task.StatusTodo, task.PriorityLow, fixedNow),
	)
	snap.Stats = []task.StatsCard{{ID: "bars-priority", Kind: config.StatsKindBars, Title: "Priority",
		Bars: []task.StatsBar{{Value: "high", Total: 1, Done: 1}}}}

	done := column(newBoardView(snap, nil, fixedNow, 5), "done")
	if done.Count != 2 {
		t.Errorf("done Count = %d, want 2: done tasks are still counted", done.Count)
	}
	if len(done.Cards) != 0 {
		t.Errorf("done column has cards %v, want none", cardIDs(done.Cards))
	}
	if len(done.Stats) != 1 || done.Stats[0].ID != "bars-priority" || done.Stats[0].Title != "Priority" {
		t.Errorf("done Stats = %+v, want the bars-priority card", done.Stats)
	}
	for _, status := range []string{"todo", "in_progress"} {
		if column(newBoardView(snap, nil, fixedNow, 5), status).Stats != nil {
			t.Errorf("%s column has Stats, want nil", status)
		}
	}
}

func TestStatsCardsKeepConfigOrder(t *testing.T) {
	got := newStatsCards([]task.StatsCard{
		{ID: "bars-type", Kind: config.StatsKindBars},
		{ID: "lines-14d", Kind: config.StatsKindLines},
		{ID: "bars-priority", Kind: config.StatsKindBars},
		{ID: "odd", Kind: "pie"},
	})
	var ids, kinds []string
	for _, c := range got {
		ids = append(ids, c.ID)
		kinds = append(kinds, c.Kind)
	}
	if want := []string{"bars-type", "lines-14d", "bars-priority"}; !slices.Equal(ids, want) {
		t.Errorf("stats cards = %v, want %v (known kinds, config order)", ids, want)
	}
	if want := []string{"bars", "lines", "bars"}; !slices.Equal(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
}

func TestStatsBarOffsets(t *testing.T) {
	cards := newStatsCards([]task.StatsCard{{Kind: config.StatsKindBars, Bars: []task.StatsBar{
		{Value: "full", Total: 12, Done: 7, InProgress: 3, Todo: 2, ClosedRecently: 2},
		{Value: "done-only", Total: 4, Done: 4},
		{Value: "open-only", Total: 3, InProgress: 1, Todo: 2},
	}}})
	want := []StatsBarView{
		{Value: "full", Total: 12, Done: 7, InProgress: 3, Todo: 2, Recent: 2, Open: 5, RecentX: 5, TodoX: 10},
		{Value: "done-only", Total: 4, Done: 4, RecentX: 4, TodoX: 4},
		{Value: "open-only", Total: 3, InProgress: 1, Todo: 2, Open: 3, TodoX: 1},
	}
	if !slices.Equal(cards[0].Bars, want) {
		t.Errorf("bars = %+v\nwant %+v", cards[0].Bars, want)
	}
}

func TestStatsBarSkipsEmptyTotal(t *testing.T) {
	cards := newStatsCards([]task.StatsCard{{ID: "x", Kind: config.StatsKindBars, Bars: []task.StatsBar{
		{Value: "none"}, {Value: "one", Total: 1, Todo: 1},
	}}})
	if len(cards) != 1 || len(cards[0].Bars) != 1 || cards[0].Bars[0].Value != "one" {
		t.Errorf("cards = %+v, want only the row with tasks", cards)
	}
}

// statsDays returns n consecutive local midnights ending on Oct 6 2026.
func statsDays(n int) []time.Time {
	days := make([]time.Time, n)
	for i := range days {
		days[i] = time.Date(2026, 10, 6-(n-1-i), 0, 0, 0, 0, time.UTC)
	}
	return days
}

func linesCard(lines ...task.StatsLine) task.StatsCard {
	n := 0
	if len(lines) > 0 {
		n = len(lines[0].Values)
	}
	return task.StatsCard{ID: "lines", Kind: config.StatsKindLines, Days: statsDays(n), Lines: lines}
}

func TestStatsChartPoints(t *testing.T) {
	c := newStatsChart(linesCard(task.StatsLine{Key: "created", Values: []int{1, 0, 2}}))
	if c == nil {
		t.Fatal("chart is nil")
	}
	if c.Width != 6 || c.First != "Oct 4" || c.Last != "Oct 6" {
		t.Errorf("chart = width %d, %s..%s; want 6, Oct 4..Oct 6", c.Width, c.First, c.Last)
	}
	if len(c.Groups) != 1 {
		t.Fatalf("groups = %+v, want one", c.Groups)
	}
	g := c.Groups[0]
	if g.Cumulative || g.Width != 6 || g.Max != 2 || g.Peak != 2 {
		t.Errorf("group = %+v, want per-day, width 6, max 2", g)
	}
	if got := g.Lines[0].Points; got != "1,1 3,2 5,0" {
		t.Errorf("points = %q, want %q", got, "1,1 3,2 5,0")
	}
}

func TestStatsChartGroupsByKind(t *testing.T) {
	c := newStatsChart(linesCard(
		task.StatsLine{Key: "created_cumulative", Values: []int{3, 5, 9}, Cumulative: true},
		task.StatsLine{Key: "created", Values: []int{3, 2, 4}},
		task.StatsLine{Key: "closed", Values: []int{0, 1, 0}},
	))
	if len(c.Groups) != 2 || c.Groups[0].Cumulative || !c.Groups[1].Cumulative {
		t.Fatalf("groups = %+v, want per-day then cumulative", c.Groups)
	}
	if c.Groups[0].Max != 4 || len(c.Groups[0].Lines) != 2 {
		t.Errorf("per-day group = %+v, want max 4 with created and closed", c.Groups[0])
	}
	if c.Groups[1].Max != 9 || c.Groups[1].Lines[0].Points != "1,6 3,4 5,0" {
		t.Errorf("cumulative group = %+v, want max 9, points 1,6 3,4 5,0", c.Groups[1])
	}
	var legend []string
	for _, s := range c.Legend {
		legend = append(legend, s.Key)
	}
	if want := []string{"created_cumulative", "created", "closed"}; !slices.Equal(legend, want) {
		t.Errorf("legend = %v, want card order %v", legend, want)
	}

	split := linesCard(task.StatsLine{Key: "high", Values: []int{1, 2, 2}, Cumulative: true}, task.StatsLine{Key: "low", Values: []int{0, 0, 1}, Cumulative: true})
	split.Field = "priority"
	sc := newStatsChart(split)
	if len(sc.Groups) != 1 || !sc.Groups[0].Cumulative || len(sc.Groups[0].Lines) != 2 {
		t.Errorf("cumulative split groups = %+v, want one cumulative group of two", sc.Groups)
	}
}

func TestStatsChartZeroAndSingleDay(t *testing.T) {
	zero := newStatsChart(linesCard(task.StatsLine{Key: "closed", Values: []int{0, 0}}))
	g := zero.Groups[0]
	if g.Max != 1 || g.Peak != 0 || g.Lines[0].Points != "1,1 3,1" {
		t.Errorf("all-zero group = %+v, want max 1, peak 0, points on the baseline", g)
	}

	one := newStatsChart(linesCard(task.StatsLine{Key: "created", Values: []int{3}}))
	if one.Width != 2 || one.Groups[0].Lines[0].Points != "0,0 2,0" {
		t.Errorf("one-day chart = width %d, points %q; want 2, a flat segment 0,0 2,0", one.Width, one.Groups[0].Lines[0].Points)
	}
	if len(one.Days) != 1 || one.Days[0].X != 0 {
		t.Errorf("one-day hover columns = %+v", one.Days)
	}
}

func TestStatsChartColours(t *testing.T) {
	plain := newStatsChart(linesCard(
		task.StatsLine{Key: "created", Values: []int{1}},
		task.StatsLine{Key: "closed_cumulative", Values: []int{1}},
	))
	if plain.Legend[0].Color != "series-created" || plain.Legend[1].Color != "series-closed_cumulative" {
		t.Errorf("plain colours = %+v", plain.Legend)
	}

	var lines []task.StatsLine
	for i := range 9 {
		lines = append(lines, task.StatsLine{Key: "user" + strconv.Itoa(i), Values: []int{1}})
	}
	card := linesCard(lines...)
	card.Field = "created_by"
	split := newStatsChart(card)
	if split.Legend[0].Color != "series-0" || split.Legend[7].Color != "series-7" || split.Legend[8].Color != "series-0" {
		t.Errorf("split colours = %s, %s, %s; want series-0, series-7, series-0",
			split.Legend[0].Color, split.Legend[7].Color, split.Legend[8].Color)
	}
}

func TestStatsChartHiddenAndTitles(t *testing.T) {
	c := newStatsChart(linesCard(
		task.StatsLine{Key: "created", Values: []int{1, 2}},
		task.StatsLine{Key: "closed", Values: []int{0, 3}, Hidden: true},
	))
	if c.Legend[0].Hidden || !c.Legend[1].Hidden || !c.Groups[0].Lines[1].Hidden {
		t.Errorf("hidden flags = legend %+v, lines %+v", c.Legend, c.Groups[0].Lines)
	}
	if c.Groups[0].Max != 3 {
		t.Errorf("max = %d, want 3: a hidden line still sets the scale", c.Groups[0].Max)
	}
	want := []StatsDayView{
		{X: 0, Title: "Oct 5 · created 1 · closed 0"},
		{X: 2, Title: "Oct 6 · created 2 · closed 3"},
	}
	if !slices.Equal(c.Days, want) {
		t.Errorf("days = %+v\nwant %+v", c.Days, want)
	}
}

func TestStatsChartNoLines(t *testing.T) {
	card := task.StatsCard{ID: "x", Kind: config.StatsKindLines, Days: statsDays(3)}
	if c := newStatsChart(card); c != nil {
		t.Errorf("chart = %+v, want nil without lines", c)
	}
	if got := newStatsCards([]task.StatsCard{card}); len(got) != 1 || got[0].Chart != nil {
		t.Errorf("cards = %+v, want the card without a chart", got)
	}
}

// TestTodoSubtaskNestsUnderInProgressParent pins the one case that renders
// twice: the parent's card lists the work still ahead of it, and the
// subtask keeps its own card in the To do queue.
func TestTodoSubtaskNestsUnderInProgressParent(t *testing.T) {
	snap := boardOf(
		tk("p", "", task.StatusInProgress, task.PriorityHigh, fixedNow),
		tk("s", "p", task.StatusTodo, task.PriorityHigh, fixedNow),
	)
	snap.Phases["p"] = task.PhaseImplementation
	v := newBoardView(snap, nil, fixedNow, 5)

	inProgress := column(v, "in_progress")
	if got := cardIDs(inProgress.Cards); !slices.Equal(got, []string{"p"}) {
		t.Fatalf("in progress = %v, want [p]", got)
	}
	if got := cardIDs(inProgress.Cards[0].Subtasks); !slices.Equal(got, []string{"s"}) {
		t.Errorf("p nests %v, want [s]: a todo subtask shows on its parent's card", got)
	}
	if got := cardIDs(column(v, "todo").Cards); !slices.Equal(got, []string{"s"}) {
		t.Errorf("todo = %v, want [s]: the subtask keeps its own card", got)
	}
	if impl := lane(v, task.PhaseImplementation); impl.Count != 1 {
		t.Errorf("implementation lane Count = %d, want 1: a todo subtask counts in To do", impl.Count)
	}
	if inProgress.Count != 1 {
		t.Errorf("in progress Count = %d, want 1", inProgress.Count)
	}
}
