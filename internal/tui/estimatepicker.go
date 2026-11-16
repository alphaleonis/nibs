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

// estimateSelectedMsg is sent when an estimate is selected from the picker
type estimateSelectedMsg struct {
	nibIDs   []string
	estimate string
}

// closeEstimatePickerMsg is sent when the estimate picker is canceled
type closeEstimatePickerMsg struct{}

// openEstimatePickerMsg requests opening the estimate picker for nib(s)
type openEstimatePickerMsg struct {
	nibIDs          []string
	nibTitle        string
	currentEstimate string
}

// estimateItem wraps an estimate to implement list.Item
type estimateItem struct {
	name        string
	description string
	color       string
	isCurrent   bool
	key         string
}

func (i estimateItem) accelerator() string { return i.key }
func (i estimateItem) Title() string       { return i.name }
func (i estimateItem) Description() string { return i.description }
func (i estimateItem) FilterValue() string { return i.name + " " + i.description }

// estimateItemDelegate handles rendering of estimate picker items
type estimateItemDelegate struct{}

func (d estimateItemDelegate) Height() int                             { return 1 }
func (d estimateItemDelegate) Spacing() int                            { return 0 }
func (d estimateItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d estimateItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(estimateItem)
	if !ok {
		return
	}

	var cursor string
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	} else {
		cursor = "  "
	}

	estimateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(item.color))
	estimateText := estimateStyle.Render(item.name)

	var currentIndicator string
	if item.isCurrent {
		currentIndicator = ui.Muted.Render(" (current)")
	}

	_, _ = fmt.Fprint(w, cursor+renderAccelerator(item.key)+estimateText+currentIndicator)
}

// estimatePickerModel is the model for the estimate picker view
type estimatePickerModel struct {
	table           []action // the keymap in effect
	list            list.Model
	nibIDs          []string
	nibTitle        string
	currentEstimate string
	width           int
	height          int
}

func newEstimatePickerModel(table []action, nibIDs []string, nibTitle, currentEstimate string, width, height int) estimatePickerModel {
	estimates := config.DefaultEstimates

	delegate := estimateItemDelegate{}

	hints := make([]string, len(estimates))
	for i, e := range estimates {
		hints[i] = e.Name
	}
	keys := assignAccelerators(table, ctxEstimatePicker, hints)

	items := make([]list.Item, 0, len(estimates))
	selectedIndex := 0

	for i, e := range estimates {
		isCurrent := e.Name == currentEstimate
		if isCurrent {
			selectedIndex = i
		}
		items = append(items, estimateItem{
			name:        e.Name,
			description: e.Description,
			color:       e.Color,
			isCurrent:   isCurrent,
			key:         keys[i],
		})
	}

	modalWidth := pickerModalWidth(width, 0, 0)
	modalHeight := pickerModalHeight(height, 0, 0)
	listWidth := modalWidth - 6
	listHeight := modalHeight - 7

	l := list.New(items, delegate, listWidth, listHeight)
	l.Title = "Select Estimate"
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

	return estimatePickerModel{
		table:           table,
		list:            l,
		nibIDs:          nibIDs,
		nibTitle:        nibTitle,
		currentEstimate: currentEstimate,
		width:           width,
		height:          height,
	}
}

func (m estimatePickerModel) Init() tea.Cmd {
	return nil
}

func (m estimatePickerModel) Update(msg tea.Msg) (estimatePickerModel, tea.Cmd) {
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
			if item, ok := m.list.SelectedItem().(estimateItem); ok {
				return m, func() tea.Msg {
					return estimateSelectedMsg{nibIDs: m.nibIDs, estimate: item.name}
				}
			}
		case actLeave:
			return m, func() tea.Msg { return closeEstimatePickerMsg{} }
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

func (m estimatePickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	return renderPickerModal(m.modal())
}

// modal is the modal View draws.
func (m estimatePickerModel) modal() pickerModalConfig {
	var selected string
	var allDescs []string
	for _, li := range m.list.Items() {
		if item, ok := li.(estimateItem); ok {
			allDescs = append(allDescs, item.description)
		}
	}
	if item, ok := m.list.SelectedItem().(estimateItem); ok {
		selected = item.description
	}
	description := reservePickerDescription(selected, allDescs, pickerModalWidth(m.width, 0, 0))

	var nibID string
	if len(m.nibIDs) == 1 {
		nibID = m.nibIDs[0]
	}

	return pickerModalConfig{
		Keys:        m.table,
		Context:     ctxEstimatePicker,
		Title:       "Select Estimate",
		NibTitle:    m.nibTitle,
		NibID:       nibID,
		ListContent: m.list.View(),
		Rows:        listRows(m.list),
		Description: description,
		Width:       m.width,
	}
}
