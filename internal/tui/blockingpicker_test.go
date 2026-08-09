package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// newTestBlockingPicker builds a blocking picker over two candidate targets,
// with the picker's own nib excluded from the list by the constructor.
func newTestBlockingPicker(t *testing.T, currentBlocking []string) blockingPickerModel {
	t.Helper()

	nibs := []*nib.Nib{
		{ID: "src", Title: "Source", Type: "task", Status: "todo", Order: "1"},
		{ID: "tgt-a", Title: "Target A", Type: "task", Status: "todo", Order: "2"},
		{ID: "tgt-b", Title: "Target B", Type: "task", Status: "todo", Order: "3"},
	}
	nibMap := make(map[string]*nib.Nib, len(nibs))
	for _, n := range nibs {
		nibMap[n.ID] = n
	}
	stub := &StubBackend{Nibs: nibMap, AllNibs: nibs}

	return newBlockingPickerModel("src", "Source", currentBlocking, stub, config.Default(), 120, 40)
}

// selectItem moves the cursor onto the item with the given nib ID.
func selectItem(t *testing.T, m blockingPickerModel, id string) blockingPickerModel {
	t.Helper()
	for i := range m.list.Items() {
		m.list.Select(i)
		if item, ok := m.list.SelectedItem().(blockingItem); ok && item.nib.ID == id {
			return m
		}
	}
	t.Fatalf("item %q not in picker", id)
	return m
}

// The space bar toggles the highlighted target in and out of the pending set.
// Bubble Tea reports the space bar as "space" rather than " ", so this pins the
// label the picker actually matches on — a mismatch silently disables toggling
// while every other key in the picker keeps working.
func TestBlockingPicker_SpaceTogglesPendingTarget(t *testing.T) {
	m := newTestBlockingPicker(t, nil)
	m = selectItem(t, m, "tgt-a")

	if m.pendingBlocking["tgt-a"] {
		t.Fatal("tgt-a should not start pending")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !m.pendingBlocking["tgt-a"] {
		t.Error("space did not add tgt-a to the pending set")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.pendingBlocking["tgt-a"] {
		t.Error("second space did not remove tgt-a from the pending set")
	}
}

// Enter reports the pending set as a diff against the original blocking set,
// so a target toggled with space has to reach the confirmation message.
func TestBlockingPicker_SpaceThenEnterReportsTheDiff(t *testing.T) {
	m := newTestBlockingPicker(t, []string{"tgt-b"})
	m = selectItem(t, m, "tgt-a")

	m, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = selectItem(t, m, "tgt-b")
	m, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(blockingConfirmedMsg)
	if !ok {
		t.Fatalf("expected blockingConfirmedMsg, got %T", cmd())
	}

	if len(msg.toAdd) != 1 || msg.toAdd[0] != "tgt-a" {
		t.Errorf("toAdd = %v, want [tgt-a]", msg.toAdd)
	}
	if len(msg.toRemove) != 1 || msg.toRemove[0] != "tgt-b" {
		t.Errorf("toRemove = %v, want [tgt-b]", msg.toRemove)
	}
}
