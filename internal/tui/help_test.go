package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
	for _, size := range []struct{ w, h int }{{80, 24}, {200, 60}, {40, 10}, {177, 58}} {
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
