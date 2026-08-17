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
	// helpIndent is the binding grid's left margin, matching the table's. It is
	// decoration, so the narrowest overlays spend it on keys instead.
	helpIndent = 2
	// helpGutter is the gap between two columns of the grid, and between the
	// grid and an omission count folded onto its first row.
	helpGutter = 3
	// helpMinAction is the narrowest an action is ever squeezed to: enough that
	// the fragment left still says something. A width that cannot give every
	// column this much drops the action field outright — see helpFieldsFor —
	// rather than drawing a grid of ellipses or, as this once did, nothing.
	helpMinAction = 8
)

// helpFields says how much of each binding the overlay draws. Cells are scarce,
// so the fields go from the right — the action first, then the context — and
// only once even bare keys will not fit does a binding go.
type helpFields int

const (
	fieldsFull helpFields = iota
	fieldsNoAction
	fieldsKeysOnly
)

// helpColumn is one column of the binding grid: the bindings it holds, which of
// their fields it draws, and the width each field is drawn at.
type helpColumn struct {
	bindings []helpBinding
	fields   helpFields
	keys     int
	context  int
	action   int
}

// width is the column's display width, including the single space between each
// pair of the fields it actually draws.
func (c helpColumn) width() int {
	width := c.keys
	if c.fields <= fieldsNoAction {
		width += 1 + c.context
	}
	if c.fields == fieldsFull {
		width += 1 + c.action
	}
	return width
}

// helpNotice says where the count of dropped bindings goes. It is never left
// out while anything is dropped: with no row to spare the count folds onto the
// end of the grid's first row, in a shorter form if that is all that fits.
type helpNotice int

const (
	noticeOwnLine helpNotice = iota
	noticeFolded
	noticeFoldedShort
	noticeFoldedMark
	noticeNone
)

// helpNoticeStyles are the places a count can go, roomiest first. The bare mark
// is not among them: it says something is missing without saying how much, so
// helpPlanFields only reaches for it when every counted form would leave the
// pane with no binding on it at all.
var helpNoticeStyles = []helpNotice{noticeOwnLine, noticeFolded, noticeFoldedShort}

// helpChrome is the overlay's furniture: the heading, the command hint, the
// blank rows framing the grid, and the grid's own left margin. None of it is a
// binding, so all of it is given up before a binding is.
type helpChrome struct {
	indent  int
	spaced  bool
	heading bool
	command bool
}

// rows is how many of the viewport's lines this furniture costs.
func (c helpChrome) rows() int {
	rows := 0
	if c.heading {
		rows++
	}
	if c.command {
		rows++
	}
	if c.spaced {
		rows += 2
	}
	return rows
}

// helpChromeLadder runs from the fullest overlay to the barest: the blank rows
// go first, then the heading — a title telling the reader they are looking at
// keys is the least of what they came for — then the command hint. The left
// margin is tried both ways on every rung, since two cells of air are the
// cheapest thing on the overlay to lose and losing them buys width, not rows.
// helpPlan walks the ladder from the top and keeps the richest rung that costs
// no binding.
var helpChromeLadder = []helpChrome{
	{indent: helpIndent, spaced: true, heading: true, command: true},
	{indent: 0, spaced: true, heading: true, command: true},
	{indent: helpIndent, heading: true, command: true},
	{indent: 0, heading: true, command: true},
	{indent: helpIndent, command: true},
	{indent: 0, command: true},
	{indent: helpIndent},
	{},
}

// helpShape is the overlay's decided layout: the grid, the furniture it can
// afford, how many bindings did not make it and where that count is written.
type helpShape struct {
	columns []helpColumn
	chrome  helpChrome
	notice  helpNotice
	omitted int
}

// helpEssential marks the bindings a stuck reader opened the overlay for. They
// are the last rows dropped, never the first: someone reading help on a pane
// too small to hold it is usually looking for the way out.
func helpEssential(binding helpBinding) bool { return binding.action == "Quit" }

// helpRanked is helpBindings in survival order: the ways out first — "q ctrl-c"
// before ":q", since that is the order they are tried in — then spec order. A
// cramped overlay fills from the front of this list, so the row a stuck reader
// opened it for is the one row that is always there.
var helpRanked = rankHelpBindings()

func rankHelpBindings() []helpBinding {
	ranked := make([]helpBinding, 0, len(helpBindings))
	for _, essential := range []bool{true, false} {
		for _, binding := range helpBindings {
			if helpEssential(binding) == essential {
				ranked = append(ranked, binding)
			}
		}
	}
	return ranked
}

// helpOrder is the order a chosen set of bindings is drawn in: spec order while
// the whole keymap is there, so a roomy overlay reads like §5.5, and survival
// order the moment anything has been dropped, so the ways out lead.
func helpOrder(chosen []helpBinding) []helpBinding {
	if len(chosen) == len(helpBindings) {
		return helpBindings
	}
	return chosen
}

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

// fitHelpColumns sizes each column to the widest entry it holds in the fields
// this shape draws, and reports whether the row fits in width cells. When it
// does not and shrink is set, the action fields are squeezed to a common
// ceiling — the widest one that fits, so the short actions ("Quit") stay whole
// while only the long ones are cut. An action squeezed past helpMinAction says
// nothing worth the cells, so that shape is refused and the caller falls back
// to a narrower field set rather than to an empty pane.
func fitHelpColumns(columns []helpColumn, width int, fields helpFields, shrink bool) bool {
	total := helpGutter * (len(columns) - 1)
	for i := range columns {
		column := &columns[i]
		column.fields, column.keys, column.context, column.action = fields, 0, 0, 0
		for _, binding := range column.bindings {
			column.keys = max(column.keys, lipgloss.Width(binding.keys))
			if fields <= fieldsNoAction {
				column.context = max(column.context, lipgloss.Width(binding.context))
			}
			if fields == fieldsFull {
				column.action = max(column.action, lipgloss.Width(binding.action))
			}
		}
		total += column.width()
	}
	if total <= width {
		return true
	}
	if fields != fieldsFull || !shrink {
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
// so the budget freed by a narrow column goes to the wide ones. The cells a cap
// costs only grow with the cap, so the widest one is found by halving the range
// rather than walking it — this runs inside the layout search's inner loop.
func helpActionCeiling(columns []helpColumn, budget int) int {
	used := func(candidate int) int {
		total := 0
		for _, column := range columns {
			total += min(column.action, candidate)
		}
		return total
	}
	widest := 0
	for _, column := range columns {
		widest = max(widest, column.action)
	}
	low, high := helpMinAction, widest
	for low < high {
		candidate := (low + high + 1) / 2
		if used(candidate) > budget {
			high = candidate - 1
		} else {
			low = candidate
		}
	}
	return low
}

// helpGrid deals bindings into a grid at most rows deep that fits in width
// cells, less the reserve held back on the first row for a folded omission
// count. It takes as many columns as the width affords while nothing has to be
// squeezed — that is the point of the grid — and the fewest once something
// does, since those cells are the roomiest and so the least cut.
func helpGrid(bindings []helpBinding, fields helpFields, width, rows, reserve int,
	shrink bool) ([]helpColumn, bool) {
	if len(bindings) == 0 || rows < 1 {
		return nil, false
	}
	// No column is narrower than the slimmest binding in it, so this caps the
	// columns worth dealing at all — the search runs this loop thousands of
	// times per frame, and most of those shapes never had the cells.
	slimmest := helpBindingWidth(bindings[0], fields, shrink)
	for _, binding := range bindings[1:] {
		slimmest = min(slimmest, helpBindingWidth(binding, fields, shrink))
	}
	widest := (width - reserve + helpGutter) / (slimmest + helpGutter)
	for i := 0; i < len(bindings); i++ {
		cols := len(bindings) - i
		if shrink {
			cols = i + 1
		}
		if cols > widest || (len(bindings)+cols-1)/cols > rows {
			continue
		}
		columns := splitHelpColumns(bindings, cols)
		if fitHelpColumns(columns, width-reserve, fields, shrink) {
			return columns, true
		}
	}
	return nil, false
}

// helpBindingWidth is the least this one binding can be drawn in with these
// fields: its action squeezed to helpMinAction where squeezing is allowed, and
// whole where it is not.
func helpBindingWidth(binding helpBinding, fields helpFields, shrink bool) int {
	width := lipgloss.Width(binding.keys)
	if fields <= fieldsNoAction {
		width += 1 + lipgloss.Width(binding.context)
	}
	if fields == fieldsFull {
		action := lipgloss.Width(binding.action)
		if shrink {
			action = min(action, helpMinAction)
		}
		width += 1 + action
	}
	return width
}

// helpMoreLong and helpMoreShort are the two forms of the omission count: the
// one a reader can read, and the one that fits when the row has three cells
// left. Both are counts, not hints that something might be missing.
func helpMoreLong(omitted int) string  { return fmt.Sprintf("… %d more", omitted) }
func helpMoreShort(omitted int) string { return fmt.Sprintf("+%d", omitted) }

// helpNoticeText is the omission count on a line of its own, in the longest
// form fitting width cells: the readable one, else the bare count, else a lone
// ellipsis, which no longer says how many are missing but still says that some
// are. Only a width of nothing gets nothing.
func helpNoticeText(omitted, width int) string {
	if omitted <= 0 {
		return ""
	}
	for _, text := range []string{helpMoreLong(omitted), helpMoreShort(omitted), "…"} {
		if lipgloss.Width(text) <= width {
			return text
		}
	}
	return ""
}

// helpNoticeReserve is the width a folded count needs on the grid's first row,
// gap included. A count on its own line costs a row instead, not cells.
func helpNoticeReserve(notice helpNotice, omitted int) int {
	if omitted <= 0 {
		return 0
	}
	switch notice {
	case noticeFolded:
		return helpGutter + lipgloss.Width(helpMoreLong(omitted))
	case noticeFoldedShort:
		return 1 + lipgloss.Width(helpMoreShort(omitted))
	case noticeFoldedMark:
		return 1 + lipgloss.Width("…")
	}
	return 0
}

// helpWaysOut scores a drawn set by the ways out it shows: 2 for "q ctrl-c",
// 1 for the prompt's ":q" alone, 0 for a grid the reader cannot leave from.
// helpPlan weighs this above the number of bindings drawn — four keys and no
// way out has failed the one reader the overlay is for.
func helpWaysOut(chosen []helpBinding) int {
	ways := 0
	for _, binding := range chosen {
		if !helpEssential(binding) {
			continue
		}
		if binding == helpRanked[0] {
			return 2
		}
		ways = 1
	}
	return ways
}

// helpFill takes as many bindings as this shape holds, in survival order. A
// binding too wide for the column it would land in is stepped over rather than
// stopped at, so one long key list cannot cost the reader the dozen rows behind
// it. It returns the grid, how many bindings that grid draws, and how good a
// way out it leaves them.
func helpFill(fields helpFields, notice helpNotice, width, rows int,
	shrink bool) (columns []helpColumn, drawn, ways int) {
	chosen := make([]helpBinding, 0, len(helpBindings))
	stepped := false
	for _, binding := range helpRanked {
		trial := append(chosen[:len(chosen):len(chosen)], binding)
		omitted := len(helpBindings) - len(trial)
		gridRows := rows
		if notice == noticeOwnLine && omitted > 0 {
			gridRows--
		}
		grid, ok := helpGrid(helpOrder(trial), fields, width, gridRows,
			helpNoticeReserve(notice, omitted), shrink)
		if !ok {
			stepped = true
			continue
		}
		chosen, columns = trial, grid
	}
	// Stepping over one binding can cost the whole keymap a column split that
	// would have fitted, so the complete grid gets a last look of its own.
	if stepped && len(chosen) < len(helpBindings) {
		if grid, ok := helpGrid(helpBindings, fields, width, rows, 0, shrink); ok {
			return grid, len(helpBindings), 2
		}
	}
	return columns, len(chosen), helpWaysOut(chosen)
}

// helpWidthCapacity is how many bindings a single column this wide can hold
// with these fields, height aside: the width's own verdict on a field set.
func helpWidthCapacity(fields helpFields, width int) int {
	chosen := make([]helpBinding, 0, len(helpBindings))
	for _, binding := range helpRanked {
		trial := append(chosen[:len(chosen):len(chosen)], binding)
		if fitHelpColumns([]helpColumn{{bindings: trial}}, width, fields,
			fields == fieldsFull) {
			chosen = trial
		}
	}
	return len(chosen)
}

// helpFieldsFor decides how much of each binding this width can carry. A field
// earns its cells only while most of the keymap still fits beside it: the
// action column is worth having at 30 cells across, and worth nothing at 20,
// where it would leave room for the two Quit rows and nothing else. Height
// never enters into it — a short viewport drops rows, a narrow one drops
// fields, and the two pressures stay separate.
func helpFieldsFor(width int) helpFields {
	for _, fields := range []helpFields{fieldsFull, fieldsNoAction} {
		if 2*helpWidthCapacity(fields, width) >= len(helpBindings) {
			return fields
		}
	}
	return fieldsKeysOnly
}

// helpShrinkModes is whether squeezing the action is on the table: only where
// there is an action field to squeeze, and full text is tried before cut text.
func helpShrinkModes(fields helpFields) []bool {
	if fields == fieldsFull {
		return []bool{false, true}
	}
	return []bool{false}
}

// helpPlan chooses the overlay's shape for this viewport. Width picks the field
// set; everything else is decided by what costs the reader least, in one pass
// over every arrangement. A shape that draws "q ctrl-c" → Quit beats one that
// does not however many rows the latter fits, because the reader who opened
// help on a pane this size is looking for the way out. Past that, the most
// bindings win, and among equals the richest furniture, then the roomiest
// omission count, then the fullest action text — the ladders are walked
// richest-first and only a strictly better shape displaces the incumbent, so
// furniture is kept exactly while it is free and dropped the moment it costs a
// binding.
func helpPlan(width, height int) helpShape {
	best, bestWays := helpShape{
		chrome:  helpChromeLadder[len(helpChromeLadder)-1],
		notice:  noticeOwnLine,
		omitted: len(helpBindings),
	}, -1
	// Width names the field set, but a field set this viewport cannot show a
	// way out in is not one it can afford, however wide it looks: 14 cells hold
	// "? any" and no Quit row, where bare keys hold "q ctrl-c". Richer fields
	// win ties, so the width's verdict stands wherever it can be honoured.
	for fields := helpFieldsFor(width - helpIndent); fields <= fieldsKeysOnly; fields++ {
		shape, ways := helpPlanFields(fields, width, height)
		if ways > bestWays {
			best, bestWays = shape, ways
		}
		if bestWays == 2 {
			break
		}
	}
	if best.omitted == 0 {
		best.notice = noticeNone
	}
	return best
}

// helpPlanFields is helpPlan's search for one field set: the best arrangement
// of furniture, omission count and action width this viewport affords, and the
// way out it leaves the reader.
func helpPlanFields(fields helpFields, width, height int) (helpShape, int) {
	best := helpShape{
		chrome:  helpChromeLadder[len(helpChromeLadder)-1],
		notice:  noticeOwnLine,
		omitted: len(helpBindings),
	}
	shown, bestWays := 0, 0
	walk := func(notices []helpNotice, waysOnly bool) {
		for _, chrome := range helpChromeLadder {
			rows, gridWidth := height-chrome.rows(), width-chrome.indent
			if rows < 1 || gridWidth < 1 {
				continue
			}
			for _, notice := range notices {
				for _, shrink := range helpShrinkModes(fields) {
					columns, drawn, ways := helpFill(fields, notice, gridWidth, rows, shrink)
					if ways < bestWays || ways == bestWays && (waysOnly || drawn <= shown) {
						continue
					}
					shown, bestWays = drawn, ways
					best = helpShape{columns: columns, chrome: chrome, notice: notice,
						omitted: len(helpBindings) - drawn}
				}
				if bestWays == 2 && shown == len(helpBindings) {
					break
				}
			}
			// The whole keymap, with the way out in it, on the richest rung that
			// holds it: nothing further down the ladder can beat that, and this
			// is the case every terminal anyone actually uses lands in.
			if bestWays == 2 && shown == len(helpBindings) {
				break
			}
		}
	}
	walk(helpNoticeStyles, false)
	// Where no counted form leaves a way out on screen — or leaves anything at
	// all — the bare mark buys back the cells for one: at five cells ":q …" is
	// the whole overlay, and it still says there is more. It is only ever taken
	// for a way out, never for one more ordinary row, so no overlay trades a
	// count it could have shown for an extra binding.
	if bestWays < 2 {
		walk([]helpNotice{noticeFoldedMark}, shown > 0)
	}
	return best, bestWays
}

// helpGridRow renders one row of the grid. Only the last column can be short,
// since the fill is column-major, so stopping there cannot misalign the rest. A
// folded omission count rides the first row, which is the row the ways out are
// on: "q ctrl-c any Quit   … 12 more" tells the reader both things at once.
func helpGridRow(shape helpShape, row int) string {
	cells := make([]string, 0, len(shape.columns))
	for _, column := range shape.columns {
		if row >= len(column.bindings) {
			break
		}
		binding := column.bindings[row]
		cell := padStyled(binding.keys, styleAccent, column.keys)
		if column.fields <= fieldsNoAction {
			cell += " " + padStyled(binding.context, styleDim, column.context)
		}
		if column.fields == fieldsFull {
			cell += " " + padLine(binding.action, column.action)
		}
		cells = append(cells, cell)
	}
	line := strings.Repeat(" ", shape.chrome.indent) +
		strings.Join(cells, strings.Repeat(" ", helpGutter))
	if row > 0 || shape.omitted == 0 {
		return line
	}
	switch shape.notice {
	case noticeFolded:
		return line + strings.Repeat(" ", helpGutter) +
			styleDim.Render(helpMoreLong(shape.omitted))
	case noticeFoldedShort:
		return line + " " + styleDim.Render(helpMoreShort(shape.omitted))
	case noticeFoldedMark:
		return line + " " + styleDim.Render("…")
	}
	return line
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
// width affords — 15 of them need 15 rows in one column but five in three — so
// on any realistic terminal the whole keymap fits. Where it does not, cells go
// to the keys: the blank rows, the heading and the command hint are spent
// first, then the action text and the context column, and only then whole
// bindings — and whatever is dropped is counted on screen, on its own line
// where there is a row for one and on the end of the first row where there is
// not. The heading is written after the grid is planned, never before it, so it
// can no longer take the row the payload needed.
func helpBody(width, height int) []string {
	width, height = max(width, 0), max(height, 0)
	shape := helpPlan(width, height)
	lines := make([]string, 0, height)
	add := func(text string) {
		if len(lines) < height {
			lines = append(lines, padLine(text, width))
		}
	}
	if shape.chrome.heading {
		add(padStyled(" Keybindings", styleHeading, width))
	}
	if shape.chrome.spaced {
		add("")
	}
	rows := 0
	for _, column := range shape.columns {
		rows = max(rows, len(column.bindings))
	}
	for row := 0; row < rows; row++ {
		add(helpGridRow(shape, row))
	}
	if shape.omitted > 0 && shape.notice == noticeOwnLine {
		add(padStyled(strings.Repeat(" ", shape.chrome.indent)+
			helpNoticeText(shape.omitted, width-shape.chrome.indent), styleDim, width))
	}
	if shape.chrome.spaced {
		add("")
	}
	if shape.chrome.command {
		add(padStyled(helpCommandLine(), styleDim, width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height]
}
