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

// spanText renders a span the way the preset keys name it: 24h stays "24h"
// because that is the filter the user pressed, and only a span past one day
// collapses to days.
func spanText(d time.Duration) string {
	switch {
	case d > 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}
