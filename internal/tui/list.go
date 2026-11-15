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
	cols         ui.ResponsiveColumns
	idColWidth   int              // includes the tree prefix
	selectedNibs *map[string]bool // IDs marked for multi-select
	ascii        bool             // draw the row's glyphs in ASCII
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
			ASCIIGlyphs:   d.ascii,
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

	ascii bool // draw rows and chrome in ASCII; set through setASCII

	selectedNibs map[string]bool // IDs marked for multi-select

	borderTitle string

	statusMessage string
	statusKind    statusKind

	// Selected on the next reload, then cleared.
	selectByID string

	// Set by App. hints is the rendered hint row, which the footer shows when it
	// has no status message.
	hints           string
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
	l.KeyMap = listKeyMap()
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
	items := ui.FlattenTreeFiltered(tree, m.collapsedIDs, m.ascii)

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

	prevIndex := m.list.Index()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for border, "\n", and footer
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
		// Under an applied filter SetItems clears the visible set and returns
		// the command that re-matches it.
		cmd = m.list.SetItems(items)
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
		return m, cmd

	case errMsg:
		m.err = msg.err
		return m, nil

	case actionMsg:
		cmd = m.runAction(msg.name)
		return m, tea.Batch(cmd, m.cursorChangedCmd(prevIndex))

	case tea.KeyPressMsg:
		// Only a filter being typed takes keys; actions do everything else.
		if m.list.FilterState() != list.Filtering {
			return m, nil
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, tea.Batch(cmd, m.cursorChangedCmd(prevIndex))
}

// cursorChangedCmd reports the nib under the cursor when the cursor has left
// prevIndex, or returns nil.
func (m listModel) cursorChangedCmd(prevIndex int) tea.Cmd {
	if m.list.Index() == prevIndex {
		return nil
	}
	item, ok := m.list.SelectedItem().(nibItem)
	if !ok {
		return nil
	}
	return func() tea.Msg { return cursorChangedMsg{nibID: item.nib.ID} }
}

// targets returns the marked nibs' IDs, or the ID of the nib under the cursor
// when none are marked, or nil when neither is. title names the nib, or counts
// the marked ones.
func (m listModel) targets() (ids []string, title string, focused *nib.Nib) {
	focused = m.focusedNib()
	if len(m.selectedNibs) > 0 {
		ids = make([]string, 0, len(m.selectedNibs))
		for id := range m.selectedNibs {
			ids = append(ids, id)
		}
		return ids, fmt.Sprintf("%d selected nibs", len(ids)), focused
	}
	if focused == nil {
		return nil, "", nil
	}
	return []string{focused.ID}, focused.Title, focused
}

// focusedNib returns the nib under the cursor, or nil when the list is empty.
func (m listModel) focusedNib() *nib.Nib {
	if item, ok := m.list.SelectedItem().(nibItem); ok {
		return item.nib
	}
	return nil
}

// focusedItem returns the row under the cursor; ok is false when the list is
// empty.
func (m listModel) focusedItem() (item nibItem, ok bool) {
	item, ok = m.list.SelectedItem().(nibItem)
	return item, ok
}

// runAction does what the named action does in the list.
func (m *listModel) runAction(name actionName) tea.Cmd {
	switch name {
	case actUp:
		m.list.CursorUp()
	case actDown:
		m.list.CursorDown()
	case actPageUp:
		m.pageUp()
	case actPageDown:
		m.pageDown()
	case actTop:
		m.list.GoToStart()
	case actBottom:
		m.list.GoToEnd()
	case actFilter, actFilterApply, actInputAccept, actInputCancel:
		cmd, _ := browseList(&m.list, name)
		return cmd
	case actFoldRow:
		return m.foldRow()
	case actUnfoldRow:
		if item, ok := m.focusedItem(); ok && item.hasChildren && item.collapsed {
			delete(m.collapsedIDs, item.nib.ID)
			return m.reflattenTree()
		}
	case actFoldSubtree:
		// Collapse every descendant, not the node itself.
		if item, ok := m.focusedItem(); ok && item.hasChildren {
			if node := ui.FindNode(m.tree, item.nib.ID); node != nil {
				ui.CollectParentIDs(node.Children, m.collapsedIDs)
				return m.reflattenTree()
			}
		}
	case actUnfoldSubtree:
		if item, ok := m.focusedItem(); ok && item.hasChildren {
			if node := ui.FindNode(m.tree, item.nib.ID); node != nil {
				var removeCollapsed func([]*ui.TreeNode)
				removeCollapsed = func(nodes []*ui.TreeNode) {
					for _, child := range nodes {
						delete(m.collapsedIDs, child.Nib.ID)
						removeCollapsed(child.Children)
					}
				}
				removeCollapsed(node.Children)
				return m.reflattenTree()
			}
		}
	case actFoldAll:
		if m.tree != nil {
			ui.CollectParentIDs(m.tree, m.collapsedIDs)
			return m.reflattenTree()
		}
	case actUnfoldAll:
		clear(m.collapsedIDs)
		return m.reflattenTree()
	case actOpen:
		if n := m.focusedNib(); n != nil {
			return func() tea.Msg { return selectNibMsg{nib: n} }
		}
	case actLeave:
		return m.leave()
	case actSelect:
		if n := m.focusedNib(); n != nil {
			if m.selectedNibs[n.ID] {
				delete(m.selectedNibs, n.ID)
			} else {
				m.selectedNibs[n.ID] = true
			}
			m.list.CursorDown()
		}
	case actCreate:
		selectedType := ""
		if n := m.focusedNib(); n != nil {
			selectedType = n.EffectiveType()
		}
		smartDefault := defaultTypeForContext(selectedType)
		return func() tea.Msg { return openCreateTypePickerMsg{defaultType: smartDefault} }
	case actEdit:
		if n := m.focusedNib(); n != nil {
			return func() tea.Msg { return openEditorMsg{nibID: n.ID, nibPath: n.Path} }
		}
	case actStatus:
		ids, title, focused := m.targets()
		if ids == nil {
			return nil
		}
		msg := openStatusPickerMsg{nibIDs: ids, nibTitle: title}
		if len(m.selectedNibs) == 0 {
			msg.currentStatus = focused.Status
		}
		return func() tea.Msg { return msg }
	case actType:
		return m.openTypePicker()
	case actParent:
		return m.openParentPicker()
	case actPriority:
		ids, title, focused := m.targets()
		if ids == nil {
			return nil
		}
		msg := openPriorityPickerMsg{nibIDs: ids, nibTitle: title}
		if len(m.selectedNibs) == 0 {
			msg.currentPriority = focused.EffectivePriority()
		}
		return func() tea.Msg { return msg }
	case actEstimate:
		ids, title, focused := m.targets()
		if ids == nil {
			return nil
		}
		msg := openEstimatePickerMsg{nibIDs: ids, nibTitle: title}
		if len(m.selectedNibs) == 0 {
			msg.currentEstimate = focused.Estimate
		}
		return func() tea.Msg { return msg }
	case actModify:
		ids, title, _ := m.targets()
		nibs := make([]*nib.Nib, 0, len(ids))
		for _, id := range ids {
			if n, err := m.backend.GetNib(context.Background(), id); err == nil && n != nil {
				nibs = append(nibs, n)
			}
		}
		if len(nibs) == 0 {
			return nil
		}
		targets := modifyTargets{nibs: nibs, title: title}
		return func() tea.Msg { return openModifyPickerMsg{targets: targets} }
	case actBlocking:
		if n := m.focusedNib(); n != nil {
			currentBlocking := computeCurrentBlocking(m.backend, n.ID)
			return func() tea.Msg {
				return openBlockingPickerMsg{nibID: n.ID, nibTitle: n.Title, currentBlocking: currentBlocking}
			}
		}
	case actMoveUp:
		return m.dispatchBlockMove(true)
	case actMoveDown:
		return m.dispatchBlockMove(false)
	case actArchive:
		return m.confirm("archive")
	case actDelete:
		return m.confirm("delete")
	case actCopyID:
		if ids, _, _ := m.targets(); ids != nil {
			return func() tea.Msg { return copyNibIDMsg{ids: ids} }
		}
	case actHideClosed:
		m.toggleHideCompleted()
		return m.loadNibs
	case actWideMode:
		m.wideMode = !m.wideMode
		m.cols = ui.CalculateResponsiveColumns(m.width, m.hasTags)
		m.applyWideMode()
		m.updateDelegate()
	}
	return nil
}

// foldRow collapses an expanded row, or else selects its parent.
func (m *listModel) foldRow() tea.Cmd {
	item, ok := m.focusedItem()
	if !ok {
		return nil
	}
	if item.hasChildren && !item.collapsed {
		m.collapsedIDs[item.nib.ID] = true
		return m.reflattenTree()
	}
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
	return nil
}

// leave clears the selection, else the tag filter, else the text filter.
func (m *listModel) leave() tea.Cmd {
	switch {
	case len(m.selectedNibs) > 0:
		clear(m.selectedNibs)
	case m.hasActiveFilter():
		return func() tea.Msg { return clearFilterMsg{} }
	case m.list.IsFiltered():
		m.list.ResetFilter()
	}
	return nil
}

// pageUp moves to the page's first row, or to the previous page from there.
func (m *listModel) pageUp() {
	if m.list.Cursor() > 0 {
		m.list.Select(m.list.Paginator.Page * m.list.Paginator.PerPage)
		return
	}
	m.list.PrevPage()
}

// pageDown moves to the page's last row, or to the next page from there.
func (m *listModel) pageDown() {
	itemsOnPage := m.list.Paginator.ItemsOnPage(len(m.list.VisibleItems()))
	if m.list.Cursor() < itemsOnPage-1 {
		m.list.Select(m.list.Paginator.Page*m.list.Paginator.PerPage + itemsOnPage - 1)
		return
	}
	m.list.NextPage()
}

// confirm asks before archiving or deleting the nib under the cursor and its
// descendants.
func (m *listModel) confirm(verb string) tea.Cmd {
	n := m.focusedNib()
	if n == nil {
		return nil
	}
	ids := gatherNibAndDescendants(m.backend, n.ID)
	dialog := buildConfirmDialog(verb, n.Title, ids, len(ids)-1)
	return func() tea.Msg { return openConfirmMsg{dialog: dialog} }
}

func (m *listModel) openTypePicker() tea.Cmd {
	ids, title, focused := m.targets()
	if ids == nil {
		return nil
	}
	if len(m.selectedNibs) == 0 {
		msg := openTypePickerMsg{nibIDs: ids, nibTitle: title, currentType: focused.EffectiveType(), validTypes: validTypesForNib(focused, m.backend)}
		return func() tea.Msg { return msg }
	}
	var validTypes []string
	first := true
	for _, id := range ids {
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
	msg := openTypePickerMsg{nibIDs: ids, nibTitle: title, validTypes: validTypes}
	return func() tea.Msg { return msg }
}

func (m *listModel) openParentPicker() tea.Cmd {
	ids, title, focused := m.targets()
	if ids == nil {
		return nil
	}
	if len(m.selectedNibs) == 0 {
		msg := openParentPickerMsg{nibIDs: ids, nibTitle: title, nibTypes: []string{focused.EffectiveType()}, currentParent: focused.Parent}
		return func() tea.Msg { return msg }
	}
	types := make([]string, 0, len(ids))
	for _, id := range ids {
		for _, item := range m.list.Items() {
			if bi, ok := item.(nibItem); ok && bi.nib.ID == id {
				types = append(types, bi.nib.EffectiveType())
				break
			}
		}
	}
	msg := openParentPickerMsg{nibIDs: ids, nibTitle: title, nibTypes: types}
	return func() tea.Msg { return msg }
}

func (m *listModel) updateDelegate() {
	delegate := itemDelegate{
		cfg:          m.config,
		cols:         m.cols,
		idColWidth:   m.idColWidth,
		selectedNibs: &m.selectedNibs,
		ascii:        m.ascii,
	}
	m.list.SetDelegate(delegate)
}

// setASCII makes the list draw its rows, box and page dots in ASCII, or in
// UTF-8 when ascii is false. Call it before the first load, which draws the
// tree prefixes.
func (m *listModel) setASCII(ascii bool) {
	m.ascii = ascii
	active, inactive := "•", "•"
	if ascii {
		active, inactive = "*", "."
	}
	m.list.Paginator.ActiveDot = m.list.Styles.ActivePaginationDot.SetString(active).String()
	m.list.Paginator.InactiveDot = m.list.Styles.InactivePaginationDot.SetString(inactive).String()
	m.updateDelegate()
}

// boxBorder returns the border the list is drawn in.
func (m listModel) boxBorder() lipgloss.Border {
	if m.ascii {
		return lipgloss.ASCIIBorder()
	}
	return lipgloss.RoundedBorder()
}

// applyWideMode forces full type/status column widths when wide mode is active
func (m *listModel) applyWideMode() {
	if m.wideMode {
		m.cols = m.cols.WithFullNames()
	}
}

// reflattenTree rebuilds the items from the cached tree after a collapse
// change, keeping the cursor on the same nib or its nearest visible ancestor.
// It returns the command SetItems returns, which re-matches an applied filter.
func (m *listModel) reflattenTree() tea.Cmd {
	if m.tree == nil {
		return nil
	}

	var currentNibID string
	if item, ok := m.list.SelectedItem().(nibItem); ok {
		currentNibID = item.nib.ID
	}

	flatItems := ui.FlattenTreeFiltered(m.tree, m.collapsedIDs, m.ascii)

	items := make([]list.Item, len(flatItems))
	m.hasTags = false
	for i, flatItem := range flatItems {
		items[i] = nibItem{
			nib:         flatItem.Nib,
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
	cmd := m.list.SetItems(items)

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
	return cmd
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

	// Inner height: total - border(2) - "\n"(1) - footer height
	footer := m.Footer()
	innerHeight := m.height - 3 - max(1, lipgloss.Height(footer))
	return m.viewContent(innerHeight) + "\n" + footer
}

// viewContent renders the bordered list; innerHeight excludes the border rows.
func (m listModel) viewContent(innerHeight int) string {
	// Size the list to this box: the footer may have grown since the last resize.
	m.list.SetSize(m.width-2, innerHeight)

	border := lipgloss.NewStyle().
		Border(m.boxBorder()).
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

// truncateBorderTitle fits title into budget cells, ending a cut with mark.
// Spaces left before mark are dropped.
func truncateBorderTitle(title string, budget int, mark string) string {
	if budget <= 0 {
		return ""
	}
	if lipgloss.Width(title) <= budget {
		return title
	}
	room := budget - lipgloss.Width(mark)
	if room <= 0 {
		return lipgloss.NewStyle().MaxWidth(budget).Render(mark)
	}
	cut := strings.TrimRight(lipgloss.NewStyle().MaxWidth(room).Render(title), " ")
	return cut + mark
}

// buildBorderTopLine draws the top border with the title on the left and the
// badges on the right: ╭─ Title ─────┤Badge├─┤Badge├╮
func (m listModel) buildBorderTopLine() string {
	borderStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	br := func(s string) string { return borderStyle.Render(s) }
	border := m.boxBorder()

	if m.width < 20 {
		fill := m.width - 2 // ╭ and ╮
		if fill < 0 {
			fill = 0
		}
		return br(border.TopLeft) + br(strings.Repeat(border.Top, fill)) + br(border.TopRight)
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
	title := truncateBorderTitle(m.borderTitle, titleBudget, ellipsis(m.ascii))
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
			open:  br(border.Top + border.MiddleRight),
			text:  b.style.Render(b.text),
			close: br(border.MiddleLeft),
		})
		badgesWidth += width
	}

	var buf strings.Builder
	buf.WriteString(br(border.TopLeft + border.Top + " "))
	buf.WriteString(listTitleStyle.Render(title))
	buf.WriteString(br(" "))

	fill := m.width - titleWidth - badgesWidth - 1 // -1 for closing ╮
	if fill > 0 {
		buf.WriteString(br(strings.Repeat(border.Top, fill)))
	}

	for _, rb := range renderedBadges {
		buf.WriteString(rb.open)
		buf.WriteString(rb.text)
		buf.WriteString(rb.close)
	}

	buf.WriteString(br(border.TopRight))
	return buf.String()
}

// Footer renders the list footer: the selection count, the status message or
// else the hint row, and the update indicator.
func (m listModel) Footer() string {
	var selectionPrefix string
	if len(m.selectedNibs) > 0 {
		selectionStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning).Bold(true)
		selectionPrefix = selectionStyle.Render(fmt.Sprintf("(%d selected) ", len(m.selectedNibs)))
	}

	footer := selectionPrefix
	if m.statusMessage != "" {
		footer += renderStatusMessage(m.statusMessage, m.statusKind, m.width-lipgloss.Width(selectionPrefix), maxStatusFooterLines(m.height), m.ascii)
	} else {
		footer += m.hints
	}

	footer += m.updateIndicator()

	// The hint row and the update indicator do not wrap, so clip them.
	return clipToWidth(footer, m.width)
}

// footerHeight is the row count of Footer, at least one.
func (m listModel) footerHeight() int {
	return max(1, lipgloss.Height(m.Footer()))
}

// updateIndicator returns the footer's "update available" hint, or "" when no
// newer release is known.
func (m listModel) updateIndicator() string {
	if !m.updateAvailable || m.updateLatest == "" {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	text := fmt.Sprintf("↑ nibs %s available — nibs upgrade", m.updateLatest)
	if m.ascii {
		text = fmt.Sprintf("^ nibs %s available - nibs upgrade", m.updateLatest)
	}
	return "  " + style.Render(text)
}

// updateTitle sets the border title from the project name; buildBorderTopLine
// adds the badges.
func (m *listModel) updateTitle() {
	m.borderTitle = fmt.Sprintf("Nibs - %s", m.config.GetProjectName())
}

// ViewConstrained renders the bordered list without footer, for the left pane
// in two-column mode. The result is height rows tall, except at small heights:
// however small a height it is given, a paginated list renders six rows and one
// that fits on a page five.
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
