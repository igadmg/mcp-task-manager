package task

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestPhaseFromFiles(t *testing.T) {
	tests := []struct {
		names []string
		want  Phase
	}{
		{nil, PhaseResearch},
		{[]string{}, PhaseResearch},
		{[]string{"task"}, PhaseResearch},
		{[]string{"task.md"}, PhaseResearch},
		{[]string{"TASK"}, PhaseResearch},

		{[]string{"research"}, PhaseDesign},
		{[]string{"task", "research"}, PhaseDesign},
		{[]string{"research.md"}, PhaseDesign},
		{[]string{"RESEARCH"}, PhaseDesign},

		{[]string{"design"}, PhasePlanning},
		{[]string{"research", "design"}, PhasePlanning},
		{[]string{"design.md"}, PhasePlanning},
		{[]string{"Design.md"}, PhasePlanning},

		{[]string{"plan"}, PhaseImplementation},
		{[]string{"task", "research", "design", "plan"}, PhaseImplementation},
		{[]string{"plan", "task", "design", "research"}, PhaseImplementation},
		{[]string{"implementation"}, PhaseImplementation},
		{[]string{"plan.md"}, PhaseImplementation},
		{[]string{"PLAN"}, PhaseImplementation},
		{[]string{"Plan.MD"}, PhaseImplementation},

		// Near misses: each one alone proves nothing.
		{[]string{"notes.md"}, PhaseResearch},
		{[]string{"plan.txt"}, PhaseResearch},
		{[]string{"plan-v2"}, PhaseResearch},
		{[]string{"myplan"}, PhaseResearch},
		{[]string{"research-notes.md"}, PhaseResearch},
		{[]string{"plan.md.md"}, PhaseResearch},
		{[]string{"plan.tmp"}, PhaseResearch},
		{[]string{".md"}, PhaseResearch},
		{[]string{"md"}, PhaseResearch},

		{[]string{"notes.md", "Research.md", "x"}, PhaseDesign},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.names), func(t *testing.T) {
			if got := phaseFromFiles(tt.names); got != tt.want {
				t.Errorf("phaseFromFiles(%q) = %q, want %q", tt.names, got, tt.want)
			}
			// Order-independent: the reversed list proves the same phase.
			reversed := slices.Clone(tt.names)
			slices.Reverse(reversed)
			if got := phaseFromFiles(reversed); got != tt.want {
				t.Errorf("phaseFromFiles(%q) = %q, want %q", reversed, got, tt.want)
			}
		})
	}
}

func TestPhaseOrder(t *testing.T) {
	for i, p := range Phases() {
		if got := p.Order(); got != i {
			t.Errorf("Phase(%q).Order() = %d, want %d", p, got, i)
		}
	}
	for _, p := range []Phase{"", "bogus"} {
		if got := p.Order(); got != 0 {
			t.Errorf("Phase(%q).Order() = %d, want 0", p, got)
		}
	}
}

func TestParsePhase(t *testing.T) {
	for _, p := range Phases() {
		if got, err := ParsePhase(string(p)); err != nil || got != p {
			t.Errorf("ParsePhase(%q) = %q, %v", p, got, err)
		}
	}
	for _, bad := range []string{"", "plan", "Research", "testing"} {
		_, err := ParsePhase(bad)
		want := fmt.Sprintf("unknown phase %q (want research, design, planning, implementation)", bad)
		if err == nil || err.Error() != want {
			t.Errorf("ParsePhase(%q) error = %v, want %q", bad, err, want)
		}
	}
}

func TestPhasePrev(t *testing.T) {
	tests := []struct {
		p      Phase
		want   Phase
		wantOK bool
	}{
		{PhaseResearch, "", false},
		{PhaseDesign, PhaseResearch, true},
		{PhasePlanning, PhaseDesign, true},
		{PhaseImplementation, PhasePlanning, true},
		{"bogus", "", false},
	}
	for _, tt := range tests {
		if got, ok := tt.p.Prev(); got != tt.want || ok != tt.wantOK {
			t.Errorf("%q.Prev() = %q, %v; want %q, %v", tt.p, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestPhaseFileName(t *testing.T) {
	if got := PhaseFileName(PhasePlanning); got != "planning.phase" {
		t.Errorf("PhaseFileName(planning) = %q", got)
	}
	if got := PhaseStrings(); !slices.Equal(got, []string{"research", "design", "planning", "implementation"}) {
		t.Errorf("PhaseStrings() = %v", got)
	}
}

func TestIsReservedFileName(t *testing.T) {
	for _, name := range []string{"research.phase", "Design.PHASE", "x.phase", "research.phase.", "plan.phase .", ".phase"} {
		if !IsReservedFileName(name) {
			t.Errorf("IsReservedFileName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"phase.md", "research", "phase", "research.phases", "design.phase.md"} {
		if IsReservedFileName(name) {
			t.Errorf("IsReservedFileName(%q) = true, want false", name)
		}
	}
}

func TestPhaseRecordTokens(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	done := time.Date(2026, 10, 4, 13, 0, 0, 0, time.UTC)
	rec := &PhaseRecord{Phase: PhaseDesign, Runs: []PhaseRun{
		{FinishedAt: &done, Tokens: n(100)},
		{FinishedAt: &done},               // finished, no tokens reported
		{FinishedAt: &done, Tokens: n(0)}, // a real 0
		{Tokens: n(5000)},                 // open: not counted
	}}
	if total, ok := rec.Tokens(); total != 100 || !ok {
		t.Errorf("Tokens() = %d, %v; want 100, true", total, ok)
	}
	if last := rec.Last(); last == nil || !last.Open() {
		t.Errorf("Last() = %+v, want the open run", last)
	}
	none := &PhaseRecord{Phase: PhaseDesign, Runs: []PhaseRun{{FinishedAt: &done}}}
	if total, ok := none.Tokens(); total != 0 || ok {
		t.Errorf("Tokens() without reports = %d, %v; want 0, false", total, ok)
	}
	if (&PhaseRecord{}).Last() != nil {
		t.Error("Last() of an empty record is not nil")
	}
}
