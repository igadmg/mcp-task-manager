# Research request: done-stats-config

**Additional research needed: 15 / 100**

## Already covered by research.md
- `Config` and `WebConfig` structs, `DefaultConfig`, `Resolve` → `yaml.Unmarshal` → `applyDefaults` → `applyEnvOverrides` (`config.go:101-287`).
- yaml.v3 behaviour, checked by experiment:
  - a partial mapping keeps the defaults;
  - a sequence replaces the slice, and each element starts from zero;
  - `cards: []` decodes to a non-nil empty slice, `cards:` to nil;
  - unknown keys are ignored;
  - a type mismatch is a hard error.
- Import direction: `config` cannot use the `task` enums.
- Test patterns: `writeConfig`, `isolateEnv`, `resolveGit`, the partial-section tests, `TestDefaultBaseBranchesNotAliased`.
- How the config reaches the web layer: `resolved.Config` → `newBoardView`.

## Gaps (small)
1. How `NewBacklog` builds the config (as a literal, without `applyDefaults`), and whether tests in other packages then get `cards == nil`. The default must not depend on whether `applyDefaults` was called. Decide where the default lives: in `DefaultConfig` or as a fallback on read.
2. Whether `DefaultConfig()` itself must copy the default card list, so tests cannot mutate it. The BaseBranches precedent says it should.
3. The CLI `version` command and any other place that prints or serializes the config: check that a new nested section does not break their output.
4. A stable card key for localStorage (the toggles subtask). Should it be part of the config (an explicit `id`), or derived from the index and the content? Decide here, because the key lives in the config shape.
