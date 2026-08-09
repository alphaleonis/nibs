package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
)

// viewState represents which view is currently active
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
	viewCreateModal
	viewCreateTypePicker
	viewConfirmDialog
)

// Two-column layout constants
const (
	TwoColumnMinWidth = 120 // minimum terminal width for two-column layout
	RightPaneMaxWidth = 80  // max width of preview pane (text files follow 80 char convention)
)

// calculatePaneWidths returns (leftWidth, rightWidth) for two-column layout.
// Right pane is capped at RightPaneMaxWidth, left pane gets remaining space.
func calculatePaneWidths(totalWidth int) (int, int) {
	rightWidth := RightPaneMaxWidth
	if totalWidth-rightWidth < 40 { // ensure left pane has reasonable minimum
		rightWidth = totalWidth - 40
	}
	leftWidth := totalWidth - rightWidth - 1 // 1 for separator
	return leftWidth, rightWidth
}

// nibsChangedMsg is sent when nibs change on disk (via file watcher)
type nibsChangedMsg struct{}

// updateCheckMsg carries the result of the background "is a newer release
// available?" check.
type updateCheckMsg struct {
	available bool
	latest    string
}

// checkForUpdateCmd runs the update check off the render loop (Bubbletea runs
// each tea.Cmd in its own goroutine). It is entirely best-effort: a gated-off
// (dev build / CI / opt-out) or failed check simply yields available=false, so
// the indicator stays hidden and the UI is never blocked.
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

// openTagPickerMsg requests opening the tag picker
type openTagPickerMsg struct{}

// tagSelectedMsg is sent when a tag is selected from the picker
type tagSelectedMsg struct {
	tag string
}

// clearFilterMsg is sent to clear any active filter
type clearFilterMsg struct{}

// copyNibIDMsg requests copying nib ID(s) to the clipboard
type copyNibIDMsg struct {
	ids []string
}

// reorderNibMsg requests reordering a nib among its siblings
type reorderNibMsg struct {
	nibID    string
	afterID  *string
	beforeID *string
	first    *bool
}

// reorderBlockMsg requests a block-move: swap a single "displaced" sibling
// past a contiguous block of selected siblings using one backend ReorderNib
// call. Move-up sets afterID (the last item of the block); move-down sets
// beforeID (the first item of the block). focusID is the list row to re-
// select after the reload.
type reorderBlockMsg struct {
	displacedID string
	afterID     *string
	beforeID    *string
	focusID     string
}

// reorderRefusedMsg reports why a requested reorder cannot happen. Without it
// a refused reorder is indistinguishable from a dropped keypress.
type reorderRefusedMsg struct {
	reason string
}

// openEditorMsg requests opening the editor for a nib
type openEditorMsg struct {
	nibID   string
	nibPath string
}

// editorFinishedMsg is sent when the editor closes
type editorFinishedMsg struct {
	err error
}

// openParentPickerMsg requests opening the parent picker for nib(s)
type openParentPickerMsg struct {
	nibIDs        []string // IDs of nibs to update
	nibTitle      string   // Display title (single title or "N selected nibs")
	nibTypes      []string // Types of the nibs (to filter eligible parents)
	currentParent string   // Only meaningful for single nib
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
	createModal    createModalModel
	confirmDialog  confirmDialog
	helpExpanded   bool          // help panel expanded (non-modal, toggles with ?)
	history        []detailModel // stack of previous detail views for back navigation
	backend        Backend
	config         *config.Config
	width          int
	height         int
	program        *tea.Program // reference to program for sending messages from watcher

	// Key chord state - tracks partial key sequences like "g" waiting for "t"
	pendingKey string

	// Modal state - tracks view behind modal pickers
	previousState viewState

	// Editor state - tracks nib being edited to update updated_at on save
	editingNibID      string
	editingNibModTime time.Time

	// Running binary version, used for the background update check.
	version string
}

// New creates a new TUI application. version is the running binary version,
// used for the best-effort "update available" indicator.
func New(backend Backend, cfg *config.Config, version string) *App {
	return &App{
		state:   viewList,
		backend: backend,
		config:  cfg,
		list:    newListModel(backend, cfg),
		preview: newPreviewModel(nil, 0, 0),
		version: version,
	}
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.list.Init(), checkForUpdateCmd(a.version))
}

// isTwoColumnMode returns true if the terminal width supports two-column layout
// and wide mode is not active (wide mode uses full width for the list)
func (a *App) isTwoColumnMode() bool {
	return a.width >= TwoColumnMinWidth && !a.list.wideMode
}

// Update handles messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		// Propagate help state to sub-models so their sizing accounts for the panel
		a.list.helpExpanded = a.helpExpanded
		a.detail.helpExpanded = a.helpExpanded

		// Resize list to account for help panel height
		if a.helpExpanded {
			helpHt := a.list.currentHelpHeight()
			footerH := max(1, helpHt)
			if a.isTwoColumnMode() {
				leftWidth, _ := calculatePaneWidths(a.width)
				contentHeight := a.height - footerH
				a.list.list.SetSize(leftWidth-2, contentHeight-2)
			} else {
				a.list.list.SetSize(a.width-2, a.height-3-footerH)
			}
		}

		// Update preview dimensions if in two-column mode
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

		// Handle key chord sequences
		if a.state == viewList && a.list.list.FilterState() != 1 {
			if a.pendingKey == "g" {
				a.pendingKey = ""
				switch msg.String() {
				case "t":
					// "g t" - go to tags
					return a, func() tea.Msg { return openTagPickerMsg{} }
				default:
					// Invalid second key, ignore the chord
				}
				// Don't forward this key since it was part of a chord attempt
				return a, nil
			}

			// Start of potential chord
			if msg.String() == "g" {
				a.pendingKey = "g"
				return a, nil
			}
		}

		// Clear pending key on any other key press
		a.pendingKey = ""

		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "?":
			// Toggle non-modal help panel (skip if user is typing in a filter)
			if a.state == viewList && a.list.list.FilterState() == 1 {
				break // let list handle the keystroke
			}
			if a.state == viewList || a.state == viewDetail {
				a.helpExpanded = !a.helpExpanded
				if a.state == viewList {
					a.list.helpExpanded = a.helpExpanded
					helpHt := a.list.currentHelpHeight()
					footerH := max(1, helpHt)
					if a.isTwoColumnMode() {
						leftWidth, _ := calculatePaneWidths(a.width)
						contentHeight := a.height - footerH
						a.list.list.SetSize(leftWidth-2, contentHeight-2)
					} else {
						a.list.list.SetSize(a.width-2, a.height-3-footerH)
					}
				} else { // viewDetail
					a.detail.helpExpanded = a.helpExpanded
					a.detail, _ = a.detail.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
				}
				return a, nil
			}
		case "q":
			if a.state == viewDetail || a.state == viewTagPicker || a.state == viewParentPicker || a.state == viewStatusPicker || a.state == viewTypePicker || a.state == viewCreateTypePicker || a.state == viewBlockingPicker || a.state == viewPriorityPicker || a.state == viewEstimatePicker {
				return a, tea.Quit
			}
			// For list, only quit if not filtering
			if a.state == viewList && a.list.list.FilterState() != 1 {
				return a, tea.Quit
			}
		}

	case cursorChangedMsg:
		// Update preview with the newly highlighted nib
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
		// Forward to list view
		a.list, cmd = a.list.Update(msg)
		// Update preview with current cursor position
		_, rightWidth := calculatePaneWidths(a.width)
		if len(msg.items) == 0 {
			a.preview = newPreviewModel(nil, rightWidth, a.height-2)
		} else if item, ok := a.list.list.SelectedItem().(nibItem); ok {
			a.preview = newPreviewModel(item.nib, rightWidth, a.height-2)
		}
		return a, cmd

	case updateCheckMsg:
		// Best-effort: surface the indicator when a newer release exists.
		if msg.available {
			a.list.updateAvailable = true
			a.list.updateLatest = msg.latest
		}
		return a, nil

	case nibsChangedMsg:
		// Nibs changed on disk - refresh
		if a.state == viewDetail {
			// Try to reload the current nib via backend
			updatedNib, err := a.backend.GetNib(context.Background(), a.detail.nib.ID)
			if err != nil || updatedNib == nil {
				// Nib was deleted - return to list
				a.state = viewList
				a.history = nil
			} else {
				// Recreate detail view with fresh nib data
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		// Trigger list refresh
		return a, a.list.loadNibs

	case openTagPickerMsg:
		// Collect all tags with their counts
		tags := a.collectTagsWithCounts()
		if len(tags) == 0 {
			// No tags in system, don't open picker
			return a, nil
		}
		a.tagPicker = newTagPickerModel(tags, a.width, a.height)
		a.state = viewTagPicker
		return a, a.tagPicker.Init()

	case tagSelectedMsg:
		a.state = viewList
		a.list.setTagFilter(msg.tag)
		return a, a.list.loadNibs

	case openParentPickerMsg:
		// Check if all nib types can have parents
		for _, nibType := range msg.nibTypes {
			if nibtypes.ValidParentTypes(nibType) == nil {
				// At least one nib type (e.g., milestone) cannot have parents - don't open the picker
				return a, nil
			}
		}
		a.previousState = a.state // Remember where we came from for the modal background
		a.parentPicker = newParentPickerModel(msg.nibIDs, msg.nibTitle, msg.nibTypes, msg.currentParent, a.backend, a.config, a.width, a.height)
		a.state = viewParentPicker
		return a, a.parentPicker.Init()

	case closeParentPickerMsg:
		// Return to previous view and refresh in case nibs changed while picker was open
		a.state = a.previousState
		return a, a.list.loadNibs

	case openStatusPickerMsg:
		a.previousState = a.state
		a.statusPicker = newStatusPickerModel(msg.nibIDs, msg.nibTitle, msg.currentStatus, a.config, a.width, a.height)
		a.state = viewStatusPicker
		return a, a.statusPicker.Init()

	case closeStatusPickerMsg:
		// Return to previous view and refresh in case nibs changed while picker was open
		a.state = a.previousState
		return a, a.list.loadNibs

	case statusSelectedMsg:
		// Update all nibs' status via backend mutations
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Status: &msg.status,
			})
			if err != nil {
				// Continue with other nibs even if one fails
				continue
			}
		}
		// Return to the previous view and refresh
		a.state = a.previousState
		// Clear selection after batch edit
		clear(a.list.selectedNibs)
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		return a, a.list.loadNibs

	case openCreateTypePickerMsg:
		a.previousState = a.state
		// Open type picker for creation (no nibIDs, all types valid)
		a.typePicker = newTypePickerModel(nil, "", msg.defaultType, nil, a.config, a.width, a.height)
		a.state = viewCreateTypePicker
		return a, a.typePicker.Init()

	case openTypePickerMsg:
		a.previousState = a.state
		a.typePicker = newTypePickerModel(msg.nibIDs, msg.nibTitle, msg.currentType, msg.validTypes, a.config, a.width, a.height)
		a.state = viewTypePicker
		return a, a.typePicker.Init()

	case closeTypePickerMsg:
		// Return to previous view and refresh
		a.state = a.previousState
		return a, a.list.loadNibs

	case typeSelectedMsg:
		// Check if this came from the create flow
		if a.state == viewCreateTypePicker {
			// Transition to create modal with the chosen type
			return a, func() tea.Msg {
				return createTypeSelectedMsg{nibType: msg.nibType}
			}
		}
		// Update all nibs' type via backend mutations
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Type: &msg.nibType,
			})
			if err != nil {
				// Continue with other nibs even if one fails
				continue
			}
		}
		// Return to the previous view and refresh
		a.state = a.previousState
		// Clear selection after batch edit
		clear(a.list.selectedNibs)
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		return a, a.list.loadNibs

	case createTypeSelectedMsg:
		// Type selected during create flow → open title input modal
		a.createModal = newCreateModalModel(msg.nibType, a.config, a.width, a.height)
		a.state = viewCreateModal
		return a, a.createModal.Init()

	case openPriorityPickerMsg:
		a.previousState = a.state
		a.priorityPicker = newPriorityPickerModel(msg.nibIDs, msg.nibTitle, msg.currentPriority, a.config, a.width, a.height)
		a.state = viewPriorityPicker
		return a, a.priorityPicker.Init()

	case closePriorityPickerMsg:
		// Return to previous view and refresh in case nibs changed while picker was open
		a.state = a.previousState
		return a, a.list.loadNibs

	case prioritySelectedMsg:
		// Update all nibs' priority via backend mutations
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Priority: graphql.OmittableOf(&msg.priority),
			})
			if err != nil {
				// Continue with other nibs even if one fails
				continue
			}
		}
		// Return to the previous view and refresh
		a.state = a.previousState
		// Clear selection after batch edit
		clear(a.list.selectedNibs)
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		return a, a.list.loadNibs

	case openEstimatePickerMsg:
		a.previousState = a.state
		a.estimatePicker = newEstimatePickerModel(msg.nibIDs, msg.nibTitle, msg.currentEstimate, a.config, a.width, a.height)
		a.state = viewEstimatePicker
		return a, a.estimatePicker.Init()

	case closeEstimatePickerMsg:
		a.state = a.previousState
		return a, a.list.loadNibs

	case estimateSelectedMsg:
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.UpdateNib(context.Background(), nibID, model.UpdateNibInput{
				Estimate: graphql.OmittableOf(&msg.estimate),
			})
			if err != nil {
				continue
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
		return a, a.list.loadNibs

	case openBlockingPickerMsg:
		a.previousState = a.state
		a.blockingPicker = newBlockingPickerModel(msg.nibID, msg.nibTitle, msg.currentBlocking, a.backend, a.config, a.width, a.height)
		a.state = viewBlockingPicker
		return a, a.blockingPicker.Init()

	case closeBlockingPickerMsg:
		// Return to previous view and refresh in case nibs changed while picker was open
		a.state = a.previousState
		return a, a.list.loadNibs

	case blockingConfirmedMsg:
		// Apply all blocking changes via backend mutations
		for _, targetID := range msg.toAdd {
			_, err := a.backend.AddBlocking(context.Background(), msg.nibID, targetID)
			if err != nil {
				// Continue with other changes even if one fails
				continue
			}
		}
		for _, targetID := range msg.toRemove {
			_, err := a.backend.RemoveBlocking(context.Background(), msg.nibID, targetID)
			if err != nil {
				// Continue with other changes even if one fails
				continue
			}
		}
		// Return to previous view and refresh
		a.state = a.previousState
		if a.state == viewDetail {
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibID)
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		return a, a.list.loadNibs

	case reorderNibMsg:
		// Execute reorder via backend
		result, err := a.backend.ReorderNib(context.Background(), msg.nibID, msg.afterID, msg.beforeID, msg.first)
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("Reorder failed: %v", err)
			a.list.statusKind = statusWarn
			return a, nil
		}
		// Set selectByID synchronously before loadNibs to guarantee re-selection
		if result != nil {
			a.list.selectByID = result.ID
		}
		return a, a.list.loadNibs

	case reorderBlockMsg:
		// Block move: move the single displaced sibling past the selected block
		// in one ReorderNib call. Preserves the block's internal order
		// automatically.
		_, err := a.backend.ReorderNib(context.Background(), msg.displacedID, msg.afterID, msg.beforeID, nil)
		if err != nil {
			a.list.statusMessage = fmt.Sprintf("Reorder failed: %v", err)
			a.list.statusKind = statusWarn
			return a, nil
		}
		// Preserve focus on the originally-focused row (not the displaced sibling).
		a.list.selectByID = msg.focusID
		return a, a.list.loadNibs

	case reorderRefusedMsg:
		a.list.statusMessage = msg.reason
		a.list.statusKind = statusWarn
		return a, nil

	case openConfirmMsg:
		a.previousState = a.state
		a.confirmDialog = msg.dialog
		a.state = viewConfirmDialog
		return a, nil

	case confirmActionMsg:
		// Execute the confirmed action, collecting any errors
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
		// Create the nib via backend mutation with draft status
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
		// Pre-fill body with template stubs based on the chosen type
		if tmpl := bodytemplate.BodyTemplate(nibType); tmpl != "" {
			input.Body = &tmpl
		}
		// Infer parent from chosen type and currently selected nib
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
		// Return to list and open the new nib in editor
		a.state = viewList
		return a, tea.Batch(
			a.list.loadNibs,
			func() tea.Msg {
				return openEditorMsg{nibID: createdNib.ID, nibPath: createdNib.Path}
			},
		)

	case openEditorMsg:
		// Launch editor for the nib file
		editor := getEditor()
		fullPath := filepath.Join(a.backend.Root(), msg.nibPath)

		// Record the nib ID and file mod time before editing
		a.editingNibID = msg.nibID
		if info, err := os.Stat(fullPath); err == nil {
			a.editingNibModTime = info.ModTime()
		}

		c := exec.Command(editor, fullPath)
		return a, tea.ExecProcess(c, func(err error) tea.Msg {
			return editorFinishedMsg{err: err}
		})

	case editorFinishedMsg:
		// Editor closed - check if file was modified and reload if so
		if a.editingNibID != "" {
			if n, err := a.backend.GetNib(context.Background(), a.editingNibID); err == nil && n != nil {
				fullPath := filepath.Join(a.backend.Root(), n.Path)
				if info, err := os.Stat(fullPath); err == nil {
					if info.ModTime().After(a.editingNibModTime) {
						_, _ = a.backend.ReloadAfterEdit(a.editingNibID)
					}
				}
			}
			a.editingNibID = ""
			a.editingNibModTime = time.Time{}
		}
		return a, nil

	case parentSelectedMsg:
		// Set the new parent via backend mutation for all nibs
		var parentID *string
		if msg.parentID != "" {
			parentID = &msg.parentID
		}
		for _, nibID := range msg.nibIDs {
			_, err := a.backend.SetParent(context.Background(), nibID, parentID, nil)
			if err != nil {
				// Continue with other nibs even if one fails
				continue
			}
		}
		// Return to the previous view and refresh
		a.state = a.previousState
		// Clear selection after batch edit
		clear(a.list.selectedNibs)
		if a.state == viewDetail && len(msg.nibIDs) == 1 {
			// Refresh the nib to show updated parent
			updatedNib, _ := a.backend.GetNib(context.Background(), msg.nibIDs[0])
			if updatedNib != nil {
				a.detail = a.initDetailModel(updatedNib)
			}
		}
		return a, a.list.loadNibs

	case clearFilterMsg:
		a.list.clearFilter()
		return a, a.list.loadNibs

	case copyNibIDMsg:
		var statusMsg string
		statusMsgKind := statusOK
		text := strings.Join(msg.ids, ", ")
		if err := clipboard.WriteAll(text); err != nil {
			statusMsg = fmt.Sprintf("Failed to copy: %v", err)
			statusMsgKind = statusWarn
		} else if len(msg.ids) == 1 {
			statusMsg = fmt.Sprintf("Copied %s to clipboard", msg.ids[0])
		} else {
			statusMsg = fmt.Sprintf("Copied %d nib IDs to clipboard", len(msg.ids))
		}

		// Set status on current view
		switch a.state {
		case viewList:
			a.list.statusMessage = statusMsg
			a.list.statusKind = statusMsgKind
		case viewDetail:
			a.detail.statusMessage = statusMsg
		}

		return a, nil

	case selectNibMsg:
		// Push current detail view to history if we're already viewing a nib
		if a.state == viewDetail {
			a.history = append(a.history, a.detail)
		}
		a.state = viewDetail
		a.detail = a.initDetailModel(msg.nib)
		return a, a.detail.Init()

	case backToListMsg:
		// Pop from history if available, otherwise go to list
		if len(a.history) > 0 {
			a.detail = a.history[len(a.history)-1]
			a.history = a.history[:len(a.history)-1]
			// Re-sync help state and layout in case it was toggled since push
			a.detail.helpExpanded = a.helpExpanded
			a.detail, _ = a.detail.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
		} else {
			a.state = viewList
			// Force list to pick up any size changes that happened while in detail view
			a.list, cmd = a.list.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
			return a, cmd
		}
		return a, nil
	}

	// Forward all messages to the current view
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
	case viewBlockingPicker:
		a.blockingPicker, cmd = a.blockingPicker.Update(msg)
	case viewCreateModal:
		a.createModal, cmd = a.createModal.Update(msg)
	case viewConfirmDialog:
		a.confirmDialog, cmd = a.confirmDialog.Update(msg)
	}

	return a, cmd
}

// collectTagsWithCounts returns all tags with their usage counts
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

// renderTwoColumnView renders the list and preview side by side with app-global footer
func (a *App) renderTwoColumnView() string {
	leftWidth, rightWidth := calculatePaneWidths(a.width)

	// Calculate help panel dimensions
	helpHt := 0
	var helpPanel string
	if a.helpExpanded {
		entries := a.list.expandedHelpEntries()
		helpPanel = renderHelpPanel(entries, a.width)
		helpHt = helpPanelHeight(entries, a.width)
	}

	// Footer/panel height: 1 for compact footer, helpHt for expanded panel
	contentHeight := a.height - max(1, helpHt)

	// Render left pane (list) with constrained width, no footer
	leftPane := a.list.ViewConstrained(leftWidth, contentHeight)

	// Render right pane (preview) with same height
	a.preview.width = rightWidth
	a.preview.height = contentHeight
	rightPane := a.preview.View()

	// Compose columns
	columns := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	// When expanded, the panel includes esc/?/q — no separate footer
	if helpPanel != "" {
		return columns + "\n" + helpPanel
	}
	return columns + "\n" + a.list.Footer()
}

// View renders the current view
// View renders the current screen. Terminal features that v1 set once at
// program construction are declared here instead, so the alt screen is
// re-asserted on every frame rather than by a NewProgram option.
func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	return v
}

// render produces the screen content for the active state.
func (a *App) render() string {
	switch a.state {
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
	case viewBlockingPicker:
		return a.blockingPicker.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewCreateModal:
		return a.createModal.ModalView(a.getBackgroundView(), a.width, a.height)
	case viewConfirmDialog:
		return a.confirmDialog.ModalView(a.getBackgroundView(), a.width, a.height)
	}
	return ""
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

// initDetailModel creates a new detail model with the current help state propagated.
func (a *App) initDetailModel(n *nib.Nib) detailModel {
	m := newDetailModel(n, a.backend, a.config, a.width, a.height)
	m.helpExpanded = a.helpExpanded
	if a.helpExpanded {
		m, _ = m.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	}
	return m
}

// getEditor returns the user's preferred editor using the fallback chain:
// $VISUAL -> $EDITOR -> vi -> nano
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	// Fallback chain: vi is more universal, nano as last resort
	if _, err := exec.LookPath("vi"); err == nil {
		return "vi"
	}
	return "nano"
}

// Run starts the TUI application with file watching. version is the running
// binary version, used for the best-effort update-available indicator.
func Run(backend Backend, cfg *config.Config, version string) error {
	app := New(backend, cfg, version)
	p := tea.NewProgram(app)

	// Store reference to program for sending messages from watcher
	app.program = p

	// Start file watching
	if err := backend.StartWatching(); err != nil {
		return err
	}
	defer backend.StopWatching()

	// Subscribe to nib events
	eventCh, unsubscribe := backend.Subscribe()
	defer unsubscribe()

	// Forward events to TUI in a goroutine
	go func() {
		for range eventCh {
			// Send message to TUI when nibs change
			if app.program != nil {
				app.program.Send(nibsChangedMsg{})
			}
		}
	}()

	_, err := p.Run()
	return err
}
