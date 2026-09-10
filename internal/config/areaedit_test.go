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

// writeAreaEditStore materializes vocab as a store's areas.yml and returns the
// store directory.
func writeAreaEditStore(t *testing.T, vocab string) string {
	t.Helper()
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.NewLayout(storeDir).AreasPath(), []byte(vocab), 0644); err != nil {
		t.Fatal(err)
	}
	return storeDir
}

func readAreaEditStore(t *testing.T, storeDir string) string {
	t.Helper()
	raw, err := os.ReadFile(store.NewLayout(storeDir).AreasPath())
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
	if cfg.Get("web/panel") == nil {
		t.Errorf("web/panel is not declared after the rename: %v", cfg.Paths())
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
	if got := cfg.Paths(); strings.Join(got, ",") != strings.Join(want, ",") {
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
	if cfg := loadAreaEditConfig(t, storeDir); cfg.Declared() {
		t.Errorf("an emptied block still reports a declared vocabulary: %v", cfg.Paths())
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
	path := store.NewLayout(storeDir).AreasPath()
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

func loadAreaEditConfig(t *testing.T, storeDir string) *Areas {
	t.Helper()
	areas, err := LoadAreasFromStore(storeDir)
	if err != nil {
		t.Fatalf("the edited vocabulary no longer loads: %v", err)
	}
	return areas
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

// mergedChildrenFixture is the same divergence one level up: `web` carries a
// literal `name:` an edit can find, but its `children:` arrive through a merge
// key, so the node tree sees a parent with no children where the loader sees a
// declared subtree.
const mergedChildrenFixture = `nibs:
    prefix: tnib-
shared:
    webbase: &webbase
        children:
            - name: dashboard
              description: Charts
areas:
    - name: auth
    - <<: *webbase
      name: web
      description: The browser client
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

// colorMergeFixture is inheritance that costs the vocabulary nothing: the merge
// supplies a `color:` and no children at all, so a literal `children:` written
// under `web` would override nothing that is declared.
const colorMergeFixture = `nibs:
    prefix: tnib-
shared:
    base: &base
        color: blue
areas:
    - name: auth
    - <<: *base
      name: web
      description: The browser client
`

// nestedMergeFixture reaches that same harmless shape through a merge whose own
// anchor merges another, which is where "does this merge supply children?" stops
// being a question about one node.
const nestedMergeFixture = `nibs:
    prefix: tnib-
shared:
    base: &base
        color: blue
    styled: &styled
        <<: *base
        order: a
areas:
    - name: auth
    - <<: *styled
      name: web
      description: The browser client
`

// mergeListFixture supplies it through a SEQUENCE of merges, the shape a
// per-node inspection has to enumerate separately from the single-merge one.
const mergeListFixture = `nibs:
    prefix: tnib-
shared:
    base: &base
        color: blue
    ordered: &ordered
        order: a
areas:
    - name: auth
    - <<: [*base, *ordered]
      name: web
      description: The browser client
`

// siblingMergeFixture puts the children-bearing merge on a node BESIDE the one
// an edit names, so the node being written to inherits nothing and the file
// still does.
const siblingMergeFixture = `nibs:
    prefix: tnib-
shared:
    withkids: &withkids
        children:
            - name: dashboard
areas:
    - <<: *withkids
      name: api
    - name: web
      description: The browser client
`

// loneAnchorFixture anchors the `areas:` block and aliases it nowhere, so
// nothing in the file is inherited yet. That is the point of it being here: the
// gate refuses the CONSTRUCT rather than the harm, because an anchor is one
// `*name` away from every shape above it and no edit to this file would be
// there to notice.
const loneAnchorFixture = `nibs:
    prefix: tnib-
areas: &vocabulary
    - name: web
      description: The browser client
      children:
        - name: dashboard
`

// inlineMergeFixture merges a mapping written in place: no anchor and no alias
// appear anywhere in the file, so the merge KEY is the only signal there is —
// and the loader resolves it into `web`'s color like any other.
const inlineMergeFixture = `nibs:
    prefix: tnib-
areas:
    - name: web
      <<: {color: blue}
      children:
        - name: dashboard
`

// anchoredNullAreasFixture declares nothing and anchors the null `areas:` value,
// so an edit that converted that node in place would silently repoint every
// alias to it at the area list.
const anchoredNullAreasFixture = `areas: &empty
other: *empty
`

// anchorOutsideAreasFixture keeps every construct away from the block entirely.
// The gate refuses it anyway: whether a given anchor "actually affects areas" is
// the case-by-case reasoning it exists to replace.
const anchorOutsideAreasFixture = `nibs:
    prefix: tnib-
shared:
    palette: &palette
        primary: blue
    theme:
        <<: *palette
areas:
    - name: web
      description: The browser client
      children:
        - name: dashboard
`

// inheritedVocabularyFixtures is every areas.yml shape that reaches some of its
// content through YAML inheritance rather than writing it out — the shapes three
// rounds of per-site guards were written against, plus the ones each round left
// for the next. target is a path the LOADER declares, or "" for a file that
// declares none.
//
// construct is the phrase the refusal has to quote: the first inheritance
// construct in the file, and the line it is written on. It is pinned per fixture
// because the remedy is to remove that construct, which is only a remedy if the
// message names one the file actually has — and a line number nothing checks is
// a claim that goes stale in silence.
var inheritedVocabularyFixtures = []struct {
	name      string
	vocab     string
	target    string
	construct string
}{
	{"a child declared through an alias node", aliasAreaFixture, "web/dashboard", "the anchor `&dashboard` at line 4"},
	{"a child whose name arrives through a merge key", mergeAreaFixture, "web/dashboard", "the anchor `&dashboard` at line 4"},
	{"the whole areas block reached through an alias", aliasedBlockFixture, "web", "the anchor `&tree` at line 4"},
	{"the whole areas block reached through a merge key", mergedBlockFixture, "web", "a `<<:` merge key at line 7"},
	{"a node whose children arrive through a merge key", mergedChildrenFixture, "web", "the anchor `&webbase` at line 4"},
	{"a merge supplying a color and no children", colorMergeFixture, "web", "the anchor `&base` at line 4"},
	{"a merge whose own anchor merges another", nestedMergeFixture, "web", "the anchor `&base` at line 4"},
	{"a list of merges, none supplying children", mergeListFixture, "web", "the anchor `&base` at line 4"},
	{"a children-bearing merge on a sibling node", siblingMergeFixture, "web", "the anchor `&withkids` at line 4"},
	{"a merge of a mapping written in place, with no anchor at all", inlineMergeFixture, "web", "a `<<:` merge key at line 5"},
	{"an anchor nothing aliases yet", loneAnchorFixture, "web", "the anchor `&vocabulary` at line 3"},
	{"an anchored null `areas:` something else aliases", anchoredNullAreasFixture, "", "the anchor `&empty` at line 1"},
	{"an anchor and a merge nowhere near the block", anchorOutsideAreasFixture, "web", "the anchor `&palette` at line 4"},
}

// TestStoredAreaEditsRefuseAnInheritedVocabulary is the whole class as one
// refusal, and the reason it is one refusal rather than a guard per write site.
//
// The loader resolves anchors, aliases and merge keys; the node tree these edits
// walk sees only what is literally typed. Where a file uses any of them the two
// disagree about what is declared, and since YAML resolves an explicit key over
// a merged one, a key written on the tree's "absent" answer overrides the
// inherited one it could not see — data loss on an exit-0 success. For a MERGE
// KEY the re-read backstop cannot catch that: the decoder resolves the merge and
// lets the written key override what it supplied, so the field is bound once and
// the edited document reads back cleanly, with the inherited value gone. What
// the re-read does reject is a document that binds one field TWICE, which is
// what a divergently written key leaves behind, not a merge. Three consecutive
// rounds guarded the site that had just been demonstrated and left the next one
// open, so the construct is refused up front instead, for every verb, whatever
// it happens to supply.
//
// Every row is a shape one of those rounds reasoned about. The two at the end
// are shapes no write site touches at all — they are here because the gate is
// deliberately total, and a later "this one is harmless" narrowing is exactly
// the move that has failed three times.
func TestStoredAreaEditsRefuseAnInheritedVocabulary(t *testing.T) {
	for _, tt := range inheritedVocabularyFixtures {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writeAreaEditStore(t, tt.vocab)
			before := loadAreaEditConfig(t, storeDir).Paths()
			target := tt.target
			if target == "" {
				target = "web"
			} else if !loadAreaEditConfig(t, storeDir).IsValid(target) {
				t.Fatalf("the fixture does not declare %q for the loader: %v", target, before)
			}

			for _, edit := range []struct {
				verb string
				plan func() (*StoredAreaEdit, error)
			}{
				{"rename", func() (*StoredAreaEdit, error) { return PlanRenameStoredArea(storeDir, target, "panel") }},
				{"remove", func() (*StoredAreaEdit, error) { return PlanRemoveStoredArea(storeDir, target) }},
				{"create", func() (*StoredAreaEdit, error) { return PlanCreateStoredArea(storeDir, "platform", "", "") }},
			} {
				t.Run(edit.verb, func(t *testing.T) {
					plan, err := edit.plan()
					if err == nil {
						t.Fatalf("planning must refuse a vocabulary that inherits its content, got a plan for %s", plan.Path())
					}
					var refusal *AreaEditRefusal
					if !errors.As(err, &refusal) {
						t.Errorf("error = %v (%T), want an *AreaEditRefusal so the CLI reports it as a validation refusal", err, err)
					}
					areasPath := store.NewLayout(storeDir).AreasPath()
					for _, want := range []string{areasPath, tt.construct, "anchors, aliases or merge keys", "then rerun"} {
						if !strings.Contains(err.Error(), want) {
							t.Errorf("error = %q, want it to carry %q", err, want)
						}
					}
					if got := readAreaEditStore(t, storeDir); got != tt.vocab {
						t.Errorf("the refused edit rewrote the file:\n%s", got)
					}
					if got := loadAreaEditConfig(t, storeDir).Paths(); strings.Join(got, ",") != strings.Join(before, ",") {
						t.Errorf("Paths() = %v, want the vocabulary untouched (%v)", got, before)
					}
				})
			}
		})
	}
}

// TestStoredAreaEditsRefuseAVocabularyTheLoaderCannotRead is the gate's other
// half. A file that does not load declares nothing this edit can reason about,
// and the message has to say so: "the edit would leave it unreadable" blames the
// edit for a fault the file arrived with, and sends the caller to change an
// argument that was never the problem.
func TestStoredAreaEditsRefuseAVocabularyTheLoaderCannotRead(t *testing.T) {
	const broken = `nibs:
    prefix: tnib-
areas:
    - name: web
      order: {a: mapping where a string belongs}
      children:
        - name: dashboard
`
	storeDir := writeAreaEditStore(t, broken)
	if _, err := LoadAreasFromStore(storeDir); err == nil {
		t.Fatal("the fixture must be a file the loader rejects")
	}

	for _, edit := range []struct {
		verb string
		plan func() (*StoredAreaEdit, error)
	}{
		{"rename", func() (*StoredAreaEdit, error) { return PlanRenameStoredArea(storeDir, "web", "frontend") }},
		{"remove", func() (*StoredAreaEdit, error) { return PlanRemoveStoredArea(storeDir, "web") }},
		{"create", func() (*StoredAreaEdit, error) { return PlanCreateStoredArea(storeDir, "web/panel", "", "") }},
	} {
		t.Run(edit.verb, func(t *testing.T) {
			plan, err := edit.plan()
			if err == nil {
				t.Fatalf("planning must refuse a file the loader cannot read, got a plan for %s", plan.Path())
			}
			var refusal *AreaEditRefusal
			if !errors.As(err, &refusal) {
				t.Errorf("error = %v (%T), want an *AreaEditRefusal", err, err)
			}
			for _, want := range []string{store.NewLayout(storeDir).AreasPath(), "cannot be read as an areas vocabulary", "repair it, then rerun"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to carry %q", err, want)
				}
			}
			if got := readAreaEditStore(t, storeDir); got != broken {
				t.Errorf("the refused edit rewrote the file:\n%s", got)
			}
		})
	}
}

// TestStoredAreaEditsLeaveAPlainVocabularyAlone is the guard that matters most
// for everyday users: the gate above refuses a construct nobody writes in this
// file, and it must cost a file that writes its areas out plainly nothing at
// all. All three verbs, on one file, with its prose intact afterwards.
func TestStoredAreaEditsLeaveAPlainVocabularyAlone(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := CreateStoredArea(storeDir, "web/panel", "Charts and widgets", "#00aaff"); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	if _, err := RenameStoredArea(storeDir, "auth", "identity"); err != nil {
		t.Fatalf("RenameStoredArea: %v", err)
	}
	if _, err := RemoveStoredArea(storeDir, "api/webhooks"); err != nil {
		t.Fatalf("RemoveStoredArea: %v", err)
	}

	want := []string{"identity", "api", "web", "web/dashboard", "web/panel"}
	if got := loadAreaEditConfig(t, storeDir).Paths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
	got := readAreaEditStore(t, storeDir)
	for _, keep := range []string{
		"# Where the work happens.", // a comment above the block
		"# the public surface",      // a comment beside a node
		"description: Sign-in, sessions and tokens",
		`color: "#ff8800"`,
		"order: a",
		"a_newer_nibs_wrote_this: true", // a key this build does not model
		"prefix: tnib-",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("three edits dropped %q:\n%s", keep, got)
		}
	}
	// The emptied `children:` goes with the last child, and nothing else moved.
	if strings.Contains(got, "webhooks") {
		t.Errorf("the removed node survived:\n%s", got)
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
	if cfg := loadAreaEditConfig(t, storeDir); !cfg.IsValid("web") {
		t.Fatalf("the fixture must be a config the loader accepts: %v", cfg.Paths())
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

areas:
  - name: auth # the sign-in surface
    description: >-
      Sign-in, sessions
      and tokens
    color: "#ff8800"
    order: a
  - name: api
    color: gray
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

// TestCreateStoredAreaDeclaresARootArea is the create path end to end: the plan
// resolves against the file, the write lands, and a fresh load finds the node.
func TestCreateStoredAreaDeclaresARootArea(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := CreateStoredArea(storeDir, "infra", "", ""); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	if cfg := loadAreaEditConfig(t, storeDir); cfg.Get("infra") == nil {
		t.Errorf("infra is not declared after the add: %v", cfg.Paths())
	}
}

// TestCreateStoredAreaNestsUnderItsParent pins that the argument is a PATH: the
// leaf is declared as a child of the node the rest of it names, never as a root
// whose name carries a separator.
func TestCreateStoredAreaNestsUnderItsParent(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := CreateStoredArea(storeDir, "web/panel", "", ""); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	cfg := loadAreaEditConfig(t, storeDir)
	if cfg.Get("web/panel") == nil {
		t.Errorf("web/panel is not declared after the add: %v", cfg.Paths())
	}
	if cfg.Get("panel") != nil {
		t.Errorf("the child was declared at the root as well: %v", cfg.Paths())
	}
	// The parent keeps the child it already had.
	if cfg.Get("web/dashboard") == nil {
		t.Errorf("the parent's existing child was displaced: %v", cfg.Paths())
	}
}

// TestCreateStoredAreaRefusesAParentWhoseChildrenAreMerged is the first defect
// of the class, kept as the observable it was found by. An explicit key
// overrides a merged one, so appending a literal `children:` to a parent whose
// children arrive through `<<:` would replace the subtree the file still
// declares — a valid vocabulary simply missing declarations, which the
// revalidation cannot see. The wrong answer is Paths() losing a node on an
// exit-0 success, so that is what this asserts, through the writing form.
//
// What refuses it now is the gate, not a guard at the write site: the file
// inherits content, so no verb reaches the site at all.
func TestCreateStoredAreaRefusesAParentWhoseChildrenAreMerged(t *testing.T) {
	storeDir := writeAreaEditStore(t, mergedChildrenFixture)
	before := loadAreaEditConfig(t, storeDir).Paths()
	if !strings.Contains(strings.Join(before, ","), "web/dashboard") {
		t.Fatalf("the fixture does not reproduce the merged subtree: %v", before)
	}

	_, err := CreateStoredArea(storeDir, "web/panel", "", "")
	if err == nil {
		t.Fatalf("the create must refuse a parent whose children it cannot address; it wrote instead, leaving Paths() = %v (was %v)",
			loadAreaEditConfig(t, storeDir).Paths(), before)
	}
	var refusal *AreaEditRefusal
	if !errors.As(err, &refusal) {
		t.Errorf("error = %v (%T), want an *AreaEditRefusal so the CLI reports it as a validation refusal", err, err)
	}
	if !strings.Contains(err.Error(), "anchors, aliases or merge keys") {
		t.Errorf("error = %q, want the inherited-vocabulary refusal", err)
	}
	if got := readAreaEditStore(t, storeDir); got != mergedChildrenFixture {
		t.Errorf("the refused create rewrote the file:\n%s", got)
	}
	if got := loadAreaEditConfig(t, storeDir).Paths(); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("Paths() = %v, want the vocabulary untouched (%v)", got, before)
	}
}

// mergedBlockFixture reaches the whole `areas:` block through a merge key at the
// document root: the loader resolves it into a full vocabulary, and the node
// tree — which matches literal keys — sees a document with no `areas:` key at
// all.
const mergedBlockFixture = `defaults: &defaults
    areas:
        - name: auth
        - name: web
          children:
            - name: dashboard
<<: *defaults
`

// TestCreateStoredAreaRefusesAnAreasBlockItCannotAddress is the second defect of
// the class, one level up from its neighbour above. A document whose `areas:`
// arrives through a merge key looks empty to the node tree, so a bootstrap
// deciding from that tree writes a literal `areas:` — and an explicit key
// overrides a merged one, so the whole declared vocabulary is replaced by the
// one new node.
func TestCreateStoredAreaRefusesAnAreasBlockItCannotAddress(t *testing.T) {
	storeDir := writeAreaEditStore(t, mergedBlockFixture)
	before := loadAreaEditConfig(t, storeDir).Paths()
	if !strings.Contains(strings.Join(before, ","), "web/dashboard") {
		t.Fatalf("the fixture does not reproduce the merged block: %v", before)
	}

	_, err := CreateStoredArea(storeDir, "platform", "", "")
	if err == nil {
		t.Fatalf("the create must refuse a block it cannot address; it wrote instead, leaving Paths() = %v (was %v)",
			loadAreaEditConfig(t, storeDir).Paths(), before)
	}
	var refusal *AreaEditRefusal
	if !errors.As(err, &refusal) {
		t.Errorf("error = %v (%T), want an *AreaEditRefusal so the CLI reports it as a validation refusal", err, err)
	}
	if got := readAreaEditStore(t, storeDir); got != mergedBlockFixture {
		t.Errorf("the refused create rewrote the file:\n%s", got)
	}
	if got := loadAreaEditConfig(t, storeDir).Paths(); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("Paths() = %v, want the vocabulary untouched (%v)", got, before)
	}
}

// TestCreateStoredAreaRefusesAHarmlessMerge is the cost of the gate, written
// down. This merge supplies a `color:` and no children, so the literal
// `children:` a create would append overrides nothing and the edit would be
// safe — an earlier round admitted exactly this shape by asking the loaded
// vocabulary what the node really had.
//
// It is refused now, and the refusal is the point rather than a gap in it. The
// question "does this particular merge supply anything the write would
// override?" is the one three rounds answered correctly for the shape in front
// of them and wrongly for the next; a `<<:` that supplies only a color today
// supplies children the day somebody adds one to the anchor, with no edit to
// this file to notice it. What a project gives up is inheritance in a small
// vocabulary file, which is one `nibs area add` away from being written out.
func TestCreateStoredAreaRefusesAHarmlessMerge(t *testing.T) {
	storeDir := writeAreaEditStore(t, colorMergeFixture)
	before := loadAreaEditConfig(t, storeDir).Paths()

	_, err := CreateStoredArea(storeDir, "web/panel", "", "")
	if err == nil {
		t.Fatalf("the create must refuse a file that inherits its content, even harmlessly; Paths() = %v (was %v)",
			loadAreaEditConfig(t, storeDir).Paths(), before)
	}
	if !strings.Contains(err.Error(), "anchors, aliases or merge keys") {
		t.Errorf("error = %q, want the inherited-vocabulary refusal", err)
	}
	if got := readAreaEditStore(t, storeDir); got != colorMergeFixture {
		t.Errorf("the refused create rewrote the file:\n%s", got)
	}
	// Writing the same vocabulary out plainly is what the refusal prescribes,
	// and it has to be a file the create then takes.
	const written = `nibs:
    prefix: tnib-
areas:
    - name: auth
    - name: web
      description: The browser client
      color: blue
`
	plain := writeAreaEditStore(t, written)
	if _, err := CreateStoredArea(plain, "web/panel", "", ""); err != nil {
		t.Fatalf("the prescribed remedy is refused too: %v", err)
	}
	if cfg := loadAreaEditConfig(t, plain); cfg.Get("web/panel") == nil || cfg.Get("web").Color != "blue" {
		t.Errorf("the remedy did not declare web/panel under a web that keeps its color: %v", cfg.Paths())
	}
}

// TestRemoveStoredAreaStillRefusesAMergedParent is the same shape from the
// remove side. It was already refused, but as "declares no area" — a message
// that describes the node tree's view and contradicts `nibs area list`, which
// shows the node. The gate refuses it before the search runs, so the refusal now
// names the file's shape and prescribes a repair the caller can perform.
func TestRemoveStoredAreaStillRefusesAMergedParent(t *testing.T) {
	storeDir := writeAreaEditStore(t, mergedChildrenFixture)

	_, err := PlanRemoveStoredArea(storeDir, "web/dashboard")
	if err == nil {
		t.Fatal("removing a node under a merged `children:` must be refused, got a plan")
	}
	if !strings.Contains(err.Error(), "anchors, aliases or merge keys") {
		t.Errorf("error = %q, want the inherited-vocabulary refusal", err)
	}
	if got := readAreaEditStore(t, storeDir); got != mergedChildrenFixture {
		t.Errorf("the refused remove rewrote the file:\n%s", got)
	}
}

// TestCreateStoredAreaBootstrapsAMissingFile is the first `nibs area add` in a
// store that has never declared an area. Rename and remove refuse a missing
// vocabulary because there is nothing there to name; a create is exactly the
// command that has to work from it, so it starts from an empty document.
func TestCreateStoredAreaBootstrapsAMissingFile(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.NewLayout(storeDir).AreasPath()); !os.IsNotExist(err) {
		t.Fatalf("the fixture must start with no areas.yml, got %v", err)
	}

	if _, err := CreateStoredArea(storeDir, "web", "", ""); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	if cfg := loadAreaEditConfig(t, storeDir); cfg.Get("web") == nil {
		t.Errorf("web is not declared after bootstrapping the file: %v", cfg.Paths())
	}
}

// declaresNothingFixtures are the areas.yml shapes that exist but declare no
// vocabulary. Nothing in the product writes one — Areas.Save deletes the file
// when nothing is declared — so each is hand-authored, and creating the file
// with a header comment before running the verb that populates it is an ordinary
// way to arrive at `nibs area add`.
var declaresNothingFixtures = []struct {
	name     string
	vocab    string
	survives []string
}{
	{name: "a zero-byte file", vocab: ""},
	{
		name:     "a comment-only file",
		vocab:    "# The vocabulary, once we agree on one.\n",
		survives: []string{"# The vocabulary, once we agree on one."},
	},
	{
		name:     "other keys and no `areas:` block",
		vocab:    "nibs:\n    prefix: tnib-\n    id_length: 4\nfuture_key:\n    a_newer_nibs_wrote_this: true\n",
		survives: []string{"prefix: tnib-", "id_length: 4", "a_newer_nibs_wrote_this: true"},
	},
	{
		// The shape `nibs area list` prescribes to a store declaring no areas,
		// written in its most natural minimal form.
		name:     "an `areas:` key with no value under it",
		vocab:    "# Where the work happens.\nareas:\n",
		survives: []string{"# Where the work happens."},
	},
	{name: "a document holding only `---`", vocab: "---\n"},
	{
		// A root that is a bare null, which the parser hangs the file's comments
		// on rather than on the document. Converting the node in place is what
		// keeps them; replacing it drops prose a person wrote, on an exit-0
		// success that says nothing about it.
		name:     "a null root carrying comments",
		vocab:    "# the vocabulary lives here\n# ask alice before editing\nnull\n",
		survives: []string{"# the vocabulary lives here", "# ask alice before editing"},
	},
	{
		name:     "a `~` root with a comment above it",
		vocab:    "# keep this note\n~\n",
		survives: []string{"# keep this note"},
	},
	{
		name:     "a null root with a comment below it",
		vocab:    "null\n# trailing note\n",
		survives: []string{"# trailing note"},
	},
	{
		name:     "a `---` document with a comment above it",
		vocab:    "# please keep this note\n---\n",
		survives: []string{"# please keep this note"},
	},
}

// TestCreateStoredAreaBootstrapsAFileDeclaringNothing widens the bootstrap to
// every shape that declares nothing, not only the file that is not there: `nibs
// area add` promises a store declaring no areas its vocabulary from the first
// add, and the alternative remedy for these shapes is the counter-intuitive
// "delete the file first".
//
// Every one of them keeps what it was carrying, which is the same contract the
// populated file is held to: these files are hand-authored, so a bootstrap that
// replaced one would delete prose a person wrote, on an exit-0 success that says
// nothing about it.
func TestCreateStoredAreaBootstrapsAFileDeclaringNothing(t *testing.T) {
	for _, tt := range declaresNothingFixtures {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writeAreaEditStore(t, tt.vocab)

			if _, err := CreateStoredArea(storeDir, "platform", "Build and release", ""); err != nil {
				t.Fatalf("CreateStoredArea: %v", err)
			}
			cfg := loadAreaEditConfig(t, storeDir)
			if cfg.Get("platform") == nil {
				t.Errorf("platform is not declared after the add: %v", cfg.Paths())
			}
			got := readAreaEditStore(t, storeDir)
			for _, want := range tt.survives {
				if !strings.Contains(got, want) {
					t.Errorf("the bootstrap dropped %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestStoredAreaEditsStillRefuseAFileDeclaringNothing keeps the widened
// bootstrap on the create alone. Rename and remove both NAME a node the file
// would have to already declare, so a vocabulary that declares nothing is a
// refusal with nothing to say about the argument — the same answer they give a
// missing file.
//
// Refusing is the weaker half: with nothing declared, findStoredArea has no node
// to hand back either, so a rename that bootstrapped would still fail. What the
// dispositions decide is WHICH refusal, and that is what this pins — the message
// names the file that declares nothing rather than the path the caller typed,
// which is the difference between the true cause and a typo they did not make.
func TestStoredAreaEditsStillRefuseAFileDeclaringNothing(t *testing.T) {
	for _, tt := range declaresNothingFixtures {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writeAreaEditStore(t, tt.vocab)
			for _, edit := range []struct {
				verb string
				plan func() (*StoredAreaEdit, error)
			}{
				{"rename", func() (*StoredAreaEdit, error) { return PlanRenameStoredArea(storeDir, "web", "frontend") }},
				{"remove", func() (*StoredAreaEdit, error) { return PlanRemoveStoredArea(storeDir, "web") }},
			} {
				t.Run(edit.verb, func(t *testing.T) {
					plan, err := edit.plan()
					if err == nil {
						t.Fatalf("planning must refuse a vocabulary declaring nothing, got a plan for %s", plan.Path())
					}
					var refusal *AreaEditRefusal
					if !errors.As(err, &refusal) {
						t.Errorf("error = %v (%T), want an *AreaEditRefusal so the CLI reports it as a validation refusal", err, err)
					}
					if areasPath := store.NewLayout(storeDir).AreasPath(); !strings.Contains(err.Error(), areasPath) {
						t.Errorf("error = %q, want it to name %s — the file declaring nothing, not the path that was typed", err, areasPath)
					}
					if got := readAreaEditStore(t, storeDir); got != tt.vocab {
						t.Errorf("the refused edit rewrote the file:\n%s", got)
					}
				})
			}
		})
	}
}

// TestStoredAreaEditsStillRefuseAMissingFile keeps the bootstrap narrow: only a
// create may synthesize a vocabulary, because only a create names a node that
// does not have to be there already.
func TestStoredAreaEditsStillRefuseAMissingFile(t *testing.T) {
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
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
				t.Fatal("planning must refuse a missing vocabulary, got nil")
			}
			if !strings.Contains(err.Error(), "no areas vocabulary at") {
				t.Errorf("error = %q, want the missing-vocabulary refusal", err)
			}
			if _, statErr := os.Stat(store.NewLayout(storeDir).AreasPath()); !os.IsNotExist(statErr) {
				t.Errorf("a refused edit created the file: %v", statErr)
			}
		})
	}
}

// TestCreateStoredAreaRefusesADuplicateSibling: two siblings with one name make
// one path mean two nodes, so a create onto a name already taken is refused and
// the file is left exactly as it was.
func TestCreateStoredAreaRefusesADuplicateSibling(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"a root already declared", "auth"},
		{"a child already declared", "web/dashboard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writeAreaEditStore(t, areaEditFixture)

			_, err := PlanCreateStoredArea(storeDir, tt.path, "", "")
			if err == nil {
				t.Fatal("expected a refusal, got nil")
			}
			var refusal *AreaEditRefusal
			if !errors.As(err, &refusal) {
				t.Errorf("error = %v (%T), want an *AreaEditRefusal so the CLI reports it as a validation refusal", err, err)
			}
			if !strings.Contains(err.Error(), RenderAreaPath(tt.path)) {
				t.Errorf("error = %q, want it to name the path already declared (%q)", err, tt.path)
			}
			if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
				t.Errorf("a refused create rewrote the file:\n%s", got)
			}
		})
	}
}

// TestCreateStoredAreaRefusesAMalformedPath holds the create to the rule
// validateAreaNodes applies on load: a name that is empty, is whitespace, or
// carries leading or trailing whitespace is one no `area:` value can match. A
// create is the one edit that supplies a name the file has never seen, so the
// rule is checked here rather than only met by the vocabulary that comes out.
//
// The empty SEGMENT rows are how a stray separator arrives, and they are the
// reason the whole path is judged rather than its last segment alone: `/web`
// otherwise reads as a root named `web` and declares one.
func TestCreateStoredAreaRefusesAMalformedPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"no path at all", ""},
		{"whitespace only", "   "},
		{"a leading space in the name", " web"},
		{"a trailing space in the name", "web "},
		{"a trailing separator", "web/"},
		{"a leading separator", "/infra"},
		{"an empty interior segment", "web//panel"},
		{"only a separator", "/"},
		{"a leading space in the leaf", "web/ panel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := writeAreaEditStore(t, areaEditFixture)

			_, err := PlanCreateStoredArea(storeDir, tt.path, "", "")
			if err == nil {
				t.Fatal("expected a refusal, got nil")
			}
			var refusal *AreaEditRefusal
			if !errors.As(err, &refusal) {
				t.Errorf("error = %v (%T), want an *AreaEditRefusal", err, err)
			}
			if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
				t.Errorf("a refused create rewrote the file:\n%s", got)
			}
		})
	}
}

// TestCreateStoredAreaRefusesAnUndeclaredParent: the intermediate node is NOT
// minted on the caller's behalf. The vocabulary is authorization data, so one
// typo auto-creating two areas is exactly the failure a declared vocabulary
// exists to prevent — the refusal names the parent that is missing instead.
func TestCreateStoredAreaRefusesAnUndeclaredParent(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	_, err := PlanCreateStoredArea(storeDir, "wbe/dashboard", "", "")
	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	var refusal *AreaEditRefusal
	if !errors.As(err, &refusal) {
		t.Errorf("error = %v (%T), want an *AreaEditRefusal", err, err)
	}
	if !strings.Contains(err.Error(), "wbe") {
		t.Errorf("error = %q, want it to name the parent that is not declared", err)
	}
	if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
		t.Errorf("a refused create rewrote the file:\n%s", got)
	}

	// Neither half of the path was minted: not the parent that was missing, and
	// not the leaf that would have gone under it.
	cfg := loadAreaEditConfig(t, storeDir)
	for _, path := range []string{"wbe", "wbe/dashboard"} {
		if cfg.Get(path) != nil {
			t.Errorf("the refused create declared %q anyway: %v", path, cfg.Paths())
		}
	}
}

// TestCreateStoredAreaStoresDescriptionAndColor: the description is what tells
// an agent which area new work belongs in, so a create that dropped it would
// declare a node nothing can be placed in on purpose.
func TestCreateStoredAreaStoresDescriptionAndColor(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := CreateStoredArea(storeDir, "infra", "Servers, CI and deploys", "#00aaff"); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	node := loadAreaEditConfig(t, storeDir).Get("infra")
	if node == nil {
		t.Fatal("infra is not declared after the add")
	}
	if node.Description != "Servers, CI and deploys" {
		t.Errorf("description = %q, want it stored", node.Description)
	}
	if node.Color != "#00aaff" {
		t.Errorf("color = %q, want it stored", node.Color)
	}
}

// TestCreateStoredAreaOmitsTheKeysItWasGivenNothingFor keeps an unadorned add
// from writing keys with nothing in them: `description: ""` is a shape the file
// only ever gets from here, and it reads as a description someone deleted.
func TestCreateStoredAreaOmitsTheKeysItWasGivenNothingFor(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := CreateStoredArea(storeDir, "infra", "", ""); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)
	for _, unwanted := range []string{`description: ""`, `color: ""`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the create wrote %s:\n%s", unwanted, got)
		}
	}
}

// TestCreateStoredAreaRefusesAColorTheLoaderWouldReject holds the new input to
// the same contract the rename's new name has: whatever reaches the planner, the
// file it leaves behind must be one the loader accepts.
func TestCreateStoredAreaRefusesAColorTheLoaderWouldReject(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := PlanCreateStoredArea(storeDir, "infra", "", "#xyz"); err == nil {
		t.Fatal("expected a refusal for a malformed color, got nil")
	}
	if got := readAreaEditStore(t, storeDir); got != areaEditFixture {
		t.Errorf("the refused create rewrote the file:\n%s", got)
	}
}

// TestCreateStoredAreaKeepsEverythingElse is why the create goes through the
// yaml.Node tree like its two neighbours rather than re-marshaling an Areas: a
// project's own prose about its own vocabulary is the expensive thing to lose,
// and an add is the edit a project runs most often.
func TestCreateStoredAreaKeepsEverythingElse(t *testing.T) {
	storeDir := writeAreaEditStore(t, areaEditFixture)

	if _, err := CreateStoredArea(storeDir, "infra", "Servers and deploys", ""); err != nil {
		t.Fatalf("CreateStoredArea: %v", err)
	}
	got := readAreaEditStore(t, storeDir)

	if !strings.Contains(got, "- name: infra") {
		t.Errorf("the create did not land:\n%s", got)
	}
	for _, want := range []string{
		"# Where the work happens.", // a comment above the block
		"# the public surface",      // a comment beside a node
		"description: Sign-in, sessions and tokens",
		`color: "#ff8800"`,
		"order: a",
		"color: teal",
		"- name: webhooks",              // the nesting under an untouched root
		"- name: dashboard",             // and under another
		"a_newer_nibs_wrote_this: true", // a key this build does not model
		"prefix: tnib-",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the create dropped %q:\n%s", want, got)
		}
	}
	// The merged read model's system defaults are the tell that a struct
	// marshal ran: the fixture declares none of them and the file must gain none.
	for _, unwanted := range []string{"default_status", "default_type", "hide_completed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the create wrote the merged read model — %q appeared:\n%s", unwanted, got)
		}
	}
	// A root lands after the last one already declared, so `want` is declaration
	// order and not alphabetical: Paths() enumerates in the file's own order and
	// `nibs area list` renders that order, which makes appending the one
	// placement that leaves every already-declared node where the project put it.
	want := []string{"auth", "api", "api/webhooks", "web", "web/dashboard", "infra"}
	if got := loadAreaEditConfig(t, storeDir).Paths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
}
