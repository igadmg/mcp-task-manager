# MCP Task Manager — agent workflow

This server manages a project's task backlog and workflow files assosiated with task.
Workflows should use tools `write_task_file`, `read_task_file`, `list_task_files`
to access and write data assosiated with workflow step (ex. task, research, design etc.)
Tasks are identified by an id — a numeric auto-increment string by default, or a
custom text id passed explicitly via `create_task`'s `id` parameter. `start_task`
makes a task your current task; `get_current_task` returns it, so a workflow
never has to keep the id itself.

## Planning
- `create_task` — add work items as they're identified (title, description,
  priority, type, optional `parent_id` for a subtask, optional `id` for a
  caller-supplied custom task id).
- `update_task` — adjust status, priority, or details as understanding changes.
- `list_tasks` — review current backlog; top-level tasks by default, pass
  `parent_id` to see subtasks of one task.
- `add_relation` / `remove_relation` — link tasks, e.g. `blocked_by` to
  express a dependency (`get_next_task` and `start_task` respect it).

## Implementation loop
1. `get_next_task` — fetch the highest-priority actionable `todo` task. It
   skips parents with incomplete subtasks and tasks blocked by an unfinished
   `blocked_by` relation.
2. `start_task` — mark it `in_progress` before making changes. It becomes
   your current task: `get_current_task` returns it from then on.
3. Do the work.
4. `complete_task` — mark it `done` when finished. Completing the last open
   subtask auto-completes its parent (unless the parent works on a git
   branch, see below).

## Git branching (when enabled)
When the project enables `git.branching`, `start_task` and `complete_task` do
all the git work. Do not commit code, create branches or switch branches
around them — the server does, and refuses to move away from uncommitted
changes it does not own.

- After `start_task`, you are on the task's wip branch (`branch` in the
  result): `<user>/wip/<name>`, or `<parent wip>--<name>` for a subtask.
  Calling `start_task` again on a task whose branch exists restarts it: the
  branch is rebased onto the current base and checked out.
- After `complete_task` of a top-level task, you are on its final branch
  (`final_branch`), which holds exactly one commit over the base. Pass the
  message for that commit as `commit_message`. A completed subtask is merged
  as one commit onto its parent's wip branch, and you are back on it.
- Closing with another resolution while on the task's branch saves your
  uncommitted work on the wip branch and returns you to the base branch (or
  the parent's wip).
- `update_task` refuses status moves that belong to these calls; reopening to
  `todo` is allowed and keeps the branch for a later restart.
- The server never commits task records. If the tasks directory is versioned,
  commit it yourself.

## Closing a task whose work will not happen
Not every task ends in delivered work. Pass `complete_task` a `resolution` to
record which kind of ending it was, instead of marking it `done` and explaining
in the description:

- `obsolete` — the task no longer applies; the code or the plan moved out from
  under it.
- `superseded` — another task covers it now; also `add_relation` a
  `superseded_by` edge to it.
- `duplicate` — already tracked elsewhere; pair with `duplicate_of`.
- `wontfix` — understood, and deliberately not being done.

Add a `resolution_note` with the one-line why. A task closed this way does not
need to be `in_progress` first, and its open subtasks are closed with it.
`list_tasks` can then filter by `resolution`, so abandoned work never again
looks like shipped work.

## Reviewing the backlog
When checking whether tasks still reflect the code, `update_task(verified:
true)` stamps `verified_at` on the ones you confirmed are still accurate. It
affects nothing else — it just tells the next review which tasks have already
been checked and which have not been looked at since they were filed.

Use `get_task` to pull full details (including subtasks) for a specific task
when the user names one directly, rather than `get_next_task`.

## Notes and research
Attach free-form files to a task instead of losing context between turns:
`write_task_file`, `read_task_file`, `list_task_files`. These live alongside
the task and move with it on archive/delete.

## Cleanup
`archive_task` moves a closed task (and its files) out of the active list,
whatever its resolution; `delete_task` removes a task outright (`delete_subtasks: true` to
cascade). Archived tasks are read-only but still listable/gettable.
