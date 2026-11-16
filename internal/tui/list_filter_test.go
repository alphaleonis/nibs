package tui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestList_CollapseKeysKeepAnAppliedFilter(t *testing.T) {
	// n-1 Alpha epic
	//   n-2 Alpha feature
	//     n-3 Alpha leaf
	//   n-4 Beta task
	// n-5 Gamma root
	tests := []struct {
		name      actionName
		collapsed []string // collapsed before the filter is applied
	}{
		{name: actFoldRow},
		{name: actUnfoldRow, collapsed: []string{"n-1"}},
		{name: actFoldSubtree},
		{name: actUnfoldSubtree, collapsed: []string{"n-2"}},
		{name: actFoldAll},
		{name: actUnfoldAll, collapsed: []string{"n-1"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			app, _ := setupTestApp(t, []*nib.Nib{
				makeNib("n-1", "Alpha epic", "epic", "todo", ""),
				makeNib("n-2", "Alpha feature", "feature", "todo", "n-1"),
				makeNib("n-3", "Alpha leaf", "task", "todo", "n-2"),
				makeNib("n-4", "Beta task", "task", "todo", "n-1"),
				makeNib("n-5", "Gamma root", "epic", "todo", ""),
			})
			for _, id := range tt.collapsed {
				app.list.collapsed()[id] = true
			}
			pressKeys(app, "/")
			typeText(app, "Alpha")
			pressKeys(app, "enter home")
			if selectedID(app) != "n-1" {
				t.Fatalf("premise failed: the cursor is on %q, want n-1", selectedID(app))
			}
			before := len(shownRows(app))

			processCmdNonBlocking(app, app.runAction(findAction(defaultActions, tt.name)))

			if got := len(shownRows(app)); got == before {
				t.Fatalf("%s did not change what the tree shows (%d rows), so it never reshaped", tt.name, before)
			}
			if app.list.editing || !strings.Contains(app.list.query, "Alpha") {
				t.Errorf("after %s the box is open = %v with query %q, want Alpha applied", tt.name, app.list.editing, app.list.query)
			}
			for _, r := range shownRows(app) {
				if !r.dimmed && !strings.Contains(app.list.byID[r.id].Title, "Alpha") {
					t.Errorf("after %s the list shows %s, which does not match Alpha", tt.name, r.id)
				}
			}
		})
	}
}
