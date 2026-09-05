---
name: design
description: Use when the user asks for a design, architecture proposal, or implementation approach for mengine before coding. Trigger for requests about solution design, entity/component modeling, sequence of changes, diagrams, risk analysis, codegen impact, or testing strategy in this repository.
---

# mengine Design

Use this skill to turn approved research into an implementable design for mengine.

## Preconditions

- Start from the current codebase and, when available, approved research findings.
- Read `CLAUDE.md`.
- Read `README.md`, `src/core/README.md`, and the task-relevant notes (`01.dev.md`, `01.todo*.md`, `doc/**`).

## Design Rules

- Preserve the existing architecture instead of inventing a new one.
- Reuse current patterns: ECS archetypes and components, `ecs.Ref[T]` for cross-entity links, `gog:"new"` construction parameters, `ecs:"a"` generated accessors, `lazy` functions, and the `World` subsystem integration point.
- Model reusable UI controls as their own entities with their own components and input handlers; parents hold them as `ecs.Ref[ControlEntity]` and forward input scheme selection.
- Keep the lifecycle intact: entity `New*` -> Prepare -> Layout; screen activation Prepare -> SelectInputScheme -> Layout.
- Never design manual edits into `0.gen_*.go`; if generated behavior must change, add an extension point in handwritten code or change the tags that drive generation.
- Keep the proposed diff minimal and aligned with current naming and dependency direction.
- Do not write code.

## Required Output

- Problem summary and scope boundaries
- Assumptions and non-goals
- Touched packages and expected files
- Primary data flow and execution path
- ADR-style decision with alternatives and consequences
- Risk analysis for correctness, lifecycle/ordering, performance, codegen, and compatibility concerns
- Testing strategy: targeted package tests, regression checks, and manual in-game validation notes
- Codegen, subsystem, asset, and documentation impacts

## Output Format

Use exactly these sections, in this order:

### Design Summary

### Current-State Evidence

### Proposed Architecture

### Context Diagram

- Use Mermaid for non-trivial changes that alter subsystem, game, or data flow boundaries.
- For trivial/localized changes, write `Not required for this trivial change` and explain why.

### Entity/Component Diagram

- Use Mermaid for the changed slice when the change is non-trivial.
- Name actual archetypes, components, entities, and systems already in the repo when known.
- Show `ecs.Ref[T]` ownership direction explicitly.

### Data Flow Diagram

- Use Mermaid for non-trivial data-flow changes.
- For trivial/localized changes, write `Not required for this trivial change` and explain why.

### Sequence Diagram

- Use Mermaid for the main non-trivial lifecycle, input, or frame-update flow.
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

- List every tag change (`ecs`, `gog`, `lazy`) and the generated files it affects.
- State whether `msh generate --fast` is sufficient or a full `msh generate` is required.

### File Impact Map

### Open Questions

## Special Attention

- Call out ownership and cleanup when a parent entity creates child entities (`DeferPersistent`).
- Call out input scheme conflicts when adding or changing `input.InputSchemeComponent` usage.
- Call out per-frame allocation or layout cost for anything on the hot path.
- Name the expected test locations under `src/core/<module>/`, `src/projects/<game>/`, or `tests/`.
