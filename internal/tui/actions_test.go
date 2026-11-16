package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/nib"
)

// keyPress builds the message a terminal sends for one key, written the way
// the action table writes it.
func keyPress(k string) tea.KeyPressMsg {
	named := map[string]tea.KeyPressMsg{
		"enter":      {Code: tea.KeyEnter},
		"esc":        {Code: tea.KeyEscape},
		"backspace":  {Code: tea.KeyBackspace},
		"tab":        {Code: tea.KeyTab},
		"shift+tab":  {Code: tea.KeyTab, Mod: tea.ModShift},
		"space":      {Code: tea.KeySpace, Text: " "},
		"up":         {Code: tea.KeyUp},
		"down":       {Code: tea.KeyDown},
		"left":       {Code: tea.KeyLeft},
		"right":      {Code: tea.KeyRight},
		"ctrl+left":  {Code: tea.KeyLeft, Mod: tea.ModCtrl},
		"ctrl+right": {Code: tea.KeyRight, Mod: tea.ModCtrl},
		"ctrl+up":    {Code: tea.KeyUp, Mod: tea.ModCtrl},
		"ctrl+down":  {Code: tea.KeyDown, Mod: tea.ModCtrl},
		"pgup":       {Code: tea.KeyPgUp},
		"pgdown":     {Code: tea.KeyPgDown},
		"home":       {Code: tea.KeyHome},
		"end":        {Code: tea.KeyEnd},
		"ctrl+home":  {Code: tea.KeyHome, Mod: tea.ModCtrl},
		"ctrl+end":   {Code: tea.KeyEnd, Mod: tea.ModCtrl},
		"delete":     {Code: tea.KeyDelete},
		"ctrl+c":     {Code: 'c', Mod: tea.ModCtrl},
		"ctrl+u":     {Code: 'u', Mod: tea.ModCtrl},
		"ctrl+f":     {Code: 'f', Mod: tea.ModCtrl},
		"f3":         {Code: tea.KeyF3},
		"shift+f3":   {Code: tea.KeyF3, Mod: tea.ModShift},
	}
	if msg, ok := named[k]; ok {
		return msg
	}
	r := []rune(k)
	if len(r) != 1 {
		panic("keyPress: no message for " + k)
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}

// pressKeys sends each space-separated key in turn, as a user types them.
func pressKeys(app *App, keys string) {
	for _, k := range strings.Fields(keys) {
		sendKeyNonBlocking(app, keyPress(k))
	}
}

func TestKeyPressWritesKeysTheWayTheTableDoes(t *testing.T) {
	for _, a := range defaultActions {
		for _, binding := range a.Keys {
			for _, k := range strings.Fields(binding) {
				if got := keyPress(k).String(); got != k {
					t.Errorf("%s: key %q arrives as %q", a.Name, k, got)
				}
			}
		}
	}
}

// Two actions active in one context cannot share a key, and no key in a
// context can start another, longer sequence there, or one of them never runs.
func TestNoTwoActionsShareAKeyInAContext(t *testing.T) {
	if errs := keyConflicts(defaultActions); len(errs) > 0 {
		t.Errorf("the action table has %d conflict(s):\n%s", len(errs), conflictLines(errs))
	}
}

func TestKeyConflictsFindsACollisionAndAShadowedPrefix(t *testing.T) {
	table := []action{
		{Name: "a.one", Contexts: []keyContext{ctxList}, Keys: []string{"x"}},
		{Name: "a.two", Contexts: []keyContext{ctxList, ctxNibBody}, Keys: []string{"x"}},
		{Name: "a.three", Contexts: []keyContext{ctxNibBody}, Keys: []string{"z"}},
		{Name: "a.four", Contexts: []keyContext{ctxNibBody}, Keys: []string{"z z"}},
		{Name: "a.five", Contexts: []keyContext{ctxConfirm}, Keys: []string{"x"}},
	}
	errs := keyConflicts(table)
	if len(errs) != 2 {
		t.Fatalf("got %d conflicts, want 2 (x in the list, z before z z):\n%s", len(errs), conflictLines(errs))
	}
}

func conflictLines(conflicts []keyConflict) string {
	lines := make([]string, len(conflicts))
	for i, c := range conflicts {
		lines[i] = c.String()
	}
	return strings.Join(lines, "\n")
}

func TestEveryActionIsReachable(t *testing.T) {
	seen := map[actionName]bool{}
	for _, a := range defaultActions {
		if seen[a.Name] {
			t.Errorf("%s is in the table twice", a.Name)
		}
		seen[a.Name] = true
		if len(a.Contexts) == 0 {
			t.Errorf("%s is active in no context", a.Name)
		}
		if a.Group == "" || a.Desc == "" {
			t.Errorf("%s has no group or description", a.Name)
		}
	}
}

func listKeysNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "nib-1", Title: "Epic", Type: "epic", Status: "todo", Order: "1"},
		{ID: "nib-2", Title: "Child", Type: "task", Status: "todo", Parent: "nib-1", Order: "2"},
		{ID: "nib-3", Title: "Done", Type: "task", Status: "completed", Order: "3"},
		{ID: "nib-4", Title: "Last", Type: "task", Status: "todo", Order: "4"},
	}
}

func selectedID(app *App) string {
	if item, ok := app.list.list.SelectedItem().(nibItem); ok {
		return item.nib.ID
	}
	return ""
}

func TestNOpensTheCreateFlow(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "n")
	if app.state != viewCreateTypePicker {
		t.Errorf("state = %d after n, want the create type picker", app.state)
	}
}

func TestAAsksBeforeArchiving(t *testing.T) {
	app, stub := setupTestApp(t, listKeysNibs())
	focusOn(app, "nib-4")
	pressKeys(app, "a")
	if app.state != viewConfirmDialog || app.confirmDialog.action != "archive" {
		t.Fatalf("state = %d, action %q after a, want the archive confirm", app.state, app.confirmDialog.action)
	}
	pressKeys(app, "y")
	if len(stub.ArchiveCalls) != 1 || stub.ArchiveCalls[0] != "nib-4" {
		t.Errorf("archived %v, want [nib-4]", stub.ArchiveCalls)
	}
}

func TestAngleBracketsFoldAndUnfoldEveryRow(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	rows := len(app.list.list.Items())
	pressKeys(app, "<")
	if !app.list.collapsed()["nib-1"] || len(app.list.list.Items()) != rows-1 {
		t.Fatalf("after <: collapsed %v, %d rows, want nib-1 folded and %d rows", app.list.collapsed(), len(app.list.list.Items()), rows-1)
	}
	pressKeys(app, ">")
	if len(app.list.collapsed()) != 0 || len(app.list.list.Items()) != rows {
		t.Errorf("after >: collapsed %v, %d rows, want none folded and %d rows", app.list.collapsed(), len(app.list.list.Items()), rows)
	}
}

// z h toggles the -status:closed token in the query, keeping the other tokens.
func TestZHTogglesTheClosedToken(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	if app.list.query != closedToken || slices.Contains(rowIDs(app), "nib-3") {
		t.Fatalf("premise failed: the list opened on query %q with rows %v, want %s hiding nib-3", app.list.query, rowIDs(app), closedToken)
	}
	pressKeys(app, "/")
	typeText(app, "type:task")
	pressKeys(app, "enter z h")
	if app.list.query != "type:task" || !slices.Contains(rowIDs(app), "nib-3") {
		t.Errorf("after z h the query is %q with rows %v, want type:task showing nib-3", app.list.query, rowIDs(app))
	}
	pressKeys(app, "z h")
	if app.list.query != "type:task -status:closed" || slices.Contains(rowIDs(app), "nib-3") {
		t.Errorf("after a second z h the query is %q with rows %v, want type:task -status:closed hiding nib-3", app.list.query, rowIDs(app))
	}
}

func TestZHHidesClosedNibsInTheParentPicker(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	focusOn(app, "nib-4")
	pressKeys(app, "p")
	if app.state != viewParentPicker {
		t.Fatalf("premise failed: state = %d after p, want the parent picker", app.state)
	}
	before, listBefore := app.parentPicker.hideCompleted, app.list.query
	pressKeys(app, "z h")
	if app.parentPicker.hideCompleted == before {
		t.Errorf("z h left the parent picker hiding closed nibs at %v", before)
	}
	if app.list.query != listBefore {
		t.Error("z h in the parent picker also toggled the list")
	}
}

func TestTopAndBottomKeys(t *testing.T) {
	for _, tt := range []struct{ top, bottom string }{{"g g", "G"}, {"home", "end"}} {
		t.Run(tt.top+"/"+tt.bottom, func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			pressKeys(app, tt.bottom)
			if got := selectedID(app); got != "nib-4" {
				t.Errorf("after %s the cursor is on %q, want nib-4", tt.bottom, got)
			}
			pressKeys(app, tt.top)
			if got := selectedID(app); got != "nib-1" {
				t.Errorf("after %s the cursor is on %q, want nib-1", tt.top, got)
			}
		})
	}
}

func TestAPrefixAndAnUnboundKeyDoNothing(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "down g x")
	if got := selectedID(app); got != "nib-2" {
		t.Errorf("g x moved the cursor to %q", got)
	}
	if app.pendingKey != "" {
		t.Errorf("g x left %q pending", app.pendingKey)
	}
	pressKeys(app, "g")
	if app.pendingKey != "g" {
		t.Errorf("g left %q pending, want g", app.pendingKey)
	}
}

// Tags are filtered with a tags: token, so g t is bound to nothing.
func TestGTDoesNothing(t *testing.T) {
	nibs := listKeysNibs()
	nibs[0].Tags = []string{"quux"}
	app, _ := setupTestApp(t, nibs)
	before := plainView(app)
	pressKeys(app, "g t")
	if app.state != viewList || app.pendingKey != "" || app.list.query != closedToken {
		t.Errorf("g t left state %d, pending %q and query %q", app.state, app.pendingKey, app.list.query)
	}
	if after := plainView(app); after != before {
		t.Errorf("g t changed the screen:\n%s", after)
	}
	for _, a := range defaultActions {
		if slices.Contains(a.Keys, "g t") {
			t.Errorf("%s is bound to g t", a.Name)
		}
	}
}

// Keys no action takes in the list do nothing, the list widget's own keys
// among them.
func TestRetiredKeysDoNothingInTheList(t *testing.T) {
	for _, k := range []string{"c", "A", "H", "]", "u", "d", "P", "E", "tab", "shift+tab", "backspace", "esc", "h", "l"} {
		t.Run(k, func(t *testing.T) {
			nibs := make([]*nib.Nib, 0, 60)
			nibs = append(nibs, listKeysNibs()...)
			nibs = append(nibs, makeTestNibs(56)...)
			app, _ := setupTestApp(t, nibs)
			pressKeys(app, "down")
			before := selectedID(app)
			page := app.list.list.Paginator.Page
			folded := len(app.list.collapsed())
			query := app.list.query

			_, cmd := app.Update(keyPress(k))
			if cmd != nil {
				if _, quit := runCmdNonBlocking(cmd).(tea.QuitMsg); quit {
					t.Fatalf("%s quit the app", k)
				}
			}
			processCmd(app, cmd)

			if app.state != viewList {
				t.Errorf("%s left the list for state %d", k, app.state)
			}
			if got := selectedID(app); got != before || app.list.list.Paginator.Page != page {
				t.Errorf("%s moved the cursor from %q to %q (page %d to %d)", k, before, got, page, app.list.list.Paginator.Page)
			}
			if len(app.list.collapsed()) != folded || app.list.query != query {
				t.Errorf("%s changed the folds or the closed filter", k)
			}
		})
	}
}

func TestATextBoxTakesKeysTheTableBinds(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "/ ctrl+u g g n a z h")
	if got := app.list.box.Value(); got != "ggnazh" {
		t.Errorf("filter text = %q, want %q", got, "ggnazh")
	}
	if app.state != viewList {
		t.Errorf("typing into the filter left the list for state %d", app.state)
	}
	pressKeys(app, "esc")
	if app.list.editing || app.list.query != "" {
		t.Errorf("esc left the box open = %v with query %q, want it closed and the query cleared", app.list.editing, app.list.query)
	}
}

func TestArchiveIsNotReadyOnAnEmptyList(t *testing.T) {
	app, _ := setupTestApp(t, nil)
	if ok, why := findAction(defaultActions, actArchive).Ready(app.actionState()); ok || why == "" {
		t.Errorf("archive on an empty list: ready = %v, reason %q; want not ready, with a reason", ok, why)
	}
}

// Create opens the create flow whenever its handler runs, so only the Ready
// gate keeps it closed here.
func TestAnActionThatIsNotReadyDoesNothing(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	create := *findAction(defaultActions, actCreate)
	create.Ready = func(actionState) (bool, string) { return false, "not now" }

	processCmdNonBlocking(app, app.runAction(&create))

	if app.state != viewList {
		t.Errorf("a create that is not ready left the list for state %d", app.state)
	}
}

// Up and down leave a filter being typed and apply it, leaving the cursor
// where the filter put it.
func TestArrowsApplyAFilterBeingTyped(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "nib-1", Title: "Alpha one", Type: "task", Status: "todo", Order: "1"},
		{ID: "nib-2", Title: "Beta", Type: "task", Status: "todo", Order: "2"},
		{ID: "nib-3", Title: "Alpha two", Type: "task", Status: "todo", Order: "3"},
	}
	for _, k := range []string{"down", "up"} {
		t.Run(k, func(t *testing.T) {
			app, _ := setupTestApp(t, nibs)
			pressKeys(app, "/ ctrl+u A l p h a")
			if !app.list.editing || len(app.list.completions) > 0 {
				t.Fatalf("premise failed: after typing the box is open = %v with completions %v, want open with none", app.list.editing, app.list.completions)
			}
			before := selectedID(app)

			pressKeys(app, k)

			if app.list.editing || app.list.query != "Alpha" {
				t.Errorf("after %s the box is open = %v with query %q, want it closed on Alpha", k, app.list.editing, app.list.query)
			}
			if got := len(app.list.list.VisibleItems()); got != 2 {
				t.Errorf("%d rows visible after %s, want the 2 that match", got, k)
			}
			if got := selectedID(app); got != before {
				t.Errorf("%s moved the cursor from %q to %q", k, before, got)
			}
		})
	}
}
