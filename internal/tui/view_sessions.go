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
