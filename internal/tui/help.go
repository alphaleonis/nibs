package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/ui"
)

// helpEntry represents a single keybinding for the help panel.
type helpEntry struct {
	Key  string
	Desc string
}

// listHelpEntries returns the list view's keybindings for the help panel.
// expandedHelpEntries appends esc, ? and q.
func listHelpEntries() []helpEntry {
	return []helpEntry{
		{"enter", "view details"},
		{"space", "select"},
		{"c", "create"},
		{"e", "edit in $EDITOR"},
		{"b", "manage blocking"},
		{"p", "set parent"},
		{"s", "change status"},
		{"t", "change type"},
		{"P", "change priority"},
		{"E", "change estimate"},
		{"y", "copy nib ID"},
		{"H", "hide completed"},
		{"W", "wide mode"},
		{"tab", "collapse/expand"},
		{"\u2190/\u2192", "collapse/expand"},
		{"shift+tab", "collapse all"},
		{"]", "expand all"},
		{"ctrl+\u2191/\u2193", "reorder (block if multi-selected)"},
		{"/", "filter"},
		{"g t", "filter by tag"},
		{"A", "archive"},
		{"del", "delete"},
	}
}

// detailHelpEntries returns the non-obvious keybindings for the detail view.
func detailHelpEntries() []helpEntry {
	return []helpEntry{
		{"tab", "switch links/body"},
		{"enter", "go to link"},
		{"/", "filter links"},
		{"b", "manage blocking"},
		{"e", "edit in $EDITOR"},
		{"p", "set parent"},
		{"s", "change status"},
		{"t", "change type"},
		{"P", "change priority"},
		{"E", "change estimate"},
		{"y", "copy nib ID"},
		{"j/k", "scroll"},
	}
}

// detailExpandedEntries returns all keybindings for the expanded detail help panel.
func detailExpandedEntries() []helpEntry {
	entries := detailHelpEntries()
	entries = append(entries,
		helpEntry{"esc", "back"},
		helpEntry{"?", "less"},
		helpEntry{"q", "quit"},
	)
	return entries
}

// minHelpPanelWidth is the narrowest terminal the help panel draws into.
const minHelpPanelWidth = 20

// minHelpDescWidth is the narrowest description column packing squeezes to.
const minHelpDescWidth = 10

// helpLayout is the column-major grid renderHelpPanel draws entries into.
// hidden counts the entries the row budget had no room for; a marker in the
// grid's last cell names them.
type helpLayout struct {
	cols     int
	rows     int
	colWidth int
	keyWidth int
	hidden   int
}

// helpPanelLayout arranges entries into columns, filling down then across.
//
// maxRows bounds the panel's height; 0 leaves it unbounded. When the natural
// layout exceeds it, entries pack into more, narrower columns down to
// minHelpDescWidth, and whatever still does not fit is counted in hidden.
func helpPanelLayout(entries []helpEntry, width, maxRows int) helpLayout {
	if len(entries) == 0 || width < minHelpPanelWidth {
		return helpLayout{}
	}

	keyWidth := helpKeyWidth(entries)
	natural := helpColWidth(entries)
	// Each row is " " + cols*colWidth, leaving width-1 cells. The column count
	// comes from width and colWidth is capped to avail/cols, so the leading space
	// costs each column a cell rather than costing a column.
	avail := width - 1
	cols := max(1, width/natural)
	colWidth := min(natural, avail/cols)

	if maxRows > 0 && helpRows(len(entries), cols) > maxRows {
		widest := max(1, avail/(keyWidth+1+minHelpDescWidth))
		if packed := min(helpRows(len(entries), maxRows), widest); packed > cols {
			cols = packed
			colWidth = avail / cols
		}
	}

	l := helpLayout{cols: cols, rows: helpRows(len(entries), cols), colWidth: colWidth, keyWidth: keyWidth}
	if maxRows > 0 && l.rows > maxRows {
		// The marker takes a cell, not a row.
		l.rows = maxRows
		l.hidden = len(entries) - (l.rows*l.cols - 1)
	}
	return l
}

// helpRows is the number of rows n entries occupy across per columns.
func helpRows(n, per int) int {
	if per < 1 {
		return n
	}
	return (n + per - 1) / per
}

// renderHelpPanel renders keybindings in a column-major grid, keys right-aligned
// in a fixed column. maxRows bounds the height; see helpPanelLayout.
func renderHelpPanel(entries []helpEntry, width, maxRows int) string {
	l := helpPanelLayout(entries, width, maxRows)
	if l.cols == 0 {
		return ""
	}

	descWidth := max(1, l.colWidth-l.keyWidth-1)

	keyStyle := lipgloss.NewStyle().
		Foreground(ui.ColorPrimary).
		Bold(true).
		Width(l.keyWidth).
		Align(lipgloss.Right)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaa")).
		MaxWidth(descWidth)

	entryStyle := lipgloss.NewStyle().Width(l.colWidth)

	// With entries hidden, the marker takes the grid's last cell.
	shown := len(entries)
	if l.hidden > 0 {
		shown = l.rows*l.cols - 1
	}

	var lines []string
	for row := 0; row < l.rows; row++ {
		var line string
		for col := 0; col < l.cols; col++ {
			idx := col*l.rows + row
			switch {
			case idx < shown:
				e := entries[idx]
				entry := keyStyle.Render(e.Key) + " " + descStyle.Render(truncateHelpDesc(e.Desc, descWidth))
				line += entryStyle.Render(entry)
			case idx == shown && l.hidden > 0:
				line += entryStyle.Render(helpHiddenStyle.Render(helpHiddenMarker(l.hidden, l.colWidth)))
			}
		}
		lines = append(lines, " "+line)
	}

	return strings.Join(lines, "\n")
}

// helpHiddenMarker says how many keybindings the panel had no room for.
func helpHiddenMarker(hidden, width int) string {
	return lipgloss.NewStyle().MaxWidth(max(1, width)).Render(fmt.Sprintf("\u2026 %d more keys", hidden))
}

// truncateHelpDesc cuts desc to width cells, ending in an ellipsis when cut.
func truncateHelpDesc(desc string, width int) string {
	if lipgloss.Width(desc) <= width {
		return desc
	}
	return lipgloss.NewStyle().MaxWidth(max(1, width-1)).Render(desc) + "\u2026"
}

// helpPanelHeight returns the number of terminal lines the help panel occupies.
func helpPanelHeight(entries []helpEntry, width, maxRows int) int {
	return helpPanelLayout(entries, width, maxRows).rows
}

// helpKeyWidth returns the width of the panel's right-aligned key column, in
// display cells: some keys are multi-byte.
func helpKeyWidth(entries []helpEntry) int {
	maxKeyLen := 0
	for _, e := range entries {
		maxKeyLen = max(maxKeyLen, lipgloss.Width(e.Key))
	}
	return maxKeyLen
}

// helpColWidth returns the help panel's column width: key, space, description,
// and a three-cell gap.
func helpColWidth(entries []helpEntry) int {
	maxDescLen := 0
	for _, e := range entries {
		maxDescLen = max(maxDescLen, lipgloss.Width(e.Desc))
	}
	return helpKeyWidth(entries) + 1 + maxDescLen + 3
}

// expandedFooterRegion renders what a view draws below itself while the help
// panel is expanded: the status message, then the panel. The panel replaces the
// compact help keys, never the message.
//
// holdBack is the fewest rows the view above occupies at any height. Both blocks
// fit in height-holdBack rows, and when that region is taller than one row the
// message leaves the panel at least one.
func expandedFooterRegion(entries []helpEntry, msg string, kind statusKind, width, height, holdBack int) string {
	region := max(1, height-holdBack)
	status := statusBlock(msg, kind, width, min(maxStatusFooterLines(height), max(1, region-1)))
	var panel string
	if rows := helpRowBudget(height, holdBack, blockLines(status)); rows > 0 {
		panel = renderHelpPanel(entries, width, rows)
	}
	switch {
	case status == "":
		return panel
	case panel == "":
		return status
	default:
		return status + "\n" + panel
	}
}

// renderHelpKey renders a single key-description pair in the footer style.
func renderHelpKey(key, desc string) string {
	return helpKeyStyle.Render(key) + " " + helpStyle.Render(desc)
}
