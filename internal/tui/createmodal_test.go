package tui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

func TestCreateModalHeaderIncludesTypeName(t *testing.T) {
	cfg := config.Default()
	m := newCreateModalModel("bug", cfg, 80, 40)

	view := m.View()

	// The header should say "Create New bug" not "Create New Nib"
	if strings.Contains(view, "Create New Nib") {
		t.Error("header still says 'Create New Nib', expected type name instead")
	}
	if !strings.Contains(view, "Create New") {
		t.Error("header missing 'Create New' prefix")
	}
	if !strings.Contains(view, "bug") {
		t.Error("header missing type name 'bug'")
	}
}

func TestCreateModalHeaderTypeIsColored(t *testing.T) {
	cfg := config.Default()
	m := newCreateModalModel("bug", cfg, 80, 40)

	view := m.View()

	// bug has color "red" in default config. The rendered type text should
	// use ui.RenderTypeText which applies ANSI color codes.
	tc := cfg.GetType("bug")
	if tc == nil {
		t.Fatal("expected 'bug' to be a known type in default config")
		return
	}
	styledType := ui.RenderTypeText("bug", tc.Color)
	if !strings.Contains(view, styledType) {
		t.Errorf("header should contain styled type text %q, but View() output does not contain it", styledType)
	}
}

func TestCreateModalHeaderUnknownTypeFallsBackToMuted(t *testing.T) {
	cfg := config.Default()
	m := newCreateModalModel("unknown-type", cfg, 80, 40)

	view := m.View()

	// Unknown type has no config entry → typeColor is empty → RenderTypeText uses muted styling
	mutedType := ui.RenderTypeText("unknown-type", "")
	if !strings.Contains(view, mutedType) {
		t.Errorf("header should contain muted type text %q for unknown type, but View() output does not contain it", mutedType)
	}
	if !strings.Contains(view, "unknown-type") {
		t.Error("header should still contain the type name 'unknown-type'")
	}
}
