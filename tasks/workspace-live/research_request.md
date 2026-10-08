# Research request: workspace-live

**Additional research needed: 65 / 100**

Second highest, for the same reason as the tiler: the parent's `research.md`
established that htmx 2.0.4 *contains* the attributes this subtask needs, and
nothing more. Not one of them is used anywhere in the repository, so every
behavioural claim about them is currently unverified. The cost side also needs
measuring rather than reasoning.

## Already covered by research.md
- Exactly one element polls today: `#board`, replacing itself with
  `hx-get="/board" hx-trigger="every Ns" hx-swap="outerHTML"`
  (`internal/web/templates/_board.html:1`, same on the placeholder).
- `#panel` sits outside `#board` (`internal/web/templates/board.html:7-11`),
  so an open panel survives polls and is never refreshed — refreshing a column
  is new behaviour, not a copy of an existing pattern.
- `PollSeconds` is never set by either production call site
  (`internal/app/app.go:70`, `:110`), so it is always 5 and has no config key;
  the comment at `internal/web/server.go:12-15` explains why it is not 1.
- Each poll takes the service mutex, and `Index.All` / `Index.Get` call
  `syncIfStale`, which rebuilds the entire index when any task file's mtime is
  newer than `builtAt` or the directory count diverges
  (`internal/storage/index.go:202-249`, `:265`, `:290`).
- `app.js`'s existing shape: two IIFEs, document-delegated clicks, no request
  API, state re-applied on `htmx:afterSettle`, `htmx:load` and
  `htmx:historyRestore` (`internal/web/static/app.js:37-53`, `:141-159`).
- `TestAppJSStatsToggles` bans `fetch(`, `XMLHttpRequest`, `htmx.ajax`, `hx-`
  and `htmx-history-cache` inside `app.js`
  (`internal/web/assets_test.go:74-93`).
- The read-only proofs that must keep passing: the hardcoded path list plus the
  byte-identical directory sweep (`internal/web/handlers_test.go:252-275`,
  `:347-370`), and the race test's fetch list
  (`internal/web/race_test.go:52-71`).
- Assets are served immutable and linked by content hash
  (`internal/web/assets.go:19-41`).

## Gaps
1. **Scroll preservation — the core unknown.** Decide between `hx-preserve`,
   the `scroll:` / `show:` swap modifiers, and restoring `scrollTop` from
   `app.js` on `htmx:afterSettle`. Verify in a browser, not from the docs:
   whether `hx-preserve` keeps a *scrolled* element's position when its
   content changed (it preserves the element, which may mean the new content
   never appears — that would defeat the purpose), and whether the `scroll:`
   modifier can target a container rather than the window. The `app.js`
   fallback must stay request-free to keep `TestAppJSStatsToggles` green.
2. **Several polling fragments at once.** With a column per chain entry plus
   the working area plus `#board`, a depth-5 chain polls 7 endpoints every 5
   seconds, each taking the same mutex. Measure the real cost on this repo's
   backlog (which has an archive and dozens of tasks) and decide whether to
   poll one endpoint that returns the whole strip, use `hx-sync` to serialize,
   or stagger the triggers. This is also where the tiler's "board keeps
   polling off-screen" decision gets its cost confirmed — or revisited with
   evidence.
3. **Does a poll trigger an index rebuild in practice?** An agent writing a
   task file bumps its mtime, which makes `isStaleOnDisk` true and rebuilds
   everything on the next read. Measure the rebuild on a realistic tasks
   directory, including the archive, and whether a workspace polling 7
   endpoints can keep the service lock saturated while an agent is writing.
4. **Interaction with `hx-push-url` history.** Polling re-renders markup that
   carries the chain's links and the `translateX` step. Verify a poll does not
   push a history entry, does not fight Back, and that `htmx:historyRestore`
   leaves a sane strip. Depends on the tiler's gap 7 and should be checked
   together with it.
5. **A file being written mid-poll.** `WriteTaskFile` is atomic (temp file plus
   rename), so a torn read should be impossible — confirm that for the viewer
   path specifically, and decide what the column shows for a file that was
   deleted between the listing and the read.
6. **What "archived while open" actually renders.** Archiving moves the whole
   directory, the task leaves the active index, and `Detail` then returns no
   subtasks, blockers or relations by design
   (`internal/task/view.go:162-171`). Establish what each column in a chain
   rooted at or passing through that task shows at the next poll, and that
   nothing logs an error.
7. **Cap on `app.css` growth and the rebuild step.** Confirm
   `scripts/build-css.sh` still produces deterministic output from a clean
   checkout given the warning in README:697-700 that an unclean tree adds
   stray classes — this subtask is the one that commits the regenerated file,
   so the reproducibility question lands here.
8. **Doc scope.** The CLAUDE.md Web UI section is already very long and bullet
   dense; decide whether the workspace becomes one bullet, several, or its own
   subsection, and keep the "Dependencies list deliberately unchanged" note
   somewhere a future reader will find it (otherwise someone will "fix" it
   against the parent's original AC, which approved a markdown dependency that
   was later declined).
