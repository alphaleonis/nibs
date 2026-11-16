package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// hitArea is the part of the screen a cell belongs to.
type hitArea uint8

const (
	// hitNone is a cell no click acts on, such as the divider beside a docked
	// nib view.
	hitNone         hitArea = iota
	hitList                 // the list pane off its rows: its rules, its header, and below the last row
	hitListRow              // a row of the list
	hitNibView              // a nib view off its body and relations, such as its head
	hitNibBody              // a nib view's body
	hitNibRelations         // a nib view's relations off any relation, such as a group's heading
	hitNibRelation          // a relation in a nib view
	hitFooter               // the footer
	hitModal                // the open picker or dialog off its rows
	hitModalRow             // a row of the open picker
	hitOutsideModal         // a cell outside the open picker or dialog
)

// hit is what the screen shows at one cell; see App.hitAt.
type hit struct {
	area hitArea
	// docked is whether a nib view's area is the docked view's rather than
	// the expanded view's.
	docked bool
	// row is the index of a list row among the list's visible rows, of a
	// relation in nibViewModel.entries, or of a picker row among its rows.
	row   int
	rowID string // the id of a list row; see rowIDOf
	// fold is whether the cell is on the fold mark of a list row that has
	// children.
	fold bool
}

// clickRowMsg is a click on a row of the open picker. The picker moves its
// cursor to the row, then does what the row's accelerator does when choose is
// set.
type clickRowMsg struct {
	row    int
	choose bool
}

// hitAt says what the frame on screen draws at cell (x, y). It reads the
// layout the frame is drawn from: listFrame for the list screen, the nib
// view's layout, and for a modal the string drawn and modalBox.
func (a *App) hitAt(x, y int) hit {
	if view, rowAt, ok := a.modalOf(a.state); ok {
		box := modalBox(view, a.width, a.height)
		if !box.contains(x, y) {
			return hit{area: hitOutsideModal}
		}
		if rowAt != nil {
			if row, ok := rowAt(y - box.y); ok {
				return hit{area: hitModalRow, row: row}
			}
		}
		return hit{area: hitModal}
	}
	if a.state == viewExpanded {
		height := a.expanded.contentHeight()
		if y >= height {
			return hit{area: hitFooter}
		}
		return a.expanded.hitAt(a.expanded.width, height, x, y)
	}
	f := a.listFrame(a.list.footerHeight())
	switch {
	case f.footer.contains(x, y):
		return hit{area: hitFooter}
	case f.list.contains(x, y):
		return a.list.hitAt(f.list.w, f.list.h, x-f.list.x, y-f.list.y)
	case f.view.contains(x, y):
		h := a.docked.hitAt(f.view.w, f.view.h, x-f.view.x, y-f.view.y)
		h.docked = true
		return h
	}
	return hit{}
}

// modalOf returns the picker or dialog state draws over the list or the nib
// view, and the row of it drawn on each line, with ok false for a state that
// draws none. rowAt is nil for a dialog without rows.
func (a *App) modalOf(state viewState) (view string, rowAt func(line int) (int, bool), ok bool) {
	var p interface {
		View() string
		modal() pickerModalConfig
	}
	switch state {
	case viewList, viewExpanded:
		return "", nil, false
	case viewCreateModal:
		return a.createModal.View(), nil, true
	case viewConfirmDialog:
		return a.confirmDialog.View(a.width, a.height), nil, true
	case viewPalette:
		return a.palette.View(), nil, true
	case viewParentPicker:
		p = a.parentPicker
	case viewStatusPicker:
		p = a.statusPicker
	case viewTypePicker, viewCreateTypePicker:
		p = a.typePicker
	case viewPriorityPicker:
		p = a.priorityPicker
	case viewEstimatePicker:
		p = a.estimatePicker
	case viewModifyPicker:
		p = a.modifyPicker
	case viewAssignPicker:
		p = a.assignPicker
	case viewColumnPicker:
		p = a.columnPicker
	case viewViewPicker:
		p = a.viewPicker
	case viewFilterPicker:
		p = a.filterPicker
	case viewFilterValuesPicker:
		p = a.filterValues
	case viewBlockingPicker:
		p = a.blockingPicker
	}
	if p == nil {
		return "", nil, false
	}
	return p.View(), func(line int) (int, bool) { return pickerModalRowAt(p.modal(), line) }, true
}

// handleMouse acts on a left click or a turn of the wheel at the cell hitAt
// reads. Other buttons, releases and motion do nothing, and so does any mouse
// message while the mouse is off.
func (a *App) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !a.mouse {
		return nil
	}
	m := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if m.Button == tea.MouseLeft {
			return a.click(a.hitAt(m.X, m.Y))
		}
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelDown:
			return a.turnWheel(a.hitAt(m.X, m.Y), actDown)
		case tea.MouseWheelUp:
			return a.turnWheel(a.hitAt(m.X, m.Y), actUp)
		}
	}
	return nil
}

// click moves the list cursor to a clicked row, focuses a clicked pane or
// region, and chooses a clicked picker row. A click anywhere else, outside
// an open modal included, does nothing.
//
// While a box being typed in takes the keys, a click leaves them there: it
// moves the list or picker cursor to a clicked row without choosing it, and a
// click on the docked view does nothing.
func (a *App) click(h hit) tea.Cmd {
	typing := slices.Contains(textInputs, a.keyContext())
	switch h.area {
	case hitListRow:
		a.docked.active = false
		a.list.list.Select(h.row)
		a.followCursor()
	case hitList:
		a.docked.active = false
	case hitNibView, hitNibBody, hitNibRelations, hitNibRelation:
		if typing {
			return nil
		}
		view := &a.expanded
		if h.docked {
			view = &a.docked
			view.active = true
		}
		switch h.area {
		case hitNibBody:
			view.focus = regionBody
		case hitNibRelations:
			view.focus = regionRelations
		case hitNibRelation:
			view.focus, view.cursor = regionRelations, h.row
		}
	case hitModalRow:
		return a.updateActiveView(clickRowMsg{row: h.row, choose: !typing})
	}
	return nil
}

// turnWheel moves the pane under the pointer as act, actUp or actDown, does:
// it moves the list cursor, a nib view's relations cursor or an open picker's
// cursor, or scrolls a nib view's body. The focus stays where it is.
func (a *App) turnWheel(h hit, act actionName) tea.Cmd {
	switch h.area {
	case hitList, hitListRow:
		var cmd tea.Cmd
		a.list, cmd = a.list.Update(actionMsg{name: act})
		a.followCursor()
		return cmd
	case hitNibView, hitNibBody, hitNibRelations, hitNibRelation:
		view := &a.expanded
		if h.docked {
			view = &a.docked
		}
		if h.area == hitNibRelations || h.area == hitNibRelation {
			view.moveCursor(act)
		} else if act == actDown {
			view.body.ScrollDown(view.body.MouseWheelDelta)
		} else {
			view.body.ScrollUp(view.body.MouseWheelDelta)
		}
	case hitModal, hitModalRow:
		if _, rowAt, _ := a.modalOf(a.state); rowAt != nil {
			return a.updateActiveView(actionMsg{name: act})
		}
	}
	return nil
}
