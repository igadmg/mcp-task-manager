# Implementation: done-stats-service

### Phase Summary
P1 to P4 of `plan.md` are implemented on `igor.cwer/wip/done-column-stats--done-stats-service`. The work is not committed yet; `complete_task` squash-merges it onto the parent's wip.

### Lead Checklist
- [x] Stats are computed in `boardSnapshot` from the same `All()` read and under the same lock. No new exported Service method, so the twin discipline is unchanged.
- [x] The existing `BoardSnapshot` entries in `TestServiceRace` and `TestServiceNoSelfDeadlock` now run the stats path, because the mock config gives the default cards.
- [x] `WithClock` option, defaulting to `time.Now`; its `Location()` sets the day zone. Other stamping sites are untouched.
- [x] Cumulative lines start at 0. Unknown values come after the known ones, alphabetically. Empty values are dropped.
- [x] Invalid cards give empty data, never an error. Reporting them belongs to `done-stats-config-errors`.

### Coder Report
- **`internal/task/stats.go`** (new):
  - the types `StatsCard`, `StatsBar` and `StatsLine`;
  - `computeStats`, `statsBars` and `statsLines`;
  - the helpers `statsMetric`, `metricTime`, `closeTime`, `dayWindow` (civil-date buckets), `runningSum`, `statsValue` and a generic `statsOrder`.
- **`internal/task/view.go`:** the `BoardSnapshot.Stats` field. `boardSnapshot` reads `s.now()` once for both `TakenAt` (UTC) and `Stats`.
- **`internal/task/service.go`:** the write-once `now` field, defaulting to `time.Now` in `NewService`.
- **`internal/task/git.go`:** `WithClock`; a nil function keeps the default.
- **`CLAUDE.md`:** a "Done statistics (data)" Web UI bullet, `stats.go` in Project Structure, and the clock in the Concurrency write-once note.

### Tester Report
- `gofmt -l .`: clean.
- `go vet ./...`: clean.
- `go test ./...`: all packages pass.
- `go test -race` on the stats, `TestServiceRace`, `TestServiceNoSelfDeadlock` and `TestBoard*` in `internal/task`, and on `./internal/web`: pass.
- New tests in `internal/task/stats_test.go`:
  - status split and subtasks;
  - value order for priority, type, status and created_by, with unknown values;
  - resolution counting done tasks only, with the legacy default;
  - the 24 h window: edge, future, `updated_at` fallback, `closed_at` precedence and open tasks;
  - unknown field, kind and metric;
  - local midnights and month rollover;
  - Europe/Berlin across the DST end;
  - created, closed and cumulative series, with unknown and duplicate names skipped and `Hidden`;
  - split, cumulative split and resolution split;
  - card order from config;
  - snapshot-level checks on the clock and zone;
  - `WithClock(nil)`.

### Deviations From Plan
- The snapshot-level tests live in `stats_test.go`, not `view_test.go`, to keep the stats tests together.

### Next Phase Handoff
`done-stats-bars` and `done-stats-lines` read `snap.Stats`:
- bars cards have `Bars` with `ClosedRecently`;
- lines cards have `Days` (local midnights) and `Lines` (`Key`, `Values`, `Hidden`);
- `Field` and `Metric` name a split card's legend.
