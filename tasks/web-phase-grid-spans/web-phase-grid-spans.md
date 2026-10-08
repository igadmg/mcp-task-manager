---
id: web-phase-grid-spans
title: 'In progress: phase grid, parent cards span their subtasks'' phase range'
status: in_progress
priority: medium
type: feature
relations:
  - type: relates_to
    task: in-progress-phase-columns
created_at: "2026-10-07T21:05:19Z"
created_by: igor.cwer
updated_at: "2026-10-08T19:21:47Z"
---

**Complexity: high** — a full rework of the In progress column geometry (view model + templates + CSS grid), not a cosmetic change.

Rework the In progress column from the current "one vertical stack of half-card-shifted lanes" into a real four-column phase grid, and make a parent card with in-progress subtasks span the phase range of its group instead of sitting in a single lane.

## Why

Today (`internal/web/view.go:353` `newPhaseLanes`, `_board.html` `.lanes`, `assets/input.css:50-73`) every card lives in exactly one lane, and a parent holding nested in-progress subtasks is placed in the *furthest* phase of its group. That hides the spread: a parent whose subtasks are in research, design and planning looks like a planning task, and the nested rows say nothing about where each subtask actually is.

## Wanted behaviour

- **Real lane grid.** The column becomes a four-column grid (research, design, planning, implementation). Every in-progress card is positioned in it, not just indented by `margin-left`. This replaces the current overlapping-lane geometry; the lane headers and their counts stay.
- **Parent spans its range.** A card with nested in-progress subtasks spans from the earliest to the latest phase in its group (its own phase included), e.g. research..planning. A card without subtasks occupies its single phase column as before.
- **Subtasks shift inside the card.** Each nested subtask row is indented horizontally to its own phase lane, aligned with the grid the parent spans (the chosen presentation: offset only, no phase badge). A subtask with no readable phase falls back the same way a card does today (`task.Phases` / artifact-name fallback).
- **Counts stay honest.** The column header counts every in-progress task; a lane count still counts the cards sitting on that lane, so the lane counts add up to the column total. Decide and write down what a spanning card counts as (likely: its own phase lane only) and keep the invariant tested.
- **Narrow columns.** Keep the current graceful degradation: cards keep a minimum width and the grid collapses to a plain stack below the existing container-query threshold.

## Scope notes

- Data is mostly there: `BoardSnapshot.Phases` / `PhaseInfo` already covers in-progress tasks, and `CardView.Subtasks` (`view.go:176`) holds the nested rows. Likely needs a per-card phase span (first/last lane) plus a per-subtask lane rank in the view model, and the geometry expressed as named CSS classes so Go keeps computing no pixels (existing rule: `--lane` comes from `.lane-<phase>`).
- Read-only, presentation only. No service mutation, no new routes.
- `scripts/build-css.sh` must be re-run after the CSS changes.
- Tests: lane bucketing / span computation in `internal/web`, counts invariant, markup assertions for the spanning card and the shifted subtask rows.

Related: `in-progress-phase-columns` (the task that introduced the current lanes).