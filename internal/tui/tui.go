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

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	viewDetail
	viewTagPicker
	viewParentPicker
	viewStatusPicker
	viewTypePicker
	viewBlockingPicker
	viewPriorityPicker
	viewEstimatePicker
	viewModifyPicker
	viewCreateModal
	viewCreateTypePicker
	viewConfirmDialog
	viewPalette
)

const (
	// previewMinWidth is the narrowest terminal the nib view is drawn beside
	// the list in: calculatePaneWidths then leaves the list 39 cells and the
	// view 40.
	previewMinWidth   = 80
	RightPaneMaxWidth = 80 // the 80-column text convention
)

// calculatePaneWidths returns (leftWidth, rightWidth) for the two-column layout.
func calculatePaneWidths(totalWidth int) (int, int) {
	rightWidth := RightPaneMaxWidth
	if totalWidth-rightWidth < 40 {
		rightWidth = totalWidth - 40
	}
	leftWidth := totalWidth - rightWidth - 1 // 1 for separator
	return leftWidth, rightWidth
}

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

// cursorChangedMsg is sent when the list cursor moves to a different nib
type cursorChangedMsg struct {
	nibID string
}

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

type reorderNibMsg struct {
	nibID    string
	afterID  *string
	beforeID *string
	first    *bool
}

// reorderBlockMsg moves a contiguous block of siblings by reordering the one
// sibling it displaces: after the block's last item when moving up (afterID),
// before its first when moving down (beforeID). focusID is the row to select
// after the reload.
type reorderBlockMsg struct {
	displacedID string
	afterID     *string
	beforeID    *string
	focusID     string
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
	detail         detailModel
	preview        previewModel
	tagPicker      tagPickerModel
	parentPicker   parentPickerModel
	statusPicker   statusPickerModel
	typePicker     typePickerModel
	blockingPicker blockingPickerModel
	priorityPicker priorityPickerModel
	estimatePicker estimatePickerModel
	modifyPicker   modifyPickerModel
	createModal    createModalModel
	confirmDialog  confirmDialog
	palette        paletteModel
	history        []detailModel // stack of previous detail views for back navigation
	backend        Backend
	config         *config.Config
	keys           keymap
	display        display
	// ignored lists the entries of the user config's tui block that were
	// ignored, and settingsSource the file they are in, or "" for none.
	ignored        []ignoredEntry
	settingsSource string
	width          int
	height         int
	program        *tea.Program // receives watcher events; see Run

	// The keys typed so far of a sequence such as "g g", or "" when none are.
	pendingKey string

	// The view drawn behind a modal, restored when it closes.
	previousState viewState

	// Whether the nib view is shown beside the list. It is drawn only while the
	// terminal is at least previewMinWidth wide.
	nibViewShown bool

	// The $EDITOR session in progress: the nib, and its file's mtime at launch.
	editingNibID      string
	editingNibModTime time.Time

	version string // running binary version, for the update check
}

// New creates the TUI application.
//
// It applies the tui block of user over the default settings, as loadSettings
// does. Icons are ascii when the terminal does not display UTF-8, whatever the
// block sets. With ascii icons the list's rows and rules, and the nib view's
// link rows, are ASCII too. A user config that did not load, or a tui entry
// that was ignored, opens the TUI with a warning in the status line.
// version is the running binary version, for the update-available indicator.
func New(backend Backend, cfg *config.Config, user config.UserLayer, version string) *App {
	var tui *yaml.Node
	if user.Config != nil {
		tui = &user.Config.TUI
	}
	s := loadSettings(defaultActions, tui, user.Path)
	s.display.prefix = cfg.Nibs.Prefix
	if !terminalUTF8() {
		s.display.icons = iconsASCII
	}
	a := &App{
		state:          viewList,
		backend:        backend,
		config:         cfg,
		keys:           s.keys,
		display:        s.display,
		ignored:        s.ignored,
		settingsSource: s.source,
		list:           newListModel(backend, cfg),
		preview:        newPreviewModel(nil, 0, 0),
		version:        version,
	}
	a.list.setDisplay(s.display)
	if user.Failure != nil {
		a.list.statusMessage = "Warning: " + user.Failure.Message()
		a.list.statusKind = statusWarn
	} else if n := len(s.ignored); n > 0 {
		a.list.statusMessage = ignoredSettingsWarning(n, s.keys.firstKey(actHelp))
		a.list.statusKind = statusWarn
	}
	return a
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

// isTwoColumnMode reports whether the nib view is drawn beside the list.
func (a *App) isTwoColumnMode() bool {
	return a.nibViewShown && a.width >= previewMinWidth
}

// toggleNibView shows or hides the nib view beside the list. Shown in a
// terminal too narrow to draw it, it says so in the footer.
func (a *App) toggleNibView() {
	a.nibViewShown = !a.nibViewShown
	a.syncListPane()
	if a.nibViewShown && !a.isTwoColumnMode() {
		a.list.statusMessage = fmt.Sprintf("The nib view needs %d columns", previewMinWidth)
		a.list.statusKind = statusWarn
	}
}

// syncListPane tells the list the width its pane is drawn at.
func (a *App) syncListPane() {
	a.list.paneWidth = 0
	if a.isTwoColumnMode() {
		a.list.paneWidth, _ = calculatePaneWidths(a.width)
	}
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
		return inList(a.list.list, ctxList)
	case viewDetail:
		if a.detail.linksActive {
			return inList(a.detail.linkList, ctxNibLinks)
		}
		return ctxNibBody
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
	case viewCreateModal:
		return ctxTextInput
	case viewConfirmDialog:
		return ctxConfirm
	case viewPalette:
		return ctxPalette
	}
	return ctxList
}

// actionState snapshots what the actions' Hint, Label and Ready read.
func (a *App) actionState() actionState {
	st := actionState{
		ctx:      a.keyContext(),
		prefix:   a.pendingKey,
		selected: len(a.list.selectedNibs),
		filtered: a.list.hasActiveFilter() || a.list.list.IsFiltered(),
	}
	if a.state == viewDetail {
		st.hasNib = a.detail.nib != nil
		st.linksShown = a.detail.linksSection() != ""
	} else {
		_, st.hasNib = a.list.list.SelectedItem().(nibItem)
	}
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
		return func() tea.Msg { return openTagPickerMsg{} }
	case actNibView:
		a.toggleNibView()
		return nil
	}
	return a.updateActiveView(actionMsg{name: act.Name})
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

// Update handles messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.syncListPane()

		// The view under the palette is drawn too, so it takes the new size.
		if a.state == viewPalette {
			a.state = a.palette.origin
			a.updateActiveView(msg)
			a.state = viewPalette
		}

		if a.isTwoColumnMode() {
			_, rightWidth := calculatePaneWidths(a.width)
			a.preview.width = rightWidth
			a.preview.height = a.height - 2
		}

	case tea.KeyPressMsg:
		// Clear status messages on any keypress
		a.list.statusMessage = ""
		a.list.statusKind = statusOK
		a.detail.statusMessage = ""
		a.detail.statusKind = statusOK
		return a, a.handleKey(msg)

	case cursorChangedMsg:
		_, rightWidth := calculatePaneWidths(a.width)
		if msg.nibID != "" {
			nib, err := a.backend.GetNib(context.Background(), msg.nibID)
			if err == nil && nib != nil {
				a.preview = newPreviewModel(nib, rightWidth, a.height-2)
			}
		} else {
			a.preview = newPreviewModel(nil, rightWidth, a.height-2)
		}
		return a, nil

	case nibsLoadedMsg:
		a.list, cmd = a.list.Update(msg)
		_, rightWidth := calculatePaneWidths(a.width)
		if len(msg.items) == 0 {
			a.preview = newPreviewModel(nil, rightWidth, a.height-2)
		} else if item, ok := a.list.list.SelectedItem().(nibItem); ok {
			a.preview = newPreviewModel(item.nib, rightWidth, a.height-2)
		}
		return a, cmd

	case updateCheckMsg:
		if msg.available {
			a.list.updateAvailable = true
			a.list.updateLatest = msg.latest
		}
		return a, nil

	case nibsChangedMsg:
		if a.nibViewOnScreen() {
			updatedNib, err := a.backend.GetNib(context.Background(), a.detail.nib.ID)
			if err != nil || updatedNib == nil {
				// The nib is gone or unreadable, so any overlay on it closes too.
				a.state = viewList
				a.history = nil
			} else {
				// Carry the footer across the rebuild: a file changing on disk
				// does not dismiss a status message.
				message, kind := a.detail.statusMessage, a.detail.statusKind
				a.detail = a.initDetailModel(updatedNib)
				a.detail.statusMessage = message
				a.detail.statusKind = kind
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
		a.previousState = a.state
		a.parentPicker = newParentPickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.nibTypes, msg.currentParent, a.backend, a.config, a.width, a.height)
		a.state = viewParentPicker
		return a, a.parentPicker.Init()

	case closeParentPickerMsg:
		// Nibs may have changed while the picker was open.
		a.state = a.previousState
		return a, a.list.loadNibs

	case openStatusPickerMsg:
		a.previousState = a.state
		a.statusPicker = newStatusPickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentStatus, a.config, a.width, a.height)
		a.state = viewStatusPicker
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
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		a.reportMutationFailures("Status change", "nib", errs)
		return a, a.list.loadNibs

	case openCreateTypePickerMsg:
		a.previousState = a.state
		// For creation: no nibIDs, and every type is valid.
		a.typePicker = newTypePickerModel(a.keys.actions, nil, "", msg.defaultType, nil, a.width, a.height)
		a.state = viewCreateTypePicker
		return a, a.typePicker.Init()

	case openTypePickerMsg:
		a.previousState = a.state
		a.typePicker = newTypePickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentType, msg.validTypes, a.width, a.height)
		a.state = viewTypePicker
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
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		a.reportMutationFailures("Type change", "nib", errs)
		return a, a.list.loadNibs

	case createTypeSelectedMsg:
		a.createModal = newCreateModalModel(a.keys.actions, msg.nibType, a.config, a.width, a.height)
		a.state = viewCreateModal
		return a, a.createModal.Init()

	case openPriorityPickerMsg:
		a.previousState = a.state
		a.priorityPicker = newPriorityPickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentPriority, a.width, a.height)
		a.state = viewPriorityPicker
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
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		a.reportMutationFailures("Priority change", "nib", errs)
		return a, a.list.loadNibs

	case openModifyPickerMsg:
		a.previousState = a.state
		a.modifyPicker = newModifyPickerModel(a.keys.actions, msg.targets, a.width, a.height)
		a.state = viewModifyPicker
		return a, nil

	case closeModifyPickerMsg:
		a.state = a.previousState
		return a, nil

	case modifyChosenMsg:
		// The field's picker opens over the view m was typed in, and returns there.
		a.state = a.previousState
		return a.Update(msg.open)

	case openEstimatePickerMsg:
		a.previousState = a.state
		a.estimatePicker = newEstimatePickerModel(a.keys.actions, msg.nibIDs, msg.nibTitle, msg.currentEstimate, a.width, a.height)
		a.state = viewEstimatePicker
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
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		a.reportMutationFailures("Estimate change", "nib", errs)
		return a, a.list.loadNibs

	case openBlockingPickerMsg:
		a.previousState = a.state
		a.blockingPicker = newBlockingPickerModel(a.keys.actions, msg.nibID, msg.nibTitle, msg.currentBlocking, a.backend, a.config, a.width, a.height)
		a.state = viewBlockingPicker
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
		if a.state == viewDetail {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibID)
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		a.reportMutationFailures("Blocking change", "link", errs)
		return a, a.list.loadNibs

	case reorderNibMsg:
		result, err := a.backend.ReorderNib(context.Background(), msg.nibID, msg.afterID, msg.beforeID, msg.first)
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("Reorder failed: %v", err)
			a.list.statusKind = statusWarn
			return a, nil
		}
		// Set before loadNibs runs so the reloaded list selects the moved nib.
		if result != nil {
			a.list.selectByID = result.ID
		}
		return a, a.list.loadNibs

	case reorderBlockMsg:
		_, err := a.backend.ReorderNib(context.Background(), msg.displacedID, msg.afterID, msg.beforeID, nil)
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("Reorder failed: %v", err)
			a.list.statusKind = statusWarn
			return a, nil
		}
		// Keep focus on the user's row, not the displaced sibling.
		a.list.selectByID = msg.focusID
		return a, a.list.loadNibs

	case reorderRefusedMsg:
		a.list.statusMessage = msg.reason
		a.list.statusKind = statusWarn
		return a, nil

	case openConfirmMsg:
		a.previousState = a.state
		a.confirmDialog = msg.dialog
		a.confirmDialog.table = a.keys.actions
		a.state = viewConfirmDialog
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
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
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
		case viewDetail:
			a.detail.statusMessage = statusMsg
			a.detail.statusKind = statusMsgKind
		}

		return a, nil

	case selectNibMsg:
		if a.state == viewDetail {
			a.history = append(a.history, a.detail)
		}
		a.state = viewDetail
		a.detail = a.initDetailModel(msg.nib)
		return a, a.detail.Init()

	case backToListMsg:
		if len(a.history) > 0 {
			a.detail = a.history[len(a.history)-1]
			a.history = a.history[:len(a.history)-1]
			// The terminal may have been resized since this view was pushed.
			a.detail, _ = a.detail.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
		} else {
			a.state = viewList
			// The list missed any resize that happened in the detail view.
			a.list, cmd = a.list.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
			return a, cmd
		}
		return a, nil
	}

	// Everything not handled above goes to the active view.
	return a, a.updateActiveView(msg)
}

// updateActiveView hands msg to the view on screen.
func (a *App) updateActiveView(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.state {
	case viewList:
		a.list, cmd = a.list.Update(msg)
	case viewDetail:
		a.detail, cmd = a.detail.Update(msg)
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
		a.modifyPicker, cmd = a.modifyPicker.Update(msg)
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

// renderTwoColumnView renders the list and preview side by side above a
// full-width footer.
func (a *App) renderTwoColumnView() string {
	leftWidth, rightWidth := calculatePaneWidths(a.width)

	// The footer may be several lines tall.
	footer := a.list.Footer()
	contentHeight := a.height - max(1, lipgloss.Height(footer))

	leftPane := a.list.pane(leftWidth, contentHeight)

	a.preview.width = rightWidth
	a.preview.height = contentHeight
	rightPane := a.preview.View()

	columns := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	return columns + "\n" + footer
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
	a.detail.hints = hints
	return a.renderState(a.state)
}

// renderState draws state, which is the active state or the one the palette
// was opened from.
func (a *App) renderState(state viewState) string {
	switch state {
	case viewList:
		if a.isTwoColumnMode() {
			return a.renderTwoColumnView()
		}
		return a.list.View()
	case viewDetail:
		return a.detail.View()
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

// nibViewOnScreen reports whether the nib view is drawn, on its own or under
// the palette or a modal, the same way render finds the view underneath.
func (a *App) nibViewOnScreen() bool {
	state := a.state
	if state == viewPalette {
		state = a.palette.origin
	}
	switch state {
	case viewList, viewDetail, viewTagPicker:
		return state == viewDetail
	}
	return a.previousState == viewDetail
}

// getBackgroundView returns the view to show behind modal pickers
func (a *App) getBackgroundView() string {
	switch a.previousState {
	case viewList:
		if a.isTwoColumnMode() {
			return a.renderTwoColumnView()
		}
		return a.list.View()
	case viewDetail:
		return a.detail.View()
	default:
		return a.list.View()
	}
}

// initDetailModel creates a detail model with an empty footer.
func (a *App) initDetailModel(n *nib.Nib) detailModel {
	return newDetailModel(n, a.backend, a.config, a.width, a.height, a.display.icons)
}

// reportMutationFailures reports a picker's failed mutations in the footer of
// the view the app returned to. Call it after any detail-model rebuild, which
// empties the footer.
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
// view on screen. Call it after any detail-model rebuild.
//
// Report every failed write. Do not report a failed read that only refreshes
// what is on screen.
func (a *App) reportFailure(message string) {
	if a.state == viewDetail {
		a.detail.statusMessage = message
		a.detail.statusKind = statusWarn
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

// Run starts the TUI and forwards store changes to it until it exits. The
// parameters are as for New.
func Run(backend Backend, cfg *config.Config, user config.UserLayer, version string) error {
	app := New(backend, cfg, user, version)
	p := tea.NewProgram(app)

	app.program = p

	if err := backend.StartWatching(); err != nil {
		return err
	}
	defer backend.StopWatching()

	eventCh, unsubscribe := backend.Subscribe()
	defer unsubscribe()

	go func() {
		for range eventCh {
			if app.program != nil {
				app.program.Send(nibsChangedMsg{})
			}
		}
	}()

	_, err := p.Run()
	return err
}
