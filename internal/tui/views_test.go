package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

func seedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "a1", Tool: model.ToolClaude, TS: time.Unix(1_700_000_000, 0),
			Model: "claude-opus-5", Project: "/home/u/alpha", Session: "s1", OutputTok: 1000},
		{ID: "a2", Tool: model.ToolClaude, TS: time.Unix(1_700_086_400, 0),
			Model: "claude-opus-5", Project: "/home/u/alpha", Session: "s1",
			Agent: "agent-x", Workflow: "wf-1", Depth: 1, OutputTok: 2000},
		{ID: "b1", Tool: model.ToolCodex, TS: time.Unix(1_700_172_800, 0),
			Model: "gpt-5-codex", Project: "/home/u/beta", Session: "s2", OutputTok: 500},
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProjectsViewShape(t *testing.T) {
	s := seedStore(t)
	view := ProjectsView{}
	rows, err := view.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d projects, want 2", len(rows))
	}
	if len(rows[0].Cells) != len(view.Columns()) {
		t.Fatalf("row has %d cells, columns declare %d",
			len(rows[0].Cells), len(view.Columns()))
	}
	if rows[0].Key == "" {
		t.Error("rows need a stable key for selection to survive refreshes")
	}
	for _, column := range view.Columns() {
		if column.Kind == CellSparkline {
			return
		}
	}
	t.Error("the projects view should carry a sparkline column")
}

func TestProjectsViewTruncatesOnSeparator(t *testing.T) {
	s := seedStore(t)
	rows, err := ProjectsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		text := row.Cells[0].Text
		if strings.HasPrefix(text, "…") && !strings.HasPrefix(text, "…/") {
			t.Errorf("truncated path %q must break on a separator", text)
		}
	}
}

func TestProjectsDrillNarrowsToProject(t *testing.T) {
	s := seedStore(t)
	rows, _ := ProjectsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	next, scope, ok := ProjectsView{}.Drill(rows[0], Scope{})
	if !ok {
		t.Fatal("projects must drill into sessions")
	}
	if scope.Project != rows[0].Key {
		t.Errorf("scope.Project = %q, want %q", scope.Project, rows[0].Key)
	}
	if next.Title() != "Sessions" {
		t.Errorf("drill target = %q, want Sessions", next.Title())
	}
}

func TestModelsViewIncludesUnpricedRow(t *testing.T) {
	s := seedStore(t)
	rows, err := ModelsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if strings.Contains(row.Cells[0].Text, "gpt-5-codex") {
			found = true
			costCell := row.Cells[len(row.Cells)-1].Text
			if !strings.Contains(costCell, "—") {
				t.Errorf("an unpriceable model must show — for cost, got %q", costCell)
			}
		}
	}
	if !found {
		t.Error("gpt-5-codex must be listed even though it has no rate")
	}
}

func TestDaysViewOrdersNewestFirst(t *testing.T) {
	s := seedStore(t)
	rows, err := DaysView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("got %d days, want at least 2", len(rows))
	}
	if rows[0].Cells[0].Value < rows[1].Cells[0].Value {
		t.Error("days should be newest first")
	}
}

func TestUnpricedViewListsOnlyUnpriceable(t *testing.T) {
	s := seedStore(t)
	rows, err := UnpricedView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Key != "gpt-5-codex" {
		t.Errorf("key = %q, want gpt-5-codex", rows[0].Key)
	}
}

func TestAllViewsDeclareCellsMatchingColumns(t *testing.T) {
	s := seedStore(t)
	pricing := model.DefaultPricing()
	for _, view := range []View{
		ProjectsView{}, ModelsView{}, DaysView{}, UnpricedView{},
	} {
		rows, err := view.Rows(s.DB(), pricing, Scope{})
		if err != nil {
			t.Fatalf("%s: %v", view.Title(), err)
		}
		for i, row := range rows {
			if len(row.Cells) != len(view.Columns()) {
				t.Errorf("%s row %d has %d cells, want %d",
					view.Title(), i, len(row.Cells), len(view.Columns()))
			}
		}
	}
}
