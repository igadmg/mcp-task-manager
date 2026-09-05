---
name: implementation
description: Use when the user wants code changes implemented in this project following repository rules and quality gates. Trigger for requests to execute an approved phase, make minimal code changes, regenerate generated code, run verification, review findings, or report build and test results in this repository.
---

# Implementation

Use this skill to execute one approved change phase in this project.

## Team Structure

- Lead: manages work, keeps the phase aligned to the approved plan, coordinates sub-agents, and does not write production code directly when delegation is active.
- Coder: implements the approved phase exactly as designed, using existing repository patterns.
- Reviewer: performs line-level review for architecture compliance, correctness, regressions, and lifecycle issues. Reviewer does not fix code in the same pass.
- Tester: verifies behavior with the repository quality gates, task-specific tests, and targeted manual validation notes.

## Load Order

1. Read `CLAUDE.md`.
2. Read `README.md` and any module-level README.
3. Read only the task-relevant notes listed in `CLAUDE.md` and under `doc/`.
4. Load only the code needed for the current approved phase.

## Execution Rules

- Implement one approved phase at a time.
- Keep diffs minimal and reuse existing abstractions and patterns.
- Keep functions small, explicit, and deterministic; prefer direct code over abstraction layers.
- Preserve this project's existing ownership model for cross-unit references — follow established conventions rather than introducing new patterns.
- Put construction parameters where this project's convention expects them, and pass them through the appropriate constructor.
- Keep state in fields that follow this project's accessor-generation convention, if one exists, so accessors are generated instead of hand-written.
- Put control-specific input handlers in the control entity itself, not the parent; the parent forwards input handling as needed.
- Clean up owned child entities in the parent's lifecycle, following this project's existing cleanup convention.
- Never hand-edit generated files; change the inputs that drive generation and regenerate.
- Keep code-generation directives centralized, following this project's convention.
- Use ASCII by default and keep documentation updates in English.
- Do not commit, push, or tag unless the user explicitly asked for it.

## Mandatory Verification

- Run this project's code generation step when generation inputs or entity/component signatures changed.
  If a specific generated file is known to fail generation, consult this project's known-issues notes for the documented workaround.
- Run this project's formatter on every modified file (output must be clean).
- Run this project's build command.
- Run targeted tests for every impacted package.
- Run any task-specific tests identified by the plan.

## Reviewer Checklist

- No architecture drift from the approved design
- Architecture matches this project's existing ownership model and conventions
- New reusable/interactive control logic lives in its own entity
- Input responsibilities are placed in the correct entity
- Regeneration was run when generation inputs or signatures changed, and no manual edits leaked into generated files
- No stale generated code committed
- No accidental edits in unrelated files or vendored/third-party directories
- No hidden side effects in constructors or initializers outside lifecycle requirements

## Required Output

- Execution status for the approved phase
- Reviewer findings, or explicit `no findings`
- Commands executed for verification
- Pass or fail status
- Failures with the smallest useful reproduction scope
- Unverified items or blockers, if any

## Output Format

Use exactly these sections, in this order:

### Phase Summary

### Lead Checklist

### Coder Report

### Reviewer Findings

### Tester Report

### Quality Gate Result

### Files Changed

### Deviations From Plan

### Next Phase Handoff
