package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
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
	nib     *nib.Nib
	incoming bool // true if another nib links TO this one
}

// linkItem wraps a resolvedLink to implement list.Item
type linkItem struct {
	link   resolvedLink
	cfg    *config.Config
	width  int
	cols   ui.ResponsiveColumns
	label  string // pre-computed label like "Blocks:" or "Blocked by:"
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

	// Get colors from config
	colors := d.cfg.GetNibColors(link.nib.Status, link.nib.Type, link.nib.Priority)

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
		link.nib.Type,
		link.nib.Title,
		ui.NibRowConfig{
			StatusColor:   colors.StatusColor,
			TypeColor:     colors.TypeColor,
			PriorityColor: colors.PriorityColor,
			Priority:      link.nib.Priority,
			IsArchive:     colors.IsArchive,
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

	_, _ = fmt.Fprint(w, cursor+labelCol+row)
}

// detailModel displays a single nib's details
type detailModel struct {
	viewport      viewport.Model
	nib          *nib.Nib
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
	helpExpanded  bool                 // Help panel state — set by App when ? is toggled
}

func newDetailModel(b *nib.Nib, backend Backend, cfg *config.Config, width, height int) detailModel {
	m := detailModel{
		nib:        b,
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

	m.viewport = viewport.New(vpWidth, vpHeight)
	m.viewport.SetContent(m.renderBody(vpWidth))

	return m
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

	// Calculate list height: show all links up to 1/3 of screen height
	// Add 2 for the title row and padding
	maxHeight := max(3, m.height/3)
	listHeight := min(len(m.links), maxHeight) + 2

	l := list.New(items, delegate, m.width-8, listHeight)
	l.Title = "Linked Nibs"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)

	// Style the title bar similar to the detail header title (badge style) but with different color
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#fff")).
		Background(ui.ColorBlue).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 1) // Left padding to align with header title
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
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

		// Update link list size: show all links up to 1/3 of screen height
		// Add 2 for the title row and padding
		maxHeight := max(3, msg.Height/3)
		listHeight := min(len(m.links), maxHeight) + 2
		m.linkList.SetSize(msg.Width-8, listHeight)

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
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.SetContent(m.renderBody(vpWidth))
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
			m.viewport.SetContent(m.renderBody(vpWidth))
		}

	case tea.KeyMsg:
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
					nibIDs:       []string{m.nib.ID},
					nibTitle:     m.nib.Title,
					nibTypes:     []string{m.nib.Type},
					currentParent: m.nib.Parent,
				}
			}

		case "s":
			// Open status picker
			return m, func() tea.Msg {
				return openStatusPickerMsg{
					nibIDs:       []string{m.nib.ID},
					nibTitle:     m.nib.Title,
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
					currentType: m.nib.Type,
					validTypes:  validTypes,
				}
			}

		case "P":
			// Open priority picker
			return m, func() tea.Msg {
				return openPriorityPickerMsg{
					nibIDs:         []string{m.nib.ID},
					nibTitle:       m.nib.Title,
					currentPriority: m.nib.Priority,
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

	// Links section (if any)
	var linksSection string
	if len(m.links) > 0 {
		linksBorderColor := ui.ColorMuted
		if m.linksActive {
			linksBorderColor = ui.ColorPrimary
		}
		linksBorder := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(linksBorderColor).
			Width(m.width - 4)
		linksSection = linksBorder.Render(m.linkList.View()) + "\n"
	}

	// Body
	bodyBorderColor := ui.ColorMuted
	if !m.linksActive {
		bodyBorderColor = ui.ColorPrimary
	}
	bodyBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(bodyBorderColor).
		Width(m.width - 4)
	body := bodyBorder.Render(m.viewport.View())

	// When expanded, the panel includes esc/q/? and replaces the footer entirely
	if m.helpExpanded {
		panel := renderHelpPanel(detailExpandedEntries(), m.width)
		return header + "\n" + linksSection + body + "\n" + panel
	}
	return header + "\n" + linksSection + body + "\n" + m.renderFooter()
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
		statusStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess).Bold(true)
		footer = statusStyle.Render(m.statusMessage) + "  " + footer
	}
	return footer
}

// currentHelpHeight returns the help panel height (0 when collapsed).
func (m detailModel) currentHelpHeight() int {
	if !m.helpExpanded {
		return 0
	}
	return helpPanelHeight(detailExpandedEntries(), m.width)
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
		// Links list height + borders (matches createLinkList calculation)
		// +2 for title row and padding, +3 for borders and spacing
		maxHeight := max(3, m.height/3)
		listHeight := min(len(m.links), maxHeight) + 2
		baseHeight += listHeight + 3
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
	isArchive := m.config.IsArchiveStatus(m.nib.Status)
	status := ui.RenderStatusWithColor(m.nib.Status, statusColor, isArchive)

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
		Width(m.width - 4)

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

	// Filter out resolved nibs (completed/scrapped) from blocking relationships
	activeOnly := &model.NibFilter{
		ExcludeStatus: []string{"completed", "scrapped"},
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
	// Tertiary: type order
	ti, tj := getTypeOrder(a.Type), getTypeOrder(b.Type)
	if ti != tj {
		return ti < tj
	}
	// Quaternary: title (case-insensitive)
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}


func (m detailModel) renderBody(_ int) string {
	if m.nib.Body == "" {
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

	return strings.TrimSpace(rendered)
}
