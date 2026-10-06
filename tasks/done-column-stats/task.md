# done-column-stats

## Original input

> начнем таск на тему надо в колонке тасков done показывать карточки но не с тасками а с граффиками тасков по тегам типа прогрессбары такие сколько таких тасков всего сколько выполнено сколько ещё открыто и выделено отдельно изменение за последний день

Follow-up:

> имей ввиду что я график не столбиками хочу показывать а линиями
> и чтобы можно было настраивать в карточке какие линии показывать
> эти карточки должно быть лекго задавать из конфига

## Clarifications

| Question | Answer |
|----------|--------|
| What is a "tag"? The task model has no `tags` field. | Tags are the task's fields. Example: a **priority** card holds bars for how many tasks are high, medium and low. For time: a chart of task counts per period. |
| Fields that get a bars card by default | `priority`, `type`, `resolution`. Nothing hardcoded: **cards are easy to define in the config.** |
| What happens to the regular done-task cards in the Done column? | They are removed. The column holds only statistics cards. A done task stays reachable at `/tasks/{id}`. |
| What is "the change in the last day"? | **+N closed**: tasks of that value that became `done` in the last 24 h. |
| Scope of "total" | **Active index only**. Archived tasks are not counted. |
| Subtasks | Counted: every task in the index counts once. |
| Bar segments | **done / in progress / todo**. The part of done closed in the last 24 h is highlighted. Label like `7/12 · 5 open · +2`. |
| Time chart form | **Lines, not bars.** Per-day series over a configurable period. |
| Choosing which lines a card shows | **Config + UI toggles.** The config sets which lines a card has and which start visible. Clicking a legend entry in the card hides or shows that line. The choice is kept per browser in `localStorage` and survives the htmx board refresh. The server stays read-only. |
| Line kinds available | **created / closed per day**, **cumulative created / closed** (burn-up), **per field value** (one line per value of a chosen field for a chosen metric, e.g. closed per day for each priority). An "open at end of day" backlog line was **not** requested. |
| Config shape | **A list of cards**, rendered in list order. Each card is either `bars` (one field) or `lines` (time chart). Several `lines` cards may coexist with different periods and line sets. A missing section falls back to defaults. |

## Problem statement

The board's Done column lists finished task cards that say little about progress. Replace them with a config-defined list of statistics cards:
- bar cards: per task field, each value's total / done / in progress / todo with the last day's closures highlighted;
- line-chart cards: per-day created / closed counts (plain, cumulative or split by a field's values), whose lines the viewer can toggle.

## Acceptance criteria

### Column
- The Done column renders no individual task cards, only the configured statistics cards, in config order. The column header still shows the number of done tasks.

### Configuration
- Cards are defined in `mcp-tasks.yaml` as a list. Adding, removing or reordering a card, or changing a card's lines, needs no code change. Proposed shape (final names decided in design):
  ```yaml
  web:
    done_stats:
      cards:
        - kind: bars
          field: priority
        - kind: bars
          field: type
        - kind: bars
          field: resolution
        - kind: lines
          title: Last 2 weeks
          days: 14
          lines: [created, closed, created_cumulative, closed_cumulative]
          hidden: [created_cumulative, closed_cumulative]   # present in the legend, off by default
        - kind: lines
          title: Closed by priority
          days: 30
          split_by: priority
          metric: closed           # created | closed (cumulative variant decided in design)
  ```
- Defaults when `done_stats` (or `cards`) is absent: bars for `priority`, `type`, `resolution`, plus one `lines` card for the last 14 days with `created` and `closed`. Applied through `config.applyDefaults`, consistent with the existing partial-section rule. Per-card defaults (for example a missing `days` → 14, missing `title` → derived) are filled the same way.
- Invalid entries (unknown `kind`, unknown field, unknown line name, non-positive `days`) are handled consistently. The design decides between a config load error and skipping the card with a stderr log line, and documents the choice.
- The supported fields at least cover `priority`, `type`, `resolution`. The design considers `created_by` and `status` as cheap additions.

### Bars cards
- One row per value: a stacked bar with done / in progress / todo segments, sized relative to that value's total. Label `done/total · N open · +M`, where `+M` is omitted when M = 0.
- The part of done closed in the last 24 h (`closed_at`, falling back to `updated_at` for done tasks without it) is visually distinct inside the done segment.
- Value order follows the domain order where one exists: priority critical→low, type in config `task_types` order, resolution in its enum order. Values with zero tasks are not shown.
- `resolution` applies to done tasks only. Its card counts done tasks only (a done task without a resolution counts as `completed`) and has no open segments.

### Lines cards
- An SVG line chart rendered on the server (inline SVG from the template, no JS charting library, offline). One point per calendar day over the last `days` days, in server local time, today included.
- Line kinds:
  - `created`: tasks with `created_at` on that day;
  - `closed`: done tasks whose close time (`closed_at`, fallback `updated_at`) is on that day;
  - `created_cumulative` / `closed_cumulative`: the running total over the window. The design decides whether the total starts at 0 or counts tasks before the window, and documents it;
  - split by field: one line per value of `split_by` for the given `metric`.
- A legend lists every line of the card with its color. Clicking a legend entry toggles that line. Initial visibility comes from the config (`hidden`). The viewer's toggles are stored in `localStorage` per card and line (keyed so that a config change does not break it) and are re-applied after each htmx swap. With storage unavailable, the config defaults apply and the chart still renders.
- Axis or day labels and per-point values are readable, for example through SVG `<title>` tooltips. Colors are distinguishable and legible in light and dark mode.
- The toggle JS lives in `static/app.js` (delegated listener, no new dependency, no server request).

### Data and architecture
- Counts come from the active index only. Archived tasks are excluded and no archive scan runs on board polls.
- Every task in the index counts once, subtasks included.
- Statistics are computed in `internal/task` (a new or extended `task.Service` view method, per the package boundary rule) from the card definitions. `internal/web` maps and renders only. The web layer stays read-only (GET only) and never resolves the project (`Resolver.Current()`).
- Works with the existing htmx poll refresh, at the column's narrow width, in light and dark mode. The vendored CSS is regenerated with `scripts/build-css.sh` if new classes are used.

### Tests and docs
- Tests: service-level counting (per field, 24 h window, closed_at fallback, subtasks, resolution default, value order, per-day buckets, cumulative series, split-by series), config defaults / partial sections / invalid entries, web view mapping, and a handler test showing the Done column renders the configured cards and no done-task cards.
- CLAUDE.md (Web UI, Configuration) and README document the new column, the card config and the line toggles.
