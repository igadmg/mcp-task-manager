# Plan: done-stats-config

### Planning Summary

This plan implements the approved `design.md` in two phases:
- **P1**: the config types, defaults, normalizer, accessor and their tests.
- **P2**: the reference `mcp-tasks.yaml` and the docs.

Git branching is on:
- `start_phase implementation` cuts the parent's wip (`done-column-stats`) and then this subtask's wip.
- Each phase is a checkpoint on the wip branch.
- `complete_task` squash-merges the subtask as one commit onto the parent's wip.

The project has no code generation.

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| P1 | `web.done_stats.cards`: types, defaults, normalizer, accessor | `internal/config/stats.go`, `config.go`, `stats_test.go` | none | design approved | `go test ./internal/config -race`, `go test ./...`, `go vet`, `gofmt -l` |
| P2 | Reference config and docs | `mcp-tasks.yaml`, `CLAUDE.md`, `README.md` | none | P1 | `go test ./...` (this repo's config still resolves), `version` runs, docs match the code |

### Phase Details

**P1**
- `Phase ID:` P1
- `Goal:` `Config.Web.DoneStats.Cards` exists, defaults correctly, and is read through `(*Config).DoneStatsCards()`.
- `Why this phase exists:` this is the whole behavioural change. Later subtasks depend only on this API.
- `Files likely to change:` `internal/config/stats.go` (new), `internal/config/config.go`, `internal/config/stats_test.go` (new).
- `Regeneration required:` no.
- `Dependencies:` none.
- `Implementation tasks:`
  1. Add to `stats.go`:
     - `DoneStatsConfig` and `StatsCard` with yaml tags, as in the design;
     - the constants: the kinds, the four line names and `DefaultStatsDays`;
     - `DefaultStatsCards()`;
     - `normalizeStatsCards`, with helpers for trimming, kind inference, the per-kind defaults, title humanizing, slugging and unique IDs;
     - `(*Config).DoneStatsCards()`.
  2. In `config.go`:
     - add `DoneStats DoneStatsConfig \`yaml:"done_stats"\`` to `WebConfig`;
     - make `DefaultConfig` set `Cards: DefaultStatsCards()`;
     - make `applyDefaults` assign the normalized list;
     - extend the `applyDefaults` comment.
  3. Add the tests from the design's Testing Strategy, using a `resolveDoneStats` helper in the style of `resolveGit`.
- `Tests and checks:`
  - `go test -race ./internal/config`;
  - `go test ./...` (backlogs now carry default cards; nothing should consume them yet);
  - `go vet ./...` and `gofmt -l .`;
  - LSP or `go build` diagnostics clean.
- `Commit boundary:` the config code and its tests.
- `Definition of done:`
  - all new tests pass;
  - the full suite is green;
  - `DefaultStatsCards()` IDs are `bars-priority`, `bars-type`, `bars-resolution` and `lines-14d`.
- `Rollback note:` purely additive. Reverting the three files removes it, and no consumer references it yet.

**P2**
- `Phase ID:` P2
- `Goal:` the reference config and the docs describe the section.
- `Why this phase exists:` the task requires the docs, and keeping prose apart from logic keeps the P1 diff readable.
- `Files likely to change:` `mcp-tasks.yaml`, `CLAUDE.md` (the Configuration block and partial-section paragraph, the Validation "Config:" line, Project Structure), `README.md` (Config File).
- `Regeneration required:` no.
- `Dependencies:` P1.
- `Implementation tasks:`
  1. In `mcp-tasks.yaml`, write out `web.done_stats.cards` equal to the defaults, plus a commented split-card example.
  2. In CLAUDE.md:
     - add the `done_stats` cards example to the YAML block;
     - add a paragraph covering nil versus `[]`, the per-card defaults, derived IDs and titles, and the pointer to `done-stats-config-errors`;
     - update the Validation line;
     - add `stats.go` and `stats_test.go` to Project Structure.
  3. In README Config File, add the same YAML and a short paragraph.
- `Tests and checks:`
  - `go test ./...`;
  - `go run ./cmd/mcp-task-manager version`, which resolves this repo's config and must show no parse error;
  - a short test-free check that the repo config decodes to the defaults: a `go run` snippet in the scratchpad, or reasoning from the P1 tests.
- `Commit boundary:` the docs and the reference config.
- `Definition of done:` the docs name every key and default exactly as the code does.
- `Rollback note:` docs only. The yaml change is equivalent to the defaults, so reverting it changes no behaviour.

### Cross-Phase Risks

- **The repo's own config:** a YAML mistake in `mcp-tasks.yaml` would break every MCP tool in this repo through a `Resolve` error. P2 runs `version` and the suite to catch it.
- **Hidden consumers:** test backlogs now carry four default cards. No code reads them yet, so no test changes are expected; any failure points to unexpected coupling.
- **Doc drift:** the docs must not promise validation that only the errors task will deliver.

### Execution Order Rationale

P1 first, because the docs describe behaviour that has to exist and be tested. P2 is small and could be folded into P1, but kept separate it keeps the code checkpoint reviewable. Nothing runs in parallel: the phases are sequential and small.
