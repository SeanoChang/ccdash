package tui

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/model"
)

// ansiPattern strips SGR sequences so a test can reason about column positions.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(text string) string { return ansiPattern.ReplaceAllString(text, "") }

// displayColumn is the display cell needle starts at, or -1. A byte offset will
// not do: the border rune "│" is three bytes wide and one cell wide.
func displayColumn(line, needle string) int {
	at := strings.Index(line, needle)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(line[:at])
}

func frameLines(t *testing.T, m Model) []string {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	for i, line := range lines {
		lines[i] = stripANSI(line)
	}
	return lines
}

// fakePagedView is a Paginator whose pages always come back full, so its loaded
// row count is provisional and the border title has to mark it "+".
type fakePagedView struct{}

func (fakePagedView) Title() string { return "Paged" }

func (fakePagedView) Columns() []Column {
	return []Column{{Title: "NAME", Sort: SortString, Kind: CellText}}
}

func (fakePagedView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) { return nil, nil }

func (fakePagedView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }

func (fakePagedView) PageSize() int { return 2 }

func (fakePagedView) Page(_ *sql.DB, _ *model.Pricing, _ Scope, offset, limit int) ([]Row, bool, error) {
	rows := make([]Row, 0, limit)
	for i := 0; i < limit; i++ {
		rows = append(rows, textRow(fmt.Sprintf("k%d", offset+i), fmt.Sprintf("row%d", offset+i)))
	}
	return rows, true, nil
}

func TestBodySitsInsideABorderTitledResourceScopeCount(t *testing.T) {
	m := newTestModel()
	out := stripANSI(m.View())
	if !strings.Contains(out, "Root(all)[2]") {
		t.Errorf("body border title must read Resource(scope)[count]; frame was:\n%s", out)
	}
	for _, corner := range []string{"┌", "┐", "└", "┘", "│"} {
		if !strings.Contains(out, corner) {
			t.Errorf("body must sit inside a border; %q is missing", corner)
		}
	}
}

func TestFilteredBodyTitleShowsVisibleOverTotal(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	for _, r := range []string{"a", "l"} {
		next, _ = m.Update(key(r))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	out := stripANSI(m.View())
	if !strings.Contains(out, "Root(all)[1/2]") {
		t.Errorf("a filtered body title must read [visible/total]; frame was:\n%s", out)
	}
}

func TestDrilledBodyTitleNamesTheNarrowing(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	out := stripANSI(m.View())
	if !strings.Contains(out, "Child(k1)[2]") {
		t.Errorf("a drilled body title must name the narrowing; frame was:\n%s", out)
	}
}

func TestPaginatedBodyTitleMarksFurtherPages(t *testing.T) {
	m := New(nil, model.DefaultPricing(), "/tmp/usage.db", fakePagedView{}, nil)
	m.width, m.height = 100, 24
	m.reloadCurrent()
	out := stripANSI(m.View())
	if !strings.Contains(out, "Paged(all)[2+]") {
		t.Errorf("a paginated body title must mark a provisional count with +; frame was:\n%s", out)
	}
}

// TestRealRequestsTitleMarksAndClearsThePlusMarker drives the one production
// Paginator over a store holding one row more than a page: the first page comes
// back full, so its count is provisional and marked "+", and the marker has to
// disappear once the next fetch comes back short.
func TestRealRequestsTitleMarksAndClearsThePlusMarker(t *testing.T) {
	s := seedStore(t)
	records := make([]model.Record, 0, requestsPageSize+1)
	for i := 0; i <= requestsPageSize; i++ {
		records = append(records, model.Record{
			ID: fmt.Sprintf("page-%04d", i), Tool: model.ToolClaude,
			TS: time.Unix(1_700_000_000+int64(i), 0), Model: "claude-opus-5",
			Project: "/home/u/alpha", Session: "s1", OutputTok: 10,
		})
	}
	if _, err := s.UpsertRecords(records); err != nil {
		t.Fatal(err)
	}
	m := New(s, model.DefaultPricing(), "/tmp/usage.db", RequestsView{}, nil)
	m.width, m.height = 120, 30
	m.reloadCurrent()
	if got := m.bodyTitle(m.current()); got != "Requests(all)[500+]" {
		t.Errorf("first page title = %q, want Requests(all)[500+]", got)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "Requests(all)[500+]") {
		t.Errorf("the frame must carry the marker; frame was:\n%s", out)
	}
	// G jumps to the last loaded row, which is what asks for the next page. The
	// corpus is one row past a page, so that fetch comes back short.
	next, _ := m.Update(key("G"))
	m = next.(Model)
	total := requestsPageSize + 1 + 3 // seedStore's own three records
	want := fmt.Sprintf("Requests(all)[%d]", total)
	if got := m.bodyTitle(m.current()); got != want {
		t.Errorf("after loading the last page, title = %q, want %s", got, want)
	}
}

// TestHeaderBodyAndFooterAreFlush pins the cosmetic defect: the header and
// footer carry a one-column left margin, so the table's first column has to
// start in the same column or the panels read as misaligned.
func TestHeaderBodyAndFooterAreFlush(t *testing.T) {
	m := newTestModel()
	lines := frameLines(t, m)
	context := displayColumn(lines[0], "Context:")
	crumb := displayColumn(lines[len(lines)-1], "<Root>")
	name := -1
	for _, line := range lines {
		if at := displayColumn(line, "NAME"); at >= 0 {
			name = at
			break
		}
	}
	if context < 0 || crumb < 0 || name < 0 {
		t.Fatalf("frame is missing a landmark: Context:=%d NAME=%d <Root>=%d",
			context, name, crumb)
	}
	if name != context || name != crumb {
		t.Errorf("panels are not flush: Context: at %d, NAME at %d, <Root> at %d",
			context, name, crumb)
	}
}

func TestHelpKeepsTheBodyBorder(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("?"))
	m = next.(Model)
	out := stripANSI(m.View())
	if !strings.Contains(out, "┌") || !strings.Contains(out, "└") {
		t.Error("help replaces the body content, not the body border")
	}
	if strings.Contains(out, "Root(all)") {
		t.Error("help is on top of the stack, so the border must not still name the " +
			"resource underneath it")
	}
}

func TestBodyTitleFormats(t *testing.T) {
	cases := []struct {
		name                     string
		resource, scope          string
		visible, total           int
		filtered, more, rendered bool
		want                     string
	}{
		{name: "plain", resource: "Projects", scope: "all", visible: 20, total: 20,
			want: "Projects(all)[20]"},
		{name: "drilled", resource: "Requests", scope: "sess-4f2a", visible: 312,
			total: 312, want: "Requests(sess-4f2a)[312]"},
		{name: "filtered", resource: "Projects", scope: "all", visible: 7, total: 20,
			filtered: true, want: "Projects(all)[7/20]"},
		{name: "paginated", resource: "Requests", scope: "sess-4f2a", visible: 500,
			total: 500, more: true, want: "Requests(sess-4f2a)[500+]"},
		{name: "filtered and paginated", resource: "Requests", scope: "sess-4f2a",
			visible: 7, total: 500, filtered: true, more: true,
			want: "Requests(sess-4f2a)[7/500+]"},
		{name: "empty scope reads all", resource: "Days", scope: "", visible: 3, total: 3,
			want: "Days(all)[3]"},
		{name: "a rendered body has no row count", resource: "Pulse", scope: "all",
			rendered: true, want: "Pulse(all)"},
	}
	for _, test := range cases {
		got := bodyTitle(test.resource, test.scope, test.visible, test.total,
			test.filtered, test.more, test.rendered)
		if got != test.want {
			t.Errorf("%s: bodyTitle = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestBodyPanelIsExactlySizedAndTitled(t *testing.T) {
	for _, width := range []int{177, 80, 40, 13, 7, 6, 3, 2, 1, 0} {
		body := []string{"one", "two", "three"}
		panel := bodyPanel("Projects(all)[20]", body, width)
		if len(panel) != len(body)+borderLines {
			t.Fatalf("width %d: panel = %d lines, want %d",
				width, len(panel), len(body)+borderLines)
		}
		for i, line := range panel {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("width %d: panel line %d is %d cells, want %d: %q",
					width, i, got, width, stripANSI(line))
			}
		}
		if width >= 24 && !strings.Contains(stripANSI(panel[0]), "Projects(all)[20]") {
			t.Errorf("width %d: top border must carry the title, got %q",
				width, stripANSI(panel[0]))
		}
	}
}

func TestBodyPanelDropsTheTitleBeforeOverflowing(t *testing.T) {
	panel := bodyPanel("Requests(sess-4f2a)[7/500+]", []string{"x"}, 12)
	top := stripANSI(panel[0])
	if lipgloss.Width(top) != 12 {
		t.Fatalf("top border is %d cells, want 12: %q", lipgloss.Width(top), top)
	}
	if strings.Contains(top, "500") {
		t.Errorf("a title that does not fit must be cut, not overflow: %q", top)
	}
}

func TestPulseBodyTitleOmitsTheRowCount(t *testing.T) {
	m := New(nil, model.DefaultPricing(), "/tmp/usage.db", PulseView{}, nil)
	m.width, m.height = 100, 24
	if got := m.bodyTitle(m.current()); got != "Pulse(all)" {
		t.Errorf("pulse title = %q, want Pulse(all) — a chart has no rows to count", got)
	}
}

func TestScopeLabelNamesTheMostSpecificNarrowing(t *testing.T) {
	tool := Scope{}
	tool.Tool = model.ToolClaude
	project := Scope{}
	project.Project = "/home/u/dev/projects/cli-tools/ccdash"
	session := project
	session.Session = "4f2a"
	if got := scopeLabel(Scope{}); got != "all" {
		t.Errorf("unnarrowed scope = %q, want all", got)
	}
	if got := scopeLabel(tool); got != "claude" {
		t.Errorf("tool-filtered scope = %q, want claude", got)
	}
	if got := scopeLabel(session); got != "4f2a" {
		t.Errorf("session scope = %q, want the session", got)
	}
	got := scopeLabel(project)
	if len(got) > scopeLabelWidth {
		t.Errorf("project scope %q is %d cells, want at most %d",
			got, len(got), scopeLabelWidth)
	}
	if !strings.Contains(got, "ccdash") {
		t.Errorf("a truncated project scope must keep its last segment, got %q", got)
	}
}

// TestWindowSizeSizesTheTableToTheBorderInterior guards the width bookkeeping:
// the table is handed the interior width, not the terminal width, or the border
// column pushes every row one cell wide.
func TestWindowSizeSizesTheTableToTheBorderInterior(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	if got := m.current().table.width; got != bodyWidth(120) {
		t.Errorf("table width = %d, want the border interior %d", got, bodyWidth(120))
	}
	if got := m.current().table.height; got != bodyHeight(40) {
		t.Errorf("table height = %d, want %d", got, bodyHeight(40))
	}
}
