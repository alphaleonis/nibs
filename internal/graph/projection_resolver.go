package graph

import (
	"context"
	"math"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/projection"
)

// ProgressRollup is the value projected for the computed `progress` field, and
// the canonical child-completion rollup the recipe views (context, roadmap)
// reuse, so `nibs get <id> -f progress` and those views report the same number.
// Build it only via ComputeProgress — do not fork the rule.
//
// Canonical definition (single source of truth). Each child falls into exactly
// one of three buckets:
//   - Done    = children whose status is "completed" — the one status that means
//     the work actually happened. They also count toward Total.
//   - Dropped = children whose status is "scrapped". The work will not be done
//     and is no longer scope, so it leaves the denominator entirely rather than
//     pinning the percentage below 100 forever.
//   - Pending = every other child, including "deferred". Counts toward Total,
//     not toward Done. Deferred work is set aside, not resolved — it is coming
//     back, so it is outstanding scope and the percentage must say so.
//
//   - Total    = Done + Pending; only scrapped children are excluded.
//   - Percent  = round(Done/Total*100); 0 when Total == 0.
//   - Scrapped = direct children with status "scrapped", disclosed so the
//     children missing from Total are visible rather than silently dropped.
//   - Deferred = direct children with status "deferred", disclosed so a
//     set-aside child inside Total can be told apart from work still in flight.
//
// The three closed statuses get three different treatments, so the rule names
// them individually: no combination of the Closed/ReleasesDependents flags
// separates completed from scrapped (both are closed and both release their
// dependents), and each named status is a single member rather than a rival
// definition of a status group. See config.StatusConfig.
//
// A leaf nib (no children) reports zeros across the board: progress is a rollup
// over children, not a reflection of the nib's own status.
//
// This rule is one half of a seam with the roadmap's item filter
// (cmd/roadmap.go filterChildren): the direct children a default `nibs roadmap`
// lists under a container are exactly the ones this rollup counts in Total but
// not in Done, so a container cannot both list items and claim 100%. The rollup
// is over direct children only, so a milestone can sit at 100% while an epic
// below it still lists a deferred task under a closed parent.
type ProgressRollup struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Percent  int `json:"percent"`
	Scrapped int `json:"scrapped"`
	Deferred int `json:"deferred"`
}

// ComputeProgress builds the canonical ProgressRollup from a set of child
// status strings. It is the single place the done/total/percent rule lives; the
// projected `progress` field and every recipe view call it, so the rollup is
// identical everywhere. See ProgressRollup for the exact definition.
//
// An unrecognized status (including the empty status of a nib whose front
// matter omits it) lands in the default arm and counts as outstanding scope, so
// a typo holds the percentage down rather than inflating it.
func ComputeProgress(childStatuses []string) ProgressRollup {
	var r ProgressRollup
	for _, s := range childStatuses {
		switch s {
		case "completed":
			r.Total++
			r.Done++
		case "scrapped":
			// Not scope any more — out of the denominator entirely.
			r.Scrapped++
		case "deferred":
			// Set aside, not resolved: still scope, still not done.
			r.Deferred++
			r.Total++
		default:
			r.Total++
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
			// Reading Status off the live link.FromNib pointer is safe here,
			// unlike the resolvers that hand nibs to gqlgen: Status is not
			// mutated in place on a stored pointer (status changes go through
			// Update, which installs a fresh pointer), and this method returns a
			// computed ProgressRollup value, so no pointer escapes to async
			// marshaling. No snapshot/clone is needed. As of the pyei
			// copy-on-write change no non-Path field is mutated in place on a
			// published stored pointer; only Path is (see NibReader.GetSnapshot
			// for the full contract).
			statuses = append(statuses, link.FromNib.Status)
		}
	}
	return ComputeProgress(statuses)
}

// Ready reports whether the nib is startable: not already closed and with no
// active blockers. BlockedByIds drops blockers whose status released them, so a
// nib blocked only by completed or scrapped work is ready — but one blocked by
// a deferred nib is not: the set-aside work is coming back and the dependency
// is unmet.
func (p *projectionResolver) Ready(id string) bool {
	b, err := p.r.Reader.Get(id)
	if err != nil {
		return false
	}
	if p.r.isClosedStatus(b.Status) {
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
