package graph

import (
	"context"
	"math"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/projection"
)

// ProgressRollup is the value projected for the computed `progress` field: a
// direct-children completion rollup. Total is the number of direct children;
// Done counts children in a resolved status (completed or scrapped — the
// canonical "finished" set from nib.IsResolvedStatus); Percent is
// round(Done/Total*100), and 0 when the nib has no children. A leaf nib (no
// children) therefore reports {total:0, done:0, percent:0}: progress is a
// rollup over descendants, not a reflection of the nib's own status.
type ProgressRollup struct {
	Total   int `json:"total"`
	Done    int `json:"done"`
	Percent int `json:"percent"`
}

// projectionResolver adapts the GraphQL resolver's store logic to the
// projection.Resolver interface consumed by internal/projection. It delegates
// the relation and readiness computations to the exact nib-field resolvers
// (BlockingIds, MentionIds, MentionedByIds, BlockedByIds) so the projection
// engine and the GraphQL surface cannot drift on blocking / mention / ready
// semantics. The child rollups (children count, progress) are read straight
// off the parent-link graph via FindIncomingLinks, matching the child set the
// Children resolver derives from GetSortedSiblings.
type projectionResolver struct {
	r   *Resolver
	nib NibResolver
	ctx context.Context
}

// ProjectionResolver returns a projection.Resolver backed by this resolver's
// store. The ctx is threaded through the delegated field resolvers so a caller
// that attached a per-request mention cache (WithRequestCache) reuses it; CLI
// callers pass context.Background() and the cache falls through to the reader.
// A nil ctx is treated as context.Background().
func (r *Resolver) ProjectionResolver(ctx context.Context) projection.Resolver {
	if ctx == nil {
		ctx = context.Background()
	}
	return &projectionResolver{r: r, nib: r.Nib(), ctx: ctx}
}

// Compile-time assertion that the adapter satisfies the engine's contract.
var _ projection.Resolver = (*projectionResolver)(nil)

// NibByID returns the shared, read-only nib pointer for a related id, or
// (nil, false) when no nib has that id. The engine only calls this to expand a
// nested relation sub-selection over ids it already read off the store, so the
// pointer is never mutated.
func (p *projectionResolver) NibByID(id string) (*nib.Nib, bool) {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return nil, false
	}
	return b, true
}

// ChildCount returns the number of direct children of the nib, counting the
// parent links that point at it — the same child set GetSortedSiblings derives,
// without the ordering-key backfill a count does not need.
func (p *projectionResolver) ChildCount(id string) int {
	count := 0
	for _, link := range p.r.Reader.FindIncomingLinks(id) {
		if link.LinkType == "parent" {
			count++
		}
	}
	return count
}

// Progress returns a ProgressRollup over the nib's direct children. See
// ProgressRollup for the exact semantics.
func (p *projectionResolver) Progress(id string) any {
	var total, done int
	for _, link := range p.r.Reader.FindIncomingLinks(id) {
		if link.LinkType != "parent" {
			continue
		}
		total++
		if nib.IsResolvedStatus(link.FromNib.Status) {
			done++
		}
	}
	percent := 0
	if total > 0 {
		percent = int(math.Round(float64(done) / float64(total) * 100))
	}
	return ProgressRollup{Total: total, Done: done, Percent: percent}
}

// Ready reports whether the nib is startable: not already resolved
// (completed/scrapped) and with no active blockers. BlockedByIds already drops
// resolved blockers, so a nib blocked only by finished work is ready.
func (p *projectionResolver) Ready(id string) bool {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return false
	}
	if nib.IsResolvedStatus(b.Status) {
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
