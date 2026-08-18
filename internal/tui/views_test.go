package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

func seedStore(t *testing.T) *store.Store {
	t.Helper()
	requireUnpriced(t, model.DefaultPricing(), unpricedFixtureModel)
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
			Model: unpricedFixtureModel, Project: "/home/u/beta", Session: "s2", OutputTok: 500},
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
	view := ProjectsView{}
	rows, err := ProjectsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		text := strings.TrimSpace(formatCell(row.Cells[0], view.Columns()[0], 12, 0))
		if strings.HasPrefix(text, "…") && !strings.HasPrefix(text, "…/") {
			t.Errorf("truncated path %q must break on a separator", text)
		}
	}
}

func TestProjectsViewCostShareUsesTotalCost(t *testing.T) {
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	day := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "small", Tool: model.ToolClaude, TS: day, Model: "claude-opus-5",
			Project: "/work/small", Session: "small", OutputTok: 1_000_000},
		{ID: "large", Tool: model.ToolClaude, TS: day, Model: "claude-opus-5",
			Project: "/work/large", Session: "large", OutputTok: 3_000_000},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := (ProjectsView{}).Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	shares := make(map[string]float64, len(rows))
	total := 0.0
	for _, row := range rows {
		shares[row.Key] = row.Cells[2].Value
		total += row.Cells[2].Value
		if row.Cells[0].Text != row.Key {
			t.Errorf("path cell = %q, want full project path %q", row.Cells[0].Text, row.Key)
		}
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("cost shares sum to %.4f, want 1", total)
	}
	if math.Abs(shares["/work/small"]-0.25) > 1e-9 ||
		math.Abs(shares["/work/large"]-0.75) > 1e-9 {
		t.Errorf("shares = %#v, want small=.25 large=.75", shares)
	}
}

func TestProjectsViewHasOnlyPathAndCostShareSortOptions(t *testing.T) {
	entry := newEntry(ProjectsView{}, Scope{})
	if entry.table.sortCol != 2 || !entry.table.sortDesc {
		t.Fatalf("default sort = column %d desc=%v, want COST SHARE descending",
			entry.table.sortCol, entry.table.sortDesc)
	}
	entry.table.NextSort()
	if entry.table.sortCol != 0 || entry.table.sortDesc {
		t.Fatalf("next sort = column %d desc=%v, want NAME ascending",
			entry.table.sortCol, entry.table.sortDesc)
	}
	entry.table.NextSort()
	if entry.table.sortCol != 2 || !entry.table.sortDesc {
		t.Fatalf("sort cycle = column %d desc=%v, want COST SHARE descending",
			entry.table.sortCol, entry.table.sortDesc)
	}
}

func TestProjectsViewUsesRelativePerProjectTrendScale(t *testing.T) {
	columns := (ProjectsView{}).Columns()
	trend := columns[len(columns)-1]
	if trend.Kind != CellSparkline || trend.SparkScale != SparkScaleLocal {
		t.Fatalf("trend kind/scale = %v/%v, want sparkline/local", trend.Kind, trend.SparkScale)
	}
	if !strings.Contains(trend.Title, "REL") {
		t.Errorf("relative trend scale must be labelled in the header, got %q", trend.Title)
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
		if strings.Contains(row.Cells[0].Text, unpricedFixtureModel) {
			found = true
			costCell := row.Cells[len(row.Cells)-1].Text
			if !strings.Contains(costCell, "—") {
				t.Errorf("an unpriceable model must show — for cost, got %q", costCell)
			}
		}
	}
	if !found {
		t.Errorf("%s must be listed even though it has no rate", unpricedFixtureModel)
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
	if rows[0].Key != unpricedFixtureModel {
		t.Errorf("key = %q, want %s", rows[0].Key, unpricedFixtureModel)
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
