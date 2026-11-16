package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// forceUTF8 makes terminalUTF8 report utf8 until the test ends.
func forceUTF8(t *testing.T, utf8 bool) {
	t.Helper()
	prev := terminalUTF8
	terminalUTF8 = func() bool { return utf8 }
	t.Cleanup(func() { terminalUTF8 = prev })
}

// vocabularyGlyphs returns, for each field, the values of cfg's vocabulary and
// the lookup that finds their glyphs in a set.
func vocabularyGlyphs(cfg *config.Config) []struct {
	field  string
	values []string
	glyph  func(iconSet, string) (string, bool)
} {
	return []struct {
		field  string
		values []string
		glyph  func(iconSet, string) (string, bool)
	}{
		{"status", cfg.StatusNames(), func(s iconSet, v string) (string, bool) { return s.lookup(statusGlyphs, v) }},
		{"type", cfg.TypeNames(), func(s iconSet, v string) (string, bool) { return s.lookup(typeGlyphs, v) }},
		{"priority", cfg.PriorityNames(), func(s iconSet, v string) (string, bool) { return s.lookup(priorityGlyphs, v) }},
	}
}

// Rows lay out one cell per glyph, so a glyph of another width shifts the row.
func TestEveryVocabularyValueHasAOneCellGlyphInEverySet(t *testing.T) {
	for _, set := range iconSets {
		for _, f := range vocabularyGlyphs(config.Default()) {
			if len(f.values) == 0 {
				t.Fatalf("premise failed: the %s vocabulary is empty", f.field)
			}
			for _, v := range f.values {
				g, ok := f.glyph(set, v)
				if !ok {
					t.Errorf("%s %s has no glyph in the %s set", f.field, v, set)
					continue
				}
				if w := lipgloss.Width(g); w != 1 {
					t.Errorf("%s %s in the %s set is %q, %d cells wide; want 1", f.field, v, set, g, w)
				}
			}
		}
	}
}

func TestNoTwoValuesOfAFieldShareAGlyph(t *testing.T) {
	for _, set := range iconSets {
		for _, f := range vocabularyGlyphs(config.Default()) {
			seen := map[string]string{}
			for _, v := range f.values {
				g, _ := f.glyph(set, v)
				if other, ok := seen[g]; ok {
					t.Errorf("%s %s and %s share %q in the %s set", f.field, other, v, g, set)
				}
				seen[g] = v
			}
		}
	}
}

func TestASCIIGlyphsArePrintableASCII(t *testing.T) {
	for _, table := range []map[string]glyph{statusGlyphs, typeGlyphs, priorityGlyphs} {
		for v, g := range table {
			for _, r := range g.ascii {
				if r > unicode.MaxASCII || !unicode.IsPrint(r) {
					t.Errorf("the ascii glyph of %s is %q, which holds %U", v, g.ascii, r)
				}
			}
		}
	}
	if g := iconsASCII.status("no-such-status"); g != "?" {
		t.Errorf("an unknown status is %q in the ascii set, want ?", g)
	}
}

func TestIconsAreDrawnInTheirConfiguredColors(t *testing.T) {
	cfg := config.Default()
	in := func(color, glyph string) string {
		return lipgloss.NewStyle().Foreground(ui.ResolveColor(color)).Render(glyph)
	}
	for _, set := range iconSets {
		for _, s := range cfg.StatusNames() {
			if got, want := set.renderStatus(cfg, s), in(cfg.GetStatus(s).Color, set.status(s)); got != want {
				t.Errorf("%s status %s renders %q, want %q", set, s, got, want)
			}
		}
		for _, ty := range cfg.TypeNames() {
			if got, want := set.renderType(cfg, ty), in(cfg.GetType(ty).Color, set.nibType(ty)); got != want {
				t.Errorf("%s type %s renders %q, want %q", set, ty, got, want)
			}
		}
		for _, p := range cfg.PriorityNames() {
			if got, want := set.renderPriority(cfg, p), in(cfg.GetPriority(p).Color, set.priority(p)); got != want {
				t.Errorf("%s priority %s renders %q, want %q", set, p, got, want)
			}
		}
	}
}

func TestShortIDStripsOnlyTheStoresOwnPrefix(t *testing.T) {
	tests := []struct{ id, prefix, want string }{
		{"tnib-ab12", "tnib-", "ab12"},
		{"other-ab12", "tnib-", "other-ab12"},
		{"ab12", "", "ab12"},
		{"tnib-", "tnib-", "tnib-"},
		{"xtnib-ab12", "tnib-", "xtnib-ab12"},
	}
	for _, tt := range tests {
		if got := shortID(tt.id, tt.prefix); got != tt.want {
			t.Errorf("shortID(%q, %q) = %q, want %q", tt.id, tt.prefix, got, tt.want)
		}
	}
}

func prefixedConfig(prefix string) *config.Config {
	cfg := config.Default()
	cfg.Nibs.Prefix = prefix
	return cfg
}

func TestTheDisplayDefaultsToUnicodeAndShortIDs(t *testing.T) {
	forceUTF8(t, true)
	app := setupTestAppWithUserConfig(t, listKeysNibs(), prefixedConfig("tnib-"), "nibs:\n  id_length: 4\n")
	if app.display.icons != iconsUnicode {
		t.Errorf("icons = %s, want unicode", app.display.icons)
	}
	if got := app.display.id("tnib-ab12"); got != "ab12" {
		t.Errorf("id = %q, want ab12", got)
	}
}

func TestTheIconsSettingChoosesTheSet(t *testing.T) {
	forceUTF8(t, true)
	for _, set := range iconSets {
		app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  icons: "+string(set)+"\n")
		if app.display.icons != set {
			t.Errorf("icons: %s gives the %s set", set, app.display.icons)
		}
		if len(app.ignored) != 0 {
			t.Errorf("icons: %s ignored %+v, want none", set, app.ignored)
		}
	}
}

func TestATerminalWithoutUTF8GetsASCIIWhateverTheSetting(t *testing.T) {
	forceUTF8(t, false)
	for _, text := range []string{"tui:\n  icons: nerd\n", "tui:\n  icons: unicode\n", "nibs:\n  id_length: 4\n"} {
		app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), text)
		if app.display.icons != iconsASCII {
			t.Errorf("%q: icons = %s without UTF-8, want ascii", text, app.display.icons)
		}
	}
}

func TestShortIDsFalseKeepsFullIDs(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), prefixedConfig("tnib-"), "tui:\n  short_ids: false\n")
	if got := app.display.id("tnib-ab12"); got != "tnib-ab12" {
		t.Errorf("id = %q with short_ids: false, want tnib-ab12", got)
	}
}

func TestAnUnusableDisplaySettingFallsBackAndWarns(t *testing.T) {
	forceUTF8(t, true)
	tests := []struct {
		name, text, entry, reason string
		line                      int
	}{
		{"unknown icon set", "tui:\n  icons: emoji\n", "icons: emoji", "is not unicode, nerd or ascii", 2},
		{"icon set in capitals", "tui:\n  icons: Nerd\n", "icons: Nerd", "is not unicode, nerd or ascii", 2},
		{"icon set as a list", "tui:\n  icons: [nerd]\n", "icons: [nerd]", "is not unicode, nerd or ascii", 2},
		{"short_ids not a bool", "tui:\n  keys:\n    nib.edit: E\n  short_ids: maybe\n", "short_ids: maybe", "is not true or false", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTestAppWithUserConfig(t, listKeysNibs(), prefixedConfig("tnib-"), tt.text)
			if app.display.icons != iconsUnicode || app.display.id("tnib-ab12") != "ab12" {
				t.Errorf("icons = %s, id = %q; want the defaults unicode and ab12", app.display.icons, app.display.id("tnib-ab12"))
			}
			if len(app.ignored) != 1 || app.ignored[0].entry != tt.entry || app.ignored[0].reason != tt.reason || app.ignored[0].line != tt.line {
				t.Fatalf("ignored %+v, want %q on line %d: %s", app.ignored, tt.entry, tt.line, tt.reason)
			}

			want := "Ignored 1 TUI setting in the user config; ? lists it"
			if footer := footerLine(app); !strings.Contains(footer, want) {
				t.Errorf("footer = %q, want %q", footer, want)
			}
			pressKeys(app, "?")
			view := stripAnsi(app.palette.View())
			for _, part := range []string{"Ignored TUI settings in /", tt.entry, tt.reason} {
				if !strings.Contains(view, part) {
					t.Errorf("the palette does not show %q:\n%s", part, view)
				}
			}
		})
	}
}

// A block that does not decode is one ignored entry, not one per setting.
func TestABrokenTUIBlockIsOneIgnoredEntry(t *testing.T) {
	forceUTF8(t, true)
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  icons: nerd\n  icons: ascii\n")
	if len(app.ignored) != 1 || app.ignored[0].line != 2 {
		t.Errorf("ignored %+v, want one entry on line 2", app.ignored)
	}
	if app.display.icons != iconsUnicode {
		t.Errorf("icons = %s, want the default unicode", app.display.icons)
	}
}

// asciiListNibs is a tree two levels deep with a collapsed parent, every
// priority, a long title and enough rows for more than one page.
func asciiListNibs() []*nib.Nib {
	nibs := []*nib.Nib{
		{ID: "nib-1", Title: strings.Repeat("A long title ", 20), Type: "epic", Status: "todo", Order: "01", Priority: "critical", Tags: []string{"alpha", "a-rather-long-tag-name", "gamma"}},
		{ID: "nib-2", Title: "Child", Type: "task", Status: "in-progress", Parent: "nib-1", Order: "02", Priority: "low"},
		{ID: "nib-3", Title: "Grandchild", Type: "task", Status: "todo", Parent: "nib-2", Order: "03", Priority: "high"},
		{ID: "nib-4", Title: "Second child", Type: "bug", Status: "draft", Parent: "nib-1", Order: "04"},
		{ID: "nib-5", Title: "Collapsed parent", Type: "feature", Status: "todo", Order: "05"},
		{ID: "nib-6", Title: "Hidden child", Type: "task", Status: "todo", Parent: "nib-5", Order: "06"},
	}
	for i := 7; i < 60; i++ {
		nibs = append(nibs, &nib.Nib{ID: fmt.Sprintf("nib-%d", i), Title: "Filler", Type: "task", Status: "todo", Order: fmt.Sprintf("%02d", i)})
	}
	return nibs
}

// asciiApp is an App on asciiListNibs with UTF-8 forced off, one ignored
// setting and an update available, sized 100x20.
func asciiApp(t *testing.T) *App {
	t.Helper()
	forceUTF8(t, false)
	nibs := asciiListNibs()
	stub := &StubBackend{
		Nibs:     map[string]*nib.Nib{},
		AllNibs:  nibs,
		Blocked:  map[string]bool{"nib-2": true},
		Blocking: map[string]bool{"nib-4": true},
	}
	for _, n := range nibs {
		stub.Nibs[n.ID] = n
	}
	user, path := writeUserConfig(t, "tui:\n  icons: nerd\n  short_ids: maybe\n")
	app := New(stub, config.Default(), config.UserLayer{Config: user, Path: path}, "dev")
	app.list.collapsed()["nib-5"] = true
	app.list.updateAvailable, app.list.updateLatest = true, "v9.9.9"
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	processCmd(app, initCmd)
	return app
}

// nonASCII returns the first rune of s that is not ASCII, or false when s has
// none.
func nonASCII(s string) (rune, bool) {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return r, true
		}
	}
	return 0, false
}

func TestATerminalWithoutUTF8GetsAnASCIIListView(t *testing.T) {
	app := asciiApp(t)

	// The status line warns until a key is pressed. Folding nib-2 then redraws
	// the tree, and space marks a row.
	views := map[string]string{}
	for _, width := range []int{60, 100, 160} {
		app.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		views[fmt.Sprintf("warning at %d columns", width)] = stripAnsi(app.View().Content)
	}
	if !focusOn(app, "nib-2") {
		t.Fatal("premise failed: could not focus nib-2")
	}
	pressKeys(app, "left space")
	if !app.list.collapsed()["nib-2"] {
		t.Fatal("premise failed: left did not fold nib-2")
	}
	for _, width := range []int{60, 100, 160} {
		app.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		views[fmt.Sprintf("selection at %d columns", width)] = stripAnsi(app.View().Content)
	}
	for _, part := range []struct{ view, text string }{
		{"warning at 160 columns", "Ignored 1 TUI setting"},
		{"warning at 160 columns", "v9.9.9 available"},
		{"selection at 100 columns", "(1 selected)"},
		{"selection at 100 columns", "+- > Child"},
	} {
		if !strings.Contains(views[part.view], part.text) {
			t.Fatalf("premise failed: the %s lacks %q:\n%s", part.view, part.text, views[part.view])
		}
	}

	for name, view := range views {
		for i, line := range strings.Split(view, "\n") {
			if r, ok := nonASCII(line); ok {
				t.Errorf("the %s: line %d holds %q (%U):\n%s", name, i+1, r, r, line)
			}
		}
	}
}

// The nib view opens with no resize after it, so its link rows come from the
// delegate it was built with.
func TestATerminalWithoutUTF8GetsASCIILinkRows(t *testing.T) {
	app := asciiApp(t)
	if !focusOn(app, "nib-2") {
		t.Fatal("premise failed: could not focus nib-2")
	}
	pressKeys(app, "enter")
	if app.state != viewDetail || len(app.detail.links) < 2 {
		t.Fatalf("premise failed: state = %d with %d links after enter, want the nib view with the parent and the child", app.state, len(app.detail.links))
	}
	rows := stripAnsi(app.detail.linkList.View())
	if !strings.Contains(rows, "nib-1") || !strings.Contains(rows, "nib-3") {
		t.Fatalf("premise failed: the link rows lack nib-1 or nib-3:\n%s", rows)
	}
	for i, line := range strings.Split(rows, "\n") {
		if r, ok := nonASCII(line); ok {
			t.Errorf("link row %d holds %q (%U):\n%s", i+1, r, r, line)
		}
	}
}

// A block-style value starts on the line after its key, and the palette names
// the key's line, as it does for a key binding.
func TestAnIgnoredDisplaySettingNamesTheLineOfItsKey(t *testing.T) {
	s := loadUserSettings(t, "tui:\n  icons:\n    - nerd\n  short_ids:\n    x: y\n  keys:\n    [P]\n")
	want := map[string]int{"icons": 2, "short_ids": 4, "tui.keys": 6}
	if len(s.ignored) != len(want) {
		t.Fatalf("ignored %+v, want %d entries", s.ignored, len(want))
	}
	for _, e := range s.ignored {
		name, _, _ := strings.Cut(e.entry, ":")
		if e.line != want[name] {
			t.Errorf("%s is ignored on line %d, want %d", name, e.line, want[name])
		}
	}
}
