package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/yamlfile"
	"gopkg.in/yaml.v3"
)

// PrefixEditRefusal is a refusal about the config file's content, not a failure
// to read or write it. Report as a validation error; retrying will not help.
type PrefixEditRefusal struct{ msg string }

func (e *PrefixEditRefusal) Error() string { return e.msg }

func refusePrefixEdit(format string, a ...any) error {
	return &PrefixEditRefusal{msg: fmt.Sprintf(format, a...)}
}

// StoredPrefixEdit is one edit to a store's `nibs.prefix` key, resolved against
// the file and rendered to bytes, but NOT yet written.
//
// Keep planning and writing separate. `nibs config set-prefix` renames every nib
// file in the store between them, and a rename is durable the moment it lands,
// so every refusal this editor can make comes before the first file moves. Only
// the filesystem can fail at Write, and a rerun repairs that.
//
// Hold the store's write lock (nibcore.AcquireStoreLock) across both steps; a
// plan is only as current as the file it was read from.
type StoredPrefixEdit struct {
	path string
	out  []byte
}

func (e *StoredPrefixEdit) Path() string { return e.path }

// Write applies the planned edit, keeping the file's permission bits. A symlink
// at the path is replaced by a regular file, reported as staleLinkTarget.
func (e *StoredPrefixEdit) Write() (staleLinkTarget string, err error) {
	return yamlfile.WritePreservingMode(e.path, e.out)
}

// PlanSetStoredPrefix resolves a change of the `nibs.prefix` key in the config
// inside storeDir and renders the resulting file, writing nothing. An existing
// key keeps its position. A config that omits the `nibs:` mapping, writes
// `nibs:` with no value, or does not exist gains one. A file that is nothing but
// comments parses to no document at all, so it comes back holding the new key
// and none of its comments.
//
// Do not marshal a Config back over the file instead. The Config a command holds
// is the MERGED read model — the user's config and the system defaults layered
// onto the project's own values — so Config.Save would write those defaults into
// a committed config and drop every key this build does not model, including
// keys a newer nibs wrote.
//
// The edit is a semantic-preserving RE-MARSHAL, not a byte-preserving splice.
// Content survives: key order, nesting, anchors and aliases, keys this build
// does not model at any depth, and comments — though an inline comment on a
// `nibs:` written as an explicit null moves to the next key, or is lost when
// there is none. Layout does not: indentation becomes four spaces, blank lines,
// a leading `---` and a BOM go, CRLF becomes LF, an inline comment loses its
// alignment, a folded scalar is re-flowed, a `<<:` merge key gains an explicit
// `!!merge` tag, and the rewritten key loses its quoting style.
func PlanSetStoredPrefix(storeDir, prefix string) (*StoredPrefixEdit, error) {
	path := store.NewLayout(storeDir).ConfigPath()
	data, err := yamlfile.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	doc, err := yamlfile.SoleDocument(data)
	if err != nil {
		if errors.Is(err, yamlfile.ErrMultipleDocuments) {
			return nil, refusePrefixEdit(
				"%s holds more than one YAML document, and rewriting its prefix would rewrite the file from the first one alone — move anything after the `---` into its own file, or delete the marker if nothing follows it, then rerun",
				path)
		}
		return nil, refusePrefixEdit("parsing %s: %v", path, err)
	}
	// A section setPath cannot reach is left as it was, and the re-read below
	// refuses it.
	setPath(&doc, []string{"nibs", "prefix"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: prefix})

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	// The re-read is what catches a `nibs:` written as an alias: it decodes to an
	// alias node whose Content the marshaller does not emit, so the new key lands
	// in a node that renders as `*base` and the marshal above still succeeds.
	var edited Config
	if err := yaml.Unmarshal(out, &edited); err != nil {
		return nil, refusePrefixEdit("the edit would leave %s unreadable: %v", path, err)
	}
	if edited.Nibs.Prefix != prefix {
		return nil, refusePrefixEdit(
			"%s would still read its prefix as %q after the edit, because this edit can only address a literal `nibs:` mapping — write `prefix: %s` out under `nibs:`, then rerun",
			path, safetext.StripBounded(edited.Nibs.Prefix), prefix)
	}
	return &StoredPrefixEdit{path: path, out: out}, nil
}

// setPath sets the value at path in doc to value. It creates the document and
// each missing mapping on the way, and turns a key written with no value into a
// mapping. A value it replaces goes through replaceValue.
//
// It returns the first node on the way that is not a mapping or a key with no
// value, and the path to it. doc is then partly edited and must not be written.
func setPath(doc *yaml.Node, path []string, value *yaml.Node) (blockedAt []string, blocked *yaml.Node) {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	parent := doc.Content[0]
	for i, key := range path {
		nullToEmptyMapping(parent)
		if parent.Kind != yaml.MappingNode {
			return path[:i], parent
		}
		last := i == len(path)-1
		keyNode, child := mappingEntry(parent, key)
		switch {
		case child == nil:
			child = value
			if !last {
				child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			parent.Content = append(parent.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
				child)
		case last:
			replaceValue(keyNode, child, value)
		}
		parent = child
	}
	return nil, nil
}

// mappingEntry returns the key and value nodes for key in mapping, or two nils.
func mappingEntry(mapping *yaml.Node, key string) (keyNode, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

// replaceValue makes old, the value of keyNode, a copy of value. It keeps the
// anchor of old, so an alias of old reads the new value, and the line comment.
// The comments inside old are lost with its content.
func replaceValue(keyNode, old, value *yaml.Node) {
	anchor, line := old.Anchor, old.LineComment
	*old = *value
	old.Anchor = anchor
	// The key carries the line comment, because yaml.v3 writes the line comment
	// of a block collection after its last item, on the next key's line.
	keyNode.LineComment = strings.TrimSpace(keyNode.LineComment + " " + line)
}

// nullToEmptyMapping turns a key written with no value — `nibs:` alone on its
// line, `nibs: null` or `nibs: ~` — into an empty mapping the caller can append
// to. An alias, a scalar or a sequence is left as it is, because converting one
// would delete what the file holds.
func nullToEmptyMapping(node *yaml.Node) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!null" {
		return
	}
	node.Kind = yaml.MappingNode
	node.Tag = "!!map"
	node.Value = ""
	node.Style = 0
}
