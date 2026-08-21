package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// TestMembershipConsumerFixturePins pins the rendered output of every surface
// that derives container membership — roadmap, context (overview and rooted),
// and the projected children/progress fields — byte-for-byte on the sample
// fixture. The goldens pin the v2-axes semantics: a milestone's members are
// its `milestone:` assignees listed in milestone_order (its progress rolls
// over them while childCount reports 0), and every other container keeps its
// structural decomposition. The five context goldens are byte-identical to
// the pre-cutover captures — the queue preserved the old sibling order — so
// they still witness nibs-a3fb's "output otherwise byte-identical" guarantee.
// Regenerate deliberately with NIBS_UPDATE_GOLDENS=1 when an output change is
// intended and reviewed.
func TestMembershipConsumerFixturePins(t *testing.T) {
	dir := fixtures.CopySampleProject(t)
	nibsPath := filepath.Join(dir, ".nibs")

	cases := []struct {
		name string
		args []string
	}{
		{"roadmap", []string{"--nibs-path", nibsPath, "roadmap", "--no-links"}},
		{"roadmap-include-done", []string{"--nibs-path", nibsPath, "roadmap", "--include-done", "--no-links"}},
		{"roadmap-json", []string{"--nibs-path", nibsPath, "roadmap", "--json"}},
		{"context-overview", []string{"--nibs-path", nibsPath, "context"}},
		{"context-milestone", []string{"--nibs-path", nibsPath, "context", "tnib-m001"}},
		{"context-json", []string{"--nibs-path", nibsPath, "context", "--json"}},
		// The two goldens below were captured by running the PRE-cutover binary
		// (commit bdb7692, the branch base) against the fixture, so they pin
		// base behavior even though they entered the tree after the cutover.
		{"context-epic", []string{"--nibs-path", nibsPath, "context", "tnib-e002"}},
		{"context-milestone-json", []string{"--nibs-path", nibsPath, "context", "tnib-m001", "--json"}},
		{"list-projection", []string{"--nibs-path", nibsPath, "list", "--all", "-f", "id,children,progress"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(resetMembershipPinFlags)
			resetMembershipPinFlags()

			out, err := runRootWith(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v", tc.args, err)
			}

			goldenPath := filepath.Join("testdata", "membership_pin_"+tc.name+".golden")
			if os.Getenv("NIBS_UPDATE_GOLDENS") == "1" {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden: %v — run with NIBS_UPDATE_GOLDENS=1 to capture it", err)
			}
			if out != string(want) {
				t.Errorf("output diverged from the pinned pre-membership rendering.\n--- got ---\n%s\n--- want ---\n%s", out, want)
			}
		})
	}
}

// resetMembershipPinFlags clears the package-level flag state the pinned
// commands share, so one case's flags cannot leak into the next run.
func resetMembershipPinFlags() {
	resetListFlags()
	roadmapJSON = false
	roadmapIncludeDone = false
	roadmapStatus = nil
	roadmapNoStatus = nil
	roadmapNoLinks = false
	roadmapLinkPrefix = ""
	contextJSON = false
}
