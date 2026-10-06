---
id: done-stats-config
parent_id: done-column-stats
title: 'Config: web.done_stats.cards list with defaults'
status: done
priority: medium
type: feature
created_at: "2026-10-06T17:56:10Z"
created_by: igor.cwer
updated_at: "2026-10-06T18:14:21Z"
resolution: completed
closed_at: "2026-10-06T18:14:21Z"
branch: igor.cwer/wip/done-column-stats--done-stats-config
base_branch: igor.cwer/wip/done-column-stats
start_commit: 800eea5c01c78e65d8eeaa0544eeeab5d7963fc9
squash_commit: cf1bf77546f2f45e2185f31c1f6a6854ecaf688e
---

Add the `web.done_stats.cards` list to `internal/config`, with `bars` cards (field) and `lines` cards (title, days, lines, hidden, split_by, metric). Defaults when the list is absent (nil): bars for priority, type and resolution, plus a 14-day lines card with created and closed. `cards: []` means no cards. Per-card defaults are filled in `applyDefaults` after decoding, because yaml replaces the slice and each item starts from zero. Copy the default slices so they are not aliased. Tests follow web_test.go/git_test.go. Update the reference mcp-tasks.yaml and the Configuration docs. Validating invalid entries is out of scope: see task done-stats-config-errors.