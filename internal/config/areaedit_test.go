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

// areaEditFixture is a config written the way a project's own file reads: the
// keys a store really carries, a comment above the block and one beside a node,
// per-node description / color / order, a nested child, and a key this build
// does not model. Every one of those is something the edit must give back
// unchanged, and the file is deliberately NOT what config.Save would marshal —
// that is the failure the node-tree edit exists to avoid.
const areaEditFixture = `nibs:
    prefix: tnib-
    id_length: 4
# Where the work happens.
areas:
    - name: auth
      description: Sign-in, sessions and tokens
      color: "#ff8800"
      order: a
    - name: api # the public surface
      description: The public HTTP API
      children:
        - name: webhooks
          description: Outbound webhook delivery
          color: teal
    - name: web
      description: The browser client
      children:
        - name: dashboard
          description: Charts
future_key:
    a_newer_nibs_wrote_this: true
`

// writeAreaEditStore materializes cfg as a store's config.yml and returns the
// store directory.
func writeAreaEditStore(t *testing.T, cfg string) string {
	t.Helper()
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.NewLayout(storeDir).ConfigPath(), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	return storeDir
}

func readAreaEditStore(t *testing.T, storeDir string) string {
	t.Helper()
	raw, err := os.ReadFile(store.NewLayout(storeDir).ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestRenameStoredAreaKeepsEverythingElse is hazard #1 as a test: the edit must
// go through the yaml.Node tree, never through Config.Save, whose input is the
// MERGED read model. Descriptions, colors, orders, nesting, comments, key order
// and keys this build does not model all have to come back byte for byte.
func TestRenameStoredAreaKeepsEverythingElse(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := RenameStoredArea(storeDir, "web", "frontend"); err != nil {
		t.Fatalf("RenameStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)

	if !strings.Contains(got, "- name: frontend") {
		t.Errorf("the rename did not land:\n%s", got)
	}
	if strings.Contains(got, "- name: web\n") {
		t.Errorf("the old name survived:\n%s", got)
	}
	for _, want := range []string{
		"# Where the work happens.", // a comment above the block
		"# the public surface",      // a comment beside a node
		"description: Sign-in, sessions and tokens",
		`color: "#ff8800"`,
		"order: a",
		"color: teal",
		"- name: webhooks",                // the nesting under an untouched root
		"- name: dashboard",               // the nesting under the RENAMED root
		"description: The browser client", // the renamed node's own description
		"a_newer_nibs_wrote_this: true",   // a key this build does not model
		"prefix: tnib-",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the edit dropped %q:\n%s", want, got)
		}
	}
	// The merged read model's system defaults are the tell that Config.Save ran:
	// the fixture declares none of them and the file must gain none.
	for _, unwanted := range []string{"default_status", "default_type", "hide_completed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the edit wrote the merged read model — %q appeared:\n%s", unwanted, got)
		}
	}
}

// TestRenameStoredAreaRenamesTheNestedNode pins that a path is resolved by
// descending the tree, so `web/dashboard` renames the CHILD.
func TestRenameStoredAreaRenamesTheNestedNode(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := RenameStoredArea(storeDir, "web/dashboard", "panel"); err != nil {
		t.Fatalf("RenameStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)
	if !strings.Contains(got, "- name: panel") {
		t.Errorf("the nested rename did not land:\n%s", got)
	}
	if !strings.Contains(got, "- name: web\n") {
		t.Errorf("the parent was renamed instead of the child:\n%s", got)
	}

	cfg := loadAreaEditConfig(t, storeDir)
	if cfg.GetArea("web/panel") == nil {
		t.Errorf("web/panel is not declared after the rename: %v", cfg.AreaPaths())
	}
}

// TestRemoveStoredAreaTakesTheSubtree pins what retiring a node means in the
// file: the node and everything declared beneath it, and nothing else.
func TestRemoveStoredAreaTakesTheSubtree(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := RemoveStoredArea(storeDir, "api"); err != nil {
		t.Fatalf("RemoveStoredArea: %v", err)
	}
	cfg := loadAreaEditConfig(t, storeDir)
	want := []string{"auth", "web", "web/dashboard"}
	if got := cfg.AreaPaths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("AreaPaths() = %v, want %v", got, want)
	}
	got := readAreaEditStore(t, storeDir)
	if strings.Contains(got, "webhooks") {
		t.Errorf("the retired node's child survived:\n%s", got)
	}
	if !strings.Contains(got, "# Where the work happens.") {
		t.Errorf("the block's comment was lost:\n%s", got)
	}
}

// TestRemoveStoredAreaDropsAnEmptiedChildrenKey: `children:` describes a shape
// the node no longer has, so an emptied one goes rather than being left behind
// as `children: []` in a file the project commits.
func TestRemoveStoredAreaDropsAnEmptiedChildrenKey(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := RemoveStoredArea(storeDir, "api/webhooks"); err != nil {
		t.Fatalf("RemoveStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)
	if strings.Contains(got, "children: []") {
		t.Errorf("an emptied children key was left behind:\n%s", got)
	}
	if !strings.Contains(got, "- name: api") {
		t.Errorf("the parent went with its last child:\n%s", got)
	}
	// The OTHER node's children are untouched.
	if !strings.Contains(got, "- name: dashboard") {
		t.Errorf("an unrelated subtree was disturbed:\n%s", got)
	}
}

// TestRemoveStoredAreaKeepsTheEmptiedBlock: the top-level `areas:` key is the
// block the project authored, so retiring the last area empties it rather than
// deleting it — which is also what keeps whatever the project wrote above it.
// An empty block declares no areas, which is the state the axis reports.
func TestRemoveStoredAreaKeepsTheEmptiedBlock(t *testing.T) {
	storeDir := writeAreaEditStore(t, "nibs:\n    prefix: tnib-\n# Where the work happens.\nareas:\n    - name: auth\n")

	if _, err := RemoveStoredArea(storeDir, "auth"); err != nil {
		t.Fatalf("RemoveStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)
	if !strings.Contains(got, "areas: []") {
		t.Errorf("the emptied block was deleted rather than emptied:\n%s", got)
	}
	if !strings.Contains(got, "# Where the work happens.") {
		t.Errorf("deleting the key would have taken the comment with it:\n%s", got)
	}
	if cfg := loadAreaEditConfig(t, storeDir); cfg.AreasDeclared() {
		t.Errorf("an emptied block still reports a declared vocabulary: %v", cfg.AreaPaths())
	}
}

// TestStoredAreaEditsRefuseAnUndeclaredPath: the file is the authority these
// functions edit, so a path it does not declare is refused rather than being
// created or silently ignored.
func TestStoredAreaEditsRefuseAnUndeclaredPath(t *testing.T) {
	tests := []struct {
		name string
		edit func(storeDir string) error
	}{
		{
			name: "rename a root that is not there",
			edit: func(dir string) error { _, err := RenameStoredArea(dir, "nosuch", "x"); return err },
		},
		{
			name: "rename a child of a declared root that is not there",
			edit: func(dir string) error { _, err := RenameStoredArea(dir, "web/legacy", "x"); return err },
		},
		{
			name: "remove a path that is not there",
			edit: func(dir string) error { _, err := RemoveStoredArea(dir, "auth/sub"); return err },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writeAreaEditStore(t, areaEditFixture)
			if err := tt.edit(storeDir); err == nil {
				t.Fatal("expected a refusal, got nil")
			}
			if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
				t.Errorf("a refused edit rewrote the file:\n%s", got)
			}
		})
	}
}

// TestRenameStoredAreaRefusesAResultTheLoaderWouldReject is the backstop under
// the CLI's own uniqueness refusal: whatever reaches this function, the file it
// leaves behind has to be one the loader accepts. A config the loader rejects is
// a store no command can open, so the check runs before the write and the
// original file survives.
func TestRenameStoredAreaRefusesAResultTheLoaderWouldReject(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := RenameStoredArea(storeDir, "web", "auth"); err == nil {
		t.Fatal("renaming onto an existing sibling must be refused, got nil")
	}
	if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
		t.Errorf("the refused rename rewrote the file:\n%s", got)
	}
}

// TestStoredAreaEditsPreserveMode holds the edits to the same contract
// SetStoredPrefix has: a config kept private stays private.
func TestStoredAreaEditsPreserveMode(t *testing.T) {
	testskip.NeedPosixFileModes(t, t.TempDir())
	storeDir := writeAreaEditStore(t, areaEditFixture)
	path := store.NewLayout(storeDir).ConfigPath()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RenameStoredArea(storeDir, "web", "frontend"); err != nil {
		t.Fatalf("RenameStoredArea: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

func loadAreaEditConfig(t *testing.T, storeDir string) *Config {
	t.Helper()
	cfg, err := LoadFromStore(storeDir)
	if err != nil {
		t.Fatalf("the edited config no longer loads: %v", err)
	}
	return cfg
}

// aliasAreaFixture declares `web/dashboard` through a YAML alias: the loader
// resolves it, so the vocabulary reads normally, but the node carries no `name:`
// key of its own for an edit to address.
const aliasAreaFixture = `nibs:
    prefix: tnib-
shared:
    dashboard: &dashboard
        name: dashboard
        description: Charts
areas:
    - name: web
      description: The browser client
      children:
        - *dashboard
`

// mergeAreaFixture is the same divergence reached the other way: the node is a
// mapping, but its `name` arrives through a merge key.
const mergeAreaFixture = `nibs:
    prefix: tnib-
shared:
    dashboard: &dashboard
        name: dashboard
        description: Charts
areas:
    - name: web
      description: The browser client
      children:
        - <<: *dashboard
`

// aliasedBlockFixture reaches the whole `areas:` block through an alias.
const aliasedBlockFixture = `nibs:
    prefix: tnib-
shared:
    tree: &tree
        - name: web
          description: The browser client
areas: *tree
`

// TestStoredAreaEditsRefuseAVocabularyTheyCannotAddress is finding #2 as a test.
// The loader resolves aliases and merge keys, so cfg.IsValidArea says the node
// exists; the node tree matches a literal `name:` and it does not. The edit must
// refuse — with nothing written and a refusal the caller can act on — rather
// than let a caller cascade its members first and fail here forever.
func TestStoredAreaEditsRefuseAVocabularyTheyCannotAddress(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		path    string
		wantMsg string
	}{
		{
			name:    "a child declared through an alias node",
			config:  aliasAreaFixture,
			path:    "web/dashboard",
			wantMsg: "alias",
		},
		{
			name:    "a child whose name arrives through a merge key",
			config:  mergeAreaFixture,
			path:    "web/dashboard",
			wantMsg: "merge key",
		},
		{
			name:    "the whole areas block reached through an alias",
			config:  aliasedBlockFixture,
			path:    "web",
			wantMsg: "alias",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The loaded model sees what the file's shape hides, which is the
			// divergence the refusal exists for.
			storeDir := writeAreaEditStore(t, tt.config)
			if cfg := loadAreaEditConfig(t, storeDir); !cfg.IsValidArea(tt.path) {
				t.Fatalf("the fixture does not reproduce the divergence: %v", cfg.AreaPaths())
			}
			for _, edit := range []struct {
				verb string
				plan func() (*StoredAreaEdit, error)
			}{
				{"rename", func() (*StoredAreaEdit, error) { return PlanRenameStoredArea(storeDir, tt.path, "panel") }},
				{"remove", func() (*StoredAreaEdit, error) { return PlanRemoveStoredArea(storeDir, tt.path) }},
			} {
				t.Run(edit.verb, func(t *testing.T) {
					plan, err := edit.plan()
					if err == nil {
						t.Fatalf("planning must refuse a node it cannot address, got a plan for %s", plan.Path())
					}
					var refusal *AreaEditRefusal
					if !errors.As(err, &refusal) {
						t.Errorf("error = %v (%T), want an *AreaEditRefusal so the CLI reports it as a validation refusal", err, err)
					}
					if !strings.Contains(err.Error(), tt.wantMsg) {
						t.Errorf("error = %q, want it to name the shape (%q)", err, tt.wantMsg)
					}
					if got := readAreaEditStore(t, storeDir); got != tt.config {
						t.Errorf("the refused edit rewrote the file:\n%s", got)
					}
				})
			}
		})
	}
}

// TestStoredAreaEditsRefuseAMultiDocumentConfig is the loss half of finding #3.
// yaml.Unmarshal decodes only the first document and yaml.Marshal re-emits from
// that tree, so writing back would silently delete everything after the `---`.
// A config nibs itself never writes is refused rather than halved.
func TestStoredAreaEditsRefuseAMultiDocumentConfig(t *testing.T) {
	const twoDocs = `nibs:
    prefix: tnib-
areas:
    - name: web
      description: The browser client
---
# a second document some other tool appends
extra: true
`
	storeDir := writeAreaEditStore(t, twoDocs)
	// The loader accepts it, which is why the editor has to say something.
	if cfg := loadAreaEditConfig(t, storeDir); !cfg.IsValidArea("web") {
		t.Fatalf("the fixture must be a config the loader accepts: %v", cfg.AreaPaths())
	}

	for _, edit := range []struct {
		verb string
		plan func() (*StoredAreaEdit, error)
	}{
		{"rename", func() (*StoredAreaEdit, error) { return PlanRenameStoredArea(storeDir, "web", "frontend") }},
		{"remove", func() (*StoredAreaEdit, error) { return PlanRemoveStoredArea(storeDir, "web") }},
	} {
		t.Run(edit.verb, func(t *testing.T) {
			_, err := edit.plan()
			if err == nil {
				t.Fatal("planning must refuse a multi-document config, got nil")
			}
			var refusal *AreaEditRefusal
			if !errors.As(err, &refusal) {
				t.Errorf("error = %v (%T), want an *AreaEditRefusal", err, err)
			}
			if !strings.Contains(err.Error(), "more than one YAML document") {
				t.Errorf("error = %q, want it to name the shape", err)
			}
			// yaml.v3 reads a bare trailing `---` as a second, null document, so
			// this refusal also lands on a file with nothing after the marker to
			// move. The remedy has to fit that case or it prescribes an action
			// the user cannot perform.
			if !strings.Contains(err.Error(), "delete the marker if nothing follows it") {
				t.Errorf("error = %q, want a remedy that fits a marker with nothing after it", err)
			}
			if got := readAreaEditStore(t, storeDir); got != twoDocs {
				t.Errorf("the refused edit rewrote the file:\n%s", got)
			}
		})
	}
}

// TestPlanStoredAreaEditWritesNothingUntilWrite is the whole point of the split:
// the caller resolves the config edit BEFORE it cascades the members, so a
// config the editor cannot take is a refusal with an untouched store rather than
// a member rewrite that can never be completed.
func TestPlanStoredAreaEditWritesNothingUntilWrite(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	plan, err := PlanRenameStoredArea(storeDir, "web", "frontend")
	if err != nil {
		t.Fatalf("PlanRenameStoredArea: %v", err)
	}
	if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
		t.Fatalf("planning wrote the file:\n%s", got)
	}
	if _, err := plan.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := readAreaEditStore(t, storeDir); !strings.Contains(got, "- name: frontend") {
		t.Errorf("Write did not apply the planned edit:\n%s", got)
	}
}

// TestStoredAreaEditRoundTripPreservesWhatItClaims executes the doc comment on
// editStoredAreas rather than trusting it. The edit is a semantic-preserving
// re-marshal, so this pins both halves: what a project's committed config gets
// back, and the layout it does not.
func TestStoredAreaEditRoundTripPreservesWhatItClaims(t *testing.T) {
	const authored = `# The vocabulary this project places work in.
nibs:
  prefix: tnib-
  id_length: 4

defaults: &defaults
  color: gray
areas:
  - name: auth # the sign-in surface
    description: >-
      Sign-in, sessions
      and tokens
    color: "#ff8800"
    order: a
  - name: api
    <<: *defaults
    description: The public API
  - name: web
    description: The browser client
    children:
      - name: dashboard
        description: Charts
future_key:
  nested:
    a_newer_nibs_wrote_this: true
# The last word.
`
	storeDir := writeAreaEditStore(t, authored)
	if _, err := RenameStoredArea(storeDir, "web", "frontend"); err != nil {
		t.Fatalf("RenameStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)

	for _, want := range []string{
		"# The vocabulary this project places work in.", // a head comment
		"# the sign-in surface",                         // an inline comment
		"# The last word.",                              // a footer comment
		`color: "#ff8800"`,
		"order: a",
		"- name: dashboard",
		"a_newer_nibs_wrote_this: true", // an unmodeled key, nested
		"&defaults",                     // an anchor
		"*defaults",                     // and the alias that uses it
		"- name: frontend",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the round trip dropped %q:\n%s", want, got)
		}
	}
	// Key order is preserved, which a re-marshal from a struct would not do.
	if strings.Index(got, "nibs:") >= strings.Index(got, "areas:") ||
		strings.Index(got, "areas:") >= strings.Index(got, "future_key:") {
		t.Errorf("key order changed:\n%s", got)
	}
	// The other half of the doc comment, which is the half a reader is likelier
	// to be surprised by: this is a re-marshal, so the file comes back with
	// yaml.v3's layout rather than the project's. Every clause below is a
	// sentence planStoredAreaEdit writes, kept honest by being executed here.
	for _, want := range []string{
		"    prefix: tnib-",              // indentation normalized to four spaces
		"      !!merge <<: *defaults",    // a merge key gains an explicit tag
		"        Sign-in, sessions and ", // a folded scalar is re-flowed
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the layout change documented as %q did not happen:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n") {
		t.Errorf("blank lines are documented as dropped, but one survived:\n%s", got)
	}
}
