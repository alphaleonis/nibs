package ui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// titleDisplayColumn returns the display column where the title text starts,
// accounting for multi-byte UTF-8 characters that occupy 1 display cell.
func titleDisplayColumn(rendered, title string) int {
	stripped := ansi.Strip(rendered)
	idx := strings.Index(stripped, title)
	if idx < 0 {
		return -1
	}
	prefix := stripped[:idx]
	return lipgloss.Width(prefix)
}

func TestRenderNibRow_IndicatorsDontShiftTitle(t *testing.T) {
	// All rows should have the title at the same horizontal position,
	// regardless of which indicators (blocked, blocking, priority) are present.
	baseCfg := NibRowConfig{
		MaxTitleWidth: 60,
		StatusColor:   "green",
		TypeColor:     "blue",
	}

	// Render a baseline row with no indicators
	baseRow := RenderNibRow("abc123", "todo", "task", "My Title", baseCfg)
	basePos := titleDisplayColumn(baseRow, "My Title")
	if basePos < 0 {
		t.Fatal("title not found in baseline row")
	}

	tests := []struct {
		name string
		cfg  NibRowConfig
	}{
		{"blocked", NibRowConfig{IsBlocked: true}},
		{"blocking", NibRowConfig{IsBlocking: true}},
		{"priority high", NibRowConfig{Priority: "high", PriorityColor: "red"}},
		{"priority critical", NibRowConfig{Priority: "critical", PriorityColor: "red"}},
		{"priority low", NibRowConfig{Priority: "low", PriorityColor: "blue"}},
		{"blocked + priority", NibRowConfig{IsBlocked: true, Priority: "high", PriorityColor: "red"}},
		{"blocking + priority", NibRowConfig{IsBlocking: true, Priority: "critical", PriorityColor: "red"}},
		{"dimmed", NibRowConfig{Dimmed: true}},
		{"dimmed blocked", NibRowConfig{Dimmed: true, IsBlocked: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg
			cfg.IsBlocked = tt.cfg.IsBlocked
			cfg.IsBlocking = tt.cfg.IsBlocking
			cfg.Priority = tt.cfg.Priority
			cfg.PriorityColor = tt.cfg.PriorityColor
			cfg.Dimmed = tt.cfg.Dimmed

			row := RenderNibRow("abc123", "todo", "task", "My Title", cfg)
			pos := titleDisplayColumn(row, "My Title")
			if pos < 0 {
				t.Fatal("title not found in rendered row")
			}
			if pos != basePos {
				t.Errorf("title at column %d, want %d (same as baseline)\n  baseline: %q\n  got:      %q",
					pos, basePos, ansi.Strip(baseRow), ansi.Strip(row))
			}
		})
	}
}

func TestRenderNibRow_IndicatorsRightAligned(t *testing.T) {
	// Indicators should be right-aligned within the fixed-width column,
	// so a single symbol sits adjacent to the title text.
	// Expected layouts (4-cell column, | marks column boundary):
	//   no indicators:  "    |Title"
	//   priority only:  "  ↓ |Title"  (right-aligned, slot 2)
	//   blocked only:   "  ● |Title"  (right-aligned, slot 2)
	//   both:           "◆ ↓ |Title"  (fills both slots)
	baseCfg := NibRowConfig{
		MaxTitleWidth: 60,
		StatusColor:   "green",
		TypeColor:     "blue",
	}

	// Helper: extract the 4 display cells before the title.
	// We work in runes (not bytes) since indicator symbols are multi-byte.
	indicatorColumn := func(row, title string) string {
		stripped := ansi.Strip(row)
		runes := []rune(stripped)
		titleRunes := []rune(title)
		// Find title start in rune slice
		for i := range runes {
			if i+len(titleRunes) <= len(runes) {
				match := true
				for j, tr := range titleRunes {
					if runes[i+j] != tr {
						match = false
						break
					}
				}
				if match && i >= 4 {
					return string(runes[i-4 : i])
				}
			}
		}
		return ""
	}

	tests := []struct {
		name      string
		cfg       NibRowConfig
		wantCol   string // expected 4-rune indicator column
	}{
		{"no indicators", NibRowConfig{}, "    "},
		{"priority only", NibRowConfig{Priority: "low", PriorityColor: "blue"}, "  ↓ "},
		{"blocked only", NibRowConfig{IsBlocked: true}, "  ● "},
		{"blocking only", NibRowConfig{IsBlocking: true}, "  ◆ "},
		{"blocking + priority", NibRowConfig{IsBlocking: true, Priority: "low", PriorityColor: "blue"}, "◆ ↓ "},
		{"blocked + priority", NibRowConfig{IsBlocked: true, Priority: "high", PriorityColor: "red"}, "● ! "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg
			cfg.IsBlocked = tt.cfg.IsBlocked
			cfg.IsBlocking = tt.cfg.IsBlocking
			cfg.Priority = tt.cfg.Priority
			cfg.PriorityColor = tt.cfg.PriorityColor

			row := RenderNibRow("abc123", "todo", "task", "My Title", cfg)
			got := indicatorColumn(row, "My Title")
			if got != tt.wantCol {
				t.Errorf("indicator column = %q, want %q\n  row: %q", got, tt.wantCol, ansi.Strip(row))
			}
		})
	}
}

func TestRenderNibRow_IndicatorsRightAligned_ASCII(t *testing.T) {
	// Same alignment contract as the UTF-8 variant, but with ASCII fallbacks
	// forced via the test hook. Each indicator slot is 2 cells wide:
	//   no indicators:        "    |Title"
	//   priority low only:    "  v |Title"
	//   blocked only:         "  * |Title"
	//   blocking only:        "  # |Title"
	//   blocking + priority:  "# v |Title"
	//   blocked + crit:       "* !!|Title"   ("!!" fills slot 2)
	//   blocked + high:       "* ! |Title"
	withASCIIGlyphs(t, true)

	baseCfg := NibRowConfig{
		MaxTitleWidth: 60,
		StatusColor:   "green",
		TypeColor:     "blue",
	}

	indicatorColumn := func(row, title string) string {
		stripped := ansi.Strip(row)
		runes := []rune(stripped)
		titleRunes := []rune(title)
		for i := range runes {
			if i+len(titleRunes) <= len(runes) {
				match := true
				for j, tr := range titleRunes {
					if runes[i+j] != tr {
						match = false
						break
					}
				}
				if match && i >= 4 {
					return string(runes[i-4 : i])
				}
			}
		}
		return ""
	}

	tests := []struct {
		name    string
		cfg     NibRowConfig
		wantCol string
	}{
		{"no indicators", NibRowConfig{}, "    "},
		{"priority low only", NibRowConfig{Priority: "low", PriorityColor: "blue"}, "  v "},
		{"blocked only", NibRowConfig{IsBlocked: true}, "  * "},
		{"blocking only", NibRowConfig{IsBlocking: true}, "  # "},
		{"blocking + priority low", NibRowConfig{IsBlocking: true, Priority: "low", PriorityColor: "blue"}, "# v "},
		{"blocked + high", NibRowConfig{IsBlocked: true, Priority: "high", PriorityColor: "red"}, "* ! "},
		{"blocked + critical", NibRowConfig{IsBlocked: true, Priority: "critical", PriorityColor: "red"}, "* !!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg
			cfg.IsBlocked = tt.cfg.IsBlocked
			cfg.IsBlocking = tt.cfg.IsBlocking
			cfg.Priority = tt.cfg.Priority
			cfg.PriorityColor = tt.cfg.PriorityColor

			row := RenderNibRow("abc123", "todo", "task", "My Title", cfg)
			got := indicatorColumn(row, "My Title")
			if got != tt.wantCol {
				t.Errorf("indicator column = %q, want %q\n  row: %q", got, tt.wantCol, ansi.Strip(row))
			}
		})
	}
}

func TestRenderNibRow_NarrowWidth(t *testing.T) {
	// Test that RenderNibRow doesn't panic with very small MaxTitleWidth values
	// This was a bug where MaxTitleWidth < 4 caused a slice bounds panic

	tests := []struct {
		name          string
		maxTitleWidth int
		title         string
	}{
		{"zero width", 0, "Test Title"},
		{"width 1", 1, "Test Title"},
		{"width 2", 2, "Test Title"},
		{"width 3", 3, "Test Title"},
		{"width 4", 4, "Test Title"},
		{"width 5", 5, "Test Title"},
		{"short title fits", 10, "Hi"},
		{"exact fit", 10, "0123456789"},
		{"needs truncation", 10, "This is a longer title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderNibRow panicked with MaxTitleWidth=%d: %v", tt.maxTitleWidth, r)
				}
			}()

			cfg := NibRowConfig{
				MaxTitleWidth: tt.maxTitleWidth,
				StatusColor:   "green",
				TypeColor:     "blue",
			}

			result := RenderNibRow("abc123", "todo", "task", tt.title, cfg)
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestRenderNibRow_NarrowWidthWithPriority(t *testing.T) {
	// Priority symbol takes 2 extra chars, which reduces available title width
	// This tests that the adjustment doesn't cause negative slice bounds

	tests := []struct {
		name          string
		maxTitleWidth int
		priority      string
	}{
		{"width 1 with priority", 1, "high"},
		{"width 2 with priority", 2, "high"},
		{"width 3 with priority", 3, "critical"},
		{"width 4 with priority", 4, "high"},
		{"width 5 with priority", 5, "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderNibRow panicked with MaxTitleWidth=%d and priority=%s: %v",
						tt.maxTitleWidth, tt.priority, r)
				}
			}()

			cfg := NibRowConfig{
				MaxTitleWidth: tt.maxTitleWidth,
				Priority:      tt.priority,
				PriorityColor: "red",
				StatusColor:   "green",
				TypeColor:     "blue",
			}

			result := RenderNibRow("abc123", "todo", "task", "Long title that needs truncation", cfg)
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestRenderNibRow_NarrowWidthWithBlocked(t *testing.T) {
	// Blocked indicator takes 2 extra chars, which reduces available title width.
	// Combined with priority symbol, that's 4 chars less for the title.

	tests := []struct {
		name          string
		maxTitleWidth int
		priority      string
	}{
		{"width 1 blocked", 1, ""},
		{"width 2 blocked", 2, ""},
		{"width 3 blocked with priority", 3, "high"},
		{"width 4 blocked with priority", 4, "critical"},
		{"width 5 blocked", 5, ""},
		{"width 10 blocked with priority", 10, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderNibRow panicked with MaxTitleWidth=%d, blocked=true, priority=%s: %v",
						tt.maxTitleWidth, tt.priority, r)
				}
			}()

			cfg := NibRowConfig{
				MaxTitleWidth: tt.maxTitleWidth,
				Priority:      tt.priority,
				PriorityColor: "red",
				StatusColor:   "green",
				TypeColor:     "blue",
				IsBlocked:     true,
			}

			result := RenderNibRow("abc123", "todo", "task", "Long title that needs truncation", cfg)
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestShortType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"milestone", "M"},
		{"epic", "E"},
		{"bug", "B"},
		{"feature", "F"},
		{"task", "T"},
		{"research", "R"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ShortType(tt.input)
			if result != tt.expected {
				t.Errorf("ShortType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShortStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"draft", "D"},
		{"todo", "T"},
		{"in-progress", "I"},
		{"deferred", "F"},
		{"completed", "C"},
		{"scrapped", "S"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ShortStatus(tt.input)
			if result != tt.expected {
				t.Errorf("ShortStatus(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestShortType_NoCollisions guards the implicit invariant that every entry in
// config.DefaultTypes has a unique first letter. ShortType derives its
// single-character abbreviation from def.Name[:1] — if a future type addition
// collides on the first letter (e.g. adding "fix" alongside "feature"), the
// list view would render two rows with the same code and no visual
// distinction. This test fails loudly at PR time so the regression is
// caught before merge.
func TestShortType_NoCollisions(t *testing.T) {
	seen := map[string]string{}
	for _, def := range config.DefaultTypes {
		s := strings.ToUpper(def.Name[:1])
		if existing, ok := seen[s]; ok {
			t.Fatalf("ShortType collision: %q and %q both abbreviate to %q",
				existing, def.Name, s)
		}
		seen[s] = def.Name
	}
}

// TestShortStatus_NoCollisions is the status counterpart of
// TestShortType_NoCollisions — see that test for rationale. It validates the
// actual ShortStatus output (not the raw first letter) because some statuses
// (e.g. "deferred" vs "draft") share a first letter and are disambiguated
// inside ShortStatus.
func TestShortStatus_NoCollisions(t *testing.T) {
	seen := map[string]string{}
	for _, def := range config.DefaultStatuses {
		s := ShortStatus(def.Name)
		if existing, ok := seen[s]; ok {
			t.Fatalf("ShortStatus collision: %q and %q both abbreviate to %q",
				existing, def.Name, s)
		}
		seen[s] = def.Name
	}
}

// TestRenderNibRow_DeferredStatusCell locks in that a "deferred" nib renders a
// visible, config-colored status cell — never blank, never the "?" unknown
// marker, and never dimmed like an archived nib. "deferred" was added to config
// in a prior slice; this guards the full TUI render chain
// (GetNibColors -> ShortStatus -> RenderStatusTextWithColor) against a
// regression that would drop the status glyph.
func TestRenderNibRow_DeferredStatusCell(t *testing.T) {
	cfg := config.Default()
	nc := cfg.GetNibColors("deferred", "task", "")

	// Config drives the render inputs: deferred is a real (non-fallback) color
	// and, crucially, is NOT an archive status (so the row is not dimmed).
	if nc.IsArchive {
		t.Fatal("deferred must be non-archive so its row is not dimmed")
	}
	if nc.StatusColor != "gray" {
		t.Errorf("deferred status colour = %q, want %q", nc.StatusColor, "gray")
	}
	if ResolveColor(nc.StatusColor) == ColorMuted {
		t.Error("deferred status colour resolved to the unknown-colour fallback (ColorMuted)")
	}

	// Title intentionally contains no "F" so the only "F" in the row is the
	// ShortStatus glyph for deferred.
	const title = "Parked work"

	t.Run("single-char status column", func(t *testing.T) {
		row := RenderNibRow("tnib-1", "deferred", "task", title, NibRowConfig{
			MaxTitleWidth: 60,
			StatusColor:   nc.StatusColor,
			TypeColor:     nc.TypeColor,
			IsArchive:     nc.IsArchive,
		})
		stripped := ansi.Strip(row)
		if !strings.Contains(stripped, "F") {
			t.Errorf("status cell missing 'F' glyph for deferred; row: %q", stripped)
		}
		if strings.Contains(stripped, "?") {
			t.Errorf("deferred rendered as unknown status '?'; row: %q", stripped)
		}
	})

	t.Run("full-name status column", func(t *testing.T) {
		row := RenderNibRow("tnib-1", "deferred", "task", title, NibRowConfig{
			MaxTitleWidth: 60,
			StatusColor:   nc.StatusColor,
			TypeColor:     nc.TypeColor,
			IsArchive:     nc.IsArchive,
			UseFullNames:  true,
		})
		stripped := ansi.Strip(row)
		if !strings.Contains(stripped, "deferred") {
			t.Errorf("full-name status cell missing 'deferred'; row: %q", stripped)
		}
	})
}
