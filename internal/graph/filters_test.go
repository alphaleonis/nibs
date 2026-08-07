package graph

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// applyFilterOK runs ApplyFilter and fails the test if it reports an error.
// Every test whose subject is WHICH nibs come back uses it, so a filter that
// fails where it should match cannot be mistaken for a filter that matched
// nothing. The tests whose subject is the error itself call
// ApplyFilter directly.
func applyFilterOK(t *testing.T, ctx context.Context, nibs []*nib.Nib, filter *model.NibFilter, reader NibReader, blocking BlockingChecker) []*nib.Nib {
	t.Helper()
	got, err := ApplyFilter(ctx, nibs, filter, reader, blocking)
	if err != nil {
		t.Fatalf("ApplyFilter: unexpected error: %v", err)
	}
	return got
}

// TestResolveFilterID exercises the shared helper used by every filter.*ID
// branch in ApplyFilter. It must return the full ID for a known short form
// and the echoed input with ok=false for an unknown target.
func TestResolveFilterID(t *testing.T) {
	target := &nib.Nib{ID: "nibs-target", Title: "Target"}
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"nibs-target": target},
		allNibs: []*nib.Nib{target},
		prefix:  "nibs-",
	}

	t.Run("returns full ID for known short form", func(t *testing.T) {
		fullID, ok := resolveFilterID(reader, "target")
		if !ok {
			t.Fatalf("expected ok=true for known short form")
		}
		if fullID != "nibs-target" {
			t.Errorf("fullID = %q, want %q", fullID, "nibs-target")
		}
	})

	t.Run("returns echoed id and false for unknown target (matches NibReader.NormalizeID)", func(t *testing.T) {
		fullID, ok := resolveFilterID(reader, "nonexistent")
		if ok {
			t.Errorf("expected ok=false for unknown target, got ok=true (fullID=%q)", fullID)
		}
		// resolveFilterID is a pass-through to NormalizeID; on miss, NormalizeID
		// echoes the input id (Core convention). Callers gate on ok, not on the
		// string, so the echoed value is informational only.
		if fullID != "nonexistent" {
			t.Errorf("fullID = %q, want echoed input %q on miss", fullID, "nonexistent")
		}
	})
}

// TestApplyFilterBlockedByIDShortForm is the tracer bullet: a filter with
// a short `BlockedByID` must match nibs whose `blocked_by` contains the
// full (prefixed) ID. A short BlockedByID must be normalized before matching —
// passing it raw to filterBySliceField makes short IDs silently match nothing.
func TestApplyFilterBlockedByIDShortForm(t *testing.T) {
	target := &nib.Nib{ID: "nibs-target", Title: "Target"}
	blocked := &nib.Nib{ID: "nibs-blocked", Title: "Blocked", BlockedBy: []string{"nibs-target"}}
	unrelated := &nib.Nib{ID: "nibs-other", Title: "Other"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-target":  target,
			"nibs-blocked": blocked,
			"nibs-other":   unrelated,
		},
		allNibs: []*nib.Nib{target, blocked, unrelated},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	filter := &model.NibFilter{BlockedByID: strPtr("target")}
	got := applyFilterOK(t, context.Background(), reader.allNibs, filter, reader, blocking)

	if len(got) != 1 {
		t.Fatalf("got %d nibs, want 1 (nibs-blocked)", len(got))
	}
	if got[0].ID != "nibs-blocked" {
		t.Errorf("got %q, want %q", got[0].ID, "nibs-blocked")
	}
}

// TestApplyFilterParentIDShortForm pins that a short ParentID is normalized
// before matching, exactly like the other filter.*ID branches: a filter with
// ParentID "parent" must match nibs whose Parent field holds the full
// "nibs-parent". Passing the short id raw to the exact-match filter makes
// `list --parent <short-id>` silently return nothing.
func TestApplyFilterParentIDShortForm(t *testing.T) {
	parent := &nib.Nib{ID: "nibs-parent", Title: "Parent"}
	child := &nib.Nib{ID: "nibs-child", Title: "Child", Parent: "nibs-parent"}
	unrelated := &nib.Nib{ID: "nibs-other", Title: "Other"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-parent": parent,
			"nibs-child":  child,
			"nibs-other":  unrelated,
		},
		allNibs: []*nib.Nib{parent, child, unrelated},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	filter := &model.NibFilter{ParentID: strPtr("parent")}
	got := applyFilterOK(t, context.Background(), reader.allNibs, filter, reader, blocking)

	if len(got) != 1 {
		t.Fatalf("got %d nibs, want 1 (nibs-child); short ParentID was not normalized", len(got))
	}
	if got[0].ID != "nibs-child" {
		t.Errorf("got %q, want %q", got[0].ID, "nibs-child")
	}
}

// TestApplyFilterUnknownTargetIsNotFound is the guard for the whole point of
// ApplyFilter's error return: every filter field that names a single nib must
// REFUSE an id no nib answers to, rather than answering it with the empty set.
//
// All eight *ID branches are covered in one table because the contract is
// shared — a branch added without its guard, or a guard deleted from one, is
// exactly the regression this catches. The assertions are on the CLASS, not on
// message text: nib.ErrNotFound is what the GraphQL presenter and the CLI
// boundary both key on, so a type that stopped carrying it would keep passing a
// message-shaped assertion while silently losing its exit code.
//
// The Field payload is checked too: an error naming the wrong field is still a
// not-found and still exits 3, and still leaves the caller unable to find the
// typo. The ID is asserted for shape only — NormalizeID echoes its input on a
// miss, so the supplied and normalized forms are identical on every error path
// and these rows cannot tell them apart. Should resolveFilterID ever gain a
// transform (trimming, case folding), "echo back what the caller typed" needs
// its own row with a fixture where the two genuinely differ.
func TestApplyFilterUnknownTargetIsNotFound(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name      string
		filter    *model.NibFilter
		wantField string
	}{
		{"parentId", &model.NibFilter{ParentID: strPtr("nonexistent")}, "parentId"},
		{"ancestorId", &model.NibFilter{AncestorID: strPtr("nonexistent")}, "ancestorId"},
		{"descendantId", &model.NibFilter{DescendantID: strPtr("nonexistent")}, "descendantId"},
		{"siblingId", &model.NibFilter{SiblingID: strPtr("nonexistent")}, "siblingId"},
		{"blockingId", &model.NibFilter{BlockingID: strPtr("nonexistent")}, "blockingId"},
		{"blockedById", &model.NibFilter{BlockedByID: strPtr("nonexistent")}, "blockedById"},
		{"mentionsId", &model.NibFilter{MentionsID: strPtr("nonexistent")}, "mentionsId"},
		{"mentionedById", &model.NibFilter{MentionedByID: strPtr("nonexistent")}, "mentionedById"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking)
			if err == nil {
				t.Fatalf("%s with an unknown target returned %d nibs and no error; an unanswerable question must not read as an empty answer", tt.wantField, len(got))
			}
			if got != nil {
				t.Errorf("result = %v, want nil alongside the error", got)
			}
			if !errors.Is(err, nib.ErrNotFound) {
				t.Errorf("error does not carry nib.ErrNotFound, so the presenter cannot tag it NOT_FOUND and the CLI cannot exit 3: %v", err)
			}
			var notFound *FilterTargetNotFoundError
			if !errors.As(err, &notFound) {
				t.Fatalf("error = %T (%v), want *FilterTargetNotFoundError", err, err)
			}
			if notFound.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", notFound.Field, tt.wantField)
			}
			if notFound.ID != "nonexistent" {
				t.Errorf("ID = %q, want the id as supplied (%q)", notFound.ID, "nonexistent")
			}
		})
	}
}

// TestEveryIDValuedFilterFieldHasAGuard derives the same contract from
// model.NibFilter itself instead of restating it: every *string field whose json
// tag ends in "Id", set alone to an id no nib answers to, must make ApplyFilter
// refuse with a *FilterTargetNotFoundError naming that field.
//
// The table above hand-lists the eight fields that exist today, so a NINTH one
// shipped without its guard fails nothing: it resolves through resolveFilterID,
// narrows silently to the empty set, and every existing row keeps passing. The
// per-branch guards are otherwise enforced only by review, on a surface that is
// not settled.
//
// The json tag is the whole selector, and it is applied BEFORE any check on the
// field's shape — an id-named field this test cannot drive fails here rather
// than being skipped, so a plural or differently-typed id filter cannot be
// exempted by accident. Search is the only other *string field and does not end
// in "Id"; every list facet is a []string whose tag does not either. A field
// that genuinely should not refuse has to be excepted deliberately.
func TestEveryIDValuedFilterFieldHasAGuard(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	for _, field := range idValuedFilterFields(t) {
		t.Run(field.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, field.filterWith("nonexistent"), reader, blocking)
			if err == nil {
				t.Fatalf("%s naming no nib returned %d nibs and no error; that branch ships without its guard", field.name, len(got))
			}
			var notFound *FilterTargetNotFoundError
			if !errors.As(err, &notFound) {
				t.Fatalf("error = %T (%v), want *FilterTargetNotFoundError", err, err)
			}
			if notFound.Field != field.name {
				t.Errorf("Field = %q, want the schema spelling %q", notFound.Field, field.name)
			}
		})
	}
}

// idFilterField is one id-valued field of model.NibFilter, paired with a
// constructor that builds a filter setting only that field. Driving the field
// through the constructor rather than through a raw reflect.Value keeps the
// reflection in one place.
type idFilterField struct {
	name  string
	index int
}

// filterWith returns a NibFilter with this field — and only this field — set to
// value.
func (f idFilterField) filterWith(value string) *model.NibFilter {
	filter := &model.NibFilter{}
	reflect.ValueOf(filter).Elem().Field(f.index).Set(reflect.ValueOf(&value))
	return filter
}

// idValuedFilterFields selects every *string field of model.NibFilter whose json
// tag ends in "Id", which is the set every "all the id filters behave alike"
// test derives its rows from instead of restating them. A NINTH id field shipped
// without its guards therefore fails those tests rather than passing them by
// omission.
//
// The json tag is the whole selector, and it is applied BEFORE any check on the
// field's shape — an id-named field these tests cannot drive fails here rather
// than being skipped, so a plural or differently-typed id filter cannot be
// exempted by accident. Search is the only other *string field and does not end
// in "Id"; every list facet is a []string whose tag does not either. A field
// that genuinely should not refuse has to be excepted deliberately.
func idValuedFilterFields(t *testing.T) []idFilterField {
	t.Helper()

	filterType := reflect.TypeOf(model.NibFilter{})
	var fields []idFilterField
	for i := range filterType.NumField() {
		field := filterType.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !strings.HasSuffix(name, "Id") && !strings.HasSuffix(name, "Ids") {
			continue
		}
		// Shape is checked only after the NAME has selected the field. Selecting
		// on shape first would exempt a plural id filter silently, since these
		// tests can only drive a *string; failing here instead forces whoever
		// adds one to extend them rather than slip past.
		if field.Type.Kind() != reflect.Pointer || field.Type.Elem().Kind() != reflect.String {
			t.Fatalf("%s is id-named but %s, which these tests cannot drive — extend them rather than exempting the field", name, field.Type)
		}
		fields = append(fields, idFilterField{name: name, index: i})
	}

	// Without this the walk is free to match nothing and report success — a
	// renamed json tag or a change of field type would quietly empty every
	// table derived from it.
	if len(fields) == 0 {
		t.Fatal("the reflective rule matched no field of model.NibFilter, so the tests derived from it guard nothing")
	}
	return fields
}

// TestEveryIDValuedFilterFieldRefusesAnEmptyValue is the guard for the
// strongest form of the silent-widening bug: an id-valued filter set to the
// EMPTY STRING must be refused as a validation error, not dropped.
//
// An empty id must never be read as "unset": dropping the branch answers
// `nibs(filter:{parentId:""})` with the WHOLE STORE. That is the worst possible
// answer for the input that produces it — an empty id is what a client sends
// when a variable did not interpolate (`--parent "$ID"` with ID unset), so the
// query that most needs refusing is the one that would widen to everything.
//
// Empty is a distinct class from unknown, not a NOT_FOUND: there is no nib
// whose id is "", so nothing was mistyped and no store lookup could ever
// succeed. The CLI flag layer already answers this the same way
// (cmd/list.go rejects `--parent ""` as a validation error), and the two
// surfaces must not disagree about one user error.
//
// Derived reflectively for the same reason the unknown-target guard is: a ninth
// id field added without the check fails here instead of shipping silent.
func TestEveryIDValuedFilterFieldRefusesAnEmptyValue(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	for _, field := range idValuedFilterFields(t) {
		t.Run(field.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, field.filterWith(""), reader, blocking)
			if err == nil {
				t.Fatalf("%s set to the empty string returned %d of %d nibs and no error; the branch was dropped instead of refused",
					field.name, len(got), len(reader.allNibs))
			}
			if got != nil {
				t.Errorf("result = %v, want nil alongside the error", got)
			}
			var empty *FilterTargetEmptyError
			if !errors.As(err, &empty) {
				t.Fatalf("error = %T (%v), want *FilterTargetEmptyError", err, err)
			}
			if empty.Field != field.name {
				t.Errorf("Field = %q, want the schema spelling %q", empty.Field, field.name)
			}
			// The class must stay validation, not not-found: exit 2 says the
			// caller's input was malformed, exit 3 says a real id named a
			// missing nib. Carrying the sentinel would collapse the two at
			// every classifier that keys on it.
			if errors.Is(err, nib.ErrNotFound) {
				t.Error("an empty id carries nib.ErrNotFound, so it classifies as NOT_FOUND (exit 3) — an empty string names no nib to be missing")
			}
			var notFound *FilterTargetNotFoundError
			if errors.As(err, &notFound) {
				t.Error("errors.As matched *FilterTargetNotFoundError, so the empty and unknown classes are not distinguishable")
			}
		})
	}
}

// TestApplyFilterIDTargetBoundaries walks one id-valued branch across every
// value an id-valued filter field can hold, INCLUDING the values between the
// interesting cases. The branch is a decision point with four outcomes and the
// boundaries between them are where it breaks: a guard written only against nil
// and a valid id ships broken for "" and for " ".
//
// The whitespace-only row is the deliberate one. " " is NOT treated as empty:
// the emptiness test is an exact `== ""`, so a whitespace-only value travels
// into NormalizeID like any other id, resolves to nothing and is refused as
// NOT_FOUND. That matches cmd/list.go, which tests its flags the same exact way
// — trimming here would make the graph layer stricter than the flag surface
// feeding it, and would mean resolveFilterID silently rewrote the id the
// not-found error echoes back.
//
// parentId is the representative branch; the two reflective tests above cover
// the empty and unknown cases on all eight.
func TestApplyFilterIDTargetBoundaries(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	// outcome names the four answers a branch can give, so a row states its
	// expectation rather than a tangle of booleans.
	type outcome int
	const (
		unfiltered     outcome = iota // no filtering applied at all
		matched                       // the branch ran and selected
		refusedEmpty                  // *FilterTargetEmptyError, validation class
		refusedUnknown                // *FilterTargetNotFoundError, not-found class
	)

	tests := []struct {
		name    string
		value   *string
		want    outcome
		wantIDs []string
	}{
		{"nil leaves the field unfiltered", nil, unfiltered, nil},
		{"the empty string is refused as malformed input", strPtr(""), refusedEmpty, nil},
		{"a whitespace-only id is an unresolvable id, not an empty one", strPtr(" "), refusedUnknown, nil},
		{"a short id resolves and selects", strPtr("e1"), matched, []string{"nibs-f1", "nibs-t2"}},
		{"a full id resolves and selects", strPtr("nibs-e1"), matched, []string{"nibs-f1", "nibs-t2"}},
		{"an unknown id is refused as not-found", strPtr("nonexistent"), refusedUnknown, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{ParentID: tt.value}, reader, blocking)

			switch tt.want {
			case refusedEmpty:
				var empty *FilterTargetEmptyError
				if !errors.As(err, &empty) {
					t.Fatalf("error = %T (%v) over %d nibs, want *FilterTargetEmptyError", err, err, len(got))
				}
			case refusedUnknown:
				var notFound *FilterTargetNotFoundError
				if !errors.As(err, &notFound) {
					t.Fatalf("error = %T (%v) over %d nibs, want *FilterTargetNotFoundError", err, err, len(got))
				}
				if notFound.ID != *tt.value {
					t.Errorf("ID = %q, want the id as supplied (%q) — the error echoes what the caller typed", notFound.ID, *tt.value)
				}
			case unfiltered:
				if err != nil {
					t.Fatalf("a nil field must not filter or fail: %v", err)
				}
				if len(got) != len(reader.allNibs) {
					t.Errorf("got %d nibs, want all %d — a nil field filters nothing", len(got), len(reader.allNibs))
				}
			case matched:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				gotIDs := make([]string, 0, len(got))
				for _, b := range got {
					gotIDs = append(gotIDs, b.ID)
				}
				sort.Strings(gotIDs)
				want := append([]string(nil), tt.wantIDs...)
				sort.Strings(want)
				if !reflect.DeepEqual(gotIDs, want) {
					t.Errorf("got IDs %v, want %v", gotIDs, want)
				}
			default:
				// A row naming an outcome no case handles would run zero
				// assertions and pass whatever ApplyFilter did — the vacuous
				// pass this enum exists to make impossible.
				t.Fatalf("unhandled outcome %v", tt.want)
			}
		})
	}
}

// TestApplyFilterEmptyMatchIsNotAnError is the other half of the contract: a
// filter whose target EXISTS and simply matches nothing returns an empty result
// and no error. Without these rows, "make an unknown target fail" is satisfiable
// by failing on every narrow filter, which would break every legitimate query
// that happens to return nothing.
//
// Each *ID row names a real nib in the fixture that genuinely has no match for
// that relationship — a leaf has no descendants, a root has no ancestors, an
// only child has no siblings — so the empty answer is a fact rather than a
// rejection.
func TestApplyFilterEmptyMatchIsNotAnError(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name   string
		filter *model.NibFilter
	}{
		{"parentId of a childless nib", &model.NibFilter{ParentID: strPtr("t1")}},
		{"ancestorId of a leaf", &model.NibFilter{AncestorID: strPtr("t1")}},
		{"descendantId of a root", &model.NibFilter{DescendantID: strPtr("m1")}},
		{"siblingId of an only child", &model.NibFilter{SiblingID: strPtr("x1")}},
		{"blockingId of a nib nothing blocks", &model.NibFilter{BlockingID: strPtr("t1")}},
		{"blockedById of a nib that blocks nothing", &model.NibFilter{BlockedByID: strPtr("t1")}},
		{"mentionsId of an unmentioned nib", &model.NibFilter{MentionsID: strPtr("t1")}},
		{"mentionedById of a nib that mentions nothing", &model.NibFilter{MentionedByID: strPtr("t1")}},
		{"a scalar filter matching no nib", &model.NibFilter{Status: []string{"scrapped"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking)
			if err != nil {
				t.Fatalf("a genuine empty match must not be an error: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("got %d nibs, want an empty result", len(got))
			}
		})
	}
}

// vanishingReader resolves an id through NormalizeID and then refuses to Get
// it — the store state a concurrent delete produces between a filter branch
// resolving its target and the same branch fetching it. Overriding only Get
// keeps the rest of the stub's behavior, so the candidate nibs' own parent
// lookups still work and the failure under test is exactly the target fetch.
type vanishingReader struct {
	*stubReader
	vanished map[string]bool
}

func (r *vanishingReader) Get(id string) (*nib.Nib, error) {
	if r.vanished[id] {
		return nil, nib.ErrNotFound
	}
	return r.stubReader.Get(id)
}

// TestApplyFilterUnreadableTargetIsNotNotFound covers the third outcome: the
// target resolved, so the caller's id was right, and the fetch failed anyway.
//
// The distinction is the assertion. Reporting this as a not-found would tell an
// agent to fix an id that was never wrong, so the test pins that the error does
// NOT satisfy errors.Is(err, nib.ErrNotFound) even though its cause does — that
// is what routes it to the io/internal exit code instead of exit 3.
//
// Only the three branches that fetch their target defensively can reach this
// state; the other five never Get the target at all.
func TestApplyFilterUnreadableTargetIsNotNotFound(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		filter    *model.NibFilter
		wantField string
	}{
		{"descendantId", "nibs-t1", &model.NibFilter{DescendantID: strPtr("nibs-t1")}, "descendantId"},
		{"siblingId", "nibs-f1", &model.NibFilter{SiblingID: strPtr("nibs-f1")}, "siblingId"},
		{"blockingId", "nibs-t1", &model.NibFilter{BlockingID: strPtr("nibs-t1")}, "blockingId"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := hierarchyFixture()
			// The target is present for NormalizeID and absent for Get, which
			// is the whole point: the branch gets past its resolve step.
			reader := &vanishingReader{stubReader: base, vanished: map[string]bool{tt.target: true}}

			got, err := ApplyFilter(context.Background(), base.allNibs, tt.filter, reader, &stubBlockingChecker{})
			if err == nil {
				t.Fatalf("%s over a vanished target returned %d nibs and no error", tt.wantField, len(got))
			}
			if got != nil {
				t.Errorf("result = %v, want nil alongside the error", got)
			}
			var unreadable *FilterTargetUnreadableError
			if !errors.As(err, &unreadable) {
				t.Fatalf("error = %T (%v), want *FilterTargetUnreadableError", err, err)
			}
			if unreadable.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", unreadable.Field, tt.wantField)
			}
			if !errors.Is(unreadable.ReaderErr, nib.ErrNotFound) {
				t.Errorf("ReaderErr = %v, want the reader failure to be kept for diagnosis", unreadable.ReaderErr)
			}
			if errors.Is(err, nib.ErrNotFound) {
				t.Error("a target that vanished mid-filter must not classify as NOT_FOUND — that would report a concurrent delete as the caller's typo")
			}
			var notFound *FilterTargetNotFoundError
			if errors.As(err, &notFound) {
				t.Error("errors.As matched *FilterTargetNotFoundError, so the two classes are not distinguishable")
			}
		})
	}
}

// TestFilterErrorReachesEveryResolver pins that a refused filter reaches the
// CALLER of every resolver that accepts one, rather than being swallowed into an
// empty field.
//
// ApplyFilter refusing the query is only half of the fix: a resolver that
// discards the error hands gqlgen an empty list, which is precisely the
// indistinguishable answer the error exists to replace — and it does so on a
// nested field, where it is even harder to notice. Each row names one resolver,
// so a call site that stops propagating fails here by name.
//
// The blocking resolver appears twice on purpose: its status-released early
// return filters a nil slice through a SECOND ApplyFilter call, a separate call
// site from the one below it, and each needs its own propagation guard.
func TestFilterErrorReachesEveryResolver(t *testing.T) {
	resolver, core := setupTestResolver(t)
	subject := createTestNib(t, core, "subj", "Subject", "todo")
	released := createTestNib(t, core, "done", "Released", "completed")

	// Every row uses the same unknown parentId: which field carries the bad
	// target is ApplyFilter's business, already covered above. What is under
	// test here is only whether the resolver passes the refusal on.
	filter := &model.NibFilter{ParentID: strPtr("nonexistent")}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() ([]*nib.Nib, error)
	}{
		{"Nib.children", func() ([]*nib.Nib, error) {
			return resolver.Nib().Children(ctx, subject, filter, nil)
		}},
		{"Nib.blockedBy", func() ([]*nib.Nib, error) {
			return resolver.Nib().BlockedBy(ctx, subject, filter)
		}},
		{"Nib.blocking", func() ([]*nib.Nib, error) {
			return resolver.Nib().Blocking(ctx, subject, filter)
		}},
		{"Nib.blocking on a status-released nib", func() ([]*nib.Nib, error) {
			return resolver.Nib().Blocking(ctx, released, filter)
		}},
		{"Nib.mentions", func() ([]*nib.Nib, error) {
			return resolver.Nib().Mentions(ctx, subject, filter)
		}},
		{"Nib.mentionedBy", func() ([]*nib.Nib, error) {
			return resolver.Nib().MentionedBy(ctx, subject, filter)
		}},
		{"Query.nibs", func() ([]*nib.Nib, error) {
			return resolver.Query().Nibs(ctx, filter, nil)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.call()
			if err == nil {
				t.Fatalf("returned %d nibs and no error; the refusal was swallowed into an empty field", len(got))
			}
			if !errors.Is(err, nib.ErrNotFound) {
				t.Errorf("error does not carry nib.ErrNotFound: %v", err)
			}
		})
	}
}

// TestApplyFilterIDBranchesKnownAndUnknown verifies the unknown-target contract
// across the link-based single-ID filter branches. Each routes through
// resolveFilterID and fails on miss — the trap is a branch that passes a raw ID
// through and narrows to the empty set instead.
//
// The table pairs a negative case (unknown → error) with a positive control
// (known → non-nil with a specific ID in the result). Without the positive
// rows, a regression that failed unconditionally would pass the unknown-only
// suite silently.
func TestApplyFilterIDBranchesKnownAndUnknown(t *testing.T) {
	// Fixture: four nibs wired so every *ID filter has a non-trivial
	// positive case.
	//   - nibs-a: target of blocking queries (blocked_by: [nibs-b]) and
	//     source of an outbound mention to nibs-c
	//   - nibs-b: blocker of nibs-a; also blocks via blocked_by
	//   - nibs-c: mentioned by nibs-a (outbound set)
	//   - nibs-d: mentions nibs-a (inbound mentioner)
	nibA := &nib.Nib{ID: "nibs-a", Title: "A", BlockedBy: []string{"nibs-b"}}
	nibB := &nib.Nib{ID: "nibs-b", Title: "B", BlockedBy: []string{"nibs-a"}}
	nibC := &nib.Nib{ID: "nibs-c", Title: "C"}
	nibD := &nib.Nib{ID: "nibs-d", Title: "D"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-a": nibA, "nibs-b": nibB, "nibs-c": nibC, "nibs-d": nibD,
		},
		allNibs: []*nib.Nib{nibA, nibB, nibC, nibD},
		prefix:  "nibs-",
		// MentionsID filter: "nibs that mention target". Seed so nibs-d
		// shows up as an inbound mentioner of nibs-a.
		mentionsIn: map[string][]*nib.Nib{"nibs-a": {nibD}},
		// MentionedByID filter: "nibs the source mentions". Seed so nibs-c
		// shows up in nibs-a's outbound set.
		mentionsOut: map[string][]*nib.Nib{"nibs-a": {nibC}},
	}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name      string
		filter    *model.NibFilter
		wantError bool     // true → the target names no nib, so the filter fails
		wantIDs   []string // expected nib IDs in the result (when wantError=false)
	}{
		// BlockingID — "nibs blocking the target"; target's blocked_by lists them.
		{"BlockingID known — returns target's blockers", &model.NibFilter{BlockingID: strPtr("a")}, false, []string{"nibs-b"}},
		{"BlockingID unknown — fails", &model.NibFilter{BlockingID: strPtr("nonexistent")}, true, nil},

		// BlockedByID — "nibs whose blocked_by contains target".
		{"BlockedByID known — returns nibs blocked by target", &model.NibFilter{BlockedByID: strPtr("a")}, false, []string{"nibs-b"}},
		{"BlockedByID unknown — fails", &model.NibFilter{BlockedByID: strPtr("nonexistent")}, true, nil},

		// MentionsID — "nibs that mention the target in their body".
		{"MentionsID known — returns inbound mentioners", &model.NibFilter{MentionsID: strPtr("a")}, false, []string{"nibs-d"}},
		{"MentionsID unknown — fails", &model.NibFilter{MentionsID: strPtr("nonexistent")}, true, nil},

		// MentionedByID — "nibs mentioned in the source's body".
		{"MentionedByID known — returns source's outbound mentions", &model.NibFilter{MentionedByID: strPtr("a")}, false, []string{"nibs-c"}},
		{"MentionedByID unknown — fails", &model.NibFilter{MentionedByID: strPtr("nonexistent")}, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking)
			if tt.wantError {
				if err == nil {
					t.Errorf("got %d nibs and no error, want a not-found for the unknown target", len(got))
				} else if !errors.Is(err, nib.ErrNotFound) {
					t.Errorf("error does not carry nib.ErrNotFound: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatalf("got nil, want non-nil result with %v", tt.wantIDs)
			}
			gotIDs := make([]string, 0, len(got))
			for _, b := range got {
				gotIDs = append(gotIDs, b.ID)
			}
			sort.Strings(gotIDs)
			want := append([]string(nil), tt.wantIDs...)
			sort.Strings(want)
			if !reflect.DeepEqual(gotIDs, want) {
				t.Errorf("got IDs %v, want %v", gotIDs, want)
			}
		})
	}
}

// hierarchyFixture builds the tree the hierarchy-predicate tests share:
//
//	nibs-m1 ── nibs-e1 ─┬─ nibs-f1 ── nibs-t1
//	                    └─ nibs-t2
//	nibs-r2 ── nibs-x1
//	nibs-r3
//
// Three roots (m1, r2, r3) give siblingId a non-trivial root-level case, and
// the m1→e1→f1→t1 chain is deep enough that a one-level-only implementation of
// ancestorId/descendantId fails instead of accidentally passing.
//
// nibs-e1 is the one completed nib: it sits mid-chain, so a status filter can
// remove it from the candidate slice while leaving its subtree in place. That
// is what the "resolves ancestry through the store" row needs.
func hierarchyFixture() *stubReader {
	nibs := []*nib.Nib{
		{ID: "nibs-m1", Title: "Milestone", Status: "todo"},
		{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1", Status: "completed"},
		{ID: "nibs-f1", Title: "Feature", Parent: "nibs-e1", Status: "todo"},
		{ID: "nibs-t1", Title: "Task", Parent: "nibs-f1", Status: "todo"},
		{ID: "nibs-t2", Title: "Second child of the epic", Parent: "nibs-e1", Status: "todo"},
		{ID: "nibs-r2", Title: "Second root", Status: "todo"},
		{ID: "nibs-x1", Title: "Only child of the second root", Parent: "nibs-r2", Status: "todo"},
		{ID: "nibs-r3", Title: "Third root", Status: "todo"},
	}
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	return &stubReader{nibs: byID, allNibs: nibs, prefix: "nibs-"}
}

// TestParentChain pins what parentChain banks for each shape of parent link.
// It is the unit the three hierarchy predicates are built on. The dangling and
// cyclic shapes reach a loaded store only by hand-editing a nib file, and an
// unresolvable filter target short-circuits before any walk starts. The raw
// short-form shape needs no hand-editing: canonicalization leaves a link that
// already resolves exactly alone, so a bare-token nib sitting alongside its
// prefixed twin keeps one, and deleting the bare token promotes that link to
// the twin without rewriting the stored spelling.
//
// The load-bearing rule: every id in the chain is a RESOLVED id, so the chain
// only ever names nibs that exist. A link that resolves under a different
// spelling is banked under the resolved one; a link that resolves to nothing
// contributes nothing at all.
func TestParentChain(t *testing.T) {
	// f1's stored link is the short form "e1". Core.Get resolves it by
	// prepending the prefix, so the walk continues through it — the chain must
	// record "nibs-e1", the spelling every filter target is normalized to.
	shortLinker := &nib.Nib{ID: "nibs-f1", Title: "Short-form parent link", Parent: "e1"}
	orphan := &nib.Nib{ID: "nibs-orphan", Title: "Dangling parent link", Parent: "nibs-ghost"}
	selfParent := &nib.Nib{ID: "nibs-self", Title: "Self-parented", Parent: "nibs-self"}
	nibs := []*nib.Nib{
		{ID: "nibs-m1", Title: "Milestone"},
		{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"},
		shortLinker,
		{ID: "nibs-t1", Title: "Task", Parent: "nibs-f1"},
		orphan,
		{ID: "nibs-d1", Title: "Below a dangling link", Parent: "nibs-orphan"},
		selfParent,
		{ID: "nibs-c1", Title: "C1", Parent: "nibs-c2"},
		{ID: "nibs-c2", Title: "C2", Parent: "nibs-c1"},
	}
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: nibs, prefix: "nibs-"}

	tests := []struct {
		name  string
		nibID string
		want  []string
	}{
		{"root has no chain", "nibs-m1", nil},
		{"walks to the root, nearest ancestor first", "nibs-e1", []string{"nibs-m1"}},
		{"records the resolved id for a short-form link, not the stored spelling",
			"nibs-f1", []string{"nibs-e1", "nibs-m1"}},
		{"a short-form rung stays resolved for chains passing through it",
			"nibs-t1", []string{"nibs-f1", "nibs-e1", "nibs-m1"}},
		{"a dangling link contributes nothing", "nibs-orphan", nil},
		// The rung below the dangling link is still reported: the walk ends AT
		// the unresolvable link rather than failing, so everything reached
		// before it stands.
		{"a dangling link mid-chain ends the chain there", "nibs-d1", []string{"nibs-orphan"}},
		{"a self-parented nib yields an empty chain (the seed excludes it)", "nibs-self", nil},
		{"a cycle terminates and never contains self", "nibs-c1", []string{"nibs-c2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := reader.Get(tt.nibID)
			if err != nil {
				t.Fatalf("fixture nib %q missing: %v", tt.nibID, err)
			}
			got := parentChain(b, reader)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parentChain(%s) = %v, want %v", tt.nibID, got, tt.want)
			}
		})
	}
}

// TestApplyFilterHierarchyShortFormParentLink is the end-to-end consequence of
// parentChain banking resolved ids. A hand-edited `parent: e1` is followed by
// Core.Get, so the tree is intact as far as the walk is concerned; if the raw
// spelling were banked instead, the chain would name a nib that does not
// exist and the predicates would report a hierarchy with a missing rung.
func TestApplyFilterHierarchyShortFormParentLink(t *testing.T) {
	m1 := &nib.Nib{ID: "nibs-m1", Title: "Milestone"}
	e1 := &nib.Nib{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"}
	// Hand-edited short form; validateAndSetParent would have stored "nibs-e1".
	f1 := &nib.Nib{ID: "nibs-f1", Title: "Feature", Parent: "e1"}
	t1 := &nib.Nib{ID: "nibs-t1", Title: "Task", Parent: "nibs-f1"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-m1": m1, "nibs-e1": e1, "nibs-f1": f1, "nibs-t1": t1,
		},
		allNibs: []*nib.Nib{m1, e1, f1, t1},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	t.Run("AncestorID matches through a short-form rung", func(t *testing.T) {
		got := applyFilterOK(t, context.Background(), reader.allNibs, &model.NibFilter{AncestorID: strPtr("e1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-f1", "nibs-t1"})
	})

	t.Run("DescendantID reports the whole chain, not one with a hole in it", func(t *testing.T) {
		got := applyFilterOK(t, context.Background(), reader.allNibs, &model.NibFilter{DescendantID: strPtr("t1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-f1", "nibs-e1", "nibs-m1"})
	})
}

// TestApplyFilterHasParentResolvesParentLink pins that hasParent decides
// parent-ness the same way the rest of the surface does — through the resolved
// parent, not the raw stored string. A dangling link is the shape that
// separates the two: nibResolver.Parent collapses it to nil and fetchSiblings
// puts the nib in the root set, so a raw emptiness check here would report a
// parent that nothing else in the graph can show.
func TestApplyFilterHasParentResolvesParentLink(t *testing.T) {
	root := &nib.Nib{ID: "nibs-root", Title: "Root"}
	par := &nib.Nib{ID: "nibs-par", Title: "Parent"}
	child := &nib.Nib{ID: "nibs-chi", Title: "Child", Parent: "nibs-par"}
	// Short form resolves, so this nib genuinely has a parent.
	shortChild := &nib.Nib{ID: "nibs-sho", Title: "Short-form parent link", Parent: "par"}
	// Names no nib under either spelling, so it has no parent to resolve to.
	dangling := &nib.Nib{ID: "nibs-dng", Title: "Dangling parent link", Parent: "nibs-ghost"}

	all := []*nib.Nib{root, par, child, shortChild, dangling}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		want    bool
		wantIDs []string
	}{
		{"true keeps only the nibs whose parent link resolves", true,
			[]string{"nibs-chi", "nibs-sho"}},
		{"false keeps the parentless and the dangling alike", false,
			[]string{"nibs-root", "nibs-par", "nibs-dng"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &model.NibFilter{HasParent: &tt.want}
			assertNibIDs(t, applyFilterOK(t, context.Background(), reader.allNibs, filter, reader, blocking), tt.wantIDs)
		})
	}
}

// TestParentIDFilterAndFieldResolveShortFormLinks pins that the parentId FILTER
// and the parentId FIELD both read the resolved parent rather than the raw
// stored string, on the shape that separates the two readings: a link stored in
// short form that resolves to a prefixed nib.
//
// It uses a stub reader deliberately. Every mutation that can change the store's
// key set runs the canonicalization sweep — Load, Core.Create, Core.Delete and
// the watcher's batch — so a real store never holds an unresolved short-form
// link, and a test over one would pass whichever reading the code took. What is
// pinned here is the filter and the field agreeing WITHOUT that pass, which is
// the property that makes them independent of it.
//
// The dangling half of the same rule is reachable over a real Load and lives in
// parent_resolution_test.go.
func TestParentIDFilterAndFieldResolveShortFormLinks(t *testing.T) {
	par := &nib.Nib{ID: "nibs-par", Title: "Parent"}
	full := &nib.Nib{ID: "nibs-ful", Title: "Full-form parent link", Parent: "nibs-par"}
	// Short form resolves through the prefix, so this nib's parent IS nibs-par.
	short := &nib.Nib{ID: "nibs-sho", Title: "Short-form parent link", Parent: "par"}
	dangling := &nib.Nib{ID: "nibs-dng", Title: "Dangling parent link", Parent: "nibs-ghost"}
	other := &nib.Nib{ID: "nibs-oth", Title: "Unrelated root"}

	all := []*nib.Nib{par, full, short, dangling, other}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}
	blocking := &stubBlockingChecker{}
	ctx := context.Background()
	resolver := &Resolver{Reader: reader, Blocking: blocking}

	// Both spellings of the filter argument, since a short --parent and a full one
	// must reach the same answer.
	for _, arg := range []string{"nibs-par", "par"} {
		t.Run("the filter parentId="+arg+" matches both stored spellings", func(t *testing.T) {
			got := applyFilterOK(t, ctx, all, &model.NibFilter{ParentID: strPtr(arg)}, reader, blocking)
			assertNibIDs(t, got, []string{"nibs-ful", "nibs-sho"})
		})
	}

	t.Run("the field reports the resolved id for both stored spellings", func(t *testing.T) {
		for _, b := range []*nib.Nib{full, short} {
			got, err := resolver.Nib().ParentID(ctx, b)
			if err != nil {
				t.Fatalf("ParentID(%s): %v", b.ID, err)
			}
			if got == nil || *got != "nibs-par" {
				t.Errorf("ParentID(%s) = %v, want %q", b.ID, got, "nibs-par")
			}
		}
	})

	t.Run("storedParentId keeps the two spellings distinguishable", func(t *testing.T) {
		want := map[string]string{"nibs-ful": "nibs-par", "nibs-sho": "par", "nibs-dng": "nibs-ghost"}
		for id, wantStored := range want {
			got, err := resolver.Nib().StoredParentID(ctx, byID[id])
			if err != nil {
				t.Fatalf("StoredParentID(%s): %v", id, err)
			}
			if got == nil || *got != wantStored {
				t.Errorf("StoredParentID(%s) = %v, want %q", id, got, wantStored)
			}
		}
	})
}

// TestApplyFilterSiblingIDResolvesParentLinks pins that siblingId decides
// "same parent" through the resolved parent, the way nibResolver.Parent and
// fetchSiblings do, rather than by comparing the raw stored strings. Two stored
// shapes make the two disagree:
//
//   - a dangling link presents as a root everywhere the object graph is
//     walked, so it belongs in a root-level sibling set;
//   - a short-form link names the same parent as its full-form spelling, so
//     the two spellings are siblings.
//
// Both are injected straight into the reader here, which pins the filter's own
// behavior rather than the loader's — the filter has to be right on either
// shape regardless of how it arrived. Canonicalization rewrites a short-form
// link whose target it can resolve, so that spelling is rarer through a real
// Load than a dangling one, which survives verbatim; it is not impossible
// there, since a link that already resolves exactly is left alone (see
// resolvedParent). TestDanglingParentClassifiedAlikeAcrossSurfaces covers the
// dangling shape through a real Load.
func TestApplyFilterSiblingIDResolvesParentLinks(t *testing.T) {
	m1 := &nib.Nib{ID: "nibs-m1", Title: "Root"}
	r2 := &nib.Nib{ID: "nibs-r2", Title: "Second root"}
	orphan := &nib.Nib{ID: "nibs-orphan", Title: "Dangling parent link", Parent: "nibs-ghost"}
	e1 := &nib.Nib{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"}
	// f1 and t2 are siblings: f1 spells the shared parent short, t2 spells it full.
	f1 := &nib.Nib{ID: "nibs-f1", Title: "Short-form parent link", Parent: "e1"}
	t2 := &nib.Nib{ID: "nibs-t2", Title: "Full-form parent link", Parent: "nibs-e1"}

	all := []*nib.Nib{m1, r2, orphan, e1, f1, t2}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"a root's siblings include a nib whose parent link is dangling",
			&model.NibFilter{SiblingID: strPtr("m1")}, []string{"nibs-r2", "nibs-orphan"}},
		{"a dangling-parent nib is itself treated as a root",
			&model.NibFilter{SiblingID: strPtr("orphan")}, []string{"nibs-m1", "nibs-r2"}},
		{"a full-form spelling finds the short-form sibling",
			&model.NibFilter{SiblingID: strPtr("t2")}, []string{"nibs-f1"}},
		{"a short-form spelling finds the full-form sibling",
			&model.NibFilter{SiblingID: strPtr("f1")}, []string{"nibs-t2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNibIDs(t, applyFilterOK(t, context.Background(), reader.allNibs, tt.filter, reader, blocking), tt.wantIDs)
		})
	}
}

// TestApplyFilterHierarchyPredicates covers the ancestorId / descendantId /
// siblingId branches over a multi-level tree.
//
// Direction is the thing to get right and the easy thing to invert: like every
// other *ID filter, each field names the relationship the MATCHED nib holds
// toward the supplied target. ancestorId: X therefore keeps the nibs whose
// ancestor is X (X's descendants), and descendantId: X keeps the nibs whose
// descendant is X (X's ancestors). The table pins both directions against the
// same fixture, so swapping the two branches fails rather than reshuffles.
//
// Most filter arguments are short IDs: the branches must normalize through
// resolveFilterID, since the stored Parent links are full prefixed IDs. One
// row per field passes the already-full form, which must resolve to the same
// answer rather than being double-prefixed into a miss.
//
// Unknown targets are not in this table: they are not a matching outcome at
// all, and are covered as errors by TestApplyFilterUnknownTargetIsNotFound. The
// "matches nothing" rows here all name real nibs, so each empty answer is a fact
// about the tree rather than a rejected question.
func TestApplyFilterHierarchyPredicates(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string // expected nib IDs (empty → matched nothing)
	}{
		// ancestorId — "nibs that have the target among their ancestors".
		{"AncestorID keeps descendants at every depth, target excluded",
			&model.NibFilter{AncestorID: strPtr("e1")},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
		{"AncestorID on the root keeps the whole subtree, root excluded",
			&model.NibFilter{AncestorID: strPtr("m1")},
			[]string{"nibs-e1", "nibs-f1", "nibs-t1", "nibs-t2"}},
		{"AncestorID accepts the already-full form",
			&model.NibFilter{AncestorID: strPtr("nibs-e1")},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
		{"AncestorID on a leaf matches nothing",
			&model.NibFilter{AncestorID: strPtr("t1")}, nil},

		// descendantId — "nibs that have the target among their descendants",
		// i.e. exactly the target's ancestor chain.
		{"DescendantID keeps the whole ancestor chain, target excluded",
			&model.NibFilter{DescendantID: strPtr("t1")},
			[]string{"nibs-f1", "nibs-e1", "nibs-m1"}},
		{"DescendantID on a mid-level nib keeps only what is above it",
			&model.NibFilter{DescendantID: strPtr("e1")},
			[]string{"nibs-m1"}},
		{"DescendantID accepts the already-full form",
			&model.NibFilter{DescendantID: strPtr("nibs-t1")},
			[]string{"nibs-f1", "nibs-e1", "nibs-m1"}},
		{"DescendantID on a root matches nothing",
			&model.NibFilter{DescendantID: strPtr("m1")}, nil},

		// siblingId — "nibs sharing the target's parent", root-level target
		// included (matches fetchSiblings in cmd/rel.go).
		{"SiblingID keeps the other children of the same parent, target excluded",
			&model.NibFilter{SiblingID: strPtr("f1")},
			[]string{"nibs-t2"}},
		{"SiblingID on a root nib keeps the other roots, target excluded",
			&model.NibFilter{SiblingID: strPtr("m1")},
			[]string{"nibs-r2", "nibs-r3"}},
		{"SiblingID accepts the already-full form",
			&model.NibFilter{SiblingID: strPtr("nibs-f1")},
			[]string{"nibs-t2"}},
		{"SiblingID on an only child matches nothing",
			&model.NibFilter{SiblingID: strPtr("x1")}, nil},

		// Two hierarchy predicates AND-composed: each is a pure per-element
		// predicate, so the result is the intersection regardless of the order
		// ApplyFilter runs the branches in. m1's subtree is {e1,f1,t1,t2} and
		// t1's ancestor chain is {f1,e1,m1}.
		{"AncestorID and DescendantID compose as an intersection",
			&model.NibFilter{AncestorID: strPtr("m1"), DescendantID: strPtr("t1")},
			[]string{"nibs-e1", "nibs-f1"}},

		// Ancestry comes from the store, not from the candidate slice: the
		// status filter runs first and drops the completed nibs-e1, but its
		// subtree must still resolve through it up to m1. This row is what
		// fails if a future optimization indexes the candidate slice instead
		// of walking the reader.
		{"AncestorID resolves ancestry through the store, not the candidate slice",
			&model.NibFilter{AncestorID: strPtr("m1"), Status: []string{"todo"}},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNibIDs(t, applyFilterOK(t, context.Background(), reader.allNibs, tt.filter, reader, blocking), tt.wantIDs)
		})
	}
}

// TestApplyFilterHierarchyPredicatesCycleSafe pins that the parent-chain walks
// behind ancestorId and descendantId terminate on a parent cycle, and that the
// target stays out of its own result even when a cycle makes it structurally
// reachable from itself. The mutation resolvers reject cycles, but a
// hand-edited nib file can still create one, and an unguarded
// `for parent != ""` walk hangs every query that uses these filters.
func TestApplyFilterHierarchyPredicatesCycleSafe(t *testing.T) {
	c1 := &nib.Nib{ID: "nibs-c1", Title: "C1", Parent: "nibs-c2"}
	c2 := &nib.Nib{ID: "nibs-c2", Title: "C2", Parent: "nibs-c1"}
	outside := &nib.Nib{ID: "nibs-out", Title: "Outside the cycle"}

	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"nibs-c1": c1, "nibs-c2": c2, "nibs-out": outside},
		allNibs: []*nib.Nib{c1, c2, outside},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	t.Run("AncestorID terminates and excludes the target itself", func(t *testing.T) {
		got := applyFilterOK(t, context.Background(), reader.allNibs, &model.NibFilter{AncestorID: strPtr("c1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-c2"})
	})

	t.Run("DescendantID terminates and excludes the target itself", func(t *testing.T) {
		got := applyFilterOK(t, context.Background(), reader.allNibs, &model.NibFilter{DescendantID: strPtr("c1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-c2"})
	})
}

// TestQueryNibsSearchReAddsHierarchyTargets pins what queryResolver.Nibs
// actually returns when a hierarchy predicate is combined with search, which
// is not what ApplyFilter alone returns. The search branch runs
// includeAncestors afterwards so the client can render a complete tree, and
// that step re-adds ancestors of the survivors:
//
//   - ancestorId: the target comes back, contradicting "itself excluded" taken
//     as an absolute promise. The schema description says so.
//   - siblingId: the target stays out (it is nobody's ancestor), but the
//     shared parent arrives.
//   - descendantId: unaffected — every ancestor added is already on the
//     target's ancestor chain, so it satisfies the predicate anyway.
//
// This is deliberate, not a defect to fix in ApplyFilter: the web UI's tree
// rendering and ancestor dimming depend on the completion. The test exists so
// that changing it is a conscious decision with a visible cost.
func TestQueryNibsSearchReAddsHierarchyTargets(t *testing.T) {
	reader := hierarchyFixture()
	// Search is a wide net here so the hierarchy predicate, not the search
	// term, is what narrows the result; includeAncestors then widens it again.
	reader.searchOut = map[string][]*nib.Nib{"anything": reader.allNibs}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    &stubWriter{store: reader},
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, &stubWriter{store: reader}),
	}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"ancestorId: the excluded target is added back as an ancestor",
			&model.NibFilter{Search: strPtr("anything"), AncestorID: strPtr("e1")},
			// ApplyFilter alone returns f1, t1, t2; completion adds e1 and m1.
			[]string{"nibs-f1", "nibs-t1", "nibs-t2", "nibs-e1", "nibs-m1"}},
		{"siblingId: the target stays out but the shared parent arrives",
			&model.NibFilter{Search: strPtr("anything"), SiblingID: strPtr("f1")},
			// ApplyFilter alone returns t2; completion adds e1 and m1.
			[]string{"nibs-t2", "nibs-e1", "nibs-m1"}},
		{"descendantId: completion adds nothing the predicate did not already keep",
			&model.NibFilter{Search: strPtr("anything"), DescendantID: strPtr("t1")},
			[]string{"nibs-f1", "nibs-e1", "nibs-m1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Query().Nibs(context.Background(), tt.filter, nil)
			if err != nil {
				t.Fatalf("Nibs: %v", err)
			}
			assertNibIDs(t, got, tt.wantIDs)
		})
	}
}

// assertNibIDs compares the IDs of got against want, order-insensitively.
// A nil result is treated as the empty set — ApplyFilter builds its results
// with append, so "matched nothing" and "short-circuited" both surface as nil;
// callers that care about the difference assert on nil-ness themselves.
func assertNibIDs(t *testing.T, got []*nib.Nib, want []string) {
	t.Helper()
	gotIDs := make([]string, 0, len(got))
	for _, b := range got {
		gotIDs = append(gotIDs, b.ID)
	}
	wantIDs := append([]string(nil), want...)
	sort.Strings(gotIDs)
	sort.Strings(wantIDs)
	if len(gotIDs) == 0 && len(wantIDs) == 0 {
		return
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("got IDs %v, want %v", gotIDs, wantIDs)
	}
}

// assertNibIDsInOrder compares the IDs of got against want as a SEQUENCE —
// neither side is sorted — for the assertions whose subject is the order itself.
//
// Two contracts need it and cannot be expressed with assertNibIDs. On a
// relationship field the schema promises the term selects rather than ranks, so
// the field keeps its own order; at the top level queryResolver.Nibs promises
// the search seeding's order (id matches first, then relevance) survives the
// pipeline. Both are membership-identical to the orders they must NOT produce,
// so a set comparison cannot see either one break.
func assertNibIDsInOrder(t *testing.T, got []*nib.Nib, want []string) {
	t.Helper()
	gotIDs := make([]string, 0, len(got))
	for _, b := range got {
		gotIDs = append(gotIDs, b.ID)
	}
	if len(gotIDs) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Errorf("got IDs %v, want %v (in this order)", gotIDs, want)
	}
}

func TestFilterByPredicate(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Parent: "p1"},
		{ID: "b", Parent: ""},
		{ID: "c", Parent: "p2"},
	}

	hasParent := func(b *nib.Nib) bool { return b.Parent != "" }
	bTrue := true
	bFalse := false

	tests := []struct {
		name    string
		apply   *bool
		wantLen int
		wantIDs []string
	}{
		{"nil is no-op", nil, 3, []string{"a", "b", "c"}},
		{"true keeps matching", &bTrue, 2, []string{"a", "c"}},
		{"false keeps non-matching", &bFalse, 1, []string{"b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByPredicate(nibs, tt.apply, hasParent)
			if len(got) != tt.wantLen {
				t.Fatalf("got %d nibs, want %d", len(got), tt.wantLen)
			}
			for i, id := range tt.wantIDs {
				if got[i].ID != id {
					t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestFilterBySliceField(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Tags: []string{"frontend", "urgent"}},
		{ID: "b", Tags: []string{"backend"}},
		{ID: "c", Tags: []string{"frontend", "backend"}},
		{ID: "d", Tags: nil},
	}

	getTags := func(b *nib.Nib) []string { return b.Tags }

	t.Run("include matches any", func(t *testing.T) {
		got := filterBySliceField(nibs, []string{"frontend"}, getTags)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "a" || got[1].ID != "c" {
			t.Errorf("got IDs %s, %s, want a, c", got[0].ID, got[1].ID)
		}
	})

	t.Run("include with multiple values OR", func(t *testing.T) {
		got := filterBySliceField(nibs, []string{"urgent", "backend"}, getTags)
		if len(got) != 3 {
			t.Fatalf("got %d nibs, want 3", len(got))
		}
		wantIDs := []string{"a", "b", "c"}
		for i, id := range wantIDs {
			if got[i].ID != id {
				t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, id)
			}
		}
	})

	t.Run("nil values is no-op", func(t *testing.T) {
		got := filterBySliceField(nibs, nil, getTags)
		if len(got) != len(nibs) {
			t.Errorf("got %d nibs, want %d (no-op)", len(got), len(nibs))
		}
	})
}

func TestExcludeBySliceField(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Tags: []string{"frontend", "urgent"}},
		{ID: "b", Tags: []string{"backend"}},
		{ID: "c", Tags: nil},
	}

	getTags := func(b *nib.Nib) []string { return b.Tags }

	t.Run("excludes matching", func(t *testing.T) {
		got := excludeBySliceField(nibs, []string{"frontend"}, getTags)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "b" || got[1].ID != "c" {
			t.Errorf("got IDs %s, %s, want b, c", got[0].ID, got[1].ID)
		}
	})

	t.Run("nil values is no-op", func(t *testing.T) {
		got := excludeBySliceField(nibs, nil, getTags)
		if len(got) != len(nibs) {
			t.Errorf("got %d nibs, want %d (no-op)", len(got), len(nibs))
		}
	})
}

func TestFilterByEstimate(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Estimate: "s"},
		{ID: "b", Estimate: "m"},
		{ID: "c", Estimate: "l"},
		{ID: "d", Estimate: ""},
	}

	getEstimate := func(b *nib.Nib) string { return b.Estimate }

	t.Run("include by estimate", func(t *testing.T) {
		got := filterByField(nibs, []string{"s", "l"}, getEstimate)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "a" || got[1].ID != "c" {
			t.Errorf("got IDs %s, %s, want a, c", got[0].ID, got[1].ID)
		}
	})

	t.Run("exclude by estimate", func(t *testing.T) {
		got := excludeByField(nibs, []string{"m"}, getEstimate)
		if len(got) != 3 {
			t.Fatalf("got %d nibs, want 3", len(got))
		}
		wantIDs := []string{"a", "c", "d"}
		for i, id := range wantIDs {
			if got[i].ID != id {
				t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, id)
			}
		}
	})

	t.Run("include empty estimate", func(t *testing.T) {
		got := filterByField(nibs, []string{""}, getEstimate)
		if len(got) != 1 || got[0].ID != "d" {
			t.Errorf("got %v, want [d]", got)
		}
	})
}

// TestApplyFilterDefaultAwarePriorityAndType is the direct coverage for
// ApplyFilter's default-aware Type/Priority filtering (the EffectiveType()/
// EffectivePriority() routing). A default-omitting nib (empty Priority/Type) must filter
// as though the "normal"/"task" presentation defaults were on disk: including it
// under Priority=["normal"] / Type=["task"], and excluding it under the symmetric
// ExcludePriority / ExcludeType. Each exclude row keeps a non-default control nib
// so a regression that dropped everything would not pass silently.
func TestApplyFilterDefaultAwarePriorityAndType(t *testing.T) {
	// defaulted omits both priority: and type: (empty fields); explicit carries
	// non-default values so each case has a surviving control.
	defaulted := &nib.Nib{ID: "nibs-defaulted", Title: "Defaulted"}
	explicit := &nib.Nib{ID: "nibs-explicit", Title: "Explicit", Priority: "high", Type: "bug"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-defaulted": defaulted,
			"nibs-explicit":  explicit,
		},
		allNibs: []*nib.Nib{defaulted, explicit},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"Priority normal includes default-omitting nib", &model.NibFilter{Priority: []string{"normal"}}, []string{"nibs-defaulted"}},
		{"ExcludePriority normal excludes default-omitting nib", &model.NibFilter{ExcludePriority: []string{"normal"}}, []string{"nibs-explicit"}},
		{"Type task includes default-omitting nib", &model.NibFilter{Type: []string{"task"}}, []string{"nibs-defaulted"}},
		{"ExcludeType task excludes default-omitting nib", &model.NibFilter{ExcludeType: []string{"task"}}, []string{"nibs-explicit"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyFilterOK(t, context.Background(), reader.allNibs, tt.filter, reader, blocking)
			gotIDs := make([]string, 0, len(got))
			for _, b := range got {
				gotIDs = append(gotIDs, b.ID)
			}
			sort.Strings(gotIDs)
			want := append([]string(nil), tt.wantIDs...)
			sort.Strings(want)
			if !reflect.DeepEqual(gotIDs, want) {
				t.Errorf("got IDs %v, want %v", gotIDs, want)
			}
		})
	}
}

// TestApplyFilterPresenceTriState pins HasParent and HasBlocking as tri-state
// fields: nil filters nothing, &true keeps the nibs that have the thing, and
// &false keeps exactly the complement. The &false rows are what let the CLI's
// --no-parent/--no-blocking spellings route through a single field, so they are
// the model contract those flags stand on.
func TestApplyFilterPresenceTriState(t *testing.T) {
	root := &nib.Nib{ID: "nibs-root", Title: "Root"}
	// The child also carries a blocked_by entry, so the HasBlockedBy rows below
	// have something to discriminate on. It is consistent with the blocking stub:
	// root blocks the child, the child is blocked by root.
	child := &nib.Nib{ID: "nibs-child", Title: "Child", Parent: "nibs-root", BlockedBy: []string{"nibs-root"}}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-root":  root,
			"nibs-child": child,
		},
		allNibs: []*nib.Nib{root, child},
		prefix:  "nibs-",
	}
	// Only the root blocks anything, so each blocking answer is a singleton.
	blocking := &stubBlockingChecker{blocking: map[string]bool{"nibs-root": true}}

	yes, no := true, false
	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"HasParent nil filters nothing", &model.NibFilter{}, []string{"nibs-root", "nibs-child"}},
		{"HasParent true keeps parented nibs", &model.NibFilter{HasParent: &yes}, []string{"nibs-child"}},
		{"HasParent false keeps exactly the parentless nibs", &model.NibFilter{HasParent: &no}, []string{"nibs-root"}},
		{"HasBlocking true keeps blocking nibs", &model.NibFilter{HasBlocking: &yes}, []string{"nibs-root"}},
		{"HasBlocking false keeps exactly the non-blocking nibs", &model.NibFilter{HasBlocking: &no}, []string{"nibs-child"}},
		// HasBlockedBy had a NoBlockedBy twin until the pair was collapsed. These
		// two rows are what stop the survivor regressing to an include-only
		// filter, which is how the twin came to exist in the first place.
		{"HasBlockedBy true keeps nibs with blocked_by entries", &model.NibFilter{HasBlockedBy: &yes}, []string{"nibs-child"}},
		{"HasBlockedBy false keeps exactly those with none", &model.NibFilter{HasBlockedBy: &no}, []string{"nibs-root"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyFilterOK(t, context.Background(), reader.allNibs, tt.filter, reader, blocking)
			gotIDs := make([]string, 0, len(got))
			for _, b := range got {
				gotIDs = append(gotIDs, b.ID)
			}
			sort.Strings(gotIDs)
			want := append([]string(nil), tt.wantIDs...)
			sort.Strings(want)
			if !reflect.DeepEqual(gotIDs, want) {
				t.Errorf("got IDs %v, want %v", gotIDs, want)
			}
		})
	}
}

func TestIncludeAncestors(t *testing.T) {
	// Build a hierarchy: milestone -> epic -> task
	milestone := &nib.Nib{ID: "m1", Title: "Release", Type: "milestone"}
	epic := &nib.Nib{ID: "e1", Title: "Auth", Type: "epic", Parent: "m1"}
	task := &nib.Nib{ID: "t1", Title: "Login page", Type: "task", Parent: "e1"}
	unrelated := &nib.Nib{ID: "u1", Title: "Unrelated", Type: "task"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"m1": milestone,
			"e1": epic,
			"t1": task,
			"u1": unrelated,
		},
	}

	t.Run("adds missing ancestors", func(t *testing.T) {
		input := []*nib.Nib{task}
		got := includeAncestors(input, reader)

		ids := make([]string, len(got))
		for i, b := range got {
			ids[i] = b.ID
		}
		sort.Strings(ids)

		want := []string{"e1", "m1", "t1"}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
		for i, id := range ids {
			if id != want[i] {
				t.Errorf("ids[%d] = %q, want %q", i, id, want[i])
			}
		}
	})

	t.Run("does not duplicate already-present ancestors", func(t *testing.T) {
		input := []*nib.Nib{epic, task}
		got := includeAncestors(input, reader)

		ids := make([]string, len(got))
		for i, b := range got {
			ids[i] = b.ID
		}
		sort.Strings(ids)

		want := []string{"e1", "m1", "t1"}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
	})

	t.Run("no-op when all ancestors present", func(t *testing.T) {
		input := []*nib.Nib{milestone, epic, task}
		got := includeAncestors(input, reader)
		if len(got) != 3 {
			t.Errorf("got %d nibs, want 3 (no extras)", len(got))
		}
	})

	t.Run("no-op for root nibs", func(t *testing.T) {
		input := []*nib.Nib{unrelated}
		got := includeAncestors(input, reader)
		if len(got) != 1 {
			t.Errorf("got %d nibs, want 1", len(got))
		}
	})
}

// TestIncludeAncestorsChainShapes pins how ancestor completion walks the
// awkward link shapes — the same set TestParentChain pins for the filter walk —
// plus the property unique to this site: its visited set spans the WHOLE batch,
// so ancestry banked while completing one nib is neither re-walked nor re-added
// while completing the next. assertNibIDs compares sorted id lists, so a
// re-added ancestor fails as a length mismatch.
//
// As in TestParentChain, the dangling and cyclic shapes reach a loaded store
// only by hand-editing a file, while the raw short-form one survives
// canonicalization on its own — see there for the store state that keeps it.
func TestIncludeAncestorsChainShapes(t *testing.T) {
	m1 := &nib.Nib{ID: "nibs-m1", Title: "Milestone"}
	e1 := &nib.Nib{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"}
	// Two children of the same epic: completing the first banks e1 and m1, and
	// completing the second must find them already banked.
	t1 := &nib.Nib{ID: "nibs-t1", Title: "Task 1", Parent: "nibs-e1"}
	t2 := &nib.Nib{ID: "nibs-t2", Title: "Task 2", Parent: "nibs-e1"}
	// f1 reaches that same epic through the short form `parent: e1`, so the
	// batch only stays deduplicated if the walk banks the RESOLVED id.
	f1 := &nib.Nib{ID: "nibs-f1", Title: "Short-form parent link", Parent: "e1"}
	orphan := &nib.Nib{ID: "nibs-orphan", Title: "Dangling parent link", Parent: "nibs-ghost"}
	d1 := &nib.Nib{ID: "nibs-d1", Title: "Below a dangling link", Parent: "nibs-orphan"}
	selfParent := &nib.Nib{ID: "nibs-self", Title: "Self-parented", Parent: "nibs-self"}
	c1 := &nib.Nib{ID: "nibs-c1", Title: "C1", Parent: "nibs-c2"}
	c2 := &nib.Nib{ID: "nibs-c2", Title: "C2", Parent: "nibs-c1"}

	all := []*nib.Nib{m1, e1, t1, t2, f1, orphan, d1, selfParent, c1, c2}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}

	tests := []struct {
		name  string
		input []*nib.Nib
		want  []string
	}{
		{"adds the whole chain of a single leaf",
			[]*nib.Nib{t1}, []string{"nibs-t1", "nibs-e1", "nibs-m1"}},
		{"a chain shared by two nibs is added once for the batch",
			[]*nib.Nib{t1, t2}, []string{"nibs-t1", "nibs-t2", "nibs-e1", "nibs-m1"}},
		{"a short-form link does not re-add an ancestor banked under its resolved id",
			[]*nib.Nib{t1, f1}, []string{"nibs-t1", "nibs-f1", "nibs-e1", "nibs-m1"}},
		{"a short-form link banks the ancestor a later full-form link then finds",
			[]*nib.Nib{f1, t1}, []string{"nibs-f1", "nibs-t1", "nibs-e1", "nibs-m1"}},
		{"a dangling link adds nothing", []*nib.Nib{orphan}, []string{"nibs-orphan"}},
		{"a dangling link mid-chain ends the chain there",
			[]*nib.Nib{d1}, []string{"nibs-d1", "nibs-orphan"}},
		{"a self-parented nib adds nothing", []*nib.Nib{selfParent}, []string{"nibs-self"}},
		{"a cycle terminates and adds only the other rung",
			[]*nib.Nib{c1}, []string{"nibs-c1", "nibs-c2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy the input so the append inside includeAncestors can never
			// write through into the shared fixture slice.
			got := includeAncestors(append([]*nib.Nib(nil), tt.input...), reader)
			assertNibIDs(t, got, tt.want)
		})
	}
}

// pickNibs returns the fixture nibs with the given ids, in the order named. It
// fails the test on an id the fixture does not hold, so a row naming a nib that
// was renamed out of the fixture fails loudly instead of quietly narrowing the
// set it was meant to describe.
func pickNibs(t *testing.T, reader *stubReader, ids ...string) []*nib.Nib {
	t.Helper()
	picked := make([]*nib.Nib, 0, len(ids))
	for _, id := range ids {
		b, ok := reader.nibs[id]
		if !ok {
			t.Fatalf("fixture has no nib %q", id)
		}
		picked = append(picked, b)
	}
	return picked
}

// TestApplyFilterSearchIntersectsTheWorkingSet pins what a search term means to
// ApplyFilter: it NARROWS the candidates it was handed and never reaches outside
// them.
//
// That is a different job from the one search does in queryResolver.Nibs, which
// SEEDS its input with Search() in place of All() — there the term decides which
// nibs are considered at all. Every other caller hands ApplyFilter a set some
// relationship already determined (the children of X, the nibs blocking X), and
// on those the same term can only mean "of those, the ones matching": a hit
// outside the relation is not a child of X, so admitting it would answer a
// question nobody asked.
//
// The rows walk the space between the two extremes. A branch that only ever saw
// a term matching part of the working set would pass while widening on any term
// that also matches something outside it, which is why the whole-store and
// outside-only rows are here.
func TestApplyFilterSearchIntersectsTheWorkingSet(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}
	reader.searchOut = map[string][]*nib.Nib{
		"whole-store": reader.allNibs,
		"the-pair":    pickNibs(t, reader, "nibs-f1", "nibs-t2"),
		"one-inside":  pickNibs(t, reader, "nibs-t2"),
		"outside":     pickNibs(t, reader, "nibs-m1", "nibs-x1"),
		"no-nib":      nil,
	}

	// The children of nibs-e1 — the set nibResolver.Children hands ApplyFilter.
	children := []string{"nibs-f1", "nibs-t2"}

	tests := []struct {
		name       string
		workingIDs []string
		filter     *model.NibFilter
		wantIDs    []string
	}{
		{"an unset search leaves the set unfiltered",
			children, &model.NibFilter{}, children},
		{"an empty search leaves the set unfiltered",
			children, &model.NibFilter{Search: strPtr("")}, children},
		{"a term matching every nib keeps the set and adds none of the rest",
			children, &model.NibFilter{Search: strPtr("whole-store")}, children},
		{"a term matching exactly the set keeps all of it",
			children, &model.NibFilter{Search: strPtr("the-pair")}, children},
		{"a term matching part of the set keeps that part",
			children, &model.NibFilter{Search: strPtr("one-inside")}, []string{"nibs-t2"}},
		{"a term matching only nibs outside the set keeps nothing",
			children, &model.NibFilter{Search: strPtr("outside")}, nil},
		{"a term matching no nib at all keeps nothing",
			children, &model.NibFilter{Search: strPtr("no-nib")}, nil},

		// Composed with an id-valued branch: each narrows independently, so the
		// answer is the intersection of the two, whichever order they run in.
		{"a wide term leaves an id filter's answer alone",
			nil, &model.NibFilter{Search: strPtr("whole-store"), AncestorID: strPtr("e1")},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
		{"a term and an id filter intersect",
			nil, &model.NibFilter{Search: strPtr("one-inside"), AncestorID: strPtr("e1")},
			[]string{"nibs-t2"}},
		{"a term disjoint from an id filter's answer keeps nothing",
			nil, &model.NibFilter{Search: strPtr("outside"), AncestorID: strPtr("e1")}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			working := reader.allNibs
			if tt.workingIDs != nil {
				working = pickNibs(t, reader, tt.workingIDs...)
			}
			got := applyFilterOK(t, context.Background(), working, tt.filter, reader, blocking)
			assertNibIDs(t, got, tt.wantIDs)
		})
	}
}

// TestSearchKeepsTheOrderItsSurfacePromises pins the ORDER each surface
// promises, which is the justification the rest of the design rests on and the
// one thing a set comparison cannot see.
//
// On a relationship field the term selects rather than ranks, so the answer
// keeps the relation's own order. That holds because filterBySearch walks the
// nibs it was handed and consults the match set, rather than walking the matches
// and consulting the input — the two produce the same MEMBERS and different
// SEQUENCES, so every membership assertion in this file passes either way.
//
// At the top level the opposite is promised: queryResolver.Nibs seeds from
// Search(), and the seeding is what carries relevance order — Core.Search hands
// back its id matches first, then its full-text hits — so the pipeline must
// leave that sequence alone.
func TestSearchKeepsTheOrderItsSurfacePromises(t *testing.T) {
	t.Run("a relationship field keeps the relation's order, not the index's", func(t *testing.T) {
		reader := hierarchyFixture()
		// The index answers in the opposite order to the one the relation is in,
		// so the two sequences are distinguishable and the members are identical.
		reader.searchOut = map[string][]*nib.Nib{
			"reversed": pickNibs(t, reader, "nibs-t2", "nibs-f1"),
		}

		// The working set, standing in for the members a relationship resolver
		// hands over. Its order is the one the answer has to keep.
		children := pickNibs(t, reader, "nibs-f1", "nibs-t2")

		got := applyFilterOK(t, context.Background(), children, &model.NibFilter{Search: strPtr("reversed")}, reader, &stubBlockingChecker{})
		assertNibIDsInOrder(t, got, []string{"nibs-f1", "nibs-t2"})
	})

	t.Run("the top-level query keeps the seeding order: id matches, then relevance", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		// "zebra" reaches each nib through a different leg of Core.Search: one by
		// ID substring, one by its indexed title. The ID leg is the one that must
		// come first, and neither nib has a parent, so tree completion cannot
		// reorder the pair.
		mustCreate(t, core, &nib.Nib{ID: "zebra1", Title: "Ordinary chore", Status: "todo"})
		mustCreate(t, core, &nib.Nib{ID: "t1", Title: "Zebra migration", Status: "todo"})
		ctx := context.Background()

		// Premise: the store itself answers in that order, so what the assertion
		// below tests is that the resolver preserved it rather than re-derived it.
		matched, err := core.Search("zebra")
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		assertNibIDsInOrder(t, matched, []string{"zebra1", "t1"})

		got, err := resolver.Query().Nibs(ctx, &model.NibFilter{Search: strPtr("zebra")}, nil)
		if err != nil {
			t.Fatalf("Nibs: %v", err)
		}
		assertNibIDsInOrder(t, got, []string{"zebra1", "t1"})
	})
}

// TestTopLevelSearchQueriesTheIndexOnce pins the cost of one top-level search:
// a single Core.Search, not two.
//
// queryResolver.Nibs seeds its input from the term, so the set it hands
// ApplyFilter already IS the term's answer. Leaving Search set on that filter
// makes ApplyFilter's intersection branch query the identical term again only to
// intersect that answer with itself — invisible in the result, and Core.Search
// is the expensive read here (a write lock for lazy init, a Bleve query, then a
// full store scan for id matches).
//
// The second assertion covers the half that is not about cost: the term is
// cleared on a COPY, so the caller's filter still carries it afterwards. A
// caller that reuses the filter for a second query — cmd/list.go's hidden-closed
// count does exactly that — would otherwise silently run it unsearched.
func TestTopLevelSearchQueriesTheIndexOnce(t *testing.T) {
	reader := hierarchyFixture()
	reader.searchOut = map[string][]*nib.Nib{"anything": reader.allNibs}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    &stubWriter{store: reader},
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, &stubWriter{store: reader}),
	}

	filter := &model.NibFilter{Search: strPtr("anything")}
	if _, err := resolver.Query().Nibs(context.Background(), filter, nil); err != nil {
		t.Fatalf("Nibs: %v", err)
	}

	if reader.searchCalls != 1 {
		t.Errorf("Core.Search called %d times, want 1", reader.searchCalls)
	}
	if filter.Search == nil || *filter.Search != "anything" {
		t.Errorf("caller's filter.Search = %v, want it left carrying the term", filter.Search)
	}
}

// TestApplyFilterSearchFailureIsReported pins that an index that cannot answer
// is reported rather than read as "nothing matched". The reader's own error
// travels out unwrapped, exactly as queryResolver.Nibs already propagates it
// from its seeding call, so both surfaces report one index failure alike.
func TestApplyFilterSearchFailureIsReported(t *testing.T) {
	reader := hierarchyFixture()
	indexDown := errors.New("search index unavailable")
	reader.searchErr = indexDown

	got, err := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{Search: strPtr("anything")}, reader, &stubBlockingChecker{})
	if err == nil {
		t.Fatalf("got %d nibs and no error; an index failure must not read as an empty match", len(got))
	}
	if !errors.Is(err, indexDown) {
		t.Errorf("error = %v, want the reader's own failure", err)
	}
	if got != nil {
		t.Errorf("result = %v, want nil alongside the error", got)
	}
}

// TestSearchOnARelationshipFieldDoesNotCompleteTheTree pins the one place the
// two search surfaces deliberately differ. Both narrow to the matching nibs;
// only the top-level one then adds every match's ancestors.
//
// That completion exists so a client can render a whole tree from a sparse
// match (see includeAncestors and the schema descriptions that promise it), and
// it belongs to a query over the whole store. On a relationship field the caller
// named the relation — "the children of this epic" — so an ancestor that is not
// a child of it is not part of the answer, however useful it would be for
// drawing a tree.
//
// Both halves are asserted against one fixture, because what is under test is
// the CONTRAST: a change that moved either surface onto the other's rule would
// leave a single-surface test passing.
func TestSearchOnARelationshipFieldDoesNotCompleteTheTree(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "m1", Title: "Milestone", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "e1", Title: "Epic", Status: "todo", Parent: "m1"})
	// Only this child carries the search term, so the term genuinely narrows the
	// epic's children instead of keeping all of them.
	mustCreate(t, core, &nib.Nib{ID: "t1", Title: "Zebra migration", Status: "todo", Parent: "e1"})
	mustCreate(t, core, &nib.Nib{ID: "t2", Title: "Ordinary chore", Status: "todo", Parent: "e1"})
	ctx := context.Background()

	epic, err := core.Get("e1")
	if err != nil {
		t.Fatalf("Get(e1): %v", err)
	}

	// Premise: the term matches exactly one nib in the store, so anything else
	// in either answer got there by a rule and not by the search.
	matched, err := core.Search("zebra")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertNibIDs(t, matched, []string{"t1"})

	t.Run("children narrow to the match and gain no ancestors", func(t *testing.T) {
		got, err := resolver.Nib().Children(ctx, epic, &model.NibFilter{Search: strPtr("zebra")}, nil)
		if err != nil {
			t.Fatalf("Children: %v", err)
		}
		assertNibIDs(t, got, []string{"t1"})
	})

	t.Run("the top-level query still completes the tree", func(t *testing.T) {
		got, err := resolver.Query().Nibs(ctx, &model.NibFilter{Search: strPtr("zebra")}, nil)
		if err != nil {
			t.Fatalf("Nibs: %v", err)
		}
		assertNibIDs(t, got, []string{"t1", "e1", "m1"})
	})
}

// contradictionFixture builds the smallest store in which each half of a
// contradictory pair has something real to match, so a row that is supposed to
// be answered has a non-empty answer and cannot pass by accident:
//
//	nibs-par ── nibs-chi   (nibs-chi is also blocked_by nibs-par)
//	nibs-oth               (a second root, with no parent and no blocker)
func contradictionFixture() *stubReader {
	par := &nib.Nib{ID: "nibs-par", Title: "Parent and blocker", Status: "todo"}
	chi := &nib.Nib{ID: "nibs-chi", Title: "Child", Status: "todo", Parent: "nibs-par", BlockedBy: []string{"nibs-par"}}
	oth := &nib.Nib{ID: "nibs-oth", Title: "Unrelated root", Status: "todo"}

	all := []*nib.Nib{par, chi, oth}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	return &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}
}

// TestApplyFilterRefusesContradictoryPairs pins the two combinations that no
// store state can satisfy. Each pairs an id-valued field with the presence field
// covering the same relationship, set to false:
//
//   - parentId + hasParent: false. parentId matches the stored b.Parent against
//     a target that already resolved, so a nib it matches has a parent link that
//     reader.Get answers for — which is what hasParent asks (resolvedParentID).
//   - blockedById + hasBlockedBy: false. blockedById requires the target in
//     b.BlockedBy, which forces len(b.BlockedBy) > 0, which is hasBlockedBy.
//
// Answering either with an empty list is the failure the refusal classes exist
// to remove: it is a factual claim about the store ("that parent has no
// children") for a question that was never answerable. cmd/list.go already
// refuses the flag spelling of the first pair, so without this the two surfaces
// disagree about one user error.
//
// The unresolvable-target rows pin the ORDER of the two refusals, which is a
// decision and not an accident: the contradiction is decided first, so an id
// that names no nib still reports the contradiction. The pair cannot be
// satisfied whatever the id is, so telling the caller to fix the id would send
// them to repair the half of the query that is not the problem — and cmd/list.go
// makes the same call, rejecting `--parent zzz --no-parent` as mutually
// exclusive rather than as an unknown id.
//
// The empty-id row is the deliberate exception. "" is not a target that would
// contradict the presence filter, it is a malformed argument, and
// FilterTargetEmptyError says so with the hint that names hasParent: false as
// the thing the caller probably wanted. Both classes exit 2, so the surfaces
// still agree on the exit; only the message differs, and this row keeps the more
// useful one.
func TestApplyFilterRefusesContradictoryPairs(t *testing.T) {
	reader := contradictionFixture()
	blocking := &stubBlockingChecker{}
	no := false

	tests := []struct {
		name          string
		filter        *model.NibFilter
		wantField     string
		wantPresence  string
		wantEmptyOnly bool // the empty-id class rather than the contradiction
	}{
		{"parentId with hasParent false",
			&model.NibFilter{ParentID: strPtr("par"), HasParent: &no}, "parentId", "hasParent", false},
		{"blockedById with hasBlockedBy false",
			&model.NibFilter{BlockedByID: strPtr("par"), HasBlockedBy: &no}, "blockedById", "hasBlockedBy", false},
		{"parentId naming no nib with hasParent false",
			&model.NibFilter{ParentID: strPtr("nonexistent"), HasParent: &no}, "parentId", "hasParent", false},
		{"blockedById naming no nib with hasBlockedBy false",
			&model.NibFilter{BlockedByID: strPtr("nonexistent"), HasBlockedBy: &no}, "blockedById", "hasBlockedBy", false},
		{"an empty parentId with hasParent false keeps the empty-id refusal",
			&model.NibFilter{ParentID: strPtr(""), HasParent: &no}, "parentId", "", true},
		{"an empty blockedById with hasBlockedBy false keeps the empty-id refusal",
			&model.NibFilter{BlockedByID: strPtr(""), HasBlockedBy: &no}, "blockedById", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking)
			if err == nil {
				t.Fatalf("got %d nibs and no error; an unsatisfiable pair answered with a list is indistinguishable from an empty store", len(got))
			}
			if got != nil {
				t.Errorf("result = %v, want nil alongside the error", got)
			}
			// Both classes are the validation class, so neither may carry the
			// not-found sentinel: exit 3 would tell the caller to fix an id
			// when the pair, not the id, is what cannot be answered.
			if errors.Is(err, nib.ErrNotFound) {
				t.Error("the refusal carries nib.ErrNotFound, so it classifies as NOT_FOUND (exit 3) instead of the caller's malformed query (exit 2)")
			}

			var contradiction *FilterTargetContradictionError
			if tt.wantEmptyOnly {
				var empty *FilterTargetEmptyError
				if !errors.As(err, &empty) {
					t.Fatalf("error = %T (%v), want *FilterTargetEmptyError — an empty id is malformed input, not a contradiction", err, err)
				}
				if empty.Field != tt.wantField {
					t.Errorf("Field = %q, want %q", empty.Field, tt.wantField)
				}
				if errors.As(err, &contradiction) {
					t.Error("errors.As matched *FilterTargetContradictionError, so the empty and contradictory classes are not distinguishable")
				}
				return
			}

			if !errors.As(err, &contradiction) {
				t.Fatalf("error = %T (%v), want *FilterTargetContradictionError", err, err)
			}
			if contradiction.Field != tt.wantField {
				t.Errorf("Field = %q, want the schema spelling %q", contradiction.Field, tt.wantField)
			}
			if contradiction.PresenceField != tt.wantPresence {
				t.Errorf("PresenceField = %q, want the schema spelling %q", contradiction.PresenceField, tt.wantPresence)
			}
			var notFound *FilterTargetNotFoundError
			if errors.As(err, &notFound) {
				t.Error("errors.As matched *FilterTargetNotFoundError, so an unsatisfiable pair reads as a mistyped id")
			}
		})
	}
}

// TestApplyFilterAnswersSatisfiablePairs is the other half of the refusal: every
// neighboring combination must still be answered, because a guard written one
// value too wide silently deletes working queries.
//
// The true rows are the closest neighbors — `parentId X, hasParent: true` is
// merely redundant, and cmd/list.go leaves the flag spelling of it alone for the
// same reason.
//
// blockingId + hasBlocking: false is the row that looks like a third
// contradictory pair and is not. hasBlocking asks BlockingChecker.IsBlocking,
// which is ACTIVE blocking — false when the candidate's own status released its
// dependents, and equally when every nib listing it as a blocker has itself
// been released — while blockingId matches anything in the target's blocked_by
// regardless of status. The combination therefore selects the blockers the
// target still lists that block nothing, by either route.
// TestBlockingIDWithHasBlockingFalseSelectsReleasedBlockers pins the same row
// against the real checker, since a stub is only as honest as the test that
// seeds it.
func TestApplyFilterAnswersSatisfiablePairs(t *testing.T) {
	reader := contradictionFixture()
	// The checker reports nothing as actively blocking, which is what a real
	// store shows once a blocker's status has released its dependents — the
	// state the blockingId row below is about, pinned against nibcore's own
	// IsBlocking by TestBlockingIDWithHasBlockingFalseSelectsReleasedBlockers.
	blocking := &stubBlockingChecker{}
	yes, no := true, false

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"parentId with hasParent true is redundant, not contradictory",
			&model.NibFilter{ParentID: strPtr("par"), HasParent: &yes}, []string{"nibs-chi"}},
		{"blockedById with hasBlockedBy true is redundant, not contradictory",
			&model.NibFilter{BlockedByID: strPtr("par"), HasBlockedBy: &yes}, []string{"nibs-chi"}},
		{"parentId alone",
			&model.NibFilter{ParentID: strPtr("par")}, []string{"nibs-chi"}},
		{"blockedById alone",
			&model.NibFilter{BlockedByID: strPtr("par")}, []string{"nibs-chi"}},
		{"hasParent false alone keeps the roots",
			&model.NibFilter{HasParent: &no}, []string{"nibs-par", "nibs-oth"}},
		{"hasBlockedBy false alone keeps the nibs listing no blocker",
			&model.NibFilter{HasBlockedBy: &no}, []string{"nibs-par", "nibs-oth"}},
		{"blockingId with hasBlocking false selects the target's released blockers",
			&model.NibFilter{BlockingID: strPtr("chi"), HasBlocking: &no}, []string{"nibs-par"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyFilterOK(t, context.Background(), reader.allNibs, tt.filter, reader, blocking)
			assertNibIDs(t, got, tt.wantIDs)
		})
	}
}

// TestBlockingIDWithHasBlockingFalseSelectsReleasedBlockers is why blockingId +
// hasBlocking: false is NOT refused alongside the other two pairs. The claim
// rests on nibcore.Core's own IsBlocking rather than on a stub, because the
// exclusion is only correct if "blocking" really is narrower than "named in a
// blocked_by list" in the implementation the CLI runs.
//
// The store: x1 lists both b1 and b2 as blockers, and b1 is completed — a status
// that releases its dependents (config.Config.StatusReleasesDependents). So b1 is
// a resolved blocker of x1 that blocks nothing, which is exactly the nib the
// combination selects. b1 reaches that false by its own status; the other route
// into the same set — a still-open blocker every one of whose dependents has
// itself been released — is not seeded here.
func TestBlockingIDWithHasBlockingFalseSelectsReleasedBlockers(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "b1", Title: "Released blocker", Status: "completed"})
	mustCreate(t, core, &nib.Nib{ID: "b2", Title: "Active blocker", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "x1", Title: "Blocked nib", Status: "todo", BlockedBy: []string{"b1", "b2"}})
	ctx := context.Background()

	// Premise: the two blockers differ exactly where the exclusion says they do.
	if core.IsBlocking("b1") {
		t.Fatalf("premise failed: a completed blocker reports as actively blocking, so the pair really would be contradictory")
	}
	if !core.IsBlocking("b2") {
		t.Fatalf("premise failed: an open blocker reports as blocking nothing")
	}

	yes, no := true, false
	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"blockingId alone reports every blocker the nib names",
			&model.NibFilter{BlockingID: strPtr("x1")}, []string{"b1", "b2"}},
		{"blockingId with hasBlocking false narrows to the released ones",
			&model.NibFilter{BlockingID: strPtr("x1"), HasBlocking: &no}, []string{"b1"}},
		{"blockingId with hasBlocking true narrows to the active ones",
			&model.NibFilter{BlockingID: strPtr("x1"), HasBlocking: &yes}, []string{"b2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyFilterOK(t, ctx, core.All(), tt.filter, core, resolver.Blocking)
			assertNibIDs(t, got, tt.wantIDs)
		})
	}
}

// TestARefusalShortCircuitsBeforeTheSearchIndex pins the ordering the search
// branch's placement rests on: within ApplyFilter, a filter that is going to be
// refused never queries the index.
//
// The reader here fails every Search, so a term that reached the index would
// surface that failure in place of the refusal — which is the wrong answer
// twice over. It names an infrastructure problem for a malformed query, and it
// hides the one thing the caller can act on.
//
// The scope is ApplyFilter itself, and with it every caller that hands over a
// filter untouched — the five relationship resolvers and cmd/rel.go's
// traversal. It is NOT the whole top-level nibs query: queryResolver.Nibs seeds
// from Search() before calling ApplyFilter and returns that error directly, so
// on a down index a contradictory top-level query reports the index failure
// rather than the refusal. Hoisting the refusal checks ahead of the seeding
// call would close that gap at the cost of running the same validation in two
// places.
func TestARefusalShortCircuitsBeforeTheSearchIndex(t *testing.T) {
	reader := contradictionFixture()
	indexDown := errors.New("search index unavailable")
	reader.searchErr = indexDown
	no := false

	tests := []struct {
		name   string
		filter *model.NibFilter
		wantAs func(error) bool
	}{
		{"a contradictory pair",
			&model.NibFilter{ParentID: strPtr("par"), HasParent: &no, Search: strPtr("anything")},
			func(err error) bool {
				var e *FilterTargetContradictionError
				return errors.As(err, &e)
			}},
		{"an empty id",
			&model.NibFilter{ParentID: strPtr(""), Search: strPtr("anything")},
			func(err error) bool {
				var e *FilterTargetEmptyError
				return errors.As(err, &e)
			}},
		{"an unknown target",
			&model.NibFilter{ParentID: strPtr("nonexistent"), Search: strPtr("anything")},
			func(err error) bool {
				var e *FilterTargetNotFoundError
				return errors.As(err, &e)
			}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, &stubBlockingChecker{})
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if errors.Is(err, indexDown) {
				t.Fatalf("the index was queried for a filter that was going to be refused: %v", err)
			}
			if !tt.wantAs(err) {
				t.Errorf("error = %T (%v), want the refusal class this row names", err, err)
			}
		})
	}
}
