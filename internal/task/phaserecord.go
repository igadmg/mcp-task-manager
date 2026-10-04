package task

import "time"

// PhaseRun is one run of a delivery phase: who started it and when, and -
// once finished - who finished it, when, and what it cost. A phase that is
// redone gets a new run; earlier runs are kept.
type PhaseRun struct {
	StartedAt  time.Time  `json:"started_at"`
	StartedBy  string     `json:"started_by"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	FinishedBy string     `json:"finished_by"`
	// Tokens is what the caller reported the run cost; nil when it reported
	// nothing, so a real 0 stays distinguishable.
	Tokens *int64 `json:"tokens,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Open reports whether the run has not been finished yet.
func (r PhaseRun) Open() bool {
	return r.FinishedAt == nil
}

// finish stamps the run finished now by by, with note and tokens (nil:
// unknown).
func (r *PhaseRun) finish(by, note string, tokens *int64) {
	now := phaseNow()
	r.FinishedAt, r.FinishedBy, r.Note, r.Tokens = &now, by, note, tokens
}

// phaseNow is the time a run is stamped with: UTC, to the second, as the
// record stores it.
func phaseNow() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// PhaseRecord is the content of a task's <phase>.phase file: every run of
// one phase, oldest first.
type PhaseRecord struct {
	Phase Phase      `json:"phase"`
	Runs  []PhaseRun `json:"runs"`
}

// Last returns the latest run, or nil when there is none.
func (r *PhaseRecord) Last() *PhaseRun {
	if r == nil || len(r.Runs) == 0 {
		return nil
	}
	return &r.Runs[len(r.Runs)-1]
}

// Tokens sums the tokens of the finished runs that reported any; ok is
// false when none did.
func (r *PhaseRecord) Tokens() (total int64, ok bool) {
	if r == nil {
		return 0, false
	}
	for _, run := range r.Runs {
		if !run.Open() && run.Tokens != nil {
			total += *run.Tokens
			ok = true
		}
	}
	return total, ok
}

// clone returns a deep copy of r, so a journaled undo can restore it after
// the original was mutated.
func (r *PhaseRecord) clone() *PhaseRecord {
	c := &PhaseRecord{Phase: r.Phase, Runs: make([]PhaseRun, len(r.Runs))}
	for i, run := range r.Runs {
		c.Runs[i] = run
		if run.FinishedAt != nil {
			at := *run.FinishedAt
			c.Runs[i].FinishedAt = &at
		}
		if run.Tokens != nil {
			n := *run.Tokens
			c.Runs[i].Tokens = &n
		}
	}
	return c
}

// TotalTokens sums the tokens of the finished runs of every record that
// reported any; ok is false when none did.
func TotalTokens(recs []PhaseRecord) (total int64, ok bool) {
	for i := range recs {
		if n, has := recs[i].Tokens(); has {
			total += n
			ok = true
		}
	}
	return total, ok
}

// PhaseSummary is what a board card shows of an in-progress task's phase
// records.
type PhaseSummary struct {
	// Current is the phase of the latest started run: the card's lane;
	// empty when no record has a run.
	Current Phase
	// Run is that phase's latest run.
	Run PhaseRun
	// Tokens sums the finished runs of every phase that reported any;
	// HasTokens is false when none did.
	Tokens    int64
	HasTokens bool
}

// summarizePhases picks the latest started run across recs - ties go to the
// later phase - and totals the tokens.
func summarizePhases(recs []PhaseRecord) PhaseSummary {
	var sum PhaseSummary
	sum.Tokens, sum.HasTokens = TotalTokens(recs)
	for i := range recs {
		rec := &recs[i]
		last := rec.Last()
		if last == nil {
			continue
		}
		later := last.StartedAt.After(sum.Run.StartedAt) ||
			(last.StartedAt.Equal(sum.Run.StartedAt) && rec.Phase.Order() > sum.Current.Order())
		if sum.Current == "" || later {
			sum.Current, sum.Run = rec.Phase, *last
		}
	}
	return sum
}

// PhaseFinish is what finish_phase records on the open run besides the
// finish time and user.
type PhaseFinish struct {
	// Tokens is the run's cost as reported by the caller; nil when unknown.
	Tokens *int64
	Note   string
}
