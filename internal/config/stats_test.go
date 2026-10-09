package config

import (
	"reflect"
	"strings"
	"testing"
)

// resolveDoneStats resolves a fresh project whose mcp-tasks.yaml holds body
// (none when body is empty) and returns its Done-column cards as stored.
func resolveDoneStats(t *testing.T, body string) []StatsCard {
	t.Helper()
	root := tempDir(t)
	if body != "" {
		writeConfig(t, root, body)
	}
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return cfg.Web.DoneStats.Cards
}

func TestDefaultStatsCards(t *testing.T) {
	want := []StatsCard{
		{ID: "bars-priority", Kind: StatsKindBars, Title: "Priority", Field: "priority",
			RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		{ID: "bars-type", Kind: StatsKindBars, Title: "Type", Field: "type",
			RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		{ID: "bars-resolution", Kind: StatsKindBars, Title: "Resolution", Field: "resolution",
			RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		{ID: "lines-14d", Kind: StatsKindLines, Title: "Last 14 days", Days: 14, Lines: []string{"created", "closed"}},
	}
	if got := DefaultStatsCards(); !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultStatsCards() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDoneStatsAbsentMeansDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"no config file", ""},
		{"partial web section", "web:\n  enabled: true\n"},
		{"null done_stats", "web:\n  done_stats:\n"},
		{"empty done_stats", "web:\n  done_stats: {}\n"},
		{"null cards", "web:\n  done_stats:\n    cards:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			if got := resolveDoneStats(t, tc.body); !reflect.DeepEqual(got, DefaultStatsCards()) {
				t.Errorf("Cards =\n%+v\nwant the defaults", got)
			}
		})
	}
}

func TestDoneStatsEmptyListMeansNoCards(t *testing.T) {
	isolateEnv(t)
	cards := resolveDoneStats(t, "web:\n  done_stats:\n    cards: []\n")
	if cards == nil || len(cards) != 0 {
		t.Fatalf("Cards = %#v, want a non-nil empty list: `cards: []` means no cards", cards)
	}
	cfg := &Config{Web: WebConfig{DoneStats: DoneStatsConfig{Cards: cards}}}
	if got := cfg.DoneStatsCards(); got == nil || len(got) != 0 {
		t.Errorf("DoneStatsCards() = %#v, want a non-nil empty list", got)
	}
}

func TestDoneStatsPerCardDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want StatsCard
	}{
		{
			"lines card with only a kind",
			"- kind: lines\n",
			StatsCard{ID: "lines-14d", Kind: "lines", Title: "Last 14 days", Days: 14, Lines: []string{"created", "closed"}},
		},
		{
			"split card",
			"- kind: lines\n  days: 30\n  split_by: priority\n",
			StatsCard{ID: "lines-priority-closed-30d", Kind: "lines", Title: "Closed by priority, 30 days", Days: 30, SplitBy: "priority", Metric: "closed"},
		},
		{
			"split card with a cumulative metric",
			"- kind: lines\n  split_by: created_by\n  metric: closed_cumulative\n",
			StatsCard{ID: "lines-created-by-closed-cumulative-14d", Kind: "lines", Title: "Closed cumulative by created by, 14 days", Days: 14, SplitBy: "created_by", Metric: "closed_cumulative"},
		},
		{
			"written values kept",
			"- id: mine\n  kind: lines\n  title: Two weeks\n  days: 7\n  lines: [created_cumulative]\n  hidden: [created_cumulative]\n",
			StatsCard{ID: "mine", Kind: "lines", Title: "Two weeks", Days: 7, Lines: []string{"created_cumulative"}, Hidden: []string{"created_cumulative"}},
		},
		{
			"kind inferred from field",
			"- field: created_by\n",
			StatsCard{ID: "bars-created-by", Kind: "bars", Title: "Created by", Field: "created_by",
				RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		},
		{
			"kind inferred without field",
			"- days: 30\n",
			StatsCard{ID: "lines-30d", Kind: "lines", Title: "Last 30 days", Days: 30, Lines: []string{"created", "closed"}},
		},
		{
			"strings trimmed and blank entries dropped",
			"- kind: \" lines \"\n  lines: [\" closed \", \"\", \"  \"]\n  hidden: [\"\"]\n",
			StatsCard{ID: "lines-14d", Kind: "lines", Title: "Last 14 days", Days: 14, Lines: []string{"closed"}},
		},
		{
			"zero days defaulted",
			"- kind: lines\n  days: 0\n",
			StatsCard{ID: "lines-14d", Kind: "lines", Title: "Last 14 days", Days: 14, Lines: []string{"created", "closed"}},
		},
		{
			"negative days defaulted",
			"- kind: lines\n  days: -3\n",
			StatsCard{ID: "lines-14d", Kind: "lines", Title: "Last 14 days", Days: 14, Lines: []string{"created", "closed"}},
		},
		{
			"zero and absent windows default to 24 h, on both ends",
			"- kind: bars\n  field: type\n  recent_hours: 0\n",
			StatsCard{ID: "bars-type", Kind: "bars", Title: "Type", Field: "type",
				RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		},
		{
			"written windows kept, and they do not touch the id",
			"- kind: bars\n  field: type\n  recent_hours: 48\n  new_hours: 168\n",
			StatsCard{ID: "bars-type", Kind: "bars", Title: "Type", Field: "type",
				RecentHours: 48, NewHours: 168},
		},
		{
			"negative windows defaulted",
			"- kind: bars\n  field: type\n  recent_hours: -1\n  new_hours: -9\n",
			StatsCard{ID: "bars-type", Kind: "bars", Title: "Type", Field: "type",
				RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		},
		{
			"a lines card gets no bar windows",
			"- kind: lines\n  days: 7\n",
			StatsCard{ID: "lines-7d", Kind: "lines", Title: "Last 7 days", Days: 7,
				Lines: []string{"created", "closed"}},
		},
		{
			"field slugged into the id",
			"- kind: bars\n  field: Odd Field!\n",
			StatsCard{ID: "bars-odd-field", Kind: "bars", Title: "Odd Field!", Field: "Odd Field!",
				RecentHours: DefaultStatsHours, NewHours: DefaultStatsHours},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			cards := resolveDoneStats(t, "web:\n  done_stats:\n    cards:\n"+indent(tc.body, "      "))
			if len(cards) != 1 {
				t.Fatalf("len(Cards) = %d, want 1: %+v", len(cards), cards)
			}
			if !reflect.DeepEqual(cards[0], tc.want) {
				t.Errorf("card =\n%+v\nwant\n%+v", cards[0], tc.want)
			}
		})
	}
}

func TestDoneStatsIDsUnique(t *testing.T) {
	in := []StatsCard{
		{Kind: StatsKindBars, Field: "type"},
		{Kind: StatsKindBars, Field: "type"},
		{ID: "bars-type-3", Kind: StatsKindLines},
		{Kind: StatsKindBars, Field: "type"},
		{ID: "lines-14d", Kind: StatsKindBars, Field: "priority"},
		{Kind: StatsKindLines},
	}
	var got []string
	for _, c := range normalizeStatsCards(in, DefaultStatsHours) {
		got = append(got, c.ID)
	}
	want := []string{"bars-type", "bars-type-2", "bars-type-3", "bars-type-4", "lines-14d", "lines-14d-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IDs = %q, want %q", got, want)
	}
}

func TestNormalizeStatsCardsIdempotentAndPure(t *testing.T) {
	in := []StatsCard{
		{Kind: " lines ", Lines: []string{" created ", ""}, Hidden: []string{"created"}},
		{Field: "type"},
		{Field: "type"},
		{Kind: StatsKindLines, SplitBy: "priority"},
	}
	snapshot := []StatsCard{
		{Kind: " lines ", Lines: []string{" created ", ""}, Hidden: []string{"created"}},
		{Field: "type"},
		{Field: "type"},
		{Kind: StatsKindLines, SplitBy: "priority"},
	}

	once := normalizeStatsCards(in, DefaultStatsHours)
	if !reflect.DeepEqual(in, snapshot) {
		t.Errorf("input = %+v, want it untouched: the config is read concurrently", in)
	}
	if twice := normalizeStatsCards(once, DefaultStatsHours); !reflect.DeepEqual(twice, once) {
		t.Errorf("second pass =\n%+v\nwant\n%+v", twice, once)
	}

	once[0].Hidden[0] = "mutated"
	if in[0].Hidden[0] != "created" {
		t.Error("normalized card shares its Hidden list with the input")
	}
}

func TestDoneStatsCardsAccessor(t *testing.T) {
	var nilCfg *Config
	if got := nilCfg.DoneStatsCards(); !reflect.DeepEqual(got, DefaultStatsCards()) {
		t.Errorf("nil config: DoneStatsCards() = %+v, want the defaults", got)
	}
	if got := (&Config{}).DoneStatsCards(); !reflect.DeepEqual(got, DefaultStatsCards()) {
		t.Errorf("literal config: DoneStatsCards() = %+v, want the defaults", got)
	}

	cfg := &Config{Web: WebConfig{DoneStats: DoneStatsConfig{Cards: []StatsCard{{Kind: StatsKindLines}}}}}
	got := cfg.DoneStatsCards()
	if got[0].Days != DefaultStatsDays || got[0].ID != "lines-14d" {
		t.Errorf("DoneStatsCards()[0] = %+v, want per-card defaults filled", got[0])
	}
	got[0].Lines[0] = "mutated"
	if cfg.Web.DoneStats.Cards[0].Days != 0 || cfg.Web.DoneStats.Cards[0].Lines != nil {
		t.Errorf("config card = %+v, want it unchanged by the accessor and its caller", cfg.Web.DoneStats.Cards[0])
	}
}

func TestDefaultStatsCardsNotAliased(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Web.DoneStats.Cards[3].Lines[0] = "mutated"
	cfg.Web.DoneStats.Cards[0].Field = "mutated"
	cfg.applyDefaults()

	fresh := DefaultStatsCards()
	if fresh[3].Lines[0] != StatsLineCreated || fresh[0].Field != "priority" {
		t.Errorf("DefaultStatsCards() = %+v: a returned config must not alias the defaults", fresh)
	}
}

func TestDoneStatsTypeErrorFailsResolve(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  done_stats:\n    cards:\n      - kind: lines\n        days: abc\n")
	t.Setenv(EnvProjectDir, root)

	if _, err := Resolve(nil); err == nil {
		t.Error("Resolve() error = nil, want a parse error for a non-integer days")
	}
}

// indent prefixes every line of s, which ends in a newline.
func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n"+prefix) + "\n"
}
