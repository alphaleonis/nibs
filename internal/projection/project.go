package projection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Resolver is the store accessor the projection engine depends on for anything
// it cannot read off a single nib: nested relation expansion and the computed
// fields. It is an interface so the engine stays unit-testable against a fake;
// a later feature supplies the real implementation backed by internal/nibcore.
//
// All methods take a nib ID. Relation methods return the related nib IDs (in
// whatever order the implementation defines); the engine preserves that order.
type Resolver interface {
	// NibByID returns the nib with the given ID and whether it exists. Used to
	// expand a nested relation sub-selection.
	NibByID(id string) (*nib.Nib, bool)
	// ParentID returns the nib's RESOLVED parent id, or "" when it has no parent
	// — including when its stored link names no nib. Reading the stored link
	// instead is the `stored_parent` field, which needs no resolver.
	ParentID(id string) string
	// ChildCount returns the number of direct children of the nib.
	ChildCount(id string) int
	// Progress returns the progress rollup value for the nib. The engine treats
	// it as an opaque, JSON-serializable value; the concrete shape (a percentage,
	// an acceptance-count struct, …) is defined by the implementation.
	Progress(id string) any
	// Ready reports whether the nib is ready (startable / unblocked).
	Ready(id string) bool
	// Blocking returns the IDs of nibs this nib is blocking.
	Blocking(id string) []string
	// Mentions returns the IDs of nibs this nib's body mentions.
	Mentions(id string) []string
	// MentionedBy returns the IDs of nibs whose bodies mention this nib.
	MentionedBy(id string) []string
}

// ProjectedField is one projected (JSON key, value) pair. Value is the typed Go
// value (string, *time.Time, []string, int, bool, []*Projected, or an opaque
// computed value), so callers can render it as text or serialize it as JSON.
type ProjectedField struct {
	Key   string
	Value any
}

// Projected is the projection of one nib: an ordered set of selected fields.
// Field order is the canonical menu order, stable regardless of how the caller
// listed its fields, and used for both text rendering and JSON. It marshals to
// a flat JSON object of the selected fields with NO wrapper — the inner object
// of the later {nib} contract.
type Projected struct {
	fields []ProjectedField
}

// Project projects a single nib through a selection. A nil Resolver is allowed
// as long as the selection touches only scalar fields (and bare blocked-by,
// which is read directly off the nib); a computed field, a resolver-backed
// relation, or any nested relation with a nil Resolver returns an error rather
// than silently omitting data.
//
// `parent` is computed, so the card and full view tiers both need a Resolver.
// Only `stored_parent` reads a parent link without one, and it reads the raw
// stored id rather than the parent.
func Project(n *nib.Nib, sel Selection, r Resolver) (*Projected, error) {
	if n == nil {
		return nil, fmt.Errorf("cannot project a nil nib")
	}
	p := &Projected{}
	for _, d := range registry {
		subset, ok := sel.sel[d.name]
		if !ok {
			continue
		}
		val, err := projectField(n, d, subset, r)
		if err != nil {
			return nil, err
		}
		p.fields = append(p.fields, ProjectedField{Key: d.key(), Value: val})
	}
	return p, nil
}

func projectField(n *nib.Nib, d fieldDef, sub map[Field]struct{}, r Resolver) (any, error) {
	switch d.kind {
	case kindScalar:
		return d.extract(n), nil
	case kindComputed:
		return projectComputed(n, d.name, r)
	case kindRelation:
		return projectRelation(n, d.name, sub, r)
	default:
		return nil, fmt.Errorf("internal: field %q has unknown kind", d.name)
	}
}

func projectComputed(n *nib.Nib, f Field, r Resolver) (any, error) {
	if r == nil {
		return nil, resolverRequired(f)
	}
	switch f {
	case FieldParent:
		return r.ParentID(n.ID), nil
	case FieldChildren:
		return r.ChildCount(n.ID), nil
	case FieldProgress:
		return r.Progress(n.ID), nil
	case FieldReady:
		return r.Ready(n.ID), nil
	default:
		return nil, fmt.Errorf("internal: unknown computed field %q", f)
	}
}

func projectRelation(n *nib.Nib, f Field, sub map[Field]struct{}, r Resolver) (any, error) {
	ids, err := relationIDs(n, f, r)
	if err != nil {
		return nil, err
	}
	if len(sub) == 0 {
		// Bare / id-list form.
		return ids, nil
	}
	// Nested form: expand each related nib and project the sub-selection. This
	// always needs NibByID; a dangling reference (id resolves to no nib) is
	// skipped rather than erroring, since a stored id may point at a deleted or
	// archived nib.
	if r == nil {
		return nil, resolverRequired(f)
	}
	subSel := subSelection(sub)
	out := make([]*Projected, 0, len(ids))
	for _, id := range ids {
		child, ok := r.NibByID(id)
		if !ok {
			continue
		}
		cp, err := Project(child, subSel, r)
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, nil
}

// relationIDs returns the related nib IDs for a relation field. blocked-by is
// read straight off the nib (no Resolver needed for the bare form); the other
// three are computed from the rest of the store via the Resolver.
func relationIDs(n *nib.Nib, f Field, r Resolver) ([]string, error) {
	switch f {
	case FieldBlockedBy:
		return normStrings(n.BlockedBy), nil
	case FieldBlocking:
		if r == nil {
			return nil, resolverRequired(f)
		}
		return normStrings(r.Blocking(n.ID)), nil
	case FieldMentions:
		if r == nil {
			return nil, resolverRequired(f)
		}
		return normStrings(r.Mentions(n.ID)), nil
	case FieldMentionedBy:
		if r == nil {
			return nil, resolverRequired(f)
		}
		return normStrings(r.MentionedBy(n.ID)), nil
	default:
		return nil, fmt.Errorf("internal: unknown relation field %q", f)
	}
}

// subSelection builds a Selection from a validated sub-field set. The parser
// guarantees the set holds only scalar/computed fields, so the resulting child
// projection never recurses into another relation.
func subSelection(sub map[Field]struct{}) Selection {
	s := newSelection()
	for f := range sub {
		s.add(f, nil)
	}
	return s
}

func resolverRequired(f Field) error {
	return fmt.Errorf("field %q requires a resolver but none was provided", f)
}

// Keys returns the projected JSON keys in canonical menu order.
func (p *Projected) Keys() []string {
	out := make([]string, len(p.fields))
	for i, f := range p.fields {
		out[i] = f.Key
	}
	return out
}

// Fields returns a copy of the projected fields in canonical menu order.
func (p *Projected) Fields() []ProjectedField {
	out := make([]ProjectedField, len(p.fields))
	copy(out, p.fields)
	return out
}

// Get returns the value for a JSON key and whether it was projected.
func (p *Projected) Get(key string) (any, bool) {
	for _, f := range p.fields {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// MarshalJSON serializes the projection as a flat JSON object of the selected
// fields in menu order (an ordinary map would lose that order). There is no
// wrapper: this is the inner object of the later {nib} contract.
func (p *Projected) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range p.fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(f.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// TextValue renders a projected leaf value as a single stable string, following
// the internal/output conventions (RFC3339 timestamps, comma-joined string
// lists, empty string for a missing value). It is the leaf formatter for a
// later TSV renderer. Non-leaf values (a nested relation's []*Projected, or an
// opaque computed value) fall back to their JSON encoding so text output stays
// lossless.
func TextValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case []string:
		return strings.Join(t, ",")
	case *time.Time:
		return timeText(t)
	case time.Time:
		return timeText(&t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
