package tui

import (
	"database/sql"

	"github.com/SeanoChang/ccdash/internal/agg"
	"github.com/SeanoChang/ccdash/internal/model"
)

// UnpricedView promotes the old footer warning into an inspectable resource.
// Rows disappear from it the moment pricing.toml gains a matching rate, with
// no re-ingest, because agg.UnpricedList derives from the live rate table.
type UnpricedView struct{}

func (UnpricedView) Title() string { return "Unpriced" }

func (UnpricedView) Columns() []Column {
	return []Column{
		{Title: "MODEL", Sort: SortString, Kind: CellText},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 12, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "FIRST SEEN", Width: 12, Sort: SortTime, Kind: CellText},
		{Title: "LAST SEEN", Width: 12, Sort: SortTime, Kind: CellText},
	}
}

func (UnpricedView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.UnpricedList(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Model,
			Cells: []Cell{
				{Text: bucket.Model},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: bucket.FirstSeen.Format("2006-01-02"),
					Value: float64(bucket.FirstSeen.Unix())},
				{Text: bucket.LastSeen.Format("2006-01-02"),
					Value: float64(bucket.LastSeen.Unix())},
			},
		})
	}
	return rows, nil
}

func (UnpricedView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
