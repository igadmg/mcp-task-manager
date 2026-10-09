package config

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kinds of a Done-column statistics card.
const (
	StatsKindBars  = "bars"
	StatsKindLines = "lines"
)

// Line names of a lines card; also the values a split card's metric takes.
const (
	StatsLineCreated           = "created"
	StatsLineClosed            = "closed"
	StatsLineCreatedCumulative = "created_cumulative"
	StatsLineClosedCumulative  = "closed_cumulative"
)

// DefaultStatsDays is the window of a lines card that names none.
const DefaultStatsDays = 14

// DefaultStatsHours is a bars card's highlight window when it names none, for
// both ends of the bar: the closures over the end of done, and the arrivals
// over the start of todo. One default for both, because the two are the same
// feature seen from its two ends.
const DefaultStatsHours = 24

// statsKinds, statsFieldNames and statsLineNames are the vocabularies a
// written card is checked against. The behaviour behind every field and line
// name lives in internal/task, which imports this package and so cannot be
// asked; TestStatsFieldsMatchConfig and TestStatsLineNamesMatchConfig there
// fail if the lists ever drift apart.
var (
	statsKinds      = []string{StatsKindBars, StatsKindLines}
	statsFieldNames = []string{"priority", "type", "status", "resolution", "created_by"}
	statsLineNames  = []string{
		StatsLineCreated,
		StatsLineClosed,
		StatsLineCreatedCumulative,
		StatsLineClosedCumulative,
	}
)

// StatsFields returns the task fields a bars card can group on and a lines
// card can split by. The order is the fields' own (priority to created_by),
// not the sorted order a diagnostic lists them in. Every call returns the
// caller's own copy.
func StatsFields() []string { return slices.Clone(statsFieldNames) }

// StatsLineNames returns the line names a lines card can draw, cumulative
// variants included. Every call returns the caller's own copy.
func StatsLineNames() []string { return slices.Clone(statsLineNames) }

// DoneStatsConfig defines the statistics cards the board's Done column shows.
type DoneStatsConfig struct {
	// Cards in board order. nil (the key absent, or `cards:`) means the
	// defaults; an empty list (`cards: []`) means no cards.
	Cards []StatsCard `yaml:"cards"`
	// Problems lists what is wrong with the written cards, filled by
	// applyDefaults and reported by Resolve. It is never fatal and never
	// changes Cards - see ValidateStatsCards.
	Problems []StatsCardProblem `yaml:"-"`
}

// StatsCardProblem is one thing wrong with a written Done-statistics card:
// where it is, which card it is, and what the server does about it.
type StatsCardProblem struct {
	// Index is the card's position in web.done_stats.cards.
	Index int
	// ID is the normalized card's ID, so a problem can be tied to the card
	// the board draws (or does not).
	ID string
	// Detail says what is wrong and what follows from it.
	Detail string
}

func (p StatsCardProblem) String() string {
	return fmt.Sprintf("web.done_stats.cards[%d] (%s): %s", p.Index, p.ID, p.Detail)
}

// ValidateStatsCards reports what is wrong with the cards as written, without
// changing them: an invalid entry is kept, normalized and drawn (or left
// empty) exactly as before. Nothing here is an error - a typo in a dashboard
// card must not break the config load, and with it every MCP tool and the
// CLI. See the task's decision.md for the rejected alternatives.
//
// It is pure: it normalizes its own copy and takes the written cards for the
// one check normalization erases (a negative days).
func ValidateStatsCards(written []StatsCard) []StatsCardProblem {
	// The board window only fills a blank new_hours, which no check looks at.
	cards := normalizeStatsCards(written, DefaultStatsHours)
	if len(cards) != len(written) {
		// written named no cards at all, so these are the defaults,
		// which are valid by construction.
		return nil
	}
	var out []StatsCardProblem
	for i, c := range cards {
		for _, detail := range statsCardProblems(c, written[i]) {
			out = append(out, StatsCardProblem{Index: i, ID: c.ID, Detail: detail})
		}
	}
	return out
}

// statsCardProblems checks one normalized card, with the card as written for
// the values normalization has already repaired.
func statsCardProblems(c, written StatsCard) []string {
	var out []string
	switch c.Kind {
	case StatsKindBars:
		switch {
		case c.Field == "":
			out = append(out, fmt.Sprintf("a bars card needs a field (one of %s); the card stays empty", oneOf(statsFieldNames)))
		case !slices.Contains(statsFieldNames, c.Field):
			out = append(out, fmt.Sprintf("unknown field %q (one of %s); the card stays empty", c.Field, oneOf(statsFieldNames)))
		}
		// Like days: only a negative value proves the key was written,
		// since yaml decodes an absent key and a written 0 alike.
		if written.RecentHours < 0 {
			out = append(out, fmt.Sprintf("recent_hours: %d is not a positive number; using %d", written.RecentHours, c.RecentHours))
		}
		if written.NewHours < 0 {
			out = append(out, fmt.Sprintf("new_hours: %d is not a positive number; using %d", written.NewHours, c.NewHours))
		}
	case StatsKindLines:
		// Only a negative value proves the key was written: yaml decodes
		// an absent days and `days: 0` alike, so 0 stays a silent default.
		if written.Days < 0 {
			out = append(out, fmt.Sprintf("days: %d is not a positive number; using %d", written.Days, c.Days))
		}
		if c.SplitBy != "" {
			if !slices.Contains(statsFieldNames, c.SplitBy) {
				out = append(out, fmt.Sprintf("unknown split_by %q (one of %s); the card stays empty", c.SplitBy, oneOf(statsFieldNames)))
			}
			if !slices.Contains(statsLineNames, c.Metric) {
				out = append(out, fmt.Sprintf("unknown metric %q (one of %s); the card stays empty", c.Metric, oneOf(statsLineNames)))
			}
			// A split card's hidden entries name field values, and which
			// values exist is a property of the backlog, not the config.
			return out
		}
		for _, name := range c.Lines {
			if !slices.Contains(statsLineNames, name) {
				out = append(out, fmt.Sprintf("unknown line %q (one of %s); it is not drawn", name, oneOf(statsLineNames)))
			}
		}
		for _, name := range c.Hidden {
			if !slices.Contains(c.Lines, name) {
				out = append(out, fmt.Sprintf("hidden line %q is not one of the card's lines; it has no effect", name))
			}
		}
	default:
		out = append(out, fmt.Sprintf("unknown kind %q (one of %s); the card is not drawn", c.Kind, oneOf(statsKinds)))
	}
	return out
}

// oneOf lists a vocabulary for a diagnostic, sorted so the message is stable.
func oneOf(names []string) string {
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	return strings.Join(sorted, ", ")
}

// statsWarnTo is where Resolve reports a card's problems; a test swaps it.
// Never stdout: in stdio mode that is the JSON-RPC channel.
var statsWarnTo io.Writer = os.Stderr

// reportStatsProblems writes one line per problem. It is the only place the
// diagnostics leave the config, so a consumer that wants to show them itself
// reads Web.DoneStats.Problems instead.
func reportStatsProblems(c *Config) {
	for _, p := range c.Web.DoneStats.Problems {
		fmt.Fprintf(statsWarnTo, "%s: %s\n", ConfigFileName, p)
	}
}

// StatsCard is one Done-column card: a bars card groups the tasks on one
// field, a lines card charts per-day counts over a window. Blank fields are
// filled by normalizeStatsCards; nothing here is validated.
type StatsCard struct {
	// ID is the card's stable key (the viewer's line toggles are stored
	// under it). Derived from the card's content when blank.
	ID string `yaml:"id,omitempty"`
	// Kind is bars or lines; when blank, a card with a field is bars and
	// any other card is lines.
	Kind string `yaml:"kind,omitempty"`
	// Title is derived from the content when blank.
	Title string `yaml:"title,omitempty"`
	// Field is the task field a bars card groups on.
	Field string `yaml:"field,omitempty"`
	// RecentHours is a bars card's outflow window: the done tasks closed
	// inside it are highlighted over the end of the done run.
	RecentHours int `yaml:"recent_hours,omitempty"`
	// NewHours is a bars card's inflow window: the todo tasks created
	// inside it are highlighted over the start of the todo run. It is a
	// separate key from RecentHours on purpose - arrivals and departures
	// are interesting at different scales ("created in the last week and
	// still waiting" against "closed since yesterday").
	NewHours int `yaml:"new_hours,omitempty"`
	// Days is a lines card's window, today included.
	Days int `yaml:"days,omitempty"`
	// Lines are the line names of a lines card without split_by.
	Lines []string `yaml:"lines,omitempty"`
	// Hidden lists the lines (or split values) that start switched off.
	Hidden []string `yaml:"hidden,omitempty"`
	// SplitBy draws one line per value of this field.
	SplitBy string `yaml:"split_by,omitempty"`
	// Metric is the line name a split card draws per value.
	Metric string `yaml:"metric,omitempty"`
}

// DefaultStatsCards returns the cards shown when the config names none: bars
// for priority, type and resolution, and created/closed over 14 days. Every
// call builds a fresh list, so callers may change it.
func DefaultStatsCards() []StatsCard {
	return defaultStatsCards(DefaultStatsHours)
}

func defaultStatsCards(newHours int) []StatsCard {
	return normalizeStatsCards([]StatsCard{
		{Kind: StatsKindBars, Field: "priority"},
		{Kind: StatsKindBars, Field: "type"},
		{Kind: StatsKindBars, Field: "resolution"},
		{Kind: StatsKindLines, Days: DefaultStatsDays, Lines: []string{StatsLineCreated, StatsLineClosed}},
	}, newHours)
}

// DoneStatsCards returns the Done-column cards with every default filled in.
// It does not depend on applyDefaults having run (a nil config or a literal
// without the section gets the defaults), and the result is the caller's own
// copy.
func (c *Config) DoneStatsCards() []StatsCard {
	if c == nil {
		return DefaultStatsCards()
	}
	return normalizeStatsCards(c.Web.DoneStats.Cards, c.Web.NewHours)
}

// normalizeStatsCards fills what the written cards left out. yaml decodes
// every list item from zero, so per-card defaults cannot come from the
// pre-filled DefaultConfig. It never writes to in, and running it twice
// changes nothing. newHours is the board window (web.new_hours): the arrivals
// window of a bars card that names none; <= 0 means DefaultStatsHours.
func normalizeStatsCards(in []StatsCard, newHours int) []StatsCard {
	if newHours <= 0 {
		newHours = DefaultStatsHours
	}
	if in == nil {
		return defaultStatsCards(newHours)
	}
	out := make([]StatsCard, len(in))
	seen := make(map[string]bool, len(in))
	for i, c := range in {
		c = normalizeStatsCard(c, newHours)
		// Unique in list order: a repeated key gets -2, -3, ...
		base := c.ID
		for n := 2; seen[c.ID]; n++ {
			c.ID = fmt.Sprintf("%s-%d", base, n)
		}
		seen[c.ID] = true
		out[i] = c
	}
	return out
}

func normalizeStatsCard(c StatsCard, newHours int) StatsCard {
	c.ID = strings.TrimSpace(c.ID)
	c.Kind = strings.TrimSpace(c.Kind)
	c.Title = strings.TrimSpace(c.Title)
	c.Field = strings.TrimSpace(c.Field)
	c.SplitBy = strings.TrimSpace(c.SplitBy)
	c.Metric = strings.TrimSpace(c.Metric)
	c.Lines = trimList(c.Lines)
	c.Hidden = trimList(c.Hidden)

	if c.Kind == "" {
		c.Kind = StatsKindLines
		if c.Field != "" {
			c.Kind = StatsKindBars
		}
	}

	var title string
	var key []string
	switch c.Kind {
	case StatsKindBars:
		if c.RecentHours <= 0 {
			c.RecentHours = DefaultStatsHours
		}
		if c.NewHours <= 0 {
			c.NewHours = newHours
		}
		title = humanize(c.Field)
		// Deliberately NOT the windows: a bars card's id is its identity
		// on the board and the key viewer state is stored under, so
		// changing a window must not rename the card.
		key = []string{c.Kind, c.Field}
	case StatsKindLines:
		if c.Days <= 0 {
			c.Days = DefaultStatsDays
		}
		days := fmt.Sprintf("%dd", c.Days)
		if c.SplitBy != "" {
			if c.Metric == "" {
				c.Metric = StatsLineClosed
			}
			title = fmt.Sprintf("%s by %s, %d days", humanize(c.Metric), strings.ReplaceAll(c.SplitBy, "_", " "), c.Days)
			key = []string{c.Kind, c.SplitBy, c.Metric, days}
		} else {
			if len(c.Lines) == 0 {
				c.Lines = []string{StatsLineCreated, StatsLineClosed}
			}
			title = fmt.Sprintf("Last %d days", c.Days)
			key = []string{c.Kind, days}
		}
	default:
		key = []string{c.Kind}
	}

	if c.Title == "" {
		c.Title = title
	}
	if c.ID == "" {
		c.ID = slug(strings.Join(key, "-"))
	}
	return c
}

// trimList returns a fresh copy of list without surrounding spaces and blank
// entries; nil when nothing is left.
func trimList(list []string) []string {
	var out []string
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// humanize turns a field or line name into a title: created_by → "Created by".
func humanize(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	r, size := utf8.DecodeRuneInString(name)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(r)) + name[size:]
}

// slug lowercases s and collapses every run of characters outside [a-z0-9]
// into one dash, so a derived card ID is safe in an attribute or a storage
// key.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	return b.String()
}
