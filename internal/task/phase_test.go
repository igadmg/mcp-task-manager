package task

import (
	"fmt"
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
		})
	}
}

// permutations returns every ordering of names.
func permutations(names []string) [][]string {
	if len(names) <= 1 {
		return [][]string{append([]string(nil), names...)}
	}
	var out [][]string
	for i := range names {
		rest := make([]string, 0, len(names)-1)
		rest = append(rest, names[:i]...)
		rest = append(rest, names[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]string{names[i]}, p...))
		}
	}
	return out
}

func TestPhaseFromFilesIsOrderIndependent(t *testing.T) {
	cases := []struct {
		names []string
		count int
		want  Phase
	}{
		{[]string{"task", "research", "design", "plan"}, 24, PhaseImplementation},
		{[]string{"task", "research", "design"}, 6, PhasePlanning},
	}
	for _, c := range cases {
		perms := permutations(c.names)
		if len(perms) != c.count {
			t.Fatalf("permutations(%q) = %d orders, want %d", c.names, len(perms), c.count)
		}
		for _, p := range perms {
			if got := phaseFromFiles(p); got != c.want {
				t.Errorf("phaseFromFiles(%q) = %q, want %q", p, got, c.want)
			}
		}
	}
}

func TestPhaseOrder(t *testing.T) {
	tests := []struct {
		phase Phase
		want  int
	}{
		{PhaseResearch, 0},
		{PhaseDesign, 1},
		{PhasePlanning, 2},
		{PhaseImplementation, 3},
		{Phase(""), 0},
		{Phase("bogus"), 0},
	}
	for _, tt := range tests {
		if got := tt.phase.Order(); got != tt.want {
			t.Errorf("Phase(%q).Order() = %d, want %d", tt.phase, got, tt.want)
		}
	}
}
