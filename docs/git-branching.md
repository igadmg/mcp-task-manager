# Git Branch-per-Task

When git branching is on, every task you start gets its own branch, and
completing the task turns all of its work into one clean commit. The MCP server
does the git work itself inside `start_task` (or `start_phase` with phase
`implementation`) and `complete_task`. You and your agents never need to
create, switch, squash or commit branches by hand.

This guide covers how to turn branching on, what a normal task looks like, and
what to do when a call is refused. For a short reference, see
[Git Branch-per-Task in the README](../README.md#git-branch-per-task).

## Turning it on

Branching is off by default. Turn it on in the project's `mcp-tasks.yaml`:

```yaml
git:
  branching: true
  # Optional. The first of these branches that exists is where new tasks start.
  base_branches: [main_patched, master_patched, main, master]
```

Or set `MCP_GIT_BRANCHING=true` in the MCP server's environment. The variable
overrides the config file in both directions.

You also need:

- **A git repository at the project root.** The server runs git there, not in
  its own working directory.
- **`git config user.email`.** The part before `@` becomes the prefix of your
  branch names, so `jane@example.com` gets branches under `jane/`.
- **A recent git.** Restarting a task uses `git replay --ref-action=print`,
  which has been verified with git 2.54 and 2.55.
- **A server build that includes the feature.** After you rebuild the binary,
  restart the MCP server so your client picks up the new build.

## A typical task

```bash
mcp-task-manager start 42
# Task #42 started on branch jane/wip/42-login-form.

# ...edit code, commit as often as you like (or not at all)...

mcp-task-manager complete 42 -m "feat: login form"
# ... Final branch: jane/42-login-form.
```

The MCP tools behave the same way. `start_task` returns the branch you are now
on, and `complete_task` takes the commit message as `commit_message`.

1. **`start_task` creates a work-in-progress (wip) branch.** For a top-level
   task, the server creates `<user>/wip/<name>` at the tip of the base branch
   (the first branch in `base_branches` that exists) and checks it out. This
   happens no matter which branch you were on before. The commit the branch
   starts from is saved on the task as `start_commit`.
2. **You work on the wip branch.** Commit as often as you like. The commit
   history on this branch is never rewritten or deleted.
3. **`complete_task` produces the final branch.** Everything on the wip branch
   becomes exactly one commit on `<user>/<name>`, placed directly on the
   start commit. That includes uncommitted and untracked changes. The server
   then checks out the final branch.
4. **Merging is up to you.** Merge `<user>/<name>` into your main branch or
   open a pull request from it. The server does neither.

If you leave out the commit message, the server uses the task's title and
description, followed by a `Task: <id>` trailer.

### Starting through phases

A workflow that records delivery phases (the `begin_task` skill does) never
calls `start_task`. It calls `start_phase` for research, design and planning,
which move the task to `in_progress` without touching git, and then
`start_phase` with phase `implementation`, which is where the branch is cut:

```bash
mcp-task-manager start-phase 42 research        # in_progress, no branch
# ...finish-phase, then design and planning the same way...
mcp-task-manager start-phase 42 implementation
# Started phase implementation of task 42 (run 1) on branch jane/wip/42-login-form.
```

The implementation start does everything `start_task` does - the same branch
names, base, uncommitted-change rules and restart - also for a task that is
already `in_progress` from its earlier phases. `start_task` on such a task
(in progress, no branch) refuses and points you to
`start_phase implementation`.

### Branch names

| Task | Wip branch | Final branch |
|------|------------|--------------|
| id `login-form` (readable text id) | `jane/wip/login-form` | `jane/login-form` |
| id `42`, title "Login form" | `jane/wip/42-login-form` | `jane/42-login-form` |
| subtask `43` "Validation" of task 42 | `jane/wip/42-login-form--43-validation` | none (merged into the parent's branch) |

A text id is used as it is if it contains at least one letter, uses only
lowercase letters, digits and hyphens, and is at most 48 characters long.
Otherwise the name is the id plus the title, converted to a slug.

If a branch with the final name already exists and this task did not create
it, `start_task` refuses to start.

## Uncommitted changes

Before `start_task` switches branches, it checks for uncommitted changes:

| Where your changes are | What happens |
|------------------------|--------------|
| On this task's own wip branch | The server saves them in a checkpoint commit on that branch first |
| On the base branch (fresh start) | They move along to the new wip branch |
| On any other branch | `start_task` refuses and lists up to 20 changed paths. Commit or stash them, then try again |
| Detached HEAD | `start_task` refuses |

Changes inside the tasks directory never count as uncommitted changes.

## Subtasks

- **Starting a subtask** creates `<parent wip>--<name>` from the tip of the
  parent's wip branch. If the parent is still `todo`, the server starts the
  parent in the same call.
- **A parent still in its own earlier phases** (`in_progress`, no branch yet)
  gets its wip branch cut, at the base tip, by the subtask's
  `start_phase implementation`, in the same call; the sub-wip then comes off
  it. A later implementation start of the parent restarts on that branch.
  `start_task` keeps the older rule: under such a parent the subtask starts
  without a branch.
- **Completing a subtask** squash-merges its work into the parent's wip branch
  as one commit and checks out the parent's branch. Subtasks have no final
  branch of their own. If the merge conflicts, nothing changes and the error
  lists the conflicting paths.
- **The parent is never completed automatically** while it has a branch, even
  when its last subtask is done. Complete it yourself.
- **The parent cannot be completed while a subtask is unfinished or missing.**
  The server refuses if any subtask is still open, or if a completed subtask's
  commit is no longer on the parent's branch (for example, after a reset).
- **Subtasks you never started** get no branch.

## Closing a task without delivering it

If you complete a task with a resolution other than `completed` (`obsolete`,
`superseded`, `duplicate` or `wontfix`), nothing is squashed:

- **If the task's branch is checked out,** any leftover changes go into a
  safety commit on the wip branch. Then HEAD goes back to the base branch, or
  to the parent's wip branch for a subtask.
- **If the branch is not checked out,** only the task record changes and git
  is left alone.

In both cases the wip branch stays where it is, so no work is lost.

## Resuming a task

If a task's wip branch still exists, running `start_task` on it again resumes
the task. This works when the task is `in_progress`, or when it was reopened
to `todo` with `update_task`. The server rebases the wip branch onto the
current tip of its base branch (or its parent's wip branch), checks it out,
and updates `start_commit`. This is how you pick up a task after `main` has
moved on, or rework a task that was already delivered.

If the rebase conflicts, nothing changes and the error lists the conflicting
paths. Resolve the conflict by hand, for example by merging the base branch
into the wip branch, and then call `start_task` again.

## The tasks directory

The server never commits task records, and server commits never contain the
tasks directory. The tasks directory can be:

- tracked in the code repository (you commit `tasks/` yourself),
- gitignored or nested inside the code repository,
- in a separate repository, or
- outside any repository.

Task records are ordinary files on disk, so they look the same whichever
branch is checked out.

Your current task is saved per user in
`<tasks_dir>/.users/<user>/current_task`. `start_task` sets it, and
`get_current_task` reads it. It describes what one person is working on, so
add `.users/` inside your tasks directory to `.gitignore`.

## What you should not do

- **Don't use `update_task` to start or close a task.** It refuses to move a
  task to `in_progress`, to close an `in_progress` task that has a branch, and
  to reopen a `done` branched task directly to `in_progress`. Use
  `start_task` (or `start_phase`) and `complete_task` instead. Reopening to
  `todo` is allowed.
- **Don't switch branches before `complete_task`.** Completing a task
  requires its own wip branch to be checked out.
- **Older tasks keep working without branches.** A task started while
  branching was off has no branch, and `complete_task` only updates its
  record.

## Troubleshooting

| Symptom or error | Cause and fix |
|------------------|---------------|
| No branch is created and the result has no `branch` field | Branching is off, or the server was built without the feature. Check `git.branching` and `MCP_GIT_BRANCHING`, rebuild if needed, and restart the server |
| `git config user.email is not set` | Run `git config user.email you@example.com` |
| `none of the base branches [...] exist` | Add your main branch to `git.base_branches` |
| `uncommitted changes on X (...)` | You are on a branch that is neither the base nor this task's wip. Commit or stash, then start again |
| `task N works on branch B, but HEAD is X` | Switch to `B` before calling `complete_task` |
| `branch B exists and was not created by task N` | Another branch already has that name. Rename or delete it |
| `cannot complete task P: subtask S is todo` | Finish the subtask, or close it with a resolution such as `obsolete` |
| `subtask S was completed but its work (...) is not in B` | The parent branch lost the subtask's commit. Restart `S`, or merge its commit back in |
| `rebasing B onto X conflicts in: ...; nothing was changed` | See [Resuming a task](#resuming-a-task) |
| `git branching is enabled; use start_task ...` | `update_task` refused a status change. Use the workflow tool it names |

Every refusal or failure leaves branches, HEAD, the index, your working files,
task records and the current-task pointer exactly as they were before the
call.

## Further reading

The full design, including sequence diagrams, rollback details and the
alternatives that were rejected, is in
[`tasks/git-branch-per-task/design`](../tasks/git-branch-per-task/design).
