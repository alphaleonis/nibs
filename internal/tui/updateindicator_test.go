package tui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestApp_UpdateCheckMsgTogglesIndicator(t *testing.T) {
	app, _ := setupTestApp(t, []*nib.Nib{{ID: "nib-1", Title: "T", Status: "todo", Type: "task"}})

	updated, _ := app.Update(updateCheckMsg{available: true, latest: "v0.6.0"})
	a := updated.(*App)
	if !a.list.updateAvailable || a.list.updateLatest != "v0.6.0" {
		t.Fatalf("expected indicator set on available update, got available=%v latest=%q",
			a.list.updateAvailable, a.list.updateLatest)
	}
}

func TestApp_UpdateCheckMsgNoUpdateStaysHidden(t *testing.T) {
	app, _ := setupTestApp(t, []*nib.Nib{{ID: "nib-1", Title: "T", Status: "todo", Type: "task"}})

	updated, _ := app.Update(updateCheckMsg{available: false})
	a := updated.(*App)
	if a.list.updateAvailable {
		t.Error("expected the indicator to stay hidden when no update is available")
	}
}

func TestListModel_UpdateIndicator(t *testing.T) {
	t.Run("hidden by default", func(t *testing.T) {
		var m listModel
		if got := m.updateIndicator(); got != "" {
			t.Errorf("expected empty indicator by default, got %q", got)
		}
	})

	t.Run("hidden when latest is empty", func(t *testing.T) {
		m := listModel{updateAvailable: true}
		if got := m.updateIndicator(); got != "" {
			t.Errorf("expected empty indicator with no version, got %q", got)
		}
	})

	t.Run("shown with version and upgrade hint", func(t *testing.T) {
		m := listModel{updateAvailable: true, updateLatest: "v0.6.0"}
		got := m.updateIndicator()
		if !strings.Contains(got, "v0.6.0") || !strings.Contains(got, "nibs upgrade") {
			t.Errorf("indicator missing version or upgrade hint: %q", got)
		}
	})
}

func TestListModel_FooterShowsUpdateIndicator(t *testing.T) {
	m := newListModel(&StubBackend{}, config.Default())

	if strings.Contains(m.Footer(), "available") {
		t.Fatal("footer should not show an update indicator before a check")
	}

	m.updateAvailable = true
	m.updateLatest = "v0.6.0"
	if !strings.Contains(m.Footer(), "v0.6.0") {
		t.Errorf("footer should include the update indicator once available: %q", m.Footer())
	}
}
