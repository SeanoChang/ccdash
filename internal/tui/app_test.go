package tui

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SeanoChang/ccdash/internal/model"
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

// isQuit reports whether cmd is tea.Quit. A tea.Cmd is only comparable by the
// message it produces, so the command is run.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQOpensQuitConfirmationInNormalMode(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(key("q"))
	m = next.(Model)
	if isQuit(cmd) {
		t.Error("q in normal mode must ask for confirmation before quitting")
	}
	if m.mode != modeQuit {
		t.Errorf("mode after q = %v, want modeQuit", m.mode)
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Error("ctrl+c must quit")
	}
}

func TestQIsTypedIntoAnOpenPrompt(t *testing.T) {
	for _, open := range []string{"/", ":"} {
		m := newTestModel()
		next, _ := m.Update(key(open))
		m = next.(Model)
		next, cmd := m.Update(key("q"))
		m = next.(Model)
		if isQuit(cmd) {
			t.Errorf("q must not quit while the %q prompt is open", open)
		}
		if m.input != "q" {
			t.Errorf("after %q then q, input = %q, want \"q\"", open, m.input)
		}
	}
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
	if got := m.timeRange.label(); got != "last 7d" {
		t.Errorf("range = %q, want last 7d", got)
	}
	next, _ = m.Update(key("a"))
	m = next.(Model)
	if got := m.timeRange.label(); got != "all" || !m.scope.From.IsZero() {
		t.Errorf("range 'a' must clear the window, got %q", got)
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

// TestRollingWindowFollowsTheClock is the regression test for a window that
// froze at the keystroke that chose it. Pressing "d" then leaving ccdash open
// for six hours showed a 30-hour window still labelled "last 24h".
func TestRollingWindowFollowsTheClock(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)
	m := newTestModel()
	m.now = func() time.Time { return clock }

	next, _ := m.Update(key("d"))
	m = next.(Model)
	first := m.scope.From

	clock = clock.Add(6 * time.Hour)
	m.resolveScope()

	if !m.scope.From.After(first) {
		t.Errorf("From = %v after six hours, want later than %v",
			m.scope.From, first)
	}
	if got := m.scope.To.Sub(m.scope.From); got != 24*time.Hour {
		t.Errorf("window spans %v, want 24h — a rolling window keeps its "+
			"width as it follows the clock", got)
	}
}

// TestResolveScopeReachesEveryStackLevel keeps a drilled view consistent with
// the header rather than holding the bounds it was pushed with.
func TestResolveScopeReachesEveryStackLevel(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)
	m := newTestModel()
	m.now = func() time.Time { return clock }

	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Fatalf("stack depth = %d, want 2", len(m.stack))
	}

	next, _ = m.Update(key("w"))
	m = next.(Model)
	for i, entry := range m.stack {
		if !entry.scope.From.Equal(m.scope.From) {
			t.Errorf("stack[%d].From = %v, want %v",
				i, entry.scope.From, m.scope.From)
		}
	}
}

// TestRangeAllClearsBothBounds guards the one kind with no bounds at all.
func TestRangeAllClearsBothBounds(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local) }

	next, _ := m.Update(key("w"))
	m = next.(Model)
	next, _ = m.Update(key("a"))
	m = next.(Model)

	if !m.scope.From.IsZero() || !m.scope.To.IsZero() {
		t.Errorf("all-time bounds = %v..%v, want both zero",
			m.scope.From, m.scope.To)
	}
}

// TestCalendarPresetKeys covers the windows a rolling shortcut cannot express:
// the ones a bill is reconciled against.
func TestCalendarPresetKeys(t *testing.T) {
	clock := time.Date(2026, 8, 18, 15, 30, 0, 0, time.Local)

	for _, c := range []struct {
		key      string
		wantFrom time.Time
		label    string
	}{
		{"D", time.Date(2026, 8, 18, 0, 0, 0, 0, time.Local), "today"},
		{"W", time.Date(2026, 8, 17, 0, 0, 0, 0, time.Local), "this week"},
		{"M", time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local), "this month"},
	} {
		m := newTestModel()
		m.now = func() time.Time { return clock }
		next, _ := m.Update(key(c.key))
		m = next.(Model)

		if !m.scope.From.Equal(c.wantFrom) {
			t.Errorf("%s: From = %v, want %v", c.key, m.scope.From, c.wantFrom)
		}
		if got := m.timeRange.label(); got != c.label {
			t.Errorf("%s: label = %q, want %q", c.key, got, c.label)
		}
	}
}

// TestRangeChangeResetsPagination: a narrower window cannot need the depth the
// previous one was paged into.
func TestRangeChangeResetsPagination(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local) }
	m.stack[0].pages = 4

	next, _ := m.Update(key("D"))
	m = next.(Model)

	if m.stack[0].pages != 1 {
		t.Errorf("pages = %d after a range change, want 1", m.stack[0].pages)
	}
}
