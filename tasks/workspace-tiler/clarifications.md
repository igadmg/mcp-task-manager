# Clarifications — workspace-tiler

Answered 2026-10-08, after the additional research run (`research.md`). These
decide points that research surfaced and that the design phase must honour.
They extend, and do not contradict, the "Decided 2026-10-07" block in the
subtask's own description.

| Question | Decision |
|----------|----------|
| The `#` / `?` escaping defect in the shipped attached-file link (`_detail.html:122`, from `8b16e42`): `html/template`'s URL normalizer leaves both unescaped in an `href` path, so a file named `a#b.md` or `a?b.md` is unreachable from the panel | **Fix it in this subtask.** The tiler introduces Go-side escaping (`url.PathEscape` per segment) for the chain anyway; the existing link is corrected through the same mechanism, with a test covering `#` and `?`. |
| Browser verification — the one gap no amount of reading closes, since every test in the repository asserts over response bytes or compiled CSS text | **Part of this subtask's acceptance.** A manual pass via the `open_board` skill: rebuild, open the board, walk a chain at `lg` and below, confirm the strip does not scroll horizontally, the rail stays pinned, and the slide is a transition. The automated assertions stay as they are — bytes and compiled CSS — and do not try to imitate a browser. |
| How real the columns that ship with the tiler should be, given the markup belongs to `workspace-task-column` and `workspace-file-column` | **Stubs.** Each kind renders the minimum needed to prove the mechanism and drive the chain tests. The two blocked siblings fill them in and keep their scope. |
| The page `<title>`: `detail.html`'s `{{ define "title" }}` (`#id Title`) disappears with the standalone page, and the board page has no such block | **Keep a task-specific title.** The board page gains a title block that the chain state fills: `#id Title` when a panel or chain is open, `Task board` on the bare board. htmx caches and restores `document.title` on Back, so history entries and bookmarks stay distinguishable. |

## Consequences for the acceptance criteria

- The round-trip criterion's list of names to cover ("dots, spaces, unicode
  and `+`") gains **`#` and `?`** — the two characters that actually break,
  per `research.md`.
- One extra acceptance item: the attached-file link in the task panel
  round-trips a name containing `#` or `?`.
- One extra acceptance item: a manual browser pass through `open_board`,
  recorded in the implementation report.
