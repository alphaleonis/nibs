package nib

import (
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
