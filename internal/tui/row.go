package tui

import (
	"fmt"
	"io"
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

const (
	// tagsColumnWidth is the width of the tags column.
	tagsColumnWidth = 20
	// minTitleWidth is the narrowest room for the tree prefix and title that
	// the tags column may leave. Below it the column is hidden.
	minTitleWidth = 30
)

// rowLayout is the widths a list row is drawn at. A row is the lead, the tree
// prefix and title, then the tags column when it is shown, then the ID.
type rowLayout struct {
	width   int // the whole row
	idWidth int // the widest ID as rows show it
	tags    int // the tags column, or 0 while it is hidden
}

func newRowLayout(width, idWidth int) rowLayout {
	l := rowLayout{width: width, idWidth: idWidth, tags: tagsColumnWidth}
	if l.titleWidth() < minTitleWidth {
		l.tags = 0
	}
	return l
}

// titleWidth is the cells left for the tree prefix and the title: the row
// less the lead, each column after the title and the space before each.
func (l rowLayout) titleWidth() int {
	w := l.width - rowLeadWidth - (l.idWidth + 1)
	if l.tags > 0 {
		w -= l.tags + 1
	}
	return max(0, w)
}

// itemDelegate renders a nibItem as one list row.
type itemDelegate struct {
	cfg          *config.Config
	display      display
	layout       rowLayout
	selectedNibs *map[string]bool // IDs marked for multi-select
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
	item, ok := listItem.(nibItem)
	if !ok {
		return
	}
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
	b.WriteString(treeStyle.Render(item.treePrefix) + titleStyle.Render(title) + pad)
	if d.layout.tags > 0 {
		tagsCellStyle := tagStyle
		if context {
			tagsCellStyle = ui.Muted
		}
		b.WriteString(" " + tagsCell(n.Tags, d.layout.tags, tagsCellStyle, ellipsis(ascii)))
	}
	id := d.display.id(n.ID)
	if context {
		id = ui.Muted.Render(id)
	}
	b.WriteString(" " + id)

	// Clip to the list width: the lead, the tree prefix and the ID do not
	// shrink, so a row can be wider than a narrow list.
	_, _ = fmt.Fprint(w, clipToWidth(b.String(), m.Width()))
}

// tagsCell renders tags in exactly width cells: the tags that fit whole, in
// style, then "+N" for the rest. A first tag that does not fit is cut, ending
// with mark.
func tagsCell(tags []string, width int, style lipgloss.Style, mark string) string {
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
	cell := style.Render(strings.Join(shown, " ")) + ui.Muted.Render(rest)
	return cell + strings.Repeat(" ", max(0, width-used-len(rest)))
}
