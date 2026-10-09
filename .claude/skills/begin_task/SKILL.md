---
name: begin_task
description: Use when the user provides a new feature request, bug report, or ticket description for this project and wants it processed through research, design, planning, and implementation phases with saved artifacts and explicit approval gates between each phase.
---

# Begin Task Skill

Use this skill to formalize a new development task from a vague or informal ticket and drive it through all four delivery phases (research → design → planning → implementation), saving artifacts after each phase and waiting for explicit approval before proceeding.

Every phase is recorded by the task manager server: `start_phase` opens a run of the phase (who started it, when) and `finish_phase` closes it with the tokens it cost. The server keeps these records in `<phase>.phase` files next to the artifacts, and the board shows each task in the lane of its current phase.

## Step 1: Task Intake and Naming

1. Read the user's input carefully.
2.  Ask targeted clarifying questions before continuing. Gather only what is needed to produce an unambiguous problem statement. Do not over-ask.
3. Once the task is sufficiently clear, generate a short kebab-case task name (3–5 words, e.g. `add-tooltip-control`, `fix-input-scheme-leak`).
4. Ask the task manager MCP server to create a task for you with that kebab name as the id (`create_task`). The server records you as its creator.
5. **Do not start it.** The task stays in `todo` until research begins; remember its id until then.
6. Write a `task` file with `write_task_file(task_id=<id>)` — a `.md` document with the following content:
   - **Original input** — verbatim text from the user or ticket
   - **Clarifications** — any Q&A gathered in this step (omit if none needed)
   - **Problem statement** — one unambiguous sentence describing what must change and why
   - **Acceptance criteria** — bullet list of verifiable done conditions

## Running a Phase

Every phase below follows the same frame:

1. Call `start_phase(phase=<phase>)`. For research pass `id=<id>` as well: that moves the task to `in_progress` and makes it your current task, so later phases find it without an id (`get_current_task`).
2. Spawn the phase sub-agent and save its output to the phase's artifact file.
3. Call `finish_phase(phase=<phase>, tokens=<n>)`, where `<n>` is the sub-agent's reported total token usage (the `total_tokens` of the Agent tool's usage report). Omit `tokens` when the host does not report it.
4. Summarize and ask for approval.

## Step 2: Research Phase

> Only start after explicit user approval.

1. Announce that the research phase is starting and call `start_phase(id=<id>, phase=research)`.
2. Read the `task` file from the current task.
3. Spawn a sub-agent (new context window) using the `$research` skill. Pass the full contents of `task` as context. Instruct the agent to produce a complete research report covering:
   - Relevant documents loaded
   - Current execution path with file references and line numbers
   - Modules, data structures, and generated accessors involved
   - Subsystem dependencies and side effects
   - Existing tests and quality constraints
   - Open questions or missing evidence
4. Save the sub-agent output verbatim to the `research` file of the current task.
5. Call `finish_phase(phase=research, tokens=<sub-agent total>)`.
6. Show the user a brief summary (key findings, open questions).
7. Ask: **"Research complete. Proceed to design? (yes / no — or describe what to change first)"**

## Step 3: Design Phase

> Only start after explicit user approval.

1. Call `start_phase(phase=design)`.
2. Read the `task` and `research` files from the current task.
3. Spawn a sub-agent (new context window) using the `$design` skill. Pass the full contents of both files as context. Instruct the agent to produce a complete architecture and design proposal.
4. Save the sub-agent output verbatim to the `design` file of the current task.
5. Call `finish_phase(phase=design, tokens=<sub-agent total>)`.
6. Show the user a brief summary (proposed approach, key decisions, risks).
7. Ask: **"Design complete. Proceed to planning? (yes / no — or describe what to revise)"**

## Step 4: Planning Phase

> Only start after explicit user approval.

1. Call `start_phase(phase=planning)`.
2. Read the `task`, `research`, and `design` files from the current task.
3. Spawn a sub-agent (new context window) using the `$planning` skill. Pass the full contents of all three files as context. Instruct the agent to produce a phased implementation plan where each phase is logically complete, independently testable, and suitable for one commit.
4. Save the sub-agent output verbatim to the `plan` file of the current task.
5. Call `finish_phase(phase=planning, tokens=<sub-agent total>)`.
6. Show the user a brief summary (number of phases, execution order, risk notes).
7. Ask: **"Planning complete. Proceed to implementation? (yes / no — or describe what to adjust)"**

## Step 5: Implementation Phase

> Only start after explicit user approval.

1. Call `start_phase(phase=implementation)`. When the project enables git branching, this creates the task's wip branch and checks it out (a subtask's off its parent's); the implementation commits go there. Do not create, switch or commit branches around it.
2. Read the `task`, `research`, `design`, and `plan` files from the current task.
3. Spawn a sub-agent (new context window) using the `$implementation` skill. Pass the full contents of all four files as context. Instruct the agent to execute one approved phase at a time, run quality gates, and report status.
4. Save the sub-agent implementation summary to the `implementation` file of the current task.
5. Call `finish_phase(phase=implementation, tokens=<sub-agent total>)`.
6. Report the final status: what changed, what was verified, what could not be verified.
7. Ask: **"Implementation complete. Complete the task? (yes / no)"** On yes, call `complete_task` (optionally with a `commit_message`). Under git branching this squashes the wip branch onto the task's final branch.

## Rules

- Never skip an approval gate. Always wait for an explicit "yes" before the next phase.
- Never start a phase if the previous phase's file does not exist or is empty.
- If the user rejects a phase output, ask what needs to change, call `start_phase` for the same phase again (a new run; earlier runs are kept), update the relevant file, and `finish_phase` it with that run's tokens before moving on. Every revision is a run.
- Never write `*.phase` files yourself: the server owns them and refuses such writes.
- If a phase run was left open (for example after a crash), `finish_phase` it without tokens before starting again; only one phase run of a task may be open at a time.
- Each sub-agent must receive the prior phase files as full context — not summaries.
- Keep phase outputs complete and self-contained so future sub-agents need no additional context beyond those files.
- The task id must be stable once created; do not rename it mid-task.
