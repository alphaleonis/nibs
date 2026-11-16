package filterlang

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/area"
)

// CompletionKind is what a completion inserts.
type CompletionKind string

// The completion kinds. The first three are spelled as the web's getCompletion
// spells them.
const (
	// CompletionField is a field name, inserted with its colon.
	CompletionField CompletionKind = "field"
	// CompletionValue is a metadata value or group name, an existence value, or
	// an area path.
	CompletionValue CompletionKind = "value"
	// CompletionTag is a tag from Vocabulary.Tags.
	CompletionTag CompletionKind = "tag"
	// CompletionNib is a nib id from Vocabulary.SearchNibs.
	CompletionNib CompletionKind = "nib"
)

// NibSuggestion is a nib that a relationship-id token's value may name.
type NibSuggestion struct {
	ID, Title, Type, Status string
}

// Vocabulary holds the values completion offers that the token tables do not.
type Vocabulary struct {
	// Tags are the tags offered for `tags:`, in the order the menu lists them.
	Tags []string
	// Areas declares the paths offered for `area:` and `area-exact:`. A nil
	// Areas offers none.
	Areas *area.Vocabulary
	// SearchNibs returns the nibs offered for a relationship-id token's value,
	// best match first. The fragment is the value as typed, never empty. Types
	// holds the nib types the token accepts, or nil when it accepts any. A nil
	// SearchNibs offers none.
	SearchNibs func(fragment string, types []string) []NibSuggestion
}

// Completion is one menu row, and the edit that accepting it makes.
type Completion struct {
	Kind CompletionKind
	// Item is the row's text: a field name without its colon, a value, a tag,
	// an area path or a nib id.
	Item string
	// Nib is the suggested nib of a CompletionNib.
	Nib NibSuggestion
	// Start and End are the byte offsets of the text that Insert replaces.
	Start, End int
	Insert     string
	// Cursor is the byte offset of the cursor in the edited text.
	Cursor int
}

// Apply returns text with c's edit made, and the cursor's byte offset in it.
func (c Completion) Apply(text string) (string, int) {
	return text[:c.Start] + c.Insert + text[c.End:], c.Cursor
}

// RelValue is the relationship-id token value a cursor sits in.
type RelValue struct {
	// Token is the token's table entry.
	Token RelToken
	// Fragment is the whole run after the colon, as typed.
	Fragment string
	// Start and End are the byte offsets of Fragment, the range a chosen id
	// replaces.
	Start, End int
}

// RelValueAt returns the relationship-id token value that the cursor sits in,
// as the web's relTokenValueContext finds it. The cursor is a byte offset into
// text, and it sits in the value when it is past the token's colon. A negated
// token has no value, since the parser rejects it.
func RelValueAt(text string, cursor int) (RelValue, bool) {
	cursor = clampCursor(text, cursor)
	start, end := tokenStart(text, cursor), tokenEnd(text, cursor)
	s := text[start:end]
	colon := strings.IndexByte(s, ':')
	if strings.HasPrefix(s, "-") || colon <= 0 || cursor <= start+colon {
		return RelValue{}, false
	}
	r, ok := relIDByName[lower(s[:colon])]
	if !ok {
		return RelValue{}, false
	}
	r.SearchTypes = slices.Clone(r.SearchTypes)
	return RelValue{Token: r, Fragment: s[colon+1:], Start: start + colon + 1, End: end}, true
}

// Complete returns the completions for the cursor, a byte offset into text, as
// the web's filter box offers them while the user types.
//
// In a relationship-id token's value, as RelValueAt finds it, Complete offers
// the nibs v.SearchNibs returns for the whole value, and nothing while the
// value is empty. A chosen id replaces the whole value.
//
// Elsewhere Complete offers what the web's getCompletion offers for the token
// from the last whitespace up to the cursor, for tags that match nib.TagPattern:
//
//   - A partial name offers the field names it begins, and after a minus only
//     the metadata names. A chosen name replaces the token up to the cursor
//     with the name and a colon, and a space when the cursor sits against more
//     text.
//   - After a metadata field's last comma or colon, the field's group names and
//     values, or v.Tags for `tags`, that contain the partial value in lower
//     case. A value listed earlier in the token is not offered again.
//   - After `has:`, `no:` or `is:`, the values the word takes that contain the
//     partial value in lower case.
//   - After `area:` or `area-exact:`, the paths v.Areas declares that contain
//     the partial path in any letter case.
//
// A chosen value replaces the partial value. An empty token offers nothing.
func Complete(text string, cursor int, v Vocabulary) []Completion {
	return complete(text, cursor, v, false)
}

// CompleteExplicit is Complete for a completion the user asked for, such as by
// a key press. An empty token then offers every field name.
func CompleteExplicit(text string, cursor int, v Vocabulary) []Completion {
	return complete(text, cursor, v, true)
}

func complete(text string, cursor int, v Vocabulary, explicit bool) []Completion {
	cursor = clampCursor(text, cursor)
	if rel, ok := RelValueAt(text, cursor); ok {
		return completeNib(rel, v)
	}
	start := tokenStart(text, cursor)
	prefix := text[start:cursor]
	if prefix == "" && !explicit {
		return nil
	}
	body, negated := strings.CutPrefix(prefix, "-")
	colon := strings.IndexByte(body, ':')
	if colon < 0 {
		return completeFieldName(text, start, cursor, body, negated)
	}
	name, val := lower(body[:colon]), body[colon+1:]
	if !negated {
		if _, ok := areaByName[name]; ok {
			return completeArea(cursor, val, v.Areas)
		}
		if words, ok := existenceValues[name]; ok {
			return valueCompletions(CompletionValue, cursor, val, words, nil)
		}
	}
	f, ok := fieldByName[name]
	if !ok {
		return nil
	}
	lastComma := strings.LastIndexByte(val, ',')
	chosen := map[string]bool{}
	for part := range strings.SplitSeq(val[:lastComma+1], ",") {
		chosen[lower(part)] = true
	}
	if f.Values == TagValues {
		return valueCompletions(CompletionTag, cursor, val[lastComma+1:], v.Tags, chosen)
	}
	return valueCompletions(CompletionValue, cursor, val[lastComma+1:], completionValues(f), chosen)
}

func completeNib(rel RelValue, v Vocabulary) []Completion {
	if rel.Fragment == "" || v.SearchNibs == nil {
		return nil
	}
	var out []Completion
	for _, n := range v.SearchNibs(rel.Fragment, rel.Token.SearchTypes) {
		out = append(out, Completion{
			Kind: CompletionNib, Item: n.ID, Nib: n,
			Start: rel.Start, End: rel.End, Insert: n.ID, Cursor: rel.Start + len(n.ID),
		})
	}
	return out
}

func completeFieldName(text string, start, cursor int, partial string, negated bool) []Completion {
	names, dash := allFieldNames, ""
	if negated {
		names, dash = metadataFieldNames, "-"
	}
	partial = lower(partial)
	separator := ""
	if r, _ := utf8.DecodeRuneInString(text[cursor:]); cursor < len(text) && !isSpace(r) {
		separator = " "
	}
	var out []Completion
	for _, name := range names {
		if strings.HasPrefix(name, partial) {
			insert := dash + name + ":"
			out = append(out, Completion{
				Kind: CompletionField, Item: name,
				Start: start, End: cursor, Insert: insert + separator, Cursor: start + len(insert),
			})
		}
	}
	return out
}

func completeArea(cursor int, partial string, areas *area.Vocabulary) []Completion {
	if areas == nil {
		return nil
	}
	start := cursor - len(partial)
	partial = lower(partial)
	var out []Completion
	for _, path := range areas.Paths() {
		if strings.Contains(lower(path), partial) {
			out = append(out, Completion{
				Kind: CompletionValue, Item: path,
				Start: start, End: cursor, Insert: path, Cursor: start + len(path),
			})
		}
	}
	return out
}

// valueCompletions offers each item of pool that chosen does not hold and that
// contains partial in lower case. Each replaces partial, which ends at the
// cursor.
func valueCompletions(kind CompletionKind, cursor int, partial string, pool []string, chosen map[string]bool) []Completion {
	start := cursor - len(partial)
	partial = lower(partial)
	var out []Completion
	for _, item := range pool {
		if !chosen[item] && strings.Contains(item, partial) {
			out = append(out, Completion{
				Kind: kind, Item: item,
				Start: start, End: cursor, Insert: item, Cursor: start + len(item),
			})
		}
	}
	return out
}

// completionValues returns the values offered for f: its group names, then its
// values in canonical order.
func completionValues(f Field) []string {
	var out []string
	for _, g := range valueGroups[f.Values] {
		out = append(out, g.name)
	}
	return append(out, valueOrder[f.Values]...)
}

var (
	// metadataFieldNames are the field names a minus may precede.
	metadataFieldNames []string
	// allFieldNames are every field name in canonical order: metadata fields,
	// relationship ids, existence words, then area tokens.
	allFieldNames []string
	// existenceValues maps each existence word to the values it takes, in
	// table order.
	existenceValues = map[string][]string{}
)

func init() {
	for _, f := range fields {
		metadataFieldNames = append(metadataFieldNames, f.Name)
	}
	var idNames, words []string
	for _, r := range relTokens {
		if r.Kind == RelID {
			idNames = append(idNames, r.Name)
			continue
		}
		word, val, _ := strings.Cut(r.Token, ":")
		if _, ok := existenceValues[word]; !ok {
			words = append(words, word)
		}
		if !slices.Contains(existenceValues[word], val) {
			existenceValues[word] = append(existenceValues[word], val)
		}
	}
	allFieldNames = slices.Concat(metadataFieldNames, idNames, words)
	for _, a := range areaTokens {
		allFieldNames = append(allFieldNames, a.Name)
	}
}

func clampCursor(text string, cursor int) int {
	return min(max(cursor, 0), len(text))
}

// tokenStart returns the offset after the last whitespace before cursor.
func tokenStart(text string, cursor int) int {
	for cursor > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:cursor])
		if isSpace(r) {
			break
		}
		cursor -= size
	}
	return cursor
}

// tokenEnd returns the offset of the first whitespace from cursor on, or the
// end of text.
func tokenEnd(text string, cursor int) int {
	for cursor < len(text) {
		r, size := utf8.DecodeRuneInString(text[cursor:])
		if isSpace(r) {
			break
		}
		cursor += size
	}
	return cursor
}
