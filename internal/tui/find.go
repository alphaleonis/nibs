package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/filterlang"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/viewshape"
	"github.com/charmbracelet/x/ansi"
)

// The notes a find that passes the end of the rows shows.
const (
	findWrappedToFirst = "Wrapped to the first match"
	findWrappedToLast  = "Wrapped to the last match"
)

// newFindBox returns the text box find's text is typed in.
func newFindBox() textinput.Model {
	box := textinput.New()
	box.Prompt = "find: "
	return box
}

// findNeedle returns text as find compares it: its runes in lower case. It is
// nil when text is empty.
func findNeedle(text string) []rune {
	if text == "" {
		return nil
	}
	return []rune(foldCase(text))
}

// foldCase returns s with each rune in lower case. Each rune maps to one
// rune, so a rune index into the result is one into s.
func foldCase(s string) string {
	return strings.Map(unicode.ToLower, s)
}

// foundRunes reports, for each rune of text, whether it is part of a match for
// needle in any letter case.
func foundRunes(text string, needle []rune) []bool {
	runes := []rune(foldCase(text))
	found := make([]bool, len(runes))
	if len(needle) == 0 {
		return found
	}
	for i := 0; i+len(needle) <= len(runes); i++ {
		if slices.Equal(runes[i:i+len(needle)], needle) {
			for j := range needle {
				found[i+j] = true
			}
		}
	}
	return found
}

// findMatches reports whether n's title, ID or one of its tags holds needle,
// in any letter case, as the TUI draws them.
func findMatches(n *nib.Nib, needle []rune) bool {
	holds := func(s string) bool { return slices.Contains(foundRunes(safetext.StripLine(s), needle), true) }
	return holds(n.Title) || holds(n.ID) || slices.ContainsFunc(n.Tags, holds)
}

// markFound draws shown in base with each match for needle reversed. shown is
// full as fitText fits it, so it starts with full's runes and may end with the
// cut mark. A match the cut passes through is reversed up to the cut.
//
// It reverses whole grapheme clusters, widening a match that covers part of
// one, so no escape splits a cluster.
func markFound(shown, full string, needle []rune, base lipgloss.Style) string {
	found := foundRunes(full, needle)
	if !slices.Contains(found, true) {
		return base.Render(shown)
	}
	runes, fullRunes := []rune(shown), []rune(full)
	kept := 0
	for kept < len(runes) && kept < len(fullRunes) && runes[kept] == fullRunes[kept] {
		kept++
	}
	hit := base.Reverse(true)
	var b strings.Builder
	var segment strings.Builder
	segmentHit := false
	flush := func() {
		if segment.Len() == 0 {
			return
		}
		style := base
		if segmentHit {
			style = hit
		}
		b.WriteString(style.Render(segment.String()))
		segment.Reset()
	}
	rest := string(runes[:kept])
	for pos := 0; rest != ""; {
		cluster, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		n := utf8.RuneCountInString(cluster)
		clusterHit := slices.Contains(found[pos:pos+n], true)
		if clusterHit != segmentHit {
			flush()
			segmentHit = clusterHit
		}
		segment.WriteString(cluster)
		rest, pos = rest[len(cluster):], pos+n
	}
	flush()
	if kept < len(runes) {
		b.WriteString(base.Render(string(runes[kept:])))
	}
	return b.String()
}

// openFind opens the find box empty. The last text found stays in force
// until a key types into the box, so enter on the empty box keeps it.
func (m *listModel) openFind() {
	m.finding = true
	m.findBox.Reset()
	m.findBox.Focus()
}

// closeFind closes the find box, keeping the text for find.next when keep is
// set. The cursor stays on the row the last jump put it on.
func (m *listModel) closeFind(keep bool) {
	m.finding = false
	m.findBox.Blur()
	if !keep {
		m.find = ""
	}
}

// findAgain moves the cursor to the next row that matches the find text, or
// to the previous when back is set; see findFrom.
func (m *listModel) findAgain(back bool) {
	dir := 1
	if back {
		dir = -1
	}
	m.findFrom(m.focusedRowID(), dir, false)
}

// findTyped moves the cursor to the first row at or after it that matches the
// find text; see findFrom. An empty text leaves the cursor where it is.
func (m *listModel) findTyped() {
	m.find = m.findBox.Value()
	if m.find == "" {
		return
	}
	m.findFrom(m.focusedRowID(), 1, true)
}

// findFrom puts the cursor on the first nib row from the row of from, in the
// direction dir, that matches the find text, unfolding the rows it is nested
// under. The row of from counts only when inclusive is set. Find searches the
// rows the filter shows, dimmed context rows among them, so a row the filter
// hides is never found. Past the last row it continues from the first, and the
// reverse, with a note. A find with no match leaves the cursor and notes it.
func (m *listModel) findFrom(from string, dir int, inclusive bool) {
	needle := findNeedle(m.find)
	if needle == nil {
		return
	}
	rows := m.unfolded
	n := len(rows)
	start := slices.IndexFunc(rows, func(r viewshape.Row) bool { return rowIDOf(r) == from })
	switch {
	case start < 0 && dir > 0:
		start, inclusive = 0, true
	case start < 0:
		start, inclusive = n-1, true
	}
	// Step n reaches the row of from again, so a lone match is found from itself.
	for step := range n + 1 {
		if n == 0 || step == 0 && !inclusive {
			continue
		}
		i := start + step*dir
		wrapped := i < 0 || i >= n
		i = (i%n + n) % n
		r := rows[i]
		if r.NibID == "" || !findMatches(m.byID[r.NibID], needle) {
			continue
		}
		m.reveal(r.NibID)
		if wrapped {
			m.statusMessage, m.statusKind = findWrappedToFirst, statusOK
			if dir < 0 {
				m.statusMessage = findWrappedToLast
			}
		}
		return
	}
	m.statusMessage, m.statusKind = fmt.Sprintf("No nib in the list matches %q", m.find), statusWarn
}

// findBoxView draws the find box's text with its cursor in at most width cells.
func (m listModel) findBoxView(width int) string {
	text := m.findBox.Value()
	var spans []filterlang.Span
	if text != "" {
		spans = []filterlang.Span{{Start: 0, End: len(text)}}
	}
	return renderQuery(text, spans, m.findBox.Position(), width)
}
