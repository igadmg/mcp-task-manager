---
name: begin_task
description: Use when the user provides a new feature request, bug report, or ticket description for this project and wants it processed through research, design, planning, and implementation phases with saved artifacts and explicit approval gates between each phase.
---

# Begin Task Skill

Use this skill to formalize a new development task from a vague or informal ticket and drive it through all four delivery phases (research → design → planning → implementation), saving artifacts after each phase and waiting for explicit approval before proceeding.

## Step 1: Task Intake and Naming

1. Read the user's input carefully.
2. If the task is vague, ambiguous, missing acceptance criteria, or contains undefined terms — ask targeted clarifying questions before continuing. Gather only what is needed to produce an unambiguous problem statement. Do not over-ask.
3. Once the task is sufficiently clear, generate a short kebab-case task name (3–5 words, e.g. `add-tooltip-control`, `fix-input-scheme-leak`).
4. Ask the task manager MCP server to create a task for you with that kebab name as the id.
5. Store the task id in `.current_task` for future references.
6. Write a `task` file with the task manager MCP tool to the current task — a `.md` document with the following content:
   - **Original input** — verbatim text from the user or ticket
   - **Clarifications** — any Q&A gathered in this step (omit if none needed)
   - **Problem statement** — one unambiguous sentence describing what must change and why
   - **Acceptance criteria** — bullet list of verifiable done conditions

## Step 2: Research Phase

> Only start after explicit user approval.

1. Announce that the research phase is starting.
2. Read the `task` file from the current task.
3. Spawn a sub-agent (new context window) using the `$research` skill. Pass the full contents of `task` as context. Instruct the agent to produce a complete research report covering:
   - Relevant documents loaded
   - Current execution path with file references and line numbers
   - Modules, data structures, and generated accessors involved
   - Subsystem dependencies and side effects
   - Existing tests and quality constraints
   - Open questions or missing evidence
4. Save the sub-agent output verbatim to the `research` file of the current task.
5. Show the user a brief summary (key findings, open questions).
6. Ask: **"Research complete. Proceed to design? (yes / no — or describe what to change first)"**

## Step 3: Design Phase

> Only start after explicit user approval.

1. Read the `task` and `research` files from the current task.
2. Spawn a sub-agent (new context window) using the `$design` skill. Pass the full contents of both files as context. Instruct the agent to produce a complete architecture and design proposal.
3. Save the sub-agent output verbatim to the `design` file of the current task.
4. Show the user a brief summary (proposed approach, key decisions, risks).
5. Ask: **"Design complete. Proceed to planning? (yes / no — or describe what to revise)"**

## Step 4: Planning Phase

> Only start after explicit user approval.

1. Read the `task`, `research`, and `design` files from the current task.
2. Spawn a sub-agent (new context window) using the `$planning` skill. Pass the full contents of all three files as context. Instruct the agent to produce a phased implementation plan where each phase is logically complete, independently testable, and suitable for one commit.
3. Save the sub-agent output verbatim to the `plan` file of the current task.
4. Show the user a brief summary (number of phases, execution order, risk notes).
5. Ask: **"Planning complete. Proceed to implementation? (yes / no — or describe what to adjust)"**

## Step 5: Implementation Phase

> Only start after explicit user approval.

1. Read the `task`, `research`, `design`, and `plan` files from the current task.
2. Spawn a sub-agent (new context window) using the `$implementation` skill. Pass the full contents of all four files as context. Instruct the agent to execute one approved phase at a time, run quality gates, and report status.
3. Save the sub-agent implementation summary to the `implementation` file of the current task.
4. Report the final status: what changed, what was verified, what could not be verified.

## Rules

- Never skip an approval gate. Always wait for an explicit "yes" before the next phase.
- Never start a phase if the previous phase's file does not exist or is empty.
- If the user rejects a phase output, ask what needs to change, update the relevant file, and retry before moving on.
- Each sub-agent must receive the prior phase files as full context — not summaries.
- Keep phase outputs complete and self-contained so future sub-agents need no additional context beyond those files.
- The task id must be stable once created; do not rename it mid-task.
