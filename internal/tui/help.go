package tui

import (
	"database/sql"
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/model"
)

// helpBinding is one row of the help view: the keys, where they apply, and what
// they do.
type helpBinding struct {
	keys    string
	context string
	action  string
}

// helpBindings is spec §5.5, in the same order. Anything not listed in §5.5 is
// unbound, so anything listed here that handleKey does not implement is the
// help lying about the keymap. The "?" row is the one place the text departs
// from the spec table: help is a view on the stack now, so j and k scroll it
// and only "?" and esc close it — "any key dismisses" would document a
// behaviour the code no longer has.
var helpBindings = []helpBinding{
	{"j k ↓ ↑", "table", "Move selection one row"},
	{"ctrl-f ctrl-b", "table", "Page down / up"},
	{"g G", "table", "Jump to first / last row"},
	{"enter", "table", "Drill into selected row (no-op on a leaf)"},
	{"esc", "table", "Pop the view stack; no-op at root"},
	{"esc", "prompt", "Cancel the command or filter prompt"},
	{"s S", "table", "Next sort option / reverse direction"},
	{"/", "table", "Open the filter prompt"},
	{":", "table", "Open the command prompt"},
	{"r", "table", "Manual refresh now"},
	{"1 2 3", "table", "Tool filter: all / claude / codex"},
	{"d w m a", "table", "Rolling range: 24h / 7d / 30d / all"},
	{"D W M", "table", "Calendar range: today / this week / this month"},
	{"?", "any", "Open this help; ? or esc closes it"},
	{"q ctrl-c", "any", "Quit (q confirms; ctrl-c immediately)"},
	{":q", "prompt", "Open quit confirmation"},
}

// helpCommands lists the canonical name of every command in DefaultRegistry —
// aliases are omitted, they are discoverable by prefix. TestHelpCommandsMatchRegistry
// keeps the two in step.
var helpCommands = []string{
	"projects", "sessions", "requests", "agents", "workflows",
	"models", "days", "limits", "pulse", "unpriced",
}

// The help table's column titles. They are named rather than written inline
// because the fixed column widths are measured from them as well as from the
// data.
const (
	helpKeysTitle    = "KEYS"
	helpContextTitle = "CONTEXT"
	helpActionTitle  = "ACTION"
)

// helpKeysWidth and helpContextWidth are the widest value each of those two
// fields holds, title included, so every remaining cell goes to the action and
// nothing is cut on a terminal wide enough to hold the keymap. They are
// measured from the data rather than written down, so a longer binding cannot
// silently outgrow them.
var helpKeysWidth, helpContextWidth = helpFieldWidths()

func helpFieldWidths() (keys, context int) {
	keys, context = lipgloss.Width(helpKeysTitle), lipgloss.Width(helpContextTitle)
	for _, row := range helpRows() {
		keys = max(keys, lipgloss.Width(row.Cells[0].Text))
		context = max(context, lipgloss.Width(row.Cells[1].Text))
	}
	return keys, context
}

// HelpView is the keymap as an ordinary resource. Making it a View rather than
// an overlay is what removes the question the overlay kept answering wrongly —
// "what do we drop when it does not fit?". A Table scrolls, so nothing is ever
// dropped and the question does not arise; esc pops it, j/k/g/G/ctrl-f/ctrl-b
// scroll it, "/" filters it and the border carries its count, none of which is
// code this file has to carry.
type HelpView struct{}

func (HelpView) Title() string { return "Help" }

func (HelpView) Columns() []Column {
	return []Column{
		{Title: helpKeysTitle, Width: helpKeysWidth, Sort: SortString, Kind: CellText},
		{Title: helpContextTitle, Width: helpContextWidth, Sort: SortString, Kind: CellText},
		{Title: helpActionTitle, Sort: SortString, Kind: CellText},
	}
}

// Rows takes neither the database nor the pricing table: the keymap is static,
// so both arguments are ignored the way PulseView ignores what it does not need.
func (HelpView) Rows(*sql.DB, *model.Pricing, Scope) ([]Row, error) { return helpRows(), nil }

// Drill reports false: the keymap is a leaf, there is nothing under a binding.
func (HelpView) Drill(Row, Scope) (View, Scope, bool) { return nil, Scope{}, false }

// UnscopedTitle reports that no filter narrows this view, so its border title
// carries the count without a scope: Help[25], never Help(claude)[25].
func (HelpView) UnscopedTitle() bool { return true }

// helpRows is the keymap followed by the command prompt's targets. The commands
// are rows of the same table rather than a hint line beneath it, so their
// discoverability costs no "does this fit?" decision: a row in a scrolling
// table always fits.
func helpRows() []Row {
	rows := make([]Row, 0, len(helpBindings)+len(helpCommands))
	for i, binding := range helpBindings {
		rows = append(rows, Row{
			Key: fmt.Sprintf("bind-%02d", i),
			Cells: []Cell{
				{Text: binding.keys}, {Text: binding.context}, {Text: binding.action},
			},
		})
	}
	for _, name := range helpCommands {
		rows = append(rows, Row{
			Key: "cmd-" + name,
			Cells: []Cell{
				{Text: ":" + name}, {Text: "prompt"}, {Text: "Open the " + name + " view"},
			},
		})
	}
	return rows
}
