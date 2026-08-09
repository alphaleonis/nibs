package projection

import (
	"fmt"
	"strings"
)

// View is a coarse, one-token preset expanding to a field set. View tiers are
// the cheap "give me roughly this shape" mechanism; `-f` refines them.
type View string

// The view tiers, from leanest to fullest.
const (
	ViewID   View = "id"
	ViewRef  View = "ref"
	ViewCard View = "card"
	ViewFull View = "full"
)

// viewOrder fixes the display order of view names in error messages.
var viewOrder = []View{ViewID, ViewRef, ViewCard, ViewFull}

// viewSets maps each view tier to its field list (§5.2). All fields are in
// id-list form for relations. `full` is every field except the opt-in rollups
// (children/progress/ready), which are never in a tier and are reachable only
// via an explicit `-f`.
//
// `parent` is in card and full and is a COMPUTED field (it resolves the stored
// link through the store), so both tiers require a non-nil Resolver — see
// Project.
var viewSets = map[View][]Field{
	ViewID:  {FieldID},
	ViewRef: {FieldID, FieldTitle, FieldStatus, FieldType, FieldPriority},
	ViewCard: {
		FieldID, FieldTitle, FieldStatus, FieldType, FieldPriority,
		FieldEstimate, FieldTags, FieldParent, FieldBlockedBy, FieldOrder, FieldUpdatedAt,
	},
	ViewFull: fullViewFields(),
}

// rollupFields are the fields no view tier carries: each costs a store walk and
// is wanted only when named explicitly. Membership is by NAME rather than by
// kind because `parent` is computed too — it resolves its stored link — yet is a
// plain identifying field that belongs in a full read.
var rollupFields = map[Field]bool{FieldChildren: true, FieldProgress: true, FieldReady: true}

// fullViewFields is every field except the opt-in rollups, in menu order.
func fullViewFields() []Field {
	out := make([]Field, 0, len(registry))
	for _, d := range registry {
		if !rollupFields[d.name] {
			out = append(out, d.name)
		}
	}
	return out
}

// viewNames renders the valid view names for error messages.
func viewNames() string {
	names := make([]string, len(viewOrder))
	for i, v := range viewOrder {
		names[i] = string(v)
	}
	return strings.Join(names, ", ")
}

// Selection is a parsed, validated field selection: the set of fields to
// project, each relation optionally carrying a one-level sub-selection. It is a
// set (not an ordered list): output order is always the canonical menu order,
// so a Selection is stable regardless of how the caller listed its fields, and
// merging view + `-f` (or re-listing a field) is idempotent.
type Selection struct {
	// sel maps each selected field to its sub-selection set. The sub-selection
	// is non-empty only for a relation selected in nested form; scalar, computed,
	// and id-list-form relation fields map to an empty (but present) set.
	sel map[Field]map[Field]struct{}
}

func newSelection() Selection {
	return Selection{sel: map[Field]map[Field]struct{}{}}
}

// add records a field selection, unioning any sub-fields into the existing
// sub-selection (so bare + nested, or two nested forms, merge monotonically).
func (s Selection) add(f Field, sub map[Field]struct{}) {
	existing, ok := s.sel[f]
	if !ok {
		existing = map[Field]struct{}{}
		s.sel[f] = existing
	}
	for k := range sub {
		existing[k] = struct{}{}
	}
}

// Has reports whether the field is selected.
func (s Selection) Has(f Field) bool {
	_, ok := s.sel[f]
	return ok
}

// IsEmpty reports whether no fields are selected.
func (s Selection) IsEmpty() bool { return len(s.sel) == 0 }

// Fields returns the selected top-level fields in canonical menu order.
func (s Selection) Fields() []Field {
	out := make([]Field, 0, len(s.sel))
	for _, d := range registry {
		if _, ok := s.sel[d.name]; ok {
			out = append(out, d.name)
		}
	}
	return out
}

// Sub returns the nested sub-selection for a relation field in menu order, or
// nil when the field is absent or was selected in id-list (bare) form.
func (s Selection) Sub(f Field) []Field {
	subset, ok := s.sel[f]
	if !ok || len(subset) == 0 {
		return nil
	}
	out := make([]Field, 0, len(subset))
	for _, d := range registry {
		if _, ok := subset[d.name]; ok {
			out = append(out, d.name)
		}
	}
	return out
}

// Merge returns a new Selection that is the union of s and other. Relation
// sub-selections are unioned per field; a field selected bare in one and nested
// in the other becomes nested (more specific wins). Neither input is mutated.
func (s Selection) Merge(other Selection) Selection {
	out := newSelection()
	for f, sub := range s.sel {
		out.add(f, sub)
	}
	for f, sub := range other.sel {
		out.add(f, sub)
	}
	return out
}

// ViewFields resolves a view tier name to its preset Selection. An unknown name
// is rejected with an error naming the valid views.
func ViewFields(name string) (Selection, error) {
	fields, ok := viewSets[View(name)]
	if !ok {
		return Selection{}, fmt.Errorf("unknown view %q; valid views: %s", name, viewNames())
	}
	s := newSelection()
	for _, f := range fields {
		s.add(f, nil)
	}
	return s, nil
}

// ParseFields parses a `-f`/--fields spec: a comma-separated list where each
// token is a bare field, a view/preset name (expanded to its field set), or a
// relation with a one-level parenthesized sub-selection (e.g.
// "blocked-by(id,status)"). An empty spec, an unknown token, a scalar given a
// sub-selection, a relation nested inside a sub-selection, or nesting deeper
// than one level are all rejected — the "unknown token" error names the whole
// menu so the surface is self-documenting.
//
// Fields are a set: duplicates (including a field already implied by an expanded
// view name) merge idempotently. This is deliberately unlike
// output.ParseColumns, whose duplicate-rejection guards TSV column indices;
// here output is keyed by name and menu-ordered, and the additive view + `-f`
// model requires idempotent re-listing.
func ParseFields(spec string) (Selection, error) {
	if strings.TrimSpace(spec) == "" {
		return Selection{}, fmt.Errorf("fields spec is empty; valid fields: %s", FieldMenuString())
	}
	tokens, err := splitTopLevel(spec)
	if err != nil {
		return Selection{}, err
	}
	s := newSelection()
	for _, tok := range tokens {
		if err := parseToken(s, tok); err != nil {
			return Selection{}, err
		}
	}
	return s, nil
}

// Compile builds a Selection from an optional view tier and an optional additive
// `-f` fields spec. The view (if any) is applied first, then the fields are
// merged on top (§5.2: `-f` is additive over the view). Either argument may be
// empty; if both are empty the result is an empty Selection and the caller
// decides on a default.
func Compile(view, fields string) (Selection, error) {
	sel := newSelection()
	if strings.TrimSpace(view) != "" {
		vsel, err := ViewFields(view)
		if err != nil {
			return Selection{}, err
		}
		sel = vsel
	}
	if strings.TrimSpace(fields) != "" {
		fsel, err := ParseFields(fields)
		if err != nil {
			return Selection{}, err
		}
		sel = sel.Merge(fsel)
	}
	return sel, nil
}

// splitTopLevel splits a fields spec on commas that sit outside any parentheses,
// so a relation sub-selection ("blocked-by(id,status)") stays one token. It
// rejects unbalanced parentheses.
func splitTopLevel(spec string) ([]string, error) {
	var tokens []string
	depth := 0
	start := 0
	for i := 0; i < len(spec); i++ {
		switch spec[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced ')' in fields spec %q", spec)
			}
		case ',':
			if depth == 0 {
				tokens = append(tokens, spec[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced '(' in fields spec %q", spec)
	}
	return append(tokens, spec[start:]), nil
}

// parseToken parses one top-level token into s: a relation-with-sub-selection,
// a bare field, or a view name (expanded).
func parseToken(s Selection, tok string) error {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return fmt.Errorf("empty field entry; valid fields: %s", FieldMenuString())
	}

	name, inner, hasParen, err := cutParen(tok)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if hasParen {
		return parseRelationToken(s, name, inner)
	}

	if d, ok := defByName[Field(name)]; ok {
		s.add(d.name, nil)
		return nil
	}
	if fields, ok := viewSets[View(name)]; ok {
		for _, f := range fields {
			s.add(f, nil)
		}
		return nil
	}
	return fmt.Errorf("unknown field %q; valid fields: %s", name, FieldMenuString())
}

// cutParen splits a token "rel(sub,sub)" into its name and inner sub-selection.
// A token with no '(' returns hasParen=false. It rejects a token that has a '('
// but no trailing ')', and a sub-selection that itself contains parentheses
// (nesting deeper than one level).
func cutParen(tok string) (name, inner string, hasParen bool, err error) {
	open := strings.IndexByte(tok, '(')
	if open < 0 {
		return tok, "", false, nil
	}
	if !strings.HasSuffix(tok, ")") {
		return "", "", false, fmt.Errorf("malformed field %q: expected ')' at end of sub-selection", tok)
	}
	inner = tok[open+1 : len(tok)-1]
	if strings.ContainsAny(inner, "()") {
		return "", "", false, fmt.Errorf("field %q nests too deep: relation sub-selections are one level only", tok)
	}
	return tok[:open], inner, true, nil
}

// parseRelationToken validates and records a relation with a sub-selection.
// The named field must be a relation; each sub-field must be a scalar or
// computed field (a relation may not be nested inside a sub-selection).
func parseRelationToken(s Selection, name, inner string) error {
	d, ok := defByName[Field(name)]
	if !ok {
		return fmt.Errorf("unknown field %q; valid fields: %s", name, FieldMenuString())
	}
	if d.kind != kindRelation {
		return fmt.Errorf("field %q does not support a sub-selection; only relation fields (%s) can be nested", name, relationNames())
	}
	if strings.TrimSpace(inner) == "" {
		return fmt.Errorf("relation %q has an empty sub-selection; valid sub-fields: %s", name, subFieldNames())
	}

	sub := map[Field]struct{}{}
	for _, st := range strings.Split(inner, ",") {
		st = strings.TrimSpace(st)
		if st == "" {
			return fmt.Errorf("relation %q has an empty sub-selection entry; valid sub-fields: %s", name, subFieldNames())
		}
		sd, ok := defByName[Field(st)]
		if !ok {
			return fmt.Errorf("unknown sub-field %q in %s(...); valid sub-fields: %s", st, name, subFieldNames())
		}
		if sd.kind == kindRelation {
			return fmt.Errorf("sub-field %q in %s(...) is a relation; only scalar and computed fields may be nested one level deep", st, name)
		}
		sub[sd.name] = struct{}{}
	}
	s.add(d.name, sub)
	return nil
}
