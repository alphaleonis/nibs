package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// unclosedOverCapHeader is an opening fence followed by enough short key lines
// to push the scan one byte past its read budget, and no closing fence.
//
// The SIZE is the point: bufio.ScanLines cannot tell the LimitReader's
// artificial EOF from the real end of a file, so the loop simply runs out of
// input and returns whatever it had. Sized to the boundary rather than to the
// 440 KB of the original report — a file that only just crosses the cap is the
// harder case, and the one a fencepost error would slip through.
func unclosedOverCapHeader() string {
	var b strings.Builder
	b.WriteString("---\n")
	for b.Len() <= nib.MaxFrontMatterBytes {
		b.WriteString("k000000: v\n")
	}
	return b.String()
}

// TestHeaderScanAndParseAgree pins the invariant readFrontMatterHeader's own
// fence rule states and runMigrations' post-condition depends on: the scan may
// never call a file a nib that nib.Parse refuses.
//
// nib.Parse is the REFERENCE rather than a second opinion — it is what Core.Load
// runs, so a file the scan counts pending and the parse rejects is counted toward
// a step that can never process it.
//
// The shape rows mirror internal/nib's TestParseRequiresFrontMatter, which is the
// canonical corpus for what Parse considers a nib. Kept in step by hand: they
// live in different packages, and what this asserts is that the two SIDES agree,
// which is only meaningful over shapes Parse actually has opinions about.
func TestHeaderScanAndParseAgree(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		// The reported shape, and the same divergence without needing the cap.
		{"a header that never closes, one byte past the read budget", unclosedOverCapHeader()},
		{"a header that never closes, well under the cap", "---\ntitle: Short\n"},
		{"only an opening fence", "---\n"},
		{"an opening fence with no trailing newline", "---"},

		// Shapes Parse ACCEPTS. The scan must not refuse any of them — that
		// direction would be a worse regression than the bug, since such a file
		// would be counted unreadable and block a content step.
		{"a properly closed header", "---\nversion: 1\ntitle: Good\nstatus: todo\ntype: task\n---\n\nBody.\n"},
		{"empty front matter", "---\n---\n"},
		{"crlf throughout", "---\r\nversion: 1\r\ntitle: T\r\nstatus: todo\r\n---\r\n\r\nBody.\r\n"},
		{"a ---yaml opening fence", "---yaml\nversion: 1\ntitle: T\nstatus: todo\n---\n\nBody.\n"},
		{"a space-padded opening fence", "---   \nversion: 1\ntitle: T\n---\n\nBody.\n"},
		{"a tab-padded opening fence", "---\t\nversion: 1\ntitle: T\n---\n\nBody.\n"},
		{"a space-padded closing fence", "---\nversion: 1\ntitle: T\n---  \n\nBody.\n"},
		{"a closing fence as the last line, no trailing newline", "---\nversion: 1\ntitle: T\n---"},

		// Shapes Parse REFUSES for a reason other than truncation. The scan
		// answers these with hasFrontMatter=false, which every caller reads as
		// "not a nib" — already consistent, and here to keep it that way.
		{"no opening fence at all", "just some markdown\n"},
		{"an empty file", ""},
		{"a BOM before the fence", "\uFEFF---\nversion: 1\ntitle: T\n---\n\nBody.\n"},
		{"a blank line before the fence", "\n---\nversion: 1\ntitle: T\n---\n\nBody.\n"},
		{"---- is a rule, not a fence", "----\nversion: 1\ntitle: T\n---\n\nBody.\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "x.md")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			_, parseErr := nib.Parse(strings.NewReader(tt.content))
			h, scanErr := readFrontMatterHeader(path)

			if parseErr != nil {
				// The scan may answer either way — an error, or "no front
				// matter" — but it may NOT hand back a nib classification.
				if scanErr == nil && h.hasFrontMatter {
					t.Errorf("nib.Parse refuses this file (%v) while the scan classifies it as a nib "+
						"(hasFrontMatter=true, version=%d, err=nil); a file counted pending by a step "+
						"that can never process it is exactly the divergence the engine's post-condition forbids",
						parseErr, h.version)
				}
				return
			}
			if scanErr != nil {
				t.Errorf("nib.Parse accepts this file while the scan refuses it (%v); such a file is recorded "+
					"unreadable and blocks a content step, which is the more expensive direction of disagreement", scanErr)
			}
			if !h.hasFrontMatter {
				t.Error("nib.Parse accepts this file while the scan reports no front matter")
			}
		})
	}
}

// TestUnclosedHeaderIsNotCountedPending is the classification half: the file must
// reach the scan's PROBLEM list, not a step's count.
//
// Counted pending, it inflates the number a preview shows and names a step that
// cannot process it. Recorded as a problem it lands where the engine already
// knows what to do — and it must land as UNREADABLE, because that flag is what
// decides whether the layout step keeps moving the file: the scan cannot prove a
// file it could not read is not a nib, so leaving it behind would strand
// something that might be one.
func TestUnclosedHeaderIsNotCountedPending(t *testing.T) {
	nibsDir := writeStoreFiles(t, map[string]string{
		"t-0001--good.md":    "---\nversion: 1\ntitle: Good\nstatus: todo\ntype: task\n---\n\nBody.\n",
		"t-0002--overcap.md": unclosedOverCapHeader(),
		"t-0003--short.md":   "---\ntitle: Short\n",
	})

	scan, err := scanStore(newMigrateEnv(nibsDir))
	if err != nil {
		t.Fatalf("scanStore: %v", err)
	}

	// Every nib in this store is already v1, so any pending step can only have
	// come from misclassifying one of the two unclosed files.
	if pending := scan.pending(); len(pending) != 0 {
		names := make([]string, 0, len(pending))
		for _, s := range pending {
			names = append(names, s.name)
		}
		t.Errorf("pending = %v, want none; an unclosed header was counted toward a step nib.Parse guarantees will never see the file", names)
	}

	for _, want := range []string{"t-0002--overcap.md", "t-0003--short.md"} {
		var found *scanProblem
		for i := range scan.problems {
			if strings.Contains(scan.problems[i].path, want) {
				found = &scan.problems[i]
				break
			}
		}
		if found == nil {
			t.Errorf("%s is in no scan problem: %+v", want, scan.problems)
			continue
		}
		if !found.unreadable {
			t.Errorf("%s recorded with unreadable=false; the scan cannot prove it is not a nib, so the layout step must still move it", want)
		}
	}
}

// TestUnclosedHeaderAtAPreLayoutStoreRootRefusesBeforeAnyRename exercises the
// consequence the flag carries, which the data/ fixture above cannot reach: at a
// pre-layout store's ROOT, whether the file blocks depends on `unreadable` alone
// (under data/ it would block either way).
//
// It also pins the property blockingScanProblems' own comment says has already
// diverged twice — the preview and the run answering identically — for a shape
// that reaches the gate by a new route.
func TestUnclosedHeaderAtAPreLayoutStoreRootRefusesBeforeAnyRename(t *testing.T) {
	build := func(t *testing.T) (projectDir, storeDir string) {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetRootPersistentFlags()
		resetMigrateFlags()
		t.Setenv("NIBS_PATH", "")
		// A v0 nib makes a CONTENT step pending, which is what consults the
		// blocking set at all; the torn file beside it is what must block.
		return writeLegacyStore(t, "", map[string]string{
			"leg-a1--one.md":  "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
			"leg-b2--torn.md": "---\ntitle: Torn\n",
		})
	}

	t.Run("the preview predicts the refusal", func(t *testing.T) {
		_, storeDir := build(t)
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run", "--allow-dirty")
		if err != nil {
			t.Fatalf("migrate --dry-run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "refusing to migrate around") {
			t.Errorf("the preview does not predict the refusal the run raises:\n%s", out)
		}
		if !strings.Contains(out, "leg-b2--torn.md") {
			t.Errorf("the preview does not name the torn file:\n%s", out)
		}
	})

	t.Run("the run refuses before moving anything", func(t *testing.T) {
		_, storeDir := build(t)
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err == nil {
			t.Fatalf("migrate proceeded with a file it cannot classify\n%s", out)
		}
		// The gate must fire BEFORE the layout step's renames, or the store is
		// left half-migrated — files under data/ and a store that still refuses.
		if _, statErr := os.Stat(filepath.Join(storeDir, store.DataDirName)); statErr == nil {
			t.Error("the refused run created data/; the gate fired after the movement it guards")
		}
		for _, name := range []string{"leg-a1--one.md", "leg-b2--torn.md"} {
			if _, statErr := os.Stat(filepath.Join(storeDir, name)); statErr != nil {
				t.Errorf("%s left the store root despite the refusal: %v", name, statErr)
			}
		}
	})
}
