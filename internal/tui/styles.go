package tui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/ui"
)

// withBorder converts a content size into the size a bordered Lip Gloss style
// needs to hold it.
//
// Lip Gloss v2 counts the border inside Width and Height; v1 counted only the
// content and let the border add its two cells on top. Layout code here works
// in content sizes — modalWidth is also what inner elements are measured
// against — so a bordered box asks for two cells more than the content it
// frames.
func withBorder(content int) int { return content + 2 }

// minStatusWrapWidth is the narrowest width renderStatusMessage will wrap to,
// so a terminal narrower than that breaks words instead of every message.
const minStatusWrapWidth = 24

// maxStatusFooterLines caps how much of the screen a wrapped status message may
// take. The footer borrows its rows from the content above it, so an unbounded
// message — a queue refusal naming two hundred nibs — would push the view it is
// about off the screen entirely.
func maxStatusFooterLines(height int) int { return max(1, height/3) }

// renderStatusMessage colors a footer status message by kind and wraps it to
// width, cut off after maxLines with an ellipsis.
//
// It wraps rather than letting the terminal do the cutting. The alt screen is a
// fixed cell grid and Bubbletea draws the frame with wrapping off, so every
// cell past the right edge is dropped with no ellipsis and nothing on screen to
// say anything is missing. A refusal is longer than a terminal is wide — the
// milestone queue guard's fills 229 columns of footer — and the half that gets
// dropped is the remedy, which is the half that says how to proceed.
//
// Lines are trimmed rather than padded out to width because callers append to
// the block: padding would push the list's update indicator and the detail
// view's help row past the edge this exists to stay inside.
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

// applyFilterStyles paints a list's filter input in the primary color.
//
// The prompt is set on both focus states because a list keeps showing the
// filter prompt after the input blurs, and an unstyled blurred prompt would
// change color the moment filtering stops taking keystrokes.
func applyFilterStyles(s *list.Styles) {
	prompt := lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	s.Filter.Focused.Prompt = prompt
	s.Filter.Blurred.Prompt = prompt
	s.Filter.Cursor.Color = ui.ColorPrimary
}

var (
	// List title style
	listTitleStyle = lipgloss.NewStyle().
			Foreground(ui.ColorPrimary).
			Bold(true)

	// Detail title style
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#fff")).
				Background(ui.ColorPrimary).
				Padding(0, 1)

	// Help text style
	helpStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	// Help key style
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(ui.ColorPrimary).
			Bold(true)
)
