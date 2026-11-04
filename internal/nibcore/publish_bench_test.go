package nibcore

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// publishBenchCore returns a Core holding a 5000-nib working copy with up to five
// mentions per body, not yet published.
func publishBenchCore(b *testing.B) (*Core, string) {
	b.Helper()
	nibs, target := buildMentionFixture(b, 5000, 5)
	core := New(b.TempDir(), config.DefaultWithPrefix("nibs-"))
	core.SetWarnWriter(nil)
	core.nibs = nibs
	core.mentionIdx.Rebuild(nibs)
	return core, target
}

// BenchmarkPublish measures publishLocked alone, the copy every writer pays once
// per critical section.
func BenchmarkPublish(b *testing.B) {
	core, _ := publishBenchCore(b)
	for b.Loop() {
		core.publishLocked()
	}
}

// BenchmarkPublishAfterOneEdit adds the working-copy changes of a one-nib
// Update, so the mention index clones the inbound sets it shares with the last
// publish.
func BenchmarkPublishAfterOneEdit(b *testing.B) {
	core, target := publishBenchCore(b)
	core.publishLocked()
	edited := core.nibs[target].Clone()
	edited.Body = "#nibs-aab #nibs-aac #aad"
	for b.Loop() {
		core.nibs[target] = edited
		core.mentionIdx.Replace(target, edited.Body)
		core.publishLocked()
	}
}
