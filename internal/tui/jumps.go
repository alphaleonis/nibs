package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/alphaleonis/nibs/internal/nib"
)

// maxJumps is the most jumps the history keeps. A jump past it drops the
// oldest.
const maxJumps = 100

// jump is one entry of the jump history: what the nib view showed, and the
// row the list cursor was on, before a followed link or a go-to. One history
// serves the docked and the expanded view, and the list.
type jump struct {
	nib string // the nib's id
	row string // a row id; see rowIDOf
	// The view's relations cursor, as relationEntry.key names it, its body's
	// scroll offset and its size, or zero values when no view showed the nib.
	relation string
	scroll   int
	size     paneSize
}

// pin is a nib the nib view shows in place of the one under the list cursor,
// because the list has no row for it, and the row the cursor was on then. The
// zero pin pins nothing.
//
// A pin lasts while the focus stays in a nib view and the cursor on that row;
// see dockTarget. One whose nib gains a row ends with the cursor on that row;
// see syncPin. So whenever the focus is in the list, the docked view shows the
// cursor's nib.
type pin struct {
	nib string // the nib's id
	row string // a row id; see rowIDOf
}

// viewOnScreen returns the nib view on screen, or nil when there is none.
func (a *App) viewOnScreen() *nibViewModel {
	switch {
	case a.state == viewExpanded:
		return &a.expanded
	case a.dockPlacement() != "":
		return &a.docked
	}
	return nil
}

// here returns the jump back to what the screen shows now: the nib the nib
// view on screen shows, or the nib under the list cursor when no view is on
// screen, and the cursor's row.
func (a *App) here() jump {
	j := jump{row: a.list.focusedRowID(), nib: a.list.focusedID()}
	view := a.viewOnScreen()
	if view == nil {
		return j
	}
	j.nib = ""
	if view == &a.docked {
		// The docked view catches up with the cursor when an Update ends, so
		// it can lag behind a cursor move made earlier in this one.
		a.followCursor()
	}
	if n := view.nib; n != nil {
		j.nib, j.scroll = n.ID, view.body.YOffset()
		j.size = paneSize{width: view.width, height: view.height}
		if es := view.entries(); view.cursor < len(es) {
			j.relation = es[view.cursor].key()
		}
	}
	return j
}

// pushJump adds j to the history, unless it names no nib.
func (a *App) pushJump(j jump) {
	if j.nib == "" {
		return
	}
	a.jumps = append(a.jumps, j)
	if over := len(a.jumps) - maxJumps; over > 0 {
		a.jumps = slices.Delete(a.jumps, 0, over)
	}
}

// follow shows the nib id in the nib view on screen and puts the list cursor
// on its row, unfolding the rows above it, after adding a jump back to what
// was shown. When the list has no row for id, the cursor and the filter stay,
// the view shows the nib with notInListNote, and a docked view takes the
// focus, which the pin needs. With no nib view on screen such a nib has
// nowhere to go, so the status line says so and nothing changes.
func (a *App) follow(id string) {
	n, err := a.backend.GetNib(context.Background(), id)
	if err == nil && n == nil {
		err = errors.New("not found")
	}
	if err != nil {
		a.reportFailure(fmt.Sprintf("Could not show %s: %v", a.display.id(id), err))
		return
	}
	if a.refuseOffList(id) {
		return
	}
	a.pushJump(a.here())
	a.list.reveal(id)
	a.pinNib(n)
	a.show(n)
}

// refuseOffList reports whether no nib view is on screen to show the nib id,
// which the list has no row for, and then says so in the status line.
func (a *App) refuseOffList(id string) bool {
	if a.viewOnScreen() != nil || a.list.hasRow(id) {
		return false
	}
	a.list.statusMessage, a.list.statusKind = a.display.id(id)+" is not in this list", statusWarn
	return true
}

// goToParent follows the link to the parent of the nib the nib view shows, or
// in the list of the nib under the cursor; see follow.
func (a *App) goToParent() {
	n := a.list.focusedNib()
	switch {
	case a.state == viewExpanded:
		n = a.expanded.nib
	case a.dockFocused():
		n = a.docked.nib
	}
	if n != nil && n.Parent != "" {
		a.follow(n.Parent)
	}
}

// jumpBack removes the latest jump from the history and returns the list
// cursor and the nib view to it, with the view's relations cursor as it was.
// The body scrolls back to the jump's offset when the view has the size it had
// then, and starts at the top otherwise, because the offset counts lines
// wrapped at that size. A jump to a nib deleted since is skipped. A jump to a nib the
// list has no row for stays in the history while no nib view is on screen to
// show it, and the status line says so; see refuseOffList.
func (a *App) jumpBack() {
	for len(a.jumps) > 0 {
		j := a.jumps[len(a.jumps)-1]
		n, err := a.backend.GetNib(context.Background(), j.nib)
		if err != nil || n == nil {
			a.jumps = a.jumps[:len(a.jumps)-1]
			continue
		}
		if a.refuseOffList(n.ID) {
			return
		}
		a.jumps = a.jumps[:len(a.jumps)-1]
		if !a.list.reveal(n.ID) && !a.list.reveal(j.row) {
			a.list.restoreCursor(j.row)
		}
		a.pinNib(n)
		a.show(n)
		if view := a.viewOnScreen(); view != nil {
			scroll := 0
			if (paneSize{width: view.width, height: view.height}) == j.size {
				scroll = j.scroll
			}
			view.restore(j.relation, scroll)
		}
		return
	}
}

// dropDeletedJumps drops the jumps and the pin whose nib the list's last load
// did not hold.
func (a *App) dropDeletedJumps() {
	a.jumps = slices.DeleteFunc(a.jumps, func(j jump) bool { return a.list.byID[j.nib] == nil })
	if a.list.byID[a.pinned.nib] == nil {
		a.pinned = pin{}
	}
}

// pinNib has the nib view show n in place of the nib under the list cursor,
// or drops the pin when n is the cursor's nib or nil. A docked view takes the
// focus, so the pin lasts; see pin.
func (a *App) pinNib(n *nib.Nib) {
	a.pinned = pin{}
	if n == nil || n.ID == a.list.focusedID() {
		return
	}
	a.pinned = pin{nib: n.ID, row: a.list.focusedRowID()}
	if a.state == viewList && a.dockPlacement() != "" {
		a.docked.active = true
	}
}

// show shows n in the nib view on screen, which keeps its focus region.
func (a *App) show(n *nib.Nib) {
	if a.state != viewExpanded {
		a.syncDocked()
		return
	}
	focus := a.expanded.focus
	a.expanded = a.newNibView(n)
	a.expanded.focus = focus
}

// dockTarget returns the id of the nib the docked view shows: the pinned nib
// while the focus is in a nib view and the list cursor on the row it was
// pinned at, and the cursor's nib otherwise. It drops a pin that no longer
// holds.
func (a *App) dockTarget() string {
	focused := a.expandedOnScreen() || a.dockFocused()
	if a.pinned.nib != "" && focused && a.list.focusedRowID() == a.pinned.row {
		return a.pinned.nib
	}
	a.pinned = pin{}
	return a.list.focusedID()
}

// syncPin drops a pin that no longer holds, and ends one whose nib the list
// has a row for again by putting the cursor on that row. The docked view then
// shows the result.
func (a *App) syncPin() {
	a.dockTarget()
	if a.pinned.nib != "" && a.list.hasRow(a.pinned.nib) {
		a.list.reveal(a.pinned.nib)
		a.pinned = pin{}
	}
	a.followCursor()
}

// noteListMembership sets each nib view's notInList from the list's rows as
// they are now.
func (a *App) noteListMembership() {
	for _, m := range []*nibViewModel{&a.expanded, &a.docked} {
		notInList := m.nib != nil && !a.list.hasRow(m.nib.ID)
		if notInList != m.notInList {
			m.notInList = notInList
			m.syncBody()
		}
	}
}
