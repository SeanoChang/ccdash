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
