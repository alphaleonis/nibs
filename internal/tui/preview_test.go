package tui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestPreviewView(t *testing.T) {
	b := &nib.Nib{
		ID:       "nibs-test",
		Title:    "Test Nib",
		Status:   "todo",
		Type:     "feature",
		Priority: "high",
		Tags:     []string{"frontend", "design"},
		Body:     "## Summary\n\nThis is the body.",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	// Should contain the title
	if !strings.Contains(view, "Test Nib") {
		t.Error("preview should contain nib title")
	}

	// Should contain the ID
	if !strings.Contains(view, "nibs-test") {
		t.Error("preview should contain nib ID")
	}

	// Should contain status
	if !strings.Contains(view, "todo") {
		t.Error("preview should contain status")
	}

	// Should contain type
	if !strings.Contains(view, "feature") {
		t.Error("preview should contain type")
	}

	// Should contain body content
	if !strings.Contains(view, "Summary") {
		t.Error("preview should contain body")
	}
}

func TestPreviewViewEmpty(t *testing.T) {
	preview := newPreviewModel(nil, 60, 20)
	view := preview.View()

	if !strings.Contains(view, "No nib selected") {
		t.Error("empty preview should show 'No nib selected'")
	}
}

func TestPreviewViewWithTags(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Nib with Tags",
		Status: "in-progress",
		Type:   "bug",
		Tags:   []string{"urgent", "backend"},
		Body:   "Test body",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	// Should show tags
	if !strings.Contains(view, "urgent") || !strings.Contains(view, "backend") {
		t.Error("preview should display tags")
	}
}

func TestPreviewViewWithPriority(t *testing.T) {
	b := &nib.Nib{
		ID:       "nibs-test",
		Title:    "High Priority Nib",
		Status:   "todo",
		Type:     "task",
		Priority: "critical",
		Body:     "Important work",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	// Should show priority
	if !strings.Contains(view, "critical") {
		t.Error("preview should display priority when not normal")
	}
}

func TestPreviewViewWithEstimate(t *testing.T) {
	b := &nib.Nib{
		ID:       "nibs-test",
		Title:    "Estimated Nib",
		Status:   "todo",
		Type:     "task",
		Estimate: "l",
		Body:     "Some work",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	if !strings.Contains(view, "Estimate:") || !strings.Contains(view, "l") {
		t.Error("preview should display estimate when set")
	}
}

func TestPreviewViewWithoutEstimate(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "No Estimate Nib",
		Status: "todo",
		Type:   "task",
		Body:   "Some work",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	if strings.Contains(view, "Estimate:") {
		t.Error("preview should not display estimate label when estimate is empty")
	}
}

func TestPreviewViewWithDocuments(t *testing.T) {
	b := &nib.Nib{
		ID:        "nibs-test",
		Title:     "Nib with Docs",
		Status:    "todo",
		Type:      "task",
		Documents: []string{"docs/design.md", "README.md"},
		Body:      "Test body",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	if !strings.Contains(view, "docs/design.md") {
		t.Error("preview should display document path 'docs/design.md'")
	}
	if !strings.Contains(view, "README.md") {
		t.Error("preview should display document path 'README.md'")
	}
}

func TestPreviewViewWithoutDocuments(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Nib without Docs",
		Status: "todo",
		Type:   "task",
		Body:   "Test body",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	// Should not contain any document-related label
	if strings.Contains(view, "Docs:") {
		t.Error("preview should not show 'Docs:' when documents is empty")
	}
}

func TestPreviewViewEmptyBody(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Nib without body",
		Status: "todo",
		Type:   "task",
		Body:   "",
	}

	preview := newPreviewModel(b, 60, 20)
	view := preview.View()

	// Should show placeholder for empty body
	if !strings.Contains(view, "No description") {
		t.Error("preview should show 'No description' for empty body")
	}
}
