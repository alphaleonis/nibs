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

// AreaEditRefusal is a refusal about the config file's content, not a failure to
// read or write it. Report as a validation error; retrying will not help.
type AreaEditRefusal struct {
	// The config file the error relates to. May be empty.
	File string

	msg    string
	format string
	args   []any
}

func (e *AreaEditRefusal) Error() string { return e.msg }

// Naming returns Error()'s message with file named in it. A refusal with no File
// renders the same either way.
func (e *AreaEditRefusal) Naming(file string) string {
	if e.format == "" {
		return e.msg
	}
	return fmt.Sprintf(e.format, append([]any{file}, e.args...)...)
}

func refuseAreaEdit(format string, a ...any) error {
	return &AreaEditRefusal{msg: fmt.Sprintf(format, a...)}
}

const storedAreasNoun = "this store's areas.yml"

// refuseAreaEditAbout keeps the path out of Error(), which reaches an
// unauthenticated HTTP client, and in File for Naming to render.
func refuseAreaEditAbout(file, format string, a ...any) error {
	return &AreaEditRefusal{
		File:   file,
		msg:    fmt.Sprintf(format, append([]any{storedAreasNoun}, a...)...),
		format: format,
		args:   a,
	}
}

// StoredAreaEdit is a store's areas.yml rendered with one edit applied, not yet
// written. Hold the store's write lock (nibcore.AcquireStoreLock) from planning
// through Write.
type StoredAreaEdit struct {
	path string
	out  []byte // the whole rendered file, not a fragment
}

func (e *StoredAreaEdit) Path() string { return e.path }

// Write applies the edit, keeping the file's permission bits. A symlink at the
// path is replaced by a regular file, reported as staleLinkTarget. Any error here
// is the filesystem's; every refusal fires at plan time.
func (e *StoredAreaEdit) Write() (staleLinkTarget string, err error) {
	return writeConfigPreservingMode(e.path, e.out)
}

// PlanRenameStoredArea plans a rename of the declared area at path in storeDir's
// areas.yml, writing nothing. newName is a bare name; nothing is re-parented.
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
		// Drop any style the old scalar carried; it described the old value.
		name.Style = 0
		return nil
	})
}

// PlanRemoveStoredArea plans retiring the declared area at path and every area
// beneath it, writing nothing. An emptied `children:` key goes with them; the
// top-level `areas:` key stays, empty.
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

// RenameStoredArea plans and writes a rename in one step.
func RenameStoredArea(storeDir, path, newName string) (staleLinkTarget string, err error) {
	edit, err := PlanRenameStoredArea(storeDir, path, newName)
	if err != nil {
		return "", err
	}
	return edit.Write()
}

// RemoveStoredArea plans and writes a retire in one step.
func RemoveStoredArea(storeDir, path string) (staleLinkTarget string, err error) {
	edit, err := PlanRemoveStoredArea(storeDir, path)
	if err != nil {
		return "", err
	}
	return edit.Write()
}

// storedArea is one declared node found in the vocabulary's node tree.
type storedArea struct {
	node  *yaml.Node
	seq   *yaml.Node // the sequence holding node; the `areas:` block at top level
	index int        // node's position in seq
	owner *yaml.Node // the mapping whose `children:` key seq is; nil at top level
}

// missingVocabulary says what planStoredAreaEdit does when the store has no
// areas.yml, or one nothing parses out of.
type missingVocabulary int

const (
	refuseMissingVocabulary     missingVocabulary = iota // rename, remove: the node must exist
	synthesizeMissingVocabulary                          // create: start from an empty document
)

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

// yamlInheritance is one inheritance construct found in the document.
type yamlInheritance struct {
	construct string // as the message renders it, e.g. "the anchor `&x`"
	line      int
}

// inheritsContent returns an inheritance construct the document reaches content
// through — an anchor, an alias, or a `<<:` merge key — at any depth, and nil for
// a document that uses none. It searches the whole document, not the `areas:`
// subtree.
func inheritsContent(node *yaml.Node) *yamlInheritance {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		// Unreachable: a valid document presents the anchor first, and an alias
		// preceding its anchor is refused by yaml.v3 before the walk starts.
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

// echoedYAMLName bounds and strips a file-sourced name on its way into a message.
func echoedYAMLName(name string) string {
	return truncateListedArea(safetext.Strip(name))
}

// planStoredAreaEdit applies edit to the `areas:` sequence of the vocabulary
// inside storeDir and renders the result, writing nothing. It edits a yaml.Node
// tree rather than going through Areas.Save, which drops the file's comments and
// any key the Areas and AreaConfig structs do not model; the render is a
// re-marshal, so the file comes back with yaml.v3's layout.
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
		// Nothing to read; the synthesized empty document stands in.
	case errors.Is(readErr, fs.ErrNotExist):
		return nil, refuseAreaEditAbout(path, "no areas vocabulary at %s to edit; a store declares its areas there, beside its config.yml")
	default:
		return nil, readErr
	}

	// The loader resolves inheritance and this tree does not, so a key written on
	// this tree's "absent" answer would override a merged one it could not see.
	if found := inheritsContent(&doc); found != nil {
		return nil, refuseAreaEditAbout(path,
			"%s uses %s at line %d, and these edits cannot safely change a file that inherits any of its content — rewrite it without anchors, aliases or merge keys, writing out in place whatever they stand for, then rerun",
			found.construct, found.line)
	}
	if err := yaml.Unmarshal(data, new(Areas)); err != nil {
		// On the file as read, so an already-broken file is not reported as the
		// edit breaking it; the re-read below answers for the output.
		return nil, refuseAreaEditAbout(path, "%s cannot be read as an areas vocabulary (%v) — repair it, then rerun", err)
	}

	bootstrap := missing == synthesizeMissingVocabulary
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		if !bootstrap {
			return nil, refuseAreaEditAbout(path, "%s declares no areas to edit")
		}
		// Nothing parsed, so these bytes are all anybody wrote; yaml.v3 re-emits
		// them as a comment block.
		doc = emptyAreasDocument()
		doc.Content[0].HeadComment = strings.TrimRight(string(data), "\r\n \t")
	}
	root := doc.Content[0]
	areas := mappingValueNode(root, "areas")
	if bootstrap {
		switch {
		case root.Kind != yaml.MappingNode:
			// What a file holding only `---`, `null` or `~` parses as. Converting
			// the node in place keeps the comments the file put on it.
			root.Kind, root.Tag, root.Value, root.Style, root.Content = yaml.MappingNode, "!!map", "", 0, nil
			areas = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			appendMappingEntry(root, "areas", areas)
		case areas == nil:
			// The block is missing, or written in a form this tree cannot match.
			areas = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			appendMappingEntry(root, "areas", areas)
		case areas.Kind != yaml.SequenceNode:
			// `areas:` with no value — every other non-sequence fails the
			// unmarshal above.
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
	// The re-marshal normalizes indentation, so a vocabulary near the cap crosses
	// it on an edit that adds nothing.
	if len(out) > MaxConfigBytes {
		return nil, refuseAreaEditAbout(path,
			"the edit would leave %s at %d bytes, past the %d-byte configuration limit, and a store whose areas.yml is over that limit cannot be opened by any command — declare fewer areas, or shorter names, then rerun",
			len(out), MaxConfigBytes)
	}
	// Catches a key the loader binds but this tree cannot match: `!!binary
	// "YXJlYXM="` carries no anchor or merge for the gate above, and the literal
	// `areas:` appended beside it binds the field twice, which fails here.
	var edited Areas
	if err := yaml.Unmarshal(out, &edited); err != nil {
		return nil, refuseAreaEditAbout(path, "the edit would leave %s unreadable: %v", err)
	}
	if err := edited.Validate(); err != nil {
		return nil, refuseAreaEditAbout(path, "the edit would leave %s declaring an unusable vocabulary: %v", err)
	}
	return &StoredAreaEdit{path: path, out: out}, nil
}

// findStoredArea locates the node one area path (`web/dashboard`) names, matching
// a literal `name:` key. A key the loader binds but this match cannot see — a
// `!!binary` spelling — makes the path look undeclared, so the edit is refused
// rather than aimed at the wrong node.
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

func missingStoredArea(path string) error {
	return refuseAreaEdit("%s declares no area %q", storedAreasNoun, RenderAreaPath(path))
}

// appendMappingEntry appends unconditionally: a key already present is
// duplicated, which planStoredAreaEdit's re-read turns into a refusal.
func appendMappingEntry(node *yaml.Node, key string, value *yaml.Node) {
	node.Content = append(node.Content, scalarNode(key), value)
}

// scalarNode leaves Style unset, so yaml.v3 quotes only what needs quoting.
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

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

// PlanCreateStoredArea plans declaring a new area at path, writing nothing. path
// is the full path, so `web/dashboard` declares `dashboard` under `web`; a parent
// the file does not declare is refused rather than created along the way.
//
// No nib needs rewriting alongside this edit: a nib already carrying the path
// becomes valid the moment the vocabulary declares it.
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

func refuseAreaNameWhitespace(name string) error {
	return refuseAreaEdit(
		"the area name %q has leading or trailing whitespace; an `area:` value would have to carry the same spaces to match it",
		RenderAreaPath(name))
}

// ValidateNewAreaPath refuses a path that could never name an area, without
// reading the file.
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
			return refuseAreaNameWhitespace(segment)
		}
		// Only the length clause can fire; the two above answer for an empty or
		// padded segment, in wording that names the whole path.
		if err := ValidateAreaName(segment); err != nil {
			return err
		}
	}
	return nil
}

// maxAreaNameRunes bounds the length of a name an EDIT may write, not what a
// store may HOLD. It is the bound RenderAreaPath applies.
const maxAreaNameRunes = maxListedAreaRunes

// ValidateAreaName refuses a name an edit must not write, before the file is
// read. The length clause is what it adds over the load-time rule, which bounds
// no name.
func ValidateAreaName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return refuseAreaEdit("an area is declared under a name, and none was given")
	}
	if trimmed != name {
		return refuseAreaNameWhitespace(name)
	}
	// The name is not echoed: quoting it says nothing the count does not.
	if n := utf8.RuneCountInString(name); n > maxAreaNameRunes {
		return refuseAreaEdit("the area name is %d characters long, and a declared name is bounded at %d — a longer one only writes an areas.yml no command could read back",
			n, maxAreaNameRunes)
	}
	return nil
}

func splitStoredAreaPath(path string) (parent, name string) {
	i := strings.LastIndex(path, AreaPathSeparator)
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+len(AreaPathSeparator):]
}

// storedAreaChildren returns the sequence a node's `children:` key holds, adding
// an empty one when it has none — the shape PlanRemoveStoredArea drops when the
// last child goes.
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

// newStoredAreaNode builds the mapping one declared area is written as, in
// AreaConfig's key order. A field given nothing is left out; an explicit
// `description: ""` reads as one somebody deleted.
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

// CreateStoredArea plans and writes a create in one step.
func CreateStoredArea(storeDir, path, description, color string) (staleLinkTarget string, err error) {
	edit, err := PlanCreateStoredArea(storeDir, path, description, color)
	if err != nil {
		return "", err
	}
	return edit.Write()
}
