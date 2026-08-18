package tui

import (
	"database/sql"
	"fmt"

	"github.com/SeanoChang/ccdash/internal/agg"
	"github.com/SeanoChang/ccdash/internal/model"
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
		{Title: "NAME", Sort: SortString, Kind: CellPath},
		{Title: "COST", Width: 12, Align: AlignRight, Sort: SortNumeric,
			Kind: CellNumber, DisableSort: true},
		{Title: "COST SHARE", Width: 18, Sort: SortNumeric, Kind: CellPercentBar,
			DefaultSortDesc: true},
		// Magnitude is already explicit in COST SHARE. A local scale gives each
		// project enough vertical resolution to show its own day-to-day shape.
		{Title: "TREND (REL)", Width: 14, Sort: SortNumeric, Kind: CellSparkline,
			SparkScale: SparkScaleLocal, DisableSort: true},
	}
}

// DefaultSort makes the initial cost-share order explicit in the header.
// Pressing s alternates between this and full project-path order.
func (ProjectsView) DefaultSort() (int, bool) { return 2, true }

func (ProjectsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByProject(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	total := 0.0
	for _, bucket := range buckets {
		total += bucket.Cost
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		share := 0.0
		if total > 0 {
			share = bucket.Cost / total
		}
		rows = append(rows, Row{
			Key: bucket.Project,
			Cells: []Cell{
				{Text: bucket.Project},
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
