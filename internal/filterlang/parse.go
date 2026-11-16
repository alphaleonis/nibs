package filterlang

import (
	"slices"
	"strings"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

// Reason is why a Diagnostic's token set nothing.
type Reason uint8

const (
	// UnknownValue is a metadata value outside its field's vocabulary, or a tag
	// that does not match nib.TagPattern.
	UnknownValue Reason = iota + 1
	// UndeclaredArea is an area path the vocabulary does not declare.
	UndeclaredArea
	// NegatedToken is a relationship, existence or area token with a leading
	// minus. Only metadata fields can be excluded.
	NegatedToken
)

// Diagnostic is a token, or one value of a token, that the parser kept out of
// the filter.
type Diagnostic struct {
	// Token is the rejected token as the web's filter box keeps it among its
	// invalid tokens: lowercased except an area path, and for a metadata field
	// one `[-]name:value` per rejected value.
	Token string
	// Start and End are the byte offsets of the rejected text: the value for a
	// rejected value or area path, the whole token for a NegatedToken.
	Start, End int
	Reason     Reason
}

// Parse reads filter-box text into a NibFilter as the web's parseQuery does
// when it has no area vocabulary, so every area path is kept.
//
// Each Diagnostic is one rejected occurrence, in text order. ParseWithAreas
// says how the text is read.
func Parse(text string) (model.NibFilter, []Diagnostic) {
	return ParseWithAreas(text, nil)
}

// ParseWithAreas is Parse that also rejects an area path areas does not
// declare. A nil areas keeps every path.
//
// The text is split into tokens on whitespace as JavaScript's `\s` matches it.
// Each token is read as follows:
//
//   - `field:v1,v2` adds each value to the field's list, and `-field:v1,v2` to
//     its exclude list. A group name such as `status:open` adds its members.
//     Lists keep the first occurrence of each value, in text order.
//   - A relationship-id, existence or area token sets its field. The last one
//     wins.
//   - A rejected value or path, or a negated relationship, existence or area
//     token, sets nothing and yields a Diagnostic.
//   - Anything else, including `title:foo` and a value-less `type:`, joins the
//     full-text search, separated by single spaces.
//
// Field names and values are lowercased, except area paths.
func ParseWithAreas(text string, areas *area.Vocabulary) (model.NibFilter, []Diagnostic) {
	var f model.NibFilter
	var diags []Diagnostic
	var words []string
	for _, r := range runs(text) {
		if r.space {
			continue
		}
		t := classify(text, r.start, r.end)
		switch t.kind {
		case textToken:
			words = append(words, text[t.start:t.end])
		case fieldToken:
			key, prefix := t.field.FilterKey, ""
			if t.negated {
				key, prefix = t.field.ExcludeKey, "-"
			}
			list := fieldOf[[]string](&f, key)
			for _, p := range t.values {
				switch {
				case p.value == "":
				case p.valid:
					*list = append(*list, expandValue(t.field, p.value)...)
				default:
					diags = append(diags, Diagnostic{Token: prefix + t.field.Name + ":" + p.value, Start: p.start, End: p.end, Reason: UnknownValue})
				}
			}
		case relIDToken:
			*fieldOf[*string](&f, t.rel.Field) = &t.value
		case existenceToken:
			*fieldOf[*bool](&f, t.rel.Field) = &t.rel.Value
		case areaToken:
			if areas != nil && !areas.Exists(t.value) {
				diags = append(diags, Diagnostic{Token: t.area.Name + ":" + t.value, Start: t.colon + 1, End: t.end, Reason: UndeclaredArea})
				continue
			}
			*fieldOf[*string](&f, t.area.Field) = &t.value
		case parkedToken:
			diags = append(diags, Diagnostic{Token: t.parked, Start: t.start, End: t.end, Reason: NegatedToken})
		}
	}
	for _, field := range fields {
		for _, key := range []string{field.FilterKey, field.ExcludeKey} {
			list := fieldOf[[]string](&f, key)
			*list = dedupe(*list)
		}
	}
	if search := strings.Join(words, " "); search != "" {
		f.Search = &search
	}
	return f, diags
}

// InvalidTokens returns the distinct tokens of diags in order, the list the
// web's filter box keeps and Serialize appends.
func InvalidTokens(diags []Diagnostic) []string {
	tokens := make([]string, 0, len(diags))
	for _, d := range diags {
		tokens = append(tokens, d.Token)
	}
	return dedupe(tokens)
}

// dedupe keeps the first occurrence of each value, in order. It returns nil
// for nil.
func dedupe(values []string) []string {
	if values == nil {
		return nil
	}
	seen := make(map[string]bool, len(values))
	return slices.DeleteFunc(slices.Clone(values), func(v string) bool {
		if seen[v] {
			return true
		}
		seen[v] = true
		return false
	})
}
