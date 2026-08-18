package agg

import (
	"database/sql"
	"testing"
	"time"

	"github.com/seanochang/ccdash/internal/model"
	"github.com/seanochang/ccdash/internal/store"
)

func TestRequestsNewestFirstAndPaginated(t *testing.T) {
	db := seedDetail(t)
	pricing := model.DefaultPricing()
	first, err := Requests(db, pricing, Filter{}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("page size = %d, want 2", len(first))
	}
	if first[0].ID != "r4" {
		t.Errorf("first row = %q, want r4 (newest first)", first[0].ID)
	}
	second, err := Requests(db, pricing, Filter{}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("second page = %d, want 2", len(second))
	}
	if second[0].ID == first[0].ID {
		t.Error("pages overlap")
	}
}

func TestRequestsMarksUnpricedWithoutDropping(t *testing.T) {
	db := seedUnpriced(t)
	got, err := Requests(db, model.DefaultPricing(), Filter{}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 — an unpriceable row must still be listed", len(got))
	}
	var priced, unpriced int
	for _, row := range got {
		if row.Priced {
			priced++
		} else {
			unpriced++
		}
	}
	if priced != 1 || unpriced != 1 {
		t.Errorf("priced/unpriced = %d/%d, want 1/1", priced, unpriced)
	}
}

func TestUnpricedListGroupsAndSpans(t *testing.T) {
	db := seedUnpriced(t)
	got, err := UnpricedList(db, model.DefaultPricing(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d unpriced models, want 1", len(got))
	}
	if got[0].Model != unpricedFixtureModel {
		t.Errorf("model = %q, want %s", got[0].Model, unpricedFixtureModel)
	}
	if got[0].Requests != 1 {
		t.Errorf("requests = %d, want 1", got[0].Requests)
	}
	if got[0].Tokens == 0 {
		t.Error("tokens must be summed even when the model has no rate")
	}
}

// seedUnpriced builds a store with one priced and one deliberately unpriced
// model. unpricedFixtureModel is absent from the default table by design.
func seedUnpriced(t *testing.T) *sql.DB {
	t.Helper()
	requireUnpriced(t, model.DefaultPricing(), unpricedFixtureModel)
	s, err := store.Open(t.TempDir() + "/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.UpsertRecords([]model.Record{
		{ID: "p1", Tool: model.ToolClaude, TS: time.Unix(1000, 0),
			Model: "claude-opus-5", Session: "s1", OutputTok: 100},
		{ID: "u1", Tool: model.ToolCodex, TS: time.Unix(2000, 0),
			Model: unpricedFixtureModel, Session: "s1", OutputTok: 200},
	}); err != nil {
		t.Fatal(err)
	}
	return s.DB()
}
