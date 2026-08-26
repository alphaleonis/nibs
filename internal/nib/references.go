package nib

import (
	"regexp"
	"slices"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MentionIDPattern is the raw token-grammar fragment shared between the
// scanner's sigil-anchored regex and the test-side shape invariant, so a
// widening of the grammar automatically flows to the fuzz/table invariants
// without requiring a second literal update.
const MentionIDPattern = `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`

// mentionPattern matches a `#` sigil followed by a nib ID token.
// The token is alphanumeric-with-hyphens, starting and ending with alphanumeric.
// Captured group 1 is the id (short or full form), without the leading `#`.
var mentionPattern = regexp.MustCompile(`#(` + MentionIDPattern + `)`)

// MentionSpan locates one occurrence of a `#<id>` mention in a body.
//
// Start is the offset of the `#` sigil and Stop is one past the last byte of
// the id, so body[Start:Stop] is exactly the text `#`+Token. Both are byte
// offsets into the body the span was extracted from, not rune indices.
type MentionSpan struct {
	Token string
	Start int
	Stop  int
}

// ExtractMentionSpans scans a nib body and returns every occurrence of a
// `#<id>` mention, located by byte offset and ordered by ascending Start.
//
// It is the located, un-deduplicated form of ExtractMentionTokens, which is
// built on it — see that function for the full node-skipping and token rules,
// which are shared because there is one walk behind both.
//
// Two properties are what callers that REWRITE a body depend on, and neither is
// incidental:
//
//   - Every occurrence is reported. A body may mention the same id repeatedly,
//     and a rewrite driven by the deduplicated token list would move only the
//     first.
//   - Spans ascend by Start and never overlap, so a caller splicing the body
//     right-to-left (highest offset first) keeps the offsets it has not reached
//     yet valid against the string it is editing.
func ExtractMentionSpans(body string) []MentionSpan {
	if body == "" {
		return nil
	}

	source := []byte(body)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(text.NewReader(source))

	var out []MentionSpan

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n.(type) {
		case *ast.CodeSpan,
			*ast.CodeBlock,
			*ast.FencedCodeBlock,
			*ast.Link,
			*ast.AutoLink,
			*ast.Image,
			*ast.RawHTML,
			*ast.HTMLBlock:
			return ast.WalkSkipChildren, nil
		}

		t, ok := n.(*ast.Text)
		if !ok {
			return ast.WalkContinue, nil
		}

		out = scanMentions(source, t.Segment.Start, t.Segment.Stop, out)
		return ast.WalkContinue, nil
	})

	// The AST walk visits text leaves in document order, so this sort is a
	// no-op today. It stays because the ascending-offset guarantee is what a
	// right-to-left body rewrite rests on, and inheriting it from goldmark's
	// traversal order would make it a property of a dependency rather than of
	// this function. The cost is bounded by the mention count, not body length.
	slices.SortFunc(out, func(a, b MentionSpan) int { return a.Start - b.Start })

	return out
}

// ExtractMentionTokens scans a nib body and returns the list of mention tokens
// referenced via the `#<id>` sigil convention. Tokens are returned in the order
// of their first appearance in document order and deduplicated. Callers that
// need every occurrence, or the offsets, use ExtractMentionSpans — this is that
// list with repeats dropped.
//
// Parsing is delegated to a CommonMark parser (goldmark). Mentions are
// extracted from *text* leaves only; the following node types are skipped
// entirely and their contents never scanned:
//
//   - Fenced code blocks and indented code blocks
//   - Inline code spans (including multi-backtick spans)
//   - Link text and link URLs (inline, reference, and autolinks)
//   - Image alt text and URLs
//   - HTML blocks, and raw inline HTML tags — only the TAG is raw HTML, so the
//     text between `<b>` and `</b>` is an ordinary text leaf and is scanned
//
// Heading text is walked normally — a mention inside a heading (e.g.
// `## Related: #abc`) produces a token. The leading `#` run that marks the
// heading itself is not part of the heading's text children, so it cannot
// be mistaken for a mention sigil.
//
// Reference-link definitions (`[foo]: #anchor`) are likewise not walked as
// text, so their URLs never contribute mentions.
//
// Additional token rules applied to each text leaf:
//
//   - `<id>` is matched by the regex `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?` —
//     lowercase alphanumerics with optional internal hyphens. Uppercase IDs
//     do not match. A leading/trailing hyphen is not part of the token
//     (e.g. `#abc-` yields `abc`).
//   - `#` preceded by a word-like byte (letters of either case, digits, or
//     `_`) is rejected, so `email#foo` and `name_#bar` do not match. This
//     rule is enforced against the absolute source offset, not the
//     segment-local offset — goldmark can split inline text at emphasis
//     delimiters (e.g. `_`) such that the `#` ends up at index 0 of a
//     new text segment while the previous source byte is still word-like.
//   - Tokens are deduplicated across the whole body.
//
// The function does not verify that the returned tokens resolve to real nibs;
// callers perform that check against the nib map.
//
// Complexity is O(n) in body length: a single goldmark AST walk plus one
// regex scan per text leaf. Callers may pass arbitrarily large bodies
// without quadratic blowup.
func ExtractMentionTokens(body string) []string {
	spans := ExtractMentionSpans(body)
	if len(spans) == 0 {
		return nil
	}

	out := make([]string, 0, len(spans))
	seen := make(map[string]struct{}, len(spans))
	for _, s := range spans {
		if _, dup := seen[s.Token]; dup {
			continue
		}
		seen[s.Token] = struct{}{}
		out = append(out, s.Token)
	}
	return out
}

// scanMentions runs the mention regex over a text segment of source (bounded
// by [segStart, segStop)) and appends a span per match to out, returning the
// grown slice.
//
// It enforces the left-side word-boundary rule: a `#` preceded by a word-like
// byte (see isWordChar) is ignored. Because goldmark splits inline text at
// emphasis and other delimiters, a `#` may sit at index 0 of a segment while
// the previous byte in the underlying source is still word-like (e.g.
// `name_#bar` splits so the `#bar` segment starts at the `#`). The peek must
// be against the absolute source offset, not the segment-local index.
func scanMentions(source []byte, segStart, segStop int, out []MentionSpan) []MentionSpan {
	chunk := source[segStart:segStop]
	for _, match := range mentionPattern.FindAllSubmatchIndex(chunk, -1) {
		// match[0] is start of full match (the `#`) within chunk,
		// match[1] is end. match[2]/match[3] are start/end of the
		// captured id within chunk.
		absSigil := segStart + match[0]
		if absSigil > 0 {
			prev := source[absSigil-1]
			// Reject if `#` is preceded by an alphanumeric/underscore byte
			// (i.e. the `#` is inside a word like `email#foo` or
			// `name_#bar`).
			if isWordChar(prev) {
				continue
			}
		}
		out = append(out, MentionSpan{
			Token: string(chunk[match[2]:match[3]]),
			Start: absSigil,
			Stop:  segStart + match[1],
		})
	}
	return out
}

// isWordChar is intentionally broader than the mention-id regex character class.
// We reject `#` preceded by any word-like char (upper/lower letters, digits,
// underscore) so patterns like "Foo#bar" or "name_#bar" do not produce mentions,
// even though the id body only accepts lowercase alphanumerics. Keep this asymmetry.
//
// The guard is deliberately byte-level and ASCII-only: non-ASCII runes
// (e.g. `é`, CJK, emoji) are not treated as word-like, so `é#gx0f` produces
// a mention. This is acceptable because the id alphabet is ASCII — prose
// in any other script adjacent to a `#` is not strongly "word-like" for
// the purpose of distinguishing a sigil from an identifier. Tracked this
// way intentionally; do not widen to `unicode.IsLetter` without a test
// to pin the new behavior.
func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
