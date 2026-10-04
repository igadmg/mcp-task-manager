package task

import (
	"fmt"
	"slices"
	"strings"
)

// Phase is a delivery-workflow phase. Each run of a phase is recorded in
// the task's server-owned <phase>.phase file (see PhaseRecord); a task
// without such files has its phase derived from the names of its attached
// workflow artifacts (phaseFromFiles).
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

// PhaseStrings lists every phase name in workflow order, for tool enums.
func PhaseStrings() []string {
	names := make([]string, 0, len(Phases()))
	for _, p := range Phases() {
		names = append(names, string(p))
	}
	return names
}

// ParsePhase returns the phase named s, or an error naming the valid ones.
func ParsePhase(s string) (Phase, error) {
	p := Phase(s)
	if err := p.check(); err != nil {
		return "", err
	}
	return p, nil
}

// check refuses anything that is not one of Phases, in ParsePhase's words.
func (p Phase) check() error {
	if slices.Contains(Phases(), p) {
		return nil
	}
	return fmt.Errorf("unknown phase %q (want %s)", string(p), strings.Join(PhaseStrings(), ", "))
}

// Prev returns the phase before p in workflow order; false for the first
// phase and for anything unknown.
func (p Phase) Prev() (Phase, bool) {
	i := slices.Index(Phases(), p)
	if i <= 0 {
		return "", false
	}
	return Phases()[i-1], true
}

// phaseFileSuffix ends the name of every server-owned phase record file.
const phaseFileSuffix = ".phase"

// PhaseFileName is the attached-file name of p's record: "<phase>.phase".
func PhaseFileName(p Phase) string {
	return string(p) + phaseFileSuffix
}

// IsReservedFileName reports whether name is reserved for a server-owned
// phase record, i.e. ends in ".phase". The name is normalized the way
// Windows resolves it first - trailing dots and spaces dropped, case
// ignored - so "Research.PHASE" or "research.phase." cannot slip past.
func IsReservedFileName(name string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimRight(name, ". ")), phaseFileSuffix)
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
