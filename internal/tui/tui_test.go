package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanochang/llm-usage-dashboard/internal/agg"
	"github.com/seanochang/llm-usage-dashboard/internal/model"
)

func TestViewRendersEmptyState(t *testing.T) {
	output := (Model{width: 80, height: 24, loaded: true}).View()
	if !strings.Contains(output, "llm-usage ingest") {
		t.Errorf("empty state = %s", output)
	}
}

func TestViewLabelsCostAsAPIRates(t *testing.T) {
	m := Model{width: 80, height: 24, loaded: true,
		totals: agg.TotalsResult{Requests: 5, Tokens: 1_000_000, Cost: 12.34}}
	output := m.View()
	if !strings.Contains(output, "at API rates") || strings.Contains(strings.ToLower(output), "spent") {
		t.Errorf("cost label = %s", output)
	}
}

func TestViewShowsStalenessForCachedLimits(t *testing.T) {
	m := Model{width: 80, height: 24, loaded: true,
		limits: []agg.LimitState{{
			LimitSample: model.LimitSample{Tool: model.ToolClaude,
				Kind: model.KindSession, Percent: 16, Provenance: model.ProvCached},
			Age: 26 * time.Hour,
		}}}
	output := m.View()
	if !strings.Contains(output, "cached") || !strings.Contains(output, "26h") {
		t.Errorf("cached limit = %s", output)
	}
}

func TestViewWarnsWhenLiveLimitIsStale(t *testing.T) {
	m := Model{width: 80, loaded: true,
		limits: []agg.LimitState{{
			LimitSample: model.LimitSample{Tool: model.ToolCodex,
				Kind: model.KindCodex5h, Percent: 55, Provenance: model.ProvLive},
			Age: 3 * time.Hour,
		}}}
	output := m.View()
	if !strings.Contains(output, "⚠ live 3h") {
		t.Errorf("stale live limit = %s", output)
	}
}

func TestViewShowsMissingLimitsAsNoData(t *testing.T) {
	if output := (Model{loaded: true, width: 80}).View(); !strings.Contains(output, "— no data") {
		t.Errorf("missing limits = %s", output)
	}
}

func TestQuitKey(t *testing.T) {
	_, command := (Model{}).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if command == nil {
		t.Error("q must quit")
	}
}

func TestToolAndRangeFilterKeys(t *testing.T) {
	next, _ := (Model{}).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m := next.(Model)
	if m.filter.Tool != model.ToolClaude {
		t.Errorf("tool = %q", m.filter.Tool)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if next.(Model).filter.Tool != "" {
		t.Errorf("tool = %q", next.(Model).filter.Tool)
	}
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if next.(Model).filter.From.IsZero() || next.(Model).rangeLabel != "week" {
		t.Errorf("week filter = %+v", next.(Model).filter)
	}
}
