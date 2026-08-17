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
		// The series is cost per day, so the header's shared max is money.
		{Title: "TREND", Width: 14, Sort: SortNumeric, Kind: CellSparkline, Unit: UnitMoney},
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
				{Text: money(bucket.Cost, bucket.Unpriced == 0), Value: bucket.Cost},
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
