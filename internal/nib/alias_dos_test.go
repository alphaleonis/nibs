package nib

import (
	"fmt"
	"strings"
	"testing"
)

// resolveExtraAliases must fail closed on
// adversarial YAML anchors captured in unknown (Extra) front-matter keys, rather
// than recursing to a stack overflow (cyclic anchor) or exhausting memory
// (billion-laughs fan-out). These inputs are bounded: with the guard in place the
// budget/cycle check fires almost immediately, so the tests are cheap and safe to
// run uncapped. (Reverting the guard makes them OOM/crash — verify that only
// under a memory-capped scope; see scripts/go-test-capped.sh.)

// A self-referential anchor under an unknown (Extra) key must be REJECTED by
// Parse, not crash the process with a stack overflow.
func TestParseRejectsCyclicExtraAnchor(t *testing.T) {
	const in = "---\nversion: 1\ntitle: t\nstatus: todo\ncustom: &a\n  self: *a\n---\n\nBody.\n"
	if _, err := Parse(strings.NewReader(in)); err == nil {
		t.Fatal("Parse accepted a self-referential anchor; want a cyclic-reference error")
	}
}

// An exponential (billion-laughs) alias fan-out must be rejected once it exceeds
// the expansion budget, never exhaust memory. n=40 so the node budget is hit far
// below full expansion — the test's own footprint stays tiny with the guard.
func TestParseRejectsBillionLaughsExtra(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("---\nversion: 1\ntitle: t\nstatus: todo\nl0: &l0 [x, x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	sb.WriteString("top: *l40\n---\n\nBody.\n")

	if _, err := Parse(strings.NewReader(sb.String())); err == nil {
		t.Fatal("Parse accepted a billion-laughs alias fan-out; want an expansion-limit error")
	}
}

// A front-matter block with a very large number of keys must be REJECTED by
// Parse (a fast, capped error), not decoded — yaml.v3's decode into a Go
// map/struct-with-inline-catch-all is O(N²) in the key count, so an unbounded
// many-key file would hang Load under c.mu. The bound (maxFrontMatterKeys) sits
// far above any real nib (a handful of keys) but caps the quadratic cost to
// trivial. Generation is bounded (a few thousand short keys) so the test's own
// footprint stays tiny.
func TestParseRejectsManyKeyFrontMatter(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("---\nversion: 1\ntitle: t\nstatus: todo\n")
	// Well above maxFrontMatterKeys, but short keys keep the raw block under
	// maxFrontMatterBytes so this exercises the KEY-count bound (not the byte bound).
	for i := 0; i < maxFrontMatterKeys+2000; i++ {
		fmt.Fprintf(&sb, "k%06d: v\n", i)
	}
	sb.WriteString("---\n\nBody.\n")

	_, err := Parse(strings.NewReader(sb.String()))
	if err == nil {
		t.Fatal("Parse accepted a front-matter block with an excessive key count; want a capped error")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("error should mention the key-count limit; got: %v", err)
	}
}

// A front-matter block whose raw bytes exceed maxFrontMatterBytes must be
// rejected before the decode even runs, regardless of key count.
func TestParseRejectsOversizedFrontMatter(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("---\nversion: 1\ntitle: t\nstatus: todo\n")
	// A single unknown key with a value larger than the byte bound.
	sb.WriteString("blob: ")
	sb.WriteString(strings.Repeat("x", maxFrontMatterBytes+1024))
	sb.WriteString("\n---\n\nBody.\n")

	_, err := Parse(strings.NewReader(sb.String()))
	if err == nil {
		t.Fatal("Parse accepted a front-matter block exceeding the byte limit; want a capped error")
	}
	if !strings.Contains(err.Error(), "byte") {
		t.Errorf("error should mention the byte limit; got: %v", err)
	}
}

// A merge-key (`<<`) amplified front-matter block must NOT bypass the DoS
// bounds. countMappingKeys does not expand `<<`, so the key-count guard
// undercounts a merge-amplified document — but the amplification is bounded
// elsewhere: yaml.v3 implements `<<` as alias traversal, so its built-in
// alias-ratio guard trips during the real struct decode, and any merge chains
// captured under unknown (Extra) keys are additionally bounded by
// resolveExtraAliases. This pins that such a document is REJECTED (by whichever
// guard fires first) rather than silently expanding to exhaust memory. Run under
// the memory-capped scope (scripts/go-test-capped.sh): with the guards in place
// the rejection is near-immediate and cheap, but reverting them makes it blow up.
func TestParseRejectsMergeKeyExpansion(t *testing.T) {
	// A billion-laughs built from merge keys: each level merges the previous
	// anchored mapping into itself twice, and the top level merges the deepest
	// one. Expanding this fully is exponential; the alias-ratio guard aborts long
	// before that (it needs only aliasCount>100 && decodeCount>1000, reached
	// around ~10 doubling levels). n=32 gives ample margin while the guard still
	// stops it early.
	var sb strings.Builder
	sb.WriteString("---\nversion: 1\ntitle: t\nstatus: todo\n")
	sb.WriteString("l0: &l0\n  a: 1\n  b: 1\n")
	for i := 1; i <= 32; i++ {
		fmt.Fprintf(&sb, "l%d: &l%d\n  <<: [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	// A top-level merge forces yaml.v3's real merge expansion (into the frontMatter
	// mapping), exercising the alias-ratio guard on the decode path.
	sb.WriteString("<<: *l32\n---\n\nBody.\n")

	if _, err := Parse(strings.NewReader(sb.String())); err == nil {
		t.Fatal("Parse accepted a merge-key-amplified front matter; want rejection by the alias-ratio or expansion guard")
	}
}

// A benign alias (no cycle, small fan-out) must still resolve to its concrete
// value — the DoS guard must not reject legitimate anchors.
func TestParseResolvesBenignExtraAlias(t *testing.T) {
	const in = "---\nversion: 1\ntitle: t\nstatus: todo\nbase: &b hello\necho: *b\n---\n\nBody.\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse rejected a benign alias: %v", err)
	}
	n, ok := got.Extra["echo"]
	if !ok {
		t.Fatalf("Extra[echo] missing after resolution; Extra=%v", got.Extra)
	}
	if n.Value != "hello" {
		t.Errorf("Extra[echo] = %q; want resolved scalar %q", n.Value, "hello")
	}
	// The resolved copy must carry no residual anchor/alias state.
	if n.Anchor != "" {
		t.Errorf("Extra[echo] retained anchor %q; want it stripped", n.Anchor)
	}
}
