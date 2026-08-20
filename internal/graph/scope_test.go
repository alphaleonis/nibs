package graph

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// scopeFixture is one scope's entry in the exhaustiveness table: how to build
// a store holding one ordered group in that scope, plus the accessors the
// assertions need. Every grammar test below iterates Scope(0)..numScopes-1, so
// a future scope added to the enum without a fixture here — or without switch
// arms in the engine — fails these tests instead of shipping half-wired.
type scopeFixture struct {
	groupID  string
	key      func(*nib.Nib) string // the scope's own ordering key
	otherKey func(*nib.Nib) string // the OTHER scope's key, for isolation asserts
	// build returns a store holding, in this scope's group: three keyed
	// members ka/kb/kc (keys a0/b0/c0), one unkeyed member bf (backfill
	// target), plus a stranger str-1 that exists OUTSIDE the group. Every
	// member also carries the other scope's key "keep" so isolation is
	// observable.
	build func() (*ordererStubReader, *stubWriter)
}

func scopeFixtures(t *testing.T) map[Scope]scopeFixture {
	t.Helper()
	fixtures := map[Scope]scopeFixture{
		ScopeParent: {
			groupID:  "p1",
			key:      func(b *nib.Nib) string { return b.Order },
			otherKey: func(b *nib.Nib) string { return b.MilestoneOrder },
			build: func() (*ordererStubReader, *stubWriter) {
				reader := newOrdererReader(
					&nib.Nib{ID: "p1", Title: "Parent"},
					&nib.Nib{ID: "ka", Title: "A", Parent: "p1", Order: "a0", MilestoneOrder: "keep"},
					&nib.Nib{ID: "kb", Title: "B", Parent: "p1", Order: "b0", MilestoneOrder: "keep"},
					&nib.Nib{ID: "kc", Title: "C", Parent: "p1", Order: "c0", MilestoneOrder: "keep"},
					&nib.Nib{ID: "bf", Title: "Backfill", Parent: "p1", MilestoneOrder: "keep"},
					&nib.Nib{ID: "str-1", Title: "Stranger", Order: "a0"},
				)
				return reader, &stubWriter{store: &reader.stubReader}
			},
		},
		ScopeMilestone: {
			groupID:  "m1",
			key:      func(b *nib.Nib) string { return b.MilestoneOrder },
			otherKey: func(b *nib.Nib) string { return b.Order },
			build: func() (*ordererStubReader, *stubWriter) {
				reader := newOrdererReader(
					&nib.Nib{ID: "m1", Title: "Milestone", Type: "milestone"},
					&nib.Nib{ID: "m2", Title: "Other milestone", Type: "milestone"},
					&nib.Nib{ID: "ka", Title: "A", Parent: "m1", MilestoneOrder: "a0", Order: "keep"},
					&nib.Nib{ID: "kb", Title: "B", Parent: "m1", MilestoneOrder: "b0", Order: "keep"},
					&nib.Nib{ID: "kc", Title: "C", Parent: "m1", MilestoneOrder: "c0", Order: "keep"},
					&nib.Nib{ID: "bf", Title: "Backfill", Parent: "m1", Order: "keep"},
					&nib.Nib{ID: "str-1", Title: "Stranger", Parent: "m2", MilestoneOrder: "a0"},
				)
				return reader, &stubWriter{store: &reader.stubReader}
			},
		},
	}
	if len(fixtures) != int(numScopes) {
		t.Fatalf("the fixture table covers %d scopes, the engine declares %d — a new scope must be enrolled here", len(fixtures), numScopes)
	}
	return fixtures
}

func forEachScope(t *testing.T, run func(t *testing.T, scope Scope, fx scopeFixture)) {
	fixtures := scopeFixtures(t)
	for scope := Scope(0); scope < numScopes; scope++ {
		fx, ok := fixtures[scope]
		if !ok {
			t.Fatalf("no fixture for scope %d", scope)
		}
		t.Run(scope.String(), func(t *testing.T) { run(t, scope, fx) })
	}
}

// TestScopeMembersSortsAndBackfills: Members returns the group sorted by the
// scope's key, assigning a key to any member that lacks one (appended last)
// and persisting it through the writer.
func TestScopeMembersSortsAndBackfills(t *testing.T) {
	forEachScope(t, func(t *testing.T, scope Scope, fx scopeFixture) {
		reader, writer := fx.build()
		o := NewOrderer(reader, writer)

		members := o.Members(scope, fx.groupID)
		want := []string{"ka", "kb", "kc", "bf"}
		if got := nibIDs(members); !equalStrings(got, want) {
			t.Fatalf("Members(%v, %q) = %v, want %v", scope, fx.groupID, got, want)
		}
		last := members[len(members)-1]
		if fx.key(last) == "" {
			t.Error("the unkeyed member was not backfilled")
		}
		if fx.key(last) <= "c0" {
			t.Errorf("backfilled key %q does not append after c0", fx.key(last))
		}
		if len(writer.updated) != 1 || writer.updated[0].ID != "bf" {
			t.Errorf("backfill persisted %v, want exactly one update for bf", nibIDs(writer.updated))
		}
	})
}

// TestScopeMoveGrammar: Move places an existing member after/before an anchor
// or first, writing only the scope's own key.
func TestScopeMoveGrammar(t *testing.T) {
	forEachScope(t, func(t *testing.T, scope Scope, fx scopeFixture) {
		cases := []struct {
			name  string
			pos   Position
			check func(t *testing.T, key string)
		}{
			{"after", After("ka"), func(t *testing.T, key string) {
				if key <= "a0" || key >= "b0" {
					t.Errorf("key %q not between a0 and b0", key)
				}
			}},
			{"before", Before("kb"), func(t *testing.T, key string) {
				if key <= "a0" || key >= "b0" {
					t.Errorf("key %q not between a0 and b0", key)
				}
			}},
			{"first", First(), func(t *testing.T, key string) {
				if key >= "a0" {
					t.Errorf("key %q not before a0", key)
				}
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reader, writer := fx.build()
				o := NewOrderer(reader, writer)

				b, _ := reader.GetForUpdate("kc")
				if err := o.Move(scope, b, tc.pos); err != nil {
					t.Fatalf("Move: %v", err)
				}
				tc.check(t, fx.key(b))
			})
		}
	})
}

// TestScopeMoveErrorTiers: the two membership error tiers — an anchor that
// does not exist at all, and an anchor that exists outside the group — fail
// with distinct errors, in every scope.
func TestScopeMoveErrorTiers(t *testing.T) {
	forEachScope(t, func(t *testing.T, scope Scope, fx scopeFixture) {
		reader, writer := fx.build()
		o := NewOrderer(reader, writer)

		b, _ := reader.GetForUpdate("kc")
		err := o.Move(scope, b, After("no-such-nib"))
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("Move after a missing anchor = %v, want a not-found error", err)
		}

		err = o.Move(scope, b, After("str-1"))
		if err == nil || strings.Contains(err.Error(), "not found") {
			t.Errorf("Move after an out-of-group anchor = %v, want a membership error, not not-found", err)
		}
	})
}

// TestScopePlaceGrammar: Place enters a nib into its group at an explicit
// position, or at the scope's default placement (append-last here — every
// fixture member shares the default priority, where the parent scope's
// priority-aware default and plain append agree).
func TestScopePlaceGrammar(t *testing.T) {
	forEachScope(t, func(t *testing.T, scope Scope, fx scopeFixture) {
		t.Run("default appends", func(t *testing.T) {
			reader, writer := fx.build()
			o := NewOrderer(reader, writer)

			b, _ := reader.GetForUpdate("kc")
			fxSetKeyClear(t, scope, b)
			if err := o.Place(scope, b, DefaultPlacement()); err != nil {
				t.Fatalf("Place: %v", err)
			}
			if key := fx.key(b); key <= "b0" {
				t.Errorf("default placement key %q does not land after the keyed members", key)
			}
		})
		t.Run("anchored", func(t *testing.T) {
			reader, writer := fx.build()
			o := NewOrderer(reader, writer)

			b, _ := reader.GetForUpdate("kc")
			if err := o.Place(scope, b, At(Before("ka"))); err != nil {
				t.Fatalf("Place: %v", err)
			}
			if key := fx.key(b); key >= "a0" {
				t.Errorf("anchored placement key %q not before a0", key)
			}
		})
	})
}

// TestScopeRecalculate: after a reassignment, Recalculate gives the nib a
// fresh key at the scope's default position among its new group, excluding
// itself.
func TestScopeRecalculate(t *testing.T) {
	forEachScope(t, func(t *testing.T, scope Scope, fx scopeFixture) {
		reader, writer := fx.build()
		o := NewOrderer(reader, writer)

		b, _ := reader.GetForUpdate("ka")
		o.Recalculate(scope, b)
		if key := fx.key(b); key <= "c0" {
			t.Errorf("Recalculate key %q does not append after the other members", key)
		}
	})
}

// TestScopeCrossScopeIsolation: an operation in one scope never touches the
// other scope's key — a reorder in the parent tree cannot shuffle a milestone
// queue, and vice versa.
func TestScopeCrossScopeIsolation(t *testing.T) {
	forEachScope(t, func(t *testing.T, scope Scope, fx scopeFixture) {
		reader, writer := fx.build()
		o := NewOrderer(reader, writer)

		b, _ := reader.GetForUpdate("kc")
		if err := o.Move(scope, b, First()); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if got := fx.otherKey(b); got != "keep" {
			t.Errorf("a %v-scope move changed the other scope's key to %q; the two axes must be isolated", scope, got)
		}

		reader2, _ := fx.build()
		o2 := NewOrderer(reader2, &stubWriter{store: &reader2.stubReader})
		members := o2.Members(scope, fx.groupID)
		for _, m := range members {
			if got := fx.otherKey(m); got != "keep" {
				t.Errorf("a %v-scope backfill changed %s's other-scope key to %q", scope, m.ID, got)
			}
		}
	})
}

// TestScopeMilestoneUnassigned pins the milestone scope's "" semantics: the
// empty group is memberless — a nib in no queue cannot be moved within one,
// and default-placing it clears its queue key.
func TestScopeMilestoneUnassigned(t *testing.T) {
	reader := newOrdererReader(
		&nib.Nib{ID: "p1", Title: "Plain parent"},
		&nib.Nib{ID: "loose", Title: "Loose", Parent: "p1", MilestoneOrder: "stale"},
	)
	writer := &stubWriter{store: &reader.stubReader}
	o := NewOrderer(reader, writer)

	b, _ := reader.GetForUpdate("loose")
	if err := o.Move(ScopeMilestone, b, First()); err == nil {
		t.Error("Move in the milestone scope succeeded for a nib assigned to no milestone; want an error")
	}

	if err := o.Place(ScopeMilestone, b, DefaultPlacement()); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if b.MilestoneOrder != "" {
		t.Errorf("default-placing an unassigned nib kept queue key %q; want it cleared", b.MilestoneOrder)
	}

	// Recalculate is the reassignment hook: on a nib assigned to no milestone
	// it must clear the queue key, same as a default Place.
	b.MilestoneOrder = "stale"
	o.Recalculate(ScopeMilestone, b)
	if b.MilestoneOrder != "" {
		t.Errorf("Recalculate on an unassigned nib kept queue key %q; want it cleared", b.MilestoneOrder)
	}

	// And the empty group id enumerates nothing — it is no group at all.
	if members := o.Members(ScopeMilestone, ""); len(members) != 0 {
		t.Errorf("Members(ScopeMilestone, \"\") = %v, want empty", nibIDs(members))
	}
}

// TestScopeParentRootGroup pins the parent scope's "" semantics: the empty
// group is the root set, and the grammar works there.
func TestScopeParentRootGroup(t *testing.T) {
	reader := newOrdererReader(
		&nib.Nib{ID: "r1", Title: "Root one", Order: "a0"},
		&nib.Nib{ID: "r2", Title: "Root two", Order: "b0"},
	)
	writer := &stubWriter{store: &reader.stubReader}
	o := NewOrderer(reader, writer)

	if got := nibIDs(o.Members(ScopeParent, "")); !equalStrings(got, []string{"r1", "r2"}) {
		t.Fatalf("Members(ScopeParent, \"\") = %v, want the roots", got)
	}
	b, _ := reader.GetForUpdate("r2")
	if err := o.Move(ScopeParent, b, Before("r1")); err != nil {
		t.Fatalf("Move among roots: %v", err)
	}
	if b.Order >= "a0" {
		t.Errorf("root move key %q not before a0", b.Order)
	}
}

// fxSetKeyClear clears the scope's own key on b so a Place test starts from an
// unkeyed nib. The default arm keeps the enrollment complete: a future scope
// covered by the fixture table but not here would otherwise silently no-op.
func fxSetKeyClear(t *testing.T, scope Scope, b *nib.Nib) {
	switch scope {
	case ScopeParent:
		b.Order = ""
	case ScopeMilestone:
		b.MilestoneOrder = ""
	default:
		t.Fatalf("fxSetKeyClear knows no scope %v — enroll the new scope here", scope)
	}
}

func nibIDs(nibs []*nib.Nib) []string {
	out := make([]string, len(nibs))
	for i, b := range nibs {
		out[i] = b.ID
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
