# Task workspace: how the page is put together

How the dashboard's workspace page is composed — the DOM, the template graph,
the CSS boxes that make the grid, and the Go view types behind them. Source of
truth is the code: `internal/web/templates/`, `internal/web/assets/input.css`,
`internal/web/{view,chain,kinds,handlers}.go`. Diagrams are Mermaid.

The workspace has one page template (`board.html`) and one swap target
(`#strip`). Every state — the bare board, a task's panel, a chain of open
columns — is that same page, so a URL never switches surface.

---

## 1. The page, as a picture

### Depth 0 — `/` or `/tasks/{id}`

No rail, no shift. The board is the strip's only unit and fills the stage; the
side panel is the right half of the board unit's own grid.

```
┌ header.sticky ────────────────────────────────────────────────────────┐
│ Task board    .tasks                            read-only · N tasks  │
└───────────────────────────────────────────────────────────────────────┘
main.mx-auto.max-w-1600
┌ #strip.workspace ────────────── height: 100dvh − var(--shell-chrome) ─┐
│ ┌ .stage  (flex-1, overflow-x: clip) ───────────────────────────────┐ │
│ │ ┌ .strip  (flex row, gap var(--strip-gap)) ─────────────────────┐ │ │
│ │ │ ┌ .unit.kind-board   flex: 0 0 var(--board-w) = 100% ───────┐ │ │ │
│ │ │ │  grid  lg: 1fr + 22rem                                    │ │ │ │
│ │ │ │ ┌ .pane data-pane=board ──────────┐ ┌ aside#panel.pane ─┐ │ │ │ │
│ │ │ │ │ #board  hx-get /board every 5s  │ │ _detail.html      │ │ │ │ │
│ │ │ │ │ ┌ To do ┐ ┌ In progress ┐ ┌Done┐│ │ (or the dashed    │ │ │ │ │
│ │ │ │ │ │ cards │ │ lanes ×4    │ │stat││ │  "pick a card")   │ │ │ │ │
│ │ │ │ │ └───────┘ └─────────────┘ └────┘│ │                   │ │ │ │ │
│ │ │ │ └─────────────────────────────────┘ └───────────────────┘ │ │ │ │
│ │ │ └───────────────────────────────────────────────────────────┘ │ │ │
│ │ └───────────────────────────────────────────────────────────────┘ │ │
│ └───────────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────┘
```

### Depth 2 — `/tasks/42/w/t/43/f/design.md`

The rail appears outside the stage, `.strip-shifted` slides the whole strip
left by exactly one board width plus the gap, and the **root task's own
column** takes the leftmost slot. Columns are placed from there rightwards, so
free space may remain on the right — the strip is left-aligned, not
right-aligned.

```
┌ #strip.workspace ─────────────────────────────────────────────────────┐
│ ┌ nav.rail ┐ ┌ .stage ──────────────────────────────────────────────┐ │
│ │ task     │ │ ┌ .strip.strip-shifted ────────────────────────────┐ │ │
│ │ 42       │ │ │ translateX(−(var(--board-w) + var(--strip-gap))) │ │ │
│ │ ┈┈┈┈┈┈┈┈ │ │ │                                                  │ │ │
│ │ task     │ │ │ ← .unit.kind-board (off-screen left, still polls) │ │ │
│ │ 43       │ │ │ ┆                                                │ │ │
│ │ ┈┈┈┈┈┈┈┈ │ │ │ ┆┌ root t/42 ─┐ ┌ t/43 ──────┐ ┌ f/design.md ─┐  │ │ │
│ │ file     │ │ │ ┆│ .kind-task │ │ .kind-task │ │ .kind-file   │  │ │ │
│ │ design.  │ │ │ ┆│ 22rem      │ │ 22rem      │ │ 44rem        │  │ │ │
│ │ md ←cur  │ │ │ ┆│ _detail    │ │ _detail    │ │ .unit-working│  │ │ │
│ └──────────┘ │ │ ┆└────────────┘ └────────────┘ └──────────────┘  │ │ │
│    w-28      │ └──────────────────────────────────────────────────┘ │ │
│              └──────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────┘
```

Each unit is one screen tall (`.unit { h-full }`) and scrolls **inside** its
own `.pane`, so the page itself never scrolls and no column can drift out of
line with its neighbour.

---

## 2. Markup tree

```mermaid
flowchart TD
  html["html.h-full.bg-neutral-950"] --> body["body.min-h-full.font-sans"]
  body --> header["header.sticky.top-0.z-10<br/>project · task count"]
  body --> main["main.mx-auto.max-w-1600.px-4.py-5"]
  main --> strip["div#strip.workspace<br/>THE swap target, hx-swap outerHTML"]

  strip --> rail["nav.rail<br/>only when chain depth &gt; 0"]
  rail --> step["a.rail-step × depth+1<br/>last one gets .rail-current<br/>hx-get /strip/... → #strip"]
  step --> steplab["span.rail-label + span.rail-ref"]

  strip --> stage["div.stage<br/>flex-1 · overflow-x clip"]
  stage --> inner["div.strip<br/>+ .strip-shifted when depth &gt; 0"]

  inner --> ub["section.unit.kind-board"]
  inner --> ur["section.unit.kind-task.unit-root<br/>data-column=root · only when depth &gt; 0"]
  inner --> uc["section.unit.KIND × depth<br/>last gets .unit-working<br/>data-column=KIND data-ref=REF"]

  ub --> grid["div.grid.h-full.min-h-0.gap-5<br/>lg: 1fr + 22rem"]
  grid --> pb["div.pane data-pane=board"]
  grid --> pp["aside#panel.pane data-pane=panel<br/>panel swap target, innerHTML"]
  pb --> board["div#board<br/>hx-get /board every N s, outerHTML"]
  pp --> det1["_detail.html .Board.Panel<br/>or the dashed placeholder"]

  ur --> pr["div.pane data-pane=root"] --> col1["_col.html → _col_task.html"]
  uc --> pc["div.pane data-pane=KIND:REF"] --> col2["_col.html → kind fragment"]

  board --> danger["div.amber banner<br/>DangerZone, only if any in-progress"]
  board --> bgrid["div.grid.gap-3<br/>md: 3 cols · xl: 1fr + 2fr + 1fr"]
  bgrid --> colsec["section.column × 3"]
  colsec --> colhead["header: span.dot.dot-STATUS + h2 + span.chip.chip-muted count"]
  colsec --> lanes["div.lanes<br/>In progress only"]
  colsec --> stats["div: .stats-card × N<br/>Done only"]
  colsec --> cards["div.flex.flex-col.gap-2<br/>To do: cards, or the empty stub"]
  lanes --> lane["section.lane.lane-PHASE × 4<br/>data-phase=PHASE"]
  lane --> lanehead["header: h3 phase + chip count"]
  lane --> card["article.card"]
  cards --> card
  card --> cardlink["a (hx-get /tasks/ID/panel → #panel)"]
  card --> cardbranch["div: branch chip + .copy-btn"]
  card --> cardsubs["div.border-l: nested subtask rows<br/>dot + id + title + priority"]
```

Two nodes are the whole htmx contract: **`#strip`** (a chain state, swapped
`outerHTML`) and **`#panel`** (one task's plate, swapped `innerHTML`).
`#board` replaces itself on a timer and is nested inside `.pane[data-pane=board]`,
which is why `static/app.js` saves and restores pane scroll offsets by their
`data-pane` key across a swap.

---

## 3. Template graph

```mermaid
flowchart LR
  layout["layout.html<br/>html · head · header · main"]
  boardpage["board.html<br/>title + content blocks"]
  ws["_workspace.html<br/>#strip: rail + stage + strip"]
  bd["_board.html<br/>#board: banner + 3 columns"]
  un["_unresolved.html<br/>placeholder that polls itself"]
  dt["_detail.html<br/>the task plate"]
  cl["_col.html<br/>kind dispatch (if eq .Kind)"]
  ct["_col_task.html"]
  cf["_col_file.html"]
  cd["_card.html"]
  sb["_stats_bars.html"]
  sl["_stats_lines.html"]
  br["_branch.html<br/>define branch"]

  layout --> boardpage --> ws
  ws -->|Project.Resolved| bd
  ws -->|not resolved| un
  ws -->|".Board.Panel"| dt
  ws -->|".Root and .Columns"| cl
  bd --> cd
  bd --> sb
  bd --> sl
  cl -->|kind t| ct --> dt
  cl -->|kind f| cf
  cd --> br
  dt --> br
```

Three of these fragments are also HTTP responses on their own
(`internal/web/server.go`):

| Route | Renders | Target |
|---|---|---|
| `GET /{$}`, `GET /tasks/{id}`, `GET /tasks/{id}/w/{rest...}` | `board.html` (whole page) | — |
| `GET /board` | `_board.html` | `#board`, outerHTML |
| `GET /tasks/{id}/panel` | `_detail.html` | `#panel`, innerHTML |
| `GET /strip/{rest...}` | `_workspace.html` | `#strip`, outerHTML |

`_col.html` is the kind registry's dispatch: Go templates cannot take a
template name from data, so that `if`/`else if` chain is the one place a kind's
tag maps to its fragment.

---

## 4. CSS: the boxes that make the grid

Composition of the component classes in `internal/web/assets/input.css`.
A solid arrow is "contains"; a dashed arrow is "modifies".

```mermaid
flowchart TD
  W[".workspace<br/>flex row · items-start · gap-3<br/>height: 100dvh − var(--shell-chrome)<br/>declares --shell-chrome --strip-gap --board-w"]
  R[".rail<br/>flex col · w-28 · shrink-0 · self-start"]
  RS[".rail-step<br/>flex col · ring-1<br/>+ .rail-current"]
  S[".stage<br/>flex-1 · h-full · min-w-0<br/>overflow-x clip / overflow-y visible"]
  ST[".strip<br/>flex row · h-full · items-stretch · justify-start<br/>gap: var(--strip-gap)<br/>transform: translateX(−var(--shift, 0px))"]
  SH[".strip-shifted<br/>--shift = --board-w + --strip-gap<br/>animation strip-slide 260ms"]
  U[".unit<br/>flex col · h-full · min-w-0"]
  UW[".unit-working<br/>animation unit-in 220ms"]
  KB[".kind-board<br/>flex 0 0 var(--board-w)"]
  KT[".kind-task<br/>flex 0 0 min(100%, 22rem)"]
  KF[".kind-file<br/>flex 0 0 min(100%, 44rem)"]
  P[".pane<br/>h-full · min-h-0 · overflow-y auto"]
  C[".column<br/>flex col · min-w-0 · rounded-xl"]
  L[".lanes<br/>flex col · gap-3<br/>container: lanes / inline-size<br/>--lane-min 11rem<br/>--lane-step clamp(0px, (100% − --lane-min)/3, 20%)"]
  LN[".lane<br/>margin-left: var(--lane) × var(--lane-step)<br/>width: 100% − 3 × var(--lane-step)"]
  LP[".lane-research 0 · .lane-design 1<br/>.lane-planning 2 · .lane-implementation 3<br/>each sets --lane"]
  CD[".card<br/>+ .card-live · .card-blocked"]
  SC[".stats-card<br/>.stats-bar / .stats-chart SVG"]
  CH[".col-head · .col-section · .col-link · .col-file-body"]

  W --> R --> RS
  W --> S --> ST --> U --> P
  SH -.-> ST
  KB -.-> U
  KT -.-> U
  KF -.-> U
  UW -.-> U
  P --> C
  C --> L --> LN
  LP -.-> LN
  LN --> CD
  C --> CD
  C --> SC
  P --> CH
```

### Custom-property flow

Geometry lives in CSS; Go only says *whether* the board is away
(`.strip-shifted`), never how many pixels.

```mermaid
flowchart LR
  A["--shell-chrome: 5.75rem"] --> A1[".workspace height"]
  B["--strip-gap: 1.25rem"] --> B1[".strip gap"]
  B --> SHIFT["--shift"]
  C["--board-w: 100%"] --> C1[".kind-board flex-basis"]
  C --> SHIFT
  SHIFT --> D[".strip transform<br/>+ keyframes strip-slide"]
  E["--lane-min: 11rem"] --> F["--lane-step"]
  F --> G[".lane margin-left and width"]
  H["--lane: 0..3<br/>from .lane-PHASE"] --> G
  I["--series: colour<br/>from .series-NAME"] --> J[".stats-line stroke<br/>.stats-swatch background"]
```

Because exactly one unit ever leaves the stage, the offset is a single width —
the board's — not a sum over the kinds scrolled past. One property carries it.

Two details that are structural rather than scripted:

* `overflow-x: clip` on `.stage`, not `hidden`: `hidden` would make the stage a
  scroll container and drag the vertical axis away from `visible`. With `clip`
  there is no scroll container at all, so "the strip cannot be panned" needs no
  wheel handler.
* The slide is a **keyframe animation**, not a transition: htmx replaces the
  whole `#strip`, and a freshly inserted element has no previous value to
  transition from.

### The numbers, in one place

| Name | Value | Where |
|---|---|---|
| `--shell-chrome` | `5.75rem` | `.workspace` height subtrahend |
| `--strip-gap` | `1.25rem` | gap between units, part of `--shift` |
| `--board-w` | `100%` | board unit width, part of `--shift` |
| `.kind-task` | `min(100%, 22rem)` | same width as the side panel |
| `.kind-file` | `min(100%, 44rem)` | |
| `.rail` | `w-28` (7rem) | outside the stage |
| `--lane-min` | `11rem` | minimum card width in a lane |
| lane indent off | `< 14rem` | `@container lanes` |
| stack / unpin | `< 64rem` (`lg`) | strip stacks, panes grow, page scrolls |
| slide / entrance | `260ms` / `220ms` | both off under `prefers-reduced-motion` |
| `maxChainDepth` | `16` | `internal/web/chain.go` |

---

## 5. Go view types

What the templates are handed. `WorkspaceView` is the only thing `board.html`
and `_workspace.html` ever see.

```mermaid
classDiagram
  direction TB

  class WorkspaceView {
    +Title string
    +PollSeconds int
    +Shifted bool
  }
  class ProjectView {
    +Resolved bool
    +Root string
    +TasksDir string
    +TaskCount int
  }
  class BoardView {
    +PollSeconds int
    +Generated string
    +Title string
  }
  class RailEntryView {
    +Kind string
    +Label string
    +Ref string
    +Href string
    +HXGet string
    +Current bool
  }
  class ColumnUnitView {
    +Kind string
    +Class string
    +Label string
    +Ref string
    +Template string
    +Working bool
    +Href string
  }
  class ColumnData {
    <<any>>
  }
  class TaskColumnView
  class FileColumnView {
    +Name string
    +RawHref string
    +Content string
  }
  class ColumnView {
    +Status string
    +Title string
    +Count int
  }
  class PhaseLaneView {
    +Phase string
    +Count int
  }
  class CardView {
    +ID string
    +Title string
    +Href string
    +HXGet string
    +Priority string
    +Status string
    +Blocked bool
    +Phase string
    +Branch string
  }
  class DetailView {
    +Description string
    +Archived bool
    +Missing bool
    +TotalTokens string
  }
  class StatsCardView {
    +ID string
    +Kind string
    +Title string
  }

  WorkspaceView *-- ProjectView : Project
  WorkspaceView *-- BoardView : Board
  WorkspaceView *-- RailEntryView : Rail 0..17
  WorkspaceView *-- ColumnUnitView : Root 0..1
  WorkspaceView *-- ColumnUnitView : Columns 0..16

  BoardView *-- ProjectView : Project
  BoardView *-- ColumnView : Columns 3
  BoardView *-- DangerItem : DangerZone
  BoardView o-- DetailView : Panel

  ColumnView *-- CardView : Cards
  ColumnView *-- PhaseLaneView : Lanes, In progress only
  ColumnView *-- StatsCardView : Stats, Done only
  PhaseLaneView *-- CardView : Cards
  CardView *-- CardView : Subtasks
  CardView *-- BlockerView : Blockers

  ColumnUnitView o-- ColumnData : Data
  ColumnData <|.. TaskColumnView
  ColumnData <|.. FileColumnView
  TaskColumnView *-- DetailView : Detail

  DetailView *-- ProjectView : Project
  DetailView *-- CardView : Card
  DetailView *-- RelationView : Relations
  DetailView *-- FileLinkView : Files
  DetailView *-- PhaseView : Phases
  PhaseView *-- PhaseRunView : Runs

  StatsCardView *-- StatsBarView : Bars
  StatsCardView o-- StatsChartView : Chart
  StatsChartView *-- StatsLineGroupView : Groups
  StatsChartView *-- StatsSeriesView : Legend
  StatsChartView *-- StatsDayView : Days
  StatsLineGroupView *-- StatsSeriesView : Lines
```

There is no inheritance anywhere — Go has none and the view layer uses none.
The one polymorphic seam is `ColumnUnitView.Data any`, filled by the kind
registry (§6). Two reuse facts carry the layout's promises:

* **`DetailView` is shared** by the side panel, a task column and the panel's
  plate — which is why a task column looks the same at 22rem wherever it sits.
* **`CardView` is recursive** (`Subtasks []CardView`), which is how a nested
  subtask row renders with the same fields as a top-level card.

---

## 6. The kind registry and the chain

One entry per column type declares the URL tag, the width class, the rail
label, the fragment, and how a ref becomes view data. Adding a kind is one
entry, one fragment and one branch in `_col.html`; the chain, the layout and
the routes are not reopened.

```mermaid
flowchart LR
  subgraph reg["columnKinds, kinds.go, immutable after init"]
    T["KindTask t<br/>.kind-task · task · _col_task.html<br/>resolveTaskColumn"]
    F["KindFile f<br/>.kind-file · file · _col_file.html<br/>resolveFileColumn"]
    G["KindGraph g<br/>planned: web-backlog-graph"]
  end
  CTX["colCtx<br/>Svc · Cfg · Now · TaskID · Ref · Chain · At"]
  CTX --> T --> TD["TaskColumnView<br/>DetailView, links rebased onto here()"]
  CTX --> F --> FD["FileColumnView<br/>name · raw href · escaped content"]
  TD --> UNIT["ColumnUnitView.Data any"]
  FD --> UNIT
  G -.-> UNIT
```

The chain (`chain.go`) is the whole navigation state, carried in the path:

| Operation | Meaning | Used by |
|---|---|---|
| `ParseChain(EscapedPath)` | URL → `Chain`; any failure is one 404 | both workspace handlers |
| `Append(kind, ref)` | open something from a column | file and subtask links |
| `TruncateTo(n)` | roll back to column `n` | rail entries, root column href |
| `TaskAt(i)` | nearest task column before `i`, else root | a file column's owner |
| `Path()` / `Fragment()` | `/tasks/...` / `/strip/tasks/...` | `href` / `hx-get` |

`/tasks/{root}` *is* the depth-0 chain, so there is no `/w/` zero-column form.
Refs are taken by segment index from the **escaped** path and each one is
unescaped on its own, so one `%2F` in a file name cannot forge a segment
boundary; every ref then goes through `task.ValidateNameSegment`.

---

## 7. One request, end to end

```mermaid
sequenceDiagram
  autonumber
  participant B as Browser
  participant M as http.ServeMux
  participant H as handler
  participant K as columnKinds
  participant S as task.Service

  B->>M: GET /strip/tasks/42/w/t/43/f/design.md
  Note over B,M: a click on an .chip file link:<br/>hx-get, hx-target #strip, hx-push-url the page path
  M->>H: workspaceFragment
  H->>H: ParseChain, escaped path, strip the /strip prefix
  H->>H: Resolver.Current, never Get - a GET must not move files
  H->>S: Detail(42) → the panel
  H->>S: BoardSnapshot → the board behind the strip
  H->>K: Resolve kind t, Ref 42, At -1 → the Root column
  loop each column i of the chain
    H->>K: Resolve, TaskID TaskAt(i), Ref, At i
    K->>S: Detail(ref) or ReadTaskFile(TaskID, ref)
  end
  H->>H: newRailEntries, chain truncated to each i
  H-->>B: _workspace.html, the whole #strip
  Note over B: htmx swaps outerHTML and pushes the URL ·<br/>app.js restores every .pane scrollTop ·<br/>the board keeps polling off-screen
```

Any failure on that path — malformed chain, unknown kind, missing task,
missing file, unresolved project — is the same **404**: the chain is part of
the URL, so a chain that names nothing is a URL that names nothing.

---

## 8. Below `lg` (`< 64rem`)

Everything structural reverts, with no JavaScript involved:

```
.workspace  → h-auto, flex-col          .unit  → h-auto
.stage      → h-auto, overflow-x visible .pane → h-auto, overflow-y visible
.strip      → flex-col, transform none, animation none
.kind-*     → flex 0 0 auto             .rail → w-full, flex-row, flex-wrap
```

The strip stacks, the panes grow to their content, and the page scrolls
normally. Under `prefers-reduced-motion: reduce` the slide and the column
entrance are switched off — the project's first such rule.
