package mdsection

import "strings"

// AnyLevel is the wildcard match-level sentinel. Passed as the match level to
// Find, Replace, or SetAtLevel, it matches a heading at ANY level; a level N>0
// instead gates the match to a heading spelled at exactly level N (so a request
// at level 3 will not match — and later clobber — a level-2 "## Sub").
const AnyLevel = 0

// section holds the line range of a found section.
type section struct {
	headingIdx int // index of the heading line
	startIdx   int // index of first content line (headingIdx + 1)
	endIdx     int // index of first line NOT in the section (next heading or len(lines))
	level      int // heading level (number of # chars)
}

// findSection locates a section by heading text (case-insensitive) at the
// requested level and returns its line range. matchLevel is a sentinel: AnyLevel
// matches a heading of any level (wildcard); N>0 matches only a heading spelled
// at level N (so a matchLevel of 3 will not match a level-2 "## Sub").
func findSection(lines []string, heading string, matchLevel int) (section, bool) {
	target := strings.ToLower(heading)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !isHeading(trimmed) {
			continue
		}
		l := HeadingLevel(trimmed)
		text := strings.TrimSpace(trimmed[l:])
		// A spelled matchLevel (N>0) gates the match to that exact level so a
		// "### Sub" request cannot match — and later clobber — a level-2 "## Sub".
		// AnyLevel is the wildcard: match a heading at any level.
		if !matchesHeading(text, target) || (matchLevel != AnyLevel && l != matchLevel) {
			continue
		}

		// Found the heading. Now find where the section ends.
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			jTrimmed := strings.TrimSpace(lines[j])
			if isHeading(jTrimmed) && HeadingLevel(jTrimmed) <= l {
				end = j
				break
			}
		}

		return section{headingIdx: i, startIdx: i + 1, endIdx: end, level: l}, true
	}

	return section{}, false
}

// Find locates a section by heading text (case-insensitive match on text after # symbols).
// Returns the content between the heading line and the next heading at equal or higher level,
// and whether the section was found. matchLevel is a sentinel: AnyLevel matches a heading of
// any level (wildcard); N>0 matches only a heading spelled at level N.
func Find(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading, matchLevel)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// Replace replaces the content of a named section, keeping the heading line intact.
// The heading is matched case-insensitively. Content between the heading and the next
// equal/higher-level heading is replaced with newContent.
// Returns the body unchanged if the section is not found. matchLevel is a sentinel:
// AnyLevel matches a heading of any level (wildcard); N>0 matches only a heading spelled at level N.
func Replace(body, heading, newContent string, matchLevel int) string {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading, matchLevel)
	if !found {
		return body
	}

	var result []string
	result = append(result, lines[:sec.startIdx]...)
	result = append(result, strings.Split(newContent, "\n")...)
	result = append(result, lines[sec.endIdx:]...)
	return strings.Join(result, "\n")
}

// Set replaces the content of a matching section, or appends a new section if
// none matches — matching an existing heading at ANY level (wildcard). appendLevel
// is the level of the heading created when no match exists (clamped to at least
// 1). This is the wildcard-match variant: use it for callers that target a
// heading regardless of the level it is spelled at. Use SetAtLevel to gate the
// match to a specific level.
func Set(body string, appendLevel int, heading, content string) string {
	return SetAtLevel(body, AnyLevel, appendLevel, heading, content)
}

// SetAtLevel replaces a section's content if a matching section is found, or
// appends a new section if not. matchLevel selects which existing heading counts
// as a match (AnyLevel = any level/wildcard, N>0 = only level N); appendLevel is
// the level of the heading created when no match exists (clamped to at least 1).
// Separating the two lets a caller demand a match at an exact level yet still
// create a new heading at a chosen level when absent. The two intent-revealing
// entry points (Set for wildcard, SetAtLevel for a spelled level) keep callers
// from transposing the two adjacent int levels.
func SetAtLevel(body string, matchLevel, appendLevel int, heading, content string) string {
	if appendLevel < 1 {
		appendLevel = 1
	}
	lines := strings.Split(body, "\n")
	_, found := findSection(lines, heading, matchLevel)
	if found {
		return Replace(body, heading, content, matchLevel)
	}

	// Append new section.
	prefix := strings.Repeat("#", appendLevel)
	section := prefix + " " + heading + "\n" + content
	if body == "" {
		// Leading newline ensures a blank line before the heading when this
		// content is later joined with YAML front matter or other preamble.
		return "\n" + section
	}
	return body + "\n\n" + section
}

// isHeading returns true if the line starts with one or more # followed by a space.
func isHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	stripped := strings.TrimLeft(line, "#")
	return len(stripped) > 0 && stripped[0] == ' '
}

// HeadingLevel returns the number of # characters at the start of a heading
// line. It counts from the first rune, so callers pass a whitespace-trimmed
// line (e.g. "## H" → 2).
func HeadingLevel(line string) int {
	return len(line) - len(strings.TrimLeft(line, "#"))
}

// matchesHeading checks if a heading text matches the target (case-insensitive).
// Supports parenthetical suffix matching so "Key Decisions" matches "Key Decisions (Phase 2)"
// but not "Key Decisions Extended".
func matchesHeading(text, target string) bool {
	lower := strings.ToLower(text)
	return lower == target || strings.HasPrefix(lower, target+" (")
}

// trimTrailingBlanks removes trailing blank lines from content,
// keeping a single trailing newline.
func trimTrailingBlanks(s string) string {
	return strings.TrimRight(s, "\n") + "\n"
}
