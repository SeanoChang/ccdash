package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpBinding is one line of the help overlay: the keys, where they apply, and
// what they do.
type helpBinding struct {
	keys    string
	context string
	action  string
}

// helpBindings is spec §5.5 verbatim, in the same order. Anything not listed in
// §5.5 is unbound, so anything listed here that handleKey does not implement is
// the overlay lying about the keymap.
var helpBindings = []helpBinding{
	{"j k ↓ ↑", "table", "Move selection one row"},
	{"ctrl-f ctrl-b", "table", "Page down / up"},
	{"g G", "table", "Jump to first / last row"},
	{"enter", "table", "Drill into selected row (no-op on a leaf)"},
	{"esc", "table", "Pop the view stack; no-op at root"},
	{"esc", "prompt", "Cancel the command or filter prompt"},
	{"s S", "table", "Advance sort column / reverse direction"},
	{"/", "table", "Open the filter prompt"},
	{":", "table", "Open the command prompt"},
	{"r", "table", "Manual refresh now"},
	{"1 2 3", "table", "Tool filter: all / claude / codex"},
	{"d w m a", "table", "Range: day / week / month / all"},
	{"?", "any", "Help overlay; any key dismisses"},
	{"q ctrl-c", "any", "Quit"},
	{":q", "prompt", "Quit"},
}

// helpCommands lists the canonical name of every command in DefaultRegistry —
// aliases are omitted, they are discoverable by prefix. TestHelpCommandsMatchRegistry
// keeps the two in step.
var helpCommands = []string{
	"projects", "sessions", "requests", "agents", "workflows",
	"models", "days", "limits", "pulse", "unpriced",
}

// helpCommandLine is the trailing hint listing the command prompt's targets.
func helpCommandLine() string {
	parts := make([]string, 0, len(helpCommands)+1)
	for _, name := range helpCommands {
		parts = append(parts, ":"+name)
	}
	parts = append(parts, ":q")
	return "  " + strings.Join(parts, " ")
}

const (
	// helpIndent is the binding grid's left margin, matching the table's.
	helpIndent = 2
	// helpGutter is the gap between two columns of the grid.
	helpGutter = 3
	// helpMinAction is the narrowest an action is ever squeezed to: enough that
	// the fragment left still says something. A shape that cannot give every
	// column this much is rejected rather than drawn as a row of ellipses.
	helpMinAction = 8
	// helpChrome counts the overlay's two mandatory rows: the heading and the
	// command line.
	helpChrome = 2
	// helpSpacers counts the two blank rows framing the grid. They are
	// decoration, so a short viewport gives them up before it gives up text.
	helpSpacers = 2
)

// helpColumn is one column of the binding grid: the bindings it holds, and the
// width each of the three fields is drawn at.
type helpColumn struct {
	bindings []helpBinding
	keys     int
	context  int
	action   int
}

// width is the column's display width, including the single space between each
// pair of fields.
func (c helpColumn) width() int { return c.keys + 1 + c.context + 1 + c.action }

// helpEssential marks the bindings a stuck reader opened the overlay for. They
// are the last rows dropped, never the first: someone reading help on a pane
// too small to hold it is usually looking for the way out.
func helpEssential(binding helpBinding) bool { return binding.action == "Quit" }

// splitHelpColumns deals bindings into cols columns, column-major, so every
// column reads top to bottom the way the single-column list used to. Asking for
// more columns than there are bindings to fill them simply yields fewer.
func splitHelpColumns(bindings []helpBinding, cols int) []helpColumn {
	rows := (len(bindings) + cols - 1) / cols
	columns := make([]helpColumn, 0, cols)
	for start := 0; start < len(bindings); start += rows {
		end := min(start+rows, len(bindings))
		columns = append(columns, helpColumn{bindings: bindings[start:end]})
	}
	return columns
}

// fitHelpColumns sizes each column to the widest entry it holds and reports
// whether the row fits in width cells. When it does not and shrink is set, the
// action fields are squeezed to a common ceiling — the widest one that fits, so
// the short actions ("Quit") stay whole while only the long ones are cut.
func fitHelpColumns(columns []helpColumn, width int, shrink bool) bool {
	total := helpGutter * (len(columns) - 1)
	for i := range columns {
		column := &columns[i]
		for _, binding := range column.bindings {
			column.keys = max(column.keys, lipgloss.Width(binding.keys))
			column.context = max(column.context, lipgloss.Width(binding.context))
			column.action = max(column.action, lipgloss.Width(binding.action))
		}
		total += column.width()
	}
	if total <= width {
		return true
	}
	if !shrink {
		return false
	}
	budget := width - total
	for _, column := range columns {
		budget += column.action
	}
	if budget < len(columns)*helpMinAction {
		return false
	}
	ceiling := helpActionCeiling(columns, budget)
	for i := range columns {
		columns[i].action = min(columns[i].action, ceiling)
	}
	return true
}

// helpActionCeiling is the widest cap on the action fields whose total still
// fits budget cells. Columns needing less than the cap keep their full width,
// so the budget freed by a narrow column goes to the wide ones.
func helpActionCeiling(columns []helpColumn, budget int) int {
	widest := 0
	for _, column := range columns {
		widest = max(widest, column.action)
	}
	ceiling := helpMinAction
	for candidate := helpMinAction; candidate <= widest; candidate++ {
		used := 0
		for _, column := range columns {
			used += min(column.action, candidate)
		}
		if used > budget {
			break
		}
		ceiling = candidate
	}
	return ceiling
}

// helpLayout finds the best grid holding these bindings in width cells across
// and at most rows lines down, or reports false if no shape does. Without
// shrink only shapes showing every action in full are accepted and the widest
// wins — as many columns as the terminal affords, which is the whole point of
// the grid. With shrink an action may be cut, so the fewest columns that still
// fit vertically wins instead: those are the widest cells and so the least cut.
func helpLayout(bindings []helpBinding, width, rows int, shrink bool) ([]helpColumn, bool) {
	if len(bindings) == 0 || rows < 1 || width < 1 {
		return nil, false
	}
	for i := 0; i < len(bindings); i++ {
		cols := len(bindings) - i
		if shrink {
			cols = i + 1
		}
		if (len(bindings)+cols-1)/cols > rows {
			continue
		}
		columns := splitHelpColumns(bindings, cols)
		if fitHelpColumns(columns, width, shrink) {
			return columns, true
		}
	}
	return nil, false
}

// helpSubset picks the keep bindings worth showing when they cannot all fit:
// the ways out first, then the rest in spec order. The result is returned in
// spec order, so the grid still reads the way the full one does.
func helpSubset(keep int) []helpBinding {
	chosen := make([]bool, len(helpBindings))
	for _, essential := range []bool{true, false} {
		for i, binding := range helpBindings {
			if keep == 0 {
				break
			}
			if helpEssential(binding) != essential || chosen[i] {
				continue
			}
			chosen[i], keep = true, keep-1
		}
	}
	subset := make([]helpBinding, 0, len(helpBindings))
	for i, binding := range helpBindings {
		if chosen[i] {
			subset = append(subset, binding)
		}
	}
	return subset
}

// helpRows is the number of lines the grid may occupy at this height, once the
// heading, the command line and — when spaced — the two blank rows are paid for.
func helpRows(height int, spaced bool) int {
	rows := height - helpChrome
	if spaced {
		rows -= helpSpacers
	}
	return rows
}

// helpPlan chooses the overlay's shape for this viewport, degrading in the
// order that costs the reader least: the blank spacers go first, then the width
// of the action text, and only then whole bindings. omitted counts the
// bindings that did not survive, which the caller must own up to on screen.
func helpPlan(width, height int) (columns []helpColumn, spaced bool, omitted int) {
	for _, attempt := range []struct{ spaced, shrink bool }{
		{true, false}, {false, false}, {true, true}, {false, true},
	} {
		if grid, ok := helpLayout(helpBindings, width,
			helpRows(height, attempt.spaced), attempt.shrink); ok {
			return grid, attempt.spaced, 0
		}
	}
	// Nothing holds the whole keymap. Drop bindings — never the ways out — and
	// keep a line back to say how many went.
	for keep := len(helpBindings) - 1; keep >= 1; keep-- {
		subset := helpSubset(keep)
		if grid, ok := helpLayout(subset, width, helpRows(height, false)-1, true); ok {
			return grid, false, len(helpBindings) - len(subset)
		}
	}
	return nil, false, len(helpBindings)
}

// helpGridRow renders one row of the grid. Only the last column can be short,
// since the fill is column-major, so stopping there cannot misalign the rest.
func helpGridRow(columns []helpColumn, row int) string {
	cells := make([]string, 0, len(columns))
	for _, column := range columns {
		if row >= len(column.bindings) {
			break
		}
		binding := column.bindings[row]
		cells = append(cells,
			padStyled(binding.keys, styleAccent, column.keys)+" "+
				padStyled(binding.context, styleDim, column.context)+" "+
				padLine(binding.action, column.action))
	}
	return strings.Repeat(" ", helpIndent) +
		strings.Join(cells, strings.Repeat(" ", helpGutter))
}

// padStyled is padLine with the text cut to fit before it is styled, so the
// padding stays outside the styled span and a cut can never eat the escape
// that closes it — which would leak the colour into the rest of the line.
func padStyled(text string, style lipgloss.Style, width int) string {
	return padLine(style.Render(truncateDisplay(text, width)), width)
}

// helpBody renders the overlay in place of the table body. It returns exactly
// height lines of exactly width display cells, so the frame invariant holds
// while the overlay is up. The bindings are laid out in as many columns as the
// width affords — 25 of them need 25 rows in one column but nine in three — so
// on any realistic terminal the whole keymap fits. Where it still cannot, the
// quit bindings survive and the last line counts what did not.
func helpBody(width, height int) []string {
	lines := make([]string, 0, height)
	add := func(text string) {
		if len(lines) < height {
			lines = append(lines, padLine(text, width))
		}
	}
	columns, spaced, omitted := helpPlan(width-helpIndent, height)
	rows := 0
	for _, column := range columns {
		rows = max(rows, len(column.bindings))
	}
	add(padStyled(" Keybindings", styleHeading, width))
	if spaced {
		add("")
	}
	for row := 0; row < rows; row++ {
		add(helpGridRow(columns, row))
	}
	if omitted > 0 {
		add(padStyled(fmt.Sprintf("%s… %d more", strings.Repeat(" ", helpIndent), omitted),
			styleDim, width))
	}
	if spaced {
		add("")
	}
	add(padStyled(helpCommandLine(), styleDim, width))
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height]
}
