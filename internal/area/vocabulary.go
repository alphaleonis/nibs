// Package area is a store's declared areas vocabulary: its nodes, validation,
// queries, and the planner that edits its file's bytes.
//
// It does no file I/O. The vocabulary reloads while a process runs and its edits
// run inside the store's write lock, so the reads and writes belong to
// internal/nibcore, which holds that lock and owns the store layout; this package
// takes bytes and hands bytes back. It imports internal/yamlfile only for its
// decoding helpers and size cap; call no file I/O here, yamlfile's included, and
// no store-layout package, for that reason.
package area

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/alphaleonis/nibs/internal/safetext"
	"gopkg.in/yaml.v3"
)

// PathSeparator joins area path segments (`web/dashboard`). It is not a
// filesystem separator — never substitute the operating system's one.
const PathSeparator = "/"

// Node is one node of the areas vocabulary declared in a store's areas.yml.
type Node struct {
	// May not contain PathSeparator.
	Name string `yaml:"name"`

	Description string `yaml:"description,omitempty"`

	// A `#` hex code of 3, 4, 6 or 8 digits, or a bare color name. Names are not
	// checked against any known set.
	Color string `yaml:"color,omitempty"`

	Children []Node `yaml:"children,omitempty"`
}

// Vocabulary is a store's declared area vocabulary, as <store>/areas.yml
// declares it.
//
// A nil *Vocabulary answers as the empty vocabulary, so call any method on it
// without a nil check.
//
// Treat a parsed value as immutable. A reload builds a new one and swaps the
// pointer; see nibcore.Core.ValidateArea for what reads one off-lock.
type Vocabulary struct {
	// The declared forest, in the file's declaration order.
	Nodes []Node `yaml:"areas,omitempty"`
}

// ParseError is areas.yml content that cannot be honored as a vocabulary.
type ParseError struct {
	// The file the content was read from, set by the caller that read it. Error
	// names it when set.
	File string

	// Malformed separates content that decodes but declares an unusable vocabulary
	// from content that does not decode at all.
	Malformed bool

	Err error
}

func (e *ParseError) Error() string {
	file := e.File
	if file == "" {
		file = storedAreasNoun
	}
	if e.Malformed {
		return fmt.Sprintf("%s declares a malformed area vocabulary: %v", file, e.Err)
	}
	return fmt.Sprintf("%s is not readable as an areas vocabulary: %v", file, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// Parse decodes and validates an areas.yml. Empty input is the empty vocabulary.
//
// Content that cannot be honored is refused with a *ParseError. Never fall back
// to an empty vocabulary over one: that undeclares every `area:` the store
// already carries, and every write to those nibs is then refused.
func Parse(data []byte) (*Vocabulary, error) {
	var vocab Vocabulary
	if err := yaml.Unmarshal(data, &vocab); err != nil {
		return nil, &ParseError{Err: err}
	}
	if err := vocab.Validate(); err != nil {
		return nil, &ParseError{Malformed: true, Err: err}
	}
	return &vocab, nil
}

// Validate returns the first fault in the declared vocabulary. An absent or
// empty vocabulary is valid.
func (a *Vocabulary) Validate() error {
	// File order, so the "area #N" a fault names counts entries as the file does.
	return validateNodes(a.fileNodes(), "")
}

// Roots returns the declared forest's top-level nodes, each carrying its own
// children, with every set of siblings sorted by name. Where a node sits in
// areas.yml carries no meaning; alphabetical is what keeps a name findable as the
// vocabulary grows. The result is a copy. Read through this rather than the Nodes
// field — the receiver may be nil.
func (a *Vocabulary) Roots() []Node {
	return sortedNodes(a.fileNodes())
}

func (a *Vocabulary) fileNodes() []Node {
	if a == nil {
		return nil
	}
	return a.Nodes
}

func sortedNodes(nodes []Node) []Node {
	if len(nodes) == 0 {
		return nil
	}
	sorted := slices.Clone(nodes)
	for i := range sorted {
		sorted[i].Children = sortedNodes(sorted[i].Children)
	}
	slices.SortFunc(sorted, func(x, y Node) int {
		// Case-insensitive first; the byte comparison only orders names that
		// differ in case alone, so the order is total.
		return cmp.Or(
			strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)),
			strings.Compare(x.Name, y.Name),
		)
	})
	return sorted
}

// parent is the path these areas hang under, empty at the top level.
func validateNodes(nodes []Node, parent string) error {
	seen := make(map[string]struct{}, len(nodes))
	for i, node := range nodes {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			return fmt.Errorf("area #%d %s has no name; every declared area needs one", i+1, location(parent))
		}
		if name != node.Name {
			return fmt.Errorf("area %q %s has leading or trailing whitespace in its name; an `area:` value would have to carry the same spaces to match it",
				RenderPath(node.Name), location(parent))
		}
		// INTERIOR whitespace is permitted: this runs on every load, so tightening
		// it would fail a config valid today.
		path := JoinPath(parent, name)
		if strings.Contains(name, PathSeparator) {
			return fmt.Errorf("area %q %s has a %q in its name; nest the child under its parent instead, which is what makes the path",
				RenderPath(name), location(parent), PathSeparator)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate area %q; two siblings with one name make one path mean two nodes", RenderPath(path))
		}
		seen[name] = struct{}{}
		if err := ValidateColor(node.Color); err != nil {
			return fmt.Errorf("area %q: %w", RenderPath(path), err)
		}
		if err := validateNodes(node.Children, path); err != nil {
			return err
		}
	}
	return nil
}

func location(parent string) string {
	if parent == "" {
		return "at the top level"
	}
	return fmt.Sprintf("under %q", RenderPath(parent))
}

// ValidateColor checks a color against the shape Node.Color permits.
func ValidateColor(color string) error {
	if color == "" {
		return nil
	}
	if rest, ok := strings.CutPrefix(color, "#"); ok {
		switch len(rest) {
		case 3, 4, 6, 8:
		default:
			return fmt.Errorf("color %q is not a usable hex code; use #RGB, #RGBA, #RRGGBB or #RRGGBBAA", safetext.StripBounded(color))
		}
		for _, r := range rest {
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return fmt.Errorf("color %q is not a usable hex code; use #RGB, #RGBA, #RRGGBB or #RRGGBBAA", safetext.StripBounded(color))
			}
		}
		return nil
	}
	for _, r := range color {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isLetter {
			return fmt.Errorf("color %q is neither a color name nor a hex code", safetext.StripBounded(color))
		}
	}
	return nil
}

// JoinPath appends name to the parent path, which is empty at the top level.
// SplitPath is its inverse.
func JoinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + PathSeparator + name
}

// SplitPath separates a path into its parent's path (empty at the top level) and
// the node's own name.
func SplitPath(path string) (parent, name string) {
	i := strings.LastIndex(path, PathSeparator)
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+len(PathSeparator):]
}

// Paths returns every declared area path in Roots' order: siblings by name, a
// parent immediately before the subtree it heads.
func (a *Vocabulary) Paths() []string {
	var paths []string
	appendPaths(&paths, a.Roots(), "")
	return paths
}

func appendPaths(paths *[]string, nodes []Node, parent string) {
	for _, node := range nodes {
		path := JoinPath(parent, node.Name)
		*paths = append(*paths, path)
		appendPaths(paths, node.Children, path)
	}
}

// A declared name is file-sourced text bounded only by yamlfile.MaxBytes; this
// bounds how many of them a message prints, and RenderPath bounds each one.
const maxListedAreas = 20

// List renders the declared paths as one comma-separated string for a message.
// Display text, not data — bounded and elided, so use Paths for the values.
func (a *Vocabulary) List() string {
	paths := a.Paths()
	shown := paths
	if len(shown) > maxListedAreas {
		shown = shown[:maxListedAreas]
	}
	rendered := make([]string, 0, len(shown)+1)
	for _, path := range shown {
		rendered = append(rendered, RenderPath(path))
	}
	if len(paths) > len(shown) {
		rendered = append(rendered, fmt.Sprintf("…and %d more", len(paths)-len(shown)))
	}
	return strings.Join(rendered, ", ")
}

// RenderPath renders one area path for a message: control characters
// neutralized, length bounded. Use it for every path a message echoes.
func RenderPath(path string) string {
	return safetext.StripBounded(path)
}

// Get returns the declared node at path (`web/dashboard`), or nil.
func (a *Vocabulary) Get(path string) *Node {
	return find(a.fileNodes(), path)
}

func (a *Vocabulary) IsEmpty() bool {
	return len(a.fileNodes()) == 0
}

func (a *Vocabulary) Exists(path string) bool {
	return a.Get(path) != nil
}

// Error is an `area:` value the declared vocabulary refuses. Render each field
// before setting it.
type Error struct {
	Path string

	// The vocabulary as List renders it, empty when the store declares none.
	Declared string

	// The nib whose STORED value is refused, empty when the caller supplied it.
	NibID string
}

func (e *Error) Error() string {
	switch {
	case e.NibID == "" && e.Declared == "":
		return fmt.Sprintf("invalid area %q: this store declares no areas — declare an `areas:` block in the store's areas.yml before assigning one", e.Path)
	case e.NibID == "":
		return fmt.Sprintf("invalid area %q: must be one of %s", e.Path, e.Declared)
	case e.Declared == "":
		// No `--area` here: this store declares no value to put in one.
		return fmt.Sprintf("invalid area %q: this store declares no areas — if the request set no area, the nib already carries it and every write to that nib is refused until `nibs set %s --clear area` replaces it; otherwise declare an `areas:` block in the store's areas.yml before assigning one",
			e.Path, e.NibID)
	default:
		return fmt.Sprintf("invalid area %q: must be one of %s — if the request set no area, the nib already carries it and every write to that nib is refused until `nibs set %s --area <declared>` or `nibs set %s --clear area` replaces it",
			e.Path, e.Declared, e.NibID, e.NibID)
	}
}

// ValidateAssignment checks an `area:` value the caller SUPPLIED against the
// declared vocabulary. The empty string passes.
func (a *Vocabulary) ValidateAssignment(path string) error {
	if path == "" || a.Exists(path) {
		return nil
	}
	return &Error{Path: RenderPath(path), Declared: a.declaredList()}
}

// declaredList renders the vocabulary for a refusal, or "" when the store
// declares none. Error's no-areas wording keys on that empty string.
func (a *Vocabulary) declaredList() string {
	if a.IsEmpty() {
		return ""
	}
	return a.List()
}

// ValidateStored re-checks the `area:` a nib ALREADY HOLDS, which need not have
// come from the request. nibID lets the refusal name the nib to fix.
func (a *Vocabulary) ValidateStored(nibID, path string) error {
	if path == "" || a.Exists(path) {
		return nil
	}
	return &Error{Path: RenderPath(path), Declared: a.declaredList(), NibID: safetext.Strip(nibID)}
}

// IsWithin reports whether path is ancestor or sits below it, over the DECLARED
// TREE rather than the strings: `webhooks` is not within `web`. Returns false
// unless both ends are declared.
func (a *Vocabulary) IsWithin(path, ancestor string) bool {
	if path == "" || ancestor == "" {
		return false
	}
	node := a.Get(ancestor)
	if node == nil {
		return false
	}
	if path == ancestor {
		return true
	}
	below, ok := strings.CutPrefix(path, ancestor+PathSeparator)
	if !ok {
		return false
	}
	return find(node.Children, below) != nil
}

// find descends segment by segment, returning the node path names or nil. An
// empty path, or one with an empty segment, matches nothing.
func find(nodes []Node, path string) *Node {
	if path == "" {
		return nil
	}
	name, rest, nested := strings.Cut(path, PathSeparator)
	for i := range nodes {
		if nodes[i].Name != name {
			continue
		}
		if !nested {
			return &nodes[i]
		}
		return find(nodes[i].Children, rest)
	}
	return nil
}

// Equal reports whether two vocabularies declare the same forest, whatever order
// their files list siblings in.
func (a *Vocabulary) Equal(other *Vocabulary) bool {
	return slices.EqualFunc(a.Roots(), other.Roots(), equalNode)
}

// equalNode compares every Node field; extend it when one is added. Both sides
// come from Roots, so their children are already in one order.
func equalNode(x, y Node) bool {
	return x.Name == y.Name &&
		x.Description == y.Description &&
		x.Color == y.Color &&
		slices.EqualFunc(x.Children, y.Children, equalNode)
}
