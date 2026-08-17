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
