package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/filterlang"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/ui"
)

// filterField is a metadata field a quick filter writes a token for, as one
// of the web's filter dropdowns does.
type filterField struct {
	label  string     // the field's row in the filter picker
	act    actionName // the action that opens the field's values picker
	values filterlang.ValueSet
	// lists returns the field's include and exclude lists in f.
	lists func(f *model.NibFilter) (include, exclude *[]string)
}

// filterFields are the fields the filter picker lists, in its order.
var filterFields = []filterField{
	{label: "Status", act: actFilterStatus, values: filterlang.StatusValues,
		lists: func(f *model.NibFilter) (*[]string, *[]string) { return &f.Status, &f.ExcludeStatus }},
	{label: "Type", act: actFilterType, values: filterlang.TypeValues,
		lists: func(f *model.NibFilter) (*[]string, *[]string) { return &f.Type, &f.ExcludeType }},
	{label: "Priority", act: actFilterPriority, values: filterlang.PriorityValues,
		lists: func(f *model.NibFilter) (*[]string, *[]string) { return &f.Priority, &f.ExcludePriority }},
	{label: "Estimate", act: actFilterEstimate, values: filterlang.EstimateValues,
		lists: func(f *model.NibFilter) (*[]string, *[]string) { return &f.Estimate, &f.ExcludeEstimate }},
	{label: "Tags", act: actFilterTags, values: filterlang.TagValues,
		lists: func(f *model.NibFilter) (*[]string, *[]string) { return &f.Tags, &f.ExcludeTags }},
}

// filterFieldOf returns the field whose values picker act opens, or false
// when act opens none.
func filterFieldOf(act actionName) (filterField, bool) {
	i := slices.IndexFunc(filterFields, func(f filterField) bool { return f.act == act })
	if i < 0 {
		return filterField{}, false
	}
	return filterFields[i], true
}

// fieldTokens returns field's tokens in text as canonical text, or "" when
// text has none.
func fieldTokens(text string, areas *area.Vocabulary, field filterField) string {
	parsed, _ := filterlang.ParseWithAreas(text, areas)
	include, exclude := field.lists(&parsed)
	var only model.NibFilter
	onlyInclude, onlyExclude := field.lists(&only)
	*onlyInclude, *onlyExclude = *include, *exclude
	return filterlang.Serialize(only, nil)
}

// withFieldValues returns text as canonical text with field's tokens replaced
// by one token that includes values, or removed when values is empty.
func withFieldValues(text string, areas *area.Vocabulary, field filterField, values []string) string {
	parsed, diags := filterlang.ParseWithAreas(text, areas)
	include, exclude := field.lists(&parsed)
	*include, *exclude = nil, nil
	if len(values) > 0 {
		*include = slices.Clone(values)
	}
	return filterlang.Serialize(parsed, filterlang.InvalidTokens(diags))
}

// filterFieldChosenMsg opens field's values picker over the view the filter
// picker opened from.
type filterFieldChosenMsg struct {
	field filterField
}

// closeFilterPickerMsg is sent when the filter picker or a values picker is
// canceled.
type closeFilterPickerMsg struct{}

// filterValuesChosenMsg writes values as field's token in the list's query.
type filterValuesChosenMsg struct {
	field  filterField
	values []string
}

type filterPickerRow struct {
	field  filterField
	tokens string // the field's tokens in the query
	key    string
}

// filterPickerModel lists the filterFields with their tokens in the query.
type filterPickerModel struct {
	table  []action // the keymap in effect
	rows   []filterPickerRow
	cursor int
	width  int
}

// filterPickerKeys returns the filter picker's accelerator for each of the
// filterFields under table.
func filterPickerKeys(table []action) []string {
	hints := make([]string, len(filterFields))
	for i, f := range filterFields {
		hints[i] = strings.ToLower(f.label)
	}
	return assignAccelerators(table, ctxFilterPicker, hints)
}

func newFilterPickerModel(table []action, query string, areas *area.Vocabulary, width int) filterPickerModel {
	keys := filterPickerKeys(table)
	rows := make([]filterPickerRow, len(filterFields))
	for i, f := range filterFields {
		rows[i] = filterPickerRow{field: f, tokens: fieldTokens(query, areas, f), key: keys[i]}
	}
	return filterPickerModel{table: table, rows: rows, width: width}
}

// Update returns the picker and the message for the App to apply at once: a
// filterFieldChosenMsg, a closeFilterPickerMsg, or nil. The App applies it
// before the next key, so a key typed after a row's accelerator reaches the
// field's values picker.
func (m filterPickerModel) Update(msg tea.Msg) (filterPickerModel, tea.Msg) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

	case actionMsg:
		switch msg.name {
		case actOpen:
			return m, filterFieldChosenMsg{field: m.rows[m.cursor].field}
		case actLeave:
			return m, closeFilterPickerMsg{}
		default:
			m.cursor = moveCursor(m.cursor, len(m.rows), msg.name)
		}

	case clickRowMsg:
		m.cursor = msg.row
		if !msg.choose {
			return m, nil
		}
		return m.Update(actionMsg{name: actOpen})

	case acceleratorMsg:
		for i, row := range m.rows {
			if row.key != "" && row.key == msg.key {
				m.cursor = i
				return m.Update(actionMsg{name: actOpen})
			}
		}
	}
	return m, nil
}

// moveCursor returns cursor moved by a move action over n rows, or cursor
// itself for any other action.
func moveCursor(cursor, n int, name actionName) int {
	last := max(0, n-1)
	switch name {
	case actUp:
		return max(0, cursor-1)
	case actDown:
		return min(last, cursor+1)
	case actTop, actPageUp:
		return 0
	case actBottom, actPageDown:
		return last
	}
	return cursor
}

// cursorMark draws the cursor before a row, or the blank it takes on any
// other row.
func cursorMark(on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	}
	return "  "
}

func (m filterPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m filterPickerModel) modal() pickerModalConfig {
	labelWidth := 0
	for _, row := range m.rows {
		labelWidth = max(labelWidth, lipgloss.Width(row.field.label))
	}
	// The modal's padding leaves its width less 2 columns, and the cursor and
	// the accelerator take 4 more.
	tokenWidth := max(1, pickerModalWidth(m.width, 0, 0)-2-4-labelWidth-2)
	lines := make([]string, len(m.rows))
	for i, row := range m.rows {
		label := lipgloss.NewStyle().Width(labelWidth).Render(row.field.label)
		tokens := ui.Muted.Render("any")
		if row.tokens != "" {
			tokens = truncateTitle(row.tokens, tokenWidth)
		}
		lines[i] = cursorMark(i == m.cursor) + renderAccelerator(row.key) + label + "  " + tokens
	}
	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxFilterPicker,
		Title:       "Filter",
		ListContent: strings.Join(lines, "\n"),
		Rows:        pickerRows{count: len(lines)},
		Width:       m.width,
	}
}

// filterValueRow is a value of the field, or the preset that checks the
// open statuses when preset is set.
type filterValueRow struct {
	value   string
	preset  bool
	checked bool
	key     string
}

// filterValuesPickerModel checks the values of one field. An accelerator or
// space toggles a value, and enter writes the checked values as the field's
// token.
type filterValuesPickerModel struct {
	table    []action // the keymap in effect
	field    filterField
	rows     []filterValueRow
	replaces string // the field's tokens in the query
	// changed reports whether a value was toggled. Until one is, enter leaves
	// the query as it is, so an exclusion no row shows as checked survives.
	changed bool
	cursor  int
	width   int
	height  int
}

// pickerValues returns the values field's picker lists: its vocabulary, or
// for tags those of the store and those the query names.
func pickerValues(field filterField, included, storeTags []string) []string {
	if field.values != filterlang.TagValues {
		return filterlang.Values(field.values)
	}
	tags := slices.Concat(storeTags, included)
	slices.Sort(tags)
	return slices.Compact(tags)
}

// filterValuesKeys gives each hint an accelerator free in the values picker.
// Besides the keys freeAccelerators leaves out, it leaves out the last key of
// field's own bindings and field's key in the filter picker, which a double
// tap would otherwise repeat.
func filterValuesKeys(table []action, field filterField, hints []string) []string {
	opener := map[string]bool{}
	addBindingEnds(opener, table, field.act)
	if i := slices.IndexFunc(filterFields, func(f filterField) bool { return f.act == field.act }); i >= 0 {
		opener[filterPickerKeys(table)[i]] = true
	}
	free := slices.DeleteFunc(freeAccelerators(table, ctxFilterValuesPicker), func(k string) bool { return opener[k] })
	return assignFrom(free, hints)
}

func newFilterValuesPickerModel(table []action, field filterField, query string, areas *area.Vocabulary, storeTags []string, width, height int) filterValuesPickerModel {
	parsed, _ := filterlang.ParseWithAreas(query, areas)
	include, _ := field.lists(&parsed)
	var rows []filterValueRow
	if field.values == filterlang.StatusValues {
		rows = append(rows, filterValueRow{value: config.StatusGroupOpen, preset: true})
	}
	for _, v := range pickerValues(field, *include, storeTags) {
		rows = append(rows, filterValueRow{value: v, checked: slices.Contains(*include, v)})
	}
	hints := make([]string, len(rows))
	for i, row := range rows {
		hints[i] = row.value
	}
	for i, k := range filterValuesKeys(table, field, hints) {
		rows[i].key = k
	}
	return filterValuesPickerModel{
		table: table, field: field, rows: rows, replaces: fieldTokens(query, areas, field),
		width: width, height: height,
	}
}

// checked returns the checked values in row order.
func (m filterValuesPickerModel) checked() []string {
	var values []string
	for _, row := range m.rows {
		if row.checked && !row.preset {
			values = append(values, row.value)
		}
	}
	return values
}

// toggle toggles the row under the cursor. The preset checks exactly the
// values of its group.
func (m *filterValuesPickerModel) toggle() {
	if m.cursor >= len(m.rows) {
		return
	}
	m.changed = true
	row := &m.rows[m.cursor]
	if !row.preset {
		row.checked = !row.checked
		return
	}
	members := filterlang.GroupMembers(m.field.values, row.value)
	for i := range m.rows {
		if !m.rows[i].preset {
			m.rows[i].checked = slices.Contains(members, m.rows[i].value)
		}
	}
}

func (m filterValuesPickerModel) Update(msg tea.Msg) (filterValuesPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case actionMsg:
		switch msg.name {
		case actSelect:
			m.toggle()
		case actOpen:
			if !m.changed {
				return m, func() tea.Msg { return closeFilterPickerMsg{} }
			}
			chosen := filterValuesChosenMsg{field: m.field, values: m.checked()}
			return m, func() tea.Msg { return chosen }
		case actLeave:
			return m, func() tea.Msg { return closeFilterPickerMsg{} }
		default:
			m.cursor = moveCursor(m.cursor, len(m.rows), msg.name)
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

// filterValuesChromeLines is the lines the picker's modal takes besides its
// rows: the border, the header, the replaced tokens, the blank lines and the
// hint row.
const filterValuesChromeLines = 8

func (m filterValuesPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m filterValuesPickerModel) modal() pickerModalConfig {
	// Each line is cut to the modal's content width, so none wraps and
	// filterValuesChromeLines holds.
	width := pickerModalWidth(m.width, 0, 0) - 2
	var lines []string
	if m.replaces != "" {
		lines = append(lines, ui.Muted.Render(truncateTitle("replaces "+m.replaces, width)), "")
	}
	if len(m.rows) == 0 {
		lines = append(lines, ui.Muted.Render(truncateTitle("No "+strings.ToLower(m.field.label)+" in the store", width)))
	}
	// A short terminal shows the rows around the cursor.
	room := max(1, min(len(m.rows), m.height-filterValuesChromeLines))
	first := max(0, min(m.cursor-room/2, len(m.rows)-room))
	shown := m.rows[first:min(len(m.rows), first+room)]
	rows := pickerRows{top: len(lines), first: first, count: len(shown)}
	for i, row := range shown {
		text := "[ ] " + row.value
		switch {
		case row.preset:
			text = strings.ToUpper(row.value[:1]) + row.value[1:] + ui.Muted.Render("  every status that is not closed")
		case row.checked:
			text = "[x] " + row.value
		}
		// The cursor and the accelerator take 4 columns.
		lines = append(lines, cursorMark(first+i == m.cursor)+renderAccelerator(row.key)+truncateTitle(text, width-4))
	}
	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxFilterValuesPicker,
		Title:       "Filter by " + strings.ToLower(m.field.label),
		ListContent: strings.Join(lines, "\n"),
		Rows:        rows,
		Width:       m.width,
	}
}
