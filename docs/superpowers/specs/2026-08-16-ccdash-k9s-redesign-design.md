# ccdash k9s-Style TUI Redesign — Design Spec

**Status:** approved for planning
**Date:** 2026-08-16
**Supersedes:** the Overview screen described in §11 of
`2026-08-16-llm-usage-dashboard-design.md` (Phase 1). Everything below the TUI
layer — ingestion, store, pricing, sources — is unchanged and out of scope.

## Goal

Replace the single static Overview screen with a k9s-style navigator: every
dataset is a resource, every resource is a scrollable table, `:` switches
between them, `enter` drills in, `esc` pops back, and the view refreshes itself
on a timer.

## Non-Goals

- No changes to ingestion, parsing, pricing, or the store schema, except three
  additive indexes (§6.3).
- No new external dependencies. Bubble Tea and Lip Gloss are already present.
- No mouse support. Keyboard only, as k9s is.
- No config file for keybindings or themes. Deferred to a later phase.

---

## 1. What Exists Today

`internal/tui/tui.go` is 411 lines: one `Model`, one `View()` that emits a
single screen top to bottom, and no selection, focus, scrolling, or routing.
`Run` starts Bubble Tea with `tea.WithAltScreen()`.

The redesign replaces this file entirely. `internal/render` survives with two
fixes (§7). `internal/agg` is extended, not rewritten.

## 2. Confirmed Defects and Their Root Causes

Each was reproduced against the live corpus and traced to a specific line
before being written down. The resolution column states whether the redesign
fixes it structurally or requires deliberate work.

### 2.1 The view never fills the terminal

`m.height` is assigned in `Update` and **never read anywhere in the file** — a
single `grep` hit. `View()` emits as many lines as the content needs and stops.
Under `WithAltScreen` with a transparent terminal background, every unwritten
cell shows the desktop.

Width has a parallel defect: it is known but barely used. `chartPanel` caps at
`min(width-2, 72)`, model names are `%-22s`, bars are a literal `16`, project
names truncate at a literal `28`. The layout cannot expand.

Not a stale viewport: `tea.WindowSizeMsg` is handled at line 103 and alt-screen
is entered at line 409. **Resolution: structural** — §4 derives every dimension
from the current `(width, height)` and pads to full height.

### 2.2 First row of every panel is indented by `len(heading) + 2`

Observed as 10 / 12 / 8 columns for `by model` / `by project` / `limits`.

Root cause: `lipgloss.Style.Render` pads **every line of a multi-line block** to
the width of the block's widest line. `dimStyle.Render("\nby model\n")` is three
lines — empty, `by model`, empty — so the trailing empty line is emitted as 8
spaces of padding, and the next `WriteString` lands on it. Raw capture:

```
ESC[38;5;240m ESC[0m        <CR>          <- empty line padded to 8
ESC[38;5;240m by model ESC[0m <CR>
ESC[38;5;240m ESC[0m          claude-fable-5 …   <- 8 padding + the row's own 2
```

**Resolution: constraint.** Newlines must never appear inside a styled
`Render()`. Style the text, emit newlines outside it. This is a standing rule in
`theme.go` (§5.4) because the failure is silent and re-introducible.

### 2.3 Chart y-domain is `[min, max]`, not `[0, max]`

`render.Braille` *does* recompute min and max from the series on every call
(render.go:94-99). The defect is the choice of domain: normalizing to
`[min, max]` forces the lowest sample onto the bottom edge and the highest onto
the top edge, so the peak always appears to touch or clip the top border. There
are also no axis labels, making magnitude unreadable.

A related observation — "the series occupies only the right 20%" — is not a
bug. `Braille` interpolates the whole series across the full pixel width; the
data really is near-flat for most of an 11-month range. Correct domain plus
labels makes that legible rather than looking broken.

**Resolution: deliberate.** §7.1.

### 2.4 Sparklines are normalized per row

`render.Sparkline` computes min and max from the values it is handed
(render.go:59-63), so each project's sparkline is scaled to its own range. Bar
heights are therefore **not comparable between rows**, which is a more
misleading failure than an obviously broken one.

The column is otherwise correctly a fixed width for every row, because its
length is the bucket count, not a magnitude. It lacks a header label.

**Resolution: deliberate.** §7.2.

### 2.5 Gauges have no track

`render.Bar` already pads to the full field width (render.go:49-51), so
alignment is correct, but unfilled cells are spaces. 15% and 16% are visually
indistinguishable and there is no 0–100% reference. **Resolution: deliberate**,
§7.3.

### 2.6 Panels truncate at six rows with no remainder

`modelPanel` and `projectPanel` both `break` at `i >= 6`. Model rows sum to
$2,004.32 against a $2,022.96 header; project rows sum to $1,511.96, hiding 25%
of total cost with no indication.

**Resolution: structural.** Tables scroll (§5.3), so every row is present and
column sums reconcile with the header by definition. No "other" row is needed.

### 2.7 Smaller issues

| Issue | Location | Resolution |
|---|---|---|
| End date prints no year (`2025-09-24 → 08-16`) | tui.go:168 | Both ends `2006-01-02` |
| Project paths truncate mid-word from the left | `truncateLeft`, tui.go:390 | Truncate on `/` boundaries (§7.4) |
| Active tool/range shown in header, not footer | tui.go:160-165 | Header info block (§4.1) keeps it, footer shows contextual keys |
| Unpriced warning sits far from the panel it invalidates | tui.go:190 | Promoted to a first-class `:unpriced` view, and surfaced in the header info block |

---

## 3. Architecture

Four pieces, each independently testable.

```
                    ┌───────────────────────────┐
   tea.Msg ────────►│  App (internal/tui)       │
                    │  view stack, command bar, │
                    │  global keys, refresh tick│
                    └────────────┬──────────────┘
                                 │ delegates body
                    ┌────────────▼──────────────┐
                    │  Table (internal/tui)     │
                    │  sort, filter, scroll,    │
                    │  selection                │
                    └────────────┬──────────────┘
                                 │ asks for data
                    ┌────────────▼──────────────┐
                    │  View (interface)         │
                    │  one impl per resource    │
                    └────────────┬──────────────┘
                                 │ queries
                    ┌────────────▼──────────────┐
                    │  agg (existing + new)     │
                    └───────────────────────────┘
```

The `View` interface is the extension point. Adding a resource is one file that
returns columns and rows; it inherits sorting, filtering, scrolling, selection,
and layout for free.

### 3.1 The View interface

```go
// View is one navigable resource. Implementations are stateless with respect
// to presentation: sorting, filtering and scrolling belong to Table.
type View interface {
    // Title is the resource name shown in the body border, e.g. "Projects".
    Title() string

    // Columns describes the schema. Order is display order.
    Columns() []Column

    // Rows fetches the full result set for the current scope. Views do not
    // paginate; Table scrolls. See Paginator for the one exception.
    Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error)

    // Drill returns the View entered by pressing enter on row, and false when
    // the resource is a leaf.
    Drill(row Row) (View, bool)
}

// Paginator is implemented only by views whose result set is too large to hold
// at once. Table type-asserts for it; a view that does not implement it is
// fetched whole via Rows. Only the requests view implements this.
type Paginator interface {
    Page(db *sql.DB, pricing *model.Pricing, scope Scope, offset, limit int) ([]Row, bool, error)
    PageSize() int
}

type Column struct {
    Title string
    Align Alignment    // AlignLeft | AlignRight
    Width int          // 0 = flexible, share remaining space
    Sort  SortKind     // SortString | SortNumeric | SortTime
    Kind  CellKind     // CellText | CellNumber | CellBar | CellSparkline
}

type Row struct {
    Key   string       // stable identity for selection across refreshes
    Cells []Cell
}

type Cell struct {
    Text   string      // display text for CellText / CellNumber
    Value  float64     // numeric value for sorting, and the fill for CellBar
    Series []float64   // raw series for CellSparkline; rendered by Table, not
                       // by the view, because the domain is shared across rows
}
```

`Scope` carries the global filter plus any drill-down narrowing:

```go
type Scope struct {
    agg.Filter                 // From, To, Tool, Project
    Session  string            // set when drilled into a session
    Agent    string
    Workflow string
    ModelID  string
}
```

### 3.2 The view stack

Navigation is a stack of `(View, Scope, selectedKey)`. `enter` pushes,
`esc` pops, `:` replaces the whole stack with a single new root. The breadcrumb
renders the stack left to right.

Selection is restored by `Row.Key` after every refresh, so a 2-second tick never
moves the cursor out from under the user. If the key disappears, selection
clamps to the nearest index.

---

## 4. Layout

Every frame recomputes from the current `(width, height)`. No fixed widths
survive anywhere in the TUI.

```
row 0..3      header info block        (fixed 4 rows)
row 4         body top border          (1 row)
row 5..h-3    table viewport           (h - 7 rows, flexes)
row h-2       body bottom border       (1 row)
row h-1       footer                   (1 row)
```

When `height < 12` the header collapses to a single line and the body takes the
remainder. When `width < 60` the header drops the logo, then the keybind column.

**The rendered frame is always exactly `height` lines of exactly `width`
cells.** Short content is padded with spaces. This is asserted in a test
(§9.2), because it is the invariant that fixes §2.1 and it is easy to regress.

### 4.1 Header info block

```
 Context:  ~/.local/share/ccdash/usage.db   <1> all      ╔═╗╔═╗╔╦╗╔═╗╔═╗╦ ╦
 Range:    all  2025-09-24 → 2026-08-16     <2> claude   ║  ║  ║║╠═╣╚═╗╠═╣
 Tokens:   2.4B          Cost: $2012.27     <3> codex    ╚═╝╚═╝═╩╝╩ ╩╚═╝╩ ╩
 Requests: 23,216        Unpriced: 9        <?> help     rev 0.1.0
```

Left column is context and totals for the current scope. Middle is the tool
filter with the active one highlighted. Right is the logo, dropped first under
width pressure.

Cost is always rendered `$N at API rates` in any context with room for it, and
never the word "spent" — carried forward from the Phase 1 spec.

### 4.2 Body

Border title is `Resource(scope)[count]`, k9s style: `Projects(all)[20]`,
`Requests(sess-4f2a)[312]`. When a filter is active it appears in the title as
`Projects(all)[7/20]`.

### 4.3 Footer

```
 <projects> <metrics/stocks/twn>                    ↻ 2s ago   [enter] sessions
```

Breadcrumb left, refresh age and contextual keys right. Refresh age turns amber
past 30 seconds and red past 5 minutes, so a wedged ticker is visible rather
than silent.

---

## 5. Components

### 5.1 Command bar

`:` opens a prompt in the footer. Input is matched against view names and
aliases; unknown input shows an inline error and leaves the stack untouched.

| Command | Aliases |
|---|---|
| `:projects` | `:proj`, `:p` |
| `:models` | `:model`, `:m` |
| `:sessions` | `:sess`, `:s` |
| `:requests` | `:req`, `:r` |
| `:agents` | `:agent`, `:a` |
| `:workflows` | `:wf`, `:w` |
| `:limits` | `:limit`, `:l` |
| `:days` | `:day`, `:d` |
| `:unpriced` | `:unp`, `:u` |
| `:pulse` | `:pu` |
| `:quit` | `:q` |

### 5.2 Filter

`/` opens a filter prompt. Plain text is a case-insensitive substring match
against the row's first column. A leading `!` inverts. A leading `~` switches to
regex; an invalid pattern shows inline and matches nothing rather than erroring
out. Filtering is client-side over already-fetched rows, so it is instant and
does not re-query.

### 5.3 Table

Owns sorting, filtering, scrolling, and selection. Rendering is a pure function
of `(rows, columns, viewport height, scroll offset, selected index, width)`.

Column widths: fixed-width columns are honored, remaining space is divided among
flexible columns proportionally to their widest cell, with a minimum of 6 cells
each. The first column absorbs any rounding remainder.

Sorting: `s` advances the sort column, `S` reverses direction. The sorted column
shows `↑`/`↓` in its header.

This deliberately departs from k9s, which binds sorting to semantic letters
(`Shift-C` for CPU). Two reasons. Terminals cannot distinguish `Shift-1` from
`!`, so an index-keyed scheme would in practice bind to `!@#$%^`, which varies
by keyboard layout. And ccdash's columns differ per view, so there is no stable
semantic letter to bind. Cycling needs no per-view key table and no layout
assumptions.

Scrolling: `j`/`k`/arrows move one row, `ctrl-f`/`ctrl-b` a page, `g`/`G` jump to
the ends. The viewport follows the selection.

**`:requests` is the one paginated view**, and the only implementor of
`Paginator` (§3.1). 23k rows is well within SQLite's reach but not worth holding
in memory, so it fetches 500 at a time and loads the next page when the
selection reaches the bottom. Every other view fetches its full result set via
`Rows`; none exceeds a few hundred rows.

Filtering a paginated view filters only loaded pages, and the body title marks
this as `Requests(sess-4f2a)[7/500+]` so a partial match is never mistaken for a
complete one.

### 5.4 Theme

All styles live in `theme.go`. The file carries this rule as a comment, and
§9.2 enforces it with a test:

> Never pass a string containing `\n` to a lipgloss `Render()`. Lip Gloss pads
> every line of a multi-line block to the widest line, which silently indents
> whatever is written next. Style the text; emit newlines outside.

### 5.5 Keybindings

The complete set. Anything not listed is unbound.

| Key | Context | Action |
|---|---|---|
| `j` `k` `↓` `↑` | table | Move selection one row |
| `ctrl-f` `ctrl-b` | table | Page down / up |
| `g` `G` | table | Jump to first / last row |
| `enter` | table | Drill into selected row (no-op on a leaf) |
| `esc` | table | Pop the view stack; no-op at root |
| `esc` | prompt | Cancel the command or filter prompt |
| `s` `S` | table | Advance sort column / reverse direction |
| `/` | table | Open the filter prompt |
| `:` | table | Open the command prompt |
| `r` | table | Manual refresh now (respects single-flight, §8) |
| `1` `2` `3` | table | Tool filter: all / claude / codex |
| `d` `w` `m` `a` | table | Range: day / week / month / all |
| `?` | any | Help overlay; any key dismisses |
| `ctrl-c` | any | Quit |
| `:q` | prompt | Quit |

Range and tool keys apply globally and persist across view switches and
drill-downs, so narrowing to `claude` on `:projects` still holds after
`:models`. They are shown in the header info block (§4.1).

---

## 6. Data Layer

### 6.1 Existing, unchanged

`agg.Totals`, `agg.ByDay`, `agg.ByModel`, `agg.ByProject`, `agg.LatestLimits`
keep their signatures. `agg.Filter` gains no fields; drill-down narrowing rides
in `Scope` and is translated to SQL predicates by the new queries.

### 6.2 New queries

```go
func BySession(db *sql.DB, p *model.Pricing, f Filter) ([]SessionBucket, error)
func ByAgent(db *sql.DB, p *model.Pricing, f Filter) ([]AgentBucket, error)
func ByWorkflow(db *sql.DB, p *model.Pricing, f Filter) ([]WorkflowBucket, error)
func Requests(db *sql.DB, p *model.Pricing, f Filter, limit, offset int) ([]RequestRow, error)
func UnpricedList(db *sql.DB) ([]UnpricedRow, error)

type SessionBucket struct {
    Session   string
    Tool      model.Tool
    Project   string
    Started   time.Time
    Ended     time.Time
    Requests  int
    Tokens    int64
    Cost      float64
    Unpriced  int
}

type AgentBucket struct {
    Agent     string
    Workflow  string
    Depth     int
    Requests  int
    Tokens    int64
    Cost      float64
}

type WorkflowBucket struct {
    Workflow  string
    Agents    int
    Requests  int
    Tokens    int64
    Cost      float64
    Started   time.Time
}

type RequestRow struct {
    ID        string
    TS        time.Time
    Tool      model.Tool
    Model     string
    Project   string
    Session   string
    Agent     string
    Input     int64
    Output    int64
    Thinking  int64
    CacheRead int64
    CacheWrite int64
    Cost      float64
    Priced    bool
    Anomaly   bool
}

type UnpricedRow struct {
    Model     string
    Count     int
    Tokens    int64
    FirstSeen time.Time
    LastSeen  time.Time
}
```

Cost stays a query-time computation from the rate table, never stored —
unchanged from Phase 1, so editing prices still never requires a re-ingest.
`Priced` is false when the model has no rate; those rows render their cost cell
as `—` and are counted in the header's `Unpriced` figure. **A row that cannot be
priced is still displayed**, per the Phase 1 constraint.

### 6.3 Indexes

Three additive indexes. No column changes, no migration of existing rows.

```sql
CREATE INDEX IF NOT EXISTS request_session  ON request(session, ts);
CREATE INDEX IF NOT EXISTS request_agent    ON request(agent, ts);
CREATE INDEX IF NOT EXISTS request_workflow ON request(workflow, ts);
```

These go in `schema.go` alongside the existing `CREATE INDEX IF NOT EXISTS`
statements, so they are created on next open for existing databases.

---

## 7. Render Fixes

### 7.1 Braille y-domain

Add an explicit domain rather than deriving `[min, max]`:

```go
func BrailleDomain(series []float64, width, height int, lo, hi float64) string
```

`Braille` keeps its signature and calls `BrailleDomain(series, w, h, 0,
max*1.05)`. The `:pulse` view prints the domain as axis labels:

```
cost / day                                        max $141.02
⣀⣀⣀⣀⣀⣀⣀⣀⣀⣠⢤⣀⣀⣠⣤⠶⣄⣀⣀⣰⢲⡤⠶⠼⠁
2025-09-24                                     $0  2026-08-16
```

### 7.2 Sparkline shared scale

```go
func SparklineDomain(values []float64, lo, hi float64) string
```

Table columns of type sparkline compute one domain across **all** rows and pass
it to every row, making heights comparable. `Sparkline` keeps its per-series
behavior for any caller that genuinely wants it, and the column header is
labelled with the shared max.

### 7.3 Bar track

```go
func BarTrack(fraction float64, width int, track rune) string
```

Unfilled cells render `track` (default `·`) instead of a space, giving a visible
0–100% reference. `Bar` is retained and delegates with `track = ' '`, so
existing tests and callers are untouched.

### 7.4 Path truncation on separators

`truncateLeft` moves into `render` as `TruncatePath(path string, width int)` and
drops whole `/`-separated segments from the left rather than cutting mid-word:

```
before: …ading/stocks/twn/data-cloud   …s/metrics/crypto/data-cloud
after:  …/stocks/twn/data-cloud        …/crypto/data-cloud
```

When the final segment alone exceeds the width it is cut mid-word as a last
resort.

---

## 8. Refresh and Concurrency

A `tea.Tick` every 2 seconds emits `refreshMsg`. The handler runs an incremental
`ingest.Run` followed by the current view's `Rows`, both in a `tea.Cmd`
goroutine, and returns the result as a message. The UI thread never touches the
database.

**Single-flight.** An `inFlight bool` on the app model gates it: a tick arriving
while a refresh is running is dropped, not queued. Manual `r` sets the same
flag. This is what prevents a slow ingest from stacking writers.

**Ticking pauses** while the command bar or filter prompt is open, so input
never fights a re-render.

The store already opens with `journal_mode(WAL)` and `busy_timeout(5000)`, so a
reader is not blocked by the writer and a contended write retries rather than
failing. No store changes are required.

**Failure is visible, not fatal.** A refresh error leaves the last good data on
screen, turns the footer's refresh age red, and shows the error in the footer.
The ticker keeps running so a transient failure self-heals.

---

## 9. Testing

### 9.1 What carries over

All 64 existing tests must stay green. The seven `internal/tui` tests are
rewritten because they assert against the old single-screen `View()`; their
*intent* is preserved as the assertions listed below.

### 9.2 New invariants

| Test | Asserts |
|---|---|
| `TestFrameIsExactlyViewportSized` | Rendered frame is exactly `height` lines of exactly `width` cells, at 80x24, 200x60, and 40x10 |
| `TestNoNewlinesInsideStyledRender` | Parses every `internal/tui` file with `go/ast`, walks each call whose selector is `Render`, and fails on any string-literal argument containing `\n` — guards §2.2 from returning |
| `TestSelectionSurvivesRefresh` | Selected `Row.Key` is still selected after rows are replaced |
| `TestSelectionClampsWhenKeyDisappears` | Selection falls back to nearest index, never out of range |
| `TestTableSortCyclesAndReverses` | `s` advances the column, `S` reverses, header marker follows |
| `TestFilterIsClientSideAndInstant` | Filtering does not re-query |
| `TestDrillPushesAndEscPops` | Stack depth and breadcrumb track navigation |
| `TestRequestsPaginates` | Second page loads at the boundary, no duplicates |
| `TestCostLabelledAtAPIRates` | No rendered surface says "spent" |
| `TestUnpricedRowsAreDisplayed` | An unpriceable row appears with `—`, is not dropped |
| `TestSparklineSharedDomain` | Two rows with equal values render equal glyphs |
| `TestBrailleDomainStartsAtZero` | A flat non-zero series does not fill the plot |
| `TestTruncatePathBreaksOnSeparator` | Output starts at a `/` boundary |
| `TestSingleFlightDropsOverlappingTick` | Second tick during a refresh is dropped |

### 9.3 Manual gate

`ccdash` under a real PTY at 177x58: frame fills the terminal, no desktop bleed,
`:` switches views, `enter`/`esc` navigate, `/` filters, refresh age ticks. The
PTY capture technique used during Phase 1 verification applies
(`script -q /dev/null`).

---

## 10. What Gets Deleted

- `internal/tui/tui.go` in its entirety — replaced by `app.go`, `table.go`,
  `theme.go`, `views/*.go`.
- `usagePanels`, `chartPanel`, `modelPanel`, `projectPanel`, `limitsPanel`, and
  the `lipgloss.JoinHorizontal` two-column layout.
- The `i >= 6` truncation in both panels.
- `truncateLeft` (moves to `render.TruncatePath`).

`internal/render`, `internal/agg`, `internal/store`, `internal/ingest`,
`internal/source/*`, `internal/model` and `cmd/ccdash` are otherwise untouched.
The `ingest`, `limits`, `setup-statusline` and `version` subcommands keep their
current behavior and output.

---

## 11. Stated Assumptions

1. **2 seconds is the right default tick.** Taken from k9s. Not configurable in
   this phase; if it proves noisy the constant is one edit.
2. **`:projects` is the landing view.** Cost attribution is the most common
   entry question. Not configurable in this phase.
3. **`esc` at the root does nothing.** Quitting is `:q` or `ctrl-c`, so a
   reflexive `esc` cannot drop the user out of the app.
4. **Filter matches the first column only.** Multi-column filtering is deferred;
   `~regex` covers most of the gap.
5. **Sorting cycles with `s`/`S` rather than k9s's semantic `Shift-<letter>`.**
   Rationale in §5.3: terminals cannot distinguish `Shift-1` from `!`, and
   ccdash's columns vary per view so no stable letter exists. This is the one
   place the design knowingly diverges from k9s muscle memory.
6. **Range and tool filters are global, not per-view.** Narrowing on one
   resource carries to the next. The alternative — per-view scope — was
   rejected because it makes the header's totals ambiguous about what they
   cover.

## 12. Out of Scope

Config file for keys, colors, and tick rate. Mouse support. Export from the TUI.
Saved filters. Multi-column filtering. A `:contexts` equivalent for switching
between multiple databases. Long-context pricing tiers and Codex `-codex` rates
remain unpriced, as recorded in the Phase 1 spec §13.
