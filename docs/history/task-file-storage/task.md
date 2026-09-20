# Task: task-file-storage

## Original input

> Надо добавить возможность хранить текстовые данные с таском. что-то типа тула read_task_file, write_task_file - эти файлы буту хранить связынные с таском описания задачи, исследования, дизайна с помощью этих тулов ии будет читать и записывать эти файлы

Translation: Add the ability to store text data attached to a task. Something like `read_task_file` / `write_task_file` tools — these files will hold task-related descriptions, research, and design notes, and an AI agent will read/write them via these tools.

## Clarifications

**Q: How should files attached to a task be named?**
A: Free-form name, chosen by the caller (agent/user) at write time — not restricted to a fixed set of categories like `description`/`research`/`design`.

**Q: Is a `list_task_files` tool needed to enumerate a task's files?**
A: Yes — add `list_task_files` alongside `read_task_file` / `write_task_file`, so an agent can discover existing files without guessing names.

**Q: What happens to a task's files on archive / delete?**
A: They move with the task. Archiving a task moves its files together with it into `tasks/archive/`. Deleting a task deletes its files too. This should stay consistent with the existing cascade behavior for relations described in CLAUDE.md.

**Q: How should on-disk storage be restructured to support attached files?**
A: Move from the current flat-file layout (`tasks/NNN.md`, `tasks/archive/NNN.md`) to a per-task directory layout, similar in spirit to the `.tasks/{task-name}/` folder convention used by this repo's own workflow skills: `tasks/{id}/{id}.md` holds the task record (same YAML-frontmatter + markdown-body format as today, just relocated), and the same directory is where attached files (from `write_task_file`) live alongside it. Archiving becomes moving the whole `tasks/{id}/` directory to `tasks/archive/{id}/`, which naturally carries attached files along with the task — this replaces/simplifies the need for separate cascade logic to move attached files.

**Q: What about the 89 existing archived tasks currently stored as flat `tasks/archive/NNN.md` files?**
A: Migrate automatically. On startup, the server/CLI should detect old flat-file layout tasks (in both `tasks/` and `tasks/archive/`) and migrate them into the new `{id}/{id}.md` directory layout, consistent with the self-healing philosophy already documented in CLAUDE.md for the index cache ("Self-healing: if index is missing/corrupt, rebuild from .md files").

## Problem statement

Tasks currently have only a single markdown body (the task description) and no way to attach additional free-form text artifacts (e.g. research notes, design docs, longer investigation logs) that an AI agent can read and write incrementally over the life of a task. This requires two coupled changes: (1) restructure on-disk task storage from flat files (`tasks/NNN.md`) to a per-task directory layout (`tasks/{id}/{id}.md`), with automatic migration of existing flat-file tasks (active and archived) on startup; and (2) add `read_task_file`, `write_task_file`, and `list_task_files` MCP tools (plus underlying storage support) so arbitrary named text files can be placed in that same per-task directory, discovered, read, and written, and are moved/deleted together with the task on archive/delete as a natural consequence of the directory-based layout.

## Acceptance criteria

- On-disk task storage is restructured from flat files to a per-task directory: active tasks live at `tasks/{id}/{id}.md`, archived tasks at `tasks/archive/{id}/{id}.md`. The `{id}.md` file keeps the exact current format (YAML frontmatter + markdown body) — only its location changes.
- On startup, any task still found in the old flat layout (`tasks/{id}.md` or `tasks/archive/{id}.md`) is automatically migrated in place to the new `tasks/{id}/{id}.md` / `tasks/archive/{id}/{id}.md` layout, without manual intervention and without data loss, consistent with the existing self-healing philosophy for the index cache.
- `Archive(id)` moves the entire `tasks/{id}/` directory to `tasks/archive/{id}/` (directory rename), rather than moving a single file — this is expected to naturally carry any attached files along with the task.
- A task can have zero or more named text files attached to it, with names chosen freely by the caller at write time.
- `write_task_file(task_id, filename, content)` creates or overwrites a named text file attached to the given task.
- `read_task_file(task_id, filename)` returns the content of a named file attached to the given task, with a clear error if the task or file doesn't exist.
- `list_task_files(task_id)` returns the names of all files currently attached to the given task.
- Attached files live inside the task's own `tasks/{id}/` directory, alongside `{id}.md`, consistent with the restructured storage layout above.
- Archiving a task (`archive_task`) moves its attached files into `tasks/archive/{id}/` together with the task's `{id}.md` file, as a consequence of moving the whole directory.
- Deleting a task (`delete_task`) deletes its attached files together with the task, as a consequence of removing the whole directory.
- Attempting to read/write/list files for an archived task follows the same read-only semantics as archived tasks in general (per CLAUDE.md: archived tasks can be listed/viewed but not updated) — reads are allowed, writes are refused.
- Behavior for subtasks (deleting/archiving a parent with subtasks) is consistent with existing subtask cascade rules.
- New tools are registered and documented consistent with the existing MCP tool table structure in CLAUDE.md/README.md.
