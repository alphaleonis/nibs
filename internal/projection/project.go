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

// Resolver answers what the engine cannot read off one nib: computed fields and
// relations. Relation methods return ids in an order the engine keeps.
type Resolver interface {
	// NibByID returns the nib with the given id and whether it exists.
	NibByID(id string) (*nib.Nib, bool)
	// ParentID returns the resolved parent id, or "" when there is none or the
	// stored link names no nib.
	ParentID(id string) string
	// ChildCount returns the number of direct children of the nib.
	ChildCount(id string) int
	// Progress returns the progress value, serialized as is.
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

// ProjectedField is one projected JSON key and its typed value.
type ProjectedField struct {
	Key   string
	Value any
}

// Projected is one nib's selected fields in menu order. It marshals to a flat
// JSON object with no wrapper.
type Projected struct {
	fields []ProjectedField
}

// Project projects n through sel. r may be nil only when sel holds scalar fields
// and bare blocked-by; otherwise it returns an error. `parent` is computed, so the
// card and full views need a resolver.
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
		return ids, nil
	}
	// Nested form: project each related nib; an id naming no nib is skipped.
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

// relationIDs returns a relation's ids. blocked-by is read off the nib; the others
// need the Resolver.
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

// subSelection builds a Selection from a parsed sub-field set, which holds no
// relations.
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

// MarshalJSON writes the fields as a flat JSON object in menu order.
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

// TextValue renders a value as text: RFC3339 timestamps, comma-joined string
// lists, "" for nil, and JSON for anything else.
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
