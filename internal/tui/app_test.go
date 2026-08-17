package tui

import (
	"database/sql"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanochang/ccdash/internal/model"
)

// fakeView is a two-level resource used to exercise navigation.
type fakeView struct {
	title string
	leaf  bool
}

func (f fakeView) Title() string { return f.title }
func (f fakeView) Columns() []Column {
	return []Column{{Title: "NAME", Sort: SortString, Kind: CellText}}
}
func (f fakeView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) {
	return []Row{textRow("k1", "alpha"), textRow("k2", "beta")}, nil
}
func (f fakeView) Drill(row Row, scope Scope) (View, Scope, bool) {
	if f.leaf {
		return nil, scope, false
	}
	scope.Session = row.Key
	return fakeView{title: "Child", leaf: true}, scope, true
}

func newTestModel() Model {
	m := New(nil, model.DefaultPricing(), "/tmp/usage.db", fakeView{title: "Root"}, nil)
	m.width, m.height = 100, 24
	m.reloadCurrent()
	return m
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	panic("unknown key " + s)
}

func TestDrillPushesAndEscPops(t *testing.T) {
	m := newTestModel()
	if len(m.stack) != 1 {
		t.Fatalf("initial stack depth = %d, want 1", len(m.stack))
	}
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Fatalf("after enter, depth = %d, want 2", len(m.stack))
	}
	if m.current().scope.Session != "k1" {
		t.Errorf("drill did not narrow scope: %q", m.current().scope.Session)
	}
	next, _ = m.Update(key("esc"))
	m = next.(Model)
	if len(m.stack) != 1 {
		t.Errorf("after esc, depth = %d, want 1", len(m.stack))
	}
}

func TestEscAtRootDoesNotQuit(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(key("esc"))
	m = next.(Model)
	if len(m.stack) != 1 {
		t.Errorf("esc at root changed the stack to depth %d", len(m.stack))
	}
	if cmd != nil {
		t.Error("esc at root must not emit a command, least of all Quit")
	}
}

func TestEnterOnLeafIsNoOp(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	next, _ = m.Update(key("enter")) // now on the leaf
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Errorf("enter on a leaf changed depth to %d, want 2", len(m.stack))
	}
}

func TestBreadcrumbTracksStack(t *testing.T) {
	m := newTestModel()
	if got := m.breadcrumb(); got != "<Root>" {
		t.Errorf("breadcrumb = %q, want <Root>", got)
	}
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if got := m.breadcrumb(); !strings.Contains(got, "Child") {
		t.Errorf("breadcrumb = %q, want it to include Child", got)
	}
}

func TestWindowSizeDrivesTheFrame(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	assertExactFrame(t, m.View(), 120, 40)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m = next.(Model)
	assertExactFrame(t, m.View(), 60, 12)
}

func TestToolAndRangeKeysSetScope(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("2"))
	m = next.(Model)
	if m.scope.Tool != model.ToolClaude {
		t.Errorf("tool = %q, want claude", m.scope.Tool)
	}
	next, _ = m.Update(key("w"))
	m = next.(Model)
	if m.rangeLabel != "week" {
		t.Errorf("range = %q, want week", m.rangeLabel)
	}
	next, _ = m.Update(key("a"))
	m = next.(Model)
	if m.rangeLabel != "all" || !m.scope.From.IsZero() {
		t.Errorf("range 'a' must clear the window, got %q", m.rangeLabel)
	}
}

func TestGlobalScopeSurvivesDrill(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("2"))
	m = next.(Model)
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.current().scope.Tool != model.ToolClaude {
		t.Error("tool filter must carry into a drilled view")
	}
}

func TestCostIsLabelledAtAPIRates(t *testing.T) {
	m := newTestModel()
	out := m.View()
	if strings.Contains(strings.ToLower(out), "spent") {
		t.Error(`no rendered surface may say "spent" — these are subscription plans`)
	}
	if !strings.Contains(out, "at API rates") {
		t.Error(`the header must label cost "at API rates"`)
	}
}
