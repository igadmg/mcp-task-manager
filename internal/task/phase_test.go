package task

import (
	"fmt"
	"slices"
	"testing"
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
