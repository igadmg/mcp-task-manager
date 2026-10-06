package config

import (
	"fmt"
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

// DoneStatsConfig defines the statistics cards the board's Done column shows.
type DoneStatsConfig struct {
	// Cards in board order. nil (the key absent, or `cards:`) means the
	// defaults; an empty list (`cards: []`) means no cards.
	Cards []StatsCard `yaml:"cards"`
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
	return normalizeStatsCards([]StatsCard{
		{Kind: StatsKindBars, Field: "priority"},
		{Kind: StatsKindBars, Field: "type"},
		{Kind: StatsKindBars, Field: "resolution"},
		{Kind: StatsKindLines, Days: DefaultStatsDays, Lines: []string{StatsLineCreated, StatsLineClosed}},
	})
}

// DoneStatsCards returns the Done-column cards with every default filled in.
// It does not depend on applyDefaults having run (a nil config or a literal
// without the section gets the defaults), and the result is the caller's own
// copy.
func (c *Config) DoneStatsCards() []StatsCard {
	if c == nil {
		return DefaultStatsCards()
	}
	return normalizeStatsCards(c.Web.DoneStats.Cards)
}

// normalizeStatsCards fills what the written cards left out. yaml decodes
// every list item from zero, so per-card defaults cannot come from the
// pre-filled DefaultConfig. It never writes to in, and running it twice
// changes nothing.
func normalizeStatsCards(in []StatsCard) []StatsCard {
	if in == nil {
		return DefaultStatsCards()
	}
	out := make([]StatsCard, len(in))
	seen := make(map[string]bool, len(in))
	for i, c := range in {
		c = normalizeStatsCard(c)
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

func normalizeStatsCard(c StatsCard) StatsCard {
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
		title = humanize(c.Field)
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
