package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	headerLines = 4
	footerLines = 1
	logoWidth   = 26
	minLogoRoom = 96
)

// logo is four lines of exactly logoWidth cells. Never passed to Render as one
// multi-line string; each line is styled separately.
var logo = [headerLines]string{
	"╔═╗╔═╗╔╦╗╔═╗╔═╗╦ ╦     ",
	"║  ║  ║║╠═╣╚═╗╠═╣      ",
	"╚═╝╚═╝═╩╝╩ ╩╚═╝╩ ╩     ",
	"rev 0.1.0              ",
}

type headerInfo struct {
	DBPath   string
	Range    string
	Tool     string
	Tokens   string
	Cost     string
	Requests string
	Unpriced string
}

// bodyHeight is the number of lines available to the table, given the total
// terminal height. It never returns less than 1.
func bodyHeight(height int) int {
	body := height - headerLines - footerLines
	if body < 1 {
		return 1
	}
	return body
}

// padLine trims or pads a line to exactly width display cells.
func padLine(text string, width int) string {
	actual := lipgloss.Width(text)
	if actual > width {
		return truncateDisplay(text, width)
	}
	return text + strings.Repeat(" ", width-actual)
}

// headerBlock renders exactly headerLines lines of exactly width cells.
func headerBlock(info headerInfo, width int) []string {
	left := []string{
		fmt.Sprintf(" Context:  %s", info.DBPath),
		fmt.Sprintf(" Range:    %s", info.Range),
		fmt.Sprintf(" Tokens:   %-12s Cost: %s", info.Tokens, info.Cost),
		fmt.Sprintf(" Requests: %-12s Unpriced: %s", info.Requests, info.Unpriced),
	}
	keys := []string{
		"<1> all", "<2> claude", "<3> codex", "<?> help",
	}

	lines := make([]string, 0, headerLines)
	showLogo := width >= minLogoRoom
	for i := 0; i < headerLines; i++ {
		body := left[i]
		reserved := 0
		if showLogo {
			reserved = logoWidth
		}
		keyColumn := ""
		if width >= 70 {
			keyColumn = fmt.Sprintf("  %-11s", keys[i])
		}
		body = padLine(body, width-reserved-lipgloss.Width(keyColumn))
		line := body + styleDim.Render(keyColumn)
		if showLogo {
			line += styleAccent.Render(padLine(logo[i], logoWidth))
		}
		lines = append(lines, padLine(line, width))
	}
	return lines
}

// frame assembles the complete screen. The result is always exactly height
// lines of exactly width display cells — the invariant that spec §2.1's defect
// violated. Short input is padded; long input is trimmed.
func frame(header []string, body []string, footer string, width, height int) string {
	lines := make([]string, 0, height)
	for _, line := range header {
		if len(lines) >= height {
			break
		}
		lines = append(lines, padLine(line, width))
	}
	available := height - len(lines) - footerLines
	for i := 0; i < available; i++ {
		if i < len(body) {
			lines = append(lines, padLine(body[i], width))
		} else {
			lines = append(lines, strings.Repeat(" ", width))
		}
	}
	for len(lines) < height-footerLines {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) < height {
		lines = append(lines, padLine(footer, width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines[:height], "\n")
}
