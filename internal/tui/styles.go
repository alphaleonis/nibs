package tui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/ui"
)

// withBorder converts a content size into the Width or Height a bordered Lip
// Gloss style needs: Lip Gloss v2 counts the border inside both.
func withBorder(content int) int { return content + 2 }

// minStatusWrapWidth is the narrowest width renderStatusMessage wraps to,
// however narrow the terminal.
const minStatusWrapWidth = 24

// clipToWidth cuts every line of s to width display cells. Zero or less means
// no terminal size is known yet, and nothing is clipped.
//
// Blocks stacked around s are measured against its width, so it must not claim
// more than the terminal holds.
func clipToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// maxStatusFooterLines caps a wrapped status message at a third of the screen,
// so the view the message is about stays visible.
func maxStatusFooterLines(height int) int { return max(1, height/3) }

// renderStatusMessage colors a footer status message by kind and wraps it to
// width, cut off after maxLines with an ellipsis. It wraps so that a refusal
// wider than the terminal keeps its remedy, at the end, on screen.
//
// Lines are trimmed, not padded to width: the list footer appends its update
// indicator to the last line.
func renderStatusMessage(msg string, kind statusKind, width, maxLines int) string {
	wrapWidth := max(minStatusWrapWidth, width)
	lines := strings.Split(lipgloss.NewStyle().Width(wrapWidth).Render(msg), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		last := len(lines) - 1
		lines[last] = lipgloss.NewStyle().MaxWidth(wrapWidth-1).Render(lines[last]) + "…"
	}

	color := ui.ColorSuccess
	if kind == statusWarn {
		color = ui.ColorWarning
	}
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}

// statusBlock renders a footer status message in at most maxLines rows, or ""
// when there is none.
func statusBlock(msg string, kind statusKind, width, maxLines int) string {
	if msg == "" {
		return ""
	}
	return renderStatusMessage(msg, kind, width, maxLines)
}

// blockLines is lipgloss.Height, except that an absent block takes zero rows
// (lipgloss.Height("") is 1).
func blockLines(block string) int {
	if block == "" {
		return 0
	}
	return lipgloss.Height(block)
}

// helpRowBudget is how many rows the expanded help panel may take on a
// height-row terminal after holdBack rows for the view above and statusLines
// rows for the status block.
//
// holdBack is that view's floor: the fewest rows it occupies at any height,
// which differs between the list box and the detail header. Zero or less means
// no room for the panel; the status message keeps its rows.
func helpRowBudget(height, holdBack, statusLines int) int {
	return max(1, height-holdBack) - statusLines
}

// applyFilterStyles paints a list's filter prompt and cursor in the primary color.
func applyFilterStyles(s *list.Styles) {
	prompt := lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	s.Filter.Focused.Prompt = prompt
	s.Filter.Blurred.Prompt = prompt
	s.Filter.Cursor.Color = ui.ColorPrimary
}

var (
	listTitleStyle = lipgloss.NewStyle().
			Foreground(ui.ColorPrimary).
			Bold(true)

	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#fff")).
				Background(ui.ColorPrimary).
				Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	// Marks the keybindings the help panel's row budget could not hold.
	helpHiddenStyle = lipgloss.NewStyle().
			Foreground(ui.ColorWarning).
			Italic(true)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(ui.ColorPrimary).
			Bold(true)
)
