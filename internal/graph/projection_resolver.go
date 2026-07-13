package graph

import (
	"context"
	"math"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/projection"
)

// ProgressRollup is the value projected for the computed `progress` field, and
// the ONE canonical child-completion rollup reused by the recipe views
// (context, plan, roadmap) so `nibs get <id> -f progress` and those views can
// never disagree. Build it only via ComputeProgress — do not fork the rule.
//
// Canonical definition (single source of truth):
//   - Done     = direct children whose status is "completed". Scrapped work is
//     cancelled, not finished, so it is NOT counted as done.
//   - Total    = direct children EXCLUDING scrapped ones. Scrapped work is
//     abandoned — neither done nor pending — so it leaves the denominator
//     entirely. draft/todo/in-progress/deferred children all count toward Total.
//   - Percent  = round(Done/Total*100); 0 when Total == 0.
//   - Scrapped = direct children with status "scrapped", surfaced purely for
//     transparency (excluded from both Done and Total).
//
// A leaf nib (no children) reports {total:0, done:0, percent:0, scrapped:0}:
// progress is a rollup over children, not a reflection of the nib's own status.
type ProgressRollup struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Percent  int `json:"percent"`
	Scrapped int `json:"scrapped"`
}

// ComputeProgress builds the canonical ProgressRollup from a set of child
// status strings. It is the single place the done/total/percent rule lives; the
// projected `progress` field and every recipe view call it, so the rollup is
// identical everywhere. See ProgressRollup for the exact definition.
func ComputeProgress(childStatuses []string) ProgressRollup {
	var r ProgressRollup
	for _, s := range childStatuses {
		if s == "scrapped" {
			r.Scrapped++
			continue
		}
		r.Total++
		if s == "completed" {
			r.Done++
		}
	}
	if r.Total > 0 {
		r.Percent = int(math.Round(float64(r.Done) / float64(r.Total) * 100))
	}
	return r
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

// Progress returns the canonical child-completion ProgressRollup over the nib's
// direct children, collecting the child set from the incoming parent links (the
// same set the Children resolver derives). See ProgressRollup / ComputeProgress
// for the exact rule.
func (p *projectionResolver) Progress(id string) any {
	var statuses []string
	for _, link := range p.r.Reader.FindIncomingLinks(id) {
		if link.LinkType == "parent" {
			statuses = append(statuses, link.FromNib.Status)
		}
	}
	return ComputeProgress(statuses)
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
