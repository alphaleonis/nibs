package config

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

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
type AreaEditRefusal struct{ msg string }

func (e *AreaEditRefusal) Error() string { return e.msg }

func refuseAreaEdit(format string, a ...any) error {
	return &AreaEditRefusal{msg: fmt.Sprintf(format, a...)}
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
// message for it — but it is CHECKED here too, below, because the file this
// writes has to be one the loader can read.
func PlanRenameStoredArea(storeDir, path, newName string) (*StoredAreaEdit, error) {
	return planStoredAreaEdit(storeDir, func(areas *yaml.Node) error {
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
	return planStoredAreaEdit(storeDir, func(areas *yaml.Node) error {
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
// every comment — head, inline and footer — key order, nesting, anchors and
// aliases, keys this build does not model at any depth, every per-node field,
// and the file's permission bits. What does NOT survive is layout: indentation
// is normalized to four spaces, blank lines are dropped, a leading `---` goes,
// an inline comment loses its column alignment, a folded scalar is re-flowed,
// and a `<<:` merge key gains an explicit `!!merge` tag.
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
func planStoredAreaEdit(storeDir string, edit func(areas *yaml.Node) error) (*StoredAreaEdit, error) {
	path := store.NewLayout(storeDir).AreasPath()
	data, err := ReadConfigFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, refuseAreaEdit("no areas vocabulary at %s to edit; a store declares its areas there, beside its config.yml", path)
		}
		return nil, err
	}

	doc, err := soleConfigDocument(data)
	if err != nil {
		if errors.Is(err, errMultipleConfigDocuments) {
			return nil, refuseAreaEdit(
				"%s holds more than one YAML document, and editing its areas would rewrite the file from the first one alone — move anything after the `---` into its own file, or delete the marker if nothing follows it, then rerun",
				path)
		}
		return nil, refuseAreaEdit("parsing %s: %v", path, err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, refuseAreaEdit("%s declares no areas to edit", path)
	}
	areas := mappingValueNode(doc.Content[0], "areas")
	if areas != nil && areas.Kind == yaml.AliasNode {
		return nil, refuseAreaEdit(
			"%s reaches its `areas:` block through a YAML alias, and this edit can only address a literal `areas:` sequence — write the block out under `areas:`, then rerun",
			path)
	}
	if areas == nil || areas.Kind != yaml.SequenceNode {
		return nil, refuseAreaEdit("%s declares no `areas:` block", path)
	}
	if err := edit(areas); err != nil {
		return nil, err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	var edited Areas
	if err := yaml.Unmarshal(out, &edited); err != nil {
		return nil, refuseAreaEdit("the edit would leave %s unreadable: %v", path, err)
	}
	if err := edited.Validate(); err != nil {
		return nil, refuseAreaEdit("the edit would leave %s declaring an unusable vocabulary: %v", path, err)
	}
	return &StoredAreaEdit{path: path, out: out}, nil
}

// findStoredArea locates the node one area path names, descending the sequence
// one segment at a time so `web/dashboard` finds the child of `web` and never a
// top-level node that happens to be named that way — the same resolution
// findArea makes over the loaded model.
//
// It matches a LITERAL `name:` key, where the loaded model matches a resolved
// tree, and the two diverge on exactly one class of file: a node reached through
// a YAML alias (`- *dashboard`) or given its name by a merge key (`- <<: *d`) is
// declared as far as cfg.IsValidArea is concerned and invisible here. That
// divergence is reported as its own refusal rather than as "no such area",
// because the caller has already been told the area exists and the remedy is to
// spell the node out, not to pick a different path.
func findStoredArea(areas *yaml.Node, path string) (storedArea, error) {
	seq, owner := areas, (*yaml.Node)(nil)
	rest := path
	for rest != "" {
		name, tail, nested := strings.Cut(rest, AreaPathSeparator)
		index, hidden := -1, ""
		for i, item := range seq.Content {
			n := mappingValueNode(item, "name")
			if n == nil {
				if shape := unaddressableShape(item); shape != "" {
					hidden = shape
				}
				continue
			}
			if n.Value == name {
				index = i
				break
			}
		}
		if index < 0 {
			return storedArea{}, missingStoredArea(path, hidden)
		}
		node := seq.Content[index]
		if !nested {
			return storedArea{node: node, seq: seq, index: index, owner: owner}, nil
		}
		children := mappingValueNode(node, "children")
		if children != nil && children.Kind == yaml.AliasNode {
			return storedArea{}, missingStoredArea(path, "a YAML alias")
		}
		if children == nil || children.Kind != yaml.SequenceNode {
			return storedArea{}, missingStoredArea(path, "")
		}
		seq, owner, rest = children, node, tail
	}
	return storedArea{}, missingStoredArea(path, "")
}

// unaddressableShape names the YAML construct that hides a declared node's name
// from a literal search, or "" for an entry that simply is not one.
func unaddressableShape(item *yaml.Node) string {
	if item == nil {
		return ""
	}
	if item.Kind == yaml.AliasNode {
		return "a YAML alias"
	}
	if item.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(item.Content); i += 2 {
			if item.Content[i].Value == "<<" {
				return "a YAML merge key"
			}
		}
	}
	return ""
}

// missingStoredArea reports a path the node tree could not resolve, naming the
// shape that hid it when one did.
func missingStoredArea(path, shape string) error {
	if shape == "" {
		return refuseAreaEdit("this store's areas.yml declares no area %q", RenderAreaPath(path))
	}
	return refuseAreaEdit(
		"this store's areas.yml reaches an area beside %q through %s, so this edit cannot address it — give the node its own `name:` key in the `areas:` block, then rerun",
		RenderAreaPath(path), shape)
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
