package filterlang

import "github.com/alphaleonis/nibs/internal/area"

// SpanKind is how a highlight span colors its text.
type SpanKind string

// The span kinds, spelled as the web's tokenizeSpans spells them.
const (
	// SpanField is a recognized token's name, its minus included: `-tags`,
	// `blocked-by`, `has`.
	SpanField SpanKind = "field"
	// SpanOperator is the colon after a name, or a comma between values.
	SpanOperator SpanKind = "operator"
	// SpanValue is a value the parser accepts.
	SpanValue SpanKind = "value"
	// SpanInvalid is what the parser rejects: a value or area path it keeps out
	// of the filter, or a whole negated relationship, existence or area token.
	SpanInvalid SpanKind = "invalid"
	// SpanFreeText is a token the parser adds to the full-text search, `type:`
	// and `title:foo` included.
	SpanFreeText SpanKind = "freetext"
	// SpanWhitespace is a whitespace run.
	SpanWhitespace SpanKind = "whitespace"
)

// Span is one highlighted stretch of filter text, text[Start:End].
type Span struct {
	// Start and End are byte offsets into the text.
	Start, End int
	Kind       SpanKind
}

// Spans splits filter text into highlight spans as the web's tokenizeSpans
// does with no area vocabulary, so every area path is a value.
//
// The spans tile the text in order, each non-empty, so their texts joined
// reproduce it.
func Spans(text string) []Span {
	return SpansWithAreas(text, nil)
}

// SpansWithAreas is Spans that marks an area path areas does not declare
// invalid, as ParseWithAreas rejects it. A nil areas accepts every path.
func SpansWithAreas(text string, areas *area.Vocabulary) []Span {
	var out []Span
	for _, r := range runs(text) {
		if r.space {
			out = append(out, Span{r.start, r.end, SpanWhitespace})
			continue
		}
		out = appendTokenSpans(out, classify(text, r.start, r.end), areas)
	}
	return out
}

func appendTokenSpans(out []Span, t token, areas *area.Vocabulary) []Span {
	switch t.kind {
	case fieldToken:
		out = append(out, Span{t.start, t.colon, SpanField}, Span{t.colon, t.colon + 1, SpanOperator})
		for i, p := range t.values {
			if p.end > p.start {
				kind := SpanValue
				if !p.valid {
					kind = SpanInvalid
				}
				out = append(out, Span{p.start, p.end, kind})
			}
			if i < len(t.values)-1 {
				out = append(out, Span{p.end, p.end + 1, SpanOperator})
			}
		}
		return out
	case relIDToken, existenceToken:
		return appendScalarSpans(out, t, SpanValue)
	case areaToken:
		if refusesArea(areas, t.value) {
			return appendScalarSpans(out, t, SpanInvalid)
		}
		return appendScalarSpans(out, t, SpanValue)
	case parkedToken:
		return append(out, Span{t.start, t.end, SpanInvalid})
	default:
		return append(out, Span{t.start, t.end, SpanFreeText})
	}
}

// appendScalarSpans appends a `name:value` token as its name, its colon, and
// the whole run after the colon as one span, since a scalar token's value keeps
// any later comma or colon.
func appendScalarSpans(out []Span, t token, valueKind SpanKind) []Span {
	return append(out,
		Span{t.start, t.colon, SpanField},
		Span{t.colon, t.colon + 1, SpanOperator},
		Span{t.colon + 1, t.end, valueKind})
}
