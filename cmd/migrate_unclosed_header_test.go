package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// unclosedOverCapHeader is an opening fence followed by enough short key lines
// to run past nib.MaxFrontMatterBytes, and no closing fence.
//
// The SIZE is the point: bufio.ScanLines cannot tell the LimitReader's
// artificial EOF from the real end of a file, so the scan's loop simply runs out
// of input and returns whatever it had — which is why this shape reached a clean
// verdict while nib.Parse refused it.
func unclosedOverCapHeader() string {
	var b strings.Builder
	b.WriteString("---\n")
	for i := 0; b.Len() <= nib.MaxFrontMatterBytes; i++ {
		b.WriteString("k")
		b.WriteString(strings.Repeat("0", 6))
		b.WriteString(": v\n")
	}
	return b.String()
}

// TestHeaderScanAndParseAgree pins the invariant readFrontMatterHeader's own
// comments state and the migration engine's post-condition depends on: the scan
// may never call a file a nib that nib.Parse refuses.
//
// nib.Parse is the REFERENCE here rather than a second opinion — it is what
// Core.Load runs, so a file the scan counts pending and the parse rejects is
// counted toward a step that can never process it.
func TestHeaderScanAndParseAgree(t *testing.T) {
	const closed = "---\nversion: 1\ntitle: Good\nstatus: todo\ntype: task\n---\n\nBody.\n"

	tests := []struct {
		name    string
		content string
	}{
		{
			// The reported shape: a 440 KB header of short lines that never
			// closes. The scan returned hasFrontMatter=true, version=0 — v0
			// pending — with a nil error.
			name:    "a header that never closes, past the byte cap",
			content: unclosedOverCapHeader(),
		},
		{
			// The same divergence without needing the cap at all: the loop ends
			// on the real end of the file, and nothing distinguished that from
			// meeting a closing fence.
			name:    "a header that never closes, well under the cap",
			content: "---\ntitle: Short\n",
		},
		{
			name:    "a properly closed header",
			content: closed,
		},
		{
			// Already consistent, and here to keep it that way: the scan reports
			// hasFrontMatter=false, which every caller reads as "not a nib".
			name:    "no opening fence at all",
			content: "just some markdown\n",
		},
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
				t.Errorf("nib.Parse accepts this file while the scan errors: %v", scanErr)
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
// cannot process it; recorded as a problem, it lands where the engine already
// knows what to do — unreadable, so the layout step moves it with the rest
// because it cannot prove the file is not a nib.
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
			}
		}
		if found == nil {
			t.Errorf("%s is in no scan problem: %+v", want, scan.problems)
			continue
		}
		// UNREADABLE, not fence-less: the scan could not establish what the file
		// is, so the layout step must keep moving it rather than leaving behind
		// something that might be a nib.
		if !found.unreadable {
			t.Errorf("%s recorded with unreadable=false; the scan cannot prove it is not a nib, so the layout step must still move it", want)
		}
	}
}
