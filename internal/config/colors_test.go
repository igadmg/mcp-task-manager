package config

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// loadColors resolves a project whose mcp-tasks.yaml holds the given web
// section and returns the config and what was written to the diagnostics.
func loadColors(t *testing.T, web string) (*Config, string) {
	t.Helper()
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n"+indent(web, "  "))
	t.Setenv(EnvProjectDir, root)
	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)
	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return cfg, buf.String()
}

func TestColorAllowlist(t *testing.T) {
	names := ColorNames()
	if len(names) != 48 {
		t.Fatalf("len(ColorNames()) = %d, want 48", len(names))
	}
	for role, name := range DefaultColors {
		if !slices.Contains(names, name) {
			t.Errorf("default %s = %q is not in the allowlist", role, name)
		}
	}
	for _, role := range ColorRoles() {
		if _, ok := DefaultColors[role]; !ok {
			t.Errorf("role %q has no default", role)
		}
	}
	if len(ColorRoles()) != len(DefaultColors) {
		t.Errorf("roles %v and defaults %v differ", ColorRoles(), DefaultColors)
	}
}

func TestColorListsAreCopies(t *testing.T) {
	ColorNames()[0] = "mutated"
	ColorRoles()[0] = "mutated"
	if ColorNames()[0] != "neutral-200" || ColorRoles()[0] != ColorRoleTodo {
		t.Errorf("a caller changed the package lists: %q, %q", ColorNames()[0], ColorRoles()[0])
	}
}

func TestValidateColors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		roles    map[string]string
		newHours int
		recent   int
		want     []string
	}{
		{"nothing written", nil, 0, 0, nil},
		{"valid name", map[string]string{"in_progress": "sky-400"}, 24, 24, nil},
		{"unknown role", map[string]string{"inprogress": "sky-400"}, 24, 24, []string{`web.colors.inprogress: unknown role`}},
		{"unknown name", map[string]string{"in_progress": "ambr-400"}, 24, 24,
			[]string{`web.colors.in_progress: "ambr-400" is not an allowed colour (hue-shade, e.g. amber-400); using amber-400`}},
		{"wrong case", map[string]string{"new": "Sky-400"}, 24, 24, []string{`web.colors.new: "Sky-400" is not an allowed colour`}},
		{"whitespace", map[string]string{"done": " emerald-500"}, 24, 24, []string{`web.colors.done: " emerald-500" is not an allowed colour`}},
		{"negative window", nil, -3, 0, []string{`web.new_hours: -3 is negative; using 24`}},
		{"negative recent window", nil, 24, -5, []string{`web.recent_hours: -5 is negative; using 24`}},
		{"sorted", map[string]string{"todo": "x", "done": "y"}, 24, 24,
			[]string{`web.colors.done: "y"`, `web.colors.todo: "x"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateColors(tc.roles, tc.newHours, tc.recent)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %d lines", got, len(tc.want))
			}
			for i := range got {
				if !strings.HasPrefix(got[i], tc.want[i]) {
					t.Errorf("line %d = %q, want prefix %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPaletteDefaults(t *testing.T) {
	for name, web := range map[string]string{"web empty": "enabled: false\n", "colors empty": "colors: {}\n"} {
		t.Run(name, func(t *testing.T) {
			cfg, out := loadColors(t, web)
			if !reflect.DeepEqual(cfg.Palette(), DefaultColors) {
				t.Errorf("Palette() = %v, want the defaults", cfg.Palette())
			}
			if out != "" || len(cfg.Web.Colors.Problems) != 0 {
				t.Errorf("unexpected report %q / %v", out, cfg.Web.Colors.Problems)
			}
		})
	}
}

func TestPalettePartialAndInvalid(t *testing.T) {
	cfg, out := loadColors(t, "colors:\n  in_progress: sky-400\n  done: nope\n  bogus: red-400\n")
	want := map[string]string{}
	for k, v := range DefaultColors {
		want[k] = v
	}
	want["in_progress"] = "sky-400"
	if got := cfg.Palette(); !reflect.DeepEqual(got, want) {
		t.Errorf("Palette() = %v, want %v", got, want)
	}
	if len(cfg.Web.Colors.Problems) != 2 {
		t.Errorf("Problems = %q, want 2 lines", cfg.Web.Colors.Problems)
	}
	if strings.Count(out, "\n") != 2 || !strings.HasPrefix(out, ConfigFileName+": web.colors.") {
		t.Errorf("stderr = %q, want two prefixed lines", out)
	}
}

func TestPaletteNilAndZeroAreDefaultsAndCopies(t *testing.T) {
	var nilCfg *Config
	for _, c := range []*Config{nilCfg, {}} {
		p := c.Palette()
		if !reflect.DeepEqual(p, DefaultColors) {
			t.Errorf("Palette() = %v, want the defaults", p)
		}
		p["done"] = "mutated"
	}
	if DefaultColors["done"] != "emerald-500" {
		t.Error("mutating Palette() changed DefaultColors")
	}
	cfg := &Config{Web: WebConfig{Colors: ColorsConfig{Roles: map[string]string{"done": "red-500"}}}}
	cfg.Palette()["done"] = "mutated"
	if cfg.Web.Colors.Roles["done"] != "red-500" {
		t.Error("mutating Palette() changed the config")
	}
}

func TestNewHoursDefaultAndInheritance(t *testing.T) {
	cfg, _ := loadColors(t, "enabled: false\n")
	if cfg.Web.NewHours != DefaultStatsHours {
		t.Errorf("NewHours = %d, want %d", cfg.Web.NewHours, DefaultStatsHours)
	}

	cfg, out := loadColors(t, "new_hours: -5\n")
	if cfg.Web.NewHours != DefaultStatsHours || !strings.Contains(out, "web.new_hours: -5") {
		t.Errorf("NewHours = %d, stderr %q: want 24 and a report", cfg.Web.NewHours, out)
	}

	// Default cards, bars cards that name no window, and a card that names
	// its own.
	cfg, _ = loadColors(t, "new_hours: 72\n")
	for _, c := range cfg.DoneStatsCards() {
		if c.Kind == StatsKindBars && c.NewHours != 72 {
			t.Errorf("default card %s NewHours = %d, want 72", c.ID, c.NewHours)
		}
	}
	cfg, _ = loadColors(t, "new_hours: 72\ndone_stats:\n  cards:\n    - {kind: bars, field: type}\n    - {kind: bars, field: priority, new_hours: 6}\n")
	cards := cfg.DoneStatsCards()
	if cards[0].NewHours != 72 || cards[1].NewHours != 6 {
		t.Errorf("NewHours = %d, %d, want 72 and 6", cards[0].NewHours, cards[1].NewHours)
	}
	if cards[0].RecentHours != 0 {
		t.Errorf("RecentHours = %d, want 0: the per-card window is gone, web.recent_hours replaces it", cards[0].RecentHours)
	}

	// The nil-safe accessor on a literal config uses the board window too.
	lit := &Config{Web: WebConfig{NewHours: 12}}
	for _, c := range lit.DoneStatsCards() {
		if c.Kind == StatsKindBars && c.NewHours != 12 {
			t.Errorf("literal config card %s NewHours = %d, want 12", c.ID, c.NewHours)
		}
	}
}

func TestRecentHoursDefaultAndAccessor(t *testing.T) {
	cfg, _ := loadColors(t, "enabled: false\n")
	if cfg.Web.RecentHours != DefaultStatsHours || cfg.RecentHours() != DefaultStatsHours {
		t.Errorf("RecentHours = %d / %d, want %d", cfg.Web.RecentHours, cfg.RecentHours(), DefaultStatsHours)
	}

	cfg, _ = loadColors(t, "recent_hours: 48\n")
	if cfg.Web.RecentHours != 48 || cfg.RecentHours() != 48 {
		t.Errorf("RecentHours = %d / %d, want 48", cfg.Web.RecentHours, cfg.RecentHours())
	}

	cfg, out := loadColors(t, "recent_hours: -5\n")
	if cfg.Web.RecentHours != DefaultStatsHours || !strings.Contains(out, "web.recent_hours: -5") {
		t.Errorf("RecentHours = %d, stderr %q: want 24 and a report", cfg.Web.RecentHours, out)
	}

	// A recent_hours written inside a card is reported and does not move the
	// shared window.
	cfg, out = loadColors(t, "recent_hours: 48\ndone_stats:\n  cards:\n    - {kind: bars, field: type, recent_hours: 6}\n")
	if cfg.RecentHours() != 48 || !strings.Contains(out, "recent_hours: 6 on a card is ignored") {
		t.Errorf("RecentHours = %d, stderr %q: want 48 and a report", cfg.RecentHours(), out)
	}

	// The accessor works without applyDefaults: nil and literal configs.
	var nilCfg *Config
	if got := nilCfg.RecentHours(); got != DefaultStatsHours {
		t.Errorf("nil config RecentHours() = %d, want %d", got, DefaultStatsHours)
	}
	if got := (&Config{}).RecentHours(); got != DefaultStatsHours {
		t.Errorf("literal config RecentHours() = %d, want %d", got, DefaultStatsHours)
	}
	if got := (&Config{Web: WebConfig{RecentHours: 12}}).RecentHours(); got != 12 {
		t.Errorf("literal config RecentHours() = %d, want 12", got)
	}
}

func TestColorProblemsGoToStderrOncePerLoad(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  colors:\n    new: nope\n")
	t.Setenv(EnvProjectDir, root)
	var buf bytes.Buffer
	swapStatsWarnTo(t, &buf)

	if _, err := Resolve(nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Errorf("Resolve wrote %d lines, want 1: %q", got, buf.String())
	}
	buf.Reset()
	if _, err := LoadForRoot(root, ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Errorf("LoadForRoot wrote %d lines, want 1: %q", got, buf.String())
	}
	if !strings.HasPrefix(buf.String(), ConfigFileName+": web.colors.new:") {
		t.Errorf("line = %q, want the file-name prefix", buf.String())
	}
}
