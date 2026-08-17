package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/render"
)

const (
	columnGap    = 1
	minFlexWidth = 6
)

// computeWidths divides total display columns among the table's columns.
// Fixed-width columns are honored first; whatever remains is shared among
// flexible columns in proportion to their widest cell, with a floor of
// minFlexWidth. Rounding remainder lands on the first flexible column.
func computeWidths(columns []Column, rows []Row, total int) []int {
	widths := make([]int, len(columns))
	if len(columns) == 0 {
		return widths
	}
	available := total - columnGap*(len(columns)-1)
	if available < 0 {
		available = 0
	}

	fixedTotal, flexible := 0, make([]int, 0, len(columns))
	for i, column := range columns {
		if column.Width > 0 {
			widths[i] = column.Width
			fixedTotal += column.Width
		} else {
			flexible = append(flexible, i)
		}
	}

	// Over-subscribed by fixed columns alone: shrink them proportionally.
	if fixedTotal > available {
		scale := float64(available) / float64(fixedTotal)
		for i, column := range columns {
			if column.Width > 0 {
				widths[i] = int(float64(column.Width) * scale)
			}
		}
		return widths
	}

	remaining := available - fixedTotal
	if len(flexible) == 0 {
		return widths
	}

	// Weight each flexible column by its widest cell, headers included.
	weights := make([]int, len(flexible))
	weightTotal := 0
	for slot, index := range flexible {
		widest := lipgloss.Width(columns[index].Title)
		for _, row := range rows {
			if index >= len(row.Cells) {
				continue
			}
			if width := lipgloss.Width(row.Cells[index].Text); width > widest {
				widest = width
			}
		}
		if widest < minFlexWidth {
			widest = minFlexWidth
		}
		weights[slot] = widest
		weightTotal += widest
	}

	assigned := 0
	for slot, index := range flexible {
		width := minFlexWidth
		if weightTotal > 0 {
			width = remaining * weights[slot] / weightTotal
		}
		if width < minFlexWidth {
			width = minFlexWidth
		}
		widths[index] = width
		assigned += width
	}
	// Hand the remainder to the first flexible column, and claw back any
	// overshoot caused by the minimum-width floor.
	widths[flexible[0]] += remaining - assigned
	if widths[flexible[0]] < 0 {
		widths[flexible[0]] = 0
	}
	return widths
}

// sparkDomain returns the largest value across every row's series in column
// index, so sparklines on different rows are comparable.
func sparkDomain(rows []Row, index int) float64 {
	maximum := 0.0
	for _, row := range rows {
		if index >= len(row.Cells) {
			continue
		}
		for _, value := range row.Cells[index].Series {
			if value > maximum {
				maximum = value
			}
		}
	}
	return maximum
}

// truncateDisplay cuts text to at most width display cells, marking the cut.
func truncateDisplay(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// formatCell renders one cell to exactly width display cells. domain is the
// shared sparkline maximum and is ignored for other cell kinds.
func formatCell(cell Cell, column Column, width int, domain float64) string {
	if width <= 0 {
		return ""
	}
	var text string
	switch column.Kind {
	case CellBar:
		return render.BarTrack(cell.Value, width, trackRune)
	case CellSparkline:
		series := cell.Series
		if len(series) > width {
			series = series[len(series)-width:]
		}
		text = render.SparklineDomain(series, 0, domain)
	default:
		text = cell.Text
	}
	text = truncateDisplay(text, width)
	padding := width - lipgloss.Width(text)
	if padding < 0 {
		padding = 0
	}
	if column.Align == AlignRight {
		return strings.Repeat(" ", padding) + text
	}
	return text + strings.Repeat(" ", padding)
}
