// Package nibcore provides a thread-safe in-memory store for nibs with filesystem persistence
// and optional file watching for long-running processes.
package nibcore

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/search"
)

const NibsDir = ".nibs"
const ArchiveDir = "archive"

// ErrNotFound is an alias for nib.ErrNotFound for backwards compatibility.
var ErrNotFound = nib.ErrNotFound

// ETagMismatchError is returned when an ETag validation fails.
// This allows callers to distinguish concurrency conflicts from other errors.
type ETagMismatchError struct {
	Provided string
	Current  string
}

func (e *ETagMismatchError) Error() string {
	return fmt.Sprintf("etag mismatch: provided %s, current is %s", e.Provided, e.Current)
}

// ETagRequiredError is returned when require_if_match is enabled and no ETag is provided.
type ETagRequiredError struct{}

func (e *ETagRequiredError) Error() string {
	return "if-match etag is required (set require_if_match: false in config to disable)"
}

// OnDiskUnparseableError is returned by an if-match Update (and by CurrentETag,
// used in the bulk-reorder pre-validation) when the CURRENT on-disk state of a
// nib cannot be certified: the file EXISTS but is unparseable (torn/partial
// write, git merge-conflict markers, hand-edit YAML typo) or unreadable
// (permission denied, transient/torn I/O — a non-IsNotExist read error).
//
// Unlike ETagMismatchError it deliberately carries NO reusable etag token. A
// client following the textbook "409 → retry with the server's Current etag"
// reconcile pattern therefore has nothing to echo back that could satisfy the
// guard: every recomputation of an uncertifiable file yields this same
// non-reconcilable error, so the corrupt/unreadable file can never be clobbered
// by a blind retry. It must be repaired manually (or re-read once it is
// parseable/readable again). This is a distinct error class from a genuine
// concurrency conflict (ETagMismatchError), which IS reconcilable.
type OnDiskUnparseableError struct {
	ID     string // nib id whose on-disk file could not be certified
	Path   string // repo-relative path of the uncertifiable file
	Reason string // "unparseable" or "unreadable"
	Err    error  // underlying parse/read error
}

func (e *OnDiskUnparseableError) Error() string {
	return fmt.Sprintf(
		"on-disk nib file %s is %s and its current state cannot be certified for an if-match update; repair the file (this conflict is not resolvable by retrying with a server etag): %v",
		e.Path, e.Reason, e.Err,
	)
}

func (e *OnDiskUnparseableError) Unwrap() error { return e.Err }

// Core provides thread-safe in-memory storage for nibs with filesystem persistence.
type Core struct {
	root   string         // absolute path to .nibs directory
	config *config.Config // project configuration

	// In-memory state
	mu   sync.RWMutex
	nibs map[string]*nib.Nib // ID -> Nib

	// Reverse-mention index: maintained alongside c.nibs so FindMentionedBy /
	// FindMentions avoid O(N × body) re-parsing on every call. Guarded by c.mu
	// (writers under Lock, readers under RLock) — mentionIndex itself is not
	// internally synchronised.
	mentionIdx *mentionIndex

	// Search index (optional, lazy-initialized)
	searchIndex SearchIndex

	// File watching (optional)
	watching bool
	done     chan struct{}
	onChange func() // callback when nibs change (legacy API)

	// Event subscribers (for channel-based API)
	subscribers map[uint64]*subscription
	subMu       sync.RWMutex
	nextSubID   uint64

	// Warning logger for non-fatal errors (defaults to stderr)
	warnWriter io.Writer
}

// New creates a new Core with the given root path and configuration.
func New(root string, cfg *config.Config) *Core {
	return &Core{
		root:        root,
		config:      cfg,
		nibs:        make(map[string]*nib.Nib),
		mentionIdx:  newMentionIndex(),
		subscribers: make(map[uint64]*subscription),
		warnWriter:  os.Stderr,
	}
}

// SetWarnWriter sets the writer for warning messages.
// Pass nil to disable warnings.
func (c *Core) SetWarnWriter(w io.Writer) {
	c.warnWriter = w
}

// SetSearchIndex sets a custom search index implementation.
// When set, Core uses this instead of lazily initializing a Bleve index.
// It controls only the full-text leg of Search: Core unions direct ID
// matches (computed from the in-memory nib map) on top of index results.
// It must be called before Load or any concurrent operations (not safe for concurrent use).
func (c *Core) SetSearchIndex(idx SearchIndex) {
	c.searchIndex = idx
}

// logWarn logs a warning message if a warn writer is configured.
func (c *Core) logWarn(format string, args ...any) {
	if c.warnWriter != nil {
		_, _ = fmt.Fprintf(c.warnWriter, "warning: "+format+"\n", args...)
	}
}

// Root returns the absolute path to the .nibs directory.
func (c *Core) Root() string {
	return c.root
}

// Config returns the configuration.
func (c *Core) Config() *config.Config {
	return c.config
}

// Load reads all nibs from disk into memory.
func (c *Core) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.loadFromDisk()
}

// loadFromDisk reads all nibs from disk (must be called with lock held).
// Loads all .md files from the root directory and any subdirectories.
func (c *Core) loadFromDisk() error {
	// Clear existing nibs
	c.nibs = make(map[string]*nib.Nib)

	// Count of nibs whose legacy `priority: deferred` was normalized to `low`
	// and persisted during this load, so we can log a single summary (restoring
	// the visibility the removed migrateDeferredPriority pass used to provide).
	var deferredMigrated int

	// IDs of files present on disk but skipped this load (unparseable/unreadable).
	// The ID is derived from the filename (which parses regardless of content), so
	// migrateV0ToV1 can tell "target's file was skipped this load" apart from
	// "target genuinely does not exist" and DEFER a v0 nib's migration rather than
	// erasing its `blocking:` edge to a skipped target (nibs-r3y1 review #2).
	skipped := make(map[string]bool)

	// Walk the entire .nibs directory tree, loading all .md files
	err := filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip non-.md files
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		b, migrated, loadErr := c.loadNibReconciledLocked(path)
		if loadErr != nil {
			// Log-and-skip a single unparseable/unreadable file rather than
			// aborting the whole walk: yaml.v3 hard-errors on a duplicate
			// front-matter key (where yaml.v2 took last-wins), so one pre-existing
			// malformed nib (bad merge, hand-edit, partial write) would otherwise
			// make every nibs command fail to load ANY nib. Degrade to one missing
			// nib instead of a dead store, matching the fsnotify watcher's per-file
			// "log and continue" posture. The file's bytes are left untouched
			// (skip = not loaded into memory; never delete/rewrite).
			c.logWarn("skipping unparseable nib file %s: %v", path, loadErr)
			if id, _ := nib.ParseFilename(filepath.Base(path)); id != "" {
				skipped[id] = true
			}
			return nil
		}
		if migrated {
			deferredMigrated++
		}

		c.nibs[b.ID] = b
		return nil
	})
	if err != nil {
		return err
	}

	if deferredMigrated > 0 {
		c.logWarn("migrated %d nib(s): priority 'deferred' -> 'low'", deferredMigrated)
	}

	// Migrate v0 nibs to v1 (single-side blocking). This runs after the walk so
	// every blocking target is already in c.nibs. v0+deferred nibs are converged
	// here rather than in loadNibReconciledLocked (which gates persistence on
	// Version >= 1 to avoid the lossy v0 render — see its doc comment).
	if err := c.migrateV0ToV1(skipped); err != nil {
		return fmt.Errorf("migration v0→v1: %w", err)
	}

	// Rebuild the reverse-mention index from the loaded bodies. Must run
	// after migration so the index sees the final body state.
	c.mentionIdx.Rebuild(c.nibs)

	// Re-populate search index if it was active (best-effort, don't fail load).
	// We upsert all current nibs rather than closing and recreating the index,
	// so that injected SearchIndex implementations are preserved across reloads.
	// Stale entries from externally-deleted nibs are harmless: Search() filters
	// results through the c.nibs map.
	if c.searchIndex != nil {
		allNibs := make([]*nib.Nib, 0, len(c.nibs))
		for _, b := range c.nibs {
			allNibs = append(allNibs, b)
		}
		if err := c.searchIndex.IndexNibs(allNibs); err != nil {
			c.logWarn("failed to re-populate search index after reload: %v", err)
		}
	}

	return nil
}

// loadNib reads and parses a single nib file.
func (c *Core) loadNib(path string) (*nib.Nib, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	b, err := nib.Parse(f)
	if err != nil {
		return nil, err
	}

	// Set metadata from path
	relPath, err := filepath.Rel(c.root, path)
	if err != nil {
		return nil, err
	}
	b.Path = filepath.ToSlash(relPath)

	// Extract ID and slug from filename
	filename := filepath.Base(path)
	b.ID, b.Slug = nib.ParseFilename(filename)

	// Type and Priority are DELIBERATELY not defaulted here. Synthesizing them
	// in memory (Type""→"task", Priority""→"normal") while computeStoredETag
	// bare-parses the file diverges the in-memory ETag() from the stored etag for
	// a file that omits the key, false-conflicting a valid if-match Update with no
	// on-disk change (nibs-7d3o). The stored Nib keeps them EMPTY so Render (which
	// carries omitempty on both) matches the on-disk bytes; the "task"/"normal"
	// presentation defaults are applied at the consumption boundary via
	// nib.EffectiveType()/EffectivePriority() (GraphQL field resolvers, sort,
	// filter, TUI/CLI display, the JSON projection).
	//
	// The empty-slice defaults below are kept: they satisfy GraphQL's non-null
	// list fields and are etag-safe (Render's omitempty treats a nil and an empty
	// slice identically, so neither changes the canonical render).
	if b.Tags == nil {
		b.Tags = []string{}
	}
	if b.BlockedBy == nil {
		b.BlockedBy = []string{}
	}
	if b.Documents == nil {
		b.Documents = []string{}
	}
	// created_at/updated_at fallbacks are also kept. Unlike Type/Priority these
	// cannot be expressed as a pure Effective* accessor — the mtime fallback needs
	// the file's stat, unavailable at the consumption boundary — so moving them
	// would balloon scope for no real-world gain: every app-written nib always
	// carries both timestamps (Create sets them, Render emits them), so the only
	// files this synthesis touches are hand-authored ones missing EITHER timestamp
	// (created_at is synthesized from updated_at or the file mtime below, then
	// updated_at is defaulted to created_at). Such a file retains a residual etag
	// divergence (in-memory carries the synthesized timestamp; the bare-parse stored
	// etag does not), and because every Get returns the same synthesized pointer the
	// divergence never clears — an if-match Update on such a file permanently
	// false-conflicts. This is pre-existing, orthogonal to the priority/type fix,
	// deliberately accepted (not fixed here), and not exercised by any real or
	// app-created nib.
	if b.CreatedAt == nil {
		if b.UpdatedAt != nil {
			b.CreatedAt = b.UpdatedAt
		} else {
			// Use file modification time as fallback
			info, statErr := os.Stat(path)
			if statErr == nil {
				modTime := info.ModTime().UTC().Truncate(time.Second)
				b.CreatedAt = &modTime
			}
		}
	}
	if b.UpdatedAt == nil {
		b.UpdatedAt = b.CreatedAt
	}

	return b, nil
}

// loadNibReconciledLocked loads and parses a single nib file (via loadNib) and,
// for the bulk load path only, persists any load-time normalization back to
// disk so the on-disk bytes converge with the in-memory value immediately. It
// returns whether such a migration was persisted so the caller can log an
// aggregate count. Only loadFromDisk funnels through here — startup Load, where
// the file watcher is inactive.
//
// The incremental watcher path (handleChanges) deliberately does NOT call this:
// it uses the read-only loadNib and reconciles in memory only. nib.Parse already
// normalizes `deferred` → `low` in memory, so a legacy file arriving via the
// watcher is still correct in memory; persisting from the always-on fsnotify
// path is avoided because that write would be an unguarded read-modify-write
// that could clobber a concurrent external write (git checkout / editor / second
// instance), dirty the separate .nibs git tree (breaking the prescribed
// `git -C .nibs pull --rebase`), and fire a spurious content-free self-write
// event. On the watcher path disk converges on the next explicit Update/Load.
//
// The upshot for consistency: the bulk Load path persists migrations, so disk
// and memory agree immediately; the watcher path leaves disk untouched, so a
// legacy `deferred` (or v0) file that first appears post-startup stays diverged
// on disk (memory is correct) until the next explicit Update or full Load.
//
// Today the only such normalization is the legacy `priority: deferred` → `low`
// migration: nib.Parse rewrites the value in memory and flags the nib via
// PriorityMigrated(). The etag layer no longer depends on this write-back:
// computeStoredETag parses the on-disk file and hashes its canonical Render(),
// so a legacy `deferred` file yields the same etag as the in-memory `low` value
// and an if-match Update matches either way. The write-back is retained purely
// to converge the raw on-disk bytes with the in-memory value, so external
// consumers (git diffs, editors, other tooling) see the migrated `low` promptly
// rather than only after the next explicit Update.
//
// The write is gated on Version >= 1 to leave legacy v0 nibs to migrateV0ToV1,
// which performs the COMPLETE v0->v1 conversion (blocking -> blocked_by on
// targets, clear blocking, bump version) and persists it; the bulk path always
// runs that pass after the walk. Persisting a v0 nib here would write a
// half-migrated file (priority normalized but still version 0 with `blocking:`
// intact — nib.Render now preserves that field rather than dropping it) and then
// migrateV0ToV1 would rewrite it again: a redundant double-write of a transiently
// inconsistent shape. Gating on Version >= 1 avoids both.
//
// Persistence is best-effort: on a write failure (read-only mount, disk full,
// restricted permissions) it logs, returns migrated=false, and continues,
// leaving the nib correct in memory — on-disk convergence then waits for the
// next successful write. This mirrors the "don't fail load" posture of the
// search-index re-population in loadFromDisk. Must be called with c.mu held (it
// may saveToDisk).
func (c *Core) loadNibReconciledLocked(path string) (*nib.Nib, bool, error) {
	b, err := c.loadNib(path)
	if err != nil {
		return nil, false, err
	}
	if b.PriorityMigrated() && b.Version >= 1 {
		if err := c.saveToDisk(b); err != nil {
			c.logWarn("could not persist priority migration for %s: %v", b.ID, err)
			return b, false, nil
		}
		return b, true, nil
	}
	return b, false, nil
}

// ensureSearchIndexLocked initializes the in-memory search index if not already created.
// Must be called with lock held or from a method that holds the lock.
func (c *Core) ensureSearchIndexLocked() error {
	if c.searchIndex != nil {
		return nil
	}

	idx, err := search.NewIndex()
	if err != nil {
		return fmt.Errorf("initializing search index: %w", err)
	}

	c.searchIndex = idx

	// Populate the in-memory index with existing nibs
	allNibs := make([]*nib.Nib, 0, len(c.nibs))
	for _, b := range c.nibs {
		allNibs = append(allNibs, b)
	}
	if err := c.searchIndex.IndexNibs(allNibs); err != nil {
		return fmt.Errorf("populating search index: %w", err)
	}

	return nil
}

// Search returns nibs matching the query: direct ID matches first (sorted by
// ID), followed by full-text hits in relevance order. A nib matching both
// appears once, in the ID-match position. Each leg is independently capped
// at DefaultSearchLimit. The search index is lazily initialized on first use.
func (c *Core) Search(query string) ([]*nib.Nib, error) {
	// Ensure index is initialized (needs write lock for lazy init)
	c.mu.Lock()
	if err := c.ensureSearchIndexLocked(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	// Capture searchIndex reference while holding lock
	idx := c.searchIndex
	c.mu.Unlock()

	// Perform search outside the lock (Bleve is thread-safe)
	ids, err := idx.Search(query, DefaultSearchLimit)
	if err != nil {
		return nil, err
	}

	// Read from nibs map (needs read lock only)
	c.mu.RLock()
	defer c.mu.RUnlock()

	idMatches := c.idMatchesLocked(query)
	seen := make(map[string]bool, len(idMatches))
	for _, b := range idMatches {
		seen[b.ID] = true
	}

	result := make([]*nib.Nib, 0, len(idMatches)+len(ids))
	result = append(result, idMatches...)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		if b, ok := c.nibs[id]; ok {
			result = append(result, b)
		}
	}
	return result, nil
}

// idMatchesLocked returns nibs whose IDs match the query, sorted by ID and
// capped at DefaultSearchLimit.
// This complements the full-text index: the Bleve `id` field is a keyword
// (unanalyzed) field, so query-string terms never match it there.
// Must be called with at least a read lock held.
func (c *Core) idMatchesLocked(query string) []*nib.Nib {
	m := prepareIDQuery(query, c.configPrefix())
	var matches []*nib.Nib
	for id, b := range c.nibs {
		if m.matches(id) {
			matches = append(matches, b)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	// Cap after sorting so the kept set is deterministic, mirroring the
	// full-text leg's limit.
	if len(matches) > DefaultSearchLimit {
		matches = matches[:DefaultSearchLimit]
	}
	return matches
}

// minIDFragmentLen is the minimum query length for the short-ID substring
// branch of (idQueryMatcher).matches. A 1-char query matches ~10% of all short IDs,
// flooding results on the first keystroke of an interactive search.
// Quoted in user-facing docs: schema.graphqls (NibFilter.search — regenerate
// after editing) and cmd/list.go --search help; update those when changing
// this value.
const minIDFragmentLen = 2

// normalizeSearchQuery prepares a search query for ID matching: surrounding
// whitespace trimmed (pasted IDs commonly carry a trailing space) and
// lowercased for case-insensitive comparison.
func normalizeSearchQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// isIDFragment reports whether a normalized query consists solely of
// short-ID characters: [0-9a-z], as gated by nib.IsIDChar (derived from
// nib.idAlphabet, the single source of truth for the short-ID charset).
// Hyphens are deliberately excluded — they belong to
// prefixes (reprefix.prefixPattern / ValidatePrefix), not short IDs, and
// admitting them would let Bleve operator queries like `-42` (negation)
// substring-match legacy or foreign-prefix IDs, which come from filenames
// unvalidated and may keep a hyphen in their short form (e.g. `task-42`
// under prefix `nibs-`). Queries bearing the configured prefix take the
// prefix branch of (idQueryMatcher).matches before this gate applies; other
// full IDs match via its exact-equality escape, so hyphenated foreign IDs
// are otherwise findable only by charset-clean fragments.
func isIDFragment(query string) bool {
	for i := 0; i < len(query); i++ {
		if !nib.IsIDChar(query[i]) {
			return false
		}
	}
	return true
}

// matchesIDQuery reports whether a search query matches a nib ID,
// case-insensitively (via normalizeSearchQuery). A query equal to the full
// ID or the short ID always matches; a query starting with the configured
// prefix (with a non-empty remainder) must be a prefix of the full ID; any
// other query of at least minIDFragmentLen characters matches as a
// substring of the short ID (the full ID minus the prefix).
//
// Queries with internal whitespace or Bleve operators can't match: the
// substring branch requires a pure ID fragment (isIDFragment), and the
// prefix and equality branches require the query to literally match a real
// ID. This is intentional — do not tokenize the query here.
//
// Test-only seam with no production callers: a pure single-shot entry that
// delegates through prepareIDQuery, so table tests exercise the same logic
// idMatchesLocked runs per nib (cf. the oracle convention in mentions.go).
func matchesIDQuery(query, id, prefix string) bool {
	return prepareIDQuery(query, prefix).matches(id)
}

// idQueryMatcher carries the query-only parts of ID matching, precomputed
// by prepareIDQuery so idMatchesLocked doesn't redo them for every nib.
type idQueryMatcher struct {
	// query is the normalized (trimmed, lowercased) search query.
	query string
	// prefix is the lowercased configured prefix (never trimmed).
	prefix string
	// prefixed: query bears the configured prefix with a non-empty
	// remainder, so it must literally prefix the full ID.
	prefixed bool
	// fragment: query is a charset-clean fragment (isIDFragment) of at
	// least minIDFragmentLen characters, eligible for substring matching.
	fragment bool
}

// prepareIDQuery normalizes the raw query and prefix and evaluates the
// query-only checks once. Match per-ID with (idQueryMatcher).matches.
func prepareIDQuery(query, prefix string) idQueryMatcher {
	query = normalizeSearchQuery(query)
	prefix = strings.ToLower(prefix)
	return idQueryMatcher{
		query:    query,
		prefix:   prefix,
		prefixed: prefix != "" && strings.HasPrefix(query, prefix) && len(query) > len(prefix),
		fragment: len(query) >= minIDFragmentLen && isIDFragment(query),
	}
}

// matches applies the prepared query to one nib ID. See matchesIDQuery for
// the matching rules.
func (m idQueryMatcher) matches(id string) bool {
	id = strings.ToLower(id)
	shortID := strings.TrimPrefix(id, m.prefix)

	// Exact equality bypasses the fragment charset gate below, so legacy or
	// foreign-prefix IDs whose short form keeps a hyphen (e.g. task-42 under
	// prefix nibs-) stay findable by their own full ID.
	if m.query == id || m.query == shortID {
		return true
	}
	if m.prefixed {
		return strings.HasPrefix(id, m.query)
	}
	return m.fragment && strings.Contains(shortID, m.query)
}

// All returns a slice of all nibs.
func (c *Core) All() []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*nib.Nib, 0, len(c.nibs))
	for _, b := range c.nibs {
		result = append(result, b)
	}
	return result
}

// Get finds a nib by exact ID match.
// If a prefix is configured and the query doesn't include it, the prefix is automatically prepended.
// For example, with prefix "nibs-", Get("abc") will match "nibs-abc" but Get("ab") will not.
func (c *Core) Get(id string) (*nib.Nib, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try exact match
	if b, ok := c.nibs[id]; ok {
		return b, nil
	}

	// If not found and we have a configured prefix that isn't already in the query,
	// try with the prefix prepended (allows short IDs like "abc" to match "nibs-abc")
	if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		if b, ok := c.nibs[c.config.Nibs.Prefix+id]; ok {
			return b, nil
		}
	}

	return nil, ErrNotFound
}

// GetForUpdate returns a deep copy (Clone) of the nib the caller OWNS and may
// freely mutate before handing it to Update. Unlike Get — which returns the
// SHARED c.nibs[id] pointer — mutating the returned nib never touches in-memory
// store state, so a rejected Update cannot leave a phantom mutation behind.
// Returns the same errors as Get (notably ErrNotFound) when the nib is missing.
func (c *Core) GetForUpdate(id string) (*nib.Nib, error) {
	b, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	return b.Clone(), nil
}

// NormalizeID resolves a potentially short ID to its full form.
// If a prefix is configured and the query doesn't include it, the prefix is automatically prepended.
// Returns the full ID and true if found, or the original ID and false if not found.
//
// Shares resolution logic with Core.normalizeIDForLookupLocked and
// resolveMentionToken via normalizeIDInMap — behaviour changes must be
// made in the shared helper so all three stay in lockstep.
func (c *Core) NormalizeID(id string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if full, ok := normalizeIDInMap(c.nibs, id, c.configPrefix()); ok {
		return full, true
	}
	// Preserve the "echo the original id back" convention that existing
	// callers rely on when the id doesn't resolve.
	return id, false
}

// validateEnums checks that the nib's enum fields (type, status, priority,
// estimate) hold either the empty "unset -> use default" sentinel (always
// accepted) or a value valid under the current config. This is the single
// write-path chokepoint that gives every entry point — CLI, GraphQL, MCP (which
// rides the same GraphQL resolvers), and the TUI — uniform enum integrity, so a
// GraphQL/MCP client can no longer persist a nib with e.g. status "banana"
// (nibs-9tj2). It matches the CLI's `v != "" && !IsValid...` discipline exactly:
// only non-empty values are checked, so the empty sentinel that means "apply the
// default" (EffectiveType/EffectivePriority) is never rejected. No-ops when no
// config is set (several test setups run config-less).
func (c *Core) validateEnums(b *nib.Nib) error {
	if c.config == nil {
		return nil
	}
	if b.Type != "" && !c.config.IsValidType(b.Type) {
		return fmt.Errorf("invalid type %q: must be one of %s", b.Type, c.config.TypeList())
	}
	if b.Status != "" && !c.config.IsValidStatus(b.Status) {
		return fmt.Errorf("invalid status %q: must be one of %s", b.Status, c.config.StatusList())
	}
	if b.Priority != "" && !c.config.IsValidPriority(b.Priority) {
		return fmt.Errorf("invalid priority %q: must be one of %s", b.Priority, c.config.PriorityList())
	}
	if b.Estimate != "" && !c.config.IsValidEstimate(b.Estimate) {
		return fmt.Errorf("invalid estimate %q: must be one of %s", b.Estimate, c.config.EstimateList())
	}
	return nil
}

// Create adds a new nib, generating an ID if needed, and writes it to disk.
func (c *Core) Create(b *nib.Nib) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Reject invalid enum values before touching any state (nibs-9tj2).
	if err := c.validateEnums(b); err != nil {
		return err
	}

	// Generate ID if not provided
	if b.ID == "" {
		prefix := ""
		length := 4
		if c.config != nil {
			prefix = c.config.Nibs.Prefix
			if c.config.Nibs.IDLength > 0 {
				length = c.config.Nibs.IDLength
			}
		}
		b.ID = nib.NewID(prefix, length)
	}

	// Set timestamps
	now := time.Now().UTC().Truncate(time.Second)
	b.CreatedAt = &now
	b.UpdatedAt = &now

	// Write to disk
	if err := c.saveToDisk(b); err != nil {
		return err
	}

	// Add to in-memory map
	c.nibs[b.ID] = b

	// Update reverse-mention index with this source's outbound edges.
	c.mentionIdx.Add(b.ID, b.Body)

	// Update search index if active (best-effort, don't fail create)
	if c.searchIndex != nil {
		if err := c.searchIndex.IndexNib(b); err != nil {
			c.logWarn("failed to index nib %s: %v", b.ID, err)
		}
	}

	return nil
}

// CurrentETag returns the canonical ETag for the nib's on-disk content — a hash
// of the parsed file's canonical Render() (see computeStoredETag), so it agrees
// with the in-memory nib.ETag() across benign formatting drift (reordered YAML
// keys, whitespace, the `deferred`->`low` priority normalization). loadNib keeps
// the stored Nib's Type/Priority empty when the file omits them (the "task"/
// "normal" defaults are applied only at the consumption boundary via
// nib.EffectiveType()/EffectivePriority()), so a priority/type-less file no longer
// diverges from its in-memory nib.ETag() (nibs-7d3o). The one remaining residual
// is loadNib's created_at/updated_at mtime fallback for hand-authored files that
// omit those timestamps — not reproduced here — which no app-created nib ever
// hits (Render always emits both). Used by bulk-reorder pre-validation
// to check optimistic concurrency without a write. Returns ErrNotFound when the
// id does not resolve. Falls back to the in-memory etag only when no on-disk
// file exists yet (empty Path, or os.IsNotExist — a freshly created nib not yet
// flushed, or an externally removed file), matching the fallback semantics inside
// Update; an existing file that cannot be read or parsed fails CLOSED instead
// (see computeStoredETag's fail-open/fail-closed matrix).
func (c *Core) CurrentETag(id string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	storedNib, ok := c.nibs[id]
	if !ok {
		// Honour prefix-resolution like Get does, so callers can pass either
		// short or canonical ids and receive consistent behaviour.
		if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
			storedNib, ok = c.nibs[c.config.Nibs.Prefix+id]
		}
		if !ok {
			return "", ErrNotFound
		}
	}
	return c.computeStoredETag(storedNib)
}

// computeStoredETag returns the canonical etag for a stored nib by reading AND
// PARSING the current on-disk file and returning the parsed nib's ETag (a hash
// of its canonical Render()) — not a hash of the raw disk bytes. Hashing the
// canonical render (rather than the bytes) makes the stored etag equal the
// in-memory nib.ETag() whenever the on-disk content is canonically equivalent,
// so an ETag()-derived if-match (a) survives benign round-trip/formatting drift
// (reordered YAML keys, whitespace, the `deferred`->`low` priority
// normalization) yet (b) still fails with ETagMismatchError on genuine content
// divergence — including divergence in content outside Render()'s modeled fields
// (unknown/extra YAML keys, a legacy v0 `blocking:` line), which nib.Render now
// preserves. Caller must hold c.mu (read or write lock).
//
// Parsing is done with the bare nib.Parse (which already normalizes the legacy
// `deferred` priority to `low`) and only the ID is copied over from the stored
// nib so the rendered `# <id>` header line matches. It deliberately does NOT go
// through loadNib, but the two now agree on Type/Priority: loadNib keeps them
// empty when the file omits them (the "task"/"normal" presentation defaults are
// applied at the consumption boundary via nib.EffectiveType()/EffectivePriority(),
// never mutated onto the stored Nib), so a bare-parse render and the in-memory
// nib.ETag() render the same key set. That is what fixed nibs-7d3o: a priority-
// or type-less file — including every nib the CreateNib resolver writes without a
// priority — no longer false-conflicts on an if-match Update, and the just-created
// (never-Loaded) path still round-trips because its stored nib is likewise empty.
//
// loadNib's empty-slice defaults are etag-safe (Render's omitempty renders a nil
// and an empty slice identically). The lone divergence loadNib can still
// introduce is its created_at/updated_at mtime fallback, not reproduced here — but
// that only touches HAND-AUTHORED files missing those timestamps; every app-
// created nib carries both (Create sets them, Render emits them), so no real or
// app-created nib hits it.
//
// Fallback discipline when the canonical render cannot be computed from disk.
// The etag exists to certify the current on-disk bytes, so each branch is chosen
// deliberately as fail-OPEN (return the in-memory ETag with a nil error, so a
// normal if-match still matches) or fail-CLOSED (return a non-reconcilable
// *OnDiskUnparseableError and NO etag token, so Update/CurrentETag refuse the
// overwrite and no retry-with-Current can satisfy the guard — the finding #5
// hardening). A client's if-match always originates from a canonical nib.ETag()
// (16 lowercase hex chars) obtained via Get.
//
//	condition                          verdict  returns                      logged?
//	---------------------------------  -------  ---------------------------  -------
//	empty Path                         OPEN     (in-memory ETag(), nil)      no  (not flushed yet)
//	read err, os.IsNotExist            OPEN     (in-memory ETag(), nil)      no  (not flushed / P2 delete-race)
//	read err, other (perms, torn I/O)  CLOSED   ("", OnDiskUnparseableError) no  (returned; caller surfaces)
//	parse err (corrupt/conflict/typo)  CLOSED   ("", OnDiskUnparseableError) no  (returned; caller surfaces)
//	parsed OK                          --       (canonical b.ETag(), nil)    --
//
// The two OPEN branches are intentionally SILENT: "not flushed yet" is the normal
// freshly-created path, and an externally-deleted file (P2) is an accepted race
// (resurrection). The two CLOSED branches do NOT log here either: they RETURN a
// distinct non-reconcilable *OnDiskUnparseableError (no sentinel etag a naive
// reconcile-retry could echo back), and it is the CALLER's job to surface it —
// Update propagates it to the client (cmd/update.go → FILE_ERROR) and the
// bulk-reorder pre-validation wraps it, while the best-effort backfill/activation
// read paths deliberately swallow it. Logging here as well would double-handle the
// error and flood stderr on the hot Children read path (orderer.go's
// backfillOrderKeys re-attempts the Update once per read for a persistently
// uncertifiable sibling). Caller must hold c.mu (read or write lock).
func (c *Core) computeStoredETag(storedNib *nib.Nib) (string, error) {
	if storedNib.Path == "" {
		return storedNib.ETag(), nil
	}
	diskPath := filepath.Join(c.root, storedNib.Path)
	raw, err := os.ReadFile(diskPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Legitimately "in memory but not flushed yet" (freshly created) or
			// externally removed (accepted delete-race, P2): fall back to the
			// in-memory etag, matching Update's own not-flushed semantics. Silent
			// by design — this is the normal path, not an anomaly.
			return storedNib.ETag(), nil
		}
		// File EXISTS but its bytes cannot be READ (permission-denied, transient
		// or torn I/O). Fail CLOSED with a non-reconcilable error carrying no etag
		// token: the current on-disk content cannot be certified, so the overwrite
		// is refused and no retry-with-Current can satisfy the guard. We RETURN the
		// error rather than logging it here — the caller surfaces it where it
		// matters (see the matrix above); logging too would double-handle it and
		// flood stderr on the hot Children read path (orderer.go backfillOrderKeys).
		return "", &OnDiskUnparseableError{ID: storedNib.ID, Path: storedNib.Path, Reason: "unreadable", Err: err}
	}

	b, err := nib.Parse(bytes.NewReader(raw))
	if err != nil {
		// File EXISTS but is unparseable (torn/partial write, git merge-conflict
		// markers, hand-edit YAML typo). Fail CLOSED with a non-reconcilable error
		// so the divergent/corrupt file cannot be clobbered — not even by a naive
		// client that retries with a fabricated/raw-bytes etag (the finding #5
		// single-shot vulnerability). RETURN the error (do not log it here) — the
		// caller surfaces it where it matters; logging too would double-handle it
		// and flood stderr on the hot Children read path (orderer.go
		// backfillOrderKeys).
		return "", &OnDiskUnparseableError{ID: storedNib.ID, Path: storedNib.Path, Reason: "unparseable", Err: err}
	}
	// The rendered form includes the `# <id>` header, which is derived from the
	// filename (not the front matter). Use the stored id so the render — and thus
	// the etag — matches what the caller computed from the same stored nib.
	b.ID = storedNib.ID
	return b.ETag(), nil
}

// Update modifies an existing nib and writes it to disk.
// If ifMatch is provided, validates the current on-disk version's etag matches before updating.
// This provides optimistic concurrency control to prevent lost updates.
func (c *Core) Update(b *nib.Nib, ifMatch *string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Verify nib exists in memory
	storedNib, ok := c.nibs[b.ID]
	if !ok {
		return ErrNotFound
	}

	// Reject invalid enum values before the concurrency guard or any write
	// (nibs-9tj2) — input validity is independent of the etag precondition.
	if err := c.validateEnums(b); err != nil {
		return err
	}

	// Validate etag if provided or required
	requireIfMatch := c.config != nil && c.config.Nibs.RequireIfMatch

	if requireIfMatch && (ifMatch == nil || *ifMatch == "") {
		return &ETagRequiredError{}
	}

	if ifMatch != nil && *ifMatch != "" {
		currentETag, err := c.computeStoredETag(storedNib)
		if err != nil {
			// The current on-disk state cannot be certified (unparseable or
			// unreadable). Surface the distinct, non-reconcilable error rather than
			// an ETagMismatchError: there is no server etag a retry could echo back
			// to satisfy the guard, so the corrupt/unreadable file cannot be
			// clobbered by a blind reconcile-retry (finding #5).
			return err
		}
		if currentETag != *ifMatch {
			return &ETagMismatchError{
				Provided: *ifMatch,
				Current:  currentETag,
			}
		}
	}

	// Update timestamp
	now := time.Now().UTC().Truncate(time.Second)
	b.UpdatedAt = &now

	// Write to disk
	if err := c.saveToDisk(b); err != nil {
		return err
	}

	// Update in-memory map
	c.nibs[b.ID] = b

	// Refresh the reverse-mention index to reflect the new body.
	c.mentionIdx.Replace(b.ID, b.Body)

	// Update search index if active (best-effort, don't fail update)
	if c.searchIndex != nil {
		if err := c.searchIndex.IndexNib(b); err != nil {
			c.logWarn("failed to update nib %s in search index: %v", b.ID, err)
		}
	}

	return nil
}

// saveToDisk writes a nib to the filesystem.
func (c *Core) saveToDisk(b *nib.Nib) error {
	// Determine the file path
	var path string
	if b.Path != "" {
		path = filepath.Join(c.root, b.Path)
	} else {
		filename := nib.BuildFilename(b.ID, b.Slug)
		path = filepath.Join(c.root, filename)
		b.Path = filename
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Render and write
	content, err := b.Render()
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// Delete removes a nib by exact ID match.
// Supports short IDs (without prefix) if a prefix is configured.
func (c *Core) Delete(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the nib by exact match
	targetID := id
	targetNib, ok := c.nibs[id]

	// If not found and we have a configured prefix, try with prefix prepended
	if !ok && c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		fullID := c.config.Nibs.Prefix + id
		if b, found := c.nibs[fullID]; found {
			targetID = fullID
			targetNib = b
			ok = true
		}
	}

	if !ok {
		return ErrNotFound
	}

	// Remove from disk
	path := filepath.Join(c.root, targetNib.Path)
	if err := os.Remove(path); err != nil {
		return err
	}

	// Remove from in-memory map
	delete(c.nibs, targetID)

	// Drop the source from the reverse-mention index.
	c.mentionIdx.Remove(targetID)

	// Update search index if active (best-effort, don't fail delete)
	if c.searchIndex != nil {
		if err := c.searchIndex.DeleteNib(targetID); err != nil {
			c.logWarn("failed to remove nib %s from search index: %v", targetID, err)
		}
	}

	return nil
}

// Archive moves a nib to the archive directory.
// Supports short IDs (without prefix) if a prefix is configured.
func (c *Core) Archive(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the nib
	targetNib, targetID, err := c.findNibLocked(id)
	if err != nil {
		return err
	}

	// Check if already archived
	if c.isArchivedPath(targetNib.Path) {
		return nil // Already archived, nothing to do
	}

	// Ensure archive directory exists
	archivePath := filepath.Join(c.root, ArchiveDir)
	if err := os.MkdirAll(archivePath, 0755); err != nil {
		return fmt.Errorf("creating archive directory: %w", err)
	}

	// Move the file
	oldPath := filepath.Join(c.root, targetNib.Path)
	newRelPath := filepath.Join(ArchiveDir, filepath.Base(targetNib.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving nib to archive: %w", err)
	}

	// Update nib's path
	targetNib.Path = filepath.ToSlash(newRelPath)
	c.nibs[targetID] = targetNib

	return nil
}

// Unarchive moves a nib from the archive directory back to the main directory.
// Supports short IDs (without prefix) if a prefix is configured.
func (c *Core) Unarchive(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the nib
	targetNib, targetID, err := c.findNibLocked(id)
	if err != nil {
		return err
	}

	// Check if not archived
	if !c.isArchivedPath(targetNib.Path) {
		return nil // Not archived, nothing to do
	}

	// Move the file back to main directory
	oldPath := filepath.Join(c.root, targetNib.Path)
	newRelPath := filepath.Base(targetNib.Path)
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving nib from archive: %w", err)
	}

	// Update nib's path
	targetNib.Path = newRelPath
	c.nibs[targetID] = targetNib

	return nil
}

// IsArchived returns true if the nib with the given ID is in the archive.
// Supports short IDs (without prefix) if a prefix is configured.
func (c *Core) IsArchived(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	b, _, err := c.findNibLocked(id)
	if err != nil {
		return false
	}

	return c.isArchivedPath(b.Path)
}

// isArchivedPath returns true if the path indicates an archived nib.
func (c *Core) isArchivedPath(path string) bool {
	return strings.HasPrefix(path, ArchiveDir+string(filepath.Separator)) ||
		strings.HasPrefix(path, ArchiveDir+"/")
}

// normalizeID returns the full ID with prefix if a prefix is configured
// and the ID doesn't already have it.
func (c *Core) normalizeID(id string) string {
	if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		return c.config.Nibs.Prefix + id
	}
	return id
}

// findNibLocked finds a nib by ID, supporting short IDs.
// Must be called with lock held.
func (c *Core) findNibLocked(id string) (*nib.Nib, string, error) {
	// Try exact match
	if b, ok := c.nibs[id]; ok {
		return b, id, nil
	}

	// Try with prefix prepended
	fullID := c.normalizeID(id)
	if fullID != id {
		if b, ok := c.nibs[fullID]; ok {
			return b, fullID, nil
		}
	}

	return nil, "", ErrNotFound
}

// GetFromArchive loads a nib directly from the archive directory.
// This is used when a nib isn't in the main loaded set but might be archived.
// Returns nil, nil if the archive directory doesn't exist or nib not found.
func (c *Core) GetFromArchive(id string) (*nib.Nib, error) {
	fullID := c.normalizeID(id)

	archiveDir := filepath.Join(c.root, ArchiveDir)
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return nil, nil
	}

	// Look for the nib file in the archive
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		fileID, _ := nib.ParseFilename(entry.Name())
		if fileID == fullID {
			path := filepath.Join(archiveDir, entry.Name())
			return c.loadNib(path)
		}
	}

	return nil, nil
}

// LoadAndUnarchive finds a nib in the archive, loads it, unarchives it,
// and adds it to the in-memory store. Returns the nib or ErrNotFound.
func (c *Core) LoadAndUnarchive(id string) (*nib.Nib, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the nib (always loaded since we now include archived nibs)
	b, targetID, err := c.findNibLocked(id)
	if err != nil {
		return nil, ErrNotFound
	}

	// If already in main directory, just return it
	if !c.isArchivedPath(b.Path) {
		return b, nil
	}

	// Move file from archive to main directory
	oldPath := filepath.Join(c.root, b.Path)
	newRelPath := filepath.Base(b.Path)
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.Rename(oldPath, newPath); err != nil {
		return nil, fmt.Errorf("moving nib from archive: %w", err)
	}

	// Update nib's path
	b.Path = newRelPath
	c.nibs[targetID] = b

	return b, nil
}

// Init creates the .nibs directory if it doesn't exist.
func (c *Core) Init() error {
	return os.MkdirAll(c.root, 0755)
}

// FullPath returns the absolute path to a nib file.
func (c *Core) FullPath(b *nib.Nib) string {
	return filepath.Join(c.root, b.Path)
}

// Close stops any active file watcher and cleans up resources.
func (c *Core) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close search index if open
	if c.searchIndex != nil {
		if err := c.searchIndex.Close(); err != nil {
			return err
		}
		c.searchIndex = nil
	}

	return c.unwatchLocked()
}

// Init creates the .nibs directory at the given path if it doesn't exist.
// This is a standalone function for use before a Core is created.
func Init(dir string) error {
	nibsPath := filepath.Join(dir, NibsDir)
	return os.MkdirAll(nibsPath, 0755)
}
