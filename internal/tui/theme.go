package tui

import "github.com/charmbracelet/lipgloss"

// RULE: never pass a string containing "\n" to any lipgloss Render().
// lipgloss pads every line of a multi-line block to the width of its widest
// line, so a trailing newline emits a run of spaces that silently indents
// whatever is written next. Style the text; emit newlines outside.
// Enforced by TestNoNewlinesInsideStyledRender.

var (
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleWarning  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleDanger   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleHeading  = lipgloss.NewStyle().Bold(true)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("39"))
	styleColumn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	styleBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	stylePrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
)

const trackRune = '·'
