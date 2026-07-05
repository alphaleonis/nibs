package nib

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"reflect"
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

	// Blocking is DEPRECATED for computing links: in v1+, blocking is derived by
	// scanning other nibs' BlockedBy fields via FindIncomingLinks, and normal v1
	// nibs never set it. It is still parsed from v0 (legacy) files during
	// migration AND re-emitted by Render (with omitempty) so a legacy v0 file's
	// on-disk `blocking:` content round-trips into the canonical etag instead of
	// being silently stripped; see renderFrontMatter.Blocking.
	Blocking []string `yaml:"blocking,omitempty" json:"-"`

	// BlockedBy is a list of nib IDs that are blocking this nib.
	BlockedBy []string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`

	// Documents is a list of repo-root-relative paths to linked documents.
	Documents []string `yaml:"documents,omitempty" json:"documents,omitempty"`

	// Order is a fractional index string for sorting among siblings.
	Order string `yaml:"order,omitempty" json:"order,omitempty"`

	// Extra holds front-matter keys that none of the modeled fields above claim
	// (e.g. a hand-added `assignee: bob`, or forward-compatible keys written by a
	// newer tool). Parse captures them via a yaml inline catch-all and Render
	// re-emits them, so unknown keys survive a round-trip instead of being
	// silently stripped. This keeps the canonical etag (a hash of Render()) a
	// faithful witness of the on-disk content: an external edit confined to an
	// unmodeled key still changes the etag.
	//
	// Round-trip fidelity boundary: Parse decodes with yaml.v2 (untyped scalars
	// are type-inferred) while Render marshals with yaml.v3, so a value round-trips
	// byte-for-byte only when it is unambiguous across YAML 1.1<->1.2 (or quoted).
	// YAML-1.1 bool-like scalars (`y`/`yes`/`no`/`on`/`off` — the "Norway problem")
	// coerce to `true`/`false`, and signed-zero floats (`-0.0`) normalize to `0`.
	// Such values are normalized once on the first round-trip and are stable
	// thereafter; tightening this boundary is tracked in nibs-r3y1.
	//
	// Not exposed over the GraphQL/JSON surface (json:"-"). yaml.v3 sorts inline-
	// map keys, so the render (and thus the etag) stays deterministic regardless of
	// Go map iteration order.
	Extra map[string]any `yaml:"-" json:"-"`

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

// frontMatter is the subset of Nib parsed from YAML front matter (via yaml.v2).
//
// LOAD-BEARING INVARIANT: frontMatter's modeled yaml-key set must stay identical
// to renderFrontMatter's (the render projection). Parse routes every key NOT
// matched by a named field here into the inline Extra catch-all; Render re-emits
// Extra as an inline map. If a key were modeled on one side but not the other, a
// pre-existing on-disk file could parse that key into Extra and then collide with
// the modeled render field — which yaml.v3 turns into a panic. The symmetry is
// enforced by TestFrontMatterRenderProjectionSymmetry (reflection) and defended
// at render time by Render's modeledRenderTags collision drop.
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

	// Extra is a yaml inline catch-all: any front-matter key not matched by a
	// named field above lands here (via the frontmatter library's yaml.v2
	// unmarshal), so unknown keys survive parsing and can be re-emitted by
	// Render. See Nib.Extra.
	Extra map[string]any `yaml:",inline"`
}

// DefaultType and DefaultPriority are the single source of truth for the
// PRESENTATION defaults applied when a nib file omits the corresponding front
// matter key. They are consumed via EffectiveType/EffectivePriority.
//
// The stored Nib keeps Type/Priority EMPTY when the file omits them: Render
// carries `omitempty` on both, so the canonical render — and thus the etag —
// stays a faithful witness of the on-disk bytes. If loadNib synthesized these
// in memory (as it once did), a bare-parse of the same file would render no
// such key while the in-memory ETag() would render the default, diverging with
// no on-disk change and false-conflicting an if-match Update (nibs-7d3o). The
// defaults are therefore applied only at the consumption boundary (GraphQL
// field resolvers, sort/filter, TUI/CLI display, the JSON projection).
//
// They live in the nib package (not config) to avoid the nib->config layering
// edge removed alongside resolvedStatuses; the values intentionally match
// config's default type ("task") and priority ("normal"). config's DefaultTypes
// and DefaultPriorities remain the source for the full enum and colors — these
// two constants only name the fallback member of each, and a guard test in the
// config package pins them equal so the two definitions cannot drift.
const (
	DefaultType     = "task"
	DefaultPriority = "normal"
)

// EffectiveType returns the nib's type, or DefaultType when the file omitted it.
// Use this at every consumption boundary (display, sort, filter, GraphQL/JSON)
// that must treat a type-less nib as the default; never mutate b.Type to the
// default, or the etag will diverge from the on-disk bytes (see DefaultType).
func (b *Nib) EffectiveType() string {
	if b.Type == "" {
		return DefaultType
	}
	return b.Type
}

// EffectivePriority returns the nib's priority, or DefaultPriority when omitted.
// See EffectiveType for why the stored Priority is never mutated to the default.
func (b *Nib) EffectivePriority() string {
	if b.Priority == "" {
		return DefaultPriority
	}
	return b.Priority
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
	// Load, the loader persists it (see Core.loadNibReconciledLocked) so the raw
	// on-disk bytes converge with the in-memory value immediately. This is no
	// longer required for etag correctness — Core.computeStoredETag parses the
	// on-disk file and hashes its canonical Render(), so a legacy `deferred` file
	// already yields the same etag as the in-memory `low` value — but it keeps
	// disk and memory in sync for external consumers (git diffs, editors).
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
		Extra:            fm.Extra,
		priorityMigrated: priorityMigrated,
	}, nil
}

// renderFrontMatter is used for YAML output with yaml.v3 (supports custom marshalers).
//
// Blocking carries omitempty: for a v1+ nib it is always nil (blocking is
// single-side, computed at query time from other nibs' BlockedBy), so it is
// absent from the render — the normal case is unchanged. It is emitted ONLY for
// a legacy v0 nib parsed straight from disk (before migrateV0ToV1 clears it), so
// the canonical render — and thus the etag — stays a faithful witness of a v0
// file's on-disk `blocking:` content rather than silently dropping it.
//
// Extra is a yaml inline catch-all mirroring frontMatter.Extra: unknown keys
// captured on Parse are re-emitted here. yaml.v3 sorts inline-map keys, so the
// render is deterministic regardless of Go map iteration order.
//
// LOAD-BEARING INVARIANT: renderFrontMatter's modeled yaml-key set must stay
// identical to frontMatter's (the parse projection) — see that struct's note.
// yaml.v3 PANICS ("cannot have key ... in inlined map: conflicts with struct
// field") if an inline Extra key collides with a modeled field name, so Render
// pre-drops any such key (modeledRenderTags) to keep its ([]byte, error) contract
// panic-free, and TestFrontMatterRenderProjectionSymmetry pins the two key sets.
type renderFrontMatter struct {
	Version   int            `yaml:"version"`
	Title     string         `yaml:"title"`
	Status    string         `yaml:"status"`
	Type      string         `yaml:"type,omitempty"`
	Priority  string         `yaml:"priority,omitempty"`
	Estimate  string         `yaml:"estimate,omitempty"`
	Tags      []string       `yaml:"tags,omitempty"`
	CreatedAt *time.Time     `yaml:"created_at,omitempty"`
	UpdatedAt *time.Time     `yaml:"updated_at,omitempty"`
	Parent    string         `yaml:"parent,omitempty"`
	Blocking  []string       `yaml:"blocking,omitempty"`
	BlockedBy []string       `yaml:"blocked_by,omitempty"`
	Documents []string       `yaml:"documents,omitempty"`
	Order     string         `yaml:"order,omitempty"`
	Extra     map[string]any `yaml:",inline"`
}

// modeledRenderTags is the set of YAML key names that renderFrontMatter models
// with a named field (i.e. every field except the ,inline Extra catch-all),
// derived by reflection so it can never drift from the struct. Render consults it
// to drop any Extra key that collides with a modeled field name: yaml.v3 panics
// ("cannot have key ... in inlined map") on such a collision, and a modeled key
// appearing in Extra is a programming error (Parse only ever routes UNMODELED
// keys into Extra), so the modeled field wins and Render stays panic-free.
var modeledRenderTags = buildModeledRenderTags()

func buildModeledRenderTags() map[string]struct{} {
	t := reflect.TypeOf(renderFrontMatter{})
	tags := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name == "" {
			continue // the ,inline Extra catch-all (and any unnamed field)
		}
		tags[name] = struct{}{}
	}
	return tags
}

// renderExtra returns b.Extra with any key colliding with a modeled render field
// dropped, without mutating b.Extra. The common case (no Extra, or no collision)
// returns the original map with no allocation. A collision cannot arise from
// normal Parse output (parse and render model the same key set), so this is
// defense for the "promote an unknown key to a modeled field" evolution path,
// keeping Render panic-free rather than letting yaml.v3 panic on the inline map.
func (b *Nib) renderExtra() map[string]any {
	if len(b.Extra) == 0 {
		return b.Extra
	}
	filtered := b.Extra
	copied := false
	for k := range b.Extra {
		if _, collides := modeledRenderTags[k]; !collides {
			continue
		}
		if !copied {
			filtered = make(map[string]any, len(b.Extra))
			for kk, vv := range b.Extra {
				filtered[kk] = vv
			}
			copied = true
		}
		delete(filtered, k)
	}
	return filtered
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
		Blocking:  b.Blocking,
		BlockedBy: b.BlockedBy,
		Documents: b.Documents,
		Order:     b.Order,
		Extra:     b.renderExtra(),
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
// Documents), the Extra unknown-key map, and pointer fields (CreatedAt,
// UpdatedAt) are copied independently so that mutating the clone does not affect
// the original.
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

	// Deep-copy the unknown-key passthrough so a mutated clone can't alias (and
	// thus corrupt) the original's Extra map. A shallow struct copy would share
	// the same underlying map. Note this is a top-level copy; nested container
	// values (maps/slices inside a value) are still shared, which is acceptable
	// because Extra values are treated as opaque, immutable passthrough content.
	if b.Extra != nil {
		clone.Extra = make(map[string]any, len(b.Extra))
		for k, v := range b.Extra {
			clone.Extra[k] = v
		}
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

// MarshalJSON implements json.Marshaler to include the computed etag field and
// to project the PRESENTATION defaults for Type/Priority.
//
// The stored Nib keeps Type/Priority empty when the file omits them (so the etag
// witnesses the on-disk bytes — see DefaultType). The JSON surface, however, must
// present the effective value ("task"/"normal") so it agrees with the GraphQL
// field resolvers and with the pre-nibs-7d3o behavior (loadNib used to synthesize
// these). We marshal a value COPY with the effective values applied, leaving the
// receiver — and thus b.ETag(), computed from the raw Render() — untouched.
func (b *Nib) MarshalJSON() ([]byte, error) {
	type NibAlias Nib // Avoid infinite recursion
	alias := NibAlias(*b)
	alias.Type = b.EffectiveType()
	alias.Priority = b.EffectivePriority()
	return json.Marshal(&struct {
		*NibAlias
		ETag string `json:"etag"`
	}{
		NibAlias: &alias,
		ETag:     b.ETag(),
	})
}
