package ui

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// TestGlyphTreeConnector_3Cells enforces the invariant documented in tree.go
// (treeIndent = 3) and glyphs.go (every tree connector occupies 3 display
// cells).
func TestGlyphTreeConnector_3Cells(t *testing.T) {
	connectors := []struct {
		name string
		fn   func(bool) string
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
			for _, c := range connectors {
				got := lipgloss.Width(c.fn(m.ascii))
				if got != treeIndent {
					t.Errorf("%s in %s mode: lipgloss.Width(%q) = %d, want %d",
						c.name, m.name, c.fn(m.ascii), got, treeIndent)
				}
			}
		})
	}
}

func TestLocaleUTF8(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"LANG names UTF-8", map[string]string{"LANG": "en_US.UTF-8"}, true},
		{"spelled utf8", map[string]string{"LANG": "sv_SE.utf8"}, true},
		{"C.UTF-8", map[string]string{"LC_CTYPE": "C.UTF-8"}, true},
		{"LANG is C", map[string]string{"LANG": "C"}, false},
		{"Latin-1", map[string]string{"LANG": "sv_SE.ISO-8859-1"}, false},
		{"nothing set is the C locale", map[string]string{}, false},
		{"LC_ALL wins over LC_CTYPE and LANG", map[string]string{"LC_ALL": "C", "LC_CTYPE": "en_US.UTF-8", "LANG": "en_US.UTF-8"}, false},
		{"LC_CTYPE wins over LANG", map[string]string{"LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, true},
		{"an empty variable is unset", map[string]string{"LC_ALL": "", "LANG": "en_US.UTF-8"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localeUTF8(func(k string) string { return tt.env[k] }); got != tt.want {
				t.Errorf("localeUTF8(%v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
