package tui

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

type LimitsView struct{}

func (LimitsView) Title() string { return "Limits" }

func (LimitsView) Columns() []Column {
	return []Column{
		{Title: "TOOL", Width: 8, Sort: SortString, Kind: CellText},
		{Title: "LIMIT", Width: 14, Sort: SortString, Kind: CellText},
		{Title: "USED", Width: 20, Sort: SortNumeric, Kind: CellBar},
		{Title: "PCT", Width: 7, Align: AlignRight, Sort: SortNumeric, Kind: CellNumber},
		{Title: "RESETS", Width: 16, Sort: SortString, Kind: CellText},
		{Title: "SOURCE", Sort: SortString, Kind: CellText},
	}
}

func (LimitsView) Rows(db *sql.DB, _ *model.Pricing, _ Scope) ([]Row, error) {
	states, err := agg.LatestLimits(db)
	if err != nil {
		return nil, err
	}
	index := make(map[string]agg.LimitState, len(states))
	for _, state := range states {
		index[limitKey(state.Tool, state.Kind, state.Scope)] = state
	}
	expected := []expectedLimit{
		{model.ToolClaude, model.KindSession},
		{model.ToolClaude, model.KindWeeklyAll},
		{model.ToolCodex, model.KindCodex5h},
		{model.ToolCodex, model.KindCodexWeekly},
	}
	rows := make([]Row, 0, len(expected)+len(states))
	emit := func(state agg.LimitState) {
		source := fmt.Sprintf("%s %s", state.Provenance, formatAge(state.Age))
		if state.Provenance == model.ProvCached || state.Age >= time.Hour {
			source = "⚠ " + source
		}
		if state.IsActive {
			source += "  ◀ binding"
		}
		rows = append(rows, Row{
			Key: limitKey(state.Tool, state.Kind, state.Scope),
			Cells: []Cell{
				{Text: string(state.Tool)},
				{Text: limitLabel(state.Kind, state.Scope)},
				{Value: state.Percent / 100},
				{Text: fmt.Sprintf("%.1f%%", state.Percent), Value: state.Percent},
				{Text: resetIn(state.ResetsAt)},
				{Text: source},
			},
		})
	}
	for _, item := range expected {
		key := limitKey(item.tool, item.kind, "")
		if state, ok := index[key]; ok {
			emit(state)
			delete(index, key)
			continue
		}
		// A missing limit reads "no data", never 0%, which would look like
		// plenty of headroom.
		rows = append(rows, Row{
			Key: key,
			Cells: []Cell{
				{Text: string(item.tool)},
				{Text: limitLabel(item.kind, "")},
				{Value: 0},
				{Text: "—"},
				{Text: "—"},
				{Text: "no data"},
			},
		})
	}
	for _, state := range states {
		if _, ok := index[limitKey(state.Tool, state.Kind, state.Scope)]; ok {
			emit(state)
		}
	}
	return rows, nil
}

func (LimitsView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }
