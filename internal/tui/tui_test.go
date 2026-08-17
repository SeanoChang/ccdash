package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanochang/ccdash/internal/agg"
	"github.com/seanochang/ccdash/internal/model"
)

func TestViewRendersEmptyState(t *testing.T) {
	output := (legacyModel{width: 80, height: 24, loaded: true}).View()
	if !strings.Contains(output, "ccdash ingest") {
		t.Errorf("empty state = %s", output)
	}
}

func TestViewLabelsCostAsAPIRates(t *testing.T) {
	m := legacyModel{width: 80, height: 24, loaded: true,
		totals: agg.TotalsResult{Requests: 5, Tokens: 1_000_000, Cost: 12.34}}
	output := m.View()
	if !strings.Contains(output, "at API rates") || strings.Contains(strings.ToLower(output), "spent") {
		t.Errorf("cost label = %s", output)
	}
}

func TestViewShowsStalenessForCachedLimits(t *testing.T) {
	m := legacyModel{width: 80, height: 24, loaded: true,
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
	m := legacyModel{width: 80, loaded: true,
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
	if output := (legacyModel{loaded: true, width: 80}).View(); !strings.Contains(output, "— no data") {
		t.Errorf("missing limits = %s", output)
	}
}

func TestQuitKey(t *testing.T) {
	_, command := (legacyModel{}).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if command == nil {
		t.Error("q must quit")
	}
}

func TestToolAndRangeFilterKeys(t *testing.T) {
	next, _ := (legacyModel{}).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m := next.(legacyModel)
	if m.filter.Tool != model.ToolClaude {
		t.Errorf("tool = %q", m.filter.Tool)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if next.(legacyModel).filter.Tool != "" {
		t.Errorf("tool = %q", next.(legacyModel).filter.Tool)
	}
	next, _ = next.(legacyModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if next.(legacyModel).filter.From.IsZero() || next.(legacyModel).rangeLabel != "week" {
		t.Errorf("week filter = %+v", next.(legacyModel).filter)
	}
}
