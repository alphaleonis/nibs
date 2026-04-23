package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/spf13/pflag"
)

// resetShowFlags clears the package-level flag vars used by showCmd so tests
// don't pollute each other via rootCmd's singleton state. Also clears
// Cobra's "Changed" tracking, which the MarkFlagsMutuallyExclusive check
// consults — without this reset, a second Execute on rootCmd would see
// previously-set flags as still "changed" and reject any mutex-grouped
// flag from a later invocation.
func resetShowFlags() {
	showJSON = false
	showRaw = false
	showBodyOnly = false
	showETagOnly = false
	showCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupShowCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs(["--nibs-path", dir, "show", ...])` can drive the full
// Cobra pipeline.
func setupShowCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetShowFlags)
	resetShowFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// showMentionsFixture is the file set used by the mention-surfacing tests.
//   - a1 mentions b2 and c3 (outbound-only: mentions b2 and c3; no inbound).
//   - b2 has no body references (pure inbound target).
//   - c3 mentions a1 (has both outbound and inbound).
//   - d4 mentions a1 (pure outbound).
//   - solo has neither outbound nor inbound mentions.
func showMentionsFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
		"c3--gamma.md": "---\ntitle: Gamma\nstatus: todo\ntype: task\n---\n\nBackref to #a1.\n",
		"d4--delta.md": "---\ntitle: Delta\nstatus: todo\ntype: task\n---\n\nAlso mentions #a1.\n",
		"solo--solo.md": "---\ntitle: Solo\nstatus: todo\ntype: task\n---\n\nNo refs here.\n",
	}
}

func TestShowCommand_HumanOutput_OutboundOnly(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	// d4 mentions a1 and has no inbound mentions (pure outbound). Human
	// output goes via fmt.Println (os.Stdout), so we need captureStdout to
	// observe the styled relationship lines.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "d4"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show failed: %v", err)
		}
	})
	if !strings.Contains(out, "mentions:") {
		t.Errorf("expected 'mentions:' label in human output, got:\n%s", out)
	}
	if !strings.Contains(out, "a1") {
		t.Errorf("expected outbound target 'a1' in human output, got:\n%s", out)
	}
	if strings.Contains(out, "mentioned by") {
		t.Errorf("expected NO 'mentioned by' label when inbound is empty, got:\n%s", out)
	}
}

func TestShowCommand_HumanOutput_InboundOnly(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	// b2 is pure inbound — it has no body refs, but a1 mentions it.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "b2"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show failed: %v", err)
		}
	})
	if !strings.Contains(out, "mentioned by") {
		t.Errorf("expected 'mentioned by' label, got:\n%s", out)
	}
	if !strings.Contains(out, "a1") {
		t.Errorf("expected inbound mentioner 'a1' in output, got:\n%s", out)
	}
	// No outbound — ensure we don't spuriously render a "mentions:" line.
	// "mentioned by" contains "mentions" as a substring, so normalise.
	withoutInbound := strings.ReplaceAll(out, "mentioned by", "")
	if strings.Contains(withoutInbound, "mentions:") {
		t.Errorf("expected NO 'mentions:' label when outbound is empty, got:\n%s", out)
	}
}

func TestShowCommand_HumanOutput_BothInboundAndOutbound(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	// a1 mentions b2 and c3; is mentioned by c3 and d4.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show failed: %v", err)
		}
	})
	if !strings.Contains(out, "mentions:") {
		t.Errorf("expected 'mentions:' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "mentioned by") {
		t.Errorf("expected 'mentioned by' in output, got:\n%s", out)
	}
	for _, id := range []string{"b2", "c3", "d4"} {
		if !strings.Contains(out, id) {
			t.Errorf("expected id %q in output, got:\n%s", id, out)
		}
	}
}

func TestShowCommand_HumanOutput_Neither(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "solo"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show failed: %v", err)
		}
	})
	if strings.Contains(out, "mentions:") {
		t.Errorf("expected NO 'mentions:' label for nib with no refs, got:\n%s", out)
	}
	if strings.Contains(out, "mentioned by") {
		t.Errorf("expected NO 'mentioned by' label for nib with no refs, got:\n%s", out)
	}
}

func TestShowCommand_JSONOutput_IncludesMentions(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --json failed: %v", execErr)
	}

	var envelope struct {
		ID          string     `json:"id"`
		Title       string     `json:"title"`
		Mentions    []*nib.Nib `json:"mentions"`
		MentionedBy []*nib.Nib `json:"mentioned_by"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}

	if envelope.ID != "a1" {
		t.Errorf("envelope.ID = %q, want a1", envelope.ID)
	}
	// a1 mentions b2, c3.
	gotOut := idSet(envelope.Mentions)
	if !gotOut["b2"] || !gotOut["c3"] || len(gotOut) != 2 {
		t.Errorf("mentions = %v, want {b2, c3}", gotOut)
	}
	// a1 is mentioned by c3, d4.
	gotIn := idSet(envelope.MentionedBy)
	if !gotIn["c3"] || !gotIn["d4"] || len(gotIn) != 2 {
		t.Errorf("mentioned_by = %v, want {c3, d4}", gotIn)
	}
}

func TestShowCommand_JSONOutput_EmptyMentions(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "solo", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --json failed: %v", execErr)
	}

	// Empty mention slices must be omitted from JSON (omitempty). Verify by
	// confirming the raw output doesn't carry "mentions": or "mentioned_by":
	// entries.
	if strings.Contains(out, `"mentions":`) {
		t.Errorf("expected 'mentions' field omitted for empty mentions, got:\n%s", out)
	}
	if strings.Contains(out, `"mentioned_by":`) {
		t.Errorf("expected 'mentioned_by' field omitted for empty inbound, got:\n%s", out)
	}
}

func TestShowCommand_BodyOnly_Unchanged(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--body-only"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show --body-only failed: %v", err)
		}
	})
	if !strings.Contains(out, "#b2") {
		t.Errorf("expected raw body with '#b2' intact, got:\n%s", out)
	}
	// No frontmatter markers.
	if strings.Contains(out, "mentions:") {
		t.Errorf("--body-only should not include the mentions relationship line, got:\n%s", out)
	}
}

func TestShowCommand_ETagOnly_Unchanged(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--etag-only"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show --etag-only failed: %v", err)
		}
	})
	// ETag is 16 hex chars.
	trimmed := strings.TrimSpace(out)
	if len(trimmed) != 16 {
		t.Errorf("expected 16-char etag, got %q (len=%d)", trimmed, len(trimmed))
	}
}

func TestShowCommand_Raw_Unchanged(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--raw"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show --raw failed: %v", err)
		}
	})
	// --raw emits full frontmatter + body; no relationship line injection.
	if !strings.Contains(out, "---\n") {
		t.Errorf("--raw missing frontmatter markers, got:\n%s", out)
	}
	if !strings.Contains(out, "#b2") {
		t.Errorf("--raw missing body content, got:\n%s", out)
	}
	if strings.Contains(out, "mentioned by") {
		t.Errorf("--raw should NOT inject mention relationship line, got:\n%s", out)
	}
}

// idSet collapses a nib slice to a set of IDs for order-agnostic comparison.
func idSet(ns []*nib.Nib) map[string]bool {
	s := map[string]bool{}
	for _, n := range ns {
		s[n.ID] = true
	}
	return s
}
