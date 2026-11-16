package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/bodytemplate"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/updatecheck"
	"github.com/atotto/clipboard"
	"gopkg.in/yaml.v3"
)

type viewState int

const (
	viewList viewState = iota
	viewExpanded
	viewTagPicker
	viewParentPicker
	viewStatusPicker
	viewTypePicker
	viewBlockingPicker
	viewPriorityPicker
	viewEstimatePicker
	viewModifyPicker
	viewAssignPicker
	viewColumnPicker
	viewViewPicker
	viewCreateModal
	viewCreateTypePicker
	viewConfirmDialog
	viewPalette
)

// nibsChangedMsg is sent when nibs change on disk (via file watcher)
type nibsChangedMsg struct{}

type updateCheckMsg struct {
	available bool
	latest    string
}

// checkForUpdateCmd reports whether a newer release exists. A check that is
// gated off or fails reports available=false.
func checkForUpdateCmd(version string) tea.Cmd {
	return func() tea.Msg {
		res, ok := updatecheck.NewChecker(version).Check(context.Background())
		return updateCheckMsg{available: ok && res.UpdateAvailable, latest: res.Latest}
	}
}

// cursorChangedMsg is sent when the list cursor moves to a different nib.
// It carries no id: by the time it arrives the cursor may have moved again,
// so the handler reads the cursor itself.
type cursorChangedMsg struct{}

type openTagPickerMsg struct{}

type tagSelectedMsg struct {
	tag string
}

type clearFilterMsg struct{}

type copyNibIDMsg struct {
	ids []string
}

// clipboardWriteAll is a variable so tests can make the copy fail on every
// platform; clipboard.Unsupported only takes effect on Unix.
var clipboardWriteAll = clipboard.WriteAll

// reorderMsg makes the writes of plan. focusID is the row to select after the
// reload.
type reorderMsg struct {
	plan    reorderPlan
	focusID string
}

type reorderRefusedMsg struct {
	reason string
}

type openEditorMsg struct {
	nibID   string
	nibPath string
}

// editorFinishedMsg ends an $EDITOR session. started reports whether the editor
// process ran; do not infer it from err, which also carries the errors of
// releasing and restoring the terminal.
type editorFinishedMsg struct {
	err     error
	started bool
}

type openParentPickerMsg struct {
	nibIDs        []string
	nibTitle      string   // the nib's title, or "N selected nibs"
	nibTypes      []string // filters the eligible parents
	currentParent string   // set only for a single nib
}

// App is the main TUI application model
type App struct {
	state          viewState
	list           listModel
	expanded       nibViewModel // the nib view at full screen
	docked         nibViewModel // the nib view beside or below the list
	tagPicker      tagPickerModel
	parentPicker   parentPickerModel
	statusPicker   statusPickerModel
	typePicker     typePickerModel
	blockingPicker blockingPickerModel
	priorityPicker priorityPickerModel
	estimatePicker estimatePickerModel
	modifyPicker   modifyPickerModel
	assignPicker   assignPickerModel
	columnPicker   columnPickerModel
	viewPicker     viewPickerModel
	createModal    createModalModel
	confirmDialog  confirmDialog
	palette        paletteModel
	history        []nibViewModel // the expanded views that links were followed from, the latest last
	backend        Backend
	config         *config.Config
	keys           keymap
	display        display
	// ignored lists the entries of the user config's tui block that were
	// ignored, and settingsSource the file they are in, or "" for none.
	ignored        []ignoredEntry
	settingsSource string
	// userSnapshot is the user config file as the TUI last read or wrote it.
	userSnapshot config.UserConfigSnapshot
	width        int
	height       int

	// The keys typed so far of a sequence such as "g g", or "" when none are.
	pendingKey string

	// The view drawn behind a modal, restored when it closes.
	previousState viewState

	// Whether the nib view is docked, and where; see dockAt for where it fits.
	nibView nibViewSettings

	// The $EDITOR session in progress: the nib, and its file's mtime at launch.
	editingNibID      string
	editingNibModTime time.Time

	version string // running binary version, for the update check

	// The state file the view and the filter query are saved to, or nil to
	// save them nowhere.
	memory *projectMemory
}

// New creates the TUI application. It opens on Tree with no filter query, and
// saves neither; newSession opens it on what the store's state file holds.
//
// It applies the tui block of user over the default settings, as loadSettings
// does. Icons are ascii when the terminal does not display UTF-8, whatever the
// block sets. With ascii icons the list and the nib view draw everything but
// nib text in ASCII too. A user config that did not load, or a tui entry
// that was ignored, opens the TUI with a warning in the status line.
// version is the running binary version, for the update-available indicator.
// The App reads every nib through displayBackend, the rendering boundary for
// nib text.
func New(backend Backend, cfg *config.Config, user config.UserLayer, version string) *App {
	backend = displayBackend{inner: backend}
	a := &App{
		state:   viewList,
		backend: backend,
		config:  cfg,
		list:    newListModel(backend, cfg),
		version: version,
	}
	a.applyUserLayer(user)
	return a
}

// newSession is New opened on the view and the filter query that the state
// file of backend's store remembers, or on Milestones with no query when it
// remembers none. The App saves both to that file each time either changes.
// A warning from openProjectMemory is added to the status line after any
// warning New put there.
func newSession(backend Backend, cfg *config.Config, user config.UserLayer, version string) *App {
	a := New(backend, cfg, user, version)
	memory, view, warning := openProjectMemory(backend.Root())
	a.memory = memory
	a.list.restoreView(view)
	if warning != "" {
		if a.list.statusMessage != "" {
			warning = a.list.statusMessage + "; " + lowerFirst(warning)
		}
		a.list.statusMessage, a.list.statusKind = warning, statusWarn
	}
	return a
}

// lowerFirst returns s with its first rune in lower case.
func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}

// applyUserLayer applies the tui block of user as New describes, and warns in
// the status line when user did not load or has ignored entries.
func (a *App) applyUserLayer(user config.UserLayer) {
	var tui *yaml.Node
	if user.Config != nil {
		tui = &user.Config.TUI
	}
	s := loadSettings(defaultActions, tui, user.Path)
	s.display.prefix = a.config.Nibs.Prefix
	if !terminalUTF8() {
		s.display.icons = iconsASCII
	}
	a.keys, a.display, a.ignored, a.settingsSource = s.keys, s.display, s.ignored, s.source
	a.userSnapshot = user.Snapshot
	a.list.setDisplay(s.display)
	a.list.setColumns(s.columns)
	a.nibView = s.nibView
	if user.Failure != nil {
		a.list.statusMessage = "Warning: " + user.Failure.Message()
		a.list.statusKind = statusWarn
	} else if n := len(s.ignored); n > 0 {
		a.list.statusMessage = ignoredSettingsWarning(n, s.keys.firstKey(actHelp))
		a.list.statusKind = statusWarn
	}
}

// chooseColumns shows columns after the title in view, and saves them as
// tui.columns.<view> in the user config. A failed save, such as one the user
// config refuses, leaves the choice for the session, with a warning. After a save over a file
// that changed since the TUI read it, the TUI reads the file again, and the
// command it returns reloads the nibs.
func (a *App) chooseColumns(view string, columns []columnName) tea.Cmd {
	a.list.setViewColumns(view, columns)
	if a.settingsSource == "" {
		a.reportFailure("The columns apply until you quit: the OS gives no place for a user config")
		return nil
	}
	names := []string{string(colTitle)}
	for _, name := range columns {
		names = append(names, string(name))
	}
	written, changed, err := config.SetUserSetting(a.settingsSource, a.userSnapshot, []string{"tui", "columns", view}, names)
	if err != nil {
		a.reportFailure("The columns apply until you quit: " + err.Error())
		return nil
	}
	a.userSnapshot = written
	a.list.statusMessage, a.list.statusKind = "Saved the columns to the user config", statusOK
	if !changed {
		return nil
	}
	a.applyUserLayer(config.LoadUserLayerFrom(a.settingsSource))
	a.syncDock()
	return a.list.loadNibs
}

// ignoredSettingsWarning is the status line's warning that n entries of the
// user config's tui block were ignored. help is the key that opens the
// palette, which lists them.
func ignoredSettingsWarning(n int, help string) string {
	noun, them := "settings", "them"
	if n == 1 {
		noun, them = "setting", "it"
	}
	return fmt.Sprintf("Ignored %d TUI %s in the user config; %s lists %s", n, noun, help, them)
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.list.Init(), checkForUpdateCmd(a.version))
}

// keyContext reports where a key goes now.
func (a *App) keyContext() keyContext {
	inList := func(l list.Model, ctx keyContext) keyContext {
		if l.FilterState() == list.Filtering {
			return ctxFilterInput
		}
		return ctx
	}
	switch a.state {
	case viewList:
		if a.dockFocused() {
			return nibViewContext(a.docked)
		}
		return inList(a.list.list, ctxList)
	case viewExpanded:
		return nibViewContext(a.expanded)
	case viewTagPicker:
		return inList(a.tagPicker.list, ctxTagPicker)
	case viewParentPicker:
		return inList(a.parentPicker.list, ctxParentPicker)
	case viewStatusPicker:
		return inList(a.statusPicker.list, ctxStatusPicker)
	case viewPriorityPicker:
		return inList(a.priorityPicker.list, ctxPriorityPicker)
	case viewEstimatePicker:
		return inList(a.estimatePicker.list, ctxEstimatePicker)
	case viewBlockingPicker:
		return inList(a.blockingPicker.list, ctxBlockingPicker)
	case viewTypePicker, viewCreateTypePicker:
		return ctxTypePicker
	case viewModifyPicker:
		return ctxModifyPicker
	case viewAssignPicker:
		return inList(a.assignPicker.list, a.assignPicker.field.ctx)
	case viewColumnPicker:
		return ctxColumnPicker
	case viewViewPicker:
		return ctxViewPicker
	case viewCreateModal:
		return ctxTextInput
	case viewConfirmDialog:
		return ctxConfirm
	case viewPalette:
		return ctxPalette
	}
	return ctxList
}

// nibViewContext is the context of the region of m that keys go to.
func nibViewContext(m nibViewModel) keyContext {
	if m.focused() == regionRelations {
		return ctxNibRelations
	}
	return ctxNibBody
}

// actionState snapshots what the actions' Hint, Label and Ready read.
func (a *App) actionState() actionState {
	st := actionState{
		ctx:      a.keyContext(),
		prefix:   a.pendingKey,
		selected: len(a.list.selectedNibs),
		filtered: a.list.hasActiveFilter() || a.list.list.IsFiltered(),
		docked:   a.state == viewList && a.dockPlacement() != "",
	}
	view := &a.expanded
	switch {
	case a.state == viewExpanded:
	case a.state == viewList && a.dockFocused():
		view = &a.docked
	default:
		_, st.hasNib = a.list.list.SelectedItem().(nibItem)
		return st
	}
	// The nib view acts on its own nib only, never on the list's marks.
	st.selected = 0
	st.hasNib = view.nib != nil
	st.relationsShown = view.relationsShown()
	st.onDocument = view.selectedRelation() == nil
	return st
}

// handleKey runs the action the key completes, keeps a prefix pending, or hands
// the key to a focused text box or a picker's row accelerators. Any other key
// does nothing.
func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	ctx := a.keyContext()
	pending := a.pendingKey
	a.pendingKey = ""
	key := msg.String()
	act, next := resolveKey(a.keys.actions, ctx, pending, key)
	switch {
	case act != nil:
		return a.runAction(act)
	case next != "":
		a.pendingKey = next
		return nil
	case pending != "":
		// The key ended a sequence nothing is bound to.
		return nil
	case ctx == ctxFilterInput && (key == driveFilterAccept.String() || key == driveFilterCancel.String()):
		// listKeyMap binds these keys so the filter actions can drive a list. A
		// raw one would leave the filter even with input.accept or input.cancel
		// bound to another key.
		return nil
	case ctx == ctxFilterInput, ctx == ctxTextInput, ctx == ctxPalette:
		return a.updateActiveView(msg)
	case slices.Contains(allPickers, ctx):
		return a.updateActiveView(acceleratorMsg{key: key})
	}
	return nil
}

// runAction runs act in the active view, unless act is not ready.
func (a *App) runAction(act *action) tea.Cmd {
	if act.Ready != nil {
		if ok, _ := act.Ready(a.actionState()); !ok {
			return nil
		}
	}
	switch act.Name {
	case actQuit, actForceQuit:
		return tea.Quit
	case actHelp:
		a.palette = newPaletteModel(a.keys, a.ignored, a.settingsSource, a.state, a.actionState(), a.width, a.height)
		a.state = viewPalette
		return a.palette.Init()
	case actPaletteClose:
		a.state = a.palette.origin
		return nil
	case actPaletteRun:
		return a.runPaletteEntry()
	case actTagFilter:
		return a.showView(openTagPickerMsg{})
	case actNibView:
		return a.toggleNibView()
	case actNibPlacement:
		return a.cycleNibViewPlacement()
	case actNibScrollDown, actNibScrollUp:
		a.followCursor()
		if act.Name == actNibScrollDown {
			a.docked.scrollBody(actDown)
		} else {
			a.docked.scrollBody(actUp)
		}
		return nil
	case actFocusNext, actFocusPrev:
		if a.state == viewList {
			a.cycleDockFocus(act.Name == actFocusNext)
			return nil
		}
	case actLeave, actJumpBack:
		// The docked view has no views of its own to step back through.
		if a.state == viewList && a.dockFocused() {
			a.docked.active = false
			return nil
		}
	case actColumns:
		a.columnPicker = newColumnPickerModel(a.keys.actions, levelViews[a.list.level], a.list.columns, a.width, a.height)
		a.openModal(viewColumnPicker)
		return nil
	case actViewPick:
		a.viewPicker = newViewPickerModel(a.keys, a.list.level, a.list.prevLevel, a.width, a.height)
		a.openModal(viewViewPicker)
		return nil
	}
	if msg := a.viewMsg(act.Name); msg != nil {
		return a.showView(msg)
	}
	return a.updateActiveView(actionMsg{name: act.Name})
}

// viewMsg returns the message that shows another view for act in the list or
// the nib view on screen, or nil when act shows none there.
func (a *App) viewMsg(act actionName) tea.Msg {
	switch a.state {
	case viewList:
		if a.dockFocused() {
			return a.docked.viewMsg(act)
		}
		return a.list.viewMsg(act)
	case viewExpanded:
		return a.expanded.viewMsg(act)
	}
	return nil
}

// showView applies msg, which changes the view on screen, before Update
// returns. A key typed right after the one that sent msg then reaches the view
// msg shows, even when it arrives before any command's message.
func (a *App) showView(msg tea.Msg) tea.Cmd {
	_, cmd := a.update(msg)
	return cmd
}

// openModal shows state, a picker or dialog, over the list or the nib view.
// Over another modal it keeps the view under that modal, so canceling returns
// to the list or the nib view.
func (a *App) openModal(state viewState) {
	if a.state == viewList || a.state == viewExpanded {
		a.previousState = a.state
	}
	a.state = state
}

// runPaletteEntry closes the palette and runs its highlighted action in the
// view the palette was opened from, as the action's key would have. An entry
// that is not ready leaves the palette open.
func (a *App) runPaletteEntry() tea.Cmd {
	e, ok := a.palette.highlighted()
	if !ok || !e.ready {
		return nil
	}
	a.state = a.palette.origin
	return a.runAction(e.act)
}

// Update handles msg, then saves the view and the filter query when either
// changed.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := a.update(msg)
	a.remember()
	return model, cmd
}

// remember saves the view and the applied filter query to the state file when
// they differ from what it holds. A query still being typed is not saved.
func (a *App) remember() {
	if a.memory == nil {
		return
	}
	view, ok := a.list.shownView()
	if !ok {
		return
	}
	if err := a.memory.save(view); err != nil {
		a.reportFailure("Could not remember the view and the filter: " + err.Error())
	}
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.syncDock()

		// The view under the palette is drawn too, so it takes the new size.
		if a.state == viewPalette {
			a.state = a.palette.origin
			a.updateActiveView(msg)
			a.state = viewPalette
		}

		// The expanded view under a modal is drawn too. On screen, it takes the
		// size as the active view below.
		if a.state != viewExpanded {
			a.expanded, _ = a.expanded.Update(msg)
		}

	case tea.KeyPressMsg:
		// The key acts on the frame it was typed at, and clearing a status
		// message can bring the nib view's relations back on screen.
		a.expanded.focus = a.expanded.focused()
		a.docked.focus = a.docked.focused()
		// Clear status messages on any keypress
		a.list.statusMessage = ""
		a.list.statusKind = statusOK
		a.expanded.statusMessage = ""
		a.expanded.statusKind = statusOK
		return a, a.handleKey(msg)

	case cursorChangedMsg:
		// msg names the nib under the cursor when it was sent, which a later
		// move may have left.
		a.followCursor()
		return a, nil

	case nibsLoadedMsg:
		a.list, cmd = a.list.Update(msg)
		a.syncDocked()
		return a, cmd

	case updateCheckMsg:
		if msg.available {
			a.list.updateAvailable = true
			a.list.updateLatest = msg.latest
		}
		return a, nil

	case nibsChangedMsg:
		if a.expandedOnScreen() {
			updatedNib, err := a.backend.GetNib(context.Background(), a.expanded.nib.ID)
			if err != nil || updatedNib == nil {
				// The nib is gone or unreadable, so any overlay on it closes too.
				a.state = viewList
				a.history = nil
			} else {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		return a, a.list.loadNibs

	case openTagPickerMsg:
		tags := a.collectTagsWithCounts()
		if len(tags) == 0 {
			return a, nil
		}
		a.tagPicker = newTagPickerModel(a.keys.actions, tags, a.width, a.height)
		a.state = viewTagPicker
		return a, a.tagPicker.Init()

	case tagSelectedMsg:
		a.state = viewList
		a.list.setTagFilter(msg.tag)
		return a, a.list.loadNibs

	case openParentPickerMsg:
		// A type that cannot take a parent has nothing to pick.
		for _, nibType := range msg.nibTypes {
			if !nibtypes.CanHaveParent(nibType) {
				return a, nil
			}
		}
		a.parentPicker = newParentPickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.nibTypes, msg.currentParent, a.backend, a.config, a.width, a.height)
		a.openModal(viewParentPicker)
		return a, a.parentPicker.Init()

	case closeParentPickerMsg:
		// Nibs may have changed while the picker was open.
		a.state = a.previousState
		return a, a.list.loadNibs

	case openStatusPickerMsg:
		a.statusPicker = newStatusPickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentStatus, a.config, a.width, a.height)
		a.openModal(viewStatusPicker)
		return a, a.statusPicker.Init()

	case closeStatusPickerMsg:
		// Nibs may have changed while the picker was open.
		a.state = a.previousState
		return a, a.list.loadNibs

	case statusSelectedMsg:
		var errs []error
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Status: &msg.status,
			})
			if err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		clear(a.list.selectedNibs)
		if a.state == viewExpanded && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures("Status change", "nib", errs)
		return a, a.list.loadNibs

	case openCreateTypePickerMsg:
		// For creation: no nibIDs, and every type is valid.
		a.typePicker = newTypePickerModel(a.keys.actions, nil, "", msg.defaultType, nil, a.width, a.height)
		a.openModal(viewCreateTypePicker)
		return a, a.typePicker.Init()

	case openTypePickerMsg:
		a.typePicker = newTypePickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentType, msg.validTypes, a.width, a.height)
		a.openModal(viewTypePicker)
		return a, a.typePicker.Init()

	case closeTypePickerMsg:
		a.state = a.previousState
		return a, a.list.loadNibs

	case typeSelectedMsg:
		// In the create flow the type opens the create modal instead.
		if a.state == viewCreateTypePicker {
			return a, func() tea.Msg {
				return createTypeSelectedMsg{nibType: msg.nibType}
			}
		}
		var errs []error
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Type: &msg.nibType,
			})
			if err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		clear(a.list.selectedNibs)
		if a.state == viewExpanded && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures("Type change", "nib", errs)
		return a, a.list.loadNibs

	case createTypeSelectedMsg:
		a.createModal = newCreateModalModel(a.keys.actions, msg.nibType, a.config, a.width, a.height)
		a.state = viewCreateModal
		return a, a.createModal.Init()

	case openPriorityPickerMsg:
		a.priorityPicker = newPriorityPickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentPriority, a.width, a.height)
		a.openModal(viewPriorityPicker)
		return a, a.priorityPicker.Init()

	case closePriorityPickerMsg:
		// Nibs may have changed while the picker was open.
		a.state = a.previousState
		return a, a.list.loadNibs

	case prioritySelectedMsg:
		var errs []error
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Priority: graphql.OmittableOf(&msg.priority),
			})
			if err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		clear(a.list.selectedNibs)
		if a.state == viewExpanded && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures("Priority change", "nib", errs)
		return a, a.list.loadNibs

	case openModifyPickerMsg:
		a.modifyPicker = newModifyPickerModel(a.keys.actions, msg.targets, a.backend, a.width, a.height)
		a.openModal(viewModifyPicker)
		return a, nil

	case closeModifyPickerMsg:
		a.state = a.previousState
		return a, nil

	case openAssignPickerMsg:
		a.assignPicker = newAssignPickerModel(a.keys.actions, msg.field, msg.targets, a.backend, a.config, a.width, a.height)
		a.openModal(viewAssignPicker)
		return a, nil

	case closeAssignPickerMsg:
		a.state = a.previousState
		return a, a.list.loadNibs

	case assignChosenMsg:
		var errs []error
		for _, nibID := range msg.nibIDs {
			var input model.UpdateNibInput
			msg.field.set(&input, msg.value)
			if _, err := a.backend.UpdateNib(context.Background(), nibID, input); err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		clear(a.list.selectedNibs)
		if a.state == viewExpanded && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures(msg.field.title+" change", "nib", errs)
		return a, a.list.loadNibs

	case columnsChosenMsg:
		a.state = a.previousState
		return a, a.chooseColumns(msg.view, msg.columns)

	case closeColumnPickerMsg:
		a.state = a.previousState
		return a, nil

	case viewChosenMsg:
		a.state = a.previousState
		prev := a.list.focusedRowID()
		a.list.showLevel(msg.level)
		return a, a.list.cursorChangedCmd(prev)

	case closeViewPickerMsg:
		a.state = a.previousState
		return a, nil

	case modifyChosenMsg:
		// The field's picker opens over the view m was typed in, and returns there.
		a.state = a.previousState
		return a.Update(msg.open)

	case openEstimatePickerMsg:
		a.estimatePicker = newEstimatePickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentEstimate, a.width, a.height)
		a.openModal(viewEstimatePicker)
		return a, a.estimatePicker.Init()

	case closeEstimatePickerMsg:
		a.state = a.previousState
		return a, a.list.loadNibs

	case estimateSelectedMsg:
		var errs []error
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Estimate: graphql.OmittableOf(&msg.estimate),
			})
			if err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		clear(a.list.selectedNibs)
		if a.state == viewExpanded && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures("Estimate change", "nib", errs)
		return a, a.list.loadNibs

	case openBlockingPickerMsg:
		a.blockingPicker = newBlockingPickerModel(a.keys.actions, msg.nibID, msg.nibTitle, msg.currentBlocking, a.backend, a.config, a.width, a.height)
		a.openModal(viewBlockingPicker)
		return a, a.blockingPicker.Init()

	case closeBlockingPickerMsg:
		// Nibs may have changed while the picker was open.
		a.state = a.previousState
		return a, a.list.loadNibs

	case blockingConfirmedMsg:
		// Both loops collect into one errs: the footer holds one message, and a
		// second report would overwrite the first.
		var errs []error
		for _, targetID := range msg.toAdd {
			_, err := a.backend.AddBlocking(context.Background(), msg.nibID, targetID)
			if err != nil {
				errs = append(errs, err)
			}
		}
		for _, targetID := range msg.toRemove {
			_, err := a.backend.RemoveBlocking(context.Background(), msg.nibID, targetID)
			if err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		if a.state == viewExpanded {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibID)
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures("Blocking change", "link", errs)
		return a, a.list.loadNibs

	case reorderMsg:
		// Set before loadNibs runs so the reloaded list selects the row.
		a.list.selectByID = msg.focusID
		for i, w := range msg.plan.writes {
			// A later write is placed against an earlier one, so a failure stops
			// the rest.
			if _, err := a.backend.ReorderNib(context.Background(), w.id, w.afterID, w.beforeID, nil, msg.plan.scope); err != nil {
				a.list.statusKind = statusWarn
				a.list.selectByID = ""
				if i == 0 {
					a.list.statusMessage = fmt.Sprintf("Reorder failed: %v", err)
					return a, nil
				}
				a.list.statusMessage = fmt.Sprintf("Reorder partly applied (%d of %d nibs moved), list reloaded: %v", i, len(msg.plan.writes), err)
				return a, a.list.loadNibs
			}
		}
		return a, a.list.loadNibs

	case reorderRefusedMsg:
		a.list.statusMessage = msg.reason
		a.list.statusKind = statusWarn
		return a, nil

	case openConfirmMsg:
		a.confirmDialog = msg.dialog
		a.confirmDialog.table = a.keys.actions
		a.openModal(viewConfirmDialog)
		return a, nil

	case confirmActionMsg:
		var errs []string
		for _, nibID := range msg.nibIDs {
			var err error
			if msg.action == "archive" {
				err = a.backend.ArchiveNib(context.Background(), nibID)
			} else {
				err = a.backend.DeleteNib(context.Background(), nibID)
			}
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", nibID, err))
			}
		}
		a.state = viewList
		if len(errs) > 0 {
			a.list.statusMessage = fmt.Sprintf("%s failed for %d nib(s)", msg.action, len(errs))
			a.list.statusKind = statusWarn
		}
		return a, a.list.loadNibs

	case cancelConfirmMsg:
		a.state = a.previousState
		return a, nil

	case closeCreateModalMsg:
		a.state = a.previousState
		return a, nil

	case nibCreatedMsg:
		draftStatus := "draft"
		nibType := msg.nibType
		if nibType == "" {
			nibType = a.config.GetDefaultType()
		}
		input := model.CreateNibInput{
			Title:  msg.title,
			Status: &draftStatus,
			Type:   &nibType,
		}
		if tmpl := bodytemplate.BodyTemplate(nibType); tmpl != "" {
			input.Body = &tmpl
		}
		var selectedNib *nib.Nib
		if item, ok := a.list.list.SelectedItem().(nibItem); ok {
			selectedNib = item.nib
		}
		parentID, afterID := inferParent(nibType, selectedNib)
		if parentID != "" {
			input.Parent = &parentID
		}
		if afterID != "" {
			input.AfterID = &afterID
		}
		createdNib, err := a.backend.CreateNib(context.Background(), input)
		if err != nil {
			a.state = a.previousState
			a.list.statusMessage = fmt.Sprintf("Create failed: %v", err)
			a.list.statusKind = statusWarn
			return a, nil
		}
		a.state = viewList
		return a, tea.Batch(
			a.list.loadNibs,
			func() tea.Msg {
				return openEditorMsg{nibID: createdNib.ID, nibPath: createdNib.Path}
			},
		)

	case openEditorMsg:
		editor := getEditor()
		fullPath := filepath.Join(a.backend.Root(), msg.nibPath)

		a.editingNibID = msg.nibID
		if info, err := os.Stat(fullPath); err == nil {
			a.editingNibModTime = info.ModTime()
		}

		c := exec.Command(editor, fullPath)
		return a, tea.ExecProcess(c, editorFinished(c))

	case editorFinishedMsg:
		// The editor never started, so nothing was written: report the launch
		// failure instead of a write-back.
		if msg.err != nil && !msg.started {
			a.reportFailure(editorLaunchFailure(msg.err))
			a.editingNibID = ""
			a.editingNibModTime = time.Time{}
			return a, nil
		}

		// A non-zero exit is not a failure (`:cq` in vi exits non-zero); the
		// write-back depends only on whether the file changed.
		if a.editingNibID != "" {
			if err := a.recordExternalEdit(a.editingNibID, a.editingNibModTime); err != nil {
				a.reportFailure(editorWriteRefusal(a.editingNibID, err))
			}
			// Cleared whether or not the store accepted the edit.
			a.editingNibID = ""
			a.editingNibModTime = time.Time{}
		}
		return a, nil

	case parentSelectedMsg:
		var parentID *string
		if msg.parentID != "" {
			parentID = &msg.parentID
		}
		var errs []error
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.SetParent(context.Background(), nibID, parentID, nil)
			if err != nil {
				errs = append(errs, err)
			}
		}
		a.state = a.previousState
		clear(a.list.selectedNibs)
		if a.state == viewExpanded && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.expanded = a.expanded.withNib(updatedNib)
			}
		}
		a.reportMutationFailures("Parent change", "nib", errs)
		return a, a.list.loadNibs

	case clearFilterMsg:
		a.list.clearFilter()
		return a, a.list.loadNibs

	case copyNibIDMsg:
		var statusMsg string
		statusMsgKind := statusOK
		text := strings.Join(msg.ids, ", ")
		if err := clipboardWriteAll(text); err != nil {
			statusMsg = fmt.Sprintf("Failed to copy: %v", err)
			statusMsgKind = statusWarn
		} else if len(msg.ids) == 1 {
			statusMsg = fmt.Sprintf("Copied %s to clipboard", msg.ids[0])
		} else {
			statusMsg = fmt.Sprintf("Copied %d nib IDs to clipboard", len(msg.ids))
		}

		switch a.state {
		case viewList:
			a.list.statusMessage = statusMsg
			a.list.statusKind = statusMsgKind
		case viewExpanded:
			a.expanded.statusMessage = statusMsg
			a.expanded.statusKind = statusMsgKind
		}

		return a, nil

	case selectNibMsg:
		if a.state == viewExpanded {
			a.history = append(a.history, a.expanded)
		}
		a.state = viewExpanded
		a.expanded = a.newNibView(msg.nib)
		return a, nil

	case backToListMsg:
		if len(a.history) == 0 {
			return a, a.returnToList()
		}
		a.expanded = a.history[len(a.history)-1]
		a.history = a.history[:len(a.history)-1]
		// The terminal may have been resized since this view was pushed.
		a.expanded, _ = a.expanded.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
		return a, nil

	case leaveNibViewMsg:
		a.history = nil
		return a, a.returnToList()
	}

	// Everything not handled above goes to the active view.
	return a, a.updateActiveView(msg)
}

// updateActiveView hands msg to the view on screen.
func (a *App) updateActiveView(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.state {
	case viewList:
		if _, ok := msg.(actionMsg); ok && a.dockFocused() {
			a.docked, cmd = a.docked.Update(msg)
			break
		}
		a.list, cmd = a.list.Update(msg)
		a.followCursor()
	case viewExpanded:
		a.expanded, cmd = a.expanded.Update(msg)
	case viewTagPicker:
		a.tagPicker, cmd = a.tagPicker.Update(msg)
	case viewParentPicker:
		a.parentPicker, cmd = a.parentPicker.Update(msg)
	case viewStatusPicker:
		a.statusPicker, cmd = a.statusPicker.Update(msg)
	case viewTypePicker, viewCreateTypePicker:
		a.typePicker, cmd = a.typePicker.Update(msg)
	case viewPriorityPicker:
		a.priorityPicker, cmd = a.priorityPicker.Update(msg)
	case viewEstimatePicker:
		a.estimatePicker, cmd = a.estimatePicker.Update(msg)
	case viewModifyPicker:
		var chosen tea.Msg
		a.modifyPicker, chosen = a.modifyPicker.Update(msg)
		if chosen != nil {
			cmd = a.showView(chosen)
		}
	case viewAssignPicker:
		a.assignPicker, cmd = a.assignPicker.Update(msg)
	case viewColumnPicker:
		a.columnPicker, cmd = a.columnPicker.Update(msg)
	case viewViewPicker:
		a.viewPicker, cmd = a.viewPicker.Update(msg)
	case viewBlockingPicker:
		a.blockingPicker, cmd = a.blockingPicker.Update(msg)
	case viewCreateModal:
		a.createModal, cmd = a.createModal.Update(msg)
	case viewConfirmDialog:
		a.confirmDialog, cmd = a.confirmDialog.Update(msg)
	case viewPalette:
		a.palette, cmd = a.palette.Update(msg)
	}
	return cmd
}

func (a *App) collectTagsWithCounts() []tagWithCount {
	nibs, _ := a.backend.ListNibs(context.Background(), nil)
	tagCounts := make(map[string]int)
	for _, b := range nibs {
		for _, tag := range b.Tags {
			tagCounts[tag]++
		}
	}

	tags := make([]tagWithCount, 0, len(tagCounts))
	for tag, count := range tagCounts {
		tags = append(tags, tagWithCount{tag: tag, count: count})
	}

	return tags
}

// View renders the current screen in the alt screen.
func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	return v
}

// render produces the screen content for the active state.
func (a *App) render() string {
	hints := hintRow(a.keys.actions, a.actionState())
	a.list.hints = hints
	a.list.dockNote = a.dockNote()
	a.expanded.hints = hints
	return a.renderState(a.state)
}

// renderState draws state, which is the active state or the one the palette
// was opened from.
func (a *App) renderState(state viewState) string {
	switch state {
	case viewList:
		return a.renderList()
	case viewExpanded:
		return a.expanded.View()
	case viewTagPicker:
		return a.tagPicker.View()
	case viewParentPicker:
		return a.parentPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewStatusPicker:
		return a.statusPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewTypePicker, viewCreateTypePicker:
		return a.typePicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewPriorityPicker:
		return a.priorityPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewEstimatePicker:
		return a.estimatePicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewModifyPicker:
		return a.modifyPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewAssignPicker:
		return a.assignPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewColumnPicker:
		return a.columnPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewViewPicker:
		return a.viewPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewBlockingPicker:
		return a.blockingPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewCreateModal:
		return a.createModal.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewConfirmDialog:
		return a.confirmDialog.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewPalette:
		return a.palette.ModalView(a.renderState(a.palette.origin), a.width, a.height)
	}
	return ""
}

// expandedOnScreen reports whether the expanded nib view is drawn, on its own
// or under the palette or a modal, the same way render finds the view
// underneath.
func (a *App) expandedOnScreen() bool {
	state := a.state
	if state == viewPalette {
		state = a.palette.origin
	}
	switch state {
	case viewList, viewExpanded, viewTagPicker:
		return state == viewExpanded
	}
	return a.previousState == viewExpanded
}

// getBackgroundView returns the view to show behind modal pickers
func (a *App) getBackgroundView() string {
	switch a.previousState {
	case viewList:
		return a.renderList()
	case viewExpanded:
		return a.expanded.View()
	default:
		return a.list.View()
	}
}

// newNibView returns the expanded view of n, with an empty footer.
func (a *App) newNibView(n *nib.Nib) nibViewModel {
	return newNibViewModel(n, a.backend, a.config, a.display, a.width, a.height)
}

// returnToList shows the list, which missed any resize while it was not on
// screen.
func (a *App) returnToList() tea.Cmd {
	a.state = viewList
	var cmd tea.Cmd
	a.list, cmd = a.list.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	return cmd
}

// reportMutationFailures reports a picker's failed mutations in the footer of
// the view the app returned to. Call it after any rebuild of the expanded
// view, because a new view starts with an empty footer.
//
// One failure is shown with its reason, several as a count. unit names what one
// error is about: "nib", or "link" for the blocking picker, which collects one
// error per link.
func (a *App) reportMutationFailures(action, unit string, errs []error) {
	if len(errs) == 0 {
		return
	}
	message := fmt.Sprintf("%s failed for %d %s(s)", action, len(errs), unit)
	if len(errs) == 1 {
		message = fmt.Sprintf("%s failed: %v", action, errs[0])
	}
	a.reportFailure(message)
}

// reportFailure shows an already-composed failure message in the footer of the
// view on screen. Call it after any rebuild of the expanded view.
//
// Report every failed write. Do not report a failed read that only refreshes
// what is on screen.
func (a *App) reportFailure(message string) {
	if a.state == viewExpanded {
		a.expanded.statusMessage = message
		a.expanded.statusKind = statusWarn
		return
	}
	a.list.statusMessage = message
	a.list.statusKind = statusWarn
}

// recordExternalEdit takes an $EDITOR session's file back into the store by
// re-reading the nib and bumping its updated_at. This is a write, and the store
// can refuse it. Nothing is written unless the file's mtime is after since.
//
// A failed lookup is returned too: after `nibs config set-prefix` in another
// process, id may no longer resolve.
func (a *App) recordExternalEdit(id string, since time.Time) error {
	n, err := a.backend.GetNib(context.Background(), id)
	if err != nil {
		return err
	}
	if n == nil {
		return fmt.Errorf("%s is no longer in the store", id)
	}
	info, err := os.Stat(filepath.Join(a.backend.Root(), n.Path))
	if err != nil {
		return &nibFileUnreadableError{err: err}
	}
	if !info.ModTime().After(since) {
		return nil
	}
	return a.backend.ReloadAfterEdit(id)
}

// nibFileUnreadableError is a write-back whose nib file could not be stat'd, so
// its message cannot say the user's text is in that file.
type nibFileUnreadableError struct{ err error }

func (e *nibFileUnreadableError) Error() string { return e.err.Error() }

func (e *nibFileUnreadableError) Unwrap() error { return e.err }

// editorWriteRefusal describes an $EDITOR session the store did not record.
// The lead says what to do and comes first, because an overflowing footer is
// cut from the end.
//
// A failed stat gets a lead that claims neither that the text is safe nor that
// it is lost: the file may have been renamed while the editor was open.
func editorWriteRefusal(id string, err error) string {
	var unreadable *nibFileUnreadableError
	if errors.As(err, &unreadable) {
		return editorFileUnreadableLead + fmt.Sprintf(" The file recorded for %s could not be read: %v", id, err)
	}
	return editorTextSafeLead + fmt.Sprintf(" The store did not accept the edit to %s: %v", id, err)
}

// editorFinished builds the tea.ExecProcess callback. started is c.ProcessState
// != nil, which holds only for a process that was started.
//
// Reading c does not race: bubbletea's `go p.Send(fn(err))` evaluates fn(err) in
// the goroutine that ran c.
func editorFinished(c *exec.Cmd) tea.ExecCallback {
	return func(err error) tea.Msg {
		return editorFinishedMsg{err: err, started: c.ProcessState != nil}
	}
}

// editorLaunchFailure describes an $EDITOR that never started. Nothing was
// written, so its lead points at the editor setting, not a restart.
func editorLaunchFailure(err error) string {
	return editorNotStartedLead + fmt.Sprintf(" The editor could not be started: %v", err)
}

// Leads of the editor failure messages; see editorWriteRefusal.
const (
	editorTextSafeLead       = "Your text is still in the file. Restart nibs to re-read the store."
	editorFileUnreadableLead = "Nothing was recorded; the file cannot be read. Restart nibs to re-read the store."
	editorNotStartedLead     = "Nothing was opened and nothing changed. Check $VISUAL and $EDITOR."
)

// getEditor returns the user's preferred editor using the fallback chain:
// $VISUAL -> $EDITOR -> vi -> nano
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if _, err := exec.LookPath("vi"); err == nil {
		return "vi"
	}
	return "nano"
}

// Run starts the TUI as newSession does, and forwards store changes to it
// until it exits. The parameters are as for New.
func Run(backend Backend, cfg *config.Config, user config.UserLayer, version string) error {
	app := newSession(backend, cfg, user, version)
	p := tea.NewProgram(app)

	if err := backend.StartWatching(); err != nil {
		return err
	}
	defer backend.StopWatching()

	stop := forwardStoreChanges(backend, func(msg tea.Msg) { p.Send(msg) })
	defer stop()

	_, err := p.Run()
	return err
}

// forwardStoreChanges sends a nibsChangedMsg through send each time backend
// reports a change to the nibs or to the declared areas, until stop is called.
func forwardStoreChanges(backend Backend, send func(tea.Msg)) (stop func()) {
	nibEvents, stopNibs := backend.Subscribe()
	areaEvents, stopAreas := backend.SubscribeAreas()
	for _, events := range []<-chan struct{}{nibEvents, areaEvents} {
		go func() {
			for range events {
				send(nibsChangedMsg{})
			}
		}()
	}
	return func() {
		stopNibs()
		stopAreas()
	}
}
