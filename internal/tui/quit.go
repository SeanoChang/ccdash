package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const quitDialogMaxWidth = 39

// overlayQuitConfirmation centers a compact modal over the current frame. The
// ANSI-aware cuts preserve the dashboard on either side without splitting a
// style sequence, grapheme, CJK character, or emoji.
func overlayQuitConfirmation(base string, width, height int) string {
	dialog := quitDialog(width, height)
	if len(dialog) == 0 || width <= 0 || height <= 0 {
		return base
	}

	lines := strings.Split(base, "\n")
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}

	dialogWidth := lipgloss.Width(dialog[0])
	left := max((width-dialogWidth)/2, 0)
	top := max((height-len(dialog))/2, 0)
	for i, modalLine := range dialog {
		row := top + i
		if row >= height {
			break
		}
		background := lines[row]
		right := min(left+dialogWidth, width)
		prefix := padANSIRight(ansi.Cut(background, 0, left), left)
		suffixWidth := width - right
		suffix := ansi.Cut(background, right, width)
		suffix = strings.Repeat(" ", max(suffixWidth-ansi.StringWidth(suffix), 0)) + suffix
		lines[row] = padANSIRight(prefix+modalLine+suffix, width)
	}
	return strings.Join(lines[:height], "\n")
}

// quitDialog returns a bordered four-row dialog when the viewport permits it,
// then degrades to a compact unbordered prompt at extreme terminal sizes.
func quitDialog(maxWidth, maxHeight int) []string {
	if maxWidth <= 0 || maxHeight <= 0 {
		return nil
	}
	dialogWidth := min(maxWidth, quitDialogMaxWidth)
	if maxHeight < 3 || dialogWidth < 16 {
		return compactQuitDialog(dialogWidth, maxHeight)
	}

	top := modalTop(dialogWidth, "Confirm quit")
	actions := centeredANSI(styleAccent.Render(quitActions(dialogWidth-2)), dialogWidth-2)
	bottom := styleBorder.Render("╰" + strings.Repeat("─", dialogWidth-2) + "╯")
	if maxHeight == 3 {
		return []string{
			top,
			styleBorder.Render("│") + actions + styleBorder.Render("│"),
			bottom,
		}
	}
	question := centeredANSI("Quit ccdash?", dialogWidth-2)
	return []string{
		top,
		styleBorder.Render("│") + question + styleBorder.Render("│"),
		styleBorder.Render("│") + actions + styleBorder.Render("│"),
		bottom,
	}
}

func modalTop(width int, title string) string {
	inside := width - 2
	label := styleWarning.Render(" " + title + " ")
	label = ansi.Truncate(label, inside, "")
	fill := strings.Repeat("─", max(inside-ansi.StringWidth(label), 0))
	return styleBorder.Render("╭") + label + styleBorder.Render(fill+"╮")
}

func quitActions(width int) string {
	for _, text := range []string{
		"[y] Quit   [Enter/n/Esc] Cancel",
		"[y] Quit   [n/Esc] Cancel",
		"y Quit · n No",
		"y/n",
		"y",
	} {
		if lipgloss.Width(text) <= width {
			return text
		}
	}
	return ""
}

func compactQuitDialog(width, height int) []string {
	line := func(text string) string {
		return centeredANSI(styleWarning.Render(text), width)
	}
	if height == 1 {
		for _, text := range []string{"Quit? y/n", "y/n", "y"} {
			if lipgloss.Width(text) <= width {
				return []string{line(text)}
			}
		}
	}
	return []string{
		line("Quit ccdash?"),
		centeredANSI(styleAccent.Render(quitActions(width)), width),
	}
}

func centeredANSI(text string, width int) string {
	text = ansi.Truncate(text, width, "")
	remaining := max(width-ansi.StringWidth(text), 0)
	left := remaining / 2
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", remaining-left)
}

func padANSIRight(text string, width int) string {
	text = ansi.Truncate(text, width, "")
	return text + strings.Repeat(" ", max(width-ansi.StringWidth(text), 0))
}
