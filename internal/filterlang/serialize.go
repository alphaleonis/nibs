package filterlang

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/alphaleonis/nibs/internal/graph/model"
)

// Serialize renders f and the invalid tokens as canonical filter-box text, as
// the web's serializeQuery does. Serialize(Parse(s)) returns s for canonical s.
//
// The text lists the metadata fields in Fields order, each include token before
// its exclude token. The relationship and existence tokens follow in RelTokens
// order, then the area tokens in AreaTokens order, then the search, then the
// invalid tokens. A value list collapses a complete group to its name, so
// `status:draft,todo,in-progress` serializes as `status:open`.
func Serialize(f model.NibFilter, invalid []string) string {
	var parts []string
	for _, field := range fields {
		if values := *fieldOf[[]string](&f, field.FilterKey); len(values) > 0 {
			parts = append(parts, field.Name+":"+strings.Join(collapse(field, values), ","))
		}
		if values := *fieldOf[[]string](&f, field.ExcludeKey); len(values) > 0 {
			parts = append(parts, "-"+field.Name+":"+strings.Join(collapse(field, values), ","))
		}
	}
	for _, r := range relTokens {
		if r.Kind == RelID {
			if id := *fieldOf[*string](&f, r.Field); id != nil && *id != "" {
				parts = append(parts, r.Name+":"+*id)
			}
		} else if set := *fieldOf[*bool](&f, r.Field); set != nil && *set == r.Value {
			parts = append(parts, r.Token)
		}
	}
	for _, a := range areaTokens {
		if path := *fieldOf[*string](&f, a.Field); path != nil && *path != "" {
			parts = append(parts, a.Name+":"+*path)
		}
	}
	if f.Search != nil && *f.Search != "" {
		parts = append(parts, *f.Search)
	}
	parts = append(parts, invalid...)
	return strings.Join(parts, " ")
}

// collapse returns field's values as the tokens of one value list. Each group
// whose members are all present becomes the group name, tried in group order.
// The tokens sort by the lowest canonical index they cover. A value outside the
// vocabulary sorts last, and tags sort alphabetically.
func collapse(field Field, values []string) []string {
	type ranked struct {
		token string
		rank  int
	}
	order := valueOrder[field.Values]
	rank := func(members []string) int {
		lowest := math.MaxInt
		for _, m := range members {
			if i := slices.Index(order, m); i >= 0 {
				lowest = min(lowest, i)
			}
		}
		return lowest
	}

	remaining := dedupe(values)
	var tokens []ranked
	for _, g := range valueGroups[field.Values] {
		if len(g.members) == 0 || !containsAll(remaining, g.members) {
			continue
		}
		tokens = append(tokens, ranked{g.name, rank(g.members)})
		remaining = slices.DeleteFunc(remaining, func(v string) bool { return slices.Contains(g.members, v) })
	}
	if field.Values == TagValues {
		slices.SortFunc(remaining, compareUTF16)
	} else {
		slices.SortStableFunc(remaining, func(a, b string) int { return cmp.Compare(indexOr(order, a), indexOr(order, b)) })
	}
	for _, v := range remaining {
		tokens = append(tokens, ranked{v, rank([]string{v})})
	}
	slices.SortStableFunc(tokens, func(a, b ranked) int { return cmp.Compare(a.rank, b.rank) })

	out := make([]string, len(tokens))
	for i, t := range tokens {
		out[i] = t.token
	}
	return out
}

// compareUTF16 orders a and b by their UTF-16 code units, as JavaScript's `<`
// compares strings.
func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}

// indexOr returns the index of v in order, or len(order) when v is absent.
func indexOr(order []string, v string) int {
	if i := slices.Index(order, v); i >= 0 {
		return i
	}
	return len(order)
}

func containsAll(set, members []string) bool {
	for _, m := range members {
		if !slices.Contains(set, m) {
			return false
		}
	}
	return true
}
