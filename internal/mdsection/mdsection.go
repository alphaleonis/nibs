package mdsection

import "strings"

// AnyLevel matches a heading at any level. A level N>0 matches only a heading
// spelled at exactly N, so "### Sub" does not match "## Sub".
const AnyLevel = 0

type section struct {
	headingIdx int
	startIdx   int // first content line
	endIdx     int // exclusive
	level      int
}

// findSection locates a section by heading text, case-insensitively, at
// matchLevel. An exact heading wins regardless of document order; only when none
// matches does a heading beginning "<heading> (" match, so a bare "Foo" reaches
// a lone "Foo (Phase 1)".
func findSection(lines []string, heading string, matchLevel int) (section, bool) {
	target := strings.ToLower(heading)

	if sec, found := scanSection(lines, target, matchLevel, exactHeading); found {
		return sec, true
	}
	return scanSection(lines, target, matchLevel, parentheticalHeading)
}

// scanSection returns the first section whose heading satisfies match and the
// level gate. match receives the heading text with its "#" markers stripped, and
// the already-lower-cased target.
func scanSection(lines []string, target string, matchLevel int, match func(text, target string) bool) (section, bool) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !isHeading(trimmed) {
			continue
		}
		l := HeadingLevel(trimmed)
		text := strings.TrimSpace(trimmed[l:])
		if !match(text, target) || (matchLevel != AnyLevel && l != matchLevel) {
			continue
		}

		// The section ends at the next heading of equal or higher level.
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

// Find returns a section's content — the lines between its heading and the next
// heading at equal or higher level — and whether it was found. The heading
// matches as findSection describes.
func Find(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading, matchLevel)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// FindExact is Find without the "<heading> (" fallback. Use it when an exact
// heading and a parenthetical one must be told apart; Find cannot.
func FindExact(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := scanSection(lines, strings.ToLower(heading), matchLevel, exactHeading)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// Replace swaps a section's content for newContent, keeping the heading line. A
// body with no matching section comes back unchanged.
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

// Set is SetAtLevel with matchLevel AnyLevel.
func Set(body string, appendLevel int, heading, content string) (string, bool) {
	return SetAtLevel(body, AnyLevel, appendLevel, heading, content)
}

// SetAtLevel replaces the content of the section matching at matchLevel, or
// appends a new heading at appendLevel (clamped to at least 1) when none matches.
// The bool is true when it appended. Read it rather than re-deriving the outcome
// with a Find.
func SetAtLevel(body string, matchLevel, appendLevel int, heading, content string) (string, bool) {
	if appendLevel < 1 {
		appendLevel = 1
	}
	lines := strings.Split(body, "\n")
	_, found := findSection(lines, heading, matchLevel)
	if found {
		return Replace(body, heading, content, matchLevel), false
	}

	prefix := strings.Repeat("#", appendLevel)
	section := prefix + " " + heading + "\n" + content
	if body == "" {
		return "\n" + section, true
	}
	return strings.TrimRight(body, "\n") + "\n\n" + section, true
}

func isHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	stripped := strings.TrimLeft(line, "#")
	return len(stripped) > 0 && stripped[0] == ' '
}

// HeadingLevel counts the "#" characters that start line ("## H" → 2). Trim the
// line first.
func HeadingLevel(line string) int {
	return len(line) - len(strings.TrimLeft(line, "#"))
}

func exactHeading(text, target string) bool {
	return strings.ToLower(text) == target
}

func parentheticalHeading(text, target string) bool {
	return strings.HasPrefix(strings.ToLower(text), target+" (")
}

func trimTrailingBlanks(s string) string {
	return strings.TrimRight(s, "\n") + "\n"
}
