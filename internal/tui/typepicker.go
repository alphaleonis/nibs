package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/ui"
)

// validTypesForNib computes the valid types a nib can be changed to,
// considering both its parent (above) and its children (below).
func validTypesForNib(n *nib.Nib, backend Backend) []string {
	parentType := ""
	if n.Parent != "" {
		if p, err := backend.GetNib(context.Background(), n.Parent); err == nil && p != nil {
			parentType = p.EffectiveType()
		}
	}
	validFromParent := nibtypes.ValidChildTypes(parentType)

	children, _ := backend.GetChildren(context.Background(), n, nil)
	var childTypes []string
	for _, c := range children {
		childTypes = append(childTypes, c.EffectiveType())
	}
	validFromChildren := nibtypes.ValidParentTypesForChildren(childTypes)

	return intersectStrings(validFromParent, validFromChildren)
}

// typeSelectedMsg is sent when a type is selected from the picker
type typeSelectedMsg struct {
	nibIDs  []string
	nibType string
}

// closeTypePickerMsg is sent when the type picker is canceled
type closeTypePickerMsg struct{}

// openTypePickerMsg requests opening the type picker for nib(s)
type openTypePickerMsg struct {
	nibIDs      []string // IDs of nibs to update
	nibTitle    string   // Display title (single title or "N selected nibs")
	currentType string   // Only meaningful for single nib
	validTypes  []string // If non-empty, only show these types
}

// typeItem wraps a type for the picker
type typeItem struct {
	name        string
	description string
	color       string
	isCurrent   bool
}

// typePickerModel is the model for the type picker view: a cursor over items,
// with first-letter shortcuts.
type typePickerModel struct {
	items       []typeItem
	cursor      int
	nibIDs      []string
	nibTitle    string
	currentType string
	width       int
	height      int
}

func newTypePickerModel(nibIDs []string, nibTitle, currentType string, validTypes []string, width, height int) typePickerModel {
	types := config.DefaultTypes

	var validSet map[string]bool
	if len(validTypes) > 0 {
		validSet = make(map[string]bool, len(validTypes))
		for _, v := range validTypes {
			validSet[v] = true
		}
	}

	items := make([]typeItem, 0, len(types))
	cursor := 0

	for _, t := range types {
		if validSet != nil && !validSet[t.Name] {
			continue
		}
		isCurrent := t.Name == currentType
		if isCurrent {
			cursor = len(items)
		}
		items = append(items, typeItem{
			name:        t.Name,
			description: t.Description,
			color:       t.Color,
			isCurrent:   isCurrent,
		})
	}

	return typePickerModel{
		items:       items,
		cursor:      cursor,
		nibIDs:      nibIDs,
		nibTitle:    nibTitle,
		currentType: currentType,
		width:       width,
		height:      height,
	}
}

func (m typePickerModel) Init() tea.Cmd {
	return nil
}

func (m typePickerModel) Update(msg tea.Msg) (typePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				return m, func() tea.Msg {
					return typeSelectedMsg{nibIDs: m.nibIDs, nibType: item.name}
				}
			}
		case "esc", "backspace":
			return m, func() tea.Msg {
				return closeTypePickerMsg{}
			}
		default:
			// Single-key shortcut: first letter of type name selects it
			key := msg.String()
			if len(key) == 1 {
				for _, item := range m.items {
					if strings.HasPrefix(item.name, key) {
						nibType := item.name
						return m, func() tea.Msg {
							return typeSelectedMsg{nibIDs: m.nibIDs, nibType: nibType}
						}
					}
				}
			}
		}
	}

	return m, nil
}

// SelectedItem returns the highlighted item, or false when there are no items.
func (m typePickerModel) SelectedItem() (typeItem, bool) {
	if m.cursor < len(m.items) {
		return m.items[m.cursor], true
	}
	return typeItem{}, false
}

func (m typePickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var lines []string
	for i, item := range m.items {
		var cursor string
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
		} else {
			cursor = "  "
		}

		typeText := ui.RenderTypeText(item.name, item.color)

		var currentIndicator string
		if item.isCurrent {
			currentIndicator = ui.Muted.Render(" (current)")
		}

		lines = append(lines, cursor+typeText+currentIndicator)
	}
	listContent := strings.Join(lines, "\n")

	allDescs := make([]string, len(m.items))
	for i, item := range m.items {
		allDescs[i] = item.description
	}
	var selected string
	if item, ok := m.SelectedItem(); ok {
		selected = item.description
	}
	description := ui.Muted.Render(
		reservePickerDescription(selected, allDescs, pickerModalWidth(m.width, 0, 0)),
	)

	var nibID string
	if len(m.nibIDs) == 1 {
		nibID = m.nibIDs[0]
	}

	return renderPickerModal(pickerModalConfig{
		Title:       "Select Type",
		NibTitle:    m.nibTitle,
		NibID:       nibID,
		ListContent: listContent,
		Description: description,
		Width:       m.width,
	})
}

// ModalView returns the picker centered over bgView.
func (m typePickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
