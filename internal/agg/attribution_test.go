package agg

import (
	"testing"

	"github.com/SeanoChang/ccdash/internal/model"
)

func TestByAgentExcludesMainLoop(t *testing.T) {
	db := seedDetail(t)
	got, err := ByAgent(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1 — main-loop rows have an empty agent and are not agents", len(got))
	}
	if got[0].Agent != "agent-a" {
		t.Errorf("agent = %q, want agent-a", got[0].Agent)
	}
	if got[0].Workflow != "wf-1" {
		t.Errorf("workflow = %q, want wf-1", got[0].Workflow)
	}
	if got[0].Depth != 1 {
		t.Errorf("depth = %d, want 1", got[0].Depth)
	}
	if got[0].Requests != 1 {
		t.Errorf("requests = %d, want 1", got[0].Requests)
	}
}

func TestByWorkflowCountsDistinctAgents(t *testing.T) {
	db := seedDetail(t)
	got, err := ByWorkflow(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d workflows, want 1", len(got))
	}
	if got[0].Workflow != "wf-1" {
		t.Errorf("workflow = %q, want wf-1", got[0].Workflow)
	}
	if got[0].Agents != 1 {
		t.Errorf("agents = %d, want 1", got[0].Agents)
	}
	if got[0].Started.Unix() != 2000 {
		t.Errorf("started = %d, want 2000", got[0].Started.Unix())
	}
}

func TestByAgentSortsByCostThenName(t *testing.T) {
	db := seedDetail(t)
	got, err := ByAgent(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Cost < got[i].Cost {
			t.Fatalf("not sorted by descending cost at %d", i)
		}
	}
}
