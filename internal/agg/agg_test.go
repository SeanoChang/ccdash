package agg

import (
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

func seeded(t *testing.T) (*store.Store, *model.Pricing) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	day := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	records := []model.Record{
		{ID: "a", Tool: model.ToolClaude, TS: day, Model: "claude-opus-5", Project: "/p1", OutputTok: 1_000_000},
		{ID: "b", Tool: model.ToolClaude, TS: day.Add(time.Hour), Model: "claude-opus-5", Project: "/p2", Agent: "agent-x", OutputTok: 2_000_000},
		{ID: "c", Tool: model.ToolCodex, TS: day.AddDate(0, 0, 1), Model: "gpt-5", Project: "/p1", OutputTok: 1_000_000, CacheWrite5m: 10},
	}
	if _, err := st.UpsertRecords(records); err != nil {
		t.Fatal(err)
	}
	return st, model.DefaultPricing()
}

func TestTotalsSplitsMainAndSubagent(t *testing.T) {
	st, pricing := seeded(t)
	totals, err := Totals(st.DB(), pricing, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Requests != 3 || totals.Cost != 85 || totals.SubCost != 50 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestByDayGroupsCalendarDayAndIncludesCacheWrites(t *testing.T) {
	st, pricing := seeded(t)
	days, err := ByDay(st.DB(), pricing, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Cost != 75 || days[1].Tokens != 1_000_010 {
		t.Errorf("days = %+v", days)
	}
}

func TestByModelAndProject(t *testing.T) {
	st, pricing := seeded(t)
	models, err := ByModel(st.DB(), pricing, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Model != "claude-opus-5" {
		t.Fatalf("models = %+v", models)
	}
	projects, err := ByProject(st.DB(), pricing, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || len(projects[0].Spark) != sparkPoints {
		t.Fatalf("projects = %+v", projects)
	}
}

func TestFilterByToolAndModel(t *testing.T) {
	st, pricing := seeded(t)
	for _, filter := range []Filter{
		{Tool: model.ToolCodex},
		{Model: "gpt-5"},
	} {
		totals, err := Totals(st.DB(), pricing, filter)
		if err != nil {
			t.Fatal(err)
		}
		if totals.Requests != 1 {
			t.Errorf("filter %+v returned %d requests", filter, totals.Requests)
		}
	}
}

func TestLatestLimitsReturnsNewestPerKind(t *testing.T) {
	st, _ := seeded(t)
	older := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	newer := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, sample := range []model.LimitSample{
		{Tool: model.ToolClaude, Kind: model.KindSession, Percent: 10, ObservedAt: older, Provenance: model.ProvLive},
		{Tool: model.ToolClaude, Kind: model.KindSession, Percent: 20, ObservedAt: newer, Provenance: model.ProvLive},
	} {
		if _, err := st.InsertLimitIfChanged(sample); err != nil {
			t.Fatal(err)
		}
	}
	states, err := LatestLimits(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Percent != 20 || states[0].Age < 50*time.Minute {
		t.Fatalf("states = %+v", states)
	}
}
