package graph

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// readNibFileBytes returns the raw on-disk bytes of a nib's file. A refused
// mutation must leave another nib's file byte-identical, and only the bytes can
// witness that — the in-memory nib is a separate copy that a rejected write
// never installs anyway.
func readNibFileBytes(t *testing.T, core *nibcore.Core, id string) []byte {
	t.Helper()
	b, ok := core.GetSnapshot(id)
	if !ok {
		t.Fatalf("nib %s is not in the store", id)
	}
	raw, err := os.ReadFile(filepath.Join(core.Root(), b.Path))
	if err != nil {
		t.Fatalf("reading %s: %v", b.Path, err)
	}
	return raw
}

// TestUpdateNibPreValidatesSubjectBeforeWritingBlockingTargets pins the guard
// ordering inside updateNib. Its blocking handlers persist changes to OTHER
// nibs' files, so every subject guard that can be answered without a write has
// to run first — otherwise a mutation the caller is told failed still leaves a
// durable edit on the target, with a moved updated_at and a blocked_by entry
// nothing in the response mentions.
func TestUpdateNibPreValidatesSubjectBeforeWritingBlockingTargets(t *testing.T) {
	const (
		subjectID = "prevalidate-subject"
		targetID  = "prevalidate-target"
	)

	tests := []struct {
		name    string
		setup   func(t *testing.T) (*Resolver, *nibcore.Core)
		input   model.UpdateNibInput
		wantErr func(error) bool
		errDesc string
	}{
		{
			name:  "stale ifMatch",
			setup: setupTestResolver,
			input: model.UpdateNibInput{
				Title:       stringPtr("Renamed"),
				IfMatch:     stringPtr("staleetagvalue"),
				AddBlocking: []string{targetID},
			},
			wantErr: func(err error) bool {
				var mismatch *nibcore.ETagMismatchError
				return errors.As(err, &mismatch)
			},
			errDesc: "*nibcore.ETagMismatchError",
		},
		{
			name:  "invalid enum",
			setup: setupTestResolver,
			input: model.UpdateNibInput{
				Status:      stringPtr("bogus"),
				AddBlocking: []string{targetID},
			},
			wantErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), `invalid status "bogus"`)
			},
			errDesc: `an invalid status "bogus" validation error`,
		},
		{
			name:  "require_if_match with no ifMatch",
			setup: setupTestResolverWithRequireIfMatch,
			input: model.UpdateNibInput{
				Title:       stringPtr("Renamed"),
				AddBlocking: []string{targetID},
			},
			wantErr: func(err error) bool {
				var required *nibcore.ETagRequiredError
				return errors.As(err, &required)
			},
			errDesc: "*nibcore.ETagRequiredError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := tt.setup(t)
			mustCreate(t, core, &nib.Nib{ID: subjectID, Title: "Subject", Status: "todo"})
			mustCreate(t, core, &nib.Nib{ID: targetID, Title: "Target", Status: "todo"})

			before := readNibFileBytes(t, core, targetID)

			_, err := resolver.Mutation().UpdateNib(context.Background(), subjectID, tt.input)
			if !tt.wantErr(err) {
				t.Fatalf("UpdateNib() error = %v, want %s", err, tt.errDesc)
			}

			// Byte-identity is the guard that carries this test: it witnesses the
			// blocked_by entry, the moved updated_at and anything else a target
			// rewrite would leave behind, in one assertion. A separate updated_at
			// comparison would NOT add to it — both timestamps are truncated to the
			// second, so a rewrite inside the same wall-clock second leaves them
			// equal and the comparison passes while the file has changed.
			if after := readNibFileBytes(t, core, targetID); !bytes.Equal(before, after) {
				t.Errorf("refused update rewrote the target file\nbefore:\n%s\nafter:\n%s", before, after)
			}

			afterSnap, ok := core.GetSnapshot(targetID)
			if !ok {
				t.Fatalf("target %s left the store", targetID)
			}
			if slices.Contains(afterSnap.BlockedBy, subjectID) {
				t.Errorf("target blockedBy = %v, want no %s entry after a refused update", afterSnap.BlockedBy, subjectID)
			}
		})
	}
}

// TestUpdateNibPreValidatesSubjectBeforeSiblingOrderBackfill pins the guard
// ordering against updateNib's OTHER foreign write. Changing the parent
// recalculates the subject's order key, which reads the new parent's sibling
// set — and that read repairs any sibling missing an order key by persisting
// one (Orderer.backfillOrderKeys). A subject guard placed after the parent
// block therefore lets a refused update leave a durable edit on a sibling's
// file, which is the same failure class the pre-check exists to remove.
//
// The sibling here is created with no order key, which is the ordinary state
// for a nib written by anything but the createNib resolver: Core.Create never
// assigns one.
func TestUpdateNibPreValidatesSubjectBeforeSiblingOrderBackfill(t *testing.T) {
	const (
		subjectID = "backfill-subject"
		epicID    = "backfill-epic"
		siblingID = "backfill-sibling"
	)

	tests := []struct {
		name    string
		input   func(subjectETag string) model.UpdateNibInput
		wantErr func(error) bool
		errDesc string
	}{
		{
			name: "invalid enum",
			input: func(string) model.UpdateNibInput {
				return model.UpdateNibInput{
					Parent: graphql.OmittableOf(stringPtr(epicID)),
					Status: stringPtr("bogus"),
				}
			},
			wantErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), `invalid status "bogus"`)
			},
			errDesc: `an invalid status "bogus" validation error`,
		},
		{
			name: "stale ifMatch",
			input: func(string) model.UpdateNibInput {
				return model.UpdateNibInput{
					Parent:  graphql.OmittableOf(stringPtr(epicID)),
					IfMatch: stringPtr("staleetagvalue"),
				}
			},
			wantErr: func(err error) bool {
				var mismatch *nibcore.ETagMismatchError
				return errors.As(err, &mismatch)
			},
			errDesc: "*nibcore.ETagMismatchError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolver(t)
			mustCreate(t, core, &nib.Nib{ID: epicID, Title: "Epic", Status: "todo", Type: "epic"})
			mustCreate(t, core, &nib.Nib{ID: siblingID, Title: "Sibling", Status: "todo", Type: "task", Parent: epicID})
			subject := &nib.Nib{ID: subjectID, Title: "Subject", Status: "todo", Type: "task"}
			mustCreate(t, core, subject)

			if snap, ok := core.GetSnapshot(siblingID); !ok || snap.Order != "" {
				t.Fatalf("sibling order = %q, want it unset so the backfill path is reachable", snap.Order)
			}

			before := readNibFileBytes(t, core, siblingID)

			_, err := resolver.Mutation().UpdateNib(context.Background(), subjectID, tt.input(subject.ETag()))
			if !tt.wantErr(err) {
				t.Fatalf("UpdateNib() error = %v, want %s", err, tt.errDesc)
			}

			if after := readNibFileBytes(t, core, siblingID); !bytes.Equal(before, after) {
				t.Errorf("refused update rewrote the sibling file\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// shortParentReader hands updateNib a working clone whose parent is spelled
// SHORT — the shape that separates a raw parent-change comparison from a
// resolved one. The store itself cannot be put in that state (the loader
// canonicalizes every link id on the way in, nibcore/canonicalize.go), but the
// resolver does not know that, so forcing it here is what makes the difference
// observable. Everything else defers to the real Core, so any sibling backfill
// this provokes is a genuine write to a genuine file.
type shortParentReader struct {
	*nibcore.Core
	subjectID   string
	shortParent string
}

func (r *shortParentReader) GetForUpdate(id string) (*nib.Nib, error) {
	b, err := r.Core.GetForUpdate(id)
	if err == nil && b.ID == r.subjectID {
		b.Parent = r.shortParent
	}
	return b, err
}

// shortParentResolver wraps core's reader so the subject's working clone carries
// shortParent instead of its canonical stored parent.
func shortParentResolver(core *nibcore.Core, subjectID, shortParent string) *Resolver {
	reader := &shortParentReader{Core: core, subjectID: subjectID, shortParent: shortParent}
	return &Resolver{Reader: reader, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(reader, core)}
}

// TestTypeChangeBranchMakesNoForeignWrite covers the SECOND call into
// validateAndSetParent — the one in the type-change branch. That call re-checks
// the subject's EXISTING parent, which by definition is not changing, so it must
// never reach RecalculateOrder and therefore never reach the sibling order-key
// backfill that recalculation performs. A type change is not a move.
//
// The subject's working clone carries a SHORT-form parent, which is what makes
// the guard bite: deciding "did the parent change?" from the raw stored string
// sees "epic" become "nibs-epic" and recalculates, backfilling an order key onto
// a sibling this update never named. Deciding it from the resolved parent sees
// no change. Both outcomes are asserted against the sibling's on-disk bytes.
//
// The refused case additionally pins why updateNib applies all four enum fields
// before the pre-check and defers this branch until after it.
func TestTypeChangeBranchMakesNoForeignWrite(t *testing.T) {
	// A real type change (task -> bug) so the branch runs at all.
	tests := []struct {
		name    string
		input   model.UpdateNibInput
		wantErr string
	}{
		// An invalid ESTIMATE, so the refusal comes from the enum field applied
		// LAST — the one a pre-check placed above the type block could not see.
		{"refused", model.UpdateNibInput{Type: stringPtr("bug"), Estimate: graphql.OmittableOf(stringPtr("2h"))}, `invalid estimate "2h"`},
		{"accepted", model.UpdateNibInput{Type: stringPtr("bug")}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, core := setupTestResolverWithPrefix(t, "nibs-")
			mustCreate(t, core, &nib.Nib{ID: "nibs-epic", Title: "Epic", Status: "todo", Type: "epic", Order: "a0"})
			// No order key — this is the sibling a recalculation would backfill.
			mustCreate(t, core, &nib.Nib{ID: "nibs-child", Title: "Child", Status: "todo", Type: "task", Parent: "nibs-epic"})
			mustCreate(t, core, &nib.Nib{ID: "nibs-subject", Title: "Subject", Status: "todo", Type: "task", Parent: "nibs-epic", Order: "a2"})
			resolver := shortParentResolver(core, "nibs-subject", "epic")

			if snap, ok := core.GetSnapshot("nibs-child"); !ok || snap.Order != "" {
				t.Fatalf("child order = %q, want it unset so the backfill path is reachable", snap.Order)
			}
			before := readNibFileBytes(t, core, "nibs-child")

			got, err := resolver.Mutation().UpdateNib(context.Background(), "nibs-subject", tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("UpdateNib() error = %v, want one containing %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("UpdateNib() error = %v", err)
				}
				// The subject's own position is untouched too — a type change is
				// not a move, whichever way its parent link happens to be spelled.
				if got.Order != "a2" {
					t.Errorf("subject order = %q, want it unmoved at %q", got.Order, "a2")
				}
			}

			if after := readNibFileBytes(t, core, "nibs-child"); !bytes.Equal(before, after) {
				t.Errorf("type change rewrote the sibling file\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// TestUpdateNibRecalcUsesNewPriority pins the ordering consequence of applying
// priority before the parent block rather than after it.
//
// RecalculateOrder positions a child among its siblings by PRIORITY
// (positionDefaultByPriority), and it reads the priority this same mutation is
// setting — so a single update that changes parent and priority together places
// the nib by its incoming priority, not its outgoing one.
//
// The subject is "low" and moves to "critical"; the sibling group holds one
// "critical" anchor at a0 and one "low" nib at a2. By the NEW priority the nib
// lands between them; by the OLD one it lands after a2. Every sibling carries an
// order key so no backfill runs — an unordered one would be assigned a key past
// a2 and join the comparison, which decides nothing about priority.
//
// There is only one route into RecalculateOrder from updateNib. The type-change
// branch used to be a second one, reachable when the subject's stored parent was
// spelled short, and is not anymore: it compares the RESOLVED parent, which a
// type change never moves. TestTypeChangeBranchMakesNoForeignWrite guards that.
func TestUpdateNibRecalcUsesNewPriority(t *testing.T) {
	t.Run("parent-change path", func(t *testing.T) {
		resolver, core := setupTestResolverWithPrefix(t, "nibs-")
		mustCreate(t, core, &nib.Nib{ID: "nibs-epic", Title: "Epic", Status: "todo", Type: "epic", Order: "a0"})
		mustCreate(t, core, &nib.Nib{ID: "nibs-anchor", Title: "Anchor", Status: "todo", Type: "task", Priority: "critical", Parent: "nibs-epic", Order: "a0"})
		mustCreate(t, core, &nib.Nib{ID: "nibs-tail", Title: "Tail", Status: "todo", Type: "task", Priority: "low", Parent: "nibs-epic", Order: "a2"})
		mustCreate(t, core, &nib.Nib{ID: "nibs-subject", Title: "Subject", Status: "todo", Type: "task", Priority: "low", Order: "b0"})

		got, err := resolver.Mutation().UpdateNib(context.Background(), "nibs-subject", model.UpdateNibInput{
			Parent:   graphql.OmittableOf(stringPtr("nibs-epic")),
			Type:     stringPtr("bug"),
			Priority: graphql.OmittableOf(stringPtr("critical")),
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if got.Order <= "a0" || got.Order >= "a2" {
			t.Errorf("order = %q, want it between the critical anchor (a0) and the low sibling (a2) — positioned by the NEW priority", got.Order)
		}
	})
}

// TestUpdateNibPreValidationLeavesSuccessPathIntact is the other half of the
// ordering guard: pre-validating the subject must not stop an accepted update
// from reaching its blocking targets. Both directions are covered — addBlocking
// writes the target's blocked_by entry, removeBlocking clears it — and each is
// asserted against the on-disk bytes, not just the in-memory nib.
func TestUpdateNibPreValidationLeavesSuccessPathIntact(t *testing.T) {
	const (
		subjectID = "success-subject"
		targetID  = "success-target"
	)

	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	subject := &nib.Nib{ID: subjectID, Title: "Subject", Status: "todo"}
	mustCreate(t, core, subject)
	mustCreate(t, core, &nib.Nib{ID: targetID, Title: "Target", Status: "todo"})

	etag := subject.ETag()
	got, err := resolver.Mutation().UpdateNib(ctx, subjectID, model.UpdateNibInput{
		Title:       stringPtr("Renamed"),
		IfMatch:     &etag,
		AddBlocking: []string{targetID},
	})
	if err != nil {
		t.Fatalf("UpdateNib() with a valid ifMatch and addBlocking error = %v", err)
	}
	if got.Title != "Renamed" {
		t.Errorf("subject title = %q, want %q", got.Title, "Renamed")
	}

	snap, ok := core.GetSnapshot(targetID)
	if !ok {
		t.Fatalf("target %s is not in the store", targetID)
	}
	if !slices.Contains(snap.BlockedBy, subjectID) {
		t.Errorf("target blockedBy = %v, want it to contain %s", snap.BlockedBy, subjectID)
	}
	if raw := readNibFileBytes(t, core, targetID); !strings.Contains(string(raw), subjectID) {
		t.Errorf("target file does not name %s after addBlocking:\n%s", subjectID, raw)
	}

	etag = got.ETag()
	if _, err := resolver.Mutation().UpdateNib(ctx, subjectID, model.UpdateNibInput{
		IfMatch:        &etag,
		RemoveBlocking: []string{targetID},
	}); err != nil {
		t.Fatalf("UpdateNib() with removeBlocking error = %v", err)
	}

	snap, ok = core.GetSnapshot(targetID)
	if !ok {
		t.Fatalf("target %s is not in the store", targetID)
	}
	if slices.Contains(snap.BlockedBy, subjectID) {
		t.Errorf("target blockedBy = %v, want removeBlocking to have cleared %s", snap.BlockedBy, subjectID)
	}
	if raw := readNibFileBytes(t, core, targetID); strings.Contains(string(raw), subjectID) {
		t.Errorf("target file still names %s after removeBlocking:\n%s", subjectID, raw)
	}
}
