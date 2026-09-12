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
// nibs are stamped with it; a file with a higher version is refused. Migration
// steps write their own fixed output version, never this constant.
const CurrentVersion = 2

// Nib represents an issue stored as a markdown file with front matter.
type Nib struct {
	// ID is derived from the filename.
	ID string `yaml:"-" json:"id"`
	// Slug is the optional human-readable part of the filename.
	Slug string `yaml:"-" json:"slug,omitempty"`
	// Path is relative to the store directory and starts with data/ or archive/,
	// e.g. "data/x9z2--login.md".
	Path string `yaml:"-" json:"path"`

	// Version is the file format version. Absent = 0 (legacy).
	Version int `yaml:"version" json:"version"`

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

	// Parent is the optional parent nib ID (epic, feature, or bug).
	Parent string `yaml:"parent,omitempty" json:"parent,omitempty"`

	// Blocking is legacy v0 data; v1+ derives blocking from other nibs' BlockedBy.
	// Render still emits it (omitempty), so a v0 file's etag covers it.
	Blocking []string `yaml:"blocking,omitempty" json:"-"`

	// BlockedBy is a list of nib IDs that are blocking this nib.
	BlockedBy []string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`

	// Documents is a list of repo-root-relative paths to linked documents.
	Documents []string `yaml:"documents,omitempty" json:"documents,omitempty"`

	// Order is a fractional index string for sorting among siblings.
	Order string `yaml:"order,omitempty" json:"order,omitempty"`

	// Milestone is the optional id of the milestone whose queue this nib is in: a
	// link like Parent, and the only milestone-membership axis.
	Milestone string `yaml:"milestone,omitempty" json:"milestone,omitempty"`

	// MilestoneOrder is the nib's fractional position in its milestone queue, set
	// by the ordering engine (graph.Orderer) and the v1→v2 migration.
	MilestoneOrder string `yaml:"milestone_order,omitempty" json:"milestone_order,omitempty"`

	// Area is the optional area path the nib belongs to — a plain path-valued
	// string (e.g. "web/ui"), never a nib id or link.
	Area string `yaml:"area,omitempty" json:"area,omitempty"`

	// Extra holds front-matter keys no modeled field claims. Parse keeps each value
	// as a raw yaml.v3 node and Render re-emits it, so unknown keys survive a round
	// trip and an edit to one changes the etag. Scalar text, style and tag
	// round-trip verbatim; formatting does not: block scalars re-indent, head
	// comments are dropped, and anchors/aliases are resolved (resolveExtraAliases).
	Extra map[string]yaml.Node `yaml:"-" json:"-"`

	// rawLinks is the file's spelling of the link ids, nil until recorded; see
	// RawLinks.
	rawLinks *LinkSpelling
}

// LinkSpelling carries a nib's four link fields as some source spells them.
// Area is a path, not a link.
type LinkSpelling struct {
	Parent    string
	Milestone string
	BlockedBy []string
	Blocking  []string
}

// RawLinks returns the link ids as the nib's file spells them, or the live fields
// when no spelling is recorded. Re-resolve links from this, not from resolved
// fields, which resolve to themselves. Do not mutate the returned slices.
func (b *Nib) RawLinks() LinkSpelling {
	if b.rawLinks == nil {
		return LinkSpelling{Parent: b.Parent, Milestone: b.Milestone, BlockedBy: b.BlockedBy, Blocking: b.Blocking}
	}
	return *b.rawLinks
}

// CaptureRawLinks records the current link ids as the file's spelling. Call it
// whenever the nib and its file agree: after a parse and after every write.
func (b *Nib) CaptureRawLinks() {
	b.rawLinks = &LinkSpelling{
		Parent:    b.Parent,
		Milestone: b.Milestone,
		BlockedBy: slices.Clone(b.BlockedBy),
		Blocking:  slices.Clone(b.Blocking),
	}
}

// yamlFrontMatterFormats parses front matter with yaml.v3, the library Render
// marshals with, so unknown-key scalars are not re-inferred between parse and
// render. Only `---` and `---yaml` fences are nib formats.
var yamlFrontMatterFormats = []*frontmatter.Format{
	frontmatter.NewFormat("---", "---", boundedYAMLUnmarshal),
	frontmatter.NewFormat("---yaml", "---", boundedYAMLUnmarshal),
}

// Front-matter decode bounds, checked before yaml.v3's struct decode, which is
// O(N²) in a mapping's key count. Exceeding one is an ordinary parse error.
const (
	// MaxFrontMatterBytes bounds the block between the fences. cmd/migrate's
	// header scan reuses it but counts the fences too, so a block just under the
	// cap parses here and is refused there.
	MaxFrontMatterBytes = 256 * 1024
	// maxFrontMatterKeys bounds mapping keys across the whole block, nested
	// mappings included.
	maxFrontMatterKeys = 1000
)

// boundedYAMLUnmarshal refuses a block over MaxFrontMatterBytes or
// maxFrontMatterKeys, then decodes into v. Keys are counted on a yaml.Node
// decode; when that decode fails, the struct decode reports the error.
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

// countMappingKeys counts key/value pairs across every mapping under n. Merge
// keys (`<<`) are not expanded, so a merge-amplified block undercounts; the
// struct decode's alias guard bounds that case.
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

// frontMatter is the parse projection of a nib's front matter. Keep its yaml keys
// identical to renderFrontMatter's: a key modeled on one side only is parsed into
// Extra and collides with the modeled field on render.
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

	Milestone      string `yaml:"milestone,omitempty"`
	MilestoneOrder string `yaml:"milestone_order,omitempty"`
	Area           string `yaml:"area,omitempty"`

	// Extra catches every unmodeled key; see Nib.Extra.
	Extra map[string]yaml.Node `yaml:",inline"`
}

// DefaultType and DefaultPriority apply when a file omits type or priority. Nib
// keeps the field empty so Render, and the etag, match the file; apply a default
// only through EffectiveType/EffectivePriority. A config test pins both values to
// config's defaults.
const (
	DefaultType     = "task"
	DefaultPriority = "normal"
)

// EffectiveType returns b.Type, or DefaultType when it is empty.
func (b *Nib) EffectiveType() string {
	if b.Type == "" {
		return DefaultType
	}
	return b.Type
}

// EffectivePriority returns b.Priority, or DefaultPriority when it is empty.
func (b *Nib) EffectivePriority() string {
	if b.Priority == "" {
		return DefaultPriority
	}
	return b.Priority
}

// Parse reads a nib from markdown with YAML front matter. A file whose first line
// is not a `---` or `---yaml` fence, compared after TrimSpace, is not a nib file.
// Keep that rule in step with cmd/migrate's readFrontMatterHeader; the
// frontmatter library also trims before comparing the closing fence.
func Parse(r io.Reader) (*Nib, error) {
	br := bufio.NewReader(r)
	firstLine, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}
	if fence := strings.TrimSpace(firstLine); fence != "---" && fence != "---yaml" {
		// A BOM or a blank first line is not a fence either.
		return nil, fmt.Errorf("no front matter — not a nib file")
	}

	var fm frontMatter
	body, err := frontmatter.MustParse(io.MultiReader(strings.NewReader(firstLine), br), &fm, yamlFrontMatterFormats...)
	if err != nil {
		if errors.Is(err, frontmatter.ErrNotFound) {
			// The opening fence was seen, so the closing one is missing.
			return nil, fmt.Errorf("front matter never closed (missing the closing --- fence)")
		}
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	// Keep the body verbatim so Parse inverts Render, except the lone "\n" Render
	// writes for a body-less nib, which reads back as no body.
	bodyStr := string(body)
	if bodyStr == "\n" {
		bodyStr = ""
	}

	if err := ValidateOrderKey(fm.Order); err != nil {
		return nil, fmt.Errorf("invalid order key: %w", err)
	}
	if err := ValidateOrderKey(fm.MilestoneOrder); err != nil {
		return nil, fmt.Errorf("invalid milestone_order key: %w", err)
	}

	// A legacy `priority: deferred` loads as written; `nibs migrate` rewrites it.

	// Resolve anchors/aliases in Extra: an alias whose anchor sits on a modeled
	// field would render as a dangling alias. Keys go in sorted order so the key
	// named in an error, and the shared budget's consumption, are deterministic.
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
		Version:        fm.Version,
		Title:          fm.Title,
		Status:         fm.Status,
		Type:           fm.Type,
		Priority:       fm.Priority,
		Estimate:       fm.Estimate,
		Tags:           fm.Tags,
		CreatedAt:      fm.CreatedAt,
		UpdatedAt:      fm.UpdatedAt,
		Body:           bodyStr,
		Parent:         fm.Parent,
		Blocking:       fm.Blocking,
		BlockedBy:      fm.BlockedBy,
		Documents:      fm.Documents,
		Order:          fm.Order,
		Milestone:      fm.Milestone,
		MilestoneOrder: fm.MilestoneOrder,
		Area:           fm.Area,
		Extra:          fm.Extra,
	}
	// The fields as parsed are the file's spelling.
	b.CaptureRawLinks()
	return b, nil
}

// maxExtraAliasNodes bounds the nodes resolveExtraAliases may create across one
// nib's Extra values.
const maxExtraAliasNodes = 100_000

// resolveExtraAliases returns a deep copy of node with each alias replaced by a
// copy of its target and every anchor cleared. It errors on a cyclic alias and
// when budget, decremented per copied node, runs out. Returns (nil, nil) for a nil
// node.
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
		// Fail closed on an alias with no target.
		if node.Alias == nil {
			return nil, fmt.Errorf("alias node has no target (malformed anchor/alias)")
		}
		// A target already on the resolution path is a cycle.
		if active[node.Alias] {
			return nil, fmt.Errorf("cyclic anchor/alias reference")
		}
		active[node.Alias] = true
		resolved, err := resolveExtraAliasesGuarded(node.Alias, active, budget)
		delete(active, node.Alias)
		return resolved, err
	}
	// Charge every copied node against the budget; this bounds fan-out.
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

// renderFrontMatter is the render projection; keep its yaml keys identical to
// frontMatter's. yaml.v3 sorts the inline Extra keys, and panics on one that
// collides with a modeled field, so Render drops colliding keys first.
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
	Blocking  []string   `yaml:"blocking,omitempty"`
	BlockedBy []string   `yaml:"blocked_by,omitempty"`
	Documents []string   `yaml:"documents,omitempty"`
	Order     string     `yaml:"order,omitempty"`

	Milestone      string `yaml:"milestone,omitempty"`
	MilestoneOrder string `yaml:"milestone_order,omitempty"`
	Area           string `yaml:"area,omitempty"`

	Extra map[string]yaml.Node `yaml:",inline"`
}

// modeledRenderTags holds renderFrontMatter's named yaml keys, derived by
// reflection.
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

// normalizedModeledTags maps each modeled key's normalizeKeySpelling form to the
// key.
var normalizedModeledTags = buildNormalizedModeledTags()

func buildNormalizedModeledTags() map[string]string {
	m := make(map[string]string, len(modeledRenderTags))
	for tag := range modeledRenderTags {
		m[normalizeKeySpelling(tag)] = tag
	}
	return m
}

// normalizeKeySpelling lower-cases key and removes every '-' and '_'.
func normalizeKeySpelling(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "-", "")
	return strings.ReplaceAll(key, "_", "")
}

// ModeledKeyResembling returns the modeled key that key is a near-miss spelling
// of: equal after normalizeKeySpelling, different as written (`Milestone`,
// `milestone-order`). Parse puts such keys in Extra; `nibs check` names them.
func ModeledKeyResembling(key string) (string, bool) {
	modeled, ok := normalizedModeledTags[normalizeKeySpelling(key)]
	if !ok || modeled == key {
		return "", false
	}
	return modeled, true
}

// renderExtra returns b.Extra without keys that collide with a modeled render
// field, copying the map only when one does.
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

// Render serializes the nib to markdown with YAML front matter. A non-empty body
// gets a leading blank line and a trailing newline unless it has them, so a
// re-render of a parsed render is identical; computeStoredETag depends on that.
func (b *Nib) Render() ([]byte, error) {
	fm := renderFrontMatter{
		Version:        b.Version,
		Title:          b.Title,
		Status:         b.Status,
		Type:           b.Type,
		Priority:       b.Priority,
		Estimate:       b.Estimate,
		Tags:           b.Tags,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
		Parent:         b.Parent,
		Blocking:       b.Blocking,
		BlockedBy:      b.BlockedBy,
		Documents:      b.Documents,
		Order:          b.Order,
		Milestone:      b.Milestone,
		MilestoneOrder: b.MilestoneOrder,
		Area:           b.Area,
		Extra:          b.renderExtra(),
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
		if !strings.HasPrefix(b.Body, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString(b.Body)
		if !strings.HasSuffix(b.Body, "\n") {
			buf.WriteString("\n")
		}
	} else {
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// Clone returns a deep copy of the nib, except that Extra's node values still
// share their Content slices.
func (b *Nib) Clone() *Nib {
	clone := *b // shallow copy of all value fields

	// Keep rawLinks: re-resolution works on a Clone and reads it.
	if b.rawLinks != nil {
		raw := *b.rawLinks
		raw.BlockedBy = slices.Clone(b.rawLinks.BlockedBy)
		raw.Blocking = slices.Clone(b.rawLinks.Blocking)
		clone.rawLinks = &raw
	}

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

	if b.Extra != nil {
		clone.Extra = make(map[string]yaml.Node, len(b.Extra))
		for k, v := range b.Extra {
			clone.Extra[k] = v
		}
	}

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

// ETag returns the FNV-1a 64-bit hash of Render as 16 hex digits, or all zeros
// when Render fails.
func (b *Nib) ETag() string {
	content, err := b.Render()
	if err != nil {
		return "0000000000000000"
	}
	h := fnv.New64a()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// MarshalJSON adds the etag and reports Type and Priority with their defaults
// applied, on a copy.
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
