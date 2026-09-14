package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// computeCurrentBlocking returns the IDs of the nibs nibID blocks, or nil when
// they cannot be read.
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
	nibID    string   // the nib we're modifying
	toAdd    []string // IDs to add to blocking
	toRemove []string // IDs to remove from blocking
}

// closeBlockingPickerMsg is sent when the blocking picker is canceled
type closeBlockingPickerMsg struct{}

// openBlockingPickerMsg requests opening the blocking picker for a nib
type openBlockingPickerMsg struct {
	nibID           string
	nibTitle        string
	currentBlocking []string // IDs of nibs currently being blocked
}

// blockingItem wraps a nib to implement list.Item for the blocking picker
type blockingItem struct {
	nib *nib.Nib
	cfg *config.Config
}

func (i blockingItem) Title() string       { return i.nib.Title }
func (i blockingItem) Description() string { return i.nib.ID }
func (i blockingItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// blockingItemDelegate handles rendering of blocking picker items
type blockingItemDelegate struct {
	cfg             *config.Config
	pendingBlocking *map[string]bool // read at render time, so toggles show immediately
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

	isBlocking := (*d.pendingBlocking)[item.nib.ID]
	var blockingIndicator string
	if isBlocking {
		blockingIndicator = lipgloss.NewStyle().Foreground(ui.ColorDanger).Bold(true).Render("● ")
	} else {
		blockingIndicator = lipgloss.NewStyle().Foreground(ui.ColorMuted).Render("○ ")
	}

	colors := d.cfg.GetNibColors(item.nib.Status, item.nib.EffectiveType(), item.nib.Priority)

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
	nibID            string          // the nib we're setting blocking for
	nibTitle         string          // the nib's title
	originalBlocking map[string]bool // original state (for computing diff)
	pendingBlocking  map[string]bool // pending state (toggled by space)
	cfg              *config.Config
	width            int
	height           int
}

func newBlockingPickerModel(nibID, nibTitle string, currentBlocking []string, backend Backend, cfg *config.Config, width, height int) blockingPickerModel {
	allNibs, _ := backend.ListNibs(context.Background(), nil)

	originalBlocking := make(map[string]bool)
	pendingBlocking := make(map[string]bool)
	for _, id := range currentBlocking {
		originalBlocking[id] = true
		pendingBlocking[id] = true
	}

	var eligibleNibs []*nib.Nib
	for _, b := range allNibs {
		if b.ID != nibID {
			eligibleNibs = append(eligibleNibs, b)
		}
	}

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

	items := make([]list.Item, 0, len(eligibleNibs))
	for _, b := range eligibleNibs {
		items = append(items, blockingItem{
			nib: b,
			cfg: cfg,
		})
	}

	modalWidth := pickerModalWidth(width, 60, 80)
	modalHeight := pickerModalHeight(height, 60, 20)
	listWidth := modalWidth - 6
	// header(1) + subtitle(1) + blank(1) + blank(1) + description(1) + blank(1) + help(1) + border(2) = 9
	listHeight := modalHeight - 9

	delegate := blockingItemDelegate{cfg: cfg, pendingBlocking: &pendingBlocking}

	l := list.New(items, delegate, listWidth, listHeight)
	l.Title = "Manage Blocking"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	applyFilterStyles(&l.Styles)

	return blockingPickerModel{
		list:             l,
		nibID:            nibID,
		nibTitle:         nibTitle,
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
		modalWidth := pickerModalWidth(msg.Width, 60, 80)
		modalHeight := pickerModalHeight(msg.Height, 60, 20)
		listWidth := modalWidth - 6
		listHeight := modalHeight - 9
		m.list.SetSize(listWidth, listHeight)

	case tea.KeyPressMsg:
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "space":
				// The delegate reads pendingBlocking directly; the items need no update.
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
				var toAdd, toRemove []string

				for id := range m.pendingBlocking {
					if !m.originalBlocking[id] {
						toAdd = append(toAdd, id)
					}
				}

				for id := range m.originalBlocking {
					if !m.pendingBlocking[id] {
						toRemove = append(toRemove, id)
					}
				}

				return m, func() tea.Msg {
					return blockingConfirmedMsg{
						nibID:    m.nibID,
						toAdd:    toAdd,
						toRemove: toRemove,
					}
				}

			case "esc", "backspace":
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
		NibTitle:    m.nibTitle,
		NibID:       m.nibID,
		ListContent: m.list.View(),
		Description: "space toggle, enter confirm, esc cancel",
		Width:       m.width,
		WidthPct:    60,
		MaxWidth:    80,
	})
}

// ModalView returns the picker centered over bgView.
func (m blockingPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
