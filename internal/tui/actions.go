package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// keyContext is where a key goes: one view, one part of a view, or a focused
// text box.
type keyContext uint8

const (
	ctxList keyContext = iota
	ctxNibLinks
	ctxNibBody
	ctxStatusPicker
	ctxTypePicker
	ctxParentPicker
	ctxBlockingPicker
	ctxPriorityPicker
	ctxEstimatePicker
	ctxTagPicker
	ctxModifyPicker
	ctxConfirm
	// ctxFilterInput is a list's filter while it is being typed.
	ctxFilterInput
	// ctxTextInput is any other focused text box, such as the create modal.
	ctxTextInput
	// ctxPalette is the ? overlay, whose query box takes every key no action
	// there is bound to.
	ctxPalette
	numKeyContexts
)

var keyContextNames = [numKeyContexts]string{
	ctxList:           "list",
	ctxNibLinks:       "nib view links",
	ctxNibBody:        "nib view body",
	ctxStatusPicker:   "status picker",
	ctxTypePicker:     "type picker",
	ctxParentPicker:   "parent picker",
	ctxBlockingPicker: "blocking picker",
	ctxPriorityPicker: "priority picker",
	ctxEstimatePicker: "estimate picker",
	ctxTagPicker:      "tag picker",
	ctxModifyPicker:   "modify picker",
	ctxConfirm:        "confirm dialog",
	ctxFilterInput:    "filter input",
	ctxTextInput:      "text input",
	ctxPalette:        "action palette",
}

func (c keyContext) String() string {
	if c < numKeyContexts {
		return keyContextNames[c]
	}
	return fmt.Sprintf("context %d", c)
}

// actionName is an action's config name, which also identifies it in dispatch.
type actionName string

const (
	actUp            actionName = "move.up"
	actDown          actionName = "move.down"
	actPageUp        actionName = "move.page_up"
	actPageDown      actionName = "move.page_down"
	actTop           actionName = "move.top"
	actBottom        actionName = "move.bottom"
	actFoldRow       actionName = "fold.row"
	actUnfoldRow     actionName = "unfold.row"
	actFoldSubtree   actionName = "fold.subtree"
	actUnfoldSubtree actionName = "unfold.subtree"
	actFoldAll       actionName = "fold.all"
	actUnfoldAll     actionName = "unfold.all"
	actFocusNext     actionName = "focus.next"
	actFocusPrev     actionName = "focus.previous"
	actOpen          actionName = "go.open"
	actLeave         actionName = "go.leave"
	actJumpBack      actionName = "go.back"
	actSelect        actionName = "nib.select"
	actCreate        actionName = "nib.create"
	actEdit          actionName = "nib.edit"
	actStatus        actionName = "nib.status"
	actType          actionName = "nib.type"
	actParent        actionName = "nib.parent"
	actBlocking      actionName = "nib.blocking"
	actPriority      actionName = "nib.priority"
	actEstimate      actionName = "nib.estimate"
	actModify        actionName = "nib.modify"
	actMoveUp        actionName = "nib.move_up"
	actMoveDown      actionName = "nib.move_down"
	actArchive       actionName = "nib.archive"
	actDelete        actionName = "nib.delete"
	actCopyID        actionName = "nib.copy_id"
	actConfirmYes    actionName = "confirm.yes"
	actConfirmNo     actionName = "confirm.no"
	actHideClosed    actionName = "view.hide_closed"
	actWideMode      actionName = "view.wide"
	actFilter        actionName = "filter.text"
	actFilterApply   actionName = "filter.apply"
	actTagFilter     actionName = "filter.tags"
	actInputAccept   actionName = "input.accept"
	actInputCancel   actionName = "input.cancel"
	actHelp          actionName = "app.help"
	actPaletteUp     actionName = "palette.up"
	actPaletteDown   actionName = "palette.down"
	actPaletteRun    actionName = "palette.run"
	actPaletteClose  actionName = "palette.close"
	actQuit          actionName = "app.quit"
	actForceQuit     actionName = "app.force_quit"
)

// actionState is what an action's Hint, Label and Ready read: the app as it is
// when a key arrives.
type actionState struct {
	ctx        keyContext
	prefix     string // the pending prefix, or "" when none is
	hasNib     bool   // a nib is under the list cursor, or the nib view shows one
	selected   int    // nibs marked in the list
	filtered   bool   // a tag or text filter narrows the list
	linksShown bool   // the nib view draws its links box
}

// action is one named thing a key can do. Dispatch reads the actions table, and
// nothing else maps keys.
type action struct {
	Name     actionName
	Group    string // Move, Edit, View, Go, Find or App
	Desc     string
	Contexts []keyContext
	// Keys are the default bindings. A sequence is space-separated ("g g").
	Keys []string
	// Hint reports whether the action is worth a footer hint now; nil means never.
	Hint func(actionState) bool
	// Label is the hint's text; nil means Desc.
	Label func(actionState) string
	// Ready reports whether the action can run now, and why not; nil means always.
	// A key bound to an action that is not ready does nothing.
	Ready func(actionState) (ok bool, why string)
}

// The context sets that several actions in the table share.
var (
	nibContexts   = []keyContext{ctxList, ctxNibLinks, ctxNibBody}
	nibViewCtx    = []keyContext{ctxNibLinks, ctxNibBody}
	listPickers   = []keyContext{ctxStatusPicker, ctxParentPicker, ctxBlockingPicker, ctxPriorityPicker, ctxEstimatePicker, ctxTagPicker}
	allPickers    = append([]keyContext{ctxTypePicker, ctxModifyPicker}, listPickers...)
	browsing      = slices.Concat(nibContexts, allPickers)
	everyContext  = allKeyContexts()
	filterTargets = slices.Concat([]keyContext{ctxList, ctxNibLinks}, listPickers)
	textInputs    = []keyContext{ctxFilterInput, ctxTextInput}
)

func allKeyContexts() []keyContext {
	all := make([]keyContext, numKeyContexts)
	for c := range numKeyContexts {
		all[c] = c
	}
	return all
}

func hintAlways(actionState) bool { return true }

// marking reports whether the list has nibs marked, which turns its hints to
// the actions that edit every marked nib.
func marking(st actionState) bool { return st.ctx == ctxList && st.selected > 0 }

func inNibView(st actionState) bool { return slices.Contains(nibViewCtx, st.ctx) }

func hintOnFocused(st actionState) bool { return st.ctx == ctxList && st.selected == 0 }

func hintOnFocusedOrNibView(st actionState) bool { return hintOnFocused(st) || inNibView(st) }

func hintBulkOrNibView(st actionState) bool { return marking(st) || inNibView(st) }

func hintOutsideMarking(st actionState) bool { return !marking(st) }

// hintLeave hints esc in the list only when it has a selection or a filter to
// clear, and never in the confirm dialog, whose n hint already says cancel.
func hintLeave(st actionState) bool {
	switch st.ctx {
	case ctxList:
		return st.selected > 0 || st.filtered
	case ctxConfirm:
		return false
	}
	return true
}

func hintFilter(st actionState) bool {
	switch st.ctx {
	case ctxList:
		return st.selected == 0 && !st.filtered
	case ctxNibLinks:
		return false
	}
	return true
}

func label(text string) func(actionState) string {
	return func(actionState) string { return text }
}

func labelOpen(st actionState) string {
	switch st.ctx {
	case ctxList:
		return "open"
	case ctxNibLinks:
		return "follow"
	case ctxBlockingPicker:
		return "apply"
	}
	return "choose"
}

func labelLeave(st actionState) string {
	switch {
	case marking(st):
		return "clear"
	case st.ctx == ctxList:
		return "clear filter"
	case inNibView(st):
		return "back"
	}
	return "cancel"
}

func labelAccept(st actionState) string {
	if st.ctx == ctxTextInput {
		return "create"
	}
	return "apply"
}

// readyOnNib needs a nib under the cursor, or the one the nib view shows.
func readyOnNib(st actionState) (bool, string) {
	if st.hasNib {
		return true, ""
	}
	return false, "no nib under the cursor"
}

// readyOnTargets needs marked nibs or a nib under the cursor.
func readyOnTargets(st actionState) (bool, string) {
	if st.hasNib || st.selected > 0 {
		return true, ""
	}
	return false, "no nib under the cursor or selected"
}

func readyToJumpBack(st actionState) (bool, string) {
	if st.ctx == ctxList {
		return false, "nothing to jump back to"
	}
	return true, ""
}

// actions is the keymap. Two actions may share a key only when no context has
// both active; keymapConflicts checks this.
var actions = []action{
	{Name: actUp, Group: "Move", Desc: "move up", Contexts: browsing, Keys: []string{"up", "k"}},
	{Name: actDown, Group: "Move", Desc: "move down", Contexts: browsing, Keys: []string{"down", "j"}},
	{Name: actPageUp, Group: "Move", Desc: "page up", Contexts: browsing, Keys: []string{"pgup"}},
	{Name: actPageDown, Group: "Move", Desc: "page down", Contexts: browsing, Keys: []string{"pgdown"}},
	{Name: actTop, Group: "Move", Desc: "go to the top", Contexts: browsing, Keys: []string{"g g", "home"}, Label: label("top")},
	{Name: actBottom, Group: "Move", Desc: "go to the bottom", Contexts: browsing, Keys: []string{"G", "end"}, Label: label("bottom")},
	{Name: actFoldRow, Group: "Move", Desc: "fold the row, or go to its parent", Contexts: []keyContext{ctxList}, Keys: []string{"left"}},
	{Name: actUnfoldRow, Group: "Move", Desc: "unfold the row", Contexts: []keyContext{ctxList}, Keys: []string{"right"}},
	{Name: actFoldSubtree, Group: "Move", Desc: "fold the row's subtree", Contexts: []keyContext{ctxList}, Keys: []string{"ctrl+left"}},
	{Name: actUnfoldSubtree, Group: "Move", Desc: "unfold the row's subtree", Contexts: []keyContext{ctxList}, Keys: []string{"ctrl+right"}},
	{Name: actFoldAll, Group: "Move", Desc: "fold every row", Contexts: []keyContext{ctxList}, Keys: []string{"<"}},
	{Name: actUnfoldAll, Group: "Move", Desc: "unfold every row", Contexts: []keyContext{ctxList}, Keys: []string{">"}},
	{Name: actFocusNext, Group: "Move", Desc: "focus the next pane", Contexts: nibViewCtx, Keys: []string{"tab"},
		Hint: func(st actionState) bool { return st.linksShown }, Label: label("switch")},
	{Name: actFocusPrev, Group: "Move", Desc: "focus the previous pane", Contexts: nibViewCtx, Keys: []string{"shift+tab"}},
	{Name: actPaletteUp, Group: "Move", Desc: "highlight the previous action", Contexts: []keyContext{ctxPalette}, Keys: []string{"up"}},
	{Name: actPaletteDown, Group: "Move", Desc: "highlight the next action", Contexts: []keyContext{ctxPalette}, Keys: []string{"down"}},

	{Name: actOpen, Group: "Go", Desc: "open", Contexts: slices.Concat([]keyContext{ctxList, ctxNibLinks}, allPickers), Keys: []string{"enter"},
		Hint: hintOutsideMarking, Label: labelOpen},
	{Name: actLeave, Group: "Go", Desc: "leave, or clear the selection or filter", Contexts: slices.Concat(browsing, []keyContext{ctxConfirm}), Keys: []string{"esc"},
		Hint: hintLeave, Label: labelLeave},
	{Name: actJumpBack, Group: "Go", Desc: "jump back", Contexts: nibContexts, Keys: []string{"backspace"}, Ready: readyToJumpBack},
	{Name: actPaletteRun, Group: "Go", Desc: "run the highlighted action", Contexts: []keyContext{ctxPalette}, Keys: []string{"enter"},
		Hint: hintAlways, Label: label("run")},
	{Name: actPaletteClose, Group: "Go", Desc: "close the palette", Contexts: []keyContext{ctxPalette}, Keys: []string{"esc"},
		Hint: hintAlways, Label: label("close")},

	{Name: actSelect, Group: "Edit", Desc: "select", Contexts: []keyContext{ctxList, ctxBlockingPicker}, Keys: []string{"space"},
		Hint: func(st actionState) bool { return st.ctx == ctxBlockingPicker }, Label: label("toggle")},
	{Name: actCreate, Group: "Edit", Desc: "create", Contexts: []keyContext{ctxList}, Keys: []string{"n"}, Hint: hintOnFocused},
	{Name: actEdit, Group: "Edit", Desc: "edit in $EDITOR", Contexts: nibContexts, Keys: []string{"e"},
		Hint: hintOnFocusedOrNibView, Label: label("edit"), Ready: readyOnNib},
	{Name: actStatus, Group: "Edit", Desc: "change status", Contexts: nibContexts, Keys: []string{"s"},
		Hint: hintBulkOrNibView, Label: label("status"), Ready: readyOnTargets},
	{Name: actType, Group: "Edit", Desc: "change type", Contexts: nibContexts, Keys: []string{"t"},
		Hint: marking, Label: label("type"), Ready: readyOnTargets},
	{Name: actParent, Group: "Edit", Desc: "set parent", Contexts: nibContexts, Keys: []string{"p"},
		Hint: marking, Label: label("parent"), Ready: readyOnTargets},
	{Name: actBlocking, Group: "Edit", Desc: "manage blocking", Contexts: nibContexts, Keys: []string{"b"}, Ready: readyOnNib},
	{Name: actPriority, Group: "Edit", Desc: "change priority", Contexts: nibContexts, Ready: readyOnTargets},
	{Name: actEstimate, Group: "Edit", Desc: "change estimate", Contexts: nibContexts, Ready: readyOnTargets},
	{Name: actModify, Group: "Edit", Desc: "modify a field", Contexts: nibContexts, Keys: []string{"m"},
		Hint: marking, Label: label("modify"), Ready: readyOnTargets},
	{Name: actMoveUp, Group: "Edit", Desc: "move up among siblings", Contexts: []keyContext{ctxList}, Keys: []string{"ctrl+up"}},
	{Name: actMoveDown, Group: "Edit", Desc: "move down among siblings", Contexts: []keyContext{ctxList}, Keys: []string{"ctrl+down"}},
	{Name: actArchive, Group: "Edit", Desc: "archive", Contexts: []keyContext{ctxList}, Keys: []string{"a"}, Ready: readyOnNib},
	{Name: actDelete, Group: "Edit", Desc: "delete", Contexts: []keyContext{ctxList}, Keys: []string{"delete"}, Ready: readyOnNib},
	{Name: actCopyID, Group: "Edit", Desc: "copy ID", Contexts: nibContexts, Keys: []string{"y"}, Ready: readyOnTargets},
	{Name: actConfirmYes, Group: "Edit", Desc: "confirm", Contexts: []keyContext{ctxConfirm}, Keys: []string{"y", "Y"}, Hint: hintAlways},
	{Name: actConfirmNo, Group: "Edit", Desc: "cancel", Contexts: []keyContext{ctxConfirm}, Keys: []string{"n", "N"}, Hint: hintAlways},

	{Name: actHideClosed, Group: "View", Desc: "hide or show closed nibs", Contexts: []keyContext{ctxList, ctxParentPicker}, Keys: []string{"z h"},
		Hint: func(st actionState) bool { return st.ctx == ctxParentPicker }, Label: label("toggle closed")},
	{Name: actWideMode, Group: "View", Desc: "wide mode", Contexts: []keyContext{ctxList}, Keys: []string{"W"}},

	{Name: actFilter, Group: "Find", Desc: "filter", Contexts: filterTargets, Keys: []string{"/"}, Hint: hintFilter},
	{Name: actTagFilter, Group: "Find", Desc: "filter by tag", Contexts: []keyContext{ctxList}, Keys: []string{"g t"}},
	{Name: actFilterApply, Group: "Find", Desc: "apply the filter", Contexts: []keyContext{ctxFilterInput}, Keys: []string{"up", "down"}},
	{Name: actInputAccept, Group: "Find", Desc: "accept", Contexts: textInputs, Keys: []string{"enter"}, Hint: hintAlways, Label: labelAccept},
	{Name: actInputCancel, Group: "Find", Desc: "cancel", Contexts: textInputs, Keys: []string{"esc"}, Hint: hintAlways},

	{Name: actHelp, Group: "App", Desc: "list and run actions", Contexts: browsing, Keys: []string{"?"}, Hint: hintAlways, Label: label("help")},
	{Name: actQuit, Group: "App", Desc: "quit", Contexts: browsing, Keys: []string{"q"},
		Hint: func(st actionState) bool { return slices.Contains(nibContexts, st.ctx) }},
	{Name: actForceQuit, Group: "App", Desc: "quit from anywhere", Contexts: everyContext, Keys: []string{"ctrl+c"}},
}

// actionGroups is the order the palette and the hint row list the groups in.
// Edit comes before Go so a hint row clipped to a narrow terminal keeps the
// edit keys.
var actionGroups = []string{"Move", "Edit", "Go", "View", "Find", "App"}

// actionByName returns the action named name, or nil when the table has none.
func actionByName(name actionName) *action {
	for i := range actions {
		if actions[i].Name == name {
			return &actions[i]
		}
	}
	return nil
}

// resolveKey looks up key, typed after the pending prefix, among the actions
// active in ctx. It returns the action the sequence completes, or the sequence
// to keep pending when it starts a longer binding. Both are zero when nothing
// in ctx is bound to it.
func resolveKey(table []action, ctx keyContext, pending, key string) (*action, string) {
	seq := key
	if pending != "" {
		seq = pending + " " + key
	}
	isPrefix := false
	for i := range table {
		a := &table[i]
		if !slices.Contains(a.Contexts, ctx) {
			continue
		}
		for _, binding := range a.Keys {
			if binding == seq {
				return a, ""
			}
			if strings.HasPrefix(binding, seq+" ") {
				isPrefix = true
			}
		}
	}
	if isPrefix {
		return nil, seq
	}
	return nil, ""
}

// keymapConflicts describes every key two actions share in one context, and
// every key that starts a longer sequence bound in the same context, which
// leaves the longer one unreachable.
func keymapConflicts(table []action) []string {
	var conflicts []string
	for ctx := range numKeyContexts {
		owner := map[string]actionName{}
		var bindings []string
		for _, a := range table {
			if !slices.Contains(a.Contexts, ctx) {
				continue
			}
			for _, binding := range a.Keys {
				if other, taken := owner[binding]; taken {
					conflicts = append(conflicts, fmt.Sprintf("%s: %q is bound to both %s and %s", ctx, binding, other, a.Name))
					continue
				}
				owner[binding] = a.Name
				bindings = append(bindings, binding)
			}
		}
		for _, short := range bindings {
			for _, long := range bindings {
				if strings.HasPrefix(long, short+" ") {
					conflicts = append(conflicts, fmt.Sprintf("%s: %q (%s) starts %q (%s)", ctx, short, owner[short], long, owner[long]))
				}
			}
		}
	}
	return conflicts
}

// actionMsg runs an action in the view it is sent to.
type actionMsg struct {
	name actionName
}

// acceleratorMsg is a key no action takes, offered to a picker to choose the
// row that shows it.
type acceleratorMsg struct {
	key string
}

// The keys the action handlers send a bubbles list to drive its filter.
// listKeyMap binds nothing else, so a list moves only when a handler moves it.
var (
	driveFilterStart  = tea.KeyPressMsg{Code: '/', Text: "/"}
	driveFilterAccept = tea.KeyPressMsg{Code: tea.KeyEnter}
	driveFilterCancel = tea.KeyPressMsg{Code: tea.KeyEscape}
)

func listKeyMap() list.KeyMap {
	return list.KeyMap{
		Filter:               key.NewBinding(key.WithKeys(driveFilterStart.String())),
		AcceptWhileFiltering: key.NewBinding(key.WithKeys(driveFilterAccept.String())),
		CancelWhileFiltering: key.NewBinding(key.WithKeys(driveFilterCancel.String())),
	}
}

// browseList applies a move or filter action to l. It reports false for any
// other action.
func browseList(l *list.Model, name actionName) (tea.Cmd, bool) {
	var drive tea.KeyPressMsg
	switch name {
	case actUp:
		l.CursorUp()
		return nil, true
	case actDown:
		l.CursorDown()
		return nil, true
	case actPageUp:
		l.PrevPage()
		return nil, true
	case actPageDown:
		l.NextPage()
		return nil, true
	case actTop:
		l.GoToStart()
		return nil, true
	case actBottom:
		l.GoToEnd()
		return nil, true
	case actFilter:
		drive = driveFilterStart
	case actInputAccept, actFilterApply:
		drive = driveFilterAccept
	case actInputCancel:
		drive = driveFilterCancel
	default:
		return nil, false
	}
	var cmd tea.Cmd
	*l, cmd = l.Update(drive)
	return cmd, true
}
