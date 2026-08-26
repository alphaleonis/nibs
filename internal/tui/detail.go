package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// Cached glamour renderer - initialized once per width
var (
	glamourRenderer     *glamour.TermRenderer
	glamourRendererOnce sync.Once
)

func getGlamourRenderer() *glamour.TermRenderer {
	glamourRendererOnce.Do(func() {
		var err error
		// Use DarkStyle instead of WithAutoStyle() to avoid slow terminal detection
		// that can cause multi-second delays in some terminals
		glamourRenderer, err = glamour.NewTermRenderer(glamour.WithStylePath("dark"))
		if err != nil {
			glamourRenderer = nil
		}
	})
	return glamourRenderer
}

// backToListMsg signals navigation back to the list
type backToListMsg struct{}

// resolvedLink represents a link with the target nib resolved
type resolvedLink struct {
	linkType string
	nib      *nib.Nib
	incoming bool // true if another nib links TO this one
}

// linkItem wraps a resolvedLink to implement list.Item
type linkItem struct {
	link  resolvedLink
	cfg   *config.Config
	width int
	cols  ui.ResponsiveColumns
	label string // pre-computed label like "Blocks:" or "Blocked by:"
}

func (i linkItem) Title() string       { return i.link.nib.Title }
func (i linkItem) Description() string { return i.link.nib.ID }
func (i linkItem) FilterValue() string { return i.link.nib.Title + " " + i.link.nib.ID + " " + i.label }

// linkDelegate handles rendering of link list items
type linkDelegate struct {
	cfg   *config.Config
	width int
	cols  ui.ResponsiveColumns
}

func (d linkDelegate) Height() int                             { return 1 }
func (d linkDelegate) Spacing() int                            { return 0 }
func (d linkDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d linkDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(linkItem)
	if !ok {
		return
	}

	link := item.link

	// Cursor indicator
	cursor := "  "
	if index == m.Index() {
		cursor = ui.Primary.Render(ui.GlyphSectionCursor())
	}

	// Format the link type label
	labelCol := lipgloss.NewStyle().Width(12).Render(ui.Muted.Render(item.label + ":"))

	// Get colors from config. EffectiveType so a type-less nib keeps its "task"
	// badge. Raw Priority is safe here despite the missing default: GetNibColors ->
	// GetPriority yields a different PriorityColor for "" (none) vs "normal" ("white"),
	// but that color is only ever consumed by RenderPrioritySymbol, which returns ""
	// (discarding the color) whenever GetPrioritySymbol is empty — and the symbol is
	// empty for both "" and "normal", so the rendered result is identical.
	colors := d.cfg.GetNibColors(link.nib.Status, link.nib.EffectiveType(), link.nib.Priority)

	// Calculate max title width using responsive columns
	baseWidth := d.cols.ID + d.cols.Status + d.cols.Type + 12 + 4 // label + cursor + padding
	if d.cols.ShowTags {
		baseWidth += d.cols.Tags
	}
	maxTitleWidth := max(10, d.width-baseWidth-8) // 8 for border padding

	// Use shared nib row rendering (without cursor, we handle it separately)
	row := ui.RenderNibRow(
		link.nib.ID,
		link.nib.Status,
		link.nib.EffectiveType(),
		link.nib.Title,
		ui.NibRowConfig{
			StatusColor:   colors.StatusColor,
			TypeColor:     colors.TypeColor,
			PriorityColor: colors.PriorityColor,
			Priority:      link.nib.Priority,
			IsClosed:      colors.IsClosed,
			MaxTitleWidth: maxTitleWidth,
			ShowCursor:    false,
			IsSelected:    false,
			Tags:          link.nib.Tags,
			ShowTags:      d.cols.ShowTags,
			TagsColWidth:  d.cols.Tags,
			MaxTags:       d.cols.MaxTags,
			UseFullNames:  true, // Full type/status names in detail view
		},
	)

	// Clipped, not left to wrap. maxTitleWidth has a floor of ten cells, so on a
	// narrow terminal the row is wider than the box whatever the title says —
	// and a wrapped row makes the box taller than the height it was sized to,
	// which is the one thing everything stacked around it is measured against.
	_, _ = fmt.Fprint(w, clipToWidth(cursor+labelCol+row, m.Width()))
}

// detailModel displays a single nib's details
type detailModel struct {
	viewport      viewport.Model
	nib           *nib.Nib
	backend       Backend
	config        *config.Config
	width         int
	height        int
	ready         bool
	links         []resolvedLink       // combined outgoing + incoming links
	linkList      list.Model           // list component for links (supports filtering)
	linksActive   bool                 // true = links section focused
	cols          ui.ResponsiveColumns // responsive column widths for links
	statusMessage string               // Status message to display in footer
	statusKind    statusKind           // How the footer colors that message
	helpExpanded  bool                 // Help panel state — set by App when ? is toggled
}

func newDetailModel(b *nib.Nib, backend Backend, cfg *config.Config, width, height int) detailModel {
	m := detailModel{
		nib:         b,
		backend:     backend,
		config:      cfg,
		width:       width,
		height:      height,
		ready:       true,
		linksActive: false,
	}

	// Resolve all links
	m.links = m.resolveAllLinks()

	// Check if any linked nibs have tags
	hasTags := false
	for _, link := range m.links {
		if len(link.nib.Tags) > 0 {
			hasTags = true
			break
		}
	}

	// Calculate responsive columns for links section
	// Account for the label column (12 chars) + cursor (2 chars) + border padding
	linkAreaWidth := width - 12 - 2 - 8
	m.cols = ui.CalculateResponsiveColumns(linkAreaWidth, hasTags)

	// Initialize link list with items
	m.linkList = m.createLinkList()

	// If there are links, select first one and focus links by default
	if len(m.links) > 0 {
		m.linksActive = true
	}

	// Calculate header height dynamically
	headerHeight := m.calculateHeaderHeight()
	footerHeight := 2
	vpWidth := width - 4
	vpHeight := height - headerHeight - footerHeight

	m.viewport = viewport.New(viewport.WithWidth(vpWidth), viewport.WithHeight(vpHeight))
	m.viewport.SetContent(m.renderBody(vpWidth))

	return m
}

// linkRows is how many link entries the box shows: all of them, up to a third
// of the terminal.
//
// It is the whole of the list's height. The list draws no title — "Linked Nibs"
// is redundant beside a list of nibs, and the row it took, plus the padding row
// beside it, is what held the box's floor at five: taller than an eight-row
// terminal can spare once the header and the footer have been paid for. The
// filter input shares that row and is drawn only while there is a filter to
// read, which is where linksBox adds the row back.
func (m detailModel) linkRows() int {
	return min(len(m.links), max(3, m.height/3))
}

// linksBox renders the bordered links box, or "" when the nib has no links.
//
// The border is the only thing on screen that says whether the links pane has
// focus, so it is drawn whatever the height; what gives instead is the box as a
// whole, which View drops when the frame cannot hold it.
//
// The list is sized on a copy rather than in place: contentFloor renders the
// box too, and a View that resized the stored list would move the floor the
// footer region was already measured against.
func (m detailModel) linksBox() string {
	if len(m.links) == 0 {
		return ""
	}

	l := m.linkList
	rows := m.linkRows()
	filtering := l.FilterState() == list.Filtering
	if filtering {
		// The filter input is drawn into the row the title used to hold, so the
		// box grows by it — and only for as long as there is a query to read.
		rows++
	}
	l.SetShowFilter(filtering)
	l.SetSize(m.width-8, rows)

	borderColor := ui.ColorMuted
	if m.linksActive {
		borderColor = ui.ColorPrimary
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(withBorder(m.width - 4)).
		Render(l.View())
}

// createLinkList creates a new list.Model for the links
func (m detailModel) createLinkList() list.Model {
	delegate := linkDelegate{
		cfg:   m.config,
		width: m.width,
		cols:  m.cols,
	}

	// Convert links to list items
	items := make([]list.Item, len(m.links))
	for i, link := range m.links {
		items[i] = linkItem{
			link:  link,
			cfg:   m.config,
			width: m.width,
			cols:  m.cols,
			label: m.formatLinkLabel(link.linkType, link.incoming),
		}
	}

	l := list.New(items, delegate, m.width-8, m.linkRows())
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(true)

	// The title bar's row is spent on the filter input alone — see linkRows —
	// so what is styled here is that input's surroundings, not a label.
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 1) // Left padding to align with header title
	applyFilterStyles(&l.Styles)
	l.Styles.NoItems = lipgloss.NewStyle()

	return l
}

func (m detailModel) Init() tea.Cmd {
	return nil
}

func (m detailModel) Update(msg tea.Msg) (detailModel, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Recalculate responsive columns for links
		hasTags := false
		for _, link := range m.links {
			if len(link.nib.Tags) > 0 {
				hasTags = true
				break
			}
		}
		linkAreaWidth := msg.Width - 12 - 2 - 8
		m.cols = ui.CalculateResponsiveColumns(linkAreaWidth, hasTags)

		// Update link list delegate with new dimensions
		m.updateLinkListDelegate()

		m.linkList.SetSize(msg.Width-8, m.linkRows())

		headerHeight := m.calculateHeaderHeight()
		helpHt := m.currentHelpHeight()
		footerHeight := 2 // "\n" + compact footer
		if helpHt > 0 {
			footerHeight = 1 + helpHt // "\n" + panel (replaces footer)
		}
		vpWidth := msg.Width - 4
		vpHeight := msg.Height - headerHeight - footerHeight

		// Ensure vpHeight doesn't go negative
		if vpHeight < 1 {
			vpHeight = 1
		}

		if !m.ready {
			m.viewport = viewport.New(viewport.WithWidth(vpWidth), viewport.WithHeight(vpHeight))
			m.viewport.SetContent(m.renderBody(vpWidth))
			m.ready = true
		} else {
			m.viewport.SetWidth(vpWidth)
			m.viewport.SetHeight(vpHeight)
			m.viewport.SetContent(m.renderBody(vpWidth))
		}

	case tea.KeyPressMsg:
		// If links list is filtering, let it handle all keys except quit
		if m.linksActive && m.linkList.FilterState() == list.Filtering {
			m.linkList, cmd = m.linkList.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "esc", "backspace":
			return m, func() tea.Msg {
				return backToListMsg{}
			}

		case "tab":
			// Toggle focus between links and body
			if len(m.links) > 0 {
				m.linksActive = !m.linksActive
			}
			return m, nil

		case "enter":
			// Navigate to selected link
			if m.linksActive {
				if item, ok := m.linkList.SelectedItem().(linkItem); ok {
					targetNib := item.link.nib
					return m, func() tea.Msg {
						return selectNibMsg{nib: targetNib}
					}
				}
			}

		case "p":
			// Open parent picker
			return m, func() tea.Msg {
				return openParentPickerMsg{
					nibIDs:        []string{m.nib.ID},
					nibTitle:      m.nib.Title,
					nibTypes:      []string{m.nib.EffectiveType()},
					currentParent: m.nib.Parent,
				}
			}

		case "s":
			// Open status picker
			return m, func() tea.Msg {
				return openStatusPickerMsg{
					nibIDs:        []string{m.nib.ID},
					nibTitle:      m.nib.Title,
					currentStatus: m.nib.Status,
				}
			}

		case "t":
			// Open type picker — filter to types valid for this nib's parent and children
			validTypes := validTypesForNib(m.nib, m.backend)
			return m, func() tea.Msg {
				return openTypePickerMsg{
					nibIDs:      []string{m.nib.ID},
					nibTitle:    m.nib.Title,
					currentType: m.nib.EffectiveType(),
					validTypes:  validTypes,
				}
			}

		case "P":
			// Open priority picker
			return m, func() tea.Msg {
				return openPriorityPickerMsg{
					nibIDs:          []string{m.nib.ID},
					nibTitle:        m.nib.Title,
					currentPriority: m.nib.EffectivePriority(),
				}
			}

		case "b":
			// Open blocking picker — compute current blocking from blockedBy scan
			currentBlocking := computeCurrentBlocking(m.backend, m.nib.ID)
			return m, func() tea.Msg {
				return openBlockingPickerMsg{
					nibID:           m.nib.ID,
					nibTitle:        m.nib.Title,
					currentBlocking: currentBlocking,
				}
			}

		case "E":
			// Open estimate picker
			return m, func() tea.Msg {
				return openEstimatePickerMsg{
					nibIDs:          []string{m.nib.ID},
					nibTitle:        m.nib.Title,
					currentEstimate: m.nib.Estimate,
				}
			}

		case "e":
			// Open editor for this nib
			return m, func() tea.Msg {
				return openEditorMsg{
					nibID:   m.nib.ID,
					nibPath: m.nib.Path,
				}
			}

		case "y":
			// Copy nib ID to clipboard
			return m, func() tea.Msg {
				return copyNibIDMsg{ids: []string{m.nib.ID}}
			}
		}
	}

	// Forward updates to the appropriate component
	if m.linksActive && len(m.links) > 0 {
		m.linkList, cmd = m.linkList.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// updateLinkListDelegate updates the link list delegate with current dimensions
func (m *detailModel) updateLinkListDelegate() {
	delegate := linkDelegate{
		cfg:   m.config,
		width: m.width,
		cols:  m.cols,
	}
	m.linkList.SetDelegate(delegate)
}

func (m detailModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header (nib info only, no links)
	header := m.renderHeader()

	// The links box and the body are drawn into whatever the header and the
	// footer region leave, and each is dropped when that is less than a box.
	// The frame is exactly as tall as the terminal, so the rows a wrapped status
	// message takes have to come from somewhere; measuring the footer first is
	// what makes their share a budget rather than a guess corrected afterwards.
	//
	// The links box takes its rows first, being the pane the view opens focused
	// on, and below its floor it goes whole rather than showing a box with no
	// link in it. That floor still does not always fit: on an eight-row terminal
	// the header, three rows of box and a footer wrapping a refusal across two
	// rows above its help row come to ten, so something has to go, and it is not
	// the message or the keys the reader acts on it with.
	avail := m.height - lipgloss.Height(header) - lipgloss.Height(m.footerRegion())
	linksSection := m.linksBox()
	if blockLines(linksSection) > avail {
		linksSection = ""
	}

	rows := []string{header}
	if linksSection != "" {
		rows = append(rows, linksSection)
	}
	avail -= blockLines(linksSection)

	// The body is rendered BEFORE the footer even though it is drawn above it,
	// because rendering it is what sizes the viewport to the height it is
	// painted at, and the footer's scroll percentage has to describe the body
	// the reader is looking at. Update's height is only an estimate — it
	// reserves for a header taller than the one that renders — so a percentage
	// taken against it says there is more below the fold when the whole body is
	// on screen. Measuring the footer above and painting it here are the same
	// rows either way: nothing in its height depends on the viewport.
	body := m.renderBodyBox(avail)
	footer := m.footerRegion()
	if body != "" {
		rows = append(rows, body)
	}
	return strings.Join(append(rows, footer), "\n")
}

// renderBodyBox draws the body into avail rows, or nothing at all when there
// are fewer of them than the box occupies.
//
// The body is what gives when the frame cannot hold everything. At the heights
// where that happens a status message is up, and what the reader needs is the
// nib's identity, the message, and the keys to act on it — prose is not legible
// in the row or two that would be left anyway. Taking the rows from the message
// instead would cut a refusal down to its first line, which is the defect the
// footer was taught to wrap for.
//
// The receiver is a pointer so the height it gives the viewport outlives the
// call: View renders the footer afterwards, and the percentage there has to be
// measured against the body that was actually painted. The model View was
// called on is untouched — View's own receiver is a copy.
func (m *detailModel) renderBodyBox(avail int) string {
	if avail < minBodyHeight {
		return ""
	}
	borderColor := ui.ColorMuted
	if !m.linksActive {
		borderColor = ui.ColorPrimary
	}
	m.viewport.SetHeight(avail - 2) // the border's two rows
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(withBorder(m.width - 4)).
		Render(m.viewport.View())
}

// footerRegion is everything drawn below the body: the compact footer, or —
// when the help panel is expanded — the status message with the panel beneath.
//
// The expanded panel carries esc/q/? and so replaces the footer's help row, but
// not its status message: a refusal the user cannot read is the defect the
// footer was taught to wrap for in the first place.
func (m detailModel) footerRegion() string {
	if !m.helpExpanded {
		return m.renderFooter()
	}
	return expandedFooterRegion(detailExpandedEntries(), m.statusMessage, m.statusKind, m.width, m.height, m.contentFloor())
}

// minBodyHeight is the fewest rows the body box occupies: its border, around a
// viewport of one row. Below it there is no box to draw, so View draws none.
const minBodyHeight = 3

// contentFloor is the rows the frame keeps above the footer region — the
// header, the links box when there is one, and the body's minimum — and is what
// the help panel's budget is held back for.
//
// The body does give its rows back to a taller region, but not to the panel: a
// reader looking up keybindings is looking them up for the nib in front of them,
// so the panel is budgeted as though the body stays. Only the status message
// takes them, and only when what is left cannot hold a box.
//
// The header is measured rather than taken from calculateHeaderHeight, which
// reserves two rows more than it renders: the sizing estimate may be generous,
// but a hold-back derived from it would narrow the panel for rows that are not
// actually spoken for.
//
// The links box is counted even at the geometries where View ends up dropping
// it. Whether it is dropped depends on the region's height, which is what this
// is being measured for, so counting it is the answer that does not chase its
// own tail — and it errs toward a smaller panel, never a frame that overruns.
func (m detailModel) contentFloor() int {
	return lipgloss.Height(m.renderHeader()) + minBodyHeight + blockLines(m.linksBox())
}

// renderFooter returns the abbreviated footer for the detail view.
func (m detailModel) renderFooter() string {
	scrollPct := int(m.viewport.ScrollPercent() * 100)
	footer := helpStyle.Render(fmt.Sprintf("%d%%", scrollPct)) + "  "
	if len(m.links) > 0 {
		footer += renderHelpKey("tab", "switch") + "  "
	}
	footer += renderHelpKey("e", "edit") + "  " +
		renderHelpKey("s", "status") + "  " +
		renderHelpKey("esc", "back") + "  " +
		renderHelpKey("?", "more") + "  " +
		renderHelpKey("q", "quit")

	if m.statusMessage != "" {
		// Above the help row, not in front of it: prepending pushed the row off
		// the right edge for as long as the message was up, and the keys it
		// names are how the reader acts on what the message says.
		footer = renderStatusMessage(m.statusMessage, m.statusKind, m.width, maxStatusFooterLines(m.height)) + "\n" + footer
	}
	// The help row is a fixed set of key hints, so it is the same cells wide at
	// every terminal width and overruns a narrow one on its own. The status
	// message above it already wraps to the width; this is the row that does not.
	return clipToWidth(footer, m.width)
}

// currentHelpHeight returns the help panel height (0 when collapsed).
func (m detailModel) currentHelpHeight() int {
	if !m.helpExpanded {
		return 0
	}
	return helpPanelHeight(detailExpandedEntries(), m.width, helpRowBudget(m.height, m.contentFloor(), 0))
}

func (m detailModel) calculateHeaderHeight() int {
	// Base: title line + ID/status line + borders/padding = ~6
	baseHeight := 6

	// Documents line adds 1 extra line when present
	if len(m.nib.Documents) > 0 {
		baseHeight++
	}

	// Add height for links section (separate bordered box)
	if len(m.links) > 0 {
		// Link entries + 3 for borders and spacing
		baseHeight += m.linkRows() + 3
	}

	return baseHeight
}

func (m detailModel) renderHeader() string {
	// Title
	title := detailTitleStyle.Render(m.nib.Title)

	// ID
	id := ui.ID.Render(m.nib.ID)

	// Status badge
	statusCfg := m.config.GetStatus(m.nib.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isClosed := m.config.IsClosedStatus(m.nib.Status)
	status := ui.RenderStatusWithColor(m.nib.Status, statusColor, isClosed)

	// Estimate badge
	var estimate string
	if m.nib.Estimate != "" {
		estimateCfg := m.config.GetEstimate(m.nib.Estimate)
		estimateColor := "gray"
		if estimateCfg != nil {
			estimateColor = estimateCfg.Color
		}
		estimate = ui.RenderEstimateWithColor(m.nib.Estimate, estimateColor)
	}

	// Build header content
	var headerContent strings.Builder
	headerContent.WriteString(title)
	headerContent.WriteString("\n")
	headerContent.WriteString(id + "  " + status)
	if estimate != "" {
		headerContent.WriteString(" " + estimate)
	}

	// Add tags if present
	if len(m.nib.Tags) > 0 {
		headerContent.WriteString("  ")
		headerContent.WriteString(ui.RenderTags(m.nib.Tags))
	}

	// Add documents if present
	if len(m.nib.Documents) > 0 {
		headerContent.WriteString("\n")
		headerContent.WriteString(ui.RenderDocuments(m.nib.Documents))
	}

	// Header box style - always muted border (not focused, links section is separate)
	headerBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorMuted).
		Padding(0, 1).
		Width(withBorder(m.width - 4))

	return headerBox.Render(headerContent.String())
}

// formatLinkLabel returns a human-readable label for the link type
func (m detailModel) formatLinkLabel(linkType string, incoming bool) string {
	if incoming {
		switch linkType {
		case "blocking":
			return "Blocked by"
		case "parent":
			return "Child"
		default:
			return linkType + " (incoming)"
		}
	}

	// Outgoing labels - capitalize first letter
	switch linkType {
	case "blocking":
		return "Blocking"
	case "parent":
		return "Parent"
	default:
		return linkType
	}
}

func (m detailModel) resolveAllLinks() []resolvedLink {
	var links []resolvedLink
	ctx := context.Background()

	// Drop satisfied blocking relationships. This is the *releasing* set
	// (completed, scrapped) — narrower than closed, because a deferred nib is
	// closed but still blocks. Derived from config so it tracks the status
	// model, not a literal, and matches what the blockedBy/blocking resolvers
	// already filter on.
	activeOnly := &model.NibFilter{
		ExcludeStatus: m.config.ReleasingStatusNames(),
	}

	// Resolve outgoing links via backend
	if blocking, _ := m.backend.GetBlocking(ctx, m.nib, activeOnly); blocking != nil {
		for _, b := range blocking {
			links = append(links, resolvedLink{linkType: "blocking", nib: b, incoming: false})
		}
	}
	if parent, _ := m.backend.GetParent(ctx, m.nib); parent != nil {
		links = append(links, resolvedLink{linkType: "parent", nib: parent, incoming: false})
	}

	// Resolve incoming links via backend
	if blockedBy, _ := m.backend.GetBlockedBy(ctx, m.nib, activeOnly); blockedBy != nil {
		for _, b := range blockedBy {
			links = append(links, resolvedLink{linkType: "blocking", nib: b, incoming: true})
		}
	}
	if children, _ := m.backend.GetChildren(ctx, m.nib, nil); children != nil {
		for _, b := range children {
			links = append(links, resolvedLink{linkType: "parent", nib: b, incoming: true})
		}
	}

	// Sort all links by link type label first, then by nib status/type/title
	// This keeps link categories together while ordering nibs consistently with the main list
	statusNames := m.config.StatusNames()
	typeNames := m.config.TypeNames()
	sort.Slice(links, func(i, j int) bool {
		// First: group by link label (e.g., "Child", "Parent", "Blocks", etc.)
		labelI := m.formatLinkLabel(links[i].linkType, links[i].incoming)
		labelJ := m.formatLinkLabel(links[j].linkType, links[j].incoming)
		if labelI != labelJ {
			return labelI < labelJ
		}
		// Within same link type: sort by status, priority, type, then title
		return compareNibsByStatusPriorityAndType(links[i].nib, links[j].nib, statusNames, typeNames, m.config)
	})

	return links
}

// compareNibsByStatusPriorityAndType compares two nibs using the same ordering as nib.SortByStatusPriorityAndType.
func compareNibsByStatusPriorityAndType(a, b *nib.Nib, statusNames, typeNames []string, ranker nib.PriorityRanker) bool {
	// Build order maps
	statusOrder := make(map[string]int)
	for i, s := range statusNames {
		statusOrder[s] = i
	}
	typeOrder := make(map[string]int)
	for i, t := range typeNames {
		typeOrder[t] = i
	}

	// Helper to get order with unrecognized values sorted last
	getStatusOrder := func(status string) int {
		if order, ok := statusOrder[status]; ok {
			return order
		}
		return len(statusNames)
	}
	getTypeOrder := func(typ string) int {
		if order, ok := typeOrder[typ]; ok {
			return order
		}
		return len(typeNames)
	}

	// Primary: status order
	oi, oj := getStatusOrder(a.Status), getStatusOrder(b.Status)
	if oi != oj {
		return oi < oj
	}
	// Secondary: priority order
	pi, pj := ranker.PriorityRank(a.Priority), ranker.PriorityRank(b.Priority)
	if pi != pj {
		return pi < pj
	}
	// Tertiary: type order (EffectiveType so a type-less nib sorts as "task")
	ti, tj := getTypeOrder(a.EffectiveType()), getTypeOrder(b.EffectiveType())
	if ti != tj {
		return ti < tj
	}
	// Quaternary: title (case-insensitive)
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}

func (m detailModel) renderBody(_ int) string {
	// TrimSpace, not == "": Parse hands the body back verbatim, so a body
	// that is only blank lines is a stable value rather than one that
	// converges to empty, and it renders to nothing at all.
	if strings.TrimSpace(m.nib.Body) == "" {
		return lipgloss.NewStyle().
			Foreground(ui.ColorMuted).
			Padding(0, 1).
			Render("No description")
	}

	renderer := getGlamourRenderer()
	if renderer == nil {
		return m.nib.Body
	}

	rendered, err := renderer.Render(m.nib.Body)
	if err != nil {
		return m.nib.Body
	}

	// Trim only the blank lines glamour pads the document with. TrimSpace would
	// also eat the left margin off the first line: glamour v2 writes that margin
	// as plain spaces ahead of any style escape, so the opening heading would sit
	// flush against the pane border while every line below it stayed indented.
	return strings.Trim(rendered, "\n")
}
