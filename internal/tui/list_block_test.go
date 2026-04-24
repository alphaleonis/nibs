package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/alphaleonis/nibs/internal/nib"
)

// blockTestNibs returns a flat set of root-level nibs A, B, C, D for block-move
// integration tests. Using root-level siblings keeps the tree simple while
// still exercising the sibling-slice path.
func blockTestNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "A", Title: "A", Type: "task", Status: "todo", Order: "1"},
		{ID: "B", Title: "B", Type: "task", Status: "todo", Order: "2"},
		{ID: "C", Title: "C", Type: "task", Status: "todo", Order: "3"},
		{ID: "D", Title: "D", Type: "task", Status: "todo", Order: "4"},
	}
}

// focusOn advances the list cursor to the row whose nib ID matches targetID.
// Returns true if located.
func focusOn(app *App, targetID string) bool {
	for i := 0; i < 20; i++ {
		if item, ok := app.list.list.SelectedItem().(nibItem); ok {
			if item.nib.ID == targetID {
				return true
			}
		}
		sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	return false
}

// Behavior 10 (regression): 0 selected, Ctrl-Up on focused row with prev sibling
// → single reorderNib call with beforeID = prev sibling.
func TestCtrlUp_NoSelection_FocusedRowReorders(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	if !focusOn(app, "C") {
		t.Fatal("could not focus C")
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected 1 reorder call, got %d", got)
	}
	call := stub.ReorderCalls[0]
	if call.ID != "C" {
		t.Errorf("expected ID=C, got %q", call.ID)
	}
	if call.BeforeID == nil || *call.BeforeID != "B" {
		t.Errorf("expected beforeID=B, got %v", call.BeforeID)
	}
	if call.AfterID != nil {
		t.Errorf("expected nil afterID for up-move, got %v", *call.AfterID)
	}
}

// Behavior 11: 1 marked ≠ focused → Ctrl-Up moves the MARKED item, not the
// focused row. Uses the single-item reorderNibMsg path.
func TestCtrlUp_OneMarkedDifferentFromFocus_MovesMarked(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	// Focus D, but mark C.
	if !focusOn(app, "C") {
		t.Fatal("could not focus C")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C, cursor moves to D
	// Cursor is now on D.
	if item, ok := app.list.list.SelectedItem().(nibItem); !ok || item.nib.ID != "D" {
		t.Fatalf("expected focus on D after space, got %v", item.nib.ID)
	}
	if !app.list.selectedNibs["C"] {
		t.Fatal("expected C marked")
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected 1 reorder call, got %d", got)
	}
	call := stub.ReorderCalls[0]
	if call.ID != "C" {
		t.Errorf("expected marked C to be moved, got %q", call.ID)
	}
	if call.BeforeID == nil || *call.BeforeID != "B" {
		t.Errorf("expected beforeID=B, got %v", call.BeforeID)
	}
}

// Behavior 12: 2 contiguous marked under same parent, Ctrl-Up → one
// ReorderNib(prevSib, afterID=lastInBlock) call.
func TestCtrlUp_TwoContiguousMarked_SwapsPrevSiblingPastBlock(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	// Mark B and C (contiguous), focus elsewhere (say D).
	if !focusOn(app, "B") {
		t.Fatal("could not focus B")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark B → D
	// cursor now on C
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C → D
	// cursor now on D
	if !app.list.selectedNibs["B"] || !app.list.selectedNibs["C"] {
		t.Fatalf("expected B,C marked: %v", app.list.selectedNibs)
	}
	// Refocus C so we can verify focusID preservation.
	for i := 0; i < 5; i++ {
		if item, ok := app.list.list.SelectedItem().(nibItem); ok && item.nib.ID == "C" {
			break
		}
		sendKey(app, tea.KeyMsg{Type: tea.KeyUp})
	}
	if item, ok := app.list.list.SelectedItem().(nibItem); !ok || item.nib.ID != "C" {
		t.Fatalf("expected focus on C for test, got %v", item.nib.ID)
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected exactly 1 backend reorder call, got %d", got)
	}
	call := stub.ReorderCalls[0]
	// Block = [B, C]; prev sib = A; we move A past the block to after C.
	if call.ID != "A" {
		t.Errorf("expected displacedID=A, got %q", call.ID)
	}
	if call.AfterID == nil || *call.AfterID != "C" {
		t.Errorf("expected afterID=C, got %v", call.AfterID)
	}
	if call.BeforeID != nil {
		t.Errorf("expected nil beforeID, got %v", *call.BeforeID)
	}
}

// Behavior 13: 2 contiguous marked, Ctrl-Down → one
// ReorderNib(nextSib, beforeID=firstInBlock).
func TestCtrlDown_TwoContiguousMarked_SwapsNextSiblingBeforeBlock(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	if !focusOn(app, "B") {
		t.Fatal("could not focus B")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark B, cursor→C
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C, cursor→D

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlDown})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected exactly 1 backend reorder call, got %d", got)
	}
	call := stub.ReorderCalls[0]
	// Block = [B, C]; next sib = D; move D before B.
	if call.ID != "D" {
		t.Errorf("expected displacedID=D, got %q", call.ID)
	}
	if call.BeforeID == nil || *call.BeforeID != "B" {
		t.Errorf("expected beforeID=B, got %v", call.BeforeID)
	}
	if call.AfterID != nil {
		t.Errorf("expected nil afterID, got %v", *call.AfterID)
	}
}

// Behavior 14: post-move selection preserved; selectByID is the original focus,
// not the displaced sibling.
func TestCtrlUp_Block_PreservesSelectionAndFocus(t *testing.T) {
	app, _ := setupTestApp(t, blockTestNibs())
	if !focusOn(app, "B") {
		t.Fatal("could not focus B")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark B, cursor→C
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C, cursor→D
	// Refocus C.
	sendKey(app, tea.KeyMsg{Type: tea.KeyUp})
	if item, ok := app.list.list.SelectedItem().(nibItem); !ok || item.nib.ID != "C" {
		t.Fatalf("expected focus on C, got %v", item.nib.ID)
	}

	// Snapshot selection before.
	selBefore := make(map[string]bool, len(app.list.selectedNibs))
	for k, v := range app.list.selectedNibs {
		selBefore[k] = v
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	// Selection map unchanged — same keys present after.
	if len(app.list.selectedNibs) != len(selBefore) {
		t.Errorf("selection size changed: before=%d after=%d", len(selBefore), len(app.list.selectedNibs))
	}
	for k := range selBefore {
		if !app.list.selectedNibs[k] {
			t.Errorf("expected %q still marked after block move", k)
		}
	}

	// selectByID has been consumed by the reload, but we can verify it by
	// checking the list's current selection after processCmd — reload should
	// have re-selected the original focus C, not displaced sibling A.
	if item, ok := app.list.list.SelectedItem().(nibItem); !ok || item.nib.ID != "C" {
		t.Errorf("expected post-reload focus on C (original focus), got %v", item.nib.ID)
	}
}

// Behavior 15: block at top of sibling set + Ctrl-Up → silent no-op.
func TestCtrlUp_BlockAtTop_SilentNoOp(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	// Mark A,B (top two).
	if !focusOn(app, "A") {
		t.Fatal("could not focus A")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark A, cursor→B
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark B, cursor→C

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 0 {
		t.Errorf("expected 0 reorder calls (no-op), got %d", got)
	}
	if app.list.statusMessage != "" {
		t.Errorf("expected empty status (silent), got %q", app.list.statusMessage)
	}
}

// Behavior 16: block at bottom + Ctrl-Down → silent no-op.
func TestCtrlDown_BlockAtBottom_SilentNoOp(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	// Mark C,D (bottom two).
	if !focusOn(app, "C") {
		t.Fatal("could not focus C")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C, cursor→D
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark D, cursor→D (last)

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlDown})

	if got := len(stub.ReorderCalls); got != 0 {
		t.Errorf("expected 0 reorder calls (no-op), got %d", got)
	}
	if app.list.statusMessage != "" {
		t.Errorf("expected empty status (silent), got %q", app.list.statusMessage)
	}
}

// Behavior 17: 2 marked with a gap, Ctrl-Up → silent no-op.
func TestCtrlUp_MarkedWithGap_SilentNoOp(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	// Mark A and C (gap at B).
	if !focusOn(app, "A") {
		t.Fatal("could not focus A")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark A, cursor→B
	sendKey(app, tea.KeyMsg{Type: tea.KeyDown})                      // skip B (cursor→C)
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C, cursor→D

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 0 {
		t.Errorf("expected 0 reorder calls for non-contiguous selection, got %d", got)
	}
	if app.list.statusMessage != "" {
		t.Errorf("expected empty status (silent), got %q", app.list.statusMessage)
	}
}

// Behavior 18: multi-parent marked, Ctrl-Up → silent no-op.
func TestCtrlUp_MultiParent_SilentNoOp(t *testing.T) {
	// Hierarchy: P1 -> [X1, X2]; P2 -> [Y1, Y2]
	nibs := []*nib.Nib{
		{ID: "P1", Title: "P1", Type: "epic", Status: "todo", Order: "1"},
		{ID: "P2", Title: "P2", Type: "epic", Status: "todo", Order: "2"},
		{ID: "X1", Title: "X1", Type: "task", Status: "todo", Order: "1", Parent: "P1"},
		{ID: "X2", Title: "X2", Type: "task", Status: "todo", Order: "2", Parent: "P1"},
		{ID: "Y1", Title: "Y1", Type: "task", Status: "todo", Order: "1", Parent: "P2"},
		{ID: "Y2", Title: "Y2", Type: "task", Status: "todo", Order: "2", Parent: "P2"},
	}
	app, stub := setupTestApp(t, nibs)

	// Mark X1 and Y1 (different parents).
	if !focusOn(app, "X1") {
		t.Fatal("could not focus X1")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark X1
	if !focusOn(app, "Y1") {
		t.Fatal("could not focus Y1")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark Y1

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 0 {
		t.Errorf("expected 0 reorder calls for multi-parent selection, got %d", got)
	}
}

// Behavior 19: parent + descendant both marked, Ctrl-Up → effective set is
// {parent}; flows through single-item path. Moves the parent, not the
// descendant.
func TestCtrlUp_ParentAndDescendantMarked_MovesParentOnly(t *testing.T) {
	// Hierarchy: A, B, C; C has child C1. Mark C and C1, Ctrl-Up should move C.
	nibs := []*nib.Nib{
		{ID: "A", Title: "A", Type: "task", Status: "todo", Order: "1"},
		{ID: "B", Title: "B", Type: "task", Status: "todo", Order: "2"},
		{ID: "C", Title: "C", Type: "task", Status: "todo", Order: "3"},
		{ID: "C1", Title: "C1", Type: "task", Status: "todo", Order: "1", Parent: "C"},
	}
	app, stub := setupTestApp(t, nibs)

	if !focusOn(app, "C") {
		t.Fatal("could not focus C")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C, cursor→C1
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C1, cursor moves down

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected 1 reorder call (single-item path), got %d", got)
	}
	call := stub.ReorderCalls[0]
	if call.ID != "C" {
		t.Errorf("expected parent C to be moved (not descendant C1), got %q", call.ID)
	}
	// C's prev root sibling is B.
	if call.BeforeID == nil || *call.BeforeID != "B" {
		t.Errorf("expected beforeID=B, got %v", call.BeforeID)
	}
}
