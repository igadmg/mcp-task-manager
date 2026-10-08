# Plan — workspace-tiler

Phase: planning, run 1. Plans `design.md` as approved, including its ADR (no
server-set `translateX` step; `/tasks/{id}` is the depth-0 chain). No
redesign here.

### Planning Summary

Seven phases, six of them a commit. The order front-loads the two pieces that
carry no risk to the running UI (a standalone defect fix, then a pure value
type with unit tests), then migrates `/tasks/{id}` on its own so the existing
detail tests act as the regression gate, then adds the chain routes and the
shell, then regenerates the one generated asset in isolation, then documents.
The last phase is the manual browser pass that `clarifications.md` made part
of acceptance; it produces no commit.

The regeneration rule matters here: `internal/web/static/app.css` is compiled
output. It gets a phase of its own (T5) so the generated diff never hides a
logic change, and T4's templates are written to be verifiable by markup
assertions while still unstyled.

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| T1 | Fix the shipped `#`/`?` href defect by computing hrefs in Go | `view.go`, `_detail.html`, `view_test.go`, `handlers_test.go` | no | — | new test: a file named `a#b.md` links to `a%23b.md` and resolves |
| T2 | The chain value and the kind registry | `chain.go`, `kinds.go`, `chain_test.go` | no | — | `chain_test.go` tables; suite still green (nothing wired) |
| T3 | `/tasks/{id}` becomes board + panel; standalone page removed | `handlers.go`, `view.go`, `board.html`, `templates.go`, delete `detail.html` | no | — | existing detail tests pass **unchanged** + new board+panel test |
| T4 | Chain routes, rail, stage/strip shell, stub columns | `server.go`, `handlers.go`, `view.go`, `_workspace.html`, `_col_*.html`, `board.html` | no | T2, T3 | new route tests; four existing path lists extended; `-race` |
| T5 | CSS for stage/strip/kinds/rail/reduced motion + regenerate | `input.css`, `static/app.css`, `assets_test.go` | **full** | T4 | `assets_test.go` finds every new class and the media block |
| T6 | Documentation | `CLAUDE.md`, `README.md` | no | T4, T5 | prose matches the shipped routes and classes |
| T7 | Manual browser pass (no commit) | — | no | T5 | the checklist in `design.md` → Testing Strategy |

### Phase Details

- `Phase ID:` **T1 — href escaping computed in Go**
- `Goal:` Every link to an attached file carries a correctly escaped path, so
  a file named `a#b.md` or `a?b.md` is reachable.
- `Why this phase exists:` It is a defect in already-shipped code
  (`8b16e42`), it is independent of everything else in this subtask, and the
  chain needs the same Go-side escaping anyway. Landing it first keeps the
  fix reviewable on its own instead of buried in the tiler diff.
- `Files likely to change:` `internal/web/view.go` (a `FileLinkView{Name, Href}`;
  `DetailView.Files` becomes `[]FileLinkView`, filled with `url.PathEscape`),
  `internal/web/templates/_detail.html:122`,
  `internal/web/view_test.go` (assertions touching `Files`),
  `internal/web/handlers_test.go` (new test).
- `Regeneration required:` No.
- `Dependencies:` None.
- `Implementation tasks:` add the link view; escape in the view layer, not the
  template; keep the plain-strings doctrine (`internal/web/view.go:14-17`).
- `Tests and checks:` new handler test for `#` and `?` in a file name, both
  that the href is escaped and that `GET` of that href returns the content;
  `go vet`; `go test ./internal/web/`.
- `Commit boundary:` one commit, `fix(web): escape attached-file hrefs in Go`.
- `Definition of done:` the new test fails before the change and passes after;
  every other `internal/web` test unchanged and green.
- `Rollback note:` revert the commit; the only external effect is the href
  string in one template.

---

- `Phase ID:` **T2 — chain value and kind registry**
- `Goal:` A parsed, validated, round-trippable chain with a registry that
  makes a new column kind one table entry.
- `Why this phase exists:` It is the load-bearing abstraction and it is pure:
  no routes, no templates, no behaviour change. It can be proved by unit
  tests alone, which is where the tricky cases (`%2F`, `%2E%2E`, `id == "w"`,
  depth cap) belong.
- `Files likely to change:` `internal/web/chain.go` (new),
  `internal/web/kinds.go` (new), `internal/web/chain_test.go` (new).
- `Regeneration required:` No.
- `Dependencies:` None (`task.ValidateNameSegment` already exists,
  `internal/task/name.go:17-28`).
- `Implementation tasks:` `ColumnKind`, `Column`, `Chain`; `ParseChain` over
  `EscapedPath()` with per-segment `PathUnescape`, segment-index slicing (not
  a `"/w/"` search), even-pair check, registry lookup, `maxChainDepth = 16`;
  `Path`, `Fragment`, `Append`, `TruncateTo`, `TaskAt`; the registry table
  with tag, width class, rail label, template name and a self-contained
  `resolve` that takes the resolved project plus the owning task id and ref.
- `Tests and checks:` the full table from `design.md` → Testing Strategy, new
  file; `go vet`; `go test ./internal/web/`.
- `Commit boundary:` one commit, `feat(web): column chain value and kind registry`.
- `Definition of done:` chain tests green; the rest of the suite untouched and
  green because nothing calls the new code yet.
- `Rollback note:` delete three new files; nothing else references them.

---

- `Phase ID:` **T3 — `/tasks/{id}` is board + panel**
- `Goal:` The deep-linkable task URL renders the board with that task's panel
  filled, and the standalone detail page stops existing.
- `Why this phase exists:` It is the one migration with existing tests
  guarding it. Isolating it means those tests — `TestDetailPage`,
  `TestDetailArchivedTask`, `TestDetailUnknownID404`,
  `TestBoardShowsPhaseFromRecords`, `TestPhaseNoteEscaped`,
  `TestBoardShowsBranchChip`, `TestCopyButtonHasNoHtmxAttributes` — are a
  clean pass/fail signal on exactly this change, as `research.md` predicted
  they would be. Mixing it into T4 would make a failure ambiguous.
- `Files likely to change:` `internal/web/handlers.go` (`detail` renders the
  board page with the panel filled, keeps its 404s),
  `internal/web/view.go` (`BoardView` gains `Panel *DetailView` and `Title`),
  `internal/web/templates/board.html` (panel pre-filled; `{{ define "title" }}`),
  `internal/web/templates.go` (`detailPage` removed),
  `internal/web/templates/detail.html` (deleted).
- `Regeneration required:` No.
- `Dependencies:` None, but ordered after T2 so the chain exists when T4
  arrives.
- `Implementation tasks:` fill the panel from `detailView`; keep
  `Resolver.Current()` only; keep 404 on unknown id and on unresolved; title
  `#id Title`, else `Task board`.
- `Tests and checks:` **the seven tests above must pass with no edits**; new
  test that `/tasks/{id}` contains both a board column heading and the panel
  title; `go test ./internal/web/`; `go vet`.
- `Commit boundary:` one commit, `refactor(web): /tasks/{id} renders the board with its panel open`.
- `Definition of done:` no test file edited except the one new test added; the
  `detailPage` template set and `detail.html` are gone.
- `Rollback note:` restoring `detail.html` and the template set restores the
  old surface; `_detail.html` is untouched by this phase, so the panel is
  unaffected either way.

---

- `Phase ID:` **T4 — chain routes, rail and strip shell**
- `Goal:` Every chain state is served as a page and as a fragment, with the
  rail, the stage, the strip and stub column bodies.
- `Why this phase exists:` This is the subtask's substance. It is one phase
  because a route without a template cannot be verified and a template
  without a route cannot be reached.
- `Files likely to change:` `internal/web/server.go` (two patterns:
  `GET /tasks/{id}/w/{rest...}`, `GET /strip/{rest...}` — one per family, two
  in a family panics), `internal/web/handlers.go` (`workspace`,
  `workspaceFragment`, `workspaceView`), `internal/web/view.go`
  (`WorkspaceView`, `RailEntryView`, `ColumnUnitView`),
  `internal/web/templates/_workspace.html`, `_col_task.html`, `_col_file.html`
  (new), `internal/web/templates/board.html` (rail/stage/strip shell),
  `internal/web/handlers_test.go`, `internal/web/race_test.go`.
- `Regeneration required:` No. The new classes have no CSS until T5, by
  design: the markup is what T4's tests assert.
- `Dependencies:` T2 (the chain), T3 (the board page is the one page template).
- `Implementation tasks:` parse, resolve per column carrying `TaskAt`, build
  the rail from `TruncateTo(i)`, render; 404 on a malformed chain, an unknown
  kind, an over-deep chain, a missing task, a missing file and an unresolved
  project; stub bodies only (`clarifications.md`).
- `Tests and checks:` the new route tests from `design.md`; extend
  `TestNoMutatingRoutes`' path list, `TestNoExternalAssetReferences`' paths,
  `TestEscaping`'s paths, `TestUnresolvedProjectPlaceholder` (chain → 404),
  and `race_test.go`'s fetch list; `go test -race ./internal/web/`;
  `go vet ./...`.
- `Commit boundary:` one commit, `feat(web): task workspace — column chain routes, rail and strip`.
- `Definition of done:` every state deep-links and reloads to the same
  layout; no non-GET pattern exists; the GET sweep still leaves the tasks
  directory byte-identical; `-race` clean.
- `Rollback note:` the two patterns are the only entry points — removing them
  from `NewHandler` disables the feature without touching the board.

---

- `Phase ID:` **T5 — CSS and the one regeneration**
- `Goal:` The shell is styled, the strip cannot be scrolled, nothing slides
  under `prefers-reduced-motion`, and below `lg` everything stacks.
- `Why this phase exists:` `static/app.css` is compiled output. The planning
  rule is explicit: a regeneration is its own verifiable step so the
  generated diff cannot hide logic. It is also the phase whose correctness a
  test can only partly state, which is why T7 follows.
- `Files likely to change:` `internal/web/assets/input.css`,
  `internal/web/static/app.css` (regenerated),
  `internal/web/assets_test.go`.
- `Regeneration required:` **Full** — `scripts/build-css.sh` has only one
  mode. The pinned CLI is cached at `.cache/tailwindcss-macos-arm64-v4.3.3`,
  so the step is offline.
- `Dependencies:` T4 (Tailwind scans `../templates`, so the templates must
  exist first or utility classes used only there are dropped).
- `Implementation tasks:` `.stage` (`overflow-x: clip`, `overflow-y: visible`
  — **not** `hidden`, which would break `#panel`'s sticky), `.strip`
  (`flex`, `justify-content: flex-end`), `.kind-task` / `.kind-file` widths,
  `.rail`, the working-column entrance animation, the first
  `@media (prefers-reduced-motion: reduce)` block, and the below-`lg` stack;
  then run the script and commit its output.
- `Tests and checks:` `assets_test.go` asserts each new class and the
  reduced-motion block in the compiled file, in the style of
  `TestAppCSSDefinesLaneClasses`; `go test ./internal/web/`.
- `Commit boundary:` one commit containing `input.css`, the regenerated
  `app.css` and the assertions — and nothing else.
- `Definition of done:` the compiled file contains every asserted class;
  `app.css` is the script's output, not a hand edit.
- `Rollback note:` re-run the script against the previous `input.css`; the
  committed `app.css` is reproducible from it.

---

- `Phase ID:` **T6 — documentation**
- `Goal:` `CLAUDE.md` and `README.md` describe what shipped.
- `Why this phase exists:` Both files document the web UI in detail
  (`CLAUDE.md` → Web UI; `README.md` → Web Dashboard), and this subtask
  changes the route surface. Keeping it out of T4 keeps the code diff small.
- `Files likely to change:` `CLAUDE.md` (Web UI: the workspace, the chain URL
  scheme, the route family, the repurposed `/tasks/{id}`, the stage/strip
  geometry rule and why there is no server-set step; Project Structure gains
  `chain.go`, `kinds.go`, `_workspace.html`, `_col_*.html` and loses
  `detail.html`), `README.md` (Web Dashboard paragraph).
- `Regeneration required:` No.
- `Dependencies:` T4, T5 — document what exists, not what was planned.
- `Implementation tasks:` prose only; state the ADR's geometry decision in one
  sentence so the next reader does not re-litigate it.
- `Tests and checks:` none automated; re-read the route list against
  `server.go`.
- `Commit boundary:` one commit, `docs: task workspace routes and strip geometry`.
- `Definition of done:` no route or class named in prose that does not exist.
- `Rollback note:` prose only.

---

- `Phase ID:` **T7 — manual browser pass**
- `Goal:` Close the one gap no test in this repository can close.
- `Why this phase exists:` `clarifications.md` made it acceptance. Every
  automated assertion here is over response bytes or compiled CSS text; the
  ADR's layout decision is verified only against the spec.
- `Files likely to change:` none.
- `Regeneration required:` No.
- `Dependencies:` T5.
- `Implementation tasks:` via the `open_board` skill — rebuild, open, walk a
  chain to depth 4+; check at desktop width and below `lg`: no horizontal
  scrollbar at any state, wheel and trackpad cannot pan the strip, the rail
  stays pinned as columns leave, `#panel` is still sticky, browser Back steps
  the chain back, a deep-link reload reproduces the layout.
- `Tests and checks:` the checklist above, recorded in the implementation
  report.
- `Commit boundary:` none. If the pass finds a fault, the fix is a new commit
  attributed to whichever phase owns it.
- `Definition of done:` the checklist is recorded with its result. A failure
  of the animation judgement triggers the ADR's named fallback rather than an
  ad-hoc fix.
- `Rollback note:` n/a.

### Cross-Phase Risks

- **T3 is the phase most likely to surprise.** `research.md` checked all seven
  guarding tests by reading templates, not by running the migration. If one
  fails, the rule is: the migration is wrong, not the test — in particular the
  two negative assertions (`">Git<"` on `/tasks/1`, a copy button on
  `/tasks/4`) are the canaries for "the board leaked into the panel's
  surface".
- **T4 before T5 leaves the UI visibly unstyled** between two commits. That is
  intentional (the regeneration must stand alone) but means neither commit
  should be judged on appearance in isolation.
- **T5 depends on template content existing.** Tailwind scans only
  `../templates` with automatic detection off
  (`internal/web/assets/input.css:13-15`), so running the script before T4
  would silently drop any utility used only in the new templates. Component
  classes declared in `input.css` are immune; utilities are not.
- **The service lock count grows with chain depth** (up to depth + 2
  acquisitions per render). Bounded by `maxChainDepth`, flagged in
  `design.md`, and owned by `workspace-live`. If T7 shows the board stuttering
  at depth, that is the signal to pull the composite read forward rather than
  to tune anything here.
- **Phase-to-commit mapping is a plan, not an instruction to push.** The work
  stays on the task's wip branch; `complete_task` snapshots and squashes it.
  No commit is made to any shared branch by this plan.

### Execution Order Rationale

T1 and T2 are both independent of everything else and carry no risk to the
running UI — T1 because it changes one href, T2 because nothing calls it yet.
They go first so that if the subtask is interrupted, two complete, useful
commits exist.

T3 goes before T4 because it is the only phase whose regression signal is
pre-existing tests; running it alone makes that signal unambiguous. It also
leaves exactly one page template, which is the precondition T4 builds the
shell into.

T4 must precede T5 because the Tailwind scanner reads the templates. T5 must
precede T6 so the docs describe shipped classes, and T7 last because it needs
the styled result.

T1, T2 and T3 touch disjoint files and could be done in parallel by separate
hands; T4 onward is a chain and cannot. Given one implementer, the sequence
above is also the shortest path to a demonstrable state (T4 is the first
phase where the feature is reachable in a browser at all).
