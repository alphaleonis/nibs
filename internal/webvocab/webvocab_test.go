package webvocab

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGeneratedVocabularyIsFresh is the single pin between the Go vocabulary
// and the web's copy of it: the committed generated file must be byte-identical
// to what the generator renders from the current Go definitions. It replaces
// the two regex scrapers that used to read constants.ts — the copy is
// generated now, not policed, so the only thing left to check is that
// regeneration was not forgotten.
func TestGeneratedVocabularyIsFresh(t *testing.T) {
	rendered, err := Render()
	if err != nil {
		t.Fatalf("Render(): %v", err)
	}
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(OutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", OutputPath, err)
	}
	if string(committed) != rendered {
		t.Fatalf("%s is stale: the Go vocabulary changed without regenerating it — run `task codegen` and commit the result", OutputPath)
	}
}

// TestTypeRanksDeriveFromHierarchy pins the derived container ranks to the
// values the web's hand-written TYPE_RANK carried: leaf types rank 0 and a
// container ranks one above its highest-ranked possible child. A hierarchy
// change moves these on purpose; anything else moving them is a bug in the
// derivation.
func TestTypeRanksDeriveFromHierarchy(t *testing.T) {
	want := map[string]int{
		"milestone": 3,
		"epic":      2,
		"bug":       1,
		"feature":   1,
		"task":      0,
		"research":  0,
	}
	got := typeRanks()
	if len(got) != len(want) {
		t.Fatalf("typeRanks() covers %d types, want %d: %v", len(got), len(want), got)
	}
	for name, rank := range want {
		if got[name] != rank {
			t.Errorf("typeRanks()[%q] = %d, want %d", name, got[name], rank)
		}
	}
}

// TestEstimateLabels pins the labels derived from the estimate descriptions to
// the values the web's hand-written ESTIMATE_LABELS carried.
func TestEstimateLabels(t *testing.T) {
	want := map[string]string{
		"s":  "Small",
		"m":  "Medium",
		"l":  "Large",
		"xl": "Extra Large",
	}
	for name, label := range want {
		if got := estimateLabel(name); got != label {
			t.Errorf("estimateLabel(%q) = %q, want %q", name, got, label)
		}
	}
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod, so the generated file's path does not depend on test nesting depth.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}
