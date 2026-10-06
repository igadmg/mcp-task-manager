# Plan: done-stats-service

The plan follows `design.md`. There is one implementation phase: the subtask is squash-merged as one commit, so the steps are review checkpoints, not separate commits.

### P1: clock option
- `internal/task/service.go`: add the `now func() time.Time` field to `Service`, documented as write-once next to git/identity/current/phases. `NewService` sets `now: time.Now` before it applies the options.
- `internal/task/git.go`: add `WithClock(now func() time.Time) ServiceOption`. A nil `now` keeps the default.
- **Verify:** `go build ./...`.

### P2: stats computation
- `internal/task/stats.go`:
  - the types `StatsCard`, `StatsBar` and `StatsLine`;
  - `computeStats`, `statsBars`, `statsLines`, `statsValue`, `statsOrder`, `closeTime`, `dayWindow` and `runningSum`.
- `internal/task/view.go`:
  - add the `BoardSnapshot.Stats` field;
  - in `boardSnapshot`, `now := s.now()`, then `TakenAt: now.UTC()` and `Stats: computeStats(all, s.config.DoneStatsCards(), s.validTypes, now)`.
- **Verify:** `go build ./...`, `go vet ./internal/task`.

### P3: tests
- `internal/task/stats_test.go`: the counting-rule tables from the design's Testing Strategy, including the Europe/Berlin DST and month-boundary case (`_ "time/tzdata"`).
- `internal/task/view_test.go`:
  - a snapshot test with `WithClock` and the default cards: 4 cards, priority bars and `TakenAt`;
  - a test that the default clock is set.
- **Verify:**
  - `go test ./internal/task`;
  - `go test -race -run 'TestServiceRace|TestServiceNoSelfDeadlock|TestBoard|Stats' ./internal/task`.

### P4: docs
- `CLAUDE.md`:
  - a Web UI bullet "Done statistics", covering the counting rules, the clock and `BoardSnapshot.Stats`;
  - Project Structure: `stats.go`;
  - Concurrency: `now` is write-once.
- **Verify:** reread the edited sections.

### Quality gate
- `gofmt -l .` is empty.
- `go vet ./...` is clean.
- `go test ./...` passes.
- `go test -race ./internal/task ./internal/web` passes.

### Rollback
- Revert the files. No data format, config or on-disk change is involved.
