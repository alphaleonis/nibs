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
// requested level and returns its line range, preferring an EXACT text match
// over a parenthetical-suffix fallback. matchLevel is a sentinel: AnyLevel
// matches a heading of any level (wildcard); N>0 matches only a heading spelled
// at level N (so a matchLevel of 3 will not match a level-2 "## Sub").
//
// Matching runs in two passes so an exact heading always wins regardless of
// document order. Pass 1 scans for a heading whose text equals the target
// exactly; only when pass 1 finds nothing does pass 2 scan for a heading whose
// text is "<target> (…)" — a parenthetical suffix (so a bare "Foo" can still
// target a lone "Foo (Phase 1)"). Both passes apply the same level gate.
func findSection(lines []string, heading string, matchLevel int) (section, bool) {
	target := strings.ToLower(heading)

	if sec, found := scanSection(lines, target, matchLevel, exactHeading); found {
		return sec, true
	}
	return scanSection(lines, target, matchLevel, parentheticalHeading)
}

// scanSection returns the first section whose heading satisfies match and the
// level gate. match receives the heading text (with its "#" markers stripped)
// and the already-lower-cased target. Splitting the scan out lets findSection
// run it once per matching predicate (exact, then parenthetical) while the
// section-span computation and level gate stay in a single place.
func scanSection(lines []string, target string, matchLevel int, match func(text, target string) bool) (section, bool) {
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
		if !match(text, target) || (matchLevel != AnyLevel && l != matchLevel) {
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
// and whether the section was found. An EXACT heading match is preferred over a
// parenthetical-suffix match: "Key Decisions" matches an exact "Key Decisions" heading
// anywhere in the body before falling back to a "Key Decisions (…)" heading. matchLevel is a
// sentinel: AnyLevel matches a heading of any level (wildcard); N>0 matches only a heading
// spelled at level N.
func Find(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := findSection(lines, heading, matchLevel)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// FindExact locates a section by heading text like Find, but runs ONLY the exact
// case-insensitive pass — it NEVER falls back to a "<heading> (…)"
// parenthetical-suffix match. matchLevel is the same sentinel as Find (AnyLevel
// matches any level; N>0 matches only level N).
//
// Use FindExact when the distinction between an exact heading and a parenthetical
// one is load-bearing. Because Find prefers an exact heading over a parenthetical
// one, a freshly-created exact heading WINS a wildcard read even when a lone
// "<heading> (…)" also exists — so a caller reasoning about whether that new
// exact heading would be shadowed must key on an EXACT existing heading, which
// only FindExact detects.
func FindExact(body, heading string, matchLevel int) (string, bool) {
	lines := strings.Split(body, "\n")
	sec, found := scanSection(lines, strings.ToLower(heading), matchLevel, exactHeading)
	if !found {
		return "", false
	}
	content := strings.Join(lines[sec.startIdx:sec.endIdx], "\n") + "\n"
	return trimTrailingBlanks(content), true
}

// Replace replaces the content of a named section, keeping the heading line intact.
// The heading is matched case-insensitively, preferring an EXACT heading over a
// parenthetical-suffix fallback (see Find). Content between the heading and the next
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
// none matches — matching an existing heading at ANY level (wildcard) and
// preferring an EXACT heading over a parenthetical-suffix fallback (see Find).
// appendLevel is the level of the heading created when no match exists (clamped
// to at least 1). This is the wildcard-match variant: use it for callers that
// target a heading regardless of the level it is spelled at. Use SetAtLevel to
// gate the match to a specific level.
func Set(body string, appendLevel int, heading, content string) string {
	return SetAtLevel(body, AnyLevel, appendLevel, heading, content)
}

// SetAtLevel replaces a section's content if a matching section is found, or
// appends a new section if not. An EXACT heading is preferred over a
// parenthetical-suffix fallback (see Find). matchLevel selects which existing
// heading counts as a match (AnyLevel = any level/wildcard, N>0 = only level N);
// appendLevel is the level of the heading created when no match exists (clamped
// to at least 1).
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

// exactHeading reports whether a heading's text is an exact case-insensitive
// match for the target. text is the heading with its "#" markers already
// stripped; target is lower-cased by the caller. This is findSection's
// preferred (first-pass) predicate: an exact heading always wins over a
// parenthetical-suffix one.
func exactHeading(text, target string) bool {
	return strings.ToLower(text) == target
}

// parentheticalHeading reports whether a heading's text is the target followed
// by a parenthetical suffix, so "Key Decisions" matches "Key Decisions (Phase 2)"
// but not "Key Decisions Extended". This is findSection's fallback (second-pass)
// predicate, used only when no exact heading exists, letting a bare "Foo" target
// a lone "Foo (…)" heading.
func parentheticalHeading(text, target string) bool {
	return strings.HasPrefix(strings.ToLower(text), target+" (")
}

// trimTrailingBlanks removes trailing blank lines from content,
// keeping a single trailing newline.
func trimTrailingBlanks(s string) string {
	return strings.TrimRight(s, "\n") + "\n"
}
