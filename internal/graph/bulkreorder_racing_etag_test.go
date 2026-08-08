package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"

	"errors"
)

// racingWriter delegates every write to the real store, but diverges targetID's
// on-disk file immediately BEFORE its Update — deterministically reproducing the
// one window a bulk reorder's pre-validation cannot close: every listed etag is
// current when the batch is pre-checked, and one of them goes stale before that
// nib is written. The resulting refusal comes from nibcore.Update itself, so the
// test exercises the real racing path rather than a fabricated error.
type racingWriter struct {
	NibWriter
	t        *testing.T
	core     *nibcore.Core
	targetID string
	diverged bool
}

func (w *racingWriter) Update(b *nib.Nib, ifMatch *string) error {
	if b.ID == w.targetID && !w.diverged {
		w.diverged = true
		divergeNibOnDisk(w.t, w.core, w.targetID)
	}
	return w.NibWriter.Update(b, ifMatch)
}

// setupRacingReorderFixture builds a parent with four ordered children whose ids
// share no substring with a hex etag — so asserting that an error names the
// offending nib cannot pass on a coincidental match inside an etag token.
func setupRacingReorderFixture(t *testing.T) (*Resolver, *nibcore.Core, string) {
	t.Helper()
	resolver, core := setupTestResolver(t)
	parent := &nib.Nib{ID: "epic-race", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, order string }{
		{"alpha", "a0"}, {"bravo", "b0"}, {"victor", "c0"}, {"whiskey", "d0"},
	} {
		child := &nib.Nib{ID: c.id, Title: strings.ToUpper(c.id), Status: "todo", Type: "task", Parent: "epic-race", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}
	return resolver, core, "epic-race"
}

// TestBulkReorder_RacingETagMismatchNamesNib is the identification guard for the
// racing half of the bulk-reorder etag contract. A conflict carries a currentEtag
// reconcile token, and for a bulk mutation that token is worthless unless the
// caller can tell WHICH of the listed nibs it belongs to. The pre-validation
// refusal wraps its conflict with the offending id (validateIfMatchETags); the
// racing per-nib write must identify its nib the same way, or the client holds a
// token it cannot attribute.
//
// Red against `return nil, err` at either per-nib Update site: the raw
// ETagMismatchError reads "etag mismatch: provided X, current is Y" and names
// nothing.
func TestBulkReorder_RacingETagMismatchNamesNib(t *testing.T) {
	ctx := context.Background()

	// The loop writes in requested order, so the target is the LAST nib written:
	// its write is the one that races after earlier writes have already landed.
	cases := []struct {
		name   string
		target string
		call   func(r *Resolver, parentID string, ifMatch []*model.ChildEtag) error
		ids    []string
	}{
		{
			name:   "ReorderChildren",
			target: "whiskey",
			ids:    []string{"victor", "alpha", "bravo", "whiskey"},
			call: func(r *Resolver, parentID string, ifMatch []*model.ChildEtag) error {
				_, err := r.Mutation().ReorderChildren(ctx, parentID, []string{"victor", "alpha", "bravo", "whiskey"}, ifMatch)
				return err
			},
		},
		{
			name:   "ReorderSiblings",
			target: "whiskey",
			ids:    []string{"bravo", "whiskey"},
			call: func(r *Resolver, _ string, ifMatch []*model.ChildEtag) error {
				_, err := r.Mutation().ReorderSiblings(ctx, []string{"bravo", "whiskey"}, strPtr("alpha"), nil, nil, ifMatch)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver, core, parentID := setupRacingReorderFixture(t)
			ifMatch := childEtags(t, resolver, tc.ids...)

			racing := *resolver
			racing.Writer = &racingWriter{NibWriter: resolver.Writer, t: t, core: core, targetID: tc.target}

			err := tc.call(&racing, parentID, ifMatch)
			if err == nil {
				t.Fatal("expected an etag mismatch from the racing per-nib write")
			}

			// The typed, reconcilable conflict must survive the wrapping — the wire
			// code (extensions.code = "ETAG_MISMATCH") and the CLI's CONFLICT exit
			// status both route it with errors.As, not on message text.
			var mismatch *nibcore.ETagMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("got %T: %v, want a wrapped *nibcore.ETagMismatchError", err, err)
			}
			if mismatch.Current == "" {
				t.Error("mismatch.Current is empty, want the server's current etag (the token a retry echoes back)")
			}

			// The identification the reconcile token depends on. Both bulk-reorder
			// refusal paths use the same "failed to reorder <id>" wrapping, so a
			// client reads the offending nib the same way regardless of which one
			// refused.
			wantPrefix := "failed to reorder " + tc.target + ":"
			if !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("error = %q, want it to start with %q so the currentEtag is attributable to a nib", err.Error(), wantPrefix)
			}
		})
	}
}

// TestBulkReorder_ETagMismatchWrappingIsIdentical pins the asymmetry this guard
// closes: the pre-validation refusal and the racing per-nib refusal must produce
// the SAME message shape for the same offending nib. Comparing them directly
// keeps the two sites from drifting apart again.
func TestBulkReorder_ETagMismatchWrappingIsIdentical(t *testing.T) {
	ctx := context.Background()

	// Pre-validation refusal: the caller's ifMatch for "whiskey" is stale on entry.
	preResolver, _, parentID := setupRacingReorderFixture(t)
	preIfMatch := childEtags(t, preResolver, "victor", "alpha", "bravo", "whiskey")
	for _, e := range preIfMatch {
		if e.ID == "whiskey" {
			e.Etag = "deadbeefdeadbeef"
		}
	}
	_, preErr := preResolver.Mutation().ReorderChildren(ctx, parentID, []string{"victor", "alpha", "bravo", "whiskey"}, preIfMatch)
	if preErr == nil {
		t.Fatal("expected a pre-validation etag mismatch")
	}

	// Racing refusal: every etag is current at pre-check; "whiskey" diverges just
	// before its own write.
	raceResolver, core, parentID2 := setupRacingReorderFixture(t)
	raceIfMatch := childEtags(t, raceResolver, "victor", "alpha", "bravo", "whiskey")
	racing := *raceResolver
	racing.Writer = &racingWriter{NibWriter: raceResolver.Writer, t: t, core: core, targetID: "whiskey"}
	_, raceErr := racing.Mutation().ReorderChildren(ctx, parentID2, []string{"victor", "alpha", "bravo", "whiskey"}, raceIfMatch)
	if raceErr == nil {
		t.Fatal("expected a racing etag mismatch")
	}

	// Only the etag tokens differ; the identification around them must not.
	strip := func(err error) string {
		msg := err.Error()
		if i := strings.Index(msg, "provided"); i >= 0 {
			return msg[:i]
		}
		return msg
	}
	if strip(preErr) != strip(raceErr) {
		t.Errorf("pre-validation and racing refusals identify their nib differently:\n  pre-validation: %q\n  racing:         %q",
			preErr.Error(), raceErr.Error())
	}
}
