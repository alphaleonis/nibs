package graph

import (
	"context"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// seedLintFixture builds one queue whose order and dependencies AGREE: kb is
// blocked by ka and sits behind it, so the store starts with no inversion and
// every pair a test sees is one the write under test created.
func seedLintFixture(t *testing.T, core *nibcore.Core) {
	t.Helper()
	for _, b := range []*nib.Nib{
		{ID: "ms1", Title: "Waypoint one", Type: "milestone", Status: "todo"},
		{ID: "ms2", Title: "Waypoint two", Type: "milestone", Status: "todo"},
		{ID: "ka", Title: "Queued A", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
		{ID: "kb", Title: "Queued B", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "b0", BlockedBy: []string{"ka"}},
		// A third member carrying no edge, so a test can add one without
		// meeting the cycle guard on the ka/kb pair.
		{ID: "kc", Title: "Queued C", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "c0"},
		{ID: "loose", Title: "Loose task", Type: "task", Status: "todo"},
	} {
		mustCreate(t, core, b)
	}
}

// collecting runs a mutation with a collector attached, the way `nibs serve`
// and the CLI both do, and returns the pairs it reported as id couples.
func collecting(t *testing.T, run func(ctx context.Context) error) [][2]string {
	t.Helper()
	c := NewQueueInversionCollector()
	if err := run(WithQueueInversions(context.Background(), c)); err != nil {
		t.Fatalf("mutation: %v", err)
	}
	created := c.Created()
	out := make([][2]string, len(created))
	for i, inv := range created {
		out[i] = [2]string{inv.Ahead.ID, inv.Blocker.ID}
	}
	return out
}

func wantPairs(t *testing.T, got [][2]string, expect ...[2]string) {
	t.Helper()
	if len(got) != len(expect) {
		t.Fatalf("reported inversions = %v, want %v", got, expect)
	}
	for i := range expect {
		if got[i] != expect[i] {
			t.Errorf("inversion[%d] = %v, want %v", i, got[i], expect[i])
		}
	}
}

// TestReorderNibReportsCreatedInversion is the gap this whole change closes: a
// queue move through the resolver used to persist an inversion with nothing in
// the response naming it, because the lint lived in cmd/ and only CLI callers
// ran it.
func TestReorderNibReportsCreatedInversion(t *testing.T) {
	t.Run("a queue move that crosses a blocker reports the pair", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		first := true
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().ReorderNib(ctx, "kb", nil, nil, &first, nil, nil, model.OrderScopeMilestone)
			return err
		})

		// kb now sits ahead of ka, which still blocks it.
		wantPairs(t, got, [2]string{"kb", "ka"})
	})

	t.Run("a move that creates nothing reports nothing", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		// ka to the front: it is already there, and crosses no blocker.
		first := true
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().ReorderNib(ctx, "ka", nil, nil, &first, nil, nil, model.OrderScopeMilestone)
			return err
		})
		wantPairs(t, got)
	})

	t.Run("a PARENT-scope reorder is not a queue write", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		first := true
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().ReorderNib(ctx, "kb", nil, nil, &first, nil, nil, model.OrderScopeParent)
			return err
		})
		wantPairs(t, got)
	})

	t.Run("an inversion that already existed is not re-reported", func(t *testing.T) {
		// The once-at-the-creating-write property: the pair is reported by the
		// move that made it, and a later move leaving it in place says nothing.
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		first := true
		if _, err := resolver.Mutation().ReorderNib(context.Background(), "kb", nil, nil, &first, nil, nil, model.OrderScopeMilestone); err != nil {
			t.Fatalf("first move: %v", err)
		}
		// Move it again, still ahead of ka.
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().ReorderNib(ctx, "kb", nil, nil, &first, nil, nil, model.OrderScopeMilestone)
			return err
		})
		wantPairs(t, got)
	})
}

// TestUpdateNibReportsCreatedInversion covers the other half the nib names:
// the writes that put a pair into a queue without moving anything.
func TestUpdateNibReportsCreatedInversion(t *testing.T) {
	t.Run("an assignment that lands behind work it blocks reports the pair", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		// `loose` blocks ka. Assigning it appends it LAST, so it lands behind
		// the work it blocks — the direction an assignment can only create.
		if _, err := resolver.Mutation().UpdateNib(context.Background(), "ka",
			model.UpdateNibInput{AddBlockedBy: []string{"loose"}}); err != nil {
			t.Fatalf("seed blocker edge: %v", err)
		}

		ms := "ms1"
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().UpdateNib(ctx, "loose",
				model.UpdateNibInput{Milestone: graphql.OmittableOf(&ms)})
			return err
		})
		wantPairs(t, got, [2]string{"ka", "loose"})
	})

	t.Run("a new blocked-by edge inside one queue reports the pair", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		// ka sits ahead of kc; making ka blocked by kc inverts the pair with no
		// move at all.
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().UpdateNib(ctx, "ka",
				model.UpdateNibInput{AddBlockedBy: []string{"kc"}})
			return err
		})
		wantPairs(t, got, [2]string{"ka", "kc"})
	})

	t.Run("a new blocking edge reports the pair from the other side", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		// The same pair, spelled from the blocker's side: kc blocks ka, and ka
		// is ahead of it.
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().UpdateNib(ctx, "kc",
				model.UpdateNibInput{AddBlocking: []string{"ka"}})
			return err
		})
		wantPairs(t, got, [2]string{"ka", "kc"})
	})

	t.Run("writes that can only take pairs away report nothing", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			input model.UpdateNibInput
			id    string
		}{
			{"clearing the assignment", model.UpdateNibInput{Milestone: graphql.OmittableOf[*string](nil)}, "kb"},
			{"removing a blocker", model.UpdateNibInput{RemoveBlockedBy: []string{"ka"}}, "kb"},
			{"a status change", model.UpdateNibInput{Status: stringPtr("in-progress")}, "kb"},
			{"a title change", model.UpdateNibInput{Title: stringPtr("Renamed")}, "kb"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				resolver, core := setupTestResolver(t)
				seedLintFixture(t, core)
				got := collecting(t, func(ctx context.Context) error {
					_, err := resolver.Mutation().UpdateNib(ctx, tc.id, tc.input)
					return err
				})
				wantPairs(t, got)
			})
		}
	})
}

// TestEdgeMutationsReportCreatedInversion covers the two standalone edge
// mutations. The nib named updateNib's paths; these reach the same store
// through their own resolvers, and leaving them out would have left the very
// gap this change closes open on two more fields.
func TestEdgeMutationsReportCreatedInversion(t *testing.T) {
	t.Run("addBlockedBy", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().AddBlockedBy(ctx, "ka", "kc", nil)
			return err
		})
		wantPairs(t, got, [2]string{"ka", "kc"})
	})

	t.Run("addBlocking", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		// kc blocks ka, and ka is ahead of it — the same pair from the other side.
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().AddBlocking(ctx, "kc", "ka")
			return err
		})
		wantPairs(t, got, [2]string{"ka", "kc"})
	})

	t.Run("an edge outside any queue reports nothing", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedLintFixture(t, core)

		// `loose` is in no queue, so no pair can name it however it is blocked.
		got := collecting(t, func(ctx context.Context) error {
			_, err := resolver.Mutation().AddBlockedBy(ctx, "loose", "ka", nil)
			return err
		})
		wantPairs(t, got)
	})
}

// TestQueueLintWithoutCollector pins the nil path: a caller that attaches no
// collector — every pure unit test, and cmd/rel.go's direct resolver calls —
// still writes, and the lint costs it nothing.
func TestQueueLintWithoutCollector(t *testing.T) {
	resolver, core := setupTestResolver(t)
	seedLintFixture(t, core)

	first := true
	moved, err := resolver.Mutation().ReorderNib(context.Background(), "kb", nil, nil, &first, nil, nil, model.OrderScopeMilestone)
	if err != nil {
		t.Fatalf("ReorderNib with no collector: %v", err)
	}
	if moved.MilestoneOrder >= "a0" {
		t.Errorf("MilestoneOrder = %q, want a key before a0 — the write must land either way", moved.MilestoneOrder)
	}
	if QueueInversionsFrom(context.Background()) != nil {
		t.Error("QueueInversionsFrom(bare context) = non-nil, want nil")
	}
}
