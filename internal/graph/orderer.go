package graph

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync/atomic"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// Scope identifies which ordering axis an operation runs on. What varies per
// scope lives in its scopeOps entry; everything else is shared code below.
type Scope uint8

const (
	// ScopeParent orders the sibling set under one resolved parent. The empty
	// group id is the ROOT group: nibs whose parent link resolves to no nib.
	ScopeParent Scope = iota
	// ScopeMilestone orders a milestone's queue — the group a nib's
	// `milestone:` field resolves to (membership.ResolvedMilestoneID). The
	// empty group id means MEMBERLESS: a nib assigned to no milestone is in no
	// queue at all, so Move errors there and a default Place clears the key.
	ScopeMilestone
	numScopes
)

func (s Scope) String() string {
	switch s {
	case ScopeParent:
		return "parent"
	case ScopeMilestone:
		return "milestone"
	}
	return fmt.Sprintf("scope(%d)", uint8(s))
}

// scopeOps is one scope's set of switch points.
type scopeOps struct {
	key    func(*nib.Nib) string
	setKey func(*nib.Nib, string)
	// group resolves the nib's container; "" means what the Scope constant says.
	group func(*nib.Nib, NibReader) string
	// rawMembers enumerates the group unsorted and un-backfilled.
	rawMembers             func(*Orderer, string) []*nib.Nib
	emptyGroupIsMemberless bool
	// defaultPlace positions b among siblings; never called with an empty set.
	defaultPlace func(*Orderer, *nib.Nib, string, []*nib.Nib)
	// The two membership error tiers: the anchor does not exist at all, or
	// exists outside the group.
	errAnchorNotFound  func(id string) error
	errAnchorNotMember func(id string) error
	// errNoGroup is the memberless refusal (milestone scope only).
	errNoGroup func(id string) error
}

var scopeTable = [numScopes]scopeOps{
	ScopeParent: {
		key:    func(b *nib.Nib) string { return b.Order },
		setKey: func(b *nib.Nib, k string) { b.Order = k },
		group:  resolvedParentID,
		rawMembers: func(o *Orderer, groupID string) []*nib.Nib {
			if groupID == "" {
				var roots []*nib.Nib
				for _, b := range o.reader.All() {
					if resolvedParentID(b, o.reader) == "" {
						roots = append(roots, b)
					}
				}
				return roots
			}
			var siblings []*nib.Nib
			for _, link := range o.reader.FindIncomingLinks(groupID) {
				if link.LinkType == "parent" {
					siblings = append(siblings, link.FromNib)
				}
			}
			return siblings
		},
		defaultPlace: func(o *Orderer, b *nib.Nib, groupID string, siblings []*nib.Nib) {
			// Roots append last, with no priority awareness; children insert
			// last among siblings of the same priority.
			if groupID == "" {
				b.Order = nib.OrderLast(siblings[len(siblings)-1].Order)
				return
			}
			o.placeDefaultByPriority(b, siblings)
		},
		errAnchorNotFound: func(id string) error {
			return fmt.Errorf("sibling nib not found: %s", id)
		},
		errAnchorNotMember: func(id string) error {
			return fmt.Errorf("nib %s is not a sibling (different parent)", id)
		},
	},
	ScopeMilestone: {
		key:    func(b *nib.Nib) string { return b.MilestoneOrder },
		setKey: func(b *nib.Nib, k string) { b.MilestoneOrder = k },
		group:  resolvedMilestoneID,
		rawMembers: func(o *Orderer, groupID string) []*nib.Nib {
			if groupID == "" {
				return nil
			}
			var members []*nib.Nib
			for _, b := range o.reader.All() {
				if resolvedMilestoneID(b, o.reader) == groupID {
					members = append(members, b)
				}
			}
			return members
		},
		emptyGroupIsMemberless: true,
		defaultPlace: func(o *Orderer, b *nib.Nib, _ string, siblings []*nib.Nib) {
			b.MilestoneOrder = nib.OrderLast(siblings[len(siblings)-1].MilestoneOrder)
		},
		errAnchorNotFound: func(id string) error {
			return fmt.Errorf("queue nib not found: %s", id)
		},
		errAnchorNotMember: func(id string) error {
			return fmt.Errorf("nib %s is not in the same milestone queue", id)
		},
		errNoGroup: func(id string) error {
			return fmt.Errorf("nib %s is assigned to no milestone, so it has no queue position", id)
		},
	},
}

func (s Scope) ops() *scopeOps {
	return &scopeTable[s]
}

// resolvedMilestoneID is the milestone-queue group of b: the shared definition
// of "directly assigned" (membership.ResolvedMilestoneID) over a reader-backed
// Lookup.
func resolvedMilestoneID(b *nib.Nib, reader NibReader) string {
	return membership.ResolvedMilestoneID(b, func(id string) *nib.Nib {
		n, err := reader.Get(id)
		if err != nil {
			return nil
		}
		return n
	})
}

// Orderer is the two-scope ordering engine.
type Orderer struct {
	reader NibReader
	writer NibWriter

	// warnedStalePath latches the stale-path warning to ONE line per Orderer
	// (see warnStalePathOnce). Atomic because `nibs serve` builds one Orderer
	// at startup and serves every request through it concurrently.
	warnedStalePath atomic.Bool
}

func NewOrderer(reader NibReader, writer NibWriter) *Orderer {
	return &Orderer{reader: reader, writer: writer}
}

// Members returns the scope's group sorted by its ordering key, lazily
// backfilling a key onto any member that lacks one. A memberless group id
// returns nil.
func (o *Orderer) Members(scope Scope, groupID string) []*nib.Nib {
	ops := scope.ops()
	members := ops.rawMembers(o, groupID)
	o.backfillKeys(scope, members)
	nib.SortByKey(members, ops.key)
	return members
}

// Place computes b's ordering key for ENTERING its group in the scope, with the
// group derived from b itself. A default placement lands where the scope's
// policy says; an explicit position anchors among the current members. An
// unassigned nib in a memberless scope takes a default Place as "no key" (the
// key is cleared) and refuses an anchored one.
//
// Mutates only b's own scope key; the caller owns b (a clone) and persists it.
func (o *Orderer) Place(scope Scope, b *nib.Nib, pl Placement) error {
	ops := scope.ops()
	groupID := ops.group(b, o.reader)
	if groupID == "" && ops.emptyGroupIsMemberless {
		if pl.isDefault {
			ops.setKey(b, "")
			return nil
		}
		return ops.errNoGroup(b.ID)
	}

	siblings := excludeSelf(o.Members(scope, groupID), b.ID)
	if len(siblings) == 0 {
		ops.setKey(b, nib.OrderInitial())
		return nil
	}
	if pl.isDefault {
		ops.defaultPlace(o, b, groupID, siblings)
		return nil
	}
	return o.position(scope, b, pl.pos, siblings)
}

// Move repositions b within the group it is already in. Moving a nib that is in
// no group (memberless scopes only) is an error.
//
// Mutates only b's own scope key; the caller owns b (a clone) and persists it.
func (o *Orderer) Move(scope Scope, b *nib.Nib, pos Position) error {
	ops := scope.ops()
	groupID := ops.group(b, o.reader)
	if groupID == "" && ops.emptyGroupIsMemberless {
		return ops.errNoGroup(b.ID)
	}
	siblings := excludeSelf(o.Members(scope, groupID), b.ID)
	return o.position(scope, b, pos, siblings)
}

// Recalculate assigns b a fresh key at the scope's default position in its
// CURRENT group — call it after changing b's container, so b enters the new
// group where a created nib would. In a memberless scope an unassigned nib's
// key is cleared instead.
func (o *Orderer) Recalculate(scope Scope, b *nib.Nib) {
	ops := scope.ops()
	groupID := ops.group(b, o.reader)
	if groupID == "" && ops.emptyGroupIsMemberless {
		ops.setKey(b, "")
		return
	}
	siblings := excludeSelf(o.Members(scope, groupID), b.ID)
	if len(siblings) == 0 {
		ops.setKey(b, nib.OrderInitial())
		return
	}
	ops.defaultPlace(o, b, groupID, siblings)
}

// position dispatches an explicit Position over the (self-excluded) sibling
// set. First on an empty set degrades to the initial key; an anchored form on
// an empty set falls through to the anchor lookup and reports its error tier.
func (o *Orderer) position(scope Scope, b *nib.Nib, pos Position, siblings []*nib.Nib) error {
	ops := scope.ops()
	switch pos.kind {
	case posFirst:
		if len(siblings) == 0 {
			ops.setKey(b, nib.OrderInitial())
			return nil
		}
		ops.setKey(b, nib.OrderFirst(ops.key(siblings[0])))
		return nil
	case posAfter:
		return o.positionAfter(scope, b, pos.anchor, siblings)
	case posBefore:
		return o.positionBefore(scope, b, pos.anchor, siblings)
	}
	// A zero Position cannot come off the wire (PositionFromArgs refuses the
	// no-flag shape); reaching this arm is a programming error at a call site.
	return fmt.Errorf("a move requires a position (after, before or first)")
}

// excludeSelf drops the nib being positioned, so it never anchors against
// itself and default placement ignores its old spot.
func excludeSelf(members []*nib.Nib, id string) []*nib.Nib {
	filtered := make([]*nib.Nib, 0, len(members))
	for _, m := range members {
		if m.ID != id {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// sameGroup compares group ids alone: in a memberless scope two unassigned nibs
// come back as the same group. Refuse that case before asking.
func (o *Orderer) sameGroup(scope Scope, x, y *nib.Nib) bool {
	ops := scope.ops()
	return ops.group(x, o.reader) == ops.group(y, o.reader)
}

// backfillKeys assigns ordering keys to members that lack them, after the last
// keyed member.
func (o *Orderer) backfillKeys(scope Scope, members []*nib.Nib) {
	if len(members) == 0 {
		return
	}
	ops := scope.ops()

	needsBackfill := false
	for _, b := range members {
		if ops.key(b) == "" {
			needsBackfill = true
			break
		}
	}
	if !needsBackfill {
		return
	}

	nib.SortByKey(members, ops.key)

	lastKey := ""
	for _, b := range members {
		if k := ops.key(b); k != "" && k > lastKey {
			lastKey = k
		}
	}

	for i := range members {
		b := members[i]
		if ops.key(b) != "" {
			continue
		}
		newKey := nib.OrderBetween(lastKey, "")

		// The etag comes from b, before the mutation, never from the clone.
		etag := b.ETag()
		lastKey = newKey

		// Mutate an OWNED clone from GetForUpdate, never the shared reader pointer
		// (b is c.nibs[id]): a refused write must not leave the shared in-memory
		// sibling showing a phantom key that was never persisted. GetForUpdate
		// fails only not-found — the sibling was deleted concurrently, so there
		// is nothing left to backfill.
		clone, err := o.reader.GetForUpdate(b.ID)
		if err != nil {
			continue
		}
		ops.setKey(clone, newKey)

		// Best-effort persist: a sibling that cannot be written falls back to
		// title sort, and Members returns no error, so nothing propagates. This
		// runs on the hot Children/root READ path and leaves the key empty on
		// failure, so the Update is re-attempted on EVERY read — warning on a
		// refusal that repeats (an etag divergence, an `area:` the vocabulary no
		// longer declares, an unparseable file) would flood stderr under a
		// long-running `nibs serve`. fs.ErrNotExist is latched instead; see
		// warnStalePathOnce.
		if err := o.writer.Update(clone, &etag); err != nil {
			var etagMismatch *nibcore.ETagMismatchError
			var unparseable *nibcore.OnDiskUnparseableError
			var undeclaredArea *config.AreaError
			switch {
			case errors.As(err, &etagMismatch), errors.As(err, &unparseable), errors.As(err, &undeclaredArea):
			case errors.Is(err, fs.ErrNotExist):
				o.warnStalePathOnce(b.ID, err)
			default:
				fmt.Fprintf(os.Stderr, "warning: could not backfill order key for %s: %v — this sibling stays unordered (falls back to title sort) until the next successful write\n", b.ID, err)
			}
			continue
		}
		// The write installed the clone as the new c.nibs[id]; return that, not
		// the pre-write pointer.
		members[i] = clone
	}
}

// warnStalePathOnce reports, at most once per Orderer, that a backfill could not
// reach a nib's file because nothing is at the path the loaded store recorded.
//
// Once, because the condition is store-wide rather than per-nib: unlatched, one
// read warns per unkeyed sibling and every later read repeats the whole set.
//
// Not zero, because for a read-only reader this line is the only signal there
// is: browsing through `nibs serve` or the TUI triggers no mutation, so the
// write-boundary diagnostic (cmd/set.go's FILE_ERROR) never fires, and where
// StartWatching failed nothing refreshes the store either.
func (o *Orderer) warnStalePathOnce(id string, err error) {
	if o.warnedStalePath.Swap(true) {
		return
	}
	// The message prescribes a re-read and nothing else: a fresh process never
	// loads a nib whose file is gone, so `nibs check` cannot see this condition.
	fmt.Fprintf(os.Stderr, "warning: could not backfill order key for %s: %v — nothing is at the path this process recorded for it, so it stays unordered (falls back to title sort); the file was renamed or removed after this process loaded the store, and only a re-read sees that (restart a running `nibs serve`/`nibs tui`). Further occurrences are not reported.\n", id, err)
}

// positionAfter places b after the target member. The two error tiers: an
// anchor that does not exist at all reports not-found; one that exists outside
// the group reports the membership error.
func (o *Orderer) positionAfter(scope Scope, b *nib.Nib, targetID string, siblings []*nib.Nib) error {
	ops := scope.ops()
	normalizedID, ok := o.reader.NormalizeID(targetID)
	if !ok {
		return ops.errAnchorNotFound(targetID)
	}
	targetID = normalizedID
	for i, s := range siblings {
		if s.ID == targetID {
			// Defensive: production callers pass a group-filtered slice (Members).
			if !o.sameGroup(scope, s, b) {
				return ops.errAnchorNotMember(targetID)
			}
			// Scan past duplicate keys (legacy data) to a member with a
			// DIFFERENT key: OrderBetween of two equal bounds returns a key
			// greater than both, so a duplicate is no usable boundary.
			nextKey := ""
			for j := i + 1; j < len(siblings); j++ {
				if ops.key(siblings[j]) != ops.key(s) {
					nextKey = ops.key(siblings[j])
					break
				}
			}
			ops.setKey(b, nib.OrderBetween(ops.key(s), nextKey))
			return nil
		}
	}
	if t, err := o.reader.Get(targetID); err == nil && !o.sameGroup(scope, t, b) {
		return ops.errAnchorNotMember(targetID)
	}
	return ops.errAnchorNotFound(targetID)
}

// positionBefore places b before the target member; see positionAfter for the
// error tiers and the duplicate-key boundary rule.
func (o *Orderer) positionBefore(scope Scope, b *nib.Nib, targetID string, siblings []*nib.Nib) error {
	ops := scope.ops()
	normalizedID, ok := o.reader.NormalizeID(targetID)
	if !ok {
		return ops.errAnchorNotFound(targetID)
	}
	targetID = normalizedID
	for i, s := range siblings {
		if s.ID == targetID {
			if !o.sameGroup(scope, s, b) {
				return ops.errAnchorNotMember(targetID)
			}
			prevKey := ""
			for j := i - 1; j >= 0; j-- {
				if ops.key(siblings[j]) != ops.key(s) {
					prevKey = ops.key(siblings[j])
					break
				}
			}
			ops.setKey(b, nib.OrderBetween(prevKey, ops.key(s)))
			return nil
		}
	}
	if t, err := o.reader.Get(targetID); err == nil && !o.sameGroup(scope, t, b) {
		return ops.errAnchorNotMember(targetID)
	}
	return ops.errAnchorNotFound(targetID)
}

// placeDefaultByPriority inserts b last among siblings of the same or higher
// priority — the parent scope's default for child nibs.
func (o *Orderer) placeDefaultByPriority(b *nib.Nib, siblings []*nib.Nib) {
	cfg := o.reader.Config()

	newRank := cfg.PriorityRank(b.Priority)

	insertAfterIdx := -1
	for i, s := range siblings {
		if cfg.PriorityRank(s.Priority) <= newRank {
			insertAfterIdx = i
		}
	}

	switch {
	case insertAfterIdx == -1:
		// Every sibling has lower priority.
		b.Order = nib.OrderFirst(siblings[0].Order)
	case insertAfterIdx == len(siblings)-1:
		b.Order = nib.OrderLast(siblings[insertAfterIdx].Order)
	default:
		b.Order = nib.OrderBetween(siblings[insertAfterIdx].Order, siblings[insertAfterIdx+1].Order)
	}
}
