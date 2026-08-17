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
				{Text: money(bucket.Cost, bucket.Unpriced == 0), Value: bucket.Cost},
				{Value: share},
			},
		})
	}
	return rows, nil
}

func (DaysView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
