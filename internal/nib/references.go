package nib

import (
	"regexp"
	"strings"
)

// mentionPattern matches a `#` sigil followed by a nib ID token.
// The token is alphanumeric-with-hyphens, starting and ending with alphanumeric.
// Captured group 1 is the id (short or full form), without the leading `#`.
var mentionPattern = regexp.MustCompile(`#([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)`)

// ExtractMentionTokens scans a nib body and returns the list of mention tokens
// referenced via the `#<id>` sigil convention. Tokens are returned in order of
// first appearance, deduplicated.
//
// Rules:
//   - `#<id>` matches where `<id>` is a lowercase alphanumeric run with optional
//     internal hyphens (e.g. `#gx0f`, `#nibs-gx0f`).
//   - `#` at the start of a line followed by whitespace is treated as a
//     Markdown header and excluded.
//   - `#` appearing inside a word (preceded by an alphanumeric char) is
//     excluded, so `email#foo` does not match.
//   - Content inside fenced code blocks (``` or ~~~) is excluded entirely.
//   - Content inside inline code spans (single backticks) is excluded.
//
// The function does not verify that the returned tokens resolve to real nibs;
// callers perform that check against the nib map.
func ExtractMentionTokens(body string) []string {
	if body == "" {
		return nil
	}

	stripped := stripCodeSpans(stripFencedBlocks(body))

	var out []string
	seen := make(map[string]struct{})

	for _, line := range splitLinesKeepPositions(stripped) {
		// Skip Markdown headers: a line where the first non-space char is `#`
		// followed by one or more `#` and then a space (ATX heading).
		if isATXHeader(line) {
			continue
		}

		for _, match := range mentionPattern.FindAllStringSubmatchIndex(line, -1) {
			// match[0] is start of full match (the `#`), match[1] is end.
			// match[2]/match[3] are start/end of the captured id.
			sigilStart := match[0]
			if sigilStart > 0 {
				prev := line[sigilStart-1]
				// Reject if `#` is preceded by an alphanumeric char
				// (i.e. the `#` is inside a word like `email#foo`).
				if isWordChar(prev) {
					continue
				}
			}
			token := line[match[2]:match[3]]
			if _, dup := seen[token]; dup {
				continue
			}
			seen[token] = struct{}{}
			out = append(out, token)
		}
	}

	return out
}

// isATXHeader reports whether the line is a Markdown ATX heading
// (1-6 `#` characters at the start, optionally after up to 3 spaces,
// followed by a space or end of line).
func isATXHeader(line string) bool {
	i := 0
	// Allow up to 3 leading spaces per CommonMark.
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	hashes := 0
	for i < len(line) && line[i] == '#' && hashes < 6 {
		i++
		hashes++
	}
	if hashes == 0 {
		return false
	}
	// After the hashes, must be end-of-line or a space/tab.
	if i == len(line) {
		return true
	}
	return line[i] == ' ' || line[i] == '\t'
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// stripFencedBlocks removes content inside fenced code blocks delimited by
// runs of 3 or more backticks or tildes at the start of a line. The fence
// lines themselves are also removed. Unterminated fences are treated as
// extending to end-of-input.
func stripFencedBlocks(body string) string {
	var out strings.Builder
	out.Grow(len(body))

	lines := strings.SplitAfter(body, "\n")
	inFence := false
	var fenceChar byte
	var fenceLen int

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if !inFence {
			if ch, n := fenceStart(trimmed); n >= 3 {
				inFence = true
				fenceChar = ch
				fenceLen = n
				continue
			}
			out.WriteString(line)
		} else {
			if ch, n := fenceStart(trimmed); ch == fenceChar && n >= fenceLen {
				inFence = false
				continue
			}
			// Drop the line.
		}
	}
	return out.String()
}

// fenceStart inspects a line (already left-trimmed) and returns the fence
// character and the run length if the line begins with a fence, else 0.
func fenceStart(line string) (byte, int) {
	if len(line) == 0 {
		return 0, 0
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	n := 0
	for n < len(line) && line[n] == ch {
		n++
	}
	return ch, n
}

// stripCodeSpans removes content inside paired single backticks on the same
// logical span. This is a simplified CommonMark handling that removes the
// span (including the backticks) from the body so mention extraction ignores
// inline code. Unpaired backticks are left intact.
func stripCodeSpans(body string) string {
	var out strings.Builder
	out.Grow(len(body))

	i := 0
	for i < len(body) {
		if body[i] == '`' {
			// Find matching closing backtick on the same line.
			end := -1
			for j := i + 1; j < len(body); j++ {
				if body[j] == '\n' {
					break
				}
				if body[j] == '`' {
					end = j
					break
				}
			}
			if end != -1 {
				// Skip the span (including both backticks).
				i = end + 1
				continue
			}
		}
		out.WriteByte(body[i])
		i++
	}
	return out.String()
}

// splitLinesKeepPositions splits on `\n` and returns each line without its
// trailing newline. Preserves empty lines.
func splitLinesKeepPositions(body string) []string {
	return strings.Split(body, "\n")
}
