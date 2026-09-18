package area

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/yamlfile"
	"gopkg.in/yaml.v3"
)

// EditRefusal is a refusal about an areas file's content, not a failure to read
// or write it. Report as a validation error; retrying will not help.
type EditRefusal struct {
	// The areas file the refusal relates to, set by the caller that read it. May
	// be empty.
	File string

	msg    string
	render func(file string) string
}

// Error renders the refusal without a path: it reaches an unauthenticated HTTP
// client. Naming is for a reader who may be told where the file is.
func (e *EditRefusal) Error() string { return e.msg }

// Naming returns Error()'s message with file named in it. A refusal that is not
// about a file renders the same either way.
func (e *EditRefusal) Naming(file string) string {
	if e.render == nil {
		return e.msg
	}
	return e.render(file)
}

func refuseEdit(format string, a ...any) error {
	return &EditRefusal{msg: fmt.Sprintf(format, a...)}
}

const storedAreasNoun = "this store's areas.yml"

// refuseEditAbout builds a refusal that names the file. render receives the file
// as a parameter rather than a format position, so the sentence is an ordinary
// fmt.Sprintf that vet checks.
func refuseEditAbout(render func(file string) string) error {
	return &EditRefusal{msg: render(storedAreasNoun), render: render}
}

// PlanCreate declares a new area at path in current, the areas file's content,
// and returns the whole file rendered with the edit applied. exists reports
// whether the file exists at all; an absent one is started from an empty
// document. path is the full path, so `web/dashboard` declares `dashboard` under
// `web`; a parent the file does not declare is refused rather than created along
// the way.
//
// No nib needs rewriting alongside this edit: a nib already carrying the path
// becomes valid the moment the vocabulary declares it.
func PlanCreate(current []byte, exists bool, path, description, color string) ([]byte, error) {
	if err := ValidateNewPath(path); err != nil {
		return nil, err
	}
	parent, name := SplitPath(path)
	return planEdit(current, exists, synthesizeMissingVocabulary, func(areas *yaml.Node) error {
		seq := areas
		if parent != "" {
			found, err := findStored(areas, parent)
			if err != nil {
				return err
			}
			if seq, err = storedChildren(found.node, parent); err != nil {
				return err
			}
		}
		seq.Content = append(seq.Content, newStoredNode(name, description, color))
		return nil
	})
}

// NodeUpdate is what one update sets on a declared area. A nil field leaves that
// key as it stands; a non-nil EMPTY string clears it, which is a different edit
// from writing an empty value — `description: ""` reads back as a description
// someone emptied the text out of.
type NodeUpdate struct {
	// NewName is a bare name: nothing is re-parented, as the rename this
	// replaced never re-parented either.
	NewName     *string
	Description *string
	Color       *string
}

// PlanUpdate edits the declared area at path in current and returns the whole
// file rendered with the edit applied. An absent file is refused.
//
// It replaced PlanRename rather than sitting beside it: a rename is the NewName
// field, so one planner covers a name, a description and a color instead of one
// planner per field, and a caller fixing two of them at once gets one write
// rather than two chances to fail half way.
//
// NOTHING BOUNDS A DESCRIPTION — ValidateNewPath, ValidateName and ValidateColor
// are the only validators this package has — so planEdit's rendered-size cap is
// the whole defense against a value that would leave an areas.yml no command can
// open afterwards. Do not assume a caller checked.
func PlanUpdate(current []byte, exists bool, path string, u NodeUpdate) ([]byte, error) {
	if u.NewName != nil {
		if err := ValidateName(*u.NewName); err != nil {
			return nil, err
		}
	}
	if u.Color != nil {
		// "" is a clear, which ValidateColor already accepts.
		//
		// Wrapped rather than returned as it stands: every refusal this package
		// makes about content has to be an *EditRefusal, or a surface reports it
		// as a filesystem failure and offers a rerun that cannot help.
		// ValidateColor itself must NOT be changed to do this — validateNodes
		// calls it while LOADING a vocabulary, where an edit refusal is the wrong
		// noun. Its message is wrapped verbatim, keeping the safetext.StripBounded
		// it already applies to the value.
		if err := ValidateColor(*u.Color); err != nil {
			return nil, refuseEdit("%v", err)
		}
	}
	return planEdit(current, exists, refuseMissingVocabulary, func(areas *yaml.Node) error {
		found, err := findStored(areas, path)
		if err != nil {
			return err
		}
		if u.NewName != nil {
			setScalarValue(found.name, *u.NewName)
		}
		setOrClearKey(found.node, "description", u.Description)
		setOrClearKey(found.node, "color", u.Color)
		return nil
	})
}

// setScalarValue rewrites a scalar in place, dropping any style the old value
// carried: it described that value, not this one.
func setScalarValue(n *yaml.Node, value string) {
	n.Kind, n.Tag, n.Value, n.Style = yaml.ScalarNode, "!!str", value, 0
}

// setOrClearKey applies one optional field to a node's mapping. An existing key
// is rewritten in place rather than removed and re-appended, so the node keeps
// the key order its author gave it.
func setOrClearKey(node *yaml.Node, key string, value *string) {
	if value == nil {
		return
	}
	if *value == "" {
		removeMappingKey(node, key)
		return
	}
	if existing := yamlfile.MappingValue(node, key); existing != nil {
		setScalarValue(existing, *value)
		return
	}
	appendMappingEntry(node, key, scalarNode(*value))
}

// PlanRemove retires the declared area at path and every area beneath it, and
// returns the whole file rendered with the edit applied. An emptied `children:`
// key goes with them; the top-level `areas:` key stays, empty. An absent file is
// refused.
func PlanRemove(current []byte, exists bool, path string) ([]byte, error) {
	return planEdit(current, exists, refuseMissingVocabulary, func(areas *yaml.Node) error {
		found, err := findStored(areas, path)
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

// storedNode is one declared node found in the vocabulary's node tree.
type storedNode struct {
	node  *yaml.Node
	name  *yaml.Node // the `name:` value node matched to find node; never nil
	seq   *yaml.Node // the sequence holding node; the `areas:` block at top level
	index int        // node's position in seq
	owner *yaml.Node // the mapping whose `children:` key seq is; nil at top level
}

// missingVocabulary says what planEdit does when the store has no areas.yml, or
// one nothing parses out of.
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
		return &yamlInheritance{construct: fmt.Sprintf("the alias `*%s`", safetext.StripBounded(node.Value)), line: node.Line}
	}
	if node.Anchor != "" {
		return &yamlInheritance{construct: fmt.Sprintf("the anchor `&%s`", safetext.StripBounded(node.Anchor)), line: node.Line}
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

// planEdit applies edit to the `areas:` sequence of data and renders the result.
// It edits a yaml.Node tree rather than marshaling a Vocabulary, which would drop
// the file's comments and any key the Vocabulary and Node structs do not model;
// the render is a re-marshal, so the file comes back with yaml.v3's layout.
func planEdit(data []byte, exists bool, missing missingVocabulary, edit func(areas *yaml.Node) error) ([]byte, error) {
	doc := emptyAreasDocument()
	switch {
	case exists:
		parsed, err := yamlfile.SoleDocument(data)
		if err != nil {
			if errors.Is(err, yamlfile.ErrMultipleDocuments) {
				return nil, refuseEditAbout(func(file string) string {
					return fmt.Sprintf("%s holds more than one YAML document, and editing its areas would rewrite the file from the first one alone — move anything after the `---` into its own file, or delete the marker if nothing follows it, then rerun", file)
				})
			}
			return nil, refuseEditAbout(func(file string) string {
				return fmt.Sprintf("parsing %s: %v", file, err)
			})
		}
		doc = parsed
	case missing == synthesizeMissingVocabulary:
		// Nothing to read; the synthesized empty document stands in.
		data = nil
	default:
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("no areas vocabulary at %s to edit; a store declares its areas there, beside its config.yml", file)
		})
	}

	// The loader resolves inheritance and this tree does not, so a key written on
	// this tree's "absent" answer would override a merged one it could not see.
	if found := inheritsContent(&doc); found != nil {
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("%s uses %s at line %d, and these edits cannot safely change a file that inherits any of its content — rewrite it without anchors, aliases or merge keys, writing out in place whatever they stand for, then rerun",
				file, found.construct, found.line)
		})
	}
	if err := yaml.Unmarshal(data, new(Vocabulary)); err != nil {
		// On the file as read, so an already-broken file is not reported as the
		// edit breaking it; the re-read below answers for the output.
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("%s cannot be read as an areas vocabulary (%v) — repair it, then rerun", file, err)
		})
	}

	bootstrap := missing == synthesizeMissingVocabulary
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		if !bootstrap {
			return nil, refuseEditAbout(func(file string) string {
				return fmt.Sprintf("%s declares no areas to edit", file)
			})
		}
		// Nothing parsed, so these bytes are all anybody wrote; yaml.v3 re-emits
		// them as a comment block.
		doc = emptyAreasDocument()
		doc.Content[0].HeadComment = strings.TrimRight(string(data), "\r\n \t")
	}
	root := doc.Content[0]
	areas := yamlfile.MappingValue(root, "areas")
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
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("%s declares no `areas:` block", file)
		})
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
	if len(out) > yamlfile.MaxBytes {
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("the edit would leave %s at %d bytes, past the %d-byte configuration limit, and a store whose areas.yml is over that limit cannot be opened by any command — declare fewer areas, or shorter names, then rerun",
				file, len(out), yamlfile.MaxBytes)
		})
	}
	// Catches a key the loader binds but this tree cannot match: `!!binary
	// "YXJlYXM="` carries no anchor or merge for the gate above, and the literal
	// `areas:` appended beside it binds the field twice, which fails here.
	var edited Vocabulary
	if err := yaml.Unmarshal(out, &edited); err != nil {
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("the edit would leave %s unreadable: %v", file, err)
		})
	}
	if err := edited.Validate(); err != nil {
		return nil, refuseEditAbout(func(file string) string {
			return fmt.Sprintf("the edit would leave %s declaring an unusable vocabulary: %v", file, err)
		})
	}
	return out, nil
}

// findStored locates the node one area path (`web/dashboard`) names, matching a
// literal `name:` key. A key the loader binds but this match cannot see — a
// `!!binary` spelling — makes the path look undeclared, so the edit is refused
// rather than aimed at the wrong node.
func findStored(areas *yaml.Node, path string) (storedNode, error) {
	seq, owner := areas, (*yaml.Node)(nil)
	rest := path
	for rest != "" {
		name, tail, nested := strings.Cut(rest, PathSeparator)
		index, nameNode := -1, (*yaml.Node)(nil)
		for i, item := range seq.Content {
			if n := yamlfile.MappingValue(item, "name"); n != nil && n.Value == name {
				index, nameNode = i, n
				break
			}
		}
		if index < 0 {
			return storedNode{}, missingStored(path)
		}
		node := seq.Content[index]
		if !nested {
			return storedNode{node: node, name: nameNode, seq: seq, index: index, owner: owner}, nil
		}
		children := yamlfile.MappingValue(node, "children")
		if children == nil || children.Kind != yaml.SequenceNode {
			return storedNode{}, missingStored(path)
		}
		seq, owner, rest = children, node, tail
	}
	return storedNode{}, missingStored(path)
}

func missingStored(path string) error {
	return refuseEdit("%s declares no area %q", storedAreasNoun, RenderPath(path))
}

// appendMappingEntry appends unconditionally: a key already present is
// duplicated, which planEdit's re-read turns into a refusal.
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

func refuseNameWhitespace(name string) error {
	return refuseEdit(
		"the area name %q has leading or trailing whitespace; an `area:` value would have to carry the same spaces to match it",
		RenderPath(name))
}

// ValidateNewPath refuses a path that could never name an area, without reading
// the file.
func ValidateNewPath(path string) error {
	if path == "" {
		return refuseEdit("an area is declared at a path, and none was given")
	}
	for _, segment := range strings.Split(path, PathSeparator) {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			return refuseEdit("the area path %q has a segment with no name in it; every declared area needs one",
				RenderPath(path))
		}
		if trimmed != segment {
			return refuseNameWhitespace(segment)
		}
		// Only the length clause can fire; the two above answer for an empty or
		// padded segment, in wording that names the whole path.
		if err := ValidateName(segment); err != nil {
			return err
		}
	}
	return nil
}

// maxNameRunes bounds the length of a name an EDIT may write, not what a store
// may HOLD. It is the bound RenderPath applies.
const maxNameRunes = safetext.MaxBoundedRunes

// ValidateName refuses a name an edit must not write, before the file is read.
// The length clause is what it adds over the load-time rule, which bounds no
// name.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return refuseEdit("an area is declared under a name, and none was given")
	}
	if trimmed != name {
		return refuseNameWhitespace(name)
	}
	// The name is not echoed: quoting it says nothing the count does not.
	if n := utf8.RuneCountInString(name); n > maxNameRunes {
		return refuseEdit("the area name is %d characters long, and a declared name is bounded at %d — a longer one only writes an areas.yml no command could read back",
			n, maxNameRunes)
	}
	return nil
}

// storedChildren returns the sequence a node's `children:` key holds, adding an
// empty one when it has none — the shape PlanRemove drops when the last child
// goes.
func storedChildren(node *yaml.Node, path string) (*yaml.Node, error) {
	switch children := yamlfile.MappingValue(node, "children"); {
	case children == nil:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		appendMappingEntry(node, "children", seq)
		return seq, nil
	case children.Kind == yaml.SequenceNode:
		return children, nil
	default:
		return nil, refuseEdit(
			"the declared area %q has a `children:` key that is not a sequence, so there is nothing to declare a child in",
			RenderPath(path))
	}
}

// newStoredNode builds the mapping one declared area is written as, in Node's
// key order. A field given nothing is left out; an explicit `description: ""`
// reads as one somebody deleted.
func newStoredNode(name, description, color string) *yaml.Node {
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
