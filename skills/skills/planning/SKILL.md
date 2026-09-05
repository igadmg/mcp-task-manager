---
name: planning
description: Use when the user wants an implementation plan for mengine based on an approved design. Trigger for requests to break work into phases, commits, milestones, verification steps, dependencies, rollback notes, or execution order in this repository.
---

# mengine Planning

Use this skill to convert approved design into atomic implementation phases.

## Preconditions

- Plan only from approved design or an explicitly agreed approach.
- Read `CLAUDE.md` and `src/core/README.md`.

## Planning Rules

- Do not redesign in this phase.
- Each phase must be logically complete, independently testable, and suitable for one commit.
- Keep phases small and minimize cross-file noise.
- Respect the actual repo layout: engine modules under `src/core/*` (`ui`, `gfx`, `input`, `net`, `sys`, `dbg`, `prof`, `rsc`), games under `src/projects/*`, tooling in `cmd/msh`, libraries in `pkg/**`, assets in `games/**`, and `tests/**`.
- Isolate phases that change `ecs`/`gog`/`lazy` tags: regeneration is a distinct, verifiable step.
- Never plan a commit that mixes a tag change with unrelated logic changes — the generated diff hides the real change.
- If a phase cannot be validated independently, split it again.

## For Each Phase Include

- Phase name
- Goal
- Exact scope
- Files or packages expected to change
- Whether regeneration is required (`msh generate --fast` or full `msh generate`)
- Dependencies and prerequisites
- Tests to add or run
- Quality gates to clear
- Exit criteria
- Rollback or risk notes

## Global Sections

- Execution order
- Parallelization opportunities
- Phase-to-commit mapping
- Documentation update points
- Final verification plan

## Output Format

Use exactly these sections, in this order:

### Planning Summary

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |

### Phase Details

For each phase use this template:

- `Phase ID:`
- `Goal:`
- `Why this phase exists:`
- `Files likely to change:`
- `Regeneration required:`
- `Dependencies:`
- `Implementation tasks:`
- `Tests and checks:`
- `Commit boundary:`
- `Definition of done:`
- `Rollback note:`

### Cross-Phase Risks

### Execution Order Rationale
