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
// lifetime — one O(N) Compute per operation instead of an O(N) store scan per
// projected nib.
//
// The memo is why an instance is POINT-IN-TIME: create a projectionResolver
// after any write whose result it should reflect, and never reuse one across
// operations — the same staleness rule the per-operation RequestCache states
// for the mention and search memos. Every current caller already obeys it
// (read commands build one per invocation; write commands build one after the
// write, to echo the result).
type projectionResolver struct {
	r        *Resolver
	nib      NibResolver
	ctx      context.Context
	viewOnce sync.Once
	view     *membership.View
}

// membershipView returns the instance's memoized membership view, building it
// on first use. sync.Once rather than a nil check so a future concurrent
// projection fan-out collapses into a single Compute instead of racing.
func (p *projectionResolver) membershipView() *membership.View {
	p.viewOnce.Do(func() {
		p.view = membership.Compute(p.r.Reader.All())
	})
	return p.view
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

// ParentID returns the nib's resolved parent id — the same reading the GraphQL
// parentId field and the hasParent filter give, so `-f parent` cannot drift
// from them (see resolvedParent). A nib that has since been deleted resolves to
// no parent, which is the honest answer for a nib that is no longer there.
func (p *projectionResolver) ParentID(id string) string {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return ""
	}
	return resolvedParentID(b, p.r.Reader)
}

// ChildCount returns the number of direct children of the nib — the
// STRUCTURAL parent axis (membership.View.Children), the same child set
// Orderer.Members derives without the ordering-key backfill a count does not
// need. Deliberately not DirectMembers: childCount answers "how many nibs name
// this one as parent", and it keeps doing so when the membership axis moves to
// `milestone:` assignment.
func (p *projectionResolver) ChildCount(id string) int {
	return len(p.membershipView().Children(id))
}

// Progress returns the canonical child-completion progress.Rollup over the
// nib's direct members (membership.View.DirectMembers — the same set the
// Children resolver derives on legal data). See progress.Rollup /
// progress.ByCount for the exact rule.
//
// Reading Status off the view's live pointers is safe here, unlike the
// resolvers that hand nibs to gqlgen: Status is not mutated in place on a
// stored pointer (status changes go through Update, which installs a fresh
// pointer), and this method returns a computed progress.Rollup value, so no
// pointer escapes to async marshaling. No snapshot/clone is needed. As of the
// pyei copy-on-write change no non-Path field is mutated in place on a
// published stored pointer; only Path is (see NibReader.GetSnapshot for the
// full contract).
func (p *projectionResolver) Progress(id string) any {
	members := p.membershipView().DirectMembers(id)
	statuses := make([]string, len(members))
	for i, m := range members {
		statuses[i] = m.Status
	}
	return progress.ByCount(statuses)
}

// Ready reports whether the nib can be started: it carries a startable status
// and has no active blockers. BlockedByIds drops blockers whose status released
// them, so a nib blocked only by completed or scrapped work is ready — but one
// blocked by a deferred nib is not: the set-aside work is coming back and the
// dependency is unmet.
//
// The status half is config.IsStartableStatus, which is narrower than "not
// closed": a draft or in-progress nib reports ready:false. `nibs list --ready`
// reads that same flag, so the field and the filter narrow by status from one
// definition — pinned by TestReadyProjectionAndFilterAgree in cmd.
//
// The blocker half agrees as well, but on matching rules rather than on shared
// code: this field walks BlockedByIds → Reader.Get, while the filter walks
// Core.IsBlocked → findActiveBlockersInMap → normalizeIDInMap. Each spells out
// the same resolution — the exact id, then the configured prefix prepended — so
// a hand-edited nib naming its blocker by short id is withheld by both, and an
// entry naming no nib at all is dropped by both. What a resolved blocker then
// counts for is genuinely one definition: both ask
// config.StatusReleasesDependents. TestReadyProjectionAndFilterAgree drives a
// blocker under both spellings, so neither copy of the resolution rule can
// drift alone.
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
