# Research request: done-stats-service

**Additional research needed: 25 / 100**

## Already covered by research.md
- `BoardSnapshot` / `boardSnapshot` and `index.All()`: the active index only, subtasks included, the archive never scanned.
- `IndexEntry` carries Status, Priority, Type, ParentID, CreatedAt, UpdatedAt, CreatedBy, Resolution and ClosedAt.
- When `closed_at` and `updated_at` are set and cleared, including the legacy fallback and the paths that bypass `update()`.
- Ordering: `Priority.Order()`, `cfg.TaskTypes`, `Resolutions()`, `EffectiveResolution()`.
- Twin discipline: manual registration in `TestServiceNoSelfDeadlock` and `TestServiceRace`.
- The service has no clock; it calls `time.Now().UTC()` in many places.
- Mocks: `newConcurrentService`, `mockIndex.All()` in map order.

## Gaps
1. **Clock injection.** List every caller of `NewService` (`project.Build`, testsupport, tests) and choose how to add `now` without breaking signatures. Options: a `ServiceOptions` field (`task/git.go` already has `ServiceOptions`) or a setter for tests. Decide whether existing stamping sites (create/update) should also move to the injected clock, or only the stats.
2. **Time zone for day buckets.** "Local time" in tests is not deterministic. Check how to pin the location: a `loc` parameter or option next to `now`, rather than relying on `time.Local`. Account for DST: a day is not always 24 hours, so bucket with `time.Date(y, m, d, 0, 0, 0, 0, loc)`, not by subtracting 24h.
3. **Result shape.** Should the stats extend `BoardSnapshot`, which already holds everything under one lock and one `All()` read, or live in a separate method that pays a second `All()` per poll? Measure or estimate the cost of `syncIfStale` for a double read.
4. **Unknown values.** Decide where values outside the enums go (a type not in `task_types`, an unknown priority, an empty `created_by`): at the end, in alphabetical order, or in an "other" group.
5. **The cumulative baseline.** Should the series start at 0 or count earlier tasks, given that auto-archive removes old done tasks?
