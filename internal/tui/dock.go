package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// placement is where the user config asks the docked nib view to go.
type placement string

const (
	placementAuto  placement = "auto"
	placementRight placement = "right"
	placementBelow placement = "below"
)

// placements is every placement, in the order z P cycles through them.
var placements = []placement{placementAuto, placementRight, placementBelow}

// nibViewSettings is the user config's tui.nib_view: whether the nib view is
// docked beside or below the list, and where.
type nibViewSettings struct {
	shown     bool
	placement placement
}

const (
	// nibViewMinWidth is the narrowest terminal the nib view docks right in:
	// calculatePaneWidths then leaves the list 39 cells and the view 40.
	nibViewMinWidth = 80
	// autoRightMinWidth is the narrowest terminal that auto docks the view
	// right in before it tries below: the list then keeps 59 cells beside the
	// 80 of the view.
	autoRightMinWidth = 140
	// belowMinHeight is the shortest terminal the nib view docks below in: with
	// a one-line footer the list keeps 14 lines, 11 of them rows, and the view
	// gets 15.
	belowMinHeight = 30
	// RightPaneMaxWidth is the widest the view docked right gets, the
	// 80-column text convention.
	RightPaneMaxWidth = 80
)

// dockAt returns where a view placed p docks in a terminal width x height, or
// "" and the footer's note that says why it does not fit. auto docks right in
// a wide terminal, below in a tall one, and right in any other that has room.
func dockAt(p placement, width, height int) (placement, string) {
	fitsRight, fitsBelow := width >= nibViewMinWidth, height >= belowMinHeight
	switch p {
	case placementRight:
		if fitsRight {
			return placementRight, ""
		}
		return "", fmt.Sprintf("Nib view hidden: needs %d columns", nibViewMinWidth)
	case placementBelow:
		if fitsBelow {
			return placementBelow, ""
		}
		return "", fmt.Sprintf("Nib view hidden: needs %d rows", belowMinHeight)
	}
	switch {
	case width >= autoRightMinWidth:
		return placementRight, ""
	case fitsBelow:
		return placementBelow, ""
	case fitsRight:
		return placementRight, ""
	}
	return "", fmt.Sprintf("Nib view hidden: needs %d columns or %d rows", nibViewMinWidth, belowMinHeight)
}

// calculatePaneWidths returns the widths of the list and of a view docked right.
func calculatePaneWidths(totalWidth int) (int, int) {
	rightWidth := RightPaneMaxWidth
	if totalWidth-rightWidth < 40 {
		rightWidth = totalWidth - 40
	}
	leftWidth := totalWidth - rightWidth - 1 // 1 for separator
	return leftWidth, rightWidth
}

// paneSize is the cells one pane of the screen is drawn in.
type paneSize struct {
	width, height int
}

// dockSizes divides width x height, the screen above the footer, between the
// list and a view docked at p, which is right or below. A view docked right
// takes the width calculatePaneWidths gives it, behind a divider. Docked
// below, the list keeps the top half and the view takes the rest. The view's
// top rule is the rule between them.
func dockSizes(p placement, width, height int) (list, view paneSize) {
	if p == placementBelow {
		list = paneSize{width: width, height: height / 2}
		return list, paneSize{width: width, height: height - list.height}
	}
	listWidth, viewWidth := calculatePaneWidths(width)
	return paneSize{width: listWidth, height: height}, paneSize{width: viewWidth, height: height}
}

// dockPlacement returns where the nib view is docked now, or "" when it is not
// shown or does not fit.
func (a *App) dockPlacement() placement {
	if !a.nibView.shown {
		return ""
	}
	p, _ := dockAt(a.nibView.placement, a.width, a.height)
	return p
}

// dockNote returns the footer's note that says why a shown nib view is not
// docked, or "" when it is docked or not shown.
func (a *App) dockNote() string {
	if !a.nibView.shown {
		return ""
	}
	_, note := dockAt(a.nibView.placement, a.width, a.height)
	return note
}

// dockFocused reports whether the docked nib view has the focus, so the keys
// typed in the list go to it.
func (a *App) dockFocused() bool {
	return a.docked.active && a.dockPlacement() != ""
}

// syncDock sizes the list pane and the docked view to the dock as it is now,
// and shows the nib under the list cursor in the view. A view that is not
// docked leaves the list the whole screen and gives the focus to the list.
func (a *App) syncDock() {
	p := a.dockPlacement()
	if p == "" {
		a.docked.active = false
		a.list.setPane(0, 0)
		return
	}
	list, _ := dockSizes(p, a.width, a.height-a.list.footerHeight())
	a.list.setPane(list.width, list.height)
	a.syncDocked()
}

// syncDocked shows the nib under the list cursor in the docked view, or the
// pinned nib; see dockTarget.
func (a *App) syncDocked() {
	a.showDocked(a.dockTarget())
}

// followCursor shows the nib under the list cursor, or the pinned nib, in the
// docked view when it shows another, and does nothing while the view is not
// docked.
func (a *App) followCursor() {
	id := a.dockTarget()
	if a.dockPlacement() == "" {
		return
	}
	if shown := a.docked.nib; (shown == nil && id == "") || (shown != nil && shown.ID == id) {
		return
	}
	a.showDocked(id)
}

// showDocked shows the nib id in the docked view, or the placeholder when id
// is "" or does not resolve. It does nothing while the view is not docked.
// The view keeps its focus, so a reload that replaces the nib leaves the keys
// where they were.
func (a *App) showDocked(id string) {
	p := a.dockPlacement()
	if p == "" {
		return
	}
	var n *nib.Nib
	if id != "" {
		n, _ = a.backend.GetNib(context.Background(), id)
	}
	_, size := dockSizes(p, a.width, a.height-a.list.footerHeight())
	if n == nil || a.docked.nib == nil || a.docked.nib.ID != n.ID {
		active, focus := a.docked.active, a.docked.focus
		a.docked = newNibViewModel(n, a.backend, a.config, a.display, size.width, size.height)
		a.docked.active, a.docked.focus = active, focus
	}
	a.docked.docked = true
	a.docked.width, a.docked.height = size.width, size.height
	a.docked = a.docked.withNib(n)
}

// cycleDockFocus moves the focus to the next stop, or with forward false the
// previous one: the list, the docked view's relations and its body. The
// relations are a stop only while the view draws them.
func (a *App) cycleDockFocus(forward bool) {
	a.followCursor()
	type stop struct {
		docked bool
		region nibRegion
	}
	stops := []stop{{}}
	if a.docked.relationsShown() {
		stops = append(stops, stop{docked: true, region: regionRelations})
	}
	stops = append(stops, stop{docked: true, region: regionBody})
	at := stop{}
	if a.dockFocused() {
		at = stop{docked: true, region: a.docked.focused()}
	}
	i := slices.Index(stops, at)
	if forward {
		i++
	} else {
		i += len(stops) - 1
	}
	next := stops[i%len(stops)]
	a.docked.active = next.docked
	if next.docked {
		a.docked.focus = next.region
	}
}

// toggleNibView shows or hides the docked nib view, and saves the choice as
// tui.nib_view.shown; see saveNibViewSetting.
func (a *App) toggleNibView() tea.Cmd {
	a.nibView.shown = !a.nibView.shown
	a.syncDock()
	return a.saveNibViewSetting("shown", a.nibView.shown, "")
}

// cycleNibViewPlacement moves the docked nib view to the next placement, and
// saves it as tui.nib_view.placement; see saveNibViewSetting.
func (a *App) cycleNibViewPlacement() tea.Cmd {
	i := slices.Index(placements, a.nibView.placement)
	a.nibView.placement = placements[(i+1)%len(placements)]
	a.syncDock()
	saved := "Nib view placement: " + string(a.nibView.placement)
	if note := a.dockNote(); note != "" {
		saved += ". " + note
	}
	return a.saveNibViewSetting("placement", string(a.nibView.placement), saved)
}

// saveNibViewSetting saves value as tui.nib_view.<key> in the user config, and
// shows saved in the status line when it is not "". A failed save, such as one
// the user config refuses, leaves the setting for the session, with a warning.
// After a save over a file that changed since the TUI read it, the TUI reads
// the file again, and the command it returns reloads the nibs.
func (a *App) saveNibViewSetting(key string, value any, saved string) tea.Cmd {
	const lead = "The nib view setting applies until you quit: "
	if a.settingsSource == "" {
		a.reportFailure(lead + "the OS gives no place for a user config")
		return nil
	}
	written, changed, err := config.SetUserSetting(a.settingsSource, a.userSnapshot, []string{"tui", "nib_view", key}, value)
	if err != nil {
		a.reportFailure(lead + err.Error())
		return nil
	}
	a.userSnapshot = written
	if saved != "" {
		a.list.statusMessage, a.list.statusKind = saved, statusOK
	}
	if !changed {
		return nil
	}
	a.applyUserLayer(config.LoadUserLayerFrom(a.settingsSource))
	a.syncDock()
	return a.list.loadNibs
}

// renderDocked draws the list with the nib view docked at p beside or below
// it, above a full-width footer.
func (a *App) renderDocked(p placement) string {
	// The footer may be several lines tall.
	footer := a.list.Footer()
	contentHeight := a.height - max(1, lipgloss.Height(footer))
	listSize, viewSize := dockSizes(p, a.width, contentHeight)
	listPane := a.list.pane(listSize.width, listSize.height)
	viewPane := a.docked.render(viewSize.width, viewSize.height)
	if p == placementBelow {
		return listPane + "\n" + viewPane + "\n" + footer
	}
	divider := strings.TrimSuffix(strings.Repeat(ruleStyle.Render(dividerGlyph(a.display.ascii()))+"\n", max(1, contentHeight)), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, listPane, divider, viewPane) + "\n" + footer
}

// renderList draws the list, with the nib view docked when it is.
func (a *App) renderList() string {
	if p := a.dockPlacement(); p != "" {
		return a.renderDocked(p)
	}
	return a.list.View()
}
