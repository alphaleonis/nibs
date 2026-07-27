package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alphaleonis/nibs/internal/config"
)

func TestStatusPickerModel(t *testing.T) {
	cfg := config.Default()

	t.Run("shows all config statuses", func(t *testing.T) {
		m := newStatusPickerModel(
			[]string{"nib-1"}, "Test Nib", "todo",
			cfg, 80, 24,
		)

		items := m.list.Items()
		if len(items) != len(config.DefaultStatuses) {
			t.Fatalf("expected %d items, got %d", len(config.DefaultStatuses), len(items))
		}

		for i, s := range config.DefaultStatuses {
			item, ok := items[i].(statusItem)
			if !ok {
				t.Fatalf("item %d is not statusItem", i)
			}
			if item.name != s.Name {
				t.Errorf("item %d: got name %q, want %q", i, item.name, s.Name)
			}
			if item.color != s.Color {
				t.Errorf("item %d (%s): got color %q, want %q", i, s.Name, item.color, s.Color)
			}
			if item.isClosed != s.Closed {
				t.Errorf("item %d (%s): got isClosed %v, want %v", i, s.Name, item.isClosed, s.Closed)
			}
		}
	})

	t.Run("offers deferred as a selectable option", func(t *testing.T) {
		m := newStatusPickerModel(
			[]string{"nib-1"}, "Test Nib", "todo",
			cfg, 80, 24,
		)

		var deferred *statusItem
		for _, li := range m.list.Items() {
			if item, ok := li.(statusItem); ok && item.name == "deferred" {
				captured := item
				deferred = &captured
				break
			}
		}
		if deferred == nil {
			t.Fatal("status picker does not offer \"deferred\"")
		}
		// deferred is parked, not closed — it must not render dimmed.
		if deferred.isClosed {
			t.Error("deferred item should have isClosed=false (open)")
		}
		// Config color for deferred is deliberately gray.
		if deferred.color != "gray" {
			t.Errorf("deferred color = %q, want %q", deferred.color, "gray")
		}
	})

	t.Run("pre-selects current status when deferred", func(t *testing.T) {
		m := newStatusPickerModel(
			[]string{"nib-1"}, "Test Nib", "deferred",
			cfg, 80, 24,
		)

		selected, ok := m.list.SelectedItem().(statusItem)
		if !ok {
			t.Fatal("no item selected")
		}
		if selected.name != "deferred" {
			t.Errorf("got selected %q, want %q", selected.name, "deferred")
		}
		if !selected.isCurrent {
			t.Error("selected item should have isCurrent=true")
		}
	})

	// Regression test for the picker "jump" bug: the modal must render at a
	// constant height no matter which status is selected. The trap is appending
	// the selected status's raw description with no reserved height — a status
	// with a longer, more-wrapped description (e.g. "deferred") then grows the
	// box and makes list items jump. We assert equal
	// lipgloss height across every status, and call out todo vs deferred
	// (shortest vs longest description) explicitly.
	t.Run("modal height is constant across all status selections", func(t *testing.T) {
		const w, h = 80, 24

		heightByStatus := make(map[string]int, len(config.DefaultStatuses))
		for _, s := range config.DefaultStatuses {
			m := newStatusPickerModel(
				[]string{"nib-1"}, "Test Nib", s.Name,
				cfg, w, h,
			)
			view := m.View()
			if view == "Loading..." {
				t.Fatalf("status %q: View() returned Loading...; width was not applied", s.Name)
			}
			heightByStatus[s.Name] = lipgloss.Height(view)
		}

		// Every selection must yield the same modal height.
		want := heightByStatus[config.DefaultStatuses[0].Name]
		for _, s := range config.DefaultStatuses {
			if got := heightByStatus[s.Name]; got != want {
				t.Errorf("status %q: modal height = %d, want %d (height must be constant across selections)", s.Name, got, want)
			}
		}

		// Explicit shortest-vs-longest check named in the bug report.
		if heightByStatus["todo"] != heightByStatus["deferred"] {
			t.Errorf("modal height jumps: todo = %d, deferred = %d (must be equal)", heightByStatus["todo"], heightByStatus["deferred"])
		}
	})

	t.Run("enter sends statusSelectedMsg for deferred", func(t *testing.T) {
		m := newStatusPickerModel(
			[]string{"nib-1"}, "Test Nib", "deferred",
			cfg, 80, 24,
		)

		// Pre-selected item is "deferred"; press enter.
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected a command from enter key")
		}

		msg := cmd()
		selected, ok := msg.(statusSelectedMsg)
		if !ok {
			t.Fatalf("expected statusSelectedMsg, got %T", msg)
		}
		if selected.status != "deferred" {
			t.Errorf("got status %q, want %q", selected.status, "deferred")
		}
		if len(selected.nibIDs) != 1 || selected.nibIDs[0] != "nib-1" {
			t.Errorf("got nibIDs %v, want [nib-1]", selected.nibIDs)
		}
	})
}
