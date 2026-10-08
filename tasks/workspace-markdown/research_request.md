# Research request: workspace-markdown

**Additional research needed: 25 / 100**

Greenfield and self-contained: no repo precedent to discover, no dependency to
evaluate (goldmark was declined), no HTTP or template surface. The research
that is still worth doing is empirical rather than archaeological — the subset
has to be decided against the real corpus, not guessed.

## Already covered by research.md
- No markdown or sanitizer module exists anywhere in `go.mod` / `go.sum`, and
  there is no `vendor/` directory, so "zero dependencies" is a real property
  of the current tree, not an assumption.
- The package rule this renderer's output will eventually bend:
  `internal/web/templates.go:16-19` states there is deliberately no
  `template.HTML` in the web package. The wrap point is this subtask's
  consumer, not this subtask.
- The read path that will feed it: `ReadTaskFile` returns the whole file as a
  string with no size cap (`internal/storage/files.go:99-106`).
- Files without an extension are real in this repo (`design`, `research`,
  `plan`), which is why the renderer cannot key off the suffix.

## Gaps
1. **Corpus inventory.** Scan every `*.md` and extensionless artifact under
   `tasks/` and `tasks/archive/` and count which constructs actually occur:
   heading levels, nested list depth, tables (and whether any cell holds a
   pipe, inline code or a link), fenced blocks and their info strings,
   blockquotes, hard breaks, reference-style links, footnotes, task-list
   checkboxes, setext headings, inline HTML, HTML entities, raw URLs relying
   on autolinking. The subset in the task description is a guess until this
   count exists; anything with zero occurrences can be left out on evidence,
   and anything frequent that is missing from the list has to be added.
2. **Escaping strategy, decided before writing the parser.** Whether the
   renderer escapes at the point of emitting text (every writer call) or
   builds a tree and escapes once on render. This determines whether an
   escaping bug is possible at all, so it is a research question, not a style
   one. Check how `html.EscapeString` handles `'` and whether that matters for
   attribute values the renderer emits (code-block language class, link
   `href`).
3. **URL filtering, concretely.** Establish the exact accept rule against
   known bypasses: `JaVaScRiPt:`, `java\tscript:`, `java&#9;script:`,
   leading control characters and whitespace, `data:text/html`, a scheme-less
   `//host`, and a bare `#anchor`. Decide whether relative links between task
   artifacts (`[design](design.md)`) should resolve to a workspace URL or stay
   inert — and note it for `workspace-file-column`, since it is that
   subtask's link that would have to be rewritten.
4. **Termination and pathological input.** Confirm the parser cannot loop or
   blow the stack on: an unterminated fence; a fence opened inside a
   blockquote inside a list; 1000-level nested emphasis; a 10 MB single line;
   a table with a header and no body; CRLF mixed with LF. A fuzz target over
   the corpus plus random bytes is cheap here and settles it.
5. **Where the package lives.** `internal/web/markdown.go` (presentation,
   matching the project's "web owns rendering" habit) versus a top-level
   `internal/markdown` package (testable without HTTP, importable by the
   future graph column). Pick one with the project's package-boundary rule in
   hand — `internal/task` is the only package that performs task operations,
   which says nothing about presentation helpers, so this needs an explicit
   decision rather than a default.
