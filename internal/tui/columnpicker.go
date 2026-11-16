package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/ui"
)

// columnsChosenMsg applies columns, the columns view shows after the title,
// and saves them.
type columnsChosenMsg struct {
	view    string
	columns []columnName
}

// closeColumnPickerMsg is sent when the column picker is canceled.
type closeColumnPickerMsg struct{}

// columnRow is one column in the column picker.
type columnRow struct {
	name  columnName
	shown bool
	key   string // the accelerator, or "" when the row has none, as the title row never does
}

// columnPickerModel marks the columns a view shows. An accelerator or space
// toggles a column, and enter applies the marks.
type columnPickerModel struct {
	table  []action // the keymap in effect
	view   string
	rows   []columnRow // in columnOrder
	cursor int
	width  int
	height int
}

// newColumnPickerModel opens the picker on view, which shows the columns of
// shown after the title.
func newColumnPickerModel(table []action, view string, shown []columnName, width, height int) columnPickerModel {
	var hints []string
	for _, name := range columnOrder {
		if name != colTitle {
			hints = append(hints, string(name))
		}
	}
	keys := assignAccelerators(table, ctxColumnPicker, hints)
	rows := make([]columnRow, 0, len(columnOrder))
	for _, name := range columnOrder {
		row := columnRow{name: name, shown: name == colTitle || slices.Contains(shown, name)}
		if name != colTitle {
			row.key, keys = keys[0], keys[1:]
		}
		rows = append(rows, row)
	}
	return columnPickerModel{table: table, view: view, rows: rows, width: width, height: height}
}

// chosen returns the marked columns after the title, in columnOrder.
func (m columnPickerModel) chosen() []columnName {
	columns := []columnName{}
	for _, row := range m.rows {
		if row.shown && row.name != colTitle {
			columns = append(columns, row.name)
		}
	}
	return columns
}

func (m columnPickerModel) Update(msg tea.Msg) (columnPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case actionMsg:
		last := len(m.rows) - 1
		switch msg.name {
		case actUp:
			m.cursor = max(0, m.cursor-1)
		case actDown:
			m.cursor = min(last, m.cursor+1)
		case actTop, actPageUp:
			m.cursor = 0
		case actBottom, actPageDown:
			m.cursor = last
		case actSelect:
			if row := &m.rows[m.cursor]; row.name != colTitle {
				row.shown = !row.shown
			}
		case actOpen:
			chosen := columnsChosenMsg{view: m.view, columns: m.chosen()}
			return m, func() tea.Msg { return chosen }
		case actLeave:
			return m, func() tea.Msg { return closeColumnPickerMsg{} }
		}

	case clickRowMsg:
		m.cursor = msg.row
		if !msg.choose {
			return m, nil
		}
		return m.Update(actionMsg{name: actSelect})

	case acceleratorMsg:
		for i, row := range m.rows {
			if row.key != "" && row.key == msg.key {
				m.cursor = i
				return m.Update(actionMsg{name: actSelect})
			}
		}
	}
	return m, nil
}

// columnPickerChromeLines is the lines the picker's modal takes besides its
// rows: the border, the header, the blank lines and the hint row.
const columnPickerChromeLines = 6

// columnPickerWidthPct is the share of the terminal the picker's modal takes,
// enough for whole descriptions at 80 columns.
const columnPickerWidthPct = 70

func (m columnPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m columnPickerModel) modal() pickerModalConfig {
	nameWidth := 0
	for _, row := range m.rows {
		nameWidth = max(nameWidth, lipgloss.Width(string(row.name)))
	}
	// Each row is one line: the cursor, the accelerator, the mark, the name
	// and a gap take fixed cells, and the description is cut to the rest of
	// the modal's content width.
	modalWidth := pickerModalWidth(m.width, columnPickerWidthPct, 0)
	descWidth := modalWidth - 2 - (2 + 2 + 4 + nameWidth + 2)
	// A short terminal shows the rows around the cursor.
	room := max(1, min(len(m.rows), m.height-columnPickerChromeLines))
	first := min(max(0, m.cursor-room/2), len(m.rows)-room)
	var lines []string
	for i, row := range m.rows[first : first+room] {
		cursor := "  "
		if first+i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
		}
		check := "[ ]"
		if row.shown {
			check = "[x]"
		}
		name := lipgloss.NewStyle().Width(nameWidth).Render(string(row.name))
		desc := fitText(columnDescriptions[row.name], descWidth, "…")
		line := check + " " + name + "  " + ui.Muted.Render(desc)
		if row.name == colTitle {
			line = ui.Muted.Render(check + " " + name + "  " + desc)
		}
		lines = append(lines, cursor+renderAccelerator(row.key)+line)
	}
	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxColumnPicker,
		Title:       "Columns of the " + m.view + " view",
		ListContent: strings.Join(lines, "\n"),
		Rows:        pickerRows{first: first, count: len(lines)},
		Width:       m.width,
		WidthPct:    columnPickerWidthPct,
	}
}
