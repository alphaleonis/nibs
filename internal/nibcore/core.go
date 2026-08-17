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
	"sync/atomic"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/search"
	"github.com/alphaleonis/nibs/internal/store"
)

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
	layout store.Layout   // the store's directory structure, derived from root
	config *config.Config // project configuration

	// lockPath is the OS-temp-dir path of the cross-process advisory write lock
	// guarding this .nibs directory. Every write mutator holds it for the duration
	// of its read-check-write so two nibs processes (or serve + a CLI) on the same
	// machine cannot both pass the etag check and clobber each other.
	lockPath string

	// In-memory state
	mu   sync.RWMutex
	nibs map[string]*nib.Nib // ID -> Nib

	// Reverse-mention index: maintained alongside c.nibs so FindMentionedBy /
	// FindMentions avoid O(N × body) re-parsing on every call. Guarded by c.mu
	// (writers under Lock, readers under RLock) — mentionIndex itself is not
	// internally synchronized.
	mentionIdx *mentionIndex

	// Load-time integrity diagnostics from the last loadFromDisk, guarded by
	// c.mu alongside c.nibs and rebuilt from scratch on every load. Both record
	// a file that IS on disk but is not answerable through the store — a skipped
	// unparseable file, and the loser of an id collision — so neither is
	// recoverable from c.nibs afterwards. Retaining them is what lets
	// CheckAllLinks report what previously reached only logWarn's stderr, which
	// no production code redirects.
	unparseableFiles []UnparseableFile
	duplicateIDs     []DuplicateID

	// Search index (optional, lazy-initialized)
	searchIndex SearchIndex

	// File watching (optional)
	watching bool
	done     chan struct{}

	// Event subscribers (for channel-based API). Two kinds share subMu and the
	// nextSubID counter but live in separate maps: payload subscribers receive
	// cloned NibEvent batches; signal-only subscribers receive a bare struct{}
	// tick ("something changed") and never a payload.
	subscribers       map[uint64]*subscription
	signalSubscribers map[uint64]chan struct{}
	subMu             sync.RWMutex
	nextSubID         uint64

	// payloadSubCount mirrors len(subscribers): incremented/decremented under
	// subMu alongside every payload subscribe/unsubscribe, but read WITHOUT any
	// lock (a single atomic load) by handleChanges to decide whether the per-nib
	// payload clone is worth paying. Keeping it atomic lets handleChanges read it
	// while holding c.mu without acquiring subMu on that hot path — sparing the
	// lock and its contention, not for deadlock safety (the established order is
	// c.mu -> subMu, taken in unwatchLocked/Close; nothing acquires c.mu while
	// holding subMu). See handleChanges for the full reasoning.
	payloadSubCount atomic.Int64

	// Warning logger for non-fatal errors. It defaults to stderr through
	// safetext.Writer, because the highest-traffic warning here interpolates a
	// FILENAME ("skipping unparseable nib file %s") and a filename on Linux is
	// arbitrary bytes: a file named with an embedded ESC sequence would otherwise
	// repaint the terminal from every command that loads the store. The boundary
	// lives on the writer rather than at the logWarn call sites so it cannot be
	// bypassed by adding a warning that forgets it.
	warnWriter io.Writer
}

// New creates a new Core with the given root path and configuration.
func New(root string, cfg *config.Config) *Core {
	return &Core{
		root:              root,
		layout:            store.NewLayout(root),
		config:            cfg,
		lockPath:          writeLockPath(root),
		nibs:              make(map[string]*nib.Nib),
		mentionIdx:        newMentionIndex(),
		subscribers:       make(map[uint64]*subscription),
		signalSubscribers: make(map[uint64]chan struct{}),
		warnWriter:        safetext.NewWriter(os.Stderr),
	}
}

// acquireWriteLock takes the cross-process advisory write lock for the whole
// .nibs directory and returns a release func. Callers already hold c.mu; the lock
// order is always c.mu then this file lock, so cooperating processes serialize
// their mutations without any deadlock. Held only for the span of one mutating
// operation so a long-lived serve process never starves concurrent CLIs.
func (c *Core) acquireWriteLock() (func() error, error) {
	return acquireFileLock(c.lockPath)
}

// SetWarnWriter sets the writer for warning messages.
// Pass nil to disable warnings.
//
// The replacement is wrapped in the same rendering boundary the default carries,
// so a caller redirecting warnings (tests, an embedding process) cannot
// accidentally opt out of it.
func (c *Core) SetWarnWriter(w io.Writer) {
	if w == nil {
		c.warnWriter = nil
		return
	}
	c.warnWriter = safetext.NewWriter(w)
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

// Load reads all nibs from disk into memory. It NEVER writes: every load-time
// normalization that used to persist (the v0→v1 blocking migration, the
// `priority: deferred` write-back) is retired in favor of the explicit
// `nibs migrate` command. The CLI's pre-run gate refuses other commands while
// a migration is pending, so AT STARTUP a legacy shape reaching a loaded
// store is only ever observed by migrate itself — but the gate fires once per
// process: a legacy file arriving through the watcher into a live serve (a
// `git pull` in .nibs) still loads exactly as written (see handleChanges in
// watcher.go) and is observed by every query until `nibs migrate` runs.
// Legacy tolerance elsewhere (e.g. Render re-emitting a v0 `blocking:` so the
// etag stays faithful) exists for that window and must not be removed on the
// strength of the startup gate alone. Either way, what Load reports is what
// disk holds.
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

	// Both diagnostics describe THIS load only, so a repaired file stops being
	// reported the moment it loads cleanly. Cleared here rather than appended to
	// so a reload never accumulates stale accusations.
	c.unparseableFiles = nil
	c.duplicateIDs = nil

	// Walk the store's CONTENT directories — data/ and archive/, dot
	// directories pruned (see WalkStoreContent). cmd/migrate's scans walk the
	// same per-file classifier over the store root, so what loads here and
	// what the migration gates probe can never disagree about whether a given
	// file is a nib — only about which directories are in scope, which is the
	// whole difference between "content" and "everything the migration must
	// relocate".
	err := WalkStoreContent(c.layout, func(path string, err error) error {
		if err != nil {
			return err
		}

		b, loadErr := c.loadNib(path)
		if loadErr != nil {
			// Log-and-skip a single unparseable/unreadable file rather than
			// aborting the whole walk: yaml.v3 hard-errors on a duplicate
			// front-matter key (where yaml.v2 took last-wins), so one pre-existing
			// malformed nib (bad merge, hand-edit, partial write) would otherwise
			// make every nibs command fail to load ANY nib. Degrade to one missing
			// nib instead of a dead store, matching the fsnotify watcher's per-file
			// "log and continue" posture. The file's bytes are left untouched
			// (skip = not loaded into memory; never delete/rewrite).
			//
			// Retained as a diagnostic as well as logged: the warning goes to a
			// writer nothing in production redirects, while the skipped nib is
			// missing from every query with nothing to explain it. `nibs check`
			// reads these back (see Core.CheckAllLinks).
			c.logWarn("skipping unparseable nib file %s: %v", path, loadErr)
			id, _ := nib.ParseFilename(filepath.Base(path), c.configPrefix())
			c.unparseableFiles = append(c.unparseableFiles, UnparseableFile{
				NibID:  id,
				Path:   c.relPathFromRoot(path),
				Reason: loadErr.Error(),
			})
			return nil
		}

		// Two on-disk files can parse to the same id (e.g. a slugged and a
		// slugless file for one prefixed id). WalkDir visits lexically, so the
		// last file loaded wins; warn per shadowing event and name both files so
		// the duplicate is discoverable instead of silently swallowed. We warn
		// rather than fail the load: a duplicate is usually a transient abnormal
		// state (an interrupted rename, a manual copy), so degrading to a visible
		// warning beats refusing to load the entire store. Both paths are logged
		// in the walk's absolute form (like the skip warning above) so the
		// operator can go straight to the offending files.
		//
		// The same event is also retained as a diagnostic so `nibs check` can
		// report it (see Core.CheckAllLinks); there the two files are named in
		// nib.Path form, which is how every other nibs surface spells a path.
		if existing, ok := c.nibs[b.ID]; ok {
			c.logWarn("duplicate nib id %q on disk: %s shadows %s (last file loaded wins; resolve the duplicate)",
				b.ID, path, filepath.Join(c.root, existing.Path))
			c.duplicateIDs = append(c.duplicateIDs, DuplicateID{
				NibID:    b.ID,
				Loaded:   b.Path,
				Shadowed: existing.Path,
			})
		}

		// Out-of-enum diagnostic, DELIBERATELY not a normalization: the value
		// loads exactly as written (rewriting — in memory or on disk — belongs
		// to `nibs migrate`, and an in-memory-only fix would diverge the etag
		// from the on-disk bytes). The pre-run migration gate is a header-scan
		// heuristic, so a legacy or hand-edited value can reach a load; this
		// warning plus the `nibs check` finding (see CheckAllLinks) are the
		// authoritative backstop that makes such a value visible instead of
		// silently flowing into ranking, filters, and the web UI.
		if enumErr := c.ValidateEnums(b); enumErr != nil {
			c.logWarn("nib %s: %v — value loads as written; `nibs migrate` rewrites known legacy values, `nibs check` reports the rest", b.ID, enumErr)
		}

		c.nibs[b.ID] = b
		return nil
	})
	if err != nil {
		return err
	}

	// Resolve every short-form link id to its full form now that the whole map
	// exists (see canonicalize.go for why this is the single normalization
	// point). This also serves MigrateV0ToV1, whose exact c.nibs[targetID]
	// lookup relies on a legacy `blocking:` target named by short id having
	// been resolved here.
	c.canonicalizeAllLinksUnpublishedLocked()

	// Rebuild the reverse-mention index from the loaded bodies.
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

// relPathFromRoot renders an absolute path from the load walk the way loadNib
// renders nib.Path — relative to the .nibs root, forward slashes — so a
// diagnostic about a file that never became a nib still names it the way every
// other nibs surface spells a path.
//
// A path that cannot be made relative (a different Windows volume, which the
// walk of c.root should never produce) falls back to the absolute form: naming
// the file at all matters more than the shape it is named in.
func (c *Core) relPathFromRoot(path string) string {
	rel, err := filepath.Rel(c.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
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
	b.ID, b.Slug = nib.ParseFilename(filename, c.configPrefix())

	// Type and Priority are DELIBERATELY not defaulted here. Synthesizing them
	// in memory (Type""→"task", Priority""→"normal") while computeStoredETag
	// bare-parses the file diverges the in-memory ETag() from the stored etag for
	// a file that omits the key, false-conflicting a valid if-match Update with no
	// on-disk change. The stored Nib keeps them EMPTY so Render (which
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
//
// This is the TOP-LEVEL answer to a term — "the best hits for q" — where
// truncation IS the answer and the cap is what keeps a one-word query over a
// large store from materializing it. A caller that intersects the answer with a
// working set it already bounded wants SearchAll instead.
func (c *Core) Search(query string) ([]*nib.Nib, error) {
	return c.search(query, DefaultSearchLimit)
}

// SearchAll returns every nib matching the query, in the same order Search
// uses, with neither leg capped.
//
// It exists because DefaultSearchLimit bounds the wrong population for an
// INTERSECTION. "The children of X matching q" is already bounded by the
// relation; feeding that intersection from the store's global top-N answers a
// different question — "the children of X that are also among the store's top N
// hits for q" — and drops a genuine member that ranks below the cutoff with no
// error and no signal. The result is still bounded, by the store: a query can
// match no more nibs than exist.
func (c *Core) SearchAll(query string) ([]*nib.Nib, error) {
	return c.search(query, Unlimited)
}

// search is the shared body of Search and SearchAll. limit caps each leg
// independently; a limit <= 0 (Unlimited) means no cap on either.
func (c *Core) search(query string, limit int) ([]*nib.Nib, error) {
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
	ids, err := idx.Search(query, limit)
	if err != nil {
		return nil, err
	}

	// Read from nibs map (needs read lock only)
	c.mu.RLock()
	defer c.mu.RUnlock()

	idMatches := c.idMatchesLocked(query, limit)
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
// capped at limit (a limit <= 0, i.e. Unlimited, means uncapped, mirroring the
// full-text leg).
// This complements the full-text index: the Bleve `id` field is a keyword
// (unanalyzed) field, so query-string terms never match it there.
// Must be called with at least a read lock held.
func (c *Core) idMatchesLocked(query string, limit int) []*nib.Nib {
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
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
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
// Resolution mirrors Get (exact id, then the configured prefix prepended);
// returns ErrNotFound when the nib is missing.
//
// The Clone is taken WHILE c.mu is held (via GetSnapshot's clone-under-RLock),
// so the shallow struct copy's field reads — notably Path — cannot race an
// in-place Path writer holding c.mu (e.g. Archive/Unarchive). Cloning the
// shared pointer off-lock would race that writer.
func (c *Core) GetForUpdate(id string) (*nib.Nib, error) {
	if b, ok := c.GetSnapshot(id); ok {
		return b, nil
	}
	return nil, ErrNotFound
}

// GetSnapshot returns a detached deep copy of the nib, cloned WHILE c.mu is
// held, so the returned value never aliases the live store pointer and no field
// (notably Path) is read off-lock. This is the read accessor callers use when
// the result outlives the lock — e.g. GraphQL relationship resolvers whose
// fields gqlgen marshals asynchronously, concurrently with in-place mutations
// like Archive/Unarchive rewriting a stored nib's Path. Get returns the live
// pointer (and would leave that later read racing the writer); GetSnapshot
// returns a safe copy. Resolution mirrors Get (exact id, then the configured
// prefix prepended); ok is false when the nib is absent.
func (c *Core) GetSnapshot(id string) (*nib.Nib, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if b, ok := c.nibs[id]; ok {
		return b.Clone(), true // clone under the lock — this is the whole point
	}
	if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		if b, ok := c.nibs[c.config.Nibs.Prefix+id]; ok {
			return b.Clone(), true
		}
	}
	return nil, false
}

// NormalizeID resolves a potentially short ID to its full form.
// If a prefix is configured and the query doesn't include it, the prefix is automatically prepended.
// Returns the full ID and true if found, or the original ID and false if not found.
//
// Shares resolution logic with Core.normalizeIDForLookupLocked and
// resolveMentionToken via normalizeIDInMap — behavior changes must be
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

// ValidateEnums checks that the nib's enum fields (type, status, priority,
// estimate) hold either the empty "unset -> use default" sentinel (always
// accepted) or a value valid under the current config. This is the single
// write-path chokepoint that gives every entry point — CLI, GraphQL, MCP (which
// rides the same GraphQL resolvers), and the TUI — uniform enum integrity, so a
// GraphQL/MCP client cannot persist a nib with e.g. status "banana".
// It matches the CLI's `v != "" && !IsValid...` discipline exactly:
// only non-empty values are checked, so the empty sentinel that means "apply the
// default" (EffectiveType/EffectivePriority) is never rejected. No-ops when no
// config is set (several test setups run config-less).
//
// It reads no store state — only the nib passed in and the config's enum
// tables, and those tables are the package-level DefaultStatuses/DefaultTypes/
// DefaultPriorities/DefaultEstimates rather than anything held per-config. So it
// takes no lock and is safe to call from outside the store — Create and Update
// call it while holding c.mu, and the GraphQL updateNib pre-check calls it
// off-lock through NibValidator to refuse a doomed update before its later steps
// write to another nib's file.
//
// The immutability that matters here is the enum tables', not the config's: the
// c.config POINTER is fixed at construction, but the struct behind it is live
// and writable — `nibs config set-prefix` assigns cfg.Nibs.Prefix in place. A
// future read of a per-config field from this method would therefore need its
// own argument for going off-lock; the one below does not.
func (c *Core) ValidateEnums(b *nib.Nib) error {
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

	unlock, err := c.acquireWriteLock()
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Reject invalid enum values before touching any state.
	if err := c.ValidateEnums(b); err != nil {
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

	// An id ARRIVING can re-point a link that another nib already holds: a short
	// form left verbatim because nothing answered to it starts resolving, and a
	// bare token arriving alongside its prefixed twin takes a link the twin was
	// answering (normalizeIDInMap tries the exact key first). The watcher cannot
	// cover for this one — the in-process insert above happens BEFORE fsnotify
	// reports the file, so by then the id is already stored and the batch pass sees
	// an ordinary edit rather than an arrival. Ungated, unlike the removal sweep:
	// there is no cheap test for "some stored link spelling names this new id" that
	// is not itself the sweep. See canonicalize.go.
	//
	// Copy-on-write, like every other mutator that rewrites a non-Path field.
	//
	// An in-place edit would be safe HERE in isolation — b is published for the
	// length of a few statements under an exclusive c.mu, so no off-lock reader
	// can hold it — but the copy-on-write rule is stated as an invariant with an
	// exhaustively enumerated exception list (see NibReader.GetSnapshot in
	// internal/graph/interfaces.go), and the whole off-lock read pipeline's
	// safety argument rests on that list being closed. Adding a member costs one
	// allocation here and a permanent hazard there: the next person to move this
	// line past a lock release, or to let a caller publish b beforehand, would
	// read the invariant, see "never rewritten in place on a published pointer",
	// and be wrong.
	//
	// The caller's own pointer keeps the spelling it passed in, which matches
	// every sibling mutator: they rewrite the STORE, not the caller's object.
	if set := canonicalizeLinksInMap(c.nibs, b, c.configPrefix()); set.changed {
		resolved := b.Clone()
		set.applyTo(resolved)
		c.nibs[b.ID] = resolved
	}
	// Warn per rebind for the same reason Core.Delete does: a create moving a THIRD
	// nib's link changes no file and publishes no event, yet the next unrelated
	// write to that bystander persists the new spelling. b's stored entry is
	// already resolved above, so it never appears here.
	for _, rebind := range c.canonicalizeStoreLocked() {
		c.logWarn("creating %s re-pointed %s", b.ID, rebind)
	}

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
// keys, whitespace). loadNib keeps
// the stored Nib's Type/Priority empty when the file omits them (the "task"/
// "normal" defaults are applied only at the consumption boundary via
// nib.EffectiveType()/EffectivePriority()), so a priority/type-less file no longer
// diverges from its in-memory nib.ETag(). The one remaining residual
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
		// Honor prefix-resolution like Get does, so callers can pass either
		// short or canonical ids and receive consistent behavior.
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
// (reordered YAML keys, whitespace) yet (b) still fails with ETagMismatchError
// on genuine content divergence — including divergence in content outside
// Render()'s modeled fields
// (unknown/extra YAML keys, a legacy v0 `blocking:` line), which nib.Render now
// preserves. Caller must hold c.mu (read or write lock).
//
// Parsing is done with the bare nib.Parse and only the ID is copied over from
// the stored
// nib so the rendered `# <id>` header line matches. It deliberately does NOT go
// through loadNib, but the two agree on Type/Priority: loadNib keeps them
// empty when the file omits them (the "task"/"normal" presentation defaults are
// applied at the consumption boundary via nib.EffectiveType()/EffectivePriority(),
// never mutated onto the stored Nib), so a bare-parse render and the in-memory
// nib.ETag() render the same key set. The upshot: a priority-
// or type-less file — including every nib the CreateNib resolver writes without a
// priority — does not false-conflict on an if-match Update, and the just-created
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
// overwrite and no retry-with-Current can satisfy the guard). A client's
// if-match always originates from a canonical nib.ETag()
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
		// client that retries with a fabricated/raw-bytes etag in a single shot.
		// RETURN the error (do not log it here) — the
		// caller surfaces it where it matters; logging too would double-handle it
		// and flood stderr on the hot Children read path (orderer.go
		// backfillOrderKeys).
		return "", &OnDiskUnparseableError{ID: storedNib.ID, Path: storedNib.Path, Reason: "unparseable", Err: err}
	}
	// The rendered form includes the `# <id>` header, which is derived from the
	// filename (not the front matter). Use the stored id so the render — and thus
	// the etag — matches what the caller computed from the same stored nib.
	b.ID = storedNib.ID
	// Resolve short-form link ids the same way the store did when it loaded this
	// file (see canonicalize.go). Without this the two renders diverge on every
	// hand-edited short-form nib — the stored nib carries `parent: nibs-par`, the
	// bare parse `parent: par` — and its if-match Update would conflict forever
	// against an unchanged file. Resolving both sides keeps the two spellings
	// canonically equivalent, while genuine content divergence still mismatches.
	if set := canonicalizeLinksInMap(c.nibs, b, c.configPrefix()); set.changed {
		set.applyTo(b)
	}
	return b.ETag(), nil
}

// Update modifies an existing nib and writes it to disk.
// If ifMatch is provided, validates the current on-disk version's etag matches before updating.
// This provides optimistic concurrency control to prevent lost updates.
//
// KNOWN RESIDUAL RACE (migrate under a live serve, nibs-7ist): the caller's b
// was built from a snapshot taken BEFORE this call, and Update takes c.mu and
// then parks on the store flock while holding it. If `nibs migrate` holds
// that flock (AcquireStoreLock), the whole wait happens with c.mu held — so
// the watcher, which needs c.mu, cannot refresh c.nibs with the migrated
// files first. When migrate releases, an Update WITH ifMatch fails safe (the
// stored etag no longer matches), but an Update with NO ifMatch — the web
// UI's batch mutations call updateNib without one — writes b's pre-migration
// render straight back, erasing e.g. a freshly transferred blocked_by edge;
// the source file is already stamped v1, so no migration detect ever fires
// again and the loss is silent and permanent. This is precisely why migrate
// prints "stop any running `nibs serve`" (see AcquireStoreLock's doc);
// enforcement instead of advice is deferred to nibs-7ist. Note the residual
// is THIS stale-in-memory-clone chain, not the watcher or any layout move —
// the watcher is a bystander that c.mu keeps parked.
func (c *Core) Update(b *nib.Nib, ifMatch *string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = unlock() }()

	// Verify nib exists in memory
	storedNib, ok := c.nibs[b.ID]
	if !ok {
		return ErrNotFound
	}

	// Reject invalid enum values before the concurrency guard or any write
	// — input validity is independent of the etag precondition.
	if err := c.ValidateEnums(b); err != nil {
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
			// clobbered by a blind reconcile-retry.
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
	// Determine the file path. A nib with no Path yet is new, and new nibs are
	// written into the store's data/ directory — the store root holds
	// directories and the config, never nib files.
	var path string
	if b.Path != "" {
		path = filepath.Join(c.root, b.Path)
	} else {
		b.Path = c.layout.DataRel(nib.BuildFilename(b.ID, b.Slug))
		path = filepath.Join(c.root, b.Path)
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

	// Write atomically (temp file + rename) so a crash or a concurrent reader
	// never observes a half-written nib — a torn file would fail nib.Parse on the
	// next snapshot build and surface as an OnDiskUnparseableError.
	if err := fsutil.AtomicWriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	// The bytes just written ARE b's link spelling, so this is the one write path
	// where the nib and its file are known to agree. Canonicalization re-resolves
	// every stored nib from the file's spelling (see nib.RawLinks and
	// canonicalize.go), so that mirror has to be refreshed by whoever last touched
	// the disk — including a write, which never re-reads. Miss it and a nib whose
	// link was changed by an Update keeps answering with its PRE-update spelling,
	// and the next sweep — fired by an unrelated create or delete — reverts the
	// user's edit in memory. Every persisting caller funnels through here, so the
	// obligation lives in exactly one place; keep it that way. Deliberately AFTER
	// the write: a failed write leaves the old bytes on disk, and the mirror must
	// keep describing them.
	b.CaptureRawLinks()

	return nil
}

// Delete removes a nib by exact ID match.
// Supports short IDs (without prefix) if a prefix is configured.
func (c *Core) Delete(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, err := c.acquireWriteLock()
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

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

	// Removing a key can re-point a link that already resolved: a stored
	// `parent: e1` matched the bare-token nib exactly, and with that key gone the
	// same spelling falls through to the prefixed twin `nibs-e1`. Re-resolve so
	// the stored spelling, the reverse traversals and Get all name the same nib,
	// rather than leaving the store saying one thing while Get answers another
	// (see canonicalize.go). Gated because the sweep is O(N) over the store and
	// no other removal shape can re-point anything.
	//
	// Re-pointing is NOT how a link to the removed nib gets cleared, and must not
	// become it: a link that named the nib being deleted has to go, not migrate to
	// whatever twin happens to answer to the same token. Clearing is owned by
	// RemoveLinksTo, which the only production caller — the GraphQL DeleteNib
	// resolver — runs BEFORE this, while the target is still in the store. So on
	// that path the Parent/BlockedBy links spelled with the removed token are
	// already gone, leaving the legacy Blocking field (which RemoveLinksTo does
	// not touch) as the one thing THIS REMOVAL can re-point. The sweep itself is
	// store-wide and re-resolves every nib's links, so it also rewrites short-form
	// links naming other nibs — spellings Core.Create can leave behind, unrelated
	// to what was removed. The watcher's removal branch (an external
	// delete, a pull in the separate .nibs repo) has no such partner and is where
	// the sweep earns its keep.
	//
	// Warn per rebind: a delete moving a THIRD nib's link is invisible otherwise
	// — no event is published from any direct Core mutator, and no file changes,
	// yet the next unrelated write to that bystander persists the new spelling.
	if c.removalCanRebindLinksLocked(targetID) {
		for _, rebind := range c.canonicalizeStoreLocked() {
			c.logWarn("deleting %s re-pointed %s", targetID, rebind)
		}
	}

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

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = unlock() }()

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
	if err := os.MkdirAll(c.layout.ArchiveDir(), 0755); err != nil {
		return fmt.Errorf("creating archive directory: %w", err)
	}

	// Move the file
	oldPath := filepath.Join(c.root, targetNib.Path)
	newRelPath := c.layout.ArchiveRel(filepath.Base(targetNib.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving nib to archive: %w", err)
	}

	// Update nib's path
	targetNib.Path = newRelPath
	c.nibs[targetID] = targetNib

	return nil
}

// Unarchive moves a nib from the archive directory back to the main directory.
// Supports short IDs (without prefix) if a prefix is configured.
func (c *Core) Unarchive(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = unlock() }()

	// Find the nib
	targetNib, targetID, err := c.findNibLocked(id)
	if err != nil {
		return err
	}

	// Check if not archived
	if !c.isArchivedPath(targetNib.Path) {
		return nil // Not archived, nothing to do
	}

	// Move the file back to the data directory — NOT the store root, which
	// holds no nib files: a file returned there would still exist but would
	// stop being store content, vanishing from every query on the next load.
	oldPath := filepath.Join(c.root, targetNib.Path)
	newRelPath := c.layout.DataRel(filepath.Base(targetNib.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.MkdirAll(c.layout.DataDir(), 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving nib from archive: %w", err)
	}

	// Update nib's path (forward slashes, matching Archive and loadNib)
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

// isArchivedPath returns true if the store-relative path indicates an archived nib.
func (c *Core) isArchivedPath(path string) bool {
	return c.layout.IsArchivedRel(path)
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

	archiveDir := c.layout.ArchiveDir()
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

		fileID, _ := nib.ParseFilename(entry.Name(), c.configPrefix())
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

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return nil, lockErr
	}
	defer func() { _ = unlock() }()

	// Find the nib (always loaded — archived nibs are included)
	b, targetID, err := c.findNibLocked(id)
	if err != nil {
		return nil, ErrNotFound
	}

	// If already in main directory, just return it
	if !c.isArchivedPath(b.Path) {
		return b, nil
	}

	// Move file from archive back to the data directory, for the same reason
	// Unarchive does: the store root is not store content.
	oldPath := filepath.Join(c.root, b.Path)
	newRelPath := c.layout.DataRel(filepath.Base(b.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.MkdirAll(c.layout.DataDir(), 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return nil, fmt.Errorf("moving nib from archive: %w", err)
	}

	// Update nib's path (forward slashes, matching Archive and loadNib)
	b.Path = newRelPath
	c.nibs[targetID] = b

	return b, nil
}

// Init creates the store's directories if they don't exist: the store root and
// the data/ directory every new nib is written into. archive/ is created on
// demand by the first archive, so a project that never archives keeps a store
// with nothing empty in it.
func (c *Core) Init() error {
	return os.MkdirAll(c.layout.DataDir(), 0755)
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

// Init creates the store under the given project directory if it doesn't
// exist. This is a standalone function for use before a Core is created.
func Init(dir string) error {
	return New(filepath.Join(dir, store.DirName), nil).Init()
}
