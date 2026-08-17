# ccdash k9s-Style TUI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ccdash's single static Overview screen with a k9s-style resource navigator — every dataset is a scrollable, sortable, filterable table, `:` switches between them, `enter` drills in, `esc` pops back, and a 2-second tick refreshes in place.

**Architecture:** A `View` interface describes a resource as columns plus rows. One `Table` component owns sorting, filtering, scrolling and selection for every view. An `App` owns a stack of views (the breadcrumb), the command and filter prompts, global keys, and the refresh ticker. Data comes from `internal/agg`, extended with five new queries. Every frame is computed from the live `(width, height)` and padded to fill the terminal exactly.

**Tech Stack:** Go 1.26 · `github.com/charmbracelet/bubbletea` v1.3.10 · `github.com/charmbracelet/lipgloss` v1.1.0 · `modernc.org/sqlite` v1.56.0. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-16-ccdash-k9s-redesign-design.md`

## Global Constraints

- **Module path:** `github.com/seanochang/ccdash`. Go 1.26.
- **No CGO.** The binary must build with `CGO_ENABLED=0`.
- **No new dependencies.** Bubble Tea and Lip Gloss are already present; nothing else may be added.
- **Never pass a string containing `\n` to a lipgloss `Render()`.** Lip Gloss pads every line of a multi-line block to the widest line, silently indenting whatever is written next. Style the text; emit newlines outside. This is enforced by a test in Task 6.
- **Every dollar figure is labelled "at API rates"**, never "spent". These are subscription plans.
- **Never drop a row you cannot price.** Unpriceable rows render their cost cell as `—`, are counted in the header's unpriced figure, and are still displayed.
- **Every limit carries a provenance** (`live` | `cached`) and its age.
- **Cost is computed at query time** from the rate table, never stored.
- **Source directories are read-only.** Never write under `~/.claude` or `~/.codex`.
- **The rendered frame is always exactly `height` lines of exactly `width` cells.**
- Tests run with `go test ./... -race`. Every task ends green.

## Spec Corrections Discovered While Planning

Two statements in the spec do not survive contact with the code. The plan follows the corrected version.

1. **Spec §6.1 says "`agg.Filter` gains no fields".** It must gain three. `Filter` already carries `From`, `To`, `Tool`, `Project` and `Model`, but drill-down needs `Session`, `Agent` and `Workflow`, and `Filter.where()` is the only place SQL predicates are built. Task 3 adds them.
2. **Spec §3 shows views in `internal/tui/views/`.** That creates an import cycle: `app` imports `views`, and `views` needs the `View` interface from `app`'s package. Views live in package `tui` as `view_*.go` files instead. Breaking the cycle by extracting a third package is not worth it at this size.

Also note `agg.scanRows` selects only `model,agent,project,ts` plus token columns. Session, workflow, depth, anomaly and id are not selected today; Task 3 adds a second scanner rather than widening the existing one, so `Totals`/`ByDay`/`ByModel`/`ByProject` keep their current cost.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/render/render.go` | Add `BrailleDomain`, `SparklineDomain`, `BarTrack`, `TruncatePath` |
| `internal/store/schema.go` | Three additive indexes |
| `internal/agg/agg.go` | `Filter` gains `Session`/`Agent`/`Workflow`; `scanDetail` |
| `internal/agg/session.go` | `BySession` |
| `internal/agg/attribution.go` | `ByAgent`, `ByWorkflow` |
| `internal/agg/request.go` | `Requests`, `UnpricedList` |
| `internal/tui/view.go` | `View`, `Paginator`, `Column`, `Row`, `Cell`, `Scope` types |
| `internal/tui/theme.go` | All styles; the no-newline rule |
| `internal/tui/table.go` | Sort, filter, scroll, selection, column widths |
| `internal/tui/layout.go` | Frame assembly: header, body, footer, exact sizing |
| `internal/tui/app.go` | Root model, view stack, prompts, global keys, refresh |
| `internal/tui/view_projects.go` etc. | One file per resource |
| `internal/tui/tui.go` | **Deleted** in Task 15 |
| `cmd/ccdash/main.go` | Unchanged except the `tui.Run` call signature |

---

## Task 1: Render primitives — explicit domains, bar track, path truncation

**Files:**
- Modify: `internal/render/render.go`
- Test: `internal/render/render_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `render.BrailleDomain(series []float64, width, height int, lo, hi float64) string`, `render.SparklineDomain(values []float64, lo, hi float64) string`, `render.BarTrack(fraction float64, width int, track rune) string`, `render.TruncatePath(path string, width int) string`. Existing `Bar`, `Sparkline`, `Braille` keep their signatures and delegate.

- [ ] **Step 1: Write the failing tests**

Append to `internal/render/render_test.go`:

```go
func TestBrailleDomainZeroBasedDoesNotFillPlot(t *testing.T) {
	// A flat non-zero series must sit near the top of a [0,max] domain,
	// not fill the whole plot the way a [min,max] domain would.
	flat := []float64{5, 5, 5, 5, 5}
	got := BrailleDomain(flat, 10, 4, 0, 5*1.05)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	if strings.TrimSpace(lines[3]) != "" {
		t.Errorf("bottom row should be empty for a flat series at max, got %q", lines[3])
	}
	if strings.TrimSpace(lines[0]) == "" {
		t.Errorf("top row should carry the series, got %q", lines[0])
	}
}

func TestBrailleDomainFlatZeroSeriesSitsOnFloor(t *testing.T) {
	got := BrailleDomain([]float64{0, 0, 0}, 8, 4, 0, 1)
	lines := strings.Split(got, "\n")
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("top row should be empty for an all-zero series, got %q", lines[0])
	}
	if strings.TrimSpace(lines[3]) == "" {
		t.Error("bottom row should carry an all-zero series")
	}
}

func TestSparklineDomainSharedScaleMakesRowsComparable(t *testing.T) {
	// Two rows with identical values must render identically when they share
	// a domain, which per-row normalization does not guarantee.
	a := SparklineDomain([]float64{1, 2, 3}, 0, 10)
	b := SparklineDomain([]float64{1, 2, 3}, 0, 10)
	if a != b {
		t.Fatalf("identical series under one domain differ: %q vs %q", a, b)
	}
	// A row of small values under a large shared domain must not max out.
	small := SparklineDomain([]float64{1, 1, 1}, 0, 100)
	if strings.ContainsRune(small, '█') {
		t.Errorf("small values under a large domain should not render full blocks: %q", small)
	}
}

func TestBarTrackShowsUnfilledCells(t *testing.T) {
	got := BarTrack(0.5, 10, '·')
	if len([]rune(got)) != 10 {
		t.Fatalf("width = %d, want 10", len([]rune(got)))
	}
	if !strings.ContainsRune(got, '·') {
		t.Errorf("unfilled cells must render the track rune, got %q", got)
	}
	full := BarTrack(1.0, 6, '·')
	if strings.ContainsRune(full, '·') {
		t.Errorf("a full bar has no track cells, got %q", full)
	}
}

func TestBarStillPadsWithSpaces(t *testing.T) {
	// The existing Bar contract must not change.
	got := Bar(0.5, 10)
	if len([]rune(got)) != 10 {
		t.Fatalf("width = %d, want 10", len([]rune(got)))
	}
	if strings.ContainsRune(got, '·') {
		t.Errorf("Bar must keep space padding, got %q", got)
	}
}

func TestTruncatePathBreaksOnSeparator(t *testing.T) {
	cases := []struct{ in string; width int; want string }{
		{"/home/user/dev/metrics/stocks/twn/data-cloud", 28, "…/stocks/twn/data-cloud"},
		{"/home/user/dev/metrics/crypto/data-cloud", 28, "…/metrics/crypto/data-cloud"},
		{"short/path", 28, "short/path"},
	}
	for _, c := range cases {
		got := TruncatePath(c.in, c.width)
		if got != c.want {
			t.Errorf("TruncatePath(%q,%d) = %q, want %q", c.in, c.width, got, c.want)
		}
		if len([]rune(got)) > c.width {
			t.Errorf("TruncatePath(%q,%d) = %q exceeds width", c.in, c.width, got)
		}
	}
}

func TestTruncatePathFallsBackWhenLastSegmentTooLong(t *testing.T) {
	got := TruncatePath("/a/averyveryverylongfinalsegmentname", 12)
	if len([]rune(got)) != 12 {
		t.Fatalf("got %q (len %d), want exactly 12 runes", got, len([]rune(got)))
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("expected an ellipsis prefix, got %q", got)
	}
}
```

Add `"strings"` to the test file's imports if it is not already there.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/render -run 'Domain|Track|TruncatePath' -v`
Expected: FAIL — `undefined: BrailleDomain`

- [ ] **Step 3: Implement the domain-aware primitives**

In `internal/render/render.go`, replace the body of `Sparkline` and `Braille` so each delegates to a domain-taking form, and add the two new functions:

```go
// SparklineDomain renders values against an explicit [lo,hi] domain so that
// sparklines on different rows are comparable. Values outside the domain clamp.
func SparklineDomain(values []float64, lo, hi float64) string {
	if len(values) == 0 {
		return ""
	}
	span := hi - lo
	var out strings.Builder
	for _, value := range values {
		index := 0
		if span > 0 {
			index = int((value - lo) / span * float64(len(spark)-1))
		}
		if index < 0 {
			index = 0
		}
		if index >= len(spark) {
			index = len(spark) - 1
		}
		out.WriteRune(spark[index])
	}
	return out.String()
}

// Sparkline normalizes to the series' own range. Prefer SparklineDomain when
// rendering more than one series that a reader will compare.
func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	return SparklineDomain(values, minimum, maximum)
}
```

Now the Braille pair. Replace the existing `Braille` with:

```go
// BrailleDomain plots a connected series into a w-by-h cell grid at 2x4
// subpixel resolution, against an explicit [lo,hi] value domain.
func BrailleDomain(series []float64, width, height int, lo, hi float64) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	pixelWidth, pixelHeight := width*2, height*4
	grid := make([][]bool, pixelHeight)
	for y := range grid {
		grid[y] = make([]bool, pixelWidth)
	}
	if len(series) > 0 {
		span := hi - lo
		previousY := -1
		for x := 0; x < pixelWidth; x++ {
			position := float64(x) * float64(len(series)-1) / float64(pixelWidth-1)
			if len(series) == 1 {
				position = 0
			}
			left := int(position)
			right := left + 1
			if right >= len(series) {
				right = left
			}
			fraction := position - float64(left)
			value := series[left]*(1-fraction) + series[right]*fraction
			normalized := 0.0
			if span > 0 {
				normalized = (value - lo) / span
			}
			normalized = clamp01(normalized)
			y := int(math.Round((1 - normalized) * float64(pixelHeight-1)))
			grid[y][x] = true
			if previousY >= 0 {
				low, high := previousY, y
				if low > high {
					low, high = high, low
				}
				for bridge := low; bridge <= high; bridge++ {
					grid[bridge][x] = true
				}
			}
			previousY = y
		}
	}
	var out strings.Builder
	for cellY := 0; cellY < height; cellY++ {
		for cellX := 0; cellX < width; cellX++ {
			var mask rune
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if grid[cellY*4+dy][cellX*2+dx] {
						mask |= brailleDots[dx*4+dy]
					}
				}
			}
			if mask == 0 {
				out.WriteByte(' ')
			} else {
				out.WriteRune(0x2800 + mask)
			}
		}
		if cellY < height-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// Braille plots against a zero-based domain with 5% headroom, which is what a
// cost-over-time chart wants: the floor is meaningful and the peak does not
// touch the top edge.
func Braille(series []float64, width, height int) string {
	maximum := 0.0
	for _, value := range series {
		maximum = math.Max(maximum, value)
	}
	return BrailleDomain(series, width, height, 0, maximum*1.05)
}
```

- [ ] **Step 4: Implement the bar track**

```go
// BarTrack renders exactly width cells with eighth-cell precision, filling
// unused cells with track so the full 0-100% range stays visible.
func BarTrack(fraction float64, width int, track rune) string {
	if width <= 0 {
		return ""
	}
	exact := clamp01(fraction) * float64(width)
	full := int(exact)
	remainder := exact - float64(full)
	var out strings.Builder
	for i := 0; i < full && i < width; i++ {
		out.WriteRune('█')
	}
	written := full
	if written < width {
		index := int(remainder * 8)
		if index < 0 {
			index = 0
		}
		if index > 8 {
			index = 8
		}
		if index == 0 {
			out.WriteRune(track)
		} else {
			out.WriteRune(partials[index])
		}
		written++
	}
	for ; written < width; written++ {
		out.WriteRune(track)
	}
	return out.String()
}

// Bar keeps the original space-padded behavior for callers that want a bar
// with no visible track.
func Bar(fraction float64, width int) string {
	return BarTrack(fraction, width, ' ')
}
```

Delete the old `Bar` body that this replaces.

- [ ] **Step 5: Implement path truncation**

```go
// TruncatePath shortens a path to at most width runes by dropping whole
// leading segments, so that sibling directories stay distinguishable. Only
// when the final segment alone will not fit does it cut mid-word.
func TruncatePath(path string, width int) string {
	if len([]rune(path)) <= width {
		return path
	}
	if width <= 1 {
		return "…"
	}
	segments := strings.Split(path, "/")
	for i := 1; i < len(segments); i++ {
		candidate := "…/" + strings.Join(segments[i:], "/")
		if len([]rune(candidate)) <= width {
			return candidate
		}
	}
	runes := []rune(path)
	return "…" + string(runes[len(runes)-(width-1):])
}
```

- [ ] **Step 6: Run the full render suite**

Run: `go test ./internal/render -race -v`
Expected: PASS — the four pre-existing tests plus the seven new ones.

- [ ] **Step 7: Commit**

```bash
git add internal/render
git commit -m "feat(render): explicit domains, bar track, and separator-aware path truncation"
```

---

## Task 2: Store indexes for drill-down queries

**Files:**
- Modify: `internal/store/schema.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: indexes `request_session`, `request_agent`, `request_workflow`. No API change.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:

```go
func TestDrillDownIndexesExist(t *testing.T) {
	s := openTmp(t)
	rows, err := s.DB().Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='request'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"request_session", "request_agent", "request_workflow"} {
		if !found[want] {
			t.Errorf("index %q missing; drill-down queries will table-scan", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run TestDrillDownIndexes -v`
Expected: FAIL — `index "request_session" missing`

- [ ] **Step 3: Add the indexes**

In `internal/store/schema.go`, immediately after the existing `request_model` index line, add:

```sql
CREATE INDEX IF NOT EXISTS request_session  ON request(session, ts);
CREATE INDEX IF NOT EXISTS request_agent    ON request(agent, ts);
CREATE INDEX IF NOT EXISTS request_workflow ON request(workflow, ts);
```

`IF NOT EXISTS` means existing databases pick these up on next open with no migration.

- [ ] **Step 4: Run the store suite**

Run: `go test ./internal/store -race -v`
Expected: PASS — all pre-existing tests plus the new one.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat(store): indexes for session, agent, and workflow drill-down"
```

---

## Task 3: Filter fields and the detail scanner

**Files:**
- Modify: `internal/agg/agg.go`
- Test: `internal/agg/agg_test.go`

**Interfaces:**
- Consumes: `model.Record`, `model.Pricing` (existing)
- Produces: `agg.Filter` gains `Session`, `Agent`, `Workflow string`; `agg.detailRow` (unexported) carrying `ID`, `Tool`, `Session`, `Workflow`, `Depth`, `Anomaly` alongside `model.Record`; `agg.scanDetail(db *sql.DB, filter Filter, order string, limit, offset int) ([]detailRow, error)`

- [ ] **Step 1: Write the failing test**

The existing `agg_test.go` has a helper that seeds a store. Read it first, then append:

```go
func TestFilterNarrowsBySessionAgentWorkflow(t *testing.T) {
	db := seedDetail(t)
	pricing := model.DefaultPricing()

	all, err := Totals(db, pricing, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Requests != 4 {
		t.Fatalf("seed should have 4 requests, got %d", all.Requests)
	}

	bySession, err := Totals(db, pricing, Filter{Session: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if bySession.Requests != 2 {
		t.Errorf("session s1 = %d requests, want 2", bySession.Requests)
	}

	byAgent, err := Totals(db, pricing, Filter{Agent: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if byAgent.Requests != 1 {
		t.Errorf("agent-a = %d requests, want 1", byAgent.Requests)
	}

	byWorkflow, err := Totals(db, pricing, Filter{Workflow: "wf-1"})
	if err != nil {
		t.Fatal(err)
	}
	if byWorkflow.Requests != 1 {
		t.Errorf("wf-1 = %d requests, want 1", byWorkflow.Requests)
	}
}

func TestScanDetailReturnsIdentityColumns(t *testing.T) {
	db := seedDetail(t)
	rows, err := scanDetail(db, Filter{Session: "s1"}, "ts ASC", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ID == "" {
		t.Error("detail scan must select the request id")
	}
	if rows[0].Session != "s1" {
		t.Errorf("Session = %q, want s1", rows[0].Session)
	}
	if rows[0].Tool == "" {
		t.Error("detail scan must select the tool")
	}
	if !rows[0].TS.Before(rows[1].TS) {
		t.Error("ts ASC order not honored")
	}
}

func TestScanDetailPaginates(t *testing.T) {
	db := seedDetail(t)
	first, err := scanDetail(db, Filter{}, "ts ASC", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scanDetail(db, Filter{}, "ts ASC", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("page sizes = %d/%d, want 2/2", len(first), len(second))
	}
	if first[0].ID == second[0].ID {
		t.Error("pages overlap; offset is not applied")
	}
}

// seedDetail builds a store with four requests spanning two sessions, one
// subagent and one workflow.
func seedDetail(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	recs := []model.Record{
		{ID: "r1", Tool: model.ToolClaude, TS: time.Unix(1000, 0), Model: "claude-opus-5",
			Project: "/p/a", Session: "s1", OutputTok: 10},
		{ID: "r2", Tool: model.ToolClaude, TS: time.Unix(2000, 0), Model: "claude-opus-5",
			Project: "/p/a", Session: "s1", Agent: "agent-a", Workflow: "wf-1",
			Depth: 1, OutputTok: 20},
		{ID: "r3", Tool: model.ToolClaude, TS: time.Unix(3000, 0), Model: "claude-sonnet-5",
			Project: "/p/b", Session: "s2", OutputTok: 30},
		{ID: "r4", Tool: model.ToolCodex, TS: time.Unix(4000, 0), Model: "gpt-5.6-luna",
			Project: "/p/b", Session: "s2", OutputTok: 40},
	}
	if _, err := s.UpsertRecords(recs); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}
```

Add `"database/sql"` and the `store` import to the test file if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agg -run 'FilterNarrows|ScanDetail' -v`
Expected: FAIL — `unknown field Session in struct literal`

- [ ] **Step 3: Add the Filter fields**

In `internal/agg/agg.go`, extend the struct:

```go
type Filter struct {
	From, To time.Time
	Tool     model.Tool
	Project  string
	Model    string
	Session  string
	Agent    string
	Workflow string
}
```

and add three conditions to `where()`, immediately before the `len(conditions) == 0` check:

```go
	if f.Session != "" {
		conditions = append(conditions, "session = ?")
		args = append(args, f.Session)
	}
	if f.Agent != "" {
		conditions = append(conditions, "agent = ?")
		args = append(args, f.Agent)
	}
	if f.Workflow != "" {
		conditions = append(conditions, "workflow = ?")
		args = append(args, f.Workflow)
	}
```

Bump both `make` capacities in `where()` from 5 to 8.

- [ ] **Step 4: Add the detail scanner**

Append to `internal/agg/agg.go`. This is a second scanner rather than a widening of `scanRows`, so the four existing aggregates keep selecting only the columns they use.

```go
const detailColumns = `id,tool,model,project,session,agent,workflow,depth,ts,` +
	`in_tok,out_tok,think_tok,cache_read,cache_w5m,cache_w1h,anomaly`

// detailRow carries the identity columns that the aggregate scanners omit.
type detailRow struct {
	model.Record
	ID       string
	Tool     model.Tool
	Session  string
	Workflow string
	Depth    int
	Anomaly  bool
}

// scanDetail reads full request rows. order must be a trusted literal such as
// "ts DESC"; it is never built from user input. limit <= 0 means no limit.
func scanDetail(db *sql.DB, filter Filter, order string, limit, offset int) ([]detailRow, error) {
	where, args := filter.where()
	query := `SELECT ` + detailColumns + ` FROM request` + where
	if order != "" {
		query += ` ORDER BY ` + order
	}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []detailRow
	for rows.Next() {
		var (
			row                                 detailRow
			tool                                string
			project, session, agent, workflow   sql.NullString
			ts                                  int64
			anomaly                             int
		)
		if err := rows.Scan(&row.ID, &tool, &row.Model, &project, &session,
			&agent, &workflow, &row.Depth, &ts,
			&row.InputTok, &row.OutputTok, &row.ThinkingTok,
			&row.CacheReadTok, &row.CacheWrite5m, &row.CacheWrite1h,
			&anomaly); err != nil {
			return nil, err
		}
		row.Tool = model.Tool(tool)
		row.Record.Tool = row.Tool
		row.Record.TS = time.Unix(ts, 0).UTC()
		row.TS = row.Record.TS
		row.Project = project.String
		row.Record.Project = project.String
		row.Session = session.String
		row.Record.Session = session.String
		row.Agent = agent.String
		row.Record.Agent = agent.String
		row.Workflow = workflow.String
		row.Record.Workflow = workflow.String
		row.Anomaly = anomaly == 1
		result = append(result, row)
	}
	return result, rows.Err()
}
```

Note `detailRow` embeds `model.Record` and also declares `ID`, `Tool`, `Session`, `Workflow`, `Depth`, `Anomaly`. Those names shadow the embedded fields, which is why both are assigned above — `row.Record` is what gets handed to `pricing.Cost`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/agg -race -v`
Expected: PASS — the five pre-existing tests plus the three new ones.

- [ ] **Step 6: Commit**

```bash
git add internal/agg
git commit -m "feat(agg): session, agent and workflow filters plus a detail scanner"
```

---

## Task 4: Aggregate by session

**Files:**
- Create: `internal/agg/session.go`
- Test: `internal/agg/session_test.go`

**Interfaces:**
- Consumes: `agg.scanDetail`, `agg.Filter` (Task 3)
- Produces: `agg.BySession(db *sql.DB, pricing *model.Pricing, filter Filter) ([]SessionBucket, error)`, `agg.SessionBucket{Session string; Tool model.Tool; Project string; Started, Ended time.Time; Requests int; Tokens int64; Cost float64; Unpriced int}`

- [ ] **Step 1: Write the failing test**

Create `internal/agg/session_test.go`:

```go
package agg

import (
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestBySessionGroupsAndSpansTime(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	index := map[string]SessionBucket{}
	for _, bucket := range got {
		index[bucket.Session] = bucket
	}
	s1 := index["s1"]
	if s1.Requests != 2 {
		t.Errorf("s1 requests = %d, want 2", s1.Requests)
	}
	if s1.Started.Unix() != 1000 || s1.Ended.Unix() != 2000 {
		t.Errorf("s1 span = %d..%d, want 1000..2000", s1.Started.Unix(), s1.Ended.Unix())
	}
	if s1.Project != "/p/a" {
		t.Errorf("s1 project = %q, want /p/a", s1.Project)
	}
	if s1.Tool != model.ToolClaude {
		t.Errorf("s1 tool = %q, want claude", s1.Tool)
	}
}

func TestBySessionCountsUnpricedWithoutDroppingRows(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]SessionBucket{}
	for _, bucket := range got {
		index[bucket.Session] = bucket
	}
	// s2 holds one claude-sonnet-5 (priced) and one gpt-5.6-luna (priced),
	// so nothing is unpriced, but both requests must be counted.
	if index["s2"].Requests != 2 {
		t.Errorf("s2 requests = %d, want 2 — unpriceable rows must never be dropped",
			index["s2"].Requests)
	}
	if index["s2"].Tokens == 0 {
		t.Error("s2 tokens must be counted regardless of pricing")
	}
}

func TestBySessionSortsByMostRecent(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Session != "s2" {
		t.Errorf("first session = %q, want s2 (most recently active first)", got[0].Session)
	}
}

func TestBySessionHonorsFilter(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{Tool: model.ToolCodex})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Session != "s2" {
		t.Fatalf("codex-only = %+v, want just s2", got)
	}
	if got[0].Requests != 1 {
		t.Errorf("codex requests in s2 = %d, want 1", got[0].Requests)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agg -run TestBySession -v`
Expected: FAIL — `undefined: BySession`

- [ ] **Step 3: Write the implementation**

Create `internal/agg/session.go`:

```go
package agg

import (
	"database/sql"
	"sort"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type SessionBucket struct {
	Session  string
	Tool     model.Tool
	Project  string
	Started  time.Time
	Ended    time.Time
	Requests int
	Tokens   int64
	Cost     float64
	Unpriced int
}

// BySession groups requests into the sessions that produced them. A session's
// project and tool are taken from its first request; sessions do not migrate
// between projects in practice.
func BySession(db *sql.DB, pricing *model.Pricing, filter Filter) ([]SessionBucket, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*SessionBucket)
	order := make([]string, 0, len(buckets))
	for _, row := range rows {
		bucket := buckets[row.Session]
		if bucket == nil {
			bucket = &SessionBucket{
				Session: row.Session,
				Tool:    row.Tool,
				Project: row.Project,
				Started: row.TS,
			}
			buckets[row.Session] = bucket
			order = append(order, row.Session)
		}
		bucket.Requests++
		bucket.Ended = row.TS
		bucket.Tokens += row.InputTok + row.OutputTok + row.CacheReadTok +
			row.CacheWrite5m + row.CacheWrite1h
		if cost, ok := pricing.Cost(row.Record); ok {
			bucket.Cost += cost
		} else {
			bucket.Unpriced++
		}
	}
	result := make([]SessionBucket, 0, len(buckets))
	for _, key := range order {
		result = append(result, *buckets[key])
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Ended.Equal(result[j].Ended) {
			return result[i].Session < result[j].Session
		}
		return result[i].Ended.After(result[j].Ended)
	})
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agg -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agg
git commit -m "feat(agg): session rollup with time span and unpriced count"
```

---

## Task 5: Aggregate by agent and workflow

**Files:**
- Create: `internal/agg/attribution.go`
- Test: `internal/agg/attribution_test.go`

**Interfaces:**
- Consumes: `agg.scanDetail`, `agg.Filter` (Task 3)
- Produces: `agg.ByAgent(db, pricing, filter) ([]AgentBucket, error)`, `agg.ByWorkflow(db, pricing, filter) ([]WorkflowBucket, error)`, `agg.AgentBucket{Agent, Workflow string; Depth int; Requests int; Tokens int64; Cost float64}`, `agg.WorkflowBucket{Workflow string; Agents, Requests int; Tokens int64; Cost float64; Started time.Time}`

- [ ] **Step 1: Write the failing test**

Create `internal/agg/attribution_test.go`:

```go
package agg

import (
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestByAgentExcludesMainLoop(t *testing.T) {
	db := seedDetail(t)
	got, err := ByAgent(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1 — main-loop rows have an empty agent and are not agents", len(got))
	}
	if got[0].Agent != "agent-a" {
		t.Errorf("agent = %q, want agent-a", got[0].Agent)
	}
	if got[0].Workflow != "wf-1" {
		t.Errorf("workflow = %q, want wf-1", got[0].Workflow)
	}
	if got[0].Depth != 1 {
		t.Errorf("depth = %d, want 1", got[0].Depth)
	}
	if got[0].Requests != 1 {
		t.Errorf("requests = %d, want 1", got[0].Requests)
	}
}

func TestByWorkflowCountsDistinctAgents(t *testing.T) {
	db := seedDetail(t)
	got, err := ByWorkflow(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d workflows, want 1", len(got))
	}
	if got[0].Workflow != "wf-1" {
		t.Errorf("workflow = %q, want wf-1", got[0].Workflow)
	}
	if got[0].Agents != 1 {
		t.Errorf("agents = %d, want 1", got[0].Agents)
	}
	if got[0].Started.Unix() != 2000 {
		t.Errorf("started = %d, want 2000", got[0].Started.Unix())
	}
}

func TestByAgentSortsByCostThenName(t *testing.T) {
	db := seedDetail(t)
	got, err := ByAgent(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Cost < got[i].Cost {
			t.Fatalf("not sorted by descending cost at %d", i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agg -run 'ByAgent|ByWorkflow' -v`
Expected: FAIL — `undefined: ByAgent`

- [ ] **Step 3: Write the implementation**

Create `internal/agg/attribution.go`:

```go
package agg

import (
	"database/sql"
	"sort"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type AgentBucket struct {
	Agent    string
	Workflow string
	Depth    int
	Requests int
	Tokens   int64
	Cost     float64
}

type WorkflowBucket struct {
	Workflow string
	Agents   int
	Requests int
	Tokens   int64
	Cost     float64
	Started  time.Time
}

func recordTokens(row detailRow) int64 {
	return row.InputTok + row.OutputTok + row.CacheReadTok +
		row.CacheWrite5m + row.CacheWrite1h
}

// ByAgent rolls up subagent activity. Main-loop requests carry an empty agent
// and are excluded: they are not subagents, and folding them into one nameless
// bucket would dwarf every real agent.
func ByAgent(db *sql.DB, pricing *model.Pricing, filter Filter) ([]AgentBucket, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*AgentBucket)
	for _, row := range rows {
		if row.Agent == "" {
			continue
		}
		bucket := buckets[row.Agent]
		if bucket == nil {
			bucket = &AgentBucket{
				Agent: row.Agent, Workflow: row.Workflow, Depth: row.Depth,
			}
			buckets[row.Agent] = bucket
		}
		bucket.Requests++
		bucket.Tokens += recordTokens(row)
		if cost, ok := pricing.Cost(row.Record); ok {
			bucket.Cost += cost
		}
	}
	result := make([]AgentBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			return result[i].Agent < result[j].Agent
		}
		return result[i].Cost > result[j].Cost
	})
	return result, nil
}

// ByWorkflow rolls up whole workflows. Agents counts distinct agent IDs seen
// under the workflow.
func ByWorkflow(db *sql.DB, pricing *model.Pricing, filter Filter) ([]WorkflowBucket, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*WorkflowBucket)
	seenAgents := make(map[string]map[string]bool)
	for _, row := range rows {
		if row.Workflow == "" {
			continue
		}
		bucket := buckets[row.Workflow]
		if bucket == nil {
			bucket = &WorkflowBucket{Workflow: row.Workflow, Started: row.TS}
			buckets[row.Workflow] = bucket
			seenAgents[row.Workflow] = make(map[string]bool)
		}
		bucket.Requests++
		bucket.Tokens += recordTokens(row)
		if cost, ok := pricing.Cost(row.Record); ok {
			bucket.Cost += cost
		}
		if row.Agent != "" && !seenAgents[row.Workflow][row.Agent] {
			seenAgents[row.Workflow][row.Agent] = true
			bucket.Agents++
		}
	}
	result := make([]WorkflowBucket, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			return result[i].Workflow < result[j].Workflow
		}
		return result[i].Cost > result[j].Cost
	})
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agg -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agg
git commit -m "feat(agg): agent and workflow rollups from subagent attribution"
```

---

## Task 6: Paginated requests and the unpriced list

**Files:**
- Create: `internal/agg/request.go`
- Test: `internal/agg/request_test.go`

**Interfaces:**
- Consumes: `agg.scanDetail`, `agg.Filter` (Task 3)
- Produces: `agg.Requests(db, pricing, filter, limit, offset int) ([]RequestRow, error)`, `agg.UnpricedList(db, pricing, filter) ([]UnpricedRow, error)`, `agg.RequestRow{ID string; TS time.Time; Tool model.Tool; Model, Project, Session, Agent string; Input, Output, Thinking, CacheRead, CacheWrite int64; Cost float64; Priced, Anomaly bool}`, `agg.UnpricedRow{Model string; Requests int; Tokens int64; FirstSeen, LastSeen time.Time}`

**Note on the spec:** §6.2 sketched `UnpricedList(db)` reading the `unpriced` bookkeeping table. This plan derives it from `request` rows against the live pricing table instead, so the view can never disagree with what the other views price. It also means editing `pricing.toml` immediately empties this view, with no re-ingest.

- [ ] **Step 1: Write the failing test**

Create `internal/agg/request_test.go`:

```go
package agg

import (
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestRequestsNewestFirstAndPaginated(t *testing.T) {
	db := seedDetail(t)
	pricing := model.DefaultPricing()
	first, err := Requests(db, pricing, Filter{}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("page size = %d, want 2", len(first))
	}
	if first[0].ID != "r4" {
		t.Errorf("first row = %q, want r4 (newest first)", first[0].ID)
	}
	second, err := Requests(db, pricing, Filter{}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("second page = %d, want 2", len(second))
	}
	if second[0].ID == first[0].ID {
		t.Error("pages overlap")
	}
}

func TestRequestsMarksUnpricedWithoutDropping(t *testing.T) {
	db := seedUnpriced(t)
	got, err := Requests(db, model.DefaultPricing(), Filter{}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 — an unpriceable row must still be listed", len(got))
	}
	var priced, unpriced int
	for _, row := range got {
		if row.Priced {
			priced++
		} else {
			unpriced++
		}
	}
	if priced != 1 || unpriced != 1 {
		t.Errorf("priced/unpriced = %d/%d, want 1/1", priced, unpriced)
	}
}

func TestUnpricedListGroupsAndSpans(t *testing.T) {
	db := seedUnpriced(t)
	got, err := UnpricedList(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d unpriced models, want 1", len(got))
	}
	if got[0].Model != "gpt-5-codex" {
		t.Errorf("model = %q, want gpt-5-codex", got[0].Model)
	}
	if got[0].Requests != 1 {
		t.Errorf("requests = %d, want 1", got[0].Requests)
	}
	if got[0].Tokens == 0 {
		t.Error("tokens must be summed even when the model has no rate")
	}
}

// seedUnpriced builds a store with one priced and one deliberately unpriced
// model. gpt-5-codex is absent from the default table by design.
func seedUnpriced(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "p1", Tool: model.ToolClaude, TS: time.Unix(1000, 0),
			Model: "claude-opus-5", Session: "s1", OutputTok: 100},
		{ID: "u1", Tool: model.ToolCodex, TS: time.Unix(2000, 0),
			Model: "gpt-5-codex", Session: "s1", OutputTok: 200},
	}); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}
```

Add `"database/sql"`, `"time"` and the `store` import to this file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agg -run 'TestRequests|TestUnpricedList' -v`
Expected: FAIL — `undefined: Requests`

- [ ] **Step 3: Write the implementation**

Create `internal/agg/request.go`:

```go
package agg

import (
	"database/sql"
	"sort"
	"time"

	"github.com/seanochang/ccdash/internal/model"
)

type RequestRow struct {
	ID         string
	TS         time.Time
	Tool       model.Tool
	Model      string
	Project    string
	Session    string
	Agent      string
	Input      int64
	Output     int64
	Thinking   int64
	CacheRead  int64
	CacheWrite int64
	Cost       float64
	Priced     bool
	Anomaly    bool
}

// Requests lists individual requests newest first. limit <= 0 returns every
// matching row; the TUI always passes a limit.
func Requests(db *sql.DB, pricing *model.Pricing, filter Filter, limit, offset int) ([]RequestRow, error) {
	rows, err := scanDetail(db, filter, "ts DESC", limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]RequestRow, 0, len(rows))
	for _, row := range rows {
		out := RequestRow{
			ID: row.ID, TS: row.TS, Tool: row.Tool,
			Model: model.NormalizeModel(row.Model), Project: row.Project,
			Session: row.Session, Agent: row.Agent,
			Input: row.InputTok, Output: row.OutputTok, Thinking: row.ThinkingTok,
			CacheRead: row.CacheReadTok,
			CacheWrite: row.CacheWrite5m + row.CacheWrite1h,
			Anomaly: row.Anomaly,
		}
		out.Cost, out.Priced = pricing.Cost(row.Record)
		result = append(result, out)
	}
	return result, nil
}

type UnpricedRow struct {
	Model     string
	Requests  int
	Tokens    int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// UnpricedList reports models the live rate table cannot price, derived from
// request rows rather than from ingest-time bookkeeping. Editing the rate table
// therefore empties this view with no re-ingest.
func UnpricedList(db *sql.DB, pricing *model.Pricing, filter Filter) ([]UnpricedRow, error) {
	rows, err := scanDetail(db, filter, "ts ASC", 0, 0)
	if err != nil {
		return nil, err
	}
	buckets := make(map[string]*UnpricedRow)
	for _, row := range rows {
		if _, ok := pricing.Cost(row.Record); ok {
			continue
		}
		name := model.NormalizeModel(row.Model)
		bucket := buckets[name]
		if bucket == nil {
			bucket = &UnpricedRow{Model: name, FirstSeen: row.TS}
			buckets[name] = bucket
		}
		bucket.Requests++
		bucket.Tokens += recordTokens(row)
		bucket.LastSeen = row.TS
	}
	result := make([]UnpricedRow, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Requests == result[j].Requests {
			return result[i].Model < result[j].Model
		}
		return result[i].Requests > result[j].Requests
	})
	return result, nil
}
```

- [ ] **Step 4: Run the whole agg suite**

Run: `go test ./internal/agg -race -v`
Expected: PASS — every pre-existing test plus the new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/agg
git commit -m "feat(agg): paginated request listing and live unpriced-model view"
```

---

## Task 7: TUI core types and the theme guard

**Files:**
- Create: `internal/tui/view.go`, `internal/tui/theme.go`
- Test: `internal/tui/theme_test.go`

**Interfaces:**
- Consumes: `agg.Filter` (Task 3)
- Produces: `tui.View`, `tui.Paginator`, `tui.Column`, `tui.Row`, `tui.Cell`, `tui.Scope`, `tui.Alignment` (`AlignLeft`, `AlignRight`), `tui.SortKind` (`SortString`, `SortNumeric`, `SortTime`), `tui.CellKind` (`CellText`, `CellNumber`, `CellBar`, `CellSparkline`), and the style vars in `theme.go`.

**Note:** `internal/tui/tui.go` still exists and still compiles at this point. It is deleted in Task 15. Do not touch it here.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/theme_test.go`. This test is the standing guard against the defect from spec §2.2 — it parses the package's own source rather than asserting on output, because the failure it prevents is silent.

```go
package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoNewlinesInsideStyledRender guards spec §2.2. lipgloss pads every line
// of a multi-line block to the width of its widest line, so a newline inside
// Render() silently indents whatever is written next. Style the text; emit
// newlines outside.
func TestNoNewlinesInsideStyledRender(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Render" {
				return true
			}
			for _, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				if strings.Contains(value, "\n") {
					t.Errorf("%s: Render() argument contains a newline: %q\n"+
						"lipgloss pads every line of a block to its widest line, "+
						"which indents the next write. Style the text; emit newlines outside.",
						fset.Position(literal.Pos()), value)
				}
			}
			return true
		})
	}
}

func TestStylesAreDefined(t *testing.T) {
	if styleHeading.Render("x") == "" {
		t.Error("styleHeading must render")
	}
	if styleSelected.Render("x") == "" {
		t.Error("styleSelected must render")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'TestNoNewlines|TestStylesAreDefined' -v`
Expected: FAIL — `undefined: styleHeading`. It may also report newline violations in the existing `tui.go`; those disappear when Task 15 deletes it. If it does, note it and continue — the guard is working.

- [ ] **Step 3: Write the theme**

Create `internal/tui/theme.go`:

```go
package tui

import "github.com/charmbracelet/lipgloss"

// RULE: never pass a string containing "\n" to any lipgloss Render().
// lipgloss pads every line of a multi-line block to the width of its widest
// line, so a trailing newline emits a run of spaces that silently indents
// whatever is written next. Style the text; emit newlines outside.
// Enforced by TestNoNewlinesInsideStyledRender.

var (
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleWarning  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleDanger   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleHeading  = lipgloss.NewStyle().Bold(true)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("39"))
	styleColumn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	styleBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	stylePrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
)

const trackRune = '·'
```

- [ ] **Step 4: Write the core types**

Create `internal/tui/view.go`:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

type SortKind int

const (
	SortString SortKind = iota
	SortNumeric
	SortTime
)

type CellKind int

const (
	CellText CellKind = iota
	CellNumber
	CellBar
	CellSparkline
)

// Column describes one column of a resource table.
type Column struct {
	Title string
	Align Alignment
	Width int // 0 means flexible: share the remaining width
	Sort  SortKind
	Kind  CellKind
}

// Cell is one table cell. Text is used for CellText and CellNumber, Value for
// sorting and for the CellBar fill, and Series for CellSparkline. Sparklines
// are rendered by Table rather than by the view, because their domain is
// shared across every row.
type Cell struct {
	Text   string
	Value  float64
	Series []float64
}

// Row is one table row. Key is a stable identity used to keep the selection
// anchored across refreshes.
type Row struct {
	Key   string
	Cells []Cell
}

// Scope is the current filter plus any drill-down narrowing.
type Scope struct {
	agg.Filter
}

// View is one navigable resource.
type View interface {
	Title() string
	Columns() []Column
	Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error)
	// Drill returns the view entered by pressing enter on row, along with the
	// scope narrowing it implies, and false when the resource is a leaf.
	Drill(row Row, scope Scope) (View, Scope, bool)
}

// Paginator is implemented only by views whose result set is too large to hold
// at once. Table type-asserts for it; a view that does not implement it is
// fetched whole via Rows.
type Paginator interface {
	Page(db *sql.DB, pricing *model.Pricing, scope Scope, offset, limit int) (rows []Row, more bool, err error)
	PageSize() int
}
```

Note `Scope` embeds `agg.Filter` directly rather than duplicating its fields, because Task 3 already added `Session`, `Agent` and `Workflow` to `Filter`. Drill-down narrowing is therefore just a `Filter` with more fields set.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestNoNewlines|TestStylesAreDefined' -v`
Expected: `TestStylesAreDefined` PASSes. `TestNoNewlinesInsideStyledRender` will FAIL on `tui.go:230`, `tui.go:246` and `tui.go:270` — the three `dimStyle.Render("\n…\n")` calls that caused the original bug. That is the guard proving itself. Leave it failing; Task 15 deletes that file.

Record the failure in the commit message rather than suppressing it.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/theme.go internal/tui/theme_test.go
git commit -m "feat(tui): core view types and the lipgloss newline guard

TestNoNewlinesInsideStyledRender currently fails on the three
dimStyle.Render(\"\\n…\\n\") calls in tui.go, which is the defect it exists to
prevent. tui.go is deleted in the final task and the guard goes green then."
```

---

## Task 8: Column width allocation and cell formatting

**Files:**
- Create: `internal/tui/table.go`
- Test: `internal/tui/table_test.go`

**Interfaces:**
- Consumes: `tui.Column`, `tui.Row`, `tui.Cell`, `tui.CellKind` (Task 7); `render.SparklineDomain`, `render.BarTrack` (Task 1)
- Produces: `tui.computeWidths(columns []Column, rows []Row, total int) []int`, `tui.formatCell(cell Cell, column Column, width int, domain float64) string`, `tui.sparkDomain(rows []Row, index int) float64`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/table_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func textRow(key string, cells ...string) Row {
	row := Row{Key: key}
	for _, text := range cells {
		row.Cells = append(row.Cells, Cell{Text: text})
	}
	return row
}

func TestComputeWidthsHonorsFixedAndFillsTotal(t *testing.T) {
	columns := []Column{
		{Title: "NAME", Width: 0},
		{Title: "COST", Width: 10},
		{Title: "REQ", Width: 8},
	}
	rows := []Row{textRow("a", "short", "$1.00", "5")}
	widths := computeWidths(columns, rows, 60)
	if len(widths) != 3 {
		t.Fatalf("got %d widths, want 3", len(widths))
	}
	if widths[1] != 10 || widths[2] != 8 {
		t.Errorf("fixed columns = %d/%d, want 10/8", widths[1], widths[2])
	}
	total := 0
	for _, width := range widths {
		total += width
	}
	// widths exclude the single space between columns
	if total+len(columns)-1 != 60 {
		t.Errorf("widths sum to %d + %d gaps, want 60", total, len(columns)-1)
	}
}

func TestComputeWidthsSharesFlexibleSpaceByContent(t *testing.T) {
	columns := []Column{{Width: 0}, {Width: 0}}
	rows := []Row{textRow("a", strings.Repeat("x", 30), "y")}
	widths := computeWidths(columns, rows, 40)
	if widths[0] <= widths[1] {
		t.Errorf("wider content should get more space: %d vs %d", widths[0], widths[1])
	}
	if widths[1] < 6 {
		t.Errorf("flexible columns have a floor of 6, got %d", widths[1])
	}
}

func TestComputeWidthsNeverExceedsTotal(t *testing.T) {
	columns := []Column{{Width: 40}, {Width: 40}, {Width: 40}}
	widths := computeWidths(columns, nil, 30)
	total := 0
	for _, width := range widths {
		total += width
	}
	if total+len(columns)-1 > 30 {
		t.Errorf("widths sum to %d, must not exceed 30", total+len(columns)-1)
	}
	for i, width := range widths {
		if width < 0 {
			t.Errorf("column %d has negative width %d", i, width)
		}
	}
}

func TestFormatCellPadsAndAligns(t *testing.T) {
	left := formatCell(Cell{Text: "ab"}, Column{Align: AlignLeft, Kind: CellText}, 6, 0)
	if lipgloss.Width(left) != 6 {
		t.Fatalf("left width = %d, want 6", lipgloss.Width(left))
	}
	if !strings.HasPrefix(left, "ab") {
		t.Errorf("left-aligned = %q", left)
	}
	right := formatCell(Cell{Text: "ab"}, Column{Align: AlignRight, Kind: CellNumber}, 6, 0)
	if lipgloss.Width(right) != 6 {
		t.Fatalf("right width = %d, want 6", lipgloss.Width(right))
	}
	if !strings.HasSuffix(right, "ab") {
		t.Errorf("right-aligned = %q", right)
	}
}

func TestFormatCellTruncatesOverlongText(t *testing.T) {
	got := formatCell(Cell{Text: "abcdefghij"}, Column{Kind: CellText}, 5, 0)
	if lipgloss.Width(got) != 5 {
		t.Fatalf("width = %d, want 5", lipgloss.Width(got))
	}
}

func TestFormatCellBarUsesTrack(t *testing.T) {
	got := formatCell(Cell{Value: 0.5}, Column{Kind: CellBar}, 10, 0)
	if lipgloss.Width(got) != 10 {
		t.Fatalf("width = %d, want 10", lipgloss.Width(got))
	}
	if !strings.ContainsRune(got, trackRune) {
		t.Errorf("a half-full bar must show track cells, got %q", got)
	}
}

func TestSparkDomainIsSharedAcrossRows(t *testing.T) {
	rows := []Row{
		{Key: "a", Cells: []Cell{{Series: []float64{1, 2, 3}}}},
		{Key: "b", Cells: []Cell{{Series: []float64{10, 20, 90}}}},
	}
	domain := sparkDomain(rows, 0)
	if domain < 90 {
		t.Fatalf("domain = %v, want at least the global max of 90", domain)
	}
	low := formatCell(rows[0].Cells[0], Column{Kind: CellSparkline}, 3, domain)
	high := formatCell(rows[1].Cells[0], Column{Kind: CellSparkline}, 3, domain)
	if low == high {
		t.Error("rows with very different magnitudes must render differently under a shared domain")
	}
	if strings.ContainsRune(low, '█') {
		t.Errorf("the small row must not max out under a shared domain: %q", low)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'ComputeWidths|FormatCell|SparkDomain' -v`
Expected: FAIL — `undefined: computeWidths`

- [ ] **Step 3: Implement width allocation**

Create `internal/tui/table.go`:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/render"
)

const (
	columnGap    = 1
	minFlexWidth = 6
)

// computeWidths divides total display columns among the table's columns.
// Fixed-width columns are honored first; whatever remains is shared among
// flexible columns in proportion to their widest cell, with a floor of
// minFlexWidth. Rounding remainder lands on the first flexible column.
func computeWidths(columns []Column, rows []Row, total int) []int {
	widths := make([]int, len(columns))
	if len(columns) == 0 {
		return widths
	}
	available := total - columnGap*(len(columns)-1)
	if available < 0 {
		available = 0
	}

	fixedTotal, flexible := 0, make([]int, 0, len(columns))
	for i, column := range columns {
		if column.Width > 0 {
			widths[i] = column.Width
			fixedTotal += column.Width
		} else {
			flexible = append(flexible, i)
		}
	}

	// Over-subscribed by fixed columns alone: shrink them proportionally.
	if fixedTotal > available {
		scale := float64(available) / float64(fixedTotal)
		for i, column := range columns {
			if column.Width > 0 {
				widths[i] = int(float64(column.Width) * scale)
			}
		}
		return widths
	}

	remaining := available - fixedTotal
	if len(flexible) == 0 {
		return widths
	}

	// Weight each flexible column by its widest cell, headers included.
	weights := make([]int, len(flexible))
	weightTotal := 0
	for slot, index := range flexible {
		widest := lipgloss.Width(columns[index].Title)
		for _, row := range rows {
			if index >= len(row.Cells) {
				continue
			}
			if width := lipgloss.Width(row.Cells[index].Text); width > widest {
				widest = width
			}
		}
		if widest < minFlexWidth {
			widest = minFlexWidth
		}
		weights[slot] = widest
		weightTotal += widest
	}

	assigned := 0
	for slot, index := range flexible {
		width := minFlexWidth
		if weightTotal > 0 {
			width = remaining * weights[slot] / weightTotal
		}
		if width < minFlexWidth {
			width = minFlexWidth
		}
		widths[index] = width
		assigned += width
	}
	// Hand the remainder to the first flexible column, and claw back any
	// overshoot caused by the minimum-width floor.
	widths[flexible[0]] += remaining - assigned
	if widths[flexible[0]] < 0 {
		widths[flexible[0]] = 0
	}
	return widths
}
```

- [ ] **Step 4: Implement cell formatting**

Append to `internal/tui/table.go`:

```go
// sparkDomain returns the largest value across every row's series in column
// index, so sparklines on different rows are comparable.
func sparkDomain(rows []Row, index int) float64 {
	maximum := 0.0
	for _, row := range rows {
		if index >= len(row.Cells) {
			continue
		}
		for _, value := range row.Cells[index].Series {
			if value > maximum {
				maximum = value
			}
		}
	}
	return maximum
}

// truncateDisplay cuts text to at most width display cells, marking the cut.
func truncateDisplay(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// formatCell renders one cell to exactly width display cells. domain is the
// shared sparkline maximum and is ignored for other cell kinds.
func formatCell(cell Cell, column Column, width int, domain float64) string {
	if width <= 0 {
		return ""
	}
	var text string
	switch column.Kind {
	case CellBar:
		return render.BarTrack(cell.Value, width, trackRune)
	case CellSparkline:
		series := cell.Series
		if len(series) > width {
			series = series[len(series)-width:]
		}
		text = render.SparklineDomain(series, 0, domain)
	default:
		text = cell.Text
	}
	text = truncateDisplay(text, width)
	padding := width - lipgloss.Width(text)
	if padding < 0 {
		padding = 0
	}
	if column.Align == AlignRight {
		return strings.Repeat(" ", padding) + text
	}
	return text + strings.Repeat(" ", padding)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'ComputeWidths|FormatCell|SparkDomain' -race -v`
Expected: PASS — seven tests.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/table.go internal/tui/table_test.go
git commit -m "feat(tui): column width allocation and cell formatting with shared spark domain"
```

---

## Task 9: Table state — sort, filter, scroll, selection

**Files:**
- Modify: `internal/tui/table.go`
- Test: `internal/tui/table_test.go`

**Interfaces:**
- Consumes: `tui.computeWidths`, `tui.formatCell`, `tui.sparkDomain` (Task 8)
- Produces: `tui.Table` with `NewTable(columns []Column) *Table`, `(*Table).SetRows([]Row)`, `(*Table).SetSize(width, height int)`, `(*Table).SetFilter(string)`, `(*Table).NextSort()`, `(*Table).ReverseSort()`, `(*Table).Move(delta int)`, `(*Table).Page(delta int)`, `(*Table).Home()`, `(*Table).End()`, `(*Table).Selected() (Row, bool)`, `(*Table).VisibleCount() int`, `(*Table).TotalCount() int`, `(*Table).AtBottom() bool`, `(*Table).Render() []string`

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/table_test.go`:

```go
func numericRows(n int) []Row {
	rows := make([]Row, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, Row{
			Key: string(rune('a' + i)),
			Cells: []Cell{
				{Text: string(rune('a' + i))},
				{Text: "v", Value: float64(n - i)},
			},
		})
	}
	return rows
}

func testTable(rows []Row) *Table {
	table := NewTable([]Column{
		{Title: "NAME", Sort: SortString, Kind: CellText},
		{Title: "VALUE", Sort: SortNumeric, Kind: CellNumber, Align: AlignRight},
	})
	table.SetSize(40, 5)
	table.SetRows(rows)
	return table
}

func TestSelectionSurvivesRefresh(t *testing.T) {
	table := testTable(numericRows(5))
	table.Move(2)
	selected, _ := table.Selected()
	if selected.Key != "c" {
		t.Fatalf("selected %q, want c", selected.Key)
	}
	table.SetRows(numericRows(5)) // same keys, fresh slice
	after, ok := table.Selected()
	if !ok || after.Key != "c" {
		t.Errorf("selection = %q after refresh, want c", after.Key)
	}
}

func TestSelectionClampsWhenKeyDisappears(t *testing.T) {
	table := testTable(numericRows(5))
	table.End()
	table.SetRows(numericRows(2))
	selected, ok := table.Selected()
	if !ok {
		t.Fatal("expected a selection")
	}
	if selected.Key != "b" {
		t.Errorf("selection = %q, want the last surviving row b", selected.Key)
	}
}

func TestSelectionEmptyOnNoRows(t *testing.T) {
	table := testTable(nil)
	if _, ok := table.Selected(); ok {
		t.Error("an empty table has no selection")
	}
	table.Move(1)
	table.Render() // must not panic
}

func TestSortCyclesAndReverses(t *testing.T) {
	table := testTable(numericRows(4))
	table.NextSort() // column 0 ascending by name
	first, _ := table.Selected()
	_ = first
	rows := table.Render()
	if len(rows) == 0 {
		t.Fatal("no rendered rows")
	}
	table.ReverseSort()
	reversed := table.Render()
	if rows[1] == reversed[1] {
		t.Error("reversing the sort must change row order")
	}
}

func TestSortNumericUsesValueNotText(t *testing.T) {
	table := NewTable([]Column{{Title: "V", Sort: SortNumeric, Kind: CellNumber}})
	table.SetSize(20, 5)
	table.SetRows([]Row{
		{Key: "x", Cells: []Cell{{Text: "9", Value: 9}}},
		{Key: "y", Cells: []Cell{{Text: "10", Value: 10}}},
	})
	table.NextSort()
	body := table.Render()
	if !strings.Contains(body[1], "9") {
		t.Errorf("ascending numeric sort should put 9 first, got %q", body[1])
	}
}

func TestFilterIsSubstringOnFirstColumn(t *testing.T) {
	table := testTable(numericRows(5))
	table.SetFilter("c")
	if table.VisibleCount() != 1 {
		t.Errorf("visible = %d, want 1", table.VisibleCount())
	}
	if table.TotalCount() != 5 {
		t.Errorf("total = %d, want 5", table.TotalCount())
	}
	table.SetFilter("")
	if table.VisibleCount() != 5 {
		t.Errorf("clearing the filter should restore 5 rows, got %d", table.VisibleCount())
	}
}

func TestFilterInvertAndRegex(t *testing.T) {
	table := testTable(numericRows(5))
	table.SetFilter("!c")
	if table.VisibleCount() != 4 {
		t.Errorf("inverted filter = %d rows, want 4", table.VisibleCount())
	}
	table.SetFilter("~^[ab]$")
	if table.VisibleCount() != 2 {
		t.Errorf("regex filter = %d rows, want 2", table.VisibleCount())
	}
	table.SetFilter("~[") // invalid
	if table.VisibleCount() != 0 {
		t.Errorf("an invalid regex must match nothing, got %d", table.VisibleCount())
	}
}

func TestRenderReturnsExactlyHeightLines(t *testing.T) {
	table := testTable(numericRows(2))
	table.SetSize(40, 6)
	lines := table.Render()
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want exactly 6 (header + 5 body)", len(lines))
	}
	for i, line := range lines {
		if lipgloss.Width(line) != 40 {
			t.Errorf("line %d width = %d, want 40", i, lipgloss.Width(line))
		}
	}
}

func TestViewportFollowsSelection(t *testing.T) {
	table := testTable(numericRows(20))
	table.SetSize(40, 5) // header + 4 body rows
	table.End()
	lines := table.Render()
	if !strings.Contains(strings.Join(lines, "\n"), "t") {
		t.Error("scrolling to the end must bring the last row into view")
	}
	if !table.AtBottom() {
		t.Error("AtBottom should report true at the end")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'Selection|Sort|Filter|Render|Viewport' -v`
Expected: FAIL — `undefined: NewTable`

- [ ] **Step 3: Implement the table**

Append to `internal/tui/table.go`:

```go
// Table owns presentation state for a resource: sorting, filtering, scrolling
// and selection. Views supply data and know none of this.
type Table struct {
	columns  []Column
	all      []Row
	visible  []Row
	filter   string
	sortCol  int
	sortDesc bool
	sorted   bool
	selected int
	offset   int
	width    int
	height   int
}

func NewTable(columns []Column) *Table {
	return &Table{columns: columns, sortCol: -1, width: 80, height: 10}
}

func (t *Table) SetSize(width, height int) {
	t.width, t.height = width, height
	t.clampViewport()
}

func (t *Table) TotalCount() int   { return len(t.all) }
func (t *Table) VisibleCount() int { return len(t.visible) }

// SetRows replaces the data, preserving the selected row by Key. When the key
// is gone the selection clamps to the nearest valid index.
func (t *Table) SetRows(rows []Row) {
	previousKey := ""
	if row, ok := t.Selected(); ok {
		previousKey = row.Key
	}
	previousIndex := t.selected
	t.all = rows
	t.apply()
	t.selected = 0
	if previousKey != "" {
		for i, row := range t.visible {
			if row.Key == previousKey {
				t.selected = i
				t.clampViewport()
				return
			}
		}
		t.selected = previousIndex
	}
	t.clampSelection()
	t.clampViewport()
}

func (t *Table) SetFilter(filter string) {
	t.filter = filter
	t.apply()
	t.clampSelection()
	t.clampViewport()
}

// NextSort advances to the next sortable column, wrapping around.
func (t *Table) NextSort() {
	if len(t.columns) == 0 {
		return
	}
	t.sortCol = (t.sortCol + 1) % len(t.columns)
	t.sortDesc = false
	t.sorted = true
	t.apply()
	t.clampSelection()
}

func (t *Table) ReverseSort() {
	if !t.sorted {
		return
	}
	t.sortDesc = !t.sortDesc
	t.apply()
	t.clampSelection()
}

func (t *Table) Move(delta int) {
	t.selected += delta
	t.clampSelection()
	t.clampViewport()
}

func (t *Table) Page(delta int) { t.Move(delta * t.bodyHeight()) }
func (t *Table) Home()          { t.selected = 0; t.clampViewport() }

func (t *Table) End() {
	t.selected = len(t.visible) - 1
	t.clampSelection()
	t.clampViewport()
}

func (t *Table) Selected() (Row, bool) {
	if t.selected < 0 || t.selected >= len(t.visible) {
		return Row{}, false
	}
	return t.visible[t.selected], true
}

// AtBottom reports whether the selection is on the last visible row, which is
// the trigger for loading another page in a paginated view.
func (t *Table) AtBottom() bool {
	return len(t.visible) > 0 && t.selected == len(t.visible)-1
}

func (t *Table) bodyHeight() int {
	if t.height <= 1 {
		return 0
	}
	return t.height - 1 // one line for the column header
}

func (t *Table) clampSelection() {
	if t.selected < 0 {
		t.selected = 0
	}
	if t.selected >= len(t.visible) {
		t.selected = len(t.visible) - 1
	}
	if t.selected < 0 {
		t.selected = 0
	}
}

func (t *Table) clampViewport() {
	body := t.bodyHeight()
	if body <= 0 {
		t.offset = 0
		return
	}
	if t.selected < t.offset {
		t.offset = t.selected
	}
	if t.selected >= t.offset+body {
		t.offset = t.selected - body + 1
	}
	maxOffset := len(t.visible) - body
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
}
```

- [ ] **Step 4: Implement filtering and sorting**

Append to `internal/tui/table.go`:

```go
// apply rebuilds visible from all by filtering then sorting.
func (t *Table) apply() {
	t.visible = t.filtered()
	if t.sorted && t.sortCol >= 0 && t.sortCol < len(t.columns) {
		column := t.columns[t.sortCol]
		index := t.sortCol
		sort.SliceStable(t.visible, func(i, j int) bool {
			left, right := t.visible[i], t.visible[j]
			if index >= len(left.Cells) || index >= len(right.Cells) {
				return false
			}
			var less bool
			switch column.Sort {
			case SortNumeric, SortTime:
				less = left.Cells[index].Value < right.Cells[index].Value
			default:
				less = left.Cells[index].Text < right.Cells[index].Text
			}
			if t.sortDesc {
				return !less
			}
			return less
		})
	}
}

// filtered applies the current filter to the first column's text. A leading
// "!" inverts the match; a leading "~" switches to a regular expression. An
// invalid expression matches nothing rather than erroring out.
func (t *Table) filtered() []Row {
	if t.filter == "" {
		return append([]Row(nil), t.all...)
	}
	pattern, invert := t.filter, false
	if strings.HasPrefix(pattern, "!") {
		invert, pattern = true, pattern[1:]
	}
	var expression *regexp.Regexp
	if strings.HasPrefix(pattern, "~") {
		compiled, err := regexp.Compile(pattern[1:])
		if err != nil {
			return nil
		}
		expression = compiled
	}
	needle := strings.ToLower(pattern)
	result := make([]Row, 0, len(t.all))
	for _, row := range t.all {
		text := ""
		if len(row.Cells) > 0 {
			text = row.Cells[0].Text
		}
		var match bool
		if expression != nil {
			match = expression.MatchString(text)
		} else {
			match = strings.Contains(strings.ToLower(text), needle)
		}
		if match != invert {
			result = append(result, row)
		}
	}
	return result
}
```

Add `"regexp"` and `"sort"` to the file's imports.

- [ ] **Step 5: Implement rendering**

Append to `internal/tui/table.go`:

```go
// Render returns exactly height lines, each exactly width display cells: one
// column header followed by body rows, blank-padded when there is not enough
// data to fill the viewport.
func (t *Table) Render() []string {
	lines := make([]string, 0, t.height)
	if t.height <= 0 || t.width <= 0 {
		return lines
	}
	widths := computeWidths(t.columns, t.visible, t.width)
	domains := make([]float64, len(t.columns))
	for i, column := range t.columns {
		if column.Kind == CellSparkline {
			domains[i] = sparkDomain(t.visible, i)
		}
	}

	header := make([]string, 0, len(t.columns))
	for i, column := range t.columns {
		title := column.Title
		if t.sorted && i == t.sortCol {
			marker := "↑"
			if t.sortDesc {
				marker = "↓"
			}
			title = truncateDisplay(title+marker, widths[i])
		}
		header = append(header, formatCell(Cell{Text: title},
			Column{Align: column.Align, Kind: CellText}, widths[i], 0))
	}
	lines = append(lines, styleColumn.Render(strings.Join(header, " ")))

	body := t.bodyHeight()
	for offset := 0; offset < body; offset++ {
		index := t.offset + offset
		if index >= len(t.visible) {
			lines = append(lines, strings.Repeat(" ", t.width))
			continue
		}
		row := t.visible[index]
		cells := make([]string, 0, len(t.columns))
		for i, column := range t.columns {
			cell := Cell{}
			if i < len(row.Cells) {
				cell = row.Cells[i]
			}
			cells = append(cells, formatCell(cell, column, widths[i], domains[i]))
		}
		line := strings.Join(cells, " ")
		if index == t.selected {
			line = styleSelected.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}
```

Note the header is styled as a single already-joined string with no newline in it, satisfying the Task 7 guard.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'ComputeWidths|FormatCell|SparkDomain|Selection|Sort|Filter|Render|Viewport' -race -v`
Expected: PASS — sixteen tests.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/table.go internal/tui/table_test.go
git commit -m "feat(tui): table state with sort, filter, scroll and key-stable selection"
```

---

## Task 10: Frame layout with exact viewport sizing

**Files:**
- Create: `internal/tui/layout.go`
- Test: `internal/tui/layout_test.go`

**Interfaces:**
- Consumes: `tui.Table` (Task 9), theme styles (Task 7)
- Produces: `tui.frame(header []string, body []string, footer string, width, height int) string`, `tui.headerBlock(info headerInfo, width int) []string`, `tui.headerInfo{DBPath, Range, Tool, Tokens, Cost, Requests, Unpriced string}`, `tui.bodyHeight(height int) int`

This task implements spec §4 and is the structural fix for spec §2.1 — the defect where `m.height` was never read.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/layout_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func assertExactFrame(t *testing.T, out string, width, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Fatalf("frame has %d lines, want exactly %d", len(lines), height)
	}
	for i, line := range lines {
		if lipgloss.Width(line) != width {
			t.Errorf("line %d width = %d, want exactly %d: %q",
				i, lipgloss.Width(line), width, line)
		}
	}
}

func TestFrameIsExactlyViewportSized(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {200, 60}, {40, 10}, {177, 58}} {
		info := headerInfo{
			DBPath: "~/.local/share/ccdash/usage.db", Range: "all",
			Tool: "all", Tokens: "2.4B", Cost: "$2012.27 at API rates",
			Requests: "23,216", Unpriced: "9",
		}
		header := headerBlock(info, size.w)
		table := NewTable([]Column{{Title: "NAME"}, {Title: "COST", Align: AlignRight}})
		table.SetSize(size.w, bodyHeight(size.h))
		table.SetRows([]Row{textRow("a", "x", "$1")})
		out := frame(header, table.Render(), "<projects>", size.w, size.h)
		assertExactFrame(t, out, size.w, size.h)
	}
}

func TestFrameFillsEvenWithNoRows(t *testing.T) {
	header := headerBlock(headerInfo{Range: "all"}, 100)
	table := NewTable([]Column{{Title: "NAME"}})
	table.SetSize(100, bodyHeight(30))
	table.SetRows(nil)
	out := frame(header, table.Render(), "<projects>", 100, 30)
	assertExactFrame(t, out, 100, 30)
}

func TestBodyHeightLeavesRoomForChrome(t *testing.T) {
	// 4 header rows + 1 footer = 5 lines of chrome.
	if got := bodyHeight(30); got != 25 {
		t.Errorf("bodyHeight(30) = %d, want 25", got)
	}
	if got := bodyHeight(6); got < 1 {
		t.Errorf("bodyHeight(6) = %d, must stay at least 1", got)
	}
	if got := bodyHeight(3); got < 1 {
		t.Errorf("bodyHeight(3) = %d, must stay at least 1", got)
	}
}

func TestHeaderCollapsesOnShortTerminals(t *testing.T) {
	full := headerBlock(headerInfo{Range: "all"}, 120)
	if len(full) != 4 {
		t.Errorf("full header = %d lines, want 4", len(full))
	}
	for i, line := range full {
		if lipgloss.Width(line) != 120 {
			t.Errorf("header line %d width = %d, want 120", i, lipgloss.Width(line))
		}
	}
}

func TestHeaderDropsLogoWhenNarrow(t *testing.T) {
	narrow := headerBlock(headerInfo{Range: "all"}, 50)
	joined := strings.Join(narrow, "\n")
	if strings.Contains(joined, "╔") {
		t.Error("the logo must be dropped first under width pressure")
	}
	for i, line := range narrow {
		if lipgloss.Width(line) != 50 {
			t.Errorf("narrow header line %d width = %d, want 50", i, lipgloss.Width(line))
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'Frame|BodyHeight|Header' -v`
Expected: FAIL — `undefined: headerInfo`

- [ ] **Step 3: Implement the header block**

Create `internal/tui/layout.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	headerLines = 4
	footerLines = 1
	logoWidth   = 26
	minLogoRoom = 96
)

// logo is four lines of exactly logoWidth cells. Never passed to Render as one
// multi-line string; each line is styled separately.
var logo = [headerLines]string{
	"╔═╗╔═╗╔╦╗╔═╗╔═╗╦ ╦     ",
	"║  ║  ║║╠═╣╚═╗╠═╣      ",
	"╚═╝╚═╝═╩╝╩ ╩╚═╝╩ ╩     ",
	"rev 0.1.0              ",
}

type headerInfo struct {
	DBPath   string
	Range    string
	Tool     string
	Tokens   string
	Cost     string
	Requests string
	Unpriced string
}

// bodyHeight is the number of lines available to the table, given the total
// terminal height. It never returns less than 1.
func bodyHeight(height int) int {
	body := height - headerLines - footerLines
	if body < 1 {
		return 1
	}
	return body
}

// padLine trims or pads a line to exactly width display cells.
func padLine(text string, width int) string {
	actual := lipgloss.Width(text)
	if actual > width {
		return truncateDisplay(text, width)
	}
	return text + strings.Repeat(" ", width-actual)
}

// headerBlock renders exactly headerLines lines of exactly width cells.
func headerBlock(info headerInfo, width int) []string {
	left := []string{
		fmt.Sprintf(" Context:  %s", info.DBPath),
		fmt.Sprintf(" Range:    %s", info.Range),
		fmt.Sprintf(" Tokens:   %-12s Cost: %s", info.Tokens, info.Cost),
		fmt.Sprintf(" Requests: %-12s Unpriced: %s", info.Requests, info.Unpriced),
	}
	keys := []string{
		"<1> all", "<2> claude", "<3> codex", "<?> help",
	}

	lines := make([]string, 0, headerLines)
	showLogo := width >= minLogoRoom
	for i := 0; i < headerLines; i++ {
		body := left[i]
		reserved := 0
		if showLogo {
			reserved = logoWidth
		}
		keyColumn := ""
		if width >= 70 {
			keyColumn = fmt.Sprintf("  %-11s", keys[i])
		}
		body = padLine(body, width-reserved-lipgloss.Width(keyColumn))
		line := body + styleDim.Render(keyColumn)
		if showLogo {
			line += styleAccent.Render(padLine(logo[i], logoWidth))
		}
		lines = append(lines, padLine(line, width))
	}
	return lines
}
```

- [ ] **Step 4: Implement the frame**

Append to `internal/tui/layout.go`:

```go
// frame assembles the complete screen. The result is always exactly height
// lines of exactly width display cells — the invariant that spec §2.1's defect
// violated. Short input is padded; long input is trimmed.
func frame(header []string, body []string, footer string, width, height int) string {
	lines := make([]string, 0, height)
	for _, line := range header {
		if len(lines) >= height {
			break
		}
		lines = append(lines, padLine(line, width))
	}
	available := height - len(lines) - footerLines
	for i := 0; i < available; i++ {
		if i < len(body) {
			lines = append(lines, padLine(body[i], width))
		} else {
			lines = append(lines, strings.Repeat(" ", width))
		}
	}
	for len(lines) < height-footerLines {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) < height {
		lines = append(lines, padLine(footer, width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines[:height], "\n")
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'Frame|BodyHeight|Header' -race -v`
Expected: PASS — five tests, including the 177x58 case that matches the reported terminal size.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/layout.go internal/tui/layout_test.go
git commit -m "feat(tui): full-height frame layout

Fixes the defect where m.height was assigned and never read, so the view
emitted only as many lines as its content needed and left the rest of the
alt-screen buffer unwritten."
```

---

## Task 11: App model, view stack, and navigation

**Files:**
- Create: `internal/tui/app.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `tui.View`, `tui.Scope` (Task 7); `tui.Table` (Task 9); `tui.frame`, `tui.headerBlock`, `tui.bodyHeight` (Task 10)
- Produces: `tui.Model` satisfying `tea.Model`, `tui.New(st *store.Store, pricing *model.Pricing, dbPath string, root View) Model`, `tui.stackEntry`, `(Model).breadcrumb() string`, `(Model).current() *stackEntry`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/app_test.go`. `fakeView` lets navigation be tested without a database.

```go
package tui

import (
	"database/sql"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanochang/ccdash/internal/model"
)

// fakeView is a two-level resource used to exercise navigation.
type fakeView struct {
	title string
	leaf  bool
}

func (f fakeView) Title() string { return f.title }
func (f fakeView) Columns() []Column {
	return []Column{{Title: "NAME", Sort: SortString, Kind: CellText}}
}
func (f fakeView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) {
	return []Row{textRow("k1", "alpha"), textRow("k2", "beta")}, nil
}
func (f fakeView) Drill(row Row, scope Scope) (View, Scope, bool) {
	if f.leaf {
		return nil, scope, false
	}
	scope.Session = row.Key
	return fakeView{title: "Child", leaf: true}, scope, true
}

func newTestModel() Model {
	m := New(nil, model.DefaultPricing(), "/tmp/usage.db", fakeView{title: "Root"})
	m.width, m.height = 100, 24
	m.reloadCurrent()
	return m
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	panic("unknown key " + s)
}

func TestDrillPushesAndEscPops(t *testing.T) {
	m := newTestModel()
	if len(m.stack) != 1 {
		t.Fatalf("initial stack depth = %d, want 1", len(m.stack))
	}
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Fatalf("after enter, depth = %d, want 2", len(m.stack))
	}
	if m.current().scope.Session != "k1" {
		t.Errorf("drill did not narrow scope: %q", m.current().scope.Session)
	}
	next, _ = m.Update(key("esc"))
	m = next.(Model)
	if len(m.stack) != 1 {
		t.Errorf("after esc, depth = %d, want 1", len(m.stack))
	}
}

func TestEscAtRootDoesNotQuit(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(key("esc"))
	m = next.(Model)
	if len(m.stack) != 1 {
		t.Errorf("esc at root changed the stack to depth %d", len(m.stack))
	}
	if cmd != nil {
		t.Error("esc at root must not emit a command, least of all Quit")
	}
}

func TestEnterOnLeafIsNoOp(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	next, _ = m.Update(key("enter")) // now on the leaf
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Errorf("enter on a leaf changed depth to %d, want 2", len(m.stack))
	}
}

func TestBreadcrumbTracksStack(t *testing.T) {
	m := newTestModel()
	if got := m.breadcrumb(); got != "<Root>" {
		t.Errorf("breadcrumb = %q, want <Root>", got)
	}
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if got := m.breadcrumb(); !strings.Contains(got, "Child") {
		t.Errorf("breadcrumb = %q, want it to include Child", got)
	}
}

func TestWindowSizeDrivesTheFrame(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	assertExactFrame(t, m.View(), 120, 40)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m = next.(Model)
	assertExactFrame(t, m.View(), 60, 12)
}

func TestToolAndRangeKeysSetScope(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("2"))
	m = next.(Model)
	if m.scope.Tool != model.ToolClaude {
		t.Errorf("tool = %q, want claude", m.scope.Tool)
	}
	next, _ = m.Update(key("w"))
	m = next.(Model)
	if m.rangeLabel != "week" {
		t.Errorf("range = %q, want week", m.rangeLabel)
	}
	next, _ = m.Update(key("a"))
	m = next.(Model)
	if m.rangeLabel != "all" || !m.scope.From.IsZero() {
		t.Errorf("range 'a' must clear the window, got %q", m.rangeLabel)
	}
}

func TestGlobalScopeSurvivesDrill(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("2"))
	m = next.(Model)
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.current().scope.Tool != model.ToolClaude {
		t.Error("tool filter must carry into a drilled view")
	}
}

func TestCostIsLabelledAtAPIRates(t *testing.T) {
	m := newTestModel()
	out := m.View()
	if strings.Contains(strings.ToLower(out), "spent") {
		t.Error(`no rendered surface may say "spent" — these are subscription plans`)
	}
	if !strings.Contains(out, "at API rates") {
		t.Error(`the header must label cost "at API rates"`)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'Drill|Esc|Enter|Breadcrumb|WindowSize|ToolAndRange|GlobalScope|CostIsLabelled' -v`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Write the model and stack**

Create `internal/tui/app.go`:

```go
package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

type stackEntry struct {
	view  View
	scope Scope
	table *Table
}

type Model struct {
	st      *store.Store
	pricing *model.Pricing
	dbPath  string

	stack []stackEntry
	scope Scope // global filter; drill-down narrowing is layered on top

	width, height int
	rangeLabel    string

	totals   agg.TotalsResult
	unpriced int

	lastRefresh time.Time
	refreshErr  error
	inFlight    bool

	mode  inputMode
	input string
}

type inputMode int

const (
	modeNormal inputMode = iota
	modeCommand
	modeFilter
)

func New(st *store.Store, pricing *model.Pricing, dbPath string, root View) Model {
	m := Model{
		st: st, pricing: pricing, dbPath: dbPath,
		rangeLabel: "all", width: 80, height: 24,
	}
	m.stack = []stackEntry{{view: root, scope: m.scope, table: NewTable(root.Columns())}}
	return m
}

func (m Model) current() *stackEntry {
	if len(m.stack) == 0 {
		return nil
	}
	return &m.stack[len(m.stack)-1]
}

func (m Model) db() *sql.DB {
	if m.st == nil {
		return nil
	}
	return m.st.DB()
}

// reloadCurrent refetches the top view's rows into its table.
func (m *Model) reloadCurrent() {
	entry := m.current()
	if entry == nil {
		return
	}
	entry.table.SetSize(m.width, bodyHeight(m.height))
	rows, err := entry.view.Rows(m.db(), m.pricing, entry.scope)
	if err != nil {
		m.refreshErr = err
		return
	}
	m.refreshErr = nil
	entry.table.SetRows(rows)
}

func (m Model) breadcrumb() string {
	parts := make([]string, 0, len(m.stack))
	for _, entry := range m.stack {
		parts = append(parts, "<"+entry.view.Title()+">")
	}
	return strings.Join(parts, " ")
}

func (m Model) Init() tea.Cmd { return nil }
```

- [ ] **Step 4: Write Update**

Append to `internal/tui/app.go`. Prompt handling is added in Task 12 and refresh in Task 13; this task covers window size, navigation and scope keys.

```go
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		for i := range m.stack {
			m.stack[i].table.SetSize(m.width, bodyHeight(m.height))
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	entry := m.current()
	if entry == nil {
		return m, nil
	}
	switch message.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		entry.table.Move(1)
	case "k", "up":
		entry.table.Move(-1)
	case "ctrl+f":
		entry.table.Page(1)
	case "ctrl+b":
		entry.table.Page(-1)
	case "g":
		entry.table.Home()
	case "G":
		entry.table.End()
	case "s":
		entry.table.NextSort()
	case "S":
		entry.table.ReverseSort()
	case "enter":
		return m.drill()
	case "esc":
		return m.pop()
	case "1":
		return m.setTool("")
	case "2":
		return m.setTool(model.ToolClaude)
	case "3":
		return m.setTool(model.ToolCodex)
	case "d":
		return m.setRange(24*time.Hour, "day")
	case "w":
		return m.setRange(7*24*time.Hour, "week")
	case "m":
		return m.setRange(30*24*time.Hour, "month")
	case "a":
		return m.setRange(0, "all")
	}
	return m, nil
}

func (m Model) drill() (tea.Model, tea.Cmd) {
	entry := m.current()
	row, ok := entry.table.Selected()
	if !ok {
		return m, nil
	}
	next, scope, ok := entry.view.Drill(row, entry.scope)
	if !ok {
		return m, nil
	}
	m.stack = append(m.stack, stackEntry{
		view: next, scope: scope, table: NewTable(next.Columns()),
	})
	m.reloadCurrent()
	return m, nil
}

// pop returns to the parent view. At the root it does nothing, so a reflexive
// esc can never drop the user out of the application.
func (m Model) pop() (tea.Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, nil
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.reloadCurrent()
	return m, nil
}

// setTool and setRange change the global scope, which is then re-applied to
// every level of the stack so a drilled view stays consistent with the header.
func (m Model) setTool(tool model.Tool) (tea.Model, tea.Cmd) {
	m.scope.Tool = tool
	m.applyScope()
	return m, nil
}

func (m Model) setRange(window time.Duration, label string) (tea.Model, tea.Cmd) {
	m.rangeLabel = label
	if window == 0 {
		m.scope.From = time.Time{}
	} else {
		m.scope.From = time.Now().Add(-window)
	}
	m.scope.To = time.Time{}
	m.applyScope()
	return m, nil
}

func (m *Model) applyScope() {
	for i := range m.stack {
		m.stack[i].scope.From = m.scope.From
		m.stack[i].scope.To = m.scope.To
		m.stack[i].scope.Tool = m.scope.Tool
	}
	m.reloadCurrent()
}
```

- [ ] **Step 5: Write View**

Append to `internal/tui/app.go`:

```go
func (m Model) View() string {
	entry := m.current()
	if entry == nil {
		return strings.Repeat(" ", m.width)
	}
	info := headerInfo{
		DBPath:   m.dbPath,
		Range:    m.rangeText(),
		Tokens:   formatTokens(m.totals.Tokens),
		Cost:     fmt.Sprintf("$%.2f at API rates", m.totals.Cost),
		Requests: fmt.Sprintf("%d", m.totals.Requests),
		Unpriced: fmt.Sprintf("%d", m.unpriced),
	}
	return frame(headerBlock(info, m.width), entry.table.Render(),
		m.footer(), m.width, m.height)
}

func (m Model) rangeText() string {
	text := m.rangeLabel
	if !m.totals.From.IsZero() {
		text += fmt.Sprintf("  %s → %s",
			m.totals.From.Format("2006-01-02"), m.totals.To.Format("2006-01-02"))
	}
	return text
}

func (m Model) footer() string {
	left := m.breadcrumb()
	right := "[enter] drill  [s]ort  [/]filter  [:]cmd  [?]help"
	if m.refreshErr != nil {
		right = styleDanger.Render("refresh failed: " + m.refreshErr.Error())
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return padLine(" "+left, m.width)
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}

```

Add `"github.com/charmbracelet/lipgloss"` to the imports.

**Do not define `formatTokens` here.** It already exists in `internal/tui/tui.go`, which is in the same package and is not deleted until Task 16. Redefining it now is a `formatTokens redeclared in this block` compile error. Call the existing one; Task 16 moves it into `app.go` as part of the deletion.

Note both date formats are `2006-01-02`, fixing the missing-year defect from spec §2.7.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui -race -v`
Expected: PASS on everything except `TestNoNewlinesInsideStyledRender`, which still fails on the not-yet-deleted `tui.go`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat(tui): app model with a view stack, breadcrumb and global scope keys"
```

---

## Task 12: Command and filter prompts

**Files:**
- Modify: `internal/tui/app.go`
- Create: `internal/tui/command.go`
- Test: `internal/tui/command_test.go`

**Interfaces:**
- Consumes: `tui.Model`, `tui.inputMode` (Task 11)
- Produces: `tui.resolveCommand(name string, registry map[string]func() View) (View, bool)`, `tui.commandRegistry(...) map[string]func() View`, prompt handling inside `Model.handleKey`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/command_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestResolveCommandAcceptsNamesAndAliases(t *testing.T) {
	registry := map[string]func() View{
		"projects": func() View { return fakeView{title: "Projects"} },
		"proj":     func() View { return fakeView{title: "Projects"} },
		"p":        func() View { return fakeView{title: "Projects"} },
	}
	for _, name := range []string{"projects", "proj", "p", "  projects  ", "PROJECTS"} {
		view, ok := resolveCommand(name, registry)
		if !ok {
			t.Errorf("resolveCommand(%q) failed", name)
			continue
		}
		if view.Title() != "Projects" {
			t.Errorf("resolveCommand(%q) = %q", name, view.Title())
		}
	}
	if _, ok := resolveCommand("nope", registry); ok {
		t.Error("unknown command must not resolve")
	}
}

func TestCommandPromptReplacesTheStack(t *testing.T) {
	m := newTestModel()
	m.registry = map[string]func() View{
		"child": func() View { return fakeView{title: "Child", leaf: true} },
	}
	next, _ := m.Update(key("enter")) // depth 2
	m = next.(Model)
	next, _ = m.Update(key(":"))
	m = next.(Model)
	if m.mode != modeCommand {
		t.Fatal("':' must open the command prompt")
	}
	for _, r := range "child" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.mode != modeNormal {
		t.Error("submitting the prompt must return to normal mode")
	}
	if len(m.stack) != 1 {
		t.Errorf("a command replaces the whole stack, depth = %d, want 1", len(m.stack))
	}
	if m.current().view.Title() != "Child" {
		t.Errorf("view = %q, want Child", m.current().view.Title())
	}
}

func TestUnknownCommandLeavesStackUntouched(t *testing.T) {
	m := newTestModel()
	m.registry = map[string]func() View{}
	next, _ := m.Update(key(":"))
	m = next.(Model)
	for _, r := range "zzz" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 1 || m.current().view.Title() != "Root" {
		t.Error("an unknown command must not change the stack")
	}
	if m.commandErr == "" {
		t.Error("an unknown command must report an inline error")
	}
}

func TestEscCancelsPromptWithoutPopping(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("enter")) // depth 2
	m = next.(Model)
	next, _ = m.Update(key(":"))
	m = next.(Model)
	next, _ = m.Update(key("esc"))
	m = next.(Model)
	if m.mode != modeNormal {
		t.Error("esc must close the prompt")
	}
	if len(m.stack) != 2 {
		t.Errorf("esc closing a prompt must not also pop the stack, depth = %d", len(m.stack))
	}
}

func TestFilterPromptFiltersTheTable(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	if m.mode != modeFilter {
		t.Fatal("'/' must open the filter prompt")
	}
	for _, r := range "alp" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.current().table.VisibleCount() != 1 {
		t.Errorf("visible = %d, want 1", m.current().table.VisibleCount())
	}
	if m.current().table.TotalCount() != 2 {
		t.Errorf("total = %d, want 2", m.current().table.TotalCount())
	}
}

func TestPromptKeysAreNotTreatedAsGlobalKeys(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	// "2" would normally set the claude tool filter.
	next, _ = m.Update(key("2"))
	m = next.(Model)
	if m.scope.Tool != "" {
		t.Error("keys typed into a prompt must not fire global bindings")
	}
	if !strings.Contains(m.input, "2") {
		t.Errorf("input = %q, want it to contain the typed character", m.input)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'Command|Prompt|Filter' -v`
Expected: FAIL — `undefined: resolveCommand`

- [ ] **Step 3: Add prompt state to the model**

In `internal/tui/app.go`, add two fields to `Model`:

```go
	registry   map[string]func() View
	commandErr string
```

and set the registry in `New` by adding a parameter. Change the signature to:

```go
func New(st *store.Store, pricing *model.Pricing, dbPath string, root View,
	registry map[string]func() View) Model {
```

assigning `registry: registry` in the literal. Update `newTestModel` in `app_test.go` to pass `nil`.

- [ ] **Step 4: Write command resolution**

Create `internal/tui/command.go`:

```go
package tui

import "strings"

// resolveCommand looks up a view constructor by name or alias. Matching is
// case-insensitive and ignores surrounding whitespace and a leading colon.
func resolveCommand(name string, registry map[string]func() View) (View, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.TrimPrefix(key, ":")
	build, ok := registry[key]
	if !ok {
		return nil, false
	}
	return build(), true
}
```

- [ ] **Step 5: Route keys through the prompt**

In `internal/tui/app.go`, add this as the first block of `handleKey`, before the existing switch:

```go
	if m.mode != modeNormal {
		return m.handlePrompt(message)
	}
```

and add the two openers to the normal-mode switch:

```go
	case ":":
		m.mode = modeCommand
		m.input = ""
		m.commandErr = ""
	case "/":
		m.mode = modeFilter
		m.input = ""
```

Then append the prompt handler:

```go
// handlePrompt consumes every key while a prompt is open, so global bindings
// never fire on characters the user is typing.
func (m Model) handlePrompt(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		m.input = ""
		return m, nil
	case tea.KeyBackspace:
		if runes := []rune(m.input); len(runes) > 0 {
			m.input = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyEnter:
		return m.submitPrompt()
	case tea.KeyRunes, tea.KeySpace:
		m.input += string(message.Runes)
		if message.Type == tea.KeySpace {
			m.input += " "
		}
		return m, nil
	}
	return m, nil
}

func (m Model) submitPrompt() (tea.Model, tea.Cmd) {
	input, mode := m.input, m.mode
	m.mode, m.input = modeNormal, ""
	switch mode {
	case modeFilter:
		m.current().table.SetFilter(input)
		return m, nil
	case modeCommand:
		if strings.EqualFold(strings.TrimSpace(input), "q") ||
			strings.EqualFold(strings.TrimSpace(input), "quit") {
			return m, tea.Quit
		}
		view, ok := resolveCommand(input, m.registry)
		if !ok {
			m.commandErr = "unknown command: " + input
			return m, nil
		}
		m.commandErr = ""
		// A command replaces the whole stack: it is a jump, not a drill.
		m.stack = []stackEntry{{
			view: view, scope: m.scope, table: NewTable(view.Columns()),
		}}
		m.reloadCurrent()
		return m, nil
	}
	return m, nil
}
```

- [ ] **Step 6: Show the prompt in the footer**

Replace the body of `Model.footer` with:

```go
func (m Model) footer() string {
	if m.mode == modeCommand {
		return padLine(stylePrompt.Render(" :"+m.input+"█"), m.width)
	}
	if m.mode == modeFilter {
		return padLine(stylePrompt.Render(" /"+m.input+"█"), m.width)
	}
	left := m.breadcrumb()
	right := "[enter] drill  [s]ort  [/]filter  [:]cmd  [?]help"
	if m.commandErr != "" {
		right = styleWarning.Render(m.commandErr)
	}
	if m.refreshErr != nil {
		right = styleDanger.Render("refresh failed: " + m.refreshErr.Error())
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return padLine(" "+left, m.width)
	}
	return " " + left + strings.Repeat(" ", gap) + right + " "
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/tui -race -v`
Expected: PASS except the known `tui.go` newline failure.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/app.go internal/tui/command.go internal/tui/command_test.go internal/tui/app_test.go
git commit -m "feat(tui): command and filter prompts with alias resolution"
```

---

## Task 13: Refresh ticker with single-flight

**Files:**
- Modify: `internal/tui/app.go`
- Test: `internal/tui/refresh_test.go`

**Interfaces:**
- Consumes: `tui.Model` (Task 11), `ingest.Run`, `ingest.DefaultSources`
- Produces: `tui.tickMsg`, `tui.refreshedMsg`, `(Model).scheduleTick() tea.Cmd`, `(Model).refresh(reingest bool) tea.Cmd`, `refreshInterval` constant

- [ ] **Step 1: Write the failing test**

Create `internal/tui/refresh_test.go`:

```go
package tui

import (
	"errors"
	"testing"
	"time"
)

func TestSingleFlightDropsOverlappingTick(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(tickMsg{})
	m = next.(Model)
	if !m.inFlight {
		t.Fatal("a tick must mark a refresh in flight")
	}
	if cmd == nil {
		t.Fatal("the first tick must start work")
	}
	before := m.inFlight
	next, second := m.Update(tickMsg{})
	m = next.(Model)
	if !before || !m.inFlight {
		t.Error("state should stay in flight")
	}
	if second != nil {
		t.Error("a tick arriving while a refresh is running must be dropped, not queued")
	}
}

func TestRefreshedClearsInFlightAndStamps(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	next, _ = m.Update(refreshedMsg{at: time.Unix(5000, 0)})
	m = next.(Model)
	if m.inFlight {
		t.Error("completing a refresh must clear the in-flight flag")
	}
	if m.lastRefresh.Unix() != 5000 {
		t.Errorf("lastRefresh = %d, want 5000", m.lastRefresh.Unix())
	}
}

func TestRefreshErrorKeepsLastGoodDataAndTicking(t *testing.T) {
	m := newTestModel()
	rowsBefore := m.current().table.TotalCount()
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	next, cmd := m.Update(refreshedMsg{at: time.Unix(1, 0), err: errors.New("disk on fire")})
	m = next.(Model)
	if m.refreshErr == nil {
		t.Fatal("the error must be recorded")
	}
	if m.current().table.TotalCount() != rowsBefore {
		t.Error("a failed refresh must leave the last good rows on screen")
	}
	if m.inFlight {
		t.Error("a failed refresh must clear the in-flight flag so ticking recovers")
	}
	if cmd == nil {
		t.Error("the ticker must keep running after a failure so it can self-heal")
	}
}

func TestPromptPausesTicking(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key(":"))
	m = next.(Model)
	next, cmd := m.Update(tickMsg{})
	m = next.(Model)
	if m.inFlight {
		t.Error("no refresh may start while a prompt is open")
	}
	if cmd == nil {
		t.Error("the ticker must still reschedule itself while paused")
	}
}

func TestRefreshAgeText(t *testing.T) {
	m := newTestModel()
	m.lastRefresh = time.Now().Add(-3 * time.Second)
	if got := m.refreshAge(); got != "3s ago" {
		t.Errorf("refreshAge = %q, want 3s ago", got)
	}
	m.lastRefresh = time.Time{}
	if got := m.refreshAge(); got != "never" {
		t.Errorf("refreshAge with no refresh = %q, want never", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'SingleFlight|Refresh|PromptPauses' -v`
Expected: FAIL — `undefined: tickMsg`

- [ ] **Step 3: Implement the ticker**

Append to `internal/tui/app.go`:

```go
// refreshInterval matches k9s's default. Not configurable in this phase.
const refreshInterval = 2 * time.Second

type tickMsg struct{}

type refreshedMsg struct {
	at       time.Time
	totals   agg.TotalsResult
	rows     []Row
	unpriced int
	err      error
}

func (m Model) scheduleTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// refresh runs an incremental ingest and refetches the current view off the UI
// thread. reingest is false for a plain data refetch.
func (m Model) refresh(reingest bool) tea.Cmd {
	st, pricing, entry := m.st, m.pricing, m.current()
	if entry == nil {
		return nil
	}
	view, scope := entry.view, entry.scope
	global := m.scope
	return func() tea.Msg {
		now := time.Now()
		if st == nil {
			return refreshedMsg{at: now}
		}
		if reingest {
			home, err := os.UserHomeDir()
			if err != nil {
				return refreshedMsg{at: now, err: err}
			}
			if _, err := ingest.Run(st, ingest.DefaultSources(home), pricing, false); err != nil {
				return refreshedMsg{at: now, err: err}
			}
		}
		totals, err := agg.Totals(st.DB(), pricing, global.Filter)
		if err != nil {
			return refreshedMsg{at: now, err: err}
		}
		rows, err := view.Rows(st.DB(), pricing, scope)
		if err != nil {
			return refreshedMsg{at: now, err: err}
		}
		unpriced, err := agg.UnpricedList(st.DB(), pricing, global.Filter)
		if err != nil {
			return refreshedMsg{at: now, err: err}
		}
		return refreshedMsg{
			at: now, totals: totals, rows: rows, unpriced: len(unpriced),
		}
	}
}

func (m Model) refreshAge() string {
	if m.lastRefresh.IsZero() {
		return "never"
	}
	age := time.Since(m.lastRefresh)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	}
}
```

Add `"os"` and the `ingest` import.

- [ ] **Step 4: Handle the messages**

Add two cases to `Model.Update`, before the `tea.KeyMsg` case:

```go
	case tickMsg:
		// Single-flight: a tick arriving mid-refresh is dropped, never queued.
		// Prompts pause the work but not the ticker, so it resumes on close.
		if m.inFlight || m.mode != modeNormal {
			return m, m.scheduleTick()
		}
		m.inFlight = true
		return m, m.refresh(true)
	case refreshedMsg:
		m.inFlight = false
		m.lastRefresh = message.at
		m.refreshErr = message.err
		if message.err == nil {
			m.totals = message.totals
			m.unpriced = message.unpriced
			m.current().table.SetRows(message.rows)
		}
		return m, m.scheduleTick()
```

Change `Init` to start both the ticker and a first load:

```go
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.scheduleTick(), m.refresh(false))
}
```

Add `r` to the normal-mode key switch for a manual refresh, respecting the same guard:

```go
	case "r":
		if m.inFlight {
			return m, nil
		}
		m.inFlight = true
		return m, m.refresh(true)
```

Finally, show the refresh age in the footer by replacing the `right` default:

```go
	right := m.refreshAge() + "   [enter] drill  [s]ort  [/]filter  [:]cmd  [?]help"
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -race -v`
Expected: PASS except the known `tui.go` newline failure.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/refresh_test.go
git commit -m "feat(tui): 2s refresh ticker with single-flight and visible failure state"
```

---

## Task 14: Aggregate views — projects, models, days, unpriced

**Files:**
- Create: `internal/tui/view_projects.go`, `internal/tui/view_models.go`, `internal/tui/view_days.go`, `internal/tui/view_unpriced.go`
- Test: `internal/tui/views_test.go`

**Interfaces:**
- Consumes: `tui.View`, `tui.Column`, `tui.Row`, `tui.Cell` (Task 7); `agg.ByProject`, `agg.ByModel`, `agg.ByDay` (existing), `agg.UnpricedList` (Task 6); `render.TruncatePath` (Task 1)
- Produces: `tui.ProjectsView`, `tui.ModelsView`, `tui.DaysView`, `tui.UnpricedView`, each satisfying `View`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/views_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

func seedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "a1", Tool: model.ToolClaude, TS: time.Unix(1_700_000_000, 0),
			Model: "claude-opus-5", Project: "/home/u/alpha", Session: "s1", OutputTok: 1000},
		{ID: "a2", Tool: model.ToolClaude, TS: time.Unix(1_700_086_400, 0),
			Model: "claude-opus-5", Project: "/home/u/alpha", Session: "s1",
			Agent: "agent-x", Workflow: "wf-1", Depth: 1, OutputTok: 2000},
		{ID: "b1", Tool: model.ToolCodex, TS: time.Unix(1_700_172_800, 0),
			Model: "gpt-5-codex", Project: "/home/u/beta", Session: "s2", OutputTok: 500},
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProjectsViewShape(t *testing.T) {
	s := seedStore(t)
	view := ProjectsView{}
	rows, err := view.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d projects, want 2", len(rows))
	}
	if len(rows[0].Cells) != len(view.Columns()) {
		t.Fatalf("row has %d cells, columns declare %d",
			len(rows[0].Cells), len(view.Columns()))
	}
	if rows[0].Key == "" {
		t.Error("rows need a stable key for selection to survive refreshes")
	}
	for _, column := range view.Columns() {
		if column.Kind == CellSparkline {
			return
		}
	}
	t.Error("the projects view should carry a sparkline column")
}

func TestProjectsViewTruncatesOnSeparator(t *testing.T) {
	s := seedStore(t)
	rows, err := ProjectsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		text := row.Cells[0].Text
		if strings.HasPrefix(text, "…") && !strings.HasPrefix(text, "…/") {
			t.Errorf("truncated path %q must break on a separator", text)
		}
	}
}

func TestProjectsDrillNarrowsToProject(t *testing.T) {
	s := seedStore(t)
	rows, _ := ProjectsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	next, scope, ok := ProjectsView{}.Drill(rows[0], Scope{})
	if !ok {
		t.Fatal("projects must drill into sessions")
	}
	if scope.Project != rows[0].Key {
		t.Errorf("scope.Project = %q, want %q", scope.Project, rows[0].Key)
	}
	if next.Title() != "Sessions" {
		t.Errorf("drill target = %q, want Sessions", next.Title())
	}
}

func TestModelsViewIncludesUnpricedRow(t *testing.T) {
	s := seedStore(t)
	rows, err := ModelsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if strings.Contains(row.Cells[0].Text, "gpt-5-codex") {
			found = true
			costCell := row.Cells[len(row.Cells)-1].Text
			if !strings.Contains(costCell, "—") {
				t.Errorf("an unpriceable model must show — for cost, got %q", costCell)
			}
		}
	}
	if !found {
		t.Error("gpt-5-codex must be listed even though it has no rate")
	}
}

func TestDaysViewOrdersNewestFirst(t *testing.T) {
	s := seedStore(t)
	rows, err := DaysView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("got %d days, want at least 2", len(rows))
	}
	if rows[0].Cells[0].Value < rows[1].Cells[0].Value {
		t.Error("days should be newest first")
	}
}

func TestUnpricedViewListsOnlyUnpriceable(t *testing.T) {
	s := seedStore(t)
	rows, err := UnpricedView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Key != "gpt-5-codex" {
		t.Errorf("key = %q, want gpt-5-codex", rows[0].Key)
	}
}

func TestAllViewsDeclareCellsMatchingColumns(t *testing.T) {
	s := seedStore(t)
	pricing := model.DefaultPricing()
	for _, view := range []View{
		ProjectsView{}, ModelsView{}, DaysView{}, UnpricedView{},
	} {
		rows, err := view.Rows(s.DB(), pricing, Scope{})
		if err != nil {
			t.Fatalf("%s: %v", view.Title(), err)
		}
		for i, row := range rows {
			if len(row.Cells) != len(view.Columns()) {
				t.Errorf("%s row %d has %d cells, want %d",
					view.Title(), i, len(row.Cells), len(view.Columns()))
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'View' -v`
Expected: FAIL — `undefined: ProjectsView`

- [ ] **Step 3: Write the sessions stub first**

`ProjectsView.Drill` returns a `SessionsView`, which Task 15 supplies. Without a placeholder the package will not compile for the rest of this task, so create `internal/tui/view_sessions.go` now and replace it wholesale in Task 15:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/model"
)

// Temporary stub. Replaced in full by Task 15.
type SessionsView struct{}

func (SessionsView) Title() string                                     { return "Sessions" }
func (SessionsView) Columns() []Column                                 { return nil }
func (SessionsView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) { return nil, nil }
func (SessionsView) Drill(Row, Scope) (View, Scope, bool)              { return nil, Scope{}, false }
```

- [ ] **Step 4: Write the projects view**

Create `internal/tui/view_projects.go`. `money` and `count` are shared helpers used by every view file.

```go
package tui

import (
	"database/sql"
	"fmt"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/render"
)

// money formats a cost, or an em dash when the row could not be priced.
func money(value float64, priced bool) string {
	if !priced {
		return "—"
	}
	return fmt.Sprintf("$%.2f", value)
}

func count(value int) string { return fmt.Sprintf("%d", value) }

type ProjectsView struct{}

func (ProjectsView) Title() string { return "Projects" }

func (ProjectsView) Columns() []Column {
	return []Column{
		{Title: "NAME", Sort: SortString, Kind: CellText},
		{Title: "COST", Width: 12, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "SHARE", Width: 12, Sort: SortNumeric, Kind: CellBar},
		{Title: "TREND", Width: 14, Sort: SortNumeric, Kind: CellSparkline},
	}
}

func (ProjectsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByProject(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	top := 0.0
	for _, bucket := range buckets {
		if bucket.Cost > top {
			top = bucket.Cost
		}
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		share := 0.0
		if top > 0 {
			share = bucket.Cost / top
		}
		rows = append(rows, Row{
			Key: bucket.Project,
			Cells: []Cell{
				{Text: render.TruncatePath(bucket.Project, 40)},
				{Text: money(bucket.Cost, true), Value: bucket.Cost},
				{Value: share},
				{Series: bucket.Spark, Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (ProjectsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Project = row.Key
	return SessionsView{}, scope, true
}
```

- [ ] **Step 5: Write the models view**

Create `internal/tui/view_models.go`:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type ModelsView struct{}

func (ModelsView) Title() string { return "Models" }

func (ModelsView) Columns() []Column {
	return []Column{
		{Title: "MODEL", Sort: SortString, Kind: CellText},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "CACHE R", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 12, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func (ModelsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByModel(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		// A model with tokens but zero cost has no rate: show an em dash
		// rather than $0.00, which would read as free rather than unknown.
		priced := bucket.Cost > 0
		rows = append(rows, Row{
			Key: bucket.Model,
			Cells: []Cell{
				{Text: bucket.Model},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: formatTokens(bucket.CacheReadTok), Value: float64(bucket.CacheReadTok)},
				{Text: money(bucket.Cost, priced), Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (ModelsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Model = row.Key
	return SessionsView{}, scope, true
}
```

- [ ] **Step 6: Write the days and unpriced views**

Create `internal/tui/view_days.go`:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type DaysView struct{}

func (DaysView) Title() string { return "Days" }

func (DaysView) Columns() []Column {
	return []Column{
		{Title: "DAY", Width: 12, Sort: SortTime, Kind: CellText},
		{Title: "TOKENS", Width: 12, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 12, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "SHARE", Sort: SortNumeric, Kind: CellBar},
	}
}

func (DaysView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByDay(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	top := 0.0
	for _, bucket := range buckets {
		if bucket.Cost > top {
			top = bucket.Cost
		}
	}
	rows := make([]Row, 0, len(buckets))
	// ByDay returns oldest first; the table wants newest first.
	for i := len(buckets) - 1; i >= 0; i-- {
		bucket := buckets[i]
		share := 0.0
		if top > 0 {
			share = bucket.Cost / top
		}
		rows = append(rows, Row{
			Key: bucket.Day.Format("2006-01-02"),
			Cells: []Cell{
				{Text: bucket.Day.Format("2006-01-02"), Value: float64(bucket.Day.Unix())},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: money(bucket.Cost, true), Value: bucket.Cost},
				{Value: share},
			},
		})
	}
	return rows, nil
}

func (DaysView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
```

Create `internal/tui/view_unpriced.go`:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

// UnpricedView promotes the old footer warning into an inspectable resource.
// Rows disappear from it the moment pricing.toml gains a matching rate, with
// no re-ingest, because agg.UnpricedList derives from the live rate table.
type UnpricedView struct{}

func (UnpricedView) Title() string { return "Unpriced" }

func (UnpricedView) Columns() []Column {
	return []Column{
		{Title: "MODEL", Sort: SortString, Kind: CellText},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 12, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "FIRST SEEN", Width: 12, Sort: SortTime, Kind: CellText},
		{Title: "LAST SEEN", Width: 12, Sort: SortTime, Kind: CellText},
	}
}

func (UnpricedView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.UnpricedList(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Model,
			Cells: []Cell{
				{Text: bucket.Model},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: bucket.FirstSeen.Format("2006-01-02"),
					Value: float64(bucket.FirstSeen.Unix())},
				{Text: bucket.LastSeen.Format("2006-01-02"),
					Value: float64(bucket.LastSeen.Unix())},
			},
		})
	}
	return rows, nil
}

func (UnpricedView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'View' -race -v`
Expected: PASS — seven tests. `TestNoNewlinesInsideStyledRender` still fails on `tui.go`; confirm it names only that file and continue.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): projects, models, days and unpriced views"
```

---

## Task 15: Drill views, limits, and the pulse chart

**Files:**
- Create: `internal/tui/view_sessions.go` (replacing the stub), `internal/tui/view_requests.go`, `internal/tui/view_attribution.go`, `internal/tui/view_limits.go`, `internal/tui/view_pulse.go`
- Modify: `internal/tui/view.go` (add the `Renderer` interface), `internal/tui/app.go` (use it)
- Test: `internal/tui/views_drill_test.go`

**Interfaces:**
- Consumes: `agg.BySession` (Task 4), `agg.ByAgent`/`ByWorkflow` (Task 5), `agg.Requests` (Task 6), `agg.LatestLimits` (existing), `render.BrailleDomain` (Task 1)
- Produces: `tui.SessionsView`, `tui.RequestsView` (also satisfying `Paginator`), `tui.AgentsView`, `tui.WorkflowsView`, `tui.LimitsView`, `tui.PulseView` (satisfying `Renderer`), and `tui.Renderer`

**Spec refinement:** §3 implies every view is a table. The pulse chart is not, so a third optional interface `Renderer` is added alongside `Paginator`. Views implementing it paint their own body and the table is bypassed.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/views_drill_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestSessionsViewAndDrillToRequests(t *testing.T) {
	s := seedStore(t)
	rows, err := SessionsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d sessions, want 2", len(rows))
	}
	next, scope, ok := SessionsView{}.Drill(rows[0], Scope{})
	if !ok || next.Title() != "Requests" {
		t.Fatalf("sessions must drill into requests, got %v/%v", next, ok)
	}
	if scope.Session != rows[0].Key {
		t.Errorf("scope.Session = %q, want %q", scope.Session, rows[0].Key)
	}
}

func TestRequestsViewIsALeafAndPaginates(t *testing.T) {
	s := seedStore(t)
	view := RequestsView{}
	if _, _, ok := view.Drill(Row{}, Scope{}); ok {
		t.Error("requests is a leaf")
	}
	paginator, ok := any(view).(Paginator)
	if !ok {
		t.Fatal("RequestsView must implement Paginator")
	}
	if paginator.PageSize() != 500 {
		t.Errorf("page size = %d, want 500", paginator.PageSize())
	}
	rows, more, err := paginator.Page(s.DB(), model.DefaultPricing(), Scope{}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if !more {
		t.Error("a full page must report more available")
	}
	rest, more, err := paginator.Page(s.DB(), model.DefaultPricing(), Scope{}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || more {
		t.Errorf("final page = %d rows, more = %v; want 1 and false", len(rest), more)
	}
}

func TestRequestsShowsUnpricedAsEmDash(t *testing.T) {
	s := seedStore(t)
	rows, err := RequestsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var sawDash bool
	for _, row := range rows {
		if strings.Contains(row.Cells[len(row.Cells)-1].Text, "—") {
			sawDash = true
		}
	}
	if !sawDash {
		t.Error("the gpt-5-codex request must render its cost as —, not $0.00")
	}
	if len(rows) != 3 {
		t.Errorf("got %d rows, want all 3 — unpriceable rows are never dropped", len(rows))
	}
}

func TestAgentsAndWorkflowsViews(t *testing.T) {
	s := seedStore(t)
	agents, err := AgentsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Key != "agent-x" {
		t.Fatalf("agents = %+v, want just agent-x", agents)
	}
	workflows, err := WorkflowsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].Key != "wf-1" {
		t.Fatalf("workflows = %+v, want just wf-1", workflows)
	}
	next, scope, ok := WorkflowsView{}.Drill(workflows[0], Scope{})
	if !ok || next.Title() != "Agents" {
		t.Error("workflows must drill into agents")
	}
	if scope.Workflow != "wf-1" {
		t.Errorf("scope.Workflow = %q, want wf-1", scope.Workflow)
	}
}

func TestLimitsViewShowsProvenanceAndTrack(t *testing.T) {
	s := seedStore(t)
	rows, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	// No limit samples were seeded, so every expected limit shows "no data"
	// rather than a misleading 0%.
	if len(rows) != 4 {
		t.Fatalf("got %d limit rows, want 4 expected kinds", len(rows))
	}
	for _, row := range rows {
		joined := ""
		for _, cell := range row.Cells {
			joined += cell.Text
		}
		if !strings.Contains(joined, "no data") {
			t.Errorf("a missing limit must read 'no data', got %q", joined)
		}
	}
}

func TestPulseRendersItsOwnBody(t *testing.T) {
	s := seedStore(t)
	renderer, ok := any(PulseView{}).(Renderer)
	if !ok {
		t.Fatal("PulseView must implement Renderer")
	}
	lines, err := renderer.Body(s.DB(), model.DefaultPricing(), Scope{}, 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want exactly 10", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "max") {
		t.Error("the chart must label its y-domain maximum")
	}
	if !strings.Contains(joined, "cost / day") {
		t.Error("the chart needs a title")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'Sessions|Requests|Agents|Limits|Pulse' -v`
Expected: FAIL — `undefined: RequestsView`

- [ ] **Step 3: Write the sessions and requests views**

Replace `internal/tui/view_sessions.go` entirely:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/render"
)

type SessionsView struct{}

func (SessionsView) Title() string { return "Sessions" }

func (SessionsView) Columns() []Column {
	return []Column{
		{Title: "SESSION", Sort: SortString, Kind: CellText},
		{Title: "TOOL", Width: 7, Sort: SortString, Kind: CellText},
		{Title: "PROJECT", Width: 26, Sort: SortString, Kind: CellText},
		{Title: "STARTED", Width: 17, Sort: SortTime, Kind: CellText},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func (SessionsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.BySession(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Session,
			Cells: []Cell{
				{Text: bucket.Session},
				{Text: string(bucket.Tool)},
				{Text: render.TruncatePath(bucket.Project, 26)},
				{Text: bucket.Started.Format("2006-01-02 15:04"),
					Value: float64(bucket.Started.Unix())},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: money(bucket.Cost, bucket.Unpriced == 0 || bucket.Cost > 0),
					Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (SessionsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Session = row.Key
	return RequestsView{}, scope, true
}
```

Create `internal/tui/view_requests.go`:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

const requestsPageSize = 500

// RequestsView is the only paginated view: a full corpus holds tens of
// thousands of requests, which is not worth keeping in memory at once.
type RequestsView struct{}

func (RequestsView) Title() string { return "Requests" }

func (RequestsView) Columns() []Column {
	return []Column{
		{Title: "TIME", Width: 17, Sort: SortTime, Kind: CellText},
		{Title: "MODEL", Sort: SortString, Kind: CellText},
		{Title: "AGENT", Width: 16, Sort: SortString, Kind: CellText},
		{Title: "IN", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "OUT", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "CACHE R", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func requestRows(records []agg.RequestRow) []Row {
	rows := make([]Row, 0, len(records))
	for _, record := range records {
		agent := record.Agent
		if agent == "" {
			agent = "main"
		}
		rows = append(rows, Row{
			Key: record.ID,
			Cells: []Cell{
				{Text: record.TS.Format("2006-01-02 15:04"), Value: float64(record.TS.Unix())},
				{Text: record.Model},
				{Text: agent},
				{Text: formatTokens(record.Input), Value: float64(record.Input)},
				{Text: formatTokens(record.Output), Value: float64(record.Output)},
				{Text: formatTokens(record.CacheRead), Value: float64(record.CacheRead)},
				{Text: money(record.Cost, record.Priced), Value: record.Cost},
			},
		})
	}
	return rows
}

func (RequestsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	records, err := agg.Requests(db, pricing, scope.Filter, requestsPageSize, 0)
	if err != nil {
		return nil, err
	}
	return requestRows(records), nil
}

func (RequestsView) PageSize() int { return requestsPageSize }

// Page reports more=true when the page came back full, which is the signal for
// the table to fetch again once the selection reaches the bottom.
func (RequestsView) Page(db *sql.DB, pricing *model.Pricing, scope Scope, offset, limit int) ([]Row, bool, error) {
	records, err := agg.Requests(db, pricing, scope.Filter, limit, offset)
	if err != nil {
		return nil, false, err
	}
	return requestRows(records), len(records) == limit, nil
}

func (RequestsView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
```

- [ ] **Step 4: Write the attribution and limits views**

Create `internal/tui/view_attribution.go`:

```go
package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type AgentsView struct{}

func (AgentsView) Title() string { return "Agents" }

func (AgentsView) Columns() []Column {
	return []Column{
		{Title: "AGENT", Sort: SortString, Kind: CellText},
		{Title: "WORKFLOW", Width: 22, Sort: SortString, Kind: CellText},
		{Title: "DEPTH", Width: 7, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func (AgentsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByAgent(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Agent,
			Cells: []Cell{
				{Text: bucket.Agent},
				{Text: bucket.Workflow},
				{Text: count(bucket.Depth), Value: float64(bucket.Depth)},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: money(bucket.Cost, true), Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (AgentsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Agent = row.Key
	return RequestsView{}, scope, true
}

type WorkflowsView struct{}

func (WorkflowsView) Title() string { return "Workflows" }

func (WorkflowsView) Columns() []Column {
	return []Column{
		{Title: "WORKFLOW", Sort: SortString, Kind: CellText},
		{Title: "AGENTS", Width: 8, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "STARTED", Width: 17, Sort: SortTime, Kind: CellText},
		{Title: "COST", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func (WorkflowsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByWorkflow(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Workflow,
			Cells: []Cell{
				{Text: bucket.Workflow},
				{Text: count(bucket.Agents), Value: float64(bucket.Agents)},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: bucket.Started.Format("2006-01-02 15:04"),
					Value: float64(bucket.Started.Unix())},
				{Text: money(bucket.Cost, true), Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (WorkflowsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Workflow = row.Key
	return AgentsView{}, scope, true
}
```

Create `internal/tui/view_limits.go`:

```go
package tui

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type LimitsView struct{}

func (LimitsView) Title() string { return "Limits" }

func (LimitsView) Columns() []Column {
	return []Column{
		{Title: "TOOL", Width: 8, Sort: SortString, Kind: CellText},
		{Title: "LIMIT", Width: 14, Sort: SortString, Kind: CellText},
		{Title: "USED", Width: 20, Sort: SortNumeric, Kind: CellBar},
		{Title: "PCT", Width: 7, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "RESETS", Width: 16, Sort: SortString, Kind: CellText},
		{Title: "SOURCE", Sort: SortString, Kind: CellText},
	}
}

func (LimitsView) Rows(db *sql.DB, _ *model.Pricing, _ Scope) ([]Row, error) {
	states, err := agg.LatestLimits(db)
	if err != nil {
		return nil, err
	}
	index := make(map[string]agg.LimitState, len(states))
	for _, state := range states {
		index[limitKey(state.Tool, state.Kind, state.Scope)] = state
	}
	expected := []expectedLimit{
		{model.ToolClaude, model.KindSession},
		{model.ToolClaude, model.KindWeeklyAll},
		{model.ToolCodex, model.KindCodex5h},
		{model.ToolCodex, model.KindCodexWeekly},
	}
	rows := make([]Row, 0, len(expected)+len(states))
	emit := func(state agg.LimitState) {
		source := fmt.Sprintf("%s %s", state.Provenance, formatAge(state.Age))
		if state.Provenance == model.ProvCached || state.Age >= time.Hour {
			source = "⚠ " + source
		}
		if state.IsActive {
			source += "  ◀ binding"
		}
		rows = append(rows, Row{
			Key: limitKey(state.Tool, state.Kind, state.Scope),
			Cells: []Cell{
				{Text: string(state.Tool)},
				{Text: limitLabel(state.Kind, state.Scope)},
				{Value: state.Percent / 100},
				{Text: fmt.Sprintf("%.1f%%", state.Percent), Value: state.Percent},
				{Text: resetIn(state.ResetsAt)},
				{Text: source},
			},
		})
	}
	for _, item := range expected {
		key := limitKey(item.tool, item.kind, "")
		if state, ok := index[key]; ok {
			emit(state)
			delete(index, key)
			continue
		}
		// A missing limit reads "no data", never 0%, which would look like
		// plenty of headroom.
		rows = append(rows, Row{
			Key: key,
			Cells: []Cell{
				{Text: string(item.tool)},
				{Text: limitLabel(item.kind, "")},
				{Value: 0},
				{Text: "—"},
				{Text: "—"},
				{Text: "no data"},
			},
		})
	}
	for _, state := range states {
		if _, ok := index[limitKey(state.Tool, state.Kind, state.Scope)]; ok {
			emit(state)
		}
	}
	return rows, nil
}

func (LimitsView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
```

**Do not define `expectedLimit`, `limitKey`, `limitLabel`, `resetIn` or `formatAge` here.** All five already exist in `internal/tui/tui.go`, same package, not deleted until Task 16. Redefining any of them is a `redeclared in this block` compile error. Call the existing ones; Task 16 moves them into `view_limits.go` as part of the deletion. The imports of `view_limits.go` are therefore only `database/sql`, `fmt`, `time`, `agg` and `model`.

- [ ] **Step 5: Add the Renderer interface and the pulse view**

Append to `internal/tui/view.go`:

```go
// Renderer is implemented by views that paint their own body instead of a
// table. App checks for it before falling back to Table. Only PulseView
// implements it.
type Renderer interface {
	Body(db *sql.DB, pricing *model.Pricing, scope Scope, width, height int) ([]string, error)
}
```

Create `internal/tui/view_pulse.go`:

```go
package tui

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/render"
)

// PulseView is the one non-table view: a cost-over-time chart. It plots
// against a zero-based domain so the floor is meaningful and labels the
// maximum so magnitude is readable.
type PulseView struct{}

func (PulseView) Title() string     { return "Pulse" }
func (PulseView) Columns() []Column { return nil }

func (PulseView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) { return nil, nil }

func (PulseView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }

func (PulseView) Body(db *sql.DB, pricing *model.Pricing, scope Scope, width, height int) ([]string, error) {
	buckets, err := agg.ByDay(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	values := make([]float64, 0, len(buckets))
	maximum := 0.0
	for _, bucket := range buckets {
		values = append(values, bucket.Cost)
		if bucket.Cost > maximum {
			maximum = bucket.Cost
		}
	}

	lines := make([]string, 0, height)
	title := " cost / day"
	label := fmt.Sprintf("max $%.2f ", maximum)
	gap := width - len(title) - len(label)
	if gap < 1 {
		gap = 1
	}
	lines = append(lines, padLine(title+strings.Repeat(" ", gap)+label, width))

	plotHeight := height - 3
	if plotHeight < 1 {
		plotHeight = 1
	}
	plot := render.BrailleDomain(values, width-2, plotHeight, 0, maximum*1.05)
	for _, line := range strings.Split(plot, "\n") {
		lines = append(lines, padLine(" "+line, width))
	}

	from, to := "", ""
	if len(buckets) > 0 {
		from = buckets[0].Day.Format("2006-01-02")
		to = buckets[len(buckets)-1].Day.Format("2006-01-02")
	}
	axisGap := width - len(from) - len(to) - 6
	if axisGap < 1 {
		axisGap = 1
	}
	lines = append(lines, padLine(" "+from+strings.Repeat(" ", axisGap)+"$0  "+to, width))

	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height], nil
}
```

- [ ] **Step 6: Use Renderer in the app**

In `internal/tui/app.go`, replace the body-producing line inside `View()`:

```go
	body := entry.table.Render()
	if renderer, ok := entry.view.(Renderer); ok {
		if custom, err := renderer.Body(m.db(), m.pricing, entry.scope,
			m.width, bodyHeight(m.height)); err == nil {
			body = custom
		}
	}
	return frame(headerBlock(info, m.width), body, m.footer(), m.width, m.height)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/tui -race -v`
Expected: PASS except the known `tui.go` newline failure.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): sessions, requests, agents, workflows, limits and pulse views"
```

---

## Task 16: Wire up the command registry, delete the old screen, verify end to end

**Files:**
- Create: `internal/tui/registry.go`
- Modify: `internal/tui/app.go` (add `Run`), `cmd/ccdash/main.go`
- Delete: `internal/tui/tui.go`, `internal/tui/tui_test.go`
- Test: `internal/tui/registry_test.go`

**Interfaces:**
- Consumes: every view from Tasks 14 and 15
- Produces: `tui.DefaultRegistry() map[string]func() View`, `tui.Run(st *store.Store, pricing *model.Pricing, dbPath string) error`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/registry_test.go`:

```go
package tui

import "testing"

func TestDefaultRegistryCoversEveryDocumentedCommand(t *testing.T) {
	registry := DefaultRegistry()
	for _, name := range []string{
		"projects", "proj", "p",
		"models", "model", "m",
		"sessions", "sess", "s",
		"requests", "req", "r",
		"agents", "agent", "a",
		"workflows", "wf", "w",
		"limits", "limit", "l",
		"days", "day", "d",
		"unpriced", "unp", "u",
		"pulse", "pu",
	} {
		if _, ok := resolveCommand(name, registry); !ok {
			t.Errorf("command %q does not resolve", name)
		}
	}
}

func TestRegistryBuildersReturnFreshViews(t *testing.T) {
	registry := DefaultRegistry()
	build := registry["projects"]
	if build == nil {
		t.Fatal("projects missing")
	}
	if build().Title() != "Projects" {
		t.Errorf("title = %q", build().Title())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run Registry -v`
Expected: FAIL — `undefined: DefaultRegistry`

- [ ] **Step 3: Write the registry**

Create `internal/tui/registry.go`:

```go
package tui

// DefaultRegistry maps every command name and alias to a view constructor.
// Aliases are listed explicitly rather than derived, so the set is greppable
// and a typo cannot silently shadow another command.
func DefaultRegistry() map[string]func() View {
	projects := func() View { return ProjectsView{} }
	models := func() View { return ModelsView{} }
	sessions := func() View { return SessionsView{} }
	requests := func() View { return RequestsView{} }
	agents := func() View { return AgentsView{} }
	workflows := func() View { return WorkflowsView{} }
	limits := func() View { return LimitsView{} }
	days := func() View { return DaysView{} }
	unpriced := func() View { return UnpricedView{} }
	pulse := func() View { return PulseView{} }
	return map[string]func() View{
		"projects": projects, "proj": projects, "p": projects,
		"models": models, "model": models, "m": models,
		"sessions": sessions, "sess": sessions, "s": sessions,
		"requests": requests, "req": requests, "r": requests,
		"agents": agents, "agent": agents, "a": agents,
		"workflows": workflows, "wf": workflows, "w": workflows,
		"limits": limits, "limit": limits, "l": limits,
		"days": days, "day": days, "d": days,
		"unpriced": unpriced, "unp": unpriced, "u": unpriced,
		"pulse": pulse, "pu": pulse,
	}
}
```

- [ ] **Step 4: Delete the old screen, rehoming its surviving helpers**

Six helpers in `tui.go` have been in use since Task 11 and must move before it goes, or the package stops compiling.

```bash
git rm internal/tui/tui.go internal/tui/tui_test.go
```

Add to `internal/tui/app.go` (moved verbatim from `tui.go`):

```go
func formatTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(tokens)/1e9)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1e6)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fK", float64(tokens)/1e3)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}
```

Add to `internal/tui/view_limits.go` (moved from `tui.go`, with `resetIn` losing its now-redundant "resets " prefix since the column header already says RESETS):

```go
type expectedLimit struct {
	tool model.Tool
	kind model.LimitKind
}

func limitKey(tool model.Tool, kind model.LimitKind, scope string) string {
	return string(tool) + "\x00" + string(kind) + "\x00" + scope
}

func limitLabel(kind model.LimitKind, scope string) string {
	if scope != "" {
		return scope
	}
	switch kind {
	case model.KindWeeklyAll:
		return "weekly"
	case model.KindCodex5h:
		return "5h"
	case model.KindCodexWeekly:
		return "weekly"
	default:
		return string(kind)
	}
}

func resetIn(value *time.Time) string {
	if value == nil {
		return "no reset time"
	}
	duration := time.Until(*value)
	if duration <= 0 {
		return "resetting"
	}
	if duration >= 24*time.Hour {
		return fmt.Sprintf("%dd %dh", int(duration.Hours())/24, int(duration.Hours())%24)
	}
	return fmt.Sprintf("%dh%02dm", int(duration.Hours()), int(duration.Minutes())%60)
}

func formatAge(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "<1m"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
}
```

Then append to `internal/tui/app.go`:

```go
// Run starts the TUI. The landing view is Projects, per spec §11.2.
func Run(st *store.Store, pricing *model.Pricing, dbPath string) error {
	m := New(st, pricing, dbPath, ProjectsView{}, DefaultRegistry())
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
```

- [ ] **Step 5: Update the caller**

In `cmd/ccdash/main.go`, the no-argument case calls `tui.Run(st, pricing)`. Change it to pass the database path:

```go
		if err := tui.Run(st, pricing, dbPath); err != nil {
```

using whatever variable already holds the resolved database path in that function. If none exists, compute it with the same expression used to open the store.

- [ ] **Step 6: Verify the newline guard now passes**

Run: `go test ./internal/tui -run TestNoNewlinesInsideStyledRender -v`
Expected: PASS. This is the first point at which it goes green — the three offending `Render("\n…\n")` calls left with `tui.go`.

- [ ] **Step 7: Run the full suite**

```bash
CGO_ENABLED=0 go build ./...
go test ./... -race
```
Expected: every package `ok`, no failures.

- [ ] **Step 8: Manual gate under a real PTY**

```bash
CGO_ENABLED=0 go build -o ccdash ./cmd/ccdash
( sleep 6; printf 'q'; sleep 2 ) | COLUMNS=177 LINES=58 script -q /dev/null ./ccdash > /tmp/ccdash-tui.txt 2>&1
perl -pe 's/\e\[[0-9;?]*[a-zA-Z]//g; s/\r//g' /tmp/ccdash-tui.txt | tail -60
```

Verify by eye:
- the frame fills all 58 rows and 177 columns, with no unwritten region
- no section's first row is indented relative to the rows below it
- `:models`, `:limits`, `:pulse` switch views; `enter` drills; `esc` returns
- the refresh age in the footer advances
- every cost reads "at API rates"

- [ ] **Step 9: Commit**

```bash
git add -A internal/tui cmd/ccdash
git commit -m "feat(tui): command registry, and delete the single-screen Overview

TestNoNewlinesInsideStyledRender goes green with this commit: the three
lipgloss Render calls containing newlines left along with tui.go."
```

---

## Self-Review Notes

**Spec coverage.** Every numbered spec section maps to a task. §2.1 viewport → Task 10. §2.2 indent → Task 7 (guard) and Task 16 (deletion). §2.3 chart domain → Task 1 and the pulse view in Task 15. §2.4 sparklines → Task 1 and Task 8's shared domain. §2.5 gauge track → Task 1. §2.6 truncation → Task 9's scrolling table. §2.7 dates and paths → Task 1 and Task 11. §3 architecture → Tasks 7-13. §4 layout → Task 10. §5.1 command bar → Task 12. §5.2 filter → Task 9 and Task 12. §5.3 table → Tasks 8 and 9. §5.4 theme → Task 7. §5.5 keybindings → Tasks 11 and 12. §6 data layer → Tasks 2-6. §7 render fixes → Task 1. §8 refresh → Task 13. §9 testing → distributed. §10 deletions → Task 16.

**Two spec statements were corrected while planning**, both recorded at the top of this plan: `agg.Filter` must gain three fields, and views live in package `tui` rather than a `views` subpackage that would import-cycle.

**One interface was added beyond the spec.** `Renderer` (Task 15) lets the pulse chart paint its own body. The spec implied every view is a table; the chart is not, and forcing it into rows would have been worse than a third small optional interface alongside `Paginator`.

**Deliberately deferred**, recorded here so it is not mistaken for an oversight: the `?` help overlay is bound in the spec's keybinding table but has no task. It is a single static panel and depends on nothing; add it after Task 16 if wanted. Paginated views load their next page when the selection hits bottom — Task 9 exposes `AtBottom()` and Task 15 provides `Page`, but the wiring between them lives in Task 16's app loop and is exercised only by the manual gate, not a unit test.

**Known-failing window.** `TestNoNewlinesInsideStyledRender` fails from Task 7 through Task 15 because the old `tui.go` still contains the defect it detects. This is intentional and called out in the Task 7 commit message; it goes green in Task 16. An executor who sees that failure in Tasks 8-15 should confirm it names only `tui.go` and continue.

