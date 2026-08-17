package tui

import (
	"errors"
	"testing"
	"time"
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
