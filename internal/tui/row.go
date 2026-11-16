package tui

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// rowLeadWidth is the cells a list row spends before its tree prefix: the
// cursor, a space, the status, priority and blocked marks, a space, the type
// icon and a space.
const rowLeadWidth = 8

// minTitleWidth is the narrowest room for the tree prefix and title that the
// columns may leave. Columns that would leave less are dropped.
const minTitleWidth = 30

// rowLayout is the widths a list row is drawn at. A row is the lead, the tree
// prefix and title, then each column in columns.
type rowLayout struct {
	width   int           // the whole row
	columns []shownColumn // the columns after the title, in columnOrder
}

// shownColumn is a column a row draws, and its width.
type shownColumn struct {
	name  columnName
	width int
}

// newRowLayout lays out a row width cells wide that shows the columns of
// visible, at the widths widthOf gives. While the title would get fewer than
// minTitleWidth cells, it drops the rightmost column other than the ID.
func newRowLayout(width int, visible []columnName, widthOf func(columnName) int) rowLayout {
	l := rowLayout{width: width}
	for _, name := range columnOrder {
		if name != colTitle && slices.Contains(visible, name) {
			l.columns = append(l.columns, shownColumn{name: name, width: widthOf(name)})
		}
	}
	for l.titleWidth() < minTitleWidth {
		i := len(l.columns) - 1
		for i >= 0 && l.columns[i].name == colID {
			i--
		}
		if i < 0 {
			break
		}
		l.columns = slices.Delete(l.columns, i, i+1)
	}
	return l
}

// titleWidth is the cells left for the tree prefix and the title: the row
// less the lead, each column and the space before each.
func (l rowLayout) titleWidth() int {
	w := l.width - rowLeadWidth
	for _, c := range l.columns {
		w -= c.width + 1
	}
	return max(0, w)
}

// itemDelegate renders a nibItem as one list row.
type itemDelegate struct {
	cfg          *config.Config
	display      display
	layout       rowLayout
	selectedNibs *map[string]bool // IDs marked for multi-select
	find         []rune           // the find text as findNeedle returns it
}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

var (
	cursorStyle   = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	blockedStyle  = lipgloss.NewStyle().Foreground(ui.ColorDanger).Bold(true)
	blockingStyle = lipgloss.NewStyle().Foreground(ui.ColorWarning).Bold(true)
	tagStyle      = lipgloss.NewStyle().Foreground(ui.ColorCyan)
)

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	switch item := listItem.(type) {
	case nibItem:
		d.renderNib(w, m, index, item)
	case sectionItem:
		d.renderSection(w, m, index, item)
	}
}

// renderSection draws the row of a section no nib heads: its fold mark and
// its label, muted, with an area's color as a dot where a nib row draws its
// type. It has no columns.
func (d itemDelegate) renderSection(w io.Writer, m list.Model, index int, item sectionItem) {
	ascii := d.display.ascii()
	cursor := " "
	labelStyle := ui.Muted.Bold(true)
	if index == m.Index() {
		cursor = cursorStyle.Render(ui.GlyphCursor(ascii))
		labelStyle = labelStyle.Foreground(ui.ColorPrimary)
	}
	dot := " "
	if item.color != "" {
		dot = inColor(item.color, sectionDot(ascii))
	}
	room := d.layout.width - rowLeadWidth - lipgloss.Width(item.treePrefix)
	label := fitText(item.label, room, ellipsis(ascii))
	// A nib row's space after the cursor, its three marks and the space after them.
	line := cursor + "     " + dot + " " + ui.TreeLine.Render(item.treePrefix) + labelStyle.Render(label)
	_, _ = fmt.Fprint(w, clipToWidth(line, m.Width()))
}

// sectionDot returns the mark an area's row draws in the area's color.
func sectionDot(ascii bool) string {
	if ascii {
		return "o"
	}
	return "●"
}

// renderNib draws the row of a nib.
func (d itemDelegate) renderNib(w io.Writer, m list.Model, index int, item nibItem) {
	n := item.nib
	icons := d.display.icons
	ascii := d.display.ascii()
	// An ancestor shown only as context is drawn muted throughout.
	context := !item.matched

	cursor := " "
	if index == m.Index() {
		cursor = cursorStyle.Render(ui.GlyphCursor(ascii))
	}

	var status, priority, nibType string
	mark := " "
	if context {
		status = ui.Muted.Render(icons.status(n.Status))
		priority = ui.Muted.Render(icons.priority(n.EffectivePriority()))
		nibType = ui.Muted.Render(icons.nibType(n.EffectiveType()))
	} else {
		status = icons.renderStatus(d.cfg, n.Status)
		priority = icons.renderPriority(d.cfg, n.EffectivePriority())
		nibType = icons.renderType(d.cfg, n.EffectiveType())
		switch {
		case item.isBlocked:
			mark = blockedStyle.Render(ui.GlyphBlocked(ascii))
		case item.isBlocking:
			mark = blockingStyle.Render(ui.GlyphBlocking(ascii))
		}
	}

	room := d.layout.titleWidth() - lipgloss.Width(item.treePrefix)
	title := fitText(n.Title, room, ellipsis(ascii))
	pad := strings.Repeat(" ", max(0, room-lipgloss.Width(title)))

	treeStyle, titleStyle := ui.TreeLine, lipgloss.NewStyle()
	marked := d.selectedNibs != nil && (*d.selectedNibs)[n.ID]
	switch {
	case context:
		treeStyle, titleStyle = ui.Muted, ui.Muted
	case marked:
		titleStyle = titleStyle.Foreground(ui.ColorWarning)
	case index == m.Index():
		titleStyle = titleStyle.Foreground(ui.ColorPrimary)
	}
	if index == m.Index() {
		titleStyle = titleStyle.Bold(true)
	}

	var b strings.Builder
	b.WriteString(cursor + " " + status + priority + mark + " " + nibType + " ")
	b.WriteString(treeStyle.Render(item.treePrefix) + markFound(title, n.Title, d.find, titleStyle) + pad)
	for _, c := range d.layout.columns {
		b.WriteString(" " + d.cell(c, item, context))
	}

	// Clip to the list width: the lead, the tree prefix and the ID do not
	// shrink, so a row can be wider than a narrow list.
	_, _ = fmt.Fprint(w, clipToWidth(b.String(), m.Width()))
}

// cell renders column c of item in exactly c.width cells. A row drawn only as
// context draws every cell muted.
func (d itemDelegate) cell(c shownColumn, item nibItem, context bool) string {
	mark := ellipsis(d.display.ascii())
	if c.name == colTags {
		style := tagStyle
		if context {
			style = ui.Muted
		}
		return tagsCell(item.nib.Tags, c.width, style, mark, d.find)
	}
	full, style := d.cellText(c.name, item)
	if context {
		style = ui.Muted
	}
	text := fitText(full, c.width, mark)
	drawn := style.Render(text)
	if c.name == colID {
		drawn = markFound(text, full, d.find, style)
	}
	return drawn + strings.Repeat(" ", max(0, c.width-lipgloss.Width(text)))
}

// tagsCell renders tags in exactly width cells: the tags that fit whole, in
// style, then "+N" for the rest. A first tag that does not fit is cut, ending
// with mark. Matches for find are reversed; see markFound.
func tagsCell(tags []string, width int, style lipgloss.Style, mark string, find []rune) string {
	tags, _ = stripEach(tags)
	more := func(hidden int) string {
		if hidden == 0 {
			return ""
		}
		return fmt.Sprintf(" +%d", hidden)
	}
	var shown []string
	used := 0
	for i, tag := range tags {
		w := lipgloss.Width(tag)
		if used > 0 {
			w++
		}
		if used+w+len(more(len(tags)-i-1)) > width {
			break
		}
		shown = append(shown, tag)
		used += w
	}
	if len(shown) == 0 && len(tags) > 0 {
		cut := fitText(tags[0], width-len(more(len(tags)-1)), mark)
		shown, used = []string{cut}, lipgloss.Width(cut)
	}
	rest := more(len(tags) - len(shown))
	drawn := style.Render(strings.Join(shown, " "))
	if find != nil {
		marked := make([]string, len(shown))
		for i, tag := range shown {
			marked[i] = markFound(tag, tags[i], find, style)
		}
		drawn = strings.Join(marked, style.Render(" "))
	}
	cell := drawn + ui.Muted.Render(rest)
	return cell + strings.Repeat(" ", max(0, width-used-len(rest)))
}
