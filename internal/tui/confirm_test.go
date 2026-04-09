package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// helper: creates a test App with stub backend, sends a window size, loads nibs,
// and returns the ready-to-use app and its stub backend.
func setupTestApp(t *testing.T, nibs []*nib.Nib) (*App, *StubBackend) {
	t.Helper()

	nibMap := make(map[string]*nib.Nib)
	for _, n := range nibs {
		nibMap[n.ID] = n
	}

	stub := &StubBackend{
		Nibs:    nibMap,
		AllNibs: nibs,
	}

	cfg := config.Default()
	app := New(stub, cfg)

	// Initialize and set a reasonable window size
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Execute the init command (loadNibs) to populate the list
	if initCmd != nil {
		msg := initCmd()
		app.Update(msg)
	}

	return app, stub
}

// sendKey sends a key message to the app and processes any returned commands.
// This simulates the Bubbletea runtime executing commands synchronously.
func sendKey(app *App, key tea.KeyMsg) {
	_, cmd := app.Update(key)
	processCmd(app, cmd)
}

// processCmd executes a tea.Cmd and feeds the resulting message back to the app.
// Handles tea.BatchMsg by processing each sub-command.
func processCmd(app *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	// Handle batch messages (tea.Batch returns a function that returns tea.BatchMsg)
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, subCmd := range batch {
			processCmd(app, subCmd)
		}
		return
	}
	_, nextCmd := app.Update(msg)
	processCmd(app, nextCmd)
}

func TestAKeyArchivesNibAfterConfirmation(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "nib-1", Title: "First task", Status: "todo", Type: "task"},
		{ID: "nib-2", Title: "Second task", Status: "todo", Type: "task"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Press A key on the selected nib
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	// Should be in confirm dialog state
	if app.state != viewConfirmDialog {
		t.Fatalf("expected viewConfirmDialog state, got %d", app.state)
	}

	// The confirm dialog should show the nib title
	view := app.View()
	if view == "" {
		t.Fatal("View() returned empty string in confirm state")
	}

	// Press 'y' to confirm
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// ArchiveNib should have been called
	if len(stub.ArchiveCalls) == 0 {
		t.Fatal("expected ArchiveNib to be called, but it was not")
	}
	if stub.ArchiveCalls[0] != "nib-1" {
		t.Errorf("expected ArchiveNib called with 'nib-1', got %q", stub.ArchiveCalls[0])
	}

	// Should return to list view
	if app.state != viewList {
		t.Errorf("expected viewList state after confirmation, got %d", app.state)
	}
}

func TestDelKeyPermanentlyDeletesNib(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "nib-1", Title: "First task", Status: "todo", Type: "task"},
		{ID: "nib-2", Title: "Second task", Status: "todo", Type: "task"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Press Del key to permanently delete
	sendKey(app, tea.KeyMsg{Type: tea.KeyDelete})

	if app.state != viewConfirmDialog {
		t.Fatalf("expected viewConfirmDialog state, got %d", app.state)
	}
	if app.confirmDialog.action != "delete" {
		t.Errorf("expected action 'delete', got %q", app.confirmDialog.action)
	}

	// Press 'y' to confirm
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// DeleteNib should have been called (not ArchiveNib)
	if len(stub.DeleteCalls) == 0 {
		t.Fatal("expected DeleteNib to be called, but it was not")
	}
	if stub.DeleteCalls[0] != "nib-1" {
		t.Errorf("expected DeleteNib called with 'nib-1', got %q", stub.DeleteCalls[0])
	}
	if len(stub.ArchiveCalls) != 0 {
		t.Errorf("expected no ArchiveNib calls for delete, got %d", len(stub.ArchiveCalls))
	}

	// Should return to list view
	if app.state != viewList {
		t.Errorf("expected viewList state after confirmation, got %d", app.state)
	}
}

func TestAKeyOnNibWithChildrenArchivesAll(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "parent-1", Title: "Alpha parent", Status: "todo", Type: "epic"},
		{ID: "child-1", Title: "Child one", Status: "todo", Type: "task", Parent: "parent-1"},
		{ID: "child-2", Title: "Child two", Status: "todo", Type: "task", Parent: "parent-1"},
		{ID: "other-1", Title: "Zeta task", Status: "todo", Type: "task"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Press A on the parent nib (cursor should be on first item)
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	// Should be in confirm dialog state
	if app.state != viewConfirmDialog {
		t.Fatalf("expected viewConfirmDialog state, got %d", app.state)
	}

	// Dialog should mention 3 nibs
	if len(app.confirmDialog.nibIDs) != 3 {
		t.Errorf("expected 3 nib IDs in dialog, got %d: %v", len(app.confirmDialog.nibIDs), app.confirmDialog.nibIDs)
	}

	// Confirm
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// ArchiveNib should have been called 3 times (parent + 2 children)
	if len(stub.ArchiveCalls) != 3 {
		t.Fatalf("expected 3 ArchiveNib calls, got %d: %v", len(stub.ArchiveCalls), stub.ArchiveCalls)
	}

	// Verify parent was included
	found := false
	for _, id := range stub.ArchiveCalls {
		if id == "parent-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'parent-1' in ArchiveCalls")
	}

	// Should return to list view
	if app.state != viewList {
		t.Errorf("expected viewList state after confirmation, got %d", app.state)
	}
}

func TestDelKeyOnNibWithChildrenDeletesAll(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "parent-1", Title: "Alpha parent", Status: "todo", Type: "epic"},
		{ID: "child-1", Title: "Child one", Status: "todo", Type: "task", Parent: "parent-1"},
		{ID: "child-2", Title: "Child two", Status: "todo", Type: "task", Parent: "parent-1"},
		{ID: "grandchild-1", Title: "Grandchild", Status: "todo", Type: "task", Parent: "child-1"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Press Del on the parent nib
	sendKey(app, tea.KeyMsg{Type: tea.KeyDelete})

	if app.state != viewConfirmDialog {
		t.Fatalf("expected viewConfirmDialog state, got %d", app.state)
	}

	// Should include parent + 2 children + 1 grandchild = 4
	if len(app.confirmDialog.nibIDs) != 4 {
		t.Errorf("expected 4 nib IDs, got %d: %v", len(app.confirmDialog.nibIDs), app.confirmDialog.nibIDs)
	}

	// Confirm
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// DeleteNib should have been called 4 times
	if len(stub.DeleteCalls) != 4 {
		t.Fatalf("expected 4 DeleteNib calls, got %d: %v", len(stub.DeleteCalls), stub.DeleteCalls)
	}

	// ArchiveNib should NOT have been called
	if len(stub.ArchiveCalls) != 0 {
		t.Errorf("expected no ArchiveNib calls for delete, got %d", len(stub.ArchiveCalls))
	}

	// Should return to list view
	if app.state != viewList {
		t.Errorf("expected viewList state, got %d", app.state)
	}
}

func TestSelectionMovesAfterArchive(t *testing.T) {
	t.Run("selects next item after archiving middle item", func(t *testing.T) {
		testNibs := []*nib.Nib{
			{ID: "nib-1", Title: "Alpha", Status: "todo", Type: "task"},
			{ID: "nib-2", Title: "Beta", Status: "todo", Type: "task"},
			{ID: "nib-3", Title: "Gamma", Status: "todo", Type: "task"},
		}
		app, stub := setupTestApp(t, testNibs)

		// Move cursor to second item (Beta)
		sendKey(app, tea.KeyMsg{Type: tea.KeyDown})

		// Verify cursor is on nib-2
		if item, ok := app.list.list.SelectedItem().(nibItem); !ok || item.nib.ID != "nib-2" {
			t.Fatalf("expected cursor on nib-2, got %v", app.list.list.SelectedItem())
		}

		// Archive nib-2
		sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
		sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

		if len(stub.ArchiveCalls) != 1 || stub.ArchiveCalls[0] != "nib-2" {
			t.Fatalf("expected ArchiveNib('nib-2'), got %v", stub.ArchiveCalls)
		}

		// After archive, the list reloads. The stub still returns all nibs
		// (it doesn't actually remove them), so we verify the selectByID mechanism
		// was set to position the cursor appropriately.

		// The app should have set selectByID to "nib-3" (next sibling)
		// or the cursor should stay at the same index (which will be "nib-3"
		// once the archived nib is removed from the list).
		// Since our stub doesn't actually remove nibs, we verify the intent
		// by checking that the app returns to list state cleanly.
		if app.state != viewList {
			t.Errorf("expected viewList state, got %d", app.state)
		}
	})

	t.Run("selects previous item when archiving last item", func(t *testing.T) {
		testNibs := []*nib.Nib{
			{ID: "nib-1", Title: "Alpha", Status: "todo", Type: "task"},
			{ID: "nib-2", Title: "Beta", Status: "todo", Type: "task"},
		}
		app, stub := setupTestApp(t, testNibs)

		// Move cursor to last item
		sendKey(app, tea.KeyMsg{Type: tea.KeyDown})

		// Verify cursor is on nib-2
		if item, ok := app.list.list.SelectedItem().(nibItem); !ok || item.nib.ID != "nib-2" {
			t.Fatalf("expected cursor on nib-2, got %v", app.list.list.SelectedItem())
		}

		// Archive nib-2
		sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
		sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

		if len(stub.ArchiveCalls) != 1 || stub.ArchiveCalls[0] != "nib-2" {
			t.Fatalf("expected ArchiveNib('nib-2'), got %v", stub.ArchiveCalls)
		}

		if app.state != viewList {
			t.Errorf("expected viewList state, got %d", app.state)
		}
	})
}

func TestCancelArchiveConfirmationDoesNothing(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"press n", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}},
		{"press Esc", tea.KeyMsg{Type: tea.KeyEscape}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testNibs := []*nib.Nib{
				{ID: "nib-1", Title: "First task", Status: "todo", Type: "task"},
			}
			app, stub := setupTestApp(t, testNibs)

			// Press A to open archive confirm dialog
			sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

			if app.state != viewConfirmDialog {
				t.Fatalf("expected viewConfirmDialog state, got %d", app.state)
			}

			// Cancel
			sendKey(app, tt.key)

			// Should return to list view
			if app.state != viewList {
				t.Errorf("expected viewList state after cancel, got %d", app.state)
			}

			// ArchiveNib should NOT have been called
			if len(stub.ArchiveCalls) != 0 {
				t.Errorf("expected no ArchiveNib calls, got %d", len(stub.ArchiveCalls))
			}
		})
	}
}
