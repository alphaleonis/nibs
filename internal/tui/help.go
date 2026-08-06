package tui

import (
	"strings"

	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/charmbracelet/lipgloss"
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

// renderHelpPanel renders keybindings in a multi-column layout.
// Entries are arranged column-major (filling down, then across).
// Keys are right-aligned in a fixed column so descriptions stay aligned
// and short keys sit close to their label.
func renderHelpPanel(entries []helpEntry, width int) string {
	if len(entries) == 0 || width < 20 {
		return ""
	}

	colWidth := helpColWidth(entries)
	numCols := max(1, width/colWidth)
	numRows := (len(entries) + numCols - 1) / numCols

	// Compute max widths for alignment
	maxKeyLen := 0
	maxDescLen := 0
	for _, e := range entries {
		if len(e.Key) > maxKeyLen {
			maxKeyLen = len(e.Key)
		}
		if len(e.Desc) > maxDescLen {
			maxDescLen = len(e.Desc)
		}
	}
	keyStyle := lipgloss.NewStyle().
		Foreground(ui.ColorPrimary).
		Bold(true).
		Width(maxKeyLen).
		Align(lipgloss.Right)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaa")).
		MaxWidth(maxDescLen)

	// Fixed-width column: right-aligned key + space + description + gap
	entryStyle := lipgloss.NewStyle().Width(colWidth)

	var lines []string
	for row := 0; row < numRows; row++ {
		var line string
		for col := 0; col < numCols; col++ {
			idx := col*numRows + row
			if idx < len(entries) {
				e := entries[idx]
				entry := keyStyle.Render(e.Key) + " " + descStyle.Render(e.Desc)
				line += entryStyle.Render(entry)
			}
		}
		lines = append(lines, " "+line)
	}

	return strings.Join(lines, "\n")
}

// helpPanelHeight returns the number of terminal lines the help panel occupies.
func helpPanelHeight(entries []helpEntry, width int) int {
	if len(entries) == 0 || width < 20 {
		return 0
	}

	colWidth := helpColWidth(entries)
	numCols := max(1, width/colWidth)
	numRows := (len(entries) + numCols - 1) / numCols

	return numRows
}

// helpColWidth returns the column width for the help panel.
// Layout: [right-aligned key (maxKeyLen)] [space] [description] [gap]
func helpColWidth(entries []helpEntry) int {
	maxKeyLen := 0
	maxDescLen := 0
	for _, e := range entries {
		if len(e.Key) > maxKeyLen {
			maxKeyLen = len(e.Key)
		}
		if len(e.Desc) > maxDescLen {
			maxDescLen = len(e.Desc)
		}
	}
	return maxKeyLen + 1 + maxDescLen + 3 // key + space + desc + gap
}

// renderHelpKey renders a single key-description pair in the footer style.
func renderHelpKey(key, desc string) string {
	return helpKeyStyle.Render(key) + " " + helpStyle.Render(desc)
}
