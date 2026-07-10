# Migration to Implicit Subagent Roles

## Summary

Migrate `superpowers-workflow` away from Codex custom role agents installed from
`.codex/agents/*.toml` or `~/.codex/agents/*.toml`. The workflow controller will
spawn ordinary subagents and place the complete planner, coder, or reviewer role
contract in each subagent's initial prompt.

The current `spawn_agent` tool exposed to this repository accepts only
`task_name`, `message`, and `fork_turns`. It does not expose a role, model, or
reasoning-effort parameter. The migration must therefore work without any of
those optional controls. Model selection remains capability-based guidance, not
a prerequisite for correct role dispatch.

## Goals

- Preserve the existing planner, coder, and reviewer boundaries without relying
  on Codex agent discovery or role TOML files.
- Make every spawned agent self-contained by including its complete role,
  assignment, constraints, expected result, and relevant task context in the
  initial prompt.
- Remove the plugin installation step and fallback behavior that exist only to
  discover custom agents.
- Preserve sequential planner, coder, and reviewer handoffs and controller
  ownership of task state and commits.
- Use role-specific model recommendations when the active `spawn_agent` schema
  supports them, while allowing user preferences and runtime constraints to
  override every recommendation.

## Non-Goals

- Adding model-selection fields to Codex or implementing a new agent runtime.
- Guaranteeing a sandbox boundary through prompt text alone.
- Changing task selection, task lifecycle, review requirements, or commit
  ownership beyond what is necessary for implicit role dispatch.
- Redesigning the task-manager MCP protocol.

## Current State and Constraints

The role definitions currently live in:

- `plugins/mcp-task-manager/agents/planner.toml`
- `plugins/mcp-task-manager/agents/coder.toml`
- `plugins/mcp-task-manager/agents/reviewer.toml`

They are installed globally by
`plugins/mcp-task-manager/scripts/install-codex-agents.sh`, documented by both
copies of the `install-agents` skill, declared through the plugin manifest's
`agents` entry, and referenced by the workflow skill, command, and README.

This approach no longer provides reliable dispatch because the current
`spawn_agent` call cannot select one of those role definitions. It also cannot
select a model or reasoning effort. The initial `message` is the only reliable
per-dispatch role surface available today.

The TOML files currently provide `sandbox_mode = "read-only"` for planner and
reviewer and `sandbox_mode = "workspace-write"` for coder. Moving the text into
prompts preserves behavioral instructions but not an enforceable sandbox. The
controller needs explicit checks around non-writing phases to detect accidental
workspace changes.

## Target Design

### 1. Ordinary Subagents With Complete Initial Prompts

Remove role-agent resolution. For each phase, call `spawn_agent` with:

- a descriptive `task_name`, such as `plan_task_123`, `code_task_124`, or
  `review_task_124`;
- `fork_turns: "none"`, so the workflow does not depend on cloned conversation
  context;
- a `message` containing the full role contract followed by the concrete
  assignment and all context required to perform it.

Do not describe the message as supplemental instructions. It is the role
definition for that invocation.

Each role prompt must carry forward all material rules from its current TOML
definition, including required behavior, prohibited actions, status vocabulary,
task-manager ownership, git restrictions, scope limits, and escalation rules.
The prompts must also tell the subagent to read applicable `AGENTS.md` files and
relevant repository files before acting.

### 2. Planner Prompt Contract

The planner's initial prompt must include:

- the explicit identity `You are the planning-only agent for this assignment`;
- the parent task ID, title, priority, type, and complete description;
- planning responsibilities and all current planner prohibitions;
- the exact required subtask structure: files, steps, code guidance,
  verification command, and commit message;
- permission to create only the requested child tasks through `create_task`;
- `NEEDS_CONTEXT` behavior for missing or contradictory requirements;
- a prohibition on production-file edits, implementation, review substitution,
  task completion, git operations, and further subagent delegation.

The controller records the worktree state before dispatch and compares it after
the planner returns. Unexpected file changes stop the workflow and are reported;
the controller must not silently revert them.

### 3. Coder Prompt Contract

The coder's initial prompt must include:

- the explicit identity "You are the implementation-only agent for this
  assignment";
- the subtask and parent identifiers, full task specification, priority, and
  relevant controller-provided context;
- all current coder required behavior and prohibitions;
- permission to edit only files needed by the assigned implementation;
- instructions to use relevant execution and verification skills when present;
- the required result vocabulary: `DONE`, `DONE_WITH_CONCERNS`,
  `NEEDS_CONTEXT`, or `BLOCKED`;
- a prohibition on task-manager mutations, commits, unrelated cleanup, planning
  expansion, self-approval, and further subagent delegation.

When review finds issues, continue the same coder subagent with a follow-up
message when possible. The follow-up contains the findings and repeats the
material scope and ownership constraints; it must not assume the role name alone
will restore those constraints.

### 4. Reviewer Prompt Contract

The reviewer's initial prompt must include:

- the explicit identity `You are the review-only agent for this assignment`;
- the original specification and enough repository/diff context to locate the
  implementation;
- spec-compliance-first and code-quality-second review behavior;
- concrete severity and file-reference requirements;
- an explicit approval result when no findings remain;
- a prohibition on editing files, fixing findings, mutating tasks, committing,
  replanning without a genuine plan defect, and further delegation.

As for planning, the controller compares worktree state before and after review
and stops on unexpected mutations without reverting them.

### 5. Model and Reasoning Recommendations

At every role dispatch, inspect the `spawn_agent` schema actually available in
the running session:

- If the schema has no model or reasoning controls, omit them. Continue with the
  runtime defaults and do not treat this as a fallback or an error.
- If the schema exposes such controls, pass only fields and values supported by
  that schema. Do not invent parameter names or unsupported values.
- Recommended defaults are:
  - planner: `gpt-5.6-sol` with reasoning `medium`, raised for unusually complex,
    ambiguous, or architecture-heavy planning;
  - coder: `gpt-5.6-terra` with reasoning `xhigh`;
  - reviewer: `gpt-5.6-terra` with reasoning `xhigh`.
- These are recommendations, never fixed requirements. An explicit user choice,
  model availability, policy, cost/latency constraints, or task-specific needs
  may override them at any time.
- If a requested or recommended setting is unavailable, omit or replace it with
  the user's approval when approval is relevant; role correctness must continue
  to come from the prompt, not the selected model.

This policy should be written so it remains valid if Codex later adds model and
reasoning fields to `spawn_agent`.

### 6. Remove Obsolete Resolution and Installation Behavior

Delete the custom-agent fallback ladder and `$install-agents` remediation. An
ordinary subagent receiving an implicit role prompt is now the primary path, not
a downgrade.

Retain error handling only for real dispatch failures, unavailable
task-manager/tools, missing task context, or role-agent result states such as
`NEEDS_CONTEXT` and `BLOCKED`.

Remove these obsolete package surfaces:

- `plugins/mcp-task-manager/agents/*.toml`;
- `plugins/mcp-task-manager/scripts/install-codex-agents.sh`;
- `skills/install-agents/SKILL.md`;
- `plugins/mcp-task-manager/skills/install-agents/SKILL.md`;
- the plugin manifest's `agents` entry;
- all user instructions to run `$install-agents` or restart Codex for role
  discovery.

Do not remove the historical design document
`docs/superpowers/specs/2026-04-08-task-54-role-agents-design.md`; it records the
design that was valid for Task 54. Add a short superseded note linking to this
migration plan instead of rewriting history.

## Files to Change

### Workflow Sources

- `skills/superpowers-workflow/SKILL.md`
- `plugins/mcp-task-manager/skills/superpowers-workflow/SKILL.md`

Replace custom-agent discovery, resolution, fallback, model guidance, and the
three abbreviated dispatch messages with the target behavior above. Keep the
development and packaged copies behaviorally identical; use `cmp` as a final
consistency check unless packaging intentionally requires metadata differences.

### Commands and User Documentation

- `commands/execute-all.md`
- `plugins/mcp-task-manager/commands/execute-all.md`
- `README.md`
- `docs/superpowers/specs/2026-04-08-task-54-role-agents-design.md`

Describe implicit role prompts, remove the global-agent install prerequisite,
and explain that model choices are recommendations used only when supported and
not overridden.

### Plugin Packaging and Removed Files

- `plugins/mcp-task-manager/.codex-plugin/plugin.json`
- `plugins/mcp-task-manager/agents/planner.toml`
- `plugins/mcp-task-manager/agents/coder.toml`
- `plugins/mcp-task-manager/agents/reviewer.toml`
- `plugins/mcp-task-manager/scripts/install-codex-agents.sh`
- `skills/install-agents/SKILL.md`
- `plugins/mcp-task-manager/skills/install-agents/SKILL.md`

Remove the obsolete files and manifest declaration. Bump the plugin version as
part of release preparation because installed plugin caches need a new version
to receive the migration. Choose the exact version according to the repository's
release policy at implementation time.

## Implementation Sequence

1. Copy every material rule from the three TOML definitions into the matching
   phase prompt in the development workflow skill. Add descriptive task names,
   `fork_turns: "none"`, complete task context, and non-writing phase checks.
2. Replace role resolution and fallback sections with implicit-dispatch and
   actual-dispatch-failure handling. Update follow-up behavior for coder/reviewer
   iterations.
3. Add the capability-based model policy and the recommended, overridable role
   defaults. Clearly state the current no-model-parameter behavior.
4. Synchronize the packaged workflow copy and both command descriptions.
5. Remove agent TOMLs, installer script, install-agent skills, and the manifest's
   `agents` field.
6. Update README installation and usage instructions; annotate the historical
   Task 54 design as superseded.
7. Bump plugin packaging metadata according to release policy and verify the
   packaged tree contains no dangling references to deleted files.
8. Run the focused validation below and inspect the final diff for lost role
   constraints or unrelated changes.

## Verification

Run focused static validation:

```bash
cmp skills/superpowers-workflow/SKILL.md \
  plugins/mcp-task-manager/skills/superpowers-workflow/SKILL.md
cmp commands/execute-all.md plugins/mcp-task-manager/commands/execute-all.md
jq empty plugins/mcp-task-manager/.codex-plugin/plugin.json
rg -n 'install-agents|install-codex-agents|custom role agent|custom agent|\.codex/agents|~/.codex/agents' \
  README.md commands skills plugins/mcp-task-manager
rg -n 'planning-only agent|implementation-only agent|review-only agent|fork_turns|gpt-5\.6-(sol|terra)' \
  skills/superpowers-workflow/SKILL.md
```

The first search should return no obsolete live-package references. Historical
documents may retain old terminology only where explicitly marked superseded.

Manually verify these workflow scenarios against the final skill text:

1. Parent task without subtasks spawns an ordinary planner whose first message
   contains the complete planner contract and task context.
2. Executable subtask spawns an ordinary coder, then an ordinary reviewer, with
   complete and distinct role contracts.
3. Review findings return to the coder without granting task-state or commit
   ownership.
4. Planner or reviewer workspace mutations are detected and stop the workflow.
5. A `spawn_agent` schema without model controls still executes normally.
6. A future schema with model controls applies supported recommendations unless
   the user or runtime overrides them.

## Acceptance Criteria

- No active workflow or installation path depends on custom agent TOMLs or Codex
  agent discovery.
- Planner, coder, and reviewer prompts are self-contained and preserve all
  material restrictions from the removed TOMLs.
- Every spawn uses non-forked context and supplies the task data needed by that
  role.
- Planner and reviewer mutation checks compensate for the loss of enforced
  read-only role sandboxes and never auto-revert user changes.
- Model and reasoning selection is conditional on the actual tool schema, uses
  the recommended defaults when appropriate, and remains explicitly
  user-overridable.
- Development and packaged workflow/command copies remain synchronized.
- README and plugin metadata no longer advertise `$install-agents`.
- Focused validation passes and the plugin package has no dangling references.
