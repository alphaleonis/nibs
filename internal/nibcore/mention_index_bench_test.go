package nibcore

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// buildMentionFixture constructs a fixture of n nibs where each nib's body
// includes up to mentionFanout tokens drawn from the full ID pool. Random
// seed is fixed so comparisons across runs are reproducible.
func buildMentionFixture(b *testing.B, n, mentionFanout int) (map[string]*nib.Nib, string) {
	b.Helper()
	r := rand.New(rand.NewSource(42))
	const prefix = "nibs-"

	ids := make([]string, n)
	for i := range ids {
		// Pad a 3-char suffix; with n <= 999 this is unique.
		ids[i] = prefix + padSuffix(i, 3)
	}
	nibs := make(map[string]*nib.Nib, n)
	for _, id := range ids {
		var parts []string
		k := r.Intn(mentionFanout + 1)
		for j := 0; j < k; j++ {
			ref := ids[r.Intn(len(ids))]
			if r.Intn(2) == 0 {
				ref = strings.TrimPrefix(ref, prefix)
			}
			parts = append(parts, "#"+ref)
		}
		body := strings.Join(parts, " ")
		nibs[id] = &nib.Nib{ID: id, Status: "todo", Body: body}
	}
	// Pick a target ID with known inbound fan-in.
	target := ids[0]
	return nibs, target
}

func padSuffix(n, width int) string {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = byte('a' + n%26)
		n /= 26
	}
	return string(out)
}

// BenchmarkFindMentionedBy_WithIndex measures the indexed Core path.
func BenchmarkFindMentionedBy_WithIndex(b *testing.B) {
	nibs, target := buildMentionFixture(b, 500, 5)
	cfg := config.DefaultWithPrefix("nibs-")

	// Route through New() so this bench stays resilient to Core struct
	// field drift — a new required field added to Core gets initialised
	// correctly without touching this file. We still skip Load() (and
	// therefore disk I/O) by manually wiring the synthetic nib map.
	core := New(b.TempDir(), cfg)
	core.SetWarnWriter(nil)
	core.nibs = nibs
	core.mentionIdx.Rebuild(nibs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = core.FindMentionedBy(target)
	}
}

// BenchmarkFindMentionedBy_PureFunction measures the old O(N × body) path
// (the pure-function oracle that still exists in the package).
func BenchmarkFindMentionedBy_PureFunction(b *testing.B) {
	nibs, target := buildMentionFixture(b, 500, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FindMentionedByInMap(nibs, target, "nibs-")
	}
}
