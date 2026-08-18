package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func openQuitConfirmation(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(key("q"))
	m = next.(Model)
	if isQuit(cmd) {
		t.Fatal("q quit without confirmation")
	}
	if m.mode != modeQuit {
		t.Fatalf("mode after q = %v, want modeQuit", m.mode)
	}
	return m
}

func TestQuitConfirmationIsRenderedOverTheCurrentFrame(t *testing.T) {
	m := newTestModel()
	before := strings.Split(stripANSI(m.View()), "\n")
	m = openQuitConfirmation(t, m)
	after := strings.Split(stripANSI(m.View()), "\n")

	assertExactFrame(t, m.View(), m.width, m.height)
	joined := strings.Join(after, "\n")
	for _, want := range []string{"Confirm quit", "Quit ccdash?", "[y] Quit", "Cancel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("quit confirmation is missing %q:\n%s", want, joined)
		}
	}
	if after[0] != before[0] || after[len(after)-1] != before[len(before)-1] {
		t.Error("the confirmation must overlay the current frame, not replace it")
	}
}

func TestQuitConfirmationAcceptsY(t *testing.T) {
	for _, press := range []string{"y", "Y"} {
		t.Run(press, func(t *testing.T) {
			m := openQuitConfirmation(t, newTestModel())
			_, cmd := m.Update(key(press))
			if !isQuit(cmd) {
				t.Errorf("%q in quit confirmation must quit", press)
			}
		})
	}
}

func TestQuitConfirmationCancelsSafely(t *testing.T) {
	for _, press := range []string{"n", "N", "esc", "enter"} {
		t.Run(press, func(t *testing.T) {
			m := openQuitConfirmation(t, newTestModel())
			next, cmd := m.Update(key(press))
			m = next.(Model)
			if isQuit(cmd) {
				t.Errorf("%q in quit confirmation must cancel, not quit", press)
			}
			if m.mode != modeNormal {
				t.Errorf("mode after cancelling with %q = %v, want modeNormal", press, m.mode)
			}
			if strings.Contains(stripANSI(m.View()), "Confirm quit") {
				t.Error("the confirmation remained visible after cancellation")
			}
		})
	}
}

func TestQuitConfirmationTrapsUnrelatedKeys(t *testing.T) {
	m := newTestModel()
	selected := m.current().table.selected
	m = openQuitConfirmation(t, m)
	next, cmd := m.Update(key("j"))
	m = next.(Model)
	if isQuit(cmd) {
		t.Error("an unrelated key must not quit")
	}
	if m.mode != modeQuit {
		t.Error("an unrelated key closed the quit confirmation")
	}
	if m.current().table.selected != selected {
		t.Error("an unrelated key acted on the table behind the modal")
	}
}

func TestQuitConfirmationPausesRefreshWork(t *testing.T) {
	m := openQuitConfirmation(t, newTestModel())
	next, cmd := m.Update(tickMsg{})
	m = next.(Model)
	if m.inFlight {
		t.Error("the quit confirmation must not start refresh work behind the modal")
	}
	if cmd == nil {
		t.Error("the refresh ticker must remain scheduled after the modal closes")
	}
}

func TestCtrlCStillQuitsImmediatelyFromConfirmation(t *testing.T) {
	m := openQuitConfirmation(t, newTestModel())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Error("ctrl+c must remain an immediate exit from the confirmation")
	}
}

func TestQuitCommandOpensConfirmation(t *testing.T) {
	for _, command := range []string{"q", "quit"} {
		t.Run(command, func(t *testing.T) {
			m := newTestModel()
			next, _ := m.Update(key(":"))
			m = next.(Model)
			for _, r := range command {
				next, _ = m.Update(key(string(r)))
				m = next.(Model)
			}
			next, cmd := m.Update(key("enter"))
			m = next.(Model)
			if isQuit(cmd) {
				t.Fatal("quit command bypassed confirmation")
			}
			if m.mode != modeQuit {
				t.Errorf("mode after :%s = %v, want modeQuit", command, m.mode)
			}
		})
	}
}

func TestQuitConfirmationFitsEveryViewport(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{100, 24}, {80, 24}, {40, 10}, {12, 3}, {8, 2}, {1, 1},
	} {
		m := newTestModel()
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m = next.(Model)
		m = openQuitConfirmation(t, m)
		assertExactFrame(t, m.View(), size.width, size.height)
	}
}

func TestQuitConfirmationWidthHoldsWithColorAndWideBackground(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	defer lipgloss.SetColorProfile(saved)

	const width, height = 61, 7
	background := strings.Repeat("界", 30) + " "
	lines := make([]string, height)
	for i := range lines {
		lines[i] = background
	}
	out := overlayQuitConfirmation(strings.Join(lines, "\n"), width, height)
	assertExactFrame(t, out, width, height)
	if !strings.Contains(stripANSI(out), "Confirm quit") {
		t.Error("the ANSI-aware overlay lost the dialog")
	}
}
