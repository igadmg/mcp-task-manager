package task

import (
	"reflect"
	"slices"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Berlin on machines without a zoneinfo database

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// statsNow is 2026-10-06 12:00 UTC, the reference instant of these tests.
var statsNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

var statsTypes = []string{"feature", "bug"}

// st builds an index entry for the stats tests.
func st(status Status, priority Priority, typ string) *Task {
	return &Task{ID: string(priority) + typ, Status: status, Priority: priority, Type: typ, CreatedAt: statsNow.Add(-30 * 24 * time.Hour)}
}

func closedAt(t *Task, at time.Time) *Task {
	t.Status = StatusDone
	t.ClosedAt = &at
	t.UpdatedAt = at
	return t
}

func barsCard(field string) config.StatsCard {
	return config.StatsCard{ID: "bars-" + field, Kind: config.StatsKindBars, Title: field, Field: field}
}

func linesCard(days int, lines ...string) config.StatsCard {
	return config.StatsCard{ID: "lines", Kind: config.StatsKindLines, Title: "lines", Days: days, Lines: lines}
}

func oneCard(t *testing.T, all []*Task, c config.StatsCard, now time.Time) StatsCard {
	t.Helper()
	return oneCardRecent(t, all, c, now, 0)
}

// oneCardRecent is oneCard with the board's closures window (web.recent_hours)
// given; 0 means the default.
func oneCardRecent(t *testing.T, all []*Task, c config.StatsCard, now time.Time, recentHours int) StatsCard {
	t.Helper()
	got := computeStats(all, []config.StatsCard{c}, statsTypes, now, recentHours)
	if len(got) != 1 {
		t.Fatalf("computeStats() returned %d cards, want 1", len(got))
	}
	return got[0]
}

func TestStatsBarsCountsByStatus(t *testing.T) {
	all := []*Task{
		st(StatusTodo, PriorityHigh, "feature"),
		st(StatusInProgress, PriorityHigh, "feature"),
		closedAt(st("", PriorityHigh, "bug"), statsNow.Add(-48*time.Hour)),
		closedAt(st("", PriorityHigh, "bug"), statsNow.Add(-time.Hour)),
		st(StatusTodo, PriorityLow, "bug"),
	}
	all[1].ParentID = "x" // a subtask counts like any task

	card := oneCard(t, all, barsCard("priority"), statsNow)
	want := []StatsBar{
		{Value: "high", Total: 4, Done: 2, InProgress: 1, Todo: 1, ClosedRecently: 1},
		{Value: "low", Total: 1, Todo: 1},
	}
	if !reflect.DeepEqual(card.Bars, want) {
		t.Errorf("Bars = %+v, want %+v", card.Bars, want)
	}
	if card.ID != "bars-priority" || card.Kind != config.StatsKindBars || card.Field != "priority" {
		t.Errorf("card header = %q %q %q", card.ID, card.Kind, card.Field)
	}
}

func TestStatsBarsValueOrder(t *testing.T) {
	tests := []struct {
		field string
		tasks []*Task
		want  []string
	}{
		{"priority", []*Task{
			st(StatusTodo, PriorityLow, "bug"), st(StatusTodo, "urgent", "bug"), st(StatusTodo, PriorityCritical, "bug"),
			st(StatusTodo, "asap", "bug"), st(StatusTodo, PriorityMedium, "bug"),
		}, []string{"critical", "medium", "low", "asap", "urgent"}},
		{"type", []*Task{
			st(StatusTodo, PriorityLow, "chore"), st(StatusTodo, PriorityLow, "bug"), st(StatusTodo, PriorityLow, "feature"),
			st(StatusTodo, PriorityLow, ""),
		}, []string{"feature", "bug", "chore"}},
		{"status", []*Task{
			closedAt(st("", PriorityLow, "bug"), statsNow), st(StatusTodo, PriorityLow, "bug"), st(StatusInProgress, PriorityLow, "bug"),
		}, []string{"todo", "in_progress", "done"}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			card := oneCard(t, tt.tasks, barsCard(tt.field), statsNow)
			var got []string
			for _, b := range card.Bars {
				got = append(got, b.Value)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("values = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatsBarsCreatedByAlphabeticalEmptyDropped(t *testing.T) {
	a, b, c := st(StatusTodo, PriorityLow, "bug"), st(StatusTodo, PriorityLow, "bug"), st(StatusTodo, PriorityLow, "bug")
	a.CreatedBy, b.CreatedBy = "zoe", "adam"
	card := oneCard(t, []*Task{a, b, c}, barsCard("created_by"), statsNow)
	if len(card.Bars) != 2 || card.Bars[0].Value != "adam" || card.Bars[1].Value != "zoe" {
		t.Errorf("Bars = %+v, want adam then zoe, no empty creator", card.Bars)
	}
}

func TestStatsBarsResolutionCountsDoneOnly(t *testing.T) {
	legacy := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-time.Hour)) // no resolution: completed
	dup := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-72*time.Hour))
	dup.Resolution = ResolutionDuplicate
	odd := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-72*time.Hour))
	odd.Resolution = "abandoned"
	open := st(StatusInProgress, PriorityLow, "bug")
	open.Resolution = ResolutionWontfix // a stray value on an open task does not count

	card := oneCard(t, []*Task{dup, open, odd, legacy}, barsCard("resolution"), statsNow)
	want := []StatsBar{
		{Value: "completed", Total: 1, Done: 1, ClosedRecently: 1},
		{Value: "duplicate", Total: 1, Done: 1},
		{Value: "abandoned", Total: 1, Done: 1},
	}
	if !reflect.DeepEqual(card.Bars, want) {
		t.Errorf("Bars = %+v, want %+v", card.Bars, want)
	}
}

func TestStatsBarsRecentWindow(t *testing.T) {
	inside := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-23*time.Hour))
	edge := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-24*time.Hour))
	future := closedAt(st("", PriorityLow, "bug"), statsNow.Add(time.Hour))
	atNow := closedAt(st("", PriorityLow, "bug"), statsNow)
	// Legacy done task without closed_at: updated_at stands in.
	legacy := st(StatusDone, PriorityLow, "bug")
	legacy.UpdatedAt = statsNow.Add(-2 * time.Hour)
	// closed_at wins over a recent updated_at.
	edited := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-72*time.Hour))
	edited.UpdatedAt = statsNow.Add(-time.Minute)
	// An open task touched recently is not a closure.
	open := st(StatusTodo, PriorityLow, "bug")
	open.UpdatedAt = statsNow.Add(-time.Minute)

	card := oneCard(t, []*Task{inside, edge, future, atNow, legacy, edited, open}, barsCard("priority"), statsNow)
	if got := card.Bars[0].ClosedRecently; got != 3 {
		t.Errorf("ClosedRecently = %d, want 3 (inside, at now, legacy fallback)", got)
	}
	if got := card.Bars[0].Done; got != 6 {
		t.Errorf("Done = %d, want 6", got)
	}
}

func TestStatsUnknownFieldAndKind(t *testing.T) {
	all := []*Task{st(StatusTodo, PriorityLow, "bug")}
	cards := []config.StatsCard{
		barsCard("assignee"),
		{ID: "pie", Kind: "pie", Title: "Pie"},
		{ID: "split", Kind: config.StatsKindLines, Days: 3, SplitBy: "assignee", Metric: config.StatsLineCreated},
		{ID: "metric", Kind: config.StatsKindLines, Days: 3, SplitBy: "priority", Metric: "opened"},
	}
	got := computeStats(all, cards, statsTypes, statsNow, 0)
	if len(got) != 4 {
		t.Fatalf("computeStats() returned %d cards, want 4 (none dropped)", len(got))
	}
	for i, c := range got {
		if c.ID != cards[i].ID || len(c.Bars) != 0 || len(c.Lines) != 0 {
			t.Errorf("card %d = %+v, want id %q and no data", i, c, cards[i].ID)
		}
	}
	if len(got[2].Days) != 3 {
		t.Errorf("split card Days = %d, want 3", len(got[2].Days))
	}
}

func TestStatsDaysAreLocalMidnights(t *testing.T) {
	loc := time.FixedZone("UTC+3", 3*3600)
	now := time.Date(2026, 10, 2, 1, 30, 0, 0, loc) // 2026-10-01 22:30 UTC
	card := oneCard(t, nil, linesCard(3, config.StatsLineCreated), now)

	want := []time.Time{
		time.Date(2026, 9, 30, 0, 0, 0, 0, loc),
		time.Date(2026, 10, 1, 0, 0, 0, 0, loc),
		time.Date(2026, 10, 2, 0, 0, 0, 0, loc),
	}
	if !reflect.DeepEqual(card.Days, want) {
		t.Errorf("Days = %v, want %v (month rollover, today last)", card.Days, want)
	}
}

func TestStatsLinesBucketAcrossDST(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	// DST ends on 2026-10-25 (a 25-hour day). Window: 24th to 26th.
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, berlin)
	created := func(at time.Time) *Task {
		x := st(StatusTodo, PriorityLow, "bug")
		x.CreatedAt = at
		return x
	}
	all := []*Task{
		created(time.Date(2026, 10, 25, 22, 30, 0, 0, time.UTC)), // 23:30 on the 25th in Berlin
		created(time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC)), // 00:30 on the 26th in Berlin
		created(time.Date(2026, 10, 24, 0, 30, 0, 0, berlin)),
		created(time.Date(2026, 10, 23, 23, 59, 0, 0, berlin)), // before the window
		created(time.Date(2026, 10, 27, 0, 1, 0, 0, berlin)),   // after today
		created(time.Time{}),
	}
	card := oneCard(t, all, linesCard(3, config.StatsLineCreated), now)
	if got, want := card.Lines[0].Values, []int{1, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("created = %v, want %v", got, want)
	}
	if got := card.Days[2]; !got.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, berlin)) {
		t.Errorf("Days[2] = %v, want the 26th at local midnight", got)
	}
}

func TestStatsLinesSeries(t *testing.T) {
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	mk := func(created time.Time) *Task {
		x := st(StatusTodo, PriorityLow, "bug")
		x.CreatedAt = created
		return x
	}
	a := closedAt(mk(day(4, 9)), day(6, 8))
	b := closedAt(mk(day(4, 10)), day(4, 11))
	c := mk(day(6, 1))
	legacy := mk(day(1, 1)) // created before the window
	legacy.Status, legacy.UpdatedAt = StatusDone, day(5, 3)
	all := []*Task{a, b, c, legacy}

	c4 := linesCard(4,
		config.StatsLineCreated, config.StatsLineClosed, config.StatsLineCreatedCumulative,
		config.StatsLineClosedCumulative, "opened", config.StatsLineCreated)
	c4.Hidden = []string{config.StatsLineClosedCumulative}
	card := oneCard(t, all, c4, statsNow)

	want := []StatsLine{
		{Key: "created", Values: []int{0, 2, 0, 1}},
		{Key: "closed", Values: []int{0, 1, 1, 1}},
		{Key: "created_cumulative", Values: []int{0, 2, 2, 3}, Cumulative: true},
		{Key: "closed_cumulative", Values: []int{0, 1, 2, 3}, Hidden: true, Cumulative: true},
	}
	if !reflect.DeepEqual(card.Lines, want) {
		t.Errorf("Lines = %+v, want %+v", card.Lines, want)
	}
	if card.Field != "" {
		t.Errorf("Field = %q, want empty for a plain card", card.Field)
	}
}

func TestStatsLinesSplitBy(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }
	mk := func(p Priority, closed time.Time) *Task {
		x := closedAt(st("", p, "bug"), closed)
		x.CreatedAt = day(1)
		return x
	}
	all := []*Task{
		mk(PriorityLow, day(5)), mk(PriorityLow, day(6)), mk(PriorityCritical, day(6)),
		mk(PriorityHigh, day(1)), // outside the window: no line for high
		st(StatusInProgress, PriorityMedium, "bug"),
	}
	split := config.StatsCard{ID: "s", Kind: config.StatsKindLines, Days: 3, SplitBy: "priority",
		Metric: config.StatsLineClosed, Hidden: []string{"low"}}

	card := oneCard(t, all, split, statsNow)
	want := []StatsLine{
		{Key: "critical", Values: []int{0, 0, 1}},
		{Key: "low", Values: []int{0, 1, 1}, Hidden: true},
	}
	if !reflect.DeepEqual(card.Lines, want) {
		t.Errorf("Lines = %+v, want %+v", card.Lines, want)
	}
	if card.Field != "priority" {
		t.Errorf("Field = %q, want priority", card.Field)
	}

	split.Metric = config.StatsLineClosedCumulative
	card = oneCard(t, all, split, statsNow)
	if got, want := card.Lines[1], (StatsLine{Key: "low", Values: []int{0, 1, 2}, Hidden: true, Cumulative: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("cumulative low = %+v, want %+v", got, want)
	}

	// Resolution split counts done tasks only.
	split.SplitBy, split.Metric = "resolution", config.StatsLineCreated
	open := st(StatusTodo, PriorityLow, "bug")
	open.CreatedAt = day(6)
	done := closedAt(st("", PriorityLow, "bug"), day(6))
	done.CreatedAt = day(6)
	card = oneCard(t, []*Task{open, done}, split, statsNow)
	if len(card.Lines) != 1 || card.Lines[0].Key != "completed" || card.Lines[0].Values[2] != 1 {
		t.Errorf("resolution split = %+v, want one completed line", card.Lines)
	}
}

func TestStatsCardOrderFollowsConfig(t *testing.T) {
	got := computeStats(nil, config.DefaultStatsCards(), statsTypes, statsNow, 0)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if want := []string{"bars-priority", "bars-type", "bars-resolution", "lines-14d"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if got[0].Title != "Priority" || len(got[3].Days) != 14 || len(got[3].Lines) != 2 {
		t.Errorf("cards = %+v", got)
	}
}

func TestBoardSnapshotStatsUseTheClock(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*3600)
	fixed := time.Date(2026, 10, 6, 20, 0, 0, 0, loc) // 2026-10-07 01:00 UTC
	svc := newConcurrentService()
	WithClock(func() time.Time { return fixed })(svc)
	seedBacklog(t, svc)

	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	if !snap.TakenAt.Equal(fixed) || snap.TakenAt.Location() != time.UTC {
		t.Errorf("TakenAt = %v, want %v in UTC", snap.TakenAt, fixed)
	}
	if len(snap.Stats) != 4 {
		t.Fatalf("len(Stats) = %d, want the 4 default cards", len(snap.Stats))
	}
	total := 0
	for _, b := range snap.Stats[0].Bars {
		total += b.Total
	}
	if total != len(snap.Tasks) {
		t.Errorf("priority bars total %d, want %d (every task once)", total, len(snap.Tasks))
	}
	if today := snap.Stats[3].Days[13]; !today.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, loc)) {
		t.Errorf("today = %v, want the clock's local day", today)
	}
}

func TestWithClockNilKeepsDefault(t *testing.T) {
	svc := newConcurrentService()
	WithClock(nil)(svc)
	if svc.now == nil {
		t.Fatal("now is nil after WithClock(nil)")
	}
	snap, err := svc.BoardSnapshot()
	if err != nil {
		t.Fatalf("BoardSnapshot() error = %v", err)
	}
	if snap.TakenAt.IsZero() {
		t.Error("TakenAt is zero with the default clock")
	}
}

// The config package cannot ask this one for its vocabularies - it is the
// one this package imports - so it declares them itself. These two tests are
// what keeps the copies honest: adding a field here without listing it there
// makes it unconfigurable, and listing one there without implementing it
// here makes the diagnostic promise a field that silently yields nothing.

func TestStatsFieldsMatchConfig(t *testing.T) {
	declared := config.StatsFields()
	slices.Sort(declared)

	implemented := make([]string, 0, len(statsFields))
	for name := range statsFields {
		implemented = append(implemented, name)
	}
	slices.Sort(implemented)

	if !slices.Equal(declared, implemented) {
		t.Errorf("config.StatsFields() = %q, statsFields = %q", declared, implemented)
	}
}

func TestStatsLineNamesMatchConfig(t *testing.T) {
	for _, name := range config.StatsLineNames() {
		if _, _, ok := statsMetric(name); !ok {
			t.Errorf("config.StatsLineNames() offers %q, statsMetric rejects it", name)
		}
	}
	for _, name := range []string{"", "opened", "created_total", "CREATED"} {
		if _, _, ok := statsMetric(name); ok {
			t.Errorf("statsMetric accepts %q, which config does not offer", name)
		}
	}
}

// createdAt dates a task, which is what the arrivals segment counts.
func createdAt(t *Task, at time.Time) *Task {
	t.CreatedAt = at
	return t
}

// TestStatsBarsCreatedRecentlyWindow is the mirror of
// TestStatsBarsClosedRecentlyWindow: the arrivals segment counts todo tasks
// created inside the window, excludes a future date the same way, and counts
// only tasks that are still waiting.
func TestStatsBarsCreatedRecentlyWindow(t *testing.T) {
	inside := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow.Add(-time.Hour))
	atNow := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow)
	// Exactly on the boundary is outside: the comparison is strictly After.
	edge := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow.Add(-24*time.Hour))
	older := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow.Add(-48*time.Hour))
	// A task dated tomorrow has not arrived yet.
	future := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow.Add(time.Hour))
	// Created a minute ago but already moving, or already done: not waiting.
	started := createdAt(st(StatusInProgress, PriorityLow, "bug"), statsNow.Add(-time.Minute))
	finished := closedAt(createdAt(st("", PriorityLow, "bug"), statsNow.Add(-time.Minute)), statsNow)

	all := []*Task{inside, atNow, edge, older, future, started, finished}
	card := oneCard(t, all, barsCard("priority"), statsNow)
	bar := card.Bars[0]

	if bar.CreatedRecently != 2 {
		t.Errorf("CreatedRecently = %d, want 2 (inside and at now)", bar.CreatedRecently)
	}
	if bar.Todo != 5 {
		t.Errorf("Todo = %d, want 5", bar.Todo)
	}
	// The two invariants the task asks for.
	if bar.CreatedRecently > bar.Todo {
		t.Errorf("CreatedRecently %d exceeds Todo %d", bar.CreatedRecently, bar.Todo)
	}
	if sum := bar.Done + bar.InProgress + bar.Todo; sum != bar.Total {
		t.Errorf("Done+InProgress+Todo = %d, want Total %d", sum, bar.Total)
	}
}

// TestStatsBarsWindowsAreHonoured: both windows come from the card, and they
// are independent of each other.
func TestStatsBarsWindowsAreHonoured(t *testing.T) {
	// Two days back: outside a 24 h window, inside a 72 h one.
	waiting := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow.Add(-48*time.Hour))
	shipped := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-48*time.Hour))

	all := []*Task{waiting, shipped}

	// Defaults: 24 h on both ends, so neither is highlighted.
	base := oneCard(t, all, barsCard("priority"), statsNow).Bars[0]
	if base.CreatedRecently != 0 || base.ClosedRecently != 0 {
		t.Errorf("with the default window: created=%d closed=%d, want 0 and 0",
			base.CreatedRecently, base.ClosedRecently)
	}

	// A wider arrivals window picks up the waiting task and only that.
	card := barsCard("priority")
	card.NewHours = 72
	wide := oneCard(t, all, card, statsNow).Bars[0]
	if wide.CreatedRecently != 1 {
		t.Errorf("new_hours: 72 → CreatedRecently = %d, want 1", wide.CreatedRecently)
	}
	if wide.ClosedRecently != 0 {
		t.Errorf("new_hours must not widen the closures window: ClosedRecently = %d", wide.ClosedRecently)
	}

	// And the other end, independently: the closures window is the board's
	// (web.recent_hours), handed to computeStats, not a card's own key.
	other := oneCardRecent(t, all, barsCard("priority"), statsNow, 72)
	if other.Bars[0].ClosedRecently != 1 {
		t.Errorf("recent_hours: 72 → ClosedRecently = %d, want 1", other.Bars[0].ClosedRecently)
	}
	if other.Bars[0].CreatedRecently != 0 {
		t.Errorf("recent_hours must not widen the arrivals window: CreatedRecently = %d", other.Bars[0].CreatedRecently)
	}
	if other.RecentHours != 72 {
		t.Errorf("StatsCard.RecentHours = %d, want the board window 72", other.RecentHours)
	}

	// A card's own (deprecated) RecentHours is never read.
	card = barsCard("priority")
	card.RecentHours = 72
	if got := oneCard(t, all, card, statsNow).Bars[0].ClosedRecently; got != 0 {
		t.Errorf("a card-level RecentHours moved the window: ClosedRecently = %d, want 0", got)
	}
}

// TestClosedWithin pins the one predicate behind both the bars and the
// snapshot's ClosedRecently set.
func TestClosedWithin(t *testing.T) {
	window := 24 * time.Hour
	legacy := st(StatusDone, PriorityLow, "bug") // no closed_at: updated_at stands in
	legacy.UpdatedAt = statsNow.Add(-2 * time.Hour)
	edited := closedAt(st("", PriorityLow, "bug"), statsNow.Add(-72*time.Hour)) // closed_at wins
	edited.UpdatedAt = statsNow.Add(-time.Minute)
	open := st(StatusTodo, PriorityLow, "bug")
	open.UpdatedAt = statsNow.Add(-time.Minute)

	for _, tc := range []struct {
		name string
		task *Task
		want bool
		at   time.Time
	}{
		{"inside", closedAt(st("", PriorityLow, "bug"), statsNow.Add(-23*time.Hour)), true, statsNow.Add(-23 * time.Hour)},
		{"exactly now", closedAt(st("", PriorityLow, "bug"), statsNow), true, statsNow},
		{"on the edge is outside", closedAt(st("", PriorityLow, "bug"), statsNow.Add(-24*time.Hour)), false, time.Time{}},
		{"future", closedAt(st("", PriorityLow, "bug"), statsNow.Add(time.Hour)), false, time.Time{}},
		{"updated_at fallback", legacy, true, legacy.UpdatedAt},
		{"closed_at wins over updated_at", edited, false, time.Time{}},
		{"an open task is no closure", open, false, time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, ok := closedWithin(tc.task, statsNow, window)
			if ok != tc.want {
				t.Fatalf("closedWithin = %v, want %v", ok, tc.want)
			}
			if ok && !at.Equal(tc.at) {
				t.Errorf("close time = %v, want %v", at, tc.at)
			}
		})
	}
}

// TestStatsBarsZeroWindowIsTheDefault: a card built by hand, or one that
// skipped the config's normalization, must not silently highlight nothing.
func TestStatsBarsZeroWindowIsTheDefault(t *testing.T) {
	fresh := createdAt(st(StatusTodo, PriorityLow, "bug"), statsNow.Add(-time.Hour))
	card := barsCard("priority") // RecentHours and NewHours are both 0 here
	if got := oneCard(t, []*Task{fresh}, card, statsNow).Bars[0].CreatedRecently; got != 1 {
		t.Errorf("CreatedRecently = %d with an unset window, want the 24 h default to apply", got)
	}
}
