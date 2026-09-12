package graph

import (
	"context"
	"sync"

	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/progress"
	"github.com/alphaleonis/nibs/internal/projection"
)

// projectionResolver adapts the GraphQL resolver's store logic to the
// projection.Resolver interface consumed by internal/projection. It delegates
// the relation and readiness computations to the exact nib-field resolvers
// (BlockingIds, MentionIds, MentionedByIds, BlockedByIds) so the projection
// engine and the GraphQL surface cannot drift on blocking / mention / ready
// semantics. The child rollups (children count, progress) answer from one
// membership.View built lazily on first use and memoized for the instance's
// lifetime.
//
// That memo makes an instance POINT-IN-TIME: build one after any write whose
// result it should reflect, and never reuse one across operations — the
// staleness rule RequestCache states for the mention and search memos.
type projectionResolver struct {
	r        *Resolver
	nib      NibResolver
	ctx      context.Context
	viewOnce sync.Once
	view     *membership.View
}

// membershipView returns the instance's memoized membership view, building it
// on first use.
func (p *projectionResolver) membershipView() *membership.View {
	p.viewOnce.Do(func() {
		p.view = membership.Compute(p.r.Reader.All())
	})
	return p.view
}

// ProjectionResolver returns a projection.Resolver backed by this resolver's
// store. ctx is threaded through the delegated field resolvers, so a caller
// that attached a RequestCache reuses it. A nil ctx becomes
// context.Background().
func (r *Resolver) ProjectionResolver(ctx context.Context) projection.Resolver {
	if ctx == nil {
		ctx = context.Background()
	}
	return &projectionResolver{r: r, nib: r.Nib(), ctx: ctx}
}

var _ projection.Resolver = (*projectionResolver)(nil)

// NibByID returns the shared, read-only nib pointer for a related id, or
// (nil, false) when no nib has that id. Treat it as immutable — it is the live
// store pointer.
func (p *projectionResolver) NibByID(id string) (*nib.Nib, bool) {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return nil, false
	}
	return b, true
}

// ParentID returns the nib's resolved parent id — the same reading the GraphQL
// parentId field and the hasParent filter give, so `-f parent` cannot drift
// from them (see resolvedParent). A nib that has since been deleted resolves to
// no parent.
func (p *projectionResolver) ParentID(id string) string {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return ""
	}
	return resolvedParentID(b, p.r.Reader)
}

// ChildCount returns the number of direct children of the nib — the STRUCTURAL
// parent axis (membership.View.Children). Not DirectMembers: childCount answers
// "how many nibs name this one as parent", while membership answers from the
// `milestone:` assignment axis, so a milestone honestly reports 0 children
// while its progress rolls over the assignees.
func (p *projectionResolver) ChildCount(id string) int {
	return len(p.membershipView().Children(id))
}

// Progress returns the canonical completion progress.Rollup over the nib's
// direct members (membership.View.DirectMembers): a milestone rolls over its
// `milestone:` assignees, every other container over its structural children.
// See progress.Rollup / progress.ByCount for the exact rule.
//
// Reading Status off the view's live pointers is safe here: only Path is ever
// mutated in place on a published stored pointer (see NibReader.GetSnapshot),
// and this returns a computed value, so no pointer escapes to async marshaling.
func (p *projectionResolver) Progress(id string) any {
	members := p.membershipView().DirectMembers(id)
	statuses := make([]string, len(members))
	for i, m := range members {
		statuses[i] = m.Status
	}
	return progress.ByCount(statuses)
}

// Ready reports whether the nib can be started: a startable status and no
// active blockers. BlockedByIds drops blockers whose status released them, so a
// nib blocked only by completed or scrapped work is ready — but one blocked by
// a deferred nib is not.
//
// The status half is config.IsStartableStatus, narrower than "not closed": a
// draft or in-progress nib reports ready:false. `nibs list --ready` reads the
// same flag. The blocker half agrees on matching rules rather than shared code:
// this field walks BlockedByIds → Reader.Get, the filter walks Core.IsBlocked →
// findActiveBlockersInMap → normalizeIDInMap, and each spells out the same
// resolution — the exact id, then the configured prefix prepended — before
// asking config.StatusReleasesDependents. TestReadyProjectionAndFilterAgree
// drives a blocker under both spellings, so neither copy can drift alone.
func (p *projectionResolver) Ready(id string) bool {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return false
	}
	if !p.r.isStartableStatus(b.Status) {
		return false
	}
	blockers, err := p.nib.BlockedByIds(p.ctx, b)
	if err != nil {
		return false
	}
	return len(blockers) == 0
}

// Blocking returns the ids of active nibs this nib is blocking, via the shared
// BlockingIds resolver.
func (p *projectionResolver) Blocking(id string) []string {
	return p.relationIDs(id, func(b *nib.Nib) ([]string, error) {
		return p.nib.BlockingIds(p.ctx, b)
	})
}

// Mentions returns the ids of nibs this nib's body mentions, via the shared
// MentionIds resolver.
func (p *projectionResolver) Mentions(id string) []string {
	return p.relationIDs(id, func(b *nib.Nib) ([]string, error) {
		return p.nib.MentionIds(p.ctx, b)
	})
}

// MentionedBy returns the ids of nibs whose bodies mention this nib, via the
// shared MentionedByIds resolver.
func (p *projectionResolver) MentionedBy(id string) []string {
	return p.relationIDs(id, func(b *nib.Nib) ([]string, error) {
		return p.nib.MentionedByIds(p.ctx, b)
	})
}

// relationIDs looks up the nib and runs one of the id-list field resolvers,
// returning an empty slice when the nib is missing or the resolver errors so a
// projected relation is always a JSON array rather than null.
func (p *projectionResolver) relationIDs(id string, fn func(*nib.Nib) ([]string, error)) []string {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return []string{}
	}
	ids, err := fn(b)
	if err != nil || ids == nil {
		return []string{}
	}
	return ids
}
