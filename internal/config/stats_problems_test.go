package config

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// resolveStatsProblems resolves a project whose mcp-tasks.yaml holds one
// written card and returns the problems reported for it.
func resolveStatsProblems(t *testing.T, card string) []StatsCardProblem {
	t.Helper()
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  done_stats:\n    cards:\n"+indent(card, "      "))
	t.Setenv(EnvProjectDir, root)

	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return cfg.Web.DoneStats.Problems
}

// swapStatsWarnTo points the diagnostics at buf for the test's duration.
func swapStatsWarnTo(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := statsWarnTo
	statsWarnTo = buf
	t.Cleanup(func() { statsWarnTo = prev })
}

func TestStatsCardProblems(t *testing.T) {
	for _, tc := range []struct {
		name string
		card string
		want []string
	}{
		{
			"unknown kind",
			"- kind: bar\n  field: priority\n",
			[]string{`unknown kind "bar" (one of bars, lines); the card is not drawn`},
		},
		{
			"unknown field",
			"- kind: bars\n  field: prioriry\n",
			[]string{`unknown field "prioriry" (one of created_by, priority, resolution, status, type); the card stays empty`},
		},
		{
			"bars card without a field",
			"- kind: bars\n",
			[]string{"a bars card needs a field (one of created_by, priority, resolution, status, type); the card stays empty"},
		},
		{
			"unknown split_by",
			"- kind: lines\n  split_by: prioriry\n",
			[]string{`unknown split_by "prioriry" (one of created_by, priority, resolution, status, type); the card stays empty`},
		},
		{
			"unknown metric",
			"- kind: lines\n  split_by: priority\n  metric: opened\n",
			[]string{`unknown metric "opened" (one of closed, closed_cumulative, created, created_cumulative); the card stays empty`},
		},
		{
			"unknown line among known ones",
			"- kind: lines\n  lines: [created, opened, closed]\n",
			[]string{`unknown line "opened" (one of closed, closed_cumulative, created, created_cumulative); it is not drawn`},
		},
		{
			"every line unknown",
			"- kind: lines\n  lines: [opened, shipped]\n",
			[]string{`unknown line "opened"`, `unknown line "shipped"`},
		},
		{
			"hidden line that is not one of the card's lines",
			"- kind: lines\n  lines: [created]\n  hidden: [closed]\n",
			[]string{`hidden line "closed" is not one of the card's lines; it has no effect`},
		},
		{
			"negative days",
			"- kind: lines\n  days: -3\n",
			[]string{"days: -3 is not a positive number; using 14"},
		},
		{
			"several problems on one card",
			"- kind: lines\n  days: -1\n  lines: [opened]\n",
			[]string{"days: -1 is not a positive number", `unknown line "opened"`},
		},
		// Silence is the decision in these three.
		{"zero days is an absent key", "- kind: lines\n  days: 0\n", nil},
		{"a split card's hidden names field values", "- kind: lines\n  split_by: priority\n  hidden: [whatever]\n", nil},
		{"an unknown yaml key is ignored by yaml", "- kind: bars\n  field: type\n  colour: red\n", nil},
		{"a valid bars card", "- kind: bars\n  field: created_by\n", nil},
		{"a valid cumulative lines card", "- kind: lines\n  days: 7\n  lines: [created_cumulative]\n  hidden: [created_cumulative]\n", nil},
		{"kind inferred from the field", "- field: resolution\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveStatsProblems(t, tc.card)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d problems, want %d: %v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if got[i].Index != 0 {
					t.Errorf("problem %d Index = %d, want 0", i, got[i].Index)
				}
				if got[i].ID == "" {
					t.Errorf("problem %d has no card ID", i)
				}
				if !strings.Contains(got[i].Detail, want) {
					t.Errorf("problem %d Detail = %q, want it to contain %q", i, got[i].Detail, want)
				}
			}
		})
	}
}

func TestStatsProblemsCarryTheCardIndexAndID(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  done_stats:\n    cards:\n"+
		"      - kind: bars\n        field: priority\n"+
		"      - kind: bars\n        field: prioriry\n")
	t.Setenv(EnvProjectDir, root)

	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)
	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	got := cfg.Web.DoneStats.Problems
	if len(got) != 1 {
		t.Fatalf("got %d problems, want 1: %v", len(got), got)
	}
	if got[0].Index != 1 || got[0].ID != "bars-prioriry" {
		t.Errorf("problem = %+v, want the second card (bars-prioriry)", got[0])
	}
	want := `mcp-tasks.yaml: web.done_stats.cards[1] (bars-prioriry): unknown field "prioriry"`
	if line := buf.String(); !strings.HasPrefix(line, want) || !strings.HasSuffix(line, "\n") {
		t.Errorf("reported %q, want a single line starting %q", line, want)
	}
}

func TestStatsProblemsDoNotChangeTheCards(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  done_stats:\n    cards:\n      - kind: bars\n        field: prioriry\n")
	t.Setenv(EnvProjectDir, root)

	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)
	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// The decision: report, never fail, never drop. The card is kept and
	// normalized exactly as it would be without any validation.
	want := []StatsCard{{ID: "bars-prioriry", Kind: StatsKindBars, Title: "Prioriry", Field: "prioriry",
		RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours}}
	if got := cfg.Web.DoneStats.Cards; !reflect.DeepEqual(got, want) {
		t.Errorf("Cards = %+v, want %+v", got, want)
	}
}

func TestDefaultStatsCardsHaveNoProblems(t *testing.T) {
	if got := ValidateStatsCards(nil); got != nil {
		t.Errorf("ValidateStatsCards(nil) = %v, want nil for the defaults", got)
	}
	if got := ValidateStatsCards(DefaultStatsCards()); got != nil {
		t.Errorf("ValidateStatsCards(DefaultStatsCards()) = %v, want nil", got)
	}
	if got := ValidateStatsCards([]StatsCard{}); got != nil {
		t.Errorf("ValidateStatsCards(empty) = %v, want nil for `cards: []`", got)
	}
}

func TestValidateStatsCardsIsPure(t *testing.T) {
	in := []StatsCard{{Kind: StatsKindLines, Days: -3, Lines: []string{"opened"}}}
	before := append([]StatsCard(nil), in...)

	if got := ValidateStatsCards(in); len(got) != 2 {
		t.Fatalf("got %d problems, want 2: %v", len(got), got)
	}
	if !reflect.DeepEqual(in, before) {
		t.Errorf("ValidateStatsCards changed its input: %+v, want %+v", in, before)
	}
	// Running it twice reports the same thing.
	if a, b := ValidateStatsCards(in), ValidateStatsCards(in); !reflect.DeepEqual(a, b) {
		t.Errorf("not idempotent: %v then %v", a, b)
	}
}

func TestStatsProblemString(t *testing.T) {
	p := StatsCardProblem{Index: 2, ID: "lines-14d", Detail: "something is off"}
	if got, want := p.String(), "web.done_stats.cards[2] (lines-14d): something is off"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestStatsBarWindowProblems: a negative window is reported and normalized,
// like a negative days - and a written 0 is not, because yaml cannot tell it
// from an absent key.
func TestStatsBarWindowProblems(t *testing.T) {
	for _, tc := range []struct {
		name    string
		written StatsCard
		want    []string
	}{
		{
			"negative recent_hours",
			StatsCard{Kind: StatsKindBars, Field: "priority", RecentHours: -1},
			[]string{"recent_hours: -1 is not a positive number; using 24"},
		},
		{
			"negative new_hours",
			StatsCard{Kind: StatsKindBars, Field: "priority", NewHours: -9},
			[]string{"new_hours: -9 is not a positive number; using 24"},
		},
		{
			"both negative, reported once each",
			StatsCard{Kind: StatsKindBars, Field: "priority", RecentHours: -1, NewHours: -2},
			[]string{
				"recent_hours: -1 is not a positive number; using 24",
				"new_hours: -2 is not a positive number; using 24",
			},
		},
		{
			"a written zero is a silent default",
			StatsCard{Kind: StatsKindBars, Field: "priority", RecentHours: 0, NewHours: 0},
			nil,
		},
		{
			"a sensible window says nothing",
			StatsCard{Kind: StatsKindBars, Field: "priority", RecentHours: 72, NewHours: 168},
			nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := ValidateStatsCards([]StatsCard{tc.written})
			var got []string
			for _, p := range problems {
				got = append(got, p.Detail)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("problems = %q, want %q", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("problem %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestStatsBarWindowsDoNotTouchTheID is the risk the task flagged: a card's
// id is derived from its content and is the key viewer state is stored under,
// so a window must not rename it.
func TestStatsBarWindowsDoNotTouchTheID(t *testing.T) {
	plain := normalizeStatsCards([]StatsCard{{Kind: StatsKindBars, Field: "priority"}})
	tuned := normalizeStatsCards([]StatsCard{{Kind: StatsKindBars, Field: "priority", RecentHours: 72, NewHours: 168}})
	if plain[0].ID != tuned[0].ID {
		t.Errorf("id changed with the windows: %q vs %q", plain[0].ID, tuned[0].ID)
	}
	if plain[0].ID != "bars-priority" {
		t.Errorf("id = %q, want bars-priority", plain[0].ID)
	}
	// And the title is not a window either.
	if plain[0].Title != tuned[0].Title {
		t.Errorf("title changed with the windows: %q vs %q", plain[0].Title, tuned[0].Title)
	}
}
