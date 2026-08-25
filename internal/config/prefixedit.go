package config

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/alphaleonis/nibs/internal/store"
	"gopkg.in/yaml.v3"
)

// PrefixEditRefusal is a refusal about the config file's CONTENT — a file this
// edit cannot address, or one it could only rewrite by deleting part of it — as
// opposed to a failure to read or write the file.
//
// It is the prefix editor's half of the split AreaEditRefusal draws for the
// areas editor, and it exists for the same reason: content is the caller's to
// fix, where a filesystem error is the machine's, and nothing about a refusal is
// repaired by rerunning.
type PrefixEditRefusal struct{ msg string }

func (e *PrefixEditRefusal) Error() string { return e.msg }

func refusePrefixEdit(format string, a ...any) error {
	return &PrefixEditRefusal{msg: fmt.Sprintf(format, a...)}
}

// StoredPrefixEdit is one edit to a store's `nibs.prefix` key, resolved against
// the file and rendered to bytes, but NOT yet written.
//
// Planning and writing are separate steps because the caller — `nibs config
// set-prefix` — renames every nib file in the store BETWEEN them.
//
// That order is the whole point. A rename is durable the moment it lands, so a
// config edit that can only fail after them leaves every file carrying an id
// prefix the config does not declare: `nibs new` then mints ids under the old
// prefix that no file uses, and the printed remedy is a hand edit. Planning
// first moves every refusal this editor can make to before the first file is
// touched, which leaves the store completely untouched instead. What can still
// fail at Write is the filesystem, and that is the one failure a rerun repairs.
//
// A plan is only as current as the file it was read from: the caller owes it the
// store's cross-process write lock (nibcore.AcquireStoreLock) across both steps,
// or a concurrent editor's write is lost when this one lands.
type StoredPrefixEdit struct {
	path string
	out  []byte
}

// Path is the config file this edit will write.
func (e *StoredPrefixEdit) Path() string { return e.path }

// Write applies the planned edit, keeping the file's permission bits and
// reporting a symlink it replaced, the way every other config writer does.
func (e *StoredPrefixEdit) Write() (staleLinkTarget string, err error) {
	return writeConfigPreservingMode(e.path, e.out)
}

// PlanSetStoredPrefix resolves a change of the `nibs.prefix` key in the config
// inside storeDir and renders the resulting file, without writing anything. An
// existing key keeps its position; a config with no `nibs:` mapping — one that
// omits the key, one that writes `nibs:` with no value under it, or no config
// file at all — gains one, since set-prefix's whole job is to make the file say
// the new prefix.
//
// The edit goes through a yaml.Node tree rather than Config.Save because
// Config.Save marshals a whole Config, and the Config a command holds is the
// MERGED read model: LoadStoreWithUserConfig layers the user's config and then
// the system defaults onto the project's own values in place. Saving that back
// writes advisory settings into a project's committed config and drops every key
// this build does not model — including keys a NEWER nibs wrote, which is data
// loss rather than formatting. Save keeps its meaning for the caller that wants
// it: `nibs init` writes a brand new config from the merged defaults on purpose,
// and there is no prior file to preserve.
//
// It is a semantic-preserving RE-MARSHAL, not a byte-preserving splice:
// yaml.Marshal re-emits the whole document from the node tree, so the file comes
// back with yaml.v3's layout rather than the project's. Measured on a round trip
// (TestStoredPrefixEditRoundTripPreservesWhatItClaims), what survives is every
// comment ATTACHED TO A NODE — head, inline and footer — key order, nesting,
// anchors and aliases, keys this build does not model at any depth, and the
// file's permission bits. What does NOT survive is layout: indentation is
// normalized to four spaces, blank lines are dropped, a leading `---` goes, CRLF
// line endings come back as LF, a leading BOM is stripped, an inline comment
// loses its column alignment, a folded scalar is re-flowed, a `<<:` merge key
// gains an explicit `!!merge` tag, and the rewritten key loses whatever quoting
// style the old prefix carried.
//
// "Attached to a node" is the qualifier that keeps the comment clause true. A
// config that is NOTHING but comments parses to no document at all, so its
// comments hang off nothing and the file comes back holding only the new
// `nibs.prefix`. That is documented rather than refused because no CLI route
// reaches it: every shape this paragraph and the one above describe — a comment-
// only file, a `nibs:` with no value, a missing file — declares no prefix, and
// `nibs config set-prefix` refuses an empty OLD prefix in reprefix.BuildPlan
// before it ever plans a config edit (TestSetPrefixRefusesAStoreThatDeclaresNoPrefix).
// They are here for this package's exported API, where the promise above is the
// contract.
//
// A file holding more than one YAML document is REFUSED rather than edited: the
// re-marshal emits only the first, so writing it back would silently delete the
// rest. nibs never writes such a file, so refusing costs nothing a project did
// not do on purpose, and the alternative is data loss on an exit-0 success. A
// trailing bare `---` counts — yaml.v3 reads it as a second, null document — so
// the remedy has to name deleting the marker as well as moving what follows it.
//
// The rendered file is re-read as a Config and its prefix compared against the
// one asked for, because there is a shape where every step above succeeds and
// the key is still not set: a `nibs:` section written as a YAML alias
// (`nibs: *base`) decodes to an alias node, whose Content the marshaller does
// not emit, so the new key goes into a node that renders as `*base` and the file
// comes back unchanged. Without that comparison the command exits 0 reporting a
// prefix the file does not carry, after renaming every nib file in the store.
func PlanSetStoredPrefix(storeDir, prefix string) (*StoredPrefixEdit, error) {
	path := store.NewLayout(storeDir).ConfigPath()
	data, err := ReadConfigFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	doc, err := soleConfigDocument(data)
	if err != nil {
		if errors.Is(err, errMultipleConfigDocuments) {
			return nil, refusePrefixEdit(
				"%s holds more than one YAML document, and rewriting its prefix would rewrite the file from the first one alone — move anything after the `---` into its own file, or delete the marker if nothing follows it, then rerun",
				path)
		}
		return nil, refusePrefixEdit("parsing %s: %v", path, err)
	}
	setNestedScalar(&doc, "nibs", "prefix", prefix)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	var edited Config
	if err := yaml.Unmarshal(out, &edited); err != nil {
		return nil, refusePrefixEdit("the edit would leave %s unreadable: %v", path, err)
	}
	if edited.Nibs.Prefix != prefix {
		return nil, refusePrefixEdit(
			"%s would still read its prefix as %q after the edit, because this edit can only address a literal `nibs:` mapping — write `prefix: %s` out under `nibs:`, then rerun",
			path, edited.Nibs.Prefix, prefix)
	}
	return &StoredPrefixEdit{path: path, out: out}, nil
}

// SetStoredPrefix plans and writes a prefix change in one step, for a caller
// with nothing to do between the two. `nibs config set-prefix` is not that
// caller: it renames every nib file in between, which is what the two steps are
// separated for.
func SetStoredPrefix(storeDir, prefix string) (staleLinkTarget string, err error) {
	edit, err := PlanSetStoredPrefix(storeDir, prefix)
	if err != nil {
		return "", err
	}
	return edit.Write()
}

// setNestedScalar sets doc's section.key to value, creating the document, the
// section or the key when any of them is absent, and converting a section
// written with no value at all into the mapping it has to be. An existing key
// keeps its position in the file, which is what keeps the rewrite off every
// other line.
func setNestedScalar(doc *yaml.Node, section, key, value string) {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := doc.Content[0]
	nullToEmptyMapping(root)
	sectionNode := mappingValueNode(root, section)
	if sectionNode == nil {
		sectionNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: section},
			sectionNode)
	} else {
		nullToEmptyMapping(sectionNode)
	}
	if existing := mappingValueNode(sectionNode, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		// Drop any style the old scalar carried (a quoted prefix, a folded
		// block): the value is new, so the old rendering does not describe it.
		existing.Style = 0
		return
	}
	sectionNode.Content = append(sectionNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// nullToEmptyMapping turns a key written with no value — `nibs:` on a line of
// its own, or `nibs: null` — into the empty mapping the caller can append to.
// The node itself is kept rather than replaced, so the comments hanging off it
// come back.
//
// Only an EXPLICIT null qualifies. An alias (`nibs: *base`) also has no mapping
// of its own to append to, and converting one would silently drop the alias for
// every other reader of the file; it stays refused by the round-trip re-read
// instead. A `nibs:` holding a scalar or a sequence is refused by that same
// re-read, since converting it would delete whatever the project meant by it.
func nullToEmptyMapping(node *yaml.Node) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!null" {
		return
	}
	node.Kind = yaml.MappingNode
	node.Tag = "!!map"
	node.Value = ""
	node.Style = 0
}
