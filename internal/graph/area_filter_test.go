package graph

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// areaFilterAreas declares the vocabulary these tests filter over. Two of its
// roots — `web` and `webhooks` — share a name prefix, which is what separates a
// tree descent from a string-prefix test: nothing else in the fixture can tell
// `strings.HasPrefix(area, ancestor)` apart from genuine closure.
func areaFilterAreas() *config.Areas {
	return &config.Areas{Nodes: []config.AreaConfig{
		{Name: "web", Children: []config.AreaConfig{
			{Name: "dashboard"},
			{Name: "settings", Children: []config.AreaConfig{{Name: "billing"}}},
		}},
		{Name: "webhooks"},
		{Name: "auth"},
	}}
}

// areaFilterFixture holds one nib per declared path, plus the two shapes the
// closure rule has to exclude on their own: a nib with no area at all, and one
// carrying a value below a declared root that the vocabulary does not declare.
func areaFilterFixture() *stubReader {
	nibs := []*nib.Nib{
		{ID: "nibs-web", Title: "Browser client", Status: "todo", Area: "web"},
		{ID: "nibs-dash", Title: "Dashboard", Status: "todo", Area: "web/dashboard"},
		{ID: "nibs-set", Title: "Settings", Status: "todo", Area: "web/settings"},
		{ID: "nibs-bill", Title: "Billing screens", Status: "todo", Area: "web/settings/billing"},
		{ID: "nibs-hook", Title: "Delivery", Status: "todo", Area: "webhooks"},
		{ID: "nibs-auth", Title: "Sign-in", Status: "todo", Area: "auth"},
		{ID: "nibs-stale", Title: "Retired area", Status: "todo", Area: "web/legacy"},
		{ID: "nibs-none", Title: "Unassigned", Status: "todo"},
	}
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	return &stubReader{nibs: byID, allNibs: nibs, prefix: "nibs-", areas: areaFilterAreas()}
}

// TestAreaFilterIsDownwardClosedOverTheDeclaredTree is the whole rule in one
// table: an area filter selects the path named and every declared area below
// it, and nothing else.
//
// Three rows are the ones a plausible wrong implementation passes the rest
// with. `web` must not take in `webhooks` — a string-prefix test admits it, and
// they are two roots. `web/dashboard` must not take in `web` — closure runs
// downward only, so a leaf never pulls its parent in. And `web` must not take in
// `web/legacy`, a value the vocabulary no longer declares: a filter naming a
// former ancestor is exactly how a retired value would be swept back into an
// answer that claims to be about the declared tree.
func TestAreaFilterIsDownwardClosedOverTheDeclaredTree(t *testing.T) {
	reader := areaFilterFixture()
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name string
		area string
		want []string
	}{
		{
			name: "a root takes its whole declared subtree",
			area: "web",
			want: []string{"nibs-web", "nibs-dash", "nibs-set", "nibs-bill"},
		},
		{
			name: "a leaf takes itself alone",
			area: "web/dashboard",
			want: []string{"nibs-dash"},
		},
		{
			name: "an interior node takes itself and what is below it",
			area: "web/settings",
			want: []string{"nibs-set", "nibs-bill"},
		},
		{
			name: "a root sharing a name prefix is a different tree",
			area: "webhooks",
			want: []string{"nibs-hook"},
		},
		{
			name: "an unrelated root",
			area: "auth",
			want: []string{"nibs-auth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			area := tt.area
			got := applyFilterOK(t, context.Background(), reader.allNibs,
				&model.NibFilter{Area: &area}, reader, blocking)
			ids := nibIDs(got)
			slices.Sort(ids)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(ids, want) {
				t.Errorf("area %q selected %v, want %v", tt.area, ids, want)
			}
		})
	}
}

// TestAreaFilterRefusesAnUndeclaredValue pins the refusal rather than the empty
// listing. `--area nosuch` answered with nothing is a factual claim — "no work
// is in this area" — about a value that names no area at all, and a caller who
// mistyped it cannot tell that apart from the truth. It is the class the write
// path already gives the same mistake, so one user error carries one verdict.
func TestAreaFilterRefusesAnUndeclaredValue(t *testing.T) {
	reader := areaFilterFixture()
	nosuch := "nosuch"

	got, err := ApplyFilter(context.Background(), reader.allNibs,
		&model.NibFilter{Area: &nosuch}, reader, &stubBlockingChecker{})
	if err == nil {
		t.Fatalf("an undeclared area returned %d nibs and no error; the empty listing is the answer this refusal replaces", len(got))
	}
	if got != nil {
		t.Errorf("result = %v, want nil alongside the error", got)
	}
	var areaErr *FilterAreaError
	if !errors.As(err, &areaErr) {
		t.Fatalf("error = %T (%v), want *FilterAreaError", err, err)
	}
	if errors.Is(err, nib.ErrNotFound) {
		t.Error("the refusal unwraps to nib.ErrNotFound, which routes a malformed argument to the not-found class (exit 3) instead of validation (exit 2)")
	}
	for _, want := range []string{"area filter", "nosuch", "web/dashboard", "webhooks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

// TestAreaFilterInAStoreDeclaringNoAreasSaysWhy pins the answer to the question
// the axis itself raises. "must be one of " followed by nothing reads as a bug
// in nibs — the reader looks for the list that failed to print — when the real
// answer is that the project has never declared a vocabulary, which is a config
// edit and not a different flag value.
func TestAreaFilterInAStoreDeclaringNoAreasSaysWhy(t *testing.T) {
	reader := areaFilterFixture()
	reader.areas = &config.Areas{} // no vocabulary at all
	if reader.areas.Declared() {
		t.Fatal("the fixture still declares areas, so this row proves nothing")
	}
	web := "web"

	_, err := ApplyFilter(context.Background(), reader.allNibs,
		&model.NibFilter{Area: &web}, reader, &stubBlockingChecker{})
	if err == nil {
		t.Fatal("a store declaring no areas must refuse every area filter, got nil")
	}
	if !strings.Contains(err.Error(), "declares no areas") {
		t.Errorf("error = %q, want it to say the store declares none", err.Error())
	}
	if strings.Contains(err.Error(), "must be one of") {
		t.Errorf("error = %q, must not name an empty allowed set", err.Error())
	}
}

// TestAreaFilterRefusesTheEmptyString keeps the branch from being dropped. Read
// as "unset" an empty value widens the query to the whole store — the confident
// answer to a question nobody asked, and the usual way it arrives is an unset
// shell variable, not a deliberate choice.
func TestAreaFilterRefusesTheEmptyString(t *testing.T) {
	reader := areaFilterFixture()
	empty := ""

	got, err := ApplyFilter(context.Background(), reader.allNibs,
		&model.NibFilter{Area: &empty}, reader, &stubBlockingChecker{})
	if err == nil {
		t.Fatalf("an empty area returned %d of %d nibs and no error; the branch was dropped instead of refused",
			len(got), len(reader.allNibs))
	}
	var areaErr *FilterAreaError
	if !errors.As(err, &areaErr) {
		t.Fatalf("error = %T (%v), want *FilterAreaError", err, err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want it to report the value as empty", err.Error())
	}
}

// TestAreaFilterKeepsTheStoreWideSearchCap is the behavioral half of the
// classification the NibFilter.search doc comment states: an area names a
// declared PATH and not a nib, so it bounds nothing the index has to respect
// and a term alongside it is still a question about the store's top hits.
//
// Asserted at the entry point, where the choice is made, so a regression reads
// as "the wrong search entry point" rather than as a missing row.
func TestAreaFilterKeepsTheStoreWideSearchCap(t *testing.T) {
	reader := areaFilterFixture()
	reader.searchOut = map[string][]*nib.Nib{"anything": reader.allNibs}
	resolver := &Resolver{
		Reader:    reader,
		Writer:    &stubWriter{store: reader},
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, &stubWriter{store: reader}),
	}

	area, term := "web", "anything"
	if _, err := resolver.Query().Nibs(context.Background(),
		&model.NibFilter{Area: &area, Search: &term}, nil); err != nil {
		t.Fatalf("Nibs: %v", err)
	}
	if reader.searchCalls != 1 {
		t.Errorf("Core.Search called %d times, want 1 — an area bounds no relation, so the term keeps the store-wide cap", reader.searchCalls)
	}
	if reader.searchAllCalls != 0 {
		t.Errorf("Core.SearchAll called %d times, want 0 — reading uncapped here would replace the top hits for the term with the whole match set", reader.searchAllCalls)
	}
}
