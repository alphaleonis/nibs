package nib

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// IsIDChar reports whether c is a valid short-ID character, i.e. a member of
// idAlphabet ([0-9a-z]). This is THE charset gate other packages must reuse
// instead of hand-rolling byte-range checks — idAlphabet is the single source
// of truth for the nib short-ID charset, and this predicate is derived from it.
func IsIDChar(c byte) bool {
	return strings.IndexByte(idAlphabet, c) >= 0
}

// NewID generates a new NanoID for a nib with an optional prefix and configurable length.
func NewID(prefix string, length int) string {
	id, err := gonanoid.Generate(idAlphabet, length)
	if err != nil {
		panic(err) // should never happen with valid alphabet
	}
	return prefix + id
}

// ErrIDNotFilename is the sentinel every ValidateIDForFilename refusal wraps, so
// callers can classify the failure without matching on message text.
var ErrIDNotFilename = errors.New("unusable nib id")

// ValidateIDForFilename refuses an id that cannot survive the filename round
// trip. An id becomes a filename (BuildFilename) and is read back out of one
// (ParseFilename applied to filepath.Base) on the next load, so an id that is
// not a plain file name within one directory names a DIFFERENT nib — or no nib
// at all — as soon as the store is reloaded.
//
// This is deliberately NOT a charset gate: ParseFilename accepts any id shape,
// and internal/ui/tree.go documents why that stays true. The rule here is about
// PATH SHAPE only, and it is the whole of THAT rule:
//
//   - A path separator makes the leading part a directory. `--prefix a/b-`
//     writes data/a/b-x9z2.md, whose id reads back as "b-x9z2"; `--prefix ../../`
//     resolves upward out of the store entirely, because filepath.Join CLEANS
//     its result rather than refusing it, and MkdirAll then creates whatever
//     directories the traversal names. Both separators are refused on every
//     platform: a backslash is an ordinary filename byte on Linux but a separator
//     on Windows, and nib files travel between the two through git.
//   - "." and ".." name a directory entry rather than a file. They carry no
//     separator, so only a caller that assigns Nib.ID itself can reach them —
//     the generated short id is [0-9a-z], so no prefix can compose one.
//
// Refusing separators also refuses every traversal: a ".." path SEGMENT cannot
// form without one.
//
// Naming a file in the right directory is a separate question from that file
// naming the id back, and this rule answers only the first. An id can be an
// entirely ordinary file name and still decompose into a different id, because
// ParseFilename splits on separators path shape has no interest in.
// ValidateIDRoundTrip is that second half, and Core.Create applies both.
func ValidateIDForFilename(id string) error {
	if i := strings.IndexAny(id, `/\`); i >= 0 {
		return fmt.Errorf("%w %q: an id must not contain the path separator %q — it becomes the nib's filename, so anything before a separator turns into a directory and the id no longer reads back as itself; check --prefix and the store's nibs.prefix", ErrIDNotFilename, id, id[i:i+1])
	}
	if id == "." || id == ".." {
		return fmt.Errorf("%w %q: an id must name a file, and %q names a directory entry", ErrIDNotFilename, id, id)
	}
	return nil
}

// ErrIDNotRoundTrip is the sentinel every ValidateIDRoundTrip refusal wraps, so
// callers can classify the failure without matching on message text — the same
// contract ErrIDNotFilename carries for the path-shape half.
var ErrIDNotRoundTrip = errors.New("unusable nib id")

// ValidateIDRoundTrip refuses an id whose own file name does not read back as
// that id. BuildFilename composes the name and ParseFilename decomposes it on
// every load, and nothing else records the id — a nib's id is DERIVED from its
// file name each time the store is read. An id that decomposes into something
// else therefore names a different nib, or no nib at all, from the next load
// onward: the create reports an id the very next command cannot find.
//
// This is the GRAMMAR half of the file-name contract and ValidateIDForFilename
// is the PATH SHAPE half. They stay separate because they answer to different
// mechanisms and are useful to tell apart. Path shape is about what
// filepath.Join and MkdirAll do with a separator, holds under any vocabulary,
// and refuses a write that lands somewhere else; grammar is about where
// ParseFilename splits, depends on the prefix the file is read back under, and
// refuses a write that lands in the right place under a name that decodes
// wrong. One sentinel over both would blur two failures with different remedies,
// and would force the path-shape rule to take a prefix it makes no use of.
//
// prefix is the vocabulary the file will be READ BACK under — the store's
// nibs.prefix — which is not always the prefix the id was composed from. It has
// to be a parameter because ParseFilename carries a prefix-aware branch, so the
// same id round-trips under one store's vocabulary and not another's: slugless
// "zz-924q" comes back whole in a store declaring "zz-", and as "zz" in a store
// declaring "tnib-", where the name carries no recognized prefix and the legacy
// single-dash split takes it. Asked under the wrong prefix, the answer is about
// a different store.
//
// Only the id is compared. A matching id means the split landed exactly where
// BuildFilename put its separator, so whatever follows is the slug by
// construction; for a slugless id a match means the whole name came back, hence
// an empty slug. Comparing the slug too would only restate that.
//
// This is deliberately NOT a charset gate either: it refuses the shapes that
// actually break and nothing more. A dotted prefix is the clearest case —
// "a.b-9k3y" WITH a slug round-trips, because BuildFilename's "--" is the first
// separator ParseFilename looks for and it wins over the dot, while the same id
// with NO slug leaves the dot as the only separator in the name and does not.
// That asymmetry is a property of the grammar rather than a gap in the rule,
// which is why the refusal quotes the name and what it decodes to instead of
// describing a charset the caller could have obeyed.
func ValidateIDRoundTrip(id, slug, prefix string) error {
	name := BuildFilename(id, slug)
	got, _ := ParseFilename(name, prefix)
	if got == id {
		return nil
	}
	// The title is named only where it is actually a knob: a slugless name whose
	// id would have survived WITH a slug. The slug comes from the title, so a
	// caller who reaches that shape fixes it by giving the nib a title with a
	// letter or digit in it, and the prefix may be entirely correct. Both of the
	// other shapes are the prefix's doing and no title reaches them — a name that
	// carries a slug already, and one whose id holds a separator of its own.
	remedy := "check --prefix and the store's nibs.prefix"
	if slug == "" && slugWouldFix(id, prefix) {
		remedy = "check the title as well as --prefix and the store's nibs.prefix — the slug comes from the title, and one with no letters or digits leaves no slug to separate the id from"
	}
	return fmt.Errorf("%w %q: its file name %q reads back as %q, so the nib would not be reachable by the id it was created under — a nib's id comes from its file name on every load; %s", ErrIDNotRoundTrip, id, name, got, remedy)
}

// slugWouldFix reports whether giving this id a slug would make its file name
// read back as the id. It separates the two ways a slugless name fails: the id
// is sound and only the missing separator hurt it, or the id carries a
// separator of its own that no slug can outrun. The probe slug's content cannot
// change the answer — BuildFilename writes its "--" ahead of the slug, so the
// first separator ParseFilename finds is never inside one.
func slugWouldFix(id, prefix string) bool {
	got, _ := ParseFilename(BuildFilename(id, "probe"), prefix)
	return got == id
}

// ParseFilename extracts the ID and optional slug from a nib filename. The
// configured id prefix (e.g. "nibs-", "" for none) disambiguates the legacy
// single-dash format, whose separator collides with the trailing dash every
// prefix ends in.
//
// Supports multiple formats for backward compatibility:
//   - New format: "f7g--user-registration.md" -> ("f7g", "user-registration")
//   - Dot format: "f7g.user-registration.md" -> ("f7g", "user-registration")
//   - Prefixed slugless: "nibs-x9z2.md" with prefix "nibs-" -> ("nibs-x9z2", "")
//   - Prefixed legacy slug: "nibs-x9z2-slug.md" with prefix "nibs-" -> ("nibs-x9z2", "slug")
//   - Legacy format (no prefix): "f7g-user-registration.md" -> ("f7g", "user-registration")
//   - ID only: "f7g.md" -> ("f7g", "")
func ParseFilename(name, prefix string) (id, slug string) {
	// Remove .md extension
	name = strings.TrimSuffix(name, ".md")

	// Try new format first (double-dash separator): id--slug
	if idx := strings.Index(name, "--"); idx > 0 {
		return name[:idx], name[idx+2:]
	}

	// Try dot format: id.slug
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx], name[idx+1:]
	}

	// Prefix-aware single-dash handling: a configured prefix always ends in a dash,
	// so the legacy SplitN below would mis-split the prefix's own trailing dash
	// (e.g. "nibs-x9z2" -> id "nibs"). When the name carries the prefix, split only
	// on a dash AFTER it — a legacy single-dash slug on a prefixed id — and treat a
	// prefixed id with no further dash as slugless (the whole name is the id).
	//
	// ORDER-DEPENDENT: this must stay AFTER the "--" and "." checks above. A prefixed
	// id with a double-dash or dot slug (nibs-x9z2--slug / nibs-x9z2.slug) resolves
	// via those branches; reordering this ahead of them would swallow the separator
	// and misparse the slug. (rest can never START with a dash here — that would be a
	// "--" in name, already handled above — so idx > 0 is exhaustive.)
	if prefix != "" && strings.HasPrefix(name, prefix) {
		rest := name[len(prefix):]
		if idx := strings.Index(rest, "-"); idx > 0 {
			return prefix + rest[:idx], rest[idx+1:]
		}
		return name, ""
	}

	// Fall back to original legacy format (single dash separator): id-slug. Reached
	// only with no configured prefix, or a name that doesn't carry it — preserving
	// behavior for prefix-less legacy ids.
	parts := strings.SplitN(name, "-", 2)
	id = parts[0]
	if len(parts) > 1 {
		slug = parts[1]
	}
	return id, slug
}

// BuildFilename constructs a filename from ID and optional slug.
// Uses double-dash separator: id--slug.md
func BuildFilename(id, slug string) string {
	if slug == "" {
		return id + ".md"
	}
	return id + "--" + slug + ".md"
}

// Slugify converts a title to a URL-friendly slug.
func Slugify(title string) string {
	// Convert to lowercase
	s := strings.ToLower(title)

	// Replace spaces and underscores with dashes
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	// Remove non-alphanumeric characters (except dashes)
	var result strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			result.WriteRune(r)
		}
	}
	s = result.String()

	// Collapse multiple dashes
	re := regexp.MustCompile(`-+`)
	s = re.ReplaceAllString(s, "-")

	// Trim dashes from ends
	s = strings.Trim(s, "-")

	// Truncate to reasonable length
	if len(s) > 50 {
		s = s[:50]
		// Don't end with a dash
		s = strings.TrimRight(s, "-")
	}

	return s
}
