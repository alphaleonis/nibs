package graph

import (
	"context"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// NibReader provides read-only access to the nib store.
type NibReader interface {
	// Get returns the SHARED, read-only in-memory nib pointer, not a defensive
	// copy. Treat it as immutable: mutating it corrupts the store, and a later
	// rejected Writer.Update leaves that phantom mutation visible in memory though
	// it was never persisted. A write path takes its working nib from GetForUpdate
	// or updateTargetClone instead.
	Get(id string) (*nib.Nib, error)
	// GetForUpdate returns an OWNED deep copy the caller may freely mutate before
	// handing it to Writer.Update; the store's shared pointer is never touched, so
	// a refused write cannot corrupt in-memory state. It returns ErrNotFound when
	// the nib is missing, and is the one blessed accessor for every mutation site.
	GetForUpdate(id string) (*nib.Nib, error)
	// GetSnapshot returns a detached deep copy of the nib, cloned WHILE the store
	// lock is held, so the result never aliases the live store pointer and no field
	// (notably Path) is read off-lock. This is the blessed READ accessor for values
	// that outlive the lock. ok is false when the nib is absent.
	//
	// ID RESOLUTION IS PART OF THIS CONTRACT, not an implementation detail of Core.
	// It mirrors Get: exact id first, then — when a prefix is configured and the id
	// does not already carry it — the prefix-prepended form. An implementation that
	// does the obvious map lookup instead breaks nibResolver.Parent, which hands it
	// a RAW stored parent link: that one resolver would answer null for a
	// short-form link every other surface resolves (see resolvedParent).
	//
	// CANONICAL INVARIANT (the live-pointer / copy-on-write rule). This doc is its
	// single authoritative statement; sibling comments across internal/nibcore and
	// internal/graph defer here rather than re-derive it. A Core mutator may change
	// ONLY Path in place on a pointer already published in c.nibs; every other
	// field change must be copy-on-write — install a fresh *nib.Nib under the map
	// key rather than edit the stored one. An off-lock reader (the Nibs filter/sort
	// pipeline; gqlgen's async field marshaler) still holding the old pointer then
	// never observes a non-Path field torn mid-write. The in-place writers are the
	// archive/unarchive moves; a slug rename is copy-on-write like any other field
	// change, since it also changes the filename-derived Slug. The producer half is
	// enforced by nibcore.TestCoreMutators_FreezeGuard.
	//
	// Two consequences:
	//
	//   - Return nib data that outlives the store lock as a GetSnapshot clone, from
	//     every resolver — read, query, relationship and mutation alike. Only the
	//     immutable ID may be read off a live pointer, to look the snapshot up. The
	//     mutation resolvers go through snapshotResult / snapshotResults.
	//
	//   - Snapshotting at the END of the Nibs pipeline detaches only the RETURNED
	//     data. ApplyFilter/includeAncestors/ApplySorting read non-Path fields off
	//     live pointers before it, which is safe only because of the rule above.
	GetSnapshot(id string) (*nib.Nib, bool)
	All() []*nib.Nib
	// Search returns the TOP hits for the query — id matches first, then full-text
	// hits by relevance — each leg capped at nibcore.DefaultSearchLimit.
	Search(query string) ([]*nib.Nib, error)
	// SearchAll returns EVERY match for the query, in the same order, uncapped.
	// Use it to intersect a term with a working set some relationship already
	// bounded (see filterBySearch), where a cap would drop a genuine member that
	// ranks below the store-wide cutoff.
	SearchAll(query string) ([]*nib.Nib, error)
	NormalizeID(id string) (string, bool)
	FindIncomingLinks(targetID string) []nib.IncomingLink
	FindMentions(fromID string) []*nib.Nib
	FindMentionedBy(targetID string) []*nib.Nib
	Config() *config.Config
	// Areas returns the store's declared area vocabulary as it stands NOW. A
	// running server reloads it when the store's areas.yml changes, where
	// everything on Config is fixed at startup, so take one snapshot per decision —
	// two calls may answer from two vocabularies.
	Areas() *config.Areas
	// CurrentETag returns the canonical ETag of the nib's ON-DISK content — a hash
	// of the parsed file's canonical render, so it agrees with the in-memory
	// nib.ETag() across benign formatting drift, including a file that omits
	// type/priority or the created_at/updated_at stamps. It falls back to the
	// in-memory etag only when no on-disk file exists yet (not flushed, externally
	// removed); an existing file that cannot be read or parsed fails CLOSED,
	// returning a non-reconcilable *nibcore.OnDiskUnparseableError (empty etag, no
	// token a retry could echo back) so pre-validation refuses the operation and no
	// retry can clobber the file. Returns Get's errors (notably ErrNotFound) when
	// the nib is missing.
	CurrentETag(id string) (string, error)
}

// NibWriter provides mutating operations on the nib store.
type NibWriter interface {
	Create(b *nib.Nib) error
	Update(b *nib.Nib, ifMatch *string) error
	Delete(id string) error
	Archive(id string) error
	// RemoveLinksTo strips every parent/blockedBy/milestone link that RESOLVES to
	// the target and returns how many it removed. Target and stored link ids are
	// both resolved the way Get resolves an id, so no caller pre-normalizes a short
	// id. A delete that unlinks runs this BEFORE removing the nib, while the target
	// is still resolvable.
	RemoveLinksTo(targetID string) (int, error)
}

// NibValidator provides structural integrity checks.
type NibValidator interface {
	ValidateParent(b *nib.Nib, parentID string) error
	DetectCycle(fromID, linkType, toID string) []string
	// ValidateEnums reports whether the nib's type/status/priority/estimate hold
	// known values (the empty "use the default" sentinel always passes). That
	// vocabulary is fixed, not per-config. NibWriter.Update repeats the check under
	// its write lock; it is exposed so a resolver whose later steps write to OTHER
	// nibs can refuse a doomed subject first (see preValidateSubject).
	ValidateEnums(b *nib.Nib) error
	// ValidateArea reports whether the nib's `area:` holds a path the store's
	// config declares (unset always passes). Separate from ValidateEnums because
	// areas are the one PER-CONFIG vocabulary. NibWriter.Update repeats it under
	// the write lock; it is exposed here for the same reason ValidateEnums is.
	ValidateArea(b *nib.Nib) error
}

// BlockingChecker provides blocking-relationship queries. Both methods count only
// nibs whose status has NOT released its dependents, which is narrower than
// closed-ness (see Resolver.releasesDependents).
type BlockingChecker interface {
	IsBlocked(nibID string) bool
	// IsBlocking is false once the nib itself has released its dependents.
	IsBlocking(nibID string) bool
}

// NibSubscriber provides access to nib change event streams.
type NibSubscriber interface {
	Subscribe() (<-chan []NibEvent, func())
	// SubscribeAreas ticks whenever the store reloads a declared vocabulary that
	// differs from the one it replaces. It carries no payload: read the new
	// vocabulary from NibReader.Areas.
	SubscribeAreas() (<-chan struct{}, func())
}

// AreaWriter is the store's area-vocabulary verbs: declaring an area, renaming
// one, and retiring one together with the nibs assigned to it.
//
// Each method is ONE WHOLE EDIT: it takes the store's two locks itself, in the
// one order they may be taken in, and holds both across the re-read, the plan,
// the member cascade, the areas.yml write and the reload. A caller holds no lock
// and owes no ordering: one that takes either lock itself and then calls one of
// these deadlocks against it.
//
// Answer a mutation with the vocabulary from the returned AreaEditResult rather
// than a re-ask, so a decorated or per-request Reader cannot silently disagree
// with the edit about what the store now declares.
type AreaWriter interface {
	// RenameArea renames a declared node, cascading to every nib assigned at or
	// below it. ctx ends the verb's wait for the store's file lock — the one step
	// that waits on another process.
	RenameArea(ctx context.Context, path, newName string) (nibcore.AreaEditResult, error)
	// RemoveArea retires a declared node and the subtree it heads, disposing of
	// every nib assigned at or below it as disposition says. ctx does the same here.
	RemoveArea(ctx context.Context, path string, disposition nibcore.AreaDisposition) (nibcore.AreaEditResult, error)
	// Warn reports a note about an edit to the store's warning sink, where a
	// running `nibs serve` operator reads it — for a warning the answer's own shape
	// has no room for.
	Warn(format string, args ...any)
}

// NibEvent represents a change to a nib (re-exported from nibcore).
type NibEvent = nibcore.NibEvent
