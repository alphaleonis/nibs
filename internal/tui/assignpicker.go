package tui

import (
	"context"
	"fmt"
	"io"
	"slices"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// assignField is a section assignment the modify picker sets: the milestone
// that schedules a nib, or the area that owns it.
type assignField struct {
	name   string     // the modify picker's row label
	title  string     // name, capitalized
	ctx    keyContext // the key context of the field's picker
	stored func(*nib.Nib) string
	// choices are the values the field's picker offers targets, without the
	// row that clears the field.
	choices func(b Backend, cfg *config.Config, targets []*nib.Nib) []assignChoice
	// set puts value into an update. An empty value clears the field.
	set func(input *model.UpdateNibInput, value string)
}

var milestoneField = &assignField{
	name:    "milestone",
	title:   "Milestone",
	ctx:     ctxMilestonePicker,
	stored:  func(n *nib.Nib) string { return n.Milestone },
	choices: milestoneChoices,
	set: func(input *model.UpdateNibInput, value string) {
		input.Milestone = graphql.OmittableOf(&value)
	},
}

var areaField = &assignField{
	name:    "area",
	title:   "Area",
	ctx:     ctxAreaPicker,
	stored:  func(n *nib.Nib) string { return n.Area },
	choices: areaChoices,
	set: func(input *model.UpdateNibInput, value string) {
		input.Area = graphql.OmittableOf(&value)
	},
}

// assignChoice is one value an assign picker offers.
type assignChoice struct {
	value  string // what choosing it writes, as stored
	label  string
	note   string // muted text after the label
	closed bool   // whether the label is drawn muted
}

// milestoneChoices offers every milestone but the targets, in the order of
// their order keys, as the web's milestone picker does. A closed milestone is
// offered too, with its status, and the server refuses an assignment its
// status does not accept.
func milestoneChoices(b Backend, cfg *config.Config, targets []*nib.Nib) []assignChoice {
	all, _ := b.ListNibs(context.Background(), nil)
	var milestones []*nib.Nib
	for _, n := range all {
		isTarget := slices.ContainsFunc(targets, func(t *nib.Nib) bool { return t.ID == n.ID })
		if n.EffectiveType() == "milestone" && !isTarget {
			milestones = append(milestones, n)
		}
	}
	nib.SortByOrder(milestones)
	choices := make([]assignChoice, len(milestones))
	for i, ms := range milestones {
		c := assignChoice{value: ms.ID, label: ms.Title, note: "(" + displayID(ms.ID) + ")"}
		if cfg.IsClosedStatus(ms.Status) {
			c.closed = true
			c.note = "(" + displayID(ms.ID) + ", " + ms.Status + ")"
		}
		choices[i] = c
	}
	return choices
}

// areaChoices offers every declared area by its whole path.
func areaChoices(b Backend, _ *config.Config, _ []*nib.Nib) []assignChoice {
	paths := b.Areas().Paths()
	choices := make([]assignChoice, len(paths))
	for i, path := range paths {
		choices[i] = assignChoice{value: path, label: area.RenderPath(path)}
	}
	return choices
}

// openAssignPickerMsg requests field's picker for targets.
type openAssignPickerMsg struct {
	field   *assignField
	targets modifyTargets
}

// assignChosenMsg writes value to field of the nibs in nibIDs. An empty value
// clears the field.
type assignChosenMsg struct {
	field  *assignField
	nibIDs []string
	value  string
}

// closeAssignPickerMsg is sent when an assign picker is canceled.
type closeAssignPickerMsg struct{}

// assignItem is a picker row: a choice, or the row that clears the field.
type assignItem struct {
	choice  assignChoice
	clears  bool
	current bool
	field   string
}

func (i assignItem) Title() string {
	if i.clears {
		return "(No " + i.field + ")"
	}
	return i.choice.label
}

func (i assignItem) Description() string { return i.choice.note }

func (i assignItem) FilterValue() string {
	if i.clears {
		return "none clear"
	}
	return i.choice.label + " " + i.choice.value
}

type assignItemDelegate struct {
	pageKeys []string
}

func (d assignItemDelegate) Height() int                             { return 1 }
func (d assignItemDelegate) Spacing() int                            { return 0 }
func (d assignItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d assignItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(assignItem)
	if !ok {
		return
	}
	cursor := "  "
	if index == m.Index() {
		cursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true).Render("▌") + " "
	}
	cursor += renderAccelerator(rowAccelerator(m, d.pageKeys, index))

	var suffix string
	if item.choice.note != "" {
		suffix += " " + item.choice.note
	}
	if item.current {
		suffix += " (current)"
	}
	// The row must fit on one line, because Height reports one. The title gives
	// way first, so the id stays readable.
	title := truncateTitle(item.Title(), max(1, m.Width()-lipgloss.Width(cursor)-lipgloss.Width(suffix)))
	if item.clears || item.choice.closed {
		title = ui.Muted.Render(title)
	}
	_, _ = fmt.Fprint(w, truncateTitle(cursor+title+ui.Muted.Render(suffix), m.Width()))
}

// assignPickerModel picks the value of one assignField for the nibs it was
// opened on.
type assignPickerModel struct {
	table    []action // the keymap in effect
	field    *assignField
	list     list.Model
	nibIDs   []string
	nibTitle string
	pageKeys []string // the accelerators of the rows on a page, in order
	width    int
	height   int
}

func newAssignPickerModel(table []action, field *assignField, targets modifyTargets, b Backend, cfg *config.Config, width, height int) assignPickerModel {
	current, mixed := sharedValue(targets.nibs, field.stored)
	items := []list.Item{assignItem{clears: true, field: field.name}}
	selected := 0
	for _, c := range field.choices(b, cfg, targets.nibs) {
		isCurrent := !mixed && current != "" && c.value == current
		if isCurrent {
			selected = len(items)
		}
		items = append(items, assignItem{choice: c, current: isCurrent, field: field.name})
	}

	pageKeys := freeAccelerators(table, field.ctx)
	modalWidth := pickerModalWidth(width, 60, 80)
	modalHeight := pickerModalHeight(height, 60, 20)
	l := list.New(items, assignItemDelegate{pageKeys: pageKeys}, modalWidth-6, modalHeight-7)
	l.Title = "Select " + field.title
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap = listKeyMap()
	l.SetShowPagination(false)
	l.Styles.Title = listTitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 0)
	applyFilterStyles(&l.Styles)
	l.Select(selected)

	return assignPickerModel{
		table:    table,
		field:    field,
		list:     l,
		nibIDs:   targets.ids(),
		nibTitle: targets.title,
		pageKeys: pageKeys,
		width:    width,
		height:   height,
	}
}

func (m assignPickerModel) Update(msg tea.Msg) (assignPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(pickerModalWidth(msg.Width, 60, 80)-6, pickerModalHeight(msg.Height, 60, 20)-7)
		return m, nil

	case actionMsg:
		if cmd, ok := browseList(&m.list, msg.name); ok {
			return m, cmd
		}
		switch msg.name {
		case actOpen:
			if item, ok := m.list.SelectedItem().(assignItem); ok {
				chosen := assignChosenMsg{field: m.field, nibIDs: m.nibIDs, value: item.choice.value}
				return m, func() tea.Msg { return chosen }
			}
		case actLeave:
			return m, func() tea.Msg { return closeAssignPickerMsg{} }
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

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m assignPickerModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	var nibID string
	if len(m.nibIDs) == 1 {
		nibID = m.nibIDs[0]
	}
	return renderPickerModal(pickerModalConfig{
		Keys:        m.table,
		Context:     m.field.ctx,
		NibTitle:    m.nibTitle,
		NibID:       nibID,
		ListContent: m.list.View(),
		Width:       m.width,
		WidthPct:    60,
		MaxWidth:    80,
	})
}

// ModalView returns the picker centered over bgView.
func (m assignPickerModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	return overlayModal(bgView, m.View(), fullWidth, fullHeight)
}
