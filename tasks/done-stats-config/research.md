# Research: done-stats-config (gap closure)

The broad research is in `done-column-stats/research.md`. This file closes the four gaps listed in `research_request.md`.

### Confirmed Facts

**1. How configs are built outside `Resolve`**
- `testsupport.NewBacklog` (`internal/testsupport/testsupport.go:27-34`) and `NewGitBacklog` (`internal/testsupport/gitbacklog.go:94-105`) build `config.Config` literals and copy `Web: config.DefaultConfig().Web`. Neither calls `applyDefaults`.
  - A default card list placed in `DefaultConfig().Web` therefore reaches every web, app and tools test backlog.
- Many task, storage and web tests build bare literals without `Web` (`internal/task/service_test.go:961…1755`, `concurrency_test.go:19`, `phaseflow_test.go:31`, `resolution_test.go:249`, `storage_test.go:592,891,1025`, `web/view_test.go:334`). Web view tests pass a **nil** `*config.Config` to `newBoardView` (`web/view_test.go:25-47`).
  - In those, `Web.DoneStats.Cards` is nil. The only way such a config can still show the default cards is a read-side fallback that treats nil as "absent".

**2. yaml.v3 decoding into a pre-filled list** (scratch experiment against v3.0.1; the struct is pre-filled with two cards before `Unmarshal`)

| YAML | Result |
|------|--------|
| no file, `web:`, `web: {done_stats: }` (null), `done_stats: {}` | pre-filled list kept (nil = false, len 2) |
| `cards:` (null) | **reset to nil** |
| `cards: []` | non-nil, len 0 |

- So with defaults in `DefaultConfig`, `cards:` (null) still arrives as nil, and `applyDefaults` must refill nil.
- `cards: []` survives as a non-nil empty slice, which distinguishes "no cards" from "absent".

**3. Printing and serialization of the config**
- Nothing marshals `config.Config`. `Config` has only `yaml` tags and no `MarshalYAML`. The single `json.MarshalIndent` in `internal/tools/management.go:405` marshals tool results.
- `mcp-task-manager version` prints the version and `cfg.Resolution.Explain()` only (`internal/cli/cli.go:238-243`).
- `serve web` reads only `cfg.Web` to build the controller (`internal/cli/commands.go:609`).
- A new nested section therefore changes no existing output.

**4. Card key for localStorage**
- `done-stats-toggles` keys the state "by card + line so a config change does not break it", and `done-stats-lines` puts the card key in data attributes.
- Possible keys:
  - **List index**: breaks when cards are reordered.
  - **Title**: breaks when a title is edited.
  - **Content-derived** (kind + field, days, split_by, metric): stable across reordering and title edits; breaks only when the card's own definition changes.
  - **Explicit `id`**: stable across any change.
- Two cards can share a content-derived key, for example two identical `lines` cards. Keys therefore have to be unique in the list.
- Line keys are line names (`created`, …) or split values, which are already stable. They are not part of the config shape.

### Inference
- The default should live in `DefaultConfig` (so test backlogs and `Resolve` get it) **and** in a nil-safe read accessor (so bare literals and a nil config get it). Both build from one constructor that returns fresh slices.
- Non-positive `days` is indistinguishable from a missing key after decoding (both decode to 0 when written as `0`). Treating `<= 0` as missing matches `AutoArchive.AfterDays` (`config.go:251-253`).

### Unknown
- None blocking for this subtask. Which fields are supported, and validation of unknown names, belong to `done-stats-service` and `done-stats-config-errors`.
