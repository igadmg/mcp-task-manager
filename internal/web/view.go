package web

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// The view models are plain strings on purpose. No *task.Task ever reaches a
// template: a template that could reach the model would be tempted to render
// relations it cannot resolve titles for, and every field here is escaped as
// ordinary text by html/template.

// ProjectView identifies the backlog the board is showing.
type ProjectView struct {
	Resolved  bool
	Root      string
	TasksDir  string
	Source    string
	TaskCount int
}

// BoardView is one whole kanban render.
type BoardView struct {
	Project     ProjectView
	Columns     []ColumnView
	DangerZone  []DangerItem
	PollSeconds int
	Generated   string
}

// ColumnView is one status column.
type ColumnView struct {
	Status string
	Title  string
	Count  int
	Cards  []CardView
	// Lanes are the In progress column's phase lanes (virtual sub-columns),
	// in workflow order; nil for every other column. They partition Cards.
	Lanes []PhaseLaneView
	// Stats are the Done column's statistics cards, in config order; nil
	// for every other column. Done renders them instead of task cards.
	Stats []StatsCardView
}

// StatsCardView is one statistics card of the Done column. ID is the
// configured card id, the stable key of data-stats-card. Kind picks the
// partial: a bars card fills Bars, a lines card Chart (nil when the card has
// no lines to draw).
type StatsCardView struct {
	ID    string
	Kind  string
	Title string
	Bars  []StatsBarView
	Chart *StatsChartView
}

// StatsChartView is a lines card's chart. Its coordinates are data, like a
// bar's: day i spans x 2i..2i+2 with its points at 2i+1, so Width is twice
// the day count, and each group's viewBox is its Max high with y = Max -
// value. Go prints integers only and the browser scales them.
type StatsChartView struct {
	Width  int
	First  string // the first day, "Jan 2"
	Last   string // the last day: today
	Groups []StatsLineGroupView
	Legend []StatsSeriesView
	Days   []StatsDayView
}

// StatsLineGroupView is the lines that share one y scale: the per-day
// counts, or the running totals. Peak is the largest value of any of its
// lines, hidden ones included, and Max the viewBox height: Peak, at least 1,
// so an all-zero group is a flat line on the baseline.
type StatsLineGroupView struct {
	Cumulative bool
	Width      int
	Peak       int
	Max        int
	Lines      []StatsSeriesView
}

// StatsSeriesView is one line. Key is the line name or the split value
// (data-stats-line); Color is its series-<…> class, picked from a fixed set
// so a key never becomes a class; Points is "x,y x,y …" in data units.
type StatsSeriesView struct {
	Key    string
	Color  string
	Hidden bool
	Points string
}

// StatsDayView is one day's hover column, starting at X and 2 wide; Title
// lists every line's value that day.
type StatsDayView struct {
	X     int
	Title string
}

// statsSplitColors is the number of series-<i> classes a split card cycles
// through.
const statsSplitColors = 8

// StatsBarView is one value's row of a bars card. Every number is a task
// count: the bar is an SVG whose viewBox is Total wide, so the browser
// scales the segments and Go never computes geometry. Recent (closed in the
// last 24 h) is part of Done and drawn over its end, from RecentX; Open is
// InProgress + Todo, and TodoX is where the todo segment starts.
type StatsBarView struct {
	Value      string
	Total      int
	Done       int
	InProgress int
	Todo       int
	Recent     int
	Open       int
	RecentX    int
	TodoX      int
}

// PhaseLaneView is one workflow-phase lane inside In progress. Phase is
// the raw value ("planning"): it names the lane-<phase> CSS class and the
// data-phase attribute, and is the heading (the template upper-cases it).
// Count is the in-progress tasks on its cards, nested subtasks included,
// so the lane counts add up to the column's.
type PhaseLaneView struct {
	Phase string
	Count int
	Cards []CardView
}

// CardView is one task on the board.
type CardView struct {
	ID           string
	Title        string
	Priority     string
	Type         string
	Status       string
	PriorityRank int
	IsSubtask    bool
	ParentID     string
	Blocked      bool
	Blockers     []BlockerView
	SubtaskTotal int
	SubtaskDone  int
	Resolution   string
	InProgress   bool
	UpdatedAgo   string
	// CreatedBy is the task's creator, empty for tasks that predate the
	// field; CreatedAgo is relative, CreatedAt the full time for a tooltip.
	CreatedBy  string
	CreatedAgo string
	CreatedAt  string
	// Phase is the phase of the latest started run, set only for an
	// in-progress task with phase records. PhaseBy started that run, and
	// PhaseAgo is when it started (PhaseOpen) or finished.
	Phase     string
	PhaseBy   string
	PhaseAgo  string
	PhaseOpen bool
	// Tokens is the humanized total over finished runs ("81.2k"), empty
	// when none reported any; TokensExact is the plain count.
	Tokens      string
	TokensExact string
	// Branch is the git branch to show on the card: the final branch once
	// the task has been delivered, its wip branch before that.
	Branch string
	// Subtasks are nested when they sit in the same column as this card,
	// plus the todo subtasks of an in-progress card, which also keep their
	// own card in To do; otherwise they render standalone in their column.
	Subtasks []CardView

	createdAt time.Time // sorting only
}

// BlockerView is one unresolved blocker.
type BlockerView struct {
	ID     string
	Status string
	Title  string
}

// DangerItem is one in-progress task, shown in the danger-zone banner.
type DangerItem struct {
	ID         string
	Title      string
	Type       string
	UpdatedAgo string
}

// DetailView is one task's full page or side panel.
type DetailView struct {
	Project     ProjectView
	Card        CardView
	Description string
	Archived    bool
	Missing     bool
	Relations   []RelationView
	// Files are the attached files, without the phase records that
	// Phases shows.
	Files          []string
	Phases         []PhaseView
	TotalTokens    string
	UpdatedAt      string
	ClosedAt       string
	VerifiedAt     string
	ResolutionNote string
	// Git branch data, empty for a task git branching never touched.
	// Commits are shown by a 12-character prefix, with the full SHA kept
	// for the tooltip.
	Branch            string
	FinalBranch       string
	BaseBranch        string
	StartCommit       string
	StartCommitShort  string
	SquashCommit      string
	SquashCommitShort string
}

// PhaseView is one phase's run history in the detail view.
type PhaseView struct {
	Phase string
	Runs  []PhaseRunView
}

// PhaseRunView is one run: N is its 1-based number within the phase, and
// Finished, FinishedBy and Tokens are empty while it is open - an empty
// Finished is how the template tells an open run.
type PhaseRunView struct {
	N           int
	Started     string
	StartedBy   string
	Finished    string
	FinishedBy  string
	Tokens      string
	TokensExact string
	Note        string
}

// RelationView is one edge, seen from the task being displayed.
type RelationView struct {
	Type       string
	Direction  string // "outgoing" | "incoming"
	OtherID    string
	OtherTitle string
}

// columns fixes the board's column order and headings.
var columns = []struct {
	Status task.Status
	Title  string
}{
	{task.StatusTodo, "To do"},
	{task.StatusInProgress, "In progress"},
	{task.StatusDone, "Done"},
}

const timeFormat = "2006-01-02 15:04"

// dayFormat labels a statistics day: "Oct 6".
const dayFormat = "Jan 2"

// newBoardView maps a snapshot onto the board. Pure: everything it needs was
// already read under the service lock.
func newBoardView(snap *task.BoardSnapshot, cfg *config.Config, now time.Time, poll int) BoardView {
	v := BoardView{
		Project:     newProjectView(cfg, len(snap.Tasks)),
		PollSeconds: poll,
		Generated:   now.Format("15:04:05"),
	}

	byID := make(map[string]*CardView, len(snap.Tasks))
	for _, t := range snap.Tasks {
		card := newCardView(t, snap, now)
		byID[t.ID] = &card
	}

	// A card lives in the column of its own status, and nests inside its
	// parent when the parent sits in that same column - otherwise the same
	// task would show up in a column it is not in.
	//
	// One case nests and keeps its own card: a todo subtask of an
	// in-progress parent. The parent's card then lists the work still
	// ahead of it, while the subtask stays in the To do queue, which is
	// what every column count and lane count is computed from.
	roots := make(map[string][]*CardView, len(columns))
	nested := make(map[string][]*CardView)
	for _, t := range snap.Tasks {
		card := byID[t.ID]
		parent, hasParent := byID[t.ParentID]
		if hasParent && parent.Status == card.Status {
			nested[t.ParentID] = append(nested[t.ParentID], card)
			continue
		}
		roots[card.Status] = append(roots[card.Status], card)
		if hasParent && parent.InProgress && card.Status == string(task.StatusTodo) {
			nested[t.ParentID] = append(nested[t.ParentID], card)
		}
	}
	for parentID, kids := range nested {
		sortCards(kids)
		byID[parentID].Subtasks = deref(kids)
	}

	for _, col := range columns {
		cards := roots[string(col.Status)]
		sortCards(cards)
		count := 0
		for _, t := range snap.Tasks {
			if t.Status == col.Status {
				count++
			}
		}
		cv := ColumnView{
			Status: string(col.Status),
			Title:  col.Title,
			Count:  count,
		}
		switch col.Status {
		case task.StatusInProgress:
			cv.Cards = deref(cards)
			cv.Lanes = newPhaseLanes(cards, snap)
		case task.StatusDone:
			// Done shows statistics, not tasks; its tasks are still counted.
			cv.Stats = newStatsCards(snap.Stats)
		default:
			cv.Cards = deref(cards)
		}
		v.Columns = append(v.Columns, cv)
	}

	for _, t := range snap.Tasks {
		if t.Status != task.StatusInProgress {
			continue
		}
		v.DangerZone = append(v.DangerZone, DangerItem{
			ID:         t.ID,
			Title:      t.Title,
			Type:       t.Type,
			UpdatedAgo: humanizeAgo(now, t.UpdatedAt),
		})
	}
	sort.SliceStable(v.DangerZone, func(i, j int) bool { return v.DangerZone[i].ID < v.DangerZone[j].ID })

	return v
}

// newPhaseLanes buckets the In progress root cards, already sorted, into
// one lane per task.Phases entry; lane i holds phase Order() i. A card goes
// to the furthest phase of its group: itself plus its nested in-progress
// subtasks (nested todo subtasks belong to To do). Bucketing is
// stable, so each lane keeps the column's priority, age, id order.
func newPhaseLanes(roots []*CardView, snap *task.BoardSnapshot) []PhaseLaneView {
	var lanes []PhaseLaneView
	for _, p := range task.Phases() {
		lanes = append(lanes, PhaseLaneView{Phase: string(p)})
	}
	for _, card := range roots {
		rank, count := snap.Phases[card.ID].Order(), 1
		for _, kid := range card.Subtasks {
			// A nested todo subtask has its own To do card and no phase
			// of its own, so it counts and ranks there, not here.
			if !kid.InProgress {
				continue
			}
			rank = max(rank, snap.Phases[kid.ID].Order())
			count++
		}
		lanes[rank].Cards = append(lanes[rank].Cards, *card)
		lanes[rank].Count += count
	}
	return lanes
}

// newStatsCards maps the snapshot's statistics cards, in config order.
func newStatsCards(cards []task.StatsCard) []StatsCardView {
	var out []StatsCardView
	for _, c := range cards {
		card := StatsCardView{ID: c.ID, Kind: c.Kind, Title: c.Title}
		if c.Kind == config.StatsKindLines {
			card.Chart = newStatsChart(c)
			out = append(out, card)
			continue
		}
		if c.Kind != config.StatsKindBars {
			continue
		}
		for _, b := range c.Bars {
			if b.Total <= 0 { // an empty viewBox is invalid
				continue
			}
			card.Bars = append(card.Bars, StatsBarView{
				Value:      b.Value,
				Total:      b.Total,
				Done:       b.Done,
				InProgress: b.InProgress,
				Todo:       b.Todo,
				Recent:     b.ClosedRecently,
				Open:       b.InProgress + b.Todo,
				RecentX:    b.Done - b.ClosedRecently,
				TodoX:      b.Done + b.InProgress,
			})
		}
		out = append(out, card)
	}
	return out
}

// newStatsChart maps a lines card; nil when it has no lines (an unknown
// metric, or a split card with no value in the window). Per-day lines and
// running totals get a group each, per-day first, so neither flattens the
// other. A one-day window draws a flat segment across the day, since a
// one-point polyline paints nothing.
func newStatsChart(c task.StatsCard) *StatsChartView {
	if len(c.Lines) == 0 || len(c.Days) == 0 {
		return nil
	}
	n := len(c.Days)
	value := func(l task.StatsLine, i int) int {
		if i < len(l.Values) {
			return l.Values[i]
		}
		return 0
	}
	chart := &StatsChartView{
		Width: 2 * n,
		First: c.Days[0].Format(dayFormat),
		Last:  c.Days[n-1].Format(dayFormat),
	}

	split := c.Field != ""
	groups := [2]StatsLineGroupView{{Width: 2 * n, Max: 1}, {Cumulative: true, Width: 2 * n, Max: 1}}
	member := make([]int, len(c.Lines))
	for li, l := range c.Lines {
		g := 0
		if l.Cumulative {
			g = 1
		}
		member[li] = g
		for i := range n {
			groups[g].Peak = max(groups[g].Peak, value(l, i))
		}
		groups[g].Max = max(groups[g].Peak, 1)
	}
	for li, l := range c.Lines {
		color := "series-" + l.Key
		if split {
			color = "series-" + strconv.Itoa(li%statsSplitColors)
		}
		g := &groups[member[li]]
		var pts []string
		if n == 1 {
			y := strconv.Itoa(g.Max - value(l, 0))
			pts = []string{"0," + y, "2," + y}
		} else {
			for i := range n {
				pts = append(pts, strconv.Itoa(2*i+1)+","+strconv.Itoa(g.Max-value(l, i)))
			}
		}
		s := StatsSeriesView{Key: l.Key, Color: color, Hidden: l.Hidden, Points: strings.Join(pts, " ")}
		g.Lines = append(g.Lines, s)
		chart.Legend = append(chart.Legend, s)
	}
	for _, g := range groups {
		if len(g.Lines) > 0 {
			chart.Groups = append(chart.Groups, g)
		}
	}

	for i, day := range c.Days {
		parts := []string{day.Format(dayFormat)}
		for _, l := range c.Lines {
			parts = append(parts, l.Key+" "+strconv.Itoa(value(l, i)))
		}
		chart.Days = append(chart.Days, StatsDayView{X: 2 * i, Title: strings.Join(parts, " · ")})
	}
	return chart
}

// unresolvedBoardView is what the board shows while no project is resolved
// yet, which only happens in MCP-server mode before the first tool call.
func unresolvedBoardView(poll int) BoardView {
	return BoardView{PollSeconds: poll}
}

func newProjectView(cfg *config.Config, taskCount int) ProjectView {
	p := ProjectView{Resolved: true, TaskCount: taskCount}
	if cfg == nil {
		return p
	}
	if r := cfg.Resolution; r != nil {
		p.Root, p.TasksDir, p.Source = r.Root, r.TasksDir, string(r.Source)
		return p
	}
	p.TasksDir = cfg.TasksDir()
	return p
}

func newCardView(t *task.Task, snap *task.BoardSnapshot, now time.Time) CardView {
	c := CardView{
		ID:           t.ID,
		Title:        t.Title,
		Priority:     string(t.Priority),
		Type:         t.Type,
		Status:       string(t.Status),
		PriorityRank: t.Priority.Order(),
		IsSubtask:    t.ParentID != "",
		ParentID:     t.ParentID,
		Resolution:   string(t.EffectiveResolution()),
		InProgress:   t.Status == task.StatusInProgress,
		UpdatedAgo:   humanizeAgo(now, t.UpdatedAt),
		CreatedBy:    t.CreatedBy,
		CreatedAgo:   humanizeAgo(now, t.CreatedAt),
		CreatedAt:    t.CreatedAt.Format(timeFormat),
		Branch:       cardBranch(t),
		createdAt:    t.CreatedAt,
	}
	if info, ok := snap.PhaseInfo[t.ID]; ok {
		c.Phase = string(info.Current)
		c.PhaseBy = info.Run.StartedBy
		c.PhaseOpen = info.Run.Open()
		c.PhaseAgo = humanizeAgo(now, info.Run.StartedAt)
		if !c.PhaseOpen {
			c.PhaseAgo = humanizeAgo(now, *info.Run.FinishedAt)
		}
		if info.HasTokens {
			c.Tokens, c.TokensExact = humanizeTokens(info.Tokens), strconv.FormatInt(info.Tokens, 10)
		}
	}
	if blockers := snap.Blocked[t.ID]; len(blockers) > 0 {
		c.Blocked = true
		c.Blockers = newBlockerViews(blockers)
	}
	if count, ok := snap.Counts[t.ID]; ok {
		c.SubtaskTotal, c.SubtaskDone = count.Total, count.Done
	}
	return c
}

// cardBranch is the branch a card names: where the delivered work is, or
// failing that where the work is happening.
func cardBranch(t *task.Task) string {
	if t.FinalBranch != "" {
		return t.FinalBranch
	}
	return t.Branch
}

func newBlockerViews(blockers []task.BlockingInfo) []BlockerView {
	out := make([]BlockerView, 0, len(blockers))
	for _, b := range blockers {
		out = append(out, BlockerView{ID: b.TaskID, Status: string(b.Status), Title: b.Title})
	}
	return out
}

// newDetailView maps one task's detail. titles resolves relation targets to
// their titles; a missing entry simply renders as the bare id.
func newDetailView(d *task.TaskDetail, cfg *config.Config, titles map[string]string, now time.Time) DetailView {
	t := d.Task
	card := CardView{
		ID:           t.ID,
		Title:        t.Title,
		Priority:     string(t.Priority),
		Type:         t.Type,
		Status:       string(t.Status),
		PriorityRank: t.Priority.Order(),
		IsSubtask:    t.ParentID != "",
		ParentID:     t.ParentID,
		Resolution:   string(t.EffectiveResolution()),
		InProgress:   t.Status == task.StatusInProgress,
		UpdatedAgo:   humanizeAgo(now, t.UpdatedAt),
		CreatedBy:    t.CreatedBy,
		CreatedAgo:   humanizeAgo(now, t.CreatedAt),
		CreatedAt:    t.CreatedAt.Format(timeFormat),
		Blocked:      d.Blocked,
		Blockers:     newBlockerViews(d.Blockers),
		Branch:       cardBranch(t),
		createdAt:    t.CreatedAt,
	}
	for _, sub := range d.Subtasks {
		card.SubtaskTotal++
		if sub.Status == task.StatusDone {
			card.SubtaskDone++
		}
		card.Subtasks = append(card.Subtasks, CardView{
			ID:         sub.ID,
			Title:      sub.Title,
			Priority:   string(sub.Priority),
			Type:       sub.Type,
			Status:     string(sub.Status),
			IsSubtask:  true,
			ParentID:   sub.ParentID,
			InProgress: sub.Status == task.StatusInProgress,
			UpdatedAgo: humanizeAgo(now, sub.UpdatedAt),
			createdAt:  sub.CreatedAt,
		})
	}

	v := DetailView{
		Project:        newProjectView(cfg, 0),
		Card:           card,
		Description:    t.Description,
		Archived:       d.Archived,
		Files:          d.Files,
		UpdatedAt:      t.UpdatedAt.Format(timeFormat),
		ResolutionNote: t.ResolutionNote,

		Branch:            t.Branch,
		FinalBranch:       t.FinalBranch,
		BaseBranch:        t.BaseBranch,
		StartCommit:       t.StartCommit,
		StartCommitShort:  task.ShortSHA(t.StartCommit),
		SquashCommit:      t.SquashCommit,
		SquashCommitShort: task.ShortSHA(t.SquashCommit),
	}
	if t.ClosedAt != nil {
		v.ClosedAt = t.ClosedAt.Format(timeFormat)
	}
	if t.VerifiedAt != nil {
		v.VerifiedAt = t.VerifiedAt.Format(timeFormat)
	}
	v.Phases = newPhaseViews(d.Phases)
	if total, ok := d.TotalTokens(); ok {
		v.TotalTokens = humanizeTokens(total)
	}

	for _, e := range d.Relations {
		r := RelationView{Type: e.Type, Direction: "outgoing", OtherID: e.Target}
		if e.Source != t.ID {
			r.Direction, r.OtherID = "incoming", e.Source
		}
		r.OtherTitle = titles[r.OtherID]
		v.Relations = append(v.Relations, r)
	}
	sort.SliceStable(v.Relations, func(i, j int) bool {
		if v.Relations[i].Type != v.Relations[j].Type {
			return v.Relations[i].Type < v.Relations[j].Type
		}
		return v.Relations[i].OtherID < v.Relations[j].OtherID
	})

	return v
}

// newPhaseViews maps the phase records.
func newPhaseViews(recs []task.PhaseRecord) []PhaseView {
	var views []PhaseView
	for _, rec := range recs {
		pv := PhaseView{Phase: string(rec.Phase)}
		for i, run := range rec.Runs {
			rv := PhaseRunView{
				N:         i + 1,
				Started:   run.StartedAt.Format(timeFormat),
				StartedBy: run.StartedBy,
				Note:      run.Note,
			}
			if !run.Open() {
				rv.Finished, rv.FinishedBy = run.FinishedAt.Format(timeFormat), run.FinishedBy
			}
			if run.Tokens != nil {
				rv.Tokens, rv.TokensExact = humanizeTokens(*run.Tokens), strconv.FormatInt(*run.Tokens, 10)
			}
			pv.Runs = append(pv.Runs, rv)
		}
		views = append(views, pv)
	}
	return views
}

// humanizeTokens shortens a token count: 950, 81.2k, 1.4M.
func humanizeTokens(n int64) string {
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 999_950:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "k"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	}
}

// sortCards puts a column in the order the CLI documents: priority first,
// then oldest first, then id. The index sorts by id alone, so the board has
// to sort itself.
func sortCards(cards []*CardView) {
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if a.PriorityRank != b.PriorityRank {
			return a.PriorityRank < b.PriorityRank
		}
		if !a.createdAt.Equal(b.createdAt) {
			return a.createdAt.Before(b.createdAt)
		}
		return a.ID < b.ID
	})
}

func deref(cards []*CardView) []CardView {
	out := make([]CardView, 0, len(cards))
	for _, c := range cards {
		out = append(out, *c)
	}
	return out
}

func humanizeAgo(now, then time.Time) string {
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
