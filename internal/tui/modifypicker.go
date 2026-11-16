package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// modifyRow is a field edit the modify picker offers. Choosing it opens that
// field's picker with the message open returns for the picker's targets.
type modifyRow struct {
	label string // also the accelerator's hint
	value func(*nib.Nib) string
	// show draws a value the targets share. Nil draws the value itself.
	show func(value string, b Backend) string
	open func(targets modifyTargets) tea.Msg
}

// modifyRows are the field edits without a key of their own.
var modifyRows = []modifyRow{
	{
		label: "priority",
		value: (*nib.Nib).EffectivePriority,
		open: func(t modifyTargets) tea.Msg {
			msg := openPriorityPickerMsg{nibIDs: t.ids(), nibTitle: t.title}
			if len(t.nibs) == 1 {
				msg.currentPriority = t.nibs[0].EffectivePriority()
			}
			return msg
		},
	},
	{
		label: "estimate",
		value: func(n *nib.Nib) string { return n.Estimate },
		open: func(t modifyTargets) tea.Msg {
			msg := openEstimatePickerMsg{nibIDs: t.ids(), nibTitle: t.title}
			if len(t.nibs) == 1 {
				msg.currentEstimate = t.nibs[0].Estimate
			}
			return msg
		},
	},
	assignRow(milestoneField, showMilestone),
	assignRow(areaField, func(value string, _ Backend) string { return area.RenderPath(value) }),
}

// assignRow is the modify picker's row for field, which opens field's picker.
func assignRow(field *assignField, show func(string, Backend) string) modifyRow {
	return modifyRow{
		label: field.name,
		value: field.stored,
		show:  show,
		open: func(t modifyTargets) tea.Msg {
			return openAssignPickerMsg{field: field, targets: t}
		},
	}
}

// showMilestone draws a milestone assignment as the milestone's title, or as
// the stored id when it names no milestone.
func showMilestone(id string, b Backend) string {
	if ms, err := b.GetNib(context.Background(), id); err == nil && ms != nil && ms.EffectiveType() == "milestone" {
		return ms.Title
	}
	return displayID(id)
}

// modifyTargets are the nibs the modify picker edits, fixed when it opens so
// a reload that moves the list cursor does not change them.
type modifyTargets struct {
	nibs  []*nib.Nib
	title string // the nib's title, or "N selected nibs"
}

func (t modifyTargets) ids() []string {
	ids := make([]string, len(t.nibs))
	for i, n := range t.nibs {
		ids[i] = n.ID
	}
	return ids
}

// openModifyPickerMsg requests the modify picker for targets.
type openModifyPickerMsg struct {
	targets modifyTargets
}

// modifyChosenMsg opens a field's picker over the view the modify picker
// opened from.
type modifyChosenMsg struct {
	open tea.Msg
}

// closeModifyPickerMsg is sent when the modify picker is canceled.
type closeModifyPickerMsg struct{}

type modifyItem struct {
	row   modifyRow
	value string
	key   string
}

// modifyPickerModel lists the modifyRows with the targets' current values.
type modifyPickerModel struct {
	table   []action // the keymap in effect
	items   []modifyItem
	cursor  int
	targets modifyTargets
	width   int
	height  int
}

func newModifyPickerModel(table []action, targets modifyTargets, b Backend, width, height int) modifyPickerModel {
	hints := make([]string, len(modifyRows))
	for i, row := range modifyRows {
		hints[i] = row.label
	}
	keys := assignAccelerators(table, ctxModifyPicker, hints)
	items := make([]modifyItem, len(modifyRows))
	for i, row := range modifyRows {
		items[i] = modifyItem{row: row, value: showShared(targets.nibs, row, b), key: keys[i]}
	}
	return modifyPickerModel{table: table, items: items, targets: targets, width: width, height: height}
}

// showShared draws the value of row every nib has, "mixed" when they differ,
// or "none" when the shared value is empty.
func showShared(nibs []*nib.Nib, row modifyRow, b Backend) string {
	v, mixed := sharedValue(nibs, row.value)
	switch {
	case mixed:
		return "mixed"
	case v == "":
		return "none"
	case row.show != nil:
		return row.show(v, b)
	}
	return v
}

// sharedValue returns the value every nib has, or mixed when they differ.
func sharedValue(nibs []*nib.Nib, value func(*nib.Nib) string) (v string, mixed bool) {
	if len(nibs) == 0 {
		return "", false
	}
	v = value(nibs[0])
	for _, n := range nibs[1:] {
		if value(n) != v {
			return "", true
		}
	}
	return v, false
}

// Update returns the picker and the message for the App to apply at once: a
// modifyChosenMsg, a closeModifyPickerMsg, or nil. The App applies it before
// the next key, so a key typed after a row's accelerator reaches the field's
// picker.
func (m modifyPickerModel) Update(msg tea.Msg) (modifyPickerModel, tea.Msg) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case actionMsg:
		switch msg.name {
		case actUp:
			m.cursor = max(0, m.cursor-1)
		case actDown:
			m.cursor = max(0, min(len(m.items)-1, m.cursor+1))
		case actTop, actPageUp:
			m.cursor = 0
		case actBottom, actPageDown:
			m.cursor = max(0, len(m.items)-1)
		case actOpen:
			if m.cursor < len(m.items) {
				return m, modifyChosenMsg{open: m.items[m.cursor].row.open(m.targets)}
			}
		case actLeave:
			return m, closeModifyPickerMsg{}
		}

	case clickRowMsg:
		m.cursor = msg.row
		if !msg.choose {
			return m, nil
		}
		return m.Update(actionMsg{name: actOpen})

	case acceleratorMsg:
		for i, item := range m.items {
			if item.key == msg.key {
				m.cursor = i
				return m.Update(actionMsg{name: actOpen})
			}
		}
	}
	return m, nil
}

func (m modifyPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m modifyPickerModel) modal() pickerModalConfig {
	labelWidth := 0
	for _, item := range m.items {
		labelWidth = max(labelWidth, lipgloss.Width(item.row.label))
	}
	// The modal's padding leaves its width less 2 columns, and the cursor and
	// the accelerator take 4 more.
	valueWidth := max(1, pickerModalWidth(m.width, 0, 0)-2-4-labelWidth-2)
	lines := []string{listTitleStyle.Render("Modify"), ""}
	rows := pickerRows{top: len(lines), count: len(m.items)}
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
		}
		label := lipgloss.NewStyle().Width(labelWidth).Render(item.row.label)
		value := ui.Muted.Render(truncateTitle(item.value, valueWidth))
		lines = append(lines, cursor+renderAccelerator(item.key)+label+"  "+value)
	}
	var nibID string
	if len(m.targets.nibs) == 1 {
		nibID = m.targets.nibs[0].ID
	}
	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxModifyPicker,
		NibTitle:    m.targets.title,
		NibID:       nibID,
		ListContent: strings.Join(lines, "\n"),
		Rows:        rows,
		Width:       m.width,
	}
}
