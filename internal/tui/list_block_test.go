package tui

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
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

// Behavior 15: block at top of sibling set + Ctrl-Up → no move, footer says so.
func TestCtrlUp_BlockAtTop_ReportsAtTop(t *testing.T) {
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
	if got := app.list.statusMessage; got != reorderReasonAtTop {
		t.Errorf("expected status %q, got %q", reorderReasonAtTop, got)
	}
}

// Behavior 16: block at bottom + Ctrl-Down → no move, footer says so.
func TestCtrlDown_BlockAtBottom_ReportsAtBottom(t *testing.T) {
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
	if got := app.list.statusMessage; got != reorderReasonAtBottom {
		t.Errorf("expected status %q, got %q", reorderReasonAtBottom, got)
	}
}

// Behavior 17: 2 marked with a gap, Ctrl-Up → no move, and the footer says why.
func TestCtrlUp_MarkedWithGap_ReportsReason(t *testing.T) {
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
	if got := app.list.statusMessage; got != reorderReasonNotContiguous {
		t.Errorf("expected status %q, got %q", reorderReasonNotContiguous, got)
	}
}

// Behavior 18: multi-parent marked, Ctrl-Up → no move, and the footer says why.
func TestCtrlUp_MultiParent_ReportsReason(t *testing.T) {
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
	if got := app.list.statusMessage; got != reorderReasonDifferentParents {
		t.Errorf("expected status %q, got %q", reorderReasonDifferentParents, got)
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

// danglingParentNibs returns root-level nibs where G carries a parent link
// naming a nib that does not exist. The tree therefore shows G at root level,
// and reordering must treat it as a root sibling.
func danglingParentNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "A", Title: "A", Type: "task", Status: "todo", Order: "1"},
		{ID: "G", Title: "G", Type: "task", Status: "todo", Order: "2", Parent: "ghost"},
		{ID: "C", Title: "C", Type: "task", Status: "todo", Order: "3"},
	}
}

// assertDanglingRootShape fails unless the loaded tree really is three roots
// with G still carrying its unresolvable parent link. Without this the reorder
// assertions could pass on a fixture where G is trivially parentless.
func assertDanglingRootShape(t *testing.T, app *App) {
	t.Helper()
	if got := len(app.list.tree); got != 3 {
		t.Fatalf("expected 3 root nodes, got %d", got)
	}
	node := ui.FindNode(app.list.tree, "G")
	if node == nil {
		t.Fatal("G not present in tree")
	}
	if node.Nib.Parent != "ghost" {
		t.Fatalf("expected G to keep parent %q, got %q", "ghost", node.Nib.Parent)
	}
	if ui.FindNode(app.list.tree, "ghost") != nil {
		t.Fatal("expected G's parent to be absent from the tree")
	}
}

// Behavior 21: a refusal is reported, and does not outlive the next successful
// move.
//
// Only the first half discriminates this change. The clearing is the generic
// per-keypress reset every status message already gets, so that assertion holds
// even with the reorder success path gutted — it is here to pin the end-to-end
// sequence a user actually sees, not as a guard on the success path.
func TestCtrlUp_AfterRefusal_SuccessfulMoveClearsStatus(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())

	// Refuse first: A is already at the top.
	if !focusOn(app, "A") {
		t.Fatal("could not focus A")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})
	if app.list.statusMessage != reorderReasonAtTop {
		t.Fatalf("expected the refusal to be reported, got %q", app.list.statusMessage)
	}

	// The very next keypress moves A the other way, with no navigation between.
	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlDown})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected 1 reorder call, got %d", got)
	}
	if app.list.statusMessage != "" {
		t.Errorf("expected the stale refusal to be cleared, got %q", app.list.statusMessage)
	}
}

// Behavior 24: a refusal is colored as a warning, a success is not. The kind
// rides alongside the message, so this fails if a writer sets one without the
// other — which is how a refusal would silently render in the success green.
func TestReorderRefusal_IsStyledAsAWarning(t *testing.T) {
	app, _ := setupTestApp(t, blockTestNibs())

	if !focusOn(app, "A") {
		t.Fatal("could not focus A")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})
	if app.list.statusMessage != reorderReasonAtTop {
		t.Fatalf("premise failed: expected the refusal to be reported, got %q", app.list.statusMessage)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("refusal statusKind = %v, want statusWarn — it would render in the success color", app.list.statusKind)
	}

	// A successful move must not inherit the warning styling.
	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlDown})
	if app.list.statusKind != statusOK {
		t.Errorf("statusKind after a successful move = %v, want statusOK", app.list.statusKind)
	}
}

// Behavior 20: Ctrl-Up with nothing in the list → no move, footer says so.
func TestCtrlUp_EmptyList_ReportsNothingToMove(t *testing.T) {
	app, stub := setupTestApp(t, nil)

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 0 {
		t.Errorf("expected 0 reorder calls for an empty list, got %d", got)
	}
	if got := app.list.statusMessage; got != reorderReasonNothingSelected {
		t.Errorf("expected status %q, got %q", reorderReasonNothingSelected, got)
	}
}

// Behavior 22: a nib whose parent link names no nib sits at root level,
// so Ctrl-Up moves it among the root siblings.
func TestCtrlUp_DanglingParent_MovesAmongRootSiblings(t *testing.T) {
	app, stub := setupTestApp(t, danglingParentNibs())
	assertDanglingRootShape(t, app)

	if !focusOn(app, "G") {
		t.Fatal("could not focus G")
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected 1 reorder call, got %d", got)
	}
	call := stub.ReorderCalls[0]
	if call.ID != "G" {
		t.Errorf("expected ID=G, got %q", call.ID)
	}
	if call.BeforeID == nil || *call.BeforeID != "A" {
		t.Errorf("expected beforeID=A, got %v", call.BeforeID)
	}
}

// Behavior 23: a dangling-parent nib and a genuine root are siblings on screen,
// so marking both moves them as one block.
func TestCtrlUp_DanglingParentWithRoot_MovesAsBlock(t *testing.T) {
	app, stub := setupTestApp(t, danglingParentNibs())
	assertDanglingRootShape(t, app)

	if !focusOn(app, "G") {
		t.Fatal("could not focus G")
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark G, cursor→C
	sendKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // mark C
	if !app.list.selectedNibs["G"] || !app.list.selectedNibs["C"] {
		t.Fatalf("expected G,C marked: %v", app.list.selectedNibs)
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlUp})

	if got := len(stub.ReorderCalls); got != 1 {
		t.Fatalf("expected exactly 1 backend reorder call, got %d", got)
	}
	call := stub.ReorderCalls[0]
	// Block = [G, C]; prev sibling A moves past the block to after C.
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
