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
// fixture. The goldens were captured BEFORE the internal/membership cutover,
// so these tests are the "step-1 output otherwise byte-identical" guarantee of
// nibs-a3fb: the cutover may change none of this output on validator-legal
// data. Regenerate deliberately with NIBS_UPDATE_GOLDENS=1 when an output
// change is intended and reviewed.
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
	roadmapJSON = false
	roadmapIncludeDone = false
	roadmapStatus = nil
	roadmapNoStatus = nil
	roadmapNoLinks = false
	roadmapLinkPrefix = ""
	contextJSON = false
}
