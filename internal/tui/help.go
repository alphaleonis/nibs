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

// listHelpEntries returns the non-obvious keybindings for the list view.
// Context-sensitive entries (esc, ?, q) are appended by expandedHelpEntries.
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

// minHelpPanelWidth is the narrowest terminal the panel will draw into. Below
// it a two-part entry has no room to be read as one.
const minHelpPanelWidth = 20

// minHelpDescWidth is the narrowest description column packing will squeeze to.
// Past it a description is cut to nothing that distinguishes one key from
// another, and the key alone is what the panel exists to name.
const minHelpDescWidth = 10

// helpLayout is the grid renderHelpPanel draws entries into, column-major.
//
// hidden counts the entries the row budget left no room for. They are named by
// a marker in the grid's last cell rather than dropped in silence: the panel is
// drawn into the same fixed cell grid as the rest of the frame, so a row past
// the terminal's last one disappears with nothing on screen to say the
// keybinding is there at all.
type helpLayout struct {
	cols     int
	rows     int
	colWidth int
	keyWidth int
	hidden   int
}

// helpPanelLayout arranges entries into columns, filling down then across.
//
// maxRows bounds the panel's height and is honored, not advised; 0 leaves it
// unbounded. When the natural layout — columns wide enough for the longest
// description — would run past that bound, entries are packed into more and
// narrower columns, since a description narrowed to fit still names its key.
// Packing runs out at minHelpDescWidth, and what the budget still cannot hold
// is withheld and counted in hidden rather than rendered past the bound.
func helpPanelLayout(entries []helpEntry, width, maxRows int) helpLayout {
	if len(entries) == 0 || width < minHelpPanelWidth {
		return helpLayout{}
	}

	keyWidth := helpKeyWidth(entries)
	natural := helpColWidth(entries)
	cols := max(1, width/natural)
	colWidth := natural

	if maxRows > 0 && helpRows(len(entries), cols) > maxRows {
		// The leading space each row carries is why the packed width is
		// measured against width-1: the row is " " + cols*colWidth.
		widest := max(1, (width-1)/(keyWidth+1+minHelpDescWidth))
		if packed := min(helpRows(len(entries), maxRows), widest); packed > cols {
			cols = packed
			colWidth = (width - 1) / cols
		}
	}

	l := helpLayout{cols: cols, rows: helpRows(len(entries), cols), colWidth: colWidth, keyWidth: keyWidth}
	if maxRows > 0 && l.rows > maxRows {
		// The marker takes a cell rather than a row of its own, so the rows the
		// budget does grant stay full of keybindings.
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

// renderHelpPanel renders keybindings in a multi-column layout.
// Entries are arranged column-major (filling down, then across).
// Keys are right-aligned in a fixed column so descriptions stay aligned
// and short keys sit close to their label.
//
// maxRows bounds the panel's height; see helpPanelLayout.
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

	// Fixed-width column: right-aligned key + space + description + gap
	entryStyle := lipgloss.NewStyle().Width(l.colWidth)

	// The marker takes the grid's last cell when the budget could not hold
	// every entry, so what is shown ends one short of a full grid.
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

// helpHiddenMarker names the keybindings the row budget had no room for. A
// panel that simply stops reads as the whole set of keys the view answers to.
func helpHiddenMarker(hidden, width int) string {
	return lipgloss.NewStyle().MaxWidth(max(1, width)).Render(fmt.Sprintf("\u2026 %d more keys", hidden))
}

// truncateHelpDesc cuts a description to width, marking the cut. A description
// that just stops reads as the whole of what the key does.
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

// helpKeyWidth returns the width of the panel's right-aligned key column.
//
// Keys are measured in display cells, not bytes: the column feeds how far the
// panel may pack and where descriptions are cut, and two list keys are
// multi-byte (ctrl+\u2191/\u2193 is twelve bytes of nine cells).
func helpKeyWidth(entries []helpEntry) int {
	maxKeyLen := 0
	for _, e := range entries {
		maxKeyLen = max(maxKeyLen, lipgloss.Width(e.Key))
	}
	return maxKeyLen
}

// helpColWidth returns the column width for the help panel.
// Layout: [right-aligned key (maxKeyLen)] [space] [description] [gap]
func helpColWidth(entries []helpEntry) int {
	maxDescLen := 0
	for _, e := range entries {
		maxDescLen = max(maxDescLen, lipgloss.Width(e.Desc))
	}
	return helpKeyWidth(entries) + 1 + maxDescLen + 3 // key + space + desc + gap
}

// expandedFooterRegion composes everything a view draws below itself while the
// help panel is expanded: the status message, with the panel beneath it.
//
// The panel replaces the view's compact help keys, not its status message — the
// message is the outcome of the action the user just took, and a panel drawn
// over it leaves a refused edit silent, which is the whole reason the footer
// learned to carry a refusal. It sits above the panel because that end of the
// region is furthest from the clip edge, and because the detail footer already
// puts a message above its help row.
//
// holdBack is the fewest rows the view above the region occupies however small a
// height it is handed. Both blocks are sized inside height-holdBack, one row of
// which is kept from the message so the panel always has somewhere to say what
// it could not fit.
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
