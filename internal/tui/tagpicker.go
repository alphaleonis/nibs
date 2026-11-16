package tui

import (
	"fmt"
	"io"
	"sort"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/ui"
)

// tagWithCount holds a tag and its usage count
type tagWithCount struct {
	tag   string
	count int
}

// tagItem wraps a tag with count to implement list.Item
type tagItem struct {
	tag   string
	count int
}

func (i tagItem) Title() string       { return i.tag }
func (i tagItem) Description() string { return "" }
func (i tagItem) FilterValue() string { return i.tag }

// tagItemDelegate handles rendering of tag items
type tagItemDelegate struct {
	pageKeys []string
}

func (d tagItemDelegate) Height() int                             { return 1 }
func (d tagItemDelegate) Spacing() int                            { return 0 }
func (d tagItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d tagItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(tagItem)
	if !ok {
		return
	}

	var cursor string
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	} else {
		cursor = "  "
	}

	cursor += renderAccelerator(rowAccelerator(m, d.pageKeys, index))

	tagBadge := ui.RenderTag(item.tag)
	count := ui.Muted.Render(fmt.Sprintf(" (%d)", item.count))

	_, _ = fmt.Fprint(w, cursor+tagBadge+count)
}

// tagPickerModel is the model for the tag picker view
type tagPickerModel struct {
	table    []action // the keymap in effect
	list     list.Model
	tags     []tagWithCount
	pageKeys []string // the accelerators of the rows on a page, in order
	width    int
	height   int
}

func newTagPickerModel(table []action, tags []tagWithCount, width, height int) tagPickerModel {
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].count != tags[j].count {
			return tags[i].count > tags[j].count
		}
		return tags[i].tag < tags[j].tag
	})

	pageKeys := freeAccelerators(table, ctxTagPicker)
	delegate := tagItemDelegate{pageKeys: pageKeys}

	items := make([]list.Item, len(tags))
	for i, t := range tags {
		items[i] = tagItem(t)
	}

	l := list.New(items, delegate, width-4, height-6)
	l.Title = "Select a Tag"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap = listKeyMap()
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 1)
	applyFilterStyles(&l.Styles)

	return tagPickerModel{
		table:    table,
		list:     l,
		tags:     tags,
		pageKeys: pageKeys,
		width:    width,
		height:   height,
	}
}

func (m tagPickerModel) Init() tea.Cmd {
	return nil
}

func (m tagPickerModel) Update(msg tea.Msg) (tagPickerModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width-4, msg.Height-6)

	case actionMsg:
		if cmd, ok := browseList(&m.list, msg.name); ok {
			return m, cmd
		}
		switch msg.name {
		case actOpen:
			if item, ok := m.list.SelectedItem().(tagItem); ok {
				return m, func() tea.Msg { return tagSelectedMsg{tag: item.tag} }
			}
		case actLeave:
			return m, func() tea.Msg { return returnToListMsg{} }
		}
		return m, nil

	case acceleratorMsg:
		if selectAccelerator(&m.list, m.pageKeys, msg.key) {
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

func (m tagPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Width(withBorder(m.width - 2)).
		Height(withBorder(m.height - 4))

	content := border.Render(m.list.View())

	help := hintRowWithin(m.table, actionState{ctx: ctxTagPicker}, m.width)

	return content + "\n" + help
}
