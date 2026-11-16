package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
	"gopkg.in/yaml.v3"
)

// dockNibs are everyRelationNibs and "long", whose body runs past any screen
// and which has no relations, like "rz".
func dockNibs() []*nib.Nib {
	var body strings.Builder
	for i := range 200 {
		fmt.Fprintf(&body, "Line %d of the long body.\n\n", i)
	}
	return append(everyRelationNibs(), &nib.Nib{ID: "long", Title: "Long body", Type: "task", Status: "todo", Order: "8", Body: body.String()})
}

// dockStore writes dockNibs into a fresh store and returns its directory.
func dockStore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(dir).DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	core := nibcore.New(dir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	for _, n := range dockNibs() {
		if err := core.Create(n); err != nil {
			t.Fatalf("create %s: %v", n.ID, err)
		}
	}
	return dir
}

// dockApp starts an App on the store in dir with user as the user config,
// sized width x height, as a launch of the TUI does.
func dockApp(t *testing.T, dir string, user config.UserLayer, width, height int) *App {
	t.Helper()
	cfg := config.Default()
	core := nibcore.New(dir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	resolver := &graph.Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: graph.NewOrderer(core, core)}
	app := New(NewRealBackend(core, resolver), cfg, user, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	processCmd(app, initCmd)
	return app
}

// dockedApp is dockApp with a user config that is not written yet, the nib
// view shown by z p and the list cursor on id.
func dockedApp(t *testing.T, id string, width, height int) *App {
	t.Helper()
	user := config.LoadUserLayerFrom(filepath.Join(t.TempDir(), "nibs.yml"))
	app := dockApp(t, dockStore(t), user, width, height)
	pressKeys(app, "z p")
	moveTo(t, app, id)
	return app
}

// moveTo puts the list cursor on id, and tells the App so as a cursor move does.
func moveTo(t *testing.T, app *App, id string) {
	t.Helper()
	app.list.restoreCursor(id)
	if app.list.focusedID() != id {
		t.Fatalf("premise failed: could not focus %s", id)
	}
	_, cmd := app.Update(cursorChangedMsg{})
	processCmd(app, cmd)
}

// savedNibView returns tui.nib_view as the file at path holds it.
func savedNibView(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc struct {
		TUI struct {
			NibView map[string]any `yaml:"nib_view"`
		} `yaml:"tui"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("the user config does not parse: %v", err)
	}
	return doc.TUI.NibView
}

// requireFits fails when the frame is taller or wider than the terminal.
func requireFits(t *testing.T, app *App) {
	t.Helper()
	content := app.View().Content
	if h, w := lipgloss.Height(content), lipgloss.Width(content); h > app.height || w > app.width {
		t.Fatalf("the frame is %dx%d for a %dx%d terminal", w, h, app.width, app.height)
	}
}

// The view shown by auto docks right at 160x40, below at 90x50, and hides at
// 60x20 with a footer note; see dockAt.
func TestAutoPlacementFollowsTheTerminalSize(t *testing.T) {
	const note = "Nib view hidden: needs 80 columns or 30 rows"
	t.Run("160x40 right", func(t *testing.T) {
		app := dockedApp(t, "hub", 160, 40)
		if got := app.dockPlacement(); got != placementRight {
			t.Fatalf("docked %q, want right", got)
		}
		requireFits(t, app)
		frame := plainView(app)
		right := strings.Join(railText(frame), "\n")
		if !strings.Contains(right, "Hub feature") || !strings.Contains(right, bodyMarker) {
			t.Errorf("the view is not right of the divider:\n%s", frame)
		}
		if strings.Contains(frame, note) {
			t.Errorf("a docked view has the hidden note:\n%s", frame)
		}
	})
	t.Run("90x50 below", func(t *testing.T) {
		app := dockedApp(t, "hub", 90, 50)
		if got := app.dockPlacement(); got != placementBelow {
			t.Fatalf("docked %q, want below", got)
		}
		requireFits(t, app)
		lines := strings.Split(plainView(app), "\n")
		if len(lines) != 50 {
			t.Errorf("the frame is %d lines, want 50", len(lines))
		}
		// The view's top rule, with its id, is the rule between them.
		rule := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "── hub ─") })
		if rule < 0 {
			t.Fatalf("no rule with the view's id:\n%s", strings.Join(lines, "\n"))
		}
		if !regexp.MustCompile(`─ \d+ / \d+ ─$`).MatchString(lines[rule-1]) {
			t.Errorf("the line above the view is %q, want the list's bottom rule", lines[rule-1])
		}
		above, below := strings.Join(lines[:rule], "\n"), strings.Join(lines[rule:], "\n")
		if strings.Contains(above, bodyMarker) || !strings.Contains(below, bodyMarker) {
			t.Errorf("the body is not below the rule:\n%s", strings.Join(lines, "\n"))
		}
		if !strings.Contains(above, "Hub feature") || strings.Contains(above, "│") {
			t.Errorf("the list does not have the top part to itself:\n%s", above)
		}
	})
	t.Run("60x20 hidden", func(t *testing.T) {
		app := dockedApp(t, "hub", 60, 20)
		if got := app.dockPlacement(); got != "" {
			t.Fatalf("docked %q, want hidden", got)
		}
		requireFits(t, app)
		if footer := footerLine(app); !strings.Contains(footer, note) {
			t.Errorf("footer = %q, want %q", footer, note)
		}
		if frame := plainView(app); strings.Contains(frame, bodyMarker) {
			t.Errorf("a hidden view is drawn:\n%s", frame)
		}
	})
}

// A placement the user chose docks where auto would not, and hides with its
// own note when the terminal has no room for it.
func TestAChosenPlacementDocksThereOrHides(t *testing.T) {
	tests := []struct {
		placement     placement
		width, height int
		want          placement
		note          string
	}{
		{placementRight, 90, 50, placementRight, ""},
		{placementBelow, 160, 40, placementBelow, ""},
		{placementRight, 70, 50, "", "Nib view hidden: needs 80 columns"},
		{placementBelow, 160, 20, "", "Nib view hidden: needs 30 rows"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%dx%d", tt.placement, tt.width, tt.height), func(t *testing.T) {
			app := dockedApp(t, "hub", tt.width, tt.height)
			for app.nibView.placement != tt.placement {
				pressKeys(app, "z P")
			}
			pressKeys(app, "j k") // clears the status message z P leaves
			if got := app.dockPlacement(); got != tt.want {
				t.Fatalf("docked %q, want %q", got, tt.want)
			}
			requireFits(t, app)
			footer := footerLine(app)
			if tt.note != "" && !strings.Contains(footer, tt.note) {
				t.Errorf("footer = %q, want %q", footer, tt.note)
			}
			if tt.note == "" && strings.Contains(footer, "hidden") {
				t.Errorf("footer = %q, want no hidden note", footer)
			}
		})
	}
}

func TestAHiddenViewReturnsWhenTheTerminalGrows(t *testing.T) {
	app := dockedApp(t, "hub", 60, 20)
	if app.dockPlacement() != "" {
		t.Fatal("premise failed: the view is docked at 60x20")
	}
	app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if got := app.dockPlacement(); got != placementRight {
		t.Fatalf("docked %q at 160x40, want right", got)
	}
	if frame := plainView(app); !strings.Contains(frame, bodyMarker) || strings.Contains(frame, "hidden") {
		t.Errorf("the view did not return without the note:\n%s", frame)
	}
}

// z p and z P save tui.nib_view into the user config, keeping its comments,
// and a TUI started from that file docks the same way.
func TestZPAndZShiftPSaveTheNibViewAndSurviveARestart(t *testing.T) {
	path := userConfigFile(t, commentedUserConfig)
	dir := dockStore(t)
	app := dockApp(t, dir, config.LoadUserLayerFrom(path), 90, 50)

	pressKeys(app, "z p")
	if got := savedNibView(t, path); got["shown"] != true {
		t.Errorf("tui.nib_view = %v, want shown: true", got)
	}
	pressKeys(app, "z P")
	if got := app.dockPlacement(); got != placementRight {
		t.Errorf("z P docked %q, want right", got)
	}
	if msg := app.list.statusMessage; msg != "Nib view placement: right" {
		t.Errorf("status = %q, want the placement", msg)
	}
	text := readFile(t, path)
	if got := savedNibView(t, path); got["shown"] != true || got["placement"] != "right" {
		t.Errorf("tui.nib_view = %v, want shown: true, placement: right:\n%s", got, text)
	}
	for _, comment := range []string{"# My nibs settings.", "# four is enough", "# Icons I like."} {
		if !strings.Contains(text, comment) {
			t.Errorf("the save dropped the comment %q:\n%s", comment, text)
		}
	}

	restarted := dockApp(t, dir, config.LoadUserLayerFrom(path), 90, 50)
	if len(restarted.ignored) != 0 {
		t.Errorf("the saved file has ignored entries: %+v", restarted.ignored)
	}
	if got := restarted.dockPlacement(); got != placementRight {
		t.Errorf("after a restart the view docks %q, want right", got)
	}

	pressKeys(restarted, "z p")
	if got := savedNibView(t, path); got["shown"] != false {
		t.Errorf("tui.nib_view = %v, want shown: false", got)
	}
	if again := dockApp(t, dir, config.LoadUserLayerFrom(path), 90, 50); again.dockPlacement() != "" || again.nibView.placement != placementRight {
		t.Errorf("after a second restart nib view = %+v docked %q, want hidden and placed right", again.nibView, again.dockPlacement())
	}
}

// A user config with an error is not written, and the nib view still docks
// until the TUI quits.
func TestANibViewChoiceOverABrokenUserConfigWarnsAndApplies(t *testing.T) {
	const broken = "tui: [unclosed\n"
	path := userConfigFile(t, broken)
	user := config.LoadUserLayerFrom(path)
	if user.Failure == nil {
		t.Fatal("premise failed: the broken user config loaded")
	}
	app := dockApp(t, dockStore(t), user, 160, 40)

	for _, keys := range []string{"z p", "z P"} {
		pressKeys(app, keys)
		if msg := app.list.statusMessage; !strings.Contains(msg, "until you quit") || !strings.Contains(msg, "has an error") || app.list.statusKind != statusWarn {
			t.Errorf("%s: status = %q (kind %v), want a warning with the refusal", keys, msg, app.list.statusKind)
		}
	}
	if got := app.dockPlacement(); got != placementRight || app.nibView.placement != placementRight {
		t.Errorf("docked %q placed %q, want right", got, app.nibView.placement)
	}
	if text := readFile(t, path); text != broken {
		t.Errorf("the broken user config was written:\n%s", text)
	}
}

// A user config changed since the TUI read it is read again after the save,
// and the docked view keeps its relations cursor.
func TestANibViewSaveReloadsAChangedUserConfigAndKeepsTheView(t *testing.T) {
	path := userConfigFile(t, commentedUserConfig)
	app := dockApp(t, dockStore(t), config.LoadUserLayerFrom(path), 160, 40)
	pressKeys(app, "z p")
	moveTo(t, app, "hub")
	pressKeys(app, "tab j esc")
	if app.docked.cursor != 1 {
		t.Fatalf("premise failed: the relations cursor is at %d", app.docked.cursor)
	}
	if err := os.WriteFile(path, []byte(readFile(t, path)+"  keys:\n    nib.create: N\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pressKeys(app, "z P")

	if got := app.keys.firstKey(actCreate); got != "N" {
		t.Errorf("nib.create is bound to %q, want the reloaded N", got)
	}
	if got := app.dockPlacement(); got != placementRight || app.nibView.placement != placementRight {
		t.Errorf("docked %q placed %q, want right", got, app.nibView.placement)
	}
	if app.docked.cursor != 1 || app.docked.nib == nil || app.docked.nib.ID != "hub" {
		t.Errorf("the reload reset the docked view: cursor %d", app.docked.cursor)
	}
}

func TestNibViewSettingsThatCannotBeUsedAreIgnoredWithTheirLines(t *testing.T) {
	text := "tui:\n  nib_view:\n    shown: maybe\n    placement: left\n    size: 3\n"
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), text)
	want := []ignoredEntry{
		{entry: "nib_view.shown: maybe", line: 3, reason: "is not true or false"},
		{entry: "nib_view.placement: left", line: 4, reason: "is not auto, right or below"},
		{entry: "nib_view.size: 3", line: 5, reason: "no nib view setting has this name"},
	}
	if !slices.Equal(app.ignored, want) {
		t.Errorf("ignored = %+v, want %+v", app.ignored, want)
	}
	if app.nibView != (nibViewSettings{placement: placementAuto}) {
		t.Errorf("nib view = %+v, want the defaults", app.nibView)
	}

	app = setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  nib_view: [shown]\n")
	if want := []ignoredEntry{{entry: "tui.nib_view: [shown]", line: 2, reason: "is not a map of shown and placement"}}; !slices.Equal(app.ignored, want) {
		t.Errorf("ignored = %+v, want %+v", app.ignored, want)
	}

	app = setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  nib_view: {shown: true, placement: below}\n")
	if len(app.ignored) != 0 || app.nibView != (nibViewSettings{shown: true, placement: placementBelow}) {
		t.Errorf("ignored %+v, nib view %+v, want none and shown below", app.ignored, app.nibView)
	}
}

// tab and shift+tab move the focus list → relations → body and back; see
// App.cycleDockFocus.
func TestTabCyclesTheFocusThroughTheDockedView(t *testing.T) {
	tests := []struct {
		id, keys string
		want     []keyContext
	}{
		{"hub", "tab", []keyContext{ctxNibRelations, ctxNibBody, ctxList, ctxNibRelations}},
		{"hub", "shift+tab", []keyContext{ctxNibBody, ctxNibRelations, ctxList, ctxNibBody}},
		{"rz", "tab", []keyContext{ctxNibBody, ctxList, ctxNibBody}},
		{"rz", "shift+tab", []keyContext{ctxNibBody, ctxList, ctxNibBody}},
	}
	for _, tt := range tests {
		t.Run(tt.id+"/"+tt.keys, func(t *testing.T) {
			app := dockedApp(t, tt.id, 160, 40)
			var got []keyContext
			for range tt.want {
				pressKeys(app, tt.keys)
				got = append(got, app.keyContext())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("focus went %v, want %v", got, tt.want)
			}
		})
	}
}

// With the focus in the docked view, keys move its relations cursor and
// scroll its body, the list cursor stays, the footer hints follow, and esc
// returns the focus to the list.
func TestKeysGoToTheFocusedDockedView(t *testing.T) {
	app := dockedApp(t, "hub", 160, 40)
	if footer := footerLine(app); !strings.Contains(footer, "enter open") {
		t.Fatalf("premise failed: the list footer is %q", footer)
	}
	pressKeys(app, "tab")
	if footer := footerLine(app); !strings.Contains(footer, "enter follow") || !strings.Contains(footer, "tab switch") {
		t.Errorf("footer in the relations = %q, want their hints", footer)
	}
	if frame := plainView(app); !strings.Contains(frame, "▸") {
		t.Errorf("the focused relations draw no cursor:\n%s", frame)
	}
	pressKeys(app, "j")
	if app.docked.cursor != 1 || app.list.focusedID() != "hub" {
		t.Errorf("j left the relations cursor at %d and the list on %s, want 1 and hub", app.docked.cursor, app.list.focusedID())
	}
	pressKeys(app, "esc")
	if got := app.keyContext(); got != ctxList || app.list.focusedID() != "hub" {
		t.Errorf("esc left the focus in the %s on %s, want the list on hub", got, app.list.focusedID())
	}

	moveTo(t, app, "long")
	pressKeys(app, "tab j j")
	if got := app.docked.body.YOffset(); got != 2 || app.list.focusedID() != "long" {
		t.Errorf("j j scrolled the body to %d with the list on %s, want 2 and long", got, app.list.focusedID())
	}
}

func TestJAndKScrollTheDockedBodyFromTheList(t *testing.T) {
	app := dockedApp(t, "long", 160, 40)
	pressKeys(app, "J J J")
	if got := app.docked.body.YOffset(); got != 3 {
		t.Errorf("J J J scrolled the body to %d, want 3", got)
	}
	if app.keyContext() != ctxList || app.list.focusedID() != "long" {
		t.Errorf("J moved the focus to the %s or the cursor to %s", app.keyContext(), app.list.focusedID())
	}
	pressKeys(app, "K")
	if got := app.docked.body.YOffset(); got != 2 {
		t.Errorf("K scrolled the body to %d, want 2", got)
	}
}

// Hidden for want of room, the view takes neither the focus nor J.
func TestAHiddenViewTakesNoFocus(t *testing.T) {
	app := dockedApp(t, "long", 60, 20)
	pressKeys(app, "tab J")
	if got := app.keyContext(); got != ctxList || app.list.focusedID() != "long" || app.docked.body.YOffset() != 0 {
		t.Errorf("tab J at 60x20: focus in the %s on %s, body at %d", got, app.list.focusedID(), app.docked.body.YOffset())
	}

	app = dockedApp(t, "long", 160, 40)
	pressKeys(app, "tab")
	app.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if got := app.keyContext(); got != ctxList {
		t.Errorf("after the view hid and returned, the focus is in the %s, want the list", got)
	}
}

// A reload of the nibs keeps the focus, the scroll and the placement.
func TestAReloadKeepsTheDockedViewsFocus(t *testing.T) {
	app := dockedApp(t, "long", 90, 50)
	pressKeys(app, "tab j j")
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if got := app.keyContext(); got != ctxNibBody {
		t.Errorf("after the reload the focus is in the %s, want the body", got)
	}
	if got := app.docked.body.YOffset(); got != 2 {
		t.Errorf("after the reload the body is at %d, want 2", got)
	}
	if got := app.dockPlacement(); got != placementBelow {
		t.Errorf("after the reload the view docks %q, want below", got)
	}
}

// runWithoutPanic runs f, and fails the test with what when f panics.
func runWithoutPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}

// A focused docked view can show the placeholder while the list has marks,
// as after a reload that lands the cursor on a section row. No nib view
// action may then act on a nib, whether the App or the view itself runs it.
func TestNibViewActionsWithoutANibDoNothing(t *testing.T) {
	for _, act := range defaultActions {
		if !slices.Contains(act.Contexts, ctxNibRelations) && !slices.Contains(act.Contexts, ctxNibBody) {
			continue
		}
		t.Run(string(act.Name), func(t *testing.T) {
			app := dockedApp(t, "hub", 160, 40)
			pressKeys(app, "tab")
			app.list.selectedNibs["rz"] = true
			app.showDocked("")
			if !app.dockFocused() || app.docked.nib != nil {
				t.Fatal("premise failed: the focused docked view does not show the placeholder")
			}
			onNib := []actionName{actEdit, actStatus, actType, actParent, actBlocking, actPriority, actEstimate, actModify, actCopyID}
			if ready, why := isReadyIn(app, act); slices.Contains(onNib, act.Name) && (ready || why != "no nib under the cursor") {
				t.Errorf("%s on the placeholder: ready %v, reason %q, want not ready for no nib under the cursor", act.Name, ready, why)
			}
			runWithoutPanic(t, "the App running "+string(act.Name), func() {
				if cmd := app.runAction(findAction(app.keys.actions, act.Name)); cmd != nil {
					app.Update(cmd())
				}
			})

			for _, region := range []nibRegion{regionRelations, regionBody} {
				view := newNibViewModel(nil, app.backend, app.config, app.display, 80, 30)
				view.focus = region
				runWithoutPanic(t, fmt.Sprintf("the view running %s in region %d", act.Name, region), func() {
					if cmd := view.runAction(act.Name); cmd != nil {
						cmd()
					}
				})
			}
		})
	}
}

// isReadyIn reports whether act is ready in app as it is now.
func isReadyIn(app *App, act action) (bool, string) {
	if act.Ready == nil {
		return true, ""
	}
	return act.Ready(app.actionState())
}

// Keys typed faster than the cursor's message arrives act on the nib under
// the cursor, so the docked view follows the cursor before the next key.
func TestTheDockedViewFollowsTheCursorBeforeTheNextKey(t *testing.T) {
	burst := func(app *App, keys string) {
		for _, k := range strings.Fields(keys) {
			app.Update(keyPress(k)) // the commands, cursorChangedMsg among them, never arrive
		}
	}

	app := dockedApp(t, "hub", 160, 40)
	burst(app, "j")
	moved := app.list.focusedID()
	if moved == "hub" {
		t.Fatal("premise failed: j did not move the cursor")
	}
	if got := app.docked.nib; got == nil || got.ID != moved {
		t.Errorf("after j the docked view shows %v, want the cursor's %s", got, moved)
	}
	app = dockedApp(t, "hub", 160, 40)
	app.list.restoreCursor(moved) // a cursor move no key made, as a view change makes
	burst(app, "tab s")
	if app.state != viewStatusPicker || !slices.Equal(app.statusPicker.nibIDs, []string{moved}) {
		t.Errorf("tab s opened state %d for %v, want the status picker for %s", app.state, app.statusPicker.nibIDs, moved)
	}

	app = dockedApp(t, "rz", 160, 40)
	app.list.restoreCursor("long")
	burst(app, "J")
	if got := app.docked.nib; got == nil || got.ID != "long" || app.docked.body.YOffset() != 1 {
		t.Errorf("J scrolled the docked view of %v, want long's", got)
	}
}

// A cursorChangedMsg that arrives after a later move names a nib the cursor
// has left, and the docked view stays on the cursor's nib.
func TestALateCursorMessageLeavesTheDockedViewOnTheCursor(t *testing.T) {
	app := dockedApp(t, "hub", 160, 40)
	_, late := app.Update(keyPress("j"))
	if late == nil {
		t.Fatal("premise failed: j sent no cursor message")
	}
	app.Update(keyPress("j"))
	app.Update(keyPress("tab"))
	app.Update(late())
	app.Update(keyPress("s"))
	cursor := app.list.focusedID()
	if app.state != viewStatusPicker || !slices.Equal(app.statusPicker.nibIDs, []string{cursor}) {
		t.Errorf("j j tab, the first j's message, s opened state %d for %v, want the status picker for %s", app.state, app.statusPicker.nibIDs, cursor)
	}
}
