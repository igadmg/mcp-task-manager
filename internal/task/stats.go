package task

import (
	"slices"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

// This file computes the board's Done-column statistics from the configured
// cards (config.StatsCard). It is pure: boardSnapshot feeds it the index it
// already read, so the statistics and the columns come from one read under
// one lock.

// StatsCard is one computed Done-column card, in config order. A card whose
// kind, field, lines or metric are unknown comes back without data.
type StatsCard struct {
	ID    string
	Kind  string
	Title string
	// Field is the field a bars card groups on, or a split card's split_by
	// (empty for a lines card without one).
	Field string
	// Bars holds a bars card's rows: one per value present, in value order.
	Bars []StatsBar
	// Days holds a lines card's days: the start (local midnight) of each,
	// oldest first, today last.
	Days []time.Time
	// Lines holds a lines card's lines; each has one value per day.
	Lines []StatsLine
}

// StatsBar is one value's row in a bars card.
type StatsBar struct {
	Value      string
	Total      int
	Done       int
	InProgress int
	Todo       int
	// ClosedRecently counts the done tasks closed in the last 24 h; it is
	// part of Done.
	ClosedRecently int
}

// StatsLine is one line of a lines card.
type StatsLine struct {
	// Key is the line name, or the field value of a split card.
	Key    string
	Values []int
	// Hidden says the line starts switched off: the card lists Key in its
	// hidden lines.
	Hidden bool
	// Cumulative says Values are a running total over the window, not
	// per-day counts.
	Cumulative bool
}

// statsRecent is the window ClosedRecently counts.
const statsRecent = 24 * time.Hour

// computeStats computes every card over all (the active index, subtasks
// included). types is the configured task_types order; now is the instant
// the 24 h window ends at, and its Location() is the zone the days are
// counted in.
func computeStats(all []*Task, cards []config.StatsCard, types []string, now time.Time) []StatsCard {
	out := make([]StatsCard, 0, len(cards))
	for _, c := range cards {
		card := StatsCard{ID: c.ID, Kind: c.Kind, Title: c.Title}
		switch c.Kind {
		case config.StatsKindBars:
			card.Field = c.Field
			card.Bars = statsBars(all, c.Field, types, now)
		case config.StatsKindLines:
			card.Field = c.SplitBy
			card.Days, card.Lines = statsLines(all, c, types, now)
		}
		out = append(out, card)
	}
	return out
}

func statsBars(all []*Task, field string, types []string, now time.Time) []StatsBar {
	rows := make(map[string]*StatsBar)
	for _, t := range all {
		v, ok := statsValue(t, field)
		if !ok {
			continue
		}
		row := rows[v]
		if row == nil {
			row = &StatsBar{Value: v}
			rows[v] = row
		}
		row.Total++
		switch t.Status {
		case StatusDone:
			row.Done++
			if ct := closeTime(t); ct.After(now.Add(-statsRecent)) && !ct.After(now) {
				row.ClosedRecently++
			}
		case StatusInProgress:
			row.InProgress++
		case StatusTodo:
			row.Todo++
		}
	}

	var bars []StatsBar
	for _, v := range statsOrder(field, types, rows) {
		bars = append(bars, *rows[v])
	}
	return bars
}

// statsLines returns a lines card's days and lines.
func statsLines(all []*Task, c config.StatsCard, types []string, now time.Time) ([]time.Time, []StatsLine) {
	days, bucket := dayWindow(now, c.Days)
	line := func(key string, values []int, cumulative bool) StatsLine {
		if values == nil {
			values = make([]int, len(days))
		}
		if cumulative {
			values = runningSum(values)
		}
		return StatsLine{Key: key, Values: values, Hidden: slices.Contains(c.Hidden, key), Cumulative: cumulative}
	}

	if c.SplitBy != "" {
		base, cumulative, ok := statsMetric(c.Metric)
		if !ok {
			return days, nil
		}
		perValue := countDays(all, base, len(days), bucket, func(t *Task) (string, bool) {
			return statsValue(t, c.SplitBy)
		})
		var lines []StatsLine
		for _, v := range statsOrder(c.SplitBy, types, perValue) {
			lines = append(lines, line(v, perValue[v], cumulative))
		}
		return days, lines
	}

	var lines []StatsLine
	seen := make(map[string]bool)
	for _, name := range c.Lines {
		base, cumulative, ok := statsMetric(name)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		counts := countDays(all, base, len(days), bucket, func(*Task) (string, bool) { return name, true })
		lines = append(lines, line(name, counts[name], cumulative))
	}
	return days, lines
}

// countDays counts the tasks of a base metric per key and per day of an
// n-day window: keyOf names a task's key (false skips it), bucket its day.
// A key appears only when it counted at least one task.
func countDays(all []*Task, base string, n int, bucket func(time.Time) (int, bool), keyOf func(*Task) (string, bool)) map[string][]int {
	counts := make(map[string][]int)
	for _, t := range all {
		at, ok := metricTime(t, base)
		if !ok {
			continue
		}
		i, ok := bucket(at)
		if !ok {
			continue
		}
		key, ok := keyOf(t)
		if !ok {
			continue
		}
		if counts[key] == nil {
			counts[key] = make([]int, n)
		}
		counts[key][i]++
	}
	return counts
}

// statsMetric splits a line name into its per-day base (created or closed)
// and whether it is the running total; ok is false for an unknown name.
func statsMetric(name string) (base string, cumulative, ok bool) {
	switch name {
	case config.StatsLineCreated, config.StatsLineClosed:
		return name, false, true
	case config.StatsLineCreatedCumulative:
		return config.StatsLineCreated, true, true
	case config.StatsLineClosedCumulative:
		return config.StatsLineClosed, true, true
	}
	return "", false, false
}

// metricTime is when t counts for a base metric: its creation, or, for a
// done task, its close.
func metricTime(t *Task, base string) (time.Time, bool) {
	switch base {
	case config.StatsLineCreated:
		return t.CreatedAt, true
	case config.StatsLineClosed:
		if t.Status == StatusDone {
			return closeTime(t), true
		}
	}
	return time.Time{}, false
}

// closeTime is when a done task closed: closed_at, or updated_at for a task
// closed before closed_at existed.
func closeTime(t *Task) time.Time {
	if t.ClosedAt != nil {
		return *t.ClosedAt
	}
	return t.UpdatedAt
}

// dayWindow returns the starts of the n calendar days ending today, in
// now's location, and a bucket function that maps an instant to its day's
// index (false outside the window). Days are keyed by their civil date, so
// a 23- or 25-hour DST day buckets like any other.
func dayWindow(now time.Time, n int) ([]time.Time, func(time.Time) (int, bool)) {
	if n < 1 {
		n = 1
	}
	type civil struct {
		y int
		m time.Month
		d int
	}
	loc := now.Location()
	y, m, d := now.Date()
	days := make([]time.Time, n)
	index := make(map[civil]int, n)
	for i := range days {
		day := time.Date(y, m, d-(n-1-i), 0, 0, 0, 0, loc)
		days[i] = day
		dy, dm, dd := day.Date()
		index[civil{dy, dm, dd}] = i
	}
	bucket := func(at time.Time) (int, bool) {
		if at.IsZero() {
			return 0, false
		}
		ay, am, ad := at.In(loc).Date()
		i, ok := index[civil{ay, am, ad}]
		return i, ok
	}
	return days, bucket
}

// runningSum returns the running total of values, starting from 0 at the
// window's first day.
func runningSum(values []int) []int {
	out := make([]int, len(values))
	sum := 0
	for i, v := range values {
		sum += v
		out[i] = sum
	}
	return out
}

// statsField is a field statistics group on: a task's value of it, and the
// field's domain order given the configured task_types.
type statsField struct {
	value func(*Task) string
	order func(types []string) []string
}

var statsFields = map[string]statsField{
	"priority": {
		value: func(t *Task) string { return string(t.Priority) },
		order: func([]string) []string { return strs(Priorities()) },
	},
	"type": {
		value: func(t *Task) string { return t.Type },
		order: func(types []string) []string { return types },
	},
	"status": {
		value: func(t *Task) string { return string(t.Status) },
		order: func([]string) []string { return strs(Statuses()) },
	},
	// A done task's effective resolution, so open tasks have none.
	"resolution": {
		value: func(t *Task) string { return string(t.EffectiveResolution()) },
		order: func([]string) []string { return ResolutionStrings() },
	},
	"created_by": {
		value: func(t *Task) string { return t.CreatedBy },
		order: func([]string) []string { return nil },
	},
}

// statsValue is t's value of a card field; false when the value is empty or
// the field is not one statistics group on.
func statsValue(t *Task, field string) (string, bool) {
	f, ok := statsFields[field]
	if !ok {
		return "", false
	}
	v := f.value(t)
	return v, v != ""
}

// statsOrder orders the values present in a field's domain order (priority
// critical to low, type in task_types order, status todo to done,
// resolution in Resolutions() order), followed by any other present value
// alphabetically.
func statsOrder[V any](field string, types []string, present map[string]V) []string {
	var known []string
	if f, ok := statsFields[field]; ok {
		known = f.order(types)
	}

	out := make([]string, 0, len(present))
	listed := make(map[string]bool, len(known))
	for _, v := range known {
		if _, ok := present[v]; ok && !listed[v] {
			out = append(out, v)
		}
		listed[v] = true
	}
	var rest []string
	for v := range present {
		if !listed[v] {
			rest = append(rest, v)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}
