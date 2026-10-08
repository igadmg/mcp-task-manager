# Research — workspace-markdown

Phase: research, run 1. Scope: the five gaps in `research_request.md`
(25/100). Evidence-only; empirical rather than archaeological, as the request
states. No design proposed here.

### Task Slice

- The corpus the renderer must survive: every `*.md` and extensionless
  artifact under `tasks/` and `tasks/archive/` (173 files, 1 201 352 bytes).
- The escaping primitives the standard library already offers (`html`,
  `html/template`) and what the five required characters map to.
- The URL accept rule, checked against a concrete bypass list.
- Termination and stack behaviour constraints for a hand-written parser.
- The package-boundary question: `internal/web` versus `internal/markdown`.
- Out of slice: any HTTP route, template or CSS (`workspace-file-column`),
  syntax highlighting, mermaid rendering.

Documents loaded: `CLAUDE.md` (Web UI, Attached Files, Validation, Project
Structure), `tasks/workspace-markdown/workspace-markdown.md`,
`tasks/workspace-markdown/research_request.md`,
`tasks/web-task-workspace/research.md` (the parent's confirmed facts on the
route table, `_detail.html` and the escaping rule).

## 1. Corpus inventory

Scanned with a throwaway script over all 173 artifacts. Occurrence counts
(construct instances, not files):

| Construct | Count | Verdict |
| --------- | ----: | ------- |
| inline code span (`` ` ``) | 17 071 | by far the dominant construct |
| bullet list item | 5 964 | required |
| strong (`**`) | 2 176 | required |
| ordered list item | 882 | required |
| table row (line starts `\|`) | 871 | required |
| table delimiter row | 87 | required |
| fenced code block delimiter | 386 | required |
| ATX heading h3 / h2 / h4 / h1 | 272 / 148 / 81 / 72 | h1–h4 real, **h5/h6: 0** |
| `---` line, standalone | 156 | horizontal rule |
| `---` line directly after text | 110 | YAML frontmatter closers and fenced examples, **not** setext intent |
| file without trailing newline | 114 files | must be handled; it is the norm here |
| blockquote (`> `) | 59 | required |
| GFM task-list checkbox | 33 | **frequent; the task listed it out of scope** — see below |
| emphasis, single `*` | 190 | required |
| inline link `[t](u)` | 4 | see below |
| HTML entity (`&middot;`, `&#34;`, `&#9;`, `&lt;`, `&gt;`) | 8 | decoding not worth it |
| double-backtick code span | 2 | falls out of the general backtick-run rule |
| emphasis, single `_` | 1 | see below |
| hard break (two trailing spaces) | 1 | cheap, keep |
| CRLF file | 0 | still spec'd; input is not guaranteed to come from this repo |
| image `![]()` | 0 | — |
| autolink `<http://…>` | 0 | — |
| reference-style link definition | 0 | — |
| footnote `[^…]` | 1, inside a code block | — |
| tilde fence `~~~` | 0 | — |
| setext h1 (`===`) | 0 | — |
| hr written `***` or `___` | 0 | — |
| hard break (trailing backslash) | 0 | — |

Details that change the subset:

- **Checkboxes are real.** 33 occurrences, all in acceptance-criteria lists
  (`tasks/web-ui-kanban-module/task.md`, and the same style in other
  `task.md` artifacts). The task description says setext/footnotes/checkboxes
  are out of scope, but with an explicit escape clause: "add later only if a
  real artifact in `tasks/` needs them; note which, if any, are missing when
  the tests run over the corpus". This is the one that a real artifact needs.
  Without it the item renders as literal `[ ] text`, which is legible but
  wrong-looking in the one place the viewer will be used most.
- **Links are almost absent.** 4 matches, of which two are false positives
  (`[T ~string]([]T)` inside a code span in
  `tasks/done-stats-cleanup/design.md`, and `[design](design.md)` inside
  this task's own `research_request.md`). Exactly one real link exists in the
  whole corpus: `[Releases page](https://github.com/…/releases)` in
  `tasks/archive/011/011.md`. Links stay in the subset because the
  acceptance criteria are about the URL filter, not about frequency — but no
  corpus file depends on them rendering.
- **Underscore emphasis is noise, not signal.** One real occurrence against
  constant `snake_case` in prose (`parent_id`, `created_by`, `closed_at`,
  `base_branches` — mostly, but not always, inside code spans). Supporting
  `_` as an emphasis delimiter buys one italic phrase and risks mangling
  identifiers.
- **No `---` in this corpus is a setext heading.** All 110 "ambiguous"
  occurrences are YAML frontmatter closers in `{id}.md` records or `---`
  inside fenced yaml examples. Treating `---` unconditionally as a
  horizontal rule matches author intent everywhere here.
- **`{id}.md` records start with YAML frontmatter.** Rendering one as
  markdown would show the frontmatter as an `hr`-wrapped paragraph. Which
  files the viewer offers is `workspace-file-column`'s decision; noted in
  Open Questions.
- **Fences occur indented inside list items** (29 of the 386 delimiters have
  leading spaces), so "a fence inside a list" is a corpus case, not only a
  test case.
- **Size and shape are modest.** Largest artifact ~56 KB, longest single
  line 1 188 characters, total corpus 1.2 MB. Nothing here stresses a
  renderer; the pathological inputs have to be synthesised.

## 2. Escaping strategy

- `html.EscapeString` escapes all five characters the acceptance criteria
  name, verified by running it: `<>&"'` →
  `&lt;&gt;&amp;&#34;&#39;`. `"` becomes `&#34;` and `'` becomes `&#39;`, so
  one function is safe for both text nodes and double- or single-quoted
  attribute values. No second attribute-specific escaper is needed.
- `html.UnescapeString("&#9;")` returns a tab — confirming that a browser
  decodes numeric entities inside an attribute value. This is the reason the
  renderer must escape `&` in every `href` it emits: a source
  `java&#9;script:alert(1)` must reach the browser as the literal text
  `java&#9;script:…`, i.e. `&amp;#9;` in the attribute, so no decode step can
  reconstruct a scheme.
- Two shapes are possible: build an AST and escape once at render, or escape
  at every emit. The second is cheaper here and makes the invariant
  checkable: if the only code path that copies source bytes into the output
  is one escaping writer, an escaping bug requires a *new* code path, not a
  missed call. That is a testable property — every `<` in the output must
  begin a tag from a fixed whitelist — rather than a review habit.
- No `template.HTML`, `text/template` or `html/template` involvement at all:
  the renderer returns a plain `string` and builds it with
  `strings.Builder`.

## 3. URL filtering

The accept rule that holds against the request's bypass list:

1. Strip leading and trailing bytes `<= 0x20` and `0x7F`.
2. Reject if any byte in the remainder is `< 0x21` or `== 0x7F` (this is
   what kills `java\tscript:` and newline-split schemes; `%20` is unaffected
   because it is already percent-encoded).
3. Reject a remainder starting with `//` — protocol-relative, off-site, and
   indistinguishable from a path to a reader.
4. Find the first `:`; if it appears after the first `/`, `?` or `#`, or does
   not appear at all, the URL is relative or an in-page anchor → **accept**.
5. Otherwise the part before `:` must match `[A-Za-z][A-Za-z0-9+.-]*` and,
   ASCII-lowercased, be one of `http`, `https`, `mailto` → accept; anything
   else → reject.

Checked against each named bypass:

| Input | Outcome | Why |
| ----- | ------- | --- |
| `JaVaScRiPt:alert(1)` | reject | step 5, lowercased scheme not in the set |
| `java\tscript:alert(1)` | reject | step 2, control byte |
| `java&#9;script:alert(1)` | reject | step 5, `&`/`#` are not scheme characters — and no decode can happen because the `&` is escaped |
| `\x01javascript:alert(1)` | reject | step 1 trims it, then step 5 rejects |
| `data:text/html,<script>` | reject | step 5 |
| `vbscript:msgbox` | reject | step 5 |
| `//evil.example/x` | reject | step 3 |
| `#anchor` | accept | step 4, no colon |
| `design.md` | accept | step 4, no colon |
| `./plan` | accept | step 4 |
| `https://example.com/a?b=1#c` | accept | step 5 |
| `mailto:a@b.c` | accept | step 5 |
| `HTTPS://EXAMPLE.COM` | accept | step 5 is case-insensitive on the scheme only |
| `tasks/x.md:12` | accept | step 4 — the colon is after a `/` |

A rejected URL must lose the anchor and keep its visible text, per the
acceptance criteria.

**Relative links between artifacts.** Only one relative link exists in the
corpus (`[design](design.md)`, in a research request). The honest options are
to pass it through unchanged or to render it as inert text. Passing it
through is what a markdown renderer does; it is also what
`workspace-file-column` would need to rewrite if it ever wants such a link to
open the sibling artifact in the workspace. Noted for that subtask: this
renderer emits the author's URL verbatim (escaped) and performs no
resolution.

## 4. Termination and pathological input

Constraints a hand-written parser has to satisfy, with the input that would
break each:

- **Unterminated fence** — the block loop must treat end-of-input as an
  implicit close rather than scanning past the slice end. Real risk: a
  `for` that looks for the closing line and only then advances.
- **Fence inside a blockquote inside a list** — this is the recursion path.
  A nested block parse must receive a strictly smaller line range, and a
  depth cap is needed so an adversarial `> > > > …` (or 1 000 nested list
  markers) cannot grow the Go stack without bound. Beyond the cap the
  content should degrade to paragraph text, never panic.
- **1 000-level nested emphasis** (`***…***`) — a delimiter-matching pass
  that rescans from the start on each match is O(n²). A bounded delimiter
  stack keeps it linear; surplus delimiters past the bound render as
  literal `*`.
- **A 10 MB single line** — the inline scanner must advance at least one byte
  per iteration and must not re-scan. 1 188 characters is the real maximum,
  so this is a robustness property, not a performance requirement.
- **Table with a header and no body** — a valid GFM table; the row loop must
  tolerate zero body rows.
- **CRLF mixed with LF** — no CRLF exists in the corpus, so normalising
  `\r\n` (and a lone `\r`) once on entry is both free and untested by real
  data; it has to be covered by unit tests.
- **No trailing newline** — 114 of 173 corpus files. The line splitter must
  not invent an empty final line or drop the last one.

Go 1.25.5 (`go.mod`) ships native fuzzing, so `FuzzRender` costs nothing in
dependencies: seed it with the corpus, assert no panic and assert the output
tag-whitelist invariant. No existing `Fuzz*` target exists anywhere in the
tree, so this would be the first.

## 5. Where the package lives

Facts bearing on it:

- `internal/web/templates.go:16-19` states the rule in the package's own
  comment: "There is deliberately no safeHTML helper and no `template.HTML`
  anywhere in this package — task titles and descriptions are
  user-controlled."
- `internal/web` is reachable only through HTTP handlers; its tests spin up
  `httptest` servers (`internal/web/handlers_test.go`,
  `internal/web/race_test.go`).
- `internal/vcs` is the established precedent for a leaf package: single
  purpose, standard library only, one constructing consumer
  (`project.Build`), driven through an interface.
- The package-boundary rule in `CLAUDE.md` constrains *task operations*
  (`internal/task` is the only package that performs them, `internal/storage`
  is private to it). It says nothing about a presentation helper, which is
  why `research_request.md` flags this as needing an explicit decision.
- `tasks/web-backlog-graph/` is a planned sibling column that would also
  render artifact prose.
- There is no existing markdown code anywhere in `internal/` (grep for
  "markdown" hits only storage's file format and test helpers).

Both locations are reachable; the decision belongs to the design phase.

### Confirmed Facts (reference)

- `go.mod` declares `go 1.25.5` and three direct requirements (`flaggy`,
  `mcp-go`, `yaml.v3`); no markdown or sanitizer module, direct or indirect,
  and no `vendor/` directory.
- `MarkdownStorage.ReadFile` returns the whole file as a `string` with no
  size cap (`internal/storage/files.go:85-89`), so the renderer's input is
  an unbounded in-memory string by construction.
- `internal/` currently holds: `app`, `cli`, `config`, `project`, `storage`,
  `task`, `testsupport`, `tools`, `vcs`, `web`.

### Open Questions

1. Does the viewer offer `{id}.md` itself? If so, something has to decide
   whether YAML frontmatter is stripped before rendering. Not this subtask's
   call — recorded for `workspace-file-column`.
2. Checkbox rendering shape, if adopted: a text glyph versus a disabled
   `<input>`. The styling half of the answer lives in
   `workspace-file-column`'s CSS, so the renderer should emit a class and no
   form element.

### Risks and Unknowns

- The subset is now evidence-backed for *this* corpus. Artifacts are written
  by agents following the workflow skills, so the construct mix is stable
  but not frozen; a future skill that starts emitting reference links or
  setext headings would render as literal text, degrading gracefully.
- The 8 HTML entities in the corpus will render literally (`&middot;` shown
  as text). Accepted: decoding entities would mean a named-entity table, and
  "escape `&` always" is the property the acceptance criteria ask for.
