package tui

import (
	"database/sql"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type AgentsView struct{}

func (AgentsView) Title() string { return "Agents" }

func (AgentsView) Columns() []Column {
	return []Column{
		{Title: "AGENT", Sort: SortString, Kind: CellText},
		{Title: "WORKFLOW", Width: 22, Sort: SortString, Kind: CellText},
		{Title: "DEPTH", Width: 7, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "COST", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func (AgentsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByAgent(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Agent,
			Cells: []Cell{
				{Text: bucket.Agent},
				{Text: bucket.Workflow},
				{Text: count(bucket.Depth), Value: float64(bucket.Depth)},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: money(bucket.Cost, bucket.Unpriced == 0), Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (AgentsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Agent = row.Key
	return RequestsView{}, scope, true
}

type WorkflowsView struct{}

func (WorkflowsView) Title() string { return "Workflows" }

func (WorkflowsView) Columns() []Column {
	return []Column{
		{Title: "WORKFLOW", Sort: SortString, Kind: CellText},
		{Title: "AGENTS", Width: 8, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "REQUESTS", Width: 10, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "TOKENS", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "STARTED", Width: 17, Sort: SortTime, Kind: CellText},
		{Title: "COST", Width: 11, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
	}
}

func (WorkflowsView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	buckets, err := agg.ByWorkflow(db, pricing, scope.Filter)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(buckets))
	for _, bucket := range buckets {
		rows = append(rows, Row{
			Key: bucket.Workflow,
			Cells: []Cell{
				{Text: bucket.Workflow},
				{Text: count(bucket.Agents), Value: float64(bucket.Agents)},
				{Text: count(bucket.Requests), Value: float64(bucket.Requests)},
				{Text: formatTokens(bucket.Tokens), Value: float64(bucket.Tokens)},
				{Text: bucket.Started.Format("2006-01-02 15:04"),
					Value: float64(bucket.Started.Unix())},
				{Text: money(bucket.Cost, bucket.Unpriced == 0), Value: bucket.Cost},
			},
		})
	}
	return rows, nil
}

func (WorkflowsView) Drill(row Row, scope Scope) (View, Scope, bool) {
	scope.Workflow = row.Key
	return AgentsView{}, scope, true
}
