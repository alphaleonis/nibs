package tui

import (
	"strings"

	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// pickerModalConfig holds configuration for rendering a picker modal
type pickerModalConfig struct {
	Title       string      // e.g., "Select Status"
	NibTitle    string      // the nib's title
	NibID       string      // the nib's ID
	ListContent string      // the rendered list
	Description string      // optional description shown below list
	ExtraHelp   []helpEntry // additional help entries shown before esc/cancel
	Width       int         // screen width
	WidthPct    int         // modal width percentage (default 50)
	MaxWidth    int         // max modal width (default 60)
}

// pickerModalWidth computes a picker modal's outer width from the screen width,
// honoring the width-percentage / max-width settings (0 selects the defaults of
// 50% / 60 columns). Pickers use it so their description-reservation math is
// measured against the width the modal is actually rendered at.
func pickerModalWidth(screenWidth, widthPct, maxWidth int) int {
	if widthPct == 0 {
		widthPct = 50
	}
	if maxWidth == 0 {
		maxWidth = 60
	}
	return max(40, min(maxWidth, screenWidth*widthPct/100))
}

// pickerModalHeight computes a picker modal's outer height from the screen
// height, honoring the height-percentage / max-height settings (0 selects the
// defaults of 50% / 16 rows, floored at 10). List-based pickers use it to size
// the embedded bubbles/list so the whole modal stays within the screen. It is
// the height sibling of pickerModalWidth — keep the two in step.
func pickerModalHeight(screenHeight, heightPct, maxHeight int) int {
	if heightPct == 0 {
		heightPct = 50
	}
	if maxHeight == 0 {
		maxHeight = 16
	}
	return max(10, min(maxHeight, screenHeight*heightPct/100))
}

// reservePickerDescription wraps the selected description to the modal's content
// width and pads it with blank lines to the tallest wrapped height among all the
// picker's descriptions. This keeps a picker modal's total height constant no
// matter which item is selected — otherwise landing on an item with a longer
// (more-wrapped) description grows the modal and makes the list items jump. The
// returned block is unstyled so callers can apply their own styling. Returns ""
// when there are no non-empty descriptions.
func reservePickerDescription(selected string, all []string, modalWidth int) string {
	// modalWidth-6 matches the list's content width (border + padding + a small
	// right margin), so the description aligns under the list items and the
	// pre-wrapped block fits inside the modal without being re-wrapped. The -6
	// must stay within renderPickerModal's chrome budget (its Border + Padding(0,1)
	// on Width(modalWidth)) — if that padding/border ever widens, this must track
	// it or the pre-wrapped block gets re-wrapped and the height-jump bug returns.
	// See the matching note at renderPickerModal's border block.
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

// renderPickerModal renders a standard picker modal with consistent styling
func renderPickerModal(cfg pickerModalConfig) string {
	modalWidth := pickerModalWidth(cfg.Width, cfg.WidthPct, cfg.MaxWidth)

	// Header with nib title (or modal title as fallback)
	titleWidth := modalWidth - 4
	nibTitle := cfg.NibTitle
	if nibTitle == "" {
		nibTitle = cfg.Title
	}
	if len(nibTitle) > titleWidth {
		nibTitle = nibTitle[:titleWidth-3] + "..."
	}
	header := lipgloss.NewStyle().Bold(true).Render(nibTitle)

	// Subtitle with nib ID
	subtitle := ui.Muted.Render(cfg.NibID)

	// Help footer
	help := helpKeyStyle.Render("enter") + " " + helpStyle.Render("select") + "  " +
		helpKeyStyle.Render("/") + " " + helpStyle.Render("filter") + "  "
	for _, e := range cfg.ExtraHelp {
		help += helpKeyStyle.Render(e.Key) + " " + helpStyle.Render(e.Desc) + "  "
	}
	help += helpKeyStyle.Render("esc") + " " + helpStyle.Render("cancel")

	// Border style. The Border + Padding(0,1) here is the chrome budget that
	// reservePickerDescription's descWidth (modalWidth-6) is measured against;
	// keep the two in sync so pre-wrapped descriptions aren't re-wrapped inside
	// the border (which would reintroduce the picker height-jump bug).
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(0, 1).
		Width(modalWidth)

	// Assemble content
	var parts []string
	parts = append(parts, header)
	if cfg.NibID != "" {
		parts = append(parts, subtitle)
	}
	parts = append(parts, "") // blank separator
	parts = append(parts, cfg.ListContent)
	if cfg.Description != "" {
		parts = append(parts, "", cfg.Description)
	}
	parts = append(parts, "", help)
	content := strings.Join(parts, "\n")

	return border.Render(content)
}

// overlayModal places a modal on top of a background view
func overlayModal(bgView, modal string, width, height int) string {
	// Split background into lines
	bgLines := strings.Split(bgView, "\n")

	// Pad or truncate background to fill the screen
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	if len(bgLines) > height {
		bgLines = bgLines[:height]
	}

	// Dim the background
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555"))
	for i, line := range bgLines {
		bgLines[i] = dimStyle.Render(stripAnsi(line))
	}

	// Split modal into lines
	modalLines := strings.Split(modal, "\n")
	modalHeight := len(modalLines)
	modalWidth := lipgloss.Width(modal)

	// Calculate center position
	startY := (height - modalHeight) / 2
	startX := (width - modalWidth) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	// Overlay modal onto background
	for i, modalLine := range modalLines {
		bgY := startY + i
		if bgY >= 0 && bgY < len(bgLines) {
			bgLines[bgY] = overlayLine(bgLines[bgY], modalLine, startX, width)
		}
	}

	return strings.Join(bgLines, "\n")
}

// overlayLine places a modal line on top of a background line at position x
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

// stripAnsi removes ANSI escape codes from a string
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
