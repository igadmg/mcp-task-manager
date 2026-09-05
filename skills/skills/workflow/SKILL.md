---
name: workflow
description: Use when the user wants a full mengine delivery workflow that moves through research, design, planning, and implementation in order. Trigger for requests to orchestrate the whole process, choose the current phase, enforce phase handoffs, or keep work aligned with repository guidance and quality gates.
---

# Workflow

Use this skill as the top-level workflow for mengine tasks.

## Phase Order

1. `research`
2. `design`
3. `planning`
4. `implementation`

## Operating Rules

- Optimize for context correctness and low noise.
- Read broad repository guidance first: `CLAUDE.md`, `README.md`, and `src/core/README.md`.
- After that, load only the task-relevant notes: `01.dev.md`, `01.todo.md`, `01.todo.ecs.md`, `01.todo.gone.md`, and `doc/**`.
- Treat code as the final source of truth when docs and code disagree, and call out the mismatch explicitly.
- Respect the actual architecture in this repo: ECS entities (`ecs:"archetype"`) and components (`ecs:"component"`), `ecs.Ref[T]` cross-entity links, `World` as the subsystem integration point (`Gfx/Input/Dbg/Prof/Sys`), UI screens/modals with `PrepareLayout()` + `Layout(*ui.Context)`, and `input.InputSchemeComponent` driven input.
- Generated files `src/**/0.gen_*.go` are produced by `msh generate`; never hand-edit them (the sole exception is `0.gen_gone.go`, which holds utility type definitions).
- Use the repository quality gates: `msh generate --fast` when tags or signatures changed, `gofmt` on every touched file, `go build ./src/...`, and targeted `go test ./src/core/<module>/...`.

## Phase Handoff

- `research` produces evidence only.
- `design` turns evidence into an implementable architecture.
- `planning` turns approved design into commit-sized phases.
- `implementation` executes one approved phase at a time behind hard quality gates.

## How To Apply

- If the user asks for full end-to-end delivery, start with `research` unless they explicitly provide approved design or plan inputs.
- If the user asks for a later phase directly, verify that the required input from the previous phase exists; if not, say what is missing.
- Keep each response limited to the active phase unless the user explicitly asks for combined output.
- Use the repository-local skills `$research`, `$design`, `$planning`, and `$implementation` when phase-specific behavior is needed.

## Agent Routing Policy

Keep this repository skill limited to project workflow, phase handoffs, and quality gates.
Personal agent routing, local CLI command names, provider/model choices, and notification mechanics belong to the caller's global agent policy, not to git-tracked repository skills.

When delegation is used, preserve these project-level invariants regardless of which agent performs the work:

1. Do not start a later phase until the required artifact from the previous phase exists and is approved or explicitly accepted for the current task.
2. Do not hand off unreviewed or evidence-free artifacts to the next phase.
3. Run the repository quality gates listed above before reporting implementation complete.
4. Do not commit, push, tag, or publish unless the user explicitly requested that action.
5. Report stage transitions, blockers, quality-gate results, and final status without secrets or raw command dumps.
