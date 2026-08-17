package tui

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/seanochang/ccdash/internal/model"
)

func TestDefaultRegistryCoversEveryDocumentedCommand(t *testing.T) {
	registry := DefaultRegistry()
	for _, name := range []string{
		"projects", "proj", "p",
		"models", "model", "m",
		"sessions", "sess", "s",
		"requests", "req", "r",
		"agents", "agent", "a",
		"workflows", "wf", "w",
		"limits", "limit", "l",
		"days", "day", "d",
		"unpriced", "unp", "u",
		"pulse", "pu",
	} {
		if _, ok := resolveCommand(name, registry); !ok {
			t.Errorf("command %q does not resolve", name)
		}
	}
}

func TestRegistryBuildersReturnFreshViews(t *testing.T) {
	registry := DefaultRegistry()
	build := registry["projects"]
	if build == nil {
		t.Fatal("projects missing")
	}
	if build().Title() != "Projects" {
		t.Errorf("title = %q", build().Title())
	}
}

// pagedView is a Paginator over a fixed corpus, used to exercise the app
// loop's load-more wiring without a database.
type pagedView struct{ total int }

func (pagedView) Title() string { return "Paged" }
func (pagedView) Columns() []Column {
	return []Column{{Title: "NAME", Sort: SortString, Kind: CellText}}
}
func (pagedView) PageSize() int { return 3 }

func (p pagedView) Page(_ *sql.DB, _ *model.Pricing, _ Scope, offset, limit int) ([]Row, bool, error) {
	rows := make([]Row, 0, limit)
	for i := offset; i < offset+limit && i < p.total; i++ {
		key := fmt.Sprintf("k%02d", i)
		rows = append(rows, textRow(key, key))
	}
	return rows, len(rows) == limit, nil
}

func (p pagedView) Rows(db *sql.DB, pricing *model.Pricing, scope Scope) ([]Row, error) {
	rows, _, err := p.Page(db, pricing, scope, 0, p.PageSize())
	return rows, err
}

func (pagedView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }

func TestPaginatedViewLoadsNextPageAtBottom(t *testing.T) {
	m := New(nil, model.DefaultPricing(), "/tmp/usage.db", pagedView{total: 7}, nil)
	m.width, m.height = 100, 24
	m.reloadCurrent()
	if got := m.current().table.TotalCount(); got != 3 {
		t.Fatalf("first page = %d rows, want 3", got)
	}
	// Walking onto the last loaded row must pull the next page in.
	for i := 0; i < 3; i++ {
		next, _ := m.Update(key("j"))
		m = next.(Model)
	}
	if got := m.current().table.TotalCount(); got != 6 {
		t.Fatalf("after reaching the bottom = %d rows, want 6", got)
	}
	for i := 0; i < 3; i++ {
		next, _ := m.Update(key("j"))
		m = next.(Model)
	}
	if got := m.current().table.TotalCount(); got != 7 {
		t.Fatalf("second extension = %d rows, want 7 (the whole corpus)", got)
	}
	// The corpus is exhausted, so no further page may be requested.
	if m.current().more {
		t.Error("a short page must clear more, ending pagination")
	}
	if pages := m.current().pages; pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
}

func TestNonPaginatedViewNeverLoadsMore(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("j"))
	m = next.(Model)
	if m.current().more {
		t.Error("a plain view must never be marked as having more pages")
	}
	if got := m.current().pages; got != 1 {
		t.Errorf("pages = %d, want 1", got)
	}
}
