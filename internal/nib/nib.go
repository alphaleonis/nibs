package nib

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
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

// CurrentVersion is the file format version this build reads and writes. New
// nibs are stamped with it, and a file carrying a HIGHER version refuses to be
// operated on (it was written by a newer nibs). Migration steps deliberately do
// NOT use this constant for the version they write: each step's output version
// is a fixed property of that step (v0→v1 always writes 1), so bumping
// CurrentVersion for a future format must never change an existing step's
// output.
const CurrentVersion = 1

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
	// newer tool). Parse captures each unknown key's value as a raw yaml.v3 node
	// via a yaml inline catch-all and Render re-emits it verbatim, so unknown keys
	// survive a round-trip instead of being silently stripped. This keeps the
	// canonical etag (a hash of Render()) a faithful witness of the on-disk
	// content: an external edit confined to an unmodeled key still changes the etag.
	//
	// Round-trip fidelity: unknown-key SCALAR VALUES round-trip verbatim. Both
	// Parse and Render use yaml.v3, and each unknown value is carried as a raw
	// yaml.Node preserving its original scalar text, quoting style, and tag, so no
	// yaml re-inference happens on either side. This closes the entire
	// YAML-1.1<->1.2 coercion class a yaml.v2-parse / yaml.v3-render split would
	// otherwise inflict — including the "Norway problem" bool-like scalars
	// (`y`/`yes`/`no`/`on`/`off`, kept as strings) and signed-zero floats (`-0.0`,
	// kept verbatim rather than collapsing to `0`). parse->render is a fixed point,
	// so the render — and thus the etag — is stable and never self-conflicts (a
	// TRUE fixed point).
	//
	// Non-scalar FORMATTING is NOT byte-preserved: block-scalar indentation is
	// normalized (a 2-space `|` block re-emits at 4 spaces), a standalone/head
	// comment on its own line above a key is dropped (it attaches to the key node,
	// which the inline map does not capture), and a cross-boundary anchor/alias is
	// RESOLVED to its concrete value at parse time, not preserved (see
	// resolveExtraAliases). Fidelity is a guarantee about scalar
	// values, not about arbitrary source formatting.
	//
	// Not exposed over the GraphQL/JSON surface (json:"-"). yaml.v3 sorts inline-
	// map keys, so the render (and thus the etag) stays deterministic regardless of
	// Go map iteration order.
	Extra map[string]yaml.Node `yaml:"-" json:"-"`

	// rawLinks records the link ids exactly as the nib's FILE spells them,
	// independent of whatever the modeled fields above were later resolved to.
	// Never serialized. See RawLinks for what it is for and CaptureRawLinks for
	// who maintains it; nil means "no file spelling has been recorded", which
	// RawLinks answers from the live fields instead.
	rawLinks *LinkSpelling
}

// LinkSpelling carries a nib's three link fields as one value — the ids as some
// particular source spells them, which is not necessarily how the nib holds them.
type LinkSpelling struct {
	Parent    string
	BlockedBy []string
	Blocking  []string
}

// RawLinks returns the link ids as the nib's FILE spells them.
//
// A store may resolve a short-form link id (`parent: par`) to its full form
// (`nibs-par`) in memory while leaving the file alone, and what such an id
// resolves to is a property of the whole id set, so it has to be recomputed
// every time that set changes. Recomputing it from the ALREADY-RESOLVED value
// reads the previous resolution's own output: `nibs-par` resolves to itself, so
// the answer freezes at whatever the first resolution decided and can never
// follow the file back. Resolving from this spelling instead makes each pass a
// pure function of the file, hence idempotent and reversible.
//
// A nib that has never been read from or written to a file has no recorded
// spelling; it answers with its live fields, which is exactly what a
// caller resolving "the spelling of record" wants for a nib that has none.
//
// The returned slices alias the recorded spelling — read them, do not mutate them.
func (b *Nib) RawLinks() LinkSpelling {
	if b.rawLinks == nil {
		return LinkSpelling{Parent: b.Parent, BlockedBy: b.BlockedBy, Blocking: b.Blocking}
	}
	return *b.rawLinks
}

// CaptureRawLinks records the nib's CURRENT link ids as the spelling now on
// disk. Callers are the two places a nib and its file are known to agree: Parse
// (having just read those bytes) and the store's save path (having just written
// them).
//
// The obligation is per-WRITE, not per-read: a nib whose link is changed and
// persisted without being re-read would otherwise keep answering RawLinks with
// its pre-write spelling, and the next re-resolution would revert the change in
// memory. Slices are copied so a later in-place edit of the nib's own lists
// cannot reach back into the recorded spelling.
func (b *Nib) CaptureRawLinks() {
	b.rawLinks = &LinkSpelling{
		Parent:    b.Parent,
		BlockedBy: slices.Clone(b.BlockedBy),
		Blocking:  slices.Clone(b.Blocking),
	}
}

// yamlFrontMatterFormats parses nib front matter with yaml.v3 (the same YAML
// implementation Render marshals with), NOT the frontmatter library's default
// yaml.v2. Unifying the parse and render YAML versions — combined with capturing
// unknown keys as raw yaml.Node values (see frontMatter.Extra) — makes the
// unknown-key passthrough a true parse->render fixed point: no yaml.v2->v3
// scalar re-inference can coerce a bool-like or signed-zero value.
// Only the YAML formats are registered (nibs are always YAML front matter with
// `---`/`---yaml` fences); TOML/JSON front matter is not a nib format.
//
// The registered unmarshal is boundedYAMLUnmarshal (NOT plain yaml.Unmarshal):
// it caps the raw front-matter block by byte size and key count before the
// quadratic struct decode, closing the yaml.v3 O(N²)-in-key-count DoS.
var yamlFrontMatterFormats = []*frontmatter.Format{
	frontmatter.NewFormat("---", "---", boundedYAMLUnmarshal),
	frontmatter.NewFormat("---yaml", "---", boundedYAMLUnmarshal),
}

// Front-matter decode bounds. yaml.v3's decode of a mapping into a Go
// map/struct-with-inline-catch-all is O(N²) in that mapping's key count (~2.7s
// at 40k keys, ~1 MB), and that decode runs inside frontmatter.Parse BEFORE the
// alias budget or any other guard — so a crafted many-key nib would hang
// Core.Load under c.mu and re-hang every if-match Update's computeStoredETag.
// These bounds cap the attack surface before the decode: a real nib has a
// handful of keys and well under ~2 KB of front matter, so both ceilings sit far
// above anything legitimate (128x / 66x headroom) while capping the quadratic
// cost to a couple of milliseconds. Exceeding either returns a normal parse
// error, so loadFromDisk log-and-skips the file instead of blocking on it.
const (
	// MaxFrontMatterBytes bounds the raw YAML front-matter block (the bytes
	// between the `---` fences, excluding the markdown body). 256 KiB.
	//
	// Exported (unlike MaxFrontMatterKeys' sibling below) because cmd/migrate's
	// streamed header scan derives its own read budget from this one, rather
	// than duplicating the number and drifting.
	//
	// The two are NOT the same measurement, and the scan's comment carries the
	// consequence: this bounds the block between the fences, while the scan
	// bounds bytes read from the file, fences included. A block just under this
	// cap therefore parses here while the scan cannot see its closing fence. The
	// scan abstains (errors) there rather than guessing, so the direction of the
	// disagreement is safe — but it is a disagreement, not the identity the two
	// constants look like.
	MaxFrontMatterBytes = 256 * 1024
	// maxFrontMatterKeys bounds the total number of mapping keys in the
	// front-matter block. The top-level key count is the direct O(N²) driver
	// (the inline Extra map + modeled fields); counting recursively also caps any
	// mapping-heavy nested value. A real nib has fewer than ~15 keys.
	maxFrontMatterKeys = 1000
)

// boundedYAMLUnmarshal is the frontmatter UnmarshalFunc registered for nib front
// matter. It enforces MaxFrontMatterBytes / maxFrontMatterKeys BEFORE delegating
// to the real yaml.Unmarshal, so a crafted many-key block is rejected with a fast
// normal error rather than paying yaml.v3's O(N²) map decode.
//
// The key-count check first decodes the block into a single yaml.Node — which is
// LINEAR in the input, unlike the struct decode — counts its mapping keys, and
// rejects before the quadratic decode. Node decode does not expand aliases, so a
// billion-laughs graph stays compact here (the alias fan-out is bounded later by
// resolveExtraAliases). If the node decode itself fails (malformed YAML), we fall
// through to the real yaml.Unmarshal so it produces the canonical parse error
// (duplicate key, type mismatch, syntax) unchanged.
func boundedYAMLUnmarshal(data []byte, v any) error {
	if len(data) > MaxFrontMatterBytes {
		return fmt.Errorf("front matter is %d bytes, exceeding the %d-byte limit", len(data), MaxFrontMatterBytes)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err == nil {
		if n := countMappingKeys(&root); n > maxFrontMatterKeys {
			return fmt.Errorf("front matter has %d keys, exceeding the %d-key limit", n, maxFrontMatterKeys)
		}
	}
	return yaml.Unmarshal(data, v)
}

// countMappingKeys returns the total number of key/value pairs across every
// mapping node in the tree rooted at n (computed from a linear yaml.Node decode).
// yaml.v3 stores a mapping's children as a flat [k0,v0,k1,v1,...] slice, so a
// mapping contributes len(Content)/2 keys. See maxFrontMatterKeys.
//
// It counts LITERAL mapping keys only and does NOT expand YAML merge keys (`<<`):
// a `<<` merge counts as a single key here and its referenced mapping is not
// pulled in (the yaml.Node decode this runs on never expands aliases/merges). So
// a merge-amplified document undercounts against maxFrontMatterKeys. That is
// safe: yaml.v3 implements `<<` as alias traversal, so the same built-in
// alias-ratio guard that structurally bounds billion-laughs during the real
// struct decode also bounds merge amplification — it is not the key cap that
// bounds it here. See TestParseRejectsMergeKeyExpansion.
func countMappingKeys(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	count := 0
	if n.Kind == yaml.MappingNode {
		count += len(n.Content) / 2
	}
	for _, child := range n.Content {
		count += countMappingKeys(child)
	}
	return count
}

// frontMatter is the subset of Nib parsed from YAML front matter (via yaml.v3;
// see yamlFrontMatterFormats).
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
	// named field above lands here (via yaml.v3, see yamlFrontMatterFormats). Each
	// value is captured as a raw yaml.Node so unknown keys survive parsing and are
	// re-emitted by Render with their original scalar text/style/tag intact — no
	// type re-inference. See Nib.Extra.
	Extra map[string]yaml.Node `yaml:",inline"`
}

// DefaultType and DefaultPriority are the single source of truth for the
// PRESENTATION defaults applied when a nib file omits the corresponding front
// matter key. They are consumed via EffectiveType/EffectivePriority.
//
// The stored Nib keeps Type/Priority EMPTY when the file omits them: Render
// carries `omitempty` on both, so the canonical render — and thus the etag —
// stays a faithful witness of the on-disk bytes. If loadNib synthesized these
// in memory, a bare-parse of the same file would render no
// such key while the in-memory ETag() would render the default, diverging with
// no on-disk change and false-conflicting an if-match Update. The
// defaults are therefore applied only at the consumption boundary (GraphQL
// field resolvers, sort/filter, TUI/CLI display, the JSON projection).
//
// They live in the nib package (not config) to avoid a nib->config layering
// edge; the values intentionally match
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

// Parse reads a nib from a reader (markdown with YAML front matter).
//
// A nib file OPENS with a front-matter fence (`---` or `---yaml` as its first
// line) — the same first-line rule the migration header scan applies
// (cmd/migrate's readFrontMatterHeader), so every consumer of this parse
// (Core.Load, the watcher, computeStoredETag, the scans) shares ONE
// definition of "not a nib file". Fence-less content used to parse into an
// empty v0 nib: a README in the store became a phantom row every query
// surfaced, writers could rewrite the document into a nib render, and the
// migration scan called the same file "not a nib file" while check reported
// all clear. Refusing here retires that class at the root; loaders degrade
// per file (log-and-skip into diagnostics), so one document never fails a
// store.
//
// A line IS a fence iff strings.TrimSpace(line) equals the fence token —
// whitespace padding is tolerated, `----` is not a fence. The TrimSpace rule
// is pinned to the frontmatter library this delegates to: its line handling
// is a fixed bytes.TrimSpace (not overridable), so its closing-fence compare
// accepts padded fences no matter what we do here, and TrimSpace-equivalence
// is the only rule all four fence comparisons (this pre-check, the scan's
// opening and closing compares, the library's closing compare) can share.
// Tightening any one of them re-opens the scan/parse divergence where the
// two classify the same file differently.
func Parse(r io.Reader) (*Nib, error) {
	br := bufio.NewReader(r)
	firstLine, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}
	if fence := strings.TrimSpace(firstLine); fence != "---" && fence != "---yaml" {
		// Also covers a BOM or leading blank line before a fence: TrimSpace
		// does not trim a BOM (U+FEFF is not Unicode whitespace), and a blank
		// first line trims to "" — the header scan's line compare refuses
		// both the same way, so neither counts as a nib shape here.
		return nil, fmt.Errorf("no front matter — not a nib file")
	}

	var fm frontMatter
	body, err := frontmatter.MustParse(io.MultiReader(strings.NewReader(firstLine), br), &fm, yamlFrontMatterFormats...)
	if err != nil {
		if errors.Is(err, frontmatter.ErrNotFound) {
			// The first line IS a fence (checked above), so "not found" can
			// only mean the closing fence never came: a torn or half-written
			// file, not a nib whose body is the whole document.
			return nil, fmt.Errorf("front matter never closed (missing the closing --- fence)")
		}
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	// Trim trailing newline from body (POSIX files end with newline, but it's not part of content)
	bodyStr := strings.TrimSuffix(string(body), "\n")

	if err := ValidateOrderKey(fm.Order); err != nil {
		return nil, fmt.Errorf("invalid order key: %w", err)
	}

	// Note on the legacy `priority: deferred` value: "deferred" was removed as a
	// priority (it is now a status), but Parse does NOT rewrite it — a file's
	// content loads exactly as written. Rewriting legacy values is `nibs
	// migrate`'s job (the priority-deferred step maps it to "low" ON DISK), and
	// the CLI refuses to run other commands while that migration is pending.

	// Resolve any YAML anchors/aliases captured in Extra to their concrete value.
	// A cross-boundary anchor/alias — an anchor on a MODELED field (which decodes
	// to a plain Go value, dropping the anchor) plus an alias in an unmodeled Extra
	// key — would otherwise survive as a raw AliasNode and marshal to a DANGLING
	// alias on Render, producing invalid YAML that permanently corrupts the file.
	// Resolving at parse yields concrete values while keeping scalar fidelity for
	// non-alias values.
	//
	// resolveExtraAliases fails closed on adversarial anchors: yaml.v3 does NOT
	// expand aliases when decoding into a yaml.Node, so its built-in billion-laughs
	// budget never runs and Extra can hold a compact graph our expansion would blow
	// up. A self-referential (cyclic) anchor, or a fan-out exceeding
	// maxExtraAliasNodes, therefore returns an error here rather than recursing to a
	// stack overflow or exhausting memory. A crafted file thus fails Parse and is
	// skipped by the loader (Core.Load log-and-continue) instead of crashing the
	// process. The budget is shared across all Extra values of the nib.
	// Iterate Extra keys in sorted order so that, when multiple values fail
	// independently, the key named in the returned error (and thus the
	// loadFromDisk skip warning) is DETERMINISTIC — Go map iteration order would
	// otherwise pick an arbitrary offender across runs. The shared alias budget is
	// consumed in this same order, so a budget-exhaustion error is deterministic too.
	extraKeys := make([]string, 0, len(fm.Extra))
	for k := range fm.Extra {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	aliasBudget := maxExtraAliasNodes
	for _, k := range extraKeys {
		v := fm.Extra[k]
		resolved, err := resolveExtraAliases(&v, &aliasBudget)
		if err != nil {
			return nil, fmt.Errorf("parsing front matter: unknown key %q: %w", k, err)
		}
		if resolved != nil {
			fm.Extra[k] = *resolved
		}
	}

	b := &Nib{
		Version:   fm.Version,
		Title:     fm.Title,
		Status:    fm.Status,
		Type:      fm.Type,
		Priority:  fm.Priority,
		Estimate:  fm.Estimate,
		Tags:      fm.Tags,
		CreatedAt: fm.CreatedAt,
		UpdatedAt: fm.UpdatedAt,
		Body:      bodyStr,
		Parent:    fm.Parent,
		Blocking:  fm.Blocking,
		BlockedBy: fm.BlockedBy,
		Documents: fm.Documents,
		Order:     fm.Order,
		Extra:     fm.Extra,
	}
	// The link fields as they stand right now ARE the file's spelling. Record it
	// before anything downstream resolves them to their full form (see RawLinks).
	b.CaptureRawLinks()
	return b, nil
}

// maxExtraAliasNodes bounds how many nodes anchor/alias resolution may
// materialize across all Extra values of a single nib. yaml.v3 preserves aliases
// unexpanded when decoding into yaml.Node, so its own expansion budget never
// applies here; this is the equivalent guard. A legitimate unknown-key value is a
// handful of nodes, so this ceiling sits far above any real nib while capping
// exponential (billion-laughs) fan-out at ~tens of MB.
const maxExtraAliasNodes = 100_000

// resolveExtraAliases returns a deep copy of node with all YAML anchor/alias
// state stripped: an alias node is replaced by a (recursively resolved) deep copy
// of its target, and Anchor is cleared on every node. It is applied to each
// captured Extra value at parse time (see Parse) so no cross-key anchor/alias
// dependency can survive to Render, where a dangling alias would marshal to
// invalid YAML and permanently corrupt the file. Non-alias scalar
// values are otherwise preserved verbatim (Kind, Value, Style, Tag), keeping the
// unknown-key scalar fidelity the passthrough guarantees.
//
// It fails closed on adversarial input: a self-referential anchor (a cyclic node
// graph) returns a "cyclic" error, and a fan-out that would materialize more than
// the remaining budget returns a limit error — instead of recursing to a stack
// overflow or exhausting memory. budget is decremented per copied node
// and is shared across a nib's Extra values by the caller. Returns (nil, nil) only
// for a nil input.
func resolveExtraAliases(node *yaml.Node, budget *int) (*yaml.Node, error) {
	return resolveExtraAliasesGuarded(node, make(map[*yaml.Node]bool), budget)
}

// resolveExtraAliasesGuarded carries the per-value cycle set (alias targets on the
// current resolution path) and the shared node budget.
func resolveExtraAliasesGuarded(node *yaml.Node, active map[*yaml.Node]bool, budget *int) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind == yaml.AliasNode {
		// Replace the alias with a resolved deep copy of its anchor target. A
		// well-formed parse always sets Alias; fail CLOSED if it is nil (a
		// malformed/unreachable state) rather than silently returning no value,
		// consistent with the rest of this guard's fail-closed posture.
		if node.Alias == nil {
			return nil, fmt.Errorf("alias node has no target (malformed anchor/alias)")
		}
		// A target already on the active resolution path means the alias points
		// back into its own expansion: a cycle. Without this check the recursion
		// never terminates (stack overflow).
		if active[node.Alias] {
			return nil, fmt.Errorf("cyclic anchor/alias reference")
		}
		active[node.Alias] = true
		resolved, err := resolveExtraAliasesGuarded(node.Alias, active, budget)
		delete(active, node.Alias)
		return resolved, err
	}
	// Charge each materialized node against the shared budget before copying it.
	// This is what bounds exponential fan-out: an anchor referenced N times per
	// level across M levels expands to N^M nodes.
	if *budget <= 0 {
		return nil, fmt.Errorf("anchor/alias expansion exceeds %d-node limit", maxExtraAliasNodes)
	}
	*budget--
	resolved := *node // copy scalar fields (Kind, Value, Style, Tag, ...)
	resolved.Anchor = ""
	if len(node.Content) > 0 {
		resolved.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			rc, err := resolveExtraAliasesGuarded(child, active, budget)
			if err != nil {
				return nil, err
			}
			resolved.Content[i] = rc
		}
	}
	return &resolved, nil
}

// renderFrontMatter is used for YAML output with yaml.v3 (supports custom marshalers).
//
// Blocking carries omitempty: for a v1+ nib it is always nil (blocking is
// single-side, computed at query time from other nibs' BlockedBy), so it is
// absent from the render — the normal case is unchanged. It is emitted ONLY for
// a legacy v0 nib parsed straight from disk (before `nibs migrate` clears it),
// so the canonical render — and thus the etag — stays a faithful witness of a
// v0 file's on-disk `blocking:` content rather than silently dropping it.
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
	Version   int                  `yaml:"version"`
	Title     string               `yaml:"title"`
	Status    string               `yaml:"status"`
	Type      string               `yaml:"type,omitempty"`
	Priority  string               `yaml:"priority,omitempty"`
	Estimate  string               `yaml:"estimate,omitempty"`
	Tags      []string             `yaml:"tags,omitempty"`
	CreatedAt *time.Time           `yaml:"created_at,omitempty"`
	UpdatedAt *time.Time           `yaml:"updated_at,omitempty"`
	Parent    string               `yaml:"parent,omitempty"`
	Blocking  []string             `yaml:"blocking,omitempty"`
	BlockedBy []string             `yaml:"blocked_by,omitempty"`
	Documents []string             `yaml:"documents,omitempty"`
	Order     string               `yaml:"order,omitempty"`
	Extra     map[string]yaml.Node `yaml:",inline"`
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
func (b *Nib) renderExtra() map[string]yaml.Node {
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
			filtered = make(map[string]yaml.Node, len(b.Extra))
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

	// rawLinks is the transient, parse-set field, and it must SURVIVE the
	// clone. Re-resolution applies its result to a Clone of the stored nib, so
	// a shadow dropped here would leave the very next pass reading the previous
	// pass's output — the divergence RawLinks exists to close. Deep-copied for
	// the same reason as the exported lists below.
	if b.rawLinks != nil {
		raw := *b.rawLinks
		raw.BlockedBy = slices.Clone(b.rawLinks.BlockedBy)
		raw.Blocking = slices.Clone(b.rawLinks.Blocking)
		clone.rawLinks = &raw
	}

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
	// the same underlying map. Note this is a top-level copy of the map; each
	// yaml.Node value is copied by value, but a node's internal Content slice
	// (child nodes of a map/sequence value) is still shared, which is acceptable
	// because Extra values are treated as opaque, immutable passthrough content.
	if b.Extra != nil {
		clone.Extra = make(map[string]yaml.Node, len(b.Extra))
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
// field resolvers. We marshal a value COPY with the effective values applied, leaving the
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
