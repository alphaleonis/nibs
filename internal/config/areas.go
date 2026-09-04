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
// areas.yml. Areas are the one genuinely per-project vocabulary a store holds —
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

// Validate checks the declared vocabulary and returns the first fault it
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
func (a *Areas) Validate() error {
	return validateAreaNodes(a.Roots(), "")
}

// Roots returns the declared forest's top-level nodes, each carrying its own
// children. It is the single nil-safe accessor every reader goes through —
// inside this package and out — so a nil *Areas, which is the vocabulary a store
// with no areas.yml has, answers instead of panicking, and the guard exists once
// rather than at every call site. The Nodes field is the YAML binding; read
// through this.
func (a *Areas) Roots() []AreaConfig {
	if a == nil {
		return nil
	}
	return a.Nodes
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
		// INTERIOR whitespace is permitted, and that is a deliberate asymmetry
		// with the check above rather than an oversight. The same reasoning does
		// apply — `nibs list --area "Web UI"` works because a shell quotes it,
		// while the web query box has no quoting and tokenizes on whitespace, so
		// `area:Web UI` splits and the tail lands in free text. The box therefore
		// withholds such a path from its completions rather than offering one it
		// cannot accept.
		//
		// Refusing it here would be the tidier rule and is NOT taken: this
		// function runs on every load, so tightening it would make a config that
		// is valid today fail outright, and config.yml is a stored file this
		// project keeps readable across versions. Closing the gap properly means
		// quoting in the query grammar, which is a grammar-wide change affecting
		// every field.
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

// Paths returns every declared area path in DECLARATION order, a parent
// immediately before the subtree it heads. The order is the file's own, which
// is what lets a project read its vocabulary back the way it wrote it.
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

// List RENDERS every declared area path as a comma-separated list, for the
// messages that have to say what the vocabulary holds. Each path goes through
// safetext.Strip, and both the number of paths listed and the length of one are
// bounded, with the elision stated.
//
// It is therefore display text and not data: use Paths where the values
// themselves are wanted.
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
// neutralized and the length bounded. Every message that echoes a path goes
// through it — the declared set AreaList lists, the refused value
// ValidateAssignment quotes back, and the filter argument internal/graph
// refuses — because a path reaching one of those slots is file-sourced whenever
// it is the declared set or a value a nib already carries, and caller-supplied
// text of unbounded length otherwise. Both hazards want the same treatment, and
// one renderer is what keeps the two sides of a single message rendered alike.
func RenderAreaPath(path string) string {
	return truncateListedArea(safetext.Strip(path))
}

// truncateListedArea bounds one echoed path, marking the truncation so a
// shortened rendering is distinguishable from a complete one.
func truncateListedArea(path string) string {
	if utf8.RuneCountInString(path) <= maxListedAreaRunes {
		return path
	}
	return string([]rune(path)[:maxListedAreaRunes]) + "…"
}

// Get returns the declared node at path, or nil when the vocabulary does
// not declare it. The path is resolved by descending the tree one segment at a
// time, so `web/dashboard` finds the child of `web` and never a top-level node
// that happens to be named that way.
func (a *Areas) Get(path string) *AreaConfig {
	return findArea(a.Roots(), path)
}

// Declared reports whether the store declares an areas vocabulary at all.
//
// It is the one question asked about the AXIS rather than about a value, and it
// has two callers that must agree: `nibs check` reports no undeclared-area
// finding when nothing is declared (an exemption whose cost — the nibs it
// silences are exactly the ones no write can reach — is set out at
// nibcore.Core.CheckAllLinks), and `nibs close`'s member refusal asks it of a
// member refused for its AREA, to decide whether the report it offers to point
// at will name that member. Stated here rather than spelled
// `len(areas.Nodes) > 0` at each site, because the two would otherwise be free
// to drift and the second would then name a silent diagnostic.
//
// The WRITE paths deliberately do not consult it: ValidateStored refuses a
// stored value whether or not a vocabulary exists, because a write is asked
// about one nib the caller named, where the refusal is actionable.
func (a *Areas) Declared() bool {
	return len(a.Roots()) > 0
}

// IsValid reports whether path names a declared area. The empty string is
// NOT valid here: this answers a membership question, and callers that treat an
// unset `area:` as legal check for that themselves.
func (a *Areas) IsValid(path string) bool {
	return a.Get(path) != nil
}

// AreaError is an `area:` value the declared vocabulary refuses.
//
// It is typed, rather than a bare error, so a caller can recognize the class
// without matching on its text. Orderer.backfillKeys is the reason it has to
// be recognizable: it writes from the READ path and re-attempts on every read,
// so it must be able to tell a PERMANENTLY stable refusal — which it stays
// quiet about, having no way to clear it — from a transient write failure,
// which it warns about and should.
//
// The three fields also select which of four refusals this is, because the
// message genuinely differs along both axes:
//
//   - NibID names the nib whose STORED value is refused, and is empty when the
//     caller supplied the value. A supplied value is a complaint about an
//     argument; a stored one is a standing refusal of every write to that nib,
//     which the caller may never have written and cannot act on without being
//     told which nib and how to get out.
//   - Declared is the vocabulary as AreaList renders it, empty when the store
//     declares none. "must be one of " followed by nothing reads as a bug in
//     nibs — the reader looks for the list that failed to print — where the
//     real answer is that the project has not declared a vocabulary yet, which
//     is a config edit and not a different value.
//
// Path and Declared are rendered before they get here: both are file-sourced
// (the declared set by definition, the refused value whenever the caller is a
// write path re-checking what a nib already carries on disk), and so is NibID,
// which comes from a filename.
type AreaError struct {
	Path     string
	Declared string
	NibID    string
}

func (e *AreaError) Error() string {
	switch {
	case e.NibID == "" && e.Declared == "":
		return fmt.Sprintf("invalid area %q: this store declares no areas — declare an `areas:` block in the store's areas.yml before assigning one", e.Path)
	case e.NibID == "":
		return fmt.Sprintf("invalid area %q: must be one of %s", e.Path, e.Declared)
	case e.Declared == "":
		// Only the clear is named. This branch diagnoses a store with no
		// declared value to put in an `--area`, so prescribing one would name a
		// command with no satisfiable argument in the very state it reports.
		return fmt.Sprintf("invalid area %q: this store declares no areas — if the request set no area, the nib already carries it and every write to that nib is refused until `nibs set %s --clear area` replaces it; otherwise declare an `areas:` block in the store's areas.yml before assigning one",
			e.Path, e.NibID)
	default:
		return fmt.Sprintf("invalid area %q: must be one of %s — if the request set no area, the nib already carries it and every write to that nib is refused until `nibs set %s --area <declared>` or `nibs set %s --clear area` replaces it",
			e.Path, e.Declared, e.NibID, e.NibID)
	}
}

// ValidateAssignment checks one nib's `area:` value against the declared
// vocabulary, for a caller that SUPPLIED the value — a create, or the CLI
// pre-check on the flag itself. The empty string passes: an unset area is a
// normal state, and IsValid deliberately answers only the membership
// question.
func (a *Areas) ValidateAssignment(path string) error {
	if path == "" || a.IsValid(path) {
		return nil
	}
	return &AreaError{Path: RenderAreaPath(path), Declared: a.declaredList()}
}

// declaredList renders the vocabulary for a refusal, or the empty string when
// the store declares none — which is what selects AreaError's no-areas wording.
func (a *Areas) declaredList() string {
	if !a.Declared() {
		return ""
	}
	return a.List()
}

// ValidateStored is the same rule for a write to a nib that ALREADY EXISTS,
// where the value being judged need not have come from the request at all: a
// write re-checks the `area:` the nib holds, so retiring or renaming an `areas:`
// entry turns every nib that carried it into a write dead end.
//
// Only the message differs, and it has to. "invalid area X: must be one of …"
// reads as a complaint about an argument, so a caller who passed no area is told
// to correct something they never wrote — with nothing said about which nib the
// value belongs to, that the refusal is standing rather than about this request,
// or how to get out of it. The clause is conditional because this path cannot
// tell the two apart: `--area <undeclared>` on an existing nib arrives here too,
// and for that caller the leading half is already the whole answer.
//
// Passing the nib's id is what lets the escapes be named as runnable commands
// rather than as a shape with `<id>` still in it — for the callers that reach
// here on behalf of a nib the user never typed (`nibs close`'s member guards,
// the ordering backfill), the id in the message is the only one there is.
func (a *Areas) ValidateStored(nibID, path string) error {
	if path == "" || a.IsValid(path) {
		return nil
	}
	return &AreaError{Path: RenderAreaPath(path), Declared: a.declaredList(), NibID: safetext.Strip(nibID)}
}

// IsWithin reports whether path is ancestor or sits below it — the
// downward-closed primitive an area filter needs.
//
// The answer is over the DECLARED TREE, not over the strings: `webhooks` is not
// within `web` even though one is a prefix of the other, because they are two
// roots. Both ends must be declared; an undeclared path is not within anything,
// so a value no longer in the vocabulary cannot be swept in by a filter that
// happens to name one of its former ancestors.
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
