package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/nib"
)

// runListCmd executes cmd the way the Bubbletea runtime would and feeds every
// resulting message back through Update. A command still running after a short
// wait is a timer, not a result, and is dropped.
func runListCmd(t *testing.T, m listModel, cmd tea.Cmd, depth int) listModel {
	t.Helper()
	if cmd == nil || depth > 8 {
		return m
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if msg == nil {
			return m
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				m = runListCmd(t, m, sub, depth+1)
			}
			return m
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		return runListCmd(t, m, next, depth+1)
	case <-time.After(50 * time.Millisecond):
		return m
	}
}

// visibleMatching counts the list's items whose title contains term, the rows
// an applied filter for term has to show.
func visibleMatching(m listModel, term string) int {
	n := 0
	for _, it := range m.list.Items() {
		if bi, ok := it.(nibItem); ok && strings.Contains(bi.nib.Title, term) {
			n++
		}
	}
	return n
}

func TestList_ReloadKeepsAnAppliedFilter(t *testing.T) {
	backend := &StubBackend{AllNibs: []*nib.Nib{
		makeNib("n-1", "Alpha task", "task", "todo", ""),
		makeNib("n-2", "Beta task", "task", "todo", ""),
	}}
	m := newListModel(backend, makeTestConfig())
	m = runListCmd(t, m, m.loadNibs, 0)
	m.list.SetFilterText("Alpha")
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("before the reload: %d visible rows, want 1", got)
	}

	m, cmd := m.Update(m.loadNibs())
	m = runListCmd(t, m, cmd, 0)

	if got := len(m.list.VisibleItems()); got != 1 {
		t.Errorf("after a reload with filter %q applied: %d visible rows, want 1", m.list.FilterValue(), got)
	}
}

func TestList_CollapseKeysKeepAnAppliedFilter(t *testing.T) {
	// n-1 Alpha epic
	//   n-2 Alpha feature
	//     n-3 Alpha leaf
	//   n-4 Beta task
	// n-5 Gamma root
	tests := []struct {
		name      string
		key       tea.KeyPressMsg
		collapsed []string // collapsed before the filter is applied
	}{
		{name: "tab", key: tea.KeyPressMsg{Code: tea.KeyTab}},
		{name: "left", key: tea.KeyPressMsg{Code: tea.KeyLeft}},
		{name: "right", key: tea.KeyPressMsg{Code: tea.KeyRight}, collapsed: []string{"n-1"}},
		{name: "ctrl+left", key: tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl}},
		{name: "ctrl+right", key: tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl}, collapsed: []string{"n-2"}},
		{name: "shift+tab", key: tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}},
		{name: "]", key: tea.KeyPressMsg{Code: ']', Text: "]"}, collapsed: []string{"n-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.String(); got != tt.name {
				t.Fatalf("key reads as %q, want %q", got, tt.name)
			}
			backend := &StubBackend{AllNibs: []*nib.Nib{
				makeNib("n-1", "Alpha epic", "epic", "todo", ""),
				makeNib("n-2", "Alpha feature", "feature", "todo", "n-1"),
				makeNib("n-3", "Alpha leaf", "task", "todo", "n-2"),
				makeNib("n-4", "Beta task", "task", "todo", "n-1"),
				makeNib("n-5", "Gamma root", "epic", "todo", ""),
			}}
			m := newListModel(backend, makeTestConfig())
			for _, id := range tt.collapsed {
				m.collapsedIDs[id] = true
			}
			m = runListCmd(t, m, m.loadNibs, 0)

			m.list.SetFilterText("Alpha")
			selected := false
			for i, it := range m.list.VisibleItems() {
				if bi, ok := it.(nibItem); ok && bi.nib.ID == "n-1" {
					m.list.Select(i)
					selected = true
				}
			}
			if !selected || len(m.list.VisibleItems()) == 0 {
				t.Fatalf("n-1 is not visible under the applied filter")
			}
			before := len(m.list.Items())

			m, cmd := m.Update(tt.key)
			m = runListCmd(t, m, cmd, 0)

			if len(m.list.Items()) == before {
				t.Fatalf("%s did not change what the tree shows (%d rows), so it never re-flattened", tt.name, before)
			}
			want := visibleMatching(m, "Alpha")
			if got := len(m.list.VisibleItems()); want == 0 || got != want {
				t.Errorf("after %s with filter %q applied: %d visible rows, want %d", tt.name, m.list.FilterValue(), got, want)
			}
		})
	}
}
