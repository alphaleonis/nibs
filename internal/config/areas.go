package config

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
)

// AreaPathSeparator joins area path segments (`web/dashboard`). It is not a
// filesystem separator — never substitute filepath.Separator.
const AreaPathSeparator = "/"

// AreaConfig is one node of the areas vocabulary declared in a store's areas.yml.
type AreaConfig struct {
	// May not contain AreaPathSeparator.
	Name string `yaml:"name"`

	Description string `yaml:"description,omitempty"`

	// A `#` hex code of 3, 4, 6 or 8 digits, or a bare color name. Names are not
	// checked against any known set.
	Color string `yaml:"color,omitempty"`

	// Not a sort key: Paths enumerates in declaration order.
	Order    string       `yaml:"order,omitempty"`
	Children []AreaConfig `yaml:"children,omitempty"`
}

// Validate returns the first fault in the declared vocabulary. An absent or
// empty vocabulary is valid.
func (a *Areas) Validate() error {
	return validateAreaNodes(a.Roots(), "")
}

// Roots returns the declared forest's top-level nodes, each carrying its own
// children. Read through this rather than the Nodes field — the receiver may be
// nil.
func (a *Areas) Roots() []AreaConfig {
	if a == nil {
		return nil
	}
	return a.Nodes
}

// parent is the path these areas hang under, empty at the top level.
func validateAreaNodes(areas []AreaConfig, parent string) error {
	seen := make(map[string]struct{}, len(areas))
	for i, area := range areas {
		name := strings.TrimSpace(area.Name)
		if name == "" {
			return fmt.Errorf("area #%d %s has no name; every declared area needs one", i+1, areaLocation(parent))
		}
		if name != area.Name {
			return fmt.Errorf("area %q %s has leading or trailing whitespace in its name; an `area:` value would have to carry the same spaces to match it",
				area.Name, areaLocation(parent))
		}
		// INTERIOR whitespace is permitted: this runs on every load, so tightening
		// it would fail a config valid today.
		path := joinAreaPath(parent, name)
		if strings.Contains(name, AreaPathSeparator) {
			return fmt.Errorf("area %q %s has a %q in its name; nest the child under its parent instead, which is what makes the path",
				name, areaLocation(parent), AreaPathSeparator)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate area %q; two siblings with one name make one path mean two nodes", path)
		}
		seen[name] = struct{}{}
		if err := ValidateAreaColor(area.Color); err != nil {
			return fmt.Errorf("area %q: %w", path, err)
		}
		if err := validateAreaNodes(area.Children, path); err != nil {
			return err
		}
	}
	return nil
}

func areaLocation(parent string) string {
	if parent == "" {
		return "at the top level"
	}
	return fmt.Sprintf("under %q", parent)
}

// ValidateAreaColor checks a color against the shape AreaConfig.Color permits.
func ValidateAreaColor(color string) error {
	if color == "" {
		return nil
	}
	if rest, ok := strings.CutPrefix(color, "#"); ok {
		switch len(rest) {
		case 3, 4, 6, 8:
		default:
			return fmt.Errorf("color %q is not a usable hex code; use #RGB, #RGBA, #RRGGBB or #RRGGBBAA", color)
		}
		for _, r := range rest {
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return fmt.Errorf("color %q is not a usable hex code; use #RGB, #RGBA, #RRGGBB or #RRGGBBAA", color)
			}
		}
		return nil
	}
	for _, r := range color {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isLetter {
			return fmt.Errorf("color %q is neither a color name nor a hex code", color)
		}
	}
	return nil
}

func joinAreaPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + AreaPathSeparator + name
}

// Paths returns every declared area path in DECLARATION order, a parent
// immediately before the subtree it heads.
func (a *Areas) Paths() []string {
	var paths []string
	appendAreaPaths(&paths, a.Roots(), "")
	return paths
}

func appendAreaPaths(paths *[]string, areas []AreaConfig, parent string) {
	for _, area := range areas {
		path := joinAreaPath(parent, area.Name)
		*paths = append(*paths, path)
		appendAreaPaths(paths, area.Children, path)
	}
}

// A declared name is file-sourced text bounded only by MaxConfigBytes; these
// bound what a message prints.
const (
	maxListedAreas     = 20
	maxListedAreaRunes = 200
)

// List renders the declared paths as one comma-separated string for a message.
// Display text, not data — bounded and elided, so use Paths for the values.
func (a *Areas) List() string {
	paths := a.Paths()
	shown := paths
	if len(shown) > maxListedAreas {
		shown = shown[:maxListedAreas]
	}
	rendered := make([]string, 0, len(shown)+1)
	for _, path := range shown {
		rendered = append(rendered, RenderAreaPath(path))
	}
	if len(paths) > len(shown) {
		rendered = append(rendered, fmt.Sprintf("…and %d more", len(paths)-len(shown)))
	}
	return strings.Join(rendered, ", ")
}

// RenderAreaPath renders one area path for a message: control characters
// neutralized, length bounded. Use it for every path a message echoes.
func RenderAreaPath(path string) string {
	return truncateListedArea(safetext.Strip(path))
}

func truncateListedArea(path string) string {
	if utf8.RuneCountInString(path) <= maxListedAreaRunes {
		return path
	}
	return string([]rune(path)[:maxListedAreaRunes]) + "…"
}

// Get returns the declared node at path (`web/dashboard`), or nil.
func (a *Areas) Get(path string) *AreaConfig {
	return findArea(a.Roots(), path)
}

func (a *Areas) Declared() bool {
	return len(a.Roots()) > 0
}

// IsValid reports whether path names a declared area. The empty string does not
// — check for an unset `area:` separately.
func (a *Areas) IsValid(path string) bool {
	return a.Get(path) != nil
}

// AreaError is an `area:` value the declared vocabulary refuses. Render each
// field before setting it.
type AreaError struct {
	Path string

	// The vocabulary as List renders it, empty when the store declares none.
	Declared string

	// The nib whose STORED value is refused, empty when the caller supplied it.
	NibID string
}

func (e *AreaError) Error() string {
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
func (a *Areas) ValidateAssignment(path string) error {
	if path == "" || a.IsValid(path) {
		return nil
	}
	return &AreaError{Path: RenderAreaPath(path), Declared: a.declaredList()}
}

// declaredList renders the vocabulary for a refusal, or "" when the store
// declares none. AreaError's no-areas wording keys on that empty string.
func (a *Areas) declaredList() string {
	if !a.Declared() {
		return ""
	}
	return a.List()
}

// ValidateStored re-checks the `area:` a nib ALREADY HOLDS, which need not have
// come from the request. nibID lets the refusal name the nib to fix.
func (a *Areas) ValidateStored(nibID, path string) error {
	if path == "" || a.IsValid(path) {
		return nil
	}
	return &AreaError{Path: RenderAreaPath(path), Declared: a.declaredList(), NibID: safetext.Strip(nibID)}
}

// IsWithin reports whether path is ancestor or sits below it, over the DECLARED
// TREE rather than the strings: `webhooks` is not within `web`. Returns false
// unless both ends are declared.
func (a *Areas) IsWithin(path, ancestor string) bool {
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
	below, ok := strings.CutPrefix(path, ancestor+AreaPathSeparator)
	if !ok {
		return false
	}
	return findArea(node.Children, below) != nil
}

// findArea descends segment by segment, returning the node path names or nil. An
// empty path, or one with an empty segment, matches nothing.
func findArea(areas []AreaConfig, path string) *AreaConfig {
	if path == "" {
		return nil
	}
	name, rest, nested := strings.Cut(path, AreaPathSeparator)
	for i := range areas {
		if areas[i].Name != name {
			continue
		}
		if !nested {
			return &areas[i]
		}
		return findArea(areas[i].Children, rest)
	}
	return nil
}
