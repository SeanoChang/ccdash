package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openHelp opens the help view on a model sized to this viewport, and checks it
// arrived on the view stack as an ordinary view rather than as an overlay flag:
// everything else in this file — scrolling, filtering, esc, the border title —
// follows from that and from nothing written specially for help.
func openHelp(t *testing.T, width, height int) Model {
	t.Helper()
	m := newTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(Model)
	next, cmd := m.Update(key("?"))
	m = next.(Model)
	if cmd != nil {
		t.Error("? must not emit a command")
	}
	if len(m.stack) != 2 {
		t.Fatalf("? left the stack %d deep, want 2: help is pushed, not flagged",
			len(m.stack))
	}
	if _, ok := m.current().view.(HelpView); !ok {
		t.Fatalf("? pushed %T, want HelpView", m.current().view)
	}
	return m
}

// panelRows is the interior of the body panel in a rendered frame: the lines
// between the top border and the bottom border, stripped of colour and of the
// border cell either side. A frame with no room for an interior row yields
// none, which is a fact about the frame rather than about the help.
func panelRows(frame string) []string {
	lines := strings.Split(frame, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(stripANSI(line), "┌") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := len(lines) - footerLines
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(stripANSI(lines[i]), "└") {
			end = i
			break
		}
	}
	rows := make([]string, 0, max(end-start, 0))
	for i := start; i < end; i++ {
		rows = append(rows, strings.Trim(stripANSI(lines[i]), "│"))
	}
	return rows
}

// helpFieldPattern matches one field of a drawn help row: the text whole, or
// any non-empty prefix of it followed by the cut mark, which is what a column
// too narrow to hold the field draws. Nothing else counts — a field cut without
// a mark would be the reader misinformed rather than merely short-changed.
func helpFieldPattern(text string) string {
	alternatives := []string{regexp.QuoteMeta(text)}
	runes := []rune(text)
	for n := len(runes) - 1; n >= 1; n-- {
		alternatives = append(alternatives, regexp.QuoteMeta(string(runes[:n]))+"…")
	}
	return "(?:" + strings.Join(alternatives, "|") + ")"
}

// helpRowPattern matches a rendered row that documents these fields, in order,
// from the start of the line. Fields beyond the ones asked for are not looked
// at, so a viewport with no cells left for the action still documents a binding
// by its keys and its context.
func helpRowPattern(fields ...string) *regexp.Regexp {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, helpFieldPattern(field))
	}
	return regexp.MustCompile(`^` + strings.Join(parts, ` +`))
}

// helpScrolled collects every interior row the help view draws while the
// selection is walked from the first row to the last, and asserts the frame is
// exactly width × height at every step. Nothing is dropped by a scrolling
// table, so "reachable" means "seen in this collection".
func helpScrolled(t *testing.T, width, height int) []string {
	t.Helper()
	m := openHelp(t, width, height)
	seen := []string{}
	step := func() {
		out := m.View()
		assertExactFrame(t, out, width, height)
		for i, row := range panelRows(out) {
			if got := lipgloss.Width(row); got != bodyWidth(width) {
				t.Errorf("%dx%d: interior row %d is %d cells, want exactly %d: %q",
					width, height, i, got, bodyWidth(width), row)
			}
			seen = append(seen, row)
		}
	}
	step()
	// One press per row is enough to walk the whole table from the top, however
	// few rows the viewport shows at once.
	for i := 0; i < len(helpRows()); i++ {
		next, _ := m.Update(key("j"))
		m = next.(Model)
		step()
	}
	return seen
}

// helpViewportSizes are the viewports the help must hold the whole keymap at,
// written width × height throughout. The first three are ordinary terminals.
// The rest are the sizes an adversarial pty sweep caught the old overlay
// failing at — recorded rows-first there, transposed here — plus the transpose
// of every size wide enough for one, which is the same list read the other way
// round.
//
// Widths below about ten cells are deliberately absent: three columns and two
// gutters cannot spell any binding in six cells, so no layout — this one or the
// search it replaces — can reach one there, and a test demanding it would be
// demanding the impossible.
var helpViewportSizes = []struct {
	w, h int
	// noBodyRow marks a frame whose header, border and footer consume every
	// line it has, leaving the body none. The test asserts this is still true
	// rather than taking it on trust: if the frame ever gains a row here, the
	// flag has to come off and reachability has to be asserted instead.
	noBodyRow bool
}{
	{w: 177, h: 58}, {w: 58, h: 177},
	{w: 100, h: 20}, {w: 20, h: 100},
	{w: 80, h: 24}, {w: 24, h: 80},
	{w: 20, h: 60}, {w: 60, h: 20},
	{w: 16, h: 40}, {w: 40, h: 16},
	{w: 20, h: 24}, {w: 24, h: 20},
	{w: 30, h: 8},
	{w: 120, h: 7},
	{w: 177, h: 5},
	{w: 120, h: 4},
	{w: 20, h: 3, noBodyRow: true},
}

// TestHelpReachesEveryRowAtEverySize is the property the layout search kept
// failing to hold and a scrolling table holds by construction: nothing is ever
// dropped, so every row of the keymap can be reached at every size — at once
// where the viewport is roomy, and by scrolling where it is not. The frame
// stays exactly width × height throughout.
func TestHelpReachesEveryRowAtEverySize(t *testing.T) {
	for _, size := range helpViewportSizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			m := openHelp(t, size.w, size.h)
			assertExactFrame(t, m.View(), size.w, size.h)
			if size.noBodyRow {
				if rows := panelRows(m.View()); len(rows) != 0 {
					t.Fatalf("this size is marked as having no interior row, but the "+
						"frame gives it %d: drop the flag and assert reachability", len(rows))
				}
				t.Skipf("a %d-row frame spends every line on the collapsed header, the "+
					"body's top border and the footer, so it has no interior row for a "+
					"binding to be reached in; frame exactness is asserted above",
					size.h)
			}
			seen := helpScrolled(t, size.w, size.h)
			for _, row := range helpRows() {
				keys, context := row.Cells[0].Text, row.Cells[1].Text
				pattern := helpRowPattern(keys, context)
				found := false
				for _, line := range seen {
					if pattern.MatchString(line) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("scrolling the whole table never reached %q %q; %d rows seen:\n%s",
						keys, context, len(seen), strings.Join(seen, "\n"))
				}
			}
		})
	}
}

// TestHelpDocumentsEveryRowInFullOnARealTerminal is the other half: at a size
// anyone actually runs, no field is cut at all — the keys, the context and the
// whole action text are on screen, spelled out.
func TestHelpDocumentsEveryRowInFullOnARealTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{177, 58}, {100, 20}, {80, 24}, {200, 60}} {
		seen := helpScrolled(t, size.w, size.h)
		for _, row := range helpRows() {
			keys, context, action := row.Cells[0].Text, row.Cells[1].Text, row.Cells[2].Text
			want := regexp.MustCompile(`^` + regexp.QuoteMeta(keys) + ` +` +
				regexp.QuoteMeta(context) + ` +` + regexp.QuoteMeta(action))
			found := false
			for _, line := range seen {
				if want.MatchString(line) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%dx%d: %q %q %q is not documented in full", size.w, size.h,
					keys, context, action)
			}
		}
	}
}

// TestHelpListsEverySpecBinding pins the help to spec §5.5: every key the
// application binds appears, or the help is lying about the keymap.
func TestHelpListsEverySpecBinding(t *testing.T) {
	seen := strings.Join(helpScrolled(t, 100, 24), "\n")
	for _, want := range []string{
		"j k ↓ ↑", "ctrl-f ctrl-b", "g G", "enter", "esc", "s S",
		"/", ":", "r", "1 2 3", "d w m a", "?", "q ctrl-c", ":q",
	} {
		if !strings.Contains(seen, want) {
			t.Errorf("help does not document %q", want)
		}
	}
	for _, want := range []string{"Quit", "Move selection one row", "Open the filter prompt"} {
		if !strings.Contains(seen, want) {
			t.Errorf("help is missing the action text %q", want)
		}
	}
}

// TestHelpRowsAreTheKeymapPlusTheCommands keeps the data honest: one row per
// spec §5.5 binding, one per command the prompt accepts, and a distinct key on
// each so the table can hold the selection across a refresh.
func TestHelpRowsAreTheKeymapPlusTheCommands(t *testing.T) {
	if len(helpBindings) != 15 {
		t.Errorf("spec §5.5 lists 15 bindings; helpBindings has %d", len(helpBindings))
	}
	rows := helpRows()
	if want := len(helpBindings) + len(helpCommands); len(rows) != want {
		t.Fatalf("helpRows() = %d rows, want %d", len(rows), want)
	}
	keys := map[string]bool{}
	for _, row := range rows {
		if len(row.Cells) != 3 {
			t.Fatalf("row %q has %d cells, want KEYS/CONTEXT/ACTION", row.Key, len(row.Cells))
		}
		if keys[row.Key] {
			t.Errorf("duplicate row key %q: the table anchors the selection on it", row.Key)
		}
		keys[row.Key] = true
	}
}

// TestQuestionMarkTogglesTheHelpView: "?" pushes, "?" pops. The key still reads
// as a toggle even though it is now navigation.
func TestQuestionMarkTogglesTheHelpView(t *testing.T) {
	m := openHelp(t, 100, 24)
	next, cmd := m.Update(key("?"))
	m = next.(Model)
	if isQuit(cmd) {
		t.Error("? must not quit")
	}
	if len(m.stack) != 1 {
		t.Fatalf("a second ? left the stack %d deep, want 1", len(m.stack))
	}
	if _, ok := m.current().view.(HelpView); ok {
		t.Error("a second ? must pop the help view")
	}
	// The table underneath is intact: the key that closed help did not also act
	// on it.
	if got := m.current().table.TotalCount(); got != 2 {
		t.Errorf("the view under help has %d rows, want its own 2", got)
	}
}

func TestEscClosesTheHelpView(t *testing.T) {
	m := openHelp(t, 100, 24)
	next, cmd := m.Update(key("esc"))
	m = next.(Model)
	if isQuit(cmd) {
		t.Error("esc must not quit")
	}
	if len(m.stack) != 1 {
		t.Errorf("esc left the stack %d deep, want 1", len(m.stack))
	}
}

func TestCtrlCQuitsWithHelpOpen(t *testing.T) {
	m := openHelp(t, 100, 24)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Error("ctrl+c must quit from anywhere, help included")
	}
}

// TestQQuitsWithHelpOpen holds the help to its own word: the "q ctrl-c" row
// says its context is "any", so q has to quit with help on screen too.
func TestQQuitsWithHelpOpen(t *testing.T) {
	m := openHelp(t, 100, 24)
	_, cmd := m.Update(key("q"))
	if !isQuit(cmd) {
		t.Error("q must quit with help open — the help row says its context is any")
	}
}

// TestHelpScrollsInsteadOfDismissing is the interaction the old overlay could
// not offer: the movement keys move within help rather than closing it.
func TestHelpScrollsInsteadOfDismissing(t *testing.T) {
	m := openHelp(t, 80, 24)
	for _, message := range []tea.KeyMsg{
		key("j"), key("j"), key("k"),
		{Type: tea.KeyCtrlF}, {Type: tea.KeyCtrlB},
	} {
		next, cmd := m.Update(message)
		m = next.(Model)
		if isQuit(cmd) {
			t.Fatalf("%v quit", message)
		}
		if len(m.stack) != 2 {
			t.Fatalf("%v closed the help view", message)
		}
	}
	if got := m.current().table.selected; got != 1 {
		t.Errorf("j j k left the selection on row %d, want 1", got)
	}
	next, _ := m.Update(key("G"))
	m = next.(Model)
	if got, want := m.current().table.selected, len(helpRows())-1; got != want {
		t.Errorf("G left the selection on row %d, want the last row %d", got, want)
	}
	next, _ = m.Update(key("g"))
	m = next.(Model)
	if got := m.current().table.selected; got != 0 {
		t.Errorf("g left the selection on row %d, want the first", got)
	}
	if len(m.stack) != 2 {
		t.Error("g and G must not close the help view")
	}
}

// TestHelpFilterNarrowsTheKeymap: "/" filters help the same way it filters any
// other resource, and the border title reports visible over total.
func TestHelpFilterNarrowsTheKeymap(t *testing.T) {
	m := openHelp(t, 100, 24)
	next, _ := m.Update(key("/"))
	m = next.(Model)
	for _, r := range []string{"e", "s", "c"} {
		next, _ = m.Update(key(r))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if got := m.current().table.VisibleCount(); got != 2 {
		t.Errorf("filtering help on \"esc\" left %d rows, want the 2 esc bindings", got)
	}
	want := fmt.Sprintf("Help[2/%d]", len(helpRows()))
	if got := m.bodyTitle(m.current()); got != want {
		t.Errorf("filtered help title = %q, want %q", got, want)
	}
}

// TestHelpTitleClaimsNoScope is the title-honesty rule of 77a23ea in its second
// form: help is not narrowed by the tool filter, the range or a drill-down, so
// its border title must not render a scope it never applied. Help[N], never
// Help(all)[N] and never Help(claude)[N].
func TestHelpTitleClaimsNoScope(t *testing.T) {
	want := fmt.Sprintf("Help[%d]", len(helpRows()))
	m := openHelp(t, 100, 24)
	for _, press := range []string{"", "2", "3", "d", "w", "1", "a"} {
		if press != "" {
			next, _ := m.Update(key(press))
			m = next.(Model)
		}
		got := m.bodyTitle(m.current())
		if got != want {
			t.Errorf("after %q the help title = %q, want %q", press, got, want)
		}
		if strings.ContainsAny(got, "()") {
			t.Errorf("help title %q carries a scope it does not apply", got)
		}
		if !strings.Contains(stripANSI(m.View()), want) {
			t.Errorf("the rendered border does not carry %q", want)
		}
	}
}

// TestHelpIsALeaf: enter on a help row goes nowhere, so the stack cannot grow
// under the reader.
func TestHelpIsALeaf(t *testing.T) {
	m := openHelp(t, 100, 24)
	next, _ := m.Update(key("enter"))
	m = next.(Model)
	if len(m.stack) != 2 {
		t.Errorf("enter on a help row changed the stack depth to %d", len(m.stack))
	}
	if _, _, ok := (HelpView{}).Drill(helpRows()[0], Scope{}); ok {
		t.Error("HelpView.Drill must report false: the keymap has nothing to drill into")
	}
}

// TestHelpCommandsMatchRegistry catches drift in both directions: a command help
// advertises but the registry cannot resolve, and a view added to the registry
// that help never mentions.
func TestHelpCommandsMatchRegistry(t *testing.T) {
	registry := DefaultRegistry()
	listed := map[string]bool{}
	for _, name := range helpCommands {
		view, ok := resolveCommand(name, registry)
		if !ok {
			t.Errorf("help lists :%s but the registry has no such command", name)
			continue
		}
		listed[view.Title()] = true
	}
	for name, build := range registry {
		if title := build().Title(); !listed[title] {
			t.Errorf("registry command :%s opens %s, which help never mentions", name, title)
		}
	}
	// Every advertised command is a row of the table, so it is discoverable
	// without the reader having to guess that a hint line exists.
	seen := strings.Join(helpScrolled(t, 100, 24), "\n")
	for _, name := range helpCommands {
		if !strings.Contains(seen, ":"+name) {
			t.Errorf("help does not draw a row for :%s", name)
		}
	}
}

// TestHelpDoesNotLeakIntoPrompts: "?" inside a prompt is text, not a binding.
func TestHelpDoesNotLeakIntoPrompts(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	next, _ = m.Update(key("?"))
	m = next.(Model)
	if len(m.stack) != 1 {
		t.Error("? inside a prompt is text, not a binding")
	}
	if m.input != "?" {
		t.Errorf("input = %q, want \"?\"", m.input)
	}
}
