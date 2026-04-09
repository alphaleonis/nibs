package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// NibReader provides read-only access to the nib store.
type NibReader interface {
	Get(id string) (*nib.Nib, error)
	All() []*nib.Nib
	Search(query string) ([]*nib.Nib, error)
	NormalizeID(id string) (string, bool)
	FindIncomingLinks(targetID string) []nib.IncomingLink
	Config() *config.Config
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
