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
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// computeCurrentBlocking returns the IDs of nibs that this nib is blocking.
// Computed from the backend (scans for nibs with this nib in their blockedBy).
func computeCurrentBlocking(backend Backend, nibID string) []string {
	n, err := backend.GetNib(context.Background(), nibID)
	if err != nil || n == nil {
		return nil
	}
	blockingNibs, err := backend.GetBlocking(context.Background(), n, nil)
	if err != nil {
		return nil
	}
	ids := make([]string, len(blockingNibs))
	for i, b := range blockingNibs {
		ids[i] = b.ID
	}
	return ids
}

// blockingConfirmedMsg is sent when blocking changes are confirmed
type blockingConfirmedMsg struct {
	nibID  string            // the nib we're modifying
	toAdd   []string          // IDs to add to blocking
	toRemove []string         // IDs to remove from blocking
}

// closeBlockingPickerMsg is sent when the blocking picker is canceled
type closeBlockingPickerMsg struct{}

// openBlockingPickerMsg requests opening the blocking picker for a nib
type openBlockingPickerMsg struct {
	nibID          string
	nibTitle       string
	currentBlocking []string // IDs of nibs currently being blocked
}

// blockingItem wraps a nib to implement list.Item for the blocking picker
type blockingItem struct {
	nib *nib.Nib
	cfg  *config.Config
}

func (i blockingItem) Title() string       { return i.nib.Title }
func (i blockingItem) Description() string { return i.nib.ID }
func (i blockingItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// blockingItemDelegate handles rendering of blocking picker items
type blockingItemDelegate struct {
	cfg             *config.Config
	pendingBlocking *map[string]bool // pointer to pending state for live updates
}

func (d blockingItemDelegate) Height() int                             { return 1 }
func (d blockingItemDelegate) Spacing() int                            { return 0 }
func (d blockingItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d blockingItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(blockingItem)
	if !ok {
		return
	}

	var cursor string
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	} else {
		cursor = "  "
	}

	// Show blocking indicator - read from pending state for live updates
	isBlocking := (*d.pendingBlocking)[item.nib.ID]
	var blockingIndicator string
	if isBlocking {
		blockingIndicator = lipgloss.NewStyle().Foreground(ui.ColorDanger).Bold(true).Render("● ") // Red dot for blocking
	} else {
		blockingIndicator = lipgloss.NewStyle().Foreground(ui.ColorMuted).Render("○ ") // Empty circle for not blocking
	}

	// Get colors from config. EffectiveType so a type-less nib keeps its "task"
	// badge. Raw Priority is safe here despite the missing default: GetNibColors ->
	// GetPriority yields a different PriorityColor for "" (none) vs "normal" ("white"),
	// but that color is only ever consumed by RenderPrioritySymbol, which returns ""
	// (discarding the color) whenever GetPrioritySymbol is empty — and the symbol is
	// empty for both "" and "normal", so the rendered result is identical.
	colors := d.cfg.GetNibColors(item.nib.Status, item.nib.EffectiveType(), item.nib.Priority)

	// Format: [indicator] [type] title (id)
	typeBadge := ui.RenderTypeText(item.nib.EffectiveType(), colors.TypeColor)
	title := item.nib.Title
	if colors.IsClosed {
		title = ui.Muted.Render(title)
	}
	id := ui.Muted.Render(" (" + item.nib.ID + ")")

	_, _ = fmt.Fprint(w, cursor+blockingIndicator+typeBadge+" "+title+id)
}

// blockingPickerModel is the model for the blocking picker view
type blockingPickerModel struct {
	list             list.Model
	nibID           string           // the nib we're setting blocking for
	nibTitle        string           // the nib's title
	originalBlocking map[string]bool  // original state (for computing diff)
	pendingBlocking  map[string]bool  // pending state (toggled by space)
	cfg              *config.Config
	width            int
	height           int
}

func newBlockingPickerModel(nibID, nibTitle string, currentBlocking []string, backend Backend, cfg *config.Config, width, height int) blockingPickerModel {
	// Fetch all nibs
	allNibs, _ := backend.ListNibs(context.Background(), nil)

	// Create maps for original and pending state
	originalBlocking := make(map[string]bool)
	pendingBlocking := make(map[string]bool)
	for _, id := range currentBlocking {
		originalBlocking[id] = true
		pendingBlocking[id] = true
	}

	// Filter out the current nib and build items
	var eligibleNibs []*nib.Nib
	for _, b := range allNibs {
		if b.ID != nibID {
			eligibleNibs = append(eligibleNibs, b)
		}
	}

	// Sort by type order, then by title
	typeNames := cfg.TypeNames()
	typeOrder := make(map[string]int)
	for i, t := range typeNames {
		typeOrder[t] = i
	}
	sort.Slice(eligibleNibs, func(i, j int) bool {
		ti, tj := typeOrder[eligibleNibs[i].EffectiveType()], typeOrder[eligibleNibs[j].EffectiveType()]
		if ti != tj {
			return ti < tj
		}
		return strings.ToLower(eligibleNibs[i].Title) < strings.ToLower(eligibleNibs[j].Title)
	})

	// Build items list
	items := make([]list.Item, 0, len(eligibleNibs))
	for _, b := range eligibleNibs {
		items = append(items, blockingItem{
			nib: b,
			cfg:  cfg,
		})
	}

	// Calculate modal dimensions (60% width, 60% height, with min/max constraints)
	modalWidth := max(40, min(80, width*60/100))
	modalHeight := max(10, min(20, height*60/100))
	listWidth := modalWidth - 6
	// Account for: header(1) + subtitle(1) + blank(1) + blank(1) + description(1) + blank(1) + help(1) + border(2) = 9
	listHeight := modalHeight - 9

	// Create delegate with pointer to pending state (so it can read live updates)
	delegate := blockingItemDelegate{cfg: cfg, pendingBlocking: &pendingBlocking}

	l := list.New(items, delegate, listWidth, listHeight)
	l.Title = "Manage Blocking"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary)

	return blockingPickerModel{
		list:             l,
		nibID:           nibID,
		nibTitle:        nibTitle,
		originalBlocking: originalBlocking,
		pendingBlocking:  pendingBlocking,
		cfg:              cfg,
		width:            width,
		height:           height,
	}
}

func (m blockingPickerModel) Init() tea.Cmd {
	return nil
}

func (m blockingPickerModel) Update(msg tea.Msg) (blockingPickerModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		modalWidth := max(40, min(80, msg.Width*60/100))
		modalHeight := max(10, min(20, msg.Height*60/100))
		listWidth := modalWidth - 6
		listHeight := modalHeight - 9 // Account for description line
		m.list.SetSize(listWidth, listHeight)

	case tea.KeyMsg:
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case " ":
				// Toggle the selected item's pending state
				// The delegate reads from pendingBlocking directly, so no need to update items
				if item, ok := m.list.SelectedItem().(blockingItem); ok {
					targetID := item.nib.ID
					if m.pendingBlocking[targetID] {
						delete(m.pendingBlocking, targetID)
					} else {
						m.pendingBlocking[targetID] = true
					}
				}
				return m, nil

			case "enter":
				// Confirm changes - compute diff and send message
				var toAdd, toRemove []string

				// Find additions (in pending but not in original)
				for id := range m.pendingBlocking {
					if !m.originalBlocking[id] {
						toAdd = append(toAdd, id)
					}
				}

				// Find removals (in original but not in pending)
				for id := range m.originalBlocking {
					if !m.pendingBlocking[id] {
						toRemove = append(toRemove, id)
					}
				}

				return m, func() tea.Msg {
					return blockingConfirmedMsg{
						nibID:   m.nibID,
						toAdd:    toAdd,
						toRemove: toRemove,
					}
				}

			case "esc", "backspace":
				// Cancel - discard changes
				return m, func() tea.Msg {
					return closeBlockingPickerMsg{}
				}
			}
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m blockingPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	return renderPickerModal(pickerModalConfig{
		Title:       "Manage Blocking",
		NibTitle:   m.nibTitle,
		NibID:      m.nibID,
		ListContent: m.list.View(),
		Description: "space toggle, enter confirm, esc cancel",
		Width:       m.width,
		WidthPct:    60,
		MaxWidth:    80,
	})
}

// ModalView returns the picker rendered as a centered modal overlay on top of the background
func (m blockingPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
