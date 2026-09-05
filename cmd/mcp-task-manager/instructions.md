# MCP Task Manager — agent workflow

This server manages a project's task backlog and workflow files assosiated with task.
Workflows should use tools `write_task_file`, `read_task_file`, `list_task_files`
to access and write data assosiated with workflow step (ex. task, research, design etc.)
Tasks are identified with text ids which should be stored by workflow as current 
work dir task.

## Planning
- `create_task` — add work items as they're identified (title, description,
  priority, type, optional `parent_id` for a subtask).
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

Use `get_task` to pull full details (including subtasks) for a specific task
when the user names one directly, rather than `get_next_task`.

## Notes and research
Attach free-form files to a task instead of losing context between turns:
`write_task_file`, `read_task_file`, `list_task_files`. These live alongside
the task and move with it on archive/delete.

## Cleanup
`archive_task` moves a completed task (and its files) out of the active
list; `delete_task` removes a task outright (`delete_subtasks: true` to
cascade). Archived tasks are read-only but still listable/gettable.
