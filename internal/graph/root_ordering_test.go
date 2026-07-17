package graph

import (
	"context"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestGetRootSiblings(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "Root One", Order: "b0"}
	root2 := &nib.Nib{ID: "root-2", Title: "Root Two", Order: "a0"}
	child := &nib.Nib{ID: "child-1", Title: "Child", Parent: "root-1", Order: "a0"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"root-1":  root1,
			"root-2":  root2,
			"child-1": child,
		},
		allNibs: []*nib.Nib{root1, root2, child},
	}
	writer := &stubWriter{}

	orderer := NewOrderer(reader, writer)
	roots := orderer.getRootSiblings()

	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2", len(roots))
	}
	// Should be sorted by order: a0 before b0
	if roots[0].ID != "root-2" {
		t.Errorf("first root = %q, want root-2 (order a0)", roots[0].ID)
	}
	if roots[1].ID != "root-1" {
		t.Errorf("second root = %q, want root-1 (order b0)", roots[1].ID)
	}
}

func TestGetRootSiblingsBackfillsUnordered(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "Alpha", Order: ""}
	root2 := &nib.Nib{ID: "root-2", Title: "Beta", Order: ""}

	writer := &stubWriter{}
	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"root-1": root1,
			"root-2": root2,
		},
		allNibs: []*nib.Nib{root1, root2},
	}

	orderer := NewOrderer(reader, writer)
	roots := orderer.getRootSiblings()

	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2", len(roots))
	}
	// After backfill, both should have order keys
	if roots[0].Order == "" || roots[1].Order == "" {
		t.Error("backfill should have assigned order keys to all roots")
	}
	// Keys should be different
	if roots[0].Order == roots[1].Order {
		t.Errorf("roots have same order key %q — backfill assigned duplicates", roots[0].Order)
	}
}

func TestReorderRootNibFirst(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "First", Status: "todo", Order: "a0"}
	root2 := &nib.Nib{ID: "root-2", Title: "Second", Status: "todo", Order: "b0"}
	root3 := &nib.Nib{ID: "root-3", Title: "Third", Status: "todo", Order: "c0"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"root-1": root1, "root-2": root2, "root-3": root3,
		},
		allNibs: []*nib.Nib{root1, root2, root3},
	}
	writer := &stubWriter{store: reader}

	resolver := &Resolver{
		Reader: reader, Writer: writer,
		Validator: &stubValidator{}, Blocking: &stubBlockingChecker{},
		Orderer: NewOrderer(reader, writer),
	}

	ctx := context.Background()
	first := true
	result, err := resolver.Mutation().ReorderNib(ctx, "root-3", nil, nil, &first, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// root-3 should now have an order key before root-1's "a0"
	if result.Order >= "a0" {
		t.Errorf("expected order < 'a0', got %q", result.Order)
	}
}

func TestReorderRootNibAfter(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "First", Status: "todo", Order: "a0"}
	root2 := &nib.Nib{ID: "root-2", Title: "Second", Status: "todo", Order: "b0"}
	root3 := &nib.Nib{ID: "root-3", Title: "Third", Status: "todo", Order: "c0"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"root-1": root1, "root-2": root2, "root-3": root3,
		},
		allNibs: []*nib.Nib{root1, root2, root3},
	}
	writer := &stubWriter{store: reader}

	resolver := &Resolver{
		Reader: reader, Writer: writer,
		Validator: &stubValidator{}, Blocking: &stubBlockingChecker{},
		Orderer: NewOrderer(reader, writer),
	}

	ctx := context.Background()
	afterID := "root-1"
	result, err := resolver.Mutation().ReorderNib(ctx, "root-3", &afterID, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// root-3 should now be between root-1 (a0) and root-2 (b0)
	if result.Order <= "a0" || result.Order >= "b0" {
		t.Errorf("expected order between 'a0' and 'b0', got %q", result.Order)
	}
}

func TestReorderRootNibBefore(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "First", Status: "todo", Order: "a0"}
	root2 := &nib.Nib{ID: "root-2", Title: "Second", Status: "todo", Order: "b0"}
	root3 := &nib.Nib{ID: "root-3", Title: "Third", Status: "todo", Order: "c0"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"root-1": root1, "root-2": root2, "root-3": root3,
		},
		allNibs: []*nib.Nib{root1, root2, root3},
	}
	writer := &stubWriter{store: reader}

	resolver := &Resolver{
		Reader: reader, Writer: writer,
		Validator: &stubValidator{}, Blocking: &stubBlockingChecker{},
		Orderer: NewOrderer(reader, writer),
	}

	ctx := context.Background()
	beforeID := "root-2"
	result, err := resolver.Mutation().ReorderNib(ctx, "root-3", nil, &beforeID, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// root-3 should now be between root-1 (a0) and root-2 (b0)
	if result.Order <= "a0" || result.Order >= "b0" {
		t.Errorf("expected order between 'a0' and 'b0', got %q", result.Order)
	}
}

func TestReorderBeforeWithDuplicateOrderKeys(t *testing.T) {
	// Reproduces the bug: dup and target both have order "n".
	// mover (order "nV") tries to move before target.
	// positionBefore gets prevKey="n" (dup) and target="n" (target),
	// OrderBetween("n","n") = "nV" which equals mover's current order — a no-op.
	// Names chosen so dup sorts before target by title tiebreaker: "Alpha" < "Beta".
	zs2b := &nib.Nib{ID: "zs2b", Title: "Alpha dup", Status: "scrapped", Order: "n"}
	kofy := &nib.Nib{ID: "kofy", Title: "Beta target", Status: "todo", Order: "n"}
	n1nb := &nib.Nib{ID: "n1nb", Title: "Gamma mover", Status: "todo", Order: "nV"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"zs2b": zs2b, "kofy": kofy, "n1nb": n1nb,
		},
		allNibs: []*nib.Nib{zs2b, kofy, n1nb},
	}

	writer := &stubWriter{store: reader}
	resolver := &Resolver{
		Reader: reader, Writer: writer,
		Validator: &stubValidator{}, Blocking: &stubBlockingChecker{},
		Orderer: NewOrderer(reader, writer),
	}

	ctx := context.Background()
	beforeID := "kofy"
	kofyOrder := kofy.Order // capture before mutation in case resolver modifies pointer
	result, err := resolver.Mutation().ReorderNib(ctx, "n1nb", nil, &beforeID, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The order MUST change — it should be before kofy ("n")
	if result.Order == "nV" {
		t.Error("order did not change — reorder was a no-op due to duplicate sibling keys")
	}
	if result.Order >= kofyOrder {
		t.Errorf("result.Order %q should be < kofy.Order %q", result.Order, kofyOrder)
	}
}

func TestReorderAfterWithDuplicateOrderKeys(t *testing.T) {
	// a1 and a2 have the same order key. Moving a3 after a1 should
	// produce a key between a1 and a2, not equal to a3's current key.
	a1 := &nib.Nib{ID: "a1", Title: "A1", Status: "todo", Order: "m"}
	a2 := &nib.Nib{ID: "a2", Title: "A2", Status: "todo", Order: "m"}
	a3 := &nib.Nib{ID: "a3", Title: "A3", Status: "todo", Order: "mV"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"a1": a1, "a2": a2, "a3": a3,
		},
		allNibs: []*nib.Nib{a1, a2, a3},
	}

	writer := &stubWriter{store: reader}
	resolver := &Resolver{
		Reader: reader, Writer: writer,
		Validator: &stubValidator{}, Blocking: &stubBlockingChecker{},
		Orderer: NewOrderer(reader, writer),
	}

	ctx := context.Background()
	afterID := "a1"
	a1Order := a1.Order // capture before mutation in case resolver modifies pointer
	result, err := resolver.Mutation().ReorderNib(ctx, "a3", &afterID, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Order == "mV" {
		t.Error("order did not change — reorder was a no-op due to duplicate sibling keys")
	}
	if result.Order <= a1Order {
		t.Errorf("result.Order %q should be > a1.Order %q", result.Order, a1Order)
	}
}

func TestReorderRootNibRejectsNonSibling(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "Root", Status: "todo", Order: "a0"}
	child := &nib.Nib{ID: "child-1", Title: "Child", Status: "todo", Parent: "root-1", Order: "a0"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"root-1":  root1,
			"child-1": child,
		},
		allNibs: []*nib.Nib{root1, child},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader: reader, Writer: writer,
		Validator: &stubValidator{}, Blocking: &stubBlockingChecker{},
		Orderer: NewOrderer(reader, writer),
	}

	ctx := context.Background()
	afterID := "child-1"
	_, err := resolver.Mutation().ReorderNib(ctx, "root-1", &afterID, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when positioning after a non-sibling, got nil")
	}
}

func TestCreateRootNibsGetUniqueOrderKeys(t *testing.T) {
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{},
		allNibs: []*nib.Nib{},
	}
	writer := &stubWriter{store: reader}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    writer,
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, writer),
	}

	ctx := context.Background()

	// Create first root nib
	nib1, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
		Title: "First Root",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	// Simulate core storing the nib
	reader.nibs[nib1.ID] = nib1
	reader.allNibs = append(reader.allNibs, nib1)

	// Create second root nib
	nib2, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
		Title: "Second Root",
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	reader.nibs[nib2.ID] = nib2
	reader.allNibs = append(reader.allNibs, nib2)

	// Create third root nib
	nib3, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
		Title: "Third Root",
	})
	if err != nil {
		t.Fatalf("create third: %v", err)
	}

	// All three should have different order keys
	if nib1.Order == "" || nib2.Order == "" || nib3.Order == "" {
		t.Errorf("missing order keys: %q, %q, %q", nib1.Order, nib2.Order, nib3.Order)
	}
	if nib1.Order == nib2.Order {
		t.Errorf("nib1 and nib2 have same order key %q", nib1.Order)
	}
	if nib2.Order == nib3.Order {
		t.Errorf("nib2 and nib3 have same order key %q", nib2.Order)
	}
	if nib1.Order == nib3.Order {
		t.Errorf("nib1 and nib3 have same order key %q", nib1.Order)
	}

	// Keys should be increasing (each new nib is appended after the last)
	if nib1.Order >= nib2.Order {
		t.Errorf("expected nib1.Order < nib2.Order, got %q >= %q", nib1.Order, nib2.Order)
	}
	if nib2.Order >= nib3.Order {
		t.Errorf("expected nib2.Order < nib3.Order, got %q >= %q", nib2.Order, nib3.Order)
	}
}
