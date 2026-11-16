package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
	uv "github.com/charmbracelet/ultraviolet"
)

// writeUserConfig writes text as a user config in a temp directory and loads
// it the way the tui command does. It returns the config and its path.
func writeUserConfig(t *testing.T, text string) (*config.UserConfig, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nibs.yml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	user, err := config.LoadUserConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadUserConfigFrom: %v", err)
	}
	return user, path
}

func loadUserSettings(t *testing.T, text string) settings {
	t.Helper()
	user, path := writeUserConfig(t, text)
	return loadSettings(defaultActions, &user.TUI, path)
}

func keysOf(km keymap, name actionName) []string {
	return findAction(km.actions, name).Keys
}

// ignoredNamed returns the ignored entry whose text starts with name.
func ignoredNamed(t *testing.T, km settings, name string) ignoredEntry {
	t.Helper()
	for _, b := range km.ignored {
		if strings.HasPrefix(b.entry, name+":") {
			return b
		}
	}
	t.Fatalf("no ignored entry for %s; ignored: %+v", name, km.ignored)
	return ignoredEntry{}
}

// decodeKey returns how Bubbletea's input decoder writes the key a terminal
// sends as the bytes in.
func decodeKey(t *testing.T, in string) string {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(in))
	k, ok := ev.(uv.KeyPressEvent)
	if !ok || n != len(in) {
		t.Fatalf("%q decodes as %T after %d bytes, want one key press", in, ev, n)
	}
	return k.String()
}

// The bytes are what a legacy terminal sends, or the kitty keyboard protocol's
// CSI u form where a key has no legacy one.
func TestKeysCheckKeyAcceptsArriveAsWritten(t *testing.T) {
	tests := []struct{ key, bytes string }{
		{"P", "P"},
		{"+", "+"},
		{"ctrl+p", "\x10"},
		{"ctrl++", "\x1b[43;5u"},
		{"alt+x", "\x1bx"},
		{"ctrl+alt+x", "\x1b\x18"},
		{"alt+shift+a", "\x1bA"},
		{"ctrl+shift+a", "\x1b[97;6u"},
		{"alt+Ä", "\x1bÄ"},
		{"shift+tab", "\x1b[Z"},
		{"ctrl+shift+up", "\x1b[1;6A"},
		{"shift+f3", "\x1b[13;2~"},
	}
	for _, tt := range tests {
		if why := checkKey(tt.key); why != "" {
			t.Errorf("checkKey(%q) refused it: %s", tt.key, why)
		}
		if got := decodeKey(t, tt.bytes); got != tt.key {
			t.Errorf("premise: %q arrives as %q, want %q", tt.bytes, got, tt.key)
		}
	}
}

// Every name namedKeys accepts arrives as written from at least these bytes.
func TestKeysNamedKeysArriveAsWritten(t *testing.T) {
	sent := map[string]string{
		"enter": "\r", "tab": "\t", "backspace": "\x7f", "esc": "\x1b", "space": " ",
		"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D", "begin": "\x1b[E",
		"home": "\x1b[H", "end": "\x1b[F", "insert": "\x1b[2~", "delete": "\x1b[3~", "pgup": "\x1b[5~", "pgdown": "\x1b[6~",
		"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS",
		"f5": "\x1b[15~", "f6": "\x1b[17~", "f7": "\x1b[18~", "f8": "\x1b[19~", "f9": "\x1b[20~", "f10": "\x1b[21~",
		"f11": "\x1b[23~", "f12": "\x1b[24~",
		"printscreen": "\x1b[57361u", "pause": "\x1b[57362u", "menu": "\x1b[57363u",
	}
	// Kitty numbers F13 to F35 from 57376.
	for n := 13; n <= 35; n++ {
		sent[fmt.Sprintf("f%d", n)] = fmt.Sprintf("\x1b[%du", 57363+n)
	}
	for name := range namedKeys {
		bytes, ok := sent[name]
		if !ok {
			t.Errorf("namedKeys accepts %q, which no bytes here are shown to send", name)
			continue
		}
		if got := decodeKey(t, bytes); got != name {
			t.Errorf("%q arrives as %q, want %q", bytes, got, name)
		}
	}
	for name := range sent {
		if !namedKeys[name] {
			t.Errorf("namedKeys refuses %q, which arrives as written", name)
		}
	}
}

func TestKeysCheckKeyRefusesKeysThatNeverArrive(t *testing.T) {
	tests := []struct{ key, why string }{
		{"ctrl+", "no key follows"},
		{"Enter", `lowercase: "enter"`},
		{"F3", `lowercase: "f3"`},
		{"pagedown", "not a key name"},
		{"shift+ctrl+a", "order"},
		{"ctrl+ctrl+a", "order"},
		{"cmd+a", `"cmd" is not a modifier`},
		{"shift+a", "shift types"},
		{"\x01", "not a printable"},
		{"ctrl+A", `write "ctrl+shift+a"`},
		{"alt+A", `write "alt+shift+a"`},
		{"ctrl+alt+E", `write "ctrl+alt+shift+e"`},
		{"ctrl+[", `a legacy terminal sends it as "esc"`},
		{"ctrl+m", `a legacy terminal sends it as "enter"`},
		{"ctrl+i", `a legacy terminal sends it as "tab"`},
		{"capslock", "not a key name"},
		{"leftshift", "not a key name"},
		{"plus", "not a key name"},
	}
	for _, tt := range tests {
		if got := checkKey(tt.key); !strings.Contains(got, tt.why) {
			t.Errorf("checkKey(%q) = %q, want it to say %q", tt.key, got, tt.why)
		}
	}
}

func TestKeysWithoutTUIKeysAreTheDefaults(t *testing.T) {
	km := loadUserSettings(t, "nibs:\n  id_length: 6\n")
	if len(km.ignored) != 0 || keysOf(km.keys, actStatus)[0] != "s" {
		t.Errorf("ignored %v, nib.status keys %v; want none and the default", km.ignored, keysOf(km.keys, actStatus))
	}
}

func TestKeysBindingReplacesTheDefaults(t *testing.T) {
	km := loadUserSettings(t, `tui:
  keys:
    nib.priority: P
    nib.status: [S, "ctrl+s", "x  y"]
    nib.type: s
`)
	if len(km.ignored) != 0 {
		t.Fatalf("ignored %+v, want none", km.ignored)
	}
	if got := keysOf(km.keys, actPriority); !slices.Equal(got, []string{"P"}) {
		t.Errorf("nib.priority keys = %v, want [P]", got)
	}
	if got := keysOf(km.keys, actStatus); !slices.Equal(got, []string{"S", "ctrl+s", "x y"}) {
		t.Errorf("nib.status keys = %v, want [S ctrl+s x y]", got)
	}
	if got := keysOf(km.keys, actType); !slices.Equal(got, []string{"s"}) {
		t.Errorf("nib.type keys = %v, want [s], the key nib.status gave up", got)
	}
	if got := keysOfTable(defaultActions, actStatus); !slices.Equal(got, []string{"s"}) {
		t.Errorf("the default table changed: nib.status keys = %v", got)
	}
}

func keysOfTable(table []action, name actionName) []string { return findAction(table, name).Keys }

func TestKeysUnknownActionIsIgnoredWithItsLine(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    nib.priority: P\n    nib.prority: Q\n")
	b := ignoredNamed(t, km, "nib.prority")
	if b.line != 4 || !strings.Contains(b.reason, "no action") {
		t.Errorf("ignored %+v, want line 4 and no action of that name", b)
	}
	if got := keysOf(km.keys, actPriority); !slices.Equal(got, []string{"P"}) {
		t.Errorf("the good entry beside it was lost: nib.priority keys = %v", got)
	}
}

func TestKeysMalformedEntriesFallBack(t *testing.T) {
	tests := []struct{ value, why string }{
		{`"ctrl+"`, "no key follows"},
		{"[e, Enter]", "lowercase"},
		{"[e, e]", "twice"},
		{`""`, "empty key"},
		{"[]", "names no key"},
		{"", "names no key"},
		{"{a: b}", "not a key"},
		{"[[e]]", "not a key"},
	}
	for _, tt := range tests {
		km := loadUserSettings(t, "tui:\n  keys:\n    nib.edit: "+tt.value+"\n")
		b := ignoredNamed(t, km, "nib.edit")
		if !strings.Contains(b.reason, tt.why) || b.line != 3 {
			t.Errorf("nib.edit: %s ignored as %+v, want line 3 and %q", tt.value, b, tt.why)
		}
		if got := keysOf(km.keys, actEdit); !slices.Equal(got, []string{"e"}) {
			t.Errorf("nib.edit: %s left keys %v, want the default [e]", tt.value, got)
		}
	}
}

func TestKeysCollisionBetweenTwoEntriesIgnoresBoth(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    nib.status: x\n    nib.type: x\n")
	if len(km.ignored) != 2 {
		t.Fatalf("ignored %+v, want both entries", km.ignored)
	}
	if b := ignoredNamed(t, km, "nib.type"); !strings.Contains(b.reason, `"x" is also bound to nib.status in the list`) {
		t.Errorf("nib.type ignored as %q", b.reason)
	}
	if !slices.Equal(keysOf(km.keys, actStatus), []string{"s"}) || !slices.Equal(keysOf(km.keys, actType), []string{"t"}) {
		t.Errorf("keys = %v and %v, want the defaults s and t", keysOf(km.keys, actStatus), keysOf(km.keys, actType))
	}
}

func TestKeysCollisionWithAnotherActionsDefaultIsIgnored(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    nib.status: t\n")
	b := ignoredNamed(t, km, "nib.status")
	if !strings.Contains(b.reason, "nib.type") {
		t.Errorf("nib.status ignored as %q, want it to name nib.type", b.reason)
	}
	if !slices.Equal(keysOf(km.keys, actStatus), []string{"s"}) {
		t.Errorf("nib.status keys = %v, want the default", keysOf(km.keys, actStatus))
	}
}

func TestKeysShadowedSequencesAreIgnored(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    nib.priority: g\n    nib.estimate: y x\n")
	if b := ignoredNamed(t, km, "nib.priority"); !strings.Contains(b.reason, `"g" starts move.top's "g g"`) {
		t.Errorf("nib.priority ignored as %q", b.reason)
	}
	if b := ignoredNamed(t, km, "nib.estimate"); !strings.Contains(b.reason, `nib.copy_id's "y" starts "y x"`) {
		t.Errorf("nib.estimate ignored as %q", b.reason)
	}
}

// confirm.yes is active only in the confirm dialog, and move.down never is.
func TestKeysSameKeyOnActionsThatShareNoContextIsAccepted(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    confirm.yes: [y, j]\n")
	if len(km.ignored) != 0 || !slices.Equal(keysOf(km.keys, actConfirmYes), []string{"y", "j"}) {
		t.Errorf("ignored %+v, confirm.yes keys %v; want none and [y j]", km.ignored, keysOf(km.keys, actConfirmYes))
	}
}

// Ignoring nib.type gives t back to it, which then collides with nib.status.
func TestKeysAFallbackCanCollideWithALaterEntry(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    nib.type: n\n    nib.status: t\n")
	if len(km.ignored) != 2 {
		t.Fatalf("ignored %+v, want both entries", km.ignored)
	}
	if !slices.Equal(keysOf(km.keys, actStatus), []string{"s"}) || !slices.Equal(keysOf(km.keys, actType), []string{"t"}) {
		t.Errorf("keys = %v and %v, want the defaults", keysOf(km.keys, actStatus), keysOf(km.keys, actType))
	}
	if errs := keyConflicts(km.keys.actions); len(errs) > 0 {
		t.Errorf("the keymap has conflicts:\n%s", conflictLines(errs))
	}
}

func TestKeysATextBoxKeepsTheKeysThatType(t *testing.T) {
	km := loadUserSettings(t, "tui:\n  keys:\n    input.accept: x\n    palette.run: [enter, space]\n    input.cancel: ctrl+g\n")
	if b := ignoredNamed(t, km, "input.accept"); !strings.Contains(b.reason, `"x" types text in the filter input`) {
		t.Errorf("input.accept ignored as %q", b.reason)
	}
	ignoredNamed(t, km, "palette.run")
	if !slices.Equal(keysOf(km.keys, actInputCancel), []string{"ctrl+g"}) {
		t.Errorf("input.cancel keys = %v, want [ctrl+g]", keysOf(km.keys, actInputCancel))
	}
}

func TestKeysATextBoxKeepsTheKeysThatEdit(t *testing.T) {
	for _, k := range []string{"backspace", "delete", "left", "right", "home", "end", "ctrl+a", "ctrl+e", "ctrl+w", "ctrl+u", "ctrl+k", "alt+backspace", "ctrl+v"} {
		t.Run(k, func(t *testing.T) {
			km := loadUserSettings(t, "tui:\n  keys:\n    input.cancel: "+k+"\n")
			if b := ignoredNamed(t, km, "input.cancel"); !strings.Contains(b.reason, fmt.Sprintf("%q edits text in the filter input", k)) {
				t.Errorf("input.cancel ignored as %q", b.reason)
			}
		})
	}
	// tab and up/down are the filter box's completion keys, not the text field's.
	km := loadUserSettings(t, "tui:\n  keys:\n    input.cancel: [ctrl+g, f2]\n")
	if len(km.ignored) != 0 || !slices.Equal(keysOf(km.keys, actInputCancel), []string{"ctrl+g", "f2"}) {
		t.Errorf("ignored %+v, input.cancel keys %v; want none and [ctrl+g f2]", km.ignored, keysOf(km.keys, actInputCancel))
	}
}

// A tui block or keys of the wrong shape is one ignored entry, warned about
// like any other.
func TestKeysOfTheWrongShapeAreOneIgnoredEntry(t *testing.T) {
	tests := []struct {
		tui  string
		line int
	}{
		{"tui: keys", 3},
		{"tui: [1]", 3},
		{"tui: {keys: 3}", 3},
		{"tui:\n  keys: [P]", 4},
	}
	for _, tt := range tests {
		tui := tt.tui
		app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "nibs:\n  id_length: 4\n"+tui+"\n")
		if len(app.ignored) != 1 || app.ignored[0].line != tt.line {
			t.Errorf("%q: ignored %+v, want one entry on line %d", tui, app.ignored, tt.line)
		}
		if footer := footerLine(app); !strings.Contains(footer, "Ignored 1 TUI setting") {
			t.Errorf("%q: footer = %q, want the warning", tui, footer)
		}
	}
}

func TestKeysAnEmptyTUIBlockIsTheDefaults(t *testing.T) {
	for _, tui := range []string{"tui:", "tui: {}", "tui:\n  keys:", "tui:\n  icons: nerd"} {
		if km := loadUserSettings(t, tui+"\n"); len(km.ignored) != 0 {
			t.Errorf("%q: ignored %+v, want none", tui, km.ignored)
		}
	}
}

// setupTestAppWithUserConfig is setupTestApp with text as the user config.
func setupTestAppWithUserConfig(t *testing.T, nibs []*nib.Nib, cfg *config.Config, text string) *App {
	t.Helper()
	user, path := writeUserConfig(t, text)
	return setupTestAppWithUserLayer(t, nibs, cfg, config.UserLayer{Config: user, Path: path})
}

// setupTestAppWithUserLayer is setupTestApp with user as the user config.
func setupTestAppWithUserLayer(t *testing.T, nibs []*nib.Nib, cfg *config.Config, user config.UserLayer) *App {
	t.Helper()
	stub := &StubBackend{Nibs: map[string]*nib.Nib{}, AllNibs: nibs}
	for _, n := range nibs {
		stub.Nibs[n.ID] = n
	}
	app := New(stub, cfg, user, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	processCmd(app, initCmd)
	return app
}

func TestABindingMakesAKeylessActionReachable(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  keys:\n    nib.priority: P\n")
	if !focusOn(app, "nib-4") {
		t.Fatal("premise failed: could not focus nib-4")
	}

	pressKeys(app, "P")
	if app.state != viewPriorityPicker || !slices.Equal(app.priorityPicker.nibIDs, []string{"nib-4"}) {
		t.Fatalf("state = %d editing %v after P, want the priority picker on nib-4", app.state, app.priorityPicker.nibIDs)
	}

	pressKeys(app, "esc m p")
	if app.state != viewPriorityPicker {
		t.Errorf("state = %d after m p, want the priority picker", app.state)
	}
}

// Two Apps built side by side keep their own keymaps, and a tui block in the
// project config binds nothing.
func TestTheProjectConfigBindsNoKeys(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	project := "nibs:\n  prefix: nib\ntui:\n  keys:\n    nib.priority: P\n"
	if err := os.WriteFile(filepath.Join(storeDir, store.ConfigFileName), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFromStore(storeDir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	plain := setupTestAppWithUserConfig(t, listKeysNibs(), cfg, "nibs:\n  id_length: 4\n")
	bound := setupTestAppWithUserConfig(t, listKeysNibs(), cfg, "tui:\n  keys:\n    nib.priority: P\n")
	for _, app := range []*App{plain, bound} {
		if !focusOn(app, "nib-4") {
			t.Fatal("premise failed: could not focus nib-4")
		}
		pressKeys(app, "P")
	}
	if plain.state != viewList {
		t.Errorf("state = %d after P with the binding only in the project config, want the list", plain.state)
	}
	if bound.state != viewPriorityPicker {
		t.Errorf("state = %d after P with the binding in the user config, want the priority picker", bound.state)
	}
}

func TestTheFooterAndThePaletteShowTheReboundKey(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  keys:\n    nib.edit: E\n    app.help: f1\n")

	footer := footerLine(app)
	if !strings.Contains(footer, "E edit") || strings.Contains(footer, "e edit") {
		t.Errorf("footer = %q, want E edit and not e edit", footer)
	}
	if !strings.Contains(footer, "f1 help") {
		t.Errorf("footer = %q, want f1 help", footer)
	}

	pressKeys(app, "?")
	if app.state != viewList {
		t.Fatalf("state = %d after ?, which app.help no longer has", app.state)
	}
	sendKeyNonBlocking(app, tea.KeyPressMsg{Code: tea.KeyF1})
	if app.state != viewPalette {
		t.Fatalf("state = %d after f1, want the palette", app.state)
	}
	if line := stripAnsi(paletteLine(t, app, actEdit)); !strings.Contains(line, "E ") {
		t.Errorf("palette line = %q, want the key E", line)
	}
}

func TestPickerAcceleratorsAvoidAReboundKey(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  keys:\n    app.quit: [q, d]\n")
	if !focusOn(app, "nib-4") {
		t.Fatal("premise failed: could not focus nib-4")
	}
	pressKeys(app, "s")
	if app.state != viewStatusPicker {
		t.Fatalf("state = %d after s, want the status picker", app.state)
	}

	for _, item := range app.statusPicker.list.Items() {
		if row := item.(statusItem); row.key == "d" {
			t.Errorf("the %s row takes d, which app.quit has", row.name)
		}
	}
	_, cmd := app.Update(keyPress("d"))
	if cmd == nil {
		t.Fatal("d did nothing in the status picker, want app.quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("d did not quit from the status picker")
	}
}

func TestIgnoredBindingsWarnAndThePaletteListsThem(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "nibs:\n  id_length: 4\ntui:\n  keys:\n    nib.prority: P\n")

	want := "Ignored 1 TUI setting in the user config; ? lists it"
	if footer := footerLine(app); !strings.Contains(footer, want) {
		t.Fatalf("footer = %q, want %q", footer, want)
	}
	// A store change reloads the nibs and must leave the warning in place.
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if footer := footerLine(app); !strings.Contains(footer, want) {
		t.Errorf("footer = %q after a reload, want %q", footer, want)
	}

	pressKeys(app, "?")
	view := stripAnsi(app.palette.View())
	for _, part := range []string{"Ignored TUI settings in /", "line 5", "nib.prority: P", "no action has this name"} {
		if !strings.Contains(view, part) {
			t.Errorf("the palette does not show %q:\n%s", part, view)
		}
	}

	pressKeys(app, "e s t")
	if view := stripAnsi(app.palette.View()); strings.Contains(view, "nib.prority") {
		t.Errorf("an ignored entry that does not match the query is shown:\n%s", view)
	}
}

func TestAUserConfigThatDidNotLoadWarnsAndKeepsTheDefaultKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nibs.yml")
	text := "tui:\n  keys:\n    nib.priority: P\n  broken: [\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	user := config.LoadUserLayerFrom(path)
	if user.Failure == nil {
		t.Fatal("premise failed: the user config loaded")
	}
	app := setupTestAppWithUserLayer(t, listKeysNibs(), config.Default(), user)

	if app.list.statusKind != statusWarn || !strings.Contains(app.list.statusMessage, user.Failure.Message()) {
		t.Errorf("status = %q (kind %d), want a warning carrying %q", app.list.statusMessage, app.list.statusKind, user.Failure.Message())
	}
	view := strings.Join(strings.Fields(stripAnsi(app.View().Content)), " ")
	for _, part := range []string{path, "did not load", "line 4"} {
		if !strings.Contains(view, part) {
			t.Errorf("the view does not show %q:\n%s", part, view)
		}
	}
	if len(app.ignored) != 0 {
		t.Errorf("ignored = %+v, want none: the whole file failed", app.ignored)
	}
	for _, a := range defaultActions {
		if got := keysOf(app.keys, a.Name); !slices.Equal(got, a.Keys) {
			t.Errorf("%s keys = %v, want the default %v", a.Name, got, a.Keys)
		}
	}

	pressKeys(app, "?")
	if view := stripAnsi(app.palette.View()); strings.Contains(view, "Ignored TUI settings") {
		t.Errorf("the palette lists ignored bindings for a file that did not load:\n%s", view)
	}
}

func TestAReloadKeepsTheKeymap(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  keys:\n    nib.priority: P\n")
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if !focusOn(app, "nib-4") {
		t.Fatal("premise failed: could not focus nib-4")
	}
	pressKeys(app, "P")
	if app.state != viewPriorityPicker {
		t.Errorf("state = %d after a reload and P, want the priority picker", app.state)
	}
}

func TestTheConfirmDialogHintsTheReboundKey(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  keys:\n    confirm.no: c\n")
	if !focusOn(app, "nib-4") {
		t.Fatal("premise failed: could not focus nib-4")
	}
	pressKeys(app, "a")
	if app.state != viewConfirmDialog {
		t.Fatalf("state = %d after a, want the confirm dialog", app.state)
	}
	if view := stripAnsi(app.confirmDialog.View(app.width, app.height)); !strings.Contains(view, "c cancel") {
		t.Errorf("the confirm dialog does not hint c cancel:\n%s", view)
	}
}

func TestKeysCheckKeyAcceptsEveryDefaultKey(t *testing.T) {
	for _, a := range defaultActions {
		for _, binding := range a.Keys {
			for _, k := range strings.Fields(binding) {
				if why := checkKey(k); why != "" {
					t.Errorf("%s: default key %q is refused: %s", a.Name, k, why)
				}
			}
		}
	}
}

func TestKeysASecondEntryForAnActionIsIgnored(t *testing.T) {
	user, path := writeUserConfig(t, "tui:\n  keys:\n    nib.edit: E\n    nib.edit: x\n")
	km := loadSettings(defaultActions, &user.TUI, path)
	if b := ignoredNamed(t, km, "nib.edit"); b.line != 4 {
		t.Errorf("ignored %+v, want the entry on line 4", b)
	}
}

// A picker's list widget binds enter and esc to drive its filter, so a rebound
// input.accept or input.cancel must take them out of reach.
func TestAReboundAcceptOrCancelIsTheOnlyKeyThatLeavesAPickerFilter(t *testing.T) {
	const text = "tui:\n  keys:\n    input.accept: ctrl+j\n    input.cancel: ctrl+g\n"
	tests := []struct {
		name  string
		leave tea.KeyPressMsg
		after list.FilterState
		stale string // the default key, which must not leave
	}{
		{"accept", ctrlJ, list.FilterApplied, "enter"},
		{"cancel", ctrlG, list.Unfiltered, "esc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), text)
			if !focusOn(app, "nib-4") {
				t.Fatal("premise failed: could not focus nib-4")
			}
			pressKeys(app, "s / t")
			if got := app.statusPicker.list.FilterState(); got != list.Filtering {
				t.Fatalf("premise failed: filter state = %v after / t, want filtering", got)
			}

			pressKeys(app, tt.stale)
			if got := app.statusPicker.list.FilterState(); got != list.Filtering {
				t.Errorf("filter state = %v after %s, want still filtering", got, tt.stale)
			}
			sendKeyNonBlocking(app, tt.leave)
			if got := app.statusPicker.list.FilterState(); got != tt.after {
				t.Errorf("filter state = %v after %s, want %v", got, tt.leave, tt.after)
			}
		})
	}
}

var (
	ctrlJ = tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	ctrlG = tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
)

// The filter box leaves only on the keys input.accept and input.cancel are
// bound to.
func TestAReboundAcceptOrCancelIsTheOnlyKeyThatLeavesTheFilterBox(t *testing.T) {
	const text = "tui:\n  keys:\n    input.accept: ctrl+j\n    input.cancel: ctrl+g\n"
	tests := []struct {
		name      string
		leave     tea.KeyPressMsg
		stale     string // the default key, which must not leave
		wantQuery string
	}{
		{"accept", ctrlJ, "enter", closedToken + " x"},
		{"cancel", ctrlG, "esc", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), text)
			pressKeys(app, "/ x")
			if !app.list.editing || app.keyContext() != ctxFilterBox {
				t.Fatalf("premise failed: after / x the box is open = %v in the %s", app.list.editing, app.keyContext())
			}

			pressKeys(app, tt.stale)
			if !app.list.editing {
				t.Errorf("%s closed the box", tt.stale)
			}
			sendKeyNonBlocking(app, tt.leave)
			if app.list.editing || app.list.query != tt.wantQuery {
				t.Errorf("after %s the box is open = %v with query %q, want closed with %q", tt.leave, app.list.editing, app.list.query, tt.wantQuery)
			}
		})
	}
}

// The tui block follows YAML's merge keys and aliases, and a repeated keys
// entry is one ignored entry rather than a silent choice of one of them.
func TestKeysTheTUIBlockFollowsMergesAliasesAndDuplicates(t *testing.T) {
	const base = "base: &b\n  keys:\n    nib.edit: E\n"
	for _, tt := range []struct{ name, text string }{
		{"merge", base + "tui:\n  <<: *b\n"},
		{"alias", base + "tui: *b\n"},
		{"aliased keys", "k: &k\n  nib.edit: E\ntui:\n  keys: *k\n"},
	} {
		km := loadUserSettings(t, tt.text)
		if len(km.ignored) != 0 || !slices.Equal(keysOf(km.keys, actEdit), []string{"E"}) {
			t.Errorf("%s: ignored %+v, nib.edit keys %v; want none and [E]", tt.name, km.ignored, keysOf(km.keys, actEdit))
		}
	}

	km := loadUserSettings(t, "tui:\n  keys:\n    nib.edit: E\n  keys:\n    nib.edit: x\n")
	if len(km.ignored) != 1 || km.ignored[0].line != 2 || !strings.Contains(km.ignored[0].reason, `"keys" already defined`) {
		t.Errorf("duplicate keys: ignored %+v, want one entry on line 2 naming the duplicate", km.ignored)
	}
	if !slices.Equal(keysOf(km.keys, actEdit), []string{"e"}) {
		t.Errorf("duplicate keys: nib.edit keys %v, want the default", keysOf(km.keys, actEdit))
	}
}
