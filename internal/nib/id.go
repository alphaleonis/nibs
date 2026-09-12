package nib

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// IsIDChar reports whether c is in idAlphabet. Use it instead of a hand-written
// byte range.
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

// ErrIDNotFilename wraps every ValidateIDForFilename refusal.
var ErrIDNotFilename = errors.New("unusable nib id")

// ValidateIDForFilename refuses an id that is not a plain file name: one holding
// '/' or '\', either of which makes its leading part a directory on some
// platform (`--prefix ../../` escapes the store), and "." or "..". It is not a
// charset gate. An id that passes can still fail ValidateIDRoundTrip; Core.Create
// applies both.
func ValidateIDForFilename(id string) error {
	if i := strings.IndexAny(id, `/\`); i >= 0 {
		return fmt.Errorf("%w %q: an id must not contain the path separator %q — it becomes the nib's filename, so anything before a separator turns into a directory and the id no longer reads back as itself; check --prefix and the store's nibs.prefix", ErrIDNotFilename, id, id[i:i+1])
	}
	if id == "." || id == ".." {
		return fmt.Errorf("%w %q: an id must name a file, and %q names a directory entry", ErrIDNotFilename, id, id)
	}
	return nil
}

// ErrIDNotRoundTrip wraps every ValidateIDRoundTrip refusal.
var ErrIDNotRoundTrip = errors.New("unusable nib id")

// ValidateIDRoundTrip refuses an id whose file name does not read back as that id;
// a nib's id is derived from its file name on every load. prefix is the store's
// nibs.prefix the file is read back under: slugless "zz-924q" reads back whole
// under "zz-" and as "zz" under "tnib-". Only the id is compared.
func ValidateIDRoundTrip(id, slug, prefix string) error {
	name := BuildFilename(id, slug)
	got, _ := ParseFilename(name, prefix)
	if got == id {
		return nil
	}
	// Mention the title only when a slug, which comes from it, would save the id.
	remedy := "check --prefix and the store's nibs.prefix"
	if slug == "" && slugWouldFix(id, prefix) {
		remedy = "check the title as well as --prefix and the store's nibs.prefix — the slug comes from the title, and one with no letters or digits leaves no slug to separate the id from"
	}
	return fmt.Errorf("%w %q: its file name %q reads back as %q, so the nib would not be reachable by the id it was created under — a nib's id comes from its file name on every load; %s", ErrIDNotRoundTrip, id, name, got, remedy)
}

// slugWouldFix reports whether the id would read back as itself given a slug.
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
	name = strings.TrimSuffix(name, ".md")

	// Try new format first (double-dash separator): id--slug
	if idx := strings.Index(name, "--"); idx > 0 {
		return name[:idx], name[idx+2:]
	}

	// Try dot format: id.slug
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx], name[idx+1:]
	}

	// A prefix ends in a dash, so split only on a dash after it; a prefixed name
	// with no further dash is slugless. Keep this after the "--" and "." checks.
	if prefix != "" && strings.HasPrefix(name, prefix) {
		rest := name[len(prefix):]
		if idx := strings.Index(rest, "-"); idx > 0 {
			return prefix + rest[:idx], rest[idx+1:]
		}
		return name, ""
	}

	// Legacy single-dash id-slug, for a name without the prefix.
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

// maxSlugBytes caps the slug component of a nib's file name (see BuildFilename).
const maxSlugBytes = 50

// Slugify converts a title to a URL-friendly slug.
func Slugify(title string) string {
	s := strings.ToLower(title)

	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	var result strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			result.WriteRune(r)
		}
	}
	s = result.String()

	re := regexp.MustCompile(`-+`)
	s = re.ReplaceAllString(s, "-")

	s = strings.Trim(s, "-")

	// Truncate to maxSlugBytes on a rune boundary.
	if len(s) > maxSlugBytes {
		cut := maxSlugBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
		s = strings.TrimRight(s, "-")
	}

	return s
}
