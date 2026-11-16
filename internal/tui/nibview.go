package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// railMinWidth is the narrowest nib view that draws its relations in a rail
// beside the body. A narrower one stacks them above the body.
const railMinWidth = 100

// The rail takes a third of the view's width, within these bounds.
const (
	railMinCols = 30
	railMaxCols = 48
)

// railGap is the cells between the body and the rail: a space, the divider and
// a space.
const railGap = 3

// glamourKey names a body rendering: the width it wraps at, margins included,
// and whether it is drawn in ASCII.
type glamourKey struct {
	width int
	ascii bool
}

// glamourRenderers caches one renderer per glamourKey, because a renderer's
// width is fixed at construction and the docked view renders on every frame.
var (
	glamourRenderers   = map[glamourKey]*glamour.TermRenderer{}
	glamourRenderersMu sync.Mutex
)

// getGlamourRenderer returns a renderer for key, or nil when one cannot be
// built.
func getGlamourRenderer(key glamourKey) *glamour.TermRenderer {
	key.width = max(1, key.width)
	glamourRenderersMu.Lock()
	defer glamourRenderersMu.Unlock()
	if r, ok := glamourRenderers[key]; ok {
		return r
	}
	style := glamour.WithStylePath("dark")
	if key.ascii {
		style = glamour.WithStyles(asciiMarkdownStyle())
	}
	r, err := glamour.NewTermRenderer(style, glamour.WithWordWrap(key.width))
	if err != nil {
		r = nil
	}
	glamourRenderers[key] = r
	return r
}

// asciiMarkdownStyle is glamour's ASCII style with "* " for a list item, which
// that style draws as "•" (ASCIIStyleConfig.Item in styles/styles.go of
// charm.land/glamour/v2 v2.0.1).
func asciiMarkdownStyle() ansi.StyleConfig {
	s := styles.ASCIIStyleConfig
	s.Item.BlockPrefix = "* "
	return s
}

// backToListMsg steps back to the nib view a link was followed from, or to the
// list when there is none.
type backToListMsg struct{}

// leaveNibViewMsg returns from the expanded nib view to the list, whatever
// links were followed to reach it.
type leaveNibViewMsg struct{}

// nibRegion is a part of the nib view that can hold focus.
type nibRegion uint8

const (
	regionRelations nibRegion = iota
	regionBody
)

// relationGroup is one kind of relation of a nib, under its label: the nibs it
// links, or for Documents the paths the nib names.
type relationGroup struct {
	label string
	nibs  []*nib.Nib
	docs  []string
}

// resolveRelations returns n's relations in the order the web's panel shows
// them: Parent, Children, Blocked by, Blocking, Mentions, Mentioned by and
// Documents. An empty group is left out. A relation that fails to read is left
// out too, because the view only displays them.
//
// Children keep their sibling order. The other groups are sorted by status,
// priority, type, title and id, so they draw in a stable order: the backend
// returns blockers and dependents in none.
func resolveRelations(backend Backend, cfg *config.Config, n *nib.Nib) []relationGroup {
	ctx := context.Background()
	less := nib.LessByStatusPriorityAndType(cfg.StatusNames(), cfg.TypeNames(), cfg)
	sorted := func(ns []*nib.Nib, _ error) []*nib.Nib {
		slices.SortStableFunc(ns, func(a, b *nib.Nib) int {
			switch {
			case less(a, b):
				return -1
			case less(b, a):
				return 1
			}
			return strings.Compare(a.ID, b.ID)
		})
		return ns
	}
	nibsOf := func(ns []*nib.Nib, _ error) []*nib.Nib { return ns }
	var parent []*nib.Nib
	if p, _ := backend.GetParent(ctx, n); p != nil {
		parent = []*nib.Nib{p}
	}
	var groups []relationGroup
	for _, g := range []relationGroup{
		{label: "Parent", nibs: parent},
		{label: "Children", nibs: nibsOf(backend.GetChildren(ctx, n, nil))},
		{label: "Blocked by", nibs: sorted(backend.GetBlockedBy(ctx, n, nil))},
		{label: "Blocking", nibs: sorted(backend.GetBlocking(ctx, n, nil))},
		{label: "Mentions", nibs: sorted(backend.GetMentions(ctx, n, nil))},
		{label: "Mentioned by", nibs: sorted(backend.GetMentionedBy(ctx, n, nil))},
		{label: "Documents", docs: n.Documents},
	} {
		if len(g.nibs) > 0 || len(g.docs) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// nibViewModel draws one nib as the web's panel does: a top rule with the id,
// the title, a meta band, the relations grouped by kind, and the body. It
// renders at any size, docked beside or below the list as a pane or expanded
// to the whole screen with a footer of its own.
//
// It has two focus regions, the relations and the body. Only an active view
// marks the selected relation. The docked view is active while it has the
// focus, and the list takes the keys otherwise.
type nibViewModel struct {
	nib       *nib.Nib // nil draws a placeholder
	milestone *nib.Nib // the nib's milestone, or nil when it has none or it does not resolve
	backend   Backend
	config    *config.Config
	display   display
	// The size of the view: for the expanded view its footer included, and
	// for the docked view, which has no footer, its pane.
	width, height int
	docked        bool // whether the view is the pane beside or below the list

	groups []relationGroup
	active bool      // whether the view takes keys
	focus  nibRegion // the region keys go to while it is on screen; see focused
	cursor int       // the selected relation, an index into entries

	body    viewport.Model
	bodyKey glamourKey // the rendering body holds

	statusMessage string
	statusKind    statusKind
	hints         string // the rendered hint row, set by App
}

// newNibViewModel returns the view of n at width x height, focused on the
// relations.
func newNibViewModel(n *nib.Nib, backend Backend, cfg *config.Config, disp display, width, height int) nibViewModel {
	m := nibViewModel{
		backend: backend,
		config:  cfg,
		display: disp,
		width:   width,
		height:  height,
		active:  true,
		focus:   regionRelations,
		body:    viewport.New(),
	}
	return m.withNib(n)
}

// withNib returns m showing n, which may be a newer copy of the nib on screen.
// The focus and the body's scroll position stay where they are, as far as n
// still has room for them. The cursor stays on the selected relation, or on
// the row nearest its place when n no longer has it.
func (m nibViewModel) withNib(n *nib.Nib) nibViewModel {
	var selected string
	if entries := m.entries(); m.cursor < len(entries) {
		selected = entries[m.cursor].key()
	}
	m.nib, m.milestone, m.groups = n, nil, nil
	m.bodyKey = glamourKey{}
	if n != nil {
		m.groups = resolveRelations(m.backend, m.config, n)
		if n.Milestone != "" {
			m.milestone, _ = m.backend.GetNib(context.Background(), n.Milestone)
		}
	}
	entries := m.entries()
	m.cursor = min(m.cursor, max(0, len(entries)-1))
	if i := slices.IndexFunc(entries, func(e relationEntry) bool { return e.key() == selected }); i >= 0 {
		m.cursor = i
	}
	m.syncBody()
	return m
}

// relationEntry is one row the relations cursor stops on: a related nib, or
// a document path.
type relationEntry struct {
	nib *nib.Nib
	doc string
}

// key names e's relation across reloads.
func (e relationEntry) key() string {
	if e.nib != nil {
		return "nib:" + e.nib.ID
	}
	return "doc:" + e.doc
}

// entries returns the rows of group g.
func (g relationGroup) entries() []relationEntry {
	var es []relationEntry
	for _, n := range g.nibs {
		es = append(es, relationEntry{nib: n})
	}
	for _, d := range g.docs {
		es = append(es, relationEntry{doc: d})
	}
	return es
}

// entries is every relation row, in the order the relations are drawn.
func (m nibViewModel) entries() []relationEntry {
	var es []relationEntry
	for _, g := range m.groups {
		es = append(es, g.entries()...)
	}
	return es
}

// relationNibs is every related nib, in the order the relations are drawn.
func (m nibViewModel) relationNibs() []*nib.Nib {
	var ns []*nib.Nib
	for _, g := range m.groups {
		ns = append(ns, g.nibs...)
	}
	return ns
}

// selectedRelation is the nib under the relations cursor, or nil when the
// cursor is on a document or there is no relation.
func (m nibViewModel) selectedRelation() *nib.Nib {
	if es := m.entries(); m.cursor < len(es) {
		return es[m.cursor].nib
	}
	return nil
}

// nibLayout is how a nib view divides its rows and columns.
type nibLayout struct {
	head      []string // the top rule, the title, the meta band and the rule under them
	rail      bool     // whether the relations are beside the body rather than above it
	relWidth  int
	relRows   int // 0 when the relations are not drawn
	bodyWidth int
	bodyRows  int // 0 when the body is not drawn
}

// layout divides width x height. The head comes first and the body gives way
// before the relations. Stacked relations take at most half of the rows the
// head leaves, unless the body has no row left.
func (m nibViewModel) layout(width, height int) nibLayout {
	l := nibLayout{head: m.headLines(width)}
	if len(l.head) >= height {
		l.head = l.head[:max(0, height)]
		return l
	}
	avail := height - len(l.head)
	if len(m.groups) == 0 {
		l.bodyWidth, l.bodyRows = width, avail
		return l
	}
	if width >= railMinWidth {
		l.rail = true
		l.relWidth = min(railMaxCols, max(railMinCols, width/3))
		l.bodyWidth = width - l.relWidth - railGap
		l.relRows, l.bodyRows = avail, avail
		return l
	}
	l.relWidth, l.bodyWidth = width, width
	need := len(m.groups) + len(m.entries())
	// The rule between the relations and the body takes a row.
	l.relRows = min(need, max(1, (avail-1)/2))
	l.bodyRows = avail - l.relRows - 1
	if l.bodyRows < 1 {
		l.relRows, l.bodyRows = min(need, avail), 0
	}
	return l
}

// contentHeight is the rows the expanded view's footer leaves, or the docked
// view's whole height.
func (m nibViewModel) contentHeight() int {
	if m.docked {
		return m.height
	}
	return max(0, m.height-lipgloss.Height(m.renderFooter(paintedFrame{})))
}

// focused is the region keys go to: the relations while focus names them and
// they are on screen, and the body otherwise.
func (m nibViewModel) focused() nibRegion {
	if m.focus == regionRelations && m.relationsShown() {
		return regionRelations
	}
	return regionBody
}

// relationsShown reports whether the view draws the relations, which the
// relations focus needs.
func (m nibViewModel) relationsShown() bool {
	return len(m.groups) > 0 && m.layout(m.width, m.contentHeight()).relRows > 0
}

// syncBody fits the body to the rows and columns the view gives it.
func (m *nibViewModel) syncBody() {
	m.fitBody(m.layout(m.width, m.contentHeight()))
}

// fitBody sizes the body to l, and renders it again when l changes its width.
func (m *nibViewModel) fitBody(l nibLayout) {
	if l.bodyRows <= 0 || m.nib == nil {
		return
	}
	if key := (glamourKey{width: l.bodyWidth, ascii: m.display.ascii()}); key != m.bodyKey {
		m.bodyKey = key
		m.body.SetContent(m.renderBody(key))
	}
	m.body.SetWidth(l.bodyWidth)
	m.body.SetHeight(l.bodyRows)
}

// Update resizes the expanded view or runs an action in it.
func (m nibViewModel) Update(msg tea.Msg) (nibViewModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case actionMsg:
		cmd = m.runAction(msg.name)
	}
	m.syncBody()
	return m, cmd
}

// runAction does what the named action does in the nib view.
func (m *nibViewModel) runAction(name actionName) tea.Cmd {
	if msg := m.viewMsg(name); msg != nil {
		return func() tea.Msg { return msg }
	}
	if m.focused() == regionRelations {
		if m.moveCursor(name) {
			return nil
		}
	} else if m.scrollBody(name) {
		return nil
	}
	n := m.nib
	if n == nil {
		// The placeholder has no nib to act on.
		return nil
	}
	switch name {
	case actFocusNext, actFocusPrev:
		if m.focused() == regionBody && m.relationsShown() {
			m.focus = regionRelations
		} else {
			m.focus = regionBody
		}
	case actEdit:
		return func() tea.Msg { return openEditorMsg{nibID: n.ID, nibPath: n.Path} }
	case actCopyID:
		return func() tea.Msg { return copyNibIDMsg{ids: []string{n.ID}} }
	}
	return nil
}

// viewMsg returns the message that shows another view for the named action:
// the list, the view before this one, the nib under the relations cursor, or a
// picker for the nib on screen. It returns nil when the action shows no other
// view. The App applies it at once; see App.showView.
func (m *nibViewModel) viewMsg(name actionName) tea.Msg {
	switch name {
	case actLeave:
		return leaveNibViewMsg{}
	case actJumpBack:
		return backToListMsg{}
	case actOpen:
		if r := m.selectedRelation(); r != nil && m.focused() == regionRelations {
			return selectNibMsg{nib: r}
		}
		return nil
	}
	n := m.nib
	if n == nil {
		return nil
	}
	switch name {
	case actParent:
		return openParentPickerMsg{nibIDs: []string{n.ID}, nibTitle: n.Title, nibTypes: []string{n.EffectiveType()}, currentParent: n.Parent}
	case actStatus:
		return openStatusPickerMsg{nibIDs: []string{n.ID}, nibTitle: n.Title, currentStatus: n.Status}
	case actType:
		return openTypePickerMsg{nibIDs: []string{n.ID}, nibTitle: n.Title, currentType: n.EffectiveType(), validTypes: validTypesForNib(n, m.backend)}
	case actPriority:
		return openPriorityPickerMsg{nibIDs: []string{n.ID}, nibTitle: n.Title, currentPriority: n.EffectivePriority()}
	case actEstimate:
		return openEstimatePickerMsg{nibIDs: []string{n.ID}, nibTitle: n.Title, currentEstimate: n.Estimate}
	case actModify:
		return openModifyPickerMsg{targets: modifyTargets{nibs: []*nib.Nib{n}, title: n.Title}}
	case actBlocking:
		return openBlockingPickerMsg{nibID: n.ID, nibTitle: n.Title, currentBlocking: computeCurrentBlocking(m.backend, n.ID)}
	}
	return nil
}

// moveCursor applies a move action to the relations cursor. It reports false
// for any other action.
func (m *nibViewModel) moveCursor(name actionName) bool {
	last := len(m.entries()) - 1
	page := max(1, m.layout(m.width, m.contentHeight()).relRows-1)
	switch name {
	case actUp:
		m.cursor--
	case actDown:
		m.cursor++
	case actPageUp:
		m.cursor -= page
	case actPageDown:
		m.cursor += page
	case actTop:
		m.cursor = 0
	case actBottom:
		m.cursor = last
	default:
		return false
	}
	m.cursor = max(0, min(m.cursor, last))
	return true
}

// scrollBody applies a move action to the body. It reports false for any other
// action.
func (m *nibViewModel) scrollBody(name actionName) bool {
	switch name {
	case actUp:
		m.body.ScrollUp(1)
	case actDown:
		m.body.ScrollDown(1)
	case actPageUp:
		m.body.PageUp()
	case actPageDown:
		m.body.PageDown()
	case actTop:
		m.body.GotoTop()
	case actBottom:
		m.body.GotoBottom()
	default:
		return false
	}
	return true
}

// View draws the expanded view: the nib at the full size, with the footer under
// it.
func (m nibViewModel) View() string {
	height := m.contentHeight()
	l := m.layout(m.width, height)
	// m is a copy, so sizing its body here leaves the stored one as Update
	// sized it. The footer's scroll percentage reads this copy.
	m.fitBody(l)
	content := m.renderLayout(l, m.width, height)
	footer := m.renderFooter(paintedFrame{body: l.bodyRows > 0})
	if height == 0 {
		return footer
	}
	return content + "\n" + footer
}

// render draws the nib into width x height, as the docked view does.
func (m nibViewModel) render(width, height int) string {
	l := m.layout(width, height)
	m.fitBody(l)
	return m.renderLayout(l, width, height)
}

// renderLayout draws l into width x height, padded to height lines.
func (m nibViewModel) renderLayout(l nibLayout, width, height int) string {
	if height <= 0 {
		return ""
	}
	if m.nib == nil {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, helpStyle.Render("No nib selected"))
	}
	lines := append([]string{}, l.head...)
	var body []string
	if l.bodyRows > 0 {
		body = strings.Split(m.body.View(), "\n")
	}
	rel := m.relationRows(l)
	switch {
	case l.rail:
		divider := " " + ruleStyle.Render(dividerGlyph(m.display.ascii())) + " "
		for i := range max(l.relRows, l.bodyRows) {
			left := strings.Repeat(" ", l.bodyWidth)
			if i < len(body) {
				left = padRight(body[i], l.bodyWidth)
			}
			right := ""
			if i < len(rel) {
				right = rel[i]
			}
			lines = append(lines, left+divider+right)
		}
	default:
		lines = append(lines, rel...)
		if len(rel) > 0 && len(body) > 0 {
			lines = append(lines, m.rule(width))
		}
		lines = append(lines, body...)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return clipToWidth(strings.Join(lines[:height], "\n"), width)
}

// padRight pads s with spaces to width cells.
func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

// dividerGlyph returns the character vertical dividers are drawn with.
func dividerGlyph(ascii bool) string {
	if ascii {
		return "|"
	}
	return "│"
}

// rule draws a horizontal rule width cells wide.
func (m nibViewModel) rule(width int) string {
	return ruleStyle.Render(strings.Repeat(ruleGlyph(m.display.ascii()), max(0, width)))
}

// headLines draws the head width cells wide: the top rule with the id in it,
// the title, the meta band and a rule.
func (m nibViewModel) headLines(width int) []string {
	if m.nib == nil {
		return nil
	}
	ascii := m.display.ascii()
	mark := ellipsis(ascii)
	r := ruleGlyph(ascii)
	// Two rule cells and a space lead the id. A space and one rule cell at
	// least follow it.
	top := m.rule(width)
	if id := fitText(m.display.id(m.nib.ID), width-5, mark); id != "" {
		top = ruleStyle.Render(r+r+" ") + ui.ID.Render(id) + ruleStyle.Render(" "+strings.Repeat(r, width-4-lipgloss.Width(id)))
	}
	title := m.display.icons.renderType(m.config, m.nib.EffectiveType()) + " " +
		viewNameStyle.Render(fitText(m.nib.Title, width-2, mark))
	lines := []string{top, title}
	lines = append(lines, m.metaLines(width)...)
	return append(lines, m.rule(width))
}

// metaLines draws the meta band, width cells wide: the status, type and
// priority, then the estimate, milestone, area and tags when the nib has them.
// The fields wrap as a whole onto further lines.
func (m nibViewModel) metaLines(width int) []string {
	n := m.nib
	icons := m.display.icons
	mark := ellipsis(m.display.ascii())
	field := func(label, value string) string { return helpStyle.Render(label) + " " + value }

	fields := []string{
		field("Status", icons.renderStatus(m.config, n.Status)+" "+fitText(n.Status, 20, mark)),
		field("Type", icons.renderType(m.config, n.EffectiveType())+" "+fitText(n.EffectiveType(), 20, mark)),
	}
	priority := fitText(n.EffectivePriority(), 20, mark)
	if g := icons.priority(n.EffectivePriority()); strings.TrimSpace(g) != "" {
		priority = icons.renderPriority(m.config, n.EffectivePriority()) + " " + priority
	}
	fields = append(fields, field("Priority", priority))
	if n.Estimate != "" {
		fields = append(fields, field("Estimate", fitText(n.Estimate, 20, mark)))
	}
	if n.Milestone != "" {
		name := m.display.id(n.Milestone)
		if m.milestone != nil {
			name = m.milestone.Title
		}
		fields = append(fields, field("Milestone", fitText(name, 32, mark)))
	}
	if n.Area != "" {
		fields = append(fields, field("Area", fitText(n.Area, 32, mark)))
	}
	if len(n.Tags) > 0 {
		tags := make([]string, len(n.Tags))
		for i, tag := range n.Tags {
			tags[i] = tagStyle.Render(fitText(tag, 24, mark))
		}
		fields = append(fields, field("Tags", strings.Join(tags, " ")))
	}

	const gap = "   "
	var lines []string
	line := ""
	for _, f := range fields {
		switch {
		case line == "":
			line = f
		case lipgloss.Width(line)+len(gap)+lipgloss.Width(f) <= width:
			line += gap + f
		default:
			lines = append(lines, line)
			line = f
		}
	}
	lines = append(lines, line)
	for i := range lines {
		lines[i] = clipToWidth(lines[i], width)
	}
	return lines
}

// relationLines draws the relation groups width cells wide: each a heading,
// then up to slots[i] rows of group i. A group with more rows than its slots
// shows a run of them, which holds the cursor when the cursor is in the group,
// and a line counting the rest. nil slots draws every row. It also returns the
// line the cursor is on, or -1.
func (m nibViewModel) relationLines(width int, slots []int) ([]string, int) {
	ascii := m.display.ascii()
	mark := ellipsis(ascii)
	headingStyle := helpStyle.Bold(true)
	var lines []string
	cursorLine := -1
	first := 0 // the entry index of the group's first row
	for gi, g := range m.groups {
		entries := g.entries()
		heading := g.label
		if len(entries) > 1 {
			heading += fmt.Sprintf(" %d", len(entries))
		}
		lines = append(lines, headingStyle.Render(fitText(heading, width, mark)))

		shown, start, more := len(entries), 0, false
		if slots != nil && slots[gi] < len(entries) {
			shown, more = slots[gi]-1, true
			if cursorIn := m.cursor - first; cursorIn >= 0 && cursorIn < len(entries) {
				if shown == 0 {
					shown, more = 1, false
				}
				start = max(0, min(cursorIn-shown+1, len(entries)-shown))
			}
		}
		for i, e := range entries[start : start+shown] {
			marker := "  "
			if first+start+i == m.cursor {
				cursorLine = len(lines)
				// focus, not focused: these lines are what decide whether the
				// relations are on screen.
				if m.active && m.focus == regionRelations {
					marker = ui.Primary.Render(ui.GlyphSectionCursor(ascii))
				}
			}
			row := ui.Path.Render(fitText(e.doc, width-2, mark))
			if e.nib != nil {
				row = m.relationRow(e.nib, width-2)
			}
			lines = append(lines, marker+row)
		}
		if more {
			lines = append(lines, "  "+helpStyle.Render(fitText(fmt.Sprintf("+%d more", len(entries)-shown), width-2, mark)))
		}
		first += len(entries)
	}
	return lines, cursorLine
}

// relationRow draws a related nib width cells wide: its status and type, its
// id and its title.
func (m nibViewModel) relationRow(n *nib.Nib, width int) string {
	icons := m.display.icons
	mark := ellipsis(m.display.ascii())
	id := m.display.id(n.ID)
	lead := icons.renderStatus(m.config, n.Status) + " " + icons.renderType(m.config, n.EffectiveType()) + " "
	room := width - 4 // the two icons and their spaces
	id = fitText(id, room, mark)
	room -= lipgloss.Width(id) + 1
	row := lead + ui.ID.Render(id)
	if room > 0 {
		title := n.Title
		style := lipgloss.NewStyle()
		if m.config.IsClosedStatus(n.Status) {
			style = helpStyle
		}
		row += " " + style.Render(fitText(title, room, mark))
	}
	return clipToWidth(row, width)
}

// relationRows is the part of the relations l has room for. When rows allow,
// every group keeps its heading and at least one row; see relationSlots.
// Otherwise it is a window of the relations that holds the cursor.
func (m nibViewModel) relationRows(l nibLayout) []string {
	if l.relRows <= 0 {
		return nil
	}
	if slots := m.relationSlots(l.relRows); slots != nil {
		lines, _ := m.relationLines(l.relWidth, slots)
		return lines
	}
	lines, cursorLine := m.relationLines(l.relWidth, nil)
	if len(lines) <= l.relRows {
		return lines
	}
	top := max(0, min(cursorLine-l.relRows+1, len(lines)-l.relRows))
	return lines[top : top+l.relRows]
}

// relationSlots divides rows among the groups: each gets its heading and one
// row, then the group the cursor is in takes what it needs, then the others
// take a row each in turn. It returns nil when rows cannot give every group a
// heading and a row.
func (m nibViewModel) relationSlots(rows int) []int {
	slots := make([]int, len(m.groups))
	sizes := make([]int, len(m.groups))
	budget := rows - 2*len(m.groups)
	if budget < 0 {
		return nil
	}
	cursorGroup, first := 0, 0
	for i, g := range m.groups {
		sizes[i] = len(g.entries())
		slots[i] = 1
		if m.cursor >= first && m.cursor < first+sizes[i] {
			cursorGroup = i
		}
		first += sizes[i]
	}
	grow := min(budget, sizes[cursorGroup]-1)
	slots[cursorGroup] += grow
	budget -= grow
	for budget > 0 {
		grown := false
		for i := range slots {
			if budget > 0 && slots[i] < sizes[i] {
				slots[i]++
				budget--
				grown = true
			}
		}
		if !grown {
			break
		}
	}
	return slots
}

// paintedFrame is what View drew above the footer.
type paintedFrame struct {
	body bool
}

// renderFooter returns the expanded view's footer: the status message, then
// the scroll percentage and the hint row.
//
// The scroll percentage appears only for a painted body, and leaves no
// placeholder. Keep the footer's height independent of painted, because
// contentHeight measures it with a zero paintedFrame. Everything painted
// toggles sits in the hint row, which is one row because the footer is
// clipped.
func (m nibViewModel) renderFooter(painted paintedFrame) string {
	var footer string
	if painted.body {
		scrollPct := int(m.body.ScrollPercent() * 100)
		footer += helpStyle.Render(fmt.Sprintf("%d%%", scrollPct)) + "  "
	}
	footer += m.hints

	if m.statusMessage != "" {
		// On its own lines above the hint row, so the keys stay on screen.
		footer = renderStatusMessage(m.statusMessage, m.statusKind, m.width, maxStatusFooterLines(m.height), m.display.ascii()) + "\n" + footer
	}
	// The hint row does not wrap, so clip it. The status message is already
	// wrapped to width.
	return clipToWidth(footer, m.width)
}

// renderBody renders the nib's body as markdown with the renderer key names.
func (m nibViewModel) renderBody(key glamourKey) string {
	// TrimSpace, not == "": Parse keeps a body of only blank lines as-is, and
	// glamour renders it to nothing.
	if strings.TrimSpace(m.nib.Body) == "" {
		return helpStyle.Render(" No description")
	}
	renderer := getGlamourRenderer(key)
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
