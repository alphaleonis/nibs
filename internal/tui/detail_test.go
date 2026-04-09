package tui

import (
	"strings"
	"testing"

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
