package tui

import (
	"context"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// Backend is the interface the TUI reads and mutates nibs through.
type Backend interface {
	// GetNib returns (nil, nil) when no nib has the given ID.
	GetNib(ctx context.Context, id string) (*nib.Nib, error)
	ListNibs(ctx context.Context, filter *model.NibFilter) ([]*nib.Nib, error)

	GetParent(ctx context.Context, obj *nib.Nib) (*nib.Nib, error)
	GetChildren(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error)
	GetBlockedBy(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error)
	GetBlocking(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error)

	// IsBlocked and IsBlocking consider only active links; see
	// nibcore.Core.IsBlocked.
	IsBlocked(nibID string) bool
	IsBlocking(nibID string) bool

	CreateNib(ctx context.Context, input model.CreateNibInput) (*nib.Nib, error)
	UpdateNib(ctx context.Context, id string, input model.UpdateNibInput) (*nib.Nib, error)
	SetParent(ctx context.Context, id string, parentID *string, ifMatch *string) (*nib.Nib, error)
	AddBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error)
	RemoveBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error)

	ArchiveNib(ctx context.Context, id string) error
	DeleteNib(ctx context.Context, id string) error

	ReorderNib(ctx context.Context, id string, afterID, beforeID *string, first *bool) (*nib.Nib, error)

	Root() string
	// ReloadAfterEdit reloads the store after an external editor save and stamps
	// the nib's updated_at, which a file-level edit does not set.
	ReloadAfterEdit(id string) (*nib.Nib, error)

	StartWatching() error
	StopWatching()
	Subscribe() (events <-chan struct{}, cancel func())
}
