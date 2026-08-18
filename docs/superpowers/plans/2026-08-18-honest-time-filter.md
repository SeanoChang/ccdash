# Honest Time Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ccdash's time filter tell the truth — rolling windows that follow
the clock, calendar windows aligned to the local zone, and a header that reports
the filter's bounds rather than the data's extent.

**Architecture:** Replace the frozen absolute `Model.scope.From` with a
declarative `timeRange` value resolved against an injectable clock on every
refresh. Move `internal/agg` from UTC to the machine's local zone at its two
normalization points. Make the filter's upper bound half-open so calendar
windows cannot double-count a boundary instant.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, Lip Gloss v1.1.0,
modernc.org/sqlite v1.56.0. Standard library `testing` only — no assertion
library is used in this repo.

**Spec:** `docs/superpowers/specs/2026-08-18-analytics-dashboard-design.md`
(this plan implements Stage 1 of §8 only)

## Global Constraints

- Go 1.26; no new module dependencies. `go.mod` must be unchanged by this plan.
- Tests use the standard library only. Follow the existing style: table-driven
  where natural, `t.Helper()` on seed functions, `t.TempDir()` for stores.
- Calendar arithmetic uses `time.Date` and `AddDate`, never `Add` or `Truncate`.
  A local day is 23 or 25 hours across a DST transition.
- Comments explain why, not what. The repo's existing comments are the reference
  for tone.
- Every commit leaves `go build ./...` and `go test ./...` green.
- Do not modify `internal/tui/quit.go`, `internal/tui/quit_test.go`, or
  `internal/model/pricing.go`. Those carry another session's work.

---

## Task 1: Repair the red baseline

The suite is red before any of this plan's work begins. Eleven tests across
`internal/agg` and `internal/tui` assert em-dash rendering for unpriceable
costs, using `gpt-5-codex` as the fixture model. That model acquired a published
rate on 2026-08-18, so its fixture rows now price to roughly `$0.00125`, which
formats as `$0.00`. The assertions are correct; the fixture's premise is not.

Stage 1 rewrites `internal/agg`, so this must be green first or the executor
cannot distinguish their own regressions from inherited ones.

**Files:**

- Modify: `internal/agg/unpriced_test.go` (lines 13, 27, 30, 94, 97, 100, 103)
- Modify: `internal/agg/request_test.go` (lines 68, 69, 80, 92)
- Modify: `internal/tui/views_test.go` (lines 27, 163, 172, 199, 200)
- Modify: `internal/tui/views_drill_test.go` (line 73)
- Modify: `internal/tui/views_unpriced_test.go` (lines 32, 36, 85)

Do **not** touch `internal/model/pricing_test.go`,
`internal/model/normalize_test.go`, or `internal/store/store_test.go`. Their
uses of `gpt-5-codex` are deliberate and still correct.

**Interfaces:**

- Consumes: nothing.
- Produces: `unpricedFixtureModel` constant and
  `requireUnpriced(t, pricing, name)` helper in both the `agg` and `tui` test
  packages.

- [ ] **Step 1: Confirm the failure and its cause**

Run: `go test ./... 2>&1 | grep -c -- "--- FAIL"`
Expected: `16`

That is 11 failing tests plus 5 subtests of
`TestViewsShowEmDashForUnpriceableCosts`. Go indents a subtest failure as
`    --- FAIL:`, which this grep also matches, so the line count exceeds the
test count. To count tests rather than lines, use `grep -c '^--- FAIL'`, which
prints `11`.

Run:
`go test ./internal/tui/ -run TestViewsShowEmDashForUnpriceableCosts 2>&1 | head -4`
Expected: `unpriceable row "gpt-5-codex" cost = "$0.00", want an em dash`

- [ ] **Step 2: Add the fixture guard to the agg test package**

Append to `internal/agg/unpriced_test.go`:

```go
// unpricedFixtureModel is a model the default rate table has no entry for.
// These tests previously used gpt-5-codex, which acquired a published rate on
// 2026-08-18; the fixture rows then priced to $0.00125, printed as "$0.00",
// and five em-dash assertions failed for a reason none of them named.
// requireUnpriced makes that failure say what actually happened.
const unpricedFixtureModel = "codex-auto-review"

func requireUnpriced(t *testing.T, pricing *model.Pricing, name string) {
	t.Helper()
	if pricing.HasRate(model.NormalizeModel(name)) {
		t.Fatalf("fixture model %q now has a published rate; these tests need "+
			"a model the default table cannot price — pick another", name)
	}
}
```

- [ ] **Step 3: Repoint the agg fixtures**

In `internal/agg/unpriced_test.go`, replace both record literals'
`Model: "gpt-5-codex"` with `Model: unpricedFixtureModel`, change the
`case "gpt-5-codex":` to `case unpricedFixtureModel:`, and change the three
`t.Errorf` prefixes from `gpt-5-codex` to `%s` with `unpricedFixtureModel` as
the first argument. Update the doc comment on `seedUnpricedRollups` to name
`unpricedFixtureModel` instead of `gpt-5-codex`.

Add this as the first statement inside `seedUnpricedRollups`, after
`t.Helper()`:

```go
requireUnpriced(t, model.DefaultPricing(), unpricedFixtureModel)
```

Apply the same replacement in `internal/agg/request_test.go` at lines 68, 69,
80, and 92, and add the same `requireUnpriced` call inside `seedUnpriced` after
its `t.Helper()`.

- [ ] **Step 4: Run the agg package**

Run:
`go test ./internal/agg/ -v 2>&1 | grep -E "^(--- )?(FAIL|ok|PASS: Test.*Unpriced)"`
Expected: no `FAIL` lines; the package reports `ok`.

- [ ] **Step 5: Add the fixture guard to the tui test package**

Append the identical `unpricedFixtureModel` constant and `requireUnpriced`
helper to `internal/tui/views_unpriced_test.go`. The two packages cannot share
it — they are separate packages and this repo has no test-helper package.

- [ ] **Step 6: Repoint the tui fixtures**

Replace `gpt-5-codex` with `unpricedFixtureModel` in
`internal/tui/views_test.go` (lines 27, 163, 172, 199, 200),
`internal/tui/views_drill_test.go` (line 73), and
`internal/tui/views_unpriced_test.go` (lines 32, 36, 85). Add
`requireUnpriced(t, model.DefaultPricing(), unpricedFixtureModel)` to the seed
function in each file, after `t.Helper()`.

Two of those hits are inside message strings, not expressions, so substituting
the identifier there will not compile. Handle each kind separately:

- **Expressions** take the constant directly:
  `views_test.go:27` (`Model:`), `:163`
  (`strings.Contains(row.Cells[0].Text, unpricedFixtureModel)`), `:199`
  (`rows[0].Key != unpricedFixtureModel`), and
  `views_unpriced_test.go:32`, `:36`, `:85`.
- **Message strings** switch to a format verb, so the message names whichever
  model the fixture actually uses:

```go
// views_test.go:172
t.Errorf("%s must be listed even though it has no rate", unpricedFixtureModel)

// views_test.go:200
t.Errorf("key = %q, want %s", rows[0].Key, unpricedFixtureModel)

// views_drill_test.go:73
t.Errorf("the %s request must render its cost as —, not $0.00",
	unpricedFixtureModel)
```

The same distinction applies to the three `t.Errorf` calls in
`internal/agg/unpriced_test.go` and the one in `internal/agg/request_test.go:69`
covered by Step 3.

- [ ] **Step 7: Run the full suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: every package `ok`, no `FAIL`.

- [ ] **Step 8: Commit**

```bash
git add internal/agg/unpriced_test.go internal/agg/request_test.go \
        internal/tui/views_test.go internal/tui/views_drill_test.go \
        internal/tui/views_unpriced_test.go
git commit -m "test: repoint unpriced fixtures off gpt-5-codex now that it has a rate

The em-dash assertions used gpt-5-codex as a model the default table could
not price. It gained a published rate, so the fixture rows priced to
\$0.00125 and printed as \$0.00 — eleven tests failed without naming the
cause. requireUnpriced now fails with the reason instead."
```

---

## Task 2: The timeRange value type

**Files:**

- Create: `internal/tui/timerange.go`
- Test: `internal/tui/timerange_test.go`

**Interfaces:**

- Consumes: nothing.
- Produces: `type timeRange struct{ kind rangeKind; span time.Duration }`;
  constants `rangeAll`, `rangeRolling`, `rangeToday`, `rangeWeek`, `rangeMonth`;
  methods `bounds(now time.Time) (from, to time.Time)`, `label() string`,
  `short() string`, `calendar() bool`; free functions
  `startOfDay(time.Time) time.Time`, `startOfWeek(time.Time) time.Time`,
  `startOfMonth(time.Time) time.Time`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/timerange_test.go`:

```go
package tui

import (
	"testing"
	"time"
)

// newYork is the zone the DST cases need. A local day there is 23 hours on
// 2026-03-08 and 25 hours on 2026-11-01, which is what separates calendar
// arithmetic from duration arithmetic.
func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	return loc
}

func TestBoundsForEachKind(t *testing.T) {
	loc := newYork(t)
	now := time.Date(2026, 8, 18, 15, 30, 0, 0, loc)

	cases := []struct {
		name       string
		rng        timeRange
		wantFrom   time.Time
		wantTo     time.Time
		wantIsZero bool
	}{
		{
			name:     "rolling 7d",
			rng:      timeRange{kind: rangeRolling, span: 7 * 24 * time.Hour},
			wantFrom: now.Add(-7 * 24 * time.Hour),
			wantTo:   now,
		},
		{
			name:     "today",
			rng:      timeRange{kind: rangeToday},
			wantFrom: time.Date(2026, 8, 18, 0, 0, 0, 0, loc),
			wantTo:   time.Date(2026, 8, 19, 0, 0, 0, 0, loc),
		},
		{
			name:     "this week starts Monday",
			rng:      timeRange{kind: rangeWeek},
			wantFrom: time.Date(2026, 8, 17, 0, 0, 0, 0, loc),
			wantTo:   time.Date(2026, 8, 24, 0, 0, 0, 0, loc),
		},
		{
			name:     "this month",
			rng:      timeRange{kind: rangeMonth},
			wantFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, loc),
			wantTo:   time.Date(2026, 9, 1, 0, 0, 0, 0, loc),
		},
		{
			name:       "all",
			rng:        timeRange{kind: rangeAll},
			wantIsZero: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, to := c.rng.bounds(now)
			if c.wantIsZero {
				if !from.IsZero() || !to.IsZero() {
					t.Fatalf("bounds = %v..%v, want both zero", from, to)
				}
				return
			}
			if !from.Equal(c.wantFrom) {
				t.Errorf("from = %v, want %v", from, c.wantFrom)
			}
			if !to.Equal(c.wantTo) {
				t.Errorf("to = %v, want %v", to, c.wantTo)
			}
		})
	}
}

// TestTodaySpansTheRealDayAcrossDST is the reason calendar boundaries go
// through time.Date and AddDate. now.Add(-24*time.Hour) would report 24 hours
// on both of these days and be wrong on both.
func TestTodaySpansTheRealDayAcrossDST(t *testing.T) {
	loc := newYork(t)
	for _, c := range []struct {
		day   time.Time
		hours float64
	}{
		{time.Date(2026, 3, 8, 12, 0, 0, 0, loc), 23},  // spring forward
		{time.Date(2026, 11, 1, 12, 0, 0, 0, loc), 25}, // fall back
		{time.Date(2026, 8, 17, 12, 0, 0, 0, loc), 24}, // ordinary day
	} {
		from, to := timeRange{kind: rangeToday}.bounds(c.day)
		if got := to.Sub(from).Hours(); got != c.hours {
			t.Errorf("%s: day spans %.0fh, want %.0fh",
				c.day.Format("2006-01-02"), got, c.hours)
		}
	}
}

func TestLabels(t *testing.T) {
	for _, c := range []struct {
		rng   timeRange
		label string
		short string
	}{
		{timeRange{kind: rangeAll}, "all", "all"},
		{timeRange{kind: rangeRolling, span: 24 * time.Hour}, "last 24h", "24h"},
		{timeRange{kind: rangeRolling, span: 7 * 24 * time.Hour}, "last 7d", "7d"},
		{timeRange{kind: rangeToday}, "today", "today"},
		{timeRange{kind: rangeWeek}, "this week", "week"},
		{timeRange{kind: rangeMonth}, "this month", "month"},
	} {
		if got := c.rng.label(); got != c.label {
			t.Errorf("label() = %q, want %q", got, c.label)
		}
		if got := c.rng.short(); got != c.short {
			t.Errorf("short() = %q, want %q", got, c.short)
		}
	}
}

func TestCalendarReportsAFixedEnd(t *testing.T) {
	for _, c := range []struct {
		rng  timeRange
		want bool
	}{
		{timeRange{kind: rangeToday}, true},
		{timeRange{kind: rangeWeek}, true},
		{timeRange{kind: rangeMonth}, true},
		{timeRange{kind: rangeRolling, span: time.Hour}, false},
		{timeRange{kind: rangeAll}, false},
	} {
		if got := c.rng.calendar(); got != c.want {
			t.Errorf("%v calendar() = %v, want %v", c.rng.label(), got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/tui/ -run 'TestBounds|TestToday|TestLabels|TestCalendar' 2>&1 | head -10`
Expected: build failure, `undefined: timeRange`

- [ ] **Step 3: Write the implementation**

Create `internal/tui/timerange.go`:

```go
package tui

import (
	"fmt"
	"time"
)

type rangeKind int

const (
	rangeAll rangeKind = iota
	rangeRolling
	rangeToday
	rangeWeek
	rangeMonth
)

// timeRange is the window the user asked for, held as intent rather than as
// bounds. Bounds are resolved against a clock at query time, so a rolling
// window keeps following the clock instead of freezing at the keystroke that
// chose it — which is what "last 24h" silently stopped meaning after the
// dashboard had been open for six hours.
type timeRange struct {
	kind rangeKind
	span time.Duration // rangeRolling only
}

// startOfDay is calendar arithmetic, not duration arithmetic. time.Date
// normalizes through a DST transition; t.Add(-24*time.Hour) does not, and a
// local day is 23 or 25 hours twice a year.
func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

// startOfWeek returns the Monday at or before t. Go numbers Sunday as 0, so
// the shift maps Monday to 0 and Sunday to 6.
func startOfWeek(t time.Time) time.Time {
	day := startOfDay(t)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

func startOfMonth(t time.Time) time.Time {
	year, month, _ := t.Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, t.Location())
}

// bounds resolves the window to the half-open interval [from, to). A zero
// pair means unbounded, which is how rangeAll is expressed. The interval is
// half-open because a request landing exactly on midnight must belong to one
// calendar bucket, not to two.
func (r timeRange) bounds(now time.Time) (from, to time.Time) {
	switch r.kind {
	case rangeRolling:
		return now.Add(-r.span), now
	case rangeToday:
		start := startOfDay(now)
		return start, start.AddDate(0, 0, 1)
	case rangeWeek:
		start := startOfWeek(now)
		return start, start.AddDate(0, 0, 7)
	case rangeMonth:
		start := startOfMonth(now)
		return start, start.AddDate(0, 1, 0)
	default:
		return time.Time{}, time.Time{}
	}
}

func (r timeRange) label() string {
	switch r.kind {
	case rangeRolling:
		return "last " + spanText(r.span)
	case rangeToday:
		return "today"
	case rangeWeek:
		return "this week"
	case rangeMonth:
		return "this month"
	default:
		return "all"
	}
}

// short is the label for a collapsed header, where whole fields are kept or
// dropped and a long label would push the tool filter off the line.
func (r timeRange) short() string {
	switch r.kind {
	case rangeRolling:
		return spanText(r.span)
	case rangeToday:
		return "today"
	case rangeWeek:
		return "week"
	case rangeMonth:
		return "month"
	default:
		return "all"
	}
}

// calendar reports whether the window has a fixed end. Only a window with an
// end can be projected to that end; a rolling window has none, so a caller
// showing a projection must ask before inventing a boundary.
func (r timeRange) calendar() bool {
	switch r.kind {
	case rangeToday, rangeWeek, rangeMonth:
		return true
	}
	return false
}

func spanText(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:
`go test ./internal/tui/ -run 'TestBounds|TestToday|TestLabels|TestCalendar' -v 2>&1 | grep -E "^(=== RUN|--- |ok|FAIL)"`
Expected: every listed test `PASS`, package `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/timerange.go internal/tui/timerange_test.go
git commit -m "feat(tui): add timeRange, a window held as intent not as bounds

Calendar boundaries go through time.Date and AddDate so a local day is 23
or 25 hours across a DST transition rather than always 24."
```

---

## Task 3: Bucket and display in the local zone

`internal/agg` normalizes every timestamp to UTC at two points and cuts day
buckets at UTC midnight. On a machine at UTC−4 that files any work after 20:00
local under tomorrow's date, in the Days view, the Pulse axis, and every
`Started` column.

**Files:**

- Modify: `internal/agg/agg.go:88` (`scanRows`), `internal/agg/agg.go:153-156`
  (`dayUTC`), `internal/agg/agg.go:414` (`scanDetail`)
- Test: `internal/agg/agg_test.go`

**Interfaces:**

- Consumes: nothing from earlier tasks.
- Produces: `dayIn(t time.Time, loc *time.Location) time.Time` and
  `dayLocal(t time.Time) time.Time`. `dayUTC` is removed; every caller moves to
  `dayLocal`.

- [ ] **Step 1: Write the failing test**

Append to `internal/agg/agg_test.go`:

```go
// TestDayInBucketsByTheGivenZone pins the boundary that matters: an evening
// session in a western zone belongs to the day the user experienced, not to
// the next UTC day. dayIn takes a location so this is testable without
// depending on the machine's zone.
func TestDayInBucketsByTheGivenZone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	// 2026-08-18 01:30 UTC is 2026-08-17 21:30 in New York.
	instant := time.Date(2026, 8, 18, 1, 30, 0, 0, time.UTC)

	if got := dayIn(instant, loc).Format("2006-01-02"); got != "2026-08-17" {
		t.Errorf("dayIn(New York) = %s, want 2026-08-17", got)
	}
	if got := dayIn(instant, time.UTC).Format("2006-01-02"); got != "2026-08-18" {
		t.Errorf("dayIn(UTC) = %s, want 2026-08-18", got)
	}
}

// TestScannedTimestampsCarryTheLocalZone guards the two normalization points.
// Every display site formats from these, so a UTC location here silently
// relabels every timestamp in the application.
func TestScannedTimestampsCarryTheLocalZone(t *testing.T) {
	db := seedUnpricedRollups(t)

	rows, err := scanRows(db, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows scanned")
	}
	if got := rows[0].record.TS.Location(); got != time.Local {
		t.Errorf("scanRows location = %v, want %v", got, time.Local)
	}

	detail, err := scanDetail(db, Filter{}, "ts ASC", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail) == 0 {
		t.Fatal("no detail rows scanned")
	}
	if got := detail[0].TS.Location(); got != time.Local {
		t.Errorf("scanDetail location = %v, want %v", got, time.Local)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/agg/ -run 'TestDayIn|TestScannedTimestamps' 2>&1 | head -10`
Expected: build failure, `undefined: dayIn`

- [ ] **Step 3: Write the implementation**

In `internal/agg/agg.go`, replace `dayUTC` with:

```go
// dayIn truncates to midnight in loc. It is split from dayLocal so tests can
// pin a zone without depending on the machine's.
func dayIn(t time.Time, loc *time.Location) time.Time {
	year, month, day := t.In(loc).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// dayLocal buckets by the user's wall clock. Bucketing in UTC filed an
// evening session in a western zone under the following day, in the Days
// view, the Pulse axis and every Started column at once.
func dayLocal(t time.Time) time.Time { return dayIn(t, time.Local) }
```

Change line 88 in `scanRows` from `record.TS = time.Unix(ts, 0).UTC()` to:

```go
record.TS = time.Unix(ts, 0).Local()
```

Change line 414 in `scanDetail` from `row.Record.TS = time.Unix(ts, 0).UTC()`
to:

```go
row.Record.TS = time.Unix(ts, 0).Local()
```

- [ ] **Step 4: Replace every dayUTC call site**

Run: `grep -rn "dayUTC" --include="*.go" .`
Expected: hits in `ByDay` and
`ByProject` in `internal/agg/agg.go`.

Replace each `dayUTC(` with `dayLocal(`. Re-run the grep; expect no output.

- [ ] **Step 5: Run the tests to verify they pass**

Run:
`go test ./internal/agg/ -run 'TestDayIn|TestScannedTimestamps' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: both `PASS`, package `ok`.

- [ ] **Step 6: Pin the zone-dependent fixtures**

This step is not conditional. The change was trialled against the current tree
and produced no failures beyond the eleven Task 1 removes — but only because
this machine runs at UTC−4. Every existing fixture instant is written in UTC
near midday, which lands on the same date in the Americas and on the **next**
date anywhere east of UTC+7:

| Fixture instant                            | UTC−4 date | UTC+13 date |
| ------------------------------------------ | ---------- | ----------- |
| `unpriced_test.go` `2026-08-15T12:00Z`     | 2026-08-15 | 2026-08-16  |
| `views_test.go` `time.Unix(1_700_000_000)` | 2023-11-14 | 2023-11-15  |
| `views_test.go` `time.Unix(1_700_086_400)` | 2023-11-15 | 2023-11-16  |
| `views_test.go` `time.Unix(1_700_172_800)` | 2023-11-16 | 2023-11-17  |

After this task those tests pass here and fail in New Zealand or on any CI
runner not pinned to a western zone. Pin them now rather than leaving a latent
zone dependency.

In `internal/agg/unpriced_test.go`, change the seed instant to construct in the
local zone:

```go
day := time.Date(2026, 8, 15, 12, 0, 0, 0, time.Local)
```

In `internal/tui/views_test.go`, replace the three `time.Unix(...)` literals
with local-zone dates at midday, keeping them one day apart:

```go
day1 := time.Date(2023, 11, 14, 12, 0, 0, 0, time.Local)
day2 := day1.AddDate(0, 0, 1)
day3 := day1.AddDate(0, 0, 2)
```

Apply the same treatment to the seed instants in
`internal/tui/views_unpriced_test.go`.

Never change an expected date string to match a new answer. Change the fixture
so the instant is unambiguous in every zone.

- [ ] **Step 7: Confirm the suite is green, in two zones**

Run: `go test ./... 2>&1 | grep -E "FAIL|ok " | head -20`
Expected: no `FAIL`
lines.

Run:
`TZ=Pacific/Auckland go test ./internal/agg/ ./internal/tui/ -count=1 2>&1 | tail -5`
Expected: no `FAIL` lines. This is the check that proves Step 6 worked; it fails
if any fixture still depends on the machine's zone.

- [ ] **Step 8: Commit**

```bash
git add internal/agg/agg.go internal/agg/agg_test.go
git commit -m "fix(agg): bucket and display in the local zone, not UTC

A UTC day boundary filed any work after 20:00 in a UTC-4 zone under the
following date, in the Days view, the Pulse axis and every Started column.
dayIn takes a location so the boundary is testable without the machine's."
```

---

## Task 4: A half-open upper bound

**Files:**

- Modify: `internal/agg/agg.go:29-32` (`Filter.where`)
- Test: `internal/agg/agg_test.go`

**Interfaces:**

- Consumes: nothing.
- Produces: `Filter.To` is now exclusive. Every later task that sets `To` relies
  on this.

- [ ] **Step 1: Write the failing test**

Append to `internal/agg/agg_test.go`:

```go
// TestFilterUpperBoundIsExclusive pins the interval as [From, To). A calendar
// window's To is the next window's From, so an inclusive bound would count a
// request landing exactly on midnight in both months.
func TestFilterUpperBoundIsExclusive(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	boundary := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "before", Tool: model.ToolClaude, TS: boundary.Add(-time.Second),
			Model: "claude-opus-5", Session: "s1", OutputTok: 10},
		{ID: "on", Tool: model.ToolClaude, TS: boundary,
			Model: "claude-opus-5", Session: "s1", OutputTok: 10},
	}); err != nil {
		t.Fatal(err)
	}

	totals, err := Totals(s.DB(), model.DefaultPricing(),
		Filter{From: boundary.AddDate(0, -1, 0), To: boundary})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Requests != 1 {
		t.Errorf("requests = %d, want 1 — the row on the boundary belongs to "+
			"the next window, not this one", totals.Requests)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/agg/ -run TestFilterUpperBoundIsExclusive -v 2>&1 | grep -E "^(--- |ok|FAIL|.*want 1)"`
Expected: `FAIL`, with `requests = 2, want 1`

- [ ] **Step 3: Write the implementation**

In `internal/agg/agg.go`, inside `Filter.where`, change the `To` clause from
`"ts <= ?"` to:

```go
	if !f.To.IsZero() {
		// Exclusive: a calendar window's To is the next window's From, so an
		// inclusive bound would count a midnight request in both.
		conditions = append(conditions, "ts < ?")
		args = append(args, f.To.Unix())
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/agg/ 2>&1 | tail -5` Expected:
`ok github.com/seanochang/ccdash/internal/agg`

- [ ] **Step 5: Commit**

```bash
git add internal/agg/agg.go internal/agg/agg_test.go
git commit -m "fix(agg): make the filter's upper bound exclusive

A calendar window's To is the next window's From. An inclusive bound put a
request landing exactly on midnight in both windows."
```

---

## Task 5: Hour and day bucket resolution

A 24-hour window rendered against day buckets produces a one or two bar chart.
`ByDay` gains a resolution parameter and keeps its old signature as a wrapper,
so `ByProject`'s sparkline and every existing caller are untouched.

**Files:**

- Modify: `internal/agg/agg.go` (`ByDay`, add `ByBucket`)
- Test: `internal/agg/agg_test.go`

**Interfaces:**

- Consumes: `dayLocal` and `dayIn` from Task 3.
- Produces: `type Resolution int` with `ResDay` and `ResHour`;
  `func ByBucket(db *sql.DB, pricing *model.Pricing, filter Filter, res Resolution) ([]DayBucket, error)`.
  `ByDay(db, pricing, filter)` remains and calls
  `ByBucket(db, pricing, filter, ResDay)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/agg/agg_test.go`:

```go
// TestByBucketAtHourResolution covers the window a day-bucketed chart cannot
// draw: six hours of activity is one bar by day and six by hour.
func TestByBucketAtHourResolution(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	base := time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)
	var recs []model.Record
	for i := range 6 {
		recs = append(recs, model.Record{
			ID: fmt.Sprintf("h%d", i), Tool: model.ToolClaude,
			TS: base.Add(time.Duration(i) * time.Hour),
			Model: "claude-opus-5", Session: "s1", OutputTok: 1000,
		})
	}
	if _, err := s.UpsertRecords(recs); err != nil {
		t.Fatal(err)
	}

	byHour, err := ByBucket(s.DB(), model.DefaultPricing(), Filter{}, ResHour)
	if err != nil {
		t.Fatal(err)
	}
	if len(byHour) != 6 {
		t.Errorf("hour buckets = %d, want 6", len(byHour))
	}
	if !byHour[0].Day.Equal(base) {
		t.Errorf("first hour bucket = %v, want %v", byHour[0].Day, base)
	}

	byDay, err := ByBucket(s.DB(), model.DefaultPricing(), Filter{}, ResDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDay) != 1 {
		t.Errorf("day buckets = %d, want 1", len(byDay))
	}
}

// TestByDayStillBucketsByDay keeps the wrapper honest: ByProject's sparkline
// and every existing caller depend on this signature and this behaviour.
func TestByDayStillBucketsByDay(t *testing.T) {
	got, err := ByDay(seedUnpricedRollups(t), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("day buckets = %d, want 1", len(got))
	}
}
```

Add `"fmt"` to that file's imports if it is not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/agg/ -run 'TestByBucket|TestByDayStill' 2>&1 | head -10`
Expected: build failure, `undefined: ByBucket`

- [ ] **Step 3: Write the implementation**

In `internal/agg/agg.go`, add above `ByDay`:

```go
// Resolution is the width of one chart bucket. A window narrower than a day
// has nothing to plot against day buckets, so the caller picks from the span
// it resolved rather than from a constant.
type Resolution int

const (
	ResDay Resolution = iota
	ResHour
)

// bucketOf truncates t to the start of its bucket in the local zone.
func bucketOf(t time.Time, res Resolution) time.Time {
	if res == ResHour {
		local := t.Local()
		year, month, day := local.Date()
		return time.Date(year, month, day, local.Hour(), 0, 0, 0, time.Local)
	}
	return dayLocal(t)
}
```

Rename the existing `ByDay` body to `ByBucket` by changing its signature and its
one bucketing call:

```go
func ByBucket(db *sql.DB, pricing *model.Pricing, filter Filter,
	res Resolution) ([]DayBucket, error) {
```

and inside the loop replace `day := dayUTC(row.record.TS)` — already `dayLocal`
after Task 3 — with:

```go
		day := bucketOf(row.record.TS, res)
```

Then add the wrapper:

```go
// ByDay is ByBucket at day resolution. ByProject's sparkline and the Days
// view are both day-shaped, so they keep this signature.
func ByDay(db *sql.DB, pricing *model.Pricing, filter Filter) ([]DayBucket, error) {
	return ByBucket(db, pricing, filter, ResDay)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:
`go test ./internal/agg/ -v -run 'TestByBucket|TestByDayStill' 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: both `PASS`, package `ok`.

- [ ] **Step 5: Run the full suite**

Run: `go test ./... 2>&1 | grep -E "FAIL" | head`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/agg/agg.go internal/agg/agg_test.go
git commit -m "feat(agg): add ByBucket with hour and day resolution

A 24-hour window against day buckets is a one-bar chart. ByDay stays as the
day-resolution wrapper so ByProject's sparkline is untouched."
```

---

## Task 6: Resolve the window against a clock on every refresh

This is the task that fixes the freeze. `setRange` currently stores an absolute
instant, so a window chosen once never moves again.

**Files:**

- Modify: `internal/tui/app.go` — `Model` struct (line 30), `New` (line 64),
  `setRange` (line 401), `applyScope` (line 413), the `tickMsg` case in `Update`
- Test: `internal/tui/app_test.go`

**Interfaces:**

- Consumes: `timeRange`, `rangeRolling`, `rangeAll` from Task 2.
- Produces: `Model.timeRange timeRange`, `Model.now func() time.Time`,
  `(*Model).resolveScope()`. `Model.rangeLabel` is removed; Task 7 reads
  `m.timeRange.label()` instead.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/app_test.go`:

```go
// TestRollingWindowFollowsTheClock is the regression test for a window that
// froze at the keystroke that chose it. Pressing "d" then leaving ccdash open
// for six hours showed a 30-hour window still labelled "last 24h".
func TestRollingWindowFollowsTheClock(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)
	m := newTestModel()
	m.now = func() time.Time { return clock }

	next, _ := m.Update(key("d"))
	m = next.(Model)
	first := m.scope.From

	clock = clock.Add(6 * time.Hour)
	m.resolveScope()

	if !m.scope.From.After(first) {
		t.Errorf("From = %v after six hours, want later than %v",
			m.scope.From, first)
	}
	if got := m.scope.To.Sub(m.scope.From); got != 24*time.Hour {
		t.Errorf("window spans %v, want 24h — a rolling window keeps its "+
			"width as it follows the clock", got)
	}
}

// TestResolveScopeReachesEveryStackLevel keeps a drilled view consistent with
// the header rather than holding the bounds it was pushed with.
func TestResolveScopeReachesEveryStackLevel(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)
	m := newTestModel()
	m.now = func() time.Time { return clock }

	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Fatalf("stack depth = %d, want 2", len(m.stack))
	}

	next, _ = m.Update(key("w"))
	m = next.(Model)
	for i, entry := range m.stack {
		if !entry.scope.From.Equal(m.scope.From) {
			t.Errorf("stack[%d].From = %v, want %v",
				i, entry.scope.From, m.scope.From)
		}
	}
}

// TestRangeAllClearsBothBounds guards the one kind with no bounds at all.
func TestRangeAllClearsBothBounds(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local) }

	next, _ := m.Update(key("w"))
	m = next.(Model)
	next, _ = m.Update(key("a"))
	m = next.(Model)

	if !m.scope.From.IsZero() || !m.scope.To.IsZero() {
		t.Errorf("all-time bounds = %v..%v, want both zero",
			m.scope.From, m.scope.To)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/tui/ -run 'TestRollingWindow|TestResolveScope|TestRangeAll' 2>&1 | head -10`
Expected: build failure, `m.now undefined` and `m.resolveScope undefined`

- [ ] **Step 3: Add the fields**

In `internal/tui/app.go`, in the `Model` struct, replace the line
`rangeLabel string` with:

```go
	timeRange timeRange
	// now is the clock the window resolves against. Tests replace it; nothing
	// else should read time.Now for range purposes.
	now func() time.Time
```

In `New`, replace `rangeLabel: "all",` with:

```go
		timeRange: timeRange{kind: rangeAll},
		now:       time.Now,
```

- [ ] **Step 4: Replace setRange and applyScope**

Replace the whole `setRange` function with:

```go
// setRange records the window the user asked for. The bounds are not stored:
// resolveScope derives them from the clock, so a rolling window keeps moving.
func (m Model) setRange(rng timeRange) (tea.Model, tea.Cmd) {
	m.timeRange = rng
	// A narrower window can only shrink the result set, so any depth the user
	// paged into is stale.
	for i := range m.stack {
		m.stack[i].pages = 1
	}
	m.applyScope()
	return m, nil
}
```

Replace `applyScope` with the pair:

```go
// resolveScope recomputes the window's bounds from the clock and pushes the
// whole scope down every stack level, so a drilled view stays consistent with
// the header. It performs no database work and is safe on the ticker.
func (m *Model) resolveScope() {
	m.scope.From, m.scope.To = m.timeRange.bounds(m.now())
	for i := range m.stack {
		m.stack[i].scope.From = m.scope.From
		m.stack[i].scope.To = m.scope.To
		m.stack[i].scope.Tool = m.scope.Tool
	}
}

// applyScope resolves and then refetches. Only the keypress paths use it; the
// ticker resolves and lets the asynchronous refresh do the fetching.
func (m *Model) applyScope() {
	m.resolveScope()
	m.reloadCurrent()
}
```

- [ ] **Step 5: Resolve on every tick**

In `Update`, in the `case tickMsg:` branch, insert `m.resolveScope()`
immediately before `return m, m.refresh(true)`:

```go
		m.inFlight = true
		m.resolveScope()
		return m, m.refresh(true)
```

- [ ] **Step 6: Update the range key bindings**

In `handleKey`, replace the four range cases with:

```go
	case "d":
		return m.setRange(timeRange{kind: rangeRolling, span: 24 * time.Hour})
	case "w":
		return m.setRange(timeRange{kind: rangeRolling, span: 7 * 24 * time.Hour})
	case "m":
		return m.setRange(timeRange{kind: rangeRolling, span: 30 * 24 * time.Hour})
	case "a":
		return m.setRange(timeRange{kind: rangeAll})
```

- [ ] **Step 7: Point rangeText at the new field**

`rangeText` still reads `m.rangeLabel`, which no longer exists. Change its first
line to `text := m.timeRange.label()` so the package compiles. Task 7 rewrites
this function properly.

- [ ] **Step 8: Run the tests to verify they pass**

Run:
`go test ./internal/tui/ -run 'TestRollingWindow|TestResolveScope|TestRangeAll' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: all three `PASS`.

Run: `go test ./... 2>&1 | grep FAIL | head`
Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "fix(tui): resolve the range against the clock instead of freezing it

setRange stored time.Now().Add(-window) once, so a window chosen at 09:00
still claimed to be 24h wide at 15:00 while covering 30. The Model now holds
the window as intent and resolves bounds on every tick."
```

---

## Task 7: A header that reports the filter, not the data

`rangeText` prints `m.totals.From` and `m.totals.To` — the extent of matching
data. With a filter active and a gap in the data, the one line that should say
what window is on screen says something else, and never says the bounds.

**Files:**

- Modify: `internal/tui/app.go` (`rangeText`, and the `headerInfo` literal in
  `View`), `internal/tui/layout.go` (`headerInfo`, `collapsedHeader`)
- Test: `internal/tui/layout_test.go`

**Interfaces:**

- Consumes: `Model.timeRange` and `Model.now` from Task 6; `timeRange.short()`
  from Task 2.
- Produces: `headerInfo.RangeShort string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/layout_test.go`:

```go
// TestRangeTextReportsFilterBoundsNotDataExtent is the regression test for a
// header that answered a question nobody asked. With a week selected and data
// only in its first two days, the header used to print the data's span.
func TestRangeTextReportsFilterBoundsNotDataExtent(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local) }
	next, _ := m.Update(key("w"))
	m = next.(Model)

	m.totals.From = time.Date(2026, 8, 11, 9, 0, 0, 0, time.Local)
	m.totals.To = time.Date(2026, 8, 12, 9, 0, 0, 0, time.Local)

	got := m.rangeText()
	if !strings.Contains(got, "last 7d") {
		t.Errorf("rangeText = %q, want it to name the window", got)
	}
	if !strings.Contains(got, "2026-08-11") || !strings.Contains(got, "2026-08-18") {
		t.Errorf("rangeText = %q, want the filter's own bounds with the year", got)
	}
	if strings.Contains(got, "2026-08-12") {
		t.Errorf("rangeText = %q, must not report the data's extent as the range", got)
	}
}

// TestRangeTextForAllTimeShowsTheDataExtent: with no window there are no
// bounds to print, so the data's span is the only honest thing to say — and
// it is labelled as such.
func TestRangeTextForAllTimeShowsTheDataExtent(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local) }
	m.totals.From = time.Date(2026, 6, 3, 9, 0, 0, 0, time.Local)
	m.totals.To = time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)

	got := m.rangeText()
	if !strings.Contains(got, "all") {
		t.Errorf("rangeText = %q, want it to say all", got)
	}
	if !strings.Contains(got, "data 2026-06-03") {
		t.Errorf("rangeText = %q, want the extent labelled as data", got)
	}
}

// TestCollapsedHeaderKeepsTheShortRange: the collapsed line keeps whole fields
// and drops the rest, so the range must arrive short enough to survive.
func TestCollapsedHeaderKeepsTheShortRange(t *testing.T) {
	line := collapsedHeader(headerInfo{
		Range: "last 7d  2026-08-11 15:00 → now", RangeShort: "7d",
		Tool: "claude", Tokens: "2.4B", Cost: "$412.80 at API rates",
	}, 40)
	if !strings.Contains(line, "7d") {
		t.Errorf("collapsed header = %q, want the short range", line)
	}
	if strings.Contains(line, "2026-08-11") {
		t.Errorf("collapsed header = %q, must use RangeShort at 40 cells", line)
	}
}
```

Ensure `strings` and `time` are imported in that file.

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/tui/ -run 'TestRangeText|TestCollapsedHeaderKeeps' 2>&1 | head -10`
Expected: build failure, `unknown field RangeShort in struct literal`

- [ ] **Step 3: Add the field**

In `internal/tui/layout.go`, add to `headerInfo`:

```go
type headerInfo struct {
	DBPath     string
	Range      string
	RangeShort string
	Tool       string
	Tokens     string
	Cost       string
	Requests   string
	Unpriced   string
}
```

In `collapsedHeader`, change the first field from `info.Range` to
`info.RangeShort`, falling back when it is empty:

```go
	short := info.RangeShort
	if short == "" {
		short = info.Range
	}
	fields := []field{{" " + short, " " + short}}
```

- [ ] **Step 4: Rewrite rangeText**

In `internal/tui/app.go`, replace `rangeText` with:

```go
// rangeText names the window and prints its own bounds. It used to print
// m.totals.From and .To — the extent of matching data — so a gap in the data
// read as a narrower filter, and the actual bounds appeared nowhere. Both
// endpoints carry the year, because a range is unreadable without it.
func (m Model) rangeText() string {
	text := m.timeRange.label()
	from, to := m.scope.From, m.scope.To
	if from.IsZero() && to.IsZero() {
		if m.totals.From.IsZero() {
			return text
		}
		return fmt.Sprintf("%s  (data %s → %s)", text,
			m.totals.From.Format("2006-01-02"),
			m.totals.To.Format("2006-01-02"))
	}
	end := to.Format("2006-01-02 15:04")
	if !m.timeRange.calendar() {
		// A rolling window's end is the clock itself; printing an instant
		// would claim a fixed edge it does not have.
		end = "now"
	}
	return fmt.Sprintf("%s  %s → %s", text, from.Format("2006-01-02 15:04"), end)
}
```

- [ ] **Step 5: Pass the short range through**

In `View`, add `RangeShort` to the `headerInfo` literal:

```go
	info := headerInfo{
		DBPath:     m.dbPath,
		Range:      m.rangeText(),
		RangeShort: m.timeRange.short(),
		Tool:       string(m.scope.Tool),
		Tokens:     formatTokens(m.totals.Tokens),
		Cost:       fmt.Sprintf("$%.2f at API rates", m.totals.Cost),
		Requests:   fmt.Sprintf("%d", m.totals.Requests),
		Unpriced:   fmt.Sprintf("%d", m.unpriced),
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run:
`go test ./internal/tui/ -run 'TestRangeText|TestCollapsedHeaderKeeps' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: all three `PASS`.

Run: `go test ./... 2>&1 | grep FAIL | head`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/layout.go internal/tui/layout_test.go
git commit -m "fix(tui): report the filter's bounds in the header, not the data's

rangeText printed totals.From and totals.To, so a gap in the data read as a
narrower filter and the real bounds appeared nowhere. Both endpoints now
carry the year, and a rolling window's end reads 'now' rather than an
instant it does not have."
```

---

## Task 8: Calendar preset keys

**Files:**

- Modify: `internal/tui/app.go` (`handleKey`), `internal/tui/help.go`
  (`helpBindings`)
- Test: `internal/tui/app_test.go`, `internal/tui/help_test.go`

**Interfaces:**

- Consumes: `setRange` from Task 6; `rangeToday`, `rangeWeek`, `rangeMonth` from
  Task 2.
- Produces: nothing later tasks depend on. This is the last task in Stage 1.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/app_test.go`:

```go
// TestCalendarPresetKeys covers the windows a rolling shortcut cannot express:
// the ones a bill is reconciled against.
func TestCalendarPresetKeys(t *testing.T) {
	clock := time.Date(2026, 8, 18, 15, 30, 0, 0, time.Local)

	for _, c := range []struct {
		key      string
		wantFrom time.Time
		label    string
	}{
		{"D", time.Date(2026, 8, 18, 0, 0, 0, 0, time.Local), "today"},
		{"W", time.Date(2026, 8, 17, 0, 0, 0, 0, time.Local), "this week"},
		{"M", time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local), "this month"},
	} {
		m := newTestModel()
		m.now = func() time.Time { return clock }
		next, _ := m.Update(key(c.key))
		m = next.(Model)

		if !m.scope.From.Equal(c.wantFrom) {
			t.Errorf("%s: From = %v, want %v", c.key, m.scope.From, c.wantFrom)
		}
		if got := m.timeRange.label(); got != c.label {
			t.Errorf("%s: label = %q, want %q", c.key, got, c.label)
		}
	}
}

// TestRangeChangeResetsPagination: a narrower window cannot need the depth the
// previous one was paged into.
func TestRangeChangeResetsPagination(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local) }
	m.stack[0].pages = 4

	next, _ := m.Update(key("D"))
	m = next.(Model)

	if m.stack[0].pages != 1 {
		t.Errorf("pages = %d after a range change, want 1", m.stack[0].pages)
	}
}
```

Append to `internal/tui/help_test.go`, inside `TestHelpListsEverySpecBinding`'s
string slice, the entry `"D W M"`.

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/tui/ -run 'TestCalendarPresetKeys|TestRangeChangeResets' 2>&1 | head -10`
Expected: `FAIL`, with `D: From = 0001-01-01 ...` — the key is unbound, so the
scope never changed.

- [ ] **Step 3: Bind the keys**

In `handleKey`, immediately after the `case "a":` arm, add:

```go
	case "D":
		return m.setRange(timeRange{kind: rangeToday})
	case "W":
		return m.setRange(timeRange{kind: rangeWeek})
	case "M":
		return m.setRange(timeRange{kind: rangeMonth})
```

- [ ] **Step 4: Document them**

In `internal/tui/help.go`, change the range row and add the calendar row:

```go
	{"d w m a", "table", "Rolling range: 24h / 7d / 30d / all"},
	{"D W M", "table", "Calendar range: today / this week / this month"},
```

- [ ] **Step 5: Fix the binding count**

`TestHelpRowsAreTheKeymapPlusTheCommands` asserts `len(helpBindings) != 15`. One
row was added, so change both the literal and the message:

```go
	if len(helpBindings) != 16 {
		t.Errorf("spec §5.5 lists 16 bindings; helpBindings has %d", len(helpBindings))
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run:
`go test ./internal/tui/ -run 'TestCalendarPresetKeys|TestRangeChangeResets|TestHelp' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: no `FAIL` lines.

- [ ] **Step 7: Run the full suite and vet**

Run: `go vet ./... && go test ./... 2>&1 | tail -15`
Expected: `go vet` silent;
every package `ok`.

- [ ] **Step 8: Confirm the binary runs**

Run:
`go build -o /tmp/ccdash-check ./cmd/ccdash && /tmp/ccdash-check version && rm /tmp/ccdash-check`
Expected: `0.1.0`

- [ ] **Step 9: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go \
        internal/tui/help.go internal/tui/help_test.go
git commit -m "feat(tui): add calendar range presets on D, W and M

Rolling windows cannot express the periods a bill is reconciled against.
D/W/M select today, this week and this month in the local zone; the rolling
d/w/m keys are unchanged."
```

---

## Verification

After Task 8, confirm the whole stage:

```bash
go vet ./...
go test ./... -count=1
go test ./internal/tui/ -run 'TestTodaySpansTheRealDayAcrossDST' -v
```

The DST test is called out because it is the one assertion that fails if anyone
replaces the calendar helpers with duration arithmetic during a later refactor.

## Out of scope for this plan

Stages 2 through 4 of the spec: statusline ingestion and `session_state`, the
dashboard as permanent stack root, the `Selector` interface, leader-key
navigation, and the dashboard itself. `PulseView` and `DaysView` continue to
call `ByDay`; wiring them to `ByBucket` at a resolution derived from the window
span belongs with the dashboard work in Stage 4, since that is where the span
becomes visible to a view.
