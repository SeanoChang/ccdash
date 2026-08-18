package tui

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/SeanoChang/ccdash/internal/agg"
	"github.com/SeanoChang/ccdash/internal/model"
	"github.com/SeanoChang/ccdash/internal/render"
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
