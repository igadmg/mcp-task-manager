package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// The roles of the dashboard's status palette. One value per role is shared by
// every surface that shows it: a card's dot and border, a bar segment, a graph
// node, the header's in-progress group.
const (
	ColorRoleTodo       = "todo"
	ColorRoleInProgress = "in_progress"
	ColorRoleDone       = "done"
	ColorRoleRecent     = "recent"
	ColorRoleNew        = "new"
)

// ColorHues and ColorShades make up the allowlist of colour names: every
// "<hue>-<shade>" pair, which is a Tailwind colour. The list is closed because
// the stylesheet is compiled ahead of time: internal/web/assets/input.css
// emits one class per role and name, and TestColorAllowlistMatchesCSS fails if
// the two copies drift. Shade 200 is there for `recent`, 500 for `todo` and
// `done`.
var (
	ColorHues = []string{
		"neutral", "red", "orange", "amber", "lime", "emerald",
		"teal", "sky", "blue", "violet", "fuchsia", "rose",
	}
	ColorShades = []int{200, 300, 400, 500}
)

// DefaultColors is each role's colour when the config names none. They are the
// colours the dashboard had before the palette was configurable.
var DefaultColors = map[string]string{
	ColorRoleTodo:       "neutral-500",
	ColorRoleInProgress: "amber-400",
	ColorRoleDone:       "emerald-500",
	ColorRoleRecent:     "emerald-200",
	ColorRoleNew:        "sky-400",
}

// ColorRoles returns the roles in their fixed order. Every call returns the
// caller's own copy.
func ColorRoles() []string {
	return []string{ColorRoleTodo, ColorRoleInProgress, ColorRoleDone, ColorRoleRecent, ColorRoleNew}
}

// ColorNames returns the allowlist, hue by hue. Every call returns the
// caller's own copy.
func ColorNames() []string {
	out := make([]string, 0, len(ColorHues)*len(ColorShades))
	for _, h := range ColorHues {
		for _, s := range ColorShades {
			out = append(out, h+"-"+strconv.Itoa(s))
		}
	}
	return out
}

// ColorsConfig is `web.colors`: a role to colour-name map. The map is inline
// rather than a struct with one field per role so that a typo such as
// `inprogress:` is reported instead of silently ignored.
type ColorsConfig struct {
	Roles map[string]string `yaml:",inline"`
	// Problems is what ValidateColors found; filled by applyDefaults.
	Problems []string `yaml:"-"`
}

// ValidateColors reports what is wrong with the written palette and board
// window, one line each saying what follows from it. Nothing changes and
// nothing is an error: a typo in a dashboard colour must not break the config
// load, and with it every MCP tool and the CLI. It is pure, and its output
// order does not depend on map iteration.
func ValidateColors(roles map[string]string, newHours, recentHours int) []string {
	var out []string
	known := ColorNames()
	for _, role := range slices.Sorted(maps.Keys(roles)) {
		def, ok := DefaultColors[role]
		if !ok {
			out = append(out, fmt.Sprintf("web.colors.%s: unknown role (known: %v); ignored", role, ColorRoles()))
			continue
		}
		if name := roles[role]; !slices.Contains(known, name) {
			out = append(out, fmt.Sprintf("web.colors.%s: %q is not an allowed colour (hue-shade, e.g. %s); using %s", role, name, def, def))
		}
	}
	if newHours < 0 {
		out = append(out, fmt.Sprintf("web.new_hours: %d is negative; using %d", newHours, DefaultStatsHours))
	}
	if recentHours < 0 {
		out = append(out, fmt.Sprintf("web.recent_hours: %d is negative; using %d", recentHours, DefaultStatsHours))
	}
	return out
}

// Palette returns every role's colour name: the configured one, or the default
// for a role that is absent or names a colour outside the allowlist. It does
// not depend on applyDefaults having run (a nil config or a literal without
// the section gets the defaults), and the result is the caller's own copy.
func (c *Config) Palette() map[string]string {
	out := maps.Clone(DefaultColors)
	if c == nil {
		return out
	}
	known := ColorNames()
	for role := range out {
		if name, ok := c.Web.Colors.Roles[role]; ok && slices.Contains(known, name) {
			out[role] = name
		}
	}
	return out
}

// reportColorProblems writes one line per problem, like reportStatsProblems.
func reportColorProblems(c *Config) {
	for _, p := range c.Web.Colors.Problems {
		fmt.Fprintf(statsWarnTo, "%s: %s\n", ConfigFileName, p)
	}
}
