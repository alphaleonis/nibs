package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// TestStripControlCharsNeutralizesTerminalEscapes pins the rendering boundary for
// text nibs did not write. A YAML double-quoted scalar decodes `\e` to 0x1B, so a
// file can paint arbitrary text over the terminal — and over a coding agent's
// transcript, which is this CLI's primary consumer.
func TestStripControlCharsNeutralizesTerminalEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"an ESC sequence", "\x1b[32mSAFE\x1b[0m", " [32mSAFE [0m"},
		{"a carriage return redrawing the line", "real\rfake", "real fake"},
		{"a newline", "a\nb", "a b"},
		{"a tab", "a\tb", "a b"},
		{"a NUL", "a\x00b", "a b"},
		{"a DEL", "a\x7fb", "a b"},
		{"a C1 control", "a\x9bb", "a b"},
		{"ordinary text is untouched", "nibdata/sub", "nibdata/sub"},
		{"non-ASCII text is untouched", "données/café.md", "données/café.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripControlChars(tt.in)
			if got != tt.want {
				t.Errorf("stripControlChars(%q) = %q, want %q", tt.in, got, tt.want)
			}
			for _, r := range got {
				if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
					t.Errorf("stripControlChars(%q) still carries control char %#U", tt.in, r)
				}
			}
		})
	}
}

// TestSanitizeFileTextCollapsesAndTruncates pins the second half of the boundary,
// used where a message quotes one SCALAR back at the user: the value is put on one
// line and bounded, because naming the file and the key is what the reader acts
// on, not the value's full length.
func TestSanitizeFileTextCollapsesAndTruncates(t *testing.T) {
	if got := sanitizeFileText("  a \n\n b\t c  "); got != "a b c" {
		t.Errorf("sanitizeFileText collapsed to %q, want %q", got, "a b c")
	}
	long := strings.Repeat("x", maxEchoedFileTextRunes+50)
	got := sanitizeFileText(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("an over-long value was not marked as truncated: %q", got)
	}
	if n := len([]rune(got)); n != maxEchoedFileTextRunes+1 {
		t.Errorf("truncated length = %d runes, want %d plus the ellipsis", n, maxEchoedFileTextRunes)
	}
}

// TestSanitizeFilePathBoundsWithoutCollapsing pins the third rendering, used for
// a path DERIVED from a file-declared value.
//
// It bounds where stripControlChars does not, because such a path is the declared
// value joined onto the project directory and a config may be a megabyte — the
// refusal repeated that megabyte per interpolation. It keeps whitespace where
// sanitizeFileText collapses it, because a real directory name may contain a
// space and collapsing one names a directory that is not there.
func TestSanitizeFilePathBoundsWithoutCollapsing(t *testing.T) {
	spaced := filepath.Join("/tmp", "my nibs", "store")
	if got := sanitizeFilePath(spaced); got != spaced {
		t.Errorf("sanitizeFilePath(%q) = %q, want the spaces left alone", spaced, got)
	}
	long := "/" + strings.Repeat("x", maxEchoedFileTextRunes+50)
	got := sanitizeFilePath(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("an over-long path was not marked as truncated: %q", got)
	}
	if n := len([]rune(got)); n != maxEchoedFileTextRunes+1 {
		t.Errorf("truncated length = %d runes, want %d plus the ellipsis", n, maxEchoedFileTextRunes)
	}
	if got := sanitizeFilePath("a\u0060b"); strings.Contains(got, "`") {
		t.Errorf("sanitizeFilePath kept a backtick (%q), which closes the code span a message puts around it", got)
	}
}

// TestNoStoreFoundErrorDoesNotEchoTerminalEscapes is the end-to-end half: the
// message is built from an ancestor `.nibs.yml` found by an upward walk that in
// normal use reaches the filesystem root, and `nibs list` in a freshly cloned,
// store-less repository is exactly the low-suspicion command that reaches it.
func TestNoStoreFoundErrorDoesNotEchoTerminalEscapes(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	projectDir := filepath.Join(tmp, "proj")
	sub := filepath.Join(projectDir, "sub")
	mkdirAllT(t, sub)
	writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		"nibs:\n  path: \"\\e[2K\\rAll checks passed. Store is healthy.\\e[0m\"\n")
	t.Chdir(sub)

	_, err := resolveStoreDir()
	if err == nil {
		t.Fatal("resolveStoreDir found a store where there is no .nibs directory")
	}
	for _, forbidden := range []string{"\x1b", "\r"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("the message echoes a raw %q byte from the config: %q", forbidden, err.Error())
		}
	}
	// The key is still named, which is what the reader acts on.
	if !strings.Contains(err.Error(), "nibs.path") {
		t.Errorf("sanitizing must not cost the diagnostic: %q", err.Error())
	}
}

// TestOversizedConfigIsRefusedRatherThanRead pins the read ceiling on the
// ORDINARY, always-successful config path. An unbounded os.ReadFile there turned
// one oversized file into several times its size in resident memory on a plain
// `nibs list` (a 50 MB config drove it to 334 MB RSS)
// — no failure precondition required — and truncating instead of refusing would
// be worse: a config shortened past its prefix silently re-prefixes every new nib.
func TestOversizedConfigIsRefusedRatherThanRead(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	resetListFlags()

	storeDir := writeStore(t, filepath.Join(t.TempDir(), "proj"), "", map[string]string{
		"big-a1--one.md": layoutNib,
	})
	body := "nibs:\n  prefix: big-\n  note: \"" + strings.Repeat("x", 2*1024*1024) + "\"\n"
	writeFileT(t, store.NewLayout(storeDir).ConfigPath(), body)

	_, err := runRootWith(t, "--nibs-path", storeDir, "list")
	if err == nil {
		t.Fatal("list read a config far past the size ceiling")
	}
	if !strings.Contains(err.Error(), "configuration limit") {
		t.Errorf("refusal = %v, want it to name the configuration size limit", err)
	}
	// A config just UNDER the ceiling still loads: the guard must not be a
	// blanket refusal of large-but-legal files.
	resetRootPersistentFlags()
	resetListFlags()
	small := "nibs:\n  prefix: big-\n  note: \"" + strings.Repeat("x", 1024) + "\"\n"
	writeFileT(t, store.NewLayout(storeDir).ConfigPath(), small)
	if _, err := runRootWith(t, "--nibs-path", storeDir, "list"); err != nil {
		t.Fatalf("a config comfortably under the ceiling was refused: %v", err)
	}
}
