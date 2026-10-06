# Research: done-stats-service (gap closure)

The base research is `done-column-stats/research.md`. This file closes the five gaps in `research_request.md`. The config side has landed: `cf1bf77`, with `internal/config/stats.go` and `(*Config).DoneStatsCards()`.

### Confirmed Facts

**Config input (landed in done-stats-config)**
- `(*Config).DoneStatsCards()` is nil-safe and returns a normalized copy on every call (`internal/config/stats.go`).
  - Kind is `bars` or `lines`. Lines cards have `Days > 0`.
  - A card without `split_by` has non-empty `Lines`. A split card has `Metric`, which defaults to `closed`.
  - `ID` is unique and `Title` is filled.
- Nothing is validated. A blank or unknown field, an unknown line name and an unknown metric all reach the consumer as written. `done-stats-config-errors` owns reporting them.
- Line-name constants: `StatsLineCreated`, `StatsLineClosed`, `StatsLineCreatedCumulative`, `StatsLineClosedCumulative`.

**Gap 1: clock injection**
- `task.NewService` has two non-test callers: `project.Build` (`internal/project/resolver.go:161`) and `testsupport.NewBacklog` (`internal/testsupport/testsupport.go:35`).
- Test callers are in `internal/task/{concurrency,current,service,phaseflow,resolution}_test.go` and `internal/storage/storage_test.go`.
- `NewService` already takes variadic `ServiceOption`s (`WithGit`, `WithIdentity`, `WithCurrentTaskStore`, `WithPhaseStore`; `internal/task/git.go`).
  - A `WithClock` option therefore changes no signature and no caller.
- The existing stamping sites (`time.Now().UTC()` at `service.go:253,488,594,916,956,1077,1097`, `phaserecord.go`) are not needed for the stats.
  - Their tests do not need a clock.
  - Moving them is out of scope.

**Gap 2: time zone**
- `time.Now()` carries `time.Local`. Stored timestamps carry UTC or a fixed offset (`markdown.go:261-286`).
- If the injected clock's `Location()` is the bucketing location, production gets local time and tests can pin any zone (`time.FixedZone`, or `time.LoadLocation` with `_ "time/tzdata"` for DST) without touching `time.Local`.
- A calendar-day key is `t.In(loc).Date()`. Day starts are `time.Date(y, m, d-i, 0, 0, 0, 0, loc)`, which normalizes month and year rollover and DST.

**Gap 3: result shape and cost**
- `BoardSnapshot()` has two production callers, both in `internal/web/handlers.go` (`:60` board, `:88`). It already reads `index.All()` once under one lock.
- `Index.All()` → `syncIfStale()` → `isStaleOnDisk()` does one `os.ReadDir` plus one `os.Stat` per task directory on every call (`internal/storage/index.go:202-248`).
  - A separate stats method per poll doubles that I/O.
  - It also adds a second lock acquisition, so the bars and the column counts could disagree.
- Stats computation itself is O(tasks × cards) in memory with no file reads.

**Gap 4: unknown values**
- Loading does not validate type, priority or resolution (`markdown.go:221-245`). `Priority.Order()` returns 99 for unknown values.
- `created_by` is free text and can be empty. `status` has three known values and no list helper.

**Gap 5: cumulative baseline**
- Auto-archive removes done tasks from the index:
  - delivered tasks after `after_days`;
  - non-delivered resolutions on the next pass (`service.go:1093-1118`).
- A baseline counted from tasks before the window would therefore drop as tasks are archived, and would differ between projects with and without auto-archive.

**Tests**
- `newConcurrentService()` builds a config with no `Web` section, so `DoneStatsCards()` returns the defaults and `TestServiceRace` would exercise the stats path through `BoardSnapshot`.
- `mockIndex.All()` returns tasks in map order. Mock tasks can be put straight into `ms.tasks` / the index with any timestamps.
- `BoardSnapshot` is already registered in `TestServiceNoSelfDeadlock` (twice) and in `TestServiceRace`.

### Unknown
- None blocking.
