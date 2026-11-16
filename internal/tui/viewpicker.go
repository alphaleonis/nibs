package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// viewChosenMsg shows level in the list.
type viewChosenMsg struct {
	level viewshape.Level
}

// closeViewPickerMsg is sent when the view picker is canceled.
type closeViewPickerMsg struct{}

// viewPickerModel lists the views in levels order. Each row's accelerator
// shows its view, and backKey, an accelerator of its own, shows the view the
// list showed before the current one.
type viewPickerModel struct {
	table    []action // the keymap in effect
	keys     []string // the accelerator of each row
	backKey  string
	current  viewshape.Level
	previous viewshape.Level
	cursor   int
	width    int
	height   int
}

// newViewPickerModel opens the picker over the list, which shows current and
// showed previous before it. The key that opens the picker is also backKey
// when it is one character that no action in the picker takes, so typing it
// twice returns to previous.
func newViewPickerModel(km keymap, current, previous viewshape.Level, width, height int) viewPickerModel {
	opener := km.firstKey(actViewPick)
	reuse := utf8.RuneCountInString(opener) == 1 && !takenKeys(km.actions, ctxViewPicker)[opener]
	// Placed first, the opener's hint keeps every row from taking the opener.
	hints := []string{"previous"}
	if reuse {
		hints[0] = opener
	}
	for _, level := range levels {
		hints = append(hints, strings.ToLower(levelNames[level]))
	}
	keys := assignAccelerators(km.actions, ctxViewPicker, hints)
	if reuse {
		keys[0] = opener
	}
	m := viewPickerModel{
		table: km.actions, backKey: keys[0], keys: keys[1:],
		current: current, previous: previous, width: width, height: height,
	}
	for i, level := range levels {
		if level == current {
			m.cursor = i
		}
	}
	return m
}

// choose returns the command that shows level.
func choose(level viewshape.Level) tea.Cmd {
	return func() tea.Msg { return viewChosenMsg{level: level} }
}

func (m viewPickerModel) Update(msg tea.Msg) (viewPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case actionMsg:
		last := len(levels) - 1
		switch msg.name {
		case actUp:
			m.cursor = max(0, m.cursor-1)
		case actDown:
			m.cursor = min(last, m.cursor+1)
		case actTop, actPageUp:
			m.cursor = 0
		case actBottom, actPageDown:
			m.cursor = last
		case actOpen:
			return m, choose(levels[m.cursor])
		case actLeave:
			return m, func() tea.Msg { return closeViewPickerMsg{} }
		}

	case clickRowMsg:
		if !msg.choose {
			m.cursor = msg.row
			return m, nil
		}
		return m, choose(levels[msg.row])

	case acceleratorMsg:
		if msg.key != "" && msg.key == m.backKey {
			return m, choose(m.previous)
		}
		for i, key := range m.keys {
			if key != "" && key == msg.key {
				return m, choose(levels[i])
			}
		}
	}
	return m, nil
}

func (m viewPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m viewPickerModel) modal() pickerModalConfig {
	nameWidth := 0
	for _, level := range levels {
		nameWidth = max(nameWidth, lipgloss.Width(levelNames[level]))
	}
	var lines []string
	for i, level := range levels {
		cursor := "  "
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
		}
		name := lipgloss.NewStyle().Width(nameWidth).Render(levelNames[level])
		var note string
		switch level {
		case m.current:
			note = ui.Muted.Render("current")
		case m.previous:
			note = renderAccelerator(m.backKey) + ui.Muted.Render("previous")
		}
		lines = append(lines, cursor+renderAccelerator(m.keys[i])+name+"  "+note)
	}
	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxViewPicker,
		Title:       "View",
		ListContent: strings.Join(lines, "\n"),
		Rows:        pickerRows{count: len(lines)},
		Width:       m.width,
	}
}
