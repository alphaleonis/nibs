package graph

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
)

// boundingFilterFields and nonBoundingFilterFields are the two halves of one
// classification of model.NibFilter: does this field, on its own, bound the
// working set a top-level search intersects?
//
// Both are spelled in schema names (the json tag), and both are exhaustive by
// test rather than by convention — see
// TestEveryNibFilterFieldIsClassifiedAsBoundingOrNot for what holds them to it.
// The lists exist so the choice is made explicitly for every field: a filter
// added to the schema and left out of both fails that test with instructions,
// instead of defaulting into the unbounded half and silently re-capping a query
// that is already bounded.
var boundingFilterFields = []string{
	"parentId",
	"ancestorId",
	"descendantId",
	"siblingId",
	"blockingId",
	"blockedById",
	"mentionsId",
	"mentionedById",
}

// nonBoundingFilterFields are the fields that narrow without naming a nib, so
// none of them limits the population to a relation and a term combined with one
// of them is still a question about the store's top hits. How large a set the
// field selects is not the test — see hasBoundingFilter.
var nonBoundingFilterFields = []string{
	"search",
	"status",
	"excludeStatus",
	"type",
	"excludeType",
	"priority",
	"excludePriority",
	"estimate",
	"excludeEstimate",
	"tags",
	"excludeTags",
	"hasParent",
	"hasBlocking",
	"isBlocked",
	"hasBlockedBy",
}

// nibFilterFieldNames returns the schema name of every field of
// model.NibFilter, in declaration order.
func nibFilterFieldNames(t *testing.T) []string {
	t.Helper()
	filterType := reflect.TypeOf(model.NibFilter{})
	names := make([]string, 0, filterType.NumField())
	for i := range filterType.NumField() {
		name, _, _ := strings.Cut(filterType.Field(i).Tag.Get("json"), ",")
		if name == "" {
			t.Fatalf("field %s of model.NibFilter has no json name, so it cannot be classified by schema name", filterType.Field(i).Name)
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		t.Fatal("model.NibFilter reflected as having no fields, so every table derived from it guards nothing")
	}
	return names
}

// filterWithOnly returns a NibFilter with exactly the named field populated,
// whatever its shape. An unrecognized shape fails rather than being skipped: a
// new kind of filter field must be driven by these tests, not exempted from
// them by the setter not knowing what to put in it.
func filterWithOnly(t *testing.T, name string) *model.NibFilter {
	t.Helper()
	filter := &model.NibFilter{}
	filterType := reflect.TypeOf(model.NibFilter{})
	for i := range filterType.NumField() {
		tag, _, _ := strings.Cut(filterType.Field(i).Tag.Get("json"), ",")
		if tag != name {
			continue
		}
		target := reflect.ValueOf(filter).Elem().Field(i)
		switch field := filterType.Field(i); {
		case field.Type == reflect.TypeOf((*string)(nil)):
			value := "x"
			target.Set(reflect.ValueOf(&value))
		case field.Type == reflect.TypeOf((*bool)(nil)):
			value := true
			target.Set(reflect.ValueOf(&value))
		case field.Type == reflect.TypeOf([]string(nil)):
			target.Set(reflect.ValueOf([]string{"x"}))
		default:
			t.Fatalf("field %s is a %s, a shape these tests cannot populate — extend filterWithOnly rather than exempting the field", name, field.Type)
		}
		return filter
	}
	t.Fatalf("model.NibFilter has no field named %q", name)
	return nil
}

// TestEveryNibFilterFieldIsClassifiedAsBoundingOrNot is the guard that keeps
// hasBoundingFilter from going stale.
//
// The risk it exists for is a quiet one: a new relationship filter lands, nobody
// thinks about the search bound, and `nibs(filter: {search: q, newThingId: X})`
// starts intersecting an already-bounded question with the store-wide top hits —
// dropping genuine members that rank below the global cutoff, with no error and
// exit 0. Nothing about that failure is visible from the new field's own tests.
//
// So the classification is required to be TOTAL: the two lists above must
// together name every field of model.NibFilter, and name none twice. A field in
// neither fails here, and the maintainer has to decide which half it belongs to.
func TestEveryNibFilterFieldIsClassifiedAsBoundingOrNot(t *testing.T) {
	declared := nibFilterFieldNames(t)

	classified := make(map[string]int, len(declared))
	for _, name := range slices.Concat(boundingFilterFields, nonBoundingFilterFields) {
		classified[name]++
	}

	for _, name := range declared {
		switch classified[name] {
		case 1:
			// Classified exactly once — the only acceptable state.
		case 0:
			t.Errorf("model.NibFilter field %q is in neither boundingFilterFields nor nonBoundingFilterFields; add it to whichever it is — bounding if it narrows the query to the nibs standing in some relationship to a named nib (and then teach hasBoundingFilter about it), non-bounding otherwise. boundingFilterFields is the authority: TestTopLevelSearchWithABoundingFilterReadsTheUncappedIndex walks it, so classifying a field here is the whole decision", name)
		default:
			t.Errorf("model.NibFilter field %q appears %d times across the two classification lists; the halves must be disjoint", name, classified[name])
		}
	}

	for name := range classified {
		if !slices.Contains(declared, name) {
			t.Errorf("%q is classified but is not a field of model.NibFilter; the lists have drifted from the schema", name)
		}
	}
}

// TestHasBoundingFilterMatchesTheClassification holds the predicate itself to
// the lists above, field by field.
//
// Without this the lists could be complete and hasBoundingFilter still wrong:
// classifying a new field as bounding and forgetting the corresponding clause is
// exactly the omission the classification is supposed to catch, and it leaves
// the totality check green.
func TestHasBoundingFilterMatchesTheClassification(t *testing.T) {
	for _, name := range boundingFilterFields {
		t.Run("bounding/"+name, func(t *testing.T) {
			if !hasBoundingFilter(filterWithOnly(t, name)) {
				t.Errorf("hasBoundingFilter is false for a filter setting only %s, which is classified as bounding; a top-level search combined with it would be truncated by the store-wide cap it must not use", name)
			}
		})
	}
	for _, name := range nonBoundingFilterFields {
		t.Run("nonBounding/"+name, func(t *testing.T) {
			if hasBoundingFilter(filterWithOnly(t, name)) {
				t.Errorf("hasBoundingFilter is true for a filter setting only %s, which is classified as non-bounding; a term selecting from the whole store must keep the store-wide cap", name)
			}
		})
	}
	if hasBoundingFilter(nil) {
		t.Error("hasBoundingFilter(nil) is true; an absent filter names no relationship")
	}
	if hasBoundingFilter(&model.NibFilter{}) {
		t.Error("hasBoundingFilter is true for an empty filter; an unset field bounds nothing")
	}
}
