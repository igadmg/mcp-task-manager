package web

import (
	"fmt"
	"sort"
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
}

// PhaseLaneView is one workflow-phase lane inside In progress. Phase is
// the raw value ("planning"): it names the lane-<phase> CSS class and
// the data-phase attribute. Count is the in-progress tasks on its cards,
// nested subtasks included, so the lane counts add up to the column's.
type PhaseLaneView struct {
	Phase string
	Title string
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
	// Branch is the git branch to show on the card: the final branch once
	// the task has been delivered, its wip branch before that.
	Branch string
	// Subtasks are nested only when they sit in the same column as this
	// card; otherwise they render standalone in their own column.
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
	Project        ProjectView
	Card           CardView
	Description    string
	Archived       bool
	Missing        bool
	Relations      []RelationView
	Files          []string
	CreatedAt      string
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

// phaseLanes fixes the In progress lanes' order and headings. Lane i holds
// phase Order() i (TestPhaseLanesFollowPhaseOrder).
var phaseLanes = []struct {
	Phase task.Phase
	Title string
}{
	{task.PhaseResearch, "Research"},
	{task.PhaseDesign, "Design"},
	{task.PhasePlanning, "Planning"},
	{task.PhaseImplementation, "Implementation"},
}

const timeFormat = "2006-01-02 15:04"

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

	// A card lives in the column of its own status. It nests inside its
	// parent only when the parent sits in that same column - otherwise the
	// same task would appear twice, or in a column it is not in.
	roots := make(map[string][]*CardView, len(columns))
	nested := make(map[string][]*CardView)
	for _, t := range snap.Tasks {
		card := byID[t.ID]
		if parent, ok := byID[t.ParentID]; ok && parent.Status == card.Status {
			nested[t.ParentID] = append(nested[t.ParentID], card)
			continue
		}
		roots[card.Status] = append(roots[card.Status], card)
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
			Cards:  deref(cards),
		}
		if col.Status == task.StatusInProgress {
			cv.Lanes = newPhaseLanes(cards, snap)
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
// the four phase lanes. A card goes to the furthest phase of its group:
// itself plus its nested subtasks, which in this column are exactly its
// in-progress subtasks. Bucketing is stable, so each lane keeps the
// column's priority, age, id order.
func newPhaseLanes(roots []*CardView, snap *task.BoardSnapshot) []PhaseLaneView {
	lanes := make([]PhaseLaneView, len(phaseLanes))
	for i, l := range phaseLanes {
		lanes[i] = PhaseLaneView{Phase: string(l.Phase), Title: l.Title}
	}
	for _, card := range roots {
		rank := snap.Phases[card.ID].Order()
		for _, kid := range card.Subtasks {
			if r := snap.Phases[kid.ID].Order(); r > rank {
				rank = r
			}
		}
		if rank >= len(lanes) { // a phase newer than this list lands last instead of panicking
			rank = len(lanes) - 1
		}
		lanes[rank].Cards = append(lanes[rank].Cards, *card)
		lanes[rank].Count += 1 + len(card.Subtasks)
	}
	return lanes
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
		Branch:       cardBranch(t),
		createdAt:    t.CreatedAt,
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
		CreatedAt:      t.CreatedAt.Format(timeFormat),
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
