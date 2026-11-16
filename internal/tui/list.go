package tui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// nibItem is a list.Item for one row of the list.
type nibItem struct {
	nib         *nib.Nib
	depth       int
	treePrefix  string
	matched     bool // false for an ancestor shown only as context
	hasChildren bool
	collapsed   bool
	isBlocked   bool
	isBlocking  bool
	facts       rowFacts
}

func (i nibItem) Title() string       { return i.nib.Title }
func (i nibItem) Description() string { return i.nib.ID + " · " + i.nib.Status }
func (i nibItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// levelState is what one view of the list keeps while the TUI runs.
type levelState struct {
	cursor    string          // the nib under the cursor when the view was left
	collapsed map[string]bool // the ids of the folded rows
}

// levels are the views view.next steps through, in order.
var levels = []viewshape.Level{viewshape.LevelTree, viewshape.LevelFlat}

// levelNames are the names the header gives the views.
var levelNames = map[viewshape.Level]string{
	viewshape.LevelTree: "Tree",
	viewshape.LevelFlat: "Flat",
}

// keepOrder is the list's filter. Rows reach the list already shaped for the
// query, so it keeps every row in its place.
func keepOrder(_ string, targets []string) []list.Rank {
	ranks := make([]list.Rank, len(targets))
	for i := range targets {
		ranks[i] = list.Rank{Index: i}
	}
	return ranks
}

// textMatch returns the test a nib passes when its title or ID holds query in
// any letter case, or nil for an empty query.
func textMatch(query string) func(*nib.Nib) bool {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	return func(n *nib.Nib) bool {
		return strings.Contains(strings.ToLower(n.Title), q) || strings.Contains(strings.ToLower(n.ID), q)
	}
}

// listModel is the model for the nib list view
type listModel struct {
	list    list.Model
	backend Backend
	config  *config.Config
	width   int
	height  int
	err     error

	idWidth int                 // the widest ID as rows show it, over every nib in the store
	facts   map[string]rowFacts // each nib's row facts, from the last load

	// columns are the columns the rows show after the title, in any order;
	// set through setColumns.
	columns []columnName

	// paneWidth is the width the list pane is drawn at, or 0 for the whole
	// terminal. The App sets it while the nib view takes part of the width.
	paneWidth int

	tagFilter     string
	hideCompleted bool // hides every closed status, not only completed

	level viewshape.Level
	views map[viewshape.Level]*levelState // read through state

	nibs []*nib.Nib          // every nib, from the last load
	byID map[string]*nib.Nib // nibs by id

	// unfolded is the rows the view shows with every row unfolded, from the
	// last reshape.
	unfolded []viewshape.Row
	// shapedQuery is the text query the rows were last shaped for.
	shapedQuery string
	// unqueriedCount is the nibs that match the view without the text query.
	unqueriedCount int

	// tree holds the nibs the tag and closed filters match, and their
	// ancestors. Reorders move a nib among its siblings in it that the view
	// has a row for.
	tree []*ui.TreeNode

	display display // how rows show a nib; set through setDisplay

	selectedNibs map[string]bool // IDs marked for multi-select

	title string // the project's name, drawn in the top rule

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

	// The list draws only its rows. The header carries the filter input, and
	// the bottom rule the row position.
	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.Filter = keepOrder
	l.SetShowHelp(false)
	l.SetStatusBarItemName("nib", "nibs")
	l.FilterInput.Prompt = "/"
	l.KeyMap = listKeyMap()
	applyFilterStyles(&l.Styles)

	m := listModel{
		list:          l,
		backend:       backend,
		config:        cfg,
		hideCompleted: cfg.HideCompleted(),
		selectedNibs:  selectedNibs,
		level:         viewshape.LevelTree,
		views:         make(map[viewshape.Level]*levelState),
		columns:       defaultColumns[treeView],
		title:         fmt.Sprintf("Nibs - %s", cfg.GetProjectName()),
	}
	return m
}

type nibsLoadedMsg struct {
	nibs    []*nib.Nib
	idWidth int
	facts   map[string]rowFacts
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
	nibs, err := m.backend.ListNibs(context.Background(), nil)
	if err != nil {
		return errMsg{err}
	}
	idWidth := 0
	for _, b := range nibs {
		idWidth = max(idWidth, lipgloss.Width(m.display.id(b.ID)))
	}
	return nibsLoadedMsg{nibs: nibs, idWidth: idWidth, facts: rowFactsOf(nibs, m.config)}
}

// state returns the current view's state.
func (m listModel) state() *levelState {
	st, ok := m.views[m.level]
	if !ok {
		st = &levelState{collapsed: make(map[string]bool)}
		m.views[m.level] = st
	}
	return st
}

// collapsed returns the ids of the current view's folded rows.
func (m listModel) collapsed() map[string]bool { return m.state().collapsed }

// filter is the metadata filter that the tag filter and hiding closed nibs
// make.
func (m listModel) filter() viewshape.Filter {
	var f viewshape.Filter
	if m.tagFilter != "" {
		f.Tags = []string{m.tagFilter}
	}
	if m.hideCompleted {
		f.ExcludeStatus = m.config.ClosedStatusNames()
	}
	return f
}

// textQuery is the text filter's query, or "" while there is none.
func (m listModel) textQuery() string {
	if m.list.FilterState() == list.Unfiltered {
		return ""
	}
	return m.list.FilterValue()
}

// shapeView is the current view as viewshape shapes it.
func (m listModel) shapeView() viewshape.View {
	return viewshape.View{
		Level:     m.level,
		Filter:    m.filter(),
		Match:     textMatch(m.textQuery()),
		Collapsed: m.collapsed(),
		Sort:      viewshape.SortOrder,
	}
}

// load takes the nibs of a load and shapes the rows for them, keeping the
// cursor on focus.
func (m *listModel) load(msg nibsLoadedMsg, focus string) {
	m.nibs = msg.nibs
	m.byID = make(map[string]*nib.Nib, len(msg.nibs))
	for _, b := range msg.nibs {
		m.byID[b.ID] = b
	}
	m.facts = msg.facts
	m.idWidth = msg.idWidth

	f := m.filter()
	var matched []*nib.Nib
	for _, b := range msg.nibs {
		if viewshape.Matches(b, f) {
			matched = append(matched, b)
		}
	}
	m.tree = ui.BuildTree(matched, msg.nibs, nib.SortByOrder)

	if err := m.pruneFolds(); err != nil {
		m.err = err
		return
	}
	m.reshape(focus)
	m.pruneMarks()
}

// pruneFolds forgets the fold of each row that has no children to hide under
// the metadata filter, so a row that gains children later starts unfolded. The
// text query does not count, so typing one keeps every fold.
func (m *listModel) pruneFolds() error {
	for level, st := range m.views {
		rows, err := viewshape.Shape(m.nibs, viewshape.View{Level: level, Filter: m.filter(), Sort: viewshape.SortOrder})
		if err != nil {
			return err
		}
		parents := make(map[string]bool)
		for _, r := range rows {
			if r.HasChildren {
				parents[r.NibID] = true
			}
		}
		maps.DeleteFunc(st.collapsed, func(id string, _ bool) bool { return !parents[id] })
	}
	return nil
}

// pruneMarks unmarks the nibs that have no matching row in the view, as the
// web drops them from its selection when the filter or the nibs change. A fold
// keeps every mark, so call it after a reshape for any other reason.
func (m *listModel) pruneMarks() {
	keep := make(map[string]bool, len(m.unfolded))
	for _, r := range m.unfolded {
		if !r.Dimmed {
			keep[r.NibID] = true
		}
	}
	maps.DeleteFunc(m.selectedNibs, func(id string, _ bool) bool { return !keep[id] })
}

// inView returns the test a nib passes when the view has a row for it, folded
// or not.
func (m listModel) inView() func(*nib.Nib) bool {
	ids := make(map[string]bool, len(m.unfolded))
	for _, r := range m.unfolded {
		ids[r.NibID] = true
	}
	return func(n *nib.Nib) bool { return ids[n.ID] }
}

// reshape shapes the rows again and keeps the cursor on focus, or else on its
// nearest ancestor that is shown.
func (m *listModel) reshape(focus string) {
	v := m.shapeView()
	rows, err := viewshape.Shape(m.nibs, v)
	if err == nil {
		v.Collapsed = nil
		m.unfolded, err = viewshape.Shape(m.nibs, v)
	}
	if err == nil {
		m.unqueriedCount = countMatches(m.unfolded)
		if v.Match != nil {
			v.Match = nil
			var unqueried []viewshape.Row
			unqueried, err = viewshape.Shape(m.nibs, v)
			m.unqueriedCount = countMatches(unqueried)
		}
	}
	if err != nil {
		m.err = err
		return
	}
	m.shapedQuery = m.textQuery()

	prefixes := ui.TreePrefixes(rows, m.collapsed(), m.display.ascii())
	items := make([]list.Item, 0, len(rows))
	for i, r := range rows {
		n := m.byID[r.NibID]
		if n == nil {
			// Only a section row has no nib, and Tree and Flat draw no sections.
			continue
		}
		items = append(items, nibItem{
			nib:         n,
			depth:       r.Depth,
			treePrefix:  prefixes[i],
			matched:     !r.Dimmed,
			hasChildren: r.HasChildren,
			collapsed:   r.HasChildren && m.collapsed()[r.NibID],
			isBlocked:   m.backend.IsBlocked(n.ID),
			isBlocking:  m.backend.IsBlocking(n.ID),
			facts:       m.facts[n.ID],
		})
	}
	// Under a filter, SetItems hands back the command that matches the rows
	// with keepOrder. Running it here shows the rows before the next draw.
	if cmd := m.list.SetItems(items); cmd != nil {
		m.list, _ = m.list.Update(cmd())
	}
	m.restoreCursor(focus)
}

// countMatches is the rows of rows that show a nib matching the view.
func countMatches(rows []viewshape.Row) int {
	n := 0
	for _, r := range rows {
		if r.NibID != "" && !r.Dimmed {
			n++
		}
	}
	return n
}

// followQuery shapes the rows again when the text query has changed since the
// last shape. A new query puts the cursor on its first match.
func (m *listModel) followQuery() {
	query := m.textQuery()
	if query == m.shapedQuery {
		return
	}
	m.reshape(m.focusedID())
	m.pruneMarks()
	if query == "" {
		return
	}
	for i, item := range m.list.VisibleItems() {
		if item.(nibItem).matched {
			m.list.Select(i)
			return
		}
	}
}

// nextLevel shows the view after the current one in levels, with the cursor
// and folds that view had. A view shown for the first time keeps the cursor on
// the same nib, or puts it on the first row when that nib has no row there.
func (m *listModel) nextLevel() {
	focus := m.focusedID()
	m.state().cursor = focus
	i := slices.Index(levels, m.level)
	m.level = levels[(i+1)%len(levels)]
	_, visited := m.views[m.level]
	if visited {
		m.reshape(m.state().cursor)
	} else {
		m.reshape(focus)
		if m.focusedID() != focus {
			m.list.Select(0)
		}
	}
	m.pruneMarks()
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

	prev := m.focusedID()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, m.rowsHeight(msg.Height-m.footerHeight()))
		m.updateDelegate(msg.Width)

	case nibsLoadedMsg:
		focus := prev
		if m.selectByID != "" {
			focus = m.selectByID
			m.selectByID = ""
		}
		m.load(msg, focus)
		m.updateDelegate(m.width)
		return m, m.cursorChangedCmd(prev)

	case errMsg:
		m.err = msg.err
		return m, nil

	case actionMsg:
		cmd = m.runAction(msg.name)
		m.followQuery()
		return m, tea.Batch(cmd, m.cursorChangedCmd(prev))

	case tea.KeyPressMsg:
		// Only a filter being typed takes keys; actions do everything else.
		if m.list.FilterState() != list.Filtering {
			return m, nil
		}
	}

	// The input scrolls its query by the width it has when a key arrives.
	m.sizeFilterInput(m.drawnWidth())
	if m.list.FilterState() == list.Filtering {
		// The list's own update returns a command that matches the rows it
		// holds now, which would replace the rows followQuery shapes. So the
		// input alone takes the message.
		m.list.FilterInput, cmd = m.list.FilterInput.Update(msg)
	} else {
		m.list, cmd = m.list.Update(msg)
	}
	m.followQuery()
	return m, tea.Batch(cmd, m.cursorChangedCmd(prev))
}

// focusedID is the id of the nib under the cursor, or "" when the list is
// empty.
func (m listModel) focusedID() string {
	if n := m.focusedNib(); n != nil {
		return n.ID
	}
	return ""
}

// cursorChangedCmd reports the nib under the cursor when it is not prev, or
// returns nil.
func (m listModel) cursorChangedCmd(prev string) tea.Cmd {
	id := m.focusedID()
	if id == "" || id == prev {
		return nil
	}
	return func() tea.Msg { return cursorChangedMsg{nibID: id} }
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
	case actInputAccept, actFilterApply:
		// A query that matches nothing leaves nothing to browse, so accepting
		// it clears it.
		if m.list.FilterState() == list.Filtering && len(m.list.Items()) == 0 {
			m.list.ResetFilter()
			return nil
		}
		cmd, _ := browseList(&m.list, name)
		return cmd
	case actFilter:
		// The list turns off its own filter key while it has no rows, and an
		// applied query can come to match none.
		if m.list.FilterState() == list.FilterApplied {
			m.list.SetFilterState(list.Filtering)
			return textinput.Blink
		}
		cmd, _ := browseList(&m.list, name)
		return cmd
	case actInputCancel:
		cmd, _ := browseList(&m.list, name)
		return cmd
	case actFoldRow:
		m.foldRow()
	case actUnfoldRow:
		if item, ok := m.focusedItem(); ok && item.hasChildren && item.collapsed {
			delete(m.collapsed(), item.nib.ID)
			m.reshape(item.nib.ID)
		}
	case actFoldSubtree:
		// Fold every descendant, not the row itself.
		if item, ok := m.focusedItem(); ok && item.hasChildren {
			for _, r := range m.descendants(item.nib.ID) {
				if r.HasChildren {
					m.collapsed()[r.NibID] = true
				}
			}
			m.reshape(item.nib.ID)
		}
	case actUnfoldSubtree:
		if item, ok := m.focusedItem(); ok && item.hasChildren {
			for _, r := range m.descendants(item.nib.ID) {
				delete(m.collapsed(), r.NibID)
			}
			m.reshape(item.nib.ID)
		}
	case actFoldAll:
		for _, r := range m.unfolded {
			if r.HasChildren {
				m.collapsed()[r.NibID] = true
			}
		}
		m.reshape(m.focusedID())
	case actUnfoldAll:
		clear(m.collapsed())
		m.reshape(m.focusedID())
	case actViewNext:
		m.nextLevel()
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
	}
	return nil
}

// foldRow folds an expanded row, or else moves the cursor to its parent row.
func (m *listModel) foldRow() {
	item, ok := m.focusedItem()
	if !ok {
		return
	}
	if item.hasChildren && !item.collapsed {
		m.collapsed()[item.nib.ID] = true
		m.reshape(item.nib.ID)
		return
	}
	items := m.list.Items()
	for i := m.list.Index() - 1; i >= 0; i-- {
		if items[i].(nibItem).depth < item.depth {
			m.list.Select(i)
			return
		}
	}
}

// descendants returns the rows beneath the row of id when every row is
// unfolded, or nil when the view shows no row for id.
func (m listModel) descendants(id string) []viewshape.Row {
	i := slices.IndexFunc(m.unfolded, func(r viewshape.Row) bool { return r.NibID == id })
	if i < 0 {
		return nil
	}
	end := i + 1
	for end < len(m.unfolded) && m.unfolded[end].Depth > m.unfolded[i].Depth {
		end++
	}
	return m.unfolded[i+1 : end]
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
		if n := m.byID[id]; n != nil {
			types = append(types, n.EffectiveType())
		}
	}
	msg := openParentPickerMsg{nibIDs: ids, nibTitle: title, nibTypes: types}
	return func() tea.Msg { return msg }
}

// updateDelegate lays the rows out for a list width cells wide.
func (m *listModel) updateDelegate(width int) {
	m.list.SetDelegate(itemDelegate{
		cfg:     m.config,
		display: m.display,
		layout: newRowLayout(width, m.columns, func(name columnName) int {
			return columnWidth(m.config, name, m.idWidth)
		}),
		selectedNibs: &m.selectedNibs,
	})
}

// setColumns sets the columns the rows show after the title.
func (m *listModel) setColumns(columns []columnName) {
	m.columns = columns
	m.updateDelegate(m.width)
}

// setDisplay sets how rows show a nib. Call it before the first load, which
// draws the tree prefixes and measures the IDs.
func (m *listModel) setDisplay(d display) {
	m.display = d
	m.updateDelegate(m.width)
}

// restoreCursor puts the cursor on the row of id, or else on the row of its
// nearest ancestor. A cursor past the last row moves to the last row.
func (m *listModel) restoreCursor(id string) {
	items := m.list.VisibleItems()
	indexOf := func(id string) int {
		return slices.IndexFunc(items, func(item list.Item) bool { return item.(nibItem).nib.ID == id })
	}
	for _, candidate := range append([]string{id}, m.ancestors(id)...) {
		if i := indexOf(candidate); i >= 0 {
			m.list.Select(i)
			return
		}
	}
	if len(items) > 0 && m.list.Index() >= len(items) {
		m.list.Select(len(items) - 1)
	}
}

// ancestors returns the ids of the rows above id that it is nested under when
// every row is unfolded, nearest first. When the view shows no row for id, it
// returns id's parents by their links instead.
func (m listModel) ancestors(id string) []string {
	var ids []string
	if i := slices.IndexFunc(m.unfolded, func(r viewshape.Row) bool { return r.NibID == id }); i >= 0 {
		depth := m.unfolded[i].Depth
		for j := i - 1; j >= 0 && depth > 0; j-- {
			if r := m.unfolded[j]; r.Depth < depth {
				ids = append(ids, r.NibID)
				depth = r.Depth
			}
		}
		return ids
	}
	seen := map[string]bool{id: true}
	for n := m.byID[id]; n != nil && n.Parent != "" && !seen[n.Parent]; n = m.byID[n.Parent] {
		seen[n.Parent] = true
		ids = append(ids, n.Parent)
	}
	return ids
}

func (m listModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}

	if m.width == 0 {
		return "Loading..."
	}

	footer := m.Footer()
	return m.pane(m.width, m.height-max(1, lipgloss.Height(footer))) + "\n" + footer
}

// paneChromeLines is the lines the list pane draws around its rows: the top
// rule, the header and the bottom rule.
const paneChromeLines = 3

// rowsHeight is the rows a list pane height lines tall shows, at least one.
func (m listModel) rowsHeight(height int) int { return max(1, height-paneChromeLines) }

// pane renders the list without its footer, width cells wide: the top rule,
// the header, the rows and the bottom rule. It is height lines tall when that
// leaves room for a row.
func (m listModel) pane(width, height int) string {
	// Size the list to this pane: the footer may have grown since the last
	// resize, and the nib view takes part of the width.
	m.list.SetSize(width, m.rowsHeight(height))
	m.sizeFilterInput(width)
	m.updateDelegate(width)
	return strings.Join([]string{m.topRule(width), m.header(width), m.list.View(), m.bottomRule(width)}, "\n")
}

// ruleGlyph returns the character horizontal rules are drawn with.
func ruleGlyph(ascii bool) string {
	if ascii {
		return "-"
	}
	return "─"
}

var (
	ruleStyle        = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	viewNameStyle    = lipgloss.NewStyle().Bold(true)
	filterTokenStyle = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
)

// fitText fits s into budget cells, ending a cut with mark. Spaces left before
// mark are dropped.
func fitText(s string, budget int, mark string) string {
	if budget <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= budget {
		return s
	}
	room := budget - lipgloss.Width(mark)
	if room <= 0 {
		return lipgloss.NewStyle().MaxWidth(budget).Render(mark)
	}
	cut := strings.TrimRight(lipgloss.NewStyle().MaxWidth(room).Render(s), " ")
	return cut + mark
}

// topRule draws the rule above the list, width cells wide, with the title in
// it: ── Nibs - project ─────
func (m listModel) topRule(width int) string {
	r := ruleGlyph(m.display.ascii())
	// Two rule cells and a space lead the title. A space and one rule cell at
	// least follow it.
	title := fitText(m.title, width-5, ellipsis(m.display.ascii()))
	if title == "" {
		return ruleStyle.Render(strings.Repeat(r, max(0, width)))
	}
	fill := width - 4 - lipgloss.Width(title)
	return ruleStyle.Render(r+r+" ") + listTitleStyle.Render(title) + ruleStyle.Render(" "+strings.Repeat(r, fill))
}

// bottomRule draws the rule under the rows, width cells wide, with the row
// position near its right end: ───── 7 / 23 ─
func (m listModel) bottomRule(width int) string {
	r := ruleGlyph(m.display.ascii())
	pos := m.position()
	// A space on each side of the position and one rule cell after it.
	fill := width - lipgloss.Width(pos) - 3
	if fill < 1 {
		return ruleStyle.Render(strings.Repeat(r, max(0, width)))
	}
	return ruleStyle.Render(strings.Repeat(r, fill)+" ") + helpStyle.Render(pos) + ruleStyle.Render(" "+r)
}

// position is the row under the cursor and the count of rows, as "7 / 23", or
// "0 / 0" for an empty list.
func (m listModel) position() string {
	total := len(m.list.VisibleItems())
	if total == 0 {
		return "0 / 0"
	}
	return fmt.Sprintf("%d / %d", m.list.Index()+1, total)
}

// filterInputMinWidth is the fewest cells of query the header keeps for the
// filter input while a filter is typed.
const filterInputMinWidth = 10

// headerParts is what the header of a pane draws: whether it shows the tokens
// and the count, and the cells of query the filter input gets while a filter
// is typed, or 0 while none is.
type headerParts struct {
	tokens, count bool
	input         int
}

// headerParts lays out the header of a pane width cells wide. While a filter
// is typed and the input would get fewer than filterInputMinWidth cells, the
// header drops the count, then the tokens.
func (m listModel) headerParts(width int) headerParts {
	if m.list.FilterState() != list.Filtering {
		return headerParts{tokens: true, count: true}
	}
	tokens := lipgloss.Width(strings.Join(m.filterTokens(), " "))
	// The count is reserved at its widest, so the input keeps one width while
	// the query narrows the list: the unfiltered count, or "0 nibs" when that
	// is "1 nib".
	count := max(lipgloss.Width(nibCount(m.unqueriedCount)), lipgloss.Width(nibCount(0)))
	// The view's name, the gap before the input, its prompt and its cursor.
	fixed := lipgloss.Width(levelNames[m.level]) + 2 + lipgloss.Width(m.list.FilterInput.Prompt) + 1
	for _, p := range []headerParts{{tokens: true, count: true}, {tokens: true}} {
		used := fixed
		if p.tokens && tokens > 0 {
			used += 2 + tokens
		}
		if p.count {
			used += 2 + count
		}
		if p.input = width - used; p.input >= filterInputMinWidth {
			return p
		}
	}
	return headerParts{input: max(1, width-fixed)}
}

// sizeFilterInput fits the filter input to the header of a pane width cells
// wide.
func (m *listModel) sizeFilterInput(width int) {
	m.list.FilterInput.SetWidth(m.headerParts(width).input)
}

// drawnWidth is the width the list pane is drawn at.
func (m listModel) drawnWidth() int {
	if m.paneWidth > 0 {
		return m.paneWidth
	}
	return m.width
}

// header draws the line under the top rule, width cells wide: the view's name
// and the active filter as tokens on the left, and the count of nibs that
// match on the right. While a filter is typed, its input follows the tokens.
// A narrow header drops the count first, and while a query is typed the
// tokens next. What still does not fit is cut at the right edge.
func (m listModel) header(width int) string {
	parts := m.headerParts(width)
	left := viewNameStyle.Render(levelNames[m.level])
	if tokens := m.filterTokens(); parts.tokens && len(tokens) > 0 {
		left += "  " + filterTokenStyle.Render(strings.Join(tokens, " "))
	}
	if m.list.FilterState() == list.Filtering {
		left += "  " + m.list.FilterInput.View()
	}
	if !parts.count {
		return clipToWidth(left, width)
	}
	count := nibCount(m.matchCount())
	gap := width - lipgloss.Width(left) - lipgloss.Width(count)
	if gap < 2 {
		return clipToWidth(left, width)
	}
	return left + strings.Repeat(" ", gap) + helpStyle.Render(count)
}

// nibCount is n as the header shows it: "1 nib" or "n nibs".
func nibCount(n int) string {
	if n == 1 {
		return "1 nib"
	}
	return fmt.Sprintf("%d nibs", n)
}

// filterTokens returns the active filter: the tag and closed statuses it
// hides, in the web's token language, with an applied text filter as the
// TUI's own "/text" between them. The default -status:closed comes last, so a
// narrow header cuts it before the filters the user set.
func (m listModel) filterTokens() []string {
	var tokens []string
	if m.tagFilter != "" {
		tokens = append(tokens, "tags:"+m.tagFilter)
	}
	if m.list.FilterState() == list.FilterApplied {
		tokens = append(tokens, "/"+m.list.FilterValue())
	}
	if m.hideCompleted {
		tokens = append(tokens, "-status:closed")
	}
	return tokens
}

// matchCount is the nibs that match the filter, in folded rows too, leaving
// out ancestors shown only as context.
func (m listModel) matchCount() int { return countMatches(m.unfolded) }

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
		footer += renderStatusMessage(m.statusMessage, m.statusKind, m.width-lipgloss.Width(selectionPrefix), maxStatusFooterLines(m.height), m.display.ascii())
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
	if m.display.ascii() {
		text = fmt.Sprintf("^ nibs %s available - nibs upgrade", m.updateLatest)
	}
	return "  " + style.Render(text)
}

// findSiblings returns the children of n's parent that the view has a row for,
// in tree order, or those roots when n has no parent the tree can resolve.
func (m *listModel) findSiblings(n *nib.Nib) []*nib.Nib {
	if m.tree == nil {
		return nil
	}
	return siblingsFromTree(m.tree, treeResolvedParentID(n, m.tree), m.inView())
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
	// Flat orders every nib by an order key that ranks only siblings, so the
	// row a move passes there is rarely the sibling it passes.
	if m.level == viewshape.LevelFlat {
		return refuseReorderCmd(reorderReasonFlat)
	}
	// A text filter can hide the rows a move passes, so the web refuses it too.
	if m.textQuery() != "" {
		return refuseReorderCmd(reorderReasonTextFilter)
	}
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

	siblings, startIdx, endIdx, reason := blockMovable(effective, m.tree, m.inView())
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
