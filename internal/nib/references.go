package nib

import (
	"bytes"
	"regexp"
	"slices"
	"sort"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
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

// maxDefinitionParagraphLines is the most lines a paragraph may have for the
// mention parse to read link reference definitions at its start. goldmark's
// definition parser reads labels and titles through its own block reader, whose
// cost spanReader describes, so a long paragraph of definitions parses in
// quadratic time:
// https://github.com/yuin/goldmark/blob/v1.8.5/parser/link_ref.go#L18
const maxDefinitionParagraphLines = 1024

// linkScanBudgetFactor times a body's length is the most bytes the mention
// parse lets goldmark scan for inline link destinations. Every `](` is charged,
// including ones goldmark would not treat as a link start, so it is an upper bound.
const linkScanBudgetFactor = 4

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
	boundLinkScans(source)
	doc := newMentionParser().Parse(text.NewReader(source))

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

// newMentionParser returns goldmark's default parser with each inline parser
// reading through a spanReader, and with link reference definitions read only in
// paragraphs of at most maxDefinitionParagraphLines lines.
func newMentionParser() parser.Parser {
	inline := parser.DefaultInlineParsers()
	for i := range inline {
		inline[i].Value = spanValueParser{inline[i].Value.(parser.InlineParser)}
	}
	transformers := parser.DefaultParagraphTransformers()
	for i := range transformers {
		transformers[i].Value = shortParagraphTransformer{transformers[i].Value.(parser.ParagraphTransformer)}
	}
	return parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithInlineParsers(inline...),
		parser.WithParagraphTransformers(transformers...),
	)
}

// spanValueParser is an inline parser that reads its block through a spanReader.
type spanValueParser struct {
	parser.InlineParser
}

// Parse relies on goldmark resetting the block reader to parent's lines before
// it runs the inline parsers on parent:
// https://github.com/yuin/goldmark/blob/v1.8.5/parser/parser.go#L1165
func (p spanValueParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	return p.InlineParser.Parse(parent, spanReader{block, parent.Lines()}, pc)
}

// CloseBlock forwards to the wrapped parser, because goldmark finds close
// blockers by asserting on the registered parser:
// https://github.com/yuin/goldmark/blob/v1.8.5/parser/parser.go#L809
func (p spanValueParser) CloseBlock(parent ast.Node, block text.Reader, pc parser.Context) {
	if cb, ok := p.InlineParser.(parser.CloseBlocker); ok {
		cb.CloseBlock(parent, block, pc)
	}
}

// spanReader is a block reader whose Value reads only the lines that the segment
// spans, and returns what goldmark's Value returns. goldmark's Value walks back
// from the block's last line on each call, and the link parser calls it for each
// label and title line, so a long paragraph of links parses in quadratic time:
// https://github.com/yuin/goldmark/blob/v1.8.5/text/reader.go#L361-L385
type spanReader struct {
	text.Reader
	lines *text.Segments
}

func (r spanReader) Value(seg text.Segment) []byte {
	// goldmark's Value reads from the last line that starts at or before seg.Start
	// to the first line that ends after seg.Stop, or to the block's last line.
	n := r.lines.Len()
	first := sort.Search(n, func(i int) bool { return r.lines.At(i).Start > seg.Start }) - 1
	if first < 0 {
		return r.Reader.Value(seg)
	}
	last := first + sort.Search(n-first, func(i int) bool { return r.lines.At(first+i).Stop > seg.Stop })
	spanned := text.NewSegments()
	spanned.AppendAll(r.lines.Sliced(first, min(last, n-1)+1))
	return text.NewBlockReader(r.Source(), spanned).Value(seg)
}

// shortParagraphTransformer is a paragraph transformer that skips paragraphs of
// more than maxDefinitionParagraphLines lines.
type shortParagraphTransformer struct {
	parser.ParagraphTransformer
}

func (t shortParagraphTransformer) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	if node.Lines().Len() <= maxDefinitionParagraphLines {
		t.ParagraphTransformer.Transform(node, reader, pc)
	}
}

// ExtractMentionTokens returns the distinct ids that body mentions as `#<id>` or
// `[[<id>]]`, in order of first appearance. Only CommonMark text is scanned: code
// spans and blocks, links, autolinks, images, HTML blocks, raw inline HTML tags
// and reference-link definitions are skipped, while heading text and text between
// inline tags are scanned. A `#` preceded by an ASCII letter, digit or `_` is not
// a mention, and neither are brackets that hold anything but an id. A line that
// opens more than maxContainerDepth list or blockquote markers is read as paragraph
// text past that depth. Once the body's link destinations would scan more than
// linkScanBudgetFactor times its length, that `](` and every later one are read as
// text. A paragraph of more than maxDefinitionParagraphLines lines is read as text
// even where it starts with link reference definitions. Character references are
// not decoded, and the `#` in a numeric one such as `&#35;` is not a mention.
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
		if inNumericReference(source, absSigil) {
			continue
		}
		out = append(out, MentionSpan{
			Token: string(chunk[match[2]:match[3]]),
			Start: absSigil,
			Stop:  segStart + match[1],
		})
	}
	return out
}

// numericReference matches a numeric character reference after its `&`, in the
// forms goldmark's renderer decodes:
// https://github.com/yuin/goldmark/blob/v1.8.5/renderer/html/html.go#L873-L903
var numericReference = regexp.MustCompile(`^#(?:[0-9]{1,7}|[xX][0-9a-fA-F]{1,6});`)

// inNumericReference reports whether the `#` at source[sigil] belongs to a
// numeric character reference. A backslash escapes the `&` only when it is not
// itself escaped.
func inNumericReference(source []byte, sigil int) bool {
	amp := sigil - 1
	if amp < 0 || source[amp] != '&' {
		return false
	}
	backslashes := 0
	for i := amp - 1; i >= 0 && source[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 0 && numericReference.Match(source[sigil:])
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

// boundLinkScans overwrites with '.' the '(' of each `](` from the first one
// whose destination scan does not fit the body's budget, so goldmark reads
// those as text. goldmark can scan an unclosed destination to the end of its
// line, so a run of unclosed links makes the parse quadratic.
// Byte offsets do not change, and no mention's bytes do either.
func boundLinkScans(src []byte) {
	budget := linkScanBudgetFactor * len(src)
	for i := 0; ; {
		k := bytes.Index(src[i:], linkOpen)
		if k < 0 {
			return
		}
		paren := i + k + 1
		i = paren + 1
		if budget >= 0 {
			budget -= destinationScanLen(src[i:], budget)
		}
		if budget < 0 {
			src[paren] = '.'
		}
	}
}

// destinationScanLen returns how many bytes of s goldmark scans for a link
// destination when s follows an inline link's '(', or limit+1 once that passes
// limit. It follows parseLinkDestination:
// https://github.com/yuin/goldmark/blob/v1.8.5/parser/link.go#L342-L381
func destinationScanLen(s []byte, limit int) int {
	i := 0
	for i < len(s) && i <= limit && util.IsSpace(s[i]) {
		i++
	}
	if i < len(s) && s[i] == '<' {
		for i++; i < len(s) && i <= limit && s[i] != '>' && s[i] != '\n'; i++ {
			if s[i] == '\\' && i+1 < len(s) && util.IsPunct(s[i+1]) {
				i++
			}
		}
		return min(i, limit+1)
	}
	for opened := 0; i < len(s) && i <= limit; i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && util.IsPunct(s[i+1]):
			i++
		case c == '(':
			opened++
		case c == ')':
			opened--
			if opened < 0 {
				return i
			}
		case util.IsSpace(c):
			return i
		}
	}
	return min(i, limit+1)
}

var (
	linkOpen     = []byte("](")
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
