package config

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
)

// AreaPathSeparator joins the segments of an area path (`web/dashboard`). It is
// a LOGICAL path and never a filesystem one, so this is the separator on every
// platform — filepath's would make the same vocabulary read differently on
// Windows, and a nib's `area:` value travels between machines in a file.
const AreaPathSeparator = "/"

// AreaConfig is one node of the areas vocabulary declared in a store's
// config.yml. Areas are the one genuinely per-project vocabulary in that file —
// statuses, types, priorities and estimates are hardcoded — so a node carries
// its own description: it is what tells an agent which area new work belongs in.
//
// Children nest, and a node's PATH is its name joined to its ancestors' with
// AreaPathSeparator, which is why a name may not contain that separator: the
// tree is the only thing that says where a node sits, and a name carrying a
// separator would describe a second, contradictory structure.
//
// Color and Order are stored for the surfaces that display areas; nothing in
// this package renders or sorts by them. Order is a sibling sort key of the
// same shape as a nib's, while AreaPaths enumerates in DECLARATION order — the
// order the file itself reads in.
type AreaConfig struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description,omitempty"`
	Color       string       `yaml:"color,omitempty"`
	Order       string       `yaml:"order,omitempty"`
	Children    []AreaConfig `yaml:"children,omitempty"`
}

// ValidateAreas checks the declared vocabulary and returns the first fault it
// finds, naming the offending path.
//
// It runs on every load because the vocabulary is AUTHORIZATION data: what may
// be assigned to a nib, what a filter may close downward over, what a rename
// may cascade through. A node dropped silently — because its name collides with
// a sibling, or is empty, or splits into two segments — would not merely display
// oddly, it would make a path resolve to something other than what the file
// declares, and every one of those decisions would follow the wrong tree.
//
// An absent or empty vocabulary is valid: a project that has not declared areas
// is a normal project, not a broken one.
func (c *Config) ValidateAreas() error {
	return validateAreaNodes(c.Areas, "")
}

// validateAreaNodes validates one level of the tree and recurses. parent is the
// path of the node these areas hang under, empty at the top level.
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
		path := joinAreaPath(parent, name)
		if strings.Contains(name, AreaPathSeparator) {
			return fmt.Errorf("area %q %s has a %q in its name; nest the child under its parent instead, which is what makes the path",
				name, areaLocation(parent), AreaPathSeparator)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate area %q; two siblings with one name make one path mean two nodes", path)
		}
		seen[name] = struct{}{}
		if err := validateAreaColor(area.Color); err != nil {
			return fmt.Errorf("area %q: %w", path, err)
		}
		if err := validateAreaNodes(area.Children, path); err != nil {
			return err
		}
	}
	return nil
}

// areaLocation names where a faulty node sits, for a message about a node whose
// own name cannot be quoted usefully.
func areaLocation(parent string) string {
	if parent == "" {
		return "at the top level"
	}
	return fmt.Sprintf("under %q", parent)
}

// validateAreaColor checks a color's SHAPE — `#` plus 3, 4, 6 or 8 hex digits,
// or a bare name of letters. The set of known color NAMES is not checked here:
// it lives in internal/ui, which imports this package, and an unknown name
// resolves to a muted fallback rather than failing. A malformed hex code has no
// such fallback, and neither shape is something a user meant to write.
func validateAreaColor(color string) error {
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

// joinAreaPath appends name to parent, which is empty at the top level.
func joinAreaPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + AreaPathSeparator + name
}

// AreaPaths returns every declared area path in DECLARATION order, a parent
// immediately before the subtree it heads. The order is the file's own, which
// is what lets a project read its vocabulary back the way it wrote it.
func (c *Config) AreaPaths() []string {
	var paths []string
	appendAreaPaths(&paths, c.Areas, "")
	return paths
}

func appendAreaPaths(paths *[]string, areas []AreaConfig, parent string) {
	for _, area := range areas {
		path := joinAreaPath(parent, area.Name)
		*paths = append(*paths, path)
		appendAreaPaths(paths, area.Children, path)
	}
}

// maxListedAreas bounds how many paths AreaList enumerates, and
// maxListedAreaRunes bounds how much of one path it repeats.
//
// Areas are the one vocabulary in this package a PROJECT authors — statuses,
// types, priorities and estimates enumerate hardcoded sets, so their safety is
// structural and does not transfer. A declared name is file-sourced text up to
// MaxConfigBytes long, so a message repeating the whole vocabulary hands a
// hostile config a canvas. These are the same two bounds cmd/filetext.go
// applies to every other echoed file value, in the same shape and with the same
// numbers; they are restated here because cmd imports this package and not the
// other way round.
const (
	maxListedAreas     = 20
	maxListedAreaRunes = 200
)

// AreaList RENDERS every declared area path as a comma-separated list, for the
// messages that have to say what the vocabulary holds. Each path goes through
// safetext.Strip, and both the number of paths listed and the length of one are
// bounded, with the elision stated.
//
// It is therefore display text and not data: use AreaPaths where the values
// themselves are wanted.
func (c *Config) AreaList() string {
	paths := c.AreaPaths()
	shown := paths
	if len(shown) > maxListedAreas {
		shown = shown[:maxListedAreas]
	}
	rendered := make([]string, 0, len(shown)+1)
	for _, path := range shown {
		rendered = append(rendered, truncateListedArea(safetext.Strip(path)))
	}
	if len(paths) > len(shown) {
		rendered = append(rendered, fmt.Sprintf("…and %d more", len(paths)-len(shown)))
	}
	return strings.Join(rendered, ", ")
}

// truncateListedArea bounds one echoed path, marking the truncation so a
// shortened rendering is distinguishable from a complete one.
func truncateListedArea(path string) string {
	if utf8.RuneCountInString(path) <= maxListedAreaRunes {
		return path
	}
	return string([]rune(path)[:maxListedAreaRunes]) + "…"
}

// GetArea returns the declared node at path, or nil when the vocabulary does
// not declare it. The path is resolved by descending the tree one segment at a
// time, so `web/dashboard` finds the child of `web` and never a top-level node
// that happens to be named that way.
func (c *Config) GetArea(path string) *AreaConfig {
	return findArea(c.Areas, path)
}

// IsValidArea reports whether path names a declared area. The empty string is
// NOT valid here: this answers a membership question, and callers that treat an
// unset `area:` as legal check for that themselves.
func (c *Config) IsValidArea(path string) bool {
	return c.GetArea(path) != nil
}

// IsAreaWithin reports whether path is ancestor or sits below it — the
// downward-closed primitive an area filter needs.
//
// The answer is over the DECLARED TREE, not over the strings: `webhooks` is not
// within `web` even though one is a prefix of the other, because they are two
// roots. Both ends must be declared; an undeclared path is not within anything,
// so a value no longer in the vocabulary cannot be swept in by a filter that
// happens to name one of its former ancestors.
func (c *Config) IsAreaWithin(path, ancestor string) bool {
	if path == "" || ancestor == "" {
		return false
	}
	node := c.GetArea(ancestor)
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

// findArea descends areas segment by segment, returning the node path names or
// nil. An empty path, or one with an empty segment, matches nothing: a declared
// name is never empty.
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
