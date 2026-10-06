# Implementation: done-stats-config

### Phase Summary
Both phases of `plan.md` are implemented on `igor.cwer/wip/done-column-stats--done-stats-config`, which is not committed yet.
- **P1:** `web.done_stats.cards` types, defaults, normalizer and accessor, with their tests.
- **P2:** the reference `mcp-tasks.yaml`, CLAUDE.md and README.

### Lead Checklist
- [x] Design followed: defaults in `DefaultConfig`, `normalizeStatsCards` in `applyDefaults`, nil-safe copying accessor `(*Config).DoneStatsCards()`, derived unique IDs.
- [x] No validation or rejection of cards (left to `done-stats-config-errors`).
- [x] `config` still imports only the standard library and yaml.

### Coder Report
- **`internal/config/stats.go`** (new):
  - `DoneStatsConfig` and `StatsCard`;
  - the kind and line-name constants and `DefaultStatsDays`;
  - `DefaultStatsCards()`, which builds a fresh list on every call;
  - `normalizeStatsCards`, which is pure and idempotent, never writes its input, and makes IDs unique in list order;
  - `(*Config).DoneStatsCards()`;
  - the helpers `trimList`, `humanize` and `slug`.
- **`internal/config/config.go`:**
  - the `WebConfig.DoneStats` field;
  - `DefaultConfig` fills the default cards;
  - `applyDefaults` normalizes the cards, which also refills nil after `cards:` (null).

### Reviewer Findings
No findings.
- I checked: the normalizer copies `Lines` and `Hidden`, so no input aliasing; dedup stays idempotent after `-N` suffixes; no recursion between `DefaultStatsCards` and `normalizeStatsCards` (nil only at the top level).
- A bars card with a blank field gets ID `bars` and an empty title. This is deliberate: invalid cards are for the errors task.
- The pre-existing gopls hints in `config.go` (QF1002, slicescontains) are on lines this task did not touch.

### Tester Report
- `gofmt -l .`: clean.
- `go vet ./...`: clean.
- `go test -race ./internal/config`: pass.
- `go test ./...`: all packages pass.
- `go run ./cmd/mcp-task-manager version`: resolves this repo; no parse error.
- A throwaway test, deleted afterwards, showed that this repo's `mcp-tasks.yaml` decodes to exactly `DefaultStatsCards()`.

### Quality Gate Result
PASS

### Files Changed
- `internal/config/stats.go` (new)
- `internal/config/stats_test.go` (new)
- `internal/config/config.go`
- `mcp-tasks.yaml`
- `CLAUDE.md`
- `README.md`

### Deviations From Plan
None. P1 and P2 are not separate wip commits: the subtask is squash-merged as one commit on completion anyway.

### Next Phase Handoff
`done-stats-service` reads `s.config.DoneStatsCards()`. That gives:
- `Kind`, `Field`, `Days`, `Lines`, `Hidden`, `SplitBy`, `Metric`, already defaulted;
- `ID` for data attributes and localStorage keys;
- `Title` for display.
