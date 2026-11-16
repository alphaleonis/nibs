package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// typeAhead sends each key to Update and drops the commands it returns, as
// when keys are queued before any command's message arrives.
func typeAhead(app *App, keys string) {
	for _, k := range strings.Fields(keys) {
		app.Update(keyPress(k))
	}
}

// A key typed right after the key that opens a picker reaches that picker,
// even when no command has run in between.
func TestAKeyTypedAheadReachesThePickerTheKeyBeforeOpened(t *testing.T) {
	tests := []struct {
		keys string
		want keyContext
	}{
		{keys: "m a", want: ctxAreaPicker},
		{keys: "m m", want: ctxMilestonePicker},
		{keys: "m p", want: ctxPriorityPicker},
		{keys: "m e", want: ctxEstimatePicker},
		{keys: "s t", want: ctxStatusPicker},
		{keys: "t m", want: ctxTypePicker},
	}
	for _, tt := range tests {
		t.Run(tt.keys, func(t *testing.T) {
			app, stub := setupModifyApp(t)
			typeAhead(app, tt.keys)
			if got := app.keyContext(); got != tt.want {
				t.Errorf("%q left the %s, want the %s", tt.keys, got, tt.want)
			}
			if app.editingNibID != "" || len(stub.ArchiveCalls) != 0 {
				t.Errorf("%q reached the list: editing %q, archived %v", tt.keys, app.editingNibID, stub.ArchiveCalls)
			}
		})
	}
}

// The nib view opens its pickers the same way.
func TestAKeyTypedAheadInTheNibViewReachesThePicker(t *testing.T) {
	app, _ := setupModifyApp(t)
	pressKeys(app, "enter")
	if app.state != viewDetail {
		t.Fatalf("premise failed: state = %d after enter, want the nib view", app.state)
	}
	typeAhead(app, "m a")
	if got := app.keyContext(); got != ctxAreaPicker {
		t.Errorf("m a in the nib view left the %s, want the area picker", got)
	}
}

// Canceling a picker that opened over another modal returns to the view under
// both, and never to the modal, which a second cancel would return to again.
func TestCancelingAPickerOpenedOverAModalReturnsToTheList(t *testing.T) {
	app, _ := setupModifyApp(t)
	pressKeys(app, "m")
	if app.state != viewModifyPicker {
		t.Fatalf("premise failed: state = %d after m, want the modify picker", app.state)
	}
	app.Update(openStatusPickerMsg{nibIDs: []string{"nib-1"}, nibTitle: "One"})
	if app.state != viewStatusPicker {
		t.Fatalf("premise failed: state = %d, want the status picker", app.state)
	}
	pressKeys(app, "esc")
	if app.state != viewList {
		t.Errorf("state = %d after esc, want the list", app.state)
	}
}

// A key pressed twice, as a double tap or a key repeat sends it, opens the
// picker and chooses nothing in it.
func TestTheKeyThatOpensAPickerChoosesNoRowInIt(t *testing.T) {
	tests := []struct {
		name string
		user string // the user config
		keys string
		want viewState
	}{
		{name: "status", keys: "s s", want: viewStatusPicker},
		{name: "type", keys: "t t", want: viewTypePicker},
		{name: "rebound status", user: "tui:\n  keys:\n    nib.status: c\n", keys: "c c", want: viewStatusPicker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTestAppWithUserConfig(t, modifyNibs(), config.Default(), tt.user)
			stub := app.backend.(displayBackend).inner.(*StubBackend)
			pressKeys(app, tt.keys)
			if app.state != tt.want {
				t.Errorf("%q left state %d, want %d", tt.keys, app.state, tt.want)
			}
			if len(stub.UpdateCalls) != 0 {
				t.Errorf("%q wrote %+v, want nothing", tt.keys, stub.UpdateCalls)
			}
		})
	}
}

// The key that chose a field in the modify picker chooses no row in that
// field's picker either.
func TestAFieldPickerOpenedFromModifyFreesItsKey(t *testing.T) {
	for _, row := range []struct {
		label string
		ctx   keyContext
	}{
		{"priority", ctxPriorityPicker}, {"estimate", ctxEstimatePicker},
		{"milestone", ctxMilestonePicker}, {"area", ctxAreaPicker},
	} {
		key := modifyRowKey(defaultActions, row.label)
		if key == "" {
			t.Fatalf("premise failed: the modify picker gives %s no key", row.label)
		}
		if slices.Contains(freeAccelerators(defaultActions, row.ctx), key) {
			t.Errorf("the %s offers %q, which chose it in the modify picker", row.ctx, key)
		}
	}
}

// A link followed in a burst opens before the next key, which then acts on the
// nib the link leads to, and back returns to the nib the link was followed from.
func TestAKeyTypedAfterFollowingALinkActsOnTheLinkedNib(t *testing.T) {
	app := detailOnLinkedNib(t, 2, 120, 40)
	if !app.detail.linksActive {
		t.Fatal("premise failed: the links pane is not focused")
	}
	target := app.detail.linkList.SelectedItem().(linkItem).link.nib.ID

	typeAhead(app, "enter s")

	if app.state != viewStatusPicker || !slices.Equal(app.statusPicker.nibIDs, []string{target}) {
		t.Fatalf("enter s left state %d on %v, want the status picker on %s", app.state, app.statusPicker.nibIDs, target)
	}
	pressKeys(app, "esc backspace")
	if app.state != viewDetail || app.detail.nib.ID != "hub" {
		t.Errorf("esc backspace left state %d on %s, want the nib view on hub", app.state, app.detail.nib.ID)
	}
}
