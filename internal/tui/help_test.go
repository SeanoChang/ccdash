package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpDocuments reports whether out documents this binding: some line carries
// its keys, then its context, then its action — the action possibly cut short
// with an ellipsis where the column was too narrow to hold all of it.
func helpDocuments(out string, binding helpBinding) bool {
	pattern := regexp.MustCompile(regexp.QuoteMeta(binding.keys) + ` +` +
		regexp.QuoteMeta(binding.context) + ` +(.*)`)
	for _, line := range strings.Split(stripANSI(out), "\n") {
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		rest := match[1]
		if strings.HasPrefix(rest, binding.action) {
			return true
		}
		// A squeezed column shows a prefix of the action and marks the cut.
		if cut := strings.Index(rest, "…"); cut > 0 &&
			strings.HasPrefix(binding.action, rest[:cut]) {
			return true
		}
	}
	return false
}

// openHelpAt returns the full frame with the overlay up at this viewport size.
func openHelpAt(t *testing.T, width, height int) string {
	t.Helper()
	m := newTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(Model)
	next, _ = m.Update(key("?"))
	m = next.(Model)
	out := m.View()
	assertExactFrame(t, out, width, height)
	return out
}

// TestHelpOverlayDocumentsEveryBindingAtRealWidths is the point of the columns:
// at any width a terminal is actually run at, the overlay shows the whole
// keymap. A single column needs one row per binding and so silently loses its
// tail — including both ways to quit — on anything shorter than 24 rows.
func TestHelpOverlayDocumentsEveryBindingAtRealWidths(t *testing.T) {
	for _, size := range []struct{ w, h int }{{177, 58}, {100, 20}, {80, 24}, {200, 60}} {
		out := openHelpAt(t, size.w, size.h)
		for _, binding := range helpBindings {
			if !helpDocuments(out, binding) {
				t.Errorf("%dx%d: overlay does not document %q %q %q",
					size.w, size.h, binding.keys, binding.context, binding.action)
			}
		}
		if strings.Contains(stripANSI(out), " more") {
			t.Errorf("%dx%d: overlay claims bindings are omitted, but they all fit",
				size.w, size.h)
		}
	}
}

// TestHelpOverlayKeepsQuitAndOwnsUpWhenCramped pins the two rules that make
// truncation honest: the ways out are never the rows that get dropped, and
// whatever is dropped is counted on screen rather than vanishing.
func TestHelpOverlayKeepsQuitAndOwnsUpWhenCramped(t *testing.T) {
	out := stripANSI(openHelpAt(t, 40, 10))
	for _, binding := range helpBindings {
		if binding.action != "Quit" {
			continue
		}
		if !helpDocuments(out, binding) {
			t.Errorf("a cramped overlay dropped %q → Quit, the one row a stuck "+
				"reader opened it for", binding.keys)
		}
	}
	missing := 0
	for _, binding := range helpBindings {
		if !helpDocuments(out, binding) {
			missing++
		}
	}
	if missing == 0 {
		t.Fatal("40x10 was chosen because it cannot hold every binding; it now can, " +
			"so this test no longer exercises truncation")
	}
	if want := fmt.Sprintf("… %d more", missing); !strings.Contains(out, want) {
		t.Errorf("overlay omits %d bindings without saying so; want %q", missing, want)
	}
}

// TestHelpOverlayFillsColumnsTopToBottom checks the grid is column-major: the
// binding after the first sits below it, not beside it, so each column reads
// down like the single-column list it replaced.
func TestHelpOverlayFillsColumnsTopToBottom(t *testing.T) {
	lines := helpBody(bodyWidth(177), bodyHeight(58))
	widest := 0
	for _, line := range lines {
		perLine := 0
		for _, binding := range helpBindings {
			if helpDocuments(line, binding) {
				perLine++
			}
		}
		if perLine > widest {
			widest = perLine
		}
		if helpDocuments(line, helpBindings[0]) && helpDocuments(line, helpBindings[1]) {
			t.Error("the grid is filled row-major: binding 2 sits beside binding 1, " +
				"so no column reads top to bottom")
		}
	}
	if widest < 2 {
		t.Errorf("177 cells afford several columns; the widest row holds %d binding(s)",
			widest)
	}
}

func TestQuestionMarkOpensTheHelpOverlay(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(key("?"))
	m = next.(Model)
	if cmd != nil {
		t.Error("? must not emit a command")
	}
	if !m.showHelp {
		t.Fatal("? must open the help overlay — both the header and the footer advertise it")
	}
	out := m.View()
	if !strings.Contains(out, "Keybindings") {
		t.Error("the help overlay must be rendered as the body")
	}
	// The overlay replaces the table, so table content must be gone.
	if strings.Contains(out, "alpha") {
		t.Error("the help overlay must replace the table body, not sit beside it")
	}
}

// TestHelpOverlayListsEveryBinding pins the overlay to spec §5.5: every key the
// application binds has to appear, or the overlay is lying about the keymap.
func TestHelpOverlayListsEveryBinding(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("?"))
	m = next.(Model)
	out := m.View()
	for _, want := range []string{
		"j", "k", "ctrl-f", "ctrl-b", "g", "G", "enter", "esc", "s", "S",
		"/", ":", "r", "1 2 3", "d w m a", "?", "q", "ctrl-c", ":q",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help overlay does not document %q", want)
		}
	}
	for _, want := range []string{"Quit", "Move selection one row", "Open the filter prompt"} {
		if !strings.Contains(out, want) {
			t.Errorf("help overlay is missing the action text %q", want)
		}
	}
}

func TestAnyKeyDismissesTheHelpOverlay(t *testing.T) {
	for _, dismiss := range []string{"j", "?", "q", "x", "enter", "esc"} {
		m := newTestModel()
		next, _ := m.Update(key("?"))
		m = next.(Model)
		next, cmd := m.Update(key(dismiss))
		m = next.(Model)
		if m.showHelp {
			t.Errorf("%q must dismiss the help overlay", dismiss)
		}
		if isQuit(cmd) {
			t.Errorf("%q dismisses the overlay; it must not also quit", dismiss)
		}
		// The dismissing key is swallowed: it must not act on the table under
		// the overlay.
		if m.current().table.selected != 0 {
			t.Errorf("%q moved the selection while dismissing the overlay", dismiss)
		}
		if len(m.stack) != 1 {
			t.Errorf("%q navigated while dismissing the overlay", dismiss)
		}
	}
}

func TestCtrlCQuitsWithTheHelpOverlayUp(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("?"))
	m = next.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Error("ctrl+c must quit even with the help overlay up")
	}
}

func TestHelpOverlayKeepsTheFrameExact(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{80, 24}, {200, 60}, {40, 10}, {177, 58}, {100, 20}, {30, 8},
	} {
		m := newTestModel()
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m = next.(Model)
		next, _ = m.Update(key("?"))
		m = next.(Model)
		assertExactFrame(t, m.View(), size.w, size.h)
	}
}

// TestHelpCommandsMatchRegistry catches drift in both directions: a command the
// overlay advertises but the registry cannot resolve, and a view added to the
// registry that the overlay never mentions.
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
			t.Errorf("registry command :%s opens %s, which the help overlay never mentions",
				name, title)
		}
	}
}

func TestHelpBodyIsExactlySized(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 19}, {40, 1}, {200, 55}} {
		lines := helpBody(size.w, size.h)
		if len(lines) != size.h {
			t.Fatalf("helpBody(%d,%d) = %d lines, want %d",
				size.w, size.h, len(lines), size.h)
		}
	}
}

// key() only knows a few named keys; the overlay test above uses "x" as an
// arbitrary unbound rune, which must still dismiss.
func TestHelpOverlayDoesNotLeakIntoPrompts(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key("/"))
	m = next.(Model)
	next, _ = m.Update(key("?"))
	m = next.(Model)
	if m.showHelp {
		t.Error("? inside a prompt is text, not a binding")
	}
	if m.input != "?" {
		t.Errorf("input = %q, want \"?\"", m.input)
	}
}

// helpKeysOnLine reports the key strings this line draws as bindings. A row
// draws one when its keys stand as a whole field — at the row's start or after
// a column gutter, and followed by padding or the row's end — which is what
// keeps the "/" inside "Page down / up", the ":" inside ":projects" and the "r"
// inside "Manual refresh now" from being counted as bindings.
func helpKeysOnLine(line string) []string {
	plain := stripANSI(line)
	drawn, seen := []string{}, map[string]bool{}
	for _, binding := range helpBindings {
		if seen[binding.keys] {
			continue
		}
		pattern := regexp.MustCompile(`(?:^|\s{2,})` +
			regexp.QuoteMeta(binding.keys) + `(?:\s|$)`)
		if pattern.MatchString(plain) {
			seen[binding.keys] = true
			drawn = append(drawn, binding.keys)
		}
	}
	return drawn
}

// helpKeysDrawn is every key string the body draws, plus the index of the first
// line that draws any: the row the ways out have to be on once the overlay is
// dropping bindings. The two "esc" rows share a key string, so a complete
// overlay draws helpKeyStrings() of them, not len(helpBindings).
func helpKeysDrawn(body []string) (map[string]bool, int) {
	drawn, first := map[string]bool{}, -1
	for i, line := range body {
		keys := helpKeysOnLine(line)
		if len(keys) > 0 && first < 0 {
			first = i
		}
		for _, k := range keys {
			drawn[k] = true
		}
	}
	return drawn, first
}

// helpKeyStrings is how many distinct key strings the keymap has.
func helpKeyStrings() int {
	seen := map[string]bool{}
	for _, binding := range helpBindings {
		seen[binding.keys] = true
	}
	return len(seen)
}

// helpOwnsUp reports whether this text counts the bindings it is not showing,
// in either the roomy form or the folded one.
func helpOwnsUp(text string) bool {
	plain := stripANSI(text)
	return regexp.MustCompile(`… \d+ more`).MatchString(plain) ||
		regexp.MustCompile(`\+\d+`).MatchString(plain)
}

func helpDump(body []string) string {
	shown := make([]string, 0, len(body))
	for _, line := range body {
		shown = append(shown, "|"+stripANSI(line)+"|")
	}
	return strings.Join(shown, "\n")
}

// helpCrampedSizes are the viewports an adversarial pty sweep caught the
// columned overlay failing at, written rows first the way a terminal reports
// them. Every one of them showed either no binding at all or, at 8x30, one of
// the two ways out.
var helpCrampedSizes = []struct {
	rows, cols int
	// rowStarved marks a viewport too short for the grid its width could
	// otherwise carry. There the heading and the command hint have to be gone:
	// they are the first cells sacrificed, never the last.
	rowStarved bool
	// oneRow marks a viewport whose body is a single line, so the count of what
	// was dropped has nowhere to go but onto that same line.
	oneRow bool
}{
	{rows: 60, cols: 20},
	{rows: 40, cols: 16},
	{rows: 24, cols: 20},
	{rows: 8, cols: 30, rowStarved: true},
	{rows: 7, cols: 120, rowStarved: true},
	{rows: 6, cols: 80, rowStarved: true},
	{rows: 5, cols: 177, rowStarved: true, oneRow: true},
	{rows: 4, cols: 120, rowStarved: true, oneRow: true},
	{rows: 3, cols: 20, rowStarved: true, oneRow: true},
}

// TestHelpOverlayKeepsTheKeysWhenCramped is the whole point of the overlay at a
// size that cannot hold it: cells are scarce, so the payload survives and the
// furniture goes, never the reverse. At every one of these viewports the body
// has at least one row, so it must carry at least one binding — the way out
// first — and must own up to whatever it could not draw.
func TestHelpOverlayKeepsTheKeysWhenCramped(t *testing.T) {
	for _, size := range helpCrampedSizes {
		t.Run(fmt.Sprintf("%dx%d", size.rows, size.cols), func(t *testing.T) {
			width, height := bodyWidth(size.cols), bodyHeight(size.rows)
			body := helpBody(width, height)
			if len(body) != height {
				t.Fatalf("helpBody(%d,%d) drew %d lines, want exactly %d",
					width, height, len(body), height)
			}
			for i, line := range body {
				if got := lipgloss.Width(line); got != width {
					t.Errorf("line %d is %d cells wide, want exactly %d: %q",
						i, got, width, stripANSI(line))
				}
			}
			drawn, first := helpKeysDrawn(body)
			if len(drawn) == 0 {
				t.Fatalf("%d body rows of %d cells and not one binding drawn:\n%s",
					height, width, helpDump(body))
			}
			if !drawn["q ctrl-c"] {
				t.Errorf("the overlay drops \"q ctrl-c\" — the row a stuck reader "+
					"opened it for — while drawing %v:\n%s", drawn, helpDump(body))
			}
			if len(drawn) > 1 && !drawn[":q"] {
				t.Errorf("%d bindings drawn and \":q\" is not among them; both ways "+
					"out outrank every other binding:\n%s", len(drawn), helpDump(body))
			}
			if len(drawn) < helpKeyStrings() {
				if first >= 0 && !contains(helpKeysOnLine(body[first]), "q ctrl-c") {
					t.Errorf("bindings are being dropped, so \"q ctrl-c\" must lead; "+
						"the first row of bindings is %q:\n%s",
						stripANSI(body[first]), helpDump(body))
				}
				if !helpOwnsUp(strings.Join(body, "\n")) {
					t.Errorf("%d of %d key rows drawn and the overlay says nothing "+
						"about the rest:\n%s", len(drawn), helpKeyStrings(), helpDump(body))
				}
			}
			if size.oneRow && len(drawn) < helpKeyStrings() &&
				first >= 0 && !helpOwnsUp(body[first]) {
				t.Errorf("the body is one row, so the count of what was dropped has "+
					"to ride that row: %q", stripANSI(body[first]))
			}
			if size.rowStarved && strings.Contains(stripANSI(strings.Join(body, "\n")),
				"Keybindings") {
				t.Errorf("a viewport too short for the keymap spends a row on the "+
					"heading instead:\n%s", helpDump(body))
			}
		})
	}
}

// contains is strings.Contains for a slice; only the tests need it.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestHelpOverlayFrameHoldsAtCrampedSizes runs the same viewports through the
// real model: the frame stays exact, and wherever the frame has room for a body
// row the reader can see a way out in it.
func TestHelpOverlayFrameHoldsAtCrampedSizes(t *testing.T) {
	for _, size := range helpCrampedSizes {
		t.Run(fmt.Sprintf("%dx%d", size.rows, size.cols), func(t *testing.T) {
			out := openHelpAt(t, size.cols, size.rows)
			// The frame gives the panel what is left after the header and the
			// footer; the first of those rows is the border, the rest are body.
			// The frame invariant this test is named for holds at EVERY size,
			// including those with no room for a body row. Asserting it before
			// any bail-out is the point: an early return let the size table
			// claim coverage it never delivered.
			assertExactFrame(t, out, size.cols, size.rows)

			visible := size.rows - headerHeight(size.rows) - footerLines - 1
			if visible < 1 {
				t.Skipf("no body row survives the frame at %dx%d; frame exactness asserted above", size.rows, size.cols)
			}
			if !strings.Contains(stripANSI(out), "q ctrl-c") {
				t.Errorf("%d body rows on screen and no way out in them:\n%s",
					visible, stripANSI(out))
			}
		})
	}
}
