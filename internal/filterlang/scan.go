package filterlang

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/nib"
)

var tagPattern = regexp.MustCompile(nib.TagPattern)

// run is a maximal stretch of the text that is all whitespace or all not.
// Offsets here and in token are byte offsets into the whole text.
type run struct {
	start, end int
	space      bool
}

// runs splits text into alternating whitespace and token runs that tile it.
func runs(text string) []run {
	var out []run
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		space := isSpace(r)
		if n := len(out); n > 0 && out[n-1].space == space {
			out[n-1].end = i + size
		} else {
			out = append(out, run{start: i, end: i + size, space: space})
		}
		i += size
	}
	return out
}

// tokenKind is how the parser routes a token.
type tokenKind uint8

const (
	// textToken is a bare word, which joins the full-text search.
	textToken tokenKind = iota
	// fieldToken is a metadata field with at least one non-empty value.
	fieldToken
	relIDToken
	existenceToken
	areaToken
	// parkedToken is a negated relationship, existence or area token, which
	// sets nothing because only metadata fields have an exclude list.
	parkedToken
)

// token is one token run, classified the way the web's parseQuery routes it.
type token struct {
	kind       tokenKind
	start, end int
	// colon is the offset of the colon after the name of a fieldToken,
	// relIDToken, existenceToken or areaToken.
	colon int
	// negated, field and values describe a fieldToken. values holds every
	// comma-separated part in order, empty parts included.
	negated bool
	field   Field
	values  []valuePart
	// rel is the table entry of a relIDToken or an existenceToken.
	rel RelToken
	// area is the table entry of an areaToken.
	area AreaToken
	// value is a relIDToken's id, lowercased, or an areaToken's path as typed.
	value string
	// parked is a parkedToken as the web spells it among its invalid tokens.
	parked string
}

// valuePart is one comma-separated part of a fieldToken's value list.
type valuePart struct {
	start, end int
	// value is the part lowercased.
	value string
	valid bool
}

var (
	fieldByName     = make(map[string]Field)
	relIDByName     = make(map[string]RelToken)
	existenceByText = make(map[string]RelToken)
	areaByName      = make(map[string]AreaToken)
)

func init() {
	for _, f := range fields {
		fieldByName[f.Name] = f
	}
	for _, r := range relTokens {
		if r.Kind == RelID {
			relIDByName[r.Name] = r
		} else {
			existenceByText[r.Token] = r
		}
	}
	for _, a := range areaTokens {
		areaByName[a.Name] = a
	}
}

// classify reads the token run text[start:end]. It tries a metadata field, then
// a relationship or existence token, then an area token. Anything else is a
// bare word.
func classify(text string, start, end int) token {
	s := text[start:end]
	if t, ok := classifyField(s, start); ok {
		return t
	}
	if t, ok := classifyRel(s, start); ok {
		return t
	}
	if t, ok := classifyArea(s, start); ok {
		return t
	}
	return token{kind: textToken, start: start, end: start + len(s)}
}

// classifyField reads `[-]name:values` for a metadata field name of ASCII
// letters, as the web's FIELD_TOKEN pattern does. A field whose parts are all
// empty (`type:,`) is a bare word.
func classifyField(s string, start int) (token, bool) {
	body, negated := strings.CutPrefix(s, "-")
	colon := strings.IndexByte(body, ':')
	if colon <= 0 || colon == len(body)-1 || !isASCIILetters(body[:colon]) {
		return token{}, false
	}
	f, ok := fieldByName[strings.ToLower(body[:colon])]
	if !ok {
		return token{}, false
	}
	t := token{kind: textToken, start: start, end: start + len(s), negated: negated, field: f}
	t.colon = t.end - len(body) + colon
	pos := t.colon + 1
	for part := range strings.SplitSeq(body[colon+1:], ",") {
		value := lower(part)
		t.values = append(t.values, valuePart{start: pos, end: pos + len(part), value: value, valid: isValidValue(f, value)})
		if part != "" {
			t.kind = fieldToken
		}
		pos += len(part) + 1
	}
	return t, true
}

// classifyRel reads a relationship-id or existence token. A negated one is
// parked.
func classifyRel(s string, start int) (token, bool) {
	body, negated := strings.CutPrefix(s, "-")
	t, ok := matchRel(body)
	if !ok {
		return token{}, false
	}
	t.start, t.end = start, start+len(s)
	if negated {
		return token{kind: parkedToken, start: t.start, end: t.end, parked: lower(s)}, true
	}
	t.colon += start
	return t, true
}

// matchRel reads an unnegated relationship-id or existence token. An existence
// token is matched whole, in any letter case. An id token takes the whole run
// after its first colon as the id, commas included.
func matchRel(s string) (token, bool) {
	colon := strings.IndexByte(s, ':')
	if r, ok := existenceByText[lower(s)]; ok {
		return token{kind: existenceToken, rel: r, colon: colon}, true
	}
	if colon <= 0 {
		return token{}, false
	}
	r, ok := relIDByName[lower(s[:colon])]
	value := lower(s[colon+1:])
	if !ok || value == "" {
		return token{}, false
	}
	return token{kind: relIDToken, rel: r, colon: colon, value: value}, true
}

// classifyArea reads an ownership token, keeping its path as typed. A negated
// one is parked.
func classifyArea(s string, start int) (token, bool) {
	body, negated := strings.CutPrefix(s, "-")
	colon := strings.IndexByte(body, ':')
	if colon <= 0 || colon == len(body)-1 {
		return token{}, false
	}
	a, ok := areaByName[lower(body[:colon])]
	if !ok {
		return token{}, false
	}
	t := token{kind: areaToken, start: start, end: start + len(s), area: a, value: body[colon+1:]}
	if negated {
		return token{kind: parkedToken, start: t.start, end: t.end, parked: "-" + a.Name + ":" + t.value}, true
	}
	t.colon = start + colon
	return t, true
}

func isASCIILetters(s string) bool {
	for i := range len(s) {
		if c := s[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
