package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// nibItem wraps a Nib to implement list.Item, with tree context
type nibItem struct {
	nib        *nib.Nib
	cfg         *config.Config
	treePrefix  string // tree prefix for rendering (e.g., "├─" or "  └─")
	matched     bool   // true if nib matched filter (vs. ancestor shown for context)
	hasChildren bool   // true if this node has children in the tree
	collapsed   bool   // true if this node's children are hidden
	isBlocked   bool   // true if this nib has active blockers
	isBlocking  bool   // true if this nib is actively blocking others
}

func (i nibItem) Title() string       { return i.nib.Title }
func (i nibItem) Description() string { return i.nib.ID + " · " + i.nib.Status }
func (i nibItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// itemDelegate handles rendering of list items
type itemDelegate struct {
	cfg           *config.Config
	hasTags       bool
	width         int
	cols          ui.ResponsiveColumns // cached responsive columns
	idColWidth    int                  // ID column width (accounts for tree prefix)
	selectedNibs *map[string]bool     // pointer to marked nibs for multi-select
}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(nibItem)
	if !ok {
		return
	}

	// Get colors from config. EffectiveType so a type-less nib keeps its "task"
	// badge/color. Raw Priority is safe here despite the missing default: GetNibColors
	// -> GetPriority yields a DIFFERENT PriorityColor for "" (none) vs "normal"
	// ("white"), but that color is only ever consumed by RenderPrioritySymbol, which
	// returns "" (discarding the color) whenever GetPrioritySymbol is empty — and the
	// symbol is empty for both "" and "normal", so the rendered result is identical.
	colors := d.cfg.GetNibColors(item.nib.Status, item.nib.EffectiveType(), item.nib.Priority)

	// Calculate max title width using responsive columns
	idWidth := d.cols.ID
	if d.idColWidth > 0 {
		idWidth = d.idColWidth
	}
	baseWidth := idWidth + d.cols.Status + d.cols.Type + 4 // 4 for cursor + padding
	if d.cols.ShowTags {
		baseWidth += d.cols.Tags
	}
	maxTitleWidth := max(0, m.Width()-baseWidth)

	// Check if nib is marked for multi-select
	var isMarked bool
	if d.selectedNibs != nil {
		isMarked = (*d.selectedNibs)[item.nib.ID]
	}

	str := ui.RenderNibRow(
		item.nib.ID,
		item.nib.Status,
		item.nib.EffectiveType(),
		item.nib.Title,
		ui.NibRowConfig{
			StatusColor:   colors.StatusColor,
			TypeColor:     colors.TypeColor,
			PriorityColor: colors.PriorityColor,
			Priority:      item.nib.Priority,
			IsArchive:     colors.IsArchive,
			MaxTitleWidth: maxTitleWidth,
			ShowCursor:    true,
			IsSelected:    index == m.Index(),
			IsMarked:      isMarked,
			Tags:          item.nib.Tags,
			ShowTags:      d.cols.ShowTags,
			TagsColWidth:  d.cols.Tags,
			MaxTags:       d.cols.MaxTags,
			TreePrefix:    item.treePrefix,
			Dimmed:        !item.matched,
			IDColWidth:    d.idColWidth,
			UseFullNames:  d.cols.UseFullTypeStatus,
			IsBlocked:     item.isBlocked,
			IsBlocking:    item.isBlocking,
		},
	)

	_, _ = fmt.Fprint(w, str)
}

// listModel is the model for the nib list view
type listModel struct {
	list     list.Model
	backend  Backend
	config   *config.Config
	width    int
	height   int
	err      error

	// Responsive column state
	hasTags    bool                 // whether any nibs have tags
	cols       ui.ResponsiveColumns // calculated responsive columns
	idColWidth int                  // ID column width (accounts for tree depth)

	// Active filters
	tagFilter     string // if set, only show nibs with this tag
	hideCompleted bool   // if true, hide completed and scrapped nibs

	// Collapse state
	collapsedIDs map[string]bool // set of collapsed node IDs
	tree         []*ui.TreeNode  // cached tree from last load (for re-flattening)

	// Wide mode - show full type/status names, hide preview pane
	wideMode bool

	// Multi-select state
	selectedNibs map[string]bool // IDs of nibs marked for multi-edit

	// Border title (rendered into top border line)
	borderTitle string

	// Status message to display in footer
	statusMessage string

	// After reorder, select this nib ID when the list reloads
	selectByID string

	// Help panel state — set by App when ? is toggled
	helpExpanded bool
}

func newListModel(backend Backend, cfg *config.Config) listModel {
	selectedNibs := make(map[string]bool)
	delegate := itemDelegate{cfg: cfg, selectedNibs: &selectedNibs}

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ui.ColorPrimary)

	m := listModel{
		list:          l,
		backend:       backend,
		config:        cfg,
		hideCompleted: cfg.HideCompleted(),
		wideMode:      cfg.WideMode(),
		selectedNibs: selectedNibs,
		collapsedIDs:  make(map[string]bool),
	}
	m.updateTitle()
	return m
}

// nibsLoadedMsg is sent when nibs are loaded
type nibsLoadedMsg struct {
	items      []ui.FlatItem  // flattened tree items
	idColWidth int            // calculated ID column width for tree
	tree       []*ui.TreeNode // cached tree for re-flattening (collapse/expand)
}

// errMsg is sent when an error occurs
type errMsg struct {
	err error
}

// selectNibMsg is sent when a nib is selected
type selectNibMsg struct {
	nib *nib.Nib
}

func (m listModel) Init() tea.Cmd {
	return m.loadNibs
}

func (m listModel) loadNibs() tea.Msg {
	// Build filter based on active filters
	var filter *model.NibFilter
	if m.tagFilter != "" || m.hideCompleted {
		filter = &model.NibFilter{}
		if m.tagFilter != "" {
			filter.Tags = []string{m.tagFilter}
		}
		if m.hideCompleted {
			// The hide-completed toggle is the *archive* set — derive it from
			// the canonical archive predicate so it tracks config, not a literal.
			filter.ExcludeStatus = m.config.ArchiveStatusNames()
		}
	}

	// Query filtered nibs
	filteredNibs, err := m.backend.ListNibs(context.Background(), filter)
	if err != nil {
		return errMsg{err}
	}

	// Query all nibs for tree context (ancestors)
	allNibs, err := m.backend.ListNibs(context.Background(), nil)
	if err != nil {
		return errMsg{err}
	}

	// Sort function for tree building — use Order so manual reordering is visible
	sortFn := func(nibs []*nib.Nib) {
		nib.SortByOrder(nibs)
	}

	// Build tree and flatten it (respecting collapsed state)
	tree := ui.BuildTree(filteredNibs, allNibs, sortFn)
	items := ui.FlattenTreeFiltered(tree, m.collapsedIDs)

	// Calculate ID column width based on max ID length and tree depth
	maxIDLen := 0
	for _, b := range allNibs {
		if len(b.ID) > maxIDLen {
			maxIDLen = len(b.ID)
		}
	}
	maxDepth := ui.MaxTreeDepth(items)
	// ID column = base ID width + tree indent (3 chars per depth level)
	idColWidth := maxIDLen + 2 // base padding
	if maxDepth > 0 {
		idColWidth += maxDepth * 3 // 3 chars per depth level (├─ + space)
	}
	// Add space for collapse indicator if any node has children
	if ui.HasAnyChildren(tree) {
		idColWidth += 2
	}

	return nibsLoadedMsg{items: items, idColWidth: idColWidth, tree: tree}
}

// setTagFilter sets the tag filter
func (m *listModel) setTagFilter(tag string) {
	m.tagFilter = tag
}

// toggleHideCompleted toggles the hideCompleted filter
func (m *listModel) toggleHideCompleted() {
	m.hideCompleted = !m.hideCompleted
}

// clearFilter clears all active filters
func (m *listModel) clearFilter() {
	m.tagFilter = ""
}

// hasActiveFilter returns true if any filter is active
func (m *listModel) hasActiveFilter() bool {
	return m.tagFilter != ""
}

func (m listModel) Update(msg tea.Msg) (listModel, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// Track cursor position before update
	prevIndex := m.list.Index()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for border, "\n", and footer/help panel
		helpHt := m.currentHelpHeight()
		m.list.SetSize(msg.Width-2, msg.Height-3-max(1, helpHt))
		// Recalculate responsive columns
		m.cols = ui.CalculateResponsiveColumns(msg.Width, m.hasTags)
		m.applyWideMode()
		m.updateDelegate()

	case nibsLoadedMsg:
		// Cache tree for re-flattening (collapse/expand)
		m.tree = msg.tree

		// Prune stale collapsed IDs (nodes that no longer exist in the tree)
		if len(m.collapsedIDs) > 0 {
			validIDs := make(map[string]bool)
			ui.CollectParentIDs(m.tree, validIDs)
			for id := range m.collapsedIDs {
				if !validIDs[id] {
					delete(m.collapsedIDs, id)
				}
			}
		}

		items := make([]list.Item, len(msg.items))
		// Check if any nibs have tags
		m.hasTags = false
		for i, flatItem := range msg.items {
			items[i] = nibItem{
				nib:        flatItem.Nib,
				cfg:         m.config,
				treePrefix:  flatItem.TreePrefix,
				matched:     flatItem.Matched,
				hasChildren: flatItem.HasChildren,
				collapsed:   flatItem.Collapsed,
				isBlocked:   m.backend.IsBlocked(flatItem.Nib.ID),
				isBlocking:  m.backend.IsBlocking(flatItem.Nib.ID),
			}
			if len(flatItem.Nib.Tags) > 0 {
				m.hasTags = true
			}
		}
		m.list.SetItems(items)
		m.idColWidth = msg.idColWidth

		// Re-select nib after reorder if requested
		if m.selectByID != "" {
			for i, item := range items {
				if bi, ok := item.(nibItem); ok && bi.nib.ID == m.selectByID {
					m.list.Select(i)
					break
				}
			}
			m.selectByID = ""
		}

		// Calculate responsive columns based on hasTags and width
		m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
		m.applyWideMode()
		m.updateDelegate()
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case " ":
				// Toggle selection for multi-select, then move to next item
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					if m.selectedNibs[item.nib.ID] {
						delete(m.selectedNibs, item.nib.ID)
					} else {
						m.selectedNibs[item.nib.ID] = true
					}
					m.list.CursorDown()
				}
				return m, nil
			case "enter":
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return selectNibMsg{nib: item.nib}
					}
				}
			case "p":
				// Open parent picker for selected nib(s)
				if len(m.selectedNibs) > 0 {
					// Multi-select mode
					ids := make([]string, 0, len(m.selectedNibs))
					types := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
						// Find the nib to get its type
						for _, item := range m.list.Items() {
							if bi, ok := item.(nibItem); ok && bi.nib.ID == id {
								types = append(types, bi.nib.EffectiveType())
								break
							}
						}
					}
					return m, func() tea.Msg {
						return openParentPickerMsg{
							nibIDs:   ids,
							nibTitle: fmt.Sprintf("%d selected nibs", len(ids)),
							nibTypes: types,
						}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return openParentPickerMsg{
							nibIDs:       []string{item.nib.ID},
							nibTitle:     item.nib.Title,
							nibTypes:     []string{item.nib.EffectiveType()},
							currentParent: item.nib.Parent,
						}
					}
				}
			case "s":
				// Open status picker for selected nib(s)
				if len(m.selectedNibs) > 0 {
					// Multi-select mode
					ids := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
					}
					return m, func() tea.Msg {
						return openStatusPickerMsg{
							nibIDs:   ids,
							nibTitle: fmt.Sprintf("%d selected nibs", len(ids)),
						}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return openStatusPickerMsg{
							nibIDs:       []string{item.nib.ID},
							nibTitle:     item.nib.Title,
							currentStatus: item.nib.Status,
						}
					}
				}
			case "t":
				// Open type picker for selected nib(s)
				if len(m.selectedNibs) > 0 {
					// Multi-select mode: intersect valid types across all nibs
					ids := make([]string, 0, len(m.selectedNibs))
					var validTypes []string
					first := true
					for id := range m.selectedNibs {
						ids = append(ids, id)
						if n, err := m.backend.GetNib(context.Background(), id); err == nil && n != nil {
							nibValid := validTypesForNib(n, m.backend)
							if first {
								validTypes = nibValid
								first = false
							} else {
								validTypes = intersectStrings(validTypes, nibValid)
							}
						}
					}
					return m, func() tea.Msg {
						return openTypePickerMsg{
							nibIDs:     ids,
							nibTitle:   fmt.Sprintf("%d selected nibs", len(ids)),
							validTypes: validTypes,
						}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					validTypes := validTypesForNib(item.nib, m.backend)
					return m, func() tea.Msg {
						return openTypePickerMsg{
							nibIDs:      []string{item.nib.ID},
							nibTitle:    item.nib.Title,
							currentType: item.nib.EffectiveType(),
							validTypes:  validTypes,
						}
					}
				}
			case "P":
				// Open priority picker for selected nib(s)
				if len(m.selectedNibs) > 0 {
					// Multi-select mode
					ids := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
					}
					return m, func() tea.Msg {
						return openPriorityPickerMsg{
							nibIDs:   ids,
							nibTitle: fmt.Sprintf("%d selected nibs", len(ids)),
						}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return openPriorityPickerMsg{
							nibIDs:         []string{item.nib.ID},
							nibTitle:       item.nib.Title,
							currentPriority: item.nib.EffectivePriority(),
						}
					}
				}
			case "E":
				// Open estimate picker for selected nib(s)
				if len(m.selectedNibs) > 0 {
					ids := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
					}
					return m, func() tea.Msg {
						return openEstimatePickerMsg{
							nibIDs:   ids,
							nibTitle: fmt.Sprintf("%d selected nibs", len(ids)),
						}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return openEstimatePickerMsg{
							nibIDs:          []string{item.nib.ID},
							nibTitle:        item.nib.Title,
							currentEstimate: item.nib.Estimate,
						}
					}
				}
			case "b":
				// Open blocking picker for selected nib — compute from blockedBy scan
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					currentBlocking := computeCurrentBlocking(m.backend, item.nib.ID)
					return m, func() tea.Msg {
						return openBlockingPickerMsg{
							nibID:           item.nib.ID,
							nibTitle:        item.nib.Title,
							currentBlocking: currentBlocking,
						}
					}
				}
			case "c":
				// Open type picker for create flow with smart default
				selectedType := ""
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					selectedType = item.nib.EffectiveType()
				}
				smartDefault := defaultTypeForContext(selectedType)
				return m, func() tea.Msg {
					return openCreateTypePickerMsg{defaultType: smartDefault}
				}
			case "e":
				// Open editor for selected nib
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return openEditorMsg{
							nibID:   item.nib.ID,
							nibPath: item.nib.Path,
						}
					}
				}
			case "H":
				// Toggle hide completed/scrapped nibs
				m.toggleHideCompleted()
				return m, m.loadNibs
			case "W":
				// Toggle wide mode (full type/status names, no preview pane)
				m.wideMode = !m.wideMode
				m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
				m.applyWideMode()
				m.updateDelegate()
				return m, nil
			case "tab":
				// Toggle collapse/expand on current node
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren {
					if m.collapsedIDs[item.nib.ID] {
						delete(m.collapsedIDs, item.nib.ID)
					} else {
						m.collapsedIDs[item.nib.ID] = true
					}
					m.reflattenTree()
				}
				return m, nil
			case "left":
				// Collapse expanded node, or navigate to parent
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					if item.hasChildren && !item.collapsed {
						// Collapse expanded node
						m.collapsedIDs[item.nib.ID] = true
						m.reflattenTree()
					} else {
						// Navigate to parent
						parentMap := make(map[string]string)
						buildParentMap(m.tree, parentMap)
						if parentID, ok := parentMap[item.nib.ID]; ok && parentID != "" {
							for i, li := range m.list.Items() {
								if bi, ok := li.(nibItem); ok && bi.nib.ID == parentID {
									m.list.Select(i)
									break
								}
							}
						}
					}
				}
				return m, nil
			case "right":
				// Expand current node (or no-op if already expanded/leaf)
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren && item.collapsed {
					delete(m.collapsedIDs, item.nib.ID)
					m.reflattenTree()
				}
				return m, nil
			case "ctrl+left":
				// Collapse all children of current node (not the node itself)
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren {
					if node := ui.FindNode(m.tree, item.nib.ID); node != nil {
						ui.CollectParentIDs(node.Children, m.collapsedIDs)
						m.reflattenTree()
					}
				}
				return m, nil
			case "ctrl+right":
				// Expand all children of current node
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren {
					if node := ui.FindNode(m.tree, item.nib.ID); node != nil {
						// Remove collapsed state for all descendant parents
						var removeCollapsed func([]*ui.TreeNode)
						removeCollapsed = func(nodes []*ui.TreeNode) {
							for _, child := range nodes {
								delete(m.collapsedIDs, child.Nib.ID)
								removeCollapsed(child.Children)
							}
						}
						removeCollapsed(node.Children)
						m.reflattenTree()
					}
				}
				return m, nil
			case "ctrl+up":
				return m, m.dispatchBlockMove(true)
			case "ctrl+down":
				return m, m.dispatchBlockMove(false)
			case "shift+tab":
				// Collapse all parent nodes
				if m.tree != nil {
					ui.CollectParentIDs(m.tree, m.collapsedIDs)
					m.reflattenTree()
				}
				return m, nil
			case "]":
				// Expand all
				clear(m.collapsedIDs)
				m.reflattenTree()
				return m, nil
			case "y":
				// Copy nib ID(s) to clipboard
				if len(m.selectedNibs) > 0 {
					// Multi-select mode: copy all selected IDs
					ids := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
					}
					return m, func() tea.Msg {
						return copyNibIDMsg{ids: ids}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					// Single nib mode
					return m, func() tea.Msg {
						return copyNibIDMsg{ids: []string{item.nib.ID}}
					}
				}
			case "A":
				// Archive selected nib (with confirmation)
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					ids := gatherNibAndDescendants(m.backend, item.nib.ID)
					descendantCount := len(ids) - 1
					dialog := buildConfirmDialog("archive", item.nib.Title, ids, descendantCount)
					return m, func() tea.Msg {
						return openConfirmMsg{dialog: dialog}
					}
				}
			case "delete":
				// Permanently delete selected nib (with confirmation)
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					ids := gatherNibAndDescendants(m.backend, item.nib.ID)
					descendantCount := len(ids) - 1
					dialog := buildConfirmDialog("delete", item.nib.Title, ids, descendantCount)
					return m, func() tea.Msg {
						return openConfirmMsg{dialog: dialog}
					}
				}
			case "esc", "backspace":
				// First clear selection if any nibs are selected
				if len(m.selectedNibs) > 0 {
					clear(m.selectedNibs)
					return m, nil
				}
				// Then clear active filter if any
				if m.hasActiveFilter() {
					return m, func() tea.Msg {
						return clearFilterMsg{}
					}
				}
			}

			// PgUp: if not on first line of page, snap to first line;
			// otherwise fall through to default page-change behavior.
			// Use msg.Type directly because bubbles disables PrevPage/NextPage
			// bindings on single-page lists, making key.Matches() return false.
			if msg.Type == tea.KeyPgUp && m.list.Cursor() > 0 {
				firstOnPage := m.list.Paginator.Page * m.list.Paginator.PerPage
				m.list.Select(firstOnPage)
				if m.list.Index() != prevIndex {
					if item, ok := m.list.SelectedItem().(nibItem); ok {
						cmds = append(cmds, func() tea.Msg {
							return cursorChangedMsg{nibID: item.nib.ID}
						})
					}
				}
				return m, tea.Batch(cmds...)
			}

			// PgDn: if not on last line of page, snap to last line;
			// otherwise fall through to default page-change behavior.
			if msg.Type == tea.KeyPgDown {
				itemsOnPage := m.list.Paginator.ItemsOnPage(len(m.list.VisibleItems()))
				if m.list.Cursor() < itemsOnPage-1 {
					lastOnPage := m.list.Paginator.Page*m.list.Paginator.PerPage + itemsOnPage - 1
					m.list.Select(lastOnPage)
					if m.list.Index() != prevIndex {
						if item, ok := m.list.SelectedItem().(nibItem); ok {
							cmds = append(cmds, func() tea.Msg {
								return cursorChangedMsg{nibID: item.nib.ID}
							})
						}
					}
					return m, tea.Batch(cmds...)
				}
			}
		}
	}

	// Always forward to the list component
	m.list, cmd = m.list.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Check if cursor moved and emit message
	if m.list.Index() != prevIndex {
		if item, ok := m.list.SelectedItem().(nibItem); ok {
			cmds = append(cmds, func() tea.Msg {
				return cursorChangedMsg{nibID: item.nib.ID}
			})
		}
	}

	return m, tea.Batch(cmds...)
}

// updateDelegate updates the list delegate with current responsive columns
func (m *listModel) updateDelegate() {
	delegate := itemDelegate{
		cfg:           m.config,
		hasTags:       m.hasTags,
		width:         m.width,
		cols:          m.cols,
		idColWidth:    m.idColWidth,
		selectedNibs: &m.selectedNibs,
	}
	m.list.SetDelegate(delegate)
}

// applyWideMode forces full type/status column widths when wide mode is active
func (m *listModel) applyWideMode() {
	if m.wideMode {
		m.cols.UseFullTypeStatus = true
		m.cols.Status = 12
		m.cols.Type = 12
	}
}

// reflattenTree re-flattens the cached tree with current collapsedIDs.
// Preserves cursor position on the same nib, or walks up parent chain if hidden.
func (m *listModel) reflattenTree() {
	if m.tree == nil {
		return
	}

	// Remember current nib ID for cursor restoration
	var currentNibID string
	if item, ok := m.list.SelectedItem().(nibItem); ok {
		currentNibID = item.nib.ID
	}

	// Re-flatten with current collapse state
	flatItems := ui.FlattenTreeFiltered(m.tree, m.collapsedIDs)

	// Convert to list items
	items := make([]list.Item, len(flatItems))
	m.hasTags = false
	for i, flatItem := range flatItems {
		items[i] = nibItem{
			nib:        flatItem.Nib,
			cfg:         m.config,
			treePrefix:  flatItem.TreePrefix,
			matched:     flatItem.Matched,
			hasChildren: flatItem.HasChildren,
			collapsed:   flatItem.Collapsed,
			isBlocked:   m.backend.IsBlocked(flatItem.Nib.ID),
			isBlocking:  m.backend.IsBlocking(flatItem.Nib.ID),
		}
		if len(flatItem.Nib.Tags) > 0 {
			m.hasTags = true
		}
	}
	m.list.SetItems(items)

	// Restore cursor position
	if currentNibID != "" {
		m.restoreCursor(currentNibID, items)
	}

	// Recalculate idColWidth
	maxIDLen := 0
	for _, fi := range flatItems {
		if len(fi.Nib.ID) > maxIDLen {
			maxIDLen = len(fi.Nib.ID)
		}
	}
	maxDepth := ui.MaxTreeDepth(flatItems)
	m.idColWidth = maxIDLen + 2
	if maxDepth > 0 {
		m.idColWidth += maxDepth * 3
	}
	if ui.HasAnyChildren(m.tree) {
		m.idColWidth += 2
	}

	// Recalculate responsive columns and update delegate
	m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
	m.applyWideMode()
	m.updateDelegate()
}

// restoreCursor finds the nib by ID in the items list, or walks up parents to find a visible ancestor.
func (m *listModel) restoreCursor(nibID string, items []list.Item) {
	// Try to find the exact nib
	for i, item := range items {
		if bi, ok := item.(nibItem); ok && bi.nib.ID == nibID {
			m.list.Select(i)
			return
		}
	}

	// Nib is hidden (collapsed away) — walk up parent chain to find nearest visible ancestor
	// Build a map of nib ID -> parent ID from the tree
	parentMap := make(map[string]string)
	buildParentMap(m.tree, parentMap)

	// Walk up the parent chain
	currentID := nibID
	for {
		parentID, ok := parentMap[currentID]
		if !ok || parentID == "" {
			break
		}
		for i, item := range items {
			if bi, ok := item.(nibItem); ok && bi.nib.ID == parentID {
				m.list.Select(i)
				return
			}
		}
		currentID = parentID
	}
}

// buildParentMap recursively builds a map of child ID -> parent ID from the tree.
func buildParentMap(nodes []*ui.TreeNode, m map[string]string) {
	for _, node := range nodes {
		for _, child := range node.Children {
			m[child.Nib.ID] = node.Nib.ID
			buildParentMap(node.Children, m)
		}
	}
}

func (m listModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}

	if m.width == 0 {
		return "Loading..."
	}

	// Inner height: total - border(2) - "\n"(1) - footer/panel height
	helpHt := m.currentHelpHeight()
	footerH := max(1, helpHt) // 1 for compact footer, helpHt for expanded panel
	innerHeight := m.height - 3 - footerH
	content := m.viewContent(innerHeight)

	if m.helpExpanded {
		panel := renderHelpPanel(m.expandedHelpEntries(), m.width)
		return content + "\n" + panel
	}
	return content + "\n" + m.Footer()
}

// viewContent renders just the bordered list without footer.
// innerHeight is the content height inside the border (not including border lines).
func (m listModel) viewContent(innerHeight int) string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorMuted).
		Width(m.width - 2).
		Height(innerHeight)

	rendered := border.Render(m.list.View())

	// Replace the top border line with a custom line containing title and badges
	lines := strings.SplitN(rendered, "\n", 2)
	if len(lines) < 2 {
		return rendered
	}

	topLine := m.buildBorderTopLine()
	return topLine + "\n" + lines[1]
}

// buildBorderTopLine constructs the top border line with title and badges embedded.
// Format: ╭─ Title ─│Badge1│─│Badge2│──────╮
func (m listModel) buildBorderTopLine() string {
	borderStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	br := func(s string) string { return borderStyle.Render(s) }

	// Fall back to a plain border for very narrow terminals
	if m.width < 20 {
		fill := m.width - 2 // ╭ and ╮
		if fill < 0 {
			fill = 0
		}
		return br("╭") + br(strings.Repeat("─", fill)) + br("╮")
	}

	// Collect badges: [text, style] pairs
	type badge struct {
		text  string
		style lipgloss.Style
	}
	var badges []badge

	// Tag filter badge
	if m.tagFilter != "" {
		badges = append(badges, badge{
			text:  fmt.Sprintf("tag: %s", m.tagFilter),
			style: lipgloss.NewStyle().Foreground(ui.ColorPrimary),
		})
	}

	// Completed badge — only when hiding completed items
	badgeStyle := lipgloss.NewStyle().Foreground(ui.ColorSubtle)
	if m.hideCompleted {
		badges = append(badges, badge{
			text:  "No completed",
			style: badgeStyle,
		})
	}

	// Wide badge — only when active
	if m.wideMode {
		badges = append(badges, badge{
			text:  "Wide",
			style: badgeStyle,
		})
	}

	// Pre-render badges and measure their total width
	type renderedBadge struct {
		open, text, close string
		width             int // visual width of ┤text├
	}
	var renderedBadges []renderedBadge
	badgesWidth := 0
	for _, b := range badges {
		rb := renderedBadge{
			open:  br("─┤"),
			text:  b.style.Render(b.text),
			close: br("├"),
			width: 2 + lipgloss.Width(b.text) + 1, // ─┤ + text + ├
		}
		renderedBadges = append(renderedBadges, rb)
		badgesWidth += rb.width
	}

	// Build the line: ╭─ Title ─────...─┤Badge├─┤Badge├╮
	var buf strings.Builder

	// Start + title
	buf.WriteString(br("╭─ "))
	buf.WriteString(listTitleStyle.Render(m.borderTitle))
	buf.WriteString(br(" "))
	titleWidth := 4 + lipgloss.Width(m.borderTitle) // ╭─ + title + space

	// Fill between title and badges
	fill := m.width - titleWidth - badgesWidth - 1 // -1 for closing ╮
	if fill > 0 {
		buf.WriteString(br(strings.Repeat("─", fill)))
	}

	// Badges (right-aligned)
	for _, rb := range renderedBadges {
		buf.WriteString(rb.open)
		buf.WriteString(rb.text)
		buf.WriteString(rb.close)
	}

	buf.WriteString(br("╮"))
	return buf.String()
}

// Footer renders an abbreviated help/status footer for the list view.
// Only the most important context-dependent keys are shown; press ? for full help.
func (m listModel) Footer() string {
	var help string

	// Show selection count if any nibs are selected
	var selectionPrefix string
	if len(m.selectedNibs) > 0 {
		selectionStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning).Bold(true)
		selectionPrefix = selectionStyle.Render(fmt.Sprintf("(%d selected) ", len(m.selectedNibs)))
	}

	helpLabel := "more"
	if m.helpExpanded {
		helpLabel = "less"
	}

	if len(m.selectedNibs) > 0 {
		help = renderHelpKey("space", "toggle") + "  " +
			renderHelpKey("s", "status") + "  " +
			renderHelpKey("P", "priority") + "  " +
			renderHelpKey("esc", "clear") + "  " +
			renderHelpKey("?", helpLabel) + "  " +
			renderHelpKey("q", "quit")
	} else if m.hasActiveFilter() {
		help = renderHelpKey("enter", "view") + "  " +
			renderHelpKey("c", "create") + "  " +
			renderHelpKey("e", "edit") + "  " +
			renderHelpKey("esc", "clear filter") + "  " +
			renderHelpKey("?", helpLabel) + "  " +
			renderHelpKey("q", "quit")
	} else {
		help = renderHelpKey("enter", "view") + "  " +
			renderHelpKey("c", "create") + "  " +
			renderHelpKey("e", "edit") + "  " +
			renderHelpKey("/", "filter") + "  " +
			renderHelpKey("?", helpLabel) + "  " +
			renderHelpKey("q", "quit")
	}

	// Show status message if present, otherwise show help
	footer := selectionPrefix
	if m.statusMessage != "" {
		statusStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess).Bold(true)
		footer += statusStyle.Render(m.statusMessage)
	} else {
		footer += help
	}

	return footer
}

// expandedHelpEntries returns all keybindings for the expanded help panel,
// including context-sensitive footer entries (esc, ?, q).
func (m listModel) expandedHelpEntries() []helpEntry {
	entries := listHelpEntries()
	if len(m.selectedNibs) > 0 {
		entries = append(entries, helpEntry{"esc", "clear"})
	} else if m.hasActiveFilter() {
		entries = append(entries, helpEntry{"esc", "clear filter"})
	}
	entries = append(entries, helpEntry{"?", "less"}, helpEntry{"q", "quit"})
	return entries
}

// currentHelpHeight returns the help panel height (0 when collapsed).
func (m listModel) currentHelpHeight() int {
	if !m.helpExpanded {
		return 0
	}
	return helpPanelHeight(m.expandedHelpEntries(), m.width)
}

// updateTitle rebuilds the border title from the project name.
// Badges (tag filter, completed, wide) are added at render time in viewContent().
func (m *listModel) updateTitle() {
	m.borderTitle = fmt.Sprintf("Nibs - %s", m.config.GetProjectName())
}

// ViewConstrained renders the list constrained to the given width and height.
// Used for the left pane in two-column mode. Returns only the content without footer.
// The output will be exactly `height` lines tall.
func (m listModel) ViewConstrained(width, height int) string {
	// Temporarily set constrained dimensions
	m.width = width
	m.height = height

	// Inner height for border content (height minus 2 for top/bottom border)
	innerHeight := height - 2
	m.list.SetSize(width-2, innerHeight)

	// Recalculate columns for constrained width
	m.cols = ui.CalculateResponsiveColumns(width, m.hasTags)
	m.applyWideMode()
	m.updateDelegate()

	return m.viewContent(innerHeight)
}

// findPreviousSibling returns the sibling immediately before n in the tree, or nil.
func (m *listModel) findPreviousSibling(n *nib.Nib) *nib.Nib {
	siblings := m.findSiblings(n)
	for i, s := range siblings {
		if s.ID == n.ID && i > 0 {
			return siblings[i-1]
		}
	}
	return nil
}

// findNextSibling returns the sibling immediately after n in the tree, or nil.
func (m *listModel) findNextSibling(n *nib.Nib) *nib.Nib {
	siblings := m.findSiblings(n)
	for i, s := range siblings {
		if s.ID == n.ID && i < len(siblings)-1 {
			return siblings[i+1]
		}
	}
	return nil
}

// findSiblings returns all siblings (children of the same parent) from the tree.
// For root-level nibs (no parent), returns the top-level tree nodes.
func (m *listModel) findSiblings(n *nib.Nib) []*nib.Nib {
	if m.tree == nil {
		return nil
	}
	return siblingsFromTree(m.tree, n.Parent)
}

// dispatchBlockMove selects the correct reorder strategy based on the
// effective multi-selection:
//
//   - 0 effective items: fall back to the legacy focused-row reorder.
//   - 1 effective item: single-item reorder sourced from the selection.
//   - ≥2 contiguous, same-parent items: block move via reorderBlockMsg.
//   - ≥2 with gaps or multiple parents: silent no-op.
//
// up=true means Ctrl-Up (toward previous sibling); up=false means Ctrl-Down.
func (m listModel) dispatchBlockMove(up bool) tea.Cmd {
	focused, _ := m.list.SelectedItem().(nibItem)

	effective := effectiveSelection(m.selectedNibs, m.tree)

	// Case 1: no multi-selection → today's focused-row behavior.
	if len(effective) == 0 {
		if focused.nib == nil {
			return nil
		}
		return singleReorderCmd(focused.nib, m.findSiblings(focused.nib), up)
	}

	// Case 2: exactly one effective item → single-item reorder sourced from
	// the selection rather than the focus.
	if len(effective) == 1 {
		target := effective[0]
		return singleReorderCmd(target, m.findSiblings(target), up)
	}

	// Case 3+: 2 or more effective items — block move if valid.
	siblings, startIdx, endIdx, ok := blockMovable(effective, m.tree)
	if !ok {
		return nil // silent no-op
	}

	if up {
		if startIdx == 0 {
			return nil // at top, no room
		}
		displaced := siblings[startIdx-1]
		after := siblings[endIdx].ID
		focusID := ""
		if focused.nib != nil {
			focusID = focused.nib.ID
		}
		return func() tea.Msg {
			return reorderBlockMsg{
				displacedID: displaced.ID,
				afterID:     &after,
				focusID:     focusID,
			}
		}
	}

	if endIdx == len(siblings)-1 {
		return nil // at bottom, no room
	}
	displaced := siblings[endIdx+1]
	before := siblings[startIdx].ID
	focusID := ""
	if focused.nib != nil {
		focusID = focused.nib.ID
	}
	return func() tea.Msg {
		return reorderBlockMsg{
			displacedID: displaced.ID,
			beforeID:    &before,
			focusID:     focusID,
		}
	}
}

// singleReorderCmd emits a reorderNibMsg for a single nib, moving it up or
// down among its siblings. Returns nil if the target is already at the
// boundary in that direction.
func singleReorderCmd(target *nib.Nib, siblings []*nib.Nib, up bool) tea.Cmd {
	if target == nil {
		return nil
	}
	// Locate target in siblings.
	idx := -1
	for i, s := range siblings {
		if s.ID == target.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	if up {
		if idx == 0 {
			return nil
		}
		before := siblings[idx-1].ID
		return func() tea.Msg {
			return reorderNibMsg{nibID: target.ID, beforeID: &before}
		}
	}
	if idx == len(siblings)-1 {
		return nil
	}
	after := siblings[idx+1].ID
	return func() tea.Msg {
		return reorderNibMsg{nibID: target.ID, afterID: &after}
	}
}
