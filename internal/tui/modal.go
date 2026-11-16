package tui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/ui"
)

// pickerModalConfig configures renderPickerModal.
type pickerModalConfig struct {
	Title       string     // header when NibTitle is empty, e.g. "Select Status"
	NibTitle    string     // the nib's title
	NibID       string     // the nib's ID
	ListContent string     // the rendered list
	Rows        pickerRows // where ListContent draws the picker's rows
	Description string     // optional description shown below list
	Context     keyContext // the picker's key context, whose hints the modal shows
	Keys        []action   // the keymap the hints come from
	Width       int        // screen width
	WidthPct    int        // modal width percentage (0 means 50)
	MaxWidth    int        // max modal width (0 means 60)
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
	content, _ := pickerModalContent(cfg)
	return pickerModalStyle(cfg).Render(content)
}

// pickerModalStyle is the border and padding of a picker modal.
// reservePickerDescription's width depends on them.
func pickerModalStyle(cfg pickerModalConfig) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(0, 1).
		Width(withBorder(pickerModalWidth(cfg.Width, cfg.WidthPct, cfg.MaxWidth)))
}

// pickerModalContent returns what a picker modal draws inside its border, and
// the line of it that cfg.ListContent starts on.
func pickerModalContent(cfg pickerModalConfig) (content string, listTop int) {
	modalWidth := pickerModalWidth(cfg.Width, cfg.WidthPct, cfg.MaxWidth)

	nibTitle := cfg.NibTitle
	if nibTitle == "" {
		nibTitle = cfg.Title
	}
	header := lipgloss.NewStyle().Bold(true).Render(truncateTitle(nibTitle, modalWidth-4))

	subtitle := ui.Muted.Render(displayID(cfg.NibID))

	// The padding leaves modalWidth-2 columns, and a hint row does not wrap.
	help := hintRowWithin(cfg.Keys, actionState{ctx: cfg.Context}, modalWidth-2)

	// Every part before the list is one line.
	var parts []string
	parts = append(parts, header)
	if cfg.NibID != "" {
		parts = append(parts, subtitle)
	}
	parts = append(parts, "")
	listTop = len(parts)
	parts = append(parts, cfg.ListContent)
	if cfg.Description != "" {
		parts = append(parts, "", cfg.Description)
	}
	parts = append(parts, "", help)
	return strings.Join(parts, "\n"), listTop
}

// pickerModalRowAt returns the row that renderPickerModal(cfg) draws on line of
// the modal, or false for a line without one.
func pickerModalRowAt(cfg pickerModalConfig, line int) (int, bool) {
	if cfg.Width == 0 {
		// A picker draws "Loading..." until it knows the terminal's width.
		return 0, false
	}
	_, listTop := pickerModalContent(cfg)
	style := pickerModalStyle(cfg)
	return cfg.Rows.at(line - style.GetBorderTopSize() - style.GetPaddingTop() - listTop)
}

// pickerRows is where a picker draws its rows, one a line: from line top of
// what holds them, the picker's rows first to first+count-1.
type pickerRows struct {
	top, first, count int
}

// at returns the row drawn on line, or false for a line that draws none.
func (r pickerRows) at(line int) (int, bool) {
	i := line - r.top
	if i < 0 || i >= r.count {
		return 0, false
	}
	return r.first + i, true
}

// listRows is where l.View() draws the visible rows of l, which must draw each
// row on one line and show neither pagination nor help. The rows start under
// the title or filter bar and the status bar.
//
// A copy of l laid out taller than any title bar measures that bar: the list
// gives the copy's rows the rest of its height. l itself can show a row past
// its height, because a list draws at least one row per page, and its own
// layout can predate a style that changed the bar. The rows drawn are those of
// l's own page.
func listRows(l list.Model) pickerRows {
	start, end := l.Paginator.GetSliceBounds(len(l.VisibleItems()))
	tall := l.Height() + 100
	measured := l
	measured.SetSize(l.Width(), tall)
	return pickerRows{top: tall - measured.Paginator.PerPage, first: start, count: end - start}
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
	box := modalBox(modal, width, height)
	startX, startY := box.x, box.y

	for i, modalLine := range modalLines {
		bgY := startY + i
		if bgY >= 0 && bgY < len(bgLines) {
			bgLines[bgY] = overlayLine(bgLines[bgY], modalLine, startX, width)
		}
	}

	return strings.Join(bgLines, "\n")
}

// modalBox returns the cells overlayModal draws modal on, centered in a
// terminal width x height.
func modalBox(modal string, width, height int) rect {
	w, h := lipgloss.Width(modal), lipgloss.Height(modal)
	return rect{x: max(0, (width-w)/2), y: max(0, (height-h)/2), w: w, h: h}
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
