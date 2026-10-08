# Research request: workspace-file-column

**Additional research needed: 45 / 100**

The read path, the error surface and the trust rule are all documented with
line numbers. The unresearched part is presentation: this repository has no
prose styling at all, and no precedent for emitting a trusted HTML fragment.

## Already covered by research.md
- `Service.ReadTaskFile`: locks, requires the task to exist, archive fallback
  through `s.get` (`internal/task/service.go:352-360`, `:295-307`), delegating
  to `MarkdownStorage.ReadFile` (`internal/storage/files.go:89-107`).
- Reads validate with `checkReserved=false`, so `{id}.md` and `*.phase` are
  readable through this path and must be refused by this column deliberately
  (`internal/storage/files.go:90`, `:35-45`).
- Traversal is already impossible; the `.` hole is the one gap and belongs to
  `workspace-filenames`.
- No size cap on a read (`internal/storage/files.go:99-106`).
- The "gone" state precedent and its 200-for-a-fragment convention
  (`internal/web/templates/_detail.html:1-5`, `internal/web/handlers.go:42-48`).
- `render` buffers the whole response, so a template error is a clean 500
  rather than a half page (`internal/web/handlers.go:100-112`).
- The rule being narrowed, verbatim, with its reason
  (`internal/web/templates.go:16-19`), and that `asset` is currently the only
  FuncMap entry (`:21`).
- The offline guarantee and the test that enforces it
  (`internal/web/handlers_test.go:226-250`); the three-files-by-name embed
  (`internal/web/assets.go:11-15`).
- The CSS pipeline and that only `../templates` is scanned
  (`internal/web/assets/input.css:13-15`), plus the `assets_test.go` pattern
  that catches a forgotten rebuild (`internal/web/assets_test.go:15-69`).

## Gaps
1. **Prose styling with the standalone Tailwind v4 CLI — blocking.** There is
   no prose or typography rule anywhere in `input.css` or `app.css` today.
   Establish whether the pinned standalone binary (`v4.3.3`,
   `scripts/build-css.sh:21`) can load `@plugin "@tailwindcss/typography"`
   without Node, or whether the prose scope has to be written by hand as a
   component block. This is a build-pipeline question, not a taste one, and it
   gates all the markup. Also check how much the output `app.css` grows
   (currently ~29 KB) and whether `assets_test.go` needs new assertions.
2. **The trust point, concretely.** Decide the exact shape of the single
   `template.HTML` wrap: a FuncMap helper (which makes it reachable from every
   template in the set — the parsed sets share `funcs`,
   `internal/web/templates.go:20-33`), or a `template.HTML`-typed field on one
   view struct (reachable only where that struct is). The second is narrower
   and matches the "plain strings in view models" habit; confirm it renders as
   expected and that no other field can be switched to it by accident. Then
   write the comment that replaces the current absolute rule.
3. **Rendering decision by name.** The rule is "`*.md` and extensionless →
   markdown, everything else → preformatted". Inventory the actual extensions
   present across `tasks/` and `tasks/archive/` (`.md`, `.phase`, anything
   else) before fixing the rule, and decide what a dotfile or a double
   extension (`notes.en.md`) does.
4. **Big files and the working area.** No cap exists on the read, and some
   artifacts in this repo are already thousands of lines. Measure the rendered
   size of the largest artifact, decide whether there is a byte or line limit
   with a "truncated" notice, and check that the independent-scroll container
   stays usable — the viewer must scroll without the columns moving, which is
   an `overflow` and `min-height` question inside the transformed strip the
   tiler builds (coordinate with its gap 4).
5. **Relative links between artifacts.** `[design](design.md)` appears in real
   task files. Decide whether the column rewrites such a link into a chain URL
   (appending a file column), leaves it as a dead relative link, or strips it.
   The decision belongs here because the renderer only reports the href; see
   `workspace-markdown` gap 3.
6. **Anchors and heading ids.** Decide whether headings get ids (for in-page
   anchors from a table of contents) and, if so, how collisions across two
   rendered documents on one page are avoided — and whether `#anchor`
   navigation inside a transformed, independently scrolled container does
   anything sane.
7. **The description column shares this code path.** Confirm the description
   is rendered through the same function and the same trust point, so there is
   exactly one wrap, not two.
