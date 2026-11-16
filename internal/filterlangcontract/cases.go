package filterlangcontract

import (
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

// Case is one shared parsing case: filter-box text, the vocabulary it is parsed
// against, and what the parser makes of it.
type Case struct {
	// Name identifies the case in both suites' output.
	Name string
	Text string
	// Areas is the declared area vocabulary, or nil to parse with none, which
	// keeps every area path.
	Areas *area.Vocabulary
	Want  model.NibFilter
	// Invalid is the web's invalidTokens: the distinct rejected tokens in text order.
	Invalid []string
	// Canonical is the text the parse result serializes to.
	Canonical string
}

func list(values ...string) []string { return values }
func str(s string) *string           { return &s }
func flag(b bool) *bool              { return &b }

var (
	openStatuses   = list("draft", "todo", "in-progress")
	closedStatuses = list("deferred", "completed", "scrapped")
)

// declaredAreas is a vocabulary declaring `web`, `web/dashboard` and `webhooks`.
func declaredAreas() *area.Vocabulary {
	return &area.Vocabulary{Nodes: []area.Node{
		{Name: "web", Children: []area.Node{{Name: "dashboard"}}},
		{Name: "webhooks"},
	}}
}

// Cases returns the shared parsing cases. They include every row of
// filterlang.HelpExamples and filterlang.HelpOperators.
func Cases() []Case {
	return []Case{
		// The syntax help's examples.
		{Name: "help: open bugs", Text: "type:bug status:open",
			Want: model.NibFilter{Type: list("bug"), Status: openStatuses}, Canonical: "type:bug status:open"},
		{Name: "help: unfinished and not parked", Text: "-status:closed -tags:wip",
			Want: model.NibFilter{ExcludeStatus: closedStatuses, ExcludeTags: list("wip")}, Canonical: "-status:closed -tags:wip"},
		{Name: "help: tasks under an epic", Text: "ancestor:nibs-a1b2 type:task",
			Want: model.NibFilter{AncestorID: str("nibs-a1b2"), Type: list("task")}, Canonical: "type:task ancestor:nibs-a1b2"},
		{Name: "help: stuck high-priority work", Text: "is:blocked priority:high",
			Want: model.NibFilter{IsBlocked: flag(true), Priority: list("high")}, Canonical: "priority:high is:blocked"},
		{Name: "help: top-level milestones", Text: "no:parent type:milestone",
			Want: model.NibFilter{HasParent: flag(false), Type: list("milestone")}, Canonical: "type:milestone no:parent"},
		{Name: "help: epics in the backlog", Text: "is:backlog type:epic",
			Want: model.NibFilter{NoMilestone: flag(true), Type: list("epic")}, Canonical: "type:epic is:backlog"},
		{Name: "help: open work in an area", Text: "area:web status:open",
			Want: model.NibFilter{Area: str("web"), Status: openStatuses}, Canonical: "status:open area:web"},

		// The syntax help's operator rows.
		{Name: "operator: space is AND", Text: "a b", Want: model.NibFilter{Search: str("a b")}, Canonical: "a b"},
		{Name: "operator: minus excludes", Text: "-type:bug", Want: model.NibFilter{ExcludeType: list("bug")}, Canonical: "-type:bug"},
		{Name: "operator: comma is OR", Text: "status:todo,in-progress",
			Want: model.NibFilter{Status: list("todo", "in-progress")}, Canonical: "status:todo,in-progress"},
		{Name: "operator: bare words search", Text: "login flow", Want: model.NibFilter{Search: str("login flow")}, Canonical: "login flow"},

		// Metadata fields.
		{Name: "field name and value are lowercased", Text: "TYPE:BUG", Want: model.NibFilter{Type: list("bug")}, Canonical: "type:bug"},
		{Name: "a negated field in mixed case", Text: "-STATUS:Completed", Want: model.NibFilter{ExcludeStatus: list("completed")}, Canonical: "-status:completed"},
		{Name: "comma values are lowercased each", Text: "estimate:m,XL", Want: model.NibFilter{Estimate: list("m", "xl")}, Canonical: "estimate:m,xl"},
		{Name: "repeated tokens union, first occurrence kept", Text: "type:bug,feature type:task type:bug",
			Want: model.NibFilter{Type: list("bug", "feature", "task")}, Canonical: "type:bug,feature,task"},
		{Name: "include and exclude of one field", Text: "type:bug -type:task",
			Want: model.NibFilter{Type: list("bug"), ExcludeType: list("task")}, Canonical: "type:bug -type:task"},
		{Name: "enum values serialize in vocabulary order", Text: "priority:low,critical",
			Want: model.NibFilter{Priority: list("low", "critical")}, Canonical: "priority:critical,low"},
		{Name: "all five fields serialize in field order", Text: "tags:auth estimate:m status:todo priority:high type:bug",
			Want:      model.NibFilter{Tags: list("auth"), Estimate: list("m"), Status: list("todo"), Priority: list("high"), Type: list("bug")},
			Canonical: "type:bug priority:high status:todo estimate:m tags:auth"},
		{Name: "a type token never collapses", Text: "type:milestone,epic,bug,feature,task,research",
			Want:      model.NibFilter{Type: list("milestone", "epic", "bug", "feature", "task", "research")},
			Canonical: "type:milestone,epic,bug,feature,task,research"},

		// Status groups.
		{Name: "a group name in mixed case", Text: "STATUS:Open", Want: model.NibFilter{Status: openStatuses}, Canonical: "status:open"},
		{Name: "a group unions with values, deduped", Text: "status:open,todo,completed",
			Want: model.NibFilter{Status: list("draft", "todo", "in-progress", "completed")}, Canonical: "status:open,completed"},
		{Name: "both groups", Text: "status:open,closed",
			Want:      model.NibFilter{Status: list("draft", "todo", "in-progress", "deferred", "completed", "scrapped")},
			Canonical: "status:open,closed"},
		{Name: "a spelled-out group collapses in any order", Text: "status:in-progress,draft,todo",
			Want: model.NibFilter{Status: list("in-progress", "draft", "todo")}, Canonical: "status:open"},
		{Name: "a partial group stays spelled out", Text: "status:todo,draft",
			Want: model.NibFilter{Status: list("todo", "draft")}, Canonical: "status:draft,todo"},
		{Name: "a group sorts by its lowest member", Text: "status:deferred,open",
			Want: model.NibFilter{Status: list("deferred", "draft", "todo", "in-progress")}, Canonical: "status:open,deferred"},
		{Name: "a group in an exclude token", Text: "-status:deferred,open",
			Want: model.NibFilter{ExcludeStatus: list("deferred", "draft", "todo", "in-progress")}, Canonical: "-status:open,deferred"},
		{Name: "the closed group beside an open status", Text: "status:scrapped,draft,completed,deferred",
			Want: model.NibFilter{Status: list("scrapped", "draft", "completed", "deferred")}, Canonical: "status:draft,closed"},
		{Name: "a group name is only a status value", Text: "type:open", Invalid: list("type:open"), Canonical: "type:open"},
		{Name: "no group for a single status", Text: "status:parked", Invalid: list("status:parked"), Canonical: "status:parked"},
		{Name: "an object prototype key is no group", Text: "status:constructor", Invalid: list("status:constructor"), Canonical: "status:constructor"},

		// Tags.
		{Name: "tags serialize alphabetically", Text: "tags:zebra,apple tags:never-seen",
			Want: model.NibFilter{Tags: list("zebra", "apple", "never-seen")}, Canonical: "tags:apple,never-seen,zebra"},
		{Name: "tags outside the tag pattern", Text: "tags:1bad tags:has_underscore -tags:Bad-",
			Invalid: list("tags:1bad", "tags:has_underscore", "-tags:bad-"), Canonical: "tags:1bad tags:has_underscore -tags:bad-"},

		// Rejected values.
		{Name: "a rejected value beside a valid one", Text: "type:bug,banana",
			Want: model.NibFilter{Type: list("bug")}, Invalid: list("type:banana"), Canonical: "type:bug type:banana"},
		{Name: "a rejected value keeps its minus", Text: "-priority:pink -type:bug,banana",
			Want: model.NibFilter{ExcludeType: list("bug")}, Invalid: list("-priority:pink", "-type:banana"),
			Canonical: "-type:bug -priority:pink -type:banana"},
		{Name: "rejected tokens dedupe after lowercasing", Text: "STATUS:Banana status:banana",
			Invalid: list("status:banana"), Canonical: "status:banana"},
		{Name: "rejected tokens follow the search", Text: "status:banana login type:bug",
			Want: model.NibFilter{Type: list("bug"), Search: str("login")}, Invalid: list("status:banana"),
			Canonical: "type:bug login status:banana"},

		// Empty values and bare words.
		{Name: "a trailing comma part is dropped", Text: "type:bug,", Want: model.NibFilter{Type: list("bug")}, Canonical: "type:bug"},
		{Name: "a field with only empty parts is a bare word", Text: "type:, -type:, status:,,",
			Want: model.NibFilter{Search: str("type:, -type:, status:,,")}, Canonical: "type:, -type:, status:,,"},
		{Name: "a field with no value is a bare word", Text: "status: type: -status:",
			Want: model.NibFilter{Search: str("status: type: -status:")}, Canonical: "status: type: -status:"},
		{Name: "unknown fields are bare words", Text: "title:foo -title:foo body:bar --type:bug",
			Want: model.NibFilter{Search: str("title:foo -title:foo body:bar --type:bug")}, Canonical: "title:foo -title:foo body:bar --type:bug"},
		{Name: "bare words keep their case", Text: "Login FLOW type:bug",
			Want: model.NibFilter{Type: list("bug"), Search: str("Login FLOW")}, Canonical: "type:bug Login FLOW"},
		{Name: "whitespace runs collapse in the search", Text: "  login    flow  ", Want: model.NibFilter{Search: str("login flow")}, Canonical: "login flow"},
		{Name: "empty text", Text: "", Canonical: ""},
		{Name: "whitespace only", Text: "   ", Canonical: ""},

		// Relationship and existence tokens.
		{Name: "a relationship id is lowercased", Text: "PARENT:TNIB-ABC", Want: model.NibFilter{ParentID: str("tnib-abc")}, Canonical: "parent:tnib-abc"},
		{Name: "an existence token in upper case", Text: "HAS:PARENT", Want: model.NibFilter{HasParent: flag(true)}, Canonical: "has:parent"},
		{Name: "the last existence spelling wins", Text: "has:parent no:parent", Want: model.NibFilter{HasParent: flag(false)}, Canonical: "no:parent"},
		{Name: "the last relationship id wins", Text: "parent:a parent:b", Want: model.NibFilter{ParentID: str("b")}, Canonical: "parent:b"},
		{Name: "a relationship id keeps its commas", Text: "parent:a,b", Want: model.NibFilter{ParentID: str("a,b")}, Canonical: "parent:a,b"},
		{Name: "existence spellings outside the table are bare words", Text: "has:mentions is:foo has:milestone no:sibling",
			Want: model.NibFilter{Search: str("has:mentions is:foo has:milestone no:sibling")}, Canonical: "has:mentions is:foo has:milestone no:sibling"},
		{Name: "every relationship token, in canonical order",
			Text: "mentioned-by:t9 mentions:t8 is:blocked no:blocked-by blocked-by:t7 no:blocking blocking:t6 sibling:t5 descendant:t4 ancestor:t3 has:parent parent:t2 milestone-exact:t1 milestone:t0 is:backlog",
			Want: model.NibFilter{
				ParentID: str("t2"), HasParent: flag(true), AncestorID: str("t3"), DescendantID: str("t4"), SiblingID: str("t5"),
				BlockingID: str("t6"), HasBlocking: flag(false), BlockedByID: str("t7"), HasBlockedBy: flag(false), IsBlocked: flag(true),
				MentionsID: str("t8"), MentionedByID: str("t9"), InMilestone: str("t0"), Milestone: str("t1"), NoMilestone: flag(true),
			},
			Canonical: "parent:t2 has:parent ancestor:t3 descendant:t4 sibling:t5 blocking:t6 no:blocking blocked-by:t7 no:blocked-by is:blocked mentions:t8 mentioned-by:t9 milestone:t0 milestone-exact:t1 is:backlog"},
		{Name: "the other existence spellings", Text: "has:blocking has:blocked-by",
			Want: model.NibFilter{HasBlocking: flag(true), HasBlockedBy: flag(true)}, Canonical: "has:blocking has:blocked-by"},
		{Name: "negated relationship tokens are rejected whole", Text: "-ancestor:X -has:parent -is:backlog -milestone-exact:M",
			Invalid: list("-ancestor:x", "-has:parent", "-is:backlog", "-milestone-exact:m"), Canonical: "-ancestor:x -has:parent -is:backlog -milestone-exact:m"},
		{Name: "a rejected negation leaves the rest working", Text: "type:bug -ancestor:tnib-1 login",
			Want: model.NibFilter{Type: list("bug"), Search: str("login")}, Invalid: list("-ancestor:tnib-1"), Canonical: "type:bug login -ancestor:tnib-1"},
		{Name: "a negated token that is no relationship token is a bare word", Text: "-has:mentions -parent:",
			Want: model.NibFilter{Search: str("-has:mentions -parent:")}, Canonical: "-has:mentions -parent:"},
		{Name: "a relationship name with no id is a bare word", Text: "blocking: milestone: milestone-exact:",
			Want: model.NibFilter{Search: str("blocking: milestone: milestone-exact:")}, Canonical: "blocking: milestone: milestone-exact:"},

		// Area tokens without a vocabulary.
		{Name: "an area path keeps its case", Text: "AREA:Web/Dashboard", Want: model.NibFilter{Area: str("Web/Dashboard")}, Canonical: "area:Web/Dashboard"},
		{Name: "an area path is taken whole", Text: "area:a,b:c", Want: model.NibFilter{Area: str("a,b:c")}, Canonical: "area:a,b:c"},
		{Name: "a negated area token is rejected with its path as typed", Text: "-Area:Web -AREA-EXACT:x",
			Invalid: list("-area:Web", "-area-exact:x"), Canonical: "-area:Web -area-exact:x"},
		{Name: "area words that are no area token", Text: "area: areas:web area",
			Want: model.NibFilter{Search: str("area: areas:web area")}, Canonical: "area: areas:web area"},
		{Name: "the last of each area token wins", Text: "area-exact:x area:web area:webhooks area-exact:web/dashboard",
			Want: model.NibFilter{Area: str("webhooks"), AreaExact: str("web/dashboard")}, Canonical: "area:webhooks area-exact:web/dashboard"},
		{Name: "an undeclared path is kept without a vocabulary", Text: "area:retired", Want: model.NibFilter{Area: str("retired")}, Canonical: "area:retired"},

		// Area tokens against a vocabulary.
		{Name: "declared paths", Text: "area:web/dashboard area-exact:web", Areas: declaredAreas(),
			Want: model.NibFilter{Area: str("web/dashboard"), AreaExact: str("web")}, Canonical: "area:web/dashboard area-exact:web"},
		{Name: "an undeclared path is rejected", Text: "type:bug area:retired login", Areas: declaredAreas(),
			Want: model.NibFilter{Type: list("bug"), Search: str("login")}, Invalid: list("area:retired"), Canonical: "type:bug login area:retired"},
		{Name: "a path is declared only in its own case", Text: "area-exact:Web", Areas: declaredAreas(),
			Invalid: list("area-exact:Web"), Canonical: "area-exact:Web"},
		{Name: "a path is declared only without a trailing slash", Text: "area:web/", Areas: declaredAreas(),
			Invalid: list("area:web/"), Canonical: "area:web/"},
		{Name: "a declared and an undeclared path", Text: "area:web area:retired", Areas: declaredAreas(),
			Want: model.NibFilter{Area: str("web")}, Invalid: list("area:retired"), Canonical: "area:web area:retired"},
		{Name: "a negated declared path is rejected for its minus", Text: "-area:web", Areas: declaredAreas(),
			Invalid: list("-area:web"), Canonical: "-area:web"},
		{Name: "an empty vocabulary declares nothing", Text: "area:web", Areas: &area.Vocabulary{},
			Invalid: list("area:web"), Canonical: "area:web"},

		// Whitespace as JavaScript's \s reads it.
		{Name: "a no-break space separates tokens", Text: "type:bug\u00a0status:todo",
			Want: model.NibFilter{Type: list("bug"), Status: list("todo")}, Canonical: "type:bug status:todo"},
		{Name: "a byte order mark, tab and newline separate tokens", Text: "\ufefflogin\tflow\n",
			Want: model.NibFilter{Search: str("login flow")}, Canonical: "login flow"},
		{Name: "wide spaces separate tokens", Text: "\u2003a\u3000b\u2028c", Want: model.NibFilter{Search: str("a b c")}, Canonical: "a b c"},
		{Name: "a next-line character does not separate tokens", Text: "type:bug\u0085status:todo",
			Invalid: list("type:bug\u0085status:todo"), Canonical: "type:bug\u0085status:todo"},
		{Name: "a zero-width space does not separate tokens", Text: "a\u200bb", Want: model.NibFilter{Search: str("a\u200bb")}, Canonical: "a\u200bb"},

		// Lowercasing as JavaScript's toLowerCase does it.
		{Name: "a word-final capital sigma lowercases to the final form", Text: "parent:A\u03a3", Want: model.NibFilter{ParentID: str("a\u03c2")}, Canonical: "parent:a\u03c2"},
		{Name: "a word-initial capital sigma lowercases to the medial form", Text: "parent:\u03a3A", Want: model.NibFilter{ParentID: str("\u03c3a")}, Canonical: "parent:\u03c3a"},
		{Name: "a capital sigma between letters is medial", Text: "parent:A\u03a3B", Want: model.NibFilter{ParentID: str("a\u03c3b")}, Canonical: "parent:a\u03c3b"},
		{Name: "a period does not end the word before a sigma", Text: "parent:A.\u03a3", Want: model.NibFilter{ParentID: str("a.\u03c2")}, Canonical: "parent:a.\u03c2"},
		{Name: "a negated token lowercases whole, sigma included", Text: "-PARENT:A\u03a3",
			Invalid: list("-parent:a\u03c2"), Canonical: "-parent:a\u03c2"},
		{Name: "a sigma after the colon of a parked token is final", Text: "-PARENT:\u03a3",
			Invalid: list("-parent:\u03c2"), Canonical: "-parent:\u03c2"},
		{Name: "a rejected value lowercases with JavaScript's rules", Text: "tags:A\u03a3 status:\u0130",
			Invalid: list("tags:a\u03c2", "status:i\u0307"), Canonical: "tags:a\u03c2 status:i\u0307"},
		{Name: "a dotted capital I lowercases to two code points", Text: "parent:\u0130X", Want: model.NibFilter{ParentID: str("i\u0307x")}, Canonical: "parent:i\u0307x"},
		{Name: "the Kelvin sign lowercases to k", Text: "is:bloc\u212aed", Want: model.NibFilter{IsBlocked: flag(true)}, Canonical: "is:blocked"},
	}
}
