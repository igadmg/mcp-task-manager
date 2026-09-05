---
name: implementation
description: Use when the user wants code changes implemented in mengine following repository rules and quality gates. Trigger for requests to execute an approved phase, make minimal code changes, regenerate ECS/gog code, run verification, review findings, or report build and test results in this repository.
---

# mengine Implementation

Use this skill to execute one approved change phase in mengine.

## Team Structure

- Lead: manages work, keeps the phase aligned to the approved plan, coordinates sub-agents, and does not write production code directly when delegation is active.
- Coder: implements the approved phase exactly as designed, using existing repository patterns.
- Reviewer: performs line-level review for architecture compliance, correctness, regressions, and lifecycle issues. Reviewer does not fix code in the same pass.
- Tester: verifies behavior with the repository quality gates, task-specific tests, and targeted manual validation notes.

## Load Order

1. Read `CLAUDE.md`.
2. Read `README.md` and `src/core/README.md`.
3. Read only the task-relevant notes listed in `CLAUDE.md` and under `doc/`.
4. Load only the code needed for the current approved phase.

## Execution Rules

- Implement one approved phase at a time.
- Keep diffs minimal and reuse existing abstractions and patterns.
- Keep functions small, explicit, and deterministic; prefer direct code over abstraction layers.
- Preserve the ECS ownership model: cross-entity links are `ecs.Ref[T]`, never pointers to structs.
- Put construction parameters in component fields tagged `gog:"new"` and pass them through `New...(...)`.
- Keep state in component fields tagged `ecs:"a"` so accessors are generated instead of hand-written.
- Put control-specific input handlers in the control entity, not the parent; the parent forwards input scheme selection.
- Clean up owned child entities in the parent lifecycle (`DeferPersistent`).
- Never hand-edit `0.gen_*.go`; change the tags and regenerate. `0.gen_gone.go` is the only generated file holding hand-relevant utility definitions.
- Keep `go:generate` directives centralized in `*1.gen.go` files (project convention).
- Use ASCII by default and keep documentation updates in English.
- Do not commit, push, or tag unless the user explicitly asked for it.

## Mandatory Verification

- Run `msh generate --fast` when `ecs`/`gog`/`lazy` tags or entity/component signatures changed.
  If `src/projects/colonization/0.gen_gog.go` fails to generate, delete it and regenerate — this is a known `msh` bug.
- Run `gofmt -l` on every modified Go file (output must be empty).
- Run `go build ./src/...`
- Run targeted `go test ./src/core/<module>/...` or `go test ./src/projects/<game>/...` for every impacted package
- Run any task-specific tests identified by the plan

## Reviewer Checklist

- No architecture drift from the approved design
- Architecture matches the ECS + `ecs.Ref` ownership model
- New reusable/interactive control logic lives in its own entity
- Input responsibilities are placed in the correct entity
- Regeneration was run when tags or signatures changed, and no manual edits leaked into `0.gen_*.go`
- No stale generated code committed
- No accidental edits in unrelated files, `pkg/**`, or `third_party/**`
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
