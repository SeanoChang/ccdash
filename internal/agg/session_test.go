package agg

import (
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestBySessionGroupsAndSpansTime(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	index := map[string]SessionBucket{}
	for _, bucket := range got {
		index[bucket.Session] = bucket
	}
	s1 := index["s1"]
	if s1.Requests != 2 {
		t.Errorf("s1 requests = %d, want 2", s1.Requests)
	}
	if s1.Started.Unix() != 1000 || s1.Ended.Unix() != 2000 {
		t.Errorf("s1 span = %d..%d, want 1000..2000", s1.Started.Unix(), s1.Ended.Unix())
	}
	if s1.Project != "/p/a" {
		t.Errorf("s1 project = %q, want /p/a", s1.Project)
	}
	if s1.Tool != model.ToolClaude {
		t.Errorf("s1 tool = %q, want claude", s1.Tool)
	}
}

func TestBySessionCountsUnpricedWithoutDroppingRows(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]SessionBucket{}
	for _, bucket := range got {
		index[bucket.Session] = bucket
	}
	// s2 holds one claude-sonnet-5 (priced) and one gpt-5.6-luna (priced),
	// so nothing is unpriced, but both requests must be counted.
	if index["s2"].Requests != 2 {
		t.Errorf("s2 requests = %d, want 2 — unpriceable rows must never be dropped",
			index["s2"].Requests)
	}
	if index["s2"].Tokens == 0 {
		t.Error("s2 tokens must be counted regardless of pricing")
	}
}

func TestBySessionSortsByMostRecent(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Session != "s2" {
		t.Errorf("first session = %q, want s2 (most recently active first)", got[0].Session)
	}
}

func TestBySessionHonorsFilter(t *testing.T) {
	db := seedDetail(t)
	got, err := BySession(db, model.DefaultPricing(), Filter{Tool: model.ToolCodex})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Session != "s2" {
		t.Fatalf("codex-only = %+v, want just s2", got)
	}
	if got[0].Requests != 1 {
		t.Errorf("codex requests in s2 = %d, want 1", got[0].Requests)
	}
}
