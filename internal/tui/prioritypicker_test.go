package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alphaleonis/nibs/internal/config"
)

func TestPriorityPickerModel(t *testing.T) {
	cfg := config.Default()

	t.Run("pre-selects current priority", func(t *testing.T) {
		m := newPriorityPickerModel(
			[]string{"nib-1"}, "Test Nib", "high",
			cfg, 80, 24,
		)

		selected, ok := m.list.SelectedItem().(priorityItem)
		if !ok {
			t.Fatal("no item selected")
		}
		if selected.name != "high" {
			t.Errorf("got selected %q, want %q", selected.name, "high")
		}
		if !selected.isCurrent {
			t.Error("selected item should have isCurrent=true")
		}
	})

	t.Run("shows all priority options", func(t *testing.T) {
		m := newPriorityPickerModel(
			[]string{"nib-1"}, "Test Nib", "",
			cfg, 80, 24,
		)

		items := m.list.Items()
		if len(items) != len(config.DefaultPriorities) {
			t.Fatalf("expected %d items, got %d", len(config.DefaultPriorities), len(items))
		}

		for i, p := range config.DefaultPriorities {
			item, ok := items[i].(priorityItem)
			if !ok {
				t.Fatalf("item %d is not priorityItem", i)
			}
			if item.name != p.Name {
				t.Errorf("item %d: got name %q, want %q", i, item.name, p.Name)
			}
			if item.description != p.Description {
				t.Errorf("item %d: got description %q, want %q", i, item.description, p.Description)
			}
		}
	})

	// Regression test for the picker "jump" bug (mirrors statuspicker_test.go):
	// the modal must render at a constant height no matter which priority is
	// selected. Priority descriptions differ in wrapped height at width 80
	// (e.g. "critical" wraps to more lines than "normal"), so without the
	// reserved-description fix the modal would grow and make list items jump.
	t.Run("modal height is constant across all priority selections", func(t *testing.T) {
		const w, h = 80, 24

		heightByPriority := make(map[string]int, len(config.DefaultPriorities))
		for _, p := range config.DefaultPriorities {
			m := newPriorityPickerModel(
				[]string{"nib-1"}, "Test Nib", p.Name,
				cfg, w, h,
			)
			view := m.View()
			if view == "Loading..." {
				t.Fatalf("priority %q: View() returned Loading...; width was not applied", p.Name)
			}
			heightByPriority[p.Name] = lipgloss.Height(view)
		}

		want := heightByPriority[config.DefaultPriorities[0].Name]
		for _, p := range config.DefaultPriorities {
			if got := heightByPriority[p.Name]; got != want {
				t.Errorf("priority %q: modal height = %d, want %d (height must be constant across selections)", p.Name, got, want)
			}
		}
	})

	t.Run("enter sends prioritySelectedMsg", func(t *testing.T) {
		m := newPriorityPickerModel(
			[]string{"nib-1"}, "Test Nib", "high",
			cfg, 80, 24,
		)

		// Press enter on the pre-selected item ("high").
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected a command from enter key")
		}

		msg := cmd()
		selected, ok := msg.(prioritySelectedMsg)
		if !ok {
			t.Fatalf("expected prioritySelectedMsg, got %T", msg)
		}
		if selected.priority != "high" {
			t.Errorf("got priority %q, want %q", selected.priority, "high")
		}
		if len(selected.nibIDs) != 1 || selected.nibIDs[0] != "nib-1" {
			t.Errorf("got nibIDs %v, want [nib-1]", selected.nibIDs)
		}
	})

	t.Run("esc sends closePriorityPickerMsg", func(t *testing.T) {
		m := newPriorityPickerModel(
			[]string{"nib-1"}, "Test Nib", "",
			cfg, 80, 24,
		)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
		if cmd == nil {
			t.Fatal("expected a command from esc key")
		}

		msg := cmd()
		if _, ok := msg.(closePriorityPickerMsg); !ok {
			t.Fatalf("expected closePriorityPickerMsg, got %T", msg)
		}
	})
}
