package area

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/yamlfile"
)

// areaEditFixture is an areas file written the way a project's own file reads:
// the keys a store really carries, a comment above the block and one beside a
// node, per-node description / color / order, a nested child, and a key this
// build does not model. Every one of those is something the edit must give back
// unchanged, and the file is deliberately NOT what marshaling a Vocabulary would
// produce — that is the failure the node-tree edit exists to avoid.
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

// someAreasFile stands in for the path a caller that read the file would name.
const someAreasFile = "/srv/project/.nibs/areas.yml"

func planCreate(vocab, path, description, color string) ([]byte, error) {
	return PlanCreate([]byte(vocab), true, path, description, color)
}

func planRename(vocab, path, newName string) ([]byte, error) {
	return PlanRename([]byte(vocab), true, path, newName)
}

func planRemove(vocab, path string) ([]byte, error) {
	return PlanRemove([]byte(vocab), true, path)
}

// mustPlan fails unless the planner accepted, and returns its output as text. It
// returns a function so a planner call can be passed to it whole.
func mustPlan(t *testing.T) func(out []byte, err error) string {
	return func(out []byte, err error) string {
		t.Helper()
		if err != nil {
			t.Fatalf("planning: %v", err)
		}
		return string(out)
	}
}

// mustParse parses a planner's output the way the store's loader will.
func mustParse(t *testing.T, out string) *Vocabulary {
	t.Helper()
	vocab, err := Parse([]byte(out))
	if err != nil {
		t.Fatalf("the edited vocabulary no longer parses: %v", err)
	}
	return vocab
}

// mustRefuse fails unless the planner refused with an *EditRefusal and handed
// back no output.
func mustRefuse(t *testing.T) func(out []byte, err error) *EditRefusal {
	return func(out []byte, err error) *EditRefusal {
		t.Helper()
		if err == nil {
			t.Fatalf("planning must refuse, got output:\n%s", out)
		}
		if out != nil {
			t.Errorf("a refused plan handed back output:\n%s", out)
		}
		var refusal *EditRefusal
		if !errors.As(err, &refusal) {
			t.Fatalf("error = %v (%T), want an *EditRefusal so a surface reports it as a validation refusal", err, err)
		}
		return refusal
	}
}

// TestRenameKeepsEverythingElse is hazard #1 as a test: the edit must go through
// the yaml.Node tree, never through a struct marshal. Descriptions, colors,
// orders, nesting, comments, key order and keys this build does not model all
// have to come back.
func TestRenameKeepsEverythingElse(t *testing.T) {
	got := mustPlan(t)(planRename(areaEditFixture, "web", "frontend"))

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
	// The config read model's system defaults are the tell that a struct marshal
	// ran: the fixture declares none of them and the output must gain none.
	for _, unwanted := range []string{"default_status", "default_type", "hide_completed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the edit wrote a merged read model — %q appeared:\n%s", unwanted, got)
		}
	}
}

// TestRenameRenamesTheNestedNode pins that a path is resolved by descending the
// tree, so `web/dashboard` renames the CHILD.
func TestRenameRenamesTheNestedNode(t *testing.T) {
	got := mustPlan(t)(planRename(areaEditFixture, "web/dashboard", "panel"))
	if !strings.Contains(got, "- name: panel") {
		t.Errorf("the nested rename did not land:\n%s", got)
	}
	if !strings.Contains(got, "- name: web\n") {
		t.Errorf("the parent was renamed instead of the child:\n%s", got)
	}
	if vocab := mustParse(t, got); vocab.Get("web/panel") == nil {
		t.Errorf("web/panel is not declared after the rename: %v", vocab.Paths())
	}
}

// TestRemoveTakesTheSubtree pins what retiring a node means in the file: the
// node and everything declared beneath it, and nothing else.
func TestRemoveTakesTheSubtree(t *testing.T) {
	got := mustPlan(t)(planRemove(areaEditFixture, "api"))
	want := []string{"auth", "web", "web/dashboard"}
	if paths := mustParse(t, got).Paths(); strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("Paths() = %v, want %v", paths, want)
	}
	if strings.Contains(got, "webhooks") {
		t.Errorf("the retired node's child survived:\n%s", got)
	}
	if !strings.Contains(got, "# Where the work happens.") {
		t.Errorf("the block's comment was lost:\n%s", got)
	}
}

// TestRemoveDropsAnEmptiedChildrenKey: `children:` describes a shape the node no
// longer has, so an emptied one goes rather than being left behind as
// `children: []` in a file the project commits.
func TestRemoveDropsAnEmptiedChildrenKey(t *testing.T) {
	got := mustPlan(t)(planRemove(areaEditFixture, "api/webhooks"))
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

// TestRemoveKeepsTheEmptiedBlock: the top-level `areas:` key is the block the
// project authored, so retiring the last area empties it rather than deleting it
// — which is also what keeps whatever the project wrote above it. An empty block
// declares no areas, which is the state the axis reports.
func TestRemoveKeepsTheEmptiedBlock(t *testing.T) {
	got := mustPlan(t)(planRemove("nibs:\n    prefix: tnib-\n# Where the work happens.\nareas:\n    - name: auth\n", "auth"))
	if !strings.Contains(got, "areas: []") {
		t.Errorf("the emptied block was deleted rather than emptied:\n%s", got)
	}
	if !strings.Contains(got, "# Where the work happens.") {
		t.Errorf("deleting the key would have taken the comment with it:\n%s", got)
	}
	if vocab := mustParse(t, got); !vocab.IsEmpty() {
		t.Errorf("an emptied block still reports a declared vocabulary: %v", vocab.Paths())
	}
}

// TestEditsRefuseAnUndeclaredPath: the file is the authority these planners
// edit, so a path it does not declare is refused rather than being created or
// silently ignored.
func TestEditsRefuseAnUndeclaredPath(t *testing.T) {
	tests := []struct {
		name string
		plan func() ([]byte, error)
	}{
		{"rename a root that is not there", func() ([]byte, error) { return planRename(areaEditFixture, "nosuch", "x") }},
		{"rename a child of a declared root that is not there", func() ([]byte, error) { return planRename(areaEditFixture, "web/legacy", "x") }},
		{"remove a path that is not there", func() ([]byte, error) { return planRemove(areaEditFixture, "auth/sub") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = mustRefuse(t)(tt.plan())
		})
	}
}

// TestRenameRefusesAResultTheLoaderWouldReject is the backstop under the
// surfaces' own uniqueness refusal: whatever reaches the planner, the file it
// renders has to be one the loader accepts. A file the loader rejects is a store
// no command can open.
func TestRenameRefusesAResultTheLoaderWouldReject(t *testing.T) {
	_ = mustRefuse(t)(planRename(areaEditFixture, "web", "auth"))
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

// inheritedVocabularyFixtures is every areas.yml shape that reaches some of its
// content through YAML inheritance rather than writing it out. target is a path
// the LOADER declares, or "" for a file that declares none.
//
// construct is the phrase the refusal has to quote: the first inheritance
// construct in the file, and the line it is written on. The remedy is to remove
// that construct, which is only a remedy if the message names one the file
// actually has.
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

// TestEditsRefuseAnInheritedVocabulary is the whole class as one refusal, and
// the reason it is one refusal rather than a guard per write site.
//
// The loader resolves anchors, aliases and merge keys; the node tree these edits
// walk sees only what is literally typed. Where a file uses any of them the two
// disagree about what is declared, and since YAML resolves an explicit key over
// a merged one, a key written on the tree's "absent" answer overrides the
// inherited one it could not see — data loss on an exit-0 success. For a MERGE
// KEY the re-read backstop cannot catch that: the decoder resolves the merge and
// lets the written key override what it supplied, so the field is bound once and
// the edited document reads back cleanly, with the inherited value gone. So the
// construct is refused up front, for every verb, whatever it happens to supply.
func TestEditsRefuseAnInheritedVocabulary(t *testing.T) {
	for _, tt := range inheritedVocabularyFixtures {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.target
			if target == "" {
				target = "web"
			} else if vocab := mustParse(t, tt.vocab); !vocab.Exists(target) {
				t.Fatalf("the fixture does not declare %q for the loader: %v", target, vocab.Paths())
			}

			for _, edit := range []struct {
				verb string
				plan func() ([]byte, error)
			}{
				{"rename", func() ([]byte, error) { return planRename(tt.vocab, target, "panel") }},
				{"remove", func() ([]byte, error) { return planRemove(tt.vocab, target) }},
				{"create", func() ([]byte, error) { return planCreate(tt.vocab, "platform", "", "") }},
			} {
				t.Run(edit.verb, func(t *testing.T) {
					refusal := mustRefuse(t)(edit.plan())
					// Named through Naming, not Error: the refusal renders path-free
					// by default because the same sentence reaches an HTTP client
					// through the area mutations.
					named := refusal.Naming(someAreasFile)
					for _, want := range []string{someAreasFile, tt.construct, "anchors, aliases or merge keys", "then rerun"} {
						if !strings.Contains(named, want) {
							t.Errorf("Naming = %q, want it to carry %q", named, want)
						}
					}
				})
			}
		})
	}
}

// TestEditsRefuseAVocabularyTheLoaderCannotRead is the gate's other half. A file
// that does not load declares nothing this edit can reason about, and the
// message has to say so: "the edit would leave it unreadable" blames the edit
// for a fault the file arrived with.
func TestEditsRefuseAVocabularyTheLoaderCannotRead(t *testing.T) {
	const broken = `nibs:
    prefix: tnib-
areas:
    - name: web
      color: {a: mapping where a string belongs}
      children:
        - name: dashboard
`
	if _, err := Parse([]byte(broken)); err == nil {
		t.Fatal("the fixture must be a file the loader rejects")
	}

	for _, edit := range []struct {
		verb string
		plan func() ([]byte, error)
	}{
		{"rename", func() ([]byte, error) { return planRename(broken, "web", "frontend") }},
		{"remove", func() ([]byte, error) { return planRemove(broken, "web") }},
		{"create", func() ([]byte, error) { return planCreate(broken, "web/panel", "", "") }},
	} {
		t.Run(edit.verb, func(t *testing.T) {
			refusal := mustRefuse(t)(edit.plan())
			named := refusal.Naming(someAreasFile)
			for _, want := range []string{someAreasFile, "cannot be read as an areas vocabulary", "repair it, then rerun"} {
				if !strings.Contains(named, want) {
					t.Errorf("Naming = %q, want it to carry %q", named, want)
				}
			}
		})
	}
}

// TestEditsLeaveAPlainVocabularyAlone is the guard that matters most for everyday
// users: the gate above refuses a construct nobody writes in this file, and it
// must cost a file that writes its areas out plainly nothing at all. All three
// verbs, chained, with its prose intact afterwards.
func TestEditsLeaveAPlainVocabularyAlone(t *testing.T) {
	got := mustPlan(t)(planCreate(areaEditFixture, "web/panel", "Charts and widgets", "#00aaff"))
	got = mustPlan(t)(planRename(got, "auth", "identity"))
	got = mustPlan(t)(planRemove(got, "api/webhooks"))

	want := []string{"api", "identity", "web", "web/dashboard", "web/panel"}
	if paths := mustParse(t, got).Paths(); strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("Paths() = %v, want %v", paths, want)
	}
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
	if strings.Contains(got, "webhooks") {
		t.Errorf("the removed node survived:\n%s", got)
	}
}

// TestEditsRefuseAMultiDocumentFile: yaml.Unmarshal decodes only the first
// document and yaml.Marshal re-emits from that tree, so writing back would
// silently delete everything after the `---`. A file nibs itself never writes is
// refused rather than halved.
func TestEditsRefuseAMultiDocumentFile(t *testing.T) {
	const twoDocs = `nibs:
    prefix: tnib-
areas:
    - name: web
      description: The browser client
---
# a second document some other tool appends
extra: true
`
	// The loader accepts it, which is why the editor has to say something.
	if vocab := mustParse(t, twoDocs); !vocab.Exists("web") {
		t.Fatalf("the fixture must be a file the loader accepts: %v", vocab.Paths())
	}

	for _, edit := range []struct {
		verb string
		plan func() ([]byte, error)
	}{
		{"rename", func() ([]byte, error) { return planRename(twoDocs, "web", "frontend") }},
		{"remove", func() ([]byte, error) { return planRemove(twoDocs, "web") }},
	} {
		t.Run(edit.verb, func(t *testing.T) {
			refusal := mustRefuse(t)(edit.plan())
			if !strings.Contains(refusal.Error(), "more than one YAML document") {
				t.Errorf("error = %q, want it to name the shape", refusal)
			}
			// yaml.v3 reads a bare trailing `---` as a second, null document, so
			// this refusal also lands on a file with nothing after the marker to
			// move. The remedy has to fit that case.
			if !strings.Contains(refusal.Error(), "delete the marker if nothing follows it") {
				t.Errorf("error = %q, want a remedy that fits a marker with nothing after it", refusal)
			}
		})
	}
}

// TestPlannersLeaveTheirInputAlone: the caller resolves the edit BEFORE it
// cascades the members and writes only afterwards, so a plan must not reach
// back into the bytes it was handed.
func TestPlannersLeaveTheirInputAlone(t *testing.T) {
	input := []byte(areaEditFixture)
	out, err := PlanRename(input, true, "web", "frontend")
	got := mustPlan(t)(out, err)
	if string(input) != areaEditFixture {
		t.Fatalf("planning changed its input:\n%s", input)
	}
	if !strings.Contains(got, "- name: frontend") {
		t.Errorf("the plan does not carry the edit:\n%s", got)
	}
}

// TestEditRoundTripPreservesWhatItClaims is the authority on what planEdit's
// re-marshal preserves. It pins both halves: what a project's committed file
// gets back, and the layout it does not.
func TestEditRoundTripPreservesWhatItClaims(t *testing.T) {
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
	got := mustPlan(t)(planRename(authored, "web", "frontend"))

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
	// This is a re-marshal, so the file comes back with yaml.v3's layout rather
	// than the project's.
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

// TestCreateDeclaresARootArea is the create path end to end: the plan resolves
// against the file and a fresh parse finds the node.
func TestCreateDeclaresARootArea(t *testing.T) {
	got := mustPlan(t)(planCreate(areaEditFixture, "infra", "", ""))
	if vocab := mustParse(t, got); vocab.Get("infra") == nil {
		t.Errorf("infra is not declared after the add: %v", vocab.Paths())
	}
}

// TestCreateNestsUnderItsParent pins that the argument is a PATH: the leaf is
// declared as a child of the node the rest of it names, never as a root whose
// name carries a separator.
func TestCreateNestsUnderItsParent(t *testing.T) {
	vocab := mustParse(t, mustPlan(t)(planCreate(areaEditFixture, "web/panel", "", "")))
	if vocab.Get("web/panel") == nil {
		t.Errorf("web/panel is not declared after the add: %v", vocab.Paths())
	}
	if vocab.Get("panel") != nil {
		t.Errorf("the child was declared at the root as well: %v", vocab.Paths())
	}
	// The parent keeps the child it already had.
	if vocab.Get("web/dashboard") == nil {
		t.Errorf("the parent's existing child was displaced: %v", vocab.Paths())
	}
}

// TestCreateRefusesAParentWhoseChildrenAreMerged is the first defect of the
// class, kept as the observable it was found by. An explicit key overrides a
// merged one, so appending a literal `children:` to a parent whose children
// arrive through `<<:` would replace the subtree the file still declares — a
// valid vocabulary simply missing declarations, which the revalidation cannot
// see. What refuses it is the gate, not a guard at the write site.
func TestCreateRefusesAParentWhoseChildrenAreMerged(t *testing.T) {
	before := mustParse(t, mergedChildrenFixture).Paths()
	if !strings.Contains(strings.Join(before, ","), "web/dashboard") {
		t.Fatalf("the fixture does not reproduce the merged subtree: %v", before)
	}
	refusal := mustRefuse(t)(planCreate(mergedChildrenFixture, "web/panel", "", ""))
	if !strings.Contains(refusal.Error(), "anchors, aliases or merge keys") {
		t.Errorf("error = %q, want the inherited-vocabulary refusal", refusal)
	}
}

// TestCreateRefusesAnAreasBlockItCannotAddress is the second defect of the
// class, one level up. A document whose `areas:` arrives through a merge key
// looks empty to the node tree, so a bootstrap deciding from that tree writes a
// literal `areas:` — and an explicit key overrides a merged one, so the whole
// declared vocabulary is replaced by the one new node.
func TestCreateRefusesAnAreasBlockItCannotAddress(t *testing.T) {
	before := mustParse(t, mergedBlockFixture).Paths()
	if !strings.Contains(strings.Join(before, ","), "web/dashboard") {
		t.Fatalf("the fixture does not reproduce the merged block: %v", before)
	}
	_ = mustRefuse(t)(planCreate(mergedBlockFixture, "platform", "", ""))
}

// TestCreateRefusesAHarmlessMerge is the cost of the gate, written down. This
// merge supplies a `color:` and no children, so the literal `children:` a create
// would append overrides nothing and the edit would be safe. It is refused
// anyway: a `<<:` that supplies only a color today supplies children the day
// somebody adds one to the anchor, with no edit to this file to notice it.
func TestCreateRefusesAHarmlessMerge(t *testing.T) {
	refusal := mustRefuse(t)(planCreate(colorMergeFixture, "web/panel", "", ""))
	if !strings.Contains(refusal.Error(), "anchors, aliases or merge keys") {
		t.Errorf("error = %q, want the inherited-vocabulary refusal", refusal)
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
	vocab := mustParse(t, mustPlan(t)(planCreate(written, "web/panel", "", "")))
	if vocab.Get("web/panel") == nil || vocab.Get("web").Color != "blue" {
		t.Errorf("the remedy did not declare web/panel under a web that keeps its color: %v", vocab.Paths())
	}
}

// TestRemoveStillRefusesAMergedParent is the same shape from the remove side:
// the gate refuses it before the search runs, so the refusal names the file's
// shape rather than "declares no area", which contradicts `nibs area list`.
func TestRemoveStillRefusesAMergedParent(t *testing.T) {
	refusal := mustRefuse(t)(planRemove(mergedChildrenFixture, "web/dashboard"))
	if !strings.Contains(refusal.Error(), "anchors, aliases or merge keys") {
		t.Errorf("error = %q, want the inherited-vocabulary refusal", refusal)
	}
}

// TestCreateBootstrapsAMissingFile is the first `nibs area add` in a store that
// has never declared an area. Rename and remove refuse a missing vocabulary
// because there is nothing there to name; a create is exactly the command that
// has to work from it, so it starts from an empty document.
func TestCreateBootstrapsAMissingFile(t *testing.T) {
	got := mustPlan(t)(PlanCreate(nil, false, "web", "", ""))
	if vocab := mustParse(t, got); vocab.Get("web") == nil {
		t.Errorf("web is not declared after bootstrapping the file: %v", vocab.Paths())
	}
}

// declaresNothingFixtures are the areas.yml shapes that exist but declare no
// vocabulary. Each is hand-authored, and creating the file with a header comment
// before running the verb that populates it is an ordinary way to arrive at
// `nibs area add`.
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
		// keeps them.
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

// TestCreateBootstrapsAFileDeclaringNothing widens the bootstrap to every shape
// that declares nothing, not only the file that is not there. Every one of them
// keeps what it was carrying: these files are hand-authored, so a bootstrap that
// replaced one would delete prose a person wrote, on an exit-0 success.
func TestCreateBootstrapsAFileDeclaringNothing(t *testing.T) {
	for _, tt := range declaresNothingFixtures {
		t.Run(tt.name, func(t *testing.T) {
			got := mustPlan(t)(planCreate(tt.vocab, "platform", "Build and release", ""))
			if vocab := mustParse(t, got); vocab.Get("platform") == nil {
				t.Errorf("platform is not declared after the add: %v", vocab.Paths())
			}
			for _, want := range tt.survives {
				if !strings.Contains(got, want) {
					t.Errorf("the bootstrap dropped %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestEditsStillRefuseAFileDeclaringNothing keeps the widened bootstrap on the
// create alone. Rename and remove both NAME a node the file would have to
// already declare, so a vocabulary that declares nothing is a refusal that names
// the file declaring nothing rather than the path the caller typed.
func TestEditsStillRefuseAFileDeclaringNothing(t *testing.T) {
	for _, tt := range declaresNothingFixtures {
		t.Run(tt.name, func(t *testing.T) {
			for _, edit := range []struct {
				verb string
				plan func() ([]byte, error)
			}{
				{"rename", func() ([]byte, error) { return planRename(tt.vocab, "web", "frontend") }},
				{"remove", func() ([]byte, error) { return planRemove(tt.vocab, "web") }},
			} {
				t.Run(edit.verb, func(t *testing.T) {
					refusal := mustRefuse(t)(edit.plan())
					if named := refusal.Naming(someAreasFile); !strings.Contains(named, someAreasFile) {
						t.Errorf("Naming = %q, want it to name %s — the file declaring nothing, not the path that was typed", named, someAreasFile)
					}
					if refusal.File != "" {
						t.Errorf("File = %q, want it empty — the planner never sees a path; the caller that read the file sets it", refusal.File)
					}
				})
			}
		})
	}
}

// TestEditsStillRefuseAMissingFile keeps the bootstrap narrow: only a create may
// synthesize a vocabulary, because only a create names a node that does not
// have to be there already.
func TestEditsStillRefuseAMissingFile(t *testing.T) {
	for _, edit := range []struct {
		verb string
		plan func() ([]byte, error)
	}{
		{"rename", func() ([]byte, error) { return PlanRename(nil, false, "web", "frontend") }},
		{"remove", func() ([]byte, error) { return PlanRemove(nil, false, "web") }},
	} {
		t.Run(edit.verb, func(t *testing.T) {
			refusal := mustRefuse(t)(edit.plan())
			if !strings.Contains(refusal.Error(), "no areas vocabulary at") {
				t.Errorf("error = %q, want the missing-vocabulary refusal", refusal)
			}
		})
	}
}

// TestCreateRefusesADuplicateSibling: two siblings with one name make one path
// mean two nodes, so a create onto a name already taken is refused.
func TestCreateRefusesADuplicateSibling(t *testing.T) {
	for _, path := range []string{"auth", "web/dashboard"} {
		t.Run(path, func(t *testing.T) {
			refusal := mustRefuse(t)(planCreate(areaEditFixture, path, "", ""))
			if !strings.Contains(refusal.Error(), RenderPath(path)) {
				t.Errorf("error = %q, want it to name the path already declared (%q)", refusal, path)
			}
		})
	}
}

// TestCreateRefusesAMalformedPath holds the create to the rule validateNodes
// applies on load: a name that is empty, is whitespace, or carries leading or
// trailing whitespace is one no `area:` value can match.
//
// The empty SEGMENT rows are how a stray separator arrives, and they are the
// reason the whole path is judged rather than its last segment alone: `/web`
// otherwise reads as a root named `web` and declares one.
func TestCreateRefusesAMalformedPath(t *testing.T) {
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
			_ = mustRefuse(t)(planCreate(areaEditFixture, tt.path, "", ""))
		})
	}
}

// TestCreateRefusesAnUndeclaredParent: the intermediate node is NOT minted on
// the caller's behalf. The vocabulary is authorization data, so one typo
// auto-creating two areas is exactly the failure a declared vocabulary exists to
// prevent — the refusal names the parent that is missing instead.
func TestCreateRefusesAnUndeclaredParent(t *testing.T) {
	refusal := mustRefuse(t)(planCreate(areaEditFixture, "wbe/dashboard", "", ""))
	if !strings.Contains(refusal.Error(), "wbe") {
		t.Errorf("error = %q, want it to name the parent that is not declared", refusal)
	}
}

// TestCreateStoresDescriptionAndColor: the description is what tells an agent
// which area new work belongs in, so a create that dropped it would declare a
// node nothing can be placed in on purpose.
func TestCreateStoresDescriptionAndColor(t *testing.T) {
	node := mustParse(t, mustPlan(t)(planCreate(areaEditFixture, "infra", "Servers, CI and deploys", "#00aaff"))).Get("infra")
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

// TestCreateOmitsTheKeysItWasGivenNothingFor keeps an unadorned add from writing
// keys with nothing in them: `description: ""` reads as a description someone
// deleted.
func TestCreateOmitsTheKeysItWasGivenNothingFor(t *testing.T) {
	got := mustPlan(t)(planCreate(areaEditFixture, "infra", "", ""))
	for _, unwanted := range []string{`description: ""`, `color: ""`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the create wrote %s:\n%s", unwanted, got)
		}
	}
}

// TestCreateRefusesAColorTheLoaderWouldReject holds the new input to the same
// contract the rename's new name has: whatever reaches the planner, the file it
// renders must be one the loader accepts.
func TestCreateRefusesAColorTheLoaderWouldReject(t *testing.T) {
	if _, err := planCreate(areaEditFixture, "infra", "", "#xyz"); err == nil {
		t.Fatal("expected a refusal for a malformed color, got nil")
	}
}

// TestCreateKeepsEverythingElse is why the create goes through the yaml.Node
// tree like its two neighbors rather than re-marshaling a Vocabulary: a
// project's own prose about its own vocabulary is the expensive thing to lose,
// and an add is the edit a project runs most often.
func TestCreateKeepsEverythingElse(t *testing.T) {
	got := mustPlan(t)(planCreate(areaEditFixture, "infra", "Servers and deploys", ""))

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
	for _, unwanted := range []string{"default_status", "default_type", "hide_completed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the create wrote a merged read model — %q appeared:\n%s", unwanted, got)
		}
	}
	want := []string{"api", "api/webhooks", "auth", "infra", "web", "web/dashboard"}
	if paths := mustParse(t, got).Paths(); strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("Paths() = %v, want %v", paths, want)
	}
}

// TestEditRefusesANameNoStoreCouldReadBack is the write half of the bound the
// read half has always had. yamlfile.ReadFile refuses an areas.yml over
// yamlfile.MaxBytes and Core.Load reads the vocabulary before the nibs, so a name
// long enough to carry the file past that limit leaves a store no command can
// open. The bound is on what an EDIT may write and not on what a store may hold:
// validateNodes still accepts a name of any length on load.
func TestEditRefusesANameNoStoreCouldReadBack(t *testing.T) {
	refusal := mustRefuse(t)(planRename(areaEditFixture, "web", strings.Repeat("x", maxNameRunes+1)))
	if !strings.Contains(refusal.Error(), "bounded at 200") {
		t.Errorf("error = %q, want it to name the bound", refusal)
	}

	// A name AT the bound is accepted, so the refusal above is the bound biting
	// and not the planner refusing every long name it is handed.
	if _, err := planRename(areaEditFixture, "web", strings.Repeat("x", maxNameRunes)); err != nil {
		t.Errorf("a name at the bound was refused: %v", err)
	}
}

// TestEditRefusesAnOutputPastTheConfigLimit is the same refusal reached with no
// long argument at all. The edit is a semantic-preserving RE-MARSHAL, so a
// vocabulary written with two-space indentation comes back with four and grows —
// enough for a file legally under yamlfile.MaxBytes to cross it on a rename that
// shortens the only name it touches.
func TestEditRefusesAnOutputPastTheConfigLimit(t *testing.T) {
	// Sized from the growth this re-marshal actually produces: 58,000 top-level
	// nodes read as 858,909 bytes and render as 1,090,918.
	var b strings.Builder
	b.WriteString("areas:\n")
	for i := range 58000 {
		fmt.Fprintf(&b, "- name: a%d\n", i)
	}
	b.WriteString("- name: web\n")
	vocab := b.String()
	if len(vocab) > yamlfile.MaxBytes {
		t.Fatalf("the fixture is %d bytes, past the %d-byte limit before any edit", len(vocab), yamlfile.MaxBytes)
	}

	refusal := mustRefuse(t)(planRename(vocab, "web", "ui"))
	if !strings.Contains(refusal.Error(), "configuration limit") {
		t.Errorf("error = %q, want it to name the limit it would pass", refusal)
	}
	assertEditRefusalRendering(t, refusal, false)
}

// TestEditRefusalsNameNoPathUntilAsked is the mechanism half of the rule the
// EditRefusal doc states: these sentences reach an unauthenticated HTTP client
// through the area mutations, so Error names no filesystem path, and the one
// surface entitled to name it asks for it through Naming.
//
// It drives every refusal planning builds, the path-free ones included, because
// a leak here is a per-branch one. The configuration-limit refusal is held to the
// same helper where its fixture is built.
func TestEditRefusalsNameNoPathUntilAsked(t *testing.T) {
	rename := func(path, newName string) func(vocab string) ([]byte, error) {
		return func(vocab string) ([]byte, error) { return planRename(vocab, path, newName) }
	}
	create := func(path string) func(vocab string) ([]byte, error) {
		return func(vocab string) ([]byte, error) { return planCreate(vocab, path, "", "") }
	}
	const plainVocab = "areas:\n    - name: web\n"
	tests := []struct {
		name  string
		vocab string
		plan  func(vocab string) ([]byte, error)
		want  string
		// pathFree marks a refusal that is not about the file, so Naming renders
		// exactly what Error does.
		pathFree bool
	}{
		{
			name: "a store with no areas.yml",
			plan: func(string) ([]byte, error) { return PlanRename(nil, false, "web", "frontend") },
			want: "no areas vocabulary at",
		},
		{
			name:  "an edit whose output the loader cannot read",
			vocab: "!!binary \"YXJlYXM=\": [{name: web}]\n",
			plan:  create("platform"),
			want:  "would leave this store's areas.yml unreadable",
		},
		{name: "an undeclared area", vocab: plainVocab, plan: rename("api", "v2"), want: "declares no area", pathFree: true},
		{name: "a children key that is not a sequence", vocab: "areas:\n    - name: web\n      children:\n", plan: create("web/panel"), want: "not a sequence", pathFree: true},
		{name: "no new path", vocab: plainVocab, plan: create(""), want: "at a path, and none was given", pathFree: true},
		{name: "a new path segment with no name", vocab: plainVocab, plan: create("web//panel"), want: "segment with no name", pathFree: true},
		{name: "a padded new path segment", vocab: plainVocab, plan: create("web/ panel"), want: "leading or trailing whitespace", pathFree: true},
		{name: "no new name", vocab: plainVocab, plan: rename("web", ""), want: "under a name, and none was given", pathFree: true},
		{name: "a padded new name", vocab: plainVocab, plan: rename("web", " frontend"), want: "leading or trailing whitespace", pathFree: true},
		{name: "a new name past the bound", vocab: plainVocab, plan: rename("web", strings.Repeat("a", maxNameRunes+1)), want: "characters long", pathFree: true},
		{name: "a file that is not YAML", vocab: "areas: [\n", want: "parsing"},
		{name: "more than one document", vocab: "areas:\n    - name: web\n---\nother: 1\n", want: "more than one YAML document"},
		{
			name:  "a vocabulary that inherits its content",
			vocab: "defaults: &d\n    description: shared\nareas:\n    - name: web\n      <<: *d\n",
			want:  "anchors, aliases or merge keys",
		},
		{name: "a file the loader cannot read as a vocabulary", vocab: "areas: 5\n", want: "cannot be read as an areas vocabulary"},
		{name: "a file declaring no areas to edit", vocab: "# just a comment\n", want: "declares no areas to edit"},
		{name: "a file with no `areas:` block", vocab: "other: 1\n", want: "declares no `areas:` block"},
		{
			name:  "an edit the loader would reject",
			vocab: "areas:\n    - name: web\n    - name: auth\n",
			plan:  rename("web", "auth"),
			want:  "declaring an unusable vocabulary",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.plan
			if plan == nil {
				plan = rename("web", "frontend")
			}
			refusal := mustRefuse(t)(plan(tt.vocab))
			if !strings.Contains(refusal.Error(), tt.want) {
				t.Errorf("Error = %q, want substring %q — the branch under test is not the one that fired", refusal.Error(), tt.want)
			}
			assertEditRefusalRendering(t, refusal, tt.pathFree)
		})
	}
}

// assertEditRefusalRendering holds a refusal to EditRefusal's two renderings:
// Error names no file, Naming names the file it is given when the refusal is
// about one, and neither carries a fmt verb error.
func assertEditRefusalRendering(t *testing.T, refusal *EditRefusal, pathFree bool) {
	t.Helper()
	msg := refusal.Error()
	if strings.Contains(msg, someAreasFile) {
		t.Errorf("error = %q names a file path, which reaches an HTTP client verbatim", msg)
	}
	if strings.Contains(msg, "%!") {
		t.Errorf("error = %q carries a fmt verb error", msg)
	}
	named := refusal.Naming(someAreasFile)
	if strings.Contains(named, "%!") {
		t.Errorf("Naming = %q carries a fmt verb error", named)
	}
	if pathFree {
		if named != msg {
			t.Errorf("Naming = %q, want it to render as Error (%q) for a refusal not about the file", named, msg)
		}
		return
	}
	if !strings.Contains(named, someAreasFile) {
		t.Errorf("Naming = %q, want it to name %s", named, someAreasFile)
	}
}

// TestEditRefusesAKeyTheTreeCannotMatch drives the re-read backstop in planEdit.
// A `!!binary` key decodes to "areas", so the loader binds it, but
// yamlfile.MappingValue compares the literal scalar and cannot see it — and the
// inheritance gate does not fire, since the key carries no anchor, alias or
// merge key. The create then writes a literal `areas:` beside it, binding the
// field twice, which the re-read has to catch.
func TestEditRefusesAKeyTheTreeCannotMatch(t *testing.T) {
	refusal := mustRefuse(t)(planCreate("!!binary \"YXJlYXM=\": [{name: web}]\n", "infra", "", ""))
	if !strings.Contains(refusal.Error(), "unreadable") {
		t.Errorf("Error = %q, want the re-read's refusal — another branch fired first", refusal.Error())
	}
}
