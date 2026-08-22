// Package projection is the transport-agnostic field-set projection engine over
// the nib resource. It compiles a view tier (id/ref/card/full) plus an additive
// `-f`/--fields selection into an ordered, JSON-serializable projection of a
// single nib, resolving computed fields (children/progress/ready) and one level
// of nested relation projection (blocking/blocked-by/mentions/mentioned-by)
// through a small Resolver accessor.
//
// It is deliberately free of any CLI/transport concern: the CLI, TUI, and a
// future MCP adapter all reuse this same engine. `internal/output/columns.go`
// (flat TSV column projection) is the narrow seed this generalizes; this package
// is kept separate because the field-mask engine needs a store accessor
// (Resolver) and an ordered result type that a pure string-formatting package
// like internal/output has no business depending on.
package projection

import (
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Field is a token in the closed, code-defined projection menu. The menu is the
// single source of truth for both the `-f` vocabulary and the canonical output
// ordering; an unknown token is rejected with an error naming the whole menu.
//
// The mask vocabulary is human-friendly kebab-case for the relation fields
// (blocked-by, mentioned-by); the two timestamp scalars keep the snake_case
// spelling the nib already serializes (created_at, updated_at). The JSON output
// key of each field is normalized to the nib's own serialization spelling (see
// fieldDef.jsonKey): blocked-by → "blocked_by", mentioned-by → "mentioned_by".
type Field string

// The closed field menu. Kept in menu order in the registry below.
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
	// The parent link exactly as stored on disk, unresolved — including one
	// naming no nib. The inspection counterpart to FieldParent, which reports
	// who the parent actually is; see the registry entry for FieldParent.
	FieldStoredParent Field = "stored_parent"
	FieldOrder        Field = "order"
	// The assignment axis as stored: the milestone id, the queue key beside
	// the sibling key, and the area path. milestone_order keeps the nib's own
	// serialization spelling, like the timestamps.
	FieldMilestone      Field = "milestone"
	FieldMilestoneOrder Field = "milestone_order"
	FieldArea           Field = "area"
	FieldCreatedAt      Field = "created_at"
	FieldUpdatedAt      Field = "updated_at"
	FieldPath           Field = "path"
	FieldBody           Field = "body"
	FieldETag           Field = "etag"

	// Computed scalars — need the Resolver; not nestable.
	//
	// FieldParent is computed rather than scalar because "the parent" is the
	// RESOLVED link, which no accessor on a single nib can answer — a stored id
	// has to be looked up in the store before it counts as a parent.
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
	// kindComputed is a derived scalar (children/progress/ready) resolved via
	// the Resolver; it is not nestable.
	kindComputed
	// kindRelation is an id-list of related nibs, optionally projected one level
	// deep via a parenthesized sub-selection.
	kindRelation
)

// String renders a field kind as its stable, external-facing token. It is the
// single source of truth for the kind vocabulary surfaced by FieldCatalog (and
// thus the `nibs catalog fields` view), so introspection cannot drift from the
// engine's own classification.
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

// registry is the closed field menu in canonical order. It is the single source
// of truth for the `-f` vocabulary, the JSON key mapping, and the stable output
// ordering (both JSON and text render fields in this order regardless of the
// order the caller listed them). Adding a field here is the only place a new
// field needs to be declared; the exhaustiveness test pins that every entry is
// projectable.
var registry = []fieldDef{
	{name: FieldID, kind: kindScalar, extract: func(n *nib.Nib) any { return n.ID }},
	{name: FieldSlug, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Slug }},
	{name: FieldTitle, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Title }},
	{name: FieldStatus, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Status }},
	{name: FieldType, kind: kindScalar, extract: func(n *nib.Nib) any { return n.EffectiveType() }},
	{name: FieldPriority, kind: kindScalar, extract: func(n *nib.Nib) any { return n.EffectivePriority() }},
	{name: FieldEstimate, kind: kindScalar, extract: func(n *nib.Nib) any { return n.Estimate }},
	{name: FieldTags, kind: kindScalar, extract: func(n *nib.Nib) any { return normStrings(n.Tags) }},
	// parent is computed, not scalar: it reports the RESOLVED parent id (empty
	// when the stored link names no nib), which needs the store. It keeps its
	// position here — the registry is MENU order, not an ordering by kind — so
	// existing output layouts are unchanged. stored_parent sits beside it as the
	// raw stored link, the field that keeps a broken one diagnosable.
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

// FieldMenuString returns the field menu as a comma-separated list, for
// embedding in --help text and the "unknown field" error envelope. Single
// source of truth so help and error messages cannot drift from the menu.
func FieldMenuString() string {
	return joinFields(FieldMenu())
}

// FieldInfo describes one projectable field for external introspection: its
// mask token, its kind (scalar/computed/relation), and the JSON key it
// serializes to. It is the shape the `nibs catalog fields` view renders.
type FieldInfo struct {
	Name    Field
	Kind    string
	JSONKey string
}

// FieldCatalog returns every projectable field in canonical menu order with
// its kind and JSON output key. It is derived from the same registry that
// drives projection, so a catalog built from it can never disagree with what
// `-f`/--fields actually projects or the key each field serializes to.
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

// timeText centralizes the RFC3339 rendering of a timestamp field (empty for a
// nil pointer), matching the internal/output column conventions. It is the leaf
// formatter TextValue delegates timestamps to.
func timeText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
