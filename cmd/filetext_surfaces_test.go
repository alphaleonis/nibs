package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
)

// deceptiveRunes are what a hostile file uses to make a rendered message differ
// from the string that is really there. They are chosen so the assertion below
// cannot pass vacuously:
//
//   - the bidi overrides and isolates, the zero-width space and the BOM are
//     passed through UNCHANGED by lipgloss's ANSI downsampling, so finding one in
//     captured output proves the text bypassed the rendering boundary rather than
//     proving something about the terminal. ESC alone would prove nothing on a
//     pipe, because lipgloss strips every escape sequence at NoTTY — the
//     incidental protection that made the earlier boundary look complete.
//   - ESC and a CSI sequence are included as well for the surfaces that write to
//     a plain writer, where nothing strips them.
const deceptiveRunes = "\u202e\u2066\u200b\ufeff\u00ad"

// deceptivePayload is a scalar a file can legitimately hold that renders as text
// it does not contain.
const deceptivePayload = "a\u202eevil\u2066\u200b\x1b[2K\x1b[1GAll checks passed"

// assertNoDeception fails when text carries any rune whose whole purpose is to
// make the rendering lie.
func assertNoDeception(t *testing.T, surface, text string) {
	t.Helper()
	for _, r := range deceptiveRunes {
		if strings.ContainsRune(text, r) {
			t.Errorf("%s emitted U+%04X unfiltered; file-sourced text reached the surface raw:\n%q", surface, r, text)
		}
	}
	for _, r := range text {
		if r == '\n' || r == ' ' {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Errorf("%s emitted the control character U+%04X unfiltered:\n%q", surface, r, text)
		}
	}
}

// TestFileSourcedTextNeverReachesAnEchoSurfaceRaw enumerates every surface that
// echoes text nibs did not write, and requires each one to render it through the
// boundary.
//
// The list IS the mechanism, in the same way migrateGates is: the boundary used to
// be opt-in per call site with nothing enforcing the set, and the two
// highest-traffic sites — Core.Load's "skipping unparseable nib file %s" on every
// command, and the migration engine's unclean-store refusal — were both missing.
// Two of the surfaces below are now filtered by their WRITER and cannot regress by
// omission; the rest write to stdout, which carries lipgloss styling and so cannot
// be wrapped, and are listed here because a call site is all that protects them.
func TestFileSourcedTextNeverReachesAnEchoSurfaceRaw(t *testing.T) {
	hostileName := "tnib-0001-" + deceptivePayload + ".md"

	surfaces := []struct {
		name string
		emit func(t *testing.T) string
	}{
		{
			// Structural: nibcore's warn writer. Reached by EVERY command that
			// loads a store, carrying a filename, which on Linux is arbitrary
			// bytes.
			name: "nibcore load warning",
			emit: func(t *testing.T) string {
				storeDir := writeStoreFiles(t, nil)
				writeFileT(t, dataPath(storeDir, hostileName), "---\nnot: [valid\n")
				core := nibcore.New(storeDir, config.Default())
				var warn bytes.Buffer
				core.SetWarnWriter(&warn)
				if err := core.Load(); err != nil {
					t.Fatalf("load: %v", err)
				}
				return warn.String()
			},
		},
		{
			// Structural: the CLI's error boundary. Every refusal that quotes a
			// file goes through it.
			name: "reportExitError",
			emit: func(t *testing.T) string {
				var stderr bytes.Buffer
				reportExitError(&stderr, errors.New("refusing to migrate around "+deceptivePayload))
				return stderr.String()
			},
		},
		{
			name: "loadStoreForMigration refusal",
			emit: func(t *testing.T) string {
				storeDir := writeStoreFiles(t, nil)
				writeFileT(t, dataPath(storeDir, hostileName), "---\nnot: [valid\n")
				_, err := loadStoreForMigration(newMigrateEnv(storeDir))
				if err == nil {
					t.Fatal("loadStoreForMigration accepted a store that does not load cleanly")
				}
				return err.Error()
			},
		},
		{
			name: "check renderLoadDiagnostics",
			emit: func(t *testing.T) string {
				result := &nibcore.LinkCheckResult{
					UnparseableFiles: []nibcore.UnparseableFile{
						{NibID: deceptivePayload, Path: "data/" + hostileName, Reason: "yaml: " + deceptivePayload},
					},
					DuplicateIDs: []nibcore.DuplicateID{
						{NibID: "tnib-0001", Loaded: "data/" + hostileName, Shadowed: "archive/" + hostileName},
					},
				}
				return captureStdout(t, func() { renderLoadDiagnostics(result, nil) })
			},
		},
		{
			name: "check renderFieldDiagnostics",
			emit: func(t *testing.T) string {
				storeDir := writeStoreFiles(t, map[string]string{
					"tnib-0001--one.md": "---\nversion: 1\ntitle: One\nstatus: todo\n---\n\nBody.\n",
				})
				core := nibcore.New(storeDir, config.Default())
				core.SetWarnWriter(nil)
				if err := core.Load(); err != nil {
					t.Fatalf("load: %v", err)
				}
				result := &nibcore.LinkCheckResult{
					InvalidEnums: []nibcore.InvalidEnum{
						{NibID: "tnib-0001", Reason: "invalid priority " + deceptivePayload},
					},
				}
				return captureStdout(t, func() { renderFieldDiagnostics(&App{Core: core}, result) })
			},
		},
		{
			name: "migrate scan-problem note",
			emit: func(t *testing.T) string {
				projectDir, storeDir := writeLegacyStoreNamed(t, store.DirName,
					"nibs:\n  prefix: leg-\n  id_length: 4\n", map[string]string{
						"leg-a1--one.md": layoutNib,
						hostileName:      "no front matter at all\n",
					})
				_ = projectDir
				t.Cleanup(resetRootPersistentFlags)
				t.Cleanup(resetMigrateFlags)
				resetMigrateFlags()
				out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
				if err != nil {
					t.Fatalf("migrate --dry-run: %v\nout: %s", err, out)
				}
				return out
			},
		},
		{
			name: "noStoreFoundError",
			emit: func(t *testing.T) string {
				tmp := t.TempDir()
				t.Setenv("NIBS_CONFIG_ROOT", tmp)
				projectDir := filepath.Join(tmp, "proj")
				mkdirAllT(t, projectDir)
				writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
					"nibs:\n  path: \""+strings.ReplaceAll(deceptivePayload, "\x1b", `\e`)+"\"\n")
				return noStoreFoundError(projectDir).Error()
			},
		},
		{
			name: "describeScanProblems",
			emit: func(t *testing.T) string {
				return describeScanProblems([]scanProblem{
					{path: "data/" + hostileName, reason: deceptivePayload, unreadable: true},
				})
			},
		},
		{
			name: "flattenReason",
			emit: func(t *testing.T) string { return flattenReason(deceptivePayload) },
		},
		{
			name: "sanitizeFileText",
			emit: func(t *testing.T) string { return sanitizeFileText(deceptivePayload) },
		},
		{
			name: "shellArg",
			emit: func(t *testing.T) string { return shellArg("/tmp/" + deceptivePayload) },
		},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			got := s.emit(t)
			if got == "" {
				t.Fatalf("%s emitted nothing, so this row asserts nothing", s.name)
			}
			assertNoDeception(t, s.name, got)
		})
	}
}

// TestShellArgKeepsAPathRunnable pins the other half of the boundary: a path that
// appears as a COMMAND ARGUMENT must survive intact. sanitizeFileText collapses
// whitespace and truncates past 200 runes, which corrupted exactly the string the
// user had to copy and run.
func TestShellArgKeepsAPathRunnable(t *testing.T) {
	deep := "/" + strings.Repeat("verylongdirectorycomponent/", 20) + "nibdata"
	if got := shellArg(deep); !strings.HasSuffix(got, "nibdata") || strings.Contains(got, "…") {
		t.Errorf("shellArg(%d-rune path) = %q; a remedy's argument must not be truncated", len([]rune(deep)), got)
	}
	if got := sanitizeFileText(deep); !strings.Contains(got, "…") {
		t.Fatalf("sanitizeFileText no longer truncates, so this test asserts nothing: %q", got)
	}
	spaced := "/tmp/my nibs/store"
	if got := shellArg(spaced); got != "'/tmp/my nibs/store'" {
		t.Errorf("shellArg(%q) = %q, want it quoted so the shell sees one argument", spaced, got)
	}
	plain := "/tmp/nibdata"
	if got := shellArg(plain); got != plain {
		t.Errorf("shellArg(%q) = %q, want it unquoted", plain, got)
	}
}

// TestStripControlCharsNeutralizesDeceptiveUnicode pins that the boundary is the
// whole non-printable category rather than a control-character list. The bidi
// overrides and the zero-width formatting codes are the Trojan-Source class: they
// make a message render as text it does not contain, which is precisely the
// deception the boundary exists to stop, and an earlier C0/C1 switch passed all of
// them through.
func TestStripControlCharsNeutralizesDeceptiveUnicode(t *testing.T) {
	for _, r := range []rune{
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // bidi embeddings and overrides
		'\u2066', '\u2067', '\u2068', '\u2069', // bidi isolates
		'\u200b', '\u200c', '\u200d', '\u2060', '\ufeff', // zero-width
		'\u00ad',           // soft hyphen
		'\u2028', '\u2029', // line and paragraph separators
		0x1b, 0x00, 0x7f, 0x9b, // ESC, NUL, DEL, C1 CSI
	} {
		in := "a" + string(r) + "b"
		got := stripControlChars(in)
		if got != "a b" {
			t.Errorf("stripControlChars(%q) = %q, want %q — U+%04X survived", in, got, "a b", r)
		}
	}
	// Ordinary text, including non-ASCII, must come through untouched.
	for _, in := range []string{"data/tnib-0001--h\u00f6ger.md", "\u30a8\u30d4\u30c3\u30af", "a b\tc", "emoji \U0001f389"} {
		want := strings.ReplaceAll(in, "\t", " ")
		if got := stripControlChars(in); got != want {
			t.Errorf("stripControlChars(%q) = %q, want %q", in, got, want)
		}
	}
}
