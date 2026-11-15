package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// modifyRow is a field edit the modify picker offers. Choosing it opens that
// field's picker with the message open returns for the picker's targets.
type modifyRow struct {
	label string // also the accelerator's hint
	value func(*nib.Nib) string
	open  func(targets modifyTargets) tea.Msg
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

func newModifyPickerModel(table []action, targets modifyTargets, width, height int) modifyPickerModel {
	hints := make([]string, len(modifyRows))
	for i, row := range modifyRows {
		hints[i] = row.label
	}
	keys := assignAccelerators(table, ctxModifyPicker, hints)
	items := make([]modifyItem, len(modifyRows))
	for i, row := range modifyRows {
		items[i] = modifyItem{row: row, value: sharedValue(targets.nibs, row.value), key: keys[i]}
	}
	return modifyPickerModel{table: table, items: items, targets: targets, width: width, height: height}
}

// sharedValue returns the value every nib has, "mixed" when they differ, or
// "none" when the shared value is empty.
func sharedValue(nibs []*nib.Nib, value func(*nib.Nib) string) string {
	if len(nibs) == 0 {
		return ""
	}
	v := value(nibs[0])
	for _, n := range nibs[1:] {
		if value(n) != v {
			return "mixed"
		}
	}
	if v == "" {
		return "none"
	}
	return v
}

func (m modifyPickerModel) Update(msg tea.Msg) (modifyPickerModel, tea.Cmd) {
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
				open := m.items[m.cursor].row.open(m.targets)
				return m, func() tea.Msg { return modifyChosenMsg{open: open} }
			}
		case actLeave:
			return m, func() tea.Msg { return closeModifyPickerMsg{} }
		}

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
	labelWidth := 0
	for _, item := range m.items {
		labelWidth = max(labelWidth, lipgloss.Width(item.row.label))
	}
	lines := []string{listTitleStyle.Render("Modify"), ""}
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
		}
		label := lipgloss.NewStyle().Width(labelWidth).Render(item.row.label)
		lines = append(lines, cursor+renderAccelerator(item.key)+label+"  "+ui.Muted.Render(item.value))
	}
	var nibID string
	if len(m.targets.nibs) == 1 {
		nibID = m.targets.nibs[0].ID
	}
	return renderPickerModal(pickerModalConfig{
		Keys:        m.table,
		Context:     ctxModifyPicker,
		NibTitle:    m.targets.title,
		NibID:       nibID,
		ListContent: strings.Join(lines, "\n"),
		Width:       m.width,
	})
}

// ModalView returns the picker centered over bgView.
func (m modifyPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	return overlayModal(bgView, m.View(), fullWidth, fullHeight)
}
