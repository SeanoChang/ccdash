package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestSingleFlightDropsOverlappingTick(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(tickMsg{})
	m = next.(Model)
	if !m.inFlight {
		t.Fatal("a tick must mark a refresh in flight")
	}
	if cmd == nil {
		t.Fatal("the first tick must start work")
	}
	before := m.inFlight
	next, second := m.Update(tickMsg{})
	m = next.(Model)
	if !before || !m.inFlight {
		t.Error("state should stay in flight")
	}
	if second != nil {
		t.Error("a tick arriving while a refresh is running must be dropped, not queued")
	}
}

func TestRefreshedClearsInFlightAndStamps(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	next, _ = m.Update(refreshedMsg{at: time.Unix(5000, 0)})
	m = next.(Model)
	if m.inFlight {
		t.Error("completing a refresh must clear the in-flight flag")
	}
	if m.lastRefresh.Unix() != 5000 {
		t.Errorf("lastRefresh = %d, want 5000", m.lastRefresh.Unix())
	}
}

func TestRefreshErrorKeepsLastGoodDataAndTicking(t *testing.T) {
	m := newTestModel()
	rowsBefore := m.current().table.TotalCount()
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	next, cmd := m.Update(refreshedMsg{at: time.Unix(1, 0), err: errors.New("disk on fire")})
	m = next.(Model)
	if m.refreshErr == nil {
		t.Fatal("the error must be recorded")
	}
	if m.current().table.TotalCount() != rowsBefore {
		t.Error("a failed refresh must leave the last good rows on screen")
	}
	if m.inFlight {
		t.Error("a failed refresh must clear the in-flight flag so ticking recovers")
	}
	if cmd == nil {
		t.Error("the ticker must keep running after a failure so it can self-heal")
	}
}

func TestPromptPausesTicking(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(key(":"))
	m = next.(Model)
	next, cmd := m.Update(tickMsg{})
	m = next.(Model)
	if m.inFlight {
		t.Error("no refresh may start while a prompt is open")
	}
	if cmd == nil {
		t.Error("the ticker must still reschedule itself while paused")
	}
}

func TestRefreshAgeText(t *testing.T) {
	m := newTestModel()
	m.lastRefresh = time.Now().Add(-3 * time.Second)
	if got := m.refreshAge(); got != "3s ago" {
		t.Errorf("refreshAge = %q, want 3s ago", got)
	}
	m.lastRefresh = time.Time{}
	if got := m.refreshAge(); got != "never" {
		t.Errorf("refreshAge with no refresh = %q, want never", got)
	}
}

// TestStaleRefreshAgeIsColoured covers spec §4.3: "Refresh age turns amber past
// 30 seconds and red past 5 minutes, so a wedged ticker is visible rather than
// silent." The escape sequences have to reach the frame, not just the text.
func TestStaleRefreshAgeIsColoured(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	defer lipgloss.SetColorProfile(saved)

	m := newTestModel()
	m.lastRefresh = time.Now().Add(-3 * time.Second)
	fresh := m.footer()
	if ansiPattern.MatchString(fresh) {
		t.Errorf("a fresh age must stay plain: %q", fresh)
	}

	m.lastRefresh = time.Now().Add(-45 * time.Second)
	stale := m.footer()
	if !ansiPattern.MatchString(stale) {
		t.Errorf("a 45s age must emit colour, got no escapes: %q", stale)
	}
	if !strings.Contains(stale, styleWarning.Render("45s ago")) {
		t.Errorf("a 45s age must be amber: %q", stale)
	}

	m.lastRefresh = time.Now().Add(-10 * time.Minute)
	dead := m.footer()
	if !strings.Contains(dead, styleDanger.Render("10m ago")) {
		t.Errorf("a 10m age must be red: %q", dead)
	}

	for _, line := range []string{fresh, stale, dead} {
		if lipgloss.Width(line) != m.width {
			t.Errorf("footer width = %d, want %d: %q", lipgloss.Width(line), m.width, line)
		}
	}
}

// TestRefreshErrorKeepsTheAgeVisible covers spec §8: a failed refresh reports
// itself without hiding how stale the data now is.
func TestRefreshErrorKeepsTheAgeVisible(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	defer lipgloss.SetColorProfile(saved)

	m := newTestModel()
	m.lastRefresh = time.Now().Add(-45 * time.Second)
	m.refreshErr = errors.New("database is locked")
	out := m.footer()
	if !strings.Contains(out, "45s ago") {
		t.Errorf("a refresh error must not swallow the age: %q", out)
	}
	if !strings.Contains(out, styleDanger.Render("45s ago")) {
		t.Errorf("the age beside a refresh error must be red: %q", out)
	}
	if !strings.Contains(out, "database is locked") {
		t.Errorf("the error itself must still be reported: %q", out)
	}
	if lipgloss.Width(out) != m.width {
		t.Errorf("footer width = %d, want %d: %q", lipgloss.Width(out), m.width, out)
	}
}
