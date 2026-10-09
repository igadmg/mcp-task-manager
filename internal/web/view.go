package web

import (
	"fmt"
	"net/url"
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
//
// It no longer carries a "resolved" flag: a live session always has a
// project, because registering the session is what builds it. The state that
// flag used to stand for - the dashboard is up but nothing named a project
// yet - is now the welcome page, which is a better answer because it does
// not pretend a board exists.
type ProjectView struct {
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
	// Panel is the task whose side panel is open, nil on the bare board.
	// /tasks/{id} is that state: the board with a panel, which is also
	// where the workspace rail ends.
	Panel *DetailView
	// Title is the document title: "#id Title" with a panel open, else
	// the board's own. htmx caches and restores document.title, so a
	// history entry stays distinguishable.
	Title string
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
// the raw value ("planning"): it names the data-phase attribute and the
// lane-from-<phase> / lane-to-<phase> placement classes, and is the heading
// (the template upper-cases it).
//
// Cards and Count answer different questions on purpose, because a card can
// straddle lanes:
//
//   - Cards are the cards that *begin* in this lane, i.e. whose LaneFrom it
//     is, so the column's one vertical stack is ordered by where a card
//     starts. A card listed here may reach into later lanes.
//   - Count is the in-progress tasks whose *own* phase this lane is - the
//     card's own task, plus each nested in-progress subtask counted in its
//     own lane even when it is drawn inside a parent sitting elsewhere. A
//     nested todo subtask counts in To do. Every in-progress task has
//     exactly one own phase, so the four counts partition the column's.
type PhaseLaneView struct {
	Phase string
	Count int
	Cards []CardView
}

// CardView is one task on the board.
type CardView struct {
	ID    string
	Title string
	// Href and HXGet open this card as a workspace column. They are filled
	// for a detail view's subtask rows, so a subtask opens beside its
	// parent instead of leaving the workspace; board cards leave them
	// empty and _card.html builds its own panel link.
	Href         string
	HXGet        string
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
	// LaneFrom and LaneTo are the phase lanes this card spans on the In
	// progress grid, earliest to latest: its own phase plus every nested
	// in-progress subtask's. They are equal for a card without such
	// subtasks, and empty outside In progress.
	LaneFrom string
	LaneTo   string
	// LaneOffset is a nested subtask row's indent, in lane steps right of
	// the parent card's first lane (0..3). It is 0 for a row with no phase
	// of its own - a todo subtask - and for every card outside In progress.
	LaneOffset int
	// Fields are the task's free-form fields as chips, sorted by key and
	// capped at maxCardFields; FieldsMore is how many were left off, and
	// FieldsRest names them for the +N chip's tooltip. The detail view
	// shows all of them, so a card does not have to.
	Fields     []FieldView
	FieldsMore int
	FieldsRest string
	// Subtasks are nested when they sit in the same column as this card,
	// plus the todo subtasks of an in-progress card, which also keep their
	// own card in To do; otherwise they render standalone in their column.
	Subtasks []CardView

	createdAt time.Time // sorting only
}

// FieldView is one free-form field, rendered as a chip on a card and as a row
// in the detail view.
type FieldView struct {
	Key   string
	Value string
}

// maxCardFields is how many field chips a card shows before it collapses the
// rest into a +N chip. A card with ten chips is noise, and the detail view is
// one click away.
const maxCardFields = 3

// newFieldViews renders a task's fields in the order every surface shows them
// (sorted by key).
func newFieldViews(f task.Fields) []FieldView {
	keys := f.Keys()
	out := make([]FieldView, 0, len(keys))
	for _, key := range keys {
		out = append(out, FieldView{Key: key, Value: f.String(key)})
	}
	return out
}

// cardFields splits a task's fields into the chips a card shows and a count
// plus a tooltip for the ones it does not.
func cardFields(f task.Fields) (shown []FieldView, more int, rest string) {
	all := newFieldViews(f)
	if len(all) <= maxCardFields {
		return all, 0, ""
	}
	var names []string
	for _, field := range all[maxCardFields:] {
		names = append(names, field.Key+": "+field.Value)
	}
	return all[:maxCardFields], len(all) - maxCardFields, strings.Join(names, ", ")
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
	Files []FileLinkView
	// Fields are every free-form field, uncapped - the card's chips are the
	// summary, this is the full list.
	Fields         []FieldView
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

// WorkspaceView is one workspace state: the rail, then the strip. The board
// is the strip's first unit and stays in the DOM at every depth, so it keeps
// polling while it is off-screen.
type WorkspaceView struct {
	Project     ProjectView
	Title       string
	PollSeconds int
	Board       BoardView
	Rail        []RailEntryView
	Columns     []ColumnUnitView
	// Root is the root task's own column, rendered as the strip's second
	// unit whenever anything is open. The chain does not name it - the
	// root is already in the URL - but it is what takes the board's
	// leftmost slot once the board slides away, so everything else is
	// placed from it.
	Root *ColumnUnitView
	// Shifted says the board has slid off the left, which is exactly the
	// case when something is open. Only ever one unit leaves, so the
	// offset is a single width rather than a sum over kinds, and the strip
	// can carry it in one custom property and transition it.
	Shifted bool
}

// RailEntryView is one rung of the nesting rail. Href is the chain truncated
// to this entry, which is what makes the rail the primary Back: every rung is
// a real URL and browser Back does the same thing.
type RailEntryView struct {
	Kind    string
	Label   string
	Ref     string
	Href    string
	HXGet   string
	Current bool
}

// ColumnUnitView is one column in the strip. Class comes from the kind
// registry and is declared in input.css, Template draws the body, and Data is
// whatever that kind resolved.
type ColumnUnitView struct {
	Kind     string
	Class    string
	Label    string
	Ref      string
	Template string
	Working  bool
	Href     string
	Data     any
}

// FileLinkView is one attached file: its name as the backlog spells it, and
// the URL that serves it. Href is built here rather than in a template
// because html/template's URL normalizer escapes a space but leaves '#' and
// '?' alone, which would turn the rest of a name like "a#b.md" into a
// fragment and make the file unreachable.
type FileLinkView struct {
	Name string
	// Href opens the file as a workspace column; HXGet is the same state
	// as a fragment. The panel is the depth-0 workspace, so a file chip
	// there is the chain's entry point.
	Href  string
	HXGet string
	// RawHref serves the bytes as text/plain, which is what the file
	// column's "raw" link and anything outside the workspace uses.
	RawHref string
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
			// newPhaseLanes fills in the lane span and the nested rows'
			// offsets, so it runs before the cards are copied out.
			cv.Lanes = newPhaseLanes(cards, snap)
			cv.Cards = deref(cards)
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
// one lane per task.Phases entry; lane i holds phase Order() i. On the way
// it fills in each card's LaneFrom/LaneTo span and its nested rows'
// LaneOffset, which is why it runs before the cards are copied out.
//
// A card spans its group: itself plus its nested in-progress subtasks
// (nested todo subtasks have their own To do card, no phase of their own,
// and widen nothing). It is listed in the lane it begins in, while every
// task counts in the lane of its own phase - see PhaseLaneView. Bucketing
// is stable, so each lane keeps the column's priority, age, id order.
func newPhaseLanes(roots []*CardView, snap *task.BoardSnapshot) []PhaseLaneView {
	phases := task.Phases()
	lanes := make([]PhaseLaneView, 0, len(phases))
	for _, p := range phases {
		lanes = append(lanes, PhaseLaneView{Phase: string(p)})
	}
	for _, card := range roots {
		own := snap.Phases[card.ID].Order()
		from, to := own, own
		lanes[own].Count++
		for _, kid := range card.Subtasks {
			if !kid.InProgress {
				continue
			}
			rank := snap.Phases[kid.ID].Order()
			from, to = min(from, rank), max(to, rank)
			lanes[rank].Count++
		}
		card.LaneFrom, card.LaneTo = string(phases[from]), string(phases[to])
		for i := range card.Subtasks {
			if kid := &card.Subtasks[i]; kid.InProgress {
				kid.LaneOffset = snap.Phases[kid.ID].Order() - from
			}
		}
		lanes[from].Cards = append(lanes[from].Cards, *card)
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

func newProjectView(cfg *config.Config, taskCount int) ProjectView {
	p := ProjectView{TaskCount: taskCount}
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
	c.Fields, c.FieldsMore, c.FieldsRest = cardFields(t.Fields)
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
// newFileLinkViews pairs every attached file with its URL. Both segments are
// percent-encoded here, so the template emits a finished string and a name
// holding '#', '?' or '%' survives the trip to the browser.
func newFileLinkViews(id string, names []string) []FileLinkView {
	base := Chain{Root: id}
	links := make([]FileLinkView, 0, len(names))
	for _, name := range names {
		open := base.Append(KindFile, name)
		links = append(links, FileLinkView{
			Name:    name,
			Href:    open.Path(),
			HXGet:   open.Fragment(),
			RawHref: taskFileHref(id, name),
		})
	}
	return links
}

// taskFileHref is the one place the /tasks/{id}/files/{name} URL is spelled.
func taskFileHref(id, name string) string {
	return "/tasks/" + url.PathEscape(id) + "/files/" + url.PathEscape(name)
}

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
	card.Fields, card.FieldsMore, card.FieldsRest = cardFields(t.Fields)
	for _, sub := range d.Subtasks {
		card.SubtaskTotal++
		if sub.Status == task.StatusDone {
			card.SubtaskDone++
		}
		open := Chain{Root: t.ID}.Append(KindTask, sub.ID)
		card.Subtasks = append(card.Subtasks, CardView{
			ID:         sub.ID,
			Href:       open.Path(),
			HXGet:      open.Fragment(),
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
		Files:          newFileLinkViews(t.ID, d.Files),
		Fields:         newFieldViews(t.Fields),
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

// WelcomeView is the page at /: every workspace this server may open, and
// every session already open.
type WelcomeView struct {
	// Project is empty here; the layout reads it for the header and renders
	// nothing when there is no backlog behind the page.
	Project ProjectView
	// Workspaces is the configured list in file order, unavailable entries
	// included - an operator who mistyped a path sees why here instead of
	// wondering where their workspace went.
	Workspaces []WorkspaceRow
	// Sessions are the live sessions, sorted by name. A session MCP opened
	// appears here without ever having been configured.
	Sessions []SessionRow
	// ConfigPath is the file Workspaces was read from, empty when none was
	// found.
	ConfigPath string
	// Problems are the config file's findings, shown so the page explains
	// itself rather than only the server log.
	Problems []string
	// Error is the message of a failed pick, shown above the list.
	Error string
}

// WorkspaceRow is one configured workspace on the welcome page.
type WorkspaceRow struct {
	Name     string
	Path     string
	TasksDir string
	// Problem is why this entry cannot be opened, empty when it can.
	Problem string
	// Href is where picking it goes - the live session when there is one.
	// Empty for an entry with a Problem.
	Href string
	// Live is true when a session for this backlog already exists, so the
	// page offers a link instead of the form button.
	Live bool
}

// SessionRow is one live session on the welcome page.
type SessionRow struct {
	Name     string
	TasksDir string
	Source   string
	Href     string
}

// GoneView is the unknown-token page and fragment.
type GoneView struct {
	Project ProjectView
	// Token is what the URL asked for, echoed so a stale bookmark is
	// recognizable. It is rendered as ordinary escaped text.
	Token string
}

func newWelcomeView(statuses []WorkspaceStatus, live []*Session, configPath string, problems []string, errMsg string) WelcomeView {
	v := WelcomeView{ConfigPath: configPath, Problems: problems, Error: errMsg}

	for _, st := range statuses {
		row := WorkspaceRow{
			Name:     st.Name,
			Path:     st.Path,
			TasksDir: st.Workspace.TasksDir,
			Problem:  st.Problem,
			Live:     st.Live,
		}
		if st.Live {
			row.Href = "/" + st.Token + "/"
		}
		v.Workspaces = append(v.Workspaces, row)
	}

	for _, s := range live {
		v.Sessions = append(v.Sessions, SessionRow{
			Name:     s.Name,
			TasksDir: s.TasksDir,
			Source:   string(s.Source()),
			Href:     s.Base() + "/",
		})
	}
	sort.Slice(v.Sessions, func(i, j int) bool {
		if v.Sessions[i].Name != v.Sessions[j].Name {
			return v.Sessions[i].Name < v.Sessions[j].Name
		}
		return v.Sessions[i].TasksDir < v.Sessions[j].TasksDir
	})

	return v
}
