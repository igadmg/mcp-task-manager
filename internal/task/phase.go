package task

import "strings"

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

// Order is the phase's position in the workflow, 0..3. Anything unknown,
// the empty Phase included, is 0: no information reads as the earliest
// phase, and Order is always a valid lane index.
func (p Phase) Order() int {
	switch p {
	case PhaseDesign:
		return 1
	case PhasePlanning:
		return 2
	case PhaseImplementation:
		return 3
	default:
		return 0
	}
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
	best := PhaseResearch
	for _, name := range names {
		key := strings.TrimSuffix(strings.ToLower(name), ".md")
		if p, ok := phaseMarkers[key]; ok && p.Order() > best.Order() {
			best = p
		}
	}
	return best
}
