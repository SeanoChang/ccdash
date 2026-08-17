package tui

import (
	"fmt"
	"strings"
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

// helpBody renders the overlay in place of the table body. It returns exactly
// height lines of exactly width display cells, so the frame invariant holds
// while the overlay is up; on a viewport too short for the whole table the tail
// is dropped rather than allowed to overflow the frame.
func helpBody(width, height int) []string {
	lines := make([]string, 0, height)
	add := func(text string) {
		if len(lines) < height {
			lines = append(lines, padLine(text, width))
		}
	}
	add(styleHeading.Render(" Keybindings"))
	add("")
	for _, binding := range helpBindings {
		add(fmt.Sprintf("  %-14s %-7s %s", binding.keys, binding.context, binding.action))
	}
	add("")
	add(styleDim.Render(helpCommandLine()))
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height]
}
