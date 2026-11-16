package tui

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// prioritySelectedMsg is sent when a priority is selected from the picker
type prioritySelectedMsg struct {
	nibIDs   []string
	priority string
}

// closePriorityPickerMsg is sent when the priority picker is canceled
type closePriorityPickerMsg struct{}

// openPriorityPickerMsg requests opening the priority picker for nib(s)
type openPriorityPickerMsg struct {
	nibIDs          []string // IDs of nibs to update
	nibTitle        string   // Display title (single title or "N selected nibs")
	currentPriority string   // Only meaningful for single nib
}

// priorityItem wraps a priority to implement list.Item
type priorityItem struct {
	name        string
	description string
	color       string
	isCurrent   bool
	key         string
}

func (i priorityItem) accelerator() string { return i.key }
func (i priorityItem) Title() string       { return i.name }
func (i priorityItem) Description() string { return i.description }
func (i priorityItem) FilterValue() string { return i.name + " " + i.description }

// priorityItemDelegate handles rendering of priority picker items
type priorityItemDelegate struct{}

func (d priorityItemDelegate) Height() int                             { return 1 }
func (d priorityItemDelegate) Spacing() int                            { return 0 }
func (d priorityItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d priorityItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(priorityItem)
	if !ok {
		return
	}

	var cursor string
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	} else {
		cursor = "  "
	}

	priorityStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(item.color))
	priorityText := priorityStyle.Render(item.name)

	var currentIndicator string
	if item.isCurrent {
		currentIndicator = ui.Muted.Render(" (current)")
	}

	_, _ = fmt.Fprint(w, cursor+renderAccelerator(item.key)+priorityText+currentIndicator)
}

// priorityPickerModel is the model for the priority picker view
type priorityPickerModel struct {
	table           []action // the keymap in effect
	list            list.Model
	nibIDs          []string
	nibTitle        string
	currentPriority string
	width           int
	height          int
}

func newPriorityPickerModel(table []action, nibIDs []string, nibTitle, currentPriority string, width, height int) priorityPickerModel {
	priorities := config.DefaultPriorities

	delegate := priorityItemDelegate{}

	hints := make([]string, len(priorities))
	for i, p := range priorities {
		hints[i] = p.Name
	}
	keys := assignAccelerators(table, ctxPriorityPicker, hints)

	items := make([]list.Item, 0, len(priorities))
	selectedIndex := 0

	for i, p := range priorities {
		isCurrent := p.Name == currentPriority
		if isCurrent {
			selectedIndex = i
		}
		items = append(items, priorityItem{
			name:        p.Name,
			description: p.Description,
			color:       p.Color,
			isCurrent:   isCurrent,
			key:         keys[i],
		})
	}

	modalWidth := pickerModalWidth(width, 0, 0)
	modalHeight := pickerModalHeight(height, 0, 0)
	listWidth := modalWidth - 6
	listHeight := modalHeight - 7

	l := list.New(items, delegate, listWidth, listHeight)
	l.Title = "Select Priority"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap = listKeyMap()
	l.SetShowPagination(false)
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	applyFilterStyles(&l.Styles)

	if selectedIndex < len(items) {
		l.Select(selectedIndex)
	}

	return priorityPickerModel{
		table:           table,
		list:            l,
		nibIDs:          nibIDs,
		nibTitle:        nibTitle,
		currentPriority: currentPriority,
		width:           width,
		height:          height,
	}
}

func (m priorityPickerModel) Init() tea.Cmd {
	return nil
}

func (m priorityPickerModel) Update(msg tea.Msg) (priorityPickerModel, tea.Cmd) {
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

	case actionMsg:
		if cmd, ok := browseList(&m.list, msg.name); ok {
			return m, cmd
		}
		switch msg.name {
		case actOpen:
			if item, ok := m.list.SelectedItem().(priorityItem); ok {
				return m, func() tea.Msg {
					return prioritySelectedMsg{nibIDs: m.nibIDs, priority: item.name}
				}
			}
		case actLeave:
			return m, func() tea.Msg { return closePriorityPickerMsg{} }
		}
		return m, nil

	case clickRowMsg:
		m.list.Select(msg.row)
		if !msg.choose {
			return m, nil
		}
		return m.Update(actionMsg{name: actOpen})

	case acceleratorMsg:
		if selectAccelerator(&m.list, nil, msg.key) {
			return m.Update(actionMsg{name: actOpen})
		}
		return m, nil

	case tea.KeyPressMsg:
		// Only a filter being typed takes keys; actions do everything else.
		if m.list.FilterState() != list.Filtering {
			return m, nil
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m priorityPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m priorityPickerModel) modal() pickerModalConfig {
	var selected string
	var allDescs []string
	for _, li := range m.list.Items() {
		if item, ok := li.(priorityItem); ok {
			allDescs = append(allDescs, item.description)
		}
	}
	if item, ok := m.list.SelectedItem().(priorityItem); ok {
		selected = item.description
	}
	description := reservePickerDescription(selected, allDescs, pickerModalWidth(m.width, 0, 0))

	var nibID string
	if len(m.nibIDs) == 1 {
		nibID = m.nibIDs[0]
	}

	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxPriorityPicker,
		Title:       "Select Priority",
		NibTitle:    m.nibTitle,
		NibID:       nibID,
		ListContent: m.list.View(),
		Rows:        listRows(m.list),
		Description: description,
		Width:       m.width,
	}
}
