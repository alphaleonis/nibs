package nibcore

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestMentionIndex_AddThenInboundSources is the tracer bullet for the
// reverse-mention index. After adding a source whose body contains `#b`,
// the inbound set for token "b" must include the source ID.
func TestMentionIndex_AddThenInboundSources(t *testing.T) {
	idx := newMentionIndex()
	idx.Add("a", "See #b for details.")

	got := idx.InboundSources("b")
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("InboundSources(\"b\") = %v, want [a]", got)
	}
}

// TestMentionIndex_OutboundTokens pins the outbound (source -> tokens) direction.
func TestMentionIndex_OutboundTokens(t *testing.T) {
	idx := newMentionIndex()
	idx.Add("a", "See #b and #c, then #b again.")

	got := idx.OutboundTokens("a")
	want := []string{"b", "c"}
	if len(got) != len(want) {
		t.Fatalf("OutboundTokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("OutboundTokens[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	t.Run("returned slice is a copy — caller mutation does not corrupt index", func(t *testing.T) {
		idx := newMentionIndex()
		idx.Add("a", "See #b and #c.")

		first := idx.OutboundTokens("a")
		if len(first) != 2 {
			t.Fatalf("precondition: got %d tokens, want 2", len(first))
		}
		first[0] = "MUTATED"

		second := idx.OutboundTokens("a")
		if second[0] != "b" {
			t.Errorf("index corrupted: OutboundTokens[0] = %q after external mutation, want %q", second[0], "b")
		}
	})
}

// TestMentionIndex_Remove drops a source from both outbound and inbound.
func TestMentionIndex_Remove(t *testing.T) {
	idx := newMentionIndex()
	idx.Add("a", "See #b and #c.")
	idx.Add("d", "Also #b.")

	idx.Remove("a")

	if toks := idx.OutboundTokens("a"); toks != nil {
		t.Errorf("after Remove, OutboundTokens(a) = %v, want nil", toks)
	}
	gotB := idx.InboundSources("b")
	if len(gotB) != 1 || gotB[0] != "d" {
		t.Errorf("after Remove, InboundSources(b) = %v, want [d]", gotB)
	}
	if got := idx.InboundSources("c"); got != nil {
		t.Errorf("after Remove, InboundSources(c) = %v, want nil", got)
	}
}

// TestMentionIndex_Replace swaps outbound edges for a single source.
func TestMentionIndex_Replace(t *testing.T) {
	idx := newMentionIndex()
	idx.Add("a", "See #x.")
	idx.Replace("a", "See #y instead.")

	if got := idx.InboundSources("x"); got != nil {
		t.Errorf("after Replace, InboundSources(x) = %v, want nil", got)
	}
	gotY := idx.InboundSources("y")
	if len(gotY) != 1 || gotY[0] != "a" {
		t.Errorf("after Replace, InboundSources(y) = %v, want [a]", gotY)
	}
}

// TestMentionIndex_InboundSourcesSorted pins deterministic ordering on the
// inbound direction — sources are returned sorted by ID.
func TestMentionIndex_InboundSourcesSorted(t *testing.T) {
	idx := newMentionIndex()
	// Add in scrambled order; expect sorted result.
	idx.Add("m3", "Refs #t.")
	idx.Add("m1", "Refs #t.")
	idx.Add("m2", "Refs #t.")

	got := idx.InboundSources("t")
	want := []string{"m1", "m2", "m3"}
	if len(got) != len(want) {
		t.Fatalf("InboundSources = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestMentionIndex_Rebuild populates from a nib map.
func TestMentionIndex_Rebuild(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"a": {ID: "a", Body: "Ref #b."},
		"b": {ID: "b", Body: ""},
		"c": {ID: "c", Body: "Also #b and #a."},
	}
	idx := newMentionIndex()
	idx.Rebuild(nibs)

	gotB := idx.InboundSources("b")
	want := []string{"a", "c"}
	if len(gotB) != len(want) {
		t.Fatalf("InboundSources(b) = %v, want %v", gotB, want)
	}
	for i := range want {
		if gotB[i] != want[i] {
			t.Errorf("InboundSources(b)[%d] = %q, want %q", i, gotB[i], want[i])
		}
	}

	gotA := idx.InboundSources("a")
	if len(gotA) != 1 || gotA[0] != "c" {
		t.Errorf("InboundSources(a) = %v, want [c]", gotA)
	}
}
