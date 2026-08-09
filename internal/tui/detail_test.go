package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestDetailHeaderWithDocuments(t *testing.T) {
	b := &nib.Nib{
		ID:        "nibs-test",
		Title:     "Nib with Docs",
		Status:    "todo",
		Type:      "task",
		Documents: []string{"docs/design.md", "README.md"},
	}

	m := detailModel{
		nib:    b,
		config: config.Default(),
		width:  80,
	}

	header := m.renderHeader()

	if !strings.Contains(header, "docs/design.md") {
		t.Error("detail header should display document path 'docs/design.md'")
	}
	if !strings.Contains(header, "README.md") {
		t.Error("detail header should display document path 'README.md'")
	}
}

func TestCalculateHeaderHeightWithDocuments(t *testing.T) {
	withDocs := detailModel{
		nib: &nib.Nib{
			ID: "nibs-test", Title: "Test", Status: "todo", Type: "task",
			Documents: []string{"docs/design.md"},
		},
		config: config.Default(),
		width:  80,
		height: 40,
	}
	withoutDocs := detailModel{
		nib: &nib.Nib{
			ID: "nibs-test", Title: "Test", Status: "todo", Type: "task",
		},
		config: config.Default(),
		width:  80,
		height: 40,
	}

	heightWith := withDocs.calculateHeaderHeight()
	heightWithout := withoutDocs.calculateHeaderHeight()

	if heightWith != heightWithout+1 {
		t.Errorf("header with documents should be 1 line taller: got %d, want %d", heightWith, heightWithout+1)
	}
}

func TestDetailHeaderWithoutDocuments(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Nib without Docs",
		Status: "todo",
		Type:   "task",
	}

	m := detailModel{
		nib:    b,
		config: config.Default(),
		width:  80,
	}

	header := m.renderHeader()

	if strings.Contains(header, "Docs:") {
		t.Error("detail header should not show 'Docs:' when documents is empty")
	}
}

// Lip Gloss v2 changed what Width and Height mean on a bordered style: v1
// counted content only and let the border add two cells on top, v2 counts the
// border inside the number. Every bordered pane in the TUI is sized from a
// content width, so without compensation each one renders two columns narrower
// than the layout it was designed for. This pins the detail panes to the full
// width the layout allots them.
func TestDetailPanesFillTheirAllottedWidth(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Sizing",
		Status: "todo",
		Type:   "task",
		Body:   "## Description\n\nSome body text.\n",
	}
	m := newDetailModel(b, &StubBackend{Nibs: map[string]*nib.Nib{"nibs-test": b}}, config.Default(), 120, 40)

	// The detail panes are laid out with a one-column gutter on each side.
	const want = 120 - 2

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"header", m.renderHeader()},
		{"body", m.View()},
	} {
		for _, line := range strings.Split(tc.content, "\n") {
			if !strings.ContainsAny(line, "╭╰") {
				continue
			}
			if got := lipgloss.Width(line); got != want {
				t.Errorf("%s border line width = %d, want %d\n%q", tc.name, got, want, line)
			}
			break
		}
	}
}

// Glamour v2 emits the document's left margin as plain spaces before any escape
// sequence; v1 emitted style escapes first. strings.TrimSpace therefore used to
// stop at the escape and now eats the first line's indent, leaving the opening
// heading flush against the pane border while every later line stays indented.
func TestRenderBodyKeepsFirstLineIndent(t *testing.T) {
	b := &nib.Nib{
		ID: "nibs-test", Title: "Indent", Status: "todo", Type: "task",
		Body: "## Description\n\nSome body text.\n",
	}
	m := newDetailModel(b, &StubBackend{Nibs: map[string]*nib.Nib{"nibs-test": b}}, config.Default(), 120, 40)

	out := m.renderBody(100)
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("renderBody produced no first line: %q", out)
	}
	if !strings.HasPrefix(lines[0], " ") {
		t.Errorf("first rendered line lost its left margin: %q", lines[0])
	}
	if strings.HasPrefix(out, "\n") {
		t.Errorf("renderBody should still strip glamour's leading blank line: %q", out)
	}
}
