package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func assertExactFrame(t *testing.T, out string, width, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Fatalf("frame has %d lines, want exactly %d", len(lines), height)
	}
	for i, line := range lines {
		if lipgloss.Width(line) != width {
			t.Errorf("line %d width = %d, want exactly %d: %q",
				i, lipgloss.Width(line), width, line)
		}
	}
}

func TestFrameIsExactlyViewportSized(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {200, 60}, {40, 10}, {177, 58}} {
		info := headerInfo{
			DBPath: "~/.local/share/ccdash/usage.db", Range: "all",
			Tool: "all", Tokens: "2.4B", Cost: "$2012.27 at API rates",
			Requests: "23,216", Unpriced: "9",
		}
		header := headerBlock(info, size.w, size.h)
		table := NewTable([]Column{{Title: "NAME"}, {Title: "COST", Align: AlignRight}})
		table.SetSize(bodyWidth(size.w), bodyHeight(size.h))
		table.SetRows([]Row{textRow("a", "x", "$1")})
		panel := bodyPanel("Projects(all)[1]", table.Render(), size.w)
		out := frame(header, panel, "<projects>", size.w, size.h)
		assertExactFrame(t, out, size.w, size.h)
	}
}

func TestFrameFillsEvenWithNoRows(t *testing.T) {
	header := headerBlock(headerInfo{Range: "all"}, 100, 30)
	table := NewTable([]Column{{Title: "NAME"}})
	table.SetSize(bodyWidth(100), bodyHeight(30))
	table.SetRows(nil)
	panel := bodyPanel("Projects(all)[0]", table.Render(), 100)
	out := frame(header, panel, "<projects>", 100, 30)
	assertExactFrame(t, out, 100, 30)
}

// TestHeaderCollapsesOnShortTerminals covers spec §4: "when height < 12 the
// header collapses to a single line and the body takes the remainder". The
// header must be four rows at a normal height, one row below the threshold, and
// exactly width cells in both modes.
func TestHeaderCollapsesOnShortTerminals(t *testing.T) {
	info := headerInfo{
		DBPath: "~/.local/share/ccdash/usage.db", Range: "all",
		Tool: "codex", Tokens: "2.4B", Cost: "$2012.27 at API rates",
		Requests: "23,216", Unpriced: "9",
	}
	if got := len(headerBlock(info, 120, 24)); got != headerLines {
		t.Errorf("header at height 24 = %d lines, want the full %d", got, headerLines)
	}
	if got := len(headerBlock(info, 120, collapseHeight)); got != headerLines {
		t.Errorf("header at height %d = %d lines, want the full %d",
			collapseHeight, got, headerLines)
	}
	for _, height := range []int{collapseHeight - 1, 10, 8, 4, 1} {
		if got := len(headerBlock(info, 120, height)); got != 1 {
			t.Errorf("header at height %d = %d lines, want 1", height, got)
		}
	}
	for _, size := range []struct{ w, h int }{{120, 24}, {120, 10}, {40, 10}, {40, 24},
		{200, 8}, {70, 11}} {
		for i, line := range headerBlock(info, size.w, size.h) {
			if lipgloss.Width(line) != size.w {
				t.Errorf("header %dx%d line %d width = %d, want %d",
					size.w, size.h, i, lipgloss.Width(line), size.w)
			}
			if strings.Contains(line, "\n") {
				t.Errorf("header %dx%d line %d contains a newline", size.w, size.h, i)
			}
		}
	}
	// The body takes the room the collapsed header gives up.
	if got, want := bodyHeight(10), 10-1-footerLines-borderLines; got != want {
		t.Errorf("bodyHeight(10) = %d, want %d: the body takes the remainder when "+
			"the header collapses", got, want)
	}
	// The collapsed line keeps the scope and the labelled cost while there is
	// room for them, and the frame is still exactly width x height.
	one := headerBlock(info, 120, 10)[0]
	for _, want := range []string{"all", "codex", "2.4B", "at API rates"} {
		if !strings.Contains(one, want) {
			t.Errorf("collapsed header %q is missing %q", one, want)
		}
	}
	if strings.Contains(one, "spent") {
		t.Error("cost is labelled \"at API rates\", never \"spent\"")
	}
	// A narrow collapsed header drops the totals, but the scope and the active
	// tool are the last things to go.
	narrow := headerBlock(info, 40, 10)[0]
	if !strings.Contains(narrow, "codex") {
		t.Errorf("collapsed header at width 40 must still name the active tool: %q", narrow)
	}
	out := frame(headerBlock(info, 60, 8),
		bodyPanel("Projects(all)[1]", []string{"a"}, 60), "<projects>", 60, 8)
	assertExactFrame(t, out, 60, 8)
}

// TestHeaderWidthHoldsWithColour measures the header on a colour terminal, where
// the key column and the collapsed line carry escape sequences: the styled text
// must occupy the same display width as the plain text it was measured from.
func TestHeaderWidthHoldsWithColour(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	defer lipgloss.SetColorProfile(saved)
	info := headerInfo{
		DBPath: "~/.local/share/ccdash/usage.db", Range: "all  2025-09-24 → 2026-08-16",
		Tool: "codex", Tokens: "2.4B", Cost: "$2012.27 at API rates",
		Requests: "23,216", Unpriced: "9",
	}
	for _, size := range []struct{ w, h int }{{177, 58}, {120, 24}, {80, 24}, {100, 10},
		{40, 10}, {70, 11}} {
		for i, line := range headerBlock(info, size.w, size.h) {
			if lipgloss.Width(line) != size.w {
				t.Errorf("colour header %dx%d line %d width = %d, want %d",
					size.w, size.h, i, lipgloss.Width(line), size.w)
			}
		}
	}
}

// TestActiveToolFilterIsHighlighted covers spec §4.1: "middle is the tool filter
// with the active one highlighted".
func TestActiveToolFilterIsHighlighted(t *testing.T) {
	for _, tc := range []struct {
		tool string
		row  int
	}{{"", 0}, {"all", 0}, {"claude", 1}, {"codex", 2}} {
		lines := headerBlock(headerInfo{Range: "all", Tool: tc.tool}, 120, 24)
		for i, line := range lines {
			if marked := strings.ContainsRune(line, activeMarker); marked != (i == tc.row) {
				t.Errorf("tool %q: header row %d marked = %v, want %v: %q",
					tc.tool, i, marked, i == tc.row, line)
			}
		}
	}
	plain := headerBlock(headerInfo{Range: "all"}, 120, 24)
	codex := headerBlock(headerInfo{Range: "all", Tool: "codex"}, 120, 24)
	if strings.Join(plain, "\n") == strings.Join(codex, "\n") {
		t.Error("the header must differ between tool filters")
	}
	// An unknown tool highlights nothing rather than the wrong row.
	for i, line := range headerBlock(headerInfo{Range: "all", Tool: "nope"}, 120, 24) {
		if strings.ContainsRune(line, activeMarker) {
			t.Errorf("unknown tool marked header row %d: %q", i, line)
		}
	}
}

func TestBodyHeightLeavesRoomForChrome(t *testing.T) {
	// 4 header rows + 2 body border rows + 1 footer = 7 lines of chrome, so the
	// viewport is spec §4's h - 7.
	if got := bodyHeight(30); got != 23 {
		t.Errorf("bodyHeight(30) = %d, want 23", got)
	}
	if got := bodyHeight(6); got < 1 {
		t.Errorf("bodyHeight(6) = %d, must stay at least 1", got)
	}
	if got := bodyHeight(3); got < 1 {
		t.Errorf("bodyHeight(3) = %d, must stay at least 1", got)
	}
}

// headerRows is the header slice of a rendered frame: spec §4's rows 0..3, or
// the single row the header collapses to on a short terminal — everything above
// the body's top border.
func headerRows(view string) string {
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "┌") {
			return strings.Join(lines[:i], "\n")
		}
	}
	return view
}

// TestShortTerminalCollapsesTheRenderedFrame is the end-to-end form of spec
// §4's "when height < 12 the header collapses to a single line and the body
// takes the remainder": on a 10-row terminal the body's top border must land on
// row 1, not row 4.
func TestShortTerminalCollapsesTheRenderedFrame(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 100, 10
	m.reloadCurrent()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 10 {
		t.Fatalf("frame has %d lines, want 10", len(lines))
	}
	if !strings.HasPrefix(lines[1], "┌") {
		t.Errorf("at height 10 the header must collapse to one row so the body "+
			"border sits on row 1; row 1 = %q", lines[1])
	}
}

// TestToolKeyChangesTheRenderedHeader covers spec §4.1's "middle is the tool
// filter with the active one highlighted" through the keybinding of §5.5.
func TestToolKeyChangesTheRenderedHeader(t *testing.T) {
	m := newTestModel()
	all := headerRows(m.View())
	next, _ := m.Update(key("3"))
	m = next.(Model)
	codex := headerRows(m.View())
	if all == codex {
		t.Error("pressing 3 must change the header: the active tool filter is " +
			"highlighted there (spec §4.1)")
	}
	if !strings.Contains(codex, string(activeMarker)+"<3> codex") {
		t.Errorf("after 3 the codex key must be marked active; header =\n%s", codex)
	}
	if !strings.Contains(all, string(activeMarker)+"<1> all") {
		t.Errorf("with no tool filter the all key must be marked active; header =\n%s", all)
	}
	next, _ = m.Update(key("1"))
	m = next.(Model)
	if got := headerRows(m.View()); got != all {
		t.Error("pressing 1 must restore the all-tools header")
	}
}

func TestHeaderDropsLogoWhenNarrow(t *testing.T) {
	narrow := headerBlock(headerInfo{Range: "all"}, 50, 24)
	joined := strings.Join(narrow, "\n")
	if strings.Contains(joined, "╔") {
		t.Error("the logo must be dropped first under width pressure")
	}
	for i, line := range narrow {
		if lipgloss.Width(line) != 50 {
			t.Errorf("narrow header line %d width = %d, want 50", i, lipgloss.Width(line))
		}
	}
}

// TestRangeTextReportsFilterBoundsNotDataExtent is the regression test for a
// header that answered a question nobody asked. With a week selected and data
// only in its first two days, the header used to print the data's span.
func TestRangeTextReportsFilterBoundsNotDataExtent(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local) }
	next, _ := m.Update(key("w"))
	m = next.(Model)

	m.totals.From = time.Date(2026, 8, 11, 9, 0, 0, 0, time.Local)
	m.totals.To = time.Date(2026, 8, 12, 9, 0, 0, 0, time.Local)

	got := m.rangeText()
	if !strings.Contains(got, "last 7d") {
		t.Errorf("rangeText = %q, want it to name the window", got)
	}
	if !strings.Contains(got, "2026-08-11") || !strings.Contains(got, "2026-08-18") {
		t.Errorf("rangeText = %q, want the filter's own bounds with the year", got)
	}
	if strings.Contains(got, "2026-08-12") {
		t.Errorf("rangeText = %q, must not report the data's extent as the range", got)
	}
}

// TestRangeTextForAllTimeShowsTheDataExtent: with no window there are no
// bounds to print, so the data's span is the only honest thing to say — and
// it is labelled as such.
func TestRangeTextForAllTimeShowsTheDataExtent(t *testing.T) {
	m := newTestModel()
	m.now = func() time.Time { return time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local) }
	m.totals.From = time.Date(2026, 6, 3, 9, 0, 0, 0, time.Local)
	m.totals.To = time.Date(2026, 8, 18, 9, 0, 0, 0, time.Local)

	got := m.rangeText()
	if !strings.Contains(got, "all") {
		t.Errorf("rangeText = %q, want it to say all", got)
	}
	if !strings.Contains(got, "data 2026-06-03") {
		t.Errorf("rangeText = %q, want the extent labelled as data", got)
	}
}

// TestCollapsedHeaderKeepsTheShortRange: the collapsed line keeps whole fields
// and drops the rest, so the range must arrive short enough to survive.
func TestCollapsedHeaderKeepsTheShortRange(t *testing.T) {
	line := collapsedHeader(headerInfo{
		Range: "last 7d  2026-08-11 15:00 → now", RangeShort: "7d",
		Tool: "claude", Tokens: "2.4B", Cost: "$412.80 at API rates",
	}, 40)
	if !strings.Contains(line, "7d") {
		t.Errorf("collapsed header = %q, want the short range", line)
	}
	if strings.Contains(line, "2026-08-11") {
		t.Errorf("collapsed header = %q, must use RangeShort at 40 cells", line)
	}
}
