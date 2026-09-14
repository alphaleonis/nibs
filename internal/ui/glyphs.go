package ui

import "sync"

// Glyphs used in CLI and TUI rendering: UTF-8 by default, ASCII when the
// terminal cannot display UTF-8 (e.g. a Windows console on a non-UTF-8
// codepage). Each fallback fits the cells its caller lays out.

// asciiGlyphsOverride, when non-nil, decides useASCIIGlyphs' answer; tests set
// it through withASCIIGlyphs. It is unsynchronized, so a test that sets it must
// not run in parallel.
var asciiGlyphsOverride *bool

// asciiOnce caches detectASCIIRequired, a syscall on Windows, since
// useASCIIGlyphs is asked for every glyph drawn.
var (
	asciiOnce     sync.Once
	asciiDetected bool
)

// useASCIIGlyphs reports whether to draw ASCII fallbacks. asciiGlyphsOverride
// takes precedence and bypasses the cache.
func useASCIIGlyphs() bool {
	if asciiGlyphsOverride != nil {
		return *asciiGlyphsOverride
	}
	asciiOnce.Do(func() {
		asciiDetected = detectASCIIRequired()
	})
	return asciiDetected
}

// Priority symbols. The ASCII critical glyph "!!" is two cells where "‼" is
// one; RenderNibRow's 2-cell indicator slot holds either.
func glyphCritical() string {
	if useASCIIGlyphs() {
		return "!!"
	}
	return "‼"
}

func glyphHigh() string {
	return "!"
}

func glyphLow() string {
	if useASCIIGlyphs() {
		return "v"
	}
	return "↓"
}

// Blocked and blocking indicators.
func glyphBlocked() string {
	if useASCIIGlyphs() {
		return "*"
	}
	return "●"
}

func glyphBlocking() string {
	if useASCIIGlyphs() {
		return "#"
	}
	return "◆"
}

// Selection cursor for RenderNibRow.
func glyphCursor() string {
	if useASCIIGlyphs() {
		return ">"
	}
	return "▌"
}

// Tree connectors, treeIndent cells wide in both modes.
func glyphTreeBranch() string {
	if useASCIIGlyphs() {
		return "+- "
	}
	return "├─ "
}

func glyphTreeLastBranch() string {
	if useASCIIGlyphs() {
		return "\\- "
	}
	return "└─ "
}

func glyphTreePipe() string {
	if useASCIIGlyphs() {
		return "|  "
	}
	return "│  "
}

func glyphTreeSpace() string {
	return "   "
}

// Collapse/expand indicators for the tree view.
func glyphCollapseCollapsed() string {
	if useASCIIGlyphs() {
		return "> "
	}
	return "▸ "
}

func glyphCollapseExpanded() string {
	if useASCIIGlyphs() {
		return "v "
	}
	return "▾ "
}

// GlyphSectionCursor returns a row cursor: the collapsed-node indicator, with
// its ASCII fallback.
func GlyphSectionCursor() string {
	return glyphCollapseCollapsed()
}
