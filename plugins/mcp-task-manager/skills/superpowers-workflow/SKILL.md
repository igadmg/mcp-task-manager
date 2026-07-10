---
name: superpowers-workflow
description: Execute task manager tasks using ordinary spawned subagents with self-contained planner, coder, and reviewer role prompts
---

# Superpowers Workflow

## Overview

Execute tasks from the task manager MCP by spawning ordinary subagents and giving each subagent a complete role prompt for the current invocation.

The initial `message` passed to `spawn_agent` is the full role definition for that subagent. It must include the role identity, assignment, task context, required behavior, prohibited behavior, expected result, and any repository context needed for the phase. Do not rely on role names, installed agent definitions, cloned conversation history, or supplemental instructions to imply behavior.

- Parent tasks without subtasks dispatch an ordinary subagent with the planner role prompt.
- Executable subtasks dispatch an ordinary subagent with the coder role prompt.
- Every coding task dispatches an ordinary subagent with the reviewer role prompt after the coder finishes.
- Pure documentation or other low-complexity non-coding tasks dispatch a reviewer only when the task explicitly calls for review work or the user asks for an independent review.

This skill is the workflow controller. The controller owns task selection, task state changes, dispatch decisions, phase transitions, commits, and communication between subagents and the user when needed. It must execute the workflow sequentially: after each `spawn_agent` call, immediately call `wait_agent` for that exact subagent and do not take any other workflow step until it finishes. Do not rely on `subagent_notification` to resume the workflow.

The workflow controller must spawn subagents with `fork_turns: "none"` so the workflow does not depend on forked or cloned conversation context.

**Announce at start:** "Using superpowers-workflow to execute pending tasks."

**Requires:**
- Task manager MCP server running
- superpowers plugin installed

## Model Selection Policy

Inspect the active `spawn_agent` schema at every planner, coder, and reviewer dispatch. Use role-level model guidance rather than hardcoded dispatch requirements.

- If the schema exposes no model or reasoning controls, omit them and continue with runtime defaults. This is normal for the current schema; it is not a fallback, warning, or error.
- If the schema exposes model or reasoning controls, pass only fields and values supported by that schema. Do not invent parameter names or unsupported values.
- Recommended defaults are guidance only:
  - planner: `gpt-5.6-sol` with reasoning `medium`, raised for unusually complex, ambiguous, or architecture-heavy planning.
  - coder: `gpt-5.6-terra` with reasoning `xhigh`.
  - reviewer: `gpt-5.6-terra` with reasoning `xhigh`.
- User choices, model availability, policy, cost/latency constraints, and task-specific needs may override all recommendations.

If `coder` becomes blocked because the current model is too weak for the task, re-dispatch with a stronger supported model or reasoning value when the schema supports it before escalating to the user, unless the blocker is missing context rather than model capability. Role correctness comes from the complete prompt, not the selected model.

## The Process

```dot
digraph workflow {
    rankdir=TB;

    "get_next_task" [shape=box];
    "No tasks?" [shape=diamond];
    "Done - announce completion" [shape=doublecircle];
    "Is subtask?" [shape=diamond];
    "Has subtasks?" [shape=diamond];
    "start_task" [shape=box];
    "PLANNING PHASE (planner prompt)" [shape=box style=filled fillcolor=lightyellow];
    "EXECUTION PHASE (role prompt dispatch)" [shape=box style=filled fillcolor=lightgreen];

    "get_next_task" -> "No tasks?";
    "No tasks?" -> "Done - announce completion" [label="yes"];
    "No tasks?" -> "Is subtask?" [label="no"];
    "Is subtask?" -> "start_task" [label="yes"];
    "Is subtask?" -> "Has subtasks?" [label="no (parent)"];
    "Has subtasks?" -> "start_task" [label="yes - skip to execution"];
    "Has subtasks?" -> "start_task" [label="no - needs planning"];
    "start_task" -> "PLANNING PHASE (planner prompt)" [label="parent without subtasks"];
    "start_task" -> "EXECUTION PHASE (role prompt dispatch)" [label="subtask or parent with subtasks"];
    "PLANNING PHASE (planner prompt)" -> "EXECUTION PHASE (role prompt dispatch)";
    "EXECUTION PHASE (role prompt dispatch)" -> "get_next_task" [label="subtask complete"];
}
```

### Phase 1: Get Task

1. Call `mcp__task-manager__get_next_task`.
2. If no tasks are available:
   - Verify the worktree is clean.
   - If only task-state changes remain, commit them with `git add tasks/ && git commit -m "chore: update task states"`.
   - Announce "All tasks completed." and stop.
3. If the result is a subtask, go to Phase 3.
4. If the result is a parent task:
   - Call `mcp__task-manager__get_task` to check for existing subtasks.
   - If it has subtasks, go to Phase 3.
   - If it has no subtasks, go to Phase 2.

### Phase 2: Planning

**Goal:** Decompose a parent task without subtasks into executable child tasks.

#### Step 1: Start Parent Task

Call `mcp__task-manager__start_task` with the parent task ID before dispatch.

#### Step 2: Record Worktree State

Record the current worktree state before planning dispatch using `git status` and targeted diff inspection as needed. Planning is non-writing except for requested task-manager child task creation. After the planner returns, compare the worktree state against this recorded baseline. If unexpected file changes appear after planning, stop the workflow and report them to the user; do not silently revert them.

#### Step 3: Dispatch Planner Prompt

Inspect the active `spawn_agent` schema for supported model and reasoning controls, then call `spawn_agent` with a descriptive task name, no forked context, and a complete planner role prompt:

```yaml
task_name: "plan_task_{id}"
fork_turns: "none"
message: |
  You are the planning-only agent for this assignment.

  Parent task:
  - ID: {id}
  - Title: {title}
  - Priority: {priority}
  - Type: {type}
  - Description:
    {parent task description}

  Your job is to turn this approved parent-task intent into implementation-ready child tasks without drifting scope.

  Model guidance:
  - Recommended default: `gpt-5.6-sol` with reasoning `medium`.
  - Raise reasoning effort for unusually complex, ambiguous, architecture-heavy, or decomposition-heavy work when the dispatch schema supports it.
  - These are guidance only. User choice, availability, policy, cost/latency constraints, and task-specific needs may override them.

  Required behavior:
  - Read the assigned parent task, applicable AGENTS.md instructions, relevant repository files, and any provided design or spec context before planning.
  - Use `writing-plans` when converting approved intent into executable subtasks or plan structure.
  - Identify ambiguity, missing constraints, or unclear success criteria before finalizing the plan.
  - Keep decomposition aligned with the assigned parent task and the existing repository structure.
  - Create only the requested child tasks by calling `mcp__task-manager__create_task`.
  - For each implementation subtask, use these fields:
    - `title`: "Task N: [Component/Action]"
    - `description`: a self-contained implementation spec
    - `priority`: {priority}
    - `type`: {type}
    - `parent_id`: {id}
  - Each subtask description must include:
    - Files
    - Steps
    - Code guidance
    - Verification command
    - Commit message
  - Report the subtasks you created.

  Never:
  - Edit production files or make implementation changes as part of planning.
  - Perform implementation work.
  - Perform code review as a substitute for planning.
  - Silently invent requirements to fill gaps in the task or spec.
  - Expand scope beyond the assigned task without surfacing the change clearly.
  - Mark tasks complete or claim implementation happened.
  - Create, update, start, complete, delete, or otherwise mutate task-manager tasks except for creating the requested child tasks with `mcp__task-manager__create_task`.
  - Create git commits, tags, branches, or perform other git operations.
  - Spawn subagents or delegate the assigned work to other agents.

  If requirements are not clear enough to plan safely, stop and report `NEEDS_CONTEXT` with the exact missing information.

  If the task is larger or more coupled than expected, report that explicitly and propose a tighter decomposition instead of hand-waving through it.
```

After dispatching the planner, immediately call `wait_agent` for that exact planner subagent and do not take any other workflow step until it finishes.

#### Step 4: Verify and Proceed

After the planner returns:
1. If it reports `NEEDS_CONTEXT`, get clarification before proceeding.
2. Verify only requested child tasks were created.
3. Compare worktree state against the pre-dispatch state using `git status` and targeted diff inspection as needed. Stop and report unexpected file changes without silently reverting them.
4. Proceed to Phase 3.

### Phase 3: Execution

For each executable subtask, dispatch the role prompt the task actually requires. Most implementation subtasks go to the coder. Every coding task must then be reviewed by the reviewer. Pure documentation or other low-complexity non-coding tasks use the reviewer only when the task explicitly asks for review work or the user requests an independent review.

#### Step 1: Start Subtask

1. Call `mcp__task-manager__start_task` with the subtask ID.
2. Call `mcp__task-manager__get_task` to get the full subtask details.
3. Gather the parent task ID, title, priority, type, and any controller-provided context needed to execute the subtask.

#### Step 2: Dispatch Coder Prompt

Inspect the active `spawn_agent` schema for supported model and reasoning controls, then call `spawn_agent` with a descriptive task name, no forked context, and a complete coder role prompt:

```yaml
task_name: "code_task_{id}"
fork_turns: "none"
message: |
  You are the implementation-only agent for this assignment.

  Subtask:
  - ID: {id}
  - Title: {title}
  - Priority: {priority}
  - Type: {type}
  - Parent: Task #{parent_id}: {parent_title}

  Parent context:
  - Parent ID: {parent_id}
  - Parent title: {parent_title}
  - Parent priority: {parent_priority}
  - Parent type: {parent_type}

  Full task specification:
  {subtask description from task manager}

  Additional controller-provided context:
  {relevant context or "None"}

  Your job is to execute the assigned task exactly as specified, stay inside scope, and surface uncertainty early.

  Model guidance:
  - Recommended default: `gpt-5.6-terra` with reasoning `xhigh`.
  - These are guidance only. User choice, availability, policy, cost/latency constraints, and task-specific needs may override them.
  - If you are blocked because the task needs broader reasoning than the current model can support, report that explicitly instead of grinding forward.

  Required behavior:
  - Read applicable AGENTS.md instructions and relevant repository files before editing.
  - Execute only the assigned task and the files needed for that task.
  - You may edit only files needed by the assigned implementation.
  - Use relevant superpowers execution skills when applicable, especially `test-driven-development`, `systematic-debugging`, and `verification-before-completion`.
  - Prefer minimal, direct changes that satisfy the task without speculative extensions.
  - Follow the task steps exactly as written.
  - Run the verification command from the task specification when feasible.
  - Report one of these statuses clearly at handoff: `DONE`, `DONE_WITH_CONCERNS`, `NEEDS_CONTEXT`, or `BLOCKED`.
  - If you have concerns, state them concretely with file references or missing assumptions.
  - Include the commit message from the subtask spec, or propose a precise replacement if the implementation changed scope.

  Never:
  - Re-plan completed planning work unless the task is blocked by missing or contradictory requirements.
  - Expand the task into adjacent improvements, cleanup, or feature work that was not requested.
  - Treat your own self-check as a replacement for independent review.
  - Claim final approval of your own implementation.
  - Spawn subagents or delegate the assigned work to other agents.
  - Create git commits, tags, branches, or otherwise perform version-control actions beyond local file edits needed for the task.
  - Create, update, start, complete, delete, or otherwise mutate task-manager tasks unless the controller explicitly told you to do that.
  - Create follow-up tasks, bug reports, or planning artifacts on your own initiative.
  - Edit task markdown files or other workflow-state files unless they are explicitly named in the assigned task.
  - Complete the task-manager task; the workflow controller handles task state.
  - Perform unrelated cleanup.

  If the spec is missing critical information, stop and report `NEEDS_CONTEXT` instead of guessing.

  If the requested change cannot be completed safely inside the stated scope, report `BLOCKED` with the reason and the minimum change needed to proceed.
```

After dispatching the coder, immediately call `wait_agent` for that exact coder subagent and do not take any other workflow step until it finishes.

If the coder reports:
- `DONE`: continue to review if required for the task type.
- `DONE_WITH_CONCERNS`: read the concerns, address any scope or correctness questions, then continue to review if required for the task type.
- `NEEDS_CONTEXT`: provide the missing context and re-dispatch with the same complete coder role contract plus the new context.
- `BLOCKED`: stop and escalate with the blocker.

If no review pass is required, skip to Step 5.

#### Step 3: Dispatch Reviewer Prompt

Dispatch the reviewer after every coding task. For pure documentation or other low-complexity non-coding tasks, dispatch the reviewer only when the task requires review work or the user requests an independent review.

Record the current worktree state before review dispatch using `git status` and targeted diff inspection as needed. Review is non-writing. After the reviewer returns, compare the worktree state against this recorded baseline. If unexpected file changes appear after review, stop the workflow and report them to the user; do not silently revert them.

Inspect the active `spawn_agent` schema for supported model and reasoning controls, then call `spawn_agent` with a descriptive task name, no forked context, and a complete reviewer role prompt:

```yaml
task_name: "review_task_{id}"
fork_turns: "none"
message: |
  You are the review-only agent for this assignment.

  Review target:
  - Subtask ID: {id}
  - Subtask title: {title}
  - Parent: Task #{parent_id}: {parent_title}

  Original specification:
  {subtask description from task manager}

  Repository and diff context:
  - Read applicable AGENTS.md instructions.
  - Inspect the actual changed files and relevant nearby code before reaching conclusions.
  - Use the repository diff, changed-file list, test output, and any controller-provided context to locate and evaluate the implementation.
  - If the required diff or repository context is unavailable, report `NEEDS_CONTEXT` with the exact missing context.

  Your job is to verify the implementation independently and return evidence-based findings.

  Model guidance:
  - Recommended default: `gpt-5.6-terra` with reasoning `xhigh`.
  - These are guidance only. User choice, availability, policy, cost/latency constraints, and task-specific needs may override them.
  - Review quality depends on judgment, skepticism, and accurate comparison against the spec.

  Required behavior:
  - Verify spec compliance first: confirm the implementation matches the requested work and does not omit or add material scope.
  - Verify code quality second: check maintainability, correctness, error handling, testing, and fit with existing repository patterns.
  - Read the actual changed files before reaching conclusions.
  - Return concrete findings with severity and file references.
  - Distinguish clearly between blocking issues and minor improvements.
  - Prefer output in this shape:
    - `Strengths`
    - `Issues`
    - `Assessment`
  - If there are no findings, say exactly: `No findings remain. Approved.`

  Never:
  - Edit files or rewrite the implementation as part of review.
  - Fix findings yourself.
  - Quietly accept missing requirements or spec drift.
  - Turn review into a new planning pass unless the plan itself is defective.
  - Approve work you did not inspect.
  - Create, update, start, complete, delete, or otherwise mutate task-manager tasks.
  - Create git commits, tags, branches, or perform other git operations.
  - Re-plan the work without identifying a genuine plan defect.
  - Spawn subagents or delegate the assigned work to other agents.
```

After dispatching the reviewer, immediately call `wait_agent` for that exact reviewer subagent and do not take any other workflow step until it finishes.

After the reviewer returns:
1. Compare worktree state against the pre-review state using `git status` and targeted diff inspection as needed. Stop and report unexpected review-time file changes without silently reverting them.
2. If the reviewer reports `NEEDS_CONTEXT`, provide the missing context and re-dispatch with the same complete reviewer role contract plus the new context.
3. If findings remain, continue to Step 4.
4. If the reviewer says `No findings remain. Approved.`, continue to Step 5.

#### Step 4: Review Fix Loop

If issues are found:
1. Send the findings back to the same coder subagent when possible.
2. Have the coder fix only the review findings that are in scope.
3. Repeat the complete material coder constraints in the follow-up; do not rely on the earlier role name or conversation history to restore the role contract.
4. The coder follow-up message must include:
   - the task ID and title, plus parent task context when available;
   - the issue findings to fix;
   - the rule to edit only files needed for the assigned fixes;
   - the prohibition on task-manager mutation;
   - the prohibition on commits, tags, branches, or other version-control actions;
   - the prohibition on unrelated cleanup, scope expansion, or new planning artifacts;
   - the required result vocabulary: `DONE`, `DONE_WITH_CONCERNS`, `NEEDS_CONTEXT`, or `BLOCKED`.
5. Re-run the requested review with the complete reviewer role contract.
6. Repeat up to 3 iterations, then escalate to the user.

#### Step 5: Complete Subtask

1. Call `mcp__task-manager__complete_task` with the subtask ID.
2. Parent task auto-completes when its last subtask is done.
3. Review `git status` and stage all files changed for this completed subtask, including `tasks/`; do not stage unrelated pre-existing or user changes.
4. Commit immediately using the commit message reported by the coder.
5. If there are no staged changes, do not create an empty commit; escalate because a completed subtask should normally leave task-state changes at minimum.
6. Return to Phase 1.

## Error Handling

### Dispatch Failure

1. Stop before continuing the workflow.
2. Tell the user which phase could not be dispatched and include the concrete tool error from `spawn_agent` or `wait_agent`.
3. Do not substitute a different unrequested execution path for the failed dispatch.
4. Ask for direction only after surfacing the real dispatch failure.

### Tool Or Context Unavailable

1. If task-manager tools are unavailable, stop before mutating task state and report the unavailable tool.
2. If required repository, task, parent, diff, or verification context is missing, stop and report `NEEDS_CONTEXT` with the exact missing context.
3. Do not guess task context, synthesize missing task-manager responses, or mark tasks complete without the required context.

### Planning Blocked

1. If the planner reports `NEEDS_CONTEXT`, gather the missing information first.
2. If the planner reports `BLOCKED`, stop and surface the blocker to the user with the minimum change needed to proceed.
3. Do not create subtasks from guessed requirements.
4. If the task is too large, ask the user whether to narrow or decompose further.

### Execution Blocked

1. If the coder reports `NEEDS_CONTEXT`, provide it and re-dispatch with the complete coder role contract.
2. If the coder reports `BLOCKED`, do not mark the task done.
3. Surface the blocker to the user with the minimum change needed to proceed.

### Review Failure

1. Do not complete the subtask while review findings remain open.
2. Send findings back to the same coder subagent when possible with the complete material coder constraints.
3. Re-run the requested review after fixes with the complete reviewer role contract.
4. If the reviewer reports `NEEDS_CONTEXT` or `BLOCKED`, stop and surface the missing context or blocker before continuing.

### Workflow Interruption

- Current task stays in `in_progress`.
- Resume later with `/execute-all`.
- Workflow picks up from the next `get_next_task` result.

## Example Session

```text
User: /execute-all

Claude: Using superpowers-workflow to execute pending tasks.

[Calls get_next_task]
Task #7: Add user authentication (priority: high, no subtasks)

[Calls start_task(7)]
Task #7 is now in_progress. No subtasks exist, entering planning phase.

Dispatching ordinary subagent `plan_task_7` with planner prompt...

[Calls wait_agent for plan_task_7]
[Planner creates subtasks]
Planner: Created 3 subtasks.

Planning complete. Starting execution.

[Calls get_next_task - returns Task #8]
[Calls start_task(8)]

Dispatching ordinary subagent `code_task_8` with coder prompt...

[Calls wait_agent for code_task_8]
[Coder completes]
Coder: DONE

[Coding task requires review]
Dispatching ordinary subagent `review_task_8` with reviewer prompt...

[Calls wait_agent for review_task_8]
[Reviewer completes]
Reviewer: No findings remain. Approved.

[Calls complete_task(8)]
Task #8 completed.

[Calls git status]
[Calls git add <changed files> tasks/]
[Calls git commit -m "feat: add authentication scaffolding"]
Committed Task #8.
```

## Remember

- Always start tasks before working on them.
- Always complete tasks after finishing and review approval.
- Commit immediately after every successfully completed subtask.
- The workflow controller owns task selection, task state changes, dispatch decisions, phase transitions, commits, and user communication.
- Planner, coder, and reviewer behavior comes from the complete initial prompt for that invocation.
- Provide full context to subagents because they do not share your session history.
- Use `fork_turns: "none"` for every `spawn_agent` dispatch.
- After every `spawn_agent`, immediately call `wait_agent` for that exact subagent and block until it finishes.
- Stop on real dispatch failures, `NEEDS_CONTEXT`, `BLOCKED`, unexpected non-writing phase mutations, and unresolved review findings.
- At workflow end, verify the worktree is clean. If only task-state changes remain, commit them with `git add tasks/ && git commit -m "chore: update task states"`.
