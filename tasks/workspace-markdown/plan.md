# Plan — workspace-markdown

Phase: planning, run 1. From `design.md` (run 1). No redesign here; every
decision below is already in the ADR.

### Planning Summary

Six phases, each one commit, each independently testable. The spine is
bottom-up so that every phase ships with its own passing tests and nothing is
stubbed: the escaper and the URL filter first (they are the acceptance
criteria's hard core and have no dependencies), then the inline pass, then
blocks, then containers and tables, then the corpus and fuzz gates.

Phase 1 is the only one that creates the package; phases 2–5 each add one
source file or one block family plus its table test. Phase 6 adds no
production code at all — it is the invariant gate, and keeping it separate
means a corpus failure is bisectable to the construct that caused it.

No phase touches `go.mod`, `go.sum`, `internal/web`, or any existing file.
That is what makes the whole plan safe to land before any UI exists, as the
task description promises.

This project has no code generation, so no phase requires regeneration.

### Phase Table

| Phase ID | Goal | Main files | Regen | Depends on | Verification |
| -------- | ---- | ---------- | ----- | ---------- | ------------ |
| P1 | Package skeleton, `Render` on paragraphs only, the one escaper | `internal/markdown/markdown.go`, `inline.go`, `markdown_test.go`, `escape_test.go` | no | — | `go test ./internal/markdown`; `git diff --exit-code go.mod go.sum` |
| P2 | `safeURL`: the five-step accept rule | `internal/markdown/url.go`, `url_test.go` | no | P1 | the full accept/reject table passes |
| P3 | Inline pass: code spans, links, images, emphasis, hard breaks | `internal/markdown/inline.go`, `markdown_test.go`, `escape_test.go` | no | P1, P2 | per-construct tables; no `<a` for a rejected URL |
| P4 | Leaf blocks: headings, thematic breaks, fenced code | `internal/markdown/markdown.go`, `markdown_test.go` | no | P1, P3 | unterminated fence, info-string class, h1–h6 |
| P5 | Container blocks and tables: lists (nested, tight/loose, checkboxes), blockquotes, GFM tables | `internal/markdown/markdown.go`, `markdown_test.go` | no | P4 | nesting, depth cap, fence inside a list, table cell with code and a link |
| P6 | The gates: whole-corpus render, tag-whitelist invariant, `FuzzRender` | `internal/markdown/corpus_test.go`, `fuzz_test.go` | no | P5 | corpus renders; every `<` begins a whitelisted tag; 30 s fuzz clean |

### Phase Details

- `Phase ID:` **P1 — package skeleton and the escaping spine**
- `Goal:` `internal/markdown` exists, `Render` handles paragraphs and blank
  lines, and `writeText` is the only function in the package that copies
  source bytes into the output.
- `Why this phase exists:` The escaping property is the design's central
  claim. Establishing it on the smallest possible renderer means every later
  phase is added on top of a guarantee that already has tests, instead of
  racing to retrofit one.
- `Files likely to change:` `internal/markdown/markdown.go` (package doc,
  `Render`, `normalize`, `splitLines`, the block loop with two cases: blank
  and paragraph), `internal/markdown/inline.go` (`writeText` only, calling
  `html.EscapeString`; plain-text inline rendering with no delimiters yet),
  `internal/markdown/markdown_test.go`, `internal/markdown/escape_test.go`.
- `Regeneration required:` no.
- `Dependencies:` none.
- `Implementation tasks:`
  - package doc stating the one exported symbol, the no-unsafe-mode rule and
    the `writeText` invariant in words, so the next reader finds the contract
    at the top of the file
  - `Render(src string) string`: normalise `\r\n` and lone `\r` to `\n`,
    split into lines, drop the trailing empty line a final newline produces,
    `strings.Builder` with a `src`-proportional `Grow`
  - paragraph gathering: consecutive non-blank lines joined with `\n`, one
    `<p>` per run
  - `writeText(b *strings.Builder, s string)` — the single source-byte path
- `Tests and checks:` empty input → empty string; whitespace-only input;
  one paragraph; two paragraphs; CRLF input and LF input producing identical
  output; input with and without a trailing newline producing identical
  output; `<script>alert(1)</script>` in prose escaped; `"` and `'` escaped;
  all five characters in one paragraph.
- `Quality gates:` `go build ./...`, `go vet ./internal/markdown`,
  `go test ./internal/markdown`, `gofmt -l` clean,
  `git diff --exit-code go.mod go.sum`.
- `Definition of done:` the package compiles, paragraphs render, and no input
  of prose can produce a `<` in the output that the renderer did not write
  itself.
- `Rollback note:` delete the directory. Nothing imports it.

---

- `Phase ID:` **P2 — `safeURL`**
- `Goal:` One function deciding whether a URL may become an attribute value,
  with the research table as its test.
- `Why this phase exists:` It is self-contained, it is half the acceptance
  criteria, and it has no parser dependency — so it can be proved right
  before any code calls it. Reviewing it as its own commit also means the
  security argument is readable in one diff.
- `Files likely to change:` `internal/markdown/url.go`,
  `internal/markdown/url_test.go`.
- `Regeneration required:` no.
- `Dependencies:` P1 (the package must exist).
- `Implementation tasks:` implement the five steps from `design.md` in order
  — trim bytes `<= 0x20` and `0x7F`; reject any remaining byte `< 0x21` or
  `== 0x7F`; reject a `//` prefix; accept when the first `:` falls after the
  first `/`, `?` or `#` or is absent; otherwise require
  `[A-Za-z][A-Za-z0-9+.-]*` ASCII-lowercased to be `http`, `https` or
  `mailto`. Signature `safeURL(raw string) (string, bool)`, returning the
  trimmed URL so the caller never re-derives it. A comment on the function
  naming the entity case and why escaping the `&` is the other half of the
  defence.
- `Tests and checks:` one case per row of `research.md` §3 — the fourteen
  listed inputs plus an empty string, a lone `:`, `mailto:` with no address,
  `HTTPS://EXAMPLE.COM`, a scheme with a leading digit, and a URL whose
  colon sits inside a query string.
- `Quality gates:` as P1.
- `Definition of done:` every row of the research table is a passing test
  case and the function has no caller yet.
- `Rollback note:` delete both files; P1 does not reference them.

---

- `Phase ID:` **P3 — the inline pass**
- `Goal:` Code spans, links, images, `*`/`**` emphasis, backslash escapes and
  hard breaks, all routed through `writeText` and `safeURL`.
- `Why this phase exists:` Inline is where the corpus actually lives — 17 071
  code spans and 2 176 strongs against 4 links — and where the delimiter
  stack's bound has to be proved. It is also the only phase with a
  non-linearity worth measuring, so it gets its own commit.
- `Files likely to change:` `internal/markdown/inline.go`,
  `internal/markdown/markdown_test.go`,
  `internal/markdown/escape_test.go`.
- `Regeneration required:` no.
- `Dependencies:` P1, P2.
- `Implementation tasks:`
  - one left-to-right scan that advances at least one byte per iteration —
    state this as a comment and make it structurally true, since it is the
    termination argument
  - backtick runs: N backticks closed by the next run of exactly N, one
    leading and one trailing space stripped, body through `writeText`
  - `![alt](url)` and `[text](url)`: `safeURL` decides; a rejected URL emits
    the inline-rendered text (or escaped alt) and no tag
  - `*` delimiter runs with the open-offset splice from the design, stack
    capped at 64, surplus literal
  - backslash followed by ASCII punctuation → that character as literal text
  - no `_` handling at all
  - hard break: a paragraph line ending in two or more spaces emits `<br />`
- `Tests and checks:` inline code, code span containing a backtick (double
  delimiter), code span containing `<`, `&` and `|`; strong, em, nested
  strong-in-em; unmatched `*` literal; `**` spanning a code span boundary;
  `snake_case_word` in prose unchanged (the underscore decision, asserted);
  a link with accepted and with each rejected scheme; an image likewise;
  `<script>` inside link text and inside a link URL; a 100-deep `*` nest
  terminating; backslash-escaped `*`, `` ` `` and `[`; two-space hard break;
  a trailing-space line at the end of input.
- `Quality gates:` as P1, plus `go test -run Inline -count 2` to catch
  order-dependence between table cases.
- `Definition of done:` every inline construct in the design renders, and a
  rejected URL provably emits no `<a` or `<img`.
- `Rollback note:` revert the commit; P1's paragraph path is independent of
  the delimiter code and keeps working.

---

- `Phase ID:` **P4 — leaf blocks**
- `Goal:` ATX headings, thematic breaks and fenced code blocks.
- `Why this phase exists:` These three are the block cases with no recursion,
  so they close the leaf half of the block loop before containers complicate
  it. The fence's end-of-input behaviour is the termination property the task
  calls out by name, and it belongs in a commit a reviewer can read alone.
- `Files likely to change:` `internal/markdown/markdown.go`,
  `internal/markdown/markdown_test.go`.
- `Regeneration required:` no.
- `Dependencies:` P1, P3 (headings render inline content).
- `Implementation tasks:`
  - heading: `#{1,6}` followed by a space or end of line, trailing `#`s
    stripped, content through the inline pass
  - thematic break: 3+ of `-`, `*` or `_` and nothing else but spaces →
    `<hr />`, unconditionally, never a setext heading
  - fence: 3+ backticks or tildes, closed by a run of at least the same
    length of the same character, or by end of input; body emitted verbatim
    through the escaper, never through the inline pass; info string's first
    word becomes `class="language-<info>"` only when it matches
    `[A-Za-z0-9_+.-]+`
  - the dispatch order from the design, with the block loop's index
    advancing past every consumed line on every branch
- `Tests and checks:` h1 through h6; `#######` (seven) as a paragraph;
  `#NoSpace` as a paragraph; a heading with trailing `###`; `---`, `***`,
  `___` as breaks; `---` directly after a paragraph line still a break (the
  setext decision, asserted); a fenced block with `go`, with `mermaid`, with
  no info string, with an info string that would be an invalid class, with
  `<script>` in the body, with a `---` line in the body, with backticks in
  the body under a longer fence; an unterminated fence; a tilde fence; a
  fence whose body is empty.
- `Quality gates:` as P1.
- `Definition of done:` all three leaf blocks render, an unterminated fence
  terminates, and a ` ```mermaid ` block is an ordinary `<pre><code>`.
- `Rollback note:` revert; P1–P3 remain a working paragraph-and-inline
  renderer.

---

- `Phase ID:` **P5 — container blocks and tables**
- `Goal:` Blockquotes, bullet and ordered lists with nesting, tight versus
  loose items, GFM task-list items, and GFM tables.
- `Why this phase exists:` This is the only phase with recursion, so it is
  the only one where the depth cap and the shrinking-slice argument can be
  tested. Tables ride along because a table cell is the last place inline
  content appears and because the delimiter-row lookahead is part of the same
  dispatch decision.
- `Files likely to change:` `internal/markdown/markdown.go`,
  `internal/markdown/markdown_test.go`.
- `Regeneration required:` no.
- `Dependencies:` P4.
- `Implementation tasks:`
  - blockquote: take the run of `>`-prefixed lines, strip `>` and one
    following space, recurse with `depth+1`
  - list: gather the list, split into items at marker lines of the item's
    indent, dedent each item's lines by the marker width, recurse per item;
    looseness per the design, with a tight item's single paragraph emitted
    bare
  - task-list item: `[ ]` or `[x]`/`[X]` at the item's start →
    `<li class="task-list-item">` plus a `☐`/`☑` glyph, no `<input>`
  - `maxDepth = 16`: at the cap, render the remaining lines as one escaped
    paragraph rather than recursing
  - table: current line contains `|` and the next is a delimiter row →
    header, alignment classes from the delimiter row, body rows until a
    blank line or a line without `|`; ragged rows padded or truncated to the
    header's width; cell content through the inline pass
- `Tests and checks:` bullet list; ordered list with `.` and with `)`;
  two-level and three-level nesting; a nested ordered list inside a bullet
  item; tight list; loose list; a list item holding two paragraphs; a fence
  inside a list item; a blockquote inside a list item inside a blockquote; a
  20-level nest hitting the cap without panicking; task-list items checked
  and unchecked and mixed with plain items; a table with alignment on each
  column; a cell holding inline code; a cell holding a link; a cell holding
  a pipe inside a code span (documenting whichever behaviour the
  implementation lands on — GFM splits on it); a header-only table; a table
  with more and with fewer body cells than headers; a `|`-containing line
  whose successor is not a delimiter row rendering as a paragraph.
- `Quality gates:` as P1, plus `go test ./internal/markdown -race`.
- `Definition of done:` every construct in the design's subset renders, and
  no nesting depth can panic or hang.
- `Rollback note:` revert; P1–P4 remain a working leaf-block renderer, which
  is already useful to a consumer.

---

- `Phase ID:` **P6 — corpus and fuzz gates**
- `Goal:` The two tests that turn the design's escaping argument into a
  machine-checked invariant, over real data and over random data.
- `Why this phase exists:` It adds no production code, so a failure here is
  unambiguously a bug in P1–P5 and bisects straight to the construct that
  caused it. Mixing it into P5 would make a corpus failure look like a list
  bug by default.
- `Files likely to change:` `internal/markdown/corpus_test.go`,
  `internal/markdown/fuzz_test.go`, and — only if the corpus turns up a
  construct the subset mishandles — a targeted fix in the matching source
  file, called out in the commit message.
- `Regeneration required:` no.
- `Dependencies:` P5.
- `Implementation tasks:`
  - `corpus_test.go`: walk `../../tasks` for `*.md` and extensionless files,
    `t.Skip` cleanly when the directory is absent so a checkout without it
    still passes, render each one, and assert the tag-whitelist invariant —
    every `<` in the output begins one of the whitelisted tags or their
    closing forms, with the list written out literally rather than
    pattern-matched
  - one subtest rendering
    `tasks/web-task-workspace/research.md` and
    `tasks/web-ui-kanban-module/task.md` with the output length logged, as
    the spot check the design calls for; assertions stay on the invariants,
    not on exact bytes
  - `fuzz_test.go`: `FuzzRender` seeded from the corpus plus the
    pathological inputs — deep nesting, 1 000-level emphasis, a 1 MB single
    line, a fence opened inside a blockquote inside a list, a header-only
    table, mixed CRLF — asserting no panic and the same invariant
  - record in the commit message which out-of-subset constructs the corpus
    contains and therefore renders literally, closing the loop the task
    description opens ("note which, if any, are missing when the tests run
    over the corpus")
- `Tests and checks:` `go test ./internal/markdown -race`;
  `go test ./internal/markdown -run FuzzRender -fuzz FuzzRender -fuzztime 30s`
  clean; `go test ./... -race` for the whole tree.
- `Quality gates:` as P1, plus the full-tree test run and
  `git diff --exit-code go.mod go.sum` as the final zero-dependency gate.
- `Definition of done:` all 173 corpus artifacts render without panic, every
  `<` in every output is a tag this package wrote, and 30 seconds of fuzzing
  finds nothing.
- `Rollback note:` revert the test files; the renderer still works, but the
  task's acceptance criteria are not met, so this phase is not optional —
  it is the one that proves the rest.

### Cross-Phase Risks

- **P3's emphasis splice is the one place the design accepts a
  non-linearity.** If the 64-delimiter cap turns out to be reachable by real
  prose (it should not be — the corpus peak is nowhere near it), the symptom
  is literal `*` in the output, not a hang. P6's fuzzing is what would
  surface a pathological case; the fix would be local to `inline.go`.
- **P5's list dedenting is the most likely source of a corpus surprise**,
  because 29 of the corpus's 386 fence delimiters are indented inside list
  items. P6 is where that would show up, one phase after the code lands —
  accepted, because splitting P5 further would mean committing a list
  implementation that cannot render the corpus's actual lists.
- **The tag whitelist is duplicated** between the design's list, P6's test
  and the tags the writers emit. A new tag added in a later subtask without
  updating the test would fail loudly, which is the intended direction of
  the dependency.
- **The corpus is in the repo under test.** A future artifact that uses an
  out-of-subset construct makes P6's invariant test exercise new input
  without any code change. That is a feature — it is how the subset gets
  revisited — but it means the test must assert invariants, never exact
  bytes, or it becomes a tripwire on every new task note.
- **Nothing imports the package until `workspace-file-column` lands.** Dead
  code for a while, by design and stated in the task description. The risk
  is that the consumer discovers it needs a second entry point (frontmatter
  stripping, link rewriting — Open Questions 1 and 2); both are additive and
  neither invalidates `Render`.

### Execution Order Rationale

The order is strictly bottom-up, and the reason is the escaping guarantee
rather than convenience. P1 establishes "one function copies source bytes"
on a renderer small enough that the claim is obvious by inspection; P2 proves
the URL rule in isolation, before a parser can obscure it; P3–P5 then add
constructs onto a base whose invariant is already under test, each phase
adding only writers that route through the existing escaper. Had the order
been top-down — block structure first, escaping last — the final phase would
have had to audit every writer, which is exactly the review-habit guarantee
the ADR rejected.

P4 before P5 splits the block loop at its only structural seam: leaf blocks
need no recursion, containers do. That keeps the depth cap and the
shrinking-slice termination argument inside one commit with its own tests.

P6 last and separate, because a gate that lands with the code it gates cannot
tell a gate failure from a feature bug.

**Parallelisation.** P2 is independent of P3–P5 once P1 exists and could be
written alongside them by a second pair of hands; everything else is a
chain, since each phase extends the same two source files. Given six small
phases in one package, serial execution is the honest answer — the
coordination cost of splitting P2 out exceeds what it saves.

**Phase-to-commit mapping.** One commit per phase, six total:

| Commit | Message |
| ------ | ------- |
| P1 | `feat(markdown): zero-dependency renderer skeleton, one escaping writer` |
| P2 | `feat(markdown): safeURL scheme filter` |
| P3 | `feat(markdown): inline pass - code, links, emphasis, hard breaks` |
| P4 | `feat(markdown): headings, thematic breaks, fenced code blocks` |
| P5 | `feat(markdown): lists, blockquotes, task items and GFM tables` |
| P6 | `test(markdown): whole-corpus render and tag-whitelist invariant, FuzzRender` |

**Documentation update points.** None in this subtask. `CLAUDE.md`'s Project
Structure tree and the renderer's own note are deferred to
`workspace-file-column`, where the package gains a consumer and the
`template.HTML` exception in `internal/web/templates.go:16-19` becomes real —
documenting an exception that does not exist yet would be wrong. The package
doc comment written in P1 carries the contract in the meantime.

**Final verification plan.**

1. `gofmt -l internal/markdown` — empty
2. `go vet ./...`
3. `go build ./...`
4. `go test ./... -race`
5. `go test ./internal/markdown -fuzz FuzzRender -fuzztime 30s`
6. `git diff --exit-code go.mod go.sum` — the zero-dependency gate
7. Re-read the task's acceptance criteria against the test names, one by
   one, and record in `implementation.md` which out-of-subset constructs the
   corpus contains and therefore renders as literal text.
