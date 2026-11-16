package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

func modifyNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "nib-1", Title: "One", Type: "task", Status: "todo", Priority: "high", Estimate: "s", Order: "1", Milestone: "ms-1", Area: "api"},
		{ID: "nib-2", Title: "Two", Type: "task", Status: "todo", Priority: "high", Estimate: "l", Order: "2", Milestone: "ms-1", Area: "docs"},
		{ID: "nib-3", Title: "Three", Type: "task", Status: "todo", Order: "3"},
		{ID: "ms-1", Title: "Launch", Type: "milestone", Status: "todo", Order: "4"},
		{ID: "ms-2", Title: "Beta", Type: "milestone", Status: "todo", Order: "5"},
	}
}

const modifyAreas = `
areas:
  - name: docs
  - name: api
    children:
      - name: webhooks
`

// setupModifyApp is setupTestApp on modifyNibs, with modifyAreas declared.
func setupModifyApp(t *testing.T) (*App, *StubBackend) {
	t.Helper()
	app, stub := setupTestApp(t, modifyNibs())
	vocab, err := area.Parse([]byte(modifyAreas))
	if err != nil {
		t.Fatal(err)
	}
	stub.AreaVocab = vocab
	return app, stub
}

// modifyField is one field the modify picker reaches, and how to read the
// value a picker wrote for it.
type modifyField struct {
	name    string
	key     string // the field's accelerator in the modify picker
	pick    string // the row to choose in the field's picker
	writes  string // the value choosing pick writes
	view    func(*App) string
	written func(stubUpdateCall) string
}

// writtenValue returns the value an Omittable field of an update carries, or
// "<omitted>" when the update leaves the field unchanged.
func writtenValue(v *string, set bool) string {
	switch {
	case !set:
		return "<omitted>"
	case v == nil:
		return ""
	}
	return *v
}

var modifyFields = []modifyField{
	{
		name: "priority", key: "p", pick: "critical", writes: "critical",
		view: func(a *App) string { return a.priorityPicker.View() },
		written: func(c stubUpdateCall) string {
			if v := c.Input.Priority.Value(); v != nil {
				return *v
			}
			return ""
		},
	},
	{
		name: "estimate", key: "e", pick: "xl", writes: "xl",
		view: func(a *App) string { return a.estimatePicker.View() },
		written: func(c stubUpdateCall) string {
			if v := c.Input.Estimate.Value(); v != nil {
				return *v
			}
			return ""
		},
	},
	{
		name: "milestone", key: "m", pick: "Beta", writes: "ms-2",
		view: func(a *App) string { return a.assignPicker.View() },
		written: func(c stubUpdateCall) string {
			return writtenValue(c.Input.Milestone.Value(), c.Input.Milestone.IsSet())
		},
	},
	{
		name: "area", key: "a", pick: "api/webhooks", writes: "api/webhooks",
		view: func(a *App) string { return a.assignPicker.View() },
		written: func(c stubUpdateCall) string {
			return writtenValue(c.Input.Area.Value(), c.Input.Area.IsSet())
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

// assertWritten fails unless f.writes was written to exactly the nibs in ids.
func assertWritten(t *testing.T, stub *StubBackend, f modifyField, ids ...string) {
	t.Helper()
	assertWrote(t, stub, f, f.writes, ids...)
}

// assertWrote fails unless value was written to f of exactly the nibs in ids.
func assertWrote(t *testing.T, stub *StubBackend, f modifyField, value string, ids ...string) {
	t.Helper()
	var got []string
	for _, c := range stub.UpdateCalls {
		if v := f.written(c); v != value {
			t.Errorf("%s of %s set to %q, want %q", f.name, c.ID, v, value)
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
			app, stub := setupModifyApp(t)
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
			app, stub := setupModifyApp(t)
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
			app, stub := setupModifyApp(t)
			focusOn(app, "nib-2")
			pressKeys(app, "enter")
			if app.state != viewExpanded {
				t.Fatalf("premise failed: state = %d after enter, want the nib view", app.state)
			}
			pickViaModify(t, app, f)
			assertWritten(t, stub, f, "nib-2")
			if app.state != viewExpanded {
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
		name      string
		keys      string // typed in the list before m
		priority  string
		estimate  string
		milestone string
		area      string
	}{
		{
			name: "one nib", keys: "",
			priority: "p priority high", estimate: "e estimate s", milestone: "m milestone Launch", area: "a area api",
		},
		{
			name: "unset values", keys: "down down",
			priority: "p priority " + nib.DefaultPriority, estimate: "e estimate none", milestone: "m milestone none", area: "a area none",
		},
		{
			name: "a selection", keys: "space space",
			priority: "p priority high", estimate: "e estimate mixed", milestone: "m milestone Launch", area: "a area mixed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := setupModifyApp(t)
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
			if got := modifyRowText(t, app, "milestone"); got != tt.milestone {
				t.Errorf("milestone row reads %q, want %q", got, tt.milestone)
			}
			if got := modifyRowText(t, app, "area"); got != tt.area {
				t.Errorf("area row reads %q, want %q", got, tt.area)
			}
		})
	}
}

func TestEscLeavesTheModifyPicker(t *testing.T) {
	app, _ := setupModifyApp(t)
	pressKeys(app, "enter m")
	if app.state != viewModifyPicker {
		t.Fatalf("premise failed: state = %d after m, want the modify picker", app.state)
	}
	pressKeys(app, "esc")
	if app.state != viewExpanded {
		t.Errorf("state = %d after esc, want the nib view m was typed in", app.state)
	}
}

// A reload while the picker is open moves the list cursor onto another nib when
// the nib under it closes, because the list hides closed nibs.
func TestModifyPickerChangesTheNibItShowsAfterAReload(t *testing.T) {
	for _, f := range modifyFields {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupModifyApp(t)
			focusOn(app, "nib-3")
			pressKeys(app, "m")
			if app.state != viewModifyPicker {
				t.Fatalf("premise failed: state = %d after m, want the modify picker", app.state)
			}
			closed := *stub.Nibs["nib-3"]
			closed.Status = "completed"
			stub.AllNibs[2], stub.Nibs["nib-3"] = &closed, &closed
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
			app, _ := setupModifyApp(t)
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

// assignFields are the modifyFields whose picker offers a row to clear them.
func assignFields() []modifyField {
	return slices.DeleteFunc(slices.Clone(modifyFields), func(f modifyField) bool {
		return f.name != "milestone" && f.name != "area"
	})
}

func TestModifyPickerClearsAMilestoneAndAnArea(t *testing.T) {
	for _, f := range assignFields() {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupModifyApp(t)
			pressKeys(app, "space space m "+f.key)
			none := "(No " + f.name + ")"
			key := acceleratorOf(f.view(app), none)
			if key == "" {
				t.Fatalf("premise failed: the %s picker shows no %s row:\n%s", f.name, none, stripAnsi(f.view(app)))
			}
			pressKeys(app, key)
			assertWrote(t, stub, f, "", "nib-1", "nib-2")
		})
	}
}

// The picker preselects the value the targets share, so enter alone keeps it.
func TestAssignPickerOpensOnTheCurrentValue(t *testing.T) {
	for _, f := range assignFields() {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupModifyApp(t)
			pressKeys(app, "m "+f.key+" enter")
			want := map[string]string{"milestone": "ms-1", "area": "api"}[f.name]
			assertWrote(t, stub, f, want, "nib-1")
		})
	}
}

func TestARefusedAssignmentShowsInTheStatusLine(t *testing.T) {
	for _, f := range assignFields() {
		t.Run(f.name, func(t *testing.T) {
			app, stub := setupModifyApp(t)
			rows := rowIDs(app)
			stub.UpdateErr = errors.New("the server refused it")
			focusOn(app, "nib-3")
			pickViaModify(t, app, f)

			if app.state != viewList {
				t.Errorf("state = %d after a refusal, want the list", app.state)
			}
			if !strings.Contains(app.list.statusMessage, "the server refused it") {
				t.Errorf("status line = %q, want the server's refusal", app.list.statusMessage)
			}
			if got := rowIDs(app); !slices.Equal(got, rows) {
				t.Errorf("rows = %v after a refusal, want %v", got, rows)
			}
		})
	}
}

// A milestone assignment in Milestones moves the nib out of the Backlog into
// the milestone's section, and the cursor goes with it.
func TestAssigningAMilestoneMovesTheNibToItsSection(t *testing.T) {
	app, stub := setupGroupedApp(t)
	stub.ApplyAssignments = true
	pressKeys(app, "v m")
	if !focusOn(app, "t3") {
		t.Fatal("premise failed: t3 is not in the list")
	}
	if rows := rowIDs(app); slices.Index(rows, "t3") < slices.Index(rows, backlogRow) {
		t.Fatalf("premise failed: t3 is not in the Backlog: %v", rows)
	}

	pressKeys(app, "m m")
	key := acceleratorOf(app.assignPicker.View(), "Launch")
	if key == "" {
		t.Fatalf("premise failed: the milestone picker shows no Launch row:\n%s", stripAnsi(app.assignPicker.View()))
	}
	pressKeys(app, key)

	rows := rowIDs(app)
	if at := slices.Index(rows, "t3"); at < slices.Index(rows, "ms1") || at > slices.Index(rows, backlogRow) {
		t.Errorf("rows = %v, want t3 in the section of ms1", rows)
	}
	if got := selectedID(app); got != "t3" {
		t.Errorf("the cursor is on %q, want t3", got)
	}
	if app.list.level != viewshape.LevelMilestones {
		t.Errorf("the list shows %q, want Milestones", app.list.level)
	}
}

// An area row shows its whole path, so a nested area reads apart from a root
// of the same name.
func TestTheAreaPickerShowsEveryDeclaredPath(t *testing.T) {
	app, _ := setupModifyApp(t)
	pressKeys(app, "m a")
	view := stripAnsi(app.assignPicker.View())
	for _, path := range []string{"(No area)", "api", "api/webhooks", "docs"} {
		if acceleratorOf(view, path) == "" {
			t.Errorf("no row reads %q:\n%s", path, view)
		}
	}
}

// Every milestone is offered, a closed one with its status, as on the web.
func TestTheMilestonePickerOffersEveryMilestone(t *testing.T) {
	nibs := modifyNibs()
	nibs[4].Status = "completed"
	app, _ := setupTestApp(t, nibs)
	pressKeys(app, "m m")
	view := stripAnsi(app.assignPicker.View())
	for _, title := range []string{"(No milestone)", "Launch", "Beta"} {
		if acceleratorOf(view, title) == "" {
			t.Errorf("no row reads %q:\n%s", title, view)
		}
	}
	if !regexpLine(view, `Beta.*completed`) {
		t.Errorf("the completed milestone does not show its status:\n%s", view)
	}
	if regexpLine(view, `\bTwo\b|\bThree\b`) {
		t.Errorf("the picker offers a nib that is not a milestone:\n%s", view)
	}
}

// A milestone title wider than the picker is cut to one line, so the rows
// below it keep their places.
func TestALongMilestoneTitleKeepsEachPickerRowOnOneLine(t *testing.T) {
	nibs := modifyNibs()
	nibs[3].Title = strings.Repeat("a very long milestone title ", 5) + "end"
	app, _ := setupTestApp(t, nibs)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	pressKeys(app, "m")
	assertNextRow(t, stripAnsi(app.modifyPicker.View()), " milestone ", " area ")

	pressKeys(app, "m")
	view := stripAnsi(app.assignPicker.View())
	assertNextRow(t, view, "(No milestone)", "a very long")
	assertNextRow(t, view, "a very long", "Beta")
}

// assertNextRow fails unless the line after the first line of view holding row
// holds next.
func assertNextRow(t *testing.T, view, row, next string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, row) {
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], next) {
				t.Errorf("the line after %q does not hold %q:\n%s", row, next, view)
			}
			return
		}
	}
	t.Errorf("no line holds %q:\n%s", row, view)
}
