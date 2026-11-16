package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/ui"
)

// paletteEntry is one action the palette lists, with its readiness as of when
// the palette opened.
type paletteEntry struct {
	act   *action
	keys  string // the action's bindings, comma-separated
	ready bool
	why   string // why the action cannot run, when it is not ready
}

// paletteModel is the ? overlay: every action active in the context it was
// opened from, narrowed by the query typed into it.
type paletteModel struct {
	origin  viewState  // the view ? was pressed in
	ctx     keyContext // origin's key context
	keys    keymap
	ignored []ignoredEntry // the user config's ignored tui entries
	source  string         // the user config's path, or "" for none
	entries []paletteEntry
	query   textinput.Model
	cursor  int // index into shown()
	width   int
	height  int
}

// newPaletteModel lists the actions of keys active in st.ctx, in actionsByGroup
// order, and the ignored entries of the user config at source.
func newPaletteModel(keys keymap, ignored []ignoredEntry, source string, origin viewState, st actionState, width, height int) paletteModel {
	var entries []paletteEntry
	for _, a := range actionsByGroup(keys.actions, st.ctx) {
		e := paletteEntry{act: a, keys: strings.Join(a.Keys, ", "), ready: true}
		if a.Ready != nil {
			e.ready, e.why = a.Ready(st)
		}
		entries = append(entries, e)
	}

	q := textinput.New()
	q.Placeholder = "type to filter"
	q.Prompt = "> "
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	styles.Focused.Text = lipgloss.NewStyle()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	q.SetStyles(styles)
	q.Focus()

	return paletteModel{origin: origin, ctx: st.ctx, keys: keys, ignored: ignored, source: source, entries: entries, query: q, width: width, height: height}
}

func (m paletteModel) Init() tea.Cmd {
	return textinput.Blink
}

// shown returns the entries whose name, description or keys contain the query,
// ignoring letter case.
func (m paletteModel) shown() []paletteEntry {
	q := strings.ToLower(m.query.Value())
	if q == "" {
		return m.entries
	}
	var out []paletteEntry
	for _, e := range m.entries {
		text := strings.ToLower(string(e.act.Name) + "\n" + e.act.Desc + "\n" + e.keys)
		if strings.Contains(text, q) {
			out = append(out, e)
		}
	}
	return out
}

// highlighted returns the entry under the cursor; ok is false when nothing
// matches the query.
func (m paletteModel) highlighted() (e paletteEntry, ok bool) {
	shown := m.shown()
	if m.cursor < 0 || m.cursor >= len(shown) {
		return paletteEntry{}, false
	}
	return shown[m.cursor], true
}

func (m paletteModel) Update(msg tea.Msg) (paletteModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case actionMsg:
		switch msg.name {
		case actPaletteUp:
			m.cursor = max(0, m.cursor-1)
		case actPaletteDown:
			m.cursor = min(max(0, len(m.shown())-1), m.cursor+1)
		}
		return m, nil
	case tea.KeyPressMsg:
		before := m.query.Value()
		var cmd tea.Cmd
		m.query, cmd = m.query.Update(msg)
		if m.query.Value() != before {
			m.cursor = 0
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.query, cmd = m.query.Update(msg)
	return m, cmd
}

var (
	paletteGroupStyle  = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	paletteCursorStyle = lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	paletteNameStyle   = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	// paletteDimStyle paints an action that cannot run now.
	paletteDimStyle  = lipgloss.NewStyle().Foreground(ui.ColorGrayDim)
	paletteWarnStyle = lipgloss.NewStyle().Foreground(ui.ColorWarning).Bold(true)
)

// paletteChromeRows is the rows the modal spends outside its action lines: the
// border's two, the title, the query, a blank on each side of the lines, and
// the hint row.
const paletteChromeRows = 7

// View renders the palette modal.
func (m paletteModel) View() string {
	modalWidth := max(20, min(m.width-4, 80))
	inner := modalWidth - 2 // the padding's two columns

	title := lipgloss.NewStyle().Bold(true).Render("Actions") + ui.Muted.Render(" · "+m.ctx.String())
	m.query.SetWidth(max(1, inner-lipgloss.Width(m.query.Prompt)-1))

	// Cap the body so a full modal leaves two rows above and below it, so the
	// footer stays in sight.
	room := max(1, m.height-paletteChromeRows-4)
	body := strings.Join(m.bodyLines(inner, room), "\n")

	hints := hintRowWithin(m.keys.actions, actionState{ctx: ctxPalette}, inner)
	content := strings.Join([]string{clipToWidth(title, inner), m.query.View(), "", body, "", hints}, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(0, 1).
		Width(withBorder(modalWidth)).
		Render(content)
}

// bodyLines renders the ignored entries that match the query, then the shown
// entries under their group headings. It renders at most room lines of width
// cells, scrolled so the highlighted entry is on screen.
func (m paletteModel) bodyLines(width, room int) []string {
	lines := m.ignoredLines(width)
	shown := m.shown()
	if len(shown) == 0 {
		return append(lines, ui.Muted.Render("no action matches"))
	}

	keyWidth, nameWidth := 0, 0
	for _, e := range m.entries {
		keyWidth = max(keyWidth, lipgloss.Width(e.keys))
		nameWidth = max(nameWidth, lipgloss.Width(string(e.act.Name)))
	}
	// Each line is the cursor's two cells, then key, description and name with
	// a two-cell gap between them.
	descWidth := width - 2 - keyWidth - 2 - 2 - nameWidth
	if descWidth < 12 {
		nameWidth = 0
		descWidth = width - 2 - keyWidth - 2
	}

	cursorLine := 0
	group := ""
	for i, e := range shown {
		if e.act.Group != group {
			group = e.act.Group
			lines = append(lines, paletteGroupStyle.Render(group))
		}
		if i == m.cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, m.entryLine(e, i == m.cursor, keyWidth, descWidth, nameWidth))
	}

	start := 0
	if cursorLine >= room {
		start = cursorLine - room + 1
	}
	return lines[start:min(len(lines), start+room)]
}

// ignoredLines renders the ignored tui entries whose text or reason contains
// the query, ignoring letter case, under a heading that names the file. It
// returns nothing when no ignored entry matches.
func (m paletteModel) ignoredLines(width int) []string {
	q := strings.ToLower(m.query.Value())
	var lines []string
	for _, b := range m.ignored {
		if !strings.Contains(strings.ToLower(b.entry+"\n"+b.reason), q) {
			continue
		}
		line := fmt.Sprintf("  line %d  %s  %s", b.line, b.entry, ui.Muted.Render(b.reason))
		lines = append(lines, truncateTitle(line, width))
	}
	if len(lines) == 0 {
		return nil
	}
	heading := "Ignored TUI settings"
	if m.source != "" {
		heading += " in " + safetext.StripBounded(m.source)
	}
	lines = slices.Insert(lines, 0, truncateTitle(paletteWarnStyle.Render(heading), width))
	return append(lines, "")
}

// entryLine renders one action: key, description and config name, or dimmed
// with the reason it cannot run.
func (m paletteModel) entryLine(e paletteEntry, highlighted bool, keyWidth, descWidth, nameWidth int) string {
	cursor := "  "
	if highlighted {
		cursor = paletteCursorStyle.Render("▸ ")
	}
	desc := e.act.Desc
	if !e.ready {
		desc = fmt.Sprintf("%s (%s)", desc, e.why)
	}
	desc = truncateTitle(desc, descWidth)

	keyCol := lipgloss.NewStyle().Width(keyWidth).Render(e.keys)
	descCol := lipgloss.NewStyle().Width(descWidth).Render(desc)
	var nameCol string
	if nameWidth > 0 {
		nameCol = "  " + string(e.act.Name)
	}

	if !e.ready {
		return cursor + paletteDimStyle.Render(keyCol+"  "+descCol+nameCol)
	}
	return cursor + helpKeyStyle.Render(keyCol) + "  " + descCol + paletteNameStyle.Render(nameCol)
}
