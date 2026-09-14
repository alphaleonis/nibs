package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/ui"
)

// pickerModalConfig configures renderPickerModal.
type pickerModalConfig struct {
	Title       string      // header when NibTitle is empty, e.g. "Select Status"
	NibTitle    string      // the nib's title
	NibID       string      // the nib's ID
	ListContent string      // the rendered list
	Description string      // optional description shown below list
	ExtraHelp   []helpEntry // additional help entries shown before esc/cancel
	Width       int         // screen width
	WidthPct    int         // modal width percentage (0 means 50)
	MaxWidth    int         // max modal width (0 means 60)
}

// pickerModalWidth returns the width inside a picker modal's border for a
// screen width. A zero widthPct or maxWidth selects 50% or 60 columns.
func pickerModalWidth(screenWidth, widthPct, maxWidth int) int {
	if widthPct == 0 {
		widthPct = 50
	}
	if maxWidth == 0 {
		maxWidth = 60
	}
	return max(40, min(maxWidth, screenWidth*widthPct/100))
}

// pickerModalHeight returns the rows a picker modal may take on a screen of the
// given height, for sizing its list. A zero heightPct or maxHeight selects 50%
// or 16 rows; the result is at least 10.
func pickerModalHeight(screenHeight, heightPct, maxHeight int) int {
	if heightPct == 0 {
		heightPct = 50
	}
	if maxHeight == 0 {
		maxHeight = 16
	}
	return max(10, min(maxHeight, screenHeight*heightPct/100))
}

// reservePickerDescription wraps the selected description and pads it to the
// height of the tallest wrapped description in all, so the modal's height does
// not change with the selection. The block is unstyled. Returns "" when every
// description is empty.
func reservePickerDescription(selected string, all []string, modalWidth int) string {
	// The list's width. It must not exceed renderPickerModal's content width,
	// modalWidth-2, or the block is re-wrapped there and the height changes.
	descWidth := modalWidth - 6
	style := lipgloss.NewStyle().Width(descWidth)

	maxLines := 0
	for _, d := range all {
		if d == "" {
			continue
		}
		if h := lipgloss.Height(style.Render(d)); h > maxLines {
			maxLines = h
		}
	}
	if maxLines == 0 {
		return ""
	}

	rendered := style.Render(selected)
	for h := lipgloss.Height(rendered); h < maxLines; h++ {
		rendered += "\n"
	}
	return rendered
}

// renderPickerModal renders a picker modal around cfg.ListContent.
func renderPickerModal(cfg pickerModalConfig) string {
	modalWidth := pickerModalWidth(cfg.Width, cfg.WidthPct, cfg.MaxWidth)

	nibTitle := cfg.NibTitle
	if nibTitle == "" {
		nibTitle = cfg.Title
	}
	header := lipgloss.NewStyle().Bold(true).Render(truncateTitle(nibTitle, modalWidth-4))

	subtitle := ui.Muted.Render(cfg.NibID)

	help := helpKeyStyle.Render("enter") + " " + helpStyle.Render("select") + "  " +
		helpKeyStyle.Render("/") + " " + helpStyle.Render("filter") + "  "
	for _, e := range cfg.ExtraHelp {
		help += helpKeyStyle.Render(e.Key) + " " + helpStyle.Render(e.Desc) + "  "
	}
	help += helpKeyStyle.Render("esc") + " " + helpStyle.Render("cancel")

	// reservePickerDescription's width depends on this border and padding.
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(0, 1).
		Width(withBorder(modalWidth))

	var parts []string
	parts = append(parts, header)
	if cfg.NibID != "" {
		parts = append(parts, subtitle)
	}
	parts = append(parts, "")
	parts = append(parts, cfg.ListContent)
	if cfg.Description != "" {
		parts = append(parts, "", cfg.Description)
	}
	parts = append(parts, "", help)
	content := strings.Join(parts, "\n")

	return border.Render(content)
}

// overlayModal draws modal centered over a dimmed, height-line copy of bgView.
func overlayModal(bgView, modal string, width, height int) string {
	bgLines := strings.Split(bgView, "\n")

	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	if len(bgLines) > height {
		bgLines = bgLines[:height]
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	for i, line := range bgLines {
		bgLines[i] = dimStyle.Render(stripAnsi(line))
	}

	modalLines := strings.Split(modal, "\n")
	modalHeight := len(modalLines)
	modalWidth := lipgloss.Width(modal)

	startY := (height - modalHeight) / 2
	startX := (width - modalWidth) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	for i, modalLine := range modalLines {
		bgY := startY + i
		if bgY >= 0 && bgY < len(bgLines) {
			bgLines[bgY] = overlayLine(bgLines[bgY], modalLine, startX, width)
		}
	}

	return strings.Join(bgLines, "\n")
}

// overlayLine draws modalLine over a dimmed bgLine, starting at column startX.
func overlayLine(bgLine, modalLine string, startX, maxWidth int) string {
	bgRunes := []rune(stripAnsi(bgLine))
	for len(bgRunes) < maxWidth {
		bgRunes = append(bgRunes, ' ')
	}

	prefix := string(bgRunes[:startX])
	modalWidth := lipgloss.Width(modalLine)
	suffixStart := startX + modalWidth
	suffix := ""
	if suffixStart < len(bgRunes) {
		suffix = string(bgRunes[suffixStart:])
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	return dimStyle.Render(prefix) + modalLine + dimStyle.Render(suffix)
}

// stripAnsi removes escape sequences, taking each to run from ESC through the
// next ASCII letter.
func stripAnsi(s string) string {
	result := strings.Builder{}
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
