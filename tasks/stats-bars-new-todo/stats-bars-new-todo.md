---
id: stats-bars-new-todo
title: 'Done-stats bars: segment for freshly created todo tasks'
status: todo
priority: medium
type: feature
created_at: "2026-10-07T21:13:17Z"
created_by: igor.cwer
updated_at: "2026-10-07T21:13:17Z"
---

Add a fifth colour to the Done-column stacked bars: the tasks that arrived
recently and are still waiting. Today a bar has `bar-done`, `bar-recent`
(closures in the last 24 h, drawn over the end of done), `bar-in_progress`
and `bar-todo` — inflow is invisible, only outflow is.

## What it counts

Tasks whose `created_at` is inside the window **and** whose status is still
`todo`. Mirror image of `ClosedRecently`: that one is part of `Done`, this
one is part of `Todo`. Same counting rules as the rest of `statsBars`
(every task once, subtasks included, empty field values dropped), and a
future `created_at` is excluded the way a future close time already is
(`internal/task/stats.go:98`).

## How it is drawn

A sub-segment inside todo, not a sixth column of the stack: the bar's total
width does not change, `Todo` keeps its full count, and the new `<rect>` is
painted over the **start** of the todo run (the boundary with in progress,
so recent work sits in the middle of the bar and the stale tail stays at the
far right). Fifth class `.bar-new` in `internal/web/assets/input.css`,
alongside `.bar-recent`.

Geometry stays in the template, as `_stats_bars.html` already does it:
`x` and `width` in task counts, no `style` attribute, no geometry in Go. The
new rect's `x` is the existing `TodoX`, so the view model needs only the
count (`StatsBarView.New`); `internal/web/view.go:403-405` maps it.

## Row label

The meta line gains the count next to `+M`, dropped at 0 like the others:

    done/total · N open · +M · ↑K

Pick the glyph during design — `+M` is already taken by closures, so the new
one must read as inflow and not collide with it. The bar's `aria-label`
grows a clause for it too.

## Window

Its own configurable window, **not** `statsRecent`. Proposal to settle in
design: `new_hours` on a bars card in `web.done_stats.cards`, defaulting to
24, filled in `internal/config/stats.go` with the other per-card defaults
(`<= 0` means the default, like `days`). Open question for design: whether
`bar-recent`'s 24 h becomes configurable the same way (`recent_hours`) so
the two sides of the bar are symmetric in config as well as on screen, or
whether it stays a constant. Note that a card's derived `id` is built from
its content (`bars-<field>`), so a new key must not silently change
existing ids / stored line-toggle keys.

## Touches

- `internal/task/stats.go` — `StatsBar` field, `statsBars`, the clock
  (`WithClock`) already pins "now" for tests
- `internal/config/stats.go` — the per-card key and its default
- `internal/web/view.go`, `internal/web/templates/_stats_bars.html`,
  `internal/web/assets/input.css` (+ `scripts/build-css.sh` rerun)
- CLAUDE.md: the "Done statistics (data)" and "Done column" bullets, and the
  config block

## Done when

- A bar with freshly created todo tasks shows the new colour over the start
  of its todo run, with the row count in the meta line
- Counts still add up: `Done + InProgress + Todo == Total`, and the new
  count never exceeds `Todo`
- Config: absent key behaves as 24 h; the window is honoured in a test with
  a pinned clock
- `go test ./... -race` green, CSS regenerated and committed