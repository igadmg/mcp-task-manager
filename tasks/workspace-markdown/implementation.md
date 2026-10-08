# Implementation — workspace-markdown

Phase: implementation, run 1. Executed `plan.md` phases P1–P6 in order on
branch `igor.cwer/wip/web-task-workspace--workspace-markdown`.

### Phase Summary

All six planned phases are implemented and verified. The new package
`internal/markdown` holds one exported symbol, `Render(src string) string`,
and imports only `html` and `strings`. `go.mod` and `go.sum` are unchanged.
No existing file in the tree was touched.

| Phase | Status |
| ----- | ------ |
| P1 skeleton + the one escaping writer | done |
| P2 `safeURL` | done, with one rule tightened (see Deviations) |
| P3 inline pass | done, implemented as recursive descent rather than a splicing delimiter stack (see Deviations) |
| P4 leaf blocks | done |
| P5 containers and tables | done |
| P6 corpus and fuzz gates | done |

### Lead Checklist

- Design and plan followed; every deviation is recorded below with its
  reason.
- Scope held: no HTTP route, template, CSS or `template.HTML` wrap, and no
  `CLAUDE.md` edit — all deferred to `workspace-file-column`, as planned.
- Package boundary respected: the renderer is a leaf with no importers yet,
  so `internal/web`'s "no trusted HTML in this package" comment is still
  literally true.
- No commits made by hand. Under this project's git branching the squash
  commit belongs to `complete_task`, so the plan's six-commit mapping
  collapses into one delivered commit (see Deviations).

### Coder Report

Three source files, as designed:

- **`internal/markdown/markdown.go`** — package doc stating the contract and
  the tag/attribute whitelist; `Render`; `normalize` (CRLF and lone CR);
  `splitLines` (drops the newline-induced trailing element, so a file with
  and without a trailing newline render identically — 114 of this repo's
  artifacts have none); `renderBlocks`, the block loop with the designed
  dispatch order and a `next <= i` belt that makes a hang impossible even if
  a future writer forgets to advance; the block writers for paragraphs,
  headings, thematic breaks, fences, blockquotes, lists (nesting, looseness,
  GFM task items) and GFM tables; `writeFallback` for the depth cap.
- **`internal/markdown/inline.go`** — `writeText`, the single source-byte
  path, with a comment saying so; the `inliner` type carrying the shared
  lookahead budget; the one-pass scanner; code spans with the CommonMark
  backtick-run and space-strip rules; links, images, titles and destinations;
  `*`-emphasis; backslash escapes; hard breaks; `writeAttr`.
- **`internal/markdown/url.go`** — `safeURL` and `isScheme`, with the
  accept rule written out as a numbered comment.

Points worth a reader's attention:

- **Paragraphs are handed to the inline pass whole**, lines joined with
  newlines, so a code span or an emphasis run survives a wrapped line. This
  matters here: prose in these artifacts wraps at about 76 columns and there
  are 17 071 code spans in the corpus. The cost is that the hard-break rule
  lives in the inline scanner (`case '\n'`) rather than in the block layer.
- **Emphasis is recursive descent with a depth cap, not a delimiter stack.**
  See Deviations; the bounds the design asked for are still there, enforced
  by `maxInlineDepth` and by a shared lookahead budget.
- **The depth cap degrades, never errors.** `maxBlockDepth`/`maxInlineDepth`
  of 16 turn deeper input into escaped text.
- **Task items emit numeric entities** (`&#9744;`, `&#9745;`) rather than
  literal glyphs, keeping the source ASCII per this repo's convention while
  the browser still draws a box.

### Reviewer Findings

Four findings, all raised and resolved during the phase:

1. **`safeURL` accepted `java&#9;script:`** — the `#` of the entity was read
   as the start of a URL fragment, so the scheme check never ran. The
   research note had assumed the scheme charset would reject it, which was
   wrong about the scan order. Fixed by adding the `&` rule (step 5) and
   covered by `TestSafeURLAmpersand`. The link was in fact harmless even
   before the fix, because the attribute escaper turns the `&` into `&amp;`,
   but the rule the project documents is "entity tricks lose the link", and
   now the code matches it.
2. **A trailing-space run at the very end of input survived into `<p>`** —
   cosmetic, since no line follows it and no break can be meant. Fixed in
   `writeParagraph`.
3. **An item whose first line opens a block rendered that line as prose** —
   `- ```go` and `- - nested` would have been mis-rendered. Fixed by letting
   `startsBlock` decide for the first line too, which also makes
   `- - nested` a correctly nested list.
4. **The first draft of `TestNoForbiddenConstructsInOutput` was wrong, not
   the renderer** — it searched for `style=` and `onclick` as bare
   substrings, which legitimately appear in *escaped* prose. The needles now
   carry the character that would make them live markup (`style="`), and the
   tag whitelist carries the rest of the argument.

No outstanding findings. No architecture drift: no generated files exist in
this project, no existing file was edited, and nothing was added to
`internal/web`.

### Tester Report

```
gofmt -l internal/markdown/                    # empty
go build ./...                                 # ok
go vet ./...                                   # ok
go test ./internal/markdown/                   # ok
go test ./internal/markdown/ -race -count=2    # ok
go test ./... -race                            # all 11 packages ok
go test ./internal/markdown/ -fuzz FuzzRender -fuzztime 45s
    2 645 202 executions, 342 interesting inputs, PASS
git diff --exit-code go.mod go.sum             # unchanged
```

Corpus gate, from `TestRenderCorpus -v`:

```
rendered 178 artifacts, 1264607 bytes in, 1622782 bytes out
```

(178 rather than the research pass's 173 — this task's own four artifacts
plus `tasks/` churn since the scan.) Every artifact rendered without panic,
and every `<` in all 1.6 MB of output opens a whitelisted tag.

Test inventory against the acceptance criteria, one by one:

| Criterion | Where |
| --------- | ----- |
| each supported construct | `TestRenderBlocks` (58 cases), `TestRenderInlineConstructs` (31 cases) |
| nested lists | `TestRenderBlocks` "nested bullets", "three levels", "ordered inside bullet" |
| table with inline code and a link in a cell | `TestTableCellWithCodeAndLink` |
| unterminated fenced block | `TestRenderBlocks` "unterminated fence" |
| fence inside a list | `TestRenderBlocks` "fence inside an item"; quoted variant in `TestNestingTerminates` |
| `<script>` in prose, a heading, a code block, a link title | `TestScriptIsNeverPassedThrough` (14 positions) |
| every text node, code body, cell and attribute escaped | `TestEveryTextPositionIsEscaped` (12 positions), `TestAttributeValuesCannotBreakOut` (5 attributes) |
| each rejected URL scheme | `TestSafeURL` (28 rows), `TestRejectedURLKeepsText`, `TestRejectedImageURLKeepsAltText` |
| CRLF input | `TestLineEndingsAreEquivalent` |
| file with no trailing newline | `TestLineEndingsAreEquivalent` |
| empty input | `TestRenderBlocks` "empty", "only a newline", "only whitespace" |
| no unsafe mode | `TestRenderHasNoUnsafeMode`, and the API has no second parameter |
| fragment, not a document | `TestNoForbiddenConstructsInOutput` |
| renders every artifact in `tasks/` without panic | `TestRenderCorpus` |
| deterministic | `TestRenderIsDeterministic`, `TestRenderCorpusIsStable` |
| no new `go.mod` / `go.sum` entry | the gate above |

**Out-of-subset constructs the corpus actually contains**, from
`TestCorpusOutOfSubsetConstructs -v` — the note the task description asks
for. All of these render as literal text:

| Construct | Occurrences | Consequence |
| --------- | ----------: | ----------- |
| underscore emphasis (` _`) | 43 | almost all are `snake_case` identifiers and table pipes in prose, which is exactly why `_` is not a delimiter; nothing is mis-rendered |
| HTML entity (`&middot;` and friends) | 6 | shown as written, e.g. `&middot;` |
| footnote reference (`[^`) | 2 | both inside code blocks; shown as written |
| autolink `<https://` | 0 | — |
| reference link definition | 0 | — |
| tilde fence | 0 | supported anyway, it was free |

So no corpus artifact needs a construct the renderer lacks, with one
decision already taken on that evidence: task-list checkboxes were moved
*into* scope (33 occurrences).

Manual validation: none possible — there is no HTTP surface in this subtask.
The substitute is `TestRenderCorpusIsStable`, which renders
`tasks/web-task-workspace/research.md` (20 406 bytes in, 26 263 out) and
`tasks/web-ui-kanban-module/task.md` (8 749 in, 10 518 out); both were read
by eye once during the phase, including the Cyrillic blockquote in the
latter, which survives untouched.

### Quality Gate Result

**PASS.** Every gate in the plan's final verification list ran and is green,
including the zero-dependency gate and 45 seconds of fuzzing.

### Files Changed

| Path | Lines | Change |
| ---- | ----: | ------ |
| `internal/markdown/markdown.go` | 613 | new |
| `internal/markdown/inline.go` | 390 | new |
| `internal/markdown/url.go` | 92 | new |
| `internal/markdown/markdown_test.go` | 266 | new |
| `internal/markdown/corpus_test.go` | 162 | new |
| `internal/markdown/escape_test.go` | 136 | new |
| `internal/markdown/url_test.go` | 139 | new |
| `internal/markdown/fuzz_test.go` | 86 | new |

Nothing else. `go.mod`, `go.sum`, `internal/web/**` and `CLAUDE.md` are
untouched.

### Deviations From Plan

1. **Emphasis is recursive descent with a depth cap, not a splicing
   delimiter stack.** The design specified the CommonMark reference
   technique — record an opener's output offset, splice the tag in when a
   closer matches — with a 64-entry stack. The implementation instead finds
   the matching closer by a bounded forward scan and recurses on the content,
   capped at `maxInlineDepth = 16` and sharing one `inlineLookahead` budget
   of 1 MiB per block across all nested calls. The guarantees the design
   asked for are unchanged (bounded stack, bounded total work, surplus
   delimiters render literally); the code is shorter, has no buffer splicing
   and so no O(output) insertion, and the budget bounds code-span and link
   scanning too, which the 64-delimiter cap would not have. Worth knowing if
   the subset ever grows: full CommonMark emphasis flanking rules do need the
   delimiter-stack shape.
2. **A run of three or more `*` is literal text** — no bold-italic. All four
   `***` occurrences in the corpus are inside code spans, so nothing visible
   changes. Pinned by `TestRenderInlineConstructs` "three stars are literal".
3. **`safeURL` gained a step**: an `&` before the first `/`, `?` or `#`
   rejects. The research pass had the right answer (reject
   `java&#9;script:`) for the wrong reason; see Reviewer Finding 1.
4. **A link title becomes a `title` attribute**, so the attribute whitelist
   is `class`, `href`, `src`, `alt`, `title` — one more than the design
   listed. It exercises attribute escaping in a position the acceptance
   criteria name, and it is useful to a reader.
5. **A space inside a link destination loses the link** (it falls under the
   control/whitespace rule). `[a](<b c.md>)` keeps its text and drops the
   anchor; pinned as a test case rather than left implicit.
6. **An ordered list never emits `start`**, so `3. x` renders as the first
   item of an `<ol>`. No corpus list depends on it; mentioned because it is
   a visible simplification.
7. **Six commits became one.** The plan mapped P1–P6 to six commits. Under
   this project's git branching a subtask's work is squash-merged onto the
   parent's wip branch by `complete_task`, and the tool contract says the
   agent must not commit itself, so the phase boundaries live in this
   document instead of in git history.

### Next Phase Handoff

The renderer is complete, tested and unused. For `workspace-file-column`:

- Call `markdown.Render(src)` and wrap the result in `template.HTML` in
  exactly one place. That wrap is the single documented exception to the rule
  in `internal/web/templates.go:16-19`; the comment there should be amended
  at the same time, and `CLAUDE.md`'s Project Structure tree should gain
  `internal/markdown/` then — both deliberately left out of this subtask,
  which has no consumer to document.
- CSS is needed for three classes the renderer emits and styles nowhere:
  `language-<lang>` on `<pre><code>`, `align-left` / `align-center` /
  `align-right` on table cells, and `task-list-item` on `<li>`. The task
  item also carries a `&#9744;`/`&#9745;` glyph; if CSS would rather draw
  the box, dropping the glyph is a one-line change in `writeListItem`.
- Open questions recorded in `design.md` and still open: whether the viewer
  offers `{id}.md` (whose YAML frontmatter would render as a thematic break
  plus a `key: value` paragraph — the renderer has no frontmatter awareness
  on purpose), whether relative links between artifacts should be rewritten
  to workspace URLs (the renderer emits the author's URL verbatim), and
  whether a rendered artifact is cached. `Render` is pure, so any of those
  choices works without touching this package.
- `FuzzRender` is the first fuzz target in the tree. It needs no corpus
  directory checked in; it seeds itself from `tasks/`.
