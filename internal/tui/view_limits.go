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

// Rows lists one row per limit the scope leaves in view. Only scope.Tool
// narrows the result: a limit is a standing quota rather than a time series —
// agg.LatestLimits already returns the newest sample per (tool, kind, scope) —
// so scope.From/To cannot sensibly select among them and are ignored. The tool
// filter, though, has to be honored here or the border title's count describes
// rows the body is not showing, and a pane whose header and body disagree
// teaches the user to distrust every number on screen.
func (LimitsView) Rows(db *sql.DB, _ *model.Pricing, scope Scope) ([]Row, error) {
	states, err := agg.LatestLimits(db)
	if err != nil {
		return nil, err
	}
	index := make(map[string]agg.LimitState, len(states))
	for _, state := range states {
		if !limitInScope(state.Tool, scope) {
			continue
		}
		index[limitKey(state.Tool, state.Kind, state.Scope)] = state
	}
	expected := make([]expectedLimit, 0, 4)
	for _, item := range []expectedLimit{
		{model.ToolClaude, model.KindSession},
		{model.ToolClaude, model.KindWeeklyAll},
		{model.ToolCodex, model.KindCodex5h},
		{model.ToolCodex, model.KindCodexWeekly},
	} {
		if limitInScope(item.tool, scope) {
			expected = append(expected, item)
		}
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

type expectedLimit struct {
	tool model.Tool
	kind model.LimitKind
}

// limitInScope reports whether a limit belonging to tool survives the current
// filter. An empty scope.Tool keeps every tool, which is the "all" case.
func limitInScope(tool model.Tool, scope Scope) bool {
	return scope.Tool == "" || scope.Tool == tool
}

func limitKey(tool model.Tool, kind model.LimitKind, scope string) string {
	return string(tool) + "\x00" + string(kind) + "\x00" + scope
}

func limitLabel(kind model.LimitKind, scope string) string {
	if scope != "" {
		return scope
	}
	switch kind {
	case model.KindWeeklyAll:
		return "weekly"
	case model.KindCodex5h:
		return "5h"
	case model.KindCodexWeekly:
		return "weekly"
	default:
		return string(kind)
	}
}

// resetIn has no "resets " prefix: the column header already says RESETS.
func resetIn(value *time.Time) string {
	if value == nil {
		return "no reset time"
	}
	duration := time.Until(*value)
	if duration <= 0 {
		return "resetting"
	}
	if duration >= 24*time.Hour {
		return fmt.Sprintf("%dd %dh", int(duration.Hours())/24, int(duration.Hours())%24)
	}
	return fmt.Sprintf("%dh%02dm", int(duration.Hours()), int(duration.Minutes())%60)
}

func formatAge(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "<1m"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
}
