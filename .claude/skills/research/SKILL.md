---
name: research
description: Use when the user asks to research or analyze this project's codebase and current behavior without proposing changes. Trigger for requests about module/component wiring, execution order, codegen output, subsystems, tests, or factual behavior in this repository.
---

# Research

Use this skill for evidence-only analysis of this project.

## Load Order

1. Read `CLAUDE.md`.
2. Read `README.md` and any module-level README.
3. Read only the task-relevant notes under `doc/` (or this project's equivalent notes location).
4. Inspect only the code and tests needed for the requested scope.

## Output Rules

- Report the codebase as it exists now.
- Do not propose fixes, refactors, or design changes.
- Separate facts from unknowns.
- If something is inferred, label it as an inference.
- Every non-trivial claim must cite a file reference with line numbers.
- If docs and code disagree, record both and state that code currently wins.
- When citing behavior that lives in generated code, cite both the annotation/tag in the handwritten source and the generated file it produces.

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
- Current execution path (construction -> initialization -> update/render order, or this project's equivalent)
- Modules, data structures, and generated accessors involved
- Subsystem dependencies and side effects
- Existing tests and quality constraints
- Open questions or missing evidence

## Repository Constraints

- Respect this project's actual repo layout and module boundaries.
- Library/vendored documentation is not a rule source for this project's own code unless the task explicitly targets those packages.
- Cover the real execution path when relevant: construction, data fields, generated accessors, configuration/selection steps, update/render logic, and tests.
- Keep the scope tight. Do not load unrelated modules or subsystems.
