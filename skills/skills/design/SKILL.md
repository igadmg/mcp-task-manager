---
name: design
description: Use when the user asks for a design, architecture proposal, or implementation approach for this project before coding. Trigger for requests about solution design, entity/component modeling, sequence of changes, diagrams, risk analysis, codegen impact, or testing strategy in this repository.
---

# Design

Use this skill to turn approved research into an implementable design for this project.

## Preconditions

- Start from the current codebase and, when available, approved research findings.
- Read `CLAUDE.md`.
- Read `README.md`, any module-level README, and the task-relevant notes under `doc/**` (or this project's equivalent notes location).

## Design Rules

- Preserve the existing architecture instead of inventing a new one.
- Reuse current patterns and abstractions already established in this project's codebase, including its existing conventions for cross-entity references, construction parameters, generated accessors, and subsystem integration points.
- Model reusable UI controls as their own self-contained units with their own data and input handlers; parents hold them by reference and forward input handling as needed.
- Keep the existing lifecycle intact: follow this project's established construction, initialization, and layout/activation sequence.
- Never design manual edits into generated files; if generated behavior must change, add an extension point in handwritten code or change whatever drives generation.
- Keep the proposed diff minimal and aligned with current naming and dependency direction.
- Do not write code.

## Required Output

- Problem summary and scope boundaries
- Assumptions and non-goals
- Touched packages and expected files
- Primary data flow and execution path
- ADR-style decision with alternatives and consequences
- Risk analysis for correctness, lifecycle/ordering, performance, codegen, and compatibility concerns
- Testing strategy: targeted package tests, regression checks, and manual validation notes
- Codegen, subsystem, asset, and documentation impacts

## Output Format

Use exactly these sections, in this order:

### Design Summary

### Current-State Evidence

### Proposed Architecture

### Context Diagram

- Use Mermaid for non-trivial changes that alter subsystem, module, or data flow boundaries.
- For trivial/localized changes, write `Not required for this trivial change` and explain why.

### Structure Diagram

- Use Mermaid for the changed slice when the change is non-trivial.
- Name actual modules, components, entities, and units already in the repo when known.
- Show ownership/reference direction explicitly.

### Data Flow Diagram

- Use Mermaid for non-trivial data-flow changes.
- For trivial/localized changes, write `Not required for this trivial change` and explain why.

### Sequence Diagram

- Use Mermaid for the main non-trivial lifecycle, input, or update flow.
- For trivial/localized changes, write `Not required for this trivial change` and explain why.

### ADR

Use this exact structure:

- `Title`
- `Status`
- `Date`
- `Context`
- `Decision`
- `Consequences`
- `Alternatives Rejected`

### Risk Analysis

### Testing Strategy

### Codegen Impact

- List every change to code-generation inputs (tags, annotations, config) and the generated files it affects.
- State whether a fast/partial regeneration is sufficient or a full regeneration is required, if this project uses code generation.

### File Impact Map

### Open Questions

## Special Attention

- Call out ownership and cleanup when a parent unit creates child units.
- Call out input handling conflicts when adding or changing input-scheme/handler usage.
- Call out per-frame or per-request allocation and layout cost for anything on a hot path.
- Name the expected test locations within this project's structure.
