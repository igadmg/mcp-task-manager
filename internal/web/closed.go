package web

import (
	"sort"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

// The Done column lists the tasks closed inside the board's recent window
// (web.recent_hours) under its statistics. The set comes from the snapshot
// (BoardSnapshot.ClosedRecently), computed by the same predicate the bars
// count with, so the two cannot disagree.

// closedGroup is one card of the Done list: a task, and the recently closed
// subtasks that nest inside it.
type closedGroup struct {
	card *CardView
	// self says the card's own task closed inside the window; when it did
	// not, the card is here because a subtask did (an open parent, or a
	// parent closed long ago).
	self   bool
	selfAt time.Time
	kids   []closedKid
}

type closedKid struct {
	card *CardView
	at   time.Time
}

// newest is the latest close inside the group, the key the list is ordered by.
func (g *closedGroup) newest() time.Time {
	at := g.selfAt
	for _, k := range g.kids {
		if k.at.After(at) {
			at = k.at
		}
	}
	return at
}

// newClosedCards builds the Done column's task cards from the cards
// newBoardView already made (byID, every task of the snapshot).
//
// A recently closed task with no parent on the board is a card of its own. A
// recently closed subtask nests inside its parent's card, and that parent is
// listed whatever its status - open, or closed long ago - because the work
// that just finished happened inside it. Only the recently closed subtasks
// nest: the others are not news. Newest close first, the group's newest close
// counting for a card with nested rows.
//
// Every card is a copy. The parent's own card in its own column keeps all its
// subtasks and whatever the lanes set on them; nothing here is written back.
func newClosedCards(snap *task.BoardSnapshot, byID map[string]*CardView, now time.Time) []CardView {
	if len(snap.ClosedRecently) == 0 {
		return nil
	}

	groups := make(map[string]*closedGroup)
	var order []string
	group := func(id string) *closedGroup {
		g := groups[id]
		if g == nil {
			g = &closedGroup{card: byID[id]}
			groups[id] = g
			order = append(order, id)
		}
		return g
	}
	for _, t := range snap.Tasks {
		at, ok := snap.ClosedRecently[t.ID]
		if !ok || byID[t.ID] == nil {
			continue
		}
		if _, hasParent := byID[t.ParentID]; t.ParentID != "" && hasParent {
			g := group(t.ParentID)
			g.kids = append(g.kids, closedKid{card: byID[t.ID], at: at})
			continue
		}
		g := group(t.ID)
		g.self, g.selfAt = true, at
	}

	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if an, bn := a.newest(), b.newest(); !an.Equal(bn) {
			return an.After(bn)
		}
		return order[i] < order[j]
	})

	out := make([]CardView, 0, len(order))
	for _, id := range order {
		g := groups[id]
		sort.SliceStable(g.kids, func(i, j int) bool {
			a, b := g.kids[i], g.kids[j]
			if !a.at.Equal(b.at) {
				return a.at.After(b.at)
			}
			return a.card.ID < b.card.ID
		})

		c := *g.card
		c.Subtasks = make([]CardView, 0, len(g.kids))
		for _, k := range g.kids {
			kid := *k.card
			kid.Subtasks = nil
			c.Subtasks = append(c.Subtasks, kid)
		}
		// The marker is for a todo task that just arrived; this card is not that.
		c.New = false
		// Every card in the list is closed, so "completed" says nothing; the
		// others (obsolete, duplicate, ...) are news.
		if c.Resolution == string(task.ResolutionCompleted) {
			c.Resolution = ""
		}
		if g.self {
			c.Frame = "done-recent"
			c.ClosedNote = "closed " + humanizeAgo(now, g.selfAt)
		} else {
			c.Frame = c.Status
			c.ClosedNote = "subtask closed " + humanizeAgo(now, g.newest())
		}
		out = append(out, c)
	}
	return out
}
