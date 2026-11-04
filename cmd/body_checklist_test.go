package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/store"
)

const checklistBody = "intro\n\n## Acceptance\n\n- [ ] a\n- [ ] b\n- [ ] c\n\n## Notes\n\n- [ ] n\n"

// runBodyOK runs `nibs body` with args after the store and id, failing the test
// on an error, and returns what it printed to stdout.
func runBodyOK(t *testing.T, nibsDir, id string, args ...string) string {
	t.Helper()
	resetBodyFlags()
	full := append([]string{"--nibs-path", nibsDir, "body", id}, args...)
	out, err := runRootWith(t, full...)
	if err != nil {
		t.Fatalf("body %v failed: %v", args, err)
	}
	return out
}

func TestBodyCheckTicksOnlyNamedItems(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-1", checklistBody)
	before := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id)))

	out := runBodyOK(t, nibsDir, id, "--check", "1,3")

	got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id)))
	want := strings.Replace(strings.Replace(before, "- [ ] a", "- [x] a", 1), "- [ ] c", "- [x] c", 1)
	if got != want {
		t.Errorf("body =\n%q\nwant:\n%q", got, want)
	}
	if wantOut := "1. [x] a\n2. [ ] b\n3. [x] c\n"; out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

func TestBodyChecklistVerbsUsePreEditNumbering(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-2", "## Acceptance\n- [ ] a\n- [ ] b\n- [x] c\n- [ ] d\n- [ ] e\n")

	out := runBodyOK(t, nibsDir, id, "--remove", "1,4", "--check", "2", "--uncheck", "3")

	got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id)))
	if want := "## Acceptance\n- [x] b\n- [ ] c\n- [ ] e\n"; got != want {
		t.Errorf("body =\n%q\nwant:\n%q", got, want)
	}
	if wantOut := "1. [x] b\n2. [ ] c\n3. [ ] e\n"; out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

func TestBodyRemoveTakesNestedLines(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-rm", "## Acceptance\n- [ ] a\n- [ ] b first\n  b continues\n  - [ ] b1\n- [ ] c\n")

	out := runBodyOK(t, nibsDir, id, "--remove", "2")

	if got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id))); got != "## Acceptance\n- [ ] a\n- [ ] c\n" {
		t.Errorf("body = %q", got)
	}
	if wantOut := "1. [ ] a\n2. [ ] c\n"; out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

func TestBodyCheckRepeatedFlagActsLikeAList(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-rep", "## Acceptance\n- [ ] a\n- [ ] b\n- [ ] c\n")

	out := runBodyOK(t, nibsDir, id, "--check", "1", "--check", "3")

	if wantOut := "1. [x] a\n2. [ ] b\n3. [x] c\n"; out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

// Removing an item that holds a fenced block is refused, so an unclosed fence
// cannot carry the removal into later items and sections.
func TestBodyRemoveRefusesAnItemHoldingAFence(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()
	nibsDir, id := writeSetNib(t, "bdy-chk-rmfence", "## Acceptance\n- [ ] a\n  ```sh\n  make\n- [ ] b\n- [ ] c\n\n## Notes\nimportant\n")
	path := dataPath(nibsDir, bodyNibFile(id))
	before := readFileT(t, path)

	msg := runBodyExpectValidation(t, "--nibs-path", nibsDir, "body", id, "--remove", "1", "--json")
	for _, want := range []string{"item 1", "fenced block", "--replace-old", `"## Acceptance"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message = %q, want it to contain %q", msg, want)
		}
	}
	if after := readFileT(t, path); after != before {
		t.Errorf("file changed on a refused removal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestBodyCheckSkipsFencedExamples(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-3", "## Acceptance\n- [ ] a\n```md\n- [ ] example\n```\n- [ ] b\n")

	runBodyOK(t, nibsDir, id, "--check", "2")

	got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id)))
	if want := "## Acceptance\n- [ ] a\n```md\n- [ ] example\n```\n- [x] b\n"; got != want {
		t.Errorf("body =\n%q\nwant:\n%q", got, want)
	}
}

func TestBodyCheckNamedSection(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-4", "## Acceptance\n- [ ] a\n\n## Todo\n- [ ] t1\n- [ ] t2\n")

	out := runBodyOK(t, nibsDir, id, "--section", "## Todo", "--check", "2")

	got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id)))
	if want := "## Acceptance\n- [ ] a\n\n## Todo\n- [ ] t1\n- [x] t2\n"; got != want {
		t.Errorf("body =\n%q\nwant:\n%q", got, want)
	}
	if wantOut := "1. [ ] t1\n2. [x] t2\n"; out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

func TestBodyCheckDefaultsToAcceptanceCriteria(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-5", "## Acceptance Criteria\n- [ ] a\n")

	runBodyOK(t, nibsDir, id, "--check", "1")

	if got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id))); got != "## Acceptance Criteria\n- [x] a\n" {
		t.Errorf("body = %q", got)
	}
}

func TestBodyCheckNoOpWritesNothing(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-6", "## Acceptance\n- [x] a\n- [ ] b\n")
	path := dataPath(nibsDir, bodyNibFile(id))
	before := readFileT(t, path)

	out := runBodyOK(t, nibsDir, id, "--check", "1", "--uncheck", "2")

	if after := readFileT(t, path); after != before {
		t.Errorf("a no-op rewrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if wantOut := "1. [x] a\n2. [ ] b\n"; out != wantOut {
		t.Errorf("stdout = %q, want %q", out, wantOut)
	}
}

func TestBodyCheckNoOpStillVerifiesIfMatch(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-7", "## Acceptance\n- [x] a\n")
	path := dataPath(nibsDir, bodyNibFile(id))
	before := readFileT(t, path)

	runBodyOK(t, nibsDir, id, "--check", "1", "--if-match", etagOf(t, nibsDir, id))

	resetBodyFlags()
	_, err := runRootWith(t, "--nibs-path", nibsDir, "body", id, "--check", "1", "--if-match", "0000000000000001")
	if err == nil {
		t.Fatal("a stale --if-match on a no-op must be refused, got nil")
	}
	if code := reportExitError(io.Discard, err); code != output.ExitConflict {
		t.Errorf("exit = %d, want %d (conflict): %v", code, output.ExitConflict, err)
	}
	if after := readFileT(t, path); after != before {
		t.Errorf("file changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestBodyCheckStaleIfMatchOnWriteIsRefused(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-8", "## Acceptance\n- [ ] a\n")
	path := dataPath(nibsDir, bodyNibFile(id))
	before := readFileT(t, path)

	resetBodyFlags()
	_, err := runRootWith(t, "--nibs-path", nibsDir, "body", id, "--check", "1", "--if-match", "0000000000000001")
	if code := reportExitError(io.Discard, err); code != output.ExitConflict {
		t.Errorf("exit = %d, want %d (conflict): %v", code, output.ExitConflict, err)
	}
	if after := readFileT(t, path); after != before {
		t.Errorf("file changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func compactJSON(t *testing.T, raw string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(raw)); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, raw)
	}
	return buf.String()
}

type decodedChecklistItem struct {
	Index   int    `json:"index"`
	Checked bool   `json:"checked"`
	Text    string `json:"text"`
}

func TestBodyCheckJSONAddsChecklist(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-9", checklistBody)

	out := runBodyOK(t, nibsDir, id, "--check", "2", "--json")

	var got struct {
		Nib       map[string]any         `json:"nib"`
		Checklist []decodedChecklistItem `json:"checklist"`
		Warnings  []string               `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.Nib["id"] != id {
		t.Errorf("nib.id = %v, want %q", got.Nib["id"], id)
	}
	want := []decodedChecklistItem{{1, false, "a"}, {2, true, "b"}, {3, false, "c"}}
	if len(got.Checklist) != len(want) {
		t.Fatalf("checklist = %+v, want %+v", got.Checklist, want)
	}
	for i := range want {
		if got.Checklist[i] != want[i] {
			t.Errorf("checklist[%d] = %+v, want %+v", i, got.Checklist[i], want[i])
		}
	}
	if got.Warnings != nil {
		t.Errorf("warnings = %v, want none", got.Warnings)
	}
}

func TestBodyChecklistReadsWithoutWriting(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-10", "## Acceptance\n- [x] a\n- [ ] b\n\n## Todo\n- [ ] t\n")
	path := dataPath(nibsDir, bodyNibFile(id))
	before := readFileT(t, path)

	if out := runBodyOK(t, nibsDir, id, "--checklist"); out != "1. [x] a\n2. [ ] b\n" {
		t.Errorf("--checklist stdout = %q", out)
	}
	if out := runBodyOK(t, nibsDir, id, "--checklist", "--section", "Todo"); out != "1. [ ] t\n" {
		t.Errorf("--checklist --section stdout = %q", out)
	}

	out := runBodyOK(t, nibsDir, id, "--checklist", "--json")
	if got := compactJSON(t, out); got != `{"checklist":[{"index":1,"checked":true,"text":"a"},{"index":2,"checked":false,"text":"b"}]}` {
		t.Errorf("--checklist --json = %s", got)
	}

	if after := readFileT(t, path); after != before {
		t.Errorf("--checklist rewrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestBodyChecklistEmptySection(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-11", "## Acceptance\nnone yet\n")

	if out := runBodyOK(t, nibsDir, id, "--checklist"); out != "No checklist items in \"## Acceptance\".\n" {
		t.Errorf("stdout = %q", out)
	}
	if got := compactJSON(t, runBodyOK(t, nibsDir, id, "--checklist", "--json")); got != `{"checklist":[]}` {
		t.Errorf("--json stdout = %s", got)
	}
}

// Each refusal exits 2 and leaves the file unchanged.
func TestBodyChecklistRefusals(t *testing.T) {
	const body = "## Acceptance\n- [ ] a\n- [x] b\n- [ ] c\n\n## Notes\nplain\n"
	const nested = "## Acceptance\n- [ ] a\n  - [ ] a1\n- [ ] b\n"
	for _, tc := range []struct {
		name     string
		body     string
		args     []string
		wantMsgs []string
	}{
		{"out of range", body, []string{"--check", "4"}, []string{"4", `"## Acceptance"`, "3 checklist items"}},
		{"out of range in remove", body, []string{"--remove", "1,9"}, []string{"9", "3 checklist items"}},
		{"same index in two verbs", body, []string{"--check", "1", "--remove", "1"}, []string{"--check", "--remove", "1", `"## Acceptance"`}},
		{"same index twice in one flag", body, []string{"--check", "2,2"}, []string{"--check", "2", "twice", "3 checklist items"}},
		{"same index across repeats of one flag", body, []string{"--check", "1", "--check", "3,1"}, []string{"--check", "1", "twice"}},
		{"empty value among repeats", body, []string{"--check", "1", "--check", ""}, []string{"--check", "empty"}},
		{"non-numeric", body, []string{"--uncheck", "two"}, []string{"--uncheck", `"two"`, `"## Acceptance" has 3`}},
		{"zero", body, []string{"--check", "0"}, []string{"--check", "1 or greater"}},
		{"negative", body, []string{"--check", "-1"}, []string{"--check", "1 or greater"}},
		{"empty element", body, []string{"--check", "1,"}, []string{"--check", "empty"}},
		{"empty value", body, []string{"--check", ""}, []string{"--check", "empty"}},
		{"missing default section", "## Notes\n- [ ] a\n", []string{"--check", "1"}, []string{`"Acceptance"`, `"Acceptance Criteria"`, "--section"}},
		{"missing default section on read", "## Notes\n- [ ] a\n", []string{"--checklist"}, []string{`"Acceptance Criteria"`}},
		{"missing named section", body, []string{"--section", "## Todo", "--check", "1"}, []string{`"## Todo"`}},
		{"named section at another level", body, []string{"--section", "### Acceptance", "--check", "1"}, []string{"another level"}},
		{"check inside a removed block", nested, []string{"--remove", "1", "--check", "2"}, []string{"item 2", "item 1", "--remove", "3 checklist items"}},
		{"remove inside a removed block", nested, []string{"--remove", "2,1"}, []string{"item 2", "item 1"}},
		{"with set", body, []string{"--check", "1", "--set", "-"}, []string{"choose one body operation"}},
		{"with checklist", body, []string{"--check", "1", "--checklist"}, []string{"choose one body operation"}},
		{"with create", body, []string{"--section", "## Todo", "--check", "1", "--create"}, []string{"--create is only valid"}},
		{"checklist with create", body, []string{"--checklist", "--create"}, []string{"--create is only valid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(resetBodyFlags)
			resetBodyFlags()

			nibsDir, id := writeSetNib(t, "bdy-chk-ref", tc.body)
			path := dataPath(nibsDir, bodyNibFile(id))
			before := readFileT(t, path)

			args := append([]string{"--nibs-path", nibsDir, "body", id, "--json"}, tc.args...)
			msg := runBodyExpectValidation(t, args...)
			for _, want := range tc.wantMsgs {
				if !strings.Contains(msg, want) {
					t.Errorf("message = %q, want it to contain %q", msg, want)
				}
			}
			if after := readFileT(t, path); after != before {
				t.Errorf("file changed on a refused request:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestBodyChecklistFlagsCatchSwallowedFlag(t *testing.T) {
	for _, flag := range []string{"--check", "--uncheck", "--remove"} {
		t.Run(flag, func(t *testing.T) {
			t.Cleanup(resetBodyFlags)
			resetBodyFlags()
			nibsDir, id := writeSetNib(t, "bdy-chk-swallow", "## Acceptance\n- [ ] a\n")

			msg := runBodyExpectValidation(t, "--nibs-path", nibsDir, "body", id, flag, "1", flag, "--json")
			if !strings.Contains(msg, flag) || !strings.Contains(msg, `"--json" was consumed`) {
				t.Errorf("message should name %s and the swallowed --json, got: %q", flag, msg)
			}
		})
	}
}

// A request with neither --section nor the Acceptance default stands for a rule
// row whose section mode the lookup does not handle.
func TestFindBodyChecklistRejectsUnhandledSectionMode(t *testing.T) {
	_, err := findBodyChecklist("## Acceptance\n- [ ] a\n", bodyRequest{operation: bodyOpChecklistEdit})
	var target *unhandledBodyOperationError
	if !errors.As(err, &target) {
		t.Errorf("error = %v, want *unhandledBodyOperationError", err)
	}
}

// The heading comes from the nib body, so a refusal bounds how much of it it
// echoes.
func TestBodyChecklistRefusalBoundsTheHeading(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()
	long := strings.Repeat("x", 1000)
	nibsDir, id := writeSetNib(t, "bdy-chk-long", "## Acceptance ("+long+")\n- [ ] a\n")

	msg := runBodyExpectValidation(t, "--nibs-path", nibsDir, "body", id, "--check", "2", "--json")
	if strings.Contains(msg, long) || !strings.Contains(msg, "…") {
		t.Errorf("refusal should echo a truncated heading, got %d bytes: %.120q", len(msg), msg)
	}
}

func TestBodyCheckKeepsCRLFBytes(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, id := writeSetNib(t, "bdy-chk-crlf", "## Acceptance\r\n- [ ] a\r\n- [ ] b\r\n  more of b\r\n- [ ] c\r\n")

	runBodyOK(t, nibsDir, id, "--check", "1", "--remove", "2")

	if got := nibBodyText(t, readNibFile(t, nibsDir, bodyNibFile(id))); got != "## Acceptance\r\n- [x] a\r\n- [ ] c\r\n" {
		t.Errorf("body = %q", got)
	}
}

// A checklist edit leaves an archived nib where the other body edits leave it.
func TestBodyCheckArchivedNibMatchesOtherBodyEdits(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	nibsDir, _ := writeSetNib(t, "bdy-unused", "")
	layout := store.NewLayout(nibsDir)
	if err := os.MkdirAll(layout.ArchiveDir(), 0755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bdy-arc-check", "bdy-arc-clear"} {
		content := "---\nversion: 2\ntitle: Test\nstatus: completed\ntype: task\n---\n## Acceptance\n- [ ] a\n"
		if err := os.WriteFile(filepath.Join(layout.ArchiveDir(), bodyNibFile(id)), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	runBodyOK(t, nibsDir, "bdy-arc-check", "--check", "1")
	runBodyOK(t, nibsDir, "bdy-arc-clear", "--section", "## Acceptance", "--clear")

	where := func(id string) string {
		for _, dir := range []string{layout.ArchiveDir(), layout.DataDir()} {
			if _, err := os.Stat(filepath.Join(dir, bodyNibFile(id))); err == nil {
				return dir
			}
		}
		t.Fatalf("%s is in neither data/ nor archive/", id)
		return ""
	}
	if got, want := where("bdy-arc-check"), where("bdy-arc-clear"); got != want {
		t.Errorf("--check left the nib in %s, --section --clear in %s", got, want)
	}
	if got := readFileT(t, filepath.Join(where("bdy-arc-check"), bodyNibFile("bdy-arc-check"))); !strings.Contains(got, "- [x] a") {
		t.Errorf("archived nib not checked:\n%s", got)
	}
}

// A refusal for a repeated flag quotes the value as typed, not the repeats
// joined.
func TestBodyChecklistRefusalQuotesTheRepeatAsTyped(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()
	nibsDir, id := writeSetNib(t, "bdy-chk-asis", "## Acceptance\n- [ ] a\n- [ ] b\n")

	msg := runBodyExpectValidation(t, "--nibs-path", nibsDir, "body", id, "--check", "1,", "--check", "2", "--json")
	if !strings.Contains(msg, `"1,"`) || strings.Contains(msg, `"1,,2"`) {
		t.Errorf("message should quote \"1,\" as typed, got: %q", msg)
	}
}
