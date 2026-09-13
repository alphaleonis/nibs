// Package projection turns a view tier (id/ref/card/full) plus an additive `-f`
// field selection into an ordered, JSON-serializable projection of a nib.
// Computed fields and relations reach the store through Resolver.
package projection

import (
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Field is a token in the closed projection menu. Relation tokens are kebab-case
// (blocked-by) and serialize with underscores (blocked_by); see fieldDef.jsonKey.
type Field string

// The field menu; registry holds the canonical order.
const (
	// Scalars — read directly off the nib.
	FieldID       Field = "id"
	FieldSlug     Field = "slug"
	FieldTitle    Field = "title"
	FieldStatus   Field = "status"
	FieldType     Field = "type"
	FieldPriority Field = "priority"
	FieldEstimate Field = "estimate"
	FieldTags     Field = "tags"
	// The parent link as stored, even when it names no nib; FieldParent resolves it.
	FieldStoredParent Field = "stored_parent"
	FieldOrder        Field = "order"
	// The assignment axis, as stored.
	FieldMilestone      Field = "milestone"
	FieldMilestoneOrder Field = "milestone_order"
	FieldArea           Field = "area"
	FieldCreatedAt      Field = "created_at"
	FieldUpdatedAt      Field = "updated_at"
	FieldPath           Field = "path"
	FieldBody           Field = "body"
	FieldETag           Field = "etag"

	// Computed scalars need the Resolver and cannot be nested. FieldParent is the
	// resolved parent, so it is computed.
	FieldParent   Field = "parent"
	FieldChildren Field = "children"
	FieldProgress Field = "progress"
	FieldReady    Field = "ready"

	// Relation id-lists — a bare token projects an id list; a parenthesized
	// sub-selection projects one level of nested objects.
	FieldBlocking    Field = "blocking"
	FieldBlockedBy   Field = "blocked-by"
	FieldMentions    Field = "mentions"
	FieldMentionedBy Field = "mentioned-by"
)

// fieldKind classifies how a field is resolved during projection.
type fieldKind int

const (
	// kindScalar is read straight off the nib (no Resolver required).
	kindScalar fieldKind = iota
	// kindComputed is a scalar resolved via the Resolver; it is not nestable.
	kindComputed
	// kindRelation is an id-list of related nibs, optionally projected one level
	// deep via a parenthesized sub-selection.
	kindRelation
)

// String returns the kind name FieldCatalog reports.
func (k fieldKind) String() string {
	switch k {
	case kindScalar:
		return "scalar"
	case kindComputed:
		return "computed"
	case kindRelation:
		return "relation"
	default:
		return "unknown"
	}
}

// fieldDef is a single menu entry: its kind, its JSON output key, and (for
// scalars) how to pull its value off the nib.
type fieldDef struct {
	name    Field
	kind    fieldKind
	jsonKey string               // JSON output key; defaults to string(name) when empty
	extract func(n *nib.Nib) any // scalar value accessor; nil for computed/relation
}

// key returns the JSON output key for the field, defaulting to the mask token.
func (d fieldDef) key() string {
	if d.jsonKey != "" {
		return d.jsonKey
	}
	return string(d.name)
}

// registry is the field menu in canonical order: the `-f` vocabulary, the JSON
// keys, and the output order for JSON and text. A new field also needs its Field
// constant and, when computed or a relation, a case in project.go.
var registry = []fieldDef{
	{name: FieldID, kind: kindScalar, extract: func(n *nib.Nib) any { return n.ID }},
	{name: FieldSlug, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Slug }},
	{name: FieldTitle, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Title }},
	{name: FieldStatus, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Status }},
	{name: FieldType, kind: kindScalar, extract: func(n *nib.Nib) any { return n.EffectiveType() }},
	{name: FieldPriority, kind: kindScalar, extract: func(n *nib.Nib) any { return n.EffectivePriority() }},
	{name: FieldEstimate, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Estimate }},
	{name: FieldTags, kind: kindScalar, extract: func(n *nib.Nib) any { return normStrings(n.Tags) }},
	// Menu order, not kind order: the computed parent sits beside stored_parent.
	{name: FieldParent, kind: kindComputed},
	{name: FieldStoredParent, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Parent }},
	{name: FieldOrder, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Order }},
	{name: FieldMilestone, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Milestone }},
	{name: FieldMilestoneOrder, kind: kindScalar, extract: func(n *nib.Nib) any { return n.MilestoneOrder }},
	{name: FieldArea, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Area }},
	{name: FieldCreatedAt, kind: kindScalar, extract: func(n *nib.Nib) any { return n.CreatedAt }},
	{name: FieldUpdatedAt, kind: kindScalar, extract: func(n *nib.Nib) any { return n.UpdatedAt }},
	{name: FieldPath, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Path }},
	{name: FieldBody, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Body }},
	{name: FieldETag, kind: kindScalar, extract: func(n *nib.Nib) any { return n.ETag() }},

	{name: FieldChildren, kind: kindComputed},
	{name: FieldProgress, kind: kindComputed},
	{name: FieldReady, kind: kindComputed},

	{name: FieldBlocking, kind: kindRelation},
	{name: FieldBlockedBy, kind: kindRelation, jsonKey: "blocked_by"},
	{name: FieldMentions, kind: kindRelation},
	{name: FieldMentionedBy, kind: kindRelation, jsonKey: "mentioned_by"},
}

// defByName resolves a mask token to its definition, derived from registry.
var defByName = func() map[Field]fieldDef {
	m := make(map[Field]fieldDef, len(registry))
	for _, d := range registry {
		m[d.name] = d
	}
	return m
}()

// FieldMenu returns the closed field menu in canonical order.
func FieldMenu() []Field {
	out := make([]Field, len(registry))
	for i, d := range registry {
		out[i] = d.name
	}
	return out
}

// FieldMenuString returns the field menu as a comma-separated list for error
// messages.
func FieldMenuString() string {
	return joinFields(FieldMenu())
}

// FieldInfo is one field's catalog entry: token, kind and JSON key.
type FieldInfo struct {
	Name    Field
	Kind    string
	JSONKey string
}

// FieldCatalog returns every field in menu order, derived from registry.
func FieldCatalog() []FieldInfo {
	out := make([]FieldInfo, len(registry))
	for i, d := range registry {
		out[i] = FieldInfo{Name: d.name, Kind: d.kind.String(), JSONKey: d.key()}
	}
	return out
}

// relationNames lists the nestable relation fields (for error messages).
func relationNames() string {
	return joinFields(fieldsOfKind(kindRelation))
}

// subFieldNames lists the fields valid inside a relation sub-selection — every
// scalar and computed field (relations may not be nested). For error messages.
func subFieldNames() string {
	sub := make([]Field, 0, len(registry))
	for _, d := range registry {
		if d.kind == kindScalar || d.kind == kindComputed {
			sub = append(sub, d.name)
		}
	}
	return joinFields(sub)
}

// fieldsOfKind returns the menu fields of a given kind, in menu order.
func fieldsOfKind(k fieldKind) []Field {
	out := make([]Field, 0, len(registry))
	for _, d := range registry {
		if d.kind == k {
			out = append(out, d.name)
		}
	}
	return out
}

// joinFields renders a field slice as a comma-separated string of mask tokens.
func joinFields(fs []Field) string {
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = string(f)
	}
	return strings.Join(names, ", ")
}

// normStrings maps a nil slice to a non-nil empty slice so an explicitly
// selected list field serializes to a JSON [] rather than null.
func normStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// timeText renders a timestamp as RFC3339, or "" for nil.
func timeText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
