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
	// was never persisted (nibs-twvo/nibs-e9oz). Any write path must obtain its
	// working nib via GetForUpdate (or updateTargetClone) instead.
	Get(id string) (*nib.Nib, error)
	// GetForUpdate returns an OWNED deep copy (a Clone) of the nib the caller may
	// freely mutate before handing it to Writer.Update — the store's shared
	// pointer is never touched, so a refused write cannot corrupt in-memory state.
	// It returns the same errors as Get (notably ErrNotFound) when the nib is
	// missing. This is the one blessed accessor for every mutation site.
	GetForUpdate(id string) (*nib.Nib, error)
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
	// longer diverge from their in-memory nib.ETag() (nibs-7d3o). The sole residual
	// divergence is the created_at/updated_at mtime fallback loadNib synthesizes for
	// hand-authored files missing those timestamps, which computeStoredETag does not
	// reproduce (see nibcore.computeStoredETag). Falls back to the in-memory etag only when no on-disk file
	// exists yet (not-flushed / externally removed); an existing file that cannot
	// be read or parsed fails CLOSED, returning a non-reconcilable
	// *nibcore.OnDiskUnparseableError (empty etag string, no reusable token) so
	// bulk-reorder pre-validation refuses the operation and no retry can clobber
	// the file (finding #5). Used by bulk-reorder pre-validation. Returns
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
