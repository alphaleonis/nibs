package filterlangcontract

import (
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/filterlang"
)

// SpanCase is one shared highlighting case: filter text, split into the spans
// the web's tokenizeSpans gives it.
type SpanCase struct {
	Name string
	// Areas is the declared area vocabulary, or nil to highlight with none,
	// which accepts every area path.
	Areas *area.Vocabulary
	// Spans tile the case's text in order.
	Spans []Piece
}

// Piece is the text of one span and its kind.
type Piece struct {
	Text string
	Kind filterlang.SpanKind
}

// Text returns the filter text the case's spans tile.
func (c SpanCase) Text() string {
	var text string
	for _, p := range c.Spans {
		text += p.Text
	}
	return text
}

// Caret marks the cursor in CompletionCase.Text and in Offer.Result. No case
// text holds it otherwise.
const Caret = "|"

// CompletionCase is one shared completion case: filter text with the cursor in
// it, the vocabulary, and what the web's relTokenValueContext and getCompletion
// make of it.
type CompletionCase struct {
	Name string
	// Text is the filter text with Caret at the cursor.
	Text string
	// Explicit marks a completion the user asked for, such as by Ctrl+Space.
	Explicit bool
	Tags     []string
	// Areas is the declared area vocabulary, or nil when there is none to offer.
	Areas *area.Vocabulary
	// Rel is the relationship-id value the cursor sits in, or nil when it sits
	// in none.
	Rel *RelWant
	// Kind is the kind of the offers, empty when there are none.
	Kind filterlang.CompletionKind
	// Offers are the rows the menu lists, in order.
	Offers []Offer
}

// RelWant is the relationship-id value the cursor sits in.
type RelWant struct {
	// Field is the GraphQL name of the NibFilter field the token writes.
	Field string
	// Name is the token's name in lower case.
	Name string
	// Range is the text without Caret, with `[` and `]` around the value.
	Range string
	// SearchTypes holds the nib types a chosen id must have, or nil for any.
	SearchTypes []string
}

// Offer is one menu row and the text that accepting it leaves.
type Offer struct {
	Item string
	// Result is the edited text with Caret at the cursor.
	Result string
}

const (
	field    = filterlang.SpanField
	operator = filterlang.SpanOperator
	value    = filterlang.SpanValue
	invalid  = filterlang.SpanInvalid
	freeText = filterlang.SpanFreeText
	space    = filterlang.SpanWhitespace
)

// spans pairs its arguments as a span's text and then its kind.
func spans(textsAndKinds ...any) []Piece {
	pieces := make([]Piece, 0, len(textsAndKinds)/2)
	for i := 0; i < len(textsAndKinds); i += 2 {
		pieces = append(pieces, Piece{textsAndKinds[i].(string), textsAndKinds[i+1].(filterlang.SpanKind)})
	}
	return pieces
}

// scalar is the spans of a `name:value` token whose value has the given kind.
func scalar(name, val string, kind filterlang.SpanKind) []Piece {
	return spans(name, field, ":", operator, val, kind)
}

// SpanCases returns the shared highlighting cases.
func SpanCases() []SpanCase {
	return []SpanCase{
		{Name: "empty text"},
		{Name: "whitespace only", Spans: spans("   ", space)},
		{Name: "a field token", Spans: scalar("type", "bug", value)},
		{Name: "the minus is part of the field name", Spans: scalar("-type", "bug", value)},
		{Name: "a comma is an operator between values", Spans: spans("type", field, ":", operator, "bug", value, ",", operator, "feature", value)},
		{Name: "only the bad value of a list is invalid", Spans: spans("type", field, ":", operator, "bug", value, ",", operator, "banana", invalid)},
		{Name: "an unknown value is invalid", Spans: scalar("status", "banana", invalid)},
		{Name: "an empty first value has no span", Spans: spans("type", field, ":", operator, ",", operator, "bug", value)},
		{Name: "a trailing comma is an operator", Spans: spans("type", field, ":", operator, "bug", value, ",", operator)},
		{Name: "two commas are two operators", Spans: spans("type", field, ":", operator, "bug", value, ",", operator, ",", operator, "task", value)},
		{Name: "letter case does not matter to a field token", Spans: scalar("TYPE", "BUG", value)},
		{Name: "a status group is a value", Spans: scalar("status", "open", value)},
		{Name: "a negated status group is a value", Spans: scalar("-status", "closed", value)},
		{Name: "a group beside an unknown value", Spans: spans("status", field, ":", operator, "open", value, ",", operator, "banana", invalid)},
		{Name: "a group of another field is invalid", Spans: scalar("type", "open", invalid)},
		{Name: "a tag matching the pattern is a value", Spans: scalar("tags", "never-seen", value)},
		{Name: "a tag failing the pattern is invalid", Spans: scalar("tags", "1bad", invalid)},
		{Name: "a tag outside ASCII is invalid", Spans: scalar("tags", "caf\u00e9", invalid)},
		{Name: "an astral character inside a value list", Spans: spans("tags", field, ":", operator, "a\U0001F600b", invalid, ",", operator, "wip", value)},

		{Name: "an unknown field is free text", Spans: spans("title:foo", freeText)},
		{Name: "a field with no value is free text", Spans: spans("type:", freeText)},
		{Name: "a field with only commas is free text", Spans: spans("type:,", freeText)},
		{Name: "a field with two commas only is free text", Spans: spans("status:,,", freeText)},
		{Name: "bare words are free text", Spans: spans("login", freeText, " ", space, "page", freeText)},
		{Name: "a double minus is free text", Spans: spans("--type:bug", freeText)},

		{Name: "a relationship-id token", Spans: scalar("parent", "tnib-1", value)},
		{Name: "a hyphenated relationship name is one field span", Spans: scalar("blocked-by", "tnib-1", value)},
		{Name: "another hyphenated relationship name", Spans: scalar("mentioned-by", "tnib-1", value)},
		{Name: "a relationship id keeps its comma", Spans: scalar("parent", "tnib-1,tnib-2", value)},
		{Name: "a relationship id keeps a later colon", Spans: scalar("sibling", "tnib:3", value)},
		{Name: "letter case does not matter to a relationship token", Spans: scalar("Parent", "TNIB-1", value)},
		{Name: "a milestone token", Spans: scalar("milestone", "tnib-1", value)},
		{Name: "an exact milestone token", Spans: scalar("milestone-exact", "tnib-1", value)},
		{Name: "an existence token", Spans: scalar("has", "parent", value)},
		{Name: "a no: existence token", Spans: scalar("no", "blocked-by", value)},
		{Name: "an is: existence token", Spans: scalar("is", "blocked", value)},
		{Name: "the backlog token", Spans: scalar("is", "backlog", value)},
		{Name: "letter case does not matter to an existence token", Spans: scalar("HAS", "PARENT", value)},
		{Name: "a negated relationship token is invalid whole", Spans: spans("-ancestor:tnib-1", invalid)},
		{Name: "a negated existence token is invalid whole", Spans: spans("-is:blocked", invalid)},
		{Name: "a negated non-spelling stays free text", Spans: spans("-has:mentions", freeText)},
		{Name: "a relationship name with no value is free text", Spans: spans("parent:", freeText)},
		{Name: "an unknown existence value is free text", Spans: spans("is:foo", freeText)},
		{Name: "an existence spelling with no predicate is free text", Spans: spans("has:ancestor", freeText)},
		{Name: "a sigma in a relationship id", Spans: scalar("parent", "A\u03a3", value)},

		{Name: "a declared area path", Areas: declaredAreas(), Spans: scalar("area", "web", value)},
		{Name: "the slash of a nested path is part of the value", Areas: declaredAreas(), Spans: scalar("area", "web/dashboard", value)},
		{Name: "an undeclared area path is invalid", Areas: declaredAreas(), Spans: scalar("area", "retired", invalid)},
		{Name: "an area path is declared only in its own case", Areas: declaredAreas(), Spans: scalar("area-exact", "Web", invalid)},
		{Name: "letter case does not matter to an area name", Areas: declaredAreas(), Spans: scalar("AREA", "web", value)},
		{Name: "an area path outside ASCII is undeclared", Areas: declaredAreas(), Spans: scalar("area", "w\u00e9b", invalid)},
		{Name: "without a vocabulary every area path is a value", Spans: scalar("area", "retired", value)},
		{Name: "an empty vocabulary declares no area path", Areas: &area.Vocabulary{}, Spans: scalar("area", "web", invalid)},
		{Name: "a negated area token is invalid whole", Areas: declaredAreas(), Spans: spans("-area:web", invalid)},
		{Name: "an area name with no value is free text", Areas: declaredAreas(), Spans: spans("area:", freeText)},

		{Name: "a valid, an invalid and a free-text token", Spans: spans(
			"type", field, ":", operator, "bug", value, " ", space,
			"status", field, ":", operator, "banana", invalid, " ", space,
			"login", freeText)},
		{Name: "every token shape with padding", Areas: declaredAreas(), Spans: spans(
			"  ", space, "type", field, ":", operator, "bug", value, "   ", space,
			"-parent:tnib-1", invalid, "   ", space,
			"has", field, ":", operator, "parent", value, " ", space,
			"area", field, ":", operator, "retired", invalid, "  ", space)},
		{Name: "help example: tasks under an epic", Spans: spans(
			"ancestor", field, ":", operator, "nibs-a1b2", value, " ", space,
			"type", field, ":", operator, "task", value)},
		{Name: "an astral character before a token", Spans: spans(
			"\U0001F600", freeText, " ", space, "type", field, ":", operator, "bug", value)},
		{Name: "a no-break space separates tokens", Spans: spans(
			"type", field, ":", operator, "bug", value, "\u00a0", space,
			"status", field, ":", operator, "todo", value)},
		{Name: "a byte order mark, tab and newline are whitespace", Spans: spans(
			"\ufeff", space, "login", freeText, "\t", space, "flow", freeText, "\n", space)},
		{Name: "wide spaces are whitespace", Spans: spans(
			"\u2003", space, "a", freeText, "\u3000", space, "b", freeText, "\u2028", space, "c", freeText)},
		{Name: "a next-line character stays inside a value", Spans: scalar("type", "bug\u0085status:todo", invalid)},
	}
}

// completionAreas is a vocabulary declaring `web`, `web/dashboard`,
// `webhooks` and `docs`.
func completionAreas() *area.Vocabulary {
	return &area.Vocabulary{Nodes: []area.Node{
		{Name: "web", Children: []area.Node{{Name: "dashboard"}}},
		{Name: "webhooks"},
		{Name: "docs"},
	}}
}

// offers pairs each item with the result template, Caret included, that has
// `%` where the item goes.
func offers(template string, items ...string) []Offer {
	out := make([]Offer, 0, len(items))
	for _, item := range items {
		result := ""
		for _, r := range template {
			if r == '%' {
				result += item
			} else {
				result += string(r)
			}
		}
		out = append(out, Offer{item, result})
	}
	return out
}

// allFields is every field name in the order the web's completion lists them.
var allFields = []string{
	"type", "priority", "status", "estimate", "tags",
	"parent", "ancestor", "descendant", "sibling", "blocking", "blocked-by",
	"mentions", "mentioned-by", "milestone", "milestone-exact",
	"has", "no", "is", "area", "area-exact",
}

var metadataFields = []string{"type", "priority", "status", "estimate", "tags"}

const (
	fieldKind = filterlang.CompletionField
	valueKind = filterlang.CompletionValue
	tagKind   = filterlang.CompletionTag
)

// CompletionCases returns the shared completion cases.
func CompletionCases() []CompletionCase {
	tags := []string{"frontend", "backend"}
	milestone := []string{"milestone"}
	return []CompletionCase{
		// Field names.
		{Name: "a partial field name", Text: "ty|", Kind: fieldKind, Offers: offers("%:|", "type")},
		{Name: "field names match by prefix", Text: "t|", Kind: fieldKind, Offers: offers("%:|", "type", "tags")},
		{Name: "a field name in another letter case", Text: "Ty|", Kind: fieldKind, Offers: offers("%:|", "type")},
		{Name: "a negated field name keeps its minus", Text: "-ty|", Kind: fieldKind, Offers: offers("-%:|", "type")},
		{Name: "no field name matches a search word", Text: "zzz|"},
		{Name: "an empty text offers nothing", Text: "|"},
		{Name: "an empty token after a space offers nothing", Text: "type:bug |"},
		{Name: "a field name after another token", Text: "type:bug st|", Kind: fieldKind, Offers: offers("type:bug %:|", "status")},
		{Name: "relationship names match by prefix", Text: "blo|", Kind: fieldKind, Offers: offers("%:|", "blocking", "blocked-by")},
		{Name: "a hyphenated relationship name", Text: "ment|", Kind: fieldKind, Offers: offers("%:|", "mentions", "mentioned-by")},
		{Name: "the has word", Text: "ha|", Kind: fieldKind, Offers: offers("%:|", "has")},
		{Name: "the no word", Text: "n|", Kind: fieldKind, Offers: offers("%:|", "no")},
		{Name: "the is word", Text: "is|", Kind: fieldKind, Offers: offers("%:|", "is")},
		{Name: "a bare minus offers the metadata names", Text: "-|", Kind: fieldKind, Offers: offers("-%:|", metadataFields...)},
		{Name: "a negated relationship name offers nothing", Text: "-blo|"},
		{Name: "a negated existence word offers nothing", Text: "-ha|"},
		{Name: "area names", Text: "ar|", Areas: completionAreas(), Kind: fieldKind, Offers: offers("%:|", "area", "area-exact")},
		{Name: "area names without a vocabulary", Text: "ar|", Kind: fieldKind, Offers: offers("%:|", "area", "area-exact")},
		{Name: "the hyphen narrows to the exact area name", Text: "area-|", Kind: fieldKind, Offers: offers("%:|", "area-exact")},
		{Name: "a negated area name offers nothing", Text: "-ar|", Areas: completionAreas()},
		{Name: "a field name before whitespace", Text: "ty| tags:wip", Kind: fieldKind, Offers: offers("%:| tags:wip", "type")},
		{Name: "a field name against the rest of its token is set apart", Text: "ty|pe", Kind: fieldKind, Offers: offers("%:| pe", "type")},
		{Name: "a cursor before a relationship colon completes the name", Text: "par|ent:x", Kind: fieldKind, Offers: offers("%:| ent:x", "parent")},
		{Name: "a cursor at a relationship colon completes the name", Text: "parent|:x", Kind: fieldKind, Offers: offers("%:| :x", "parent")},
		{Name: "a field name after an astral character", Text: "\U0001F600 ty|", Kind: fieldKind, Offers: offers("\U0001F600 %:|", "type")},
		{Name: "a field name after a no-break space", Text: "type:bug\u00a0st|", Kind: fieldKind, Offers: offers("type:bug\u00a0%:|", "status")},

		// An explicit request.
		{Name: "an explicit request offers every field name", Text: "|", Explicit: true, Kind: fieldKind, Offers: offers("%:|", allFields...)},
		{Name: "an explicit request after a space", Text: "type:bug |", Explicit: true, Kind: fieldKind, Offers: offers("type:bug %:|", allFields...)},
		{Name: "an explicit request against a token is set apart", Text: "|status:todo tags:wip", Explicit: true, Kind: fieldKind,
			Offers: offers("%:| status:todo tags:wip", allFields...)},
		{Name: "an explicit request after a minus", Text: "-|", Explicit: true, Kind: fieldKind, Offers: offers("-%:|", metadataFields...)},
		{Name: "an explicit request mid-token", Text: "ty|", Explicit: true, Kind: fieldKind, Offers: offers("%:|", "type")},
		{Name: "an explicit request in a value", Text: "status:in|", Explicit: true, Kind: valueKind, Offers: offers("status:%|", "in-progress")},

		// Metadata values.
		{Name: "every type after the colon", Text: "type:|", Kind: valueKind,
			Offers: offers("type:%|", "milestone", "epic", "bug", "feature", "task", "research")},
		{Name: "values match by substring", Text: "status:in|", Kind: valueKind, Offers: offers("status:%|", "in-progress")},
		{Name: "a partial value", Text: "type:bu|", Kind: valueKind, Offers: offers("type:%|", "bug")},
		{Name: "a complete value offers itself", Text: "type:bug|", Kind: valueKind, Offers: offers("type:%|", "bug")},
		{Name: "a value after a comma", Text: "status:todo,in|", Kind: valueKind, Offers: offers("status:todo,%|", "in-progress")},
		{Name: "a listed value is not offered again", Text: "type:bug,|", Kind: valueKind,
			Offers: offers("type:bug,%|", "milestone", "epic", "feature", "task", "research")},
		{Name: "a listed value in capitals is not offered again", Text: "type:BUG,|", Kind: valueKind,
			Offers: offers("type:BUG,%|", "milestone", "epic", "feature", "task", "research")},
		{Name: "the status groups come first", Text: "status:|", Kind: valueKind,
			Offers: offers("status:%|", "open", "closed", "draft", "todo", "in-progress", "deferred", "completed", "scrapped")},
		{Name: "a status group by substring", Text: "status:ope|", Kind: valueKind, Offers: offers("status:%|", "open")},
		{Name: "a listed group is not offered again", Text: "status:open,|", Kind: valueKind,
			Offers: offers("status:open,%|", "closed", "draft", "todo", "in-progress", "deferred", "completed", "scrapped")},
		{Name: "a negated field's values", Text: "-status:clos|", Kind: valueKind, Offers: offers("-status:%|", "closed")},
		{Name: "priorities", Text: "priority:|", Kind: valueKind, Offers: offers("priority:%|", "critical", "high", "normal", "low")},
		{Name: "estimates by substring", Text: "estimate:l|", Kind: valueKind, Offers: offers("estimate:%|", "l", "xl")},
		{Name: "a partial value in capitals", Text: "status:IN|", Kind: valueKind, Offers: offers("status:%|", "in-progress")},
		{Name: "a value before whitespace", Text: "type:bu| tags:wip", Kind: valueKind, Offers: offers("type:%| tags:wip", "bug")},
		{Name: "a value against the rest of its token", Text: "type:b|ug", Kind: valueKind, Offers: offers("type:%|ug", "bug")},
		{Name: "a value with a colon matches nothing", Text: "type:x:|"},
		{Name: "an unknown field offers nothing", Text: "title:fo|"},

		// Tags.
		{Name: "tags by substring", Text: "tags:fr|", Tags: tags, Kind: tagKind, Offers: offers("tags:%|", "frontend")},
		{Name: "no tag matches", Text: "tags:zzz|", Tags: tags},
		{Name: "every tag after the colon", Text: "tags:|", Tags: tags, Kind: tagKind, Offers: offers("tags:%|", "frontend", "backend")},
		{Name: "a partial tag in capitals", Text: "tags:FR|", Tags: tags, Kind: tagKind, Offers: offers("tags:%|", "frontend")},
		{Name: "a listed tag is not offered again", Text: "tags:frontend,|", Tags: tags, Kind: tagKind, Offers: offers("tags:frontend,%|", "backend")},
		{Name: "a negated tags token offers tags", Text: "-tags:|", Tags: tags, Kind: tagKind, Offers: offers("-tags:%|", "frontend", "backend")},
		{Name: "no tags offer nothing", Text: "tags:|"},

		// Existence values.
		{Name: "the has values", Text: "has:|", Kind: valueKind, Offers: offers("has:%|", "parent", "blocking", "blocked-by")},
		{Name: "the no values", Text: "no:|", Kind: valueKind, Offers: offers("no:%|", "parent", "blocking", "blocked-by")},
		{Name: "the is values", Text: "is:|", Kind: valueKind, Offers: offers("is:%|", "blocked", "backlog")},
		{Name: "an is value by substring", Text: "is:back|", Kind: valueKind, Offers: offers("is:%|", "backlog")},
		{Name: "has values by substring", Text: "has:block|", Kind: valueKind, Offers: offers("has:%|", "blocking", "blocked-by")},
		{Name: "a no value by a later substring", Text: "no:by|", Kind: valueKind, Offers: offers("no:%|", "blocked-by")},
		{Name: "an existence value in another letter case", Text: "Is:Blo|", Kind: valueKind, Offers: offers("Is:%|", "blocked")},
		{Name: "a Kelvin sign lowercases to k", Text: "is:bac\u212a|", Kind: valueKind, Offers: offers("is:%|", "backlog")},
		{Name: "a complete existence token offers its value", Text: "has:parent|", Kind: valueKind, Offers: offers("has:%|", "parent")},
		{Name: "no existence value matches", Text: "is:zzz|"},
		{Name: "an existence value with no predicate", Text: "has:mentions|"},
		{Name: "a negated existence token offers nothing", Text: "-has:|"},

		// Area paths.
		{Name: "every declared path", Text: "area:|", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("area:%|", "docs", "web", "web/dashboard", "webhooks")},
		{Name: "paths by substring", Text: "area:web|", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("area:%|", "web", "web/dashboard", "webhooks")},
		{Name: "a path by a later segment", Text: "area:dash|", Areas: completionAreas(), Kind: valueKind, Offers: offers("area:%|", "web/dashboard")},
		{Name: "paths in any letter case", Text: "area:WEB|", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("area:%|", "web", "web/dashboard", "webhooks")},
		{Name: "exact paths", Text: "area-exact:|", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("area-exact:%|", "docs", "web", "web/dashboard", "webhooks")},
		{Name: "an exact path in capitals", Text: "area-exact:DASH|", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("area-exact:%|", "web/dashboard")},
		{Name: "an area name in capitals", Text: "AREA:we|", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("AREA:%|", "web", "web/dashboard", "webhooks")},
		{Name: "a path between tokens", Text: "type:bug area:da| tags:wip", Areas: completionAreas(), Kind: valueKind,
			Offers: offers("type:bug area:%| tags:wip", "web/dashboard")},
		{Name: "no path matches", Text: "area:zzz|", Areas: completionAreas()},
		{Name: "no vocabulary offers no path", Text: "area:|"},
		{Name: "no vocabulary offers no exact path", Text: "area-exact:|"},
		{Name: "an empty vocabulary offers no path", Text: "area:|", Areas: &area.Vocabulary{}},
		{Name: "a negated area token offers nothing", Text: "-area:|", Areas: completionAreas()},

		// Relationship-id values.
		{Name: "a blocking id", Text: "blocking:tni|", Rel: &RelWant{Field: "blockingId", Name: "blocking", Range: "blocking:[tni]"}},
		{Name: "an empty id", Text: "parent:|", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "parent:[]"}},
		{Name: "an empty id on request", Text: "parent:|", Explicit: true, Rel: &RelWant{Field: "parentId", Name: "parent", Range: "parent:[]"}},
		{Name: "a hyphenated relationship's id", Text: "mentioned-by:x|", Rel: &RelWant{Field: "mentionedById", Name: "mentioned-by", Range: "mentioned-by:[x]"}},
		{Name: "a blocked-by id", Text: "blocked-by:foo|", Rel: &RelWant{Field: "blockedById", Name: "blocked-by", Range: "blocked-by:[foo]"}},
		{Name: "a mentions id", Text: "mentions:foo|", Rel: &RelWant{Field: "mentionsId", Name: "mentions", Range: "mentions:[foo]"}},
		{Name: "an ancestor id", Text: "ancestor:foo|", Rel: &RelWant{Field: "ancestorId", Name: "ancestor", Range: "ancestor:[foo]"}},
		{Name: "a descendant id", Text: "descendant:foo|", Rel: &RelWant{Field: "descendantId", Name: "descendant", Range: "descendant:[foo]"}},
		{Name: "a sibling id", Text: "sibling:tni|", Rel: &RelWant{Field: "siblingId", Name: "sibling", Range: "sibling:[tni]"}},
		{Name: "a milestone id names milestones", Text: "milestone:tni|",
			Rel: &RelWant{Field: "inMilestone", Name: "milestone", Range: "milestone:[tni]", SearchTypes: milestone}},
		{Name: "an exact milestone id names milestones", Text: "milestone-exact:tni|",
			Rel: &RelWant{Field: "milestone", Name: "milestone-exact", Range: "milestone-exact:[tni]", SearchTypes: milestone}},
		{Name: "the whole value from mid-value", Text: "parent:ab|cd", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "parent:[abcd]"}},
		{Name: "an id among several tokens", Text: "type:bug parent:tn|", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "type:bug parent:[tn]"}},
		{Name: "an id before another token", Text: "parent:tn| type:bug", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "parent:[tn] type:bug"}},
		{Name: "an id keeps its letter case", Text: "PARENT:Ab|", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "PARENT:[Ab]"}},
		{Name: "an id keeps its comma and colon", Text: "parent:a,b:c|", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "parent:[a,b:c]"}},
		{Name: "an id after an astral character", Text: "\U0001F600 parent:t|", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "\U0001F600 parent:[t]"}},
		{Name: "an id holding an astral character", Text: "parent:a\U0001F600|b", Rel: &RelWant{Field: "parentId", Name: "parent", Range: "parent:[a\U0001F600b]"}},
		{Name: "a negated relationship token has no id value", Text: "-parent:x|"},
		{Name: "an existence token has no id value", Text: "type:bug no:parent|", Kind: valueKind, Offers: offers("type:bug no:%|", "parent")},
		{Name: "a word after an id token", Text: "parent:x y|"},
	}
}
