package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/config"
)

func TestTypePickerModel(t *testing.T) {
	cfg := config.Default()

	t.Run("pre-selects current type", func(t *testing.T) {
		m := newTypePickerModel(
			[]string{"nib-1"}, "Test Nib", "bug", nil,
			cfg, 80, 24,
		)

		selected, ok := m.SelectedItem()
		if !ok {
			t.Fatal("no item selected")
		}
		if selected.name != "bug" {
			t.Errorf("got selected %q, want %q", selected.name, "bug")
		}
		if !selected.isCurrent {
			t.Error("selected item should have isCurrent=true")
		}
	})

	t.Run("shows all type options", func(t *testing.T) {
		m := newTypePickerModel(
			[]string{"nib-1"}, "Test Nib", "", nil,
			cfg, 80, 24,
		)

		if len(m.items) != len(config.DefaultTypes) {
			t.Fatalf("expected %d items, got %d", len(config.DefaultTypes), len(m.items))
		}

		for i, ty := range config.DefaultTypes {
			if m.items[i].name != ty.Name {
				t.Errorf("item %d: got name %q, want %q", i, m.items[i].name, ty.Name)
			}
			if m.items[i].description != ty.Description {
				t.Errorf("item %d: got description %q, want %q", i, m.items[i].description, ty.Description)
			}
		}
	})

	// Regression test for the picker "jump" bug (mirrors statuspicker_test.go):
	// the modal must render at a constant height no matter which type is
	// selected. Type descriptions differ in wrapped height at width 80 (e.g.
	// "epic"/"task" wrap to more lines than "bug"), so without the reserved-
	// description fix the modal would grow and make list items jump. The loop is
	// name-agnostic over every configured type.
	t.Run("modal height is constant across all type selections", func(t *testing.T) {
		const w, h = 80, 24

		heightByType := make(map[string]int, len(config.DefaultTypes))
		for _, ty := range config.DefaultTypes {
			m := newTypePickerModel(
				[]string{"nib-1"}, "Test Nib", ty.Name, nil,
				cfg, w, h,
			)
			view := m.View()
			if view == "Loading..." {
				t.Fatalf("type %q: View() returned Loading...; width was not applied", ty.Name)
			}
			heightByType[ty.Name] = lipgloss.Height(view)
		}

		want := heightByType[config.DefaultTypes[0].Name]
		for _, ty := range config.DefaultTypes {
			if got := heightByType[ty.Name]; got != want {
				t.Errorf("type %q: modal height = %d, want %d (height must be constant across selections)", ty.Name, got, want)
			}
		}
	})

	t.Run("enter sends typeSelectedMsg", func(t *testing.T) {
		m := newTypePickerModel(
			[]string{"nib-1"}, "Test Nib", "bug", nil,
			cfg, 80, 24,
		)

		// Press enter on the pre-selected item ("bug").
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected a command from enter key")
		}

		msg := cmd()
		selected, ok := msg.(typeSelectedMsg)
		if !ok {
			t.Fatalf("expected typeSelectedMsg, got %T", msg)
		}
		if selected.nibType != "bug" {
			t.Errorf("got type %q, want %q", selected.nibType, "bug")
		}
		if len(selected.nibIDs) != 1 || selected.nibIDs[0] != "nib-1" {
			t.Errorf("got nibIDs %v, want [nib-1]", selected.nibIDs)
		}
	})

	t.Run("esc sends closeTypePickerMsg", func(t *testing.T) {
		m := newTypePickerModel(
			[]string{"nib-1"}, "Test Nib", "", nil,
			cfg, 80, 24,
		)

		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd == nil {
			t.Fatal("expected a command from esc key")
		}

		msg := cmd()
		if _, ok := msg.(closeTypePickerMsg); !ok {
			t.Fatalf("expected closeTypePickerMsg, got %T", msg)
		}
	})
}
