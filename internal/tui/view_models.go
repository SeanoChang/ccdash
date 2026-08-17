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
