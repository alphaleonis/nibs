package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// statusSelectedMsg is sent when a status is selected from the picker
type statusSelectedMsg struct {
	nibIDs []string
	status  string
}

// closeStatusPickerMsg is sent when the status picker is canceled
type closeStatusPickerMsg struct{}

// openStatusPickerMsg requests opening the status picker for nib(s)
type openStatusPickerMsg struct {
	nibIDs       []string // IDs of nibs to update
	nibTitle     string   // Display title (single title or "N nibs")
	currentStatus string   // Only meaningful for single nib
}

// statusItem wraps a status to implement list.Item
type statusItem struct {
	name        string
	description string
	color       string
	isClosed    bool
	isCurrent   bool
}

func (i statusItem) Title() string       { return i.name }
func (i statusItem) Description() string { return i.description }
func (i statusItem) FilterValue() string { return i.name + " " + i.description }

// statusItemDelegate handles rendering of status picker items
type statusItemDelegate struct{}

func (d statusItemDelegate) Height() int                             { return 1 }
func (d statusItemDelegate) Spacing() int                            { return 0 }
func (d statusItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d statusItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(statusItem)
	if !ok {
		return
	}

	var cursor string
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	} else {
		cursor = "  "
	}

	// Render status with color (text only)
	statusText := ui.RenderStatusTextWithColor(item.name, item.color, item.isClosed)

	// Add current indicator
	var currentIndicator string
	if item.isCurrent {
		currentIndicator = ui.Muted.Render(" (current)")
	}

	_, _ = fmt.Fprint(w, cursor+statusText+currentIndicator)
}

// statusPickerModel is the model for the status picker view
type statusPickerModel struct {
	list          list.Model
	nibIDs       []string
	nibTitle     string
	currentStatus string
	width         int
	height        int
}

func newStatusPickerModel(nibIDs []string, nibTitle, currentStatus string, cfg *config.Config, width, height int) statusPickerModel {
	// Get all statuses (hardcoded in config package)
	statuses := config.DefaultStatuses

	delegate := statusItemDelegate{}

	// Build items list
	items := make([]list.Item, 0, len(statuses))
	selectedIndex := 0

	for i, s := range statuses {
		isCurrent := s.Name == currentStatus
		if isCurrent {
			selectedIndex = i
		}
		items = append(items, statusItem{
			name:        s.Name,
			description: s.Description,
			color:       s.Color,
			isClosed:    s.Closed,
			isCurrent:   isCurrent,
		})
	}

	// Calculate modal dimensions
	modalWidth := pickerModalWidth(width, 0, 0)
	modalHeight := pickerModalHeight(height, 0, 0)
	listWidth := modalWidth - 6
	listHeight := modalHeight - 7

	l := list.New(items, delegate, listWidth, listHeight)
	l.Title = "Select Status"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary)

	// Select the current status
	if selectedIndex < len(items) {
		l.Select(selectedIndex)
	}

	return statusPickerModel{
		list:          l,
		nibIDs:       nibIDs,
		nibTitle:     nibTitle,
		currentStatus: currentStatus,
		width:         width,
		height:        height,
	}
}

func (m statusPickerModel) Init() tea.Cmd {
	return nil
}

func (m statusPickerModel) Update(msg tea.Msg) (statusPickerModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		modalWidth := pickerModalWidth(msg.Width, 0, 0)
		modalHeight := pickerModalHeight(msg.Height, 0, 0)
		listWidth := modalWidth - 6
		listHeight := modalHeight - 7
		m.list.SetSize(listWidth, listHeight)

	case tea.KeyMsg:
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "enter":
				if item, ok := m.list.SelectedItem().(statusItem); ok {
					return m, func() tea.Msg {
						return statusSelectedMsg{nibIDs: m.nibIDs, status: item.name}
					}
				}
			case "esc", "backspace":
				return m, func() tea.Msg {
					return closeStatusPickerMsg{}
				}
			}
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m statusPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Reserve a fixed description-area height so the modal keeps a constant
	// height regardless of which status is selected (longer descriptions like
	// "deferred" would otherwise wrap to more lines and make items jump).
	var selected string
	var allDescs []string
	for _, li := range m.list.Items() {
		if item, ok := li.(statusItem); ok {
			allDescs = append(allDescs, item.description)
		}
	}
	if item, ok := m.list.SelectedItem().(statusItem); ok {
		selected = item.description
	}
	description := reservePickerDescription(selected, allDescs, pickerModalWidth(m.width, 0, 0))

	// For multi-select, don't show individual nib ID
	var nibID string
	if len(m.nibIDs) == 1 {
		nibID = m.nibIDs[0]
	}

	return renderPickerModal(pickerModalConfig{
		Title:       "Select Status",
		NibTitle:   m.nibTitle,
		NibID:      nibID,
		ListContent: m.list.View(),
		Description: description,
		Width:       m.width,
	})
}

// ModalView returns the picker rendered as a centered modal overlay on top of the background
func (m statusPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
