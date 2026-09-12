package nib

import (
	"regexp"
	"slices"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MentionIDPattern is the id grammar mentionPattern matches; tests build their
// token-shape check from it.
const MentionIDPattern = `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`

// mentionPattern matches `#` followed by an id; group 1 is the id.
var mentionPattern = regexp.MustCompile(`#(` + MentionIDPattern + `)`)

// MentionSpan is one `#<id>` mention: body[Start:Stop] is `#`+Token, in byte
// offsets.
type MentionSpan struct {
	Token string
	Start int
	Stop  int
}

// ExtractMentionSpans returns every `#<id>` mention in body, repeats included, as
// non-overlapping spans in ascending Start order, so a rewrite can splice right to
// left. ExtractMentionTokens describes what is scanned.
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

	// Sort rather than rely on goldmark's traversal order.
	slices.SortFunc(out, func(a, b MentionSpan) int { return a.Start - b.Start })

	return out
}

// ExtractMentionTokens returns the distinct `#<id>` tokens in body, in order of
// first appearance. Only CommonMark text is scanned: code spans and blocks, links,
// autolinks, images, HTML blocks, raw inline HTML tags and reference-link
// definitions are skipped, while heading text and text between inline tags are
// scanned. A `#` preceded by an ASCII letter, digit or `_` is not a mention.
// Tokens are not checked against existing nibs.
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

// scanMentions appends the mentions in source[segStart:segStop] to out. Check the
// byte before `#` in source, not in the segment: goldmark splits text at
// delimiters, so `name_#bar` can put `#` at the start of a segment.
func scanMentions(source []byte, segStart, segStop int, out []MentionSpan) []MentionSpan {
	chunk := source[segStart:segStop]
	for _, match := range mentionPattern.FindAllSubmatchIndex(chunk, -1) {
		// match holds full-match start/end, then id start/end, within chunk.
		absSigil := segStart + match[0]
		if absSigil > 0 {
			prev := source[absSigil-1]
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

// isWordChar reports whether b is an ASCII letter of either case, a digit or '_'.
// It is wider than the id grammar, so `name_#bar` is not a mention, and ASCII-only,
// so `é#gx0f` is.
func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
