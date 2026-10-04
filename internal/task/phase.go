package task

import (
	"slices"
	"strings"
)

// Phase is the delivery-workflow phase an in-progress task is in. It is
// derived on read from the names of the task's attached workflow files and
// never stored.
type Phase string

const (
	PhaseResearch       Phase = "research"
	PhaseDesign         Phase = "design"
	PhasePlanning       Phase = "planning"
	PhaseImplementation Phase = "implementation"
)

// Phases lists every phase in workflow order.
func Phases() []Phase {
	return []Phase{
		PhaseResearch,
		PhaseDesign,
		PhasePlanning,
		PhaseImplementation,
	}
}

// Order is the phase's index in Phases. Anything unknown, the empty Phase
// included, is 0: no information reads as the earliest phase, and Order is
// always a valid index into Phases.
func (p Phase) Order() int {
	return max(slices.Index(Phases(), p), 0)
}

// phaseMarkers maps a normalized attached-file name to the phase a task
// has reached once that file exists. The names are the artifacts the
// begin_task skill writes (plugins/mcp-task-manager/skills/begin_task/SKILL.md,
// mirrored in .claude/skills/begin_task/SKILL.md): each one is saved when
// its phase ends, so "research" present means the task is in design.
var phaseMarkers = map[string]Phase{
	"task":           PhaseResearch,
	"research":       PhaseDesign,
	"design":         PhasePlanning,
	"plan":           PhaseImplementation,
	"implementation": PhaseImplementation,
}

// phaseFromFiles returns the furthest phase the names prove. A name is
// lower-cased and loses one trailing ".md" (the legacy create_task
// convention), then must match a marker exactly; anything else is
// ignored. Order-independent; no marker at all means PhaseResearch.
func phaseFromFiles(names []string) Phase {
	rank := 0
	for _, name := range names {
		if p, ok := phaseMarkers[strings.TrimSuffix(strings.ToLower(name), ".md")]; ok {
			rank = max(rank, p.Order())
		}
	}
	return Phases()[rank]
}
