package web

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// recentlyClosed marks ids as closed inside the board's window, hoursAgo
// before fixedNow, the way boardSnapshot's ClosedRecently set does.
func recentlyClosed(snap *task.BoardSnapshot, hoursAgo map[string]int) *task.BoardSnapshot {
	snap.ClosedRecently = map[string]time.Time{}
	for id, h := range hoursAgo {
		snap.ClosedRecently[id] = fixedNow.Add(-time.Duration(h) * time.Hour)
	}
	snap.RecentHours = 24
	return snap
}

func doneList(snap *task.BoardSnapshot) []CardView {
	return column(newBoardView(snap, nil, fixedNow, 5), "done").Cards
}

func nestedIDs(c CardView) []string {
	return cardIDs(c.Subtasks)
}

func TestDoneListHoldsTheRecentlyClosedOnly(t *testing.T) {
	snap := recentlyClosed(boardOf(
		tk("fresh", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("stale", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("open", "", task.StatusTodo, task.PriorityLow, fixedNow),
	), map[string]int{"fresh": 1})

	cards := doneList(snap)
	if got := cardIDs(cards); !slices.Equal(got, []string{"fresh"}) {
		t.Fatalf("Done list = %v, want [fresh]", got)
	}
	if cards[0].Frame != "done-recent" || cards[0].ClosedNote != "closed 1h ago" {
		t.Errorf("Frame = %q, ClosedNote = %q, want done-recent and closed 1h ago", cards[0].Frame, cards[0].ClosedNote)
	}
	done := column(newBoardView(snap, nil, fixedNow, 5), "done")
	if done.Count != 2 {
		t.Errorf("Done Count = %d, want 2: the header still counts every done task", done.Count)
	}
	if done.RecentHours != 24 {
		t.Errorf("RecentHours = %d, want 24", done.RecentHours)
	}
}

func TestDoneListNestsOnlyTheRecentSubtasks(t *testing.T) {
	parent := tk("p", "", task.StatusDone, task.PriorityLow, fixedNow)
	snap := recentlyClosed(boardOf(
		parent,
		tk("p-new", "p", task.StatusDone, task.PriorityLow, fixedNow),
		tk("p-old", "p", task.StatusDone, task.PriorityLow, fixedNow),
	), map[string]int{"p": 2, "p-new": 1})

	cards := doneList(snap)
	if len(cards) != 1 || cards[0].ID != "p" {
		t.Fatalf("Done list = %v, want only the parent: a subtask never has a card of its own", cardIDs(cards))
	}
	if got := nestedIDs(cards[0]); !slices.Equal(got, []string{"p-new"}) {
		t.Errorf("nested = %v, want only the recently closed subtask", got)
	}
	if cards[0].Frame != "done-recent" {
		t.Errorf("Frame = %q, want done-recent: the parent itself closed in the window", cards[0].Frame)
	}
	// The subtask tally is the parent's own, not the list's.
	if cards[0].SubtaskTotal != 2 || cards[0].SubtaskDone != 2 {
		t.Errorf("tally = %d/%d, want 2/2", cards[0].SubtaskDone, cards[0].SubtaskTotal)
	}
}

func TestDoneListShowsAnOpenParentWithItsOwnFrame(t *testing.T) {
	for _, status := range []task.Status{task.StatusTodo, task.StatusInProgress} {
		t.Run(string(status), func(t *testing.T) {
			snap := recentlyClosed(boardOf(
				tk("p", "", status, task.PriorityLow, fixedNow),
				tk("p-done", "p", task.StatusDone, task.PriorityLow, fixedNow),
				tk("p-old", "p", task.StatusDone, task.PriorityLow, fixedNow),
				tk("p-open", "p", task.StatusTodo, task.PriorityLow, fixedNow),
			), map[string]int{"p-done": 3})

			board := newBoardView(snap, nil, fixedNow, 5)
			cards := column(board, "done").Cards
			if len(cards) != 1 || cards[0].ID != "p" {
				t.Fatalf("Done list = %v, want the open parent", cardIDs(cards))
			}
			c := cards[0]
			if c.Frame != string(status) {
				t.Errorf("Frame = %q, want the parent's own status %q", c.Frame, status)
			}
			if c.ClosedNote != "subtask closed 3h ago" {
				t.Errorf("ClosedNote = %q, want subtask closed 3h ago", c.ClosedNote)
			}
			if got := nestedIDs(c); !slices.Equal(got, []string{"p-done"}) || c.Subtasks[0].Status != "done" {
				t.Errorf("nested = %v, want only the closed subtask, as done", got)
			}

			// The parent stays in its own column with its own rows, untouched.
			own := column(board, string(status)).Cards
			if len(own) != 1 || own[0].ID != "p" {
				t.Fatalf("%s column = %v, want the parent", status, cardIDs(own))
			}
			if own[0].Frame != "" || own[0].ClosedNote != "" {
				t.Errorf("the parent's own card picked up the Done list's Frame %q / ClosedNote %q", own[0].Frame, own[0].ClosedNote)
			}
			if got := nestedIDs(own[0]); slices.Contains(got, "p-done") && status != task.StatusDone {
				// A done subtask never nests in an open parent's own card.
				t.Errorf("own card nests %v", got)
			}
			if !slices.Contains(nestedIDs(own[0]), "p-open") {
				t.Errorf("own card lost its todo subtask: %v", nestedIDs(own[0]))
			}
		})
	}
}

func TestDoneListShowsAnOldClosedParentForARecentSubtask(t *testing.T) {
	snap := recentlyClosed(boardOf(
		tk("p", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("p-late", "p", task.StatusDone, task.PriorityLow, fixedNow),
	), map[string]int{"p-late": 5})

	cards := doneList(snap)
	if len(cards) != 1 || cards[0].ID != "p" {
		t.Fatalf("Done list = %v, want the parent", cardIDs(cards))
	}
	if cards[0].Frame != "done" || cards[0].ClosedNote != "subtask closed 5h ago" {
		t.Errorf("Frame = %q, ClosedNote = %q, want done and subtask closed 5h ago", cards[0].Frame, cards[0].ClosedNote)
	}
}

func TestDoneListSubtaskWithoutAParentOnTheBoardIsACard(t *testing.T) {
	snap := recentlyClosed(boardOf(
		tk("orphan", "gone", task.StatusDone, task.PriorityLow, fixedNow),
	), map[string]int{"orphan": 1})

	if got := cardIDs(doneList(snap)); !slices.Equal(got, []string{"orphan"}) {
		t.Errorf("Done list = %v, want [orphan]: with no parent to nest in it is a root", got)
	}
}

func TestDoneListOrder(t *testing.T) {
	snap := recentlyClosed(boardOf(
		tk("a", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("b", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("c", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("d", "", task.StatusDone, task.PriorityLow, fixedNow),
		tk("d-1", "d", task.StatusDone, task.PriorityLow, fixedNow),
		tk("d-2", "d", task.StatusDone, task.PriorityLow, fixedNow),
	), map[string]int{"a": 6, "b": 2, "c": 2, "d": 10, "d-1": 4, "d-2": 1})

	cards := doneList(snap)
	// d's newest close is its subtask d-2, one hour ago; then b and c tie at
	// two hours and go by id; a closed six hours ago.
	if got, want := cardIDs(cards), []string{"d", "b", "c", "a"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if got, want := nestedIDs(cards[0]), []string{"d-2", "d-1"}; !slices.Equal(got, want) {
		t.Errorf("nested order = %v, want %v (newest first)", got, want)
	}
}

func TestDoneListResolutionAndNewMarker(t *testing.T) {
	plain := tk("plain", "", task.StatusDone, task.PriorityLow, fixedNow)
	dropped := tk("dropped", "", task.StatusDone, task.PriorityLow, fixedNow)
	dropped.Resolution = task.ResolutionObsolete
	parent := tk("p", "", task.StatusTodo, task.PriorityLow, fixedNow) // created just now: a new todo
	kid := tk("p-1", "p", task.StatusDone, task.PriorityLow, fixedNow)
	snap := recentlyClosed(boardOf(plain, dropped, parent, kid),
		map[string]int{"plain": 1, "dropped": 1, "p-1": 1})

	board := newBoardView(snap, nil, fixedNow, 5)
	byID := map[string]CardView{}
	for _, c := range column(board, "done").Cards {
		byID[c.ID] = c
	}
	if got := byID["plain"].Resolution; got != "" {
		t.Errorf("a completed task shows resolution %q, want none", got)
	}
	if got := byID["dropped"].Resolution; got != "obsolete" {
		t.Errorf("an obsolete task shows resolution %q, want obsolete", got)
	}
	if byID["p"].New {
		t.Error("the Done list's copy of a new todo parent carries the new marker")
	}
	if own := column(board, "todo").Cards; len(own) != 1 || !own[0].New {
		t.Errorf("the parent's own card lost its new marker: %+v", own)
	}
}

func TestDoneListHasNoCap(t *testing.T) {
	var tasks []*task.Task
	closed := map[string]int{}
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("t%02d", i)
		tasks = append(tasks, tk(id, "", task.StatusDone, task.PriorityLow, fixedNow))
		closed[id] = 1 + i%20
	}
	if got := len(doneList(recentlyClosed(boardOf(tasks...), closed))); got != 60 {
		t.Errorf("Done list has %d cards, want all 60", got)
	}
}

func TestDoneListIsEmptyWithoutASet(t *testing.T) {
	// A snapshot with no ClosedRecently (hand-built, or nothing closed) lists nothing.
	snap := boardOf(tk("a", "", task.StatusDone, task.PriorityLow, fixedNow))
	if got := doneList(snap); len(got) != 0 {
		t.Errorf("Done list = %v, want empty", cardIDs(got))
	}
	if got := column(newBoardView(snap, nil, fixedNow, 5), "done").RecentHours; got != 24 {
		t.Errorf("RecentHours = %d, want the 24 h default for a snapshot that names none", got)
	}
}
