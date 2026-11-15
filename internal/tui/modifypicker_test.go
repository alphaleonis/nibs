package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func modifyNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "nib-1", Title: "One", Type: "task", Status: "todo", Priority: "high", Estimate: "s", Order: "1"},
		{ID: "nib-2", Title: "Two", Type: "task", Status: "todo", Priority: "high", Estimate: "l", Order: "2"},
		{ID: "nib-3", Title: "Three", Type: "task", Status: "todo", Order: "3"},
	}
}

// modifyField is one field the modify picker reaches, and how to read the
// value a picker wrote for it.
type modifyField struct {
	name    string
	key     string // the field's accelerator in the modify picker
	pick    string // the value to choose in the field's picker
	view    func(*App) string
	written func(stubUpdateCall) string
}

var modifyFields = []modifyField{
	{
		name: "priority", key: "p", pick: "critical",
		view: func(a *App) string { return a.priorityPicker.View() },
		written: func(c stubUpdateCall) string {
			if v := c.Input.Priority.Value(); v != nil {
				return *v
			}
			return ""
		},
	},
	{
		name: "estimate", key: "e", pick: "xl",
		view: func(a *App) string { return a.estimatePicker.View() },
		written: func(c stubUpdateCall) string {
			if v := c.Input.Estimate.Value(); v != nil {
				return *v
			}
			return ""
		},
	},
}

// pickViaModify types m, the field's key, and the accelerator of f.pick.
func pickViaModify(t *testing.T, app *App, f modifyField) {
	t.Helper()
	pressKeys(app, "m "+f.key)
	key := acceleratorOf(f.view(app), f.pick)
	if key == "" {
		t.Fatalf("premise failed: m %s opened no picker showing %q:\n%s", f.key, f.pick, stripAnsi(app.render()))
	}
	pressKeys(app, key)
}

// assertWritten fails unless f.pick was written to exactly the nibs in ids.
func assertWritten(t *testing.T, stub *StubBackend, f modifyField, ids ...string) {
	t.Helper()
	var got []string
	for _, c := range stub.UpdateCalls {
		if v := f.written(c); v != f.pick {
			t.Errorf("%s of %s set to %q, want %q", f.name, c.ID, v, f.pick)
		}
		got = append(got, c.ID)
	}
	slices.Sort(got)
	slices.Sort(ids)
	if !slices.Equal(got, ids) {
		t.Errorf("%s written to %v, want %v", f.name, got, ids)
	}
}

func TestModifyPickerChangesAFieldOfTheNibUnderTheCursor(t *testing.T) {
	for _, f := range modifyFields {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupTestApp(t, modifyNibs())
			focusOn(app, "nib-3")
			pickViaModify(t, app, f)
			assertWritten(t, stub, f, "nib-3")
			if app.state != viewList {
				t.Errorf("state = %d after choosing, want the list", app.state)
			}
		})
	}
}

func TestModifyPickerChangesAFieldOfEverySelectedNib(t *testing.T) {
	for _, f := range modifyFields {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupTestApp(t, modifyNibs())
			pressKeys(app, "space space")
			pickViaModify(t, app, f)
			assertWritten(t, stub, f, "nib-1", "nib-2")
			if len(app.list.selectedNibs) != 0 {
				t.Errorf("the selection %v outlived the change", app.list.selectedNibs)
			}
		})
	}
}

func TestModifyPickerChangesAFieldFromTheNibView(t *testing.T) {
	for _, f := range modifyFields {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupTestApp(t, modifyNibs())
			focusOn(app, "nib-2")
			pressKeys(app, "enter")
			if app.state != viewDetail {
				t.Fatalf("premise failed: state = %d after enter, want the nib view", app.state)
			}
			pickViaModify(t, app, f)
			assertWritten(t, stub, f, "nib-2")
			if app.state != viewDetail {
				t.Errorf("state = %d after choosing, want the nib view", app.state)
			}
		})
	}
}

// modifyRowText returns the modify picker's row for field, with the spaces
// between its words collapsed.
func modifyRowText(t *testing.T, app *App, field string) string {
	t.Helper()
	for _, line := range strings.Split(stripAnsi(app.modifyPicker.View()), "\n") {
		if strings.Contains(line, " "+field+" ") {
			return strings.Join(strings.Fields(strings.Trim(line, "│ ▌")), " ")
		}
	}
	t.Fatalf("the modify picker shows no %s row:\n%s", field, stripAnsi(app.modifyPicker.View()))
	return ""
}

func TestModifyPickerShowsTheCurrentValues(t *testing.T) {
	tests := []struct {
		name     string
		keys     string // typed in the list before m
		priority string
		estimate string
	}{
		{name: "one nib", keys: "", priority: "p priority high", estimate: "e estimate s"},
		{name: "unset values", keys: "down down", priority: "p priority " + nib.DefaultPriority, estimate: "e estimate none"},
		{name: "a selection", keys: "space space", priority: "p priority high", estimate: "e estimate mixed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := setupTestApp(t, modifyNibs())
			pressKeys(app, tt.keys+" m")
			if app.state != viewModifyPicker {
				t.Fatalf("state = %d after m, want the modify picker", app.state)
			}
			if got := modifyRowText(t, app, "priority"); got != tt.priority {
				t.Errorf("priority row reads %q, want %q", got, tt.priority)
			}
			if got := modifyRowText(t, app, "estimate"); got != tt.estimate {
				t.Errorf("estimate row reads %q, want %q", got, tt.estimate)
			}
		})
	}
}

func TestEscLeavesTheModifyPicker(t *testing.T) {
	app, _ := setupTestApp(t, modifyNibs())
	pressKeys(app, "enter m")
	if app.state != viewModifyPicker {
		t.Fatalf("premise failed: state = %d after m, want the modify picker", app.state)
	}
	pressKeys(app, "esc")
	if app.state != viewDetail {
		t.Errorf("state = %d after esc, want the nib view m was typed in", app.state)
	}
}

// A reload while the picker is open moves the list cursor by index onto
// another nib.
func TestModifyPickerChangesTheNibItShowsAfterAReload(t *testing.T) {
	for _, f := range modifyFields {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupTestApp(t, modifyNibs())
			focusOn(app, "nib-3")
			pressKeys(app, "m")
			if app.state != viewModifyPicker {
				t.Fatalf("premise failed: state = %d after m, want the modify picker", app.state)
			}
			first := &nib.Nib{ID: "nib-0", Title: "Zero", Type: "task", Status: "todo", Order: "0"}
			stub.AllNibs = append([]*nib.Nib{first}, stub.AllNibs...)
			stub.Nibs[first.ID] = first
			_, cmd := app.Update(nibsChangedMsg{})
			processCmdNonBlocking(app, cmd)
			if got := selectedID(app); got == "nib-3" {
				t.Fatalf("premise failed: the reload left the list cursor on nib-3")
			}

			pressKeys(app, f.key)
			key := acceleratorOf(f.view(app), f.pick)
			if key == "" {
				t.Fatalf("premise failed: %s opened no picker showing %q", f.key, f.pick)
			}
			pressKeys(app, key)
			assertWritten(t, stub, f, "nib-3")
		})
	}
}

func TestPickersWithoutAFilterOfferNone(t *testing.T) {
	pickers := map[string]func(*App) string{
		"m": func(a *App) string { return a.modifyPicker.View() },
		"t": func(a *App) string { return a.typePicker.View() },
	}
	for open, view := range pickers {
		t.Run(open, func(t *testing.T) {
			app, _ := setupTestApp(t, modifyNibs())
			pressKeys(app, open)
			if ctx := app.keyContext(); slices.Contains(filterTargets, ctx) {
				t.Fatalf("premise failed: the %s has a filter", ctx)
			}
			if view := stripAnsi(view(app)); strings.Contains(view, "/ filter") {
				t.Errorf("%s opened a picker offering / filter:\n%s", open, view)
			}
		})
	}
}
