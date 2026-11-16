package tui

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/filterlang"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/alphaleonis/nibs/internal/viewshape"
	"github.com/charmbracelet/x/ansi"
)

// closedToken is the query token that hides every closed status.
const closedToken = "-status:closed"

// defaultQuery is the query the list opens on when nothing is remembered: the
// closed nibs hidden when the config hides them, or no filter.
func defaultQuery(cfg *config.Config) string {
	if cfg.HideCompleted() {
		return closedToken
	}
	return ""
}

// splitFilter splits f into its metadata fields, which viewshape matches, and
// the rest, which only the server evaluates. rest is nil when f sets nothing
// else.
func splitFilter(f model.NibFilter) (meta viewshape.Filter, rest *model.NibFilter) {
	meta = viewshape.Filter{
		Type: f.Type, ExcludeType: f.ExcludeType,
		Priority: f.Priority, ExcludePriority: f.ExcludePriority,
		Status: f.Status, ExcludeStatus: f.ExcludeStatus,
		Estimate: f.Estimate, ExcludeEstimate: f.ExcludeEstimate,
		Tags: f.Tags, ExcludeTags: f.ExcludeTags,
	}
	f.Type, f.ExcludeType, f.Priority, f.ExcludePriority = nil, nil, nil, nil
	f.Status, f.ExcludeStatus, f.Estimate, f.ExcludeEstimate = nil, nil, nil, nil
	f.Tags, f.ExcludeTags = nil, nil
	if reflect.ValueOf(f).IsZero() {
		return meta, nil
	}
	return meta, &f
}

// queryFilter is a query as the list applies it.
type queryFilter struct {
	text string // the query it was read from
	// parsed is text as filterlang.ParseWithAreas reads it, and invalid the
	// tokens it kept out of parsed.
	parsed  model.NibFilter
	invalid []string
	meta    viewshape.Filter
	// server reports whether parsed sets a field beyond meta. ids then holds
	// the nibs Backend.FilterIDs returns for those fields, or err why the
	// server refused them.
	server bool
	ids    map[string]bool
	err    error
}

// match returns the test a nib passes beside q.meta: membership in q.ids, no
// nib when the server refused, or nil when q has no server part.
func (q queryFilter) match() func(*nib.Nib) bool {
	switch {
	case !q.server:
		return nil
	case q.err != nil:
		return func(*nib.Nib) bool { return false }
	}
	ids := q.ids
	return func(n *nib.Nib) bool { return ids[n.ID] }
}

// searches reports whether q holds search words, under which the web refuses
// a drag.
func (q queryFilter) searches() bool {
	return q.parsed.Search != nil && *q.parsed.Search != ""
}

// canonicalQuery returns text as the web's filter box shows it once it loses
// focus: parsed and serialized again, the invalid tokens last.
func canonicalQuery(text string, areas *area.Vocabulary) string {
	f, diags := filterlang.ParseWithAreas(text, areas)
	return filterlang.Serialize(f, filterlang.InvalidTokens(diags))
}

// closedToggle is a change toggleClosed made, the query from before and the
// query it made, or none when set is false.
type closedToggle struct {
	from, made string
	set        bool
}

// toggleClosed returns text with the closed statuses excluded, or with their
// exclusion removed when every one is already excluded, as canonical text.
func toggleClosed(text string, areas *area.Vocabulary) string {
	f, diags := filterlang.ParseWithAreas(text, areas)
	closed, _ := filterlang.Parse(closedToken)
	if missing := slices.DeleteFunc(slices.Clone(closed.ExcludeStatus), func(s string) bool {
		return slices.Contains(f.ExcludeStatus, s)
	}); len(missing) > 0 {
		f.ExcludeStatus = append(f.ExcludeStatus, missing...)
	} else {
		f.ExcludeStatus = slices.DeleteFunc(f.ExcludeStatus, func(s string) bool {
			return slices.Contains(closed.ExcludeStatus, s)
		})
	}
	return filterlang.Serialize(f, filterlang.InvalidTokens(diags))
}

// newFilterBox returns the text box the query is edited in. A key bound to an
// action in the filter box's contexts never reaches it.
func newFilterBox() textinput.Model {
	box := textinput.New()
	box.Prompt = "/"
	box.KeyMap.AcceptSuggestion.SetEnabled(false)
	box.KeyMap.NextSuggestion.SetEnabled(false)
	box.KeyMap.PrevSuggestion.SetEnabled(false)
	return box
}

// spanStyles color the query's highlight spans. A kind not listed is drawn
// plain.
var spanStyles = map[filterlang.SpanKind]lipgloss.Style{
	filterlang.SpanField:    lipgloss.NewStyle().Foreground(ui.ColorBlue),
	filterlang.SpanOperator: lipgloss.NewStyle().Foreground(ui.ColorMuted),
	filterlang.SpanValue:    lipgloss.NewStyle().Foreground(ui.ColorPrimary),
	filterlang.SpanInvalid:  lipgloss.NewStyle().Foreground(ui.ColorDanger).Underline(true),
}

var (
	filterPromptStyle = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	filterCursorStyle = lipgloss.NewStyle().Reverse(true)
)

// queryCell is one rune of a drawn query.
type queryCell struct {
	text  string
	width int
	style lipgloss.Style
}

// renderQuery draws text with its spans' colors in at most width cells.
// cursor is the rune index the box's cursor is at, drawn reversed, or -1 for
// no cursor. Text cut to fit is cut at the right, or at the left when the
// cursor would fall outside the width.
//
// The text is drawn as typed: the box drops control runes, and a remembered
// query holding one is refused when it is read.
func renderQuery(text string, spans []filterlang.Span, cursor, width int) string {
	var cells []queryCell
	for _, sp := range spans {
		style := spanStyles[sp.Kind]
		for _, r := range text[sp.Start:sp.End] {
			s := string(r)
			cells = append(cells, queryCell{text: s, width: ansi.StringWidth(s), style: style})
		}
	}
	if cursor >= 0 {
		if cursor >= len(cells) {
			cells = append(cells, queryCell{text: " ", width: 1})
			cursor = len(cells) - 1
		}
		cells[cursor].style = filterCursorStyle
	}
	start, used := 0, 0
	if cursor >= 0 {
		for i := cursor; i >= 0 && used+cells[i].width <= width; i-- {
			start, used = i, used+cells[i].width
		}
	}
	var b strings.Builder
	used = 0
	for _, c := range cells[start:] {
		if used+c.width > width {
			break
		}
		used += c.width
		b.WriteString(c.style.Render(c.text))
	}
	return b.String()
}

// runeIndex returns the rune index of the byte offset i in s.
func runeIndex(s string, i int) int { return utf8.RuneCountInString(s[:i]) }

// byteOffset returns the byte offset of the rune index i in s.
func byteOffset(s string, i int) int {
	runes := []rune(s)
	return len(string(runes[:min(i, len(runes))]))
}

// nibSearchLimit is the most nibs a relationship token's completion offers,
// as the web's NIB_SEARCH_LIMIT caps them.
const nibSearchLimit = 8

// storeTags returns the distinct tags of the nibs, sorted.
func (m listModel) storeTags() []string {
	var tags []string
	for _, n := range m.nibs {
		tags = append(tags, n.Tags...)
	}
	slices.Sort(tags)
	return slices.Compact(tags)
}

// completionVocabulary returns what completion offers beside the token
// tables: the tags of the nibs, the declared areas, and nibs from a search of
// the store.
func (m listModel) completionVocabulary() filterlang.Vocabulary {
	backend := m.backend
	return filterlang.Vocabulary{
		Tags:  m.storeTags(),
		Areas: m.areas,
		SearchNibs: func(fragment string, types []string) []filterlang.NibSuggestion {
			nibs, err := backend.ListNibs(context.Background(), &model.NibFilter{Search: &fragment, Type: types})
			if err != nil {
				return nil
			}
			out := make([]filterlang.NibSuggestion, 0, min(len(nibs), nibSearchLimit))
			for _, n := range nibs[:min(len(nibs), nibSearchLimit)] {
				out = append(out, filterlang.NibSuggestion{ID: n.ID, Title: n.Title, Type: n.EffectiveType(), Status: n.Status})
			}
			return out
		},
	}
}

// maxCompletionRows is the most completions the menu shows at once.
const maxCompletionRows = 8

var (
	completionBorderStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.ColorMuted)
	completionHighlightStyle = lipgloss.NewStyle().Reverse(true)
)

// completionLabel is how the menu shows c: a field name with its colon, or a
// nib's id and title.
func completionLabel(c filterlang.Completion) (item, detail string) {
	switch c.Kind {
	case filterlang.CompletionField:
		return c.Item + ":", ""
	case filterlang.CompletionNib:
		return displayID(c.Nib.ID), c.Nib.Title
	}
	return c.Item, ""
}

// completionMenu draws the completions in a box at most width cells wide, the
// highlighted one reversed, scrolled to keep it in view. It is "" when there
// are none.
func completionMenu(completions []filterlang.Completion, highlight, width int) string {
	if len(completions) == 0 || width < 8 {
		return ""
	}
	first := 0
	if highlight >= maxCompletionRows {
		first = highlight - maxCompletionRows + 1
	}
	shown := completions[first:min(len(completions), first+maxCompletionRows)]
	inner := 0
	for _, c := range shown {
		item, detail := completionLabel(c)
		w := lipgloss.Width(fitText(item, width, "…"))
		if detail != "" {
			w += 2 + lipgloss.Width(fitText(detail, width, "…"))
		}
		inner = max(inner, w)
	}
	inner = min(inner+2, width-2)
	lines := make([]string, len(shown))
	for i, c := range shown {
		item, detail := completionLabel(c)
		text := fitText(item, inner-2, "…")
		if detail != "" && lipgloss.Width(text)+4 < inner {
			text += "  " + helpStyle.Render(fitText(detail, inner-2-lipgloss.Width(text)-2, "…"))
		}
		line := " " + text + strings.Repeat(" ", max(0, inner-1-lipgloss.Width(text)))
		if first+i == highlight {
			line = completionHighlightStyle.Render(ansi.Strip(line))
		}
		lines[i] = line
	}
	return completionBorderStyle.Render(strings.Join(lines, "\n"))
}

// overlayRows draws menu over the first lines of rows, a block of lines width
// cells wide, keeping each line's cells to the right of the menu.
func overlayRows(rows, menu string, width int) string {
	if menu == "" {
		return rows
	}
	lines := strings.Split(rows, "\n")
	for i, m := range strings.Split(menu, "\n") {
		if i >= len(lines) {
			break
		}
		// The reset keeps the menu's colors off the row's cells after it.
		lines[i] = m + ansi.ResetStyle + ansi.Cut(lines[i], lipgloss.Width(m), width)
	}
	return strings.Join(lines, "\n")
}
