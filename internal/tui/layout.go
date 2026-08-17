package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/seanochang/ccdash/internal/render"
)

const (
	headerLines = 4
	footerLines = 1
	// borderLines is the body's two border rows: spec §4's row 4 and row h-2.
	borderLines = 2
	logoWidth   = 26
	minLogoRoom = 96
	// scopeLabelWidth caps the parenthesised scope in the body title, so a deep
	// project path cannot push the count off the border.
	scopeLabelWidth = 24
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
// terminal height: spec §4's h - 7, being h less the 4 header rows, the two
// body border rows and the footer. It never returns less than 1.
func bodyHeight(height int) int {
	body := height - headerLines - footerLines - borderLines
	if body < 1 {
		return 1
	}
	return body
}

// bodyWidth is the width available to the table: the terminal width less the
// two border columns. It never returns less than 0.
func bodyWidth(width int) int {
	interior := width - 2
	if interior < 0 {
		return 0
	}
	return interior
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

// bodyTitle formats the body border's title, k9s style: Resource(scope)[count]
// — Projects(all)[20], Requests(sess-4f2a)[312] (spec §4.2). A filter shows
// visible/total, so a partial match is never mistaken for a complete one, and a
// paginated view whose next page may exist marks its count "+", giving
// Requests(sess-4f2a)[7/500+] (spec §5.3). rendered is true for a view that
// paints its own body: a chart has no rows, so it carries no count at all.
func bodyTitle(resource, scope string, visible, total int, filtered, more, rendered bool) string {
	if scope == "" {
		scope = "all"
	}
	if rendered {
		return fmt.Sprintf("%s(%s)", resource, scope)
	}
	count := strconv.Itoa(total)
	if more {
		count += "+"
	}
	if filtered {
		count = strconv.Itoa(visible) + "/" + count
	}
	return fmt.Sprintf("%s(%s)[%s]", resource, scope, count)
}

// bodyPanel wraps the body in the titled border of spec §4.2. It returns
// exactly len(body)+borderLines lines of exactly width display cells; body
// lines are padded or trimmed to the interior width, so the table's first
// column lands one cell in — flush with the header's and footer's own margin.
func bodyPanel(title string, body []string, width int) []string {
	lines := make([]string, 0, len(body)+borderLines)
	lines = append(lines, borderTop(title, width))
	interior := bodyWidth(width)
	for _, line := range body {
		if width < 2 {
			lines = append(lines, padLine("", width))
			continue
		}
		lines = append(lines, styleBorder.Render("│")+padLine(line, interior)+
			styleBorder.Render("│"))
	}
	lines = append(lines, borderBottom(width))
	return lines
}

// borderTop draws "┌─ Title ─────┐" in exactly width cells. The title is cut,
// and below six cells dropped outright, before the border can overflow.
func borderTop(title string, width int) string {
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return styleBorder.Render("─")
	}
	// "┌─", a space either side of the title, one trailing dash and "┐".
	label := ""
	if room := width - 6; room >= 1 && title != "" {
		label = truncateDisplay(title, room)
	}
	if label == "" {
		return styleBorder.Render("┌" + strings.Repeat("─", width-2) + "┐")
	}
	fill := width - 5 - lipgloss.Width(label)
	return styleBorder.Render("┌─") + " " + styleAccent.Render(label) + " " +
		styleBorder.Render(strings.Repeat("─", fill)+"┐")
}

func borderBottom(width int) string {
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return styleBorder.Render("─")
	}
	return styleBorder.Render("└" + strings.Repeat("─", width-2) + "┘")
}

// scopeLabel names the current narrowing for the body title: the most specific
// drill-down if there is one, else the tool filter, else "all". A project is a
// path, so it is shortened on separators to keep its last segment readable.
func scopeLabel(scope Scope) string {
	for _, narrowing := range []string{scope.Session, scope.Agent, scope.Workflow,
		scope.Model} {
		if narrowing != "" {
			return truncateDisplay(narrowing, scopeLabelWidth)
		}
	}
	if scope.Project != "" {
		return render.TruncatePath(scope.Project, scopeLabelWidth)
	}
	if scope.Tool != "" {
		return string(scope.Tool)
	}
	return "all"
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
