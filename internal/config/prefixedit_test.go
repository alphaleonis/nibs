package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// writePrefixEditStore materializes cfg as a store's config.yml and returns the
// store directory.
func writePrefixEditStore(t *testing.T, cfg string) string {
	t.Helper()
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.NewLayout(storeDir).ConfigPath(), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return storeDir
}

func readPrefixEditStore(t *testing.T, storeDir string) string {
	t.Helper()
	raw, err := os.ReadFile(store.NewLayout(storeDir).ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestPlanSetStoredPrefixRefusesTheFilesItCannotRewrite pins the shapes where
// rewriting the file from its first document would lose something, and pins that
// the refusal costs the store nothing: `nibs config set-prefix` renames every nib
// file after this plan is made, so a refusal that arrives late is one no rerun
// repairs.
//
// Neither shape announces itself without the refusal: dropping it makes a second
// YAML document disappear on an exit-0 success, and dropping the round-trip
// comparison makes an aliased `nibs:` section report a new prefix the file does
// not carry, because an alias node's Content is not emitted.
//
// The remedy text is pinned too, not just the diagnosis. A refusal that names an
// action the file gives the user no way to perform is a refusal they cannot clear.
func TestPlanSetStoredPrefixRefusesTheFilesItCannotRewrite(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantMsg []string
	}{
		{
			name: "a second YAML document",
			config: `nibs:
    prefix: tnib-
    id_length: 4
---
# a second document some other tool appends
extra: true
`,
			wantMsg: []string{"more than one YAML document"},
		},
		{
			// yaml.v3 reads a bare trailing `---` as a second, null document, so
			// this takes the same refusal with nothing after the marker to move.
			// The remedy has to fit that or it prescribes an action the user
			// cannot perform.
			name: "a trailing document marker with nothing after it",
			config: `nibs:
    prefix: tnib-
---
`,
			wantMsg: []string{"more than one YAML document", "delete the marker if nothing follows it"},
		},
		{
			name: "a nibs section reached through an alias",
			config: `base: &base
    prefix: tnib-
    id_length: 4
nibs: *base
`,
			wantMsg: []string{"literal `nibs:` mapping"},
		},
		{
			// An alias has no mapping of its own to append to, exactly like a
			// `nibs:` written with no value — but rewriting one into a literal
			// mapping would drop the alias for every other reader of the file, so
			// only the null is converted and this stays refused.
			name: "a nibs section aliasing an anchor that is itself null",
			config: `base: &base
nibs: *base
`,
			wantMsg: []string{"literal `nibs:` mapping"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writePrefixEditStore(t, tt.config)

			edit, err := PlanSetStoredPrefix(storeDir, "zz-")
			if err == nil {
				t.Fatalf("expected a refusal, got an edit for %s", edit.Path())
			}
			var refusal *PrefixEditRefusal
			if !errors.As(err, &refusal) {
				t.Errorf("error = %v (%T), want a *PrefixEditRefusal so the CLI reports it as a validation refusal", err, err)
			}
			for _, want := range tt.wantMsg {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err.Error(), want)
				}
			}
			if got := readPrefixEditStore(t, storeDir); got != tt.config {
				t.Errorf("the refused edit rewrote config.yml:\n%s", got)
			}
		})
	}
}

// TestPlanSetStoredPrefixWritesNothingUntilWrite is the property that makes the
// refusals above worth anything: the plan IS the edit, so everything that can be
// refused is refused while the store is still untouched, and the caller decides
// when the bytes land.
func TestPlanSetStoredPrefixWritesNothingUntilWrite(t *testing.T) {
	const authored = "nibs:\n    prefix: tnib-\n    id_length: 4\n"
	storeDir := writePrefixEditStore(t, authored)

	edit, err := PlanSetStoredPrefix(storeDir, "zz-")
	if err != nil {
		t.Fatalf("PlanSetStoredPrefix: %v", err)
	}
	if got := readPrefixEditStore(t, storeDir); got != authored {
		t.Errorf("planning wrote to config.yml:\n%s", got)
	}
	if _, err := edit.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := readPrefixEditStore(t, storeDir); !strings.Contains(got, "prefix: zz-") {
		t.Errorf("Write did not land the planned edit:\n%s", got)
	}
}

// TestPlanSetStoredPrefixMakesTheFileSayThePrefix covers the shapes where there
// is no key to edit. set-prefix's job is to make the file say the new prefix, so
// a missing `nibs:` mapping — or a missing file — is created rather than refused.
//
// "Missing" includes a `nibs:` key written with no value under it. That is a null
// scalar, not a mapping, so the append has nowhere to land and the round-trip
// check refuses the result — a dead end whose only exit is a hand edit, on a
// shape the doc comment says is supported.
func TestPlanSetStoredPrefixMakesTheFileSayThePrefix(t *testing.T) {
	tests := []struct {
		name string
		// config is the file the store starts with; noFile leaves it without one.
		config string
		noFile bool
		// keep is text elsewhere in the file the created section must not displace.
		keep string
	}{
		{
			name:   "a config with no nibs section",
			config: "areas:\n    - name: auth\n",
			keep:   "- name: auth",
		},
		{
			name:   "a nibs key written with no value under it",
			config: "nibs:\nareas:\n    - name: auth\n",
			keep:   "- name: auth",
		},
		{
			name:   "a nibs key written as an explicit null",
			config: "nibs: null\n",
		},
		{
			// A document that is only a marker parses to a null root, which is the
			// same hole one level up.
			name:   "a document that is nothing but a marker",
			config: "---\n",
		},
		{
			name:   "a store with no config file",
			noFile: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var storeDir string
			if tt.noFile {
				storeDir = filepath.Join(t.TempDir(), store.DirName)
				if err := os.MkdirAll(storeDir, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				storeDir = writePrefixEditStore(t, tt.config)
			}

			if _, err := SetStoredPrefix(storeDir, "zz-"); err != nil {
				t.Fatalf("SetStoredPrefix: %v", err)
			}
			cfg, err := LoadFromStore(storeDir)
			if err != nil {
				t.Fatalf("LoadFromStore: %v", err)
			}
			if cfg.Nibs.Prefix != "zz-" {
				t.Errorf("Prefix = %q, want \"zz-\"", cfg.Nibs.Prefix)
			}
			if tt.keep != "" {
				if got := readPrefixEditStore(t, storeDir); !strings.Contains(got, tt.keep) {
					t.Errorf("the created section displaced %q:\n%s", tt.keep, got)
				}
			}
		})
	}
}

// TestStoredPrefixEditRoundTripPreservesWhatItClaims executes the doc comment on
// PlanSetStoredPrefix rather than trusting it. The edit is a semantic-preserving
// re-marshal, so this pins both halves: what a project's committed config gets
// back, and the layout it does not.
//
// The negative half is the one that keeps the comment honest: a doc comment
// promising the file comes back untouched apart from the one key reads entirely
// plausible, and the two indentation clauses below refute it on their own.
func TestStoredPrefixEditRoundTripPreservesWhatItClaims(t *testing.T) {
	const authored = `# What this project calls its nibs.
nibs:
  prefix: "tnib-"   # the id prefix
  id_length: 4

defaults: &defaults
  color: gray
areas:
  - name: auth
    description: >-
      Sign-in, sessions
      and tokens
  - name: api
    <<: *defaults
    description: The public API
future_key:
  nested:
    a_newer_nibs_wrote_this: true
# The last word.
`
	storeDir := writePrefixEditStore(t, authored)
	if _, err := SetStoredPrefix(storeDir, "zz-"); err != nil {
		t.Fatalf("SetStoredPrefix: %v", err)
	}
	got := readPrefixEditStore(t, storeDir)

	if !strings.Contains(got, "prefix: zz-") {
		t.Errorf("the edit did not land:\n%s", got)
	}
	for _, want := range []string{
		"# What this project calls its nibs.", // a head comment
		"# the id prefix",                     // an inline comment
		"# The last word.",                    // a footer comment
		"id_length: 4",                        // the section's other key
		"color: gray",
		"- name: auth",
		"description: The public API",
		"a_newer_nibs_wrote_this: true", // an unmodeled key, nested
		"&defaults",                     // an anchor
		"*defaults",                     // and the alias that uses it
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the round trip dropped %q:\n%s", want, got)
		}
	}
	// Key order is preserved, which a re-marshal from a struct would not do.
	if strings.Index(got, "nibs:") >= strings.Index(got, "defaults:") ||
		strings.Index(got, "areas:") >= strings.Index(got, "future_key:") {
		t.Errorf("key order changed:\n%s", got)
	}
	// The merged read model's system defaults are the tell that Config.Save ran:
	// the fixture declares none of them and the file must gain none.
	for _, unwanted := range []string{"default_status", "default_type", "hide_completed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the edit wrote the merged read model — %q appeared:\n%s", unwanted, got)
		}
	}

	// The other half of the doc comment, which is the half a reader is likelier
	// to be surprised by: this is a re-marshal, so the file comes back with
	// yaml.v3's layout rather than the project's. Every clause below is a
	// sentence PlanSetStoredPrefix writes, kept honest by being executed here.
	for _, want := range []string{
		"    id_length: 4",               // indentation normalized to four spaces
		"      !!merge <<: *defaults",    // a merge key gains an explicit tag
		"        Sign-in, sessions and ", // a folded scalar is re-flowed
		"prefix: zz- # the id prefix",    // an inline comment loses its alignment
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the layout change documented as %q did not happen:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"zz-"`) {
		t.Errorf("the rewritten key is documented as losing the old quoting style, but kept it:\n%s", got)
	}
	if strings.Contains(got, "\n\n") {
		t.Errorf("blank lines are documented as dropped, but one survived:\n%s", got)
	}
}

// TestStoredPrefixEditPreservesTheConfigsMode holds set-prefix to the contract
// TestStoredAreaEditsPreserveMode holds the area edits to, and Save to through
// TestSavePreservesTheConfigsPermissions: a config kept private stays private
// across a rewrite that reads the file and writes it back.
//
// It stands apart from the round trip above rather than being one more assertion
// inside it, because it needs a filesystem that stores permission bits and the
// round trip does not. Folded together, a machine that cannot build this fixture
// would lose the round trip's coverage as well — which is what took CI's windows
// leg red rather than skipping there.
func TestStoredPrefixEditPreservesTheConfigsMode(t *testing.T) {
	testskip.NeedPosixFileModes(t, t.TempDir())

	storeDir := writePrefixEditStore(t, "nibs:\n    prefix: tnib-\n    id_length: 4\n")
	path := store.NewLayout(storeDir).ConfigPath()
	// chmod rather than a mode handed to the write, which umask would narrow.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := SetStoredPrefix(storeDir, "zz-"); err != nil {
		t.Fatalf("SetStoredPrefix: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Errorf("mode = %v, want 0640 — a config kept private must stay private", perm)
	}
}

// TestStoredPrefixEditLosesWhatTheDocSaysItLoses executes the clauses of
// PlanSetStoredPrefix's negative half that each need a fixture of their own, and
// pins the WHOLE resulting file rather than a substring — the claim is about what
// the project gets back, so anything else the re-marshal changes shows up here
// too.
//
// The comment-only row is the one that earns the doc comment's "attached to a
// node" qualifier: it is the shape that refutes the unqualified "every comment
// survives" the comment used to claim.
func TestStoredPrefixEditLosesWhatTheDocSaysItLoses(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			// A document that OPENS with `---` is one document, not two, so it is
			// edited rather than refused — and the marker does not come back.
			name:   "a leading document marker",
			config: "---\nnibs:\n    prefix: tnib-\n",
			want:   "nibs:\n    prefix: zz-\n",
		},
		{
			// A Windows-authored config comes back with every line changed, which
			// is a whole-file diff in the reader's VCS rather than a one-key one.
			name:   "CRLF line endings",
			config: "nibs:\r\n  prefix: tnib-\r\n  id_length: 4\r\n",
			want:   "nibs:\n    prefix: zz-\n    id_length: 4\n",
		},
		{
			name:   "a leading byte order mark",
			config: "\ufeffnibs:\n    prefix: tnib-\n",
			want:   "nibs:\n    prefix: zz-\n",
		},
		{
			// Comments survive by riding the node they hang off. A file that is
			// only comments parses to no document at all, so there is no node for
			// them to ride and they do not come back.
			name:   "every comment in a config that is only comments",
			config: "# everything here is commented out\n# nibs:\n#   prefix: tnib-\n",
			want:   "nibs:\n    prefix: zz-\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writePrefixEditStore(t, tt.config)
			if _, err := SetStoredPrefix(storeDir, "zz-"); err != nil {
				t.Fatalf("SetStoredPrefix: %v", err)
			}
			if got := readPrefixEditStore(t, storeDir); got != tt.want {
				t.Errorf("config.yml = %q, want %q", got, tt.want)
			}
		})
	}
}
