package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func filterPickerNibs() []*nib.Nib {
	nibs := listKeysNibs()
	nibs[0].Tags = []string{"backend"}
	nibs[3].Tags = []string{"ui", "backend"}
	return nibs
}

// setupFilterApp returns an app on filterPickerNibs whose query is query.
func setupFilterApp(t *testing.T, query string) *App {
	t.Helper()
	app, _ := setupTestApp(t, filterPickerNibs())
	app.list.setQuery(query)
	return app
}

// toggleValues presses the accelerator of each value's row in the open
// filter values picker.
func toggleValues(t *testing.T, app *App, values ...string) {
	t.Helper()
	for _, v := range values {
		key := ""
		for _, mark := range []string{"[ ] ", "[x] "} {
			if key = acceleratorOf(app.filterValues.View(), mark+v); key != "" {
				break
			}
		}
		if key == "" {
			t.Fatalf("premise failed: no row reads %q:\n%s", v, stripAnsi(app.render()))
		}
		pressKeys(app, key)
	}
}

// openValues opens field's values picker through F and the field's row.
func openValues(t *testing.T, app *App, label string) {
	t.Helper()
	pressKeys(app, "F")
	if app.state != viewFilterPicker {
		t.Fatalf("premise failed: F opened state %d, want the filter picker", app.state)
	}
	key := acceleratorOf(app.filterPicker.View(), label)
	if key == "" {
		t.Fatalf("premise failed: the filter picker shows no %s row:\n%s", label, stripAnsi(app.filterPicker.View()))
	}
	pressKeys(app, key)
	if app.state != viewFilterValuesPicker {
		t.Fatalf("premise failed: %s opened state %d, want the filter values picker", label, app.state)
	}
}

func TestFilterPickerWritesTheFieldToken(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		field  string
		toggle []string
		want   string
	}{
		{"checks two statuses", "", "Status", []string{"todo", "in-progress"}, "status:todo,in-progress"},
		{"replaces a status token", "status:completed", "Status", []string{"todo", "in-progress", "completed"}, "status:todo,in-progress"},
		{"replaces an excluding status token", "-status:closed", "Status", []string{"todo", "in-progress"}, "status:todo,in-progress"},
		{"unchecking everything removes the token", "status:todo", "Status", []string{"todo"}, ""},
		{"unchecking everything removes an excluding token too", "-status:closed status:todo", "Status", []string{"todo"}, ""},
		{"keeps the other tokens and the search words", "-priority:low type:bug status:draft parent:nib-1 fix login", "Status", []string{"draft", "todo"},
			"type:bug -priority:low status:todo parent:nib-1 fix login"},
		{"type", "", "Type", []string{"bug", "task"}, "type:bug,task"},
		{"priority", "", "Priority", []string{"high"}, "priority:high"},
		{"estimate", "", "Estimate", []string{"s", "l"}, "estimate:s,l"},
		{"tags", "", "Tags", []string{"ui"}, "tags:ui"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := setupFilterApp(t, tc.query)
			openValues(t, app, tc.field)
			toggleValues(t, app, tc.toggle...)
			pressKeys(app, "enter")
			if app.state != viewList {
				t.Errorf("state = %d after enter, want the list", app.state)
			}
			if app.list.query != tc.want {
				t.Errorf("query = %q, want %q", app.list.query, tc.want)
			}
		})
	}
}

func TestTheOpenPresetChecksTheOpenStatuses(t *testing.T) {
	app := setupFilterApp(t, "-status:closed status:completed")
	openValues(t, app, "Status")
	key := acceleratorOf(app.filterValues.View(), "Open")
	if key == "" {
		t.Fatalf("premise failed: no Open preset row:\n%s", stripAnsi(app.filterValues.View()))
	}
	pressKeys(app, key+" enter")
	if app.list.query != "status:open" {
		t.Errorf("query = %q, want status:open", app.list.query)
	}
}

func TestTheValuesPickerIsPreCheckedFromTheQuery(t *testing.T) {
	tests := []struct {
		query     string
		checked   []string
		unchecked []string
	}{
		{"status:todo,completed", []string{"todo", "completed"}, []string{"draft", "in-progress", "scrapped"}},
		{"status:open", []string{"draft", "todo", "in-progress"}, []string{"completed"}},
		{"-status:closed", nil, []string{"draft", "todo", "completed"}},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			app := setupFilterApp(t, tc.query)
			openValues(t, app, "Status")
			view := stripAnsi(app.filterValues.View())
			for _, v := range tc.checked {
				if !strings.Contains(view, "[x] "+v+" ") {
					t.Errorf("%s is not checked:\n%s", v, view)
				}
			}
			for _, v := range tc.unchecked {
				if !strings.Contains(view, "[ ] "+v+" ") {
					t.Errorf("%s is checked:\n%s", v, view)
				}
			}
		})
	}
}

func TestTheValuesPickerNamesTheExclusionItReplaces(t *testing.T) {
	app := setupFilterApp(t, "-status:closed")
	openValues(t, app, "Status")
	if view := stripAnsi(app.filterValues.View()); !strings.Contains(view, "-status:closed") {
		t.Errorf("the picker does not show the token it replaces:\n%s", view)
	}
}

func TestEscLeavesTheQueryUntouched(t *testing.T) {
	app := setupFilterApp(t, "-status:closed")
	openValues(t, app, "Status")
	toggleValues(t, app, "todo")
	pressKeys(app, "esc")
	if app.state != viewList || app.list.query != "-status:closed" {
		t.Errorf("state %d, query %q after esc, want the list and -status:closed", app.state, app.list.query)
	}
}

// Nothing is checked, so applying would drop the exclusion the picker does
// not show as a mark.
func TestEnterWithoutAChangeLeavesTheQueryUntouched(t *testing.T) {
	app := setupFilterApp(t, "-status:closed")
	openValues(t, app, "Status")
	pressKeys(app, "enter")
	if app.state != viewList || app.list.query != "-status:closed" {
		t.Errorf("state %d, query %q after enter, want the list and -status:closed", app.state, app.list.query)
	}
}

// A double tap of the key that chose the field marks no row.
func TestTheFieldKeyMarksNoValue(t *testing.T) {
	app := setupFilterApp(t, "")
	pressKeys(app, "F")
	key := acceleratorOf(app.filterPicker.View(), "Status")
	pressKeys(app, key+" "+key+" enter")
	if app.list.query != "" {
		t.Errorf("query = %q after F %s %s enter, want it empty", app.list.query, key, key)
	}
}

func TestTheFilterPickerShowsEachFieldsTokens(t *testing.T) {
	app := setupFilterApp(t, "status:todo -type:bug tags:ui fix")
	pressKeys(app, "F")
	view := stripAnsi(app.filterPicker.View())
	for label, want := range map[string]string{
		"Status":   "status:todo",
		"Type":     "-type:bug",
		"Priority": "any",
		"Estimate": "any",
		"Tags":     "tags:ui",
	} {
		found := false
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, " "+label+" ") && strings.Contains(line, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("the %s row does not read %q:\n%s", label, want, view)
		}
	}
}

func TestThePaletteRunsAFieldFilter(t *testing.T) {
	app := setupFilterApp(t, "")
	pressKeys(app, "?")
	typeText(app, "filter.type")
	pressKeys(app, "enter")
	if app.state != viewFilterValuesPicker {
		t.Fatalf("state = %d after running filter.type, want the filter values picker", app.state)
	}
	toggleValues(t, app, "bug")
	pressKeys(app, "enter")
	if app.list.query != "type:bug" {
		t.Errorf("query = %q, want type:bug", app.list.query)
	}
}

func TestAFieldFilterCanBeBound(t *testing.T) {
	app := setupTestAppWithUserConfig(t, filterPickerNibs(), config.Default(), "tui:\n  keys:\n    filter.priority: P\n")
	app.list.setQuery("")
	pressKeys(app, "P")
	if app.state != viewFilterValuesPicker {
		t.Fatalf("state = %d after P, want the filter values picker", app.state)
	}
	toggleValues(t, app, "critical")
	pressKeys(app, "enter")
	if app.list.query != "priority:critical" {
		t.Errorf("query = %q, want priority:critical", app.list.query)
	}
}

func TestApplyingAFieldFilterNarrowsTheList(t *testing.T) {
	app := setupFilterApp(t, "")
	openValues(t, app, "Status")
	toggleValues(t, app, "completed")
	pressKeys(app, "enter")
	if !focusOn(app, "nib-3") || focusOn(app, "nib-4") {
		t.Errorf("status:completed should list nib-3 alone:\n%s", stripAnsi(app.render()))
	}
}

// A long token in the replaces line, a long tag, or the preset's description
// at a narrow width would wrap and push the modal's bottom off the screen.
func TestTheValuesPickerFitsANarrowTerminal(t *testing.T) {
	longTag := "browser-compatibility-" + strings.Repeat("x", 60)
	tests := []struct {
		name          string
		width, height int
		query, field  string
	}{
		{"status at 44x14", 44, 14, "-status:closed", "Status"},
		{"long tags at 100x16", 100, 16, "tags:" + longTag + ",t1,t2,t3,t4,t5,t6,t7 -tags:" + strings.Repeat("aaaaaaaaa,", 8) + "ui", "Tags"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := setupFilterApp(t, tc.query)
			app.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			openValues(t, app, tc.field)
			content := app.View().Content
			assertFrameFitsTerminal(t, content, tc.width, tc.height)
			painted := frameText(content, tc.width, tc.height)
			if !strings.Contains(painted, "esc cancel") || !strings.Contains(painted, "╰") {
				t.Errorf("the hint row or the modal's bottom border is off the screen:\n%s", painted)
			}
		})
	}
}
