// Package nibcore provides a thread-safe in-memory store for nibs with filesystem persistence
// and optional file watching for long-running processes.
package nibcore

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/config"
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

// Core provides thread-safe in-memory storage for nibs with filesystem persistence.
type Core struct {
	root   string         // absolute path to .nibs directory
	config *config.Config // project configuration

	// In-memory state
	mu    sync.RWMutex
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
		nibs:       make(map[string]*nib.Nib),
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

	// Walk the entire .nibs directory tree, loading all .md files
	err := filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip non-.md files
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		b, loadErr := c.loadNib(path)
		if loadErr != nil {
			return fmt.Errorf("loading %s: %w", path, loadErr)
		}

		c.nibs[b.ID] = b
		return nil
	})
	if err != nil {
		return err
	}

	// Migrate v0 nibs to v1 (single-side blocking)
	if err := c.migrateV0ToV1(); err != nil {
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

	// Apply defaults for GraphQL non-nullable fields
	if b.Type == "" {
		b.Type = "task"
	}
	if b.Priority == "" {
		b.Priority = "normal"
	}
	if b.Tags == nil {
		b.Tags = []string{}
	}
	if b.BlockedBy == nil {
		b.BlockedBy = []string{}
	}
	if b.Documents == nil {
		b.Documents = []string{}
	}
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

// Search performs full-text search and returns matching nibs.
// The search index is lazily initialized on first use.
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

	result := make([]*nib.Nib, 0, len(ids))
	for _, id := range ids {
		if b, ok := c.nibs[id]; ok {
			result = append(result, b)
		}
	}
	return result, nil
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

// Create adds a new nib, generating an ID if needed, and writes it to disk.
func (c *Core) Create(b *nib.Nib) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	// Validate etag if provided or required
	requireIfMatch := c.config != nil && c.config.Nibs.RequireIfMatch

	if requireIfMatch && (ifMatch == nil || *ifMatch == "") {
		return &ETagRequiredError{}
	}

	if ifMatch != nil && *ifMatch != "" {
		// Calculate etag from the on-disk version by reading the stored nib's path
		// This is necessary because the in-memory nib may have already been modified
		// (Go uses pointers, so modifying the nib passed to Update also modifies c.nibs[id])
		var currentETag string
		if storedNib.Path != "" {
			// Read current file from disk to calculate etag
			diskPath := filepath.Join(c.root, storedNib.Path)
			content, err := os.ReadFile(diskPath)
			if err != nil {
				// If file doesn't exist yet, fall back to stored nib's etag
				// This can happen for nibs created but not yet persisted
				currentETag = storedNib.ETag()
			} else {
				// Calculate etag from on-disk content
				h := fnv.New64a()
				h.Write(content)
				currentETag = hex.EncodeToString(h.Sum(nil))
			}
		} else {
			// No path yet, use in-memory etag
			currentETag = storedNib.ETag()
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
