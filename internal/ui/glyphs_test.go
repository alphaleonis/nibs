package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// withASCIIGlyphs forces useASCIIGlyphs() to return the given value for the
// duration of the test, restoring the prior state via t.Cleanup.
func withASCIIGlyphs(t *testing.T, ascii bool) {
	t.Helper()
	prev := asciiGlyphsOverride
	asciiGlyphsOverride = &ascii
	t.Cleanup(func() {
		asciiGlyphsOverride = prev
	})
}

func TestGetPrioritySymbol_ASCII(t *testing.T) {
	withASCIIGlyphs(t, true)

	tests := []struct {
		priority string
		want     string
	}{
		{"critical", "!!"},
		{"high", "!"},
		{"low", "v"},
		{"normal", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.priority, func(t *testing.T) {
			got := GetPrioritySymbol(tt.priority)
			if got != tt.want {
				t.Errorf("GetPrioritySymbol(%q) [ASCII] = %q, want %q", tt.priority, got, tt.want)
			}
		})
	}
}

func TestGetPrioritySymbol_UTF8(t *testing.T) {
	withASCIIGlyphs(t, false)

	tests := []struct {
		priority string
		want     string
	}{
		{"critical", "‼"}, // ‼
		{"high", "!"},
		{"low", "↓"}, // ↓
		{"normal", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.priority, func(t *testing.T) {
			got := GetPrioritySymbol(tt.priority)
			if got != tt.want {
				t.Errorf("GetPrioritySymbol(%q) [UTF-8] = %q, want %q", tt.priority, got, tt.want)
			}
		})
	}
}

func TestUseASCIIGlyphs_OverrideTakesPrecedence(t *testing.T) {
	// Force ASCII, verify useASCIIGlyphs returns true.
	withASCIIGlyphs(t, true)
	if !useASCIIGlyphs() {
		t.Error("useASCIIGlyphs() = false with override=true, want true")
	}

	// Force UTF-8, verify useASCIIGlyphs returns false.
	withASCIIGlyphs(t, false)
	if useASCIIGlyphs() {
		t.Error("useASCIIGlyphs() = true with override=false, want false")
	}
}

// TestGlyphTreeConnector_3Cells enforces the invariant documented in tree.go
// (treeIndent = 3) and glyphs.go (every tree connector occupies 3 display
// cells). RenderTree's indentation math depends on this — if a future glyph
// change breaks it, every connector at every depth shifts horizontally and
// alignment regresses silently.
func TestGlyphTreeConnector_3Cells(t *testing.T) {
	connectors := []struct {
		name string
		fn   func() string
	}{
		{"glyphTreeBranch", glyphTreeBranch},
		{"glyphTreeLastBranch", glyphTreeLastBranch},
		{"glyphTreePipe", glyphTreePipe},
		{"glyphTreeSpace", glyphTreeSpace},
	}
	modes := []struct {
		name  string
		ascii bool
	}{
		{"UTF-8", false},
		{"ASCII", true},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			withASCIIGlyphs(t, m.ascii)
			for _, c := range connectors {
				got := lipgloss.Width(c.fn())
				if got != treeIndent {
					t.Errorf("%s in %s mode: lipgloss.Width(%q) = %d, want %d",
						c.name, m.name, c.fn(), got, treeIndent)
				}
			}
		})
	}
}
