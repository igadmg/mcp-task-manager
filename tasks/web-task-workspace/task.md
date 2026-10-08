# web-task-workspace

## Original input

> давай создадим таск на такую тему
> у нас в UI веба панель с тасками справа самая четыре их на экране
> там выбирается таск а в таске надо сделать списком подтаски
> и когда их выбираешь панели съезжают влево до 4-ой колонки и открывается навоый ворспейс там будут свои панели для таска
> можно будет файлы прикрепленные к таску смотреть, диаграммы и тд

Follow-up (on navigation):

> Ты выбираешь там файл открыть например и все колонки сдвигаются и остается колонка таска и большая область показать файл, а кнопкой назад можно вернуться в предидущий сетап. свободного скроллинга мышкой не надо

## Clarifications

| Question | Answer |
|----------|--------|
| What are "the four panels"? | The current board: Todo, In progress, Done and the task panel (`#panel`, 22rem) on the right. |
| Navigation model | Opening something (for example a file) from the task panel shifts all columns left. The board columns leave the screen, the task column stays on the left, and the rest is a large area showing the file. A Back button returns to the previous layout. **No free mouse scrolling** of the panel strip: discrete states only. |
| Which tasks get a workspace | **Any task.** Clicking a subtask opens the workspace for that subtask. A task panel also has an "Open workspace" button for the task itself. |
| Subtask click | A **second narrow column** for the subtask appears next to the parent's column. The parent stays visible and marks the selected subtask. The subtask column lists the subtask's files, and a file opens in the large area. |
| Workspace content | **File viewer** (this task) and **backlog relations graph** (separate task `web-backlog-graph`). Not requested: phase timeline, rendered mermaid. |
| File rendering | **Markdown rendered on the server** (headings, lists, tables, fenced code, links). **Mermaid blocks stay as code blocks.** A new Go dependency for markdown (for example goldmark) is acceptable. Rendering mermaid would need about 3 MB of vendored `mermaid.js` and was declined. |
| Live update | **Yes, polling.** The task column(s) and the open file refresh like the board, and the file keeps its scroll position. |
| Tracker structure | Two independent top-level tasks: this one and `web-backlog-graph` (`blocked_by` this one). |
| Priority | high |

Defaults taken without asking (correct them in design if wrong):
- Each workspace state has its own URL (`hx-push-url`, as card clicks already do), so browser Back, reload and deep links reproduce the layout. The on-screen Back control and browser Back behave the same.
- The dashboard stays read-only (GET only).
- Archived tasks get a workspace too, read-only, because the detail route already serves them.
- `*.phase` files are not listed, as in the detail view.
- Below `lg`, where the board already stacks, nothing slides. Columns and the viewer stack vertically.
- The slide animation is disabled under `prefers-reduced-motion`.

## Problem statement

The dashboard shows a task only in a narrow side panel: attached files appear as names that cannot be opened, and subtasks are plain links to a separate page. A task's research, design and plan therefore cannot be read in the UI. Add a task workspace: opening a file or a subtask from the task panel slides the board away, keeps the task column on the left, and shows the file, rendered as markdown, in a large viewer. A Back control returns to the previous layout.

## Acceptance criteria

### Layout and navigation
- Until something is opened from the task panel, the board looks as it does today: Todo, In progress, Done and `#panel`.
- In the task panel, the subtasks list (id, title, status) and the attached files are clickable and open inside the workspace. They no longer navigate to a separate full page. The task's own description is also an openable item, shown in the viewer like a file.
- Opening a file of the task slides the board columns out to the left. The task column moves to the leftmost slot, and the remaining width is the viewer showing the file.
- Opening a subtask works the same way, and a narrow subtask column appears next to the parent column. The parent column marks the selected subtask. A file opened from the subtask column shows in the viewer.
- Choosing another file or subtask inside the workspace replaces the viewer or the subtask column; no further columns are added. Nesting is single-level, so the maximum is parent column + subtask column + viewer.
- An "Open workspace" button in the task panel opens the workspace for the task itself, with its description in the viewer.
- A Back control steps back through the previous layouts, ending at the board with the task panel open. Browser Back does the same. Every state has its own URL, and a reload or deep link restores that layout.
- The strip moves only in discrete steps, with a slide transition (instant under `prefers-reduced-motion`). The panel strip cannot be scrolled horizontally with a mouse or trackpad.
- Links to other tasks inside a workspace column (parent, blockers, relations) keep the user in the workspace. The design decides how: re-root on that task, or replace the column.
- Below `lg`: no sliding. Columns and the viewer stack vertically, with no horizontal page scroll.

### File viewer
- Files are listed as in the detail view: `*.phase` is excluded and the order is stable. The open file is marked.
- `*.md` files and files without an extension (older tasks have `design`, `research`) are rendered as markdown on the server: headings, lists, GFM tables, fenced code, links. Other files are shown as preformatted text. ```` ```mermaid ```` blocks render as ordinary code blocks.
- Safety: raw HTML in a file is escaped and never executed. `javascript:` and other dangerous URLs are neutralized. No inline `<script>` or `style` is introduced. The rendered markdown is the only pre-trusted HTML fragment, produced with raw HTML disabled.
- The file name in the URL is validated with the existing attached-filename rules, so path traversal is impossible. An invalid name returns 400 or 404, never a 500.
- The viewer scrolls independently of the page, and the columns stay in place.
- Works for archived tasks (`task.Service.ReadTaskFile` already falls back to the archive). A missing file, or a task deleted while open, shows a clear "gone" state like `_detail.html`'s Missing state.
- Offline: no CDN. Any new JS or CSS is vendored under `internal/web/static/` and embedded. The new markdown dependency is added to the Dependencies list in CLAUDE.md.

### Live update
- While a workspace is open, the task column(s) and the open file refresh every `PollSeconds`, so a file an agent is writing updates on screen.
- The viewer keeps its scroll position across a refresh, and the open state (selected file, selected subtask) survives the poll.
- A task archived or deleted while open shows its archived or gone state at the next poll.
- The design decides what the board's own poll does while the board is off-screen: pause or keep running.

### Structure
- The read-only rule stays structural: only new `GET` routes. Handlers read `Resolver.Current()`, never `Get()`, and reach data only through `task.Service`; new read methods go in `internal/task/view.go` if needed. Nothing writes to stdout.
- The viewer area is generic: it can host views other than files. `web-backlog-graph` opens the backlog graph there next.

### Tests and docs
- Handler tests for the new routes:
  - markdown rendered to HTML (heading, table, code block);
  - raw HTML and `javascript:` links neutralized;
  - extensionless file rendered as markdown, `.txt` or `.yaml` shown as plain text;
  - bad filename returns 400 or 404;
  - `*.phase` not listed;
  - archived task;
  - missing file;
  - the subtask column fragment.
- `go test -race ./...` passes.
- New template classes are built into `app.css` (`scripts/build-css.sh`), and `assets_test` is updated if it checks them.
- CLAUDE.md is updated: the Web UI section (workspace, routes, polling), the project structure, and Dependencies. The README is updated if it describes the UI.

## Revision — 2026-10-07, before the split into subtasks

The navigation model was reopened while answering the planning questions and
now **supersedes** the single-level rule above ("Nesting is single-level, so
the maximum is parent column + subtask column + viewer" and "Choosing another
file or subtask inside the workspace replaces the viewer or the subtask
column; no further columns are added"). Those two sentences no longer hold.

Decided:

| Question | Decision |
|----------|----------|
| Chain depth | **Unbounded.** Opening anything from a column appends a new column on the right and the strip shifts left. Columns are abstract: a column is whatever its URL segment names (task, file, later the backlog graph). |
| URL scheme | Path-based, `type/ref` pairs: `/tasks/42/w/t/43/f/plan.md`. The tiler parses the path into a list of column descriptors; truncating the path is how the chain rolls back. |
| `/tasks/{id}` | Repurposed: it now renders the board with that task's panel open — the state the Back chain ends at. The standalone `max-w-3xl` detail page stops being a separate surface. |
| Nesting path | A pinned narrow rail on the far left lists the whole chain. Clicking an entry truncates the chain to it. It is the primary Back; browser Back does the same thing. |
| Strip mechanics | The whole strip, board columns included, stays in the DOM; a state is a `translateX` step. The board therefore keeps polling while off-screen. |
| Tiler layout rule | Each column type declares its width (task: narrow, file: wide, graph: wider). The tiler lays out right to left and pushes older columns off the left edge; what fell off is reachable through the rail. |
| Markdown | Own renderer, zero new dependencies: headings, lists, GFM tables, fenced code, links, emphasis. `go.mod` is not touched, so the offline guarantee covers the Go build too. CLAUDE.md's Dependencies list therefore stays as it is; the renderer is documented instead. |
| Bad filename | The existing name rules are exported from the task layer and called by the web layer before reading (→ 400). Any read failure → 404. The `.` hole (passes validation, `os.ReadFile` fails with a non-`IsNotExist` error → 500) is closed as part of that. |

The acceptance criteria above still apply except where this table contradicts
them. Split into six subtasks, by layer; see the parent's subtask list.
