// Package nibcore provides a thread-safe in-memory store for nibs with filesystem persistence
// and optional file watching for long-running processes.
package nibcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/search"
	"github.com/alphaleonis/nibs/internal/store"
)

// ErrNotFound is an alias for nib.ErrNotFound.
var ErrNotFound = nib.ErrNotFound

// IDExistsError reports a Create refused because the caller-supplied id is
// already claimed by a file in the store, active or archived. A file whose
// content does not parse claims its id too: a load reads the id from the name.
// The CLI classifies it as a conflict.
type IDExistsError struct {
	ID string
}

func (e *IDExistsError) Error() string {
	return fmt.Sprintf("nib id %q is already claimed by a file in the store — creating it again would leave two files wearing one id", e.ID)
}

// StoreRePrefixedError reports a Create refused because the store's config
// declares a different id prefix than the one this process loaded: `nibs config
// set-prefix` completed while this create waited for the store's write lock.
// Every id this process holds is retired by that rename. The CLI classifies it
// as FILE_ERROR.
type StoreRePrefixedError struct {
	// Loaded is the prefix this process read at startup; Declared is the one
	// the store's config carries now.
	Loaded   string
	Declared string

	// LongLived selects the remedy. See Core.longLived.
	LongLived bool
}

func (e *StoreRePrefixedError) Error() string {
	// A long-lived process never reloads the config (c.config is fixed at
	// construction and no watcher reads config.yml), so a rerun there prescribes
	// an identical failure for the rest of its life.
	remedy := "rerun to work against the store as it now stands"
	if e.LongLived {
		remedy = "restart the nibs process holding this store — it read the prefix once at startup and nothing reloads it, so every later create refuses the same way"
	}
	return fmt.Sprintf("nothing was created: this store's prefix changed from %q to %q while this create waited for the store's write lock, so every id this process loaded is retired — %s",
		e.Loaded, e.Declared, remedy)
}

// ETagMismatchError is returned when an ETag validation fails.
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

// OnDiskUnparseableError reports that a nib's CURRENT on-disk state cannot be
// certified for an if-match Update (or for CurrentETag): the file exists but is
// unparseable (torn write, merge-conflict markers, a YAML typo) or unreadable
// (permission denied, torn I/O — any non-IsNotExist read error).
//
// It carries no etag token, so a client retrying with the server's Current etag
// has nothing to echo back: repair the file instead. ETagMismatchError is the
// reconcilable class.
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

	// areas is the store's declared area vocabulary, the one piece of a store's
	// configuration reloaded while the process runs (an external `nibs area
	// rename` rewrites it; see config.Areas).
	//
	// An atomic pointer, not a mutex, because the read that matters happens
	// OFF-LOCK: the GraphQL updateNib pre-check reaches ValidateArea through
	// NibValidator without holding c.mu. Every reload STORES A NEW VALUE rather
	// than mutating the old, so a reader that has loaded the pointer holds one
	// coherent vocabulary for the whole of its decision.
	areas atomic.Pointer[config.Areas]

	// lockPath is the OS-temp-dir path of the cross-process advisory write lock
	// guarding this .nibs directory. Every write mutator holds it for the duration
	// of its read-check-write so two nibs processes (or serve + a CLI) on the same
	// machine cannot both pass the etag check and clobber each other.
	lockPath string

	// In-memory state
	mu   sync.RWMutex
	nibs map[string]*nib.Nib // ID -> Nib

	// Reverse-mention index, maintained alongside c.nibs. Guarded by c.mu
	// (writers under Lock, readers under RLock) — mentionIndex itself is not
	// internally synchronized.
	mentionIdx *mentionIndex

	// Load-time integrity diagnostics from the last loadFromDisk, guarded by
	// c.mu alongside c.nibs and rebuilt from scratch on every load. Each records
	// a file that is on disk but not answerable through the store — a skipped
	// unparseable file, and the loser of an id collision — and CheckAllLinks
	// reads them back.
	unparseableFiles []UnparseableFile
	duplicateIDs     []DuplicateID

	// loadWarned is every load-time warning this process has already written,
	// guarded by c.mu alongside the diagnostics above, so a re-read of an
	// unchanged store says nothing twice. Keyed by the RENDERED message, so a
	// warning about a file that went bad between two reads still reaches the
	// reader, and fed by the warnings the budget suppressed as well as the ones
	// it emitted.
	loadWarned map[string]struct{}

	// Search index (optional, lazy-initialized)
	searchIndex SearchIndex

	// File watching (optional)
	watching bool
	done     chan struct{}

	// longLived records that this process asked to watch the store, which only a
	// holder outliving a single command does (`nibs serve`, `nibs tui`). Guarded
	// by c.mu alongside c.watching.
	//
	// Not c.watching: StartWatching sets this before it can fail and StopWatching
	// never clears it, and a serve whose watcher failed to start is the stalest
	// holder of all. It is read only to choose which remedy a refusal prescribes.
	longLived bool

	// Event subscribers (for channel-based API). Two kinds share subMu and the
	// nextSubID counter but live in separate maps: payload subscribers receive
	// cloned NibEvent batches; signal-only subscribers receive a bare struct{}
	// tick ("something changed") and never a payload.
	subscribers       map[uint64]*subscription
	signalSubscribers map[uint64]chan struct{}
	areasSubscribers  map[uint64]chan struct{}
	subMu             sync.RWMutex
	nextSubID         uint64

	// payloadSubCount mirrors len(subscribers): written under subMu alongside
	// every payload subscribe/unsubscribe, read with a bare atomic load by
	// handleChanges (which holds c.mu) to decide whether the per-nib payload
	// clone is worth paying. The established lock order is c.mu -> subMu;
	// nothing acquires c.mu while holding subMu.
	payloadSubCount atomic.Int64

	// Warning sink for non-fatal notes, defaulting to stderr through
	// safetext.Writer: these warnings interpolate FILENAMES, which on Linux are
	// arbitrary bytes. Keep the boundary on the writer, not at the call sites.
	warnWriter *safetext.Writer
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
		areasSubscribers:  make(map[uint64]chan struct{}),
		warnWriter:        safetext.NewWriter(os.Stderr),
	}
}

// acquireWriteLock takes the cross-process advisory write lock for the whole
// .nibs directory and returns a release func. Callers already hold c.mu; the
// lock order is always c.mu then this file lock. Hold it only for the span of
// one mutating operation.
//
// CANONICAL INVARIANT (the c.mu-then-flock lock order). This doc is its single
// authoritative statement; sibling comments across internal/nibcore defer here
// rather than re-derive it.
func (c *Core) acquireWriteLock() (func() error, error) {
	return c.acquireWriteLockContext(context.Background())
}

// acquireWriteLockContext is acquireWriteLock for a caller that can be told to
// stop waiting — a served mutation whose client went away. The wait for the lock
// is otherwise unbounded: the holder may be a `nibs migrate` sitting on its own
// confirmation prompt, or a `nibs config set-prefix` renaming every file in the
// store. A context carrying no deadline waits exactly as acquireWriteLock does.
//
// Cancellation reaches the wait for the FILE lock only. c.mu is a plain mutex
// every mutator takes first, and the others wait for the file lock under it with
// a context carrying no end — so an edit still queued on c.mu behind one of them
// cannot be told to stop until that one is done.
func (c *Core) acquireWriteLockContext(ctx context.Context) (func() error, error) {
	return acquireFileLockWaiting(ctx, c.lockPath)
}

// SetWarnWriter sets the writer for warning messages; nil disables warnings.
// The replacement is wrapped in the same safetext boundary the default carries.
func (c *Core) SetWarnWriter(w io.Writer) {
	if w == nil {
		c.warnWriter = nil
		return
	}
	c.warnWriter = safetext.NewWriter(w)
}

// Warn reports a non-fatal note about this store to the same sink, through the
// same safetext boundary, as the loader's per-file diagnostics. Exported for a
// surface whose return value has no room for a warning — the area mutations
// return a Config.
func (c *Core) Warn(format string, args ...any) {
	c.logWarn(format, args...)
}

// SetSearchIndex replaces the lazily-initialized Bleve index. It controls only
// the full-text leg of Search: Core unions direct ID matches on top of index
// results. Call it before Load or any concurrent operation — it is not safe for
// concurrent use.
func (c *Core) SetSearchIndex(idx SearchIndex) {
	c.searchIndex = idx
}

// maxWarningsPerBatch bounds how many per-file diagnostics one batch — a whole
// store load, or one debounce window of watcher events — writes to the warn
// writer. Every one of them is O(files): a bad merge or a `git pull` in the
// separate .nibs repository reaches a whole directory as easily as one file.
//
// It matches cmd's maxEchoedListEntries by value only; change one and look at
// the other.
const maxWarningsPerBatch = 20

// warnBudget is one batch's allowance, spent per WARNING rather than per line —
// a yaml parse error spans several lines and a line budget would cut one in
// half. It is shared across the kinds a batch emits, so a store full of one kind
// can hide the others; the closing line points at `nibs check` rather than
// claiming the stream was complete.
//
// It needs no locking: both callers iterate on one goroutine, under c.mu.
type warnBudget struct {
	c          *Core
	emitted    int
	suppressed int
	// seen, when non-nil, is the across-batches set a repeat is measured against
	// — Core.loadWarned for a store load, nil for a watcher batch. A warning
	// already in it is neither emitted nor counted as suppressed.
	seen map[string]struct{}
}

func (w *warnBudget) warn(format string, args ...any) {
	if w.seen != nil {
		// Keyed on the rendered message, not the format: the format is shared by
		// every file of one kind.
		msg := fmt.Sprintf(format, args...)
		if _, repeat := w.seen[msg]; repeat {
			return
		}
		w.seen[msg] = struct{}{}
	}
	if w.emitted >= maxWarningsPerBatch {
		w.suppressed++
		return
	}
	w.emitted++
	w.c.logWarn(format, args...)
}

// close reports what the budget held back.
func (w *warnBudget) close() {
	if w.suppressed > 0 {
		w.c.logWarn("…and %d more warning(s) suppressed; run `nibs check` for the full list of store problems", w.suppressed)
	}
}

// logWarn writes a warning if a warn writer is configured. The Flush releases
// any incomplete rune the safetext boundary holds, so no warning ends one byte
// short of what Fprintf reported written.
func (c *Core) logWarn(format string, args ...any) {
	if c.warnWriter != nil {
		_, _ = fmt.Fprintf(c.warnWriter, "warning: "+format+"\n", args...)
		_ = c.warnWriter.Flush()
	}
}

// Root returns the absolute path to the .nibs directory.
func (c *Core) Root() string {
	return c.root
}

// LockDir is the directory holding the lock files this store's mutations open,
// derived from c.lockPath. `nibs serve`'s error scrub needs it to keep a failure
// to open the lock from naming that directory (cmd/serve_pathscrub.go).
func (c *Core) LockDir() string {
	return filepath.Dir(c.lockPath)
}

// Config returns the configuration.
func (c *Core) Config() *config.Config {
	return c.config
}

// Areas returns the store's declared area vocabulary as it stands now. The
// returned value is a snapshot — a reload swaps the pointer rather than editing
// it — so call this once per decision, or two questions in one refusal may
// answer from two different vocabularies.
//
// A Core that has not been loaded answers with a nil *config.Areas; see
// config.Areas for what a nil one does.
func (c *Core) Areas() *config.Areas {
	return c.areas.Load()
}

// AreasLoadError marks the half of Core.Load that failed on the VOCABULARY. It
// carries no wording of its own — Error is the cause's, so wrapping changes
// nothing a caller prints, and errors.Is still reaches the fs and yaml sentinels
// underneath.
type AreasLoadError struct{ Cause error }

func (e *AreasLoadError) Error() string { return e.Cause.Error() }

func (e *AreasLoadError) Unwrap() error { return e.Cause }

// Load reads all nibs from disk into memory. It never writes: the load-time
// normalizations that would otherwise persist (the v0→v1 blocking migration, the
// `priority: deferred` write-back) belong to `nibs migrate`.
//
// The CLI's pre-run migration gate fires once per process, so a legacy file
// arriving through the watcher into a live serve loads exactly as written and is
// answered by every query until `nibs migrate` runs. Do not remove the legacy
// tolerance elsewhere (Render re-emitting a v0 `blocking:`) on the strength of
// that gate.
func (c *Core) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.loadLocked()
}

// loadLocked is Load with c.mu already held, for the area-vocabulary verbs:
// each holds c.mu across its whole critical section (see editArea), so the
// self-locking form would deadlock on its own mutex.
//
// The vocabulary is read BEFORE the nibs, and a malformed one aborts the load —
// it is authorization data, what an `area:` may say and what a filter may close
// over. loadAreasLocked requires c.mu, which both routes here already hold, and
// is the one writer of c.areas (watcher.go).
func (c *Core) loadLocked() error {
	if err := c.loadAreasLocked(); err != nil {
		return &AreasLoadError{Cause: err}
	}
	return c.loadFromDisk()
}

// loadFromDisk reads all nibs from disk. Must be called with c.mu held.
//
// The map and both diagnostics are built beside the ones in place and installed
// only once the walk has SUCCEEDED: c.nibs is the live store every concurrent
// query in a serve is answered from, and this runs in a request handler (an area
// mutation re-reads the store under the write lock), so clearing it first would
// empty that store for every client the moment a walk failed.
func (c *Core) loadFromDisk() error {
	nibs := make(map[string]*nib.Nib)

	// Both diagnostics describe THIS load only: collected afresh, not appended to.
	var unparseable []UnparseableFile
	var duplicates []DuplicateID

	// Every per-file warning below spends this budget; the retained diagnostics
	// above do not. The budget is fresh per load while c.loadWarned outlives it.
	if c.loadWarned == nil {
		c.loadWarned = make(map[string]struct{})
	}
	warns := &warnBudget{c: c, seen: c.loadWarned}

	// The store's CONTENT directories — data/ and archive/, dot directories
	// pruned. cmd/migrate's scans run the same per-file classifier over the whole
	// store root, so the two differ only in which directories are in scope.
	err := WalkStoreContent(c.layout, func(path string, err error) error {
		if err != nil {
			// A FIFO, socket or device named `*.md` is one bad file, not a broken
			// store: it joins the unparseable family below. It cannot share the
			// path below, which OPENS the file — opening this one does not return.
			if errors.Is(err, ErrNotRegularFile) {
				c.recordUnparseable(warns, &unparseable, path, ErrNotRegularFile)
				return nil
			}
			return err
		}

		b, loadErr := c.loadNib(path)
		if loadErr != nil {
			// Log-and-skip one unparseable file rather than aborting the walk: a
			// single malformed nib would otherwise make every nibs command fail to
			// load ANY nib. The file's bytes are left untouched.
			c.recordUnparseable(warns, &unparseable, path, loadErr)
			return nil
		}

		// Two on-disk files can parse to the same id (a slugged and a slugless
		// file for one prefixed id). WalkDir visits lexically, so the last file
		// loaded wins; the warning names both files in the walk's absolute form,
		// the retained diagnostic in nib.Path form.
		if existing, ok := nibs[b.ID]; ok {
			warns.warn("duplicate nib id %q on disk: %s shadows %s (last file loaded wins; resolve the duplicate)",
				b.ID, path, filepath.Join(c.root, existing.Path))
			duplicates = append(duplicates, DuplicateID{
				NibID:    b.ID,
				Loaded:   b.Path,
				Shadowed: existing.Path,
			})
		}

		// A diagnostic, not a normalization: the value loads exactly as written.
		// An in-memory-only fix would diverge the etag from the on-disk bytes;
		// rewriting belongs to `nibs migrate`.
		if enumErr := c.ValidateEnums(b); enumErr != nil {
			warns.warn("nib %s: %v — value loads as written; `nibs migrate` rewrites known legacy values, `nibs check` reports the rest", b.ID, enumErr)
		}

		// The axis rule is tolerant here and strict on the write paths, so every
		// later update that keeps both the type and the offending keys is
		// refused. The warning names the escape command before that is hit.
		if axisErr := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); axisErr != nil {
			axes := nibtypes.RefusedAxes(b.EffectiveType(), b.Milestone, b.Area)
			warns.warn("nib %s: %v — value loads as written, but every update that keeps the type and %s is refused; `%s` is the escape, and `nibs check` names the file",
				b.ID, axisErr, AxisKeysNoun(axes), ClearAxesCommand(b.ID, axes))
		}

		nibs[b.ID] = b
		return nil
	})
	// Closed here rather than deferred, so the elision line lands at the end of
	// the per-file stream it speaks for and on the walk's error path too.
	warns.close()
	if err != nil {
		return err
	}

	c.nibs = nibs
	c.unparseableFiles = unparseable
	c.duplicateIDs = duplicates

	// Resolve every short-form link id now that the whole map exists (see
	// canonicalize.go). MigrateV0ToV1's exact c.nibs[targetID] lookup relies on a
	// legacy `blocking:` target named by short id having been resolved here.
	c.canonicalizeAllLinksUnpublishedLocked()

	c.mentionIdx.Rebuild(c.nibs)

	// Best-effort; a failure does not fail the load. Upserting rather than
	// recreating preserves an injected SearchIndex across reloads, and stale
	// entries are filtered out by Search's read of c.nibs.
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
// diagnostic about a file that never became a nib names it the same way. A path
// that cannot be made relative falls back to the absolute form.
func (c *Core) relPathFromRoot(path string) string {
	rel, err := filepath.Rel(c.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// recordUnparseable logs a file the load skipped and retains it as a diagnostic
// for `nibs check`. Only the warning is bounded by the load's budget; the
// diagnostic is retained unconditionally.
//
// The id comes from the FILENAME, which parses whatever the contents are, so the
// diagnostic names the nib that went missing. It joins the caller's own
// collection rather than the one the store is answering from — a load installs
// what it found only once its walk has finished.
func (c *Core) recordUnparseable(warns *warnBudget, into *[]UnparseableFile, path string, reason error) {
	warns.warn("skipping unparseable nib file %s: %v", path, reason)
	id, _ := nib.ParseFilename(filepath.Base(path), c.configPrefix())
	*into = append(*into, UnparseableFile{
		NibID:  id,
		Path:   c.relPathFromRoot(path),
		Reason: reason.Error(),
	})
}

// readRegularFile is os.ReadFile through OpenRegularFile, so a path recorded at
// load time and since replaced by a FIFO fails instead of blocking. Every caller
// of this holds a Core lock while it runs.
func readRegularFile(path string) ([]byte, error) {
	f, err := OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// loadNib reads and parses a single nib file.
func (c *Core) loadNib(path string) (*nib.Nib, error) {
	// OpenRegularFile, not os.Open: this is reached from the fsnotify watcher
	// with a path no walk classified, under the write lock — an open that blocks
	// there wedges every reader in the process for as long as it lasts.
	f, err := OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	b, err := nib.Parse(f)
	if err != nil {
		return nil, err
	}

	relPath, err := filepath.Rel(c.root, path)
	if err != nil {
		return nil, err
	}
	b.Path = filepath.ToSlash(relPath)

	filename := filepath.Base(path)
	b.ID, b.Slug = nib.ParseFilename(filename, c.configPrefix())

	// Type and Priority are deliberately not defaulted here: synthesizing them in
	// memory while computeStoredETag bare-parses the file would diverge the
	// in-memory ETag() from the stored etag, false-conflicting a valid if-match
	// Update with no on-disk change. The "task"/"normal" defaults are applied at
	// the consumption boundary via nib.EffectiveType()/EffectivePriority().
	//
	// The empty-slice defaults below are etag-safe: Render's omitempty treats a
	// nil and an empty slice identically.
	if b.Tags == nil {
		b.Tags = []string{}
	}
	if b.BlockedBy == nil {
		b.BlockedBy = []string{}
	}
	if b.Documents == nil {
		b.Documents = []string{}
	}
	// created_at/updated_at are synthesized here, unlike Type/Priority: the mtime
	// fallback needs the file's stat, unavailable at the consumption boundary.
	// The synthesized stamp makes the in-memory nib render bytes the file does
	// not carry; computeStoredETag reconciles that back out (see
	// reconcileLoaderDerived).
	//
	// Every branch below leaves the two stamps carrying the SAME value, and the
	// reconciliation depends on that: it is the only thing distinguishing a stamp
	// synthesized here from one deleted out of the file after it was loaded.
	if b.CreatedAt == nil {
		if b.UpdatedAt != nil {
			b.CreatedAt = b.UpdatedAt
		} else {
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

// ensureSearchIndexLocked initializes the in-memory search index if not already
// created. Must be called with c.mu held.
func (c *Core) ensureSearchIndexLocked() error {
	if c.searchIndex != nil {
		return nil
	}

	idx, err := search.NewIndex()
	if err != nil {
		return fmt.Errorf("initializing search index: %w", err)
	}

	c.searchIndex = idx

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
// appears once, in the ID-match position. Each leg is independently capped at
// DefaultSearchLimit. The index is lazily initialized on first use.
//
// A caller that intersects the answer with a working set it already bounded
// wants SearchAll instead.
func (c *Core) Search(query string) ([]*nib.Nib, error) {
	return c.search(query, DefaultSearchLimit)
}

// SearchAll returns every nib matching the query, in the same order Search uses,
// with neither leg capped. For an INTERSECTION — "the children of X matching q"
// — the caller's relation is the bound, and feeding it from the store's global
// top-N silently drops a member that ranks below the cutoff.
func (c *Core) SearchAll(query string) ([]*nib.Nib, error) {
	return c.search(query, Unlimited)
}

// search is the shared body of Search and SearchAll. limit caps each leg
// independently; a limit <= 0 (Unlimited) means no cap on either.
func (c *Core) search(query string, limit int) ([]*nib.Nib, error) {
	// Write lock for the lazy init.
	c.mu.Lock()
	if err := c.ensureSearchIndexLocked(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	// Capture the index under the lock.
	idx := c.searchIndex
	c.mu.Unlock()

	// Perform search outside the lock (Bleve is thread-safe)
	ids, err := idx.Search(query, limit)
	if err != nil {
		return nil, err
	}

	// Read lock for the map read.
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
// capped at limit (<= 0 means uncapped). The Bleve `id` field is unanalyzed, so
// query-string terms never match it in the full-text leg.
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
	// Cap after sorting, so the kept set is deterministic.
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// minIDFragmentLen is the minimum query length for the short-ID substring branch
// of (idQueryMatcher).matches. A 1-char query matches ~10% of all short IDs.
// Quoted in schema.graphqls (NibFilter.search — regenerate after editing) and in
// cmd/list.go's --search help; update those when changing this value.
const minIDFragmentLen = 2

// normalizeSearchQuery trims surrounding whitespace (pasted IDs commonly carry a
// trailing space) and lowercases for case-insensitive comparison.
func normalizeSearchQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// isIDFragment reports whether a normalized query consists solely of short-ID
// characters, as gated by nib.IsIDChar. Hyphens are excluded: they belong to
// prefixes, not short IDs, and admitting them would let a Bleve operator query
// like `-42` substring-match a foreign-prefix ID whose short form keeps a hyphen
// (`task-42` under prefix `nibs-`). Such an ID is still findable through
// matches' prefix branch and its exact-equality escape.
func isIDFragment(query string) bool {
	for i := 0; i < len(query); i++ {
		if !nib.IsIDChar(query[i]) {
			return false
		}
	}
	return true
}

// matchesIDQuery reports whether a search query matches a nib ID,
// case-insensitively. A query equal to the full ID or the short ID matches; a
// query starting with the configured prefix (with a non-empty remainder) must be
// a prefix of the full ID; any other query of at least minIDFragmentLen
// characters matches as a substring of the short ID.
//
// Queries with internal whitespace or Bleve operators cannot match — do not
// tokenize the query here.
//
// Test-only seam with no production callers: it delegates through prepareIDQuery
// so table tests exercise the logic idMatchesLocked runs per nib (cf. the oracle
// convention in mentions.go).
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

	// Bypasses the fragment charset gate below, so a foreign-prefix ID whose
	// short form keeps a hyphen stays findable by its own full ID.
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

// Get finds a nib by exact ID match, prepending the configured prefix when the
// query does not already carry it (with prefix "nibs-", Get("abc") matches
// "nibs-abc" but Get("ab") does not). It returns the LIVE store pointer — see
// GetSnapshot for a detached copy.
func (c *Core) Get(id string) (*nib.Nib, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if b, ok := c.nibs[id]; ok {
		return b, nil
	}

	if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		if b, ok := c.nibs[c.config.Nibs.Prefix+id]; ok {
			return b, nil
		}
	}

	return nil, ErrNotFound
}

// GetForUpdate returns a deep copy of the nib the caller OWNS and may freely
// mutate before handing it to Update; mutating it never touches store state, so
// a rejected Update leaves no phantom mutation behind. Resolution mirrors Get;
// ErrNotFound when the nib is missing.
//
// The Clone is taken WHILE c.mu is held (via GetSnapshot), so the struct copy's
// field reads — notably Path — cannot race an in-place Path writer holding c.mu
// (Archive/Unarchive). Cloning the shared pointer off-lock would race that
// writer.
func (c *Core) GetForUpdate(id string) (*nib.Nib, error) {
	if b, ok := c.GetSnapshot(id); ok {
		return b, nil
	}
	return nil, ErrNotFound
}

// GetSnapshot returns a detached deep copy of the nib, cloned WHILE c.mu is
// held, so the returned value never aliases the live store pointer and no field
// (notably Path) is read off-lock. Use it when the result outlives the lock —
// GraphQL relationship resolvers whose fields gqlgen marshals asynchronously,
// concurrently with in-place mutations like Archive/Unarchive rewriting a stored
// nib's Path. Get returns the live pointer. Resolution mirrors Get; ok is false
// when the nib is absent.
func (c *Core) GetSnapshot(id string) (*nib.Nib, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if b, ok := c.nibs[id]; ok {
		return b.Clone(), true // clone under the lock, never after it
	}
	if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		if b, ok := c.nibs[c.config.Nibs.Prefix+id]; ok {
			return b.Clone(), true
		}
	}
	return nil, false
}

// NormalizeID resolves a potentially short ID to its full form, prepending the
// configured prefix when the query does not carry it. Returns the full ID and
// true if found, or the original ID and false if not.
//
// Resolution lives in normalizeIDInMap; change behavior there (see mentions.go
// for its other callers).
func (c *Core) NormalizeID(id string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if full, ok := normalizeIDInMap(c.nibs, id, c.configPrefix()); ok {
		return full, true
	}
	return id, false
}

// ValidateEnums checks that the nib's enum fields (type, status, priority,
// estimate) hold either the empty "unset -> use default" sentinel or a value
// valid under the current config. Only non-empty values are checked. It no-ops
// when no config is set.
//
// It reads no store state — only the nib passed in and the config's enum tables,
// which are package-level (DefaultStatuses and friends) rather than held
// per-config. So it takes no lock: Create and Update call it under c.mu, and the
// GraphQL updateNib pre-check calls it off-lock through NibValidator.
//
// The immutability relied on is the tables', not the config's: the c.config
// POINTER is fixed at construction, but the struct behind it is live and
// writable — `nibs config set-prefix` assigns cfg.Nibs.Prefix in place. A future
// read of a per-config field here would need its own off-lock argument.
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

// ValidateArea checks the nib's `area:` assignment against the vocabulary the
// store DECLARES: unset is legal, a declared path is legal, anything else is
// refused naming the declared set.
//
// It asks ValidateStored, which judges whatever `area:` the nib will carry — for
// most writes the one it already carries. Create asks ValidateAssignment
// instead: a create has no stored value its argument could be confused with.
//
// Read tolerance: a file already carrying an undeclared area loads, lists and
// renders exactly as written. Only a write refuses it; CheckAllLinks calls this
// off the READ path to report it.
//
// Like ValidateEnums it takes no lock; unlike ValidateEnums it reads mutable
// state. The vocabulary is RELOADED while the process runs, so c.Areas() is an
// atomic load rather than a field read, and a reload publishes a whole new
// vocabulary rather than editing the one a reader is holding — which is what
// makes the GraphQL updateNib pre-check's off-lock call safe.
//
// rewriteAreaAssignmentsLocked, the rename cascade, is not a caller: no single
// vocabulary declares both the value a member is leaving and the one it is
// arriving at.
func (c *Core) ValidateArea(b *nib.Nib) error {
	return c.Areas().ValidateStored(b.ID, b.Area)
}

// storedIDs is the set of nib ids that have a FILE in the store right now. It
// reads NAMES only: a nib's id comes from its file name on every load.
//
// c.nibs answers for the store as THIS process last read it, and a create has to
// decide against the store as it stands — a concurrent `nibs serve` and `nibs
// new` each hold a map with no trace of the other's pending nib. Callers must
// hold the cross-process write lock, so no cooperating nibs process writes
// between this read and the decision it feeds.
//
// The walk is WalkStoreContent, the same enumeration Load uses. A file whose
// content does not parse is taken here while Load leaves it out of c.nibs: the
// name is what claims the id, and issuing it again would turn repairing the file
// into a second nib wearing it.
func (c *Core) storedIDs() (map[string]struct{}, error) {
	prefix := c.configPrefix()
	ids := make(map[string]struct{})
	err := WalkStoreContent(c.layout, func(path string, err error) error {
		if err != nil {
			if errors.Is(err, ErrNotRegularFile) {
				return nil
			}
			return err
		}
		if id, _ := nib.ParseFilename(filepath.Base(path), prefix); id != "" {
			ids[id] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
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
	// The axis rule runs first: a milestone takes no area at all, so "must be one
	// of …" would hand back a remedy the subject cannot follow.
	if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
		return err
	}
	// ValidateAssignment, not ValidateArea: a create has no stored `area:`.
	if err := c.Areas().ValidateAssignment(b.Area); err != nil {
		return err
	}

	// The prefix the nib's file will be READ BACK under, for the round-trip check
	// below. A minted id is checked under mintingVocabulary's prefix — the store's
	// DECLARED one, which is what a later load parses with — because a Core built
	// with no config against a store that declares one has c.configPrefix() "".
	// A caller-supplied id has no such reading and is checked under the loaded
	// prefix, the same expression loadNib parses filenames with.
	readBackPrefix := c.configPrefix()

	// Redraw on a collision with a live id — active or archived, both are in
	// c.nibs. 4-char ids give ~1.7M combinations, so at hundreds of nibs a single
	// draw carries real birthday odds. The bound turns a broken generator into an
	// error instead of a hang.
	//
	// Both branches consult the STORE as well as the map: the map is only as
	// fresh as this process's last read of the store (see storedIDs).
	if b.ID == "" {
		prefix, length, err := c.mintingVocabulary()
		if err != nil {
			return err
		}
		readBackPrefix = prefix
		onDisk, err := c.storedIDs()
		if err != nil {
			return err
		}
		for range 100 {
			id := newNibID(prefix, length)
			if _, exists := c.nibs[id]; exists {
				continue
			}
			if _, exists := onDisk[id]; exists {
				continue
			}
			b.ID = id
			break
		}
		if b.ID == "" {
			return fmt.Errorf("could not generate a free nib id in 100 draws — the id space (length %d) is exhausted or the generator is broken; raise nibs.id_length", length)
		}
	} else if _, exists := c.nibs[b.ID]; exists {
		// Refused, not replaced: c.nibs[b.ID] = b below would shadow the existing
		// nib in memory and leave two files claiming one id on disk.
		return &IDExistsError{ID: b.ID}
	} else {
		// The map said free; the STORE gets the last word (see storedIDs).
		onDisk, err := c.storedIDs()
		if err != nil {
			return err
		}
		if _, exists := onDisk[b.ID]; exists {
			return &IDExistsError{ID: b.ID}
		}
	}

	// The id is about to become a path, so it has to be a plain file name AND a
	// name that reads back as this id. A create is the only place a file name is
	// minted from an id and a slug, and nothing downstream stands in for either
	// check.
	//
	// Path shape: filepath.Join CLEANS what it is given rather than refusing it,
	// so a separator in the prefix writes the nib outside the store (`../../`) or
	// buries it in a subdirectory whose name the id loses on the next load.
	//
	// Grammar: BuildFilename joins id and slug with "--", so a prefix carrying
	// its own "--" or "." makes ParseFilename split inside the id, and the store
	// comes back holding a nib nobody can name.
	if err := nib.ValidateIDForFilename(b.ID); err != nil {
		return err
	}
	if err := nib.ValidateIDRoundTrip(b.ID, b.Slug, readBackPrefix); err != nil {
		return err
	}

	now := time.Now().UTC().Truncate(time.Second)
	b.CreatedAt = &now
	b.UpdatedAt = &now

	if err := c.saveToDisk(b); err != nil {
		return err
	}

	c.nibs[b.ID] = b

	// An id ARRIVING can re-point a link another nib already holds: a short form
	// left verbatim because nothing answered to it starts resolving, and a bare
	// token arriving alongside its prefixed twin takes a link the twin was
	// answering. The watcher cannot cover for this — the in-process insert above
	// happens BEFORE fsnotify reports the file. See canonicalize.go.
	//
	// Copy-on-write, like every other mutator that rewrites a non-Path field. An
	// in-place edit would be safe HERE in isolation, but the exception list the
	// off-lock read pipeline rests on (NibReader.GetSnapshot in
	// internal/graph/interfaces.go) is exhaustive and stays closed.
	//
	// The caller's own pointer keeps the spelling it passed in: sibling mutators
	// rewrite the STORE, not the caller's object.
	if set := canonicalizeLinksInMap(c.nibs, b, c.configPrefix()); set.changed {
		resolved := b.Clone()
		set.applyTo(resolved)
		c.nibs[b.ID] = resolved
	}
	// Warn per rebind, as Core.Delete does: a create moving a THIRD nib's link
	// changes no file and publishes no event, yet the next unrelated write to
	// that bystander persists the new spelling.
	for _, rebind := range c.canonicalizeStoreLocked() {
		c.logWarn("creating %s re-pointed %s", b.ID, rebind)
	}

	c.mentionIdx.Add(b.ID, b.Body)

	// Update search index if active (best-effort, don't fail create)
	if c.searchIndex != nil {
		if err := c.searchIndex.IndexNib(b); err != nil {
			c.logWarn("failed to index nib %s: %v", b.ID, err)
		}
	}

	return nil
}

// mintingVocabulary is the prefix and id length a NEW nib id is drawn under, or
// a StoreRePrefixedError when the store's declared prefix moved under this
// process. It re-reads the store's config from disk rather than trusting
// c.config, which this process loaded before it took the store's write lock.
// Called with c.mu and the write lock held, so nothing can supersede the value
// between this read and the file the create writes.
//
// `nibs config set-prefix` renames every nib file and rewrites nibs.prefix under
// that same lock, so a `nibs new` that parked behind one resumes into a store
// whose config already declares the new prefix. A nib's id derives from its
// filename, so minting from the loaded copy is a permanent misnaming.
//
// A divergence is REFUSED rather than followed: c.nibs is keyed by the ids this
// process loaded, every one of them retired by the rename, so Create's collision
// guard and its redraw loop go blind exactly there — a draw landing on a renamed
// nib leaves a second file claiming its id, or renames over it. Whoever can act
// on it is told instead (see StoreRePrefixedError.LongLived).
//
// The re-read is LOCAL to this one decision and c.config is left alone. c.config
// is read OFF-LOCK across the package (ValidateEnums, configPrefix and its
// callers) and handed out raw by Config(), all resting on the pointer being
// fixed at construction; swapping or mutating it here would race every one of
// those readers, which the -race gate on this package exists to catch. It reads
// <root>/config.yml through the same derivation resolveCLIStore uses.
//
// Three things are NOT a re-prefix, and each leaves the loaded values in place:
// an ABSENT config (a store need not have one); a config declaring NO prefix
// (nothing that empties the field renames a file — `nibs config set-prefix`
// validates through reprefix.ValidatePrefix, which requires at least one
// character plus the separator dash); and a config that cannot be read or
// parsed, which warns.
//
// Absence is read off the LOAD, via Config.LoadedFromFile, never off a stat of
// this function's own: a config.yml removed between the two syscalls passes the
// stat, reads as absent, and makes every create refuse a re-prefix to "" that
// never happened.
//
// A Core holding NO config adopts the stored vocabulary: it has no loaded prefix
// to have diverged FROM, and c.nibs is keyed by ids read off the filenames, so a
// draw lands in the space the guard below checks.
func (c *Core) mintingVocabulary() (string, int, error) {
	prefix := ""
	length := 4
	if c.config != nil {
		prefix = c.config.Nibs.Prefix
		if c.config.Nibs.IDLength > 0 {
			length = c.config.Nibs.IDLength
		}
	}

	configPath := c.layout.ConfigPath()
	stored, err := config.LoadStoreWithUserConfig(c.root)
	if err != nil {
		// A failed read is evidence of nothing, least of all that the prefix
		// changed; a silent fallback would look like the misnaming this catches.
		c.logWarn("could not re-read %s while minting a nib id (%v); using the prefix %q and id length %d this process loaded", configPath, err, prefix, length)
		return prefix, length, nil
	}
	if !stored.LoadedFromFile() {
		return prefix, length, nil
	}
	if stored.Nibs.Prefix != "" {
		if c.config != nil && stored.Nibs.Prefix != prefix {
			return "", 0, &StoreRePrefixedError{Loaded: prefix, Declared: stored.Nibs.Prefix, LongLived: c.longLived}
		}
		prefix = stored.Nibs.Prefix
	}
	if stored.Nibs.IDLength > 0 {
		length = stored.Nibs.IDLength
	}
	return prefix, length, nil
}

// CurrentETag returns the canonical ETag for the nib's on-disk content — a hash
// of the parsed file's canonical Render(), so it agrees with the in-memory
// nib.ETag() across benign formatting drift. Returns ErrNotFound when the id
// does not resolve. It falls back to the in-memory etag only when no on-disk
// file exists yet (empty Path, or os.IsNotExist); an existing file that cannot
// be read or parsed fails CLOSED — see computeStoredETag's matrix.
func (c *Core) CurrentETag(id string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	storedNib, ok := c.nibs[id]
	if !ok {
		// Prefix-resolution, as Get does.
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
// PARSING the current on-disk file and returning the parsed nib's ETag — a hash
// of its canonical Render(), not of the raw disk bytes. That makes the stored
// etag equal the in-memory nib.ETag() whenever the on-disk content is
// canonically equivalent, so an if-match survives benign formatting drift yet
// still fails on genuine divergence, including divergence outside Render()'s
// modeled fields (unknown YAML keys, a legacy v0 `blocking:` line), which
// nib.Render preserves. Caller must hold c.mu (read or write lock).
//
// Parsing is the bare nib.Parse with only the ID copied over from the stored nib
// so the rendered `# <id>` header matches. It deliberately does NOT go through
// loadNib, but the two agree on Type/Priority, which loadNib leaves empty when
// the file omits them. Of the three things loadNib DERIVES rather than reads,
// the empty-slice defaults are etag-safe and the created_at/updated_at fallback
// is reconciled explicitly — see reconcileLoaderDerived.
//
// Fallback discipline when the canonical render cannot be computed from disk.
// Each branch is chosen deliberately as fail-OPEN (return the in-memory ETag
// with a nil error, so a normal if-match still matches) or fail-CLOSED (return a
// non-reconcilable *OnDiskUnparseableError and NO etag token, so Update and
// CurrentETag refuse the overwrite and no retry-with-Current satisfies the
// guard).
//
//	condition                          verdict  returns                      logged?
//	---------------------------------  -------  ---------------------------  -------
//	empty Path                         OPEN     (in-memory ETag(), nil)      no  (not flushed yet)
//	read err, os.IsNotExist            OPEN     (in-memory ETag(), nil)      no  (not flushed / P2 delete-race)
//	read err, other (perms, torn I/O)  CLOSED   ("", OnDiskUnparseableError) no  (returned; caller surfaces)
//	parse err (corrupt/conflict/typo)  CLOSED   ("", OnDiskUnparseableError) no  (returned; caller surfaces)
//	parsed OK                          --       (canonical b.ETag(), nil)    --
//
// Nothing is logged here. The OPEN branches are the normal freshly-created and
// accepted-delete-race paths; the CLOSED ones RETURN a distinct error for the
// caller to surface, and logging as well would flood stderr on the hot Children
// read path (orderer.go's backfillKeys re-attempts the Update once per read for
// a persistently uncertifiable sibling). Caller must hold c.mu (read or write
// lock).
func (c *Core) computeStoredETag(storedNib *nib.Nib) (string, error) {
	if storedNib.Path == "" {
		return storedNib.ETag(), nil
	}
	diskPath := filepath.Join(c.root, storedNib.Path)
	raw, err := readRegularFile(diskPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Not flushed yet (freshly created) or externally removed (accepted
			// delete-race): fall back to the in-memory etag, matching Update.
			return storedNib.ETag(), nil
		}
		// The file EXISTS but its bytes cannot be READ. Fail CLOSED with a
		// non-reconcilable error carrying no etag token; the caller surfaces it.
		return "", &OnDiskUnparseableError{ID: storedNib.ID, Path: storedNib.Path, Reason: "unreadable", Err: err}
	}

	b, err := nib.Parse(bytes.NewReader(raw))
	if err != nil {
		// The file EXISTS but is unparseable (torn write, merge-conflict markers,
		// a YAML typo). Fail CLOSED so a blind retry cannot clobber it.
		return "", &OnDiskUnparseableError{ID: storedNib.ID, Path: storedNib.Path, Reason: "unparseable", Err: err}
	}
	c.reconcileLoaderDerived(b, storedNib)
	return b.ETag(), nil
}

// reconcileLoaderDerived brings a bare parse of a nib's file into the form the
// STORE holds it in, for the three fields loadNib DERIVES rather than reads. A
// value the file does not carry cannot be evidence that the file diverged from
// the store, because the store did not read it there either.
//
//   - id — derived from the FILENAME, and the render includes it as the
//     `# <id>` header.
//   - short-form link ids — resolved against the prefix at load time (see
//     canonicalize.go), so a hand-edited `parent: par` is held as
//     `parent: nibs-par`.
//   - created_at/updated_at — loadNib synthesizes a stamp the file omits (from
//     the other stamp, else from the file's mtime), so a hand-authored file
//     renders stamps its own bytes do not carry.
//
// The stamps are the one of the three that needs a bound: an id and a link are
// absent from the bytes BY CONSTRUCTION, while a stamp can also be absent
// because someone DELETED it after this process loaded the file. See
// loaderMaySynthesizeStamps for what separates the two.
//
// Caller must hold c.mu (read or write lock), which canonicalizeLinksInMap's
// read of c.nibs requires.
func (c *Core) reconcileLoaderDerived(parsed, storedNib *nib.Nib) {
	parsed.ID = storedNib.ID

	if set := canonicalizeLinksInMap(c.nibs, parsed, c.configPrefix()); set.changed {
		set.applyTo(parsed)
	}

	if loaderMaySynthesizeStamps(storedNib) {
		if parsed.CreatedAt == nil {
			parsed.CreatedAt = copyStamp(storedNib.CreatedAt)
		}
		if parsed.UpdatedAt == nil {
			parsed.UpdatedAt = copyStamp(storedNib.UpdatedAt)
		}
	}
}

// loaderMaySynthesizeStamps reports whether storedNib's timestamps are in the
// only shape loadNib's fallback can leave behind: every one of its branches
// assigns one stamp FROM the other, or derives both from the file's mtime, so
// the pair always comes out EQUAL. A nib holding two DIFFERENT stamps read both
// from its file, so a stamp now missing from that file was deleted rather than
// synthesized — divergence the etag must report, or Update writes the stale
// clone back and restores the deleted key (it re-stamps updated_at on every
// write, but never assigns created_at).
//
// The residual: a nib whose stamps are equal is indistinguishable from one whose
// stamps were synthesized, so a stamp deleted from ITS file is invisible to the
// etag and is refilled by the next if-match write. Not a race — a reload
// re-synthesizes the deleted stamp from the surviving equal one and lands on the
// identical render — and not confined to hand-authored files: Core.Create
// assigns ONE now to both stamps, so every nib nibs has created and not yet
// updated is in this set.
//
// This rule governs computeStoredETag, so it reaches every if-match comparison
// in the product.
func loaderMaySynthesizeStamps(storedNib *nib.Nib) bool {
	return storedNib.CreatedAt != nil && storedNib.UpdatedAt != nil &&
		storedNib.CreatedAt.Equal(*storedNib.UpdatedAt)
}

// copyStamp returns an independent copy of a timestamp pointer, so a reconciled
// bare parse never aliases the live stored nib's own stamp.
func copyStamp(stamp *time.Time) *time.Time {
	if stamp == nil {
		return nil
	}
	t := *stamp
	return &t
}

// Update modifies an existing nib and writes it to disk. A non-nil ifMatch is
// validated against the current on-disk etag first — optimistic concurrency
// control against a lost update.
//
// THE CALLER'S b IS A SNAPSHOT taken BEFORE this call, and two things about it
// can be stale by the time the write happens — its PATH and its CONTENT. They
// get different answers.
//
// The PATH is re-derived under the lock: the write goes to the path the STORE
// holds for this id, not the one the snapshot carries. Every writer that moves a
// nib's file does so under this same c.mu — the in-place Path writers the
// canonical live-pointer invariant enumerates (see NibReader.GetSnapshot in
// internal/graph/interfaces.go), plus the watcher's slug-rename branch, which
// installs a fresh pointer carrying the new Path. And the writer is the
// non-creating one, so a path that went stale in a way no process in this one
// can re-derive — `nibs config set-prefix` renames every file in the store — is
// reported rather than turned into a second copy of the nib at its old name.
//
// The CONTENT cannot be re-derived here, and that is the residual. This method
// takes c.mu and then parks on the store flock while holding it, so a migration
// holding that flock keeps the watcher — which needs c.mu — from refreshing
// c.nibs with the migrated files first. When the migration releases, an Update
// WITH ifMatch fails safe (the stored etag no longer matches), but one with NO
// ifMatch writes b's pre-migration render straight back over the same file,
// erasing e.g. a freshly transferred blocked_by edge; the source file is already
// stamped v1, so no migration detect fires again and the loss is silent. No lock
// ordering inside this method makes a pre-migration clone current again, so that
// chain is broken one level up: AcquireServeExclusion fences a migration out of
// a live serve entirely (servelock.go). What remains is a serve from a release
// that predates that interlock, which does not take the lock and so cannot be
// fenced; `nibs migrate` names exactly that case before it applies. The caller's
// own guard is an ifMatch, which the web UI's batch mutations send.
func (c *Core) Update(b *nib.Nib, ifMatch *string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = unlock() }()

	storedNib, ok := c.nibs[b.ID]
	if !ok {
		return ErrNotFound
	}

	// Input validity is independent of the etag precondition, so it runs first.
	if err := c.ValidateEnums(b); err != nil {
		return err
	}
	// Ordered as in Create, and for the same reason.
	if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
		return err
	}
	if err := c.ValidateArea(b); err != nil {
		return err
	}

	requireIfMatch := c.config != nil && c.config.Nibs.RequireIfMatch

	if requireIfMatch && (ifMatch == nil || *ifMatch == "") {
		return &ETagRequiredError{}
	}

	if ifMatch != nil && *ifMatch != "" {
		currentETag, err := c.computeStoredETag(storedNib)
		if err != nil {
			// The on-disk state cannot be certified. Surface the non-reconcilable
			// error rather than an ETagMismatchError: no server etag satisfies it.
			return err
		}
		if currentETag != *ifMatch {
			return &ETagMismatchError{
				Provided: *ifMatch,
				Current:  currentETag,
			}
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	b.UpdatedAt = &now

	// The file to write is the one the STORE says this nib lives in, not the one
	// the caller's clone remembers: every writer that moves a nib's file does so
	// under this same c.mu, so a clone taken before one of them ran names a file
	// that has since moved.
	b.Path = storedNib.Path

	// Non-creating: a creating write would leave a second copy of the nib at a
	// path another process has since renamed (see updateOnDiskDeferDirSync).
	if err := c.updateOnDisk(b); err != nil {
		return fmt.Errorf("%s: %w", b.ID, err)
	}

	c.nibs[b.ID] = b

	c.mentionIdx.Replace(b.ID, b.Body)

	// Update search index if active (best-effort, don't fail update)
	if c.searchIndex != nil {
		if err := c.searchIndex.IndexNib(b); err != nil {
			c.logWarn("failed to update nib %s in search index: %v", b.ID, err)
		}
	}

	return nil
}

// saveToDisk writes a nib to the filesystem and flushes the directory entry
// before returning, so a single write is as durable as fsutil.AtomicWriteFile
// makes it. A caller writing MANY nibs wants saveToDiskDeferDirSync plus
// an fsutil.DirSyncBatch instead: the flush is per directory, not per file.
func (c *Core) saveToDisk(b *nib.Nib) error {
	dir, err := c.saveToDiskDeferDirSync(b)
	if err != nil {
		return err
	}
	fsutil.SyncDir(dir)
	return nil
}

// saveToDiskDeferDirSync writes a nib to the filesystem without flushing the
// directory entry, returning the directory that still needs one (empty when the
// write failed before its rename). Every caller owes that directory a flush —
// fsutil.DirSyncBatch collects them for a bulk loop. See
// fsutil.AtomicWriteFileDeferDirSync for the guarantee that holds until then.
func (c *Core) saveToDiskDeferDirSync(b *nib.Nib) (string, error) {
	path := c.nibFilePath(b)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating directory: %w", err)
	}

	return c.renderAndWriteDeferDirSync(b, path, fsutil.AtomicWriteFileDeferDirSync)
}

// updateOnDisk is saveToDisk for a caller REPLACING a nib's existing file: it
// refuses a path nothing is at, and flushes the directory entry before
// returning. A caller replacing MANY files wants updateOnDiskDeferDirSync plus
// an fsutil.DirSyncBatch.
func (c *Core) updateOnDisk(b *nib.Nib) error {
	dir, err := c.updateOnDiskDeferDirSync(b)
	if err != nil {
		return err
	}
	fsutil.SyncDir(dir)
	return nil
}

// updateOnDiskDeferDirSync is saveToDiskDeferDirSync for a caller REPLACING a
// nib's existing file: it refuses a path nothing is at instead of creating one
// there. A bulk rewrite holds the path each nib carried when this process loaded
// the store, and `nibs config set-prefix` renames every one of them; a creating
// write would turn the leftover path into a second copy of the nib. See
// fsutil.AtomicUpdateFileDeferDirSync for what the refusal promises.
//
// It does NOT MkdirAll: an existing file's directory exists.
func (c *Core) updateOnDiskDeferDirSync(b *nib.Nib) (string, error) {
	return c.renderAndWriteDeferDirSync(b, c.nibFilePath(b), fsutil.AtomicUpdateFileDeferDirSync)
}

// nibFilePath resolves the absolute file a nib's bytes belong in, and ASSIGNS
// b.Path for a nib that has none: new nibs go in the store's data/ directory,
// never at the store root.
func (c *Core) nibFilePath(b *nib.Nib) string {
	if b.Path == "" {
		b.Path = c.layout.DataRel(nib.BuildFilename(b.ID, b.Slug))
	}
	return filepath.Join(c.root, b.Path)
}

// renderAndWriteDeferDirSync renders a nib and commits it through write, which
// is one of fsutil's two deferred-flush writers — the creating one for a save,
// the refusing one for an update.
func (c *Core) renderAndWriteDeferDirSync(b *nib.Nib, path string, write func(string, []byte, os.FileMode) (string, error)) (string, error) {
	content, err := b.Render()
	if err != nil {
		return "", err
	}

	// Atomic (temp file + rename), so a crash or a concurrent READER never
	// observes a half-written nib.
	//
	// The mode is the user's, not this writer's: the rename carries nothing of
	// the file it replaces, so a mode the user tightened survives only by being
	// read back below and passed through. For a file that has never existed,
	// fsutil.ModeForNewFile applies the umask this writer's Chmod would otherwise
	// bypass, over a 0644 base rather than 0666 — masking only clears bits, so a
	// permissive umask cannot hand out a group- or world-WRITABLE nib.
	//
	// A stat failure that is not "absent" is reported rather than defaulted: a
	// default could only widen a nib whose real mode was narrower.
	perm := fsutil.ModeForNewFile(0644)
	switch info, statErr := os.Stat(path); {
	case statErr == nil:
		perm = info.Mode().Perm()
	case !errors.Is(statErr, fs.ErrNotExist):
		return "", fmt.Errorf("reading the current mode of %s: %w", path, statErr)
	}

	unflushedDir, err := write(path, content, perm)
	if err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	// The bytes just written ARE b's link spelling. Canonicalization re-resolves
	// every stored nib from that mirror (see nib.RawLinks and canonicalize.go),
	// so whoever last touched the disk refreshes it — miss it and the next sweep,
	// fired by an unrelated create or delete, reverts the user's edit in memory.
	// Every persisting caller funnels through here; keep it that way. It runs
	// AFTER the write: a failed write leaves the old bytes on disk, and the
	// mirror must keep describing them.
	b.CaptureRawLinks()

	return unflushedDir, nil
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

	targetID := id
	targetNib, ok := c.nibs[id]

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

	path := filepath.Join(c.root, targetNib.Path)
	if err := os.Remove(path); err != nil {
		return err
	}

	delete(c.nibs, targetID)

	// Removing a key can re-point a link that already resolved: a stored
	// `parent: e1` matched the bare-token nib exactly, and with that key gone the
	// same spelling falls through to the prefixed twin `nibs-e1`. Re-resolve so
	// the stored spelling, the reverse traversals and Get all name the same nib
	// (see canonicalize.go). Gated because the sweep is O(N) over the store.
	//
	// Re-pointing is NOT how a link to the removed nib gets cleared, and must not
	// become it: a link that named the nib being deleted has to go, not migrate
	// to whatever twin answers to the same token. Clearing is RemoveLinksTo's,
	// which the only production caller — the GraphQL DeleteNib resolver — runs
	// BEFORE this, while the target is still in the store.
	//
	// Warn per rebind: a delete moving a THIRD nib's link changes no file and
	// publishes no event, yet the next unrelated write persists the new spelling.
	if c.removalCanRebindLinksLocked(targetID) {
		for _, rebind := range c.canonicalizeStoreLocked() {
			c.logWarn("deleting %s re-pointed %s", targetID, rebind)
		}
	}

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
//
// THE MOVE IS A BARE RENAME WITH NO DIRECTORY FSYNC ON EITHER SIDE; Unarchive
// and LoadAndUnarchive follow the same rule. No bytes are written here, so what
// a crash can cost is a nib's LOCATION: the file lands at one path or the other
// and isArchivedPath reads that path, so a reloaded store agrees with the disk
// either way. The outcome that would hurt — a torn rename leaving one id at two
// paths — is not one two after-the-fact fsyncs could order, and loadFromDisk
// already reports it as a duplicate id.
//
// This matches what the rest of the store promises: fsutil.AtomicWriteFile
// declares its directory fsync best-effort and Windows refuses one outright, so
// no directory entry here is promised to survive a crash. A recovery path must
// not key on "the file is at its new path".
func (c *Core) Archive(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = unlock() }()

	targetNib, targetID, err := c.findNibLocked(id)
	if err != nil {
		return err
	}

	if c.isArchivedPath(targetNib.Path) {
		return nil // already archived
	}

	if err := os.MkdirAll(c.layout.ArchiveDir(), 0755); err != nil {
		return fmt.Errorf("creating archive directory: %w", err)
	}

	oldPath := filepath.Join(c.root, targetNib.Path)
	newRelPath := c.layout.ArchiveRel(filepath.Base(targetNib.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving nib to archive: %w", err)
	}

	targetNib.Path = newRelPath
	c.nibs[targetID] = targetNib

	return nil
}

// Unarchive moves a nib from the archive directory back to the main directory.
// Supports short IDs (without prefix) if a prefix is configured.
//
// The move is a bare rename with no directory fsync on either side, for the reasons
// Archive's comment records.
func (c *Core) Unarchive(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	unlock, lockErr := c.acquireWriteLock()
	if lockErr != nil {
		return lockErr
	}
	defer func() { _ = unlock() }()

	targetNib, targetID, err := c.findNibLocked(id)
	if err != nil {
		return err
	}

	if !c.isArchivedPath(targetNib.Path) {
		return nil // not archived
	}

	// Back to the data directory — NOT the store root, which holds no nib files:
	// a file returned there would stop being store content, vanishing from every
	// query on the next load.
	oldPath := filepath.Join(c.root, targetNib.Path)
	newRelPath := c.layout.DataRel(filepath.Base(targetNib.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.MkdirAll(c.layout.DataDir(), 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving nib from archive: %w", err)
	}

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

// isArchivedPath reports whether a store-relative path is an archived nib's.
func (c *Core) isArchivedPath(path string) bool {
	return c.layout.IsArchivedRel(path)
}

// normalizeID prepends the configured prefix when the ID lacks it.
func (c *Core) normalizeID(id string) string {
	if c.config != nil && c.config.Nibs.Prefix != "" && !strings.HasPrefix(id, c.config.Nibs.Prefix) {
		return c.config.Nibs.Prefix + id
	}
	return id
}

// findNibLocked finds a nib by ID, supporting short IDs.
// Must be called with c.mu held.
func (c *Core) findNibLocked(id string) (*nib.Nib, string, error) {
	if b, ok := c.nibs[id]; ok {
		return b, id, nil
	}

	fullID := c.normalizeID(id)
	if fullID != id {
		if b, ok := c.nibs[fullID]; ok {
			return b, fullID, nil
		}
	}

	return nil, "", ErrNotFound
}

// GetFromArchive loads a nib directly from the archive directory, for a nib that
// is not in the main loaded set. Returns nil, nil when the archive directory
// does not exist or holds no such nib.
func (c *Core) GetFromArchive(id string) (*nib.Nib, error) {
	fullID := c.normalizeID(id)

	archiveDir := c.layout.ArchiveDir()
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return nil, nil
	}

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

// LoadAndUnarchive finds a nib in the archive, loads it, unarchives it, and adds
// it to the in-memory store. Returns the nib or ErrNotFound.
//
// The move is a bare rename with no directory fsync on either side, for the
// reasons Archive's comment records. On this path the destination flush usually
// arrives anyway: both callers (`nibs set`, `nibs body`) go on to write the nib,
// and that write's updateOnDisk flushes data/.
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

	if !c.isArchivedPath(b.Path) {
		return b, nil
	}

	// The data directory, not the store root, for the reason Unarchive gives.
	oldPath := filepath.Join(c.root, b.Path)
	newRelPath := c.layout.DataRel(filepath.Base(b.Path))
	newPath := filepath.Join(c.root, newRelPath)

	if err := os.MkdirAll(c.layout.DataDir(), 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return nil, fmt.Errorf("moving nib from archive: %w", err)
	}

	b.Path = newRelPath
	c.nibs[targetID] = b

	return b, nil
}

// Init creates the store root and the data/ directory every new nib is written
// into. archive/ is created on demand by the first archive.
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

	if c.searchIndex != nil {
		if err := c.searchIndex.Close(); err != nil {
			return err
		}
		c.searchIndex = nil
	}

	return c.unwatchLocked()
}

// newNibID is the id generator Create draws from. A variable so the collision
// tests can seed deterministic draws; nothing in production reassigns it.
var newNibID = nib.NewID
