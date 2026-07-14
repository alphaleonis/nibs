package nibcore

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// mustLoadPrefixedCore builds a fresh Core with the "nibs-" prefix and an
// empty data directory — the common shape across the Core mention tests.
func mustLoadPrefixedCore(t *testing.T) (*Core, string) {
	t.Helper()
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
	return core, nibsDir
}

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

	// Defense-in-depth: 10 subsequent calls must all produce the same
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

// TestCoreFindMentionedBy_IndexServesAfterLoad is the tracer bullet for the
// reverse-mention index wiring. Two nib files land on disk, the Core loads
// them, and FindMentionedBy resolves the inbound relationship without
// relying on body re-parses (the pure-function path is only used as an
// oracle in the differential test below).
func TestCoreFindMentionedBy_IndexServesAfterLoad(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write two nib files directly so we exercise the on-disk load path
	// (not Create). A's body references #B via a full-form token. We use
	// the double-dash filename separator so ParseFilename returns the full
	// prefixed ID rather than splitting "nibs-aaa" into id="nibs"+slug="aaa".
	writeNibFile := func(id, body string) {
		path := filepath.Join(nibsDir, id+"--note.md")
		content := "---\ntitle: " + id + "\nstatus: todo\n---\n" + body + "\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	writeNibFile("nibs-aaa", "See #nibs-bbb for details.")
	writeNibFile("nibs-bbb", "")

	cfg := config.DefaultWithPrefix("nibs-")
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := core.FindMentionedBy("nibs-bbb")
	if len(got) != 1 || got[0].ID != "nibs-aaa" {
		t.Fatalf("FindMentionedBy(nibs-bbb) = %v, want [nibs-aaa]", got)
	}
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

// Behavior 2: Core.Create registers the new source's outbound edges so that
// subsequent FindMentionedBy on the target includes it.
func TestCoreCreate_AddsToMentionIndex(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	if err := core.Create(&nib.Nib{ID: "nibs-bb2", Title: "B", Status: "todo"}); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := core.Create(&nib.Nib{ID: "nibs-aa1", Title: "A", Status: "todo", Body: "See #nibs-bb2."}); err != nil {
		t.Fatalf("create A: %v", err)
	}

	got := core.FindMentionedBy("nibs-bb2")
	if len(got) != 1 || got[0].ID != "nibs-aa1" {
		t.Errorf("FindMentionedBy(nibs-bb2) = %v, want [nibs-aa1]", got)
	}
}

// Behavior 3: Core.Update swaps outbound edges. After updating the source's
// body to mention a different target, the old target is no longer inbound
// and the new target is.
func TestCoreUpdate_SwapsMentionIndexEdges(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	for _, b := range []*nib.Nib{
		{ID: "nibs-xxx", Title: "X", Status: "todo"},
		{ID: "nibs-yyy", Title: "Y", Status: "todo"},
		{ID: "nibs-src", Title: "Src", Status: "todo", Body: "Refs #nibs-xxx."},
	} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}

	// Precondition: X has Src inbound, Y does not.
	if got := core.FindMentionedBy("nibs-xxx"); len(got) != 1 || got[0].ID != "nibs-src" {
		t.Fatalf("pre-update: FindMentionedBy(xxx) = %v, want [nibs-src]", got)
	}
	if got := core.FindMentionedBy("nibs-yyy"); len(got) != 0 {
		t.Fatalf("pre-update: FindMentionedBy(yyy) = %v, want []", got)
	}

	// Update the source's body to mention Y instead of X.
	src, err := core.Get("nibs-src")
	if err != nil {
		t.Fatalf("get src: %v", err)
	}
	src.Body = "Refs #nibs-yyy."
	if err := core.Update(src, nil); err != nil {
		t.Fatalf("update src: %v", err)
	}

	// X should no longer see Src inbound; Y should.
	if got := core.FindMentionedBy("nibs-xxx"); len(got) != 0 {
		t.Errorf("post-update: FindMentionedBy(xxx) = %v, want []", got)
	}
	if got := core.FindMentionedBy("nibs-yyy"); len(got) != 1 || got[0].ID != "nibs-src" {
		t.Errorf("post-update: FindMentionedBy(yyy) = %v, want [nibs-src]", got)
	}
}

// Behavior 4: Core.Delete drops the source from every target's inbound set.
func TestCoreDelete_RemovesFromAllTargetInboundSets(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	for _, b := range []*nib.Nib{
		{ID: "nibs-t1", Title: "T1", Status: "todo"},
		{ID: "nibs-t2", Title: "T2", Status: "todo"},
		{ID: "nibs-t3", Title: "T3", Status: "todo"},
		{ID: "nibs-src", Title: "Src", Status: "todo", Body: "Refs #nibs-t1, #nibs-t2, #nibs-t3."},
	} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}
	// Precondition: each target sees src inbound.
	for _, id := range []string{"nibs-t1", "nibs-t2", "nibs-t3"} {
		if got := core.FindMentionedBy(id); len(got) != 1 || got[0].ID != "nibs-src" {
			t.Fatalf("pre-delete FindMentionedBy(%s) = %v, want [nibs-src]", id, got)
		}
	}

	if err := core.Delete("nibs-src"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, id := range []string{"nibs-t1", "nibs-t2", "nibs-t3"} {
		if got := core.FindMentionedBy(id); len(got) != 0 {
			t.Errorf("post-delete FindMentionedBy(%s) = %v, want []", id, got)
		}
	}
}

// Behavior 5: both short-form and full-form tokens in one body resolve to
// the same target and produce exactly one inbound entry for the source.
func TestCoreFindMentionedBy_ShortAndFullFormsSingleEntry(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	if err := core.Create(&nib.Nib{ID: "nibs-abc", Title: "T", Status: "todo"}); err != nil {
		t.Fatalf("create T: %v", err)
	}
	if err := core.Create(&nib.Nib{ID: "nibs-src", Title: "Src", Status: "todo", Body: "Short #abc and full #nibs-abc."}); err != nil {
		t.Fatalf("create Src: %v", err)
	}

	got := core.FindMentionedBy("nibs-abc")
	count := 0
	for _, b := range got {
		if b.ID == "nibs-src" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("nibs-src appears %d times in FindMentionedBy(nibs-abc), want 1 (got=%v)", count, got)
	}
}

// Behavior 6: self-references are excluded.
func TestCoreFindMentionedBy_ExcludesSelf(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	if err := core.Create(&nib.Nib{ID: "nibs-self", Title: "Self", Status: "todo", Body: "Mention me #self and #nibs-self."}); err != nil {
		t.Fatalf("create self: %v", err)
	}

	if got := core.FindMentionedBy("nibs-self"); got != nil {
		t.Errorf("FindMentionedBy(nibs-self) = %v, want nil (self excluded)", got)
	}
	if got := core.FindMentions("nibs-self"); got != nil {
		t.Errorf("FindMentions(nibs-self) = %v, want nil (self excluded)", got)
	}
}

// Behavior 7: deterministic order preserved — FindMentionedBy returns
// sources sorted by ID. Runs 10x against a 5-source fixture.
func TestCoreFindMentionedBy_DeterministicOrder(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	if err := core.Create(&nib.Nib{ID: "nibs-trg", Title: "T", Status: "todo"}); err != nil {
		t.Fatalf("create trg: %v", err)
	}
	for _, id := range []string{"nibs-m3", "nibs-m5", "nibs-m1", "nibs-m4", "nibs-m2"} {
		if err := core.Create(&nib.Nib{ID: id, Status: "todo", Body: "Ref #nibs-trg."}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	first := core.FindMentionedBy("nibs-trg")
	if len(first) != 5 {
		t.Fatalf("got %d inbound, want 5", len(first))
	}
	firstIDs := make([]string, len(first))
	for i, b := range first {
		firstIDs[i] = b.ID
	}
	// Pin: sorted by ID ascending.
	for i := 1; i < len(firstIDs); i++ {
		if firstIDs[i-1] >= firstIDs[i] {
			t.Errorf("sort drift: %v", firstIDs)
		}
	}
	for i := 0; i < 10; i++ {
		got := core.FindMentionedBy("nibs-trg")
		for j, b := range got {
			if b.ID != firstIDs[j] {
				t.Fatalf("iter %d: got[%d].ID=%q, want %q; full=%v", i, j, b.ID, firstIDs[j], got)
			}
		}
	}
}

// Behavior 8: FindMentions preserves first-appearance order with dedup.
func TestCoreFindMentions_PreservesOrderAndDedupes(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	for _, b := range []*nib.Nib{
		{ID: "nibs-ta", Status: "todo"},
		{ID: "nibs-tb", Status: "todo"},
		{ID: "nibs-tc", Status: "todo"},
		{ID: "nibs-src", Status: "todo", Body: "#nibs-tb then #nibs-tc then #nibs-tb again."},
	} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}

	got := core.FindMentions("nibs-src")
	gotIDs := make([]string, len(got))
	for i, b := range got {
		gotIDs[i] = b.ID
	}
	want := []string{"nibs-tb", "nibs-tc"}
	if len(gotIDs) != len(want) {
		t.Fatalf("got %v, want %v", gotIDs, want)
	}
	for i, w := range want {
		if gotIDs[i] != w {
			t.Errorf("gotIDs[%d] = %q, want %q", i, gotIDs[i], w)
		}
	}
}

// Behavior 9: watcher Write event re-parses the source's edges. Uses
// slugged filenames so ParseFilename round-trips back to the full
// prefixed ID (plain "nibs-src.md" would split into id="nibs", slug="src").
func TestCoreWatcher_WriteReparsesMentionEdges(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	for _, b := range []*nib.Nib{
		{ID: "nibs-t1", Slug: "t1", Status: "todo"},
		{ID: "nibs-t2", Slug: "t2", Status: "todo"},
		{ID: "nibs-src", Slug: "src", Status: "todo", Body: "Ref #nibs-t1."},
	} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching: %v", err)
	}
	defer func() { _ = core.Unwatch() }()
	ch, unsub := core.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)

	// Rewrite src on disk to mention t2 instead of t1.
	srcPath := filepath.Join(nibsDir, "nibs-src--src.md")
	newContent := "---\ntitle: Src\nstatus: todo\n---\nRef #nibs-t2.\n"
	if err := os.WriteFile(srcPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("rewrite src: %v", err)
	}

	// Wait for the watcher to deliver the update event.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for watcher event")
	}

	if got := core.FindMentionedBy("nibs-t1"); len(got) != 0 {
		t.Errorf("after watcher Write, FindMentionedBy(t1) = %v, want []", got)
	}
	if got := core.FindMentionedBy("nibs-t2"); len(got) != 1 || got[0].ID != "nibs-src" {
		t.Errorf("after watcher Write, FindMentionedBy(t2) = %v, want [nibs-src]", got)
	}
}

// Behavior 10: watcher Remove event drops the source from inbound sets.
func TestCoreWatcher_RemoveDropsFromMentionIndex(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	for _, b := range []*nib.Nib{
		{ID: "nibs-tgt", Slug: "tgt", Status: "todo"},
		{ID: "nibs-src", Slug: "src", Status: "todo", Body: "Ref #nibs-tgt."},
	} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching: %v", err)
	}
	defer func() { _ = core.Unwatch() }()
	ch, unsub := core.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)

	// Delete src directly on disk.
	srcPath := filepath.Join(nibsDir, "nibs-src--src.md")
	if err := os.Remove(srcPath); err != nil {
		t.Fatalf("remove: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for watcher delete event")
	}

	if got := core.FindMentionedBy("nibs-tgt"); len(got) != 0 {
		t.Errorf("after watcher Remove, FindMentionedBy(tgt) = %v, want []", got)
	}
}

// Behavior 11: late-bound target. Src mentions a token X that does not yet
// resolve; when a nib with ID X is later Created, FindMentionedBy(X) must
// return src. This validates the "index keyed on raw tokens, not resolved
// IDs" design point.
func TestCoreFindMentionedBy_LateBoundTarget(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	// Src mentions a token whose target does not yet exist.
	if err := core.Create(&nib.Nib{ID: "nibs-src", Status: "todo", Body: "Ref #nibs-late."}); err != nil {
		t.Fatalf("create src: %v", err)
	}
	// No target yet — lookup misses.
	if got := core.FindMentionedBy("nibs-late"); got != nil {
		t.Fatalf("pre-create FindMentionedBy(nibs-late) = %v, want nil (unknown target)", got)
	}

	// Now create the target.
	if err := core.Create(&nib.Nib{ID: "nibs-late", Status: "todo"}); err != nil {
		t.Fatalf("create late: %v", err)
	}

	got := core.FindMentionedBy("nibs-late")
	if len(got) != 1 || got[0].ID != "nibs-src" {
		t.Errorf("post-create FindMentionedBy(nibs-late) = %v, want [nibs-src]", got)
	}
}

// TestCoreFindMentions_Differential_MatchesOracle is a property-style check:
// for a randomized fixture, Core.FindMentions and Core.FindMentionedBy must
// return the same *set* of results as the pure-function oracles over any
// target ID. Catches index-vs-oracle drift.
func TestCoreFindMentions_Differential_MatchesOracle(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	// Build a deterministic RNG so failures are reproducible.
	r := rand.New(rand.NewSource(1))

	// Create 20 nibs with random mention bodies (short or full form) drawn
	// from the same pool. Some tokens will not resolve; that's intentional.
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = "nibs-" + string(rune('a'+i/10)) + string(rune('a'+i%10))
	}
	for _, id := range ids {
		// Body pulls 0–4 tokens from ids (short or full form).
		var parts []string
		n := r.Intn(5)
		for j := 0; j < n; j++ {
			ref := ids[r.Intn(len(ids))]
			if r.Intn(2) == 0 {
				ref = strings.TrimPrefix(ref, "nibs-") // short form
			}
			parts = append(parts, "#"+ref)
		}
		body := strings.Join(parts, " ")
		if err := core.Create(&nib.Nib{ID: id, Status: "todo", Body: body}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	// Take a consistent snapshot of the map outside the locked calls for
	// oracle input. Core.All returns the same pointers as c.nibs.
	all := core.All()
	nibMap := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		nibMap[b.ID] = b
	}

	for _, id := range ids {
		// FindMentions differential — order matters (first-appearance).
		got := core.FindMentions(id)
		want := FindMentionsInMap(nibMap, id, "nibs-")
		if !sameIDSliceOrdered(got, want) {
			t.Errorf("FindMentions(%s) drift: core=%v oracle=%v", id, rawIDList(got), rawIDList(want))
		}
		// FindMentionedBy differential — order matters (sorted ascending).
		gotIn := core.FindMentionedBy(id)
		wantIn := FindMentionedByInMap(nibMap, id, "nibs-")
		if !sameIDSliceOrdered(gotIn, wantIn) {
			t.Errorf("FindMentionedBy(%s) drift: core=%v oracle=%v", id, rawIDList(gotIn), rawIDList(wantIn))
		}
	}
}

// rawIDList returns the IDs in a nib slice preserving slice order. Used
// by the ordered comparator and in error messages so drift is legible.
func rawIDList(bs []*nib.Nib) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ID
	}
	return out
}

// sameIDSliceOrdered compares two nib slices by ID in slice order. Use
// when the spec says "results come back in a specific order"
// (FindMentions: first-appearance order; FindMentionedBy: sorted ascending
// by ID). A future refactor that silently reorders will fail here.
//
// Negative cases (nil vs nil for unknown targets) are asserted inline via
// `== nil` / len() == 0; no separate set-equality helper is needed.
func sameIDSliceOrdered(a, b []*nib.Nib) bool {
	la, lb := rawIDList(a), rawIDList(b)
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if la[i] != lb[i] {
			return false
		}
	}
	return true
}
