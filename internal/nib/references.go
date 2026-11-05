package nib

import (
	"bytes"
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

// leadingIDPattern matches the longest id at the start of its input.
var leadingIDPattern = regexp.MustCompile(`^` + MentionIDPattern)

// maxContainerDepth is the most list and blockquote markers one line may open
// in the mention parse. goldmark rescans the rest of a line for each marker it
// opens, so an unbounded prefix makes the parse quadratic in the line's length.
const maxContainerDepth = 32

// MentionForm is the spelling a mention is written in.
type MentionForm int

const (
	// MentionHash is the `#<id>` spelling.
	MentionHash MentionForm = iota
	// MentionBracket is the `[[<id>]]` spelling.
	MentionBracket
)

// MentionForms returns every mention form, in the order the forms are declared.
func MentionForms() []MentionForm {
	return []MentionForm{MentionHash, MentionBracket}
}

// Spell returns the mention of id written in form f.
func (f MentionForm) Spell(id string) string {
	if f == MentionBracket {
		return "[[" + id + "]]"
	}
	return "#" + id
}

// MentionSpan is one mention: body[Start:Stop] is Form.Spell(Token), in byte
// offsets.
type MentionSpan struct {
	Token string
	Start int
	Stop  int
	Form  MentionForm
}

// ExtractMentionSpans returns every mention in body, repeats included, as
// non-overlapping spans in ascending Start order, so a rewrite can splice right to
// left. ExtractMentionTokens describes what is scanned.
func ExtractMentionSpans(body string) []MentionSpan {
	if body == "" {
		return nil
	}

	source := []byte(body)
	boundContainerDepth(source)
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
		out = scanBracketMentions(source, t.Segment.Start, t.Segment.Stop, out)
		return ast.WalkContinue, nil
	})

	// Sort rather than rely on goldmark's traversal order.
	slices.SortFunc(out, func(a, b MentionSpan) int { return a.Start - b.Start })

	return out
}

// ExtractMentionTokens returns the distinct ids that body mentions as `#<id>` or
// `[[<id>]]`, in order of first appearance. Only CommonMark text is scanned: code
// spans and blocks, links, autolinks, images, HTML blocks, raw inline HTML tags
// and reference-link definitions are skipped, while heading text and text between
// inline tags are scanned. A `#` preceded by an ASCII letter, digit or `_` is not
// a mention, and neither are brackets that hold anything but an id. A line that
// opens more than maxContainerDepth list or blockquote markers is read as paragraph
// text past that depth. Tokens are not checked against existing nibs.
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

// scanBracketMentions appends the `[[<id>]]` mentions whose id starts in
// source[segStart:segStop] to out. goldmark splits text into separate segments at
// an unmatched `[` or `]`, so the brackets are checked in source.
func scanBracketMentions(source []byte, segStart, segStop int, out []MentionSpan) []MentionSpan {
	from := max(segStart-len(bracketOpen), 0)
	for {
		i := bytes.Index(source[from:segStop], bracketOpen)
		if i < 0 {
			return out
		}
		open := from + i
		from = open + 1
		idStart := open + len(bracketOpen)
		idLen := len(leadingIDPattern.Find(source[idStart:segStop]))
		idStop := idStart + idLen
		if idLen == 0 || !bytes.HasPrefix(source[idStop:], bracketClose) {
			continue
		}
		out = append(out, MentionSpan{
			Token: string(source[idStart:idStop]),
			Start: open,
			Stop:  idStop + len(bracketClose),
			Form:  MentionBracket,
		})
	}
}

// boundContainerDepth overwrites with '.' each container marker past
// maxContainerDepth on a line, so goldmark reads the rest of that line as
// paragraph text. Byte offsets do not change, and no mention's bytes do either.
func boundContainerDepth(src []byte) {
	for start := 0; start < len(src); {
		end := bytes.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start
		}
		boundLineDepth(src[start:end])
		start = end + 1
	}
}

func boundLineDepth(line []byte) {
	if isThematicBreakLine(line) {
		return
	}
	for i, depth := 0, 0; ; depth++ {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		n := containerMarkerLen(line[i:])
		if n == 0 {
			return
		}
		if depth >= maxContainerDepth {
			for j := i; j < i+n; j++ {
				line[j] = '.'
			}
		}
		i += n
	}
}

// containerMarkerLen returns the length of the blockquote or list marker that s
// starts with, or 0 when it starts with neither.
func containerMarkerLen(s []byte) int {
	if len(s) == 0 {
		return 0
	}
	if s[0] == '>' {
		return 1
	}
	n := 0
	if s[0] == '-' || s[0] == '+' || s[0] == '*' {
		n = 1
	} else {
		for n < len(s) && n < 9 && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		if n == 0 || n == len(s) || (s[n] != '.' && s[n] != ')') {
			return 0
		}
		n++
	}
	if n < len(s) && s[n] != ' ' && s[n] != '\t' {
		return 0
	}
	return n
}

// isThematicBreakLine reports whether line holds only one of '-', '*' or '_',
// at least three times, and blanks.
func isThematicBreakLine(line []byte) bool {
	var mark byte
	count := 0
	for _, b := range line {
		switch {
		case b == ' ' || b == '\t' || b == '\r':
		case mark == 0 && (b == '-' || b == '*' || b == '_'):
			mark, count = b, 1
		case b == mark:
			count++
		default:
			return false
		}
	}
	return count >= 3
}

var (
	bracketOpen  = []byte("[[")
	bracketClose = []byte("]]")
)

// isWordChar reports whether b is an ASCII letter of either case, a digit or '_'.
// It is wider than the id grammar, so `name_#bar` is not a mention, and ASCII-only,
// so `é#gx0f` is.
func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
