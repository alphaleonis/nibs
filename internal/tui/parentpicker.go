package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// parentSelectedMsg is sent when a parent is selected from the picker
type parentSelectedMsg struct {
	nibIDs  []string // the nibs being modified
	parentID string   // the new parent ID (empty string to clear parent)
}

// closeParentPickerMsg is sent when the parent picker is cancelled
type closeParentPickerMsg struct{}

// parentItem wraps a nib to implement list.Item for the parent picker
type parentItem struct {
	nib *nib.Nib
	cfg  *config.Config
}

func (i parentItem) Title() string       { return i.nib.Title }
func (i parentItem) Description() string { return i.nib.ID }
func (i parentItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// clearParentItem is a special item to clear the parent
type clearParentItem struct{}

func (i clearParentItem) Title() string       { return "(No Parent)" }
func (i clearParentItem) Description() string { return "Clear the parent assignment" }
func (i clearParentItem) FilterValue() string { return "no parent clear none" }

// parentItemDelegate handles rendering of parent picker items
type parentItemDelegate struct {
	cfg *config.Config
}

func (d parentItemDelegate) Height() int                             { return 1 }
func (d parentItemDelegate) Spacing() int                            { return 0 }
func (d parentItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d parentItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	var cursor string
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	} else {
		cursor = "  "
	}

	switch item := listItem.(type) {
	case clearParentItem:
		text := ui.Muted.Render(item.Title())
		_, _ = fmt.Fprint(w, cursor+text)

	case parentItem:
		// Get colors from config. EffectiveType so a type-less nib keeps its "task"
		// badge. Raw Priority is safe here despite the missing default: GetNibColors ->
		// GetPriority yields a different PriorityColor for "" (none) vs "normal"
		// ("white"), but that color is only ever consumed by RenderPrioritySymbol, which
		// returns "" (discarding the color) whenever GetPrioritySymbol is empty — and the
		// symbol is empty for both "" and "normal", so the rendered result is identical.
		colors := d.cfg.GetNibColors(item.nib.Status, item.nib.EffectiveType(), item.nib.Priority)

		// Format: [type] title (id)
		typeBadge := ui.RenderTypeText(item.nib.EffectiveType(), colors.TypeColor)
		title := item.nib.Title
		if colors.IsArchive {
			title = ui.Muted.Render(title)
		}
		id := ui.Muted.Render(" (" + item.nib.ID + ")")

		_, _ = fmt.Fprint(w, cursor+typeBadge+" "+title+id)
	}
}

// parentPickerModel is the model for the parent picker view
type parentPickerModel struct {
	list           list.Model
	nibIDs         []string // the nibs we're setting the parent for
	nibTitle       string   // display title (single title or "N selected nibs")
	nibTypes       []string // types of the nibs (to filter eligible parents)
	currentParent  string   // current parent ID (to highlight, only for single nib)
	hideCompleted  bool     // whether to hide completed/scrapped nibs
	allEligible    []*nib.Nib // all eligible nibs before status filtering
	width          int
	height         int
	cfg            *config.Config
}

func newParentPickerModel(nibIDs []string, nibTitle string, nibTypes []string, currentParent string, backend Backend, cfg *config.Config, width, height int) parentPickerModel {
	// Get valid parent types - for multi-select, find types valid for ALL nibs
	var validParentTypes []string
	for i, nibType := range nibTypes {
		typeParents := nibtypes.ValidParentTypes(nibType)
		if i == 0 {
			validParentTypes = typeParents
		} else {
			// Intersect with existing valid types
			validParentTypes = intersectStrings(validParentTypes, typeParents)
		}
	}

	// Fetch all nibs and filter to eligible parents
	allNibs, _ := backend.ListNibs(context.Background(), nil)

	// Collect all descendants of all selected nibs (to prevent cycles)
	allDescendants := make(map[string]bool)
	for _, nibID := range nibIDs {
		for descID := range collectDescendants(nibID, allNibs) {
			allDescendants[descID] = true
		}
	}

	// Create set of selected nib IDs for quick lookup
	selectedSet := make(map[string]bool)
	for _, id := range nibIDs {
		selectedSet[id] = true
	}

	// Filter to eligible parents:
	// 1. Must be of a valid parent type for ALL selected nibs
	// 2. Must not be any of the selected nibs
	// 3. Must not be a descendant of any selected nib (to prevent cycles)
	var eligibleNibs []*nib.Nib
	for _, b := range allNibs {
		// Skip selected nibs
		if selectedSet[b.ID] {
			continue
		}
		// Skip descendants (would create cycle)
		if allDescendants[b.ID] {
			continue
		}
		// Check if type is valid
		isValidType := false
		for _, validType := range validParentTypes {
			if b.EffectiveType() == validType {
				isValidType = true
				break
			}
		}
		if !isValidType {
			continue
		}
		eligibleNibs = append(eligibleNibs, b)
	}

	// Sort by type order (milestone > epic > feature), then by title
	typeNames := cfg.TypeNames()
	typeOrder := make(map[string]int)
	for i, t := range typeNames {
		typeOrder[t] = i
	}
	sort.Slice(eligibleNibs, func(i, j int) bool {
		// Primary: type order (EffectiveType so a type-less nib sorts as "task")
		ti, tj := typeOrder[eligibleNibs[i].EffectiveType()], typeOrder[eligibleNibs[j].EffectiveType()]
		if ti != tj {
			return ti < tj
		}
		// Secondary: title (case-insensitive)
		return strings.ToLower(eligibleNibs[i].Title) < strings.ToLower(eligibleNibs[j].Title)
	})

	// Create the initial list model (rebuildList will populate items)
	delegate := parentItemDelegate{cfg: cfg}
	modalWidth := max(40, min(80, width*60/100))
	modalHeight := max(10, min(20, height*60/100))
	listWidth := modalWidth - 6
	listHeight := modalHeight - 7

	l := list.New(nil, delegate, listWidth, listHeight)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary)

	m := parentPickerModel{
		list:          l,
		nibIDs:        nibIDs,
		nibTitle:      nibTitle,
		nibTypes:      nibTypes,
		currentParent: currentParent,
		hideCompleted: true,
		allEligible:   eligibleNibs,
		width:         width,
		height:        height,
		cfg:           cfg,
	}

	m.rebuildList()
	return m
}

// toggleHideCompleted flips the completed/scrapped filter and rebuilds the list.
func (m *parentPickerModel) toggleHideCompleted() {
	m.hideCompleted = !m.hideCompleted
	m.rebuildList()
}

func hideCompletedHelpText(hiding bool) string {
	if hiding {
		return "show completed"
	}
	return "hide completed"
}

// rebuildList reconstructs the list items from allEligible, applying the hideCompleted filter.
// Uses SetItems to preserve any active text filter state.
func (m *parentPickerModel) rebuildList() {
	var filtered []*nib.Nib
	for _, b := range m.allEligible {
		if m.hideCompleted && m.cfg.IsArchiveStatus(b.Status) {
			continue
		}
		filtered = append(filtered, b)
	}

	items := make([]list.Item, 0, len(filtered)+1)
	items = append(items, clearParentItem{})

	selectedIndex := 0
	for i, b := range filtered {
		items = append(items, parentItem{nib: b, cfg: m.cfg})
		if b.ID == m.currentParent {
			selectedIndex = i + 1
		}
	}

	m.list.SetItems(items)

	if m.hideCompleted {
		m.list.Title = "Select Parent [hiding completed]"
	} else {
		m.list.Title = "Select Parent"
	}

	if selectedIndex > 0 && selectedIndex < len(items) {
		m.list.Select(selectedIndex)
	}
}

// intersectStrings returns the intersection of two string slices
func intersectStrings(a, b []string) []string {
	set := make(map[string]bool)
	for _, s := range a {
		set[s] = true
	}
	var result []string
	for _, s := range b {
		if set[s] {
			result = append(result, s)
		}
	}
	return result
}

// collectDescendants returns a set of all nib IDs that are descendants of the given nib
func collectDescendants(nibID string, allNibs []*nib.Nib) map[string]bool {
	descendants := make(map[string]bool)

	// Build parent->children map
	children := make(map[string][]string)
	for _, b := range allNibs {
		if b.Parent != "" {
			children[b.Parent] = append(children[b.Parent], b.ID)
		}
	}

	// BFS to collect all descendants
	queue := children[nibID]
	for len(queue) > 0 {
		childID := queue[0]
		queue = queue[1:]
		if !descendants[childID] {
			descendants[childID] = true
			queue = append(queue, children[childID]...)
		}
	}

	return descendants
}

func (m parentPickerModel) Init() tea.Cmd {
	return nil
}

func (m parentPickerModel) Update(msg tea.Msg) (parentPickerModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Recalculate modal dimensions
		modalWidth := max(40, min(80, msg.Width*60/100))
		modalHeight := max(10, min(20, msg.Height*60/100))
		listWidth := modalWidth - 6
		listHeight := modalHeight - 7
		m.list.SetSize(listWidth, listHeight)

	case tea.KeyMsg:
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "enter":
				switch item := m.list.SelectedItem().(type) {
				case clearParentItem:
					return m, func() tea.Msg {
						return parentSelectedMsg{nibIDs: m.nibIDs, parentID: ""}
					}
				case parentItem:
					return m, func() tea.Msg {
						return parentSelectedMsg{nibIDs: m.nibIDs, parentID: item.nib.ID}
					}
				}
			case "H":
				m.toggleHideCompleted()
				return m, nil
			case "esc", "backspace":
				// Return without selecting
				return m, func() tea.Msg {
					return closeParentPickerMsg{}
				}
			}
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m parentPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// For multi-select, don't show individual nib ID
	var nibID string
	if len(m.nibIDs) == 1 {
		nibID = m.nibIDs[0]
	}

	return renderPickerModal(pickerModalConfig{
		NibTitle:   m.nibTitle,
		NibID:      nibID,
		ListContent: m.list.View(),
		ExtraHelp:   []helpEntry{{"H", hideCompletedHelpText(m.hideCompleted)}},
		Width:       m.width,
		WidthPct:    60,
		MaxWidth:    80,
	})
}

// ModalView returns the picker rendered as a centered modal overlay on top of the background
func (m parentPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
