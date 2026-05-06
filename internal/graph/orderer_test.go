package graph

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// ordererStubReader extends stubReader to auto-derive parent links from nibs.
// This avoids having to manually specify links for each test.
type ordererStubReader struct {
	stubReader
}

func (s *ordererStubReader) FindIncomingLinks(targetID string) []nib.IncomingLink {
	var links []nib.IncomingLink
	for _, b := range s.allNibs {
		if b.Parent == targetID {
			links = append(links, nib.IncomingLink{FromNib: b, LinkType: "parent"})
		}
	}
	return links
}

func newOrdererReader(nibs ...*nib.Nib) *ordererStubReader {
	m := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		m[b.ID] = b
	}
	return &ordererStubReader{
		stubReader: stubReader{
			nibs:    m,
			allNibs: nibs,
		},
	}
}

func TestOrderer_GetRootSiblings(t *testing.T) {
	root1 := &nib.Nib{ID: "root-1", Title: "Root One", Order: "b0"}
	root2 := &nib.Nib{ID: "root-2", Title: "Root Two", Order: "a0"}
	child := &nib.Nib{ID: "child-1", Title: "Child", Parent: "root-1", Order: "a0"}

	reader := newOrdererReader(root1, root2, child)
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

func TestOrderer_GetSortedSiblings(t *testing.T) {
	parent := &nib.Nib{ID: "parent-1", Title: "Parent"}
	child1 := &nib.Nib{ID: "child-1", Title: "Child A", Parent: "parent-1", Order: "c0"}
	child2 := &nib.Nib{ID: "child-2", Title: "Child B", Parent: "parent-1", Order: "a0"}
	child3 := &nib.Nib{ID: "child-3", Title: "Child C", Parent: "parent-1", Order: "b0"}
	other := &nib.Nib{ID: "other-1", Title: "Other", Parent: "other-parent", Order: "a0"}

	reader := newOrdererReader(parent, child1, child2, child3, other)
	writer := &stubWriter{}

	orderer := NewOrderer(reader, writer)
	siblings := orderer.GetSortedSiblings("parent-1")

	if len(siblings) != 3 {
		t.Fatalf("got %d siblings, want 3", len(siblings))
	}
	// Should be sorted by order: a0, b0, c0
	if siblings[0].ID != "child-2" {
		t.Errorf("first sibling = %q, want child-2 (order a0)", siblings[0].ID)
	}
	if siblings[1].ID != "child-3" {
		t.Errorf("second sibling = %q, want child-3 (order b0)", siblings[1].ID)
	}
	if siblings[2].ID != "child-1" {
		t.Errorf("third sibling = %q, want child-1 (order c0)", siblings[2].ID)
	}
}

func TestOrderer_BackfillOrderKeys(t *testing.T) {
	t.Run("assigns keys to unordered root siblings", func(t *testing.T) {
		root1 := &nib.Nib{ID: "root-1", Title: "Alpha", Order: ""}
		root2 := &nib.Nib{ID: "root-2", Title: "Beta", Order: ""}

		writer := &stubWriter{}
		reader := newOrdererReader(root1, root2)

		orderer := NewOrderer(reader, writer)
		roots := orderer.getRootSiblings()

		if len(roots) != 2 {
			t.Fatalf("got %d roots, want 2", len(roots))
		}
		if roots[0].Order == "" || roots[1].Order == "" {
			t.Error("backfill should have assigned order keys to all roots")
		}
		if roots[0].Order == roots[1].Order {
			t.Errorf("roots have same order key %q", roots[0].Order)
		}
		// Writer should have been called to persist the backfilled keys
		if len(writer.updated) != 2 {
			t.Errorf("expected 2 update calls for backfill, got %d", len(writer.updated))
		}
	})

	t.Run("preserves existing order keys", func(t *testing.T) {
		ordered := &nib.Nib{ID: "o-1", Title: "Ordered", Order: "a0"}
		unordered := &nib.Nib{ID: "u-1", Title: "Unordered", Order: ""}

		writer := &stubWriter{}
		reader := newOrdererReader(ordered, unordered)

		orderer := NewOrderer(reader, writer)
		roots := orderer.getRootSiblings()

		if len(roots) != 2 {
			t.Fatalf("got %d roots, want 2", len(roots))
		}
		// The ordered nib should keep its key
		for _, r := range roots {
			if r.ID == "o-1" && r.Order != "a0" {
				t.Errorf("ordered nib's key changed from %q to %q", "a0", r.Order)
			}
		}
		// The unordered nib should get a key after the ordered one
		for _, r := range roots {
			if r.ID == "u-1" {
				if r.Order == "" {
					t.Error("unordered nib should have been assigned a key")
				}
				if r.Order <= "a0" {
					t.Errorf("unordered nib key %q should be after %q", r.Order, "a0")
				}
			}
		}
		// Only the unordered nib should have triggered an update
		if len(writer.updated) != 1 {
			t.Errorf("expected 1 update call, got %d", len(writer.updated))
		}
	})

	t.Run("skips backfill when all have keys", func(t *testing.T) {
		root1 := &nib.Nib{ID: "r-1", Title: "A", Order: "a0"}
		root2 := &nib.Nib{ID: "r-2", Title: "B", Order: "b0"}

		writer := &stubWriter{}
		reader := newOrdererReader(root1, root2)

		orderer := NewOrderer(reader, writer)
		orderer.getRootSiblings()

		if len(writer.updated) != 0 {
			t.Errorf("expected no update calls when all nibs already ordered, got %d", len(writer.updated))
		}
	})
}

func TestOrderer_ApplyPositioning(t *testing.T) {
	t.Run("root nib appends last", func(t *testing.T) {
		existing := &nib.Nib{ID: "r-1", Title: "Existing", Order: "a0"}
		newNib := &nib.Nib{ID: "r-2", Title: "New"}

		reader := newOrdererReader(existing)
		orderer := NewOrderer(reader, &stubWriter{})

		err := orderer.ApplyPositioning(newNib, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order == "" {
			t.Error("expected order key to be assigned")
		}
		if newNib.Order <= existing.Order {
			t.Errorf("new nib order %q should be after existing %q", newNib.Order, existing.Order)
		}
	})

	t.Run("root nib with afterId places between existing roots", func(t *testing.T) {
		r1 := &nib.Nib{ID: "r-1", Title: "First", Order: "a0"}
		r2 := &nib.Nib{ID: "r-2", Title: "Second", Order: "a1"}

		reader := newOrdererReader(r1, r2)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "r-3", Title: "Inserted"}
		afterID := "r-1"
		err := orderer.ApplyPositioning(newNib, &afterID, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order <= r1.Order {
			t.Errorf("order %q should be after r1 %q", newNib.Order, r1.Order)
		}
		if newNib.Order >= r2.Order {
			t.Errorf("order %q should be before r2 %q", newNib.Order, r2.Order)
		}
	})

	t.Run("root nib with beforeId places between existing roots", func(t *testing.T) {
		r1 := &nib.Nib{ID: "r-1", Title: "First", Order: "a0"}
		r2 := &nib.Nib{ID: "r-2", Title: "Second", Order: "a1"}

		reader := newOrdererReader(r1, r2)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "r-3", Title: "Inserted"}
		beforeID := "r-2"
		err := orderer.ApplyPositioning(newNib, nil, &beforeID, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order <= r1.Order {
			t.Errorf("order %q should be after r1 %q", newNib.Order, r1.Order)
		}
		if newNib.Order >= r2.Order {
			t.Errorf("order %q should be before r2 %q", newNib.Order, r2.Order)
		}
	})

	t.Run("root nib with first flag places before all roots", func(t *testing.T) {
		r1 := &nib.Nib{ID: "r-1", Title: "First", Order: "a0"}
		r2 := &nib.Nib{ID: "r-2", Title: "Second", Order: "a1"}

		reader := newOrdererReader(r1, r2)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "r-3", Title: "New First"}
		first := true
		err := orderer.ApplyPositioning(newNib, nil, nil, &first)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order >= r1.Order {
			t.Errorf("order %q should be before r1 %q", newNib.Order, r1.Order)
		}
	})

	t.Run("root nib with afterId returns error for unknown anchor", func(t *testing.T) {
		r1 := &nib.Nib{ID: "r-1", Title: "First", Order: "a0"}

		reader := newOrdererReader(r1)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "r-2", Title: "New"}
		afterID := "does-not-exist"
		err := orderer.ApplyPositioning(newNib, &afterID, nil, nil)
		if err == nil {
			t.Fatal("expected error for unknown anchor")
		}
		if !strings.Contains(err.Error(), "sibling nib not found") {
			t.Errorf("error %q should contain %q", err.Error(), "sibling nib not found")
		}
	})

	t.Run("root nib with afterId pointing at child returns not-a-sibling error", func(t *testing.T) {
		parent := &nib.Nib{ID: "p-1", Title: "Parent", Order: "a0"}
		child := &nib.Nib{ID: "c-1", Title: "Child", Parent: "p-1", Order: "a0"}

		reader := newOrdererReader(parent, child)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "r-1", Title: "New Root"}
		afterID := "c-1"
		err := orderer.ApplyPositioning(newNib, &afterID, nil, nil)
		if err == nil {
			t.Fatal("expected error for non-sibling anchor (anchor has a parent)")
		}
		if !strings.Contains(err.Error(), "not a sibling") {
			t.Errorf("error %q should contain %q", err.Error(), "not a sibling")
		}
	})

	t.Run("root nib with empty sibling list gets initial key", func(t *testing.T) {
		newNib := &nib.Nib{ID: "r-1", Title: "First Root"}
		reader := newOrdererReader()
		orderer := NewOrderer(reader, &stubWriter{})

		err := orderer.ApplyPositioning(newNib, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order != nib.OrderInitial() {
			t.Errorf("order = %q, want %q", newNib.Order, nib.OrderInitial())
		}
	})

	t.Run("rejects multiple positioning flags", func(t *testing.T) {
		newNib := &nib.Nib{ID: "c-1", Title: "Child", Parent: "p-1"}
		reader := newOrdererReader()
		orderer := NewOrderer(reader, &stubWriter{})

		afterID := "a"
		beforeID := "b"
		err := orderer.ApplyPositioning(newNib, &afterID, &beforeID, nil)
		if err == nil {
			t.Error("expected error when specifying both afterId and beforeId")
		}
	})

	t.Run("child nib positioned by priority among siblings", func(t *testing.T) {
		parent := &nib.Nib{ID: "p-1", Title: "Parent"}
		// critical (rank 0), high (rank 1), normal (rank 2), low (rank 3)
		criticalChild := &nib.Nib{ID: "c-1", Title: "Critical", Parent: "p-1", Priority: "critical", Order: "a0"}
		normalChild := &nib.Nib{ID: "c-2", Title: "Normal", Parent: "p-1", Priority: "normal", Order: "b0"}
		lowChild := &nib.Nib{ID: "c-3", Title: "Low", Parent: "p-1", Priority: "low", Order: "c0"}

		reader := newOrdererReader(parent, criticalChild, normalChild, lowChild)
		orderer := NewOrderer(reader, &stubWriter{})

		// Insert a high-priority nib — should go between critical and normal
		highNib := &nib.Nib{ID: "c-4", Title: "High", Parent: "p-1", Priority: "high"}
		err := orderer.ApplyPositioning(highNib, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if highNib.Order <= criticalChild.Order {
			t.Errorf("high nib order %q should be after critical %q", highNib.Order, criticalChild.Order)
		}
		if highNib.Order >= normalChild.Order {
			t.Errorf("high nib order %q should be before normal %q", highNib.Order, normalChild.Order)
		}
	})

	t.Run("child nib with first flag", func(t *testing.T) {
		parent := &nib.Nib{ID: "p-1", Title: "Parent"}
		child1 := &nib.Nib{ID: "c-1", Title: "First", Parent: "p-1", Order: "a0"}
		child2 := &nib.Nib{ID: "c-2", Title: "Second", Parent: "p-1", Order: "b0"}

		reader := newOrdererReader(parent, child1, child2)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "c-3", Title: "New First", Parent: "p-1"}
		first := true
		err := orderer.ApplyPositioning(newNib, nil, nil, &first)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order >= child1.Order {
			t.Errorf("new nib order %q should be before first child %q", newNib.Order, child1.Order)
		}
	})

	t.Run("child nib with no siblings gets initial key", func(t *testing.T) {
		parent := &nib.Nib{ID: "p-1", Title: "Parent"}

		reader := newOrdererReader(parent)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "c-1", Title: "Only Child", Parent: "p-1"}
		err := orderer.ApplyPositioning(newNib, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order != nib.OrderInitial() {
			t.Errorf("order = %q, want %q", newNib.Order, nib.OrderInitial())
		}
	})
}

func TestOrderer_PositionAfter(t *testing.T) {
	t.Run("places nib after target", func(t *testing.T) {
		s1 := &nib.Nib{ID: "s-1", Title: "A", Parent: "p-1", Order: "a0"}
		s2 := &nib.Nib{ID: "s-2", Title: "B", Parent: "p-1", Order: "b0"}
		s3 := &nib.Nib{ID: "s-3", Title: "C", Parent: "p-1", Order: "c0"}

		reader := newOrdererReader(s1, s2, s3)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "n-1", Parent: "p-1"}
		err := orderer.positionAfter(newNib, "s-1", []*nib.Nib{s1, s2, s3})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order <= s1.Order || newNib.Order >= s2.Order {
			t.Errorf("order %q should be between %q and %q", newNib.Order, s1.Order, s2.Order)
		}
	})

	t.Run("places after last sibling", func(t *testing.T) {
		s1 := &nib.Nib{ID: "s-1", Title: "A", Parent: "p-1", Order: "a0"}

		reader := newOrdererReader(s1)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "n-1", Parent: "p-1"}
		err := orderer.positionAfter(newNib, "s-1", []*nib.Nib{s1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order <= s1.Order {
			t.Errorf("order %q should be after %q", newNib.Order, s1.Order)
		}
	})

	t.Run("rejects non-sibling target", func(t *testing.T) {
		s1 := &nib.Nib{ID: "s-1", Title: "A", Parent: "p-1", Order: "a0"}
		other := &nib.Nib{ID: "o-1", Title: "Other", Parent: "p-2", Order: "a0"}

		reader := newOrdererReader(s1, other)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "n-1", Parent: "p-1"}
		err := orderer.positionAfter(newNib, "o-1", []*nib.Nib{s1, other})
		if err == nil {
			t.Error("expected error for non-sibling target")
		}
	})

	t.Run("handles duplicate order keys", func(t *testing.T) {
		// Two siblings with same order key (legacy data)
		dup := &nib.Nib{ID: "d-1", Title: "Alpha", Parent: "p-1", Order: "n"}
		target := &nib.Nib{ID: "d-2", Title: "Beta", Parent: "p-1", Order: "n"}
		mover := &nib.Nib{ID: "d-3", Title: "Gamma", Parent: "p-1", Order: "nV"}

		reader := newOrdererReader(dup, target, mover)
		orderer := NewOrderer(reader, &stubWriter{})

		// Position after dup (first "n" key). The next distinct key should skip
		// the second "n" and use "" (append last).
		siblings := []*nib.Nib{dup, target, mover}
		nib.SortByOrder(siblings)

		err := orderer.positionAfter(mover, dup.ID, siblings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mover.Order <= dup.Order {
			t.Errorf("mover order %q should be after dup %q", mover.Order, dup.Order)
		}
	})

	t.Run("returns error for unknown target", func(t *testing.T) {
		reader := newOrdererReader()
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "n-1", Parent: "p-1"}
		err := orderer.positionAfter(newNib, "nonexistent", nil)
		if err == nil {
			t.Error("expected error for nonexistent target")
		}
	})
}

func TestOrderer_PositionBefore(t *testing.T) {
	t.Run("places nib before target", func(t *testing.T) {
		s1 := &nib.Nib{ID: "s-1", Title: "A", Parent: "p-1", Order: "a0"}
		s2 := &nib.Nib{ID: "s-2", Title: "B", Parent: "p-1", Order: "b0"}
		s3 := &nib.Nib{ID: "s-3", Title: "C", Parent: "p-1", Order: "c0"}

		reader := newOrdererReader(s1, s2, s3)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "n-1", Parent: "p-1"}
		err := orderer.positionBefore(newNib, "s-2", []*nib.Nib{s1, s2, s3})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order <= s1.Order || newNib.Order >= s2.Order {
			t.Errorf("order %q should be between %q and %q", newNib.Order, s1.Order, s2.Order)
		}
	})

	t.Run("places before first sibling", func(t *testing.T) {
		s1 := &nib.Nib{ID: "s-1", Title: "A", Parent: "p-1", Order: "a0"}

		reader := newOrdererReader(s1)
		orderer := NewOrderer(reader, &stubWriter{})

		newNib := &nib.Nib{ID: "n-1", Parent: "p-1"}
		err := orderer.positionBefore(newNib, "s-1", []*nib.Nib{s1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newNib.Order >= s1.Order {
			t.Errorf("order %q should be before %q", newNib.Order, s1.Order)
		}
	})

	t.Run("handles duplicate order keys before target", func(t *testing.T) {
		dup := &nib.Nib{ID: "d-1", Title: "Alpha", Parent: "p-1", Order: "n"}
		target := &nib.Nib{ID: "d-2", Title: "Beta", Parent: "p-1", Order: "n"}
		mover := &nib.Nib{ID: "d-3", Title: "Gamma", Parent: "p-1", Order: "nV"}

		reader := newOrdererReader(dup, target, mover)
		orderer := NewOrderer(reader, &stubWriter{})

		siblings := []*nib.Nib{dup, target, mover}
		nib.SortByOrder(siblings)

		err := orderer.positionBefore(mover, target.ID, siblings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mover.Order >= target.Order {
			t.Errorf("mover order %q should be before target %q", mover.Order, target.Order)
		}
		// Crucially, the order should have changed from its original "nV"
		if mover.Order == "nV" {
			t.Error("order did not change — reorder was a no-op due to duplicate sibling keys")
		}
	})
}

func TestOrderer_RecalculateOrder(t *testing.T) {
	t.Run("root nib gets appended last", func(t *testing.T) {
		existing := &nib.Nib{ID: "r-1", Title: "Existing", Order: "a0"}
		moved := &nib.Nib{ID: "r-2", Title: "Moved", Order: "old-key"}
		// moved has no parent — it's a root nib now
		reader := newOrdererReader(existing, moved)
		orderer := NewOrderer(reader, &stubWriter{})

		orderer.RecalculateOrder(moved)

		if moved.Order <= existing.Order {
			t.Errorf("recalculated order %q should be after existing %q", moved.Order, existing.Order)
		}
	})

	t.Run("child nib positioned by priority among siblings", func(t *testing.T) {
		parent := &nib.Nib{ID: "p-1", Title: "Parent"}
		critical := &nib.Nib{ID: "c-1", Title: "Critical", Parent: "p-1", Priority: "critical", Order: "a0"}
		low := &nib.Nib{ID: "c-2", Title: "Low", Parent: "p-1", Priority: "low", Order: "c0"}

		// The nib being recalculated is high priority, currently has wrong order
		moved := &nib.Nib{ID: "c-3", Title: "High", Parent: "p-1", Priority: "high", Order: "z0"}

		reader := newOrdererReader(parent, critical, low, moved)
		orderer := NewOrderer(reader, &stubWriter{})

		orderer.RecalculateOrder(moved)

		// High should be after critical but before low
		if moved.Order <= critical.Order {
			t.Errorf("order %q should be after critical %q", moved.Order, critical.Order)
		}
		if moved.Order >= low.Order {
			t.Errorf("order %q should be before low %q", moved.Order, low.Order)
		}
	})

	t.Run("only child gets initial key", func(t *testing.T) {
		parent := &nib.Nib{ID: "p-1", Title: "Parent"}
		child := &nib.Nib{ID: "c-1", Title: "Only", Parent: "p-1", Order: "old"}

		reader := newOrdererReader(parent, child)
		orderer := NewOrderer(reader, &stubWriter{})

		orderer.RecalculateOrder(child)

		if child.Order != nib.OrderInitial() {
			t.Errorf("order = %q, want %q for only child", child.Order, nib.OrderInitial())
		}
	})

	t.Run("lone root gets initial key", func(t *testing.T) {
		root := &nib.Nib{ID: "r-1", Title: "Root", Order: "old"}

		reader := newOrdererReader(root)
		orderer := NewOrderer(reader, &stubWriter{})

		orderer.RecalculateOrder(root)

		if root.Order != nib.OrderInitial() {
			t.Errorf("order = %q, want %q for lone root", root.Order, nib.OrderInitial())
		}
	})
}
