package tui

import (
	"regexp"
	"sort"
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

// Table owns presentation state for a resource: sorting, filtering, scrolling
// and selection. Views supply data and know none of this.
type Table struct {
	columns  []Column
	all      []Row
	visible  []Row
	filter   string
	sortCol  int
	sortDesc bool
	sorted   bool
	selected int
	offset   int
	width    int
	height   int
}

func NewTable(columns []Column) *Table {
	return &Table{columns: columns, sortCol: -1, width: 80, height: 10}
}

func (t *Table) SetSize(width, height int) {
	t.width, t.height = width, height
	t.clampViewport()
}

func (t *Table) TotalCount() int   { return len(t.all) }
func (t *Table) VisibleCount() int { return len(t.visible) }

// SetRows replaces the data, preserving the selected row by Key. When the key
// is gone the selection clamps to the nearest valid index.
func (t *Table) SetRows(rows []Row) {
	previousKey := ""
	if row, ok := t.Selected(); ok {
		previousKey = row.Key
	}
	previousIndex := t.selected
	t.all = rows
	t.apply()
	t.selected = 0
	if previousKey != "" {
		for i, row := range t.visible {
			if row.Key == previousKey {
				t.selected = i
				t.clampViewport()
				return
			}
		}
		t.selected = previousIndex
	}
	t.clampSelection()
	t.clampViewport()
}

func (t *Table) SetFilter(filter string) {
	t.filter = filter
	t.apply()
	t.clampSelection()
	t.clampViewport()
}

// NextSort advances to the next sortable column, wrapping around.
func (t *Table) NextSort() {
	if len(t.columns) == 0 {
		return
	}
	t.sortCol = (t.sortCol + 1) % len(t.columns)
	t.sortDesc = false
	t.sorted = true
	t.apply()
	t.clampSelection()
}

func (t *Table) ReverseSort() {
	if !t.sorted {
		return
	}
	t.sortDesc = !t.sortDesc
	t.apply()
	t.clampSelection()
}

func (t *Table) Move(delta int) {
	t.selected += delta
	t.clampSelection()
	t.clampViewport()
}

func (t *Table) Page(delta int) { t.Move(delta * t.bodyHeight()) }
func (t *Table) Home()          { t.selected = 0; t.clampViewport() }

func (t *Table) End() {
	t.selected = len(t.visible) - 1
	t.clampSelection()
	t.clampViewport()
}

func (t *Table) Selected() (Row, bool) {
	if t.selected < 0 || t.selected >= len(t.visible) {
		return Row{}, false
	}
	return t.visible[t.selected], true
}

// AtBottom reports whether the selection is on the last visible row, which is
// the trigger for loading another page in a paginated view.
func (t *Table) AtBottom() bool {
	return len(t.visible) > 0 && t.selected == len(t.visible)-1
}

func (t *Table) bodyHeight() int {
	if t.height <= 1 {
		return 0
	}
	return t.height - 1 // one line for the column header
}

func (t *Table) clampSelection() {
	if t.selected < 0 {
		t.selected = 0
	}
	if t.selected >= len(t.visible) {
		t.selected = len(t.visible) - 1
	}
	if t.selected < 0 {
		t.selected = 0
	}
}

func (t *Table) clampViewport() {
	body := t.bodyHeight()
	if body <= 0 {
		t.offset = 0
		return
	}
	if t.selected < t.offset {
		t.offset = t.selected
	}
	if t.selected >= t.offset+body {
		t.offset = t.selected - body + 1
	}
	maxOffset := len(t.visible) - body
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

// apply rebuilds visible from all by filtering then sorting.
func (t *Table) apply() {
	t.visible = t.filtered()
	if t.sorted && t.sortCol >= 0 && t.sortCol < len(t.columns) {
		column := t.columns[t.sortCol]
		index := t.sortCol
		sort.SliceStable(t.visible, func(i, j int) bool {
			left, right := t.visible[i], t.visible[j]
			if index >= len(left.Cells) || index >= len(right.Cells) {
				return false
			}
			var less bool
			switch column.Sort {
			case SortNumeric, SortTime:
				less = left.Cells[index].Value < right.Cells[index].Value
			default:
				less = left.Cells[index].Text < right.Cells[index].Text
			}
			if t.sortDesc {
				return !less
			}
			return less
		})
	}
}

// filtered applies the current filter to the first column's text. A leading
// "!" inverts the match; a leading "~" switches to a regular expression. An
// invalid expression matches nothing rather than erroring out.
func (t *Table) filtered() []Row {
	if t.filter == "" {
		return append([]Row(nil), t.all...)
	}
	pattern, invert := t.filter, false
	if strings.HasPrefix(pattern, "!") {
		invert, pattern = true, pattern[1:]
	}
	var expression *regexp.Regexp
	if strings.HasPrefix(pattern, "~") {
		compiled, err := regexp.Compile(pattern[1:])
		if err != nil {
			return nil
		}
		expression = compiled
	}
	needle := strings.ToLower(pattern)
	result := make([]Row, 0, len(t.all))
	for _, row := range t.all {
		text := ""
		if len(row.Cells) > 0 {
			text = row.Cells[0].Text
		}
		var match bool
		if expression != nil {
			match = expression.MatchString(text)
		} else {
			match = strings.Contains(strings.ToLower(text), needle)
		}
		if match != invert {
			result = append(result, row)
		}
	}
	return result
}

// Render returns exactly height lines, each exactly width display cells: one
// column header followed by body rows, blank-padded when there is not enough
// data to fill the viewport.
func (t *Table) Render() []string {
	lines := make([]string, 0, t.height)
	if t.height <= 0 || t.width <= 0 {
		return lines
	}
	widths := computeWidths(t.columns, t.visible, t.width)
	domains := make([]float64, len(t.columns))
	for i, column := range t.columns {
		if column.Kind == CellSparkline {
			domains[i] = sparkDomain(t.visible, i)
		}
	}

	header := make([]string, 0, len(t.columns))
	for i, column := range t.columns {
		title := column.Title
		if t.sorted && i == t.sortCol {
			marker := "↑"
			if t.sortDesc {
				marker = "↓"
			}
			title = truncateDisplay(title+marker, widths[i])
		}
		header = append(header, formatCell(Cell{Text: title},
			Column{Align: column.Align, Kind: CellText}, widths[i], 0))
	}
	lines = append(lines, styleColumn.Render(strings.Join(header, " ")))

	body := t.bodyHeight()
	for offset := 0; offset < body; offset++ {
		index := t.offset + offset
		if index >= len(t.visible) {
			lines = append(lines, strings.Repeat(" ", t.width))
			continue
		}
		row := t.visible[index]
		cells := make([]string, 0, len(t.columns))
		for i, column := range t.columns {
			cell := Cell{}
			if i < len(row.Cells) {
				cell = row.Cells[i]
			}
			cells = append(cells, formatCell(cell, column, widths[i], domains[i]))
		}
		line := strings.Join(cells, " ")
		if index == t.selected {
			line = styleSelected.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}
