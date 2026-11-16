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
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/filterlang"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// rowBase is what every row of the list has.
type rowBase struct {
	id          string // see rowIDOf
	depth       int
	treePrefix  string
	hasChildren bool
	collapsed   bool
}

func (b rowBase) base() rowBase { return b }

// listRow is a row of the list: a nibItem or a sectionItem.
type listRow interface {
	list.Item
	base() rowBase
}

// nibItem is a list.Item for the row of one nib.
type nibItem struct {
	rowBase
	nib        *nib.Nib
	matched    bool // false for an ancestor shown only as context
	isBlocked  bool
	isBlocking bool
	facts      rowFacts
}

func (i nibItem) Title() string       { return i.nib.Title }
func (i nibItem) Description() string { return i.nib.ID + " · " + i.nib.Status }
func (i nibItem) FilterValue() string { return i.nib.Title + " " + i.nib.ID }

// sectionItem is the row of a section that no nib heads: a declared area, or
// a leftover section such as Backlog.
type sectionItem struct {
	rowBase
	key   string // the section's key, as viewshape.Row.Section holds it
	label string
	color string // the area's declared color, or ""
}

func (i sectionItem) FilterValue() string { return i.label }

// sectionRowPrefix leads the id of a section row that no nib heads. A nib's id
// comes from its file name, which holds no "/", so no nib has such an id.
const sectionRowPrefix = "/section:"

// rowIDOf returns the id of the list row that shows r: the nib's id, or for a
// section no nib heads, sectionRowPrefix and the section's key.
func rowIDOf(r viewshape.Row) string {
	if r.NibID != "" {
		return r.NibID
	}
	return sectionRowPrefix + r.Section
}

// levelState is what one view of the list keeps while the TUI runs.
type levelState struct {
	cursor    string          // the id of the row under the cursor when the view was left
	collapsed map[string]bool // the ids of the folded nib rows
	// collapsedSections holds the keys of the folded sections no nib heads.
	collapsedSections map[string]bool
}

// levels are the views in the order the view picker lists them and
// view.next steps through them.
var levels = []viewshape.Level{viewshape.LevelTree, viewshape.LevelFlat, viewshape.LevelMilestones, viewshape.LevelAreas}

// levelNames are the names the header and the view picker give the views.
var levelNames = map[viewshape.Level]string{
	viewshape.LevelTree:       "Tree",
	viewshape.LevelFlat:       "Flat",
	viewshape.LevelMilestones: "Milestones",
	viewshape.LevelAreas:      "Areas",
}

// levelViews are the names tui.columns gives the views.
var levelViews = map[viewshape.Level]string{
	viewshape.LevelTree:       treeView,
	viewshape.LevelFlat:       flatView,
	viewshape.LevelMilestones: milestonesView,
	viewshape.LevelAreas:      areasView,
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

	// columns are the columns the current view's rows show after the title,
	// in any order, and columnsByView those of each view by its name in
	// tui.columns. Set both through setColumns.
	columns       []columnName
	columnsByView map[string][]columnName

	// paneHeight is the height the list pane is drawn at, or 0 for the whole
	// terminal above the footer. The App sets it through setPane while the
	// docked nib view takes part of the screen.
	paneHeight int

	// query is the filter's text in the web's token language, the one source
	// of the list's filter. defaultQuery is the query esc in the list returns
	// to; see defaultQuery.
	query, defaultQuery string
	// filter is the query being typed, or else query, as the rows were last
	// shaped for it; see evaluate.
	filter queryFilter
	// box is where the query is typed while editing is set.
	box     textinput.Model
	editing bool
	// completions are the menu's rows while the box is open, and highlight the
	// index of the highlighted one, or -1 for none.
	completions []filterlang.Completion
	highlight   int
	// closedToggle is the last z h, which the next one undoes. setQuery
	// clears it when the query changes.
	closedToggle closedToggle

	// find is the text find.next and find.previous look for, which the rows
	// draw reversed, or "" for none. findBox is where it is typed while
	// finding is set.
	find    string
	findBox textinput.Model
	finding bool

	level     viewshape.Level
	prevLevel viewshape.Level                 // the view shown before level, or level itself
	views     map[viewshape.Level]*levelState // read through state

	nibs  []*nib.Nib          // every nib, from the last load
	byID  map[string]*nib.Nib // nibs by id
	areas *area.Vocabulary    // the declared areas, from the last load

	// unfolded is the rows the view shows with every row unfolded, from the
	// last reshape.
	unfolded []viewshape.Row

	display display // how rows show a nib; set through setDisplay

	selectedNibs map[string]bool // IDs marked for multi-select

	title string // the project's name, drawn in the top rule

	statusMessage string
	statusKind    statusKind

	// Selected on the next reload, then cleared.
	selectByID string

	// Set by App. hints is the rendered hint row, which the footer shows when it
	// has no status message, after dockNote, which says why a shown nib view
	// is not docked.
	hints           string
	dockNote        string
	updateAvailable bool
	updateLatest    string
}

func newListModel(backend Backend, cfg *config.Config) listModel {
	selectedNibs := make(map[string]bool)
	delegate := itemDelegate{cfg: cfg, selectedNibs: &selectedNibs}

	// The list draws only its rows, already shaped for the query. The header
	// carries the filter box, and the bottom rule the row position.
	l := list.New([]list.Item{}, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.SetStatusBarItemName("nib", "nibs")
	l.KeyMap = list.KeyMap{}

	m := listModel{
		list:          l,
		backend:       backend,
		config:        cfg,
		query:         defaultQuery(cfg),
		defaultQuery:  defaultQuery(cfg),
		box:           newFilterBox(),
		findBox:       newFindBox(),
		highlight:     -1,
		selectedNibs:  selectedNibs,
		level:         viewshape.LevelTree,
		prevLevel:     viewshape.LevelTree,
		views:         make(map[viewshape.Level]*levelState),
		columns:       defaultColumns[treeView],
		columnsByView: defaultColumns,
		title:         fmt.Sprintf("Nibs - %s", cfg.GetProjectName()),
	}
	return m
}

type nibsLoadedMsg struct {
	nibs    []*nib.Nib
	areas   *area.Vocabulary
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
	return nibsLoadedMsg{nibs: nibs, areas: m.backend.Areas(), idWidth: idWidth, facts: rowFactsOf(nibs, m.config)}
}

// state returns the current view's state.
func (m listModel) state() *levelState {
	st, ok := m.views[m.level]
	if !ok {
		st = &levelState{collapsed: make(map[string]bool), collapsedSections: make(map[string]bool)}
		m.views[m.level] = st
	}
	return st
}

// collapsed returns the ids of the current view's folded nib rows.
func (m listModel) collapsed() map[string]bool { return m.state().collapsed }

// folded reports whether the current view folds r.
func (m listModel) folded(r viewshape.Row) bool {
	if r.NibID != "" {
		return m.collapsed()[r.NibID]
	}
	return m.state().collapsedSections[r.Section]
}

// setFold folds or unfolds the row of id in the current view.
func (m listModel) setFold(id string, fold bool) {
	folds, key := m.collapsed(), id
	if section, ok := strings.CutPrefix(id, sectionRowPrefix); ok {
		folds, key = m.state().collapsedSections, section
	}
	if fold {
		folds[key] = true
	} else {
		delete(folds, key)
	}
}

// queryText is the query being typed, or else the applied query.
func (m listModel) queryText() string {
	if m.editing {
		return m.box.Value()
	}
	return m.query
}

// evaluate reads queryText into m.filter, and asks the backend for the nibs
// that the fields beyond the metadata ones select. As in the web's table, the
// rest goes to the GraphQL nibs field and the metadata fields are matched
// here, so an ancestor that the field adds to search hits matches when it
// passes the metadata fields.
func (m *listModel) evaluate() {
	text := m.queryText()
	f, diags := filterlang.ParseWithAreas(text, m.areas)
	q := queryFilter{text: text, parsed: f, invalid: filterlang.InvalidTokens(diags)}
	var rest *model.NibFilter
	q.meta, rest = splitFilter(f)
	if rest != nil {
		q.server = true
		q.ids, q.err = m.backend.FilterIDs(context.Background(), rest)
	}
	m.filter = q
}

// shapeView is the current view as viewshape shapes it.
func (m listModel) shapeView() viewshape.View {
	st := m.state()
	return viewshape.View{
		Level:             m.level,
		Filter:            m.filter.meta,
		Match:             m.filter.match(),
		Collapsed:         st.collapsed,
		CollapsedSections: st.collapsedSections,
		Sort:              viewshape.SortOrder,
		Areas:             m.areas,
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
	m.areas = msg.areas
	m.facts = msg.facts
	m.idWidth = msg.idWidth
	// The server's answer and the declared areas both change with the store.
	m.evaluate()

	if err := m.pruneFolds(); err != nil {
		m.err = err
		return
	}
	m.reshape(focus)
	m.pruneMarks()
}

// pruneFolds forgets the fold of each row that has no children to hide with
// no filter on, so a row that gains children later starts unfolded. The query
// does not count, so typing one keeps every fold.
func (m *listModel) pruneFolds() error {
	for level, st := range m.views {
		rows, err := viewshape.Shape(m.nibs, viewshape.View{Level: level, Sort: viewshape.SortOrder, Areas: m.areas})
		if err != nil {
			return err
		}
		parents := make(map[string]bool)
		for _, r := range rows {
			if r.HasChildren {
				parents[rowIDOf(r)] = true
			}
		}
		maps.DeleteFunc(st.collapsed, func(id string, _ bool) bool { return !parents[id] })
		maps.DeleteFunc(st.collapsedSections, func(key string, _ bool) bool { return !parents[sectionRowPrefix+key] })
	}
	return nil
}

// pruneMarks unmarks the nibs that have no matching row in the view, as the
// web drops them from its selection when the filter or the nibs change. A fold
// keeps every mark, so call it after a reshape for any other reason.
func (m *listModel) pruneMarks() {
	keep := make(map[string]bool, len(m.unfolded))
	for _, r := range m.unfolded {
		if r.NibID != "" && !r.Dimmed {
			keep[r.NibID] = true
		}
	}
	maps.DeleteFunc(m.selectedNibs, func(id string, _ bool) bool { return !keep[id] })
}

// reshape shapes the rows again and keeps the cursor on the row of focus, or
// else on its nearest ancestor that is shown. focus is a row id; see rowIDOf.
func (m *listModel) reshape(focus string) {
	v := m.shapeView()
	rows, err := viewshape.Shape(m.nibs, v)
	if err == nil {
		v.Collapsed, v.CollapsedSections = nil, nil
		m.unfolded, err = viewshape.Shape(m.nibs, v)
	}
	if err != nil {
		m.err = err
		return
	}

	prefixes := ui.TreePrefixes(rows, m.folded, m.display.ascii())
	items := make([]list.Item, 0, len(rows))
	for i, r := range rows {
		base := rowBase{
			id:          rowIDOf(r),
			depth:       r.Depth,
			treePrefix:  prefixes[i],
			hasChildren: r.HasChildren,
			collapsed:   r.HasChildren && m.folded(r),
		}
		if r.NibID == "" {
			items = append(items, m.newSectionItem(base, r.Section))
			continue
		}
		items = append(items, nibItem{
			rowBase:    base,
			nib:        m.byID[r.NibID],
			matched:    !r.Dimmed,
			isBlocked:  m.backend.IsBlocked(r.NibID),
			isBlocking: m.backend.IsBlocking(r.NibID),
			facts:      m.facts[r.NibID],
		})
	}
	m.list.SetItems(items)
	m.restoreCursor(focus)
}

// newSectionItem returns the row of the section key, which no nib heads. An
// area's row is labeled with the area's own name, because it is drawn inside
// its parent's. The label is stripped as an area cell is, because areas.yml can
// hold any text.
func (m listModel) newSectionItem(base rowBase, key string) sectionItem {
	item := sectionItem{rowBase: base, key: key, label: safetext.StripLine(key)}
	switch key {
	case viewshape.BacklogSection:
		item.label = "Backlog"
	case viewshape.NoAreaSection:
		item.label = "No area"
	default:
		if node := m.areas.Get(key); node != nil {
			item.label, item.color = safetext.StripLine(node.Name), node.Color
		}
	}
	return item
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

// followQuery shapes the rows again when the query has changed since the last
// shape. Text that reads as the same query, such as the space openBox adds,
// is no change. A query being typed puts the cursor on its first match.
func (m *listModel) followQuery() {
	text := m.queryText()
	if text == m.filter.text {
		return
	}
	if canonicalQuery(text, m.areas) == canonicalQuery(m.filter.text, m.areas) {
		m.filter.text = text
		return
	}
	m.evaluate()
	m.reshape(m.focusedRowID())
	m.pruneMarks()
	if !m.editing || strings.TrimSpace(m.queryText()) == "" {
		return
	}
	for i, item := range m.list.VisibleItems() {
		if item, ok := item.(nibItem); ok && item.matched {
			m.list.Select(i)
			return
		}
	}
}

// showLevel shows level with the cursor and folds it had, and its columns. A
// view shown for the first time keeps the cursor on the same row, or puts it on
// the first row when that row is not in it.
func (m *listModel) showLevel(level viewshape.Level) {
	if level == m.level {
		return
	}
	focus := m.focusedRowID()
	m.state().cursor = focus
	m.prevLevel, m.level = m.level, level
	m.columns = m.columnsByView[levelViews[level]]
	m.updateDelegate(m.width)
	_, visited := m.views[m.level]
	if visited {
		m.reshape(m.state().cursor)
	} else {
		m.reshape(focus)
		if m.focusedRowID() != focus {
			m.list.Select(0)
		}
	}
	m.pruneMarks()
}

// restoreView shows view's level and applies view's query, before the first
// load shapes any rows.
func (m *listModel) restoreView(view rememberedView) {
	m.level, m.prevLevel = view.level, view.level
	m.columns = m.columnsByView[levelViews[view.level]]
	m.query = view.query
}

// shownView returns the view shown and its applied query, and false while a
// query is being typed.
func (m listModel) shownView() (rememberedView, bool) {
	if m.editing {
		return rememberedView{}, false
	}
	return rememberedView{level: m.level, query: m.query}, true
}

// stepLevel shows the view step places after the current one in levels,
// wrapping around at either end.
func (m *listModel) stepLevel(step int) {
	i := slices.Index(levels, m.level)
	m.showLevel(levels[(i+step+len(levels))%len(levels)])
}

// setQuery applies text as the query, keeping the cursor on its row or the
// nearest ancestor shown.
func (m *listModel) setQuery(text string) {
	if canonicalQuery(text, m.areas) != canonicalQuery(m.query, m.areas) {
		m.closedToggle = closedToggle{}
	}
	m.query = text
	m.evaluate()
	m.reshape(m.focusedRowID())
	m.pruneMarks()
}

// toggleClosed hides closed nibs, or shows them when the query hides them all;
// see toggleClosed. The next press restores the query it started from, so the
// user's own status tokens survive, unless the query changed in between.
func (m *listModel) toggleClosed() {
	if m.closedToggle.set && m.closedToggle.made == m.query {
		m.setQuery(m.closedToggle.from)
		return
	}
	from := m.query
	m.setQuery(toggleClosed(from, m.areas))
	m.closedToggle = closedToggle{from: from, made: m.query, set: true}
}

// filtered reports whether the query filters more or less than the default
// query, so esc in the list has a filter to reset.
func (m listModel) filtered() bool {
	return canonicalQuery(m.query, m.areas) != canonicalQuery(m.defaultQuery, m.areas)
}

// openBox opens the filter box on the applied query, with the cursor after
// it and a space, so what is typed starts a token.
func (m *listModel) openBox() {
	m.editing = true
	text := m.query
	if text != "" {
		text += " "
	}
	m.box.SetValue(text)
	m.box.CursorEnd()
	m.box.Focus()
	m.complete(false)
}

// closeBox closes the filter box and applies text as the query, as the web's
// box shows it once it loses focus.
func (m *listModel) closeBox(text string) {
	m.editing = false
	m.box.Blur()
	m.completions, m.highlight = nil, -1
	m.setQuery(canonicalQuery(text, m.areas))
}

// complete fills the completion menu for the box's cursor, with every field
// name for an empty token when explicit is set, as a key press asks for.
func (m *listModel) complete(explicit bool) {
	text := m.box.Value()
	cursor := byteOffset(text, m.box.Position())
	if explicit {
		m.completions = filterlang.CompleteExplicit(text, cursor, m.completionVocabulary())
	} else {
		m.completions = filterlang.Complete(text, cursor, m.completionVocabulary())
	}
	m.highlight = -1
}

// acceptCompletion makes the edit of the highlighted completion, or else of
// the first, and offers the completions for where the cursor lands.
func (m *listModel) acceptCompletion() {
	if len(m.completions) == 0 {
		return
	}
	text, cursor := m.completions[max(0, m.highlight)].Apply(m.box.Value())
	m.box.SetValue(text)
	m.box.SetCursor(runeIndex(text, cursor))
	m.complete(false)
}

// moveHighlight moves the menu's highlight step rows, wrapping at either end.
func (m *listModel) moveHighlight(step int) {
	n := len(m.completions)
	if n == 0 {
		return
	}
	switch {
	case m.highlight < 0 && step < 0:
		m.highlight = n - 1
	case m.highlight < 0:
		m.highlight = 0
	default:
		m.highlight = (m.highlight + step + n) % n
	}
}

func (m listModel) Update(msg tea.Msg) (listModel, tea.Cmd) {
	var cmd tea.Cmd

	prev := m.focusedRowID()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, m.rowsHeight(m.drawnHeight()))
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

	case tea.KeyPressMsg, tea.PasteMsg:
		if m.finding {
			// The App clears the note a find left for a key press, not a paste.
			m.statusMessage, m.statusKind = "", statusOK
			value := m.findBox.Value()
			m.findBox, cmd = m.findBox.Update(msg)
			if m.findBox.Value() != value {
				m.findTyped()
			}
			return m, tea.Batch(cmd, m.cursorChangedCmd(prev))
		}
		// Only the filter box and the find box take keys; actions do everything
		// else.
		if _, key := msg.(tea.KeyPressMsg); key && !m.editing {
			return m, nil
		}
	}

	if !m.editing {
		m.list, cmd = m.list.Update(msg)
		return m, tea.Batch(cmd, m.cursorChangedCmd(prev))
	}
	value, pos := m.box.Value(), m.box.Position()
	m.box, cmd = m.box.Update(msg)
	if m.box.Value() != value || m.box.Position() != pos {
		m.complete(false)
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

// focusedRowID is the id of the row under the cursor, or "" when the list is
// empty; see rowIDOf.
func (m listModel) focusedRowID() string {
	if row, ok := m.focusedRow(); ok {
		return row.id
	}
	return ""
}

// focusedRow returns the row under the cursor; ok is false when the list is
// empty.
func (m listModel) focusedRow() (row rowBase, ok bool) {
	if item, ok := m.list.SelectedItem().(listRow); ok {
		return item.base(), true
	}
	return rowBase{}, false
}

// cursorChangedCmd reports the nib under the cursor when the cursor has left
// the row of prev, or returns nil. A section row reports no nib.
func (m listModel) cursorChangedCmd(prev string) tea.Cmd {
	row := m.focusedRowID()
	if row == "" || row == prev {
		return nil
	}
	return func() tea.Msg { return cursorChangedMsg{} }
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

// runAction does what the named action does in the list.
func (m *listModel) runAction(name actionName) tea.Cmd {
	if msg := m.viewMsg(name); msg != nil {
		return func() tea.Msg { return msg }
	}
	if m.finding {
		switch name {
		case actInputAccept:
			m.closeFind(true)
			return nil
		case actInputCancel:
			m.closeFind(false)
			return nil
		}
	}
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
	case actFilter:
		m.openBox()
	case actFind:
		m.openFind()
	case actFindNext:
		m.findAgain(false)
	case actFindPrevious:
		m.findAgain(true)
	case actInputAccept:
		// In the menu, enter takes the highlighted completion, as on the web.
		if m.highlight >= 0 && m.highlight < len(m.completions) {
			m.acceptCompletion()
			break
		}
		m.closeBox(m.box.Value())
	case actFilterApply:
		m.closeBox(m.box.Value())
	case actInputCancel:
		// As the web's clear button does, esc empties the query, the default
		// token included.
		m.closeBox("")
	case actComplete:
		if len(m.completions) > 0 {
			m.acceptCompletion()
		} else {
			m.complete(true)
		}
	case actCompleteNext:
		m.moveHighlight(1)
	case actCompletePrevious:
		m.moveHighlight(-1)
	case actCompleteClose:
		m.completions, m.highlight = nil, -1
	case actFoldRow:
		m.foldRow()
	case actUnfoldRow:
		if row, ok := m.focusedRow(); ok && row.hasChildren && row.collapsed {
			m.setFold(row.id, false)
			m.reshape(row.id)
		}
	case actFoldSubtree:
		// Fold every descendant, not the row itself.
		if row, ok := m.focusedRow(); ok && row.hasChildren {
			for _, r := range m.descendants(row.id) {
				if r.HasChildren {
					m.setFold(rowIDOf(r), true)
				}
			}
			m.reshape(row.id)
		}
	case actUnfoldSubtree:
		if row, ok := m.focusedRow(); ok && row.hasChildren {
			for _, r := range m.descendants(row.id) {
				m.setFold(rowIDOf(r), false)
			}
			m.reshape(row.id)
		}
	case actFoldAll:
		for _, r := range m.unfolded {
			if r.HasChildren {
				m.setFold(rowIDOf(r), true)
			}
		}
		m.reshape(m.focusedRowID())
	case actUnfoldAll:
		st := m.state()
		clear(st.collapsed)
		clear(st.collapsedSections)
		m.reshape(m.focusedRowID())
	case actViewNext:
		m.stepLevel(1)
	case actViewPrevious:
		m.stepLevel(-1)
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
	case actEdit:
		if n := m.focusedNib(); n != nil {
			return func() tea.Msg { return openEditorMsg{nibID: n.ID, nibPath: n.Path} }
		}
	case actMoveUp:
		return m.reorder(moveUp)
	case actMoveDown:
		return m.reorder(moveDown)
	case actMoveTop:
		return m.reorder(moveTop)
	case actMoveBottom:
		return m.reorder(moveBottom)
	case actCopyID:
		if ids, _, _ := m.targets(); ids != nil {
			return func() tea.Msg { return copyNibIDMsg{ids: ids} }
		}
	case actHideClosed:
		m.toggleClosed()
	}
	return nil
}

// foldRow folds an expanded row, or else moves the cursor to its parent row.
func (m *listModel) foldRow() {
	row, ok := m.focusedRow()
	if !ok {
		return
	}
	if row.hasChildren && !row.collapsed {
		m.setFold(row.id, true)
		m.reshape(row.id)
		return
	}
	items := m.list.Items()
	for i := m.list.Index() - 1; i >= 0; i-- {
		if items[i].(listRow).base().depth < row.depth {
			m.list.Select(i)
			return
		}
	}
}

// descendants returns the rows beneath the row of id when every row is
// unfolded, or nil when the view shows no row for id.
func (m listModel) descendants(id string) []viewshape.Row {
	i := slices.IndexFunc(m.unfolded, func(r viewshape.Row) bool { return rowIDOf(r) == id })
	if i < 0 {
		return nil
	}
	end := i + 1
	for end < len(m.unfolded) && m.unfolded[end].Depth > m.unfolded[i].Depth {
		end++
	}
	return m.unfolded[i+1 : end]
}

// leave clears the selection, else returns the query to the default query.
func (m *listModel) leave() tea.Cmd {
	switch {
	case len(m.selectedNibs) > 0:
		clear(m.selectedNibs)
	case m.filtered():
		m.setQuery(m.defaultQuery)
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
func (m *listModel) confirm(verb string) tea.Msg {
	n := m.focusedNib()
	if n == nil {
		return nil
	}
	ids := gatherNibAndDescendants(m.backend, n.ID)
	dialog := buildConfirmDialog(verb, n.Title, ids, len(ids)-1)
	return openConfirmMsg{dialog: dialog}
}

// viewMsg returns the message that shows another view for the named action:
// the focused nib, or a picker or dialog for the focused or selected nibs. It
// returns nil when the action shows no other view or has no nib to act on. The
// App applies it at once; see App.showView.
func (m *listModel) viewMsg(name actionName) tea.Msg {
	switch name {
	case actOpen:
		if n := m.focusedNib(); n != nil {
			return selectNibMsg{nib: n}
		}
	case actCreate:
		selectedType := ""
		if n := m.focusedNib(); n != nil {
			selectedType = n.EffectiveType()
		}
		return openCreateTypePickerMsg{defaultType: defaultTypeForContext(selectedType)}
	case actStatus:
		ids, title, focused := m.targets()
		if ids == nil {
			return nil
		}
		msg := openStatusPickerMsg{nibIDs: ids, nibTitle: title}
		if len(m.selectedNibs) == 0 {
			msg.currentStatus = focused.Status
		}
		return msg
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
		return msg
	case actEstimate:
		ids, title, focused := m.targets()
		if ids == nil {
			return nil
		}
		msg := openEstimatePickerMsg{nibIDs: ids, nibTitle: title}
		if len(m.selectedNibs) == 0 {
			msg.currentEstimate = focused.Estimate
		}
		return msg
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
		return openModifyPickerMsg{targets: modifyTargets{nibs: nibs, title: title}}
	case actBlocking:
		if n := m.focusedNib(); n != nil {
			currentBlocking := computeCurrentBlocking(m.backend, n.ID)
			return openBlockingPickerMsg{nibID: n.ID, nibTitle: n.Title, currentBlocking: currentBlocking}
		}
	case actArchive:
		return m.confirm("archive")
	case actDelete:
		return m.confirm("delete")
	}
	return nil
}

func (m *listModel) openTypePicker() tea.Msg {
	ids, title, focused := m.targets()
	if ids == nil {
		return nil
	}
	if len(m.selectedNibs) == 0 {
		msg := openTypePickerMsg{nibIDs: ids, nibTitle: title, currentType: focused.EffectiveType(), validTypes: validTypesForNib(focused, m.backend)}
		return msg
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
	return msg
}

func (m *listModel) openParentPicker() tea.Msg {
	ids, title, focused := m.targets()
	if ids == nil {
		return nil
	}
	if len(m.selectedNibs) == 0 {
		msg := openParentPickerMsg{nibIDs: ids, nibTitle: title, nibTypes: []string{focused.EffectiveType()}, currentParent: focused.Parent}
		return msg
	}
	types := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := m.byID[id]; n != nil {
			types = append(types, n.EffectiveType())
		}
	}
	msg := openParentPickerMsg{nibIDs: ids, nibTitle: title, nibTypes: types}
	return msg
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
		find:         findNeedle(m.find),
	})
}

// setColumns sets the columns each view's rows show after the title, by the
// view's name in tui.columns.
func (m *listModel) setColumns(byView map[string][]columnName) {
	m.columnsByView = byView
	m.columns = byView[levelViews[m.level]]
	m.updateDelegate(m.width)
}

// setViewColumns sets the columns the rows of view show after the title.
func (m *listModel) setViewColumns(view string, columns []columnName) {
	byView := maps.Clone(m.columnsByView)
	byView[view] = columns
	m.setColumns(byView)
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
		return slices.IndexFunc(items, func(item list.Item) bool { return item.(listRow).base().id == id })
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

// hasRow reports whether the current view has a row for the nib id, folded
// away or not. A row that the filter dims for context counts.
func (m listModel) hasRow(id string) bool {
	return slices.ContainsFunc(m.unfolded, func(r viewshape.Row) bool { return r.NibID == id })
}

// reveal puts the cursor on the row of id, unfolding the rows it is nested
// under in the current view. It reports false, and leaves the cursor where it
// is, when the view has no row for id; id is a row id, see rowIDOf.
func (m *listModel) reveal(id string) bool {
	if !slices.ContainsFunc(m.unfolded, func(r viewshape.Row) bool { return rowIDOf(r) == id }) {
		return false
	}
	for _, ancestor := range m.ancestors(id) {
		m.setFold(ancestor, false)
	}
	m.reshape(id)
	return m.focusedRowID() == id
}

// ancestors returns the ids of the rows above the row of id that it is nested
// under when every row is unfolded, nearest first. When the view shows no row
// for id, it returns the ids of the nib's parents by their links instead.
func (m listModel) ancestors(id string) []string {
	var ids []string
	if i := slices.IndexFunc(m.unfolded, func(r viewshape.Row) bool { return rowIDOf(r) == id }); i >= 0 {
		depth := m.unfolded[i].Depth
		for j := i - 1; j >= 0 && depth > 0; j-- {
			if r := m.unfolded[j]; r.Depth < depth {
				ids = append(ids, rowIDOf(r))
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
		return safetext.StripText(fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err))
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
	m.updateDelegate(width)
	rows := m.list.View()
	if m.filter.err != nil && len(m.list.Items()) == 0 {
		// The server refused the query, so the list's empty state would
		// misreport the store.
		lines := strings.Split(rows, "\n")
		lines[0] = dockNoteStyle.Render(fitText(m.filter.err.Error(), width, ellipsis(m.display.ascii())))
		rows = strings.Join(lines, "\n")
	}
	if m.editing {
		rows = overlayRows(rows, completionMenu(m.completions, m.highlight, width), width)
	}
	return strings.Join([]string{m.topRule(width), m.header(width), rows, m.bottomRule(width)}, "\n")
}

// ruleGlyph returns the character horizontal rules are drawn with.
func ruleGlyph(ascii bool) string {
	if ascii {
		return "-"
	}
	return "─"
}

var (
	dockNoteStyle = lipgloss.NewStyle().Foreground(ui.ColorWarning)
	ruleStyle     = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	viewNameStyle = lipgloss.NewStyle().Bold(true)
)

// fitText fits s into budget cells, ending a cut with mark. Spaces left before
// mark are dropped. It strips s with safetext.StripLine first, because row
// titles and cells pass through it, so s must be plain text, not styled.
func fitText(s string, budget int, mark string) string {
	s = safetext.StripLine(s)
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

// filterBoxMinWidth is the fewest cells of query the header keeps for the
// filter box while a query is typed.
const filterBoxMinWidth = 10

// headerLayout is what the header of a pane draws: whether it shows the
// count, and the cells of query the filter box gets while a query is typed,
// or 0 while none is.
type headerLayout struct {
	count bool
	box   int
}

// headerLayout lays out the header of a pane width cells wide. While a query
// or a find is typed and its box would get fewer than filterBoxMinWidth cells,
// the header drops the count.
func (m listModel) headerLayout(width int) headerLayout {
	prompt, typing := m.typedPrompt()
	if !typing {
		return headerLayout{count: true}
	}
	// The count is reserved at its widest, every nib, so the box keeps one
	// width while the query narrows the list. "0 nibs" is wider than "1 nib".
	count := max(lipgloss.Width(nibCount(len(m.nibs))), lipgloss.Width(nibCount(0)))
	// The view's name, the gap before the box, its prompt and its cursor.
	fixed := lipgloss.Width(levelNames[m.level]) + 2 + lipgloss.Width(prompt) + 1
	if box := width - fixed - 2 - count; box >= filterBoxMinWidth {
		return headerLayout{count: true, box: box + 1}
	}
	return headerLayout{box: max(1, width-fixed+1)}
}

// typedPrompt returns the prompt of the box the header draws while a query
// or a find is typed, and false while neither is.
func (m listModel) typedPrompt() (string, bool) {
	switch {
	case m.finding:
		return m.findBox.Prompt, true
	case m.editing:
		return m.box.Prompt, true
	}
	return "", false
}

// setPane sets the height the list pane is drawn at, or 0 for the whole
// terminal above the footer, and fits the rows to it, so a page move goes by
// the rows on screen. The width comes with each draw; see pane.
func (m *listModel) setPane(height int) {
	m.paneHeight = height
	m.list.SetSize(m.list.Width(), m.rowsHeight(m.drawnHeight()))
}

// drawnHeight is the height the list pane is drawn at.
func (m listModel) drawnHeight() int {
	if m.paneHeight > 0 {
		return m.paneHeight
	}
	return m.height - m.footerHeight()
}

// header draws the line under the top rule, width cells wide: the view's name
// and the query on the left, colored by token, and the count of nibs that
// match on the right. While a query or a find is typed, its box takes the
// query's place.
// A narrow header drops the count first. What still does not fit is cut at
// the right edge.
func (m listModel) header(width int) string {
	layout := m.headerLayout(width)
	left := viewNameStyle.Render(levelNames[m.level])
	switch {
	case m.finding:
		left += "  " + filterPromptStyle.Render(m.findBox.Prompt) + m.findBoxView(layout.box)
	case m.editing:
		text := m.box.Value()
		left += "  " + filterPromptStyle.Render(m.box.Prompt) +
			renderQuery(text, filterlang.SpansWithAreas(text, m.areas), m.box.Position(), layout.box)
	case m.query != "":
		left += "  " + renderQuery(m.query, filterlang.SpansWithAreas(m.query, m.areas), -1, max(0, width-lipgloss.Width(left)-2))
	}
	if !layout.count {
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

// matchCount is the nibs that match the filter, in folded rows too, leaving
// out ancestors shown only as context.
func (m listModel) matchCount() int { return countMatches(m.unfolded) }

// Footer renders the list footer: the selection count, the status message or
// else the dock note and the hint row, and the update indicator.
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
		if m.dockNote != "" {
			footer += dockNoteStyle.Render(m.dockNote) + "  "
		}
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

// reorder moves the marked nibs, or else the nib under the cursor, within
// their ordering group; see planReorder. Every refusal is reported in the
// footer, so no path returns nil.
func (m listModel) reorder(dir moveDir) tea.Cmd {
	switch m.level {
	// Flat orders every nib by an order key that ranks only within a group, so
	// the row a move passes there is rarely in the moved nib's group.
	case viewshape.LevelFlat:
		return refuseReorderCmd(reorderReasonFlat)
	// The server keeps no order per area.
	case viewshape.LevelAreas:
		return refuseReorderCmd(reorderReasonAreas)
	}
	// Search words can hide the rows a move passes, so the web refuses a drag
	// under them too. The other tokens keep the rows a move passes in their
	// group's order.
	if m.filter.searches() {
		return refuseReorderCmd(reorderReasonSearch)
	}
	subjects := effectiveRows(m.unfolded, m.selectedNibs)
	focusID := m.focusedRowID()
	if focused := m.focusedID(); len(subjects) == 0 && focused != "" {
		if i := slices.IndexFunc(m.unfolded, func(r viewshape.Row) bool { return r.NibID == focused }); i >= 0 {
			subjects = []int{i}
		}
	}
	if len(subjects) == 1 {
		focusID = m.unfolded[subjects[0]].NibID
	}
	unfiltered, err := viewshape.Shape(m.nibs, viewshape.View{Level: m.level, Sort: viewshape.SortOrder, Areas: m.areas})
	if err != nil {
		return refuseReorderCmd(fmt.Sprintf("Can't move: %v", err))
	}
	plan, reason := planReorder(m.nibs, m.unfolded, unfiltered, subjects, dir)
	if reason != "" {
		return refuseReorderCmd(reason)
	}
	return func() tea.Msg { return reorderMsg{plan: plan, focusID: focusID} }
}

// refuseReorderCmd reports a refused reorder so the footer can explain it.
func refuseReorderCmd(reason string) tea.Cmd {
	return func() tea.Msg {
		return reorderRefusedMsg{reason: reason}
	}
}
