---
name: research
description: Use when the user asks to research or analyze the mengine codebase and current behavior without proposing changes. Trigger for requests about entity/component wiring, systems, lifecycle order, codegen output, subsystems, tests, or factual engine behavior in this repository.
---

# mengine Research

Use this skill for evidence-only analysis of mengine.

## Load Order

1. Read `CLAUDE.md`.
2. Read `README.md` and `src/core/README.md`.
3. Read only the task-relevant notes: `01.dev.md`, `01.todo*.md`, and the matching files under `doc/`.
4. Inspect only the code and tests needed for the requested scope.

## Output Rules

- Report the engine as it exists now.
- Do not propose fixes, refactors, or design changes.
- Separate facts from unknowns.
- If something is inferred, label it as an inference.
- Every non-trivial claim must cite a file reference with line numbers.
- If docs and code disagree, record both and state that code currently wins.
- When citing behavior that lives in generated code, cite both the `ecs`/`gog`/`lazy` tag in the handwritten source and the generated `0.gen_*.go` file it produces.

## Output Format

Use exactly these sections, in this order:

### Task Slice

- 3-6 bullets describing only the area researched.

### Confirmed Facts

- Bullet list.
- Each non-trivial bullet must end with one or more file references including line numbers.

### Evidence Map

| Concern | Current implementation | Evidence |
| ------- | ---------------------- | -------- |
| Example | ... | `path:line` |

### Relevant Files

- Ordered list of the most important files to read next, each with one sentence on why it matters.

### Inference

- Only include if needed.
- Each item must explain why it is an inference and what evidence suggests it.

### Unknown

- Only include unanswered questions that block correctness.

## Minimum Coverage

- Task classification
- Relevant documents loaded
- Current execution path (entity construction -> Prepare -> Layout / system update order)
- Components, archetypes, and generated accessors involved
- Subsystem dependencies and side effects (`Gfx`, `Input`, `Dbg`, `Prof`, `Sys`)
- Existing tests and quality constraints
- Open questions or missing evidence

## Repository Constraints

- Respect the actual repo layout: engine modules under `src/core/*`, games under `src/projects/*`, tooling in `cmd/**` (notably `msh`), vendored/submodule libraries in `pkg/**`, assets in `games/**`, plus `tests/**` and `third_party/**`.
- Library markdown under `pkg/**` and `third_party/**` is not a rule source for `src/**` unless the task explicitly targets those packages.
- Cover the real execution path when relevant: entity constructor, component fields and tags, generated accessors, input scheme selection, layout, systems, and tests.
- Keep the scope tight. Do not load unrelated games or subsystems.
