# Time Filter Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the seven defects an adversarial review found in Stage 1 of the
honest time filter, every one of them reproduced against the committed code
before being written down here.

**Architecture:** Five of the seven share two root causes. `time.Date` silently
normalizes a local midnight that does not exist, which corrupts `startOfDay`
and, through it, `dayLocal`'s bucket keys — so the fix is one helper, not five
patches. Separately, the header outgrew its column budget and the manual-refresh
key never learned to resolve the window.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, Lip Gloss v1.1.0. Standard library
`testing` only.

**Spec:** `docs/superpowers/specs/2026-08-18-analytics-dashboard-design.md` —
this plan repairs Stage 1, delivered by
`docs/superpowers/plans/2026-08-18-honest-time-filter.md` in commits
`1a98b3c..ddf903b`.

## Global Constraints

- Go 1.26; no new module dependencies. `go.mod` must be unchanged.
- Tests use the standard library only.
- **Always pass `-count=1` when a test's behaviour depends on `TZ`.** Go caches
  test results, and a cached pass from a previous zone reads as a fresh pass in
  the new one. This is not hypothetical: it produced a false "New York is broken
  too" reading during the investigation behind this plan.
- Never run `git add -A`, `git add .`, or `git commit -a`. Stage only the files
  the task names.
- Every commit leaves `go build ./...` green and `go test ./... -count=1` green
  in **both** `America/New_York` and `Pacific/Auckland`.

## Reproductions

Each finding below was reproduced against the committed tree. The observed
output is quoted in the task that fixes it, so an implementer can confirm the
defect before changing anything.

Zones matter here. The three that expose the midnight defect transition **at
local midnight**: `America/Santiago` (2026-04-05, 2026-09-06) and
`America/Havana` (2026-03-08, 2026-11-01). `America/New_York` transitions at
02:00 and is unaffected — which is why none of this surfaced during Stage 1.

---

## Task 1: Stop the header truncating the bound it exists to show

Stage 1's whole purpose was a header that reports the filter's own bounds.
`rangeText` now returns 51 runes, and the Range line is ellipsis-truncated
between 96 and 100 columns — the band where `minLogoRoom = 96` turns the logo on
and takes 26 columns.

Observed, rendering the real `Model.View()` at 24 rows:

```text
w=90   Range:    last 24h  2026-08-17 15:04 → 2026-08-18 15:04 (now)
w=96   Range:    last 24h  2026-08-17 15:04 → 2026-08-18 15:04…
w=100  Range:    last 24h  2026-08-17 15:04 → 2026-08-18 15:04 (no…
w=104  Range:    last 24h  2026-08-17 15:04 → 2026-08-18 15:04 (now)
```

**Files:**

- Modify: `internal/tui/app.go` (`rangeText`)
- Test: `internal/tui/layout_test.go`

**Interfaces:**

- Consumes: `timeRange.label()`, `timeRange.calendar()`.
- Produces: no signature change. `rangeText` returns a shorter string.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/layout_test.go`:

```go
// TestHeaderRangeSurvivesTheLogoBand pins the widths where the logo turns on
// and takes 26 columns. Stage 1 exists to put the filter's own bounds in the
// header; a bound that is ellipsed away at 100 columns is not reported.
func TestHeaderRangeSurvivesTheLogoBand(t *testing.T) {
	for _, w := range []int{80, 96, 100, 104, 120} {
		m := newTestModel()
		m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 4, 0, 0, time.Local) }
		next, _ := m.Update(key("d"))
		m = next.(Model)
		m.width, m.height = w, 24

		var line string
		for _, candidate := range strings.Split(m.View(), "\n") {
			if strings.Contains(candidate, "Range:") {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Fatalf("w=%d: no Range line rendered", w)
		}
		if strings.Contains(line, "…") {
			t.Errorf("w=%d: range truncated: %q", w, strings.TrimRight(line, " "))
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/tui/ -run TestHeaderRangeSurvivesTheLogoBand -count=1 -v 2>&1 | head -12`

Expected: FAIL at `w=96` and `w=100`, passing at 80, 104 and 120.

- [ ] **Step 3: Shorten the rolling form**

The end bound of a rolling window is the clock, so printing both the resolved
instant and the word `now` says the same thing twice and costs 20 columns. In
`internal/tui/app.go`, in `rangeText`, replace the rolling branch so the end
reads `now` alone:

```go
	end := to.Format("2006-01-02 15:04")
	if !m.timeRange.calendar() {
		// A rolling window's end is the clock itself. Printing the resolved
		// instant as well as the word costs 20 columns and says it twice,
		// which pushed the line past the width where the logo turns on.
		end = "now"
	}
	return fmt.Sprintf("%s  %s → %s", text, from.Format("2006-01-02 15:04"), end)
```

That yields `last 24h  2026-08-17 15:04 → now`, 31 runes.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
`go test ./internal/tui/ -run 'TestHeaderRangeSurvivesTheLogoBand|TestRangeText' -count=1 -v 2>&1 | grep -E "^(--- |ok|FAIL)"`

Expected: all `PASS`.

- [ ] **Step 5: Confirm the calendar form still fits**

The calendar forms print a real end instant and are longer. Verify the longest
one also survives the band by running the same test with `M` instead of `d` —
add this to the test file:

```go
func TestHeaderCalendarRangeSurvivesTheLogoBand(t *testing.T) {
	for _, w := range []int{96, 100, 104} {
		m := newTestModel()
		m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 4, 0, 0, time.Local) }
		next, _ := m.Update(key("M"))
		m = next.(Model)
		m.width, m.height = w, 24
		for _, candidate := range strings.Split(m.View(), "\n") {
			if strings.Contains(candidate, "Range:") && strings.Contains(candidate, "…") {
				t.Errorf("w=%d: calendar range truncated: %q",
					w, strings.TrimRight(candidate, " "))
			}
		}
	}
}
```

Run:
`go test ./internal/tui/ -run TestHeaderCalendarRangeSurvivesTheLogoBand -count=1 -v 2>&1 | head -8`

Expected: PASS. `this month  2026-08-01 00:00 → 2026-09-01 00:00` is 45 runes,
which fits. If it fails, shorten the calendar form by dropping the times —
`this month  2026-08-01 → 2026-09-01` — since a calendar boundary is always
midnight and printing `00:00` twice carries no information.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/layout_test.go
git commit -m "fix(tui): stop the header ellipsing the bound it exists to report"
```

---

## Task 2: A local midnight that does not exist

`startOfDay` builds midnight with `time.Date`, which normalizes a nonexistent
local time backwards rather than failing. In zones that spring forward at
midnight, that returns 23:00 of the _previous_ day.

Observed:

```text
Santiago 2026-09-05  startOfDay=09-05 00:00  today=[09-05 00:00 .. 09-05 23:00] span=23h
Santiago 2026-09-06  startOfDay=09-05 23:00  today=[09-05 23:00 .. 09-06 23:00] span=23h
Havana   2026-03-08  startOfDay=03-07 23:00  today=[03-07 23:00 .. 03-08 23:00] span=23h
New_York 2026-03-08  startOfDay=03-08 00:00  today=[03-08 00:00 .. 03-09 00:00] span=23h  (correct)
```

Two distinct wrongs: an ordinary day loses its final hour, and the transition
day starts an hour before it begins.

The same helper backs `dayLocal`, so `ByProject`'s sparkline inherits it.
Observed in Santiago with three consecutive days of activity:

```text
TS=2026-09-04 12:00 -0400 dayLocal=2026-09-04
TS=2026-09-05 12:00 -0400 dayLocal=2026-09-05
TS=2026-09-06 12:00 -0300 dayLocal=2026-09-05   ← filed under the wrong day
spark=[0 0 0 0 0 0 0 0 0 0 0 0 0 25]            ← three days render as one bar
```

In New York the same fixture gives `spark=[0 … 25 25 25]`. This is a regression
the UTC-to-local move introduced: `time.UTC` has no transitions, so the old
`dayUTC` was immune.

**Files:**

- Modify: `internal/tui/timerange.go` (`startOfDay`)
- Modify: `internal/agg/agg.go` (`dayIn`)
- Test: `internal/tui/timerange_test.go`, `internal/agg/agg_test.go`

**Interfaces:**

- Consumes: nothing.
- Produces: `startOfDay` and `dayIn` return the first instant that actually
  exists on the requested calendar day. No signature change.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/timerange_test.go`:

```go
// TestStartOfDayLandsOnTheDayItWasAskedFor covers zones that spring forward at
// midnight. time.Date normalizes a nonexistent local midnight backwards, which
// returns 23:00 of the previous day — so "today" began yesterday.
func TestStartOfDayLandsOnTheDayItWasAskedFor(t *testing.T) {
	for _, c := range []struct{ zone, day string }{
		{"America/Santiago", "2026-09-06"},
		{"America/Havana", "2026-03-08"},
		{"America/New_York", "2026-03-08"},
	} {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Fatalf("load %s: %v", c.zone, err)
		}
		noon, err := time.ParseInLocation("2006-01-02 15:04", c.day+" 12:00", loc)
		if err != nil {
			t.Fatal(err)
		}
		got := startOfDay(noon)
		if got.Format("2006-01-02") != c.day {
			t.Errorf("%s %s: startOfDay = %s, want a time on %s",
				c.zone, c.day, got.Format("2006-01-02 15:04 -0700"), c.day)
		}
		if got.After(noon) {
			t.Errorf("%s %s: startOfDay = %v is after noon", c.zone, c.day, got)
		}
	}
}

// TestTodayCoversEveryHourThatExists: the window must contain every instant on
// the day and nothing from its neighbours.
func TestTodayCoversEveryHourThatExists(t *testing.T) {
	for _, c := range []struct {
		zone, day string
		hours     float64
	}{
		{"America/Santiago", "2026-09-05", 24},
		{"America/Santiago", "2026-09-06", 23},
		{"America/Havana", "2026-03-08", 23},
		{"America/New_York", "2026-03-08", 23},
		{"America/New_York", "2026-11-01", 25},
	} {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Fatal(err)
		}
		noon, err := time.ParseInLocation("2006-01-02 15:04", c.day+" 12:00", loc)
		if err != nil {
			t.Fatal(err)
		}
		from, to := timeRange{kind: rangeToday}.bounds(noon)
		if from.Format("2006-01-02") != c.day {
			t.Errorf("%s %s: window starts on %s", c.zone, c.day, from.Format("2006-01-02"))
		}
		if got := to.Sub(from).Hours(); got != c.hours {
			t.Errorf("%s %s: span = %.0fh, want %.0fh", c.zone, c.day, got, c.hours)
		}
	}
}
```

Append to `internal/agg/agg_test.go`:

```go
// TestDayInFilesUnderTheDayItHappened: in a zone that springs forward at
// midnight, dayIn used to normalize backwards and file a record under the
// previous day, which collapsed ByProject's sparkline.
func TestDayInFilesUnderTheDayItHappened(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	noon, err := time.ParseInLocation("2006-01-02 15:04", "2026-09-06 12:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayIn(noon, loc).Format("2006-01-02"); got != "2026-09-06" {
		t.Errorf("dayIn = %s, want 2026-09-06", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:
`go test ./internal/tui/ -run 'TestStartOfDayLandsOn|TestTodayCoversEvery' -count=1 -v 2>&1 | head -20`

Expected: FAIL for both Santiago cases and the Havana case; New York passes.

Run:
`go test ./internal/agg/ -run TestDayInFilesUnderTheDayItHappened -count=1 -v 2>&1 | head -6`

Expected: FAIL — `dayIn = 2026-09-05, want 2026-09-06`.

- [ ] **Step 3: Fix startOfDay**

In `internal/tui/timerange.go`, replace `startOfDay`:

```go
// startOfDay returns the first instant that exists on t's calendar day.
//
// time.Date does not reject a local time that does not exist; it normalizes,
// and for a zone that springs forward at midnight it normalizes backwards to
// 23:00 of the previous day. America/Santiago and America/Havana both do this.
// Detecting it is cheap: if the result is not on the day we asked for, the day
// began an hour later than midnight.
func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, t.Location())
	if start.Day() != day || start.Month() != month || start.Year() != year {
		start = time.Date(year, month, day, 1, 0, 0, 0, t.Location())
	}
	return start
}
```

- [ ] **Step 4: Fix the end of the window the same way**

`bounds` derives its end with `AddDate`, which lands on the next day's midnight
and hits the identical normalization. In `bounds`, route the calendar ends
through `startOfDay` so they inherit the correction:

```go
	case rangeToday:
		start := startOfDay(now)
		return start, startOfDay(start.AddDate(0, 0, 1))
	case rangeWeek:
		start := startOfWeek(now)
		return start, startOfDay(start.AddDate(0, 0, 7))
	case rangeMonth:
		start := startOfMonth(now)
		return start, startOfDay(start.AddDate(0, 1, 0))
```

`startOfWeek` and `startOfMonth` already call `startOfDay` or `time.Date` for
their start; make `startOfMonth` use the same guard:

```go
func startOfMonth(t time.Time) time.Time {
	year, month, _ := t.Date()
	return startOfDay(time.Date(year, month, 1, 12, 0, 0, 0, t.Location()))
}
```

Building at noon and then truncating avoids constructing a midnight that may not
exist in the first place.

- [ ] **Step 5: Fix dayIn**

`internal/agg/agg.go` has the same construction. Apply the same guard:

```go
// dayIn truncates to the first instant that exists on t's calendar day in loc.
// See startOfDay in internal/tui/timerange.go: a zone that springs forward at
// midnight has no 00:00 on the transition day, and time.Date normalizes such a
// time backwards into the previous day rather than reporting it.
func dayIn(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	year, month, day := local.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if start.Day() != day || start.Month() != month || start.Year() != year {
		start = time.Date(year, month, day, 1, 0, 0, 0, loc)
	}
	return start
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run:
`go test ./internal/tui/ ./internal/agg/ -run 'TestStartOfDayLandsOn|TestTodayCoversEvery|TestDayInFiles' -count=1 -v 2>&1 | grep -E "^(--- |ok|FAIL)"`

Expected: all `PASS`.

- [ ] **Step 7: Confirm the sparkline recovers**

Add to `internal/agg/agg_test.go`:

```go
// TestSparklineSurvivesAMidnightTransition: three consecutive days of activity
// must render as three bars in every zone. Before the dayIn fix, Santiago
// filed two of them under one key and drew one bar.
func TestSparklineSurvivesAMidnightTransition(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var recs []model.Record
	for i := range 3 {
		day, err := time.ParseInLocation("2006-01-02 15:04",
			fmt.Sprintf("2026-09-%02d 12:00", 4+i), loc)
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, model.Record{
			ID: fmt.Sprintf("r%d", i), Tool: model.ToolClaude, TS: day,
			Model: "claude-opus-5", Project: "/p", Session: "s", OutputTok: 1_000_000,
		})
	}
	if _, err := s.UpsertRecords(recs); err != nil {
		t.Fatal(err)
	}

	got, err := ByProject(s.DB(), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	nonzero := 0
	for _, v := range got[0].Spark {
		if v > 0 {
			nonzero++
		}
	}
	if nonzero != 3 {
		t.Errorf("spark has %d nonzero days, want 3: %v", nonzero, got[0].Spark)
	}
}
```

Run:
`TZ=America/Santiago go test ./internal/agg/ -run TestSparklineSurvivesAMidnightTransition -count=1 -v 2>&1 | head -8`

Expected: PASS. Note this test pins its own location and so passes under any
`TZ`; the explicit `TZ` here is belt and braces.

- [ ] **Step 8: Run the full suite in three zones**

Run: `go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Run: `TZ=Pacific/Auckland go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Run: `TZ=America/Santiago go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Expected: no `FAIL` in any of the three. `-count=1` is required — a cached
result from the previous zone otherwise reads as a pass.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/timerange.go internal/tui/timerange_test.go \
        internal/agg/agg.go internal/agg/agg_test.go
git commit -m "fix: handle a local midnight that does not exist

Zones that spring forward at midnight have no 00:00 on the transition day.
time.Date normalizes such a time backwards into the previous day instead of
reporting it, so today's window began yesterday and ByProject filed a day's
cost under its predecessor, collapsing the sparkline to one bar."
```

---

## Task 3: Hour buckets collapse on a fall-back day

`bucketOf` at hour resolution truncates to the local hour, so the two distinct
01:00 hours of a fall-back day land in one bucket and their tokens are summed.

Observed in New York with one record in each 01:00 hour:

```text
hour buckets = 1 (want 2)
   2026-11-01 01:00 -0400 tokens=20
```

`ByBucket` has no production caller yet — `view_days.go:24` and
`view_pulse.go:26` both call `ByDay` — so this is latent until Stage 4 wires it.
Fixing it now costs one task and keeps Stage 4 from inheriting it.

**Files:**

- Modify: `internal/agg/agg.go` (`bucketOf`, and the `ByDay` doc comment)
- Test: `internal/agg/agg_test.go`

**Interfaces:**

- Consumes: nothing.
- Produces: `bucketOf` at `ResHour` keys on the absolute hour, so the two 01:00
  hours stay distinct.

- [ ] **Step 1: Write the failing test**

Append to `internal/agg/agg_test.go`:

```go
// TestHourBucketsKeepBothFallBackHours: on a fall-back day the local clock
// reads 01:00 twice, an hour apart. Truncating to the local hour merges them.
func TestHourBucketsKeepBothFallBackHours(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 05:00Z is 01:00 EDT; 06:00Z is 01:00 EST. Two different hours.
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "edt", Tool: model.ToolClaude, Model: "claude-opus-5", Session: "s",
			TS: time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC), OutputTok: 10},
		{ID: "est", Tool: model.ToolClaude, Model: "claude-opus-5", Session: "s",
			TS: time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC), OutputTok: 10},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := ByBucket(s.DB(), model.DefaultPricing(), Filter{}, ResHour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("hour buckets = %d, want 2: %+v", len(got), got)
	}
	for _, b := range got {
		if b.Tokens != 10 {
			t.Errorf("bucket %v has %d tokens, want 10 — the two 01:00 hours merged",
				b.Day, b.Tokens)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`TZ=America/New_York go test ./internal/agg/ -run TestHourBucketsKeepBothFallBackHours -count=1 -v 2>&1 | head -8`

Expected: FAIL — `hour buckets = 1, want 2`.

- [ ] **Step 3: Key the hour bucket on the absolute hour**

In `internal/agg/agg.go`, replace `bucketOf`'s hour branch. Truncating the
instant rather than rebuilding it from local calendar fields keeps the two hours
distinct, because they are genuinely an hour apart in absolute time:

```go
// bucketOf truncates t to the start of its bucket.
//
// The hour branch truncates the instant, not the local clock reading: on a
// fall-back day the local clock reads 01:00 twice, an hour apart, and
// rebuilding from local fields would merge them into one bucket. Truncate
// works on absolute time, so the two stay distinct, and .Local() keeps the
// label in the user's zone.
func bucketOf(t time.Time, res Resolution) time.Time {
	if res == ResHour {
		return t.Truncate(time.Hour).Local()
	}
	return dayLocal(t)
}
```

`time.Truncate` operates on the absolute instant since the zero time, so it is
immune to zone transitions. Zones offset by a whole number of hours align with
the local hour; the handful at :30 or :45 offsets bucket on the absolute hour
instead, which is a defensible reading of "hourly" and is not a merge.

- [ ] **Step 4: Run the test to verify it passes**

Run:
`TZ=America/New_York go test ./internal/agg/ -run 'TestHourBuckets|TestByBucket' -count=1 -v 2>&1 | grep -E "^(--- |ok|FAIL)"`

Expected: both `PASS`.

- [ ] **Step 5: Correct the ByDay comment**

Three places claim `ByProject`'s sparkline depends on `ByDay`. It does not —
`ByProject` builds its own day map and calls `dayLocal` directly at
`internal/agg/agg.go:307`. The wrapper guarantee is real for `view_days.go` and
`view_pulse.go`, but naming `ByProject` makes the claim read as coverage it
never provided. Replace the `ByDay` doc comment:

```go
// ByDay is ByBucket at day resolution. view_days.go and view_pulse.go both
// call it and are day-shaped, so it keeps this signature.
func ByDay(db *sql.DB, pricing *model.Pricing, filter Filter) ([]DayBucket, error) {
	return ByBucket(db, pricing, filter, ResDay)
}
```

And in `internal/agg/agg_test.go`, fix `TestByDayStillBucketsByDay`'s comment to
name the two real callers rather than `ByProject`.

- [ ] **Step 6: Run the full suite in two zones**

Run: `go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Run: `TZ=Pacific/Auckland go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Expected: no `FAIL`.

- [ ] **Step 7: Commit**

```bash
git add internal/agg/agg.go internal/agg/agg_test.go
git commit -m "fix(agg): keep both 01:00 hours of a fall-back day distinct

Rebuilding an hour bucket from local calendar fields merged the two 01:00
hours, summing their tokens into one. Truncating the instant keeps them an
hour apart, which is what they are."
```

---

## Task 4: Manual refresh refreshes with stale bounds

The ticker resolves the window before refreshing; `r` does not. It sets
`inFlight` and calls `refresh(true)` with whatever bounds were last resolved.
`applyScope`'s own comment claims "only the keypress paths use it", yet `r` is a
keypress path that uses neither it nor `resolveScope`.

Observed with an injected clock: press `d` at 09:00, advance six hours, press
`r` — `From` stays at 09:00. Driving `tickMsg{}` instead moves it to 15:00.

The ticker bounds the staleness to about two seconds in practice, so the impact
is small. It is worth fixing anyway because `r` is precisely the key someone
presses when they want the window to be current _now_.

**Files:**

- Modify: `internal/tui/app.go` (the `case "r":` arm)
- Test: `internal/tui/app_test.go`

**Interfaces:**

- Consumes: `(*Model).resolveScope` from Stage 1 Task 6.
- Produces: no signature change.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/app_test.go`:

```go
// TestManualRefreshResolvesTheWindow: r is the key someone presses when they
// want the window current now, so it must resolve before refetching rather
// than reusing whatever the last tick left behind.
func TestManualRefreshResolvesTheWindow(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)
	m := newTestModel()
	m.now = func() time.Time { return clock }

	next, _ := m.Update(key("d"))
	m = next.(Model)
	before := m.scope.From

	clock = clock.Add(6 * time.Hour)

	after, _ := m.Update(key("r"))
	refreshed := after.(Model)
	if !refreshed.scope.From.After(before) {
		t.Errorf("From = %v after r, want later than %v — r refreshed with stale bounds",
			refreshed.scope.From, before)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
`go test ./internal/tui/ -run TestManualRefreshResolvesTheWindow -count=1 -v 2>&1 | head -8`

Expected: FAIL — `From = 2026-08-17 09:00 … want later`.

- [ ] **Step 3: Resolve before refreshing**

In `internal/tui/app.go`, in `handleKey`, change the `r` arm to match the
ticker's ordering:

```go
	case "r":
		if m.inFlight {
			return m, nil
		}
		m.inFlight = true
		// Same ordering as the tick path: resolve, then fetch. Without this,
		// the key that means "make this current" refetches the window the last
		// tick happened to leave behind.
		m.resolveScope()
		return m, m.refresh(true)
```

- [ ] **Step 4: Correct applyScope's comment**

The comment now describes reality if it names the fetching, not the paths:

```go
// applyScope resolves and then refetches synchronously. The keypress paths
// that change the scope use it; the tick and manual-refresh paths resolve and
// let the asynchronous refresh do the fetching.
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 2>&1 | tail -3`

Expected: `ok github.com/seanochang/ccdash/internal/tui`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "fix(tui): resolve the window on manual refresh too

r set inFlight and refetched with whatever bounds the last tick left, so the
key that means make this current did not."
```

---

## Task 5: A rolling window must include its newest row

Stage 1 made `Filter.To` exclusive, which is right for calendar windows where
one window's end is the next one's start. Rolling windows also began setting
`To = now`, so the query became `ts < now`, excluding any record whose timestamp
truncates to the current second — and excluding permanently any row timestamped
ahead of the machine clock.

Observed with two rows, one at `now` and one a minute ahead, under a 24-hour
rolling window:

```text
rolling window To=now: requests = 0 (2 rows exist)
```

Before Stage 1, `d`/`w`/`m` set only `From` and left `To` zero, so the newest
row always appeared. A rolling window has no upper edge to defend — its end is
the clock — so it should not set one.

**Files:**

- Modify: `internal/tui/timerange.go` (`bounds`)
- Test: `internal/tui/timerange_test.go`, `internal/tui/app_test.go`

**Interfaces:**

- Consumes: nothing.
- Produces: `bounds` returns a zero `to` for `rangeRolling`. Calendar kinds are
  unchanged. Any caller deriving a window width from `to.Sub(from)` must use
  `timeRange.span` for rolling instead.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/timerange_test.go`:

```go
// TestRollingWindowHasNoUpperBound: a rolling window's end is the clock, and
// Filter.To is exclusive, so setting To = now drops any row landing in the
// current second and every row timestamped ahead of the machine clock.
func TestRollingWindowHasNoUpperBound(t *testing.T) {
	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local)
	from, to := timeRange{kind: rangeRolling, span: 24 * time.Hour}.bounds(now)

	if !from.Equal(now.Add(-24 * time.Hour)) {
		t.Errorf("from = %v, want %v", from, now.Add(-24*time.Hour))
	}
	if !to.IsZero() {
		t.Errorf("to = %v, want zero — a rolling window has no upper edge", to)
	}
}
```

- [ ] **Step 2: Update the Stage 1 test that pinned the old behaviour**

`TestRollingWindowFollowsTheClock` in `internal/tui/app_test.go` asserts
`m.scope.To.Sub(m.scope.From) == 24*time.Hour`. That assertion encoded the
defect. Replace that block with one that checks what actually matters — the
lower bound keeps moving, and the upper bound stays open:

```go
	if !m.scope.From.After(first) {
		t.Errorf("From = %v after six hours, want later than %v",
			m.scope.From, first)
	}
	if !m.scope.To.IsZero() {
		t.Errorf("To = %v, want zero — a rolling window has no upper edge",
			m.scope.To)
	}
	if got := m.now().Sub(m.scope.From); got != 24*time.Hour {
		t.Errorf("window spans %v back from now, want 24h", got)
	}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run:
`go test ./internal/tui/ -run 'TestRollingWindowHasNoUpperBound|TestRollingWindowFollowsTheClock' -count=1 -v 2>&1 | head -12`

Expected: both FAIL, reporting a non-zero `to`.

- [ ] **Step 4: Drop the upper bound for rolling windows**

In `internal/tui/timerange.go`, in `bounds`:

```go
	case rangeRolling:
		// No upper bound. Filter.To is exclusive, so pinning it to now would
		// drop a record landing in the current second and would permanently
		// hide any row timestamped ahead of the machine clock. The end of a
		// rolling window is the clock, and the clock needs no defending.
		return now.Add(-r.span), time.Time{}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 2>&1 | tail -3`

Expected: `ok`.

- [ ] **Step 6: Confirm the header still reads correctly**

`rangeText` branches on `calendar()`, not on whether `to` is zero, so a rolling
window still prints `→ now`. But its all-time branch triggers when **both**
bounds are zero, and a rolling window now has a zero `to` with a set `from` —
verify the branch order still routes correctly:

Run:
`go test ./internal/tui/ -run 'TestRangeText|TestHeaderRange' -count=1 -v 2>&1 | grep -E "^(--- |ok|FAIL)"`

Expected: all `PASS`. If the all-time branch now captures rolling windows,
change its guard from `from.IsZero() && to.IsZero()` to
`m.timeRange.kind == rangeAll`, which says what it means.

- [ ] **Step 7: Add the regression test for the newest row**

Append to `internal/agg/agg_test.go`:

```go
// TestRollingWindowKeepsTheNewestRow: the row a user just generated is the one
// they are looking for, so an unbounded-above window must not drop it.
func TestRollingWindowKeepsTheNewestRow(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local)
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "now", Tool: model.ToolClaude, Model: "claude-opus-5", Session: "s",
			TS: now, OutputTok: 10},
		{ID: "ahead", Tool: model.ToolClaude, Model: "claude-opus-5", Session: "s",
			TS: now.Add(time.Minute), OutputTok: 10},
	}); err != nil {
		t.Fatal(err)
	}

	totals, err := Totals(s.DB(), model.DefaultPricing(),
		Filter{From: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Requests != 2 {
		t.Errorf("requests = %d, want 2 — an open-ended window drops nothing", totals.Requests)
	}
}
```

Run:
`go test ./internal/agg/ -run TestRollingWindowKeepsTheNewestRow -count=1 -v 2>&1 | grep -E "^(--- |ok|FAIL)"`

Expected: `PASS`.

- [ ] **Step 8: Run the full suite in two zones**

Run: `go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Run: `TZ=Pacific/Auckland go test ./... -count=1 2>&1 | grep -E "FAIL|^ok"`

Expected: no `FAIL`.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/timerange.go internal/tui/timerange_test.go \
        internal/tui/app_test.go internal/agg/agg_test.go
git commit -m "fix(tui): leave a rolling window open at the top

Filter.To became exclusive for the calendar case, but rolling windows also
started setting To = now, so ts < now dropped any row in the current second
and hid future-dated rows permanently. A rolling window's end is the clock."
```

---

## Verification

After Task 5, confirm the whole set:

```bash
go vet ./...
go test ./... -count=1
TZ=Pacific/Auckland go test ./... -count=1
TZ=America/Santiago go test ./... -count=1
TZ=America/Havana   go test ./... -count=1
```

All five must be clean. The three non-local zones are the ones that expose the
defects this plan fixes, and `-count=1` is mandatory — without it, a cached
result from the previous zone reads as a pass in the next.

## Out of scope

Stages 2 through 4 of the spec. `ByBucket` still has no production caller;
wiring `DaysView` and `PulseView` to a resolution derived from the window span
belongs with the dashboard work, where the span first becomes visible to a view.
