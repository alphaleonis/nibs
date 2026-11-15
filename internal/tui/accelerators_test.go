package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// acceleratorOf returns the key the rendered view shows before the row that
// reads label, or "" when no row reading label shows one.
func acceleratorOf(view, label string) string {
	row := regexp.MustCompile(`([^\s▌│])\s+` + regexp.QuoteMeta(label) + `(?:\s|$)`)
	for _, line := range strings.Split(stripAnsi(view), "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// pickerCase opens one picker on nib-4 of pickerNibs and names its rows.
type pickerCase struct {
	name string
	ctx  keyContext
	open string // the keys that open the picker from the list
	view func(*App) string
	// rows returns how each row reads, top to bottom, after its accelerator.
	rows func(*App) []string
	// choose is the key that chooses the row under the cursor.
	choose string
}

// pickerNibs gives the tag picker more rows than the pool has keys before g,
// the first one an action takes in a picker.
func pickerNibs() []*nib.Nib {
	nibs := listKeysNibs()
	nibs[0].Tags = []string{"quux"}
	for i := range 20 {
		nibs[3].Tags = append(nibs[3].Tags, fmt.Sprintf("tag%02d", i))
	}
	return nibs
}

func namesOf[T any](items []T, name func(T) string) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = name(item)
	}
	return names
}

var pickerCases = []pickerCase{
	{
		name: "status", ctx: ctxStatusPicker, open: "s", choose: "enter",
		view: func(a *App) string { return a.statusPicker.View() },
		rows: func(*App) []string {
			return namesOf(config.Default().WorkflowStatuses(), func(s config.StatusConfig) string { return s.Name })
		},
	},
	{
		name: "type", ctx: ctxTypePicker, open: "t", choose: "enter",
		view: func(a *App) string { return a.typePicker.View() },
		rows: func(a *App) []string {
			return namesOf(a.typePicker.items, func(i typeItem) string { return i.name })
		},
	},
	{
		name: "create type", ctx: ctxTypePicker, open: "n", choose: "enter",
		view: func(a *App) string { return a.typePicker.View() },
		rows: func(a *App) []string {
			return namesOf(a.typePicker.items, func(i typeItem) string { return i.name })
		},
	},
	{
		name: "priority", ctx: ctxPriorityPicker, open: "m p", choose: "enter",
		view: func(a *App) string { return a.priorityPicker.View() },
		rows: func(*App) []string {
			return namesOf(config.DefaultPriorities, func(p config.PriorityConfig) string { return p.Name })
		},
	},
	{
		name: "estimate", ctx: ctxEstimatePicker, open: "m e", choose: "enter",
		view: func(a *App) string { return a.estimatePicker.View() },
		rows: func(*App) []string {
			return namesOf(config.DefaultEstimates, func(e config.EstimateConfig) string { return e.Name })
		},
	},
	{
		name: "parent", ctx: ctxParentPicker, open: "p", choose: "enter",
		view: func(a *App) string { return a.parentPicker.View() },
		rows: func(a *App) []string {
			var rows []string
			for _, item := range a.parentPicker.list.VisibleItems() {
				switch item := item.(type) {
				case clearParentItem:
					rows = append(rows, item.Title())
				case parentItem:
					rows = append(rows, item.nib.EffectiveType()+" "+item.nib.Title)
				}
			}
			return rows
		},
	},
	{
		name: "blocking", ctx: ctxBlockingPicker, open: "b", choose: "space",
		view: func(a *App) string { return a.blockingPicker.View() },
		rows: func(a *App) []string {
			var rows []string
			for _, item := range a.blockingPicker.list.VisibleItems() {
				n := item.(blockingItem).nib
				rows = append(rows, "○ "+n.EffectiveType()+" "+n.Title)
			}
			return rows
		},
	},
	{
		name: "tag", ctx: ctxTagPicker, open: "g t", choose: "enter",
		view: func(a *App) string { return a.tagPicker.View() },
		rows: func(a *App) []string {
			var rows []string
			for _, item := range a.tagPicker.list.VisibleItems() {
				rows = append(rows, item.(tagItem).tag)
			}
			return rows
		},
	},
	{
		name: "modify", ctx: ctxModifyPicker, open: "m", choose: "enter",
		view: func(a *App) string { return a.modifyPicker.View() },
		rows: func(*App) []string { return []string{"priority", "estimate"} },
	},
}

// openPicker returns an app with tc's picker open on nib-4.
func openPicker(t *testing.T, tc pickerCase) (*App, *StubBackend) {
	t.Helper()
	app, stub := setupTestApp(t, pickerNibs())
	if !focusOn(app, "nib-4") {
		t.Fatal("premise failed: nib-4 is not in the list")
	}
	pressKeys(app, tc.open)
	if got := app.keyContext(); got != tc.ctx {
		t.Fatalf("premise failed: %q opened the %s, want the %s", tc.open, got, tc.ctx)
	}
	return app, stub
}

// Every row shows a key, no two rows show the same one, and none is a key an
// action takes in the picker, which would leave the row unreachable.
func TestEveryPickerShowsAnAcceleratorOnEachRowThatNoActionTakes(t *testing.T) {
	for _, tc := range pickerCases {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := openPicker(t, tc)
			view := tc.view(app)
			rows := tc.rows(app)
			if len(rows) == 0 {
				t.Fatal("premise failed: the picker has no rows")
			}
			var shown []action
			for _, row := range rows {
				key := acceleratorOf(view, row)
				if key == "" {
					t.Errorf("row %q shows no accelerator:\n%s", row, stripAnsi(view))
					continue
				}
				shown = append(shown, action{Name: actionName("row " + row), Contexts: []keyContext{tc.ctx}, Keys: []string{key}})
			}
			if errs := keymapConflicts(append(shown, actions...)); len(errs) > 0 {
				t.Errorf("%d accelerator conflict(s):\n%s", len(errs), strings.Join(errs, "\n"))
			}
		})
	}
}

// pickerOutcome is what choosing a row changed.
type pickerOutcome struct {
	State       viewState
	Updates     []stubUpdateCall
	Parents     []stubSetParentCall
	Blocks      []stubBlockingCall
	TagFilter   string
	CreateType  string
	PriorityFor []string
}

func outcomeOf(app *App, stub *StubBackend) pickerOutcome {
	o := pickerOutcome{
		State:     app.state,
		Updates:   stub.UpdateCalls,
		Parents:   stub.SetParentCalls,
		Blocks:    stub.AddBlockingCalls,
		TagFilter: app.list.tagFilter,
	}
	if app.state == viewCreateModal {
		o.CreateType = app.createModal.nibType
	}
	if app.state == viewPriorityPicker {
		o.PriorityFor = app.priorityPicker.nibIDs
	}
	return o
}

func TestChoosingByAcceleratorIsMovingToTheRowAndChoosingIt(t *testing.T) {
	for _, tc := range pickerCases {
		app, _ := openPicker(t, tc)
		view, rows := tc.view(app), tc.rows(app)
		for i, row := range rows {
			t.Run(fmt.Sprintf("%s/%s", tc.name, row), func(t *testing.T) {
				key := acceleratorOf(view, row)
				if key == "" {
					t.Fatalf("premise failed: row %q shows no accelerator", row)
				}
				// A multi-select picker's accelerator marks the row, so both
				// sides confirm afterwards.
				confirm := ""
				if tc.choose != "enter" {
					confirm = " enter"
				}

				byKey, byKeyStub := openPicker(t, tc)
				pressKeys(byKey, key+confirm)

				byMove, byMoveStub := openPicker(t, tc)
				pressKeys(byMove, "home"+strings.Repeat(" down", i)+" "+tc.choose+confirm)

				got, want := outcomeOf(byKey, byKeyStub), outcomeOf(byMove, byMoveStub)
				if want.State == openedState(tc) {
					t.Fatalf("premise failed: choosing %q by moving changed nothing", row)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("accelerator %q:\n got %+v\nwant %+v", key, got, want)
				}
			})
		}
	}
}

// openedState is the view tc's picker opens in.
func openedState(tc pickerCase) viewState {
	switch tc.ctx {
	case ctxStatusPicker:
		return viewStatusPicker
	case ctxParentPicker:
		return viewParentPicker
	case ctxBlockingPicker:
		return viewBlockingPicker
	case ctxPriorityPicker:
		return viewPriorityPicker
	case ctxEstimatePicker:
		return viewEstimatePicker
	case ctxTagPicker:
		return viewTagPicker
	case ctxModifyPicker:
		return viewModifyPicker
	}
	return viewTypePicker
}

func TestAPickerFilterBeingTypedTakesAcceleratorKeys(t *testing.T) {
	tc := pickerCases[0]
	app, stub := openPicker(t, tc)
	key := acceleratorOf(tc.view(app), "draft")
	if key == "" {
		t.Fatal("premise failed: draft shows no accelerator")
	}
	pressKeys(app, "/ "+key)
	if got := app.statusPicker.list.FilterValue(); got != key {
		t.Errorf("filter text = %q after typing %s, want %q", got, key, key)
	}
	if app.state != viewStatusPicker || len(stub.UpdateCalls) != 0 {
		t.Errorf("typing %s into the filter chose a row: state %d, updates %v", key, app.state, stub.UpdateCalls)
	}
}
