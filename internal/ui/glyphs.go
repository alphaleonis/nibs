package ui

import "sync"

// Glyphs used in CLI/TUI rendering. UTF-8 by default. When the terminal cannot
// safely display UTF-8 (e.g. a Windows console with a non-UTF-8 codepage), an
// ASCII fallback is returned instead so output stays readable.
//
// The fallbacks are chosen to occupy a small, predictable number of display
// cells so layout math elsewhere (right-aligned indicator column, tree
// connector widths) keeps working. See styles.go and tree.go for callers.

// asciiGlyphsOverride is a test hook: when non-nil it forces useASCIIGlyphs
// to return its dereferenced value, bypassing platform detection. Tests use
// withASCIIGlyphs (see glyphs_test.go) to set this and restore it via
// t.Cleanup.
//
// NOT goroutine-safe: this is a plain pointer, read and written without
// synchronization. Tests using withASCIIGlyphs must run serially — do not
// pair it with t.Parallel(), and do not exercise concurrent renderers from
// a test that mutates this hook.
var asciiGlyphsOverride *bool

// asciiOnce guards the cached platform detection result. The Windows
// implementation of detectASCIIRequired issues a syscall (GetConsoleOutputCP),
// and the renderer can call useASCIIGlyphs many times per redraw — caching
// collapses that to a single syscall per process. The console codepage
// effectively never changes mid-process, so caching is safe.
var (
	asciiOnce     sync.Once
	asciiDetected bool
)

// useASCIIGlyphs reports whether the renderer should use ASCII fallbacks
// rather than UTF-8 glyphs. The test hook takes precedence over platform
// detection (and bypasses the cache).
func useASCIIGlyphs() bool {
	if asciiGlyphsOverride != nil {
		return *asciiGlyphsOverride
	}
	asciiOnce.Do(func() {
		asciiDetected = detectASCIIRequired()
	})
	return asciiDetected
}

// Priority symbols.
//
// Note on the critical fallback: "!!" is two display cells where the UTF-8 "‼"
// is one. The indicator column in RenderNibRow allocates 2 cells per slot, so
// "!!" still fits without breaking right-alignment. If layout regresses, drop
// to a single "!" — at the cost of critical and high colliding visually under
// ASCII (acceptable: ASCII is a degraded fallback by design).
func glyphCritical() string {
	if useASCIIGlyphs() {
		return "!!"
	}
	return "‼" // ‼
}

func glyphHigh() string {
	// "!" is the same in both modes; kept as a function for symmetry.
	return "!"
}

func glyphLow() string {
	if useASCIIGlyphs() {
		return "v"
	}
	return "↓" // ↓
}

// Indicator dots for blocked/blocking nibs.
func glyphBlocked() string {
	if useASCIIGlyphs() {
		return "*"
	}
	return "●" // ●
}

func glyphBlocking() string {
	if useASCIIGlyphs() {
		return "#"
	}
	return "◆" // ◆
}

// Selection cursor (used in TUI list views).
func glyphCursor() string {
	if useASCIIGlyphs() {
		return ">"
	}
	return "▌" // ▌
}

// Tree connectors. Each connector occupies 3 display cells in both modes so
// indentation math in RenderTree keeps working.
func glyphTreeBranch() string {
	if useASCIIGlyphs() {
		return "+- "
	}
	return "├─ " // ├─
}

func glyphTreeLastBranch() string {
	if useASCIIGlyphs() {
		return "\\- "
	}
	return "└─ " // └─
}

func glyphTreePipe() string {
	if useASCIIGlyphs() {
		return "|  "
	}
	return "│  " // │
}

func glyphTreeSpace() string {
	// "   " in both modes; kept as a function for symmetry.
	return "   "
}

// Horizontal divider used under the tree header.
func glyphHRule() string {
	if useASCIIGlyphs() {
		return "-"
	}
	return "─" // ─
}

// Collapse/expand indicators for the tree view.
func glyphCollapseCollapsed() string {
	if useASCIIGlyphs() {
		return "> "
	}
	return "▸ " // ▸
}

func glyphCollapseExpanded() string {
	if useASCIIGlyphs() {
		return "v "
	}
	return "▾ " // ▾
}

// GlyphSectionCursor returns the section/item cursor used in the TUI detail
// view (e.g. for the currently focused section). Returns the UTF-8 cursor by
// default and an ASCII fallback when the terminal cannot display UTF-8.
func GlyphSectionCursor() string {
	return glyphCollapseCollapsed()
}
