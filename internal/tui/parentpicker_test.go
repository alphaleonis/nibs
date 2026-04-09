package tui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func makeTestConfig() *config.Config {
	return config.Default()
}

func makeNib(id, title, nibType, status, parent string) *nib.Nib {
	return &nib.Nib{
		ID:     id,
		Title:  title,
		Type:   nibType,
		Status: status,
		Parent: parent,
	}
}

func TestParentPicker_HidesCompletedByDefault(t *testing.T) {
	backend := &StubBackend{
		AllNibs: []*nib.Nib{
			makeNib("nib-1", "Active Epic", "epic", "todo", ""),
			makeNib("nib-2", "Done Epic", "epic", "completed", ""),
			makeNib("nib-3", "Scrapped Epic", "epic", "scrapped", ""),
			makeNib("nib-4", "In Progress Epic", "epic", "in-progress", ""),
		},
	}
	cfg := makeTestConfig()

	m := newParentPickerModel(
		[]string{"nib-child"}, "Child Task", []string{"task"}, "",
		backend, cfg, 80, 24,
	)

	// Collect visible parent items (excluding clearParentItem)
	var visibleIDs []string
	for _, item := range m.list.Items() {
		if pi, ok := item.(parentItem); ok {
			visibleIDs = append(visibleIDs, pi.nib.ID)
		}
	}

	// Should include active nibs but NOT completed/scrapped
	if len(visibleIDs) != 2 {
		t.Fatalf("expected 2 visible parents, got %d: %v", len(visibleIDs), visibleIDs)
	}
	for _, id := range visibleIDs {
		if id == "nib-2" || id == "nib-3" {
			t.Errorf("completed/scrapped nib %s should be hidden by default", id)
		}
	}

	// Title should indicate hiding is active
	if !strings.Contains(m.list.Title, "hiding completed") {
		t.Errorf("list title should indicate completed nibs are hidden, got %q", m.list.Title)
	}
}

func parentItemIDs(m parentPickerModel) []string {
	var ids []string
	for _, item := range m.list.Items() {
		if pi, ok := item.(parentItem); ok {
			ids = append(ids, pi.nib.ID)
		}
	}
	return ids
}

func TestParentPicker_ToggleShowsCompleted(t *testing.T) {
	backend := &StubBackend{
		AllNibs: []*nib.Nib{
			makeNib("nib-1", "Active Epic", "epic", "todo", ""),
			makeNib("nib-2", "Done Epic", "epic", "completed", ""),
			makeNib("nib-3", "Scrapped Epic", "epic", "scrapped", ""),
		},
	}
	cfg := makeTestConfig()

	m := newParentPickerModel(
		[]string{"nib-child"}, "Child Task", []string{"task"}, "",
		backend, cfg, 80, 24,
	)

	// Default: only active nibs
	ids := parentItemIDs(m)
	if len(ids) != 1 {
		t.Fatalf("before toggle: expected 1 visible, got %d: %v", len(ids), ids)
	}

	// Toggle to show completed
	m.toggleHideCompleted()
	ids = parentItemIDs(m)
	if len(ids) != 3 {
		t.Fatalf("after toggle (show all): expected 3 visible, got %d: %v", len(ids), ids)
	}
	if strings.Contains(m.list.Title, "hiding completed") {
		t.Error("list title should not indicate hiding after toggle to show all")
	}

	// Toggle back to hide
	m.toggleHideCompleted()
	ids = parentItemIDs(m)
	if len(ids) != 1 {
		t.Fatalf("after toggle (hide again): expected 1 visible, got %d: %v", len(ids), ids)
	}
	if !strings.Contains(m.list.Title, "hiding completed") {
		t.Error("list title should indicate hiding after toggle back")
	}
}

func TestParentPicker_ClearParentAlwaysPresent(t *testing.T) {
	backend := &StubBackend{
		AllNibs: []*nib.Nib{
			makeNib("nib-1", "Done Epic", "epic", "completed", ""),
		},
	}
	cfg := makeTestConfig()

	m := newParentPickerModel(
		[]string{"nib-child"}, "Child Task", []string{"task"}, "",
		backend, cfg, 80, 24,
	)

	// With hideCompleted=true and only completed nibs, list should still have "(No Parent)"
	items := m.list.Items()
	if len(items) != 1 {
		t.Fatalf("expected 1 item (clearParentItem), got %d", len(items))
	}
	if _, ok := items[0].(clearParentItem); !ok {
		t.Error("first item should be clearParentItem")
	}
}
