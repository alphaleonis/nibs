package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
	showActive = false
	showNoMentions = false
	showCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// TestResetShowFlagsClearsAllState walks showCmd's Cobra FlagSet and verifies
// that every registered flag is at its documented default after resetShowFlags
// runs. Unlike a hand-enumerated check, this catches additive drift: if a new
// flag is registered on showCmd but resetShowFlags doesn't clear its backing
// var, the post-reset value will diverge from DefValue and this test fires.
// Also verifies Cobra's Changed state is cleared — MarkFlagsMutuallyExclusive
// relies on it, and stale Changed state across tests would silently break the
// mutex check.
func TestResetShowFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetShowFlags)

	// Dirty every flag via the real FlagSet so Cobra's `actual` map is
	// populated — Set() is the only way to do this, and it also
	// exercises the MarkFlagsMutuallyExclusive Changed-tracking path.
	// The four format flags are mutually exclusive at Execute() time
	// but not at Set() time, so this is safe. --active and
	// --no-mentions are not in any mutex group.
	dirty := map[string]string{
		"json":        "true",
		"raw":         "true",
		"body-only":   "true",
		"etag-only":   "true",
		"active":      "true",
		"no-mentions": "true",
	}
	for name, val := range dirty {
		if err := showCmd.Flags().Set(name, val); err != nil {
			t.Fatalf("pre-populate --%s: %v", name, err)
		}
	}

	resetShowFlags()

	showCmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("flag %q = %q after reset, want default %q",
				f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("flag %q Changed = true after reset, want false", f.Name)
		}
	})
}

// setupShowCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs(["--nibs-path", dir, "show", ...])` can drive the full
// Cobra pipeline.
func setupShowCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetShowFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
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
		"a1--alpha.md":  "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3.\n",
		"b2--beta.md":   "---\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
		"c3--gamma.md":  "---\ntitle: Gamma\nstatus: todo\ntype: task\n---\n\nBackref to #a1.\n",
		"d4--delta.md":  "---\ntitle: Delta\nstatus: todo\ntype: task\n---\n\nAlso mentions #a1.\n",
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
	// Slice the output at section labels and assert IDs appear in the
	// correct section — guards against a regression that swaps the two
	// lists. "mentions:" appears first, "mentioned by" second.
	iOut := strings.Index(out, "mentions:")
	iIn := strings.Index(out, "mentioned by")
	if iOut < 0 || iIn < 0 || iOut > iIn {
		t.Fatalf("expected 'mentions:' before 'mentioned by'; got:\n%s", out)
	}
	outSection := out[iOut:iIn]
	inSection := out[iIn:]
	for _, id := range []string{"b2", "c3"} {
		if !strings.Contains(outSection, id) {
			t.Errorf("outbound 'mentions:' section missing %q; got:\n%s", id, outSection)
		}
	}
	for _, id := range []string{"c3", "d4"} {
		if !strings.Contains(inSection, id) {
			t.Errorf("inbound 'mentioned by' section missing %q; got:\n%s", id, inSection)
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

	// mentions/mentioned_by are emitted as ID arrays (parallel to
	// blocked_by), not full nib objects.
	var envelope struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		ETag        string   `json:"etag"`
		Mentions    []string `json:"mentions"`
		MentionedBy []string `json:"mentioned_by"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}

	if envelope.ID != "a1" {
		t.Errorf("envelope.ID = %q, want a1", envelope.ID)
	}
	// Etag must be a 16-char hex string. If someone "simplifies" the
	// MarshalJSON chain and drops the Nib's own MarshalJSON, the etag
	// field vanishes and this assertion fires. The error message is
	// intentionally verbose to teach the future maintainer why the
	// merge-through-map approach exists.
	matched, _ := regexp.MatchString(`^[0-9a-f]{16}$`, envelope.ETag)
	if !matched {
		t.Errorf("expected 16-char hex etag in show --json envelope, got %q (len=%d) — did the MarshalJSON chain get simplified and drop the Nib's own MarshalJSON?", envelope.ETag, len(envelope.ETag))
	}
	// a1 mentions b2, c3.
	gotOut := map[string]bool{}
	for _, id := range envelope.Mentions {
		gotOut[id] = true
	}
	if !gotOut["b2"] || !gotOut["c3"] || len(gotOut) != 2 {
		t.Errorf("mentions = %v, want {b2, c3}", gotOut)
	}
	// a1 is mentioned by c3, d4.
	gotIn := map[string]bool{}
	for _, id := range envelope.MentionedBy {
		gotIn[id] = true
	}
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

	// mentions/mentioned_by are ALWAYS emitted — empty arrays (never
	// null, never absent) so agent consumers get a stable shape.
	if !strings.Contains(out, `"mentions": []`) {
		t.Errorf("expected `\"mentions\": []` for a nib with no outbound refs, got:\n%s", out)
	}
	if !strings.Contains(out, `"mentioned_by": []`) {
		t.Errorf("expected `\"mentioned_by\": []` for a nib with no inbound refs, got:\n%s", out)
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
	// No frontmatter markers and no injected relationship lines.
	if strings.Contains(out, "mentions:") {
		t.Errorf("--body-only should not include the mentions relationship line, got:\n%s", out)
	}
	if strings.Contains(out, "mentioned by") {
		t.Errorf("--body-only should not include the 'mentioned by' relationship line, got:\n%s", out)
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
	matched, _ := regexp.MatchString(`^[0-9a-f]{16}$`, trimmed)
	if !matched {
		t.Errorf("expected 16-char hex etag, got %q (len=%d)", trimmed, len(trimmed))
	}
}

// showActiveFixture has the same relationship graph as showMentionsFixture
// but with statuses varied across todo/completed/scrapped so --active tests
// can verify resolved-status filtering on both inbound and outbound sides.
//
//   - a1 (todo) mentions b2, c3, d4.
//   - b2 (todo) — active mention target.
//   - c3 (completed) mentions a1 (completed mentioner).
//   - d4 (scrapped) mentions a1 (scrapped mentioner).
//   - e5 (todo) mentions a1 (active mentioner).
//
// This produces a1 with outbound {b2 todo, c3 completed, d4 scrapped}
// and inbound {c3 completed, d4 scrapped, e5 todo}. --active should
// narrow both to just the todo entries.
func showActiveFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md":   "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3 and #d4.\n",
		"b2--beta.md":    "---\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
		"c3--gamma.md":   "---\ntitle: Gamma\nstatus: completed\ntype: task\n---\n\nBackref to #a1.\n",
		"d4--delta.md":   "---\ntitle: Delta\nstatus: scrapped\ntype: task\n---\n\nAlso mentions #a1.\n",
		"e5--epsilon.md": "---\ntitle: Epsilon\nstatus: todo\ntype: task\n---\n\nRefs #a1.\n",
	}
}

func TestShowCommand_NoMentionsJSON_EmptyArrays(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	// a1 normally has outbound {b2, c3} and inbound {c3, d4}. With
	// --no-mentions, both fields must drop to empty arrays (still
	// emitted, never null/absent — empty-array-always contract).
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--no-mentions", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --no-mentions --json failed: %v", execErr)
	}

	var envelope struct {
		ID          string   `json:"id"`
		Mentions    []string `json:"mentions"`
		MentionedBy []string `json:"mentioned_by"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if envelope.ID != "a1" {
		t.Errorf("envelope.ID = %q, want a1", envelope.ID)
	}
	if len(envelope.Mentions) != 0 {
		t.Errorf("--no-mentions: mentions = %v, want []", envelope.Mentions)
	}
	if len(envelope.MentionedBy) != 0 {
		t.Errorf("--no-mentions: mentioned_by = %v, want []", envelope.MentionedBy)
	}
	// Empty-array-always contract: the keys must be present as [].
	if !strings.Contains(out, `"mentions": []`) {
		t.Errorf("expected `\"mentions\": []` in --no-mentions output, got:\n%s", out)
	}
	if !strings.Contains(out, `"mentioned_by": []`) {
		t.Errorf("expected `\"mentioned_by\": []` in --no-mentions output, got:\n%s", out)
	}
}

func TestShowCommand_ActiveJSON_OutboundFiltersResolved(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showActiveFixture())

	// a1 mentions b2 (todo), c3 (completed), d4 (scrapped). --active must
	// leave only b2 in outbound mentions.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--active", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --active --json failed: %v", execErr)
	}
	var envelope struct {
		ID       string   `json:"id"`
		Mentions []string `json:"mentions"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	ids := map[string]bool{}
	for _, id := range envelope.Mentions {
		ids[id] = true
	}
	if !ids["b2"] || len(ids) != 1 {
		t.Errorf("--active outbound: got %v, want {b2} only (c3 completed and d4 scrapped filtered)", ids)
	}
}

func TestShowCommand_ActiveJSON_InboundFiltersResolved(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showActiveFixture())

	// a1 is mentioned by c3 (completed), d4 (scrapped), e5 (todo).
	// --active must leave only e5 in mentioned_by.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--active", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --active --json failed: %v", execErr)
	}
	var envelope struct {
		ID          string   `json:"id"`
		MentionedBy []string `json:"mentioned_by"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	ids := map[string]bool{}
	for _, id := range envelope.MentionedBy {
		ids[id] = true
	}
	if !ids["e5"] || len(ids) != 1 {
		t.Errorf("--active inbound: got %v, want {e5} only (c3 completed and d4 scrapped filtered)", ids)
	}
}

func TestShowCommand_ActiveAndNoMentionsJSON_NoMentionsDominates(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showActiveFixture())

	// --no-mentions dominates over --active: both flags set together
	// should still emit empty arrays regardless of mention/status data.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--active", "--no-mentions", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --active --no-mentions --json failed: %v", execErr)
	}
	var envelope struct {
		Mentions    []string `json:"mentions"`
		MentionedBy []string `json:"mentioned_by"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(envelope.Mentions) != 0 {
		t.Errorf("mentions = %v, want [] (--no-mentions dominates --active)", envelope.Mentions)
	}
	if len(envelope.MentionedBy) != 0 {
		t.Errorf("mentioned_by = %v, want [] (--no-mentions dominates --active)", envelope.MentionedBy)
	}
}

func TestShowCommand_NoFlags_IncludesAllStatuses(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showActiveFixture())

	// Without --active, every mention (regardless of resolved status)
	// must appear in the JSON envelope — regression guard for the
	// default behaviour.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("show --json failed: %v", execErr)
	}
	var envelope struct {
		Mentions    []string `json:"mentions"`
		MentionedBy []string `json:"mentioned_by"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	out2 := map[string]bool{}
	for _, id := range envelope.Mentions {
		out2[id] = true
	}
	for _, want := range []string{"b2", "c3", "d4"} {
		if !out2[want] {
			t.Errorf("no-flags outbound: missing %q (default must include resolved statuses); got %v", want, out2)
		}
	}
	in2 := map[string]bool{}
	for _, id := range envelope.MentionedBy {
		in2[id] = true
	}
	for _, want := range []string{"c3", "d4", "e5"} {
		if !in2[want] {
			t.Errorf("no-flags inbound: missing %q (default must include resolved statuses); got %v", want, in2)
		}
	}
}

func TestShowCommand_ActiveHuman_DropsResolvedRows(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showActiveFixture())

	// a1 with --active: outbound b2 (todo) kept, c3/d4 (completed/scrapped)
	// dropped; inbound e5 (todo) kept, c3/d4 dropped.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--active"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show --active failed: %v", err)
		}
	})
	// Slice the output by section labels — same pattern as the
	// BothInboundAndOutbound test — so we can assert each ID appears in
	// the section it belongs to (and resolved IDs are absent from both).
	// Restrict both sections to the single label line (body content
	// below the header also contains `#c3`/`#d4` tokens that would
	// otherwise produce false-positive matches).
	iOut := strings.Index(out, "mentions:")
	iIn := strings.Index(out, "mentioned by")
	if iOut < 0 || iIn < 0 || iOut > iIn {
		t.Fatalf("expected 'mentions:' before 'mentioned by'; got:\n%s", out)
	}
	outSection := out[iOut:iIn]
	// Trim inSection at the next newline so we only look at the label
	// line, not the body that follows.
	inTail := out[iIn:]
	if nl := strings.Index(inTail, "\n"); nl >= 0 {
		inTail = inTail[:nl]
	}
	inSection := inTail

	// Outbound: only b2 should appear.
	if !strings.Contains(outSection, "b2") {
		t.Errorf("outbound 'mentions:' section missing active target 'b2'; got:\n%s", outSection)
	}
	for _, resolved := range []string{"c3", "d4"} {
		if strings.Contains(outSection, resolved) {
			t.Errorf("outbound 'mentions:' section must drop resolved %q; got:\n%s", resolved, outSection)
		}
	}

	// Inbound: only e5 should appear.
	if !strings.Contains(inSection, "e5") {
		t.Errorf("inbound 'mentioned by' section missing active mentioner 'e5'; got:\n%s", inSection)
	}
	for _, resolved := range []string{"c3", "d4"} {
		if strings.Contains(inSection, resolved) {
			t.Errorf("inbound 'mentioned by' section must drop resolved %q; got:\n%s", resolved, inSection)
		}
	}
}

func TestShowCommand_NoMentionsHuman_SectionsAbsent(t *testing.T) {
	nibsDir := setupShowCobraTest(t, showMentionsFixture())

	// a1 normally renders both "mentions:" and "mentioned by" sections.
	// With --no-mentions, neither header may appear in the human output.
	// Use strings.Index slicing (same pattern as the
	// BothInboundAndOutbound test) to pin absence precisely even though
	// "mentioned by" contains "mentions" as a substring.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "show", "a1", "--no-mentions"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show --no-mentions failed: %v", err)
		}
	})
	// "mentioned by" contains "mentions" as a substring — normalise by
	// stripping the inbound label first, then check that "mentions:"
	// (the outbound section header) is absent.
	withoutInbound := strings.ReplaceAll(out, "mentioned by", "")
	if strings.Contains(withoutInbound, "mentions:") {
		t.Errorf("--no-mentions must suppress outbound 'mentions:' section, got:\n%s", out)
	}
	if strings.Contains(out, "mentioned by") {
		t.Errorf("--no-mentions must suppress inbound 'mentioned by' section, got:\n%s", out)
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
