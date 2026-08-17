package tui

import (
	"strings"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestSessionsViewAndDrillToRequests(t *testing.T) {
	s := seedStore(t)
	rows, err := SessionsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d sessions, want 2", len(rows))
	}
	next, scope, ok := SessionsView{}.Drill(rows[0], Scope{})
	if !ok || next.Title() != "Requests" {
		t.Fatalf("sessions must drill into requests, got %v/%v", next, ok)
	}
	if scope.Session != rows[0].Key {
		t.Errorf("scope.Session = %q, want %q", scope.Session, rows[0].Key)
	}
}

func TestRequestsViewIsALeafAndPaginates(t *testing.T) {
	s := seedStore(t)
	view := RequestsView{}
	if _, _, ok := view.Drill(Row{}, Scope{}); ok {
		t.Error("requests is a leaf")
	}
	paginator, ok := any(view).(Paginator)
	if !ok {
		t.Fatal("RequestsView must implement Paginator")
	}
	if paginator.PageSize() != 500 {
		t.Errorf("page size = %d, want 500", paginator.PageSize())
	}
	rows, more, err := paginator.Page(s.DB(), model.DefaultPricing(), Scope{}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if !more {
		t.Error("a full page must report more available")
	}
	rest, more, err := paginator.Page(s.DB(), model.DefaultPricing(), Scope{}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || more {
		t.Errorf("final page = %d rows, more = %v; want 1 and false", len(rest), more)
	}
}

func TestRequestsShowsUnpricedAsEmDash(t *testing.T) {
	s := seedStore(t)
	rows, err := RequestsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	var sawDash bool
	for _, row := range rows {
		if strings.Contains(row.Cells[len(row.Cells)-1].Text, "—") {
			sawDash = true
		}
	}
	if !sawDash {
		t.Error("the gpt-5-codex request must render its cost as —, not $0.00")
	}
	if len(rows) != 3 {
		t.Errorf("got %d rows, want all 3 — unpriceable rows are never dropped", len(rows))
	}
}

func TestAgentsAndWorkflowsViews(t *testing.T) {
	s := seedStore(t)
	agents, err := AgentsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Key != "agent-x" {
		t.Fatalf("agents = %+v, want just agent-x", agents)
	}
	workflows, err := WorkflowsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].Key != "wf-1" {
		t.Fatalf("workflows = %+v, want just wf-1", workflows)
	}
	next, scope, ok := WorkflowsView{}.Drill(workflows[0], Scope{})
	if !ok || next.Title() != "Agents" {
		t.Error("workflows must drill into agents")
	}
	if scope.Workflow != "wf-1" {
		t.Errorf("scope.Workflow = %q, want wf-1", scope.Workflow)
	}
}

func TestLimitsViewShowsProvenanceAndTrack(t *testing.T) {
	s := seedStore(t)
	rows, err := LimitsView{}.Rows(s.DB(), model.DefaultPricing(), Scope{})
	if err != nil {
		t.Fatal(err)
	}
	// No limit samples were seeded, so every expected limit shows "no data"
	// rather than a misleading 0%.
	if len(rows) != 4 {
		t.Fatalf("got %d limit rows, want 4 expected kinds", len(rows))
	}
	for _, row := range rows {
		joined := ""
		for _, cell := range row.Cells {
			joined += cell.Text
		}
		if !strings.Contains(joined, "no data") {
			t.Errorf("a missing limit must read 'no data', got %q", joined)
		}
	}
}

func TestPulseRendersItsOwnBody(t *testing.T) {
	s := seedStore(t)
	renderer, ok := any(PulseView{}).(Renderer)
	if !ok {
		t.Fatal("PulseView must implement Renderer")
	}
	lines, err := renderer.Body(s.DB(), model.DefaultPricing(), Scope{}, 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want exactly 10", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "max") {
		t.Error("the chart must label its y-domain maximum")
	}
	if !strings.Contains(joined, "cost / day") {
		t.Error("the chart needs a title")
	}
}
