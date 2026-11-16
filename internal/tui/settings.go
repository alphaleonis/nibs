package tui

import (
	"cmp"
	"maps"
	"slices"

	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/ui"
	"gopkg.in/yaml.v3"
)

// terminalUTF8 reports whether the terminal displays UTF-8. Tests replace it.
var terminalUTF8 = ui.TerminalUTF8

// settings is the user config's tui block as the TUI applies it.
type settings struct {
	keys    keymap
	display display
	// columns are the columns each view shows after the title, by view name.
	columns map[string][]columnName
	// ignored lists the entries of the block that the TUI ignored, by line.
	ignored []ignoredEntry
	// source is the path of the file the block came from, or "" for none.
	source string
}

// ignoredEntry is an entry of the user config's tui block that the TUI
// ignored, so its setting kept its default.
type ignoredEntry struct {
	entry  string // the entry as written, "name: value"
	line   int    // the entry's line in the file, from 1
	reason string
}

// display is how rows show a nib.
type display struct {
	icons    iconSet // the set rows draw with
	shortIDs bool    // whether rows drop the store's own prefix from IDs
	prefix   string  // the store's own ID prefix
}

// ascii reports whether the TUI draws in ASCII, as iconSet.ascii says. The
// palette, modals and other boxes stay UTF-8.
func (d display) ascii() bool {
	return d.icons.ascii()
}

// id returns id as rows show it: without the store's own prefix while short
// IDs are on, and whole otherwise, stripped by displayID.
func (d display) id(id string) string {
	if !d.shortIDs {
		return displayID(id)
	}
	return displayID(shortID(id, d.prefix))
}

// tuiBlock is the user config's tui block. Each setting stays a node, so a
// value that cannot be used costs only its own setting.
type tuiBlock struct {
	Keys     yaml.Node `yaml:"keys"`
	Icons    yaml.Node `yaml:"icons"`
	ShortIDs yaml.Node `yaml:"short_ids"`
	Columns  yaml.Node `yaml:"columns"`

	keyLines map[string]int // the line of each key written in the block itself
}

// line returns the line of the setting name's key, or of value when the key
// is not written in the block itself, as with one a merge key brings in.
func (b *tuiBlock) line(name string, value *yaml.Node) int {
	if l, ok := b.keyLines[name]; ok {
		return l
	}
	return value.Line
}

// loadSettings applies tui, the user config's tui block, over the defaults:
// the keys of defaults, which must have no key conflicts, unicode icons, short
// IDs and defaultColumns. A setting whose value cannot be used is ignored and
// keeps its default. A block that is not a mapping or does not decode is one ignored
// entry, and every setting keeps its default. A nil or empty block gives the
// defaults. source is the file tui came from.
//
// The display it returns has no store prefix, and its icons are the configured
// set whatever the terminal can show.
func loadSettings(defaults []action, tui *yaml.Node, source string) settings {
	s := settings{
		keys:    keymap{actions: defaults},
		display: display{icons: iconsUnicode, shortIDs: true},
		columns: maps.Clone(defaultColumns),
		source:  source,
	}
	block, bad := decodeTUIBlock(tui)
	if bad != nil {
		s.ignored = []ignoredEntry{*bad}
		return s
	}
	if block == nil {
		return s
	}
	keys, ignoredKeys := buildKeymap(defaults, &block.Keys, block.line("keys", &block.Keys))
	s.keys = keys
	s.ignored = slices.Concat(ignoredKeys, s.applyDisplay(block), s.applyColumns(block))
	slices.SortStableFunc(s.ignored, func(a, b ignoredEntry) int { return cmp.Compare(a.line, b.line) })
	return s
}

// applyDisplay sets the display settings of block on s.display, and returns
// the entries it ignored.
func (s *settings) applyDisplay(block *tuiBlock) []ignoredEntry {
	var ignored []ignoredEntry
	ignore := func(name string, n *yaml.Node, reason string) {
		ignored = append(ignored, ignoredEntry{entry: safetext.StripBounded(name + ": " + nodeText(resolveAlias(n))), line: block.line(name, n), reason: reason})
	}

	if n := resolveAlias(&block.Icons); !absentNode(n) {
		if set := iconSet(n.Value); n.Kind == yaml.ScalarNode && slices.Contains(iconSets, set) {
			s.display.icons = set
		} else {
			ignore("icons", &block.Icons, "is not unicode, nerd or ascii")
		}
	}

	if n := resolveAlias(&block.ShortIDs); !absentNode(n) {
		var on bool
		if n.Kind == yaml.ScalarNode && n.Tag == "!!bool" && n.Decode(&on) == nil {
			s.display.shortIDs = on
		} else {
			ignore("short_ids", &block.ShortIDs, "is not true or false")
		}
	}
	return ignored
}

// applyColumns sets the views' columns that block names on s.columns, and
// returns the entries it ignored. A view's list of columns applies without
// the names in it that name no column. The title is shown whether or not a
// list names it.
func (s *settings) applyColumns(block *tuiBlock) []ignoredEntry {
	columns := resolveAlias(&block.Columns)
	if absentNode(columns) {
		return nil
	}
	if columns.Kind != yaml.MappingNode {
		return []ignoredEntry{{
			entry:  safetext.StripBounded("tui.columns: " + nodeText(columns)),
			line:   block.line("columns", &block.Columns),
			reason: "is not a map from views to lists of columns",
		}}
	}
	var ignored []ignoredEntry
	ignore := func(view string, value, at *yaml.Node, reason string) {
		ignored = append(ignored, ignoredEntry{entry: safetext.StripBounded("columns." + view + ": " + nodeText(value)), line: at.Line, reason: reason})
	}
	named := map[string]bool{}
	for i := 0; i+1 < len(columns.Content); i += 2 {
		key, value := columns.Content[i], resolveAlias(columns.Content[i+1])
		view := key.Value
		switch {
		case !slices.Contains(columnViews, view):
			ignore(view, value, key, "no view has this name")
			continue
		case named[view]:
			ignore(view, value, key, "names a view an earlier entry names")
			continue
		case value.Kind != yaml.SequenceNode:
			ignore(view, value, key, "is not a list of columns")
			continue
		}
		named[view] = true
		shown := []columnName{}
		for _, item := range value.Content {
			item = resolveAlias(item)
			name := columnName(item.Value)
			if item.Kind != yaml.ScalarNode || !slices.Contains(columnOrder, name) {
				ignore(view, item, item, "no column has this name")
				continue
			}
			if name != colTitle && !slices.Contains(shown, name) {
				shown = append(shown, name)
			}
		}
		s.columns[view] = shown
	}
	return ignored
}

// decodeTUIBlock returns the tui block decoded, or nil when there is none. It
// also returns nil, and the ignored entry, when the block is not a mapping or
// does not decode. yaml.v3 decodes the block, so merge keys and aliases apply
// and a repeated setting is an error.
func decodeTUIBlock(tui *yaml.Node) (*tuiBlock, *ignoredEntry) {
	if absentNode(tui) {
		return nil, nil
	}
	resolved := resolveAlias(tui)
	if resolved.Kind != yaml.MappingNode {
		return nil, &ignoredEntry{entry: safetext.StripBounded("tui: " + nodeText(resolved)), line: tui.Line, reason: "is not a map of TUI settings"}
	}
	var block tuiBlock
	if err := tui.Decode(&block); err != nil {
		return nil, &ignoredEntry{entry: "tui", line: tui.Line, reason: safetext.StripBounded(decodeError(err))}
	}
	block.keyLines = map[string]int{}
	for i := 0; i+1 < len(resolved.Content); i += 2 {
		block.keyLines[resolved.Content[i].Value] = resolved.Content[i].Line
	}
	return &block, nil
}

// resolveAlias returns the node an alias names, or n itself when it is not an
// alias.
func resolveAlias(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode {
		return n.Alias
	}
	return n
}

// absentNode reports whether n holds no value: it is nil, empty, or null.
func absentNode(n *yaml.Node) bool {
	return n == nil || n.Kind == 0 || n.Tag == "!!null"
}
