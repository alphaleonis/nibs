package config

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/store"
	"gopkg.in/yaml.v3"
)

// AreaEditRefusal is a refusal about the config file's CONTENT — a vocabulary
// declared in a shape these edits cannot address, or a result the loader would
// reject — as opposed to a failure to read or write the file.
//
// The two are separated because the caller reports them differently: content is
// the caller's argument to fix (a validation refusal), where a filesystem error
// is the machine's. Nothing about a refusal is repaired by rerunning, which is
// the sentence the CLI must not print for one.
//
// Error() NAMES NO FILESYSTEM PATH, and that is a mechanism rather than a
// convention: these refusals reach an unauthenticated HTTP client through the
// area mutations, which has no business knowing where the store sits on disk —
// an absolute path there discloses the operating-system username and the
// project layout. A refusal about a file carries it in File and renders it only
// through Naming, so a new call site leaks nothing by forgetting to redact.
type AreaEditRefusal struct {
	// File is the config file the refusal is about, empty for one that judges
	// an argument alone.
	File string

	// msg is the sentence Error() gives: File rendered as a path-free noun
	// phrase. format and args re-render the SAME sentence naming the file, and
	// are nil when File is empty. One format string produces both, so the two
	// renderings cannot drift apart.
	msg    string
	format string
	args   []any
}

func (e *AreaEditRefusal) Error() string { return e.msg }

// Naming renders this refusal with the file named as file — for a surface whose
// reader owns the directory the store sits in, which is the CLI and not the
// wire. A refusal that names no file is returned unchanged.
func (e *AreaEditRefusal) Naming(file string) string {
	if e.format == "" {
		return e.msg
	}
	return fmt.Sprintf(e.format, append([]any{file}, e.args...)...)
}

func refuseAreaEdit(format string, a ...any) error {
	return &AreaEditRefusal{msg: fmt.Sprintf(format, a...)}
}

// storedAreasNoun is how a path-free refusal refers to the file it is about.
const storedAreasNoun = "this store's areas.yml"

// refuseAreaEditAbout builds a refusal about a config FILE. format's FIRST verb
// is the file, and it is rendered twice from that one format: path-free for
// Error, named for Naming.
func refuseAreaEditAbout(file, format string, a ...any) error {
	return &AreaEditRefusal{
		File:   file,
		msg:    fmt.Sprintf(format, append([]any{storedAreasNoun}, a...)...),
		format: format,
		args:   a,
	}
}

// StoredAreaEdit is one edit to a store's `areas:` block, resolved against the
// file and rendered to bytes, but NOT yet written. Planning and writing are
// separate steps because the callers of both — `nibs area rename` and
// `nibs area rm` — must rewrite the member nibs BETWEEN them.
//
// That order is the whole point. A member rewrite is durable the moment it
// lands, so a config edit that can only fail after the cascade leaves the
// members carrying a path the vocabulary does not declare — and every later
// write to them is refused for it. Planning first moves every refusal this
// editor can make to before the first nib is touched, which leaves the store
// completely untouched instead. What can still fail at Write is the filesystem,
// and that is the one failure a rerun does repair.
//
// A plan is only as current as the file it was read from: the caller owes it the
// store's cross-process write lock (nibcore.AcquireStoreLock) across both steps,
// or a concurrent editor's write is lost when this one lands.
type StoredAreaEdit struct {
	path string
	out  []byte
}

// Path is the config file this edit will write.
func (e *StoredAreaEdit) Path() string { return e.path }

// Write applies the planned edit, keeping the file's permission bits and
// reporting a symlink it replaced, the way every other config writer does.
func (e *StoredAreaEdit) Write() (staleLinkTarget string, err error) {
	return writeConfigPreservingMode(e.path, e.out)
}

// PlanRenameStoredArea resolves a rename of the declared area at path in the
// config inside storeDir: the node's `name:` becomes newName, and nothing else
// about it changes — it keeps its description, color, order, children and its
// place in the tree.
//
// Renaming is a NAME edit, not a move, so the caller supplies a bare name and
// this never re-parents anything. Whether the new name is one the vocabulary can
// hold is the caller's refusal to make — `nibs area rename` has the better
// message for it — but it is CHECKED here too, by ValidateAreaName and by the
// re-read planStoredAreaEdit makes of its own output, because the file this
// writes has to be one the loader can read.
func PlanRenameStoredArea(storeDir, path, newName string) (*StoredAreaEdit, error) {
	if err := ValidateAreaName(newName); err != nil {
		return nil, err
	}
	return planStoredAreaEdit(storeDir, refuseMissingVocabulary, func(areas *yaml.Node) error {
		found, err := findStoredArea(areas, path)
		if err != nil {
			return err
		}
		name := mappingValueNode(found.node, "name")
		if name == nil {
			return refuseAreaEdit("the declared area %q has no `name:` key to rename", RenderAreaPath(path))
		}
		name.Kind = yaml.ScalarNode
		name.Tag = "!!str"
		name.Value = newName
		// Drop any style the old scalar carried: the value is new, so the old
		// rendering does not describe it.
		name.Style = 0
		return nil
	})
}

// PlanRemoveStoredArea resolves retiring the declared area at path, together
// with every area declared beneath it — a subtree is what a node heads, and
// leaving its children behind would declare paths with no parent.
//
// A `children:` key emptied by the removal goes with it: it describes a shape
// the surviving node no longer has. The top-level `areas:` key does not — that
// is the block the project authored, so retiring the last area leaves it empty
// rather than deleting it, which keeps whatever the project wrote above it.
// An empty block declares no areas (AreasDeclared), which is the state the axis
// then reports.
func PlanRemoveStoredArea(storeDir, path string) (*StoredAreaEdit, error) {
	return planStoredAreaEdit(storeDir, refuseMissingVocabulary, func(areas *yaml.Node) error {
		found, err := findStoredArea(areas, path)
		if err != nil {
			return err
		}
		found.seq.Content = slices.Delete(found.seq.Content, found.index, found.index+1)
		if len(found.seq.Content) == 0 && found.owner != nil {
			removeMappingKey(found.owner, "children")
		}
		return nil
	})
}

// RenameStoredArea plans and writes a rename in one step, for a caller with
// nothing to do between the two.
func RenameStoredArea(storeDir, path, newName string) (staleLinkTarget string, err error) {
	edit, err := PlanRenameStoredArea(storeDir, path, newName)
	if err != nil {
		return "", err
	}
	return edit.Write()
}

// RemoveStoredArea plans and writes a retire in one step, for a caller with
// nothing to do between the two.
func RemoveStoredArea(storeDir, path string) (staleLinkTarget string, err error) {
	edit, err := PlanRemoveStoredArea(storeDir, path)
	if err != nil {
		return "", err
	}
	return edit.Write()
}

// storedArea is where one declared node sits in the vocabulary's node tree: the
// node itself, the sequence holding it and its position in that sequence, and
// the mapping whose `children:` key that sequence is — nil for a top-level node,
// whose sequence is the `areas:` block itself.
type storedArea struct {
	node  *yaml.Node
	seq   *yaml.Node
	index int
	owner *yaml.Node
}

// missingVocabulary is what one edit needs of a store whose areas.yml declares
// nothing, and the two answers are not interchangeable.
//
// Rename and remove both NAME a node the file would have to already declare, so
// a vocabulary declaring nothing is a refusal with nothing to say about the
// argument. A create names a node that by definition is not declared yet, and
// the first one in a store is exactly the call that finds nothing declared — so
// it starts from a synthesized empty document and reaches the same single write
// path.
//
// Declaring nothing is several shapes, and they are one disposition because a
// caller cannot tell them apart: no file at all, a zero-byte file, a file
// holding only comments, a file with other keys and no `areas:` block, an
// `areas:` key with no value under it, and an empty `areas:` sequence — the
// last of which is what PlanRemoveStoredArea leaves when the final declared
// area goes. The rest are hand-authored, since Areas.Save deletes the file
// rather than writing an empty one, and writing the file before the verb that
// populates it is an ordinary way to arrive here.
//
// Areas.Save is deliberately not that path. It marshals from the struct, which
// discards a hand-written file's comments, and it DELETES the file when nothing
// is declared — a different disposition for "no areas on disk" than the empty
// `areas:` block PlanRemoveStoredArea leaves behind. Two writers would be two
// notions of that state.
type missingVocabulary int

const (
	refuseMissingVocabulary missingVocabulary = iota
	synthesizeMissingVocabulary
)

// emptyAreasDocument is the vocabulary a store that has never declared one
// would have written: a document whose `areas:` block is there and empty.
func emptyAreasDocument() yaml.Node {
	return yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "areas"},
				{Kind: yaml.SequenceNode, Tag: "!!seq"},
			},
		}},
	}
}

// yamlInheritance is one inheritance construct as the file writes it, with the
// line it is written on. The refusal quotes it so the remedy names something
// that is actually there: a remedy phrased for the `areas:` block instead is a
// no-op on a file whose block is already literal and whose anchor sits
// elsewhere, and a caller can follow that one forever without the answer
// changing.
type yamlInheritance struct {
	construct string
	line      int
}

// inheritsContent returns the first of YAML's inheritance constructs the
// document reaches any of its content through — an anchor, an alias, or a `<<:`
// merge key — anywhere in the file, at any depth, and nil for a document that
// uses none.
//
// It is asked of the WHOLE document rather than of the `areas:` subtree, and
// the bluntness is the point: deciding whether a particular anchor "actually
// affects areas" is the reasoning it exists to replace. See the refusal in
// planStoredAreaEdit for what it buys. WHICH construct comes back is for the
// message alone — every one of them refuses, so the search stops at the first.
func inheritsContent(node *yaml.Node) *yamlInheritance {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		// Here for the predicate rather than for its wording: yaml.v3 refuses a
		// document whose alias precedes its anchor ("unknown anchor"), so the
		// walk never reaches an alias without having refused already.
		return &yamlInheritance{construct: fmt.Sprintf("the alias `*%s`", echoedYAMLName(node.Value)), line: node.Line}
	}
	if node.Anchor != "" {
		return &yamlInheritance{construct: fmt.Sprintf("the anchor `&%s`", echoedYAMLName(node.Anchor)), line: node.Line}
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "<<" {
				return &yamlInheritance{construct: "a `<<:` merge key", line: node.Content[i].Line}
			}
		}
	}
	for _, child := range node.Content {
		if found := inheritsContent(child); found != nil {
			return found
		}
	}
	return nil
}

// echoedYAMLName bounds and strips an anchor or alias name on its way into a
// message — file-sourced text, given the same treatment RenderAreaPath gives an
// echoed area path.
func echoedYAMLName(name string) string {
	return truncateListedArea(safetext.Strip(name))
}

// planStoredAreaEdit applies edit to the `areas:` sequence of the vocabulary
// inside storeDir and renders the result, without writing anything.
//
// The edit goes through a yaml.Node tree rather than Areas.Save because Save
// marshals the struct this build models, and a marshal keeps only what the
// struct has fields for: every comment goes, and so does any key a project put
// on a node that AreaConfig does not declare. This file is where a project
// keeps prose about its own vocabulary, so that loss is the expensive kind —
// which is why the node tree is edited in place and re-emitted instead.
//
// It is a semantic-preserving RE-MARSHAL, not a byte-preserving splice:
// yaml.Marshal re-emits the whole document from the node tree, so the file comes
// back with yaml.v3's layout rather than the project's. Measured on a
// round trip (TestStoredAreaEditRoundTripPreservesWhatItClaims), what survives is
// every comment — head, inline and footer — key order, nesting, keys this build
// does not model at any depth, every per-node field, and the file's permission
// bits. What does NOT survive is layout: indentation is normalized to four
// spaces, blank lines are dropped, a leading `---` goes, an inline comment loses
// its column alignment, and a folded scalar is re-flowed.
//
// A file that reaches any of its content through YAML's inheritance constructs
// — an anchor, an alias, a `<<:` merge key — is REFUSED before any write
// decision is made, and that refusal is what makes the decisions below sound.
// The loader resolves inheritance where this node tree sees only what is
// literally typed, so on such a file the two disagree about what is declared;
// and since YAML resolves an explicit key over a merged one, a key written on
// the tree's "absent" answer overrides the inherited one it could not see.
//
// Refusing the constructs is what makes a nil from mappingValueNode
// trustworthy, and it is not the only thing standing behind one. A key the
// loader binds can still be one this tree does not match — `!!binary
// "YXJlYXM="` is such a key, and it carries no anchor, alias or merge for the
// gate to catch — so the second mechanism is the re-read below: the literal key
// written on that nil leaves the decoder binding the same field twice, which it
// rejects, and the edit is refused with the file untouched.
//
// That backstop answers a divergence in how a key is WRITTEN, and not an
// inherited one. YAML lets an explicit key override what a merge supplied, so
// there the field is bound once, the re-read is satisfied, and what the merge
// carried is simply gone. That case is the gate's.
//
// A file holding more than one YAML document is REFUSED rather than edited: the
// re-marshal emits only the first, so writing it back would silently delete the
// rest. nibs never writes such a file, so refusing costs nothing a project did
// not do on purpose, and the alternative is data loss on an exit-0 success.
//
// The edited document is re-read as an Areas and re-validated BEFORE it is
// returned. A vocabulary the loader rejects is a store no command can open, so this
// is the difference between a refused edit and a project that has to be repaired
// by hand — and it is where the vocabulary's uniqueness rule is enforced against
// whatever reached this function.
func planStoredAreaEdit(storeDir string, missing missingVocabulary, edit func(areas *yaml.Node) error) (*StoredAreaEdit, error) {
	path := store.NewLayout(storeDir).AreasPath()
	doc := emptyAreasDocument()
	data, readErr := ReadConfigFile(path)
	switch {
	case readErr == nil:
		parsed, err := soleConfigDocument(data)
		if err != nil {
			if errors.Is(err, errMultipleConfigDocuments) {
				return nil, refuseAreaEditAbout(path,
					"%s holds more than one YAML document, and editing its areas would rewrite the file from the first one alone — move anything after the `---` into its own file, or delete the marker if nothing follows it, then rerun",
				)
			}
			return nil, refuseAreaEditAbout(path, "parsing %s: %v", err)
		}
		doc = parsed
	case errors.Is(readErr, fs.ErrNotExist) && missing == synthesizeMissingVocabulary:
		// The synthesized document stands in for the file, and the edit then
		// takes the same path every other one does. What that costs is what a
		// file which is not there was holding: no comments, no key this build
		// does not model, nothing.
	case errors.Is(readErr, fs.ErrNotExist):
		return nil, refuseAreaEditAbout(path, "no areas vocabulary at %s to edit; a store declares its areas there, beside its config.yml")
	default:
		return nil, readErr
	}

	if found := inheritsContent(&doc); found != nil {
		return nil, refuseAreaEditAbout(path,
			"%s uses %s at line %d, and these edits cannot safely change a file that inherits any of its content — rewrite it without anchors, aliases or merge keys, writing out in place whatever they stand for, then rerun",
			found.construct, found.line)
	}
	if err := yaml.Unmarshal(data, new(Areas)); err != nil {
		// Not "the edit would leave it unreadable": the file arrived that way,
		// so the remedy is to repair the file and not to change the argument.
		return nil, refuseAreaEditAbout(path, "%s cannot be read as an areas vocabulary (%v) — repair it, then rerun", err)
	}

	bootstrap := missing == synthesizeMissingVocabulary
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		if !bootstrap {
			return nil, refuseAreaEditAbout(path, "%s declares no areas to edit")
		}
		// A file the decoder produced no node for: soleConfigDocument answers
		// that way only for the io.EOF the decoder reports when the stream holds
		// no document at all, so nothing in it is data there would be a node to
		// carry. The bytes go on as the synthesized document's head comment
		// instead — in such a file they are the whole of what somebody wrote —
		// and yaml.v3 re-emits a head comment line for line, prefixing `#` where
		// a line does not already start with one.
		doc = emptyAreasDocument()
		doc.Content[0].HeadComment = strings.TrimRight(string(data), "\r\n \t")
	}
	root := doc.Content[0]
	areas := mappingValueNode(root, "areas")
	// Only a create may synthesize the block, and only where the file has none:
	// a vocabulary that declares anything reaches here with `areas:` already a
	// literal sequence, so no case below can fire on one.
	if bootstrap {
		switch {
		case root.Kind != yaml.MappingNode:
			// A root that is not a mapping to add a key to, which is what a file
			// holding only `---`, `null` or `~` parses as. The node is converted
			// rather than replaced, which keeps the comments the file put on it
			// — the same reason the third case converts.
			root.Kind, root.Tag, root.Value, root.Style, root.Content = yaml.MappingNode, "!!map", "", 0, nil
			areas = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			appendMappingEntry(root, "areas", areas)
		case areas == nil:
			// The `areas:` block is what is missing, not the file: the key is
			// added to the document that is there, so every other key it
			// carries — and every comment on them — is re-emitted with it.
			areas = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			appendMappingEntry(root, "areas", areas)
		case areas.Kind != yaml.SequenceNode:
			// `areas:` with no value under it — the shape `nibs area list` tells
			// a store declaring none to write, and a null value is the only
			// non-sequence one that reaches here: every other `areas:` value
			// fails to unmarshal as a vocabulary and is refused above. The node
			// is converted rather than replaced, which keeps the comments the
			// file put on it.
			areas.Kind, areas.Tag, areas.Value, areas.Style, areas.Content = yaml.SequenceNode, "!!seq", "", 0, nil
		}
	}
	if areas == nil || areas.Kind != yaml.SequenceNode {
		return nil, refuseAreaEditAbout(path, "%s declares no `areas:` block")
	}
	if err := edit(areas); err != nil {
		return nil, err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	// The file this WRITES is bounded the way the file it READ is. ReadConfigFile
	// refuses anything over MaxConfigBytes, and Core.Load reads the vocabulary
	// before the nibs and aborts on that refusal, so an areas.yml written past the
	// cap is a store no command can open — this edit's own rerun included, which
	// is what makes it repairable by hand alone.
	//
	// Asked of the rendered output rather than of the edit's arguments because
	// this is a semantic-preserving RE-MARSHAL: indentation is normalized and
	// quoting added, so a vocabulary already close to the cap crosses it on an
	// edit that adds nothing at all. ValidateAreaName answers for the argument,
	// where the message can name what to shorten.
	if len(out) > MaxConfigBytes {
		return nil, refuseAreaEditAbout(path,
			"the edit would leave %s at %d bytes, past the %d-byte configuration limit, and a store whose areas.yml is over that limit cannot be opened by any command — declare fewer areas, or shorter names, then rerun",
			len(out), MaxConfigBytes)
	}
	var edited Areas
	if err := yaml.Unmarshal(out, &edited); err != nil {
		return nil, refuseAreaEditAbout(path, "the edit would leave %s unreadable: %v", err)
	}
	if err := edited.Validate(); err != nil {
		return nil, refuseAreaEditAbout(path, "the edit would leave %s declaring an unusable vocabulary: %v", err)
	}
	return &StoredAreaEdit{path: path, out: out}, nil
}

// findStoredArea locates the node one area path names, descending the sequence
// one segment at a time so `web/dashboard` finds the child of `web` and never a
// top-level node that happens to be named that way — the same resolution
// findArea makes over the loaded model.
//
// It matches a LITERAL `name:` key, and that is the same set of nodes the loaded
// model matches: planStoredAreaEdit refuses a file whose content is inherited,
// which is what would otherwise let cfg.IsValid see a node this search cannot.
func findStoredArea(areas *yaml.Node, path string) (storedArea, error) {
	seq, owner := areas, (*yaml.Node)(nil)
	rest := path
	for rest != "" {
		name, tail, nested := strings.Cut(rest, AreaPathSeparator)
		index := -1
		for i, item := range seq.Content {
			if n := mappingValueNode(item, "name"); n != nil && n.Value == name {
				index = i
				break
			}
		}
		if index < 0 {
			return storedArea{}, missingStoredArea(path)
		}
		node := seq.Content[index]
		if !nested {
			return storedArea{node: node, seq: seq, index: index, owner: owner}, nil
		}
		children := mappingValueNode(node, "children")
		if children == nil || children.Kind != yaml.SequenceNode {
			return storedArea{}, missingStoredArea(path)
		}
		seq, owner, rest = children, node, tail
	}
	return storedArea{}, missingStoredArea(path)
}

// missingStoredArea reports a path the node tree could not resolve.
func missingStoredArea(path string) error {
	return refuseAreaEdit("this store's areas.yml declares no area %q", RenderAreaPath(path))
}

// appendMappingEntry adds `key: value` to the end of a YAML mapping, which is
// where the file will read it. removeMappingKey is its counterpart.
func appendMappingEntry(node *yaml.Node, key string, value *yaml.Node) {
	node.Content = append(node.Content, scalarNode(key), value)
}

// scalarNode is one plain string as the file carries it, with no style of its
// own so yaml.v3 quotes it only where the value needs quoting.
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// removeMappingKey drops key and its value from a YAML mapping.
func removeMappingKey(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = slices.Delete(node.Content, i, i+2)
			return
		}
	}
}

// PlanCreateStoredArea resolves declaring a new area at path, carrying the
// description and color it is given and omitting the ones it is not.
//
// path is the FULL path of the new node, the same shape PlanRemoveStoredArea
// takes, so `web/dashboard` declares `dashboard` under `web`. A parent the file
// does not declare is REFUSED rather than created along the way: the vocabulary
// is authorization data, so one typo minting two permanent areas is the failure
// a declared vocabulary exists to prevent.
//
// The node is APPENDED after the last sibling, and there is no argument for
// placing it elsewhere. Paths enumerates in declaration order and `nibs area
// list` renders that order, so appending is the one placement that leaves every
// node already declared exactly where the project put it.
//
// Unlike its two neighbours this edit has no member cascade, and the reason is
// narrower than "a new area has no members": a nib may already CARRY the path,
// left on it by a retire or a hand edit, and every write to that nib is refused
// until the vocabulary declares it again (Areas.ValidateStored). Declaring it is
// that repair, and the repair rewrites nothing — the value the nib is holding
// becomes a declared one.
func PlanCreateStoredArea(storeDir, path, description, color string) (*StoredAreaEdit, error) {
	if err := ValidateNewAreaPath(path); err != nil {
		return nil, err
	}
	parent, name := splitStoredAreaPath(path)
	return planStoredAreaEdit(storeDir, synthesizeMissingVocabulary, func(areas *yaml.Node) error {
		seq := areas
		if parent != "" {
			found, err := findStoredArea(areas, parent)
			if err != nil {
				return err
			}
			if seq, err = storedAreaChildren(found.node, parent); err != nil {
				return err
			}
		}
		seq.Content = append(seq.Content, newStoredAreaNode(name, description, color))
		return nil
	})
}

// ValidateNewAreaPath refuses a path no declared node could answer to, before
// the file is even read.
//
// A create is the one edit that supplies a name the vocabulary has never held,
// so it is the one that has to judge one. Each SEGMENT is put through the rule
// validateAreaNodes applies on load, because the argument names the whole path
// and an empty segment is how a stray separator arrives: `/infra` splits into an
// empty root and `infra`, which would otherwise declare a root the caller did
// not ask for. That load rule's remaining clause — a name holding the separator
// — cannot fire on a segment splitting on the separator produced.
//
// planStoredAreaEdit re-reads the edited document and revalidates it, so this is
// not what keeps a malformed vocabulary off disk. What it buys is a message
// about the ARGUMENT: the backstop can only report that the file would be
// unusable, and names a node by its position in a block the caller never wrote.
//
// It is exported so `nibs area add` can ask it BEFORE taking the store's write
// lock, which is a blocking flock with no timeout that prints nothing while it
// waits: a question the argument alone answers must not sit silent behind
// another writer. PlanCreateStoredArea asks it again regardless — the planner is
// the API, and a caller reaching it directly gets the same refusal.
func ValidateNewAreaPath(path string) error {
	if path == "" {
		return refuseAreaEdit("an area is declared at a path, and none was given")
	}
	for _, segment := range strings.Split(path, AreaPathSeparator) {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			return refuseAreaEdit("the area path %q has a segment with no name in it; every declared area needs one",
				RenderAreaPath(path))
		}
		if trimmed != segment {
			return refuseAreaEdit("the area name %q has leading or trailing whitespace; an `area:` value would have to carry the same spaces to match it",
				RenderAreaPath(segment))
		}
		// Only the length clause can fire: the two above have already answered
		// for an empty or padded segment, in wording that names the whole path.
		if err := ValidateAreaName(segment); err != nil {
			return err
		}
	}
	return nil
}

// maxAreaNameRunes bounds the length of a name an EDIT may write. It is
// deliberately NOT a bound on what a store may HOLD: validateAreaNodes runs on
// every load and is not tightened for exactly that reason (see the note there),
// so a name already declared keeps loading whatever its length.
//
// The number is maxListedAreaRunes, the bound RenderAreaPath already applies to
// every path a message echoes, so a name an edit may write is never unboundedly
// longer than what a refusal about it can show. It is not a promise that every
// echo is complete: a refusal quoting a nested PATH built from the name renders
// the parent segments too, and RenderAreaPath elides what those push past the
// bound.
const maxAreaNameRunes = maxListedAreaRunes

// ValidateAreaName refuses a name an edit must not write, before the file is
// even read. It is the rename's counterpart to ValidateNewAreaPath, which asks
// it of every segment of a create's path.
//
// The LENGTH clause is the one this adds over the load-time rule, and it is
// about the file the edit produces rather than about the name. A name arrives
// here as caller-supplied text of no bounded length — the wire carries up to
// cmd/serve.go's 4 MiB request body — while a config file is read through
// ReadConfigFile, which refuses anything over MaxConfigBytes. Core.Load reads
// the vocabulary before it walks the nibs and aborts on that refusal, so an
// areas.yml written past the cap is a store no command can open, this edit's own
// rerun included, repairable only by hand.
//
// It does not stand alone: a rename that adds nothing can still carry a
// vocabulary already near the cap past it, since the re-marshal normalizes
// indentation. planStoredAreaEdit measures its rendered output for that.
func ValidateAreaName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return refuseAreaEdit("an area is declared under a name, and none was given")
	}
	if trimmed != name {
		return refuseAreaEdit("the area name %q has leading or trailing whitespace; an `area:` value would have to carry the same spaces to match it",
			RenderAreaPath(name))
	}
	// The name is not echoed: it is the thing that is too long, and a message
	// quoting 200 runes of it says nothing the count does not.
	if n := utf8.RuneCountInString(name); n > maxAreaNameRunes {
		return refuseAreaEdit("the area name is %d characters long, and a declared name is bounded at %d — a longer one only writes an areas.yml no command could read back",
			n, maxAreaNameRunes)
	}
	return nil
}

// splitStoredAreaPath separates a path into its parent's path — empty for a
// root — and the name of the node it ends in.
func splitStoredAreaPath(path string) (parent, name string) {
	i := strings.LastIndex(path, AreaPathSeparator)
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+len(AreaPathSeparator):]
}

// storedAreaChildren returns the sequence a declared node's `children:` key
// holds, adding an empty one when the node has no children yet — a node gains
// the key on the day it gains its first child, which is the same shape
// PlanRemoveStoredArea drops when the last one goes.
//
// No literal `children:` means the node has no children, and it means that only
// because planStoredAreaEdit refused a file whose content is inherited: children
// arriving through a merge key would be invisible here, and the key written on
// this answer would override them.
func storedAreaChildren(node *yaml.Node, path string) (*yaml.Node, error) {
	switch children := mappingValueNode(node, "children"); {
	case children == nil:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		appendMappingEntry(node, "children", seq)
		return seq, nil
	case children.Kind == yaml.SequenceNode:
		return children, nil
	default:
		return nil, refuseAreaEdit(
			"the declared area %q has a `children:` key that is not a sequence, so there is nothing to declare a child in",
			RenderAreaPath(path))
	}
}

// newStoredAreaNode builds the mapping one declared area is written as, in the
// key order AreaConfig declares. A field given nothing is left OUT rather than
// written empty, which is the same thing the struct's `omitempty` tags do — an
// explicit `description: ""` reads as one somebody deleted.
func newStoredAreaNode(name, description, color string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendMappingEntry(node, "name", scalarNode(name))
	if description != "" {
		appendMappingEntry(node, "description", scalarNode(description))
	}
	if color != "" {
		appendMappingEntry(node, "color", scalarNode(color))
	}
	return node
}

// CreateStoredArea plans and writes a create in one step, for a caller with
// nothing to do between the two.
func CreateStoredArea(storeDir, path, description, color string) (staleLinkTarget string, err error) {
	edit, err := PlanCreateStoredArea(storeDir, path, description, color)
	if err != nil {
		return "", err
	}
	return edit.Write()
}
