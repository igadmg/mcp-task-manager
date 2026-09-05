---
name: workflow
description: Use when the user wants a full delivery workflow for this project that moves through research, design, planning, and implementation in order. Trigger for requests to orchestrate the whole process, choose the current phase, enforce phase handoffs, or keep work aligned with repository guidance and quality gates.
---

# Workflow

Use this skill as the top-level workflow for this project's tasks.

## Phase Order

1. `research`
2. `design`
3. `planning`
4. `implementation`

## Operating Rules

- Optimize for context correctness and low noise.
- Read broad repository guidance first: `CLAUDE.md`, `README.md`, and any module-level README.
- After that, load only the task-relevant notes under `doc/**` (or this project's equivalent notes location).
- Treat code as the final source of truth when docs and code disagree, and call out the mismatch explicitly.
- Respect this project's actual architecture and existing patterns — reuse what's already there rather than introducing new ones.
- Never hand-edit generated files; regenerate them from their source of truth instead, following this project's convention (documented exceptions aside).
- Use this project's quality gates: regeneration when generation inputs or signatures changed, formatting on every touched file, build, and targeted tests.

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
