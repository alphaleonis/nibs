package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestEstimatePickerModel(t *testing.T) {
	cfg := config.Default()

	t.Run("pre-selects current estimate", func(t *testing.T) {
		m := newEstimatePickerModel(
			[]string{"nib-1"}, "Test Nib", "l",
			cfg, 80, 24,
		)

		selected, ok := m.list.SelectedItem().(estimateItem)
		if !ok {
			t.Fatal("no item selected")
		}
		if selected.name != "l" {
			t.Errorf("got selected %q, want %q", selected.name, "l")
		}
		if !selected.isCurrent {
			t.Error("selected item should have isCurrent=true")
		}
	})

	t.Run("shows all estimate options", func(t *testing.T) {
		m := newEstimatePickerModel(
			[]string{"nib-1"}, "Test Nib", "",
			cfg, 80, 24,
		)

		items := m.list.Items()
		if len(items) != len(config.DefaultEstimates) {
			t.Fatalf("expected %d items, got %d", len(config.DefaultEstimates), len(items))
		}

		for i, est := range config.DefaultEstimates {
			item, ok := items[i].(estimateItem)
			if !ok {
				t.Fatalf("item %d is not estimateItem", i)
			}
			if item.name != est.Name {
				t.Errorf("item %d: got name %q, want %q", i, item.name, est.Name)
			}
			if item.description != est.Description {
				t.Errorf("item %d: got description %q, want %q", i, item.description, est.Description)
			}
		}
	})

	// Regression test for the picker "jump" bug (mirrors statuspicker_test.go):
	// the modal must render at a constant height no matter which estimate is
	// selected. The stabilizing logic lives in the shared reservePickerDescription
	// helper, so this guards the estimate picker's wiring into it.
	t.Run("modal height is constant across all estimate selections", func(t *testing.T) {
		const w, h = 80, 24

		heightByEstimate := make(map[string]int, len(config.DefaultEstimates))
		for _, e := range config.DefaultEstimates {
			m := newEstimatePickerModel(
				[]string{"nib-1"}, "Test Nib", e.Name,
				cfg, w, h,
			)
			view := m.View()
			if view == "Loading..." {
				t.Fatalf("estimate %q: View() returned Loading...; width was not applied", e.Name)
			}
			heightByEstimate[e.Name] = lipgloss.Height(view)
		}

		want := heightByEstimate[config.DefaultEstimates[0].Name]
		for _, e := range config.DefaultEstimates {
			if got := heightByEstimate[e.Name]; got != want {
				t.Errorf("estimate %q: modal height = %d, want %d (height must be constant across selections)", e.Name, got, want)
			}
		}
	})

	t.Run("enter sends estimateSelectedMsg", func(t *testing.T) {
		m := newEstimatePickerModel(
			[]string{"nib-1"}, "Test Nib", "m",
			cfg, 80, 24,
		)

		// Press enter on the pre-selected item ("m")
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected a command from enter key")
		}

		msg := cmd()
		selected, ok := msg.(estimateSelectedMsg)
		if !ok {
			t.Fatalf("expected estimateSelectedMsg, got %T", msg)
		}
		if selected.estimate != "m" {
			t.Errorf("got estimate %q, want %q", selected.estimate, "m")
		}
		if len(selected.nibIDs) != 1 || selected.nibIDs[0] != "nib-1" {
			t.Errorf("got nibIDs %v, want [nib-1]", selected.nibIDs)
		}
	})

	t.Run("esc sends closeEstimatePickerMsg", func(t *testing.T) {
		m := newEstimatePickerModel(
			[]string{"nib-1"}, "Test Nib", "",
			cfg, 80, 24,
		)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
		if cmd == nil {
			t.Fatal("expected a command from esc key")
		}

		msg := cmd()
		if _, ok := msg.(closeEstimatePickerMsg); !ok {
			t.Fatalf("expected closeEstimatePickerMsg, got %T", msg)
		}
	})
}

func TestAppEstimatePickerWiring(t *testing.T) {
	newTestApp := func() (*App, *StubBackend) {
		stub := &StubBackend{
			Nibs: map[string]*nib.Nib{
				"nib-1": {ID: "nib-1", Title: "Test", Status: "todo", Type: "task", Estimate: "m"},
			},
			AllNibs: []*nib.Nib{
				{ID: "nib-1", Title: "Test", Status: "todo", Type: "task", Estimate: "m"},
			},
		}
		return New(stub, config.Default()), stub
	}

	t.Run("openEstimatePickerMsg transitions to picker state", func(t *testing.T) {
		app, _ := newTestApp()
		updated, _ := app.Update(openEstimatePickerMsg{
			nibIDs:          []string{"nib-1"},
			nibTitle:        "Test",
			currentEstimate: "m",
		})
		a := updated.(*App)
		if a.state != viewEstimatePicker {
			t.Errorf("expected viewEstimatePicker state, got %d", a.state)
		}
	})

	t.Run("estimateSelectedMsg calls UpdateNib with Estimate", func(t *testing.T) {
		app, stub := newTestApp()
		// Set up precondition: open picker first
		app.Update(openEstimatePickerMsg{
			nibIDs:          []string{"nib-1"},
			nibTitle:        "Test",
			currentEstimate: "m",
		})
		stub.UpdateCalls = nil
		updated, _ := app.Update(estimateSelectedMsg{
			nibIDs:   []string{"nib-1"},
			estimate: "xl",
		})
		a := updated.(*App)
		if a.state == viewEstimatePicker {
			t.Error("expected state to return from picker")
		}

		if len(stub.UpdateCalls) != 1 {
			t.Fatalf("expected 1 UpdateNib call, got %d", len(stub.UpdateCalls))
		}
		call := stub.UpdateCalls[0]
		if call.ID != "nib-1" {
			t.Errorf("got ID %q, want %q", call.ID, "nib-1")
		}
		if est, ok := call.Input.Estimate.ValueOK(); !ok || est == nil || *est != "xl" {
			t.Errorf("got Estimate %v, want ptr to %q", call.Input.Estimate, "xl")
		}
	})

	t.Run("detail view E key triggers openEstimatePickerMsg", func(t *testing.T) {
		app, stub := newTestApp()
		cfg := config.Default()
		app.state = viewDetail
		testNib := &nib.Nib{ID: "nib-1", Title: "Test", Status: "todo", Type: "task", Estimate: "m"}
		app.detail = newDetailModel(testNib, stub, cfg, 80, 24)

		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
		if cmd == nil {
			t.Fatal("expected a command from E key in detail view")
		}

		msg := cmd()
		openMsg, ok := msg.(openEstimatePickerMsg)
		if !ok {
			t.Fatalf("expected openEstimatePickerMsg, got %T", msg)
		}
		if len(openMsg.nibIDs) != 1 || openMsg.nibIDs[0] != "nib-1" {
			t.Errorf("got nibIDs %v, want [nib-1]", openMsg.nibIDs)
		}
		if openMsg.currentEstimate != "m" {
			t.Errorf("got currentEstimate %q, want %q", openMsg.currentEstimate, "m")
		}
	})

	t.Run("closeEstimatePickerMsg returns to previous state", func(t *testing.T) {
		app, _ := newTestApp()
		app.state = viewList
		updated, _ := app.Update(openEstimatePickerMsg{
			nibIDs:          []string{"nib-1"},
			nibTitle:        "Test",
			currentEstimate: "m",
		})
		app = updated.(*App)
		if app.state != viewEstimatePicker {
			t.Fatal("precondition: expected viewEstimatePicker after open")
		}

		updated, _ = app.Update(closeEstimatePickerMsg{})
		a := updated.(*App)
		if a.state != viewList {
			t.Errorf("expected viewList after close, got %d", a.state)
		}
	})
}
