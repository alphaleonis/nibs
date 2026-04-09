package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/ui"
)

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
