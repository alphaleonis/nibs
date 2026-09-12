package mdsection

import "strings"

// AnyLevel is the wildcard match level: it matches a heading at any level, where
// a level N>0 matches only a heading spelled at exactly N. Spell the level to
// stop a "### Sub" request matching — and then clobbering — a level-2 "## Sub".
const AnyLevel = 0

// section holds the line range of a found section.
type section struct {
	headingIdx int // index of the heading line
	startIdx   int // index of first content line (headingIdx + 1)
	endIdx     int // index of first line NOT in the section (next heading or len(lines))
	level      int // heading level (number of # chars)
}

// findSection locates a section by heading text (case-insensitive) at matchLevel
// and returns its line range.
//
// TWO PASSES, so an exact heading wins regardless of document order: pass 1 takes
// a heading whose text equals the target, and only if that finds nothing does
// pass 2 take one spelled "<target> (…)", letting a bare "Foo" reach a lone
// "Foo (Phase 1)". Both passes apply the same level gate.
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
// heading at equal or higher level — and whether it was found. The heading is
// matched case-insensitively at matchLevel, exact before parenthetical (see
// findSection).
func Find(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading, matchLevel)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// FindExact is Find with pass 2 disabled: it never falls back to a
// "<heading> (…)" match.
//
// Reach for it when the difference between an exact heading and a parenthetical
// one decides something. A newly created exact heading wins a wildcard read over
// a lone parenthetical one, so a caller asking whether that new heading would be
// shadowed has to ask about an EXACT existing heading — which Find cannot answer.
func FindExact(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := scanSection(lines, strings.ToLower(heading), matchLevel, exactHeading)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// Replace swaps a section's content for newContent, keeping the heading line. The
// section is matched as findSection describes. A body with no matching section
// comes back unchanged — check first if that is not what you want.
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

// Set is SetAtLevel matching at AnyLevel, for a caller that targets a heading
// whatever level it is spelled at. Both returns come straight through; see
// SetAtLevel for the bool.
func Set(body string, appendLevel int, heading, content string) (string, bool) {
	return SetAtLevel(body, AnyLevel, appendLevel, heading, content)
}

// SetAtLevel replaces a matching section's content, or appends a new section when
// none matches. The two levels are separate so a caller can demand a match at an
// exact level yet still create at a chosen one: matchLevel picks which existing
// heading counts (see findSection), appendLevel spells the heading created when
// none does, clamped to at least 1.
//
// CANONICAL INVARIANT (the append-vs-replace signal). The returned bool is
// appended: true when a new heading was appended at appendLevel, false when an
// existing section was replaced in place. It is the write's own record of what it
// did — read it rather than re-running a Find, which can disagree with the write.
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
	// Exactly one blank line before the new heading. Trim first: the body's own
	// trailing newlines are a terminator, not a separator, so a body ending blank
	// would push the heading down one line per append.
	return strings.TrimRight(body, "\n") + "\n\n" + section, true
}

// isHeading returns true if the line starts with one or more # followed by a space.
func isHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	stripped := strings.TrimLeft(line, "#")
	return len(stripped) > 0 && stripped[0] == ' '
}

// HeadingLevel returns the number of # characters starting a heading line
// ("## H" → 2). It counts from the first rune, so pass a trimmed line.
func HeadingLevel(line string) int {
	return len(line) - len(strings.TrimLeft(line, "#"))
}

// exactHeading reports whether a heading's text matches the target exactly,
// case-insensitively. findSection's first pass.
func exactHeading(text, target string) bool {
	return strings.ToLower(text) == target
}

// parentheticalHeading reports whether a heading's text is the target followed by
// a parenthetical suffix: "Key Decisions" matches "Key Decisions (Phase 2)" but
// not "Key Decisions Extended". findSection's second pass.
func parentheticalHeading(text, target string) bool {
	return strings.HasPrefix(strings.ToLower(text), target+" (")
}

// trimTrailingBlanks removes trailing blank lines, keeping one trailing newline.
func trimTrailingBlanks(s string) string {
	return strings.TrimRight(s, "\n") + "\n"
}
