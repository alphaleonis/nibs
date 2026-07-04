package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
			if item.isArchive != s.Archive {
				t.Errorf("item %d (%s): got isArchive %v, want %v", i, s.Name, item.isArchive, s.Archive)
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
		// deferred is parked, not archived — it must not render dimmed.
		if deferred.isArchive {
			t.Error("deferred item should have isArchive=false (non-archive)")
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
