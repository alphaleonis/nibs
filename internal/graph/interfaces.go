package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// NibReader provides read-only access to the nib store.
type NibReader interface {
	// Get returns the SHARED, read-only in-memory nib pointer (nibcore.Core.Get
	// hands back c.nibs[id] directly, not a defensive copy). Treat the result as
	// immutable: mutating it corrupts the store, and a subsequent rejected
	// Writer.Update leaves that phantom mutation visible in memory even though it
	// was never persisted. Any write path must obtain its
	// working nib via GetForUpdate (or updateTargetClone) instead.
	Get(id string) (*nib.Nib, error)
	// GetForUpdate returns an OWNED deep copy (a Clone) of the nib the caller may
	// freely mutate before handing it to Writer.Update — the store's shared
	// pointer is never touched, so a refused write cannot corrupt in-memory state.
	// It returns ErrNotFound when the nib is missing. This is the one blessed
	// accessor for every mutation site.
	GetForUpdate(id string) (*nib.Nib, error)
	// GetSnapshot returns a detached deep copy of the nib, cloned WHILE the store
	// lock is held, so the result never aliases the live store pointer and no
	// field (notably Path) is read off-lock. This is the blessed READ accessor
	// for values that outlive the lock.
	//
	// Rule for EVERY GraphQL resolver — read, query, relationship, AND mutation:
	// a resolver that returns nib data outliving the store lock MUST hand out a
	// GetSnapshot clone, never a live c.nibs pointer. gqlgen marshals a returned
	// nib's fields asynchronously and off the store lock, concurrently with an
	// in-place mutation like Archive rewriting a stored nib's Path under c.mu, so a
	// live pointer that reaches gqlgen is a data race. Only the immutable ID may be
	// read off a live pointer to look the snapshot up.
	//
	// This rule is now universal — it covers the write side too. The mutation
	// resolvers (CreateNib, UpdateNib, SetParent, the blocking add/remove pair,
	// AddBlockedBy, RemoveBlockedBy, and the reorder family ReorderNib/
	// ReorderChildren/ReorderSiblings) return a GetSnapshot clone via
	// mutationResolver.snapshotResult / snapshotResults, never the live c.nibs
	// pointer that Writer.Create/Writer.Update installed as the store entry (or
	// that Reader.Get handed back). A newly added resolver that returns nib data
	// is covered by this rule and must snapshot its result the same way.
	//
	// What snapshotting at the END of the Nibs pipeline buys: it detaches the
	// RETURNED nib data, so no live c.nibs pointer outlives the store lock to
	// reach gqlgen's async marshaler. It does NOT on its own make the synchronous
	// pre-snapshot reads race-free. The Nibs pipeline
	// (ApplyFilter/includeAncestors/ApplySorting) reads non-Path fields (Parent,
	// BlockedBy, Status, Type, Priority, Tags, Title, timestamps, Order, ...) off
	// live pointers before snapshotting at the end. Every non-Path field change on
	// a PUBLISHED stored pointer installs a FRESH pointer rather than mutating the
	// existing one: Core.Update for API writes, the file watcher's create/write
	// branch for external edits, and — as of nib nibs-pyei — RemoveLinksTo and
	// FixBrokenLinks (internal/nibcore/link_health.go), which now clone the nib,
	// apply the Parent/BlockedBy/Documents changes to the clone, and reinstall it
	// under c.nibs[id]. So an off-lock reader still holding the old pointer cannot
	// see those changes torn, and the pre-snapshot pipeline reads no longer race
	// the link-health mutators. (migrateV0ToV1 does edit BlockedBy/Blocking/Version
	// in place, but only on fresh, not-yet-published pointers while c.mu is held
	// during loadFromDisk, which rebuilds c.nibs from scratch — the same
	// not-yet-published class as loadNib's empty-slice defaults, not an off-lock
	// reader race.) Path is the sole field still mutated in place on a PUBLISHED
	// stored pointer (Archive/Unarchive/LoadAndUnarchive, slug rename, and the
	// watcher's external move detection), which is why returned data must be
	// snapshotted rather than handed out live.
	//
	// Unlike Get (live pointer) the result is race-safe to read from later; ok is
	// false when the nib is absent.
	GetSnapshot(id string) (*nib.Nib, bool)
	All() []*nib.Nib
	Search(query string) ([]*nib.Nib, error)
	NormalizeID(id string) (string, bool)
	FindIncomingLinks(targetID string) []nib.IncomingLink
	FindMentions(fromID string) []*nib.Nib
	FindMentionedBy(targetID string) []*nib.Nib
	Config() *config.Config
	// CurrentETag returns the canonical ETag of the on-disk content of the given
	// nib (a hash of the parsed file's canonical render, so it agrees with the
	// in-memory nib.ETag() across benign formatting drift). loadNib keeps Type and
	// Priority empty when the file omits them (the "task"/"normal" presentation
	// defaults are applied only at the consumption boundary via
	// nib.EffectiveType()/EffectivePriority()), so priority/type-less files no
	// longer diverge from their in-memory nib.ETag(). The sole residual
	// divergence is the created_at/updated_at mtime fallback loadNib synthesizes for
	// hand-authored files missing those timestamps, which computeStoredETag does not
	// reproduce (see nibcore.computeStoredETag). Falls back to the in-memory etag only when no on-disk file
	// exists yet (not-flushed / externally removed); an existing file that cannot
	// be read or parsed fails CLOSED, returning a non-reconcilable
	// *nibcore.OnDiskUnparseableError (empty etag string, no reusable token) so
	// bulk-reorder pre-validation refuses the operation and no retry can clobber
	// the file. Used by bulk-reorder pre-validation. Returns
	// the same errors as Get (notably ErrNotFound) when the nib is missing.
	CurrentETag(id string) (string, error)
}

// NibWriter provides mutating operations on the nib store.
type NibWriter interface {
	Create(b *nib.Nib) error
	Update(b *nib.Nib, ifMatch *string) error
	Delete(id string) error
	Archive(id string) error
	RemoveLinksTo(targetID string) (int, error)
}

// NibValidator provides structural integrity checks.
type NibValidator interface {
	ValidateParent(b *nib.Nib, parentID string) error
	DetectCycle(fromID, linkType, toID string) []string
}

// BlockingChecker provides blocking-relationship queries.
// Both methods consider only active (non-completed, non-scrapped) blockers.
type BlockingChecker interface {
	// IsBlocked returns true if the nib has active (non-completed, non-scrapped) blockers.
	IsBlocked(nibID string) bool
	// IsBlocking returns true if the nib is actively blocking non-resolved nibs.
	// The nib itself must also be non-resolved to be considered actively blocking.
	IsBlocking(nibID string) bool
}

// NibSubscriber provides access to nib change event streams.
type NibSubscriber interface {
	Subscribe() (<-chan []NibEvent, func())
}

// NibEvent represents a change to a nib (re-exported from nibcore).
type NibEvent = nibcore.NibEvent
