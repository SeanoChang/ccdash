package tui

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

// seedUnpriceable builds a store where the priced and the unpriceable traffic
// land in disjoint projects, days, agents and workflows, so every rollup yields
// one row the rate table can price and one it cannot. claude-haiku-4-5 is priced
// but carries no tokens, which is a genuine $0.00 and must not read as unknown.
func seedUnpriceable(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	priced := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	unpriced := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "p1", Tool: model.ToolClaude, TS: priced, Model: "claude-opus-5",
			Project: "/p/priced", Session: "s-priced", Agent: "agent-p",
			Workflow: "wf-p", Depth: 1, InputTok: 500_000, OutputTok: 1_000_000},
		{ID: "z1", Tool: model.ToolClaude, TS: priced.Add(time.Hour),
			Model: "claude-haiku-4-5", Project: "/p/priced", Session: "s-priced"},
		{ID: "u1", Tool: model.ToolCodex, TS: unpriced, Model: "gpt-5-codex",
			Project: "/p/unpriced", Session: "s-unpriced", Agent: "agent-u",
			Workflow: "wf-u", Depth: 1, InputTok: 60, OutputTok: 40},
		{ID: "u2", Tool: model.ToolCodex, TS: unpriced.Add(time.Hour),
			Model: "gpt-5-codex", Project: "/p/unpriced", Session: "s-unpriced",
			Agent: "agent-u", Workflow: "wf-u", Depth: 1, InputTok: 6, OutputTok: 4},
	}); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}

// costCell returns the text of the view's COST column for the row keyed key.
func costCell(t *testing.T, view View, rows []Row, key string) string {
	t.Helper()
	index := -1
	for i, column := range view.Columns() {
		if column.Title == "COST" {
			index = i
		}
	}
	if index < 0 {
		t.Fatalf("%s declares no COST column", view.Title())
	}
	for _, row := range rows {
		if row.Key == key {
			return row.Cells[index].Text
		}
	}
	t.Fatalf("%s dropped the row keyed %q; keys = %v", view.Title(), key, keysOf(rows))
	return ""
}

func keysOf(rows []Row) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	return keys
}

// TestViewsShowEmDashForUnpriceableCosts covers spec §7: a row whose model has
// no rate is displayed, with an em dash for cost. $0.00 would read as free.
func TestViewsShowEmDashForUnpriceableCosts(t *testing.T) {
	db := seedUnpriceable(t)
	pricing := model.DefaultPricing()
	for _, tc := range []struct {
		view        View
		unpricedKey string
		pricedKey   string
	}{
		{ProjectsView{}, "/p/unpriced", "/p/priced"},
		{DaysView{}, "2026-08-16", "2026-08-15"},
		{ModelsView{}, "gpt-5-codex", "claude-opus-5"},
		{AgentsView{}, "agent-u", "agent-p"},
		{WorkflowsView{}, "wf-u", "wf-p"},
	} {
		t.Run(tc.view.Title(), func(t *testing.T) {
			rows, err := tc.view.Rows(db, pricing, Scope{})
			if err != nil {
				t.Fatal(err)
			}
			if got := costCell(t, tc.view, rows, tc.unpricedKey); got != "—" {
				t.Errorf("unpriceable row %q cost = %q, want an em dash",
					tc.unpricedKey, got)
			}
			got := costCell(t, tc.view, rows, tc.pricedKey)
			if !strings.HasPrefix(got, "$") {
				t.Errorf("priced row %q cost = %q, want a dollar figure",
					tc.pricedKey, got)
			}
		})
	}
}

// TestModelsViewPricesAZeroCostModel guards the other half of the same call:
// a priced model that happens to cost nothing is known to be free, so it must
// print $0.00. A `Cost > 0` test would wrongly call it unknown.
func TestModelsViewPricesAZeroCostModel(t *testing.T) {
	db := seedUnpriceable(t)
	rows, err := ModelsView{}.Rows(db, model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if got := costCell(t, ModelsView{}, rows, "claude-haiku-4-5"); got != "$0.00" {
		t.Errorf("priced zero-token model cost = %q, want $0.00", got)
	}
}
