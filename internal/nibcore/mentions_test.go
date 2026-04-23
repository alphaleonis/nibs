package nibcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestFindMentionsInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"nibs-aaa1": {ID: "nibs-aaa1", Title: "A", Status: "todo", Body: "See #bbb2 and #nibs-ccc3 for details."},
		"nibs-bbb2": {ID: "nibs-bbb2", Title: "B", Status: "todo", Body: "No refs."},
		"nibs-ccc3": {ID: "nibs-ccc3", Title: "C", Status: "completed", Body: "Backref to #aaa1."},
		"nibs-ddd4": {ID: "nibs-ddd4", Title: "D", Status: "todo", Body: "Self ref #ddd4 and missing #nope1."},
	}

	t.Run("resolves short and full form in same body", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "nibs-aaa1", "nibs-")
		if len(got) != 2 {
			t.Fatalf("got %d mentions, want 2", len(got))
		}
		if got[0].ID != "nibs-bbb2" || got[1].ID != "nibs-ccc3" {
			t.Errorf("order/IDs = %s, %s; want nibs-bbb2, nibs-ccc3", got[0].ID, got[1].ID)
		}
	})

	t.Run("self-reference excluded", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "nibs-ddd4", "nibs-")
		if len(got) != 0 {
			t.Errorf("got %d mentions, want 0 (self + unresolved only)", len(got))
		}
	})

	t.Run("empty body returns nil", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "nibs-bbb2", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("unknown fromID returns nil", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "does-not-exist", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("empty prefix — exact-match resolution plus self-exclusion", func(t *testing.T) {
		// With configPrefix == "", every stored id is full-form by definition
		// (no prefix to prepend), so only the exact-match branch of
		// resolveMentionToken runs. The real behavior this subtest pins is
		// that the "from" nib's own mention of itself is dropped.
		noPrefix := map[string]*nib.Nib{
			"abc1": {ID: "abc1", Body: "ref #def2 and #abc1 self"},
			"def2": {ID: "def2", Body: ""},
		}
		got := FindMentionsInMap(noPrefix, "abc1", "")
		if len(got) != 1 || got[0].ID != "def2" {
			t.Errorf("got %v, want [def2]", got)
		}
	})
}

func TestFindMentionedByInMap(t *testing.T) {
	// nibs-ddd4's body uses *two distinct tokens* (#bbb2 short-form and
	// #nibs-bbb2 full-form) that both resolve to the same target. This
	// makes ExtractMentionTokens return two tokens, so the outer
	// per-source dedup (or the `break` after a first match) is what
	// keeps nibs-ddd4 from appearing twice in the result — exercising
	// the guarantee the "counts once" subtest below claims to test.
	nibs := map[string]*nib.Nib{
		"nibs-aaa1": {ID: "nibs-aaa1", Title: "A", Status: "todo", Body: "Refs #bbb2."},
		"nibs-bbb2": {ID: "nibs-bbb2", Title: "B", Status: "todo", Body: "No refs."},
		"nibs-ccc3": {ID: "nibs-ccc3", Title: "C", Status: "todo", Body: "Also see #nibs-bbb2."},
		"nibs-ddd4": {ID: "nibs-ddd4", Title: "D", Status: "todo", Body: "Mentions #bbb2 (short) and again #nibs-bbb2 (full)."},
	}

	t.Run("returns all inbound mentioners", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-bbb2", "nibs-")
		if len(got) != 3 {
			t.Fatalf("got %d, want 3", len(got))
		}
		ids := map[string]bool{}
		for _, b := range got {
			ids[b.ID] = true
		}
		for _, want := range []string{"nibs-aaa1", "nibs-ccc3", "nibs-ddd4"} {
			if !ids[want] {
				t.Errorf("missing %s from mentionedBy", want)
			}
		}
	})

	t.Run("two distinct tokens resolving to same target count source once", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-bbb2", "nibs-")
		count := 0
		for _, b := range got {
			if b.ID == "nibs-ddd4" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("nibs-ddd4 appears %d times, want 1", count)
		}
	})

	// Per-source uniqueness invariant: FindMentionedByInMap returns at
	// most one entry per source ID, regardless of how many tokens that
	// source used to mention the target. Testing the invariant directly
	// is more durable than testing the mechanism — `break`, an outer
	// slice-level dedup, a map-based collector, or any future refactor
	// must all preserve this.
	t.Run("per-source uniqueness invariant (at most one entry per source ID)", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-bbb2", "nibs-")
		seen := make(map[string]bool)
		for _, b := range got {
			if seen[b.ID] {
				t.Errorf("source %q appears more than once in inbound result", b.ID)
			}
			seen[b.ID] = true
		}
	})

	t.Run("target with no inbound mentions", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-aaa1", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("unknown target returns nil", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "does-not-exist", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("self-references excluded from inbound", func(t *testing.T) {
		self := map[string]*nib.Nib{
			"nibs-self": {ID: "nibs-self", Body: "I mention myself: #self and #nibs-self."},
		}
		got := FindMentionedByInMap(self, "nibs-self", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil (self-ref excluded)", got)
		}
	})
}

func TestFindMentionedByInMap_DeterministicOrder(t *testing.T) {
	// Five distinct mentioners each reference the same target via a
	// unique token. Map iteration in Go is randomized per-call, so if the
	// function does not sort its result the order will vary across calls.
	nibs := map[string]*nib.Nib{
		"nibs-target": {ID: "nibs-target", Title: "Target", Body: "No refs."},
		"nibs-m1":     {ID: "nibs-m1", Title: "M1", Body: "Refs #target (alpha)."},
		"nibs-m2":     {ID: "nibs-m2", Title: "M2", Body: "See #nibs-target as well."},
		"nibs-m3":     {ID: "nibs-m3", Title: "M3", Body: "Also #target."},
		"nibs-m4":     {ID: "nibs-m4", Title: "M4", Body: "And #nibs-target."},
		"nibs-m5":     {ID: "nibs-m5", Title: "M5", Body: "Plus #target again."},
	}

	first := FindMentionedByInMap(nibs, "nibs-target", "nibs-")
	if len(first) != 5 {
		t.Fatalf("expected 5 inbound mentioners, got %d", len(first))
	}

	firstIDs := make([]string, len(first))
	for i, b := range first {
		firstIDs[i] = b.ID
	}

	// Pin the contract: result is sorted by ID ascending. Without this
	// assertion, a refactor that sorted by title, insertion time, or any
	// other stable-but-non-ID key would still pass the 10-call
	// determinism check below.
	for i := 1; i < len(first); i++ {
		if firstIDs[i-1] >= firstIDs[i] {
			t.Errorf("expected sort by ID asc; got[%d].ID=%q >= got[%d].ID=%q",
				i-1, firstIDs[i-1], i, firstIDs[i])
		}
	}

	// Defence-in-depth: 10 subsequent calls must all produce the same
	// order. Catches a future regression where the sort is dropped and
	// Go's randomized map iteration surfaces through.
	for i := 0; i < 10; i++ {
		got := FindMentionedByInMap(nibs, "nibs-target", "nibs-")
		if len(got) != len(first) {
			t.Fatalf("iter %d: len = %d, want %d", i, len(got), len(first))
		}
		gotIDs := make([]string, len(got))
		for j, b := range got {
			gotIDs[j] = b.ID
		}
		for j := range gotIDs {
			if gotIDs[j] != firstIDs[j] {
				t.Errorf("iter %d: order drifted at index %d: got %s, want %s\nfull: %v\nfirst: %v",
					i, j, gotIDs[j], firstIDs[j], gotIDs, firstIDs)
				break
			}
		}
	}
}

func TestCoreFindMentions(t *testing.T) {
	core, _ := setupTestCore(t)

	nibA := &nib.Nib{ID: "aaa1", Title: "A", Status: "todo", Body: "See #bbb2."}
	nibB := &nib.Nib{ID: "bbb2", Title: "B", Status: "todo"}
	for _, b := range []*nib.Nib{nibA, nibB} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Run("outbound", func(t *testing.T) {
		got := core.FindMentions("aaa1")
		if len(got) != 1 || got[0].ID != "bbb2" {
			t.Errorf("got %v, want [bbb2]", got)
		}
	})

	t.Run("inbound", func(t *testing.T) {
		got := core.FindMentionedBy("bbb2")
		if len(got) != 1 || got[0].ID != "aaa1" {
			t.Errorf("got %v, want [aaa1]", got)
		}
	})
}

// TestCoreFindMentions_ShortIDNormalization verifies that short IDs
// (without the configured prefix) resolve to the same result as full IDs —
// matching the contract established by Core.Get / Core.NormalizeID.
func TestCoreFindMentions_ShortIDNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.DefaultWithPrefix("nibs-")
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	nibA := &nib.Nib{ID: "nibs-aa1", Title: "A", Status: "todo", Body: "See #bb2."}
	nibB := &nib.Nib{ID: "nibs-bb2", Title: "B", Status: "todo"}
	for _, b := range []*nib.Nib{nibA, nibB} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Run("FindMentions accepts short ID", func(t *testing.T) {
		got := core.FindMentions("aa1")
		if len(got) != 1 || got[0].ID != "nibs-bb2" {
			t.Errorf("short-ID FindMentions = %v, want [nibs-bb2]", got)
		}
	})

	t.Run("FindMentionedBy accepts short ID", func(t *testing.T) {
		got := core.FindMentionedBy("bb2")
		if len(got) != 1 || got[0].ID != "nibs-aa1" {
			t.Errorf("short-ID FindMentionedBy = %v, want [nibs-aa1]", got)
		}
	})

	t.Run("unknown ID returns nil on both", func(t *testing.T) {
		if got := core.FindMentions("nope"); got != nil {
			t.Errorf("FindMentions(nope) = %v, want nil", got)
		}
		if got := core.FindMentionedBy("nope"); got != nil {
			t.Errorf("FindMentionedBy(nope) = %v, want nil", got)
		}
	})
}
