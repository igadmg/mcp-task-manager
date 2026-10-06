---
id: done-stats-service
parent_id: done-column-stats
title: 'task.Service: compute done-column statistics from card definitions'
status: done
priority: medium
type: feature
relations:
  - type: blocked_by
    task: done-stats-config
created_at: "2026-10-06T17:56:10Z"
created_by: igor.cwer
updated_at: "2026-10-06T18:26:21Z"
resolution: completed
closed_at: "2026-10-06T18:26:21Z"
branch: igor.cwer/wip/done-column-stats--done-stats-service
base_branch: igor.cwer/wip/done-column-stats
start_commit: cf1bf77546f2f45e2185f31c1f6a6854ecaf688e
squash_commit: b484801d54541789479daaecd587ac92a013f769
---

Add a new view method (or extend BoardSnapshot) in `internal/task/view.go` that computes the statistics for the configured cards from `index.All()` (active index only, subtasks included).
- Bars: per field value, total / done / in_progress / todo and closed in the last 24 h. The close time is closed_at, falling back to updated_at. Values are ordered by Priority.Order(), task_types and Resolutions(); empty values are dropped. Resolution counts done tasks only, through EffectiveResolution.
- Lines: per-day series in local time over `days` days, today included: created, closed, cumulative, and split_by field with a metric.
- Add an injectable clock (`now func`) to the service for deterministic tests.
- Follow the twin discipline and register the method in TestServiceNoSelfDeadlock and TestServiceRace.
- Unit tests cover all the counting rules.