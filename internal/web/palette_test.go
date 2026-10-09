package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/task"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

var bodyClass = regexp.MustCompile(`<body class="([^"]*)"`)

// bodyClassOf is the class list of the page's <body>.
func bodyClassOf(t *testing.T, page string) string {
	t.Helper()
	m := bodyClass.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no <body class> in the page:\n%s", page)
	}
	return m[1]
}

func TestDefaultPaletteAddsNoBodyClass(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)

	for name, page := range map[string]string{
		"board":   get(t, h, "/").Body.String(),
		"welcome": getRaw(t, h, "/").Body.String(),
		"gone":    getRaw(t, h, "/nosuchtoken/").Body.String(),
	} {
		if class := bodyClassOf(t, page); strings.Contains(class, "role-") {
			t.Errorf("%s: default palette put %q on <body>", name, class)
		}
	}
}

func TestCustomPaletteClassesOnEveryShellPage(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	svc.Config().Web.Colors.Roles = map[string]string{
		"new": "rose-400", "in_progress": "sky-400", "done": config.DefaultColors["done"], "todo": "bogus",
	}

	// Class order is the role order, a role at its default (or invalid, so
	// defaulted) adds nothing, and each class appears once.
	const want = "role-in_progress-sky-400 role-new-rose-400"
	board := bodyClassOf(t, get(t, h, "/").Body.String())
	if !strings.HasSuffix(board, want) || strings.Count(board, "role-") != 2 {
		t.Errorf("board <body> class = %q, want it to end with %q", board, want)
	}

	// The welcome and gone pages have no project behind them: :root paints
	// the defaults, and they must still render with the shell.
	for name, path := range map[string]string{"welcome": "/", "gone": "/nosuchtoken/"} {
		rec := getRaw(t, h, path)
		if rec.Code == 500 {
			t.Errorf("%s page = 500", name)
		}
		if class := bodyClassOf(t, rec.Body.String()); strings.Contains(class, "role-") {
			t.Errorf("%s: <body> class = %q, want no role classes", name, class)
		}
	}
}

func TestStatsBarsTooltipNamesTheConfiguredColours(t *testing.T) {
	h, svc, _ := newTestHandler(t)
	seedBoard(t, svc)
	svc.Config().Web.Colors.Roles = map[string]string{"in_progress": "sky-400", "new": "rose-300"}

	body := get(t, h, "/board").Body.String()
	const want = "0 in progress (sky-400), 1 to do (neutral-500), 1 of them created in the last 24 h (rose-300)</title>"
	if !strings.Contains(body, want) {
		t.Errorf("the tooltip does not name the configured colours; want %q", want)
	}
	if strings.Contains(body, "in progress (amber") {
		t.Error("the tooltip still names the default in-progress colour")
	}
}

func TestNewPaletteViewIsNilSafe(t *testing.T) {
	p := newPaletteView(nil)
	if p.Class != "" || p.Names["new"] != "sky-400" {
		t.Errorf("nil config palette = %+v, want the defaults and no class", p)
	}
}

func TestChipNewOnFreshTodoCards(t *testing.T) {
	cfg := &config.Config{}
	hour := time.Hour
	snap := boardOf(
		tk("in", "", task.StatusTodo, task.PriorityLow, fixedNow.Add(-2*hour)),
		tk("old", "", task.StatusTodo, task.PriorityLow, fixedNow.Add(-25*hour)),
		tk("future", "", task.StatusTodo, task.PriorityLow, fixedNow.Add(hour)),
		tk("prog", "", task.StatusInProgress, task.PriorityLow, fixedNow.Add(-hour)),
		tk("done", "", task.StatusDone, task.PriorityLow, fixedNow.Add(-hour)),
	)
	v := newBoardView(snap, cfg, fixedNow, 5)
	got := map[string]bool{}
	for _, col := range v.Columns {
		for _, c := range col.Cards {
			got[c.ID] = c.New
		}
		for _, l := range col.Lanes {
			for _, c := range l.Cards {
				got[c.ID] = c.New
			}
		}
	}
	for id, want := range map[string]bool{"in": true, "old": false, "future": false, "prog": false} {
		if got[id] != want {
			t.Errorf("card %s New = %v, want %v", id, got[id], want)
		}
	}
}

// TestChipNewRendersOnTheCard drives the whole handler with a clock that
// agrees with the seeded tasks' creation time.
func TestChipNewRendersOnTheCard(t *testing.T) {
	rs, svc, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)
	h := &testHandler{Handler: NewHandler(Deps{Sessions: sessions, Logger: discardLogger(),
		Now: func() time.Time { return time.Now().Add(time.Minute) }}), base: sess.Base()}
	seedBoard(t, svc)

	body := get(t, h, "/board").Body.String()
	// Tasks 1, 2 and 5 are todo, but 2 nests in 1 as a compact row without
	// chips: the marker is on board cards only, so 1 and 5 carry it.
	if n := strings.Count(body, `class="chip chip-new"`); n != 2 {
		t.Errorf("board has %d new chips, want 2 (todo cards 1 and 5)", n)
	}
	if !strings.Contains(body, `, inside the last 24 h">new</span>`) {
		t.Error("the new chip does not name its window")
	}
}

func TestChipNewMarkupAndWindow(t *testing.T) {
	cfg := &config.Config{Web: config.WebConfig{NewHours: 72}}
	snap := boardOf(tk("a", "", task.StatusTodo, task.PriorityLow, fixedNow.Add(-50*time.Hour)))
	v := newBoardView(snap, cfg, fixedNow, 5)
	card := column(v, "todo").Cards[0]
	if !card.New || card.NewHours != 72 {
		t.Fatalf("card = New %v, NewHours %d, want a new card in a 72 h window", card.New, card.NewHours)
	}
}

// TestCardMarkerAgreesWithBars pins the duplicated predicate: with the board
// window as the bars' window, the todo cards marked new are exactly what the
// bars count as arrivals, over the same snapshot and the same clock.
func TestCardMarkerAgreesWithBars(t *testing.T) {
	_, svc, dir := newTestHandler(t)
	testsupport.Seed(t, svc,
		testsupport.TaskSpec{ID: "1", Priority: "low", Type: "feature"},
		testsupport.TaskSpec{ID: "2", Priority: "low", Type: "feature"},
		testsupport.TaskSpec{ID: "3", Priority: "low", Type: "feature"},
		testsupport.TaskSpec{ID: "4", Priority: "low", Type: "feature"},
		testsupport.TaskSpec{ID: "5", Priority: "low", Type: "feature", Status: "in_progress"},
	)
	now := time.Now().UTC()
	backdate := func(id string, at time.Time) {
		path := filepath.Join(dir, id, id+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(`(?m)^created_at: .*$`)
		out := re.ReplaceAllString(string(data), "created_at: "+at.Format(time.RFC3339))
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		// A distinct mtime in the future makes the index see the edit.
		future := time.Now().Add(time.Minute)
		_ = os.Chtimes(path, future, future)
	}
	backdate("2", now.Add(-48*time.Hour))
	backdate("3", now.Add(-3*time.Hour))
	backdate("4", now.Add(time.Hour))  // not arrived yet
	backdate("5", now.Add(-time.Hour)) // in progress: never an arrival

	for _, tc := range []struct {
		boardHours int
		cardHours  int // the bars card's own window; 0 inherits
		wantCards  int
		wantBars   int
	}{
		{24, 24, 2, 2}, // 1 (just now) and 3 (3 h ago)
		{2, 2, 1, 1},
		{72, 72, 3, 3},
		{24, 72, 2, 3}, // a card tuned apart from the board: documented
	} {
		cfg := svc.Config()
		cfg.Web.NewHours = tc.boardHours
		cfg.Web.DoneStats.Cards = []config.StatsCard{{Kind: config.StatsKindBars, Field: "priority", NewHours: tc.cardHours}}
		if tc.cardHours == 0 {
			cfg.Web.DoneStats.Cards[0].NewHours = tc.boardHours
		}
		snap, err := svc.BoardSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		v := newBoardView(snap, cfg, snap.TakenAt, 5)
		var marked int
		for _, c := range column(v, "todo").Cards {
			if c.New {
				marked++
			}
		}
		var bars int
		for _, c := range column(v, "done").Stats {
			for _, b := range c.Bars {
				bars += b.New
			}
		}
		if marked != tc.wantCards || bars != tc.wantBars {
			t.Errorf("board %d h, card %d h: %d cards marked, %d arrivals in the bars; want %d and %d",
				tc.boardHours, tc.cardHours, marked, bars, tc.wantCards, tc.wantBars)
		}
	}
}
