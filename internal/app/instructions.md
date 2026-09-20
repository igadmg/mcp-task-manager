# MCP Task Manager — agent workflow

This server manages a project's task backlog and workflow files assosiated with task.
Workflows should use tools `write_task_file`, `read_task_file`, `list_task_files`
to access and write data assosiated with workflow step (ex. task, research, design etc.)
Tasks are identified by an id — a numeric auto-increment string by default, or a
custom text id passed explicitly via `create_task`'s `id` parameter — which should
be stored by workflow as the current work dir task.

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
2. `start_task` — mark it `in_progress` before making changes.
3. Do the work.
4. `complete_task` — mark it `done` when finished. Completing the last open
   subtask auto-completes its parent.

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
