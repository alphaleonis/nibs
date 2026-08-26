package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// rowTitle reads back the title portion of a rendered row: everything past the
// fixed columns, which end where the last of them is padded out.
func rowTitle(t *testing.T, rendered string, cfg NibRowConfig) string {
	t.Helper()
	stripped := ansi.Strip(rendered)
	// The columns ahead of the title are fixed-width and independent of the
	// title, so a render with an empty title measures exactly where it starts.
	empty := ansi.Strip(RenderNibRow("abc123", "todo", "task", "", cfg))
	prefix := strings.TrimRight(empty, " ")
	idx := strings.Index(stripped, prefix)
	if idx < 0 {
		t.Fatalf("could not locate the column prefix %q in %q", prefix, stripped)
	}
	return strings.TrimLeft(stripped[idx+len(prefix):], " ")
}

// MaxTitleWidth is a sentinel: zero means "no limit", which is what the CLI
// tree renderer relies on. The indicator column is then subtracted from it, and
// deciding whether to truncate on what is LEFT reads an exhausted budget as the
// sentinel — so a caller with no room for a title got an untruncated one, and
// the over-long row was wrapped by the box it was rendered into, growing that
// box past the height it was handed.
func TestRenderNibRowHonorsAnExhaustedTitleBudget(t *testing.T) {
	const title = "A title far longer than any of these budgets"

	tests := []struct {
		name          string
		maxTitleWidth int
		want          string
	}{
		{name: "zero is the no-limit sentinel", maxTitleWidth: 0, want: title},
		{name: "one cell short of the indicator column", maxTitleWidth: 3, want: ""},
		{name: "exactly the indicator column", maxTitleWidth: 4, want: ""},
		{name: "one cell of budget", maxTitleWidth: 5, want: title[:1]},
		{name: "three cells, too few for the marker", maxTitleWidth: 7, want: title[:3]},
		{name: "four cells, marked cut", maxTitleWidth: 8, want: title[:1] + "..."},
		{name: "twenty cells, marked cut", maxTitleWidth: 24, want: title[:17] + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NibRowConfig{MaxTitleWidth: tt.maxTitleWidth, StatusColor: "green", TypeColor: "blue"}
			if got := rowTitle(t, RenderNibRow("abc123", "todo", "task", title, cfg), cfg); got != tt.want {
				t.Errorf("title = %q, want %q", got, tt.want)
			}
		})
	}
}

// Truncation is measured in display cells. A byte count both places the cut in
// the wrong column for a non-ASCII title and can slice a rune in half, which
// paints as a replacement glyph rather than as the character that was there.
func TestRenderNibRowCutsATitleByCellsNotBytes(t *testing.T) {
	// Each of these runes is three bytes wide and one cell wide, so a byte
	// count would cut this title at a third of the column it belongs in.
	const title = "återställ återställ återställ"

	cfg := NibRowConfig{MaxTitleWidth: 24, StatusColor: "green", TypeColor: "blue"}
	got := rowTitle(t, RenderNibRow("abc123", "todo", "task", title, cfg), cfg)

	if want := "återställ återstä..."; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, '�') {
		t.Errorf("the cut split a rune in half: %q", got)
	}
	if w := lipgloss.Width(got); w > cfg.MaxTitleWidth-4 {
		t.Errorf("title occupies %d cells for a %d-cell budget", w, cfg.MaxTitleWidth-4)
	}
}
