package graph

import (
	"errors"
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// walkFixture is a tiny parent-linked set the primitive can be exercised
// against without a reader: WalkParentChain only ever calls the step, so the
// step is the whole of the environment it needs.
type walkFixture struct {
	byID map[string]*nib.Nib
}

func newWalkFixture(parents map[string]string) *walkFixture {
	f := &walkFixture{byID: make(map[string]*nib.Nib, len(parents))}
	for id, parent := range parents {
		f.byID[id] = &nib.Nib{ID: id, Title: id, Parent: parent}
	}
	return f
}

// step resolves a parent link against the fixture, ending the chain (nil, nil)
// when the link is empty or names no nib — the resolved-parent rule every
// reader-backed walk applies.
func (f *walkFixture) step(b *nib.Nib) (*nib.Nib, error) {
	if b.Parent == "" {
		return nil, nil
	}
	parent, ok := f.byID[b.Parent]
	if !ok {
		return nil, nil
	}
	return parent, nil
}

func (f *walkFixture) get(t *testing.T, id string) *nib.Nib {
	t.Helper()
	b, ok := f.byID[id]
	if !ok {
		t.Fatalf("fixture nib %q missing", id)
	}
	return b
}

func chainIDs(chain []*nib.Nib) []string {
	var ids []string
	for _, b := range chain {
		ids = append(ids, b.ID)
	}
	return ids
}

// TestWalkParentChain pins the contract the three read-only parent walks share:
// what the depth cap counts, what a step that reports no parent does, and what
// the caller's visited set controls.
func TestWalkParentChain(t *testing.T) {
	// t1 and e1x are both children of e1, so the two walks the visited-set
	// subtests run meet at e1.
	f := newWalkFixture(map[string]string{
		"m1":     "",
		"e1":     "m1",
		"t1":     "e1",
		"e1x":    "e1",
		"orphan": "ghost", // names no nib
		"d1":     "orphan",
		"self":   "self",
		"c1":     "c2",
		"c2":     "c1",
	})

	tests := []struct {
		name  string
		start string
		depth int
		want  []string
	}{
		{"walks to the root, nearest ancestor first", "t1", -1, []string{"e1", "m1"}},
		{"a root has no chain", "m1", -1, nil},
		{"depth 0 takes no hops at all", "t1", 0, nil},
		{"depth caps the number of hops", "t1", 1, []string{"e1"}},
		{"a depth beyond the root stops at the root", "t1", 9, []string{"e1", "m1"}},
		{"a link naming no nib ends the chain", "orphan", -1, nil},
		{"a link naming no nib ends the chain, keeping the rungs below it", "d1", -1, []string{"orphan"}},
		{"a seeded start is not its own ancestor", "self", -1, nil},
		{"a cycle terminates without reporting the start", "c1", -1, []string{"c2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WalkParentChain(f.get(t, tt.start), f.step, map[string]bool{tt.start: true}, tt.depth)
			if err != nil {
				t.Fatalf("WalkParentChain: %v", err)
			}
			if !reflect.DeepEqual(chainIDs(got), tt.want) {
				t.Errorf("chain = %v, want %v", chainIDs(got), tt.want)
			}
		})
	}

	// Termination does not depend on the seed. With nothing seeded, a cycle
	// walks back through the start and stops on the second sighting of an id
	// the loop itself banked — the check-and-mark pair, not the seed, is what
	// bounds the walk.
	t.Run("an unseeded cycle still terminates", func(t *testing.T) {
		got, err := WalkParentChain(f.get(t, "c1"), f.step, map[string]bool{}, -1)
		if err != nil {
			t.Fatalf("WalkParentChain: %v", err)
		}
		if want := []string{"c2", "c1"}; !reflect.DeepEqual(chainIDs(got), want) {
			t.Errorf("chain = %v, want %v", chainIDs(got), want)
		}
	})

	// A nil visited set is accepted rather than a precondition: the walk
	// allocates one for itself, so the marking step has somewhere to write and
	// a parented start does not crash the process. Nothing is seeded, so a
	// cycle running back through start reports start.
	t.Run("a nil visited set walks unseeded", func(t *testing.T) {
		got, err := WalkParentChain(f.get(t, "t1"), f.step, nil, -1)
		if err != nil {
			t.Fatalf("WalkParentChain: %v", err)
		}
		if want := []string{"e1", "m1"}; !reflect.DeepEqual(chainIDs(got), want) {
			t.Errorf("chain = %v, want %v", chainIDs(got), want)
		}

		got, err = WalkParentChain(f.get(t, "c1"), f.step, nil, -1)
		if err != nil {
			t.Fatalf("WalkParentChain: %v", err)
		}
		if want := []string{"c2", "c1"}; !reflect.DeepEqual(chainIDs(got), want) {
			t.Errorf("cycle chain = %v, want %v", chainIDs(got), want)
		}
	})

	t.Run("a visited set shared across walks banks ancestry once", func(t *testing.T) {
		shared := map[string]bool{"t1": true, "e1x": true}
		first, err := WalkParentChain(f.get(t, "t1"), f.step, shared, -1)
		if err != nil {
			t.Fatalf("first walk: %v", err)
		}
		if want := []string{"e1", "m1"}; !reflect.DeepEqual(chainIDs(first), want) {
			t.Fatalf("first chain = %v, want %v", chainIDs(first), want)
		}
		second, err := WalkParentChain(f.get(t, "e1x"), f.step, shared, -1)
		if err != nil {
			t.Fatalf("second walk: %v", err)
		}
		if len(second) != 0 {
			t.Errorf("second chain = %v, want empty (its ancestry was banked by the first walk)", chainIDs(second))
		}
	})

	t.Run("a per-call visited set reports the shared ancestry again", func(t *testing.T) {
		want := []string{"e1", "m1"}
		first, err := WalkParentChain(f.get(t, "t1"), f.step, map[string]bool{"t1": true}, -1)
		if err != nil {
			t.Fatalf("first walk: %v", err)
		}
		if !reflect.DeepEqual(chainIDs(first), want) {
			t.Fatalf("first chain = %v, want %v", chainIDs(first), want)
		}
		second, err := WalkParentChain(f.get(t, "e1x"), f.step, map[string]bool{"e1x": true}, -1)
		if err != nil {
			t.Fatalf("second walk: %v", err)
		}
		if !reflect.DeepEqual(chainIDs(second), want) {
			t.Errorf("second chain = %v, want %v (a per-call set reports shared ancestry again)", chainIDs(second), want)
		}
	})

	t.Run("a step error aborts the walk and discards the partial chain", func(t *testing.T) {
		boom := errors.New("boom")
		// Fails on the second hop, so a chain that survived would be non-empty.
		failing := func(b *nib.Nib) (*nib.Nib, error) {
			if b.ID == "e1" {
				return nil, boom
			}
			return f.step(b)
		}
		got, err := WalkParentChain(f.get(t, "t1"), failing, map[string]bool{"t1": true}, -1)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
		if got != nil {
			t.Errorf("chain = %v, want nil on error", chainIDs(got))
		}
	})

	// The walk hands back exactly what the step returned, which is how one
	// caller can walk over live store pointers while another walks over
	// snapshots.
	t.Run("yields the step's own nibs", func(t *testing.T) {
		got, err := WalkParentChain(f.get(t, "t1"), f.step, map[string]bool{"t1": true}, 1)
		if err != nil {
			t.Fatalf("WalkParentChain: %v", err)
		}
		if len(got) != 1 || got[0] != f.get(t, "e1") {
			t.Errorf("chain does not hand back the step's own pointer")
		}
	})
}
