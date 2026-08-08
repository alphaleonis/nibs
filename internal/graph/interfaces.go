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
	// ID RESOLUTION IS PART OF THIS CONTRACT, not an implementation detail of
	// Core. It mirrors Get: exact id first, then — when a prefix is configured and
	// the id does not already carry it — the prefix-prepended form. An
	// implementation that does the obvious map lookup instead satisfies the
	// copy-on-write half of this doc and still breaks internal/graph, because
	// nibResolver.Parent hands it a RAW stored parent link and returns whatever
	// comes back. Drop the fallback and that one resolver answers null for a
	// short-form link every other surface resolves — the cross-surface split the
	// resolved-parent rule exists to prevent (see resolvedParent). The dependency
	// is pinned by TestParentResolverDependsOnGetSnapshotIDResolution, which drives
	// one fixture through a reader with the fallback and one without; change this
	// behavior and that test fails rather than the break going quiet.
	//
	// CANONICAL INVARIANT (the live-pointer / copy-on-write rule). This doc is its
	// single authoritative statement; sibling comments across internal/nibcore and
	// internal/graph defer here rather than re-derive it. A Core mutator may change
	// ONLY Path in place on a pointer already published in c.nibs; every other
	// field change must be copy-on-write — install a fresh *nib.Nib under the map
	// key rather than edit the stored one. That lets an off-lock reader (the
	// GraphQL Nibs filter/sort pipeline; gqlgen's async field marshaler) still
	// holding the old pointer never observe a non-Path field torn mid-write. The
	// only writers that rewrite Path in place are Archive, Unarchive,
	// LoadAndUnarchive, and the watcher's archive/unarchive *move* branches. The
	// watcher's same-id slug-rename branch is COPY-ON-WRITE, not an in-place Path
	// writer: a rename also changes Slug (filename-derived, an off-lock-read field),
	// so the branch installs a fresh *nib.Nib carrying the new Path AND re-derived
	// Slug rather than editing the stored pointer. The store-side (producer) half of
	// this invariant is enforced by the deterministic guard
	// nibcore.TestCoreMutators_FreezeGuard.
	//
	// Consequences that follow from the canonical rule (specifics, not a
	// re-derivation of it):
	//
	//   - EVERY GraphQL resolver — read, query, relationship, AND mutation — that
	//     returns nib data outliving the store lock MUST hand out a GetSnapshot
	//     clone, never a live c.nibs pointer; only the immutable ID may be read off
	//     a live pointer to look the snapshot up. The mutation resolvers (CreateNib,
	//     UpdateNib, SetParent, AddBlockedBy/RemoveBlockedBy, the blocking add/remove
	//     pair, and the reorder family ReorderNib/ReorderChildren/ReorderSiblings)
	//     return their result via mutationResolver.snapshotResult / snapshotResults,
	//     never the live pointer Writer.Create/Writer.Update installed. A newly added
	//     resolver that returns nib data must snapshot its result the same way.
	//
	//   - Snapshotting at the END of the Nibs pipeline detaches only the RETURNED
	//     data; it does NOT make the synchronous pre-snapshot reads race-free. The
	//     pipeline (ApplyFilter/includeAncestors/ApplySorting) reads non-Path fields
	//     (Parent, BlockedBy, Status, Type, Priority, Tags, Title, timestamps,
	//     Order, ...) off live pointers before the final snapshot — safe precisely
	//     because the canonical rule guarantees those fields are never rewritten in
	//     place on a published pointer (Core.Update, the watcher's create/write
	//     branch, and — as of nib nibs-pyei — RemoveLinksTo/FixBrokenLinks all
	//     install a fresh pointer). migrateV0ToV1 does edit BlockedBy/Blocking/
	//     Version in place, but only on fresh, not-yet-published pointers while c.mu
	//     is held during loadFromDisk, so no off-lock reader can observe it.
	//
	// Unlike Get (live pointer) the result is race-safe to read from later; ok is
	// false when the nib is absent.
	GetSnapshot(id string) (*nib.Nib, bool)
	All() []*nib.Nib
	// Search returns the TOP hits for the query — id matches first, then
	// full-text hits by relevance — each leg capped at
	// nibcore.DefaultSearchLimit. queryResolver.Nibs seeds from this: at the top
	// level the truncation is the answer.
	Search(query string) ([]*nib.Nib, error)
	// SearchAll returns EVERY match for the query, in the same order, uncapped.
	// It is the accessor for intersecting a term with a working set some
	// relationship already bounded (see filterBySearch): capping there would
	// truncate the store rather than the answer, dropping a genuine member that
	// ranks below the store-wide cutoff. Bounded by the store — a query cannot
	// match more nibs than exist.
	SearchAll(query string) ([]*nib.Nib, error)
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
	// RemoveLinksTo strips every parent/blockedBy link that RESOLVES to the
	// target and returns how many it removed. Target and stored link ids are both
	// resolved the way Get resolves an id, so no caller has to pre-normalize a
	// short id — and a delete that unlinks must run this BEFORE removing the nib,
	// while the target is still resolvable (see nibcore.Core.RemoveLinksTo).
	RemoveLinksTo(targetID string) (int, error)
}

// NibValidator provides structural integrity checks.
type NibValidator interface {
	ValidateParent(b *nib.Nib, parentID string) error
	DetectCycle(fromID, linkType, toID string) []string
	// ValidateEnums reports whether the nib's type/status/priority/estimate hold
	// values the project config accepts (the empty "use the default" sentinel
	// always passes). NibWriter.Update repeats this check under its write lock, so
	// nothing depends on a resolver calling it — it is exposed so a resolver whose
	// later steps write to OTHER nibs can refuse a doomed subject first (see
	// mutationResolver.preValidateSubject).
	ValidateEnums(b *nib.Nib) error
}

// BlockingChecker provides blocking-relationship queries.
// Both methods consider only open (non-closed) blockers.
type BlockingChecker interface {
	// IsBlocked returns true if the nib has open blockers.
	IsBlocked(nibID string) bool
	// IsBlocking returns true if the nib is actively blocking open nibs.
	// The nib itself must also be open to be considered actively blocking.
	IsBlocking(nibID string) bool
}

// NibSubscriber provides access to nib change event streams.
type NibSubscriber interface {
	Subscribe() (<-chan []NibEvent, func())
}

// NibEvent represents a change to a nib (re-exported from nibcore).
type NibEvent = nibcore.NibEvent
