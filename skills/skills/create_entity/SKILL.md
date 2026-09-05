---
name: create_entity
description: Use when the user asks to create a new ECS entity, archetype, component, or reusable UI control in mengine, with interactive approval before generating files and running codegen.
---

# Create Entity Skill

Use this skill to add a new ECS entity/component or reusable UI control interactively.

## Objective

Create a well-formed archetype + component pair (or a self-contained UI control entity) that follows the repository's ECS conventions, then regenerate accessors and verify the build.

## Expected Structure

- Archetype struct tagged `ecs:"archetype"`, one file per entity, named after the entity (`screen.*.go`, `modal.*.go`, `control.*.go`, or `<name>.go`).
- Component struct tagged `ecs:"component"` holding the entity's data.
- Construction parameters as component fields tagged `gog:"new"`, passed through the generated `New...(...)`.
- State fields tagged `ecs:"a"` (or `ecs:"a: name"`) so getters/setters are generated instead of hand-written.
- Cross-entity links as `ecs.Ref[T]` — never pointers to structs.
- For UI entities: `PrepareLayout()` + `Layout(*ui.Context)`, with input handled via `input.InputSchemeComponent` and the generated `Select/Enable/DisableInputScheme` flow.

## Interactive Flow

1. Ask for the new entity name in Go-idiomatic form, and whether it is a screen, modal, reusable control, or plain gameplay entity.
2. Ask which module it belongs to (`src/core/<module>` for engine-level, `src/projects/<game>` for game-level).
3. Ask which data it owns and which of those fields are construction parameters (`gog:"new"`) versus mutable state (`ecs:"a"`).
4. Ask which existing entity should own it, if any, and confirm the `ecs.Ref[T]` direction.
5. Present the plan before writing anything:
   - full list of files to create or modify;
   - the archetype and component definitions with their tags;
   - which generated files will change;
   - the parent wiring and cleanup point.
6. Wait for explicit approval before creating files.

## Execution After Approval

- Create the handwritten source files in the target module.
- Wire the parent: store the child as `ecs.Ref[T]`, forward input scheme selection under the same scheme name, and clean the child up in the parent lifecycle (`DeferPersistent`) if the parent owns it.
- Keep control-specific input handlers inside the control entity, not in the parent.
- Do not write anything into `0.gen_*.go` by hand.
- Keep any new `go:generate` directive in the module's `*1.gen.go` file.

## Verification

- Run `msh generate --fast`. If `src/projects/colonization/0.gen_gog.go` fails, delete it and regenerate (known `msh` bug).
- Run `gofmt -l` on every new or modified file (output must be empty).
- Run `go build ./src/...`.
- Run `go test` for the touched module.
- Report the build/test result, the generated accessors now available, and any remaining issues.
