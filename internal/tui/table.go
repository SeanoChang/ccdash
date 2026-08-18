package tui

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/SeanoChang/ccdash/internal/render"
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

// sparkHeader labels a sparkline column's header with the shared domain, so the
// glyph heights have a readable magnitude (spec §7.2). Title and label together
// when they fit; the label alone when only one of the two does, because the
// magnitude is the part the glyphs cannot convey on their own. A label that will
// not fit whole is dropped rather than cut, since a truncated number misstates
// the very magnitude it is there to report, and a zero domain is left unlabelled
// because "$0.00" reads as free rather than as absent.
func sparkHeader(title string, domain float64, unit Unit, width int) string {
	if domain <= 0 {
		return title
	}
	label := formatDomain(domain, unit)
	if full := title + " " + label; lipgloss.Width(full) <= width {
		return full
	}
	if lipgloss.Width(label) <= width {
		return label
	}
	return title
}

// formatDomain prints a column's shared maximum in that column's unit.
func formatDomain(value float64, unit Unit) string {
	if unit == UnitMoney {
		return fmt.Sprintf("$%.2f", value)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
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

const percentLabelWidth = 6

func percentLabel(value float64) string {
	if math.IsNaN(value) || value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	if value > 0 && value < 0.001 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", value*100)
}

// formatPercentBar keeps an exact percentage beside the gauge. The label
// prevents a minimum visibility tick from overstating a tiny value and makes
// every nonzero share distinguishable from zero even without color.
func formatPercentBar(value float64, width int) string {
	if width <= 0 {
		return ""
	}
	label := percentLabel(value)
	if width <= percentLabelWidth {
		label = truncateDisplay(label, width)
		return strings.Repeat(" ", width-lipgloss.Width(label)) + label
	}
	barWidth := width - percentLabelWidth - 1
	bar := render.BarTrack(value, barWidth, trackRune)
	return bar + " " + strings.Repeat(" ", percentLabelWidth-lipgloss.Width(label)) + label
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
	case CellPercentBar:
		return formatPercentBar(cell.Value, width)
	case CellPath:
		text = render.TruncatePath(cell.Text, width)
	case CellSparkline:
		series := cell.Series
		if len(series) > width {
			series = series[len(series)-width:]
		}
		if column.SparkScale == SparkScaleLocal {
			text = render.Sparkline(series)
		} else {
			text = render.SparklineDomain(series, 0, domain)
		}
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

// Filter returns the active filter expression, empty when none is set. The body
// title needs to distinguish "no filter" from "a filter that happens to match
// every row".
func (t *Table) Filter() string { return t.filter }

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

// SetSort activates an explicit sort. Views use this to make their meaningful
// default ordering visible instead of depending on an unmarked query order.
func (t *Table) SetSort(column int, descending bool) {
	if column < 0 || column >= len(t.columns) || t.columns[column].DisableSort {
		return
	}
	t.sortCol = column
	t.sortDesc = descending
	t.sorted = true
	t.apply()
	t.clampSelection()
	t.clampViewport()
}

// NextSort advances to the next sortable column, wrapping around.
func (t *Table) NextSort() {
	if len(t.columns) == 0 {
		return
	}
	for step := 1; step <= len(t.columns); step++ {
		candidate := (t.sortCol + step) % len(t.columns)
		if t.columns[candidate].DisableSort {
			continue
		}
		t.SetSort(candidate, t.columns[candidate].DefaultSortDesc)
		return
	}
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

// bodyHeight is how many data rows the viewport holds: every line but the one
// the column header takes. At a single line the header is dropped instead and
// the line goes to data — a row says more there than a row of labels, and it is
// the difference between a one-line body showing something and showing nothing.
func (t *Table) bodyHeight() int {
	if t.height <= 0 {
		return 0
	}
	if !t.showsHeader() {
		return t.height
	}
	return t.height - 1
}

// showsHeader reports whether the viewport can afford the column header.
func (t *Table) showsHeader() bool { return t.height > 1 }

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
			comparison := 0
			switch column.Sort {
			case SortNumeric, SortTime:
				leftValue, rightValue := left.Cells[index].Value, right.Cells[index].Value
				if leftValue < rightValue {
					comparison = -1
				} else if leftValue > rightValue {
					comparison = 1
				}
			default:
				comparison = strings.Compare(left.Cells[index].Text, right.Cells[index].Text)
			}
			if comparison == 0 {
				return false
			}
			if t.sortDesc {
				return comparison > 0
			}
			return comparison < 0
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
		if column.Kind == CellSparkline && column.SparkScale == SparkScaleShared {
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
		if column.Kind == CellSparkline && column.SparkScale == SparkScaleShared {
			title = sparkHeader(title, domains[i], column.Unit, widths[i])
		}
		header = append(header, formatCell(Cell{Text: title},
			Column{Align: column.Align, Kind: CellText}, widths[i], 0))
	}
	if t.showsHeader() {
		lines = append(lines, styleColumn.Render(strings.Join(header, " ")))
	}

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
