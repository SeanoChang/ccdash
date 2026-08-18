package agg

import (
	"database/sql"
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

// seedUnpricedRollups builds a store holding one priceable request and two requests on
// unpricedFixtureModel, which the default table deliberately leaves unpriced.
// Every rollup must keep the unpriced rows and report how many of them it could
// not price, so a view can print an em dash instead of a misleading $0.00.
func seedUnpricedRollups(t *testing.T) *sql.DB {
	t.Helper()
	requireUnpriced(t, model.DefaultPricing(), unpricedFixtureModel)
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	day := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	recs := []model.Record{
		{ID: "p1", Tool: model.ToolClaude, TS: day, Model: "claude-opus-5",
			Project: "/p/priced", Session: "s-priced", OutputTok: 1_000_000},
		{ID: "u1", Tool: model.ToolCodex, TS: day.Add(time.Hour), Model: unpricedFixtureModel,
			Project: "/p/unpriced", Session: "s-unpriced", Agent: "agent-u",
			Workflow: "wf-u", Depth: 1, InputTok: 60, OutputTok: 40},
		{ID: "u2", Tool: model.ToolCodex, TS: day.Add(2 * time.Hour), Model: unpricedFixtureModel,
			Project: "/p/unpriced", Session: "s-unpriced", Agent: "agent-u",
			Workflow: "wf-u", Depth: 1, InputTok: 6, OutputTok: 4},
	}
	if _, err := s.UpsertRecords(recs); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}

const unpricedTokens = 110 // u1 (60+40) + u2 (6+4)

func TestByProjectCountsUnpricedRows(t *testing.T) {
	got, err := ByProject(seedUnpricedRollups(t), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, bucket := range got {
		switch bucket.Project {
		case "/p/unpriced":
			found = true
			if bucket.Unpriced != 2 {
				t.Errorf("/p/unpriced Unpriced = %d, want 2", bucket.Unpriced)
			}
			if bucket.Cost != 0 {
				t.Errorf("/p/unpriced Cost = %v, want 0", bucket.Cost)
			}
		case "/p/priced":
			if bucket.Unpriced != 0 {
				t.Errorf("/p/priced Unpriced = %d, want 0", bucket.Unpriced)
			}
		}
	}
	if !found {
		t.Fatalf("unpriceable project dropped from rollup: %+v", got)
	}
}

func TestByDayCountsUnpricedRows(t *testing.T) {
	got, err := ByDay(seedUnpricedRollups(t), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d day buckets, want 1: %+v", len(got), got)
	}
	if got[0].Unpriced != 2 {
		t.Errorf("Unpriced = %d, want 2", got[0].Unpriced)
	}
	if got[0].Tokens != 1_000_000+unpricedTokens {
		t.Errorf("Tokens = %d, want %d — unpriced tokens must still be summed",
			got[0].Tokens, 1_000_000+unpricedTokens)
	}
}

func TestByModelCountsUnpricedRows(t *testing.T) {
	got, err := ByModel(seedUnpricedRollups(t), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, bucket := range got {
		switch bucket.Model {
		case unpricedFixtureModel:
			found = true
			if bucket.Unpriced != 2 {
				t.Errorf("%s Unpriced = %d, want 2", unpricedFixtureModel, bucket.Unpriced)
			}
			if bucket.Requests != 2 {
				t.Errorf("%s Requests = %d, want 2", unpricedFixtureModel, bucket.Requests)
			}
			if bucket.Tokens != unpricedTokens {
				t.Errorf("%s Tokens = %d, want %d", unpricedFixtureModel,
					bucket.Tokens, unpricedTokens)
			}
		case "claude-opus-5":
			if bucket.Unpriced != 0 {
				t.Errorf("claude-opus-5 Unpriced = %d, want 0", bucket.Unpriced)
			}
		}
	}
	if !found {
		t.Fatalf("unpriceable model dropped from rollup: %+v", got)
	}
}

func TestByAgentCountsUnpricedRows(t *testing.T) {
	got, err := ByAgent(seedUnpricedRollups(t), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(got), got)
	}
	if got[0].Agent != "agent-u" {
		t.Fatalf("agent = %q, want agent-u", got[0].Agent)
	}
	if got[0].Unpriced != 2 {
		t.Errorf("Unpriced = %d, want 2", got[0].Unpriced)
	}
	if got[0].Tokens != unpricedTokens {
		t.Errorf("Tokens = %d, want %d", got[0].Tokens, unpricedTokens)
	}
}

func TestByWorkflowCountsUnpricedRows(t *testing.T) {
	got, err := ByWorkflow(seedUnpricedRollups(t), model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d workflows, want 1: %+v", len(got), got)
	}
	if got[0].Workflow != "wf-u" {
		t.Fatalf("workflow = %q, want wf-u", got[0].Workflow)
	}
	if got[0].Unpriced != 2 {
		t.Errorf("Unpriced = %d, want 2", got[0].Unpriced)
	}
	if got[0].Tokens != unpricedTokens {
		t.Errorf("Tokens = %d, want %d", got[0].Tokens, unpricedTokens)
	}
}

// TestPricedRollupsLeaveUnpricedZero guards the counter against being wired to
// something other than the ok=false branch of pricing.Cost.
func TestPricedRollupsLeaveUnpricedZero(t *testing.T) {
	db := seedUnpricedRollups(t)
	pricing := model.DefaultPricing()
	agents, err := ByAgent(db, pricing, Filter{Model: "claude-opus-5"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bucket := range agents {
		if bucket.Unpriced != 0 {
			t.Errorf("agent %q Unpriced = %d, want 0", bucket.Agent, bucket.Unpriced)
		}
	}
	days, err := ByDay(db, pricing, Filter{Model: "claude-opus-5"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bucket := range days {
		if bucket.Unpriced != 0 {
			t.Errorf("day %v Unpriced = %d, want 0", bucket.Day, bucket.Unpriced)
		}
	}
}

// unpricedFixtureModel is a model the default rate table has no entry for.
// These tests previously used gpt-5-codex, which acquired a published rate on
// 2026-08-18; the fixture rows then priced to $0.00125, printed as "$0.00",
// and five em-dash assertions failed for a reason none of them named.
// requireUnpriced makes that failure say what actually happened.
const unpricedFixtureModel = "codex-auto-review"

func requireUnpriced(t *testing.T, pricing *model.Pricing, name string) {
	t.Helper()
	if pricing.HasRate(model.NormalizeModel(name)) {
		t.Fatalf("fixture model %q now has a published rate; these tests need "+
			"a model the default table cannot price — pick another", name)
	}
}
