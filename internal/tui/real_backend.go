package tui

import (
	"context"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// RealBackend implements Backend by delegating to a graph.Resolver and nibcore.Core.
type RealBackend struct {
	core     *nibcore.Core
	resolver *graph.Resolver
}

// NewRealBackend creates a Backend backed by the real resolver and core.
func NewRealBackend(core *nibcore.Core, resolver *graph.Resolver) *RealBackend {
	return &RealBackend{core: core, resolver: resolver}
}

// Reads

func (b *RealBackend) GetNib(ctx context.Context, id string) (*nib.Nib, error) {
	return b.resolver.Query().Nib(ctx, id)
}

func (b *RealBackend) ListNibs(ctx context.Context, filter *model.NibFilter) ([]*nib.Nib, error) {
	return b.resolver.Query().Nibs(ctx, filter, nil)
}

// Relationship queries

func (b *RealBackend) GetParent(ctx context.Context, obj *nib.Nib) (*nib.Nib, error) {
	return b.resolver.Nib().Parent(ctx, obj)
}

func (b *RealBackend) GetChildren(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return b.resolver.Nib().Children(ctx, obj, filter, nil)
}

func (b *RealBackend) GetBlockedBy(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return b.resolver.Nib().BlockedBy(ctx, obj, filter)
}

func (b *RealBackend) GetBlocking(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return b.resolver.Nib().Blocking(ctx, obj, filter)
}

// Blocking status

func (b *RealBackend) IsBlocked(nibID string) bool {
	return b.resolver.Blocking.IsBlocked(nibID)
}

func (b *RealBackend) IsBlocking(nibID string) bool {
	return b.resolver.Blocking.IsBlocking(nibID)
}

// Mutations

func (b *RealBackend) CreateNib(ctx context.Context, input model.CreateNibInput) (*nib.Nib, error) {
	return b.resolver.Mutation().CreateNib(ctx, input)
}

func (b *RealBackend) UpdateNib(ctx context.Context, id string, input model.UpdateNibInput) (*nib.Nib, error) {
	return b.resolver.Mutation().UpdateNib(ctx, id, input)
}

func (b *RealBackend) SetParent(ctx context.Context, id string, parentID *string, ifMatch *string) (*nib.Nib, error) {
	return b.resolver.Mutation().SetParent(ctx, id, parentID, ifMatch)
}

func (b *RealBackend) AddBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error) {
	return b.resolver.Mutation().AddBlocking(ctx, id, targetID)
}

func (b *RealBackend) RemoveBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error) {
	return b.resolver.Mutation().RemoveBlocking(ctx, id, targetID)
}

// Archive & Delete

func (b *RealBackend) ArchiveNib(ctx context.Context, id string) error {
	_, err := b.resolver.Mutation().ArchiveNib(ctx, id)
	return err
}

func (b *RealBackend) DeleteNib(ctx context.Context, id string) error {
	_, err := b.resolver.Mutation().DeleteNib(ctx, id)
	return err
}

// Reordering

func (b *RealBackend) ReorderNib(ctx context.Context, id string, afterID, beforeID *string, first *bool) (*nib.Nib, error) {
	return b.resolver.Mutation().ReorderNib(ctx, id, afterID, beforeID, first, nil, nil)
}

// Editor support

func (b *RealBackend) Root() string {
	return b.core.Root()
}

func (b *RealBackend) ReloadAfterEdit(id string) (*nib.Nib, error) {
	if err := b.core.Load(); err != nil {
		return nil, err
	}
	n, err := b.core.Get(id)
	if err != nil {
		return nil, err
	}
	// Touch updated_at so the change is persisted
	if err := b.core.Update(n, nil); err != nil {
		return nil, err
	}
	return n, nil
}

// File watching

func (b *RealBackend) StartWatching() error {
	return b.core.StartWatching()
}

func (b *RealBackend) StopWatching() {
	_ = b.core.StopWatching()
}

func (b *RealBackend) Subscribe() (events <-chan struct{}, cancel func()) {
	// The TUI only needs "something changed" — it re-reads the store on every
	// tick and never inspects a payload. SubscribeSignal delivers exactly that
	// without the per-change payload clone, and its channel already matches this
	// signature, so pass it straight through (no translate goroutine).
	return b.core.SubscribeSignal()
}
