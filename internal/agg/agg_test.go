package agg

import (
	"database/sql"
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

func TestFilterNarrowsBySessionAgentWorkflow(t *testing.T) {
	db := seedDetail(t)
	pricing := model.DefaultPricing()

	all, err := Totals(db, pricing, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Requests != 4 {
		t.Fatalf("seed should have 4 requests, got %d", all.Requests)
	}

	bySession, err := Totals(db, pricing, Filter{Session: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if bySession.Requests != 2 {
		t.Errorf("session s1 = %d requests, want 2", bySession.Requests)
	}

	byAgent, err := Totals(db, pricing, Filter{Agent: "agent-a"})
	if err != nil {
		t.Fatal(err)
	}
	if byAgent.Requests != 1 {
		t.Errorf("agent-a = %d requests, want 1", byAgent.Requests)
	}

	byWorkflow, err := Totals(db, pricing, Filter{Workflow: "wf-1"})
	if err != nil {
		t.Fatal(err)
	}
	if byWorkflow.Requests != 1 {
		t.Errorf("wf-1 = %d requests, want 1", byWorkflow.Requests)
	}
}

func TestScanDetailReturnsIdentityColumns(t *testing.T) {
	db := seedDetail(t)
	rows, err := scanDetail(db, Filter{Session: "s1"}, "ts ASC", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ID == "" {
		t.Error("detail scan must select the request id")
	}
	if rows[0].Session != "s1" {
		t.Errorf("Session = %q, want s1", rows[0].Session)
	}
	if rows[0].Tool == "" {
		t.Error("detail scan must select the tool")
	}
	if !rows[0].TS.Before(rows[1].TS) {
		t.Error("ts ASC order not honored")
	}
}

func TestScanDetailPaginates(t *testing.T) {
	db := seedDetail(t)
	first, err := scanDetail(db, Filter{}, "ts ASC", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scanDetail(db, Filter{}, "ts ASC", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("page sizes = %d/%d, want 2/2", len(first), len(second))
	}
	if first[0].ID == second[0].ID {
		t.Error("pages overlap; offset is not applied")
	}
}

// seedDetail builds a store with four requests spanning two sessions, one
// subagent and one workflow.
func seedDetail(t *testing.T) *sql.DB {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	recs := []model.Record{
		{ID: "r1", Tool: model.ToolClaude, TS: time.Unix(1000, 0), Model: "claude-opus-5",
			Project: "/p/a", Session: "s1", OutputTok: 10},
		{ID: "r2", Tool: model.ToolClaude, TS: time.Unix(2000, 0), Model: "claude-opus-5",
			Project: "/p/a", Session: "s1", Agent: "agent-a", Workflow: "wf-1",
			Depth: 1, OutputTok: 20},
		{ID: "r3", Tool: model.ToolClaude, TS: time.Unix(3000, 0), Model: "claude-sonnet-5",
			Project: "/p/b", Session: "s2", OutputTok: 30},
		{ID: "r4", Tool: model.ToolCodex, TS: time.Unix(4000, 0), Model: "gpt-5.6-luna",
			Project: "/p/b", Session: "s2", OutputTok: 40},
	}
	if _, err := s.UpsertRecords(recs); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}
