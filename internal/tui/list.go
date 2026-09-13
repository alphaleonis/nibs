package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// nibItem is a list.Item for one row of the nib tree.
type nibItem struct {
	nib         *nib.Nib
	cfg         *config.Config
	treePrefix  string
	matched     bool // false for an ancestor shown only as context
	hasChildren bool
	collapsed   bool
	isBlocked   bool
	isBlocking  bool
}

func (i nibItem) Title() string       { return i.nib.Title }
func (i nibItem) Description() string { return i.nib.ID + " · " + i.nib.Status }
func (i nibItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// itemDelegate renders a nibItem as one list row.
type itemDelegate struct {
	cfg          *config.Config
	hasTags      bool
	width        int
	cols         ui.ResponsiveColumns
	idColWidth   int              // includes the tree prefix
	selectedNibs *map[string]bool // IDs marked for multi-select
}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(nibItem)
	if !ok {
		return
	}

	// Raw Priority is safe: "" and "normal" both render no priority symbol, so
	// their differing colors never show.
	colors := d.cfg.GetNibColors(item.nib.Status, item.nib.EffectiveType(), item.nib.Priority)

	idWidth := d.cols.ID
	if d.idColWidth > 0 {
		idWidth = d.idColWidth
	}
	baseWidth := idWidth + d.cols.Status + d.cols.Type + 4 // 4 for cursor + padding
	if d.cols.ShowTags {
		baseWidth += d.cols.Tags
	}
	// Floored at one: zero is RenderNibRow's "no limit" sentinel.
	maxTitleWidth := max(1, m.Width()-baseWidth)

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
			IsClosed:      colors.IsClosed,
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

	// Clip to the list width. The fixed columns can be wider than a narrow
	// terminal, and a longer row wraps inside the bordered box, growing it past
	// its height.
	_, _ = fmt.Fprint(w, lipgloss.NewStyle().MaxWidth(m.Width()).Render(str))
}

// listModel is the model for the nib list view
type listModel struct {
	list    list.Model
	backend Backend
	config  *config.Config
	width   int
	height  int
	err     error

	hasTags    bool // whether any listed nib has tags
	cols       ui.ResponsiveColumns
	idColWidth int // includes tree indentation

	tagFilter     string
	hideCompleted bool // hides every closed status, not only completed

	collapsedIDs map[string]bool
	tree         []*ui.TreeNode // from the last load, for re-flattening

	// Full type/status names, and no preview pane.
	wideMode bool

	selectedNibs map[string]bool // IDs marked for multi-select

	borderTitle string

	statusMessage string
	statusKind    statusKind

	// Selected on the next reload, then cleared.
	selectByID string

	// Set by App.
	helpExpanded    bool
	updateAvailable bool
	updateLatest    string
}

func newListModel(backend Backend, cfg *config.Config) listModel {
	selectedNibs := make(map[string]bool)
	delegate := itemDelegate{cfg: cfg, selectedNibs: &selectedNibs}

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	applyFilterStyles(&l.Styles)

	m := listModel{
		list:          l,
		backend:       backend,
		config:        cfg,
		hideCompleted: cfg.HideCompleted(),
		wideMode:      cfg.WideMode(),
		selectedNibs:  selectedNibs,
		collapsedIDs:  make(map[string]bool),
	}
	m.updateTitle()
	return m
}

type nibsLoadedMsg struct {
	items      []ui.FlatItem
	idColWidth int
	tree       []*ui.TreeNode
}

type errMsg struct {
	err error
}

type selectNibMsg struct {
	nib *nib.Nib
}

func (m listModel) Init() tea.Cmd {
	return m.loadNibs
}

func (m listModel) loadNibs() tea.Msg {
	var filter *model.NibFilter
	if m.tagFilter != "" || m.hideCompleted {
		filter = &model.NibFilter{}
		if m.tagFilter != "" {
			filter.Tags = []string{m.tagFilter}
		}
		if m.hideCompleted {
			filter.ExcludeStatus = m.config.ClosedStatusNames()
		}
	}

	filteredNibs, err := m.backend.ListNibs(context.Background(), filter)
	if err != nil {
		return errMsg{err}
	}

	// Do not filter: treeResolvedParentID reads a parent absent from the tree as
	// nonexistent, so every ancestor must be present.
	allNibs, err := m.backend.ListNibs(context.Background(), nil)
	if err != nil {
		return errMsg{err}
	}

	sortFn := func(nibs []*nib.Nib) {
		nib.SortByOrder(nibs)
	}

	tree := ui.BuildTree(filteredNibs, allNibs, sortFn)
	items := ui.FlattenTreeFiltered(tree, m.collapsedIDs)

	maxIDLen := 0
	for _, b := range allNibs {
		if len(b.ID) > maxIDLen {
			maxIDLen = len(b.ID)
		}
	}
	maxDepth := ui.MaxTreeDepth(items)
	idColWidth := maxIDLen + 2
	if maxDepth > 0 {
		idColWidth += maxDepth * 3 // one tree connector per level
	}
	if ui.HasAnyChildren(tree) {
		idColWidth += 2 // collapse indicator
	}

	return nibsLoadedMsg{items: items, idColWidth: idColWidth, tree: tree}
}

func (m *listModel) setTagFilter(tag string) {
	m.tagFilter = tag
}

func (m *listModel) toggleHideCompleted() {
	m.hideCompleted = !m.hideCompleted
}

// clearFilter clears the tag filter; hideCompleted is kept.
func (m *listModel) clearFilter() {
	m.tagFilter = ""
}

// hasActiveFilter reports whether a tag filter is set; hideCompleted does not
// count.
func (m *listModel) hasActiveFilter() bool {
	return m.tagFilter != ""
}

func (m listModel) Update(msg tea.Msg) (listModel, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	prevIndex := m.list.Index()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for border, "\n", and footer/help panel
		m.list.SetSize(msg.Width-2, msg.Height-3-m.footerHeight())
		m.cols = ui.CalculateResponsiveColumns(msg.Width, m.hasTags)
		m.applyWideMode()
		m.updateDelegate()

	case nibsLoadedMsg:
		m.tree = msg.tree

		// Prune collapsed IDs that are no longer parents in the tree.
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
		m.hasTags = false
		for i, flatItem := range msg.items {
			items[i] = nibItem{
				nib:         flatItem.Nib,
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

		if m.selectByID != "" {
			for i, item := range items {
				if bi, ok := item.(nibItem); ok && bi.nib.ID == m.selectByID {
					m.list.Select(i)
					break
				}
			}
			m.selectByID = ""
		}

		m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
		m.applyWideMode()
		m.updateDelegate()
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyPressMsg:
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "space":
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
				if len(m.selectedNibs) > 0 {
					ids := make([]string, 0, len(m.selectedNibs))
					types := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
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
							nibIDs:        []string{item.nib.ID},
							nibTitle:      item.nib.Title,
							nibTypes:      []string{item.nib.EffectiveType()},
							currentParent: item.nib.Parent,
						}
					}
				}
			case "s":
				if len(m.selectedNibs) > 0 {
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
							nibIDs:        []string{item.nib.ID},
							nibTitle:      item.nib.Title,
							currentStatus: item.nib.Status,
						}
					}
				}
			case "t":
				if len(m.selectedNibs) > 0 {
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
				if len(m.selectedNibs) > 0 {
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
							nibIDs:          []string{item.nib.ID},
							nibTitle:        item.nib.Title,
							currentPriority: item.nib.EffectivePriority(),
						}
					}
				}
			case "E":
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
				selectedType := ""
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					selectedType = item.nib.EffectiveType()
				}
				smartDefault := defaultTypeForContext(selectedType)
				return m, func() tea.Msg {
					return openCreateTypePickerMsg{defaultType: smartDefault}
				}
			case "e":
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return openEditorMsg{
							nibID:   item.nib.ID,
							nibPath: item.nib.Path,
						}
					}
				}
			case "H":
				m.toggleHideCompleted()
				return m, m.loadNibs
			case "W":
				m.wideMode = !m.wideMode
				m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
				m.applyWideMode()
				m.updateDelegate()
				return m, nil
			case "tab":
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
				// Collapse an expanded node; otherwise select its parent.
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					if item.hasChildren && !item.collapsed {
						m.collapsedIDs[item.nib.ID] = true
						m.reflattenTree()
					} else {
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
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren && item.collapsed {
					delete(m.collapsedIDs, item.nib.ID)
					m.reflattenTree()
				}
				return m, nil
			case "ctrl+left":
				// Collapse every descendant, not the node itself.
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren {
					if node := ui.FindNode(m.tree, item.nib.ID); node != nil {
						ui.CollectParentIDs(node.Children, m.collapsedIDs)
						m.reflattenTree()
					}
				}
				return m, nil
			case "ctrl+right":
				if item, ok := m.list.SelectedItem().(nibItem); ok && item.hasChildren {
					if node := ui.FindNode(m.tree, item.nib.ID); node != nil {
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
				if m.tree != nil {
					ui.CollectParentIDs(m.tree, m.collapsedIDs)
					m.reflattenTree()
				}
				return m, nil
			case "]":
				clear(m.collapsedIDs)
				m.reflattenTree()
				return m, nil
			case "y":
				if len(m.selectedNibs) > 0 {
					ids := make([]string, 0, len(m.selectedNibs))
					for id := range m.selectedNibs {
						ids = append(ids, id)
					}
					return m, func() tea.Msg {
						return copyNibIDMsg{ids: ids}
					}
				} else if item, ok := m.list.SelectedItem().(nibItem); ok {
					return m, func() tea.Msg {
						return copyNibIDMsg{ids: []string{item.nib.ID}}
					}
				}
			case "A":
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					ids := gatherNibAndDescendants(m.backend, item.nib.ID)
					descendantCount := len(ids) - 1
					dialog := buildConfirmDialog("archive", item.nib.Title, ids, descendantCount)
					return m, func() tea.Msg {
						return openConfirmMsg{dialog: dialog}
					}
				}
			case "delete":
				if item, ok := m.list.SelectedItem().(nibItem); ok {
					ids := gatherNibAndDescendants(m.backend, item.nib.ID)
					descendantCount := len(ids) - 1
					dialog := buildConfirmDialog("delete", item.nib.Title, ids, descendantCount)
					return m, func() tea.Msg {
						return openConfirmMsg{dialog: dialog}
					}
				}
			case "esc", "backspace":
				// Clear the selection first, then the tag filter.
				if len(m.selectedNibs) > 0 {
					clear(m.selectedNibs)
					return m, nil
				}
				if m.hasActiveFilter() {
					return m, func() tea.Msg {
						return clearFilterMsg{}
					}
				}
			}

			// PgUp snaps to the page's first row before it pages. Match msg.Code:
			// bubbles disables PrevPage/NextPage on a single-page list, so
			// key.Matches fails there.
			if msg.Code == tea.KeyPgUp && m.list.Cursor() > 0 {
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

			// PgDn snaps to the page's last row before it pages.
			if msg.Code == tea.KeyPgDown {
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

	m.list, cmd = m.list.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if m.list.Index() != prevIndex {
		if item, ok := m.list.SelectedItem().(nibItem); ok {
			cmds = append(cmds, func() tea.Msg {
				return cursorChangedMsg{nibID: item.nib.ID}
			})
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *listModel) updateDelegate() {
	delegate := itemDelegate{
		cfg:          m.config,
		hasTags:      m.hasTags,
		width:        m.width,
		cols:         m.cols,
		idColWidth:   m.idColWidth,
		selectedNibs: &m.selectedNibs,
	}
	m.list.SetDelegate(delegate)
}

// applyWideMode forces full type/status column widths when wide mode is active
func (m *listModel) applyWideMode() {
	if m.wideMode {
		m.cols = m.cols.WithFullNames()
	}
}

// reflattenTree rebuilds the items from the cached tree after a collapse
// change, keeping the cursor on the same nib or its nearest visible ancestor.
func (m *listModel) reflattenTree() {
	if m.tree == nil {
		return
	}

	var currentNibID string
	if item, ok := m.list.SelectedItem().(nibItem); ok {
		currentNibID = item.nib.ID
	}

	flatItems := ui.FlattenTreeFiltered(m.tree, m.collapsedIDs)

	items := make([]list.Item, len(flatItems))
	m.hasTags = false
	for i, flatItem := range flatItems {
		items[i] = nibItem{
			nib:         flatItem.Nib,
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

	if currentNibID != "" {
		m.restoreCursor(currentNibID, items)
	}

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

	m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
	m.applyWideMode()
	m.updateDelegate()
}

// restoreCursor selects nibID, or else its nearest ancestor present in items.
func (m *listModel) restoreCursor(nibID string, items []list.Item) {
	for i, item := range items {
		if bi, ok := item.(nibItem); ok && bi.nib.ID == nibID {
			m.list.Select(i)
			return
		}
	}

	parentMap := make(map[string]string)
	buildParentMap(m.tree, parentMap)

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

// buildParentMap maps each child ID in the tree to its parent's ID.
func buildParentMap(nodes []*ui.TreeNode, m map[string]string) {
	walkTreeEdges(nodes, func(parent, child *ui.TreeNode) {
		m[child.Nib.ID] = parent.Nib.ID
	})
}

// walkTreeEdges calls visit once for every parent-child edge in the tree.
func walkTreeEdges(nodes []*ui.TreeNode, visit func(parent, child *ui.TreeNode)) {
	for _, node := range nodes {
		for _, child := range node.Children {
			visit(node, child)
		}
		walkTreeEdges(node.Children, visit)
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
	footer := m.footerRegion()
	innerHeight := m.height - 3 - max(1, lipgloss.Height(footer))
	return m.viewContent(innerHeight) + "\n" + footer
}

// footerRegion is everything drawn below the list box: the compact footer, or
// the expanded help panel laid out by expandedFooterRegion.
func (m listModel) footerRegion() string {
	if !m.helpExpanded {
		return m.Footer()
	}
	return expandedFooterRegion(m.expandedHelpEntries(), m.statusMessage, m.statusKind, m.width, m.height, listBoxFloor)
}

// listBoxFloor is the list box's minimum height: however small an innerHeight
// it is given, a paginated list renders six rows and one that fits on a page
// five. A footer region taller than height-listBoxFloor runs past the
// terminal's last row.
const listBoxFloor = 6

// viewContent renders the bordered list; innerHeight excludes the border rows.
func (m listModel) viewContent(innerHeight int) string {
	// Size the list to this box: the footer may have grown since the last resize.
	m.list.SetSize(m.width-2, innerHeight)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorMuted).
		Width(withBorder(m.width - 2)).
		Height(withBorder(innerHeight))

	rendered := border.Render(m.list.View())

	// Replace the top border line with the title and badges.
	lines := strings.SplitN(rendered, "\n", 2)
	if len(lines) < 2 {
		return rendered
	}

	topLine := m.buildBorderTopLine()
	return topLine + "\n" + lines[1]
}

// badgeWidth is the cells one badge costs on the border's top line: its own
// ─┤ and ├ around the text.
func badgeWidth(text string) int { return 2 + lipgloss.Width(text) + 1 }

// truncateBorderTitle fits title into budget cells, ending a cut with an
// ellipsis. Spaces left before the ellipsis are dropped.
func truncateBorderTitle(title string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if lipgloss.Width(title) <= budget {
		return title
	}
	if budget == 1 {
		return "…"
	}
	cut := strings.TrimRight(lipgloss.NewStyle().MaxWidth(budget-1).Render(title), " ")
	return cut + "…"
}

// buildBorderTopLine draws the top border with the title on the left and the
// badges on the right: ╭─ Title ─────┤Badge├─┤Badge├╮
func (m listModel) buildBorderTopLine() string {
	borderStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	br := func(s string) string { return borderStyle.Render(s) }

	if m.width < 20 {
		fill := m.width - 2 // ╭ and ╮
		if fill < 0 {
			fill = 0
		}
		return br("╭") + br(strings.Repeat("─", fill)) + br("╮")
	}

	type badge struct {
		text  string
		style lipgloss.Style
		// outranksTitle reserves the badge's cells ahead of the title. Only a
		// state the user turned on that can empty the list qualifies.
		outranksTitle bool
	}
	var badges []badge

	if m.tagFilter != "" {
		badges = append(badges, badge{
			text:          fmt.Sprintf("tag: %s", m.tagFilter),
			style:         lipgloss.NewStyle().Foreground(ui.ColorPrimary),
			outranksTitle: true,
		})
	}

	badgeStyle := lipgloss.NewStyle().Foreground(ui.ColorSubtle)
	if m.hideCompleted {
		badges = append(badges, badge{
			text:  "No completed",
			style: badgeStyle,
		})
	}

	if m.wideMode {
		badges = append(badges, badge{
			text:  "Wide",
			style: badgeStyle,
		})
	}

	// Cells for the title and badges; whatever does not fit is dropped, or the
	// line runs past the terminal's right edge.
	budget := m.width - 5 // ╭─ + space around the title + ╮

	// Badges are dropped from the right, so only the leftmost can be reserved
	// ahead of the title.
	titleBudget := budget
	if len(badges) > 0 && badges[0].outranksTitle {
		if first := badgeWidth(badges[0].text); first < budget {
			titleBudget = budget - first
		}
	}
	title := truncateBorderTitle(m.borderTitle, titleBudget)
	titleWidth := 4 + lipgloss.Width(title) // ╭─ + title + space

	// Pre-render the badges that fit, dropping from the right.
	type renderedBadge struct {
		open, text, close string
	}
	var renderedBadges []renderedBadge
	badgesWidth := 0
	for _, b := range badges {
		width := badgeWidth(b.text)
		if lipgloss.Width(title)+badgesWidth+width > budget {
			break
		}
		renderedBadges = append(renderedBadges, renderedBadge{
			open:  br("─┤"),
			text:  b.style.Render(b.text),
			close: br("├"),
		})
		badgesWidth += width
	}

	var buf strings.Builder
	buf.WriteString(br("╭─ "))
	buf.WriteString(listTitleStyle.Render(title))
	buf.WriteString(br(" "))

	fill := m.width - titleWidth - badgesWidth - 1 // -1 for closing ╮
	if fill > 0 {
		buf.WriteString(br(strings.Repeat("─", fill)))
	}

	for _, rb := range renderedBadges {
		buf.WriteString(rb.open)
		buf.WriteString(rb.text)
		buf.WriteString(rb.close)
	}

	buf.WriteString(br("╮"))
	return buf.String()
}

// Footer renders the compact list footer: the selection count, the status
// message or a context-dependent row of keys, and the update indicator.
func (m listModel) Footer() string {
	var help string

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

	footer := selectionPrefix
	if m.statusMessage != "" {
		footer += renderStatusMessage(m.statusMessage, m.statusKind, m.width-lipgloss.Width(selectionPrefix), maxStatusFooterLines(m.height))
	} else {
		footer += help
	}

	footer += m.updateIndicator()

	// The key row and the update indicator do not wrap, so clip them.
	return clipToWidth(footer, m.width)
}

// footerHeight is the row count of footerRegion, at least one.
func (m listModel) footerHeight() int {
	return max(1, lipgloss.Height(m.footerRegion()))
}

// updateIndicator returns the footer's "update available" hint, or "" when no
// newer release is known.
func (m listModel) updateIndicator() string {
	if !m.updateAvailable || m.updateLatest == "" {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	return "  " + style.Render(fmt.Sprintf("↑ nibs %s available — nibs upgrade", m.updateLatest))
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

// updateTitle sets the border title from the project name; buildBorderTopLine
// adds the badges.
func (m *listModel) updateTitle() {
	m.borderTitle = fmt.Sprintf("Nibs - %s", m.config.GetProjectName())
}

// ViewConstrained renders the bordered list without footer, for the left pane
// in two-column mode. The result is height rows tall, but at small heights it
// can reach listBoxFloor.
func (m listModel) ViewConstrained(width, height int) string {
	m.width = width
	m.height = height

	innerHeight := height - 2
	m.list.SetSize(width-2, innerHeight)

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

// findSiblings returns the children of n's parent in tree order, or the roots
// when n has no parent the tree can resolve.
func (m *listModel) findSiblings(n *nib.Nib) []*nib.Nib {
	if m.tree == nil {
		return nil
	}
	return siblingsFromTree(m.tree, treeResolvedParentID(n, m.tree))
}

// dispatchBlockMove moves the effective selection one sibling up or down:
//
//   - no effective items: the focused row moves.
//   - one: that item moves.
//   - two or more, contiguous under one parent: a reorderBlockMsg.
//   - otherwise: a refusal shown in the footer.
//
// No path returns nil, so a refusal is never a silent keypress.
func (m listModel) dispatchBlockMove(up bool) tea.Cmd {
	focused, _ := m.list.SelectedItem().(nibItem)

	effective := effectiveSelection(m.selectedNibs, m.tree)

	if len(effective) == 0 {
		if focused.nib == nil {
			return refuseReorderCmd(reorderReasonNothingSelected)
		}
		return singleReorderCmd(focused.nib, m.findSiblings(focused.nib), m.tree, up)
	}

	if len(effective) == 1 {
		target := effective[0]
		return singleReorderCmd(target, m.findSiblings(target), m.tree, up)
	}

	siblings, startIdx, endIdx, reason := blockMovable(effective, m.tree)
	if reason != "" {
		return refuseReorderCmd(reason)
	}

	if up {
		if startIdx == 0 {
			return refuseReorderCmd(reorderReasonAtTop)
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
		return refuseReorderCmd(reorderReasonAtBottom)
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

// refuseReorderCmd reports a refused reorder so the footer can explain it.
func refuseReorderCmd(reason string) tea.Cmd {
	return func() tea.Msg {
		return reorderRefusedMsg{reason: reason}
	}
}

// singleReorderCmd emits a reorderNibMsg moving target one place up or down
// among siblings, or a refusal when it cannot move. tree is the tree siblings
// came from; it lets the refusal name a parent cycle, and may be nil.
func singleReorderCmd(target *nib.Nib, siblings []*nib.Nib, tree []*ui.TreeNode, up bool) tea.Cmd {
	if target == nil {
		return refuseReorderCmd(reorderReasonNothingSelected)
	}
	idx := -1
	for i, s := range siblings {
		if s.ID == target.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		if inParentCycle(target, tree) {
			// BuildTree severed this nib's parent edge to break a cycle.
			return refuseReorderCmd(reorderReasonInParentCycle)
		}
		// Defensive: the tree and the sibling lookup disagree.
		return refuseReorderCmd(reorderReasonNotInList)
	}
	if up {
		if idx == 0 {
			return refuseReorderCmd(reorderReasonAtTop)
		}
		before := siblings[idx-1].ID
		return func() tea.Msg {
			return reorderNibMsg{nibID: target.ID, beforeID: &before}
		}
	}
	if idx == len(siblings)-1 {
		return refuseReorderCmd(reorderReasonAtBottom)
	}
	after := siblings[idx+1].ID
	return func() tea.Msg {
		return reorderNibMsg{nibID: target.ID, afterID: &after}
	}
}
