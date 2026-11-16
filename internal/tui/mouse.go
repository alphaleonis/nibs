package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

// doubleClickInterval is the longest time between the two clicks of a double
// click.
const doubleClickInterval = 500 * time.Millisecond

// leftClick is a left click that a second one can pair with into a double
// click; see App.pairClick. The zero leftClick is no click.
type leftClick struct {
	at   time.Time
	x, y int
	// pre is what the cell showed when the click came, and target what it
	// showed once the click had acted. settled is false until then; see
	// App.settleClick.
	pre     clickTarget
	target  clickTarget
	settled bool
	// swallow is whether a quick second click on this cell is ignored, because
	// it would act on something the user did not aim at. It is set once the
	// click changed what its cell shows, or up front for a picker row, which
	// closes on a later message.
	swallow bool
}

// clickTarget is what a cell shows, as far as a double click tells one thing
// on screen from another.
type clickTarget struct {
	state  viewState
	area   hitArea
	docked bool   // see hit.docked
	row    int    // see hit.row; zero for a list row, which rowID names
	rowID  string // a list row's id; see rowIDOf
	fold   bool   // see hit.fold
	nib    string // the id of the nib a nib view there shows
}

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

// handleMouse acts on a left click, a double click or a turn of the wheel at
// the cell hitAt reads. A second click that makes no double click, or whose
// double click finds its target no longer current, acts as a single click.
// Other buttons, releases and motion do nothing, and so does any mouse message
// while the mouse is off.
func (a *App) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !a.mouse {
		return nil
	}
	m := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			a.lastClick = leftClick{}
			return nil
		}
		at, h := a.now(), a.hitAt(m.X, m.Y)
		target := a.targetOf(h)
		switch first, pair := a.pairClick(at, m.X, m.Y, target); pair {
		case pairSwallowed:
			return nil
		case pairDouble:
			if cmd, acted := a.doubleClick(first); acted {
				return cmd
			}
		}
		a.lastClick = leftClick{at: at, x: m.X, y: m.Y, pre: target, swallow: h.area == hitModalRow}
		return a.click(h)
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

// targetOf returns what the cell of h shows now.
func (a *App) targetOf(h hit) clickTarget {
	t := clickTarget{state: a.state, area: h.area, docked: h.docked, row: h.row, fold: h.fold}
	if h.area == hitListRow {
		t.row, t.rowID = 0, h.rowID
	}
	if h.onNibView() {
		view := &a.expanded
		if h.docked {
			view = &a.docked
		}
		if view.nib != nil {
			t.nib = view.nib.ID
		}
	}
	return t
}

// settleClick runs after each message. A key, a wheel turn or a resize ends
// the latest click's chance to pair. So does a later change of the view state,
// unless the click swallows. After the click's own message, it stores what the
// cell shows, and the click swallows when its action changed that, the view
// state included.
func (a *App) settleClick(msg tea.Msg, before viewState) {
	c := &a.lastClick
	if c.at.IsZero() {
		return
	}
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseWheelMsg, tea.WindowSizeMsg:
		a.lastClick = leftClick{}
		return
	}
	if c.settled {
		if a.state != before && !c.swallow {
			a.lastClick = leftClick{}
		}
		return
	}
	c.target, c.settled = a.targetOf(a.hitAt(c.x, c.y)), true
	if c.target != c.pre {
		c.swallow = true
	}
}

// clickPair is how a left click pairs with the click before it.
type clickPair uint8

const (
	pairNone      clickPair = iota // a single click
	pairDouble                     // the second click of a double click
	pairSwallowed                  // a second click that does nothing; see leftClick.swallow
)

// pairClick returns how a left click at time at on cell (x, y), which shows
// target, pairs with the latest click, and that click's target. Two left
// clicks within doubleClickInterval pair when the first swallows and they
// share a cell, or when they hit one target: the same cell, or two cells of
// one list row. A click never makes a double click while a box being typed in
// takes the keys. pairClick ends the latest click's chance to pair.
func (a *App) pairClick(at time.Time, x, y int, target clickTarget) (clickTarget, clickPair) {
	first := a.lastClick
	a.lastClick = leftClick{}
	if !first.settled {
		return clickTarget{}, pairNone
	}
	if gap := at.Sub(first.at); gap < 0 || gap > doubleClickInterval {
		return clickTarget{}, pairNone
	}
	sameCell := x == first.x && y == first.y
	switch {
	case first.swallow && sameCell:
		return clickTarget{}, pairSwallowed
	case slices.Contains(textInputs, a.keyContext()):
		return clickTarget{}, pairNone
	case target != first.target || !sameCell && target.area != hitListRow:
		return clickTarget{}, pairNone
	}
	return first.target, pairDouble
}

// doubleClick acts on a double click on target, what the first click's cell
// showed once that click had acted as a single one. On a list row off its
// fold mark, or on the docked view, it expands the nib view as the open action
// does there. Anywhere else, a fold mark included, it does nothing more, so
// the second click repeats nothing.
//
// It reports false, and does nothing, when target is no longer current: the
// view state changed, the list cursor left the row, or the nib view shows
// another nib or is gone.
func (a *App) doubleClick(target clickTarget) (tea.Cmd, bool) {
	if a.state != target.state {
		return nil, false
	}
	var msg tea.Msg
	switch {
	case target.area == hitListRow && !target.fold:
		if a.list.focusedRowID() != target.rowID {
			return nil, false
		}
		msg = a.list.viewMsg(actOpen)
	case target.docked && isNibViewArea(target.area):
		if a.dockPlacement() == "" || a.docked.nib == nil || a.docked.nib.ID != target.nib {
			return nil, false
		}
		msg = a.docked.expandMsg()
	case isNibViewArea(target.area):
		if a.expanded.nib == nil || a.expanded.nib.ID != target.nib {
			return nil, false
		}
	}
	if msg == nil {
		return nil, true
	}
	return a.showView(msg), true
}

// onNibView reports whether h is in a nib view.
func (h hit) onNibView() bool { return isNibViewArea(h.area) }

// isNibViewArea reports whether area is part of a nib view.
func isNibViewArea(area hitArea) bool {
	switch area {
	case hitNibView, hitNibBody, hitNibRelations, hitNibRelation:
		return true
	}
	return false
}

// click moves the list cursor to a clicked row, folds or unfolds a row whose
// fold mark is clicked, focuses a clicked pane or region, follows a clicked
// relation as the open action does, and chooses a clicked picker row. A
// click anywhere else, outside an open modal included, does nothing.
//
// While a box being typed in takes the keys, a click leaves them there: it
// moves the list or picker cursor to a clicked row without choosing it, and a
// click on the docked view does nothing. A fold mark still folds then.
func (a *App) click(h hit) tea.Cmd {
	typing := slices.Contains(textInputs, a.keyContext())
	switch h.area {
	case hitListRow:
		a.docked.active = false
		if h.fold {
			a.list.toggleFold(h.rowID)
		} else {
			a.list.list.Select(h.row)
		}
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
			if msg := view.viewMsg(actOpen); msg != nil {
				return a.showView(msg)
			}
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
