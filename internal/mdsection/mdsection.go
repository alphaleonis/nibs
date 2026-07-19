package mdsection

import "strings"

// section holds the line range of a found section.
type section struct {
	headingIdx int // index of the heading line
	startIdx   int // index of first content line (headingIdx + 1)
	endIdx     int // index of first line NOT in the section (next heading or len(lines))
	level      int // heading level (number of # chars)
}

// findSection locates a section by heading text (case-insensitive) and returns its line range.
func findSection(lines []string, heading string) (section, bool) {
	target := strings.ToLower(heading)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !isHeading(trimmed) {
			continue
		}
		l := HeadingLevel(trimmed)
		text := strings.TrimSpace(trimmed[l:])
		if !matchesHeading(text, target) {
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
// and whether the section was found.
func Find(body, heading string) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// Replace replaces the content of a named section, keeping the heading line intact.
// The heading is matched case-insensitively. Content between the heading and the next
// equal/higher-level heading is replaced with newContent.
// Returns the body unchanged if the section is not found.
func Replace(body, heading, newContent string) string {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading)
	if !found {
		return body
	}

	var result []string
	result = append(result, lines[:sec.startIdx]...)
	result = append(result, strings.Split(newContent, "\n")...)
	result = append(result, lines[sec.endIdx:]...)
	return strings.Join(result, "\n")
}

// Set replaces a section's content if found, or appends a new section if not.
// When appending, creates a heading at the specified level (e.g., 2 for "##").
func Set(body string, level int, heading, content string) string {
	if level < 1 {
		level = 1
	}
	lines := strings.Split(body, "\n")
	_, found := findSection(lines, heading)
	if found {
		return Replace(body, heading, content)
	}

	// Append new section.
	prefix := strings.Repeat("#", level)
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
