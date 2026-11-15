package ui

import "strings"

// Glyphs the TUI draws, in UTF-8 or ASCII. Each function takes that choice as
// ascii, which callers set for a terminal without UTF-8 or for icons: ascii.
// Each ASCII fallback fits the cells its caller lays out.

// TerminalUTF8 reports whether the terminal displays UTF-8. Outside Windows the
// locale decides, as localeUTF8 reads it. On Windows the console's output
// codepage decides.
func TerminalUTF8() bool {
	return terminalUTF8()
}

// localeUTF8 reports whether the locale in getenv's environment names UTF-8 as
// its encoding. The first of LC_ALL, LC_CTYPE and LANG that is not empty names
// the locale, the order POSIX gives them. With none of them set the C locale
// applies, which is not UTF-8.
func localeUTF8(getenv func(string) string) bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(name); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// Priority symbols. The ASCII critical glyph "!!" is two cells where "‼" is
// one; RenderNibRow's 2-cell indicator slot holds either.
func glyphCritical(ascii bool) string {
	if ascii {
		return "!!"
	}
	return "‼"
}

func glyphHigh(_ bool) string {
	return "!"
}

func glyphLow(ascii bool) string {
	if ascii {
		return "v"
	}
	return "↓"
}

// Blocked and blocking indicators.
func glyphBlocked(ascii bool) string {
	if ascii {
		return "*"
	}
	return "●"
}

func glyphBlocking(ascii bool) string {
	if ascii {
		return "#"
	}
	return "◆"
}

// Selection cursor for RenderNibRow.
func glyphCursor(ascii bool) string {
	if ascii {
		return ">"
	}
	return "▌"
}

// Tree connectors, treeIndent cells wide in both modes.
func glyphTreeBranch(ascii bool) string {
	if ascii {
		return "+- "
	}
	return "├─ "
}

func glyphTreeLastBranch(ascii bool) string {
	if ascii {
		return "\\- "
	}
	return "└─ "
}

func glyphTreePipe(ascii bool) string {
	if ascii {
		return "|  "
	}
	return "│  "
}

func glyphTreeSpace(_ bool) string {
	return "   "
}

// Collapse/expand indicators for the tree view.
func glyphCollapseCollapsed(ascii bool) string {
	if ascii {
		return "> "
	}
	return "▸ "
}

func glyphCollapseExpanded(ascii bool) string {
	if ascii {
		return "v "
	}
	return "▾ "
}

// GlyphSectionCursor returns a row cursor: the collapsed-node indicator, with
// its ASCII fallback.
func GlyphSectionCursor(ascii bool) string {
	return glyphCollapseCollapsed(ascii)
}
