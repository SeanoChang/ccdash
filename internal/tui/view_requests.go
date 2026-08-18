package tui

import (
	"database/sql"

	"github.com/SeanoChang/ccdash/internal/agg"
	"github.com/SeanoChang/ccdash/internal/model"
)

const requestsPageSize = 500

// RequestsView is the only paginated view: a full corpus holds tens of
// thousands of requests, which is not worth keeping in memory at once.
type RequestsView struct{}

func (RequestsView) Title() string { return "Requests" }

func (RequestsView) Columns() []Column {
	return []Column{
		{Title: "TIME", Width: 17, Sort: SortTime, Kind: CellText},
		{Title: "MODEL", Sort: SortString, Kind: CellText},
		{Title: "AGENT", Width: 16, Sort: SortString, Kind: CellText},
		{Title: "IN", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "OUT", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "CACHE R", Width: 9, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func requestRows(records []agg.RequestRow) []Row {
	rows := make([]Row, 0, len(records))
	for _, record := range records {
		agent := record.Agent
		if agent == "" {
			agent = "main"
		}
		rows = append(rows, Row{
			Key: record.ID,
			Cells: []Cell{
				{Text: record.TS.Format("2006-01-02 15:04"), Value: float64(record.TS.Unix())},
				{Text: record.Model},
				{Text: agent},
				{Text: formatTokens(record.Input), Value: float64(record.Input)},
				{Text: formatTokens(record.Output), Value: float64(record.Output)},
				{Text: formatTokens(record.CacheRead), Value: float64(record.CacheRead)},
				{Text: money(record.Cost, record.Priced), Value: record.Cost},
			},
		})
	}
	return rows
}

func (RequestsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	records, err := agg.Requests(db, pricing, scope.Filter, requestsPageSize, 0)
	if err != nil {
		return nil, err
	}
	return requestRows(records), nil
}

func (RequestsView) PageSize() int { return requestsPageSize }

// Page reports more=true when the page came back full, which is the signal for
// the table to fetch again once the selection reaches the bottom.
func (RequestsView) Page(db *sql.DB, pricing *model.Pricing, scope Scope, offset, limit int) ([]Row, bool, error) {
	records, err := agg.Requests(db, pricing, scope.Filter, limit, offset)
	if err != nil {
		return nil, false, err
	}
	return requestRows(records), len(records) == limit, nil
}

func (RequestsView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
