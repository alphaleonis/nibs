package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// iconSet is a value of the user config's tui.icons: the glyphs that rows draw
// statuses, types and priorities with. Every glyph is one cell wide.
type iconSet string

const (
	iconsUnicode iconSet = "unicode"
	iconsNerd    iconSet = "nerd"
	iconsASCII   iconSet = "ascii"
)

// iconSets lists every icon set, the default first.
var iconSets = []iconSet{iconsUnicode, iconsNerd, iconsASCII}

// unknownGlyph stands for a value outside the vocabulary, in every set.
const unknownGlyph = "?"

// glyph is one value's glyph in each icon set.
type glyph struct{ unicode, nerd, ascii string }

// The glyph tables: one row for each status, type and priority. A nerd glyph
// is the Nerd Fonts glyph nearest the Lucide icon that web/src/lib/icons.ts
// draws for the value. Each row's comment names that Lucide icon, then the
// glyph's name in the Nerd Fonts cheat sheet. The codepoints are the ones
// Nerd Fonts 3.4.0 lists for those names in
// https://github.com/ryanoasis/nerd-fonts/blob/v3.4.0/glyphnames.json.
var (
	statusGlyphs = map[string]glyph{
		"draft":       {"◌", "\uf4e7", "."},     // CircleDashed: nf-oct-issue_draft
		"todo":        {"○", "\U000f0130", "o"}, // Circle: nf-md-checkbox_blank_circle_outline
		"in-progress": {"◐", "\U000f040d", ">"}, // CirclePlay: nf-md-play_circle_outline
		"deferred":    {"⊖", "\U000f03e6", "="}, // CirclePause: nf-md-pause_circle_outline
		"completed":   {"✓", "\U000f05e1", "x"}, // CircleCheck: nf-md-check_circle_outline
		"scrapped":    {"✗", "\U000f015a", "-"}, // CircleX: nf-md-close_circle_outline
	}
	typeGlyphs = map[string]glyph{
		"milestone": {"⚑", "\U000f023d", "M"}, // Flag: nf-md-flag_outline
		"epic":      {"≡", "\U000f09fe", "E"}, // Layers: nf-md-layers_outline
		"bug":       {"✱", "\U000f0a30", "B"}, // Bug: nf-md-bug_outline
		"feature":   {"✦", "\U000f0674", "F"}, // Sparkles: nf-md-creation
		"task":      {"▣", "\U000f0135", "T"}, // SquareCheck: nf-md-checkbox_marked_outline
		"research":  {"△", "\U000f0096", "R"}, // FlaskConical: nf-md-flask_outline
	}
	// The web draws priorities as the text in web/src/lib/badges.ts, not as
	// Lucide icons, so the nerd set uses that text too. Normal priority has no
	// mark there, so its glyph is a blank cell.
	priorityGlyphs = map[string]glyph{
		"critical": {"‼", "‼", "!"},
		"high":     {"!", "!", "^"},
		"normal":   {" ", " ", " "},
		"low":      {"↓", "↓", "v"},
	}
)

// ascii reports whether s is the ASCII set, which also draws the list's tree,
// cursor, markers and rules, and the nib view, in ASCII.
func (s iconSet) ascii() bool { return s == iconsASCII }

// lookup returns value's glyph from table in s, or false when table has no row
// for value.
func (s iconSet) lookup(table map[string]glyph, value string) (string, bool) {
	g, ok := table[value]
	if !ok {
		return "", false
	}
	switch s {
	case iconsNerd:
		return g.nerd, true
	case iconsASCII:
		return g.ascii, true
	}
	return g.unicode, true
}

// glyphOf returns value's glyph from table in s, or unknownGlyph for a value
// table has no row for.
func (s iconSet) glyphOf(table map[string]glyph, value string) string {
	if g, ok := s.lookup(table, value); ok {
		return g
	}
	return unknownGlyph
}

// status returns the glyph of the status name.
func (s iconSet) status(name string) string { return s.glyphOf(statusGlyphs, name) }

// nibType returns the glyph of the type name.
func (s iconSet) nibType(name string) string { return s.glyphOf(typeGlyphs, name) }

// priority returns the glyph of the priority name.
func (s iconSet) priority(name string) string { return s.glyphOf(priorityGlyphs, name) }

// renderStatus returns the glyph of the status name in the status's color in
// cfg.
func (s iconSet) renderStatus(cfg *config.Config, name string) string {
	var c string
	if st := cfg.GetStatus(name); st != nil {
		c = st.Color
	}
	return inColor(c, s.status(name))
}

// renderType returns the glyph of the type name in the type's color in cfg.
func (s iconSet) renderType(cfg *config.Config, name string) string {
	var c string
	if t := cfg.GetType(name); t != nil {
		c = t.Color
	}
	return inColor(c, s.nibType(name))
}

// renderPriority returns the glyph of the priority name in the priority's
// color in cfg.
func (s iconSet) renderPriority(cfg *config.Config, name string) string {
	var c string
	if p := cfg.GetPriority(name); p != nil {
		c = p.Color
	}
	return inColor(c, s.priority(name))
}

// inColor returns glyph in the color named color. ui.ResolveColor makes a name
// it does not know, "" included, the muted color.
func inColor(color, glyph string) string {
	return lipgloss.NewStyle().Foreground(ui.ResolveColor(color)).Render(glyph)
}

// shortID returns id without prefix, the store's own id prefix. An id that does
// not start with prefix, or that is prefix alone, is returned whole.
func shortID(id, prefix string) string {
	if rest, ok := strings.CutPrefix(id, prefix); ok && rest != "" {
		return rest
	}
	return id
}
