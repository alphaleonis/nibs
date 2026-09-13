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

// glamourRenderer is built once and wraps at glamour's default width, not the
// terminal's.
var (
	glamourRenderer     *glamour.TermRenderer
	glamourRendererOnce sync.Once
)

func getGlamourRenderer() *glamour.TermRenderer {
	glamourRendererOnce.Do(func() {
		var err error
		glamourRenderer, err = glamour.NewTermRenderer(glamour.WithStylePath("dark"))
		if err != nil {
			glamourRenderer = nil
		}
	})
	return glamourRenderer
}

// backToListMsg returns to the previous detail view, or to the list when there
// is none.
type backToListMsg struct{}

type resolvedLink struct {
	linkType string
	nib      *nib.Nib
	incoming bool // true if another nib links TO this one
}

type linkItem struct {
	link  resolvedLink
	cfg   *config.Config
	width int
	cols  ui.ResponsiveColumns
	label string // from formatLinkLabel
}

func (i linkItem) Title() string       { return i.link.nib.Title }
func (i linkItem) Description() string { return i.link.nib.ID }
func (i linkItem) FilterValue() string { return i.link.nib.Title + " " + i.link.nib.ID + " " + i.label }

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

	cursor := "  "
	if index == m.Index() {
		cursor = ui.Primary.Render(ui.GlyphSectionCursor())
	}

	labelCol := lipgloss.NewStyle().Width(12).Render(ui.Muted.Render(item.label + ":"))

	// EffectiveType so a type-less nib keeps its "task" badge. Raw Priority is
	// fine: "" and "normal" both render no priority symbol, so the color that
	// differs between them is never drawn.
	colors := d.cfg.GetNibColors(link.nib.Status, link.nib.EffectiveType(), link.nib.Priority)

	baseWidth := d.cols.ID + d.cols.Status + d.cols.Type + 12 + 4 // label + cursor + padding
	if d.cols.ShowTags {
		baseWidth += d.cols.Tags
	}
	maxTitleWidth := max(10, d.width-baseWidth-8) // 8 for border padding

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
			UseFullNames:  true,
		},
	)

	// Clip rather than wrap: maxTitleWidth's ten-cell floor can make the row
	// wider than the box, and a wrapped row makes the box taller than it was
	// sized.
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
	links         []resolvedLink // combined outgoing + incoming links
	linkList      list.Model
	linksActive   bool // links box has focus, not the body
	cols          ui.ResponsiveColumns
	statusMessage string
	statusKind    statusKind
	helpExpanded  bool // set by App
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

	m.links = m.resolveAllLinks()

	hasTags := false
	for _, link := range m.links {
		if len(link.nib.Tags) > 0 {
			hasTags = true
			break
		}
	}

	// The label column (12), cursor (2) and border padding (8).
	linkAreaWidth := width - 12 - 2 - 8
	m.cols = ui.CalculateResponsiveColumns(linkAreaWidth, hasTags)

	m.linkList = m.createLinkList()

	headerHeight := m.calculateHeaderHeight()
	footerHeight := 2
	vpWidth := width - 4
	vpHeight := height - headerHeight - footerHeight

	m.viewport = viewport.New(viewport.WithWidth(vpWidth), viewport.WithHeight(vpHeight))
	m.viewport.SetContent(m.renderBody(vpWidth))

	// Open focused on the links list only if the frame holds its box. Update
	// re-checks this after every message, but only after routing it.
	m.linksActive = len(m.links) > 0 && m.linksSection() != ""

	return m
}

// linkRows is the links list's height: one row per link, up to
// max(3, height/3). The list draws no title; the filter input takes a row only
// while filtering (see linksBox).
func (m detailModel) linkRows() int {
	return min(len(m.links), max(3, m.height/3))
}

// linksBox renders the bordered links box, or "" when the nib has no links.
//
// The border color shows focus, so the border is never dropped on its own;
// linksSection drops the whole box when it does not fit.
//
// It sizes a copy of the list: contentFloor renders this box too, and the
// stored list must keep its size.
func (m detailModel) linksBox() string {
	if len(m.links) == 0 {
		return ""
	}

	l := m.linkList
	rows := m.linkRows()
	filtering := l.FilterState() == list.Filtering
	if filtering {
		// The filter input needs a row of its own.
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

// contentAvail is the rows the header and the footer region leave for the links
// box and the body.
func (m detailModel) contentAvail() int {
	return m.height - lipgloss.Height(m.renderHeader()) - lipgloss.Height(m.measuredFooterRegion())
}

// linksSection is the links box if contentAvail can hold it, else "". The box
// takes its rows before the body.
//
// Ask this, not linksBox, whether the box is on screen: View paints what it
// returns and Update holds focus and the filter to it.
func (m detailModel) linksSection() string {
	box := m.linksBox()
	if blockLines(box) > m.contentAvail() {
		return ""
	}
	return box
}

func (m detailModel) createLinkList() list.Model {
	delegate := linkDelegate{
		cfg:   m.config,
		width: m.width,
		cols:  m.cols,
	}

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
	// Hidden, not disabled: linksBox shows the input on the copy it paints while
	// filtering. bubbles reserves a row for a shown filter even when it is empty,
	// so a shown filter here would page the stored list one entry short of the
	// painted box.
	l.SetShowFilter(false)
	l.SetFilteringEnabled(true)

	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 1) // Left padding to align with header title
	applyFilterStyles(&l.Styles)
	l.Styles.NoItems = lipgloss.NewStyle()

	return l
}

func (m detailModel) Init() tea.Cmd {
	return nil
}

// Update routes msg, then drops a links filter whose box no longer fits and
// links focus with no links box on screen.
//
// The filter's own row can be what pushes the box out, so at such heights / is a
// no-op.
func (m detailModel) Update(msg tea.Msg) (detailModel, tea.Cmd) {
	m, cmd := m.route(msg)
	if m.linkList.FilterState() == list.Filtering && m.linksSection() == "" {
		m.linkList.ResetFilter()
		// cmd is the list's, for the input just discarded.
		cmd = nil
	}
	// After the reset: dropping the filter input frees a row, which can make the
	// box fit.
	if m.linksActive && m.linksSection() == "" {
		m.linksActive = false
	}
	return m, cmd
}

// route handles msg; Update wraps it.
func (m detailModel) route(msg tea.Msg) (detailModel, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		hasTags := false
		for _, link := range m.links {
			if len(link.nib.Tags) > 0 {
				hasTags = true
				break
			}
		}
		linkAreaWidth := msg.Width - 12 - 2 - 8
		m.cols = ui.CalculateResponsiveColumns(linkAreaWidth, hasTags)

		m.updateLinkListDelegate()

		m.linkList.SetSize(msg.Width-8, m.linkRows())

		headerHeight := m.calculateHeaderHeight()
		helpHt := m.currentHelpHeight()
		footerHeight := 2
		if helpHt > 0 {
			footerHeight = 1 + helpHt
		}
		vpWidth := msg.Width - 4
		vpHeight := msg.Height - headerHeight - footerHeight

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
		// A filtering links list takes every key that reaches this view.
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
			if len(m.links) > 0 {
				m.linksActive = !m.linksActive
			}
			return m, nil

		case "enter":
			if m.linksActive {
				if item, ok := m.linkList.SelectedItem().(linkItem); ok {
					targetNib := item.link.nib
					return m, func() tea.Msg {
						return selectNibMsg{nib: targetNib}
					}
				}
			}

		case "p":
			return m, func() tea.Msg {
				return openParentPickerMsg{
					nibIDs:        []string{m.nib.ID},
					nibTitle:      m.nib.Title,
					nibTypes:      []string{m.nib.EffectiveType()},
					currentParent: m.nib.Parent,
				}
			}

		case "s":
			return m, func() tea.Msg {
				return openStatusPickerMsg{
					nibIDs:        []string{m.nib.ID},
					nibTitle:      m.nib.Title,
					currentStatus: m.nib.Status,
				}
			}

		case "t":
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
			return m, func() tea.Msg {
				return openPriorityPickerMsg{
					nibIDs:          []string{m.nib.ID},
					nibTitle:        m.nib.Title,
					currentPriority: m.nib.EffectivePriority(),
				}
			}

		case "b":
			currentBlocking := computeCurrentBlocking(m.backend, m.nib.ID)
			return m, func() tea.Msg {
				return openBlockingPickerMsg{
					nibID:           m.nib.ID,
					nibTitle:        m.nib.Title,
					currentBlocking: currentBlocking,
				}
			}

		case "E":
			return m, func() tea.Msg {
				return openEstimatePickerMsg{
					nibIDs:          []string{m.nib.ID},
					nibTitle:        m.nib.Title,
					currentEstimate: m.nib.Estimate,
				}
			}

		case "e":
			return m, func() tea.Msg {
				return openEditorMsg{
					nibID:   m.nib.ID,
					nibPath: m.nib.Path,
				}
			}

		case "y":
			return m, func() tea.Msg {
				return copyNibIDMsg{ids: []string{m.nib.ID}}
			}
		}
	}

	if m.linksActive && len(m.links) > 0 {
		m.linkList, cmd = m.linkList.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

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

	header := m.renderHeader()

	avail := m.contentAvail()
	linksSection := m.linksSection()

	rows := []string{header}
	if linksSection != "" {
		rows = append(rows, linksSection)
	}
	avail -= blockLines(linksSection)

	// Render the body before the footer, though it is drawn above it: rendering
	// sizes the viewport, and the footer's scroll percentage must describe the
	// painted body, not the height Update estimated. The footer's height does not
	// depend on this (see measuredFooterRegion).
	body := m.renderBodyBox(avail)
	footer := m.footerRegion(paintedFrame{links: linksSection != "", body: body != ""})
	if body != "" {
		rows = append(rows, body)
	}
	return strings.Join(append(rows, footer), "\n")
}

// renderBodyBox draws the body into avail rows, or nothing at all when there
// are fewer of them than the box occupies. The body is the first thing dropped;
// the header, the status message and the keys keep their rows.
//
// The pointer receiver keeps the viewport height set here for View's footer,
// whose scroll percentage reads it. View's own receiver is a copy.
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
// The expanded panel lists esc, ? and q itself, so it replaces the help row but
// not the status message.
func (m detailModel) footerRegion(painted paintedFrame) string {
	if !m.helpExpanded {
		return m.renderFooter(painted)
	}
	return expandedFooterRegion(detailExpandedEntries(), m.statusMessage, m.statusKind, m.width, m.height, m.contentFloor())
}

// paintedFrame is which of the two droppable boxes View drew above the footer.
type paintedFrame struct {
	links bool
	body  bool
}

// measuredFooterRegion renders the footer region for contentAvail to measure.
//
// It passes a zero paintedFrame, since contentAvail is what decides the painted
// frame. Keep the region's height independent of paintedFrame: everything it
// toggles sits in the help row, which is one row because renderFooter clips it.
func (m detailModel) measuredFooterRegion() string {
	return m.footerRegion(paintedFrame{})
}

// minBodyHeight is the fewest rows the body box occupies: its border, around a
// viewport of one row.
const minBodyHeight = 3

// contentFloor is the rows the help panel's budget holds back: the rendered
// header, the links box when there is one, and the body's minimum.
//
// Both the links box and the body's minimum are counted even where View drops
// them, which errs toward a smaller panel.
func (m detailModel) contentFloor() int {
	return lipgloss.Height(m.renderHeader()) + minBodyHeight + blockLines(m.linksBox())
}

// renderFooter returns the abbreviated footer for the detail view.
//
// The scroll percentage appears only for a painted body and tab only for a
// painted links box; neither leaves a placeholder.
func (m detailModel) renderFooter(painted paintedFrame) string {
	var footer string
	if painted.body {
		scrollPct := int(m.viewport.ScrollPercent() * 100)
		footer += helpStyle.Render(fmt.Sprintf("%d%%", scrollPct)) + "  "
	}
	if painted.links {
		footer += renderHelpKey("tab", "switch") + "  "
	}
	footer += renderHelpKey("e", "edit") + "  " +
		renderHelpKey("s", "status") + "  " +
		renderHelpKey("esc", "back") + "  " +
		renderHelpKey("?", "more") + "  " +
		renderHelpKey("q", "quit")

	if m.statusMessage != "" {
		// On its own lines above the help row, so the keys stay on screen.
		footer = renderStatusMessage(m.statusMessage, m.statusKind, m.width, maxStatusFooterLines(m.height)) + "\n" + footer
	}
	// The help row does not wrap, so clip it; the status message is already
	// wrapped to width.
	return clipToWidth(footer, m.width)
}

// currentHelpHeight returns the help panel height (0 when collapsed).
func (m detailModel) currentHelpHeight() int {
	if !m.helpExpanded {
		return 0
	}
	return helpPanelHeight(detailExpandedEntries(), m.width, helpRowBudget(m.height, m.contentFloor(), 0))
}

// calculateHeaderHeight estimates the header and links box height to size the
// viewport before View resizes it to the rendered frame.
func (m detailModel) calculateHeaderHeight() int {
	baseHeight := 6

	if len(m.nib.Documents) > 0 {
		baseHeight++
	}

	if len(m.links) > 0 {
		baseHeight += m.linkRows() + 3
	}

	return baseHeight
}

func (m detailModel) renderHeader() string {
	title := detailTitleStyle.Render(m.nib.Title)

	id := ui.ID.Render(m.nib.ID)

	statusCfg := m.config.GetStatus(m.nib.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isClosed := m.config.IsClosedStatus(m.nib.Status)
	status := ui.RenderStatusWithColor(m.nib.Status, statusColor, isClosed)

	var estimate string
	if m.nib.Estimate != "" {
		estimateCfg := m.config.GetEstimate(m.nib.Estimate)
		estimateColor := "gray"
		if estimateCfg != nil {
			estimateColor = estimateCfg.Color
		}
		estimate = ui.RenderEstimateWithColor(m.nib.Estimate, estimateColor)
	}

	var headerContent strings.Builder
	headerContent.WriteString(title)
	headerContent.WriteString("\n")
	headerContent.WriteString(id + "  " + status)
	if estimate != "" {
		headerContent.WriteString(" " + estimate)
	}

	if len(m.nib.Tags) > 0 {
		headerContent.WriteString("  ")
		headerContent.WriteString(ui.RenderTags(m.nib.Tags))
	}

	if len(m.nib.Documents) > 0 {
		headerContent.WriteString("\n")
		headerContent.WriteString(ui.RenderDocuments(m.nib.Documents))
	}

	// Always muted: the header never takes focus.
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

	// Releasing statuses, not closed ones: a deferred blocker still blocks. The
	// blockedBy and blocking resolvers apply the same rule themselves.
	activeOnly := &model.NibFilter{
		ExcludeStatus: m.config.ReleasingStatusNames(),
	}

	if blocking, _ := m.backend.GetBlocking(ctx, m.nib, activeOnly); blocking != nil {
		for _, b := range blocking {
			links = append(links, resolvedLink{linkType: "blocking", nib: b, incoming: false})
		}
	}
	if parent, _ := m.backend.GetParent(ctx, m.nib); parent != nil {
		links = append(links, resolvedLink{linkType: "parent", nib: parent, incoming: false})
	}

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

	// Group by label, then order each group as nib.SortByStatusPriorityAndType does.
	statusNames := m.config.StatusNames()
	typeNames := m.config.TypeNames()
	sort.Slice(links, func(i, j int) bool {
		labelI := m.formatLinkLabel(links[i].linkType, links[i].incoming)
		labelJ := m.formatLinkLabel(links[j].linkType, links[j].incoming)
		if labelI != labelJ {
			return labelI < labelJ
		}
		return compareNibsByStatusPriorityAndType(links[i].nib, links[j].nib, statusNames, typeNames, m.config)
	})

	return links
}

// compareNibsByStatusPriorityAndType reports whether a sorts before b in the
// order nib.SortByStatusPriorityAndType uses.
func compareNibsByStatusPriorityAndType(a, b *nib.Nib, statusNames, typeNames []string, ranker nib.PriorityRanker) bool {
	statusOrder := make(map[string]int)
	for i, s := range statusNames {
		statusOrder[s] = i
	}
	typeOrder := make(map[string]int)
	for i, t := range typeNames {
		typeOrder[t] = i
	}

	// Unrecognized values sort last.
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

	oi, oj := getStatusOrder(a.Status), getStatusOrder(b.Status)
	if oi != oj {
		return oi < oj
	}
	pi, pj := ranker.PriorityRank(a.Priority), ranker.PriorityRank(b.Priority)
	if pi != pj {
		return pi < pj
	}
	ti, tj := getTypeOrder(a.EffectiveType()), getTypeOrder(b.EffectiveType())
	if ti != tj {
		return ti < tj
	}
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}

func (m detailModel) renderBody(_ int) string {
	// TrimSpace, not == "": Parse keeps a body of only blank lines as-is, and
	// glamour renders it to nothing.
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

	// Trim newlines only: glamour writes the first line's left margin as plain
	// spaces, which TrimSpace would remove.
	return strings.Trim(rendered, "\n")
}
