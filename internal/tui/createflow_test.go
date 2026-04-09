package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestDefaultTypeForContext(t *testing.T) {
	tests := []struct {
		selectedType string
		want         string
	}{
		{"milestone", "epic"},
		{"epic", "feature"},
		{"feature", "task"},
		{"bug", "task"},
		{"task", "task"},
		{"research", "task"},
		{"", "feature"},       // no selection → feature
		{"unknown", "feature"}, // unknown type → feature
	}

	for _, tt := range tests {
		t.Run(tt.selectedType, func(t *testing.T) {
			got := defaultTypeForContext(tt.selectedType)
			if got != tt.want {
				t.Errorf("defaultTypeForContext(%q) = %q, want %q", tt.selectedType, got, tt.want)
			}
		})
	}
}

// sendKeyCreateFlow sends a key and processes commands, but skips blocking
// commands like textinput.Blink that would hang in tests.
func sendKeyCreateFlow(app *App, key tea.KeyMsg) {
	_, cmd := app.Update(key)
	processCmdNonBlocking(app, cmd)
}

// processCmdNonBlocking executes commands but uses a timeout to skip blocking
// commands like cursor blink that hang in test context.
func processCmdNonBlocking(app *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}

	// Run the command in a goroutine with timeout
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	// Use a short timeout - non-blocking commands complete immediately,
	// blocking commands (e.g., textinput.Blink) are skipped.
	select {
	case msg := <-done:
		if msg == nil {
			return
		}
		// Handle batch messages
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, subCmd := range batch {
				processCmdNonBlocking(app, subCmd)
			}
			return
		}
		_, nextCmd := app.Update(msg)
		processCmdNonBlocking(app, nextCmd)
	case <-time.After(10 * time.Millisecond):
		// Command is blocking (e.g., textinput.Blink) - skip it
		return
	}
}

func TestCreateFlowOpensTypePickerOnC(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "feat-1", Title: "A feature", Status: "todo", Type: "feature"},
		{ID: "task-1", Title: "A task", Status: "todo", Type: "task"},
	}
	app, _ := setupTestApp(t, testNibs)

	// Press 'c' to start creation flow
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	// Should be in type picker state (for creation), not create modal
	if app.state != viewCreateTypePicker {
		t.Fatalf("expected viewCreateTypePicker state, got %d", app.state)
	}
}

func TestCreateFlowFullCycle(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "epic-1", Title: "An epic", Status: "todo", Type: "epic"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Press 'c' to start creation flow
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if app.state != viewCreateTypePicker {
		t.Fatalf("expected viewCreateTypePicker state, got %d", app.state)
	}

	// Press enter to select the default type (should be "feature" since selected is epic)
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter})

	// Should now be in create modal (title input)
	if app.state != viewCreateModal {
		t.Fatalf("expected viewCreateModal state after type selection, got %d", app.state)
	}

	// Type a title and press enter
	for _, r := range "My new feature" {
		sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter})

	// Verify a nib was created with the correct type
	if len(stub.CreateCalls) != 1 {
		t.Fatalf("expected 1 CreateNib call, got %d", len(stub.CreateCalls))
	}
	call := stub.CreateCalls[0]
	if call.Title != "My new feature" {
		t.Errorf("expected title %q, got %q", "My new feature", call.Title)
	}
	if call.Type == nil || *call.Type != "feature" {
		t.Errorf("expected type %q, got %v", "feature", call.Type)
	}
	// Parent should be the epic (feature is a valid child of epic)
	if call.Parent == nil || *call.Parent != "epic-1" {
		t.Errorf("expected parent %q, got %v", "epic-1", call.Parent)
	}
}

func TestCreateFlowSiblingInference(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "feat-1", Title: "Parent feature", Status: "todo", Type: "feature"},
		{ID: "task-1", Title: "Existing task", Status: "todo", Type: "task", Parent: "feat-1"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Move to task-1
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyDown})

	// Press 'c' to start creation
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if app.state != viewCreateTypePicker {
		t.Fatalf("expected viewCreateTypePicker, got %d", app.state)
	}

	// Default should be "task" (since selected is a task). Press enter to accept.
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.state != viewCreateModal {
		t.Fatalf("expected viewCreateModal, got %d", app.state)
	}

	// Type title and enter
	for _, r := range "Sibling task" {
		sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter})

	if len(stub.CreateCalls) != 1 {
		t.Fatalf("expected 1 CreateNib call, got %d", len(stub.CreateCalls))
	}
	call := stub.CreateCalls[0]
	if call.Type == nil || *call.Type != "task" {
		t.Errorf("expected type %q, got %v", "task", call.Type)
	}
	// Same type as selected → sibling. Parent should be feat-1, afterID should be task-1.
	if call.Parent == nil || *call.Parent != "feat-1" {
		t.Errorf("expected parent %q, got %v", "feat-1", call.Parent)
	}
	if call.AfterID == nil || *call.AfterID != "task-1" {
		t.Errorf("expected afterID %q, got %v", "task-1", call.AfterID)
	}
}

func TestCreateFlowHigherLevelNoParent(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "task-1", Title: "A task", Status: "todo", Type: "task", Parent: "feat-1"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Press 'c', then navigate to select "milestone"
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if app.state != viewCreateTypePicker {
		t.Fatalf("expected viewCreateTypePicker, got %d", app.state)
	}

	// The type picker lists types in order: milestone, epic, bug, feature, task, research.
	// Default cursor is on "task" (since selected nib is task).
	// We need to navigate to "milestone" which is at the top.
	// Move up from task to milestone: task(4) -> feature(3) -> bug(2) -> epic(1) -> milestone(0)
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyUp})
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyUp})
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyUp})
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyUp})
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.state != viewCreateModal {
		t.Fatalf("expected viewCreateModal, got %d", app.state)
	}

	for _, r := range "A milestone" {
		sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter})

	if len(stub.CreateCalls) != 1 {
		t.Fatalf("expected 1 CreateNib call, got %d", len(stub.CreateCalls))
	}
	call := stub.CreateCalls[0]
	if call.Type == nil || *call.Type != "milestone" {
		t.Errorf("expected type %q, got %v", "milestone", call.Type)
	}
	// Milestone is higher level than task → no parent
	if call.Parent != nil {
		t.Errorf("expected nil parent for milestone, got %v", call.Parent)
	}
}

func TestCreateFlowCancelTypePicker(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "task-1", Title: "A task", Status: "todo", Type: "task"},
	}
	app, stub := setupTestApp(t, testNibs)

	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if app.state != viewCreateTypePicker {
		t.Fatalf("expected viewCreateTypePicker, got %d", app.state)
	}

	// Press Esc to cancel
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEscape})

	// Should return to list, no nib created
	if app.state != viewList {
		t.Errorf("expected viewList after cancel, got %d", app.state)
	}
	if len(stub.CreateCalls) != 0 {
		t.Errorf("expected no CreateNib calls, got %d", len(stub.CreateCalls))
	}
}

func TestCreateFlowCancelTitleInput(t *testing.T) {
	testNibs := []*nib.Nib{
		{ID: "task-1", Title: "A task", Status: "todo", Type: "task"},
	}
	app, stub := setupTestApp(t, testNibs)

	// Open type picker, select type, then cancel title input
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEnter}) // select default type

	if app.state != viewCreateModal {
		t.Fatalf("expected viewCreateModal, got %d", app.state)
	}

	sendKeyCreateFlow(app, tea.KeyMsg{Type: tea.KeyEscape})

	if app.state != viewList {
		t.Errorf("expected viewList after cancel title, got %d", app.state)
	}
	if len(stub.CreateCalls) != 0 {
		t.Errorf("expected no CreateNib calls, got %d", len(stub.CreateCalls))
	}
}

func TestInferParent(t *testing.T) {
	tests := []struct {
		name       string
		chosenType string
		selected   *nib.Nib
		wantParent string
		wantAfter  string
	}{
		{
			name:       "no selection → root level",
			chosenType: "feature",
			selected:   nil,
			wantParent: "",
			wantAfter:  "",
		},
		{
			name:       "valid child of selected → parent is selected nib",
			chosenType: "feature",
			selected:   &nib.Nib{ID: "epic-1", Type: "epic"},
			wantParent: "epic-1",
			wantAfter:  "",
		},
		{
			name:       "task under feature → parent is feature",
			chosenType: "task",
			selected:   &nib.Nib{ID: "feat-1", Type: "feature"},
			wantParent: "feat-1",
			wantAfter:  "",
		},
		{
			name:       "same level as selected → sibling (parent = selected's parent, afterID = selected)",
			chosenType: "task",
			selected:   &nib.Nib{ID: "task-1", Type: "task", Parent: "feat-1"},
			wantParent: "feat-1",
			wantAfter:  "task-1",
		},
		{
			name:       "higher level than selected → root level",
			chosenType: "milestone",
			selected:   &nib.Nib{ID: "task-1", Type: "task", Parent: "feat-1"},
			wantParent: "",
			wantAfter:  "",
		},
		{
			name:       "epic when selected on feature → root level (epic can't be sibling of feature)",
			chosenType: "epic",
			selected:   &nib.Nib{ID: "feat-1", Type: "feature", Parent: "epic-1"},
			wantParent: "",
			wantAfter:  "",
		},
		{
			name:       "same level at root (no parent) → root level, no positioning",
			chosenType: "feature",
			selected:   &nib.Nib{ID: "feat-1", Type: "feature"},
			wantParent: "",
			wantAfter:  "",
		},
		{
			name:       "bug under epic → parent is epic",
			chosenType: "bug",
			selected:   &nib.Nib{ID: "epic-1", Type: "epic"},
			wantParent: "epic-1",
			wantAfter:  "",
		},
		{
			name:       "epic chosen, selected is task under feature → should be root (epic can't be child of feature)",
			chosenType: "epic",
			selected:   &nib.Nib{ID: "task-1", Type: "task", Parent: "feat-1"},
			wantParent: "",
			wantAfter:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParent, gotAfter := inferParent(tt.chosenType, tt.selected)
			if gotParent != tt.wantParent {
				t.Errorf("inferParent(%q, %v) parentID = %q, want %q", tt.chosenType, tt.selected, gotParent, tt.wantParent)
			}
			if gotAfter != tt.wantAfter {
				t.Errorf("inferParent(%q, %v) afterID = %q, want %q", tt.chosenType, tt.selected, gotAfter, tt.wantAfter)
			}
		})
	}
}
