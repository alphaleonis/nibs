// Package filterlang defines the token tables of the filter language that the
// web's filter box reads, and renders them as a TypeScript module the web's
// parser imports. `task codegen` writes the module through go:generate, and
// TestGeneratedFilterTokensAreFresh fails until the committed file matches.
//
// The tables name the vocabulary a field's values come from instead of listing
// the values. The web reads those values from its generated vocabulary
// (internal/webvocab).
package filterlang

//go:generate go run ./gen

import "slices"

// ValueSet names the vocabulary a metadata field's values come from.
type ValueSet string

// The value sets. Each enumerated one names a vocabulary in internal/config.
const (
	TypeValues     ValueSet = "types"
	PriorityValues ValueSet = "priorities"
	StatusValues   ValueSet = "statuses"
	EstimateValues ValueSet = "estimates"
	// TagValues is not enumerated: a value is any tag that matches nib.TagPattern.
	TagValues ValueSet = "tags"
)

// valueSets lists every ValueSet in the order of the generated union type.
var valueSets = []ValueSet{TypeValues, PriorityValues, StatusValues, EstimateValues, TagValues}

// Field is a metadata field token, `name:v1,v2`, or `-name:v1,v2` to exclude.
type Field struct {
	// Name is the token's field name in lower case.
	Name string
	// FilterKey is the GraphQL name of the NibFilter list a value goes to.
	FilterKey string
	// ExcludeKey is the GraphQL name of the NibFilter list a negated token's value goes to.
	ExcludeKey string
	// Values is the vocabulary the field's values come from.
	Values ValueSet
}

// fields is the canonical field order, which is also the serialization order.
var fields = []Field{
	{Name: "type", FilterKey: "type", ExcludeKey: "excludeType", Values: TypeValues},
	{Name: "priority", FilterKey: "priority", ExcludeKey: "excludePriority", Values: PriorityValues},
	{Name: "status", FilterKey: "status", ExcludeKey: "excludeStatus", Values: StatusValues},
	{Name: "estimate", FilterKey: "estimate", ExcludeKey: "excludeEstimate", Values: EstimateValues},
	{Name: "tags", FilterKey: "tags", ExcludeKey: "excludeTags", Values: TagValues},
}

// Fields returns the metadata field tokens in canonical order.
func Fields() []Field { return slices.Clone(fields) }

// RelKind tells a relationship-id token from an existence token.
type RelKind string

const (
	// RelID is a token that names a nib, such as `parent:<id>`.
	RelID RelKind = "id"
	// RelBool is one spelling of an existence token, such as `has:parent`.
	RelBool RelKind = "bool"
)

// RelToken is a relationship-id token or one spelling of an existence token.
type RelToken struct {
	Kind RelKind
	// Field is the GraphQL name of the scalar NibFilter field the token writes.
	Field string
	// Name is the field name of a RelID token, hyphenated ones included
	// (`blocked-by`). It names the relationship the MATCHED nib holds toward the
	// id, as the NibFilter fields do: `ancestor:X` keeps X's descendants.
	Name string
	// Token is the whole RelBool token, `word:value` with one colon.
	Token string
	// Value is what a RelBool token writes to Field.
	Value bool
	// Description is one line for the filter box's syntax help, from the
	// matched nib's side.
	Description string
	// SearchTypes holds the nib types a RelID token's value may name, or nil
	// when any nib will do. The web's id typeahead sends it with its search.
	SearchTypes []string
}

// relTokens is the canonical serialization order: grouped by relationship, each
// relationship's id token before its existence tokens. Moving an entry changes
// which strings are canonical.
//
// An existence spelling exists only where the server has a matching predicate.
var relTokens = []RelToken{
	{Kind: RelID, Field: "parentId", Name: "parent", Description: "Direct children of this nib"},
	{Kind: RelBool, Field: "hasParent", Token: "has:parent", Value: true, Description: "Nibs that have a parent"},
	{Kind: RelBool, Field: "hasParent", Token: "no:parent", Value: false, Description: "Root nibs, with no parent"},
	{Kind: RelID, Field: "ancestorId", Name: "ancestor", Description: "Everything under this nib, at any depth"},
	{Kind: RelID, Field: "descendantId", Name: "descendant", Description: "This nib's ancestor chain, up to the root"},
	{Kind: RelID, Field: "siblingId", Name: "sibling", Description: "Nibs sharing this nib's parent"},
	{Kind: RelID, Field: "blockingId", Name: "blocking", Description: "Nibs that block this one"},
	{Kind: RelBool, Field: "hasBlocking", Token: "has:blocking", Value: true, Description: "Nibs that block something"},
	{Kind: RelBool, Field: "hasBlocking", Token: "no:blocking", Value: false, Description: "Nibs that block nothing"},
	{Kind: RelID, Field: "blockedById", Name: "blocked-by", Description: "Nibs this one blocks"},
	{Kind: RelBool, Field: "hasBlockedBy", Token: "has:blocked-by", Value: true, Description: "Nibs listing a blocker"},
	{Kind: RelBool, Field: "hasBlockedBy", Token: "no:blocked-by", Value: false, Description: "Nibs listing no blocker"},
	{Kind: RelBool, Field: "isBlocked", Token: "is:blocked", Value: true, Description: "Nibs held up by an unmet blocker"},
	{Kind: RelID, Field: "mentionsId", Name: "mentions", Description: "Nibs whose body mentions this nib"},
	{Kind: RelID, Field: "mentionedById", Name: "mentioned-by", Description: "Nibs mentioned in this nib's body"},
	// The assignment axis, broad reading first: `milestone:<id>` matches derived
	// membership (the milestone's whole plan) and `milestone-exact:<id>` the
	// direct assignment alone. `is:backlog` is the complement of the derived
	// reading, so a child of an assigned epic is not backlog.
	//
	// Backlog has one `is:` spelling and no `has:`/`no:` pair, because the web
	// reads a paired entry's value as its polarity, and `no:` on a field named
	// noMilestone would write true.
	//
	// The server refuses a target that is not a milestone on either id field, so
	// both typeaheads offer milestones alone.
	{Kind: RelID, Field: "inMilestone", Name: "milestone", Description: "Nibs in this milestone's plan, assigned to it or riding along under something that is", SearchTypes: []string{"milestone"}},
	{Kind: RelID, Field: "milestone", Name: "milestone-exact", Description: "Nibs assigned to this milestone itself — its queue", SearchTypes: []string{"milestone"}},
	{Kind: RelBool, Field: "noMilestone", Token: "is:backlog", Value: true, Description: "Nibs in no milestone's plan, their own or an inherited one"},
}

// RelTokens returns the relationship-id and existence tokens in canonical order.
func RelTokens() []RelToken {
	out := slices.Clone(relTokens)
	for i := range out {
		out[i].SearchTypes = slices.Clone(out[i].SearchTypes)
	}
	return out
}

// AreaToken is an ownership token, `name:<path>`.
type AreaToken struct {
	// Name is the token's field name, hyphenated ones included (`area-exact`).
	Name string
	// Field is the GraphQL name of the scalar NibFilter field the token writes.
	Field string
	// Description is one line for the filter box's syntax help.
	Description string
}

// areaTokens is the canonical serialization order, the broad reading first.
var areaTokens = []AreaToken{
	{Name: "area", Field: "area", Description: "Nibs in this area, and in every area beneath it"},
	{Name: "area-exact", Field: "areaExact", Description: "Nibs in exactly this area, and in no area beneath it"},
}

// AreaTokens returns the ownership tokens in canonical order.
func AreaTokens() []AreaToken { return slices.Clone(areaTokens) }

// HelpRow is one row of the filter box's syntax help.
type HelpRow struct {
	// Token is the query text the row shows.
	Token string
	// Description is one line of prose about Token.
	Description string
}

var helpOperators = []HelpRow{
	{Token: "a b", Description: "Space is AND — every condition must hold"},
	{Token: "-type:bug", Description: "A leading minus excludes (metadata fields only)"},
	{Token: "status:todo,in-progress", Description: "A comma is OR within one field"},
	{Token: "login flow", Description: "Bare words search id, title and body"},
}

// HelpOperators returns the syntax help's operator rows in display order.
func HelpOperators() []HelpRow { return slices.Clone(helpOperators) }

// helpExamples must each parse with no invalid token.
var helpExamples = []HelpRow{
	{Token: "type:bug status:open", Description: "Open bugs"},
	{Token: "-status:closed -tags:wip", Description: "Everything unfinished that is not parked as work-in-progress"},
	{Token: "ancestor:nibs-a1b2 type:task", Description: "Tasks anywhere under one epic"},
	{Token: "is:blocked priority:high", Description: "High-priority work that is stuck"},
	{Token: "no:parent type:milestone", Description: "Top-level milestones"},
	{Token: "is:backlog type:epic", Description: "Epics no milestone plan covers"},
	{Token: "area:web status:open", Description: "Open work in web or any area beneath it"},
}

// HelpExamples returns the syntax help's example queries in display order.
func HelpExamples() []HelpRow { return slices.Clone(helpExamples) }
