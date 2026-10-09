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
// ProjectView is what the page shell shows: which backlog this is, how big
// it is, and whether an agent is working in it right now. It is the shell's
// model rather than the project's identity - TaskCount has always been a
// backlog fact - and it is the one field every model that executes
// layout.html carries (WorkspaceView, WelcomeView, GoneView), which is why
// the header can read it with no guard. A field only some of them had would
// be an execution error, i.e. a 500, on the others.
type ProjectView struct {
	Root      string
	TasksDir  string
	Source    string
	TaskCount int
	// Danger is the in-progress tasks the header names, capped at
	// maxShellDanger; DangerCount is how many there are altogether,
	// DangerMore how many are not named and DangerRest their names for the
	// +N tooltip. The welcome and gone pages have no snapshot behind them,
	// so all four stay empty there and the header shows nothing.
	Danger      []DangerItem
	DangerCount int
	DangerMore  int
	DangerRest  string
	// Palette is the status colours the page paints with. Like Danger it is
	// on the shell's model so that every model that executes layout.html
	// carries it.
	Palette PaletteView
}

// PaletteView is the dashboard's status palette as the page needs it. Class
// is the <body> class list that selects the configured colour of each role
// that differs from its default - empty for the default palette, which
// :root already paints. Names is every role's colour name, for text that
// has to say what a colour is (the bar tooltip).
type PaletteView struct {
	Class string
	Names map[string]string
}

// BoardView is one whole kanban render.
type BoardView struct {
	Project     ProjectView
	Columns     []ColumnView
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
	// Polled says something else refreshes this board, so it must not
	// carry a trigger of its own. It is set inside an open workspace: the
	// board is the strip's first unit, so the strip's own poll replaces it
	// whole, and a second trigger would mean a second request and a second
	// BoardSnapshot for the same markup every interval.
	Polled bool
	// GraphHref/GraphHXGet open the backlog graph with nothing
	// highlighted. It is the board's own entry point, so it is not a chain
	// URL: see the graph handler.
	GraphHref  string
	GraphHXGet string
	// Fragment says this view is the htmx response itself rather than part
	// of a whole page. It is what decides whether the response carries the
	// header's out-of-band copy: a page has the real header in it already,
	// and a second element with the same id would be a duplicate id in the
	// document. Only the two fragment handlers set it.
	Fragment bool
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
	// Colors is the palette's role to colour-name map, for the bar tooltip.
	Colors map[string]string
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
// scales the segments and Go never computes geometry.
//
// The two highlights are mirror images. Recent (closed inside the card's
// recent window) is part of Done and painted over its end, from RecentX. New
// (created inside the card's new window and still waiting) is part of Todo
// and painted over its start - which is TodoX, already here for the todo
// segment itself, so the arrivals need no offset of their own. Open is
// InProgress + Todo.
//
// RecentHours and NewHours are the windows in hours. They are the only
// numbers here that are not counts, and they are labels: the <title> names
// each window, which it could not do while one of them was a constant.
type StatsBarView struct {
	Value       string
	Total       int
	Done        int
	InProgress  int
	Todo        int
	Recent      int
	New         int
	Open        int
	RecentX     int
	TodoX       int
	RecentHours int
	NewHours    int
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
	Href  string
	HXGet string
	// Selected marks a detail view's subtask row whose task the next
	// column shows. Board cards never set it.
	Selected     bool
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
	// New marks a todo task created inside the board's new window
	// (web.new_hours, NewHours here for the tooltip): a board card shows a
	// chip in the new colour. Set by newBoardView only.
	New        bool
	NewHours   int
	UpdatedAgo string
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

// BlockerView is one unresolved blocker. Href/HXGet open it as a task column
// and Selected marks it when that column is the next one; chainLinks fills
// all three.
type BlockerView struct {
	ID       string
	Status   string
	Title    string
	Href     string
	HXGet    string
	Selected bool
}

// DangerItem is one in-progress task, shown in the danger-zone banner.
type DangerItem struct {
	ID         string
	Title      string
	Type       string
	UpdatedAgo string
}

// DescView is the description block of a detail view. It never carries an id
// or a data-task attribute: the panel's scroll reset in app.js keys on
// #panel's data-task, and the block must stay invisible to it.
type DescView struct {
	Has           bool // the task has a description at all
	Body          bodyView
	Raw           bool   // the on-demand source view: Body.Text, not Body.HTML
	RawHXGet      string // root-relative, no token: the template adds it through nav
	RenderedHXGet string
	Notice        string // an inline route message instead of a description
}

// descHref is the one place the /tasks/{id}/description/{view} URL is spelled.
// It is root-relative and takes its token through nav. It is deliberately not
// one of the chainLinks families: the raw text belongs to the task, not to
// the column that shows it, so it needs no rebasing.
func descHref(id, view string) string {
	return "/tasks/" + url.PathEscape(id) + "/description/" + view
}

// newDescView renders a description as a document, the same call the d column
// makes, so both read alike.
func newDescView(id, desc string) DescView {
	return DescView{
		Has:           desc != "",
		Body:          newBodyView("", desc),
		RawHXGet:      descHref(id, "raw"),
		RenderedHXGet: descHref(id, "rendered"),
	}
}

// newRawDescView is the on-demand source view. The text goes in through a
// plain struct literal, so html/template escapes it and no second
// template.HTML exists.
func newRawDescView(id, desc string) DescView {
	return DescView{
		Has:           desc != "",
		Raw:           true,
		Body:          bodyView{Text: desc},
		RawHXGet:      descHref(id, "raw"),
		RenderedHXGet: descHref(id, "rendered"),
	}
}

// descNotice is the inline answer of the description route when there is no
// description to show (an unknown task or session).
func descNotice(msg string) DescView {
	return DescView{Notice: msg}
}

// DetailView is one task's full page or side panel.
type DetailView struct {
	Project   ProjectView
	Card      CardView
	Desc      DescView
	Archived  bool
	Missing   bool
	Relations []RelationView
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

	// ParentHref/ParentHXGet open the parent as a task column; DescHref/
	// DescHXGet open this task's own description in the working area. The
	// Selected flags mark whichever of them the next column shows.
	// chainLinks is the only writer of all six, as it is of every other
	// chain URL on this view.
	ParentHref     string
	ParentHXGet    string
	ParentSelected bool
	DescHref       string
	DescHXGet      string
	DescSelected   bool
	// GraphHref/GraphHXGet open the backlog graph with this task
	// highlighted, and GraphSelected marks it when that is the next
	// column. chainLinks fills them like every other chain URL here.
	GraphHref     string
	GraphHXGet    string
	GraphSelected bool
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
	// PollHref is the fragment URL this strip refreshes itself from, set
	// only when a workspace is open. One request brings back the board,
	// the panel, the rail and every column from one consistent render, so
	// a file an agent is writing updates on screen. Empty means nothing is
	// open and the board polls itself instead, as it always has.
	PollHref string
	// Fragment says this view is the htmx response itself rather than part
	// of a whole page - see BoardView.Fragment, which it is set alongside.
	Fragment bool
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
	Kind  string
	Class string
	Label string
	Ref   string
	// Key is this column's scroll-memory key, "<index>:<kind>:<ref>".
	// app.js stores one offset per data-pane string, so the position has
	// to be in it: a chain is a history and may name the same task twice,
	// and two columns that share a key scroll as one.
	Key      string
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
	// Selected marks the file the next column shows.
	Selected bool
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
	// Href/HXGet open the other task as a task column, and Selected marks
	// it when that column is the next one. chainLinks fills all three.
	Href     string
	HXGet    string
	Selected bool
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

	newHours := config.DefaultStatsHours
	if cfg != nil && cfg.Web.NewHours > 0 {
		newHours = cfg.Web.NewHours
	}
	fresh := time.Duration(newHours) * time.Hour

	byID := make(map[string]*CardView, len(snap.Tasks))
	for _, t := range snap.Tasks {
		card := newCardView(t, snap, now)
		card.New, card.NewHours = isNew(t, now, fresh), newHours
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
			cv.Stats = newStatsCards(snap.Stats, v.Project.Palette.Names)
		default:
			cv.Cards = deref(cards)
		}
		v.Columns = append(v.Columns, cv)
	}

	v.Project.Danger, v.Project.DangerCount, v.Project.DangerMore, v.Project.DangerRest =
		shellDanger(snap, now)

	return v
}

// maxShellDanger is how many in-progress tasks the header names before it
// collapses the rest into a +N chip, the way a card caps its field chips. The
// header is one line, and the strip's one-screen-tall math subtracts a
// hard-coded header height (--shell-chrome in input.css), so a group that can
// wrap would make every column slightly too tall.
const maxShellDanger = 2

// shellDanger is the header's in-progress indicator: every in_progress task
// of the snapshot, subtasks included and sorted by id - the same set the
// board's banner used to show - capped for the header, with the names that
// did not fit collected for the +N tooltip.
func shellDanger(snap *task.BoardSnapshot, now time.Time) (shown []DangerItem, count, more int, rest string) {
	var all []DangerItem
	for _, t := range snap.Tasks {
		if t.Status != task.StatusInProgress {
			continue
		}
		all = append(all, DangerItem{
			ID:         t.ID,
			Title:      t.Title,
			Type:       t.Type,
			UpdatedAgo: humanizeAgo(now, t.UpdatedAt),
		})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	if len(all) <= maxShellDanger {
		return all, len(all), 0, ""
	}
	var names []string
	for _, d := range all[maxShellDanger:] {
		names = append(names, "#"+d.ID+" "+d.Title)
	}
	return all[:maxShellDanger], len(all), len(all) - maxShellDanger, strings.Join(names, ", ")
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
// hours defaults a window a card never named, the way internal/task does for
// the counting side, so a hand-built card labels itself correctly instead of
// claiming a window of zero hours.
func hours(h int) int {
	if h <= 0 {
		return config.DefaultStatsHours
	}
	return h
}

func newStatsCards(cards []task.StatsCard, colors map[string]string) []StatsCardView {
	var out []StatsCardView
	for _, c := range cards {
		card := StatsCardView{ID: c.ID, Kind: c.Kind, Title: c.Title, Colors: colors}
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
				New:        b.CreatedRecently,
				Open:       b.InProgress + b.Todo,
				RecentX:    b.Done - b.ClosedRecently,
				// No offset for New: it starts where todo does.
				TodoX:       b.Done + b.InProgress,
				RecentHours: hours(c.RecentHours),
				NewHours:    hours(c.NewHours),
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
	p := ProjectView{TaskCount: taskCount, Palette: newPaletteView(cfg)}
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

// newPaletteView names the palette of cfg (the defaults for a nil one). The
// roles are walked in config.ColorRoles order so the class list is stable,
// and a role at its default adds nothing: :root paints it.
func newPaletteView(cfg *config.Config) PaletteView {
	names := cfg.Palette()
	var classes []string
	for _, role := range config.ColorRoles() {
		if names[role] != config.DefaultColors[role] {
			classes = append(classes, "role-"+role+"-"+names[role])
		}
	}
	return PaletteView{Class: strings.Join(classes, " "), Names: names}
}

// isNew is the card marker's rule: a todo task created inside the board's
// new window, a future timestamp excluded. It mirrors the arrivals clause of
// internal/task/stats.go (TestCardMarkerAgreesWithBars pins the two), which
// cannot be shared because task must not import web.
func isNew(t *task.Task, now time.Time, fresh time.Duration) bool {
	return t.Status == task.StatusTodo && t.CreatedAt.After(now.Add(-fresh)) && !t.CreatedAt.After(now)
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

// newFileLinkViews pairs every attached file with the URL that serves its
// bytes. The chain URLs are not set here: chainLinks owns every one of them.
func newFileLinkViews(id string, names []string) []FileLinkView {
	links := make([]FileLinkView, 0, len(names))
	for _, name := range names {
		links = append(links, FileLinkView{
			Name:    name,
			RawHref: taskFileHref(id, name),
		})
	}
	return links
}

// chainLinks is the one place a detail view's chain URLs come from. Every
// link family it can offer - the description, the attached files, the parent,
// the subtasks, the blockers and the relation targets - is rebased onto base
// and, when next names one of them, marked as the column to the right.
//
// There is exactly one of these on purpose. _detail.html is the side panel,
// the root column and every task column at once, so a family that is rebased
// in one caller and forgotten in another renders a link that looks right and
// 404s when clicked. One function per view, two callers: newDetailView with
// the depth-0 chain (the panel) and resolveTaskColumn with the column's own
// place.
//
// Percent-encoding happens inside Chain.Path/Fragment, so a template emits a
// finished string and a name holding '#', '?' or '%' survives the trip.
func (v *DetailView) chainLinks(base Chain, next *Column) {
	// A t column marks every row naming that task - a task can be both a
	// subtask and a blocker - because the column to the right really is
	// that one task.
	isTask := func(id string) bool {
		return next != nil && next.Kind == KindTask && next.Ref == id
	}

	desc := base.Append(KindDesc, v.Card.ID)
	v.DescHref, v.DescHXGet = desc.Path(), desc.Fragment()
	v.DescSelected = next != nil && next.Kind == KindDesc && next.Ref == v.Card.ID

	graph := base.Append(KindGraph, v.Card.ID)
	v.GraphHref, v.GraphHXGet = graph.Path(), graph.Fragment()
	v.GraphSelected = next != nil && next.Kind == KindGraph && next.Ref == v.Card.ID

	if v.Card.ParentID != "" {
		parent := base.Append(KindTask, v.Card.ParentID)
		v.ParentHref, v.ParentHXGet = parent.Path(), parent.Fragment()
		v.ParentSelected = isTask(v.Card.ParentID)
	}

	for i, f := range v.Files {
		open := base.Append(KindFile, f.Name)
		v.Files[i].Href, v.Files[i].HXGet = open.Path(), open.Fragment()
		v.Files[i].Selected = next != nil && next.Kind == KindFile && next.Ref == f.Name
	}

	for i, sub := range v.Card.Subtasks {
		open := base.Append(KindTask, sub.ID)
		v.Card.Subtasks[i].Href, v.Card.Subtasks[i].HXGet = open.Path(), open.Fragment()
		v.Card.Subtasks[i].Selected = isTask(sub.ID)
	}

	for i, b := range v.Card.Blockers {
		open := base.Append(KindTask, b.ID)
		v.Card.Blockers[i].Href, v.Card.Blockers[i].HXGet = open.Path(), open.Fragment()
		v.Card.Blockers[i].Selected = isTask(b.ID)
	}

	for i, r := range v.Relations {
		open := base.Append(KindTask, r.OtherID)
		v.Relations[i].Href, v.Relations[i].HXGet = open.Path(), open.Fragment()
		v.Relations[i].Selected = isTask(r.OtherID)
	}
}

// taskFileHref is the one place the /tasks/{id}/files/{name} URL is spelled.
func taskFileHref(id, name string) string {
	return "/tasks/" + url.PathEscape(id) + "/files/" + url.PathEscape(name)
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
	card.Fields, card.FieldsMore, card.FieldsRest = cardFields(t.Fields)
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
		Desc:           newDescView(t.ID, t.Description),
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

	// The panel is the depth-0 workspace, so its links are the chain's
	// entry point. A column rebases them onto its own place afterwards.
	v.chainLinks(Chain{Root: t.ID}, nil)
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
