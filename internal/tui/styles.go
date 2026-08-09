package tui

import (
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
