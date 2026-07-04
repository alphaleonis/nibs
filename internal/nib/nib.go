package nib

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

// tagPattern matches valid tags: lowercase letters, numbers, and hyphens.
// Must start with a letter, can contain hyphens but not consecutively or at the end.
var tagPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ValidateTag checks if a tag is valid (lowercase, URL-safe, single word).
func ValidateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("invalid tag %q: must be lowercase, start with a letter, and contain only letters, numbers, and hyphens", tag)
	}
	return nil
}

// NormalizeTag converts a tag to its canonical form (lowercase).
func NormalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// HasTag returns true if the nib has the specified tag.
func (b *Nib) HasTag(tag string) bool {
	normalized := NormalizeTag(tag)
	for _, t := range b.Tags {
		if t == normalized {
			return true
		}
	}
	return false
}

// AddTag adds a tag to the nib if it doesn't already exist.
// Returns an error if the tag is invalid.
func (b *Nib) AddTag(tag string) error {
	normalized := NormalizeTag(tag)
	if err := ValidateTag(normalized); err != nil {
		return err
	}
	if !b.HasTag(normalized) {
		b.Tags = append(b.Tags, normalized)
	}
	return nil
}

// RemoveTag removes a tag from the nib.
func (b *Nib) RemoveTag(tag string) {
	normalized := NormalizeTag(tag)
	result := make([]string, 0, len(b.Tags))
	for _, t := range b.Tags {
		if t != normalized {
			result = append(result, t)
		}
	}
	b.Tags = result
}

// HasParent returns true if the nib has a parent.
func (b *Nib) HasParent() bool {
	return b.Parent != ""
}

// Deprecated: IsBlocking only reads the legacy Blocking field, which is no longer persisted in v1+.
// Use Core.FindIncomingLinks or Core.IsBlocking instead.
func (b *Nib) IsBlocking(id string) bool {
	for _, target := range b.Blocking {
		if target == id {
			return true
		}
	}
	return false
}

// Deprecated: AddBlocking modifies the legacy Blocking field, which is no longer persisted in v1+.
func (b *Nib) AddBlocking(id string) {
	if !b.IsBlocking(id) {
		b.Blocking = append(b.Blocking, id)
	}
}

// Deprecated: RemoveBlocking modifies the legacy Blocking field, which is no longer persisted in v1+.
func (b *Nib) RemoveBlocking(id string) bool {
	result := make([]string, 0, len(b.Blocking))
	found := false
	for _, target := range b.Blocking {
		if target != id {
			result = append(result, target)
		} else {
			found = true
		}
	}
	b.Blocking = result
	return found
}

// IsBlockedBy returns true if this nib is blocked by the given nib ID.
func (b *Nib) IsBlockedBy(id string) bool {
	for _, blocker := range b.BlockedBy {
		if blocker == id {
			return true
		}
	}
	return false
}

// AddBlockedBy adds a nib ID to the blocked-by list if not already present.
func (b *Nib) AddBlockedBy(id string) {
	if !b.IsBlockedBy(id) {
		b.BlockedBy = append(b.BlockedBy, id)
	}
}

// RemoveBlockedBy removes a nib ID from the blocked-by list.
// Returns true if the ID was found and removed.
func (b *Nib) RemoveBlockedBy(id string) bool {
	result := make([]string, 0, len(b.BlockedBy))
	found := false
	for _, blocker := range b.BlockedBy {
		if blocker != id {
			result = append(result, blocker)
		} else {
			found = true
		}
	}
	b.BlockedBy = result
	return found
}

// Nib represents an issue stored as a markdown file with front matter.
type Nib struct {
	// ID is the unique NanoID identifier (from filename).
	ID string `yaml:"-" json:"id"`
	// Slug is the optional human-readable part of the filename.
	Slug string `yaml:"-" json:"slug,omitempty"`
	// Path is the relative path from .nibs/ root (e.g., "epic-auth/abc123-login.md").
	Path string `yaml:"-" json:"path"`

	// Version is the file format version. Absent = 0 (legacy).
	Version int `yaml:"version" json:"version"`

	// Front matter fields
	Title     string     `yaml:"title" json:"title"`
	Status    string     `yaml:"status" json:"status"`
	Type      string     `yaml:"type,omitempty" json:"type,omitempty"`
	Priority  string     `yaml:"priority,omitempty" json:"priority,omitempty"`
	Estimate  string     `yaml:"estimate,omitempty" json:"estimate,omitempty"`
	Tags      []string   `yaml:"tags,omitempty" json:"tags,omitempty"`
	CreatedAt *time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt *time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`

	// Body is the markdown content after the front matter.
	Body string `yaml:"-" json:"body,omitempty"`

	// Parent is the optional parent nib ID (milestone, epic, or feature).
	Parent string `yaml:"parent,omitempty" json:"parent,omitempty"`

	// Blocking is DEPRECATED — only used for parsing v0 (legacy) files during migration.
	// In v1+, blocking is computed by scanning other nibs' BlockedBy fields via FindIncomingLinks.
	Blocking []string `yaml:"blocking,omitempty" json:"-"`

	// BlockedBy is a list of nib IDs that are blocking this nib.
	BlockedBy []string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`

	// Documents is a list of repo-root-relative paths to linked documents.
	Documents []string `yaml:"documents,omitempty" json:"documents,omitempty"`

	// Order is a fractional index string for sorting among siblings.
	Order string `yaml:"order,omitempty" json:"order,omitempty"`

	// priorityMigrated is a transient, load-boundary-only flag (never
	// serialized) set by Parse when a legacy `priority: deferred` value was
	// normalized to `low`. The loader reads it via PriorityMigrated()
	// immediately after Parse to persist the normalization so the on-disk value
	// converges with memory at load time. It is not general-purpose "was this
	// nib ever migrated" state: Clone() clears it, so it is meaningful only on a
	// freshly-parsed nib.
	priorityMigrated bool
}

// PriorityMigrated reports whether Parse normalized a legacy `priority: deferred`
// value to `low` for this nib. The loader persists such nibs so the on-disk
// value converges with the in-memory value (avoiding an etag divergence that
// would break if-match updates). See the migration note in Parse.
func (b *Nib) PriorityMigrated() bool {
	return b.priorityMigrated
}

// frontMatter is the subset of Nib that gets serialized to YAML front matter.
type frontMatter struct {
	Version   int        `yaml:"version,omitempty"`
	Title     string     `yaml:"title"`
	Status    string     `yaml:"status"`
	Type      string     `yaml:"type,omitempty"`
	Priority  string     `yaml:"priority,omitempty"`
	Estimate  string     `yaml:"estimate,omitempty"`
	Tags      []string   `yaml:"tags,omitempty"`
	CreatedAt *time.Time `yaml:"created_at,omitempty"`
	UpdatedAt *time.Time `yaml:"updated_at,omitempty"`
	Parent    string     `yaml:"parent,omitempty"`
	Blocking  []string   `yaml:"blocking,omitempty"`
	BlockedBy []string   `yaml:"blocked_by,omitempty"`
	Documents []string   `yaml:"documents,omitempty"`
	Order     string     `yaml:"order,omitempty"`
}

// resolvedStatuses is the single source of truth for the "resolved" (done)
// status set — statuses that mean the nib is finished. Both IsResolvedStatus
// and ResolvedStatusNames derive from it, so the set has exactly one definition.
var resolvedStatuses = []string{"completed", "scrapped"}

// IsResolvedStatus returns true if the status means the nib is "done"
// (either completed or scrapped). This is the canonical definition used
// by all packages for filtering resolved blockers and blocking relationships.
func IsResolvedStatus(status string) bool {
	for _, s := range resolvedStatuses {
		if status == s {
			return true
		}
	}
	return false
}

// ResolvedStatusNames returns the names of all statuses considered "resolved"
// (done) — a defensive copy of the canonical resolvedStatuses set, so callers
// cannot mutate the source. Today this is {completed, scrapped}.
func ResolvedStatusNames() []string {
	names := make([]string, len(resolvedStatuses))
	copy(names, resolvedStatuses)
	return names
}

// Parse reads a nib from a reader (markdown with YAML front matter).
func Parse(r io.Reader) (*Nib, error) {
	var fm frontMatter
	body, err := frontmatter.Parse(r, &fm)
	if err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	// Trim trailing newline from body (POSIX files end with newline, but it's not part of content)
	bodyStr := strings.TrimSuffix(string(body), "\n")

	if err := ValidateOrderKey(fm.Order); err != nil {
		return nil, fmt.Errorf("invalid order key: %w", err)
	}

	// Migration: "deferred" was removed as a priority and reintroduced as a
	// status. Files written before the change may still carry
	// `priority: deferred`. Priority was never validated at parse time, so such
	// a file already loaded; the point of this normalization is to produce a
	// valid, sanely-sortable value on the current (deferred-free) priority axis.
	// We target "low" because "deferred" was the *lowest* priority — ranked
	// below "low" in the old enum — so mapping it to "low" preserves its
	// relative rank. (Do not "tidy" this to "normal": that would silently
	// re-rank legacy nibs upward.) The value is normalized in memory here so it
	// is always valid even without a Core; when loaded through a Core's bulk
	// Load, the loader persists it (see Core.loadNibReconciledLocked) so disk and
	// memory agree immediately — otherwise the read etag (hash of Render() →
	// "low") diverges from the write-side etag (hash of raw disk bytes →
	// "deferred") and if-match updates fail.
	priorityMigrated := false
	if fm.Priority == "deferred" {
		fm.Priority = "low"
		priorityMigrated = true
	}

	return &Nib{
		Version:          fm.Version,
		Title:            fm.Title,
		Status:           fm.Status,
		Type:             fm.Type,
		Priority:         fm.Priority,
		Estimate:         fm.Estimate,
		Tags:             fm.Tags,
		CreatedAt:        fm.CreatedAt,
		UpdatedAt:        fm.UpdatedAt,
		Body:             bodyStr,
		Parent:           fm.Parent,
		Blocking:         fm.Blocking,
		BlockedBy:        fm.BlockedBy,
		Documents:        fm.Documents,
		Order:            fm.Order,
		priorityMigrated: priorityMigrated,
	}, nil
}

// renderFrontMatter is used for YAML output with yaml.v3 (supports custom marshalers).
// Note: Blocking is intentionally omitted — only blockedBy is persisted (single-side storage).
type renderFrontMatter struct {
	Version   int        `yaml:"version"`
	Title     string     `yaml:"title"`
	Status    string     `yaml:"status"`
	Type      string     `yaml:"type,omitempty"`
	Priority  string     `yaml:"priority,omitempty"`
	Estimate  string     `yaml:"estimate,omitempty"`
	Tags      []string   `yaml:"tags,omitempty"`
	CreatedAt *time.Time `yaml:"created_at,omitempty"`
	UpdatedAt *time.Time `yaml:"updated_at,omitempty"`
	Parent    string     `yaml:"parent,omitempty"`
	BlockedBy []string   `yaml:"blocked_by,omitempty"`
	Documents []string   `yaml:"documents,omitempty"`
	Order     string     `yaml:"order,omitempty"`
}

// Render serializes the nib back to markdown with YAML front matter.
func (b *Nib) Render() ([]byte, error) {
	fm := renderFrontMatter{
		Version:   b.Version,
		Title:     b.Title,
		Status:    b.Status,
		Type:      b.Type,
		Priority:  b.Priority,
		Estimate:  b.Estimate,
		Tags:      b.Tags,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
		Parent:    b.Parent,
		BlockedBy: b.BlockedBy,
		Documents: b.Documents,
		Order:     b.Order,
	}

	fmBytes, err := yaml.Marshal(&fm)
	if err != nil {
		return nil, fmt.Errorf("marshaling front matter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	if b.ID != "" {
		buf.WriteString("# ")
		buf.WriteString(b.ID)
		buf.WriteString("\n")
	}
	buf.Write(fmBytes)
	buf.WriteString("---\n")
	if b.Body != "" {
		// Only add newline separator if body doesn't already start with one
		if !strings.HasPrefix(b.Body, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString(b.Body)
		// Ensure trailing newline if body doesn't end with one
		if !strings.HasSuffix(b.Body, "\n") {
			buf.WriteString("\n")
		}
	} else {
		// Even without body, add trailing newline for POSIX compliance
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// Clone returns a deep copy of the Nib. Slice fields (Tags, BlockedBy, Blocking,
// Documents) and pointer fields (CreatedAt, UpdatedAt) are copied independently
// so that mutating the clone does not affect the original.
func (b *Nib) Clone() *Nib {
	clone := *b // shallow copy of all value fields

	// priorityMigrated is a load-boundary-only signal (consumed by the loader
	// right after Parse). A clone is a working copy for mutation/update, never a
	// freshly-parsed nib, so clear it here rather than let a stale `true` ride
	// along through every Clone/Update cycle.
	clone.priorityMigrated = false

	// Deep-copy slice fields
	if b.Tags != nil {
		clone.Tags = make([]string, len(b.Tags))
		copy(clone.Tags, b.Tags)
	}
	if b.BlockedBy != nil {
		clone.BlockedBy = make([]string, len(b.BlockedBy))
		copy(clone.BlockedBy, b.BlockedBy)
	}
	if b.Blocking != nil {
		clone.Blocking = make([]string, len(b.Blocking))
		copy(clone.Blocking, b.Blocking)
	}
	if b.Documents != nil {
		clone.Documents = make([]string, len(b.Documents))
		copy(clone.Documents, b.Documents)
	}

	// Deep-copy pointer fields
	if b.CreatedAt != nil {
		t := *b.CreatedAt
		clone.CreatedAt = &t
	}
	if b.UpdatedAt != nil {
		t := *b.UpdatedAt
		clone.UpdatedAt = &t
	}

	return &clone
}

// ETag returns a hash of the nib's rendered content for optimistic concurrency control.
// Uses FNV-1a 64-bit hash, producing a 16-character hex string.
// Returns "0000000000000000" if rendering fails (should never happen for valid nibs).
func (b *Nib) ETag() string {
	content, err := b.Render()
	if err != nil {
		// Return a sentinel value that will never match a real ETag,
		// ensuring validation will fail rather than silently passing.
		return "0000000000000000"
	}
	h := fnv.New64a()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// MarshalJSON implements json.Marshaler to include computed etag field.
func (b *Nib) MarshalJSON() ([]byte, error) {
	type NibAlias Nib // Avoid infinite recursion
	return json.Marshal(&struct {
		*NibAlias
		ETag string `json:"etag"`
	}{
		NibAlias: (*NibAlias)(b),
		ETag:      b.ETag(),
	})
}
