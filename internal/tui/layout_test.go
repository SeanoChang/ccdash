package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func assertExactFrame(t *testing.T, out string, width, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Fatalf("frame has %d lines, want exactly %d", len(lines), height)
	}
	for i, line := range lines {
		if lipgloss.Width(line) != width {
			t.Errorf("line %d width = %d, want exactly %d: %q",
				i, lipgloss.Width(line), width, line)
		}
	}
}

func TestFrameIsExactlyViewportSized(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {200, 60}, {40, 10}, {177, 58}} {
		info := headerInfo{
			DBPath: "~/.local/share/ccdash/usage.db", Range: "all",
			Tool: "all", Tokens: "2.4B", Cost: "$2012.27 at API rates",
			Requests: "23,216", Unpriced: "9",
		}
		header := headerBlock(info, size.w)
		table := NewTable([]Column{{Title: "NAME"}, {Title: "COST", Align: AlignRight}})
		table.SetSize(size.w, bodyHeight(size.h))
		table.SetRows([]Row{textRow("a", "x", "$1")})
		out := frame(header, table.Render(), "<projects>", size.w, size.h)
		assertExactFrame(t, out, size.w, size.h)
	}
}

func TestFrameFillsEvenWithNoRows(t *testing.T) {
	header := headerBlock(headerInfo{Range: "all"}, 100)
	table := NewTable([]Column{{Title: "NAME"}})
	table.SetSize(100, bodyHeight(30))
	table.SetRows(nil)
	out := frame(header, table.Render(), "<projects>", 100, 30)
	assertExactFrame(t, out, 100, 30)
}

func TestBodyHeightLeavesRoomForChrome(t *testing.T) {
	// 4 header rows + 1 footer = 5 lines of chrome.
	if got := bodyHeight(30); got != 25 {
		t.Errorf("bodyHeight(30) = %d, want 25", got)
	}
	if got := bodyHeight(6); got < 1 {
		t.Errorf("bodyHeight(6) = %d, must stay at least 1", got)
	}
	if got := bodyHeight(3); got < 1 {
		t.Errorf("bodyHeight(3) = %d, must stay at least 1", got)
	}
}

func TestHeaderCollapsesOnShortTerminals(t *testing.T) {
	full := headerBlock(headerInfo{Range: "all"}, 120)
	if len(full) != 4 {
		t.Errorf("full header = %d lines, want 4", len(full))
	}
	for i, line := range full {
		if lipgloss.Width(line) != 120 {
			t.Errorf("header line %d width = %d, want 120", i, lipgloss.Width(line))
		}
	}
}

func TestHeaderDropsLogoWhenNarrow(t *testing.T) {
	narrow := headerBlock(headerInfo{Range: "all"}, 50)
	joined := strings.Join(narrow, "\n")
	if strings.Contains(joined, "╔") {
		t.Error("the logo must be dropped first under width pressure")
	}
	for i, line := range narrow {
		if lipgloss.Width(line) != 50 {
			t.Errorf("narrow header line %d width = %d, want 50", i, lipgloss.Width(line))
		}
	}
}
