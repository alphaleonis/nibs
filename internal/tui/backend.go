package tui

import (
	"context"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// Backend is the single interface the TUI uses to interact with nibs.
// Methods use GraphQL model types — the same contract the web UI will use.
type Backend interface {
	// Reads
	// GetNib returns the nib with the given ID, or (nil, nil) if not found.
	// Returns a non-nil error only for unexpected failures.
	GetNib(ctx context.Context, id string) (*nib.Nib, error)
	ListNibs(ctx context.Context, filter *model.NibFilter) ([]*nib.Nib, error)

	// Relationship queries
	GetParent(ctx context.Context, obj *nib.Nib) (*nib.Nib, error)
	GetChildren(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error)
	GetBlockedBy(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error)
	GetBlocking(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error)

	// Blocking status (used by list view for visual indicators)
	// Both methods consider only active (non-completed, non-scrapped) blockers.
	IsBlocked(nibID string) bool
	IsBlocking(nibID string) bool

	// Mutations
	CreateNib(ctx context.Context, input model.CreateNibInput) (*nib.Nib, error)
	UpdateNib(ctx context.Context, id string, input model.UpdateNibInput) (*nib.Nib, error)
	SetParent(ctx context.Context, id string, parentID *string, ifMatch *string) (*nib.Nib, error)
	AddBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error)
	RemoveBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error)

	// Archive & Delete
	ArchiveNib(ctx context.Context, id string) error
	DeleteNib(ctx context.Context, id string) error

	// Reordering
	ReorderNib(ctx context.Context, id string, afterID, beforeID *string, first *bool) (*nib.Nib, error)

	// Editor support
	Root() string
	// ReloadAfterEdit re-reads the nib from disk after an external editor save
	// and bumps updated_at to reflect the manual edit. The timestamp update is
	// necessary because file-level edits bypass the mutation layer, which normally
	// sets updated_at automatically.
	ReloadAfterEdit(id string) (*nib.Nib, error)

	// File watching
	StartWatching() error
	StopWatching()
	Subscribe() (events <-chan struct{}, cancel func())
}
