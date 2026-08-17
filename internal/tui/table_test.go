package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func textRow(key string, cells ...string) Row {
	row := Row{Key: key}
	for _, text := range cells {
		row.Cells = append(row.Cells, Cell{Text: text})
	}
	return row
}

func TestComputeWidthsHonorsFixedAndFillsTotal(t *testing.T) {
	columns := []Column{
		{Title: "NAME", Width: 0},
		{Title: "COST", Width: 10},
		{Title: "REQ", Width: 8},
	}
	rows := []Row{textRow("a", "short", "$1.00", "5")}
	widths := computeWidths(columns, rows, 60)
	if len(widths) != 3 {
		t.Fatalf("got %d widths, want 3", len(widths))
	}
	if widths[1] != 10 || widths[2] != 8 {
		t.Errorf("fixed columns = %d/%d, want 10/8", widths[1], widths[2])
	}
	total := 0
	for _, width := range widths {
		total += width
	}
	// widths exclude the single space between columns
	if total+len(columns)-1 != 60 {
		t.Errorf("widths sum to %d + %d gaps, want 60", total, len(columns)-1)
	}
}

func TestComputeWidthsSharesFlexibleSpaceByContent(t *testing.T) {
	columns := []Column{{Width: 0}, {Width: 0}}
	rows := []Row{textRow("a", strings.Repeat("x", 30), "y")}
	widths := computeWidths(columns, rows, 40)
	if widths[0] <= widths[1] {
		t.Errorf("wider content should get more space: %d vs %d", widths[0], widths[1])
	}
	if widths[1] < 6 {
		t.Errorf("flexible columns have a floor of 6, got %d", widths[1])
	}
}

func TestComputeWidthsNeverExceedsTotal(t *testing.T) {
	columns := []Column{{Width: 40}, {Width: 40}, {Width: 40}}
	widths := computeWidths(columns, nil, 30)
	total := 0
	for _, width := range widths {
		total += width
	}
	if total+len(columns)-1 > 30 {
		t.Errorf("widths sum to %d, must not exceed 30", total+len(columns)-1)
	}
	for i, width := range widths {
		if width < 0 {
			t.Errorf("column %d has negative width %d", i, width)
		}
	}
}

func TestFormatCellPadsAndAligns(t *testing.T) {
	left := formatCell(Cell{Text: "ab"}, Column{Align: AlignLeft, Kind: CellText}, 6, 0)
	if lipgloss.Width(left) != 6 {
		t.Fatalf("left width = %d, want 6", lipgloss.Width(left))
	}
	if !strings.HasPrefix(left, "ab") {
		t.Errorf("left-aligned = %q", left)
	}
	right := formatCell(Cell{Text: "ab"}, Column{Align: AlignRight, Kind: CellNumber}, 6, 0)
	if lipgloss.Width(right) != 6 {
		t.Fatalf("right width = %d, want 6", lipgloss.Width(right))
	}
	if !strings.HasSuffix(right, "ab") {
		t.Errorf("right-aligned = %q", right)
	}
}

func TestFormatCellTruncatesOverlongText(t *testing.T) {
	got := formatCell(Cell{Text: "abcdefghij"}, Column{Kind: CellText}, 5, 0)
	if lipgloss.Width(got) != 5 {
		t.Fatalf("width = %d, want 5", lipgloss.Width(got))
	}
}

func TestFormatCellBarUsesTrack(t *testing.T) {
	got := formatCell(Cell{Value: 0.5}, Column{Kind: CellBar}, 10, 0)
	if lipgloss.Width(got) != 10 {
		t.Fatalf("width = %d, want 10", lipgloss.Width(got))
	}
	if !strings.ContainsRune(got, trackRune) {
		t.Errorf("a half-full bar must show track cells, got %q", got)
	}
}

func TestSparkDomainIsSharedAcrossRows(t *testing.T) {
	rows := []Row{
		{Key: "a", Cells: []Cell{{Series: []float64{1, 2, 3}}}},
		{Key: "b", Cells: []Cell{{Series: []float64{10, 20, 90}}}},
	}
	domain := sparkDomain(rows, 0)
	if domain < 90 {
		t.Fatalf("domain = %v, want at least the global max of 90", domain)
	}
	low := formatCell(rows[0].Cells[0], Column{Kind: CellSparkline}, 3, domain)
	high := formatCell(rows[1].Cells[0], Column{Kind: CellSparkline}, 3, domain)
	if low == high {
		t.Error("rows with very different magnitudes must render differently under a shared domain")
	}
	if strings.ContainsRune(low, '█') {
		t.Errorf("the small row must not max out under a shared domain: %q", low)
	}
}

func numericRows(n int) []Row {
	rows := make([]Row, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, Row{
			Key: string(rune('a' + i)),
			Cells: []Cell{
				{Text: string(rune('a' + i))},
				{Text: "v", Value: float64(n - i)},
			},
		})
	}
	return rows
}

func testTable(rows []Row) *Table {
	table := NewTable([]Column{
		{Title: "NAME", Sort: SortString, Kind: CellText},
		{Title: "VALUE", Sort: SortNumeric, Kind: CellNumber, Align: AlignRight},
	})
	table.SetSize(40, 5)
	table.SetRows(rows)
	return table
}

func TestSelectionSurvivesRefresh(t *testing.T) {
	table := testTable(numericRows(5))
	table.Move(2)
	selected, _ := table.Selected()
	if selected.Key != "c" {
		t.Fatalf("selected %q, want c", selected.Key)
	}
	table.SetRows(numericRows(5)) // same keys, fresh slice
	after, ok := table.Selected()
	if !ok || after.Key != "c" {
		t.Errorf("selection = %q after refresh, want c", after.Key)
	}
}

func TestSelectionClampsWhenKeyDisappears(t *testing.T) {
	table := testTable(numericRows(5))
	table.End()
	table.SetRows(numericRows(2))
	selected, ok := table.Selected()
	if !ok {
		t.Fatal("expected a selection")
	}
	if selected.Key != "b" {
		t.Errorf("selection = %q, want the last surviving row b", selected.Key)
	}
}

func TestSelectionEmptyOnNoRows(t *testing.T) {
	table := testTable(nil)
	if _, ok := table.Selected(); ok {
		t.Error("an empty table has no selection")
	}
	table.Move(1)
	table.Render() // must not panic
}

func TestSortCyclesAndReverses(t *testing.T) {
	table := testTable(numericRows(4))
	table.NextSort() // column 0 ascending by name
	first, _ := table.Selected()
	_ = first
	rows := table.Render()
	if len(rows) == 0 {
		t.Fatal("no rendered rows")
	}
	table.ReverseSort()
	reversed := table.Render()
	if rows[1] == reversed[1] {
		t.Error("reversing the sort must change row order")
	}
}

func TestSortNumericUsesValueNotText(t *testing.T) {
	table := NewTable([]Column{{Title: "V", Sort: SortNumeric, Kind: CellNumber}})
	table.SetSize(20, 5)
	table.SetRows([]Row{
		{Key: "x", Cells: []Cell{{Text: "9", Value: 9}}},
		{Key: "y", Cells: []Cell{{Text: "10", Value: 10}}},
	})
	table.NextSort()
	body := table.Render()
	if !strings.Contains(body[1], "9") {
		t.Errorf("ascending numeric sort should put 9 first, got %q", body[1])
	}
}

func TestFilterIsSubstringOnFirstColumn(t *testing.T) {
	table := testTable(numericRows(5))
	table.SetFilter("c")
	if table.VisibleCount() != 1 {
		t.Errorf("visible = %d, want 1", table.VisibleCount())
	}
	if table.TotalCount() != 5 {
		t.Errorf("total = %d, want 5", table.TotalCount())
	}
	table.SetFilter("")
	if table.VisibleCount() != 5 {
		t.Errorf("clearing the filter should restore 5 rows, got %d", table.VisibleCount())
	}
}

func TestFilterInvertAndRegex(t *testing.T) {
	table := testTable(numericRows(5))
	table.SetFilter("!c")
	if table.VisibleCount() != 4 {
		t.Errorf("inverted filter = %d rows, want 4", table.VisibleCount())
	}
	table.SetFilter("~^[ab]$")
	if table.VisibleCount() != 2 {
		t.Errorf("regex filter = %d rows, want 2", table.VisibleCount())
	}
	table.SetFilter("~[") // invalid
	if table.VisibleCount() != 0 {
		t.Errorf("an invalid regex must match nothing, got %d", table.VisibleCount())
	}
}

func TestRenderReturnsExactlyHeightLines(t *testing.T) {
	table := testTable(numericRows(2))
	table.SetSize(40, 6)
	lines := table.Render()
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want exactly 6 (header + 5 body)", len(lines))
	}
	for i, line := range lines {
		if lipgloss.Width(line) != 40 {
			t.Errorf("line %d width = %d, want 40", i, lipgloss.Width(line))
		}
	}
}

func TestViewportFollowsSelection(t *testing.T) {
	table := testTable(numericRows(20))
	table.SetSize(40, 5) // header + 4 body rows
	table.End()
	lines := table.Render()
	if !strings.Contains(strings.Join(lines, "\n"), "t") {
		t.Error("scrolling to the end must bring the last row into view")
	}
	if !table.AtBottom() {
		t.Error("AtBottom should report true at the end")
	}
}

// sparkTable builds a two-column table whose second column is a sparkline of
// costs, at a chosen sparkline column width.
func sparkTable(trendWidth int, series []float64) *Table {
	table := NewTable([]Column{
		{Title: "NAME", Sort: SortString, Kind: CellText},
		{Title: "TREND", Width: trendWidth, Sort: SortNumeric, Kind: CellSparkline,
			Unit: UnitMoney},
	})
	table.SetSize(40, 4)
	table.SetRows([]Row{{Key: "a", Cells: []Cell{{Text: "a"}, {Series: series}}}})
	return table
}

// TestSparklineHeaderLabelsSharedDomain covers spec §7.2: "the column header is
// labelled with the shared max", so glyph heights have a readable magnitude.
func TestSparklineHeaderLabelsSharedDomain(t *testing.T) {
	header := sparkTable(14, []float64{1, 40, 141.02}).Render()[0]
	if !strings.Contains(header, "$141.02") {
		t.Errorf("sparkline header must carry the shared max: %q", header)
	}
	if !strings.Contains(header, "TREND") {
		t.Errorf("a wide enough sparkline header keeps its title: %q", header)
	}
	if lipgloss.Width(header) != 40 {
		t.Errorf("header width = %d, want 40", lipgloss.Width(header))
	}
}

// A domain label must never displace the row's own data: the label lives in the
// header only, and the shared normalization of the body is unchanged.
func TestSparklineHeaderTruncatesGracefully(t *testing.T) {
	narrow := sparkTable(8, []float64{1, 40, 141.02}).Render()[0]
	if !strings.Contains(narrow, "$141.02") {
		t.Errorf("when only one of the two fits, the magnitude is what matters: %q", narrow)
	}
	tiny := sparkTable(5, []float64{1, 40, 141.02}).Render()[0]
	if strings.Contains(tiny, "$14") && !strings.Contains(tiny, "$141.02") {
		t.Errorf("a cut-off number misstates the magnitude; keep the title instead: %q", tiny)
	}
	if !strings.Contains(tiny, "TREND") {
		t.Errorf("a column too narrow for the max still names itself: %q", tiny)
	}
	for _, header := range []string{narrow, tiny} {
		if lipgloss.Width(header) != 40 {
			t.Errorf("header width = %d, want 40: %q", lipgloss.Width(header), header)
		}
	}
}

// An all-zero series has no magnitude to report. "$0.00" would read as free
// rather than as absent, so the header stays bare.
func TestSparklineHeaderOmitsAZeroDomain(t *testing.T) {
	header := sparkTable(14, []float64{0, 0, 0}).Render()[0]
	if strings.Contains(header, "$0.00") {
		t.Errorf("a zero domain must not be labelled $0.00: %q", header)
	}
	if !strings.Contains(header, "TREND") {
		t.Errorf("header lost its title: %q", header)
	}
}
