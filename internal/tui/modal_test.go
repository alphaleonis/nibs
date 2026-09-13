package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func TestPickerModalHeaderCutsTheTitleByCells(t *testing.T) {
	// Screen width 80 gives a 40-cell modal, so the header has 36 cells.
	const titleCells = 36
	tests := []struct {
		name     string
		title    string
		wantKept string
		wantCut  bool
	}{
		{name: "multibyte title that fits", title: strings.Repeat("é", titleCells), wantKept: strings.Repeat("é", titleCells)},
		{name: "multibyte title too long", title: strings.Repeat("é", 60), wantKept: strings.Repeat("é", titleCells-3) + "...", wantCut: true},
		{name: "wide title too long", title: strings.Repeat("日", 20), wantKept: strings.Repeat("日", 16) + "...", wantCut: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := stripAnsi(renderPickerModal(pickerModalConfig{NibTitle: tt.title, NibID: "nib-1", Width: 80}))
			if !utf8.ValidString(out) {
				t.Fatalf("modal is not valid UTF-8:\n%q", out)
			}
			lines := strings.Split(out, "\n")
			header := lines[1]
			if !strings.Contains(header, tt.wantKept) {
				t.Errorf("header = %q, want it to hold %q", header, tt.wantKept)
			}
			if got := strings.Contains(header, "..."); got != tt.wantCut {
				t.Errorf("header %q carries an ellipsis = %v, want %v", header, got, tt.wantCut)
			}
			if w := lipgloss.Width(lines[0]); lipgloss.Width(header) != w {
				t.Errorf("header is %d cells wide, border is %d: %q", lipgloss.Width(header), w, header)
			}
		})
	}
}
