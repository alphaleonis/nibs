package graph

import (
	"errors"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// Scope identifies which ordering axis an operation runs on. The positioning
// grammar (after/before/first/default), the duplicate-key boundary skipping,
// the two membership error tiers and the lazy key backfill exist once,
// parameterized by scope; what varies per scope lives in its scopeOps entry.
type Scope uint8

const (
	// ScopeParent orders the sibling set under one resolved parent. The empty
	// group id is the ROOT group: nibs whose parent link resolves to no nib.
	ScopeParent Scope = iota
	// ScopeMilestone orders a milestone's queue — the group a nib's
	// `milestone:` field resolves to (membership.ResolvedMilestoneID). The
	// empty group id means MEMBERLESS — a nib assigned to no milestone is in
	// no queue at all: Move errors there, and a default Place clears the
	// queue key. Reached by the assignment write (validateAndSetMilestone)
	// and by reorderNib in the MILESTONE scope.
	ScopeMilestone
	numScopes
)

// String names the scope for subtests and diagnostics.
func (s Scope) String() string {
	switch s {
	case ScopeParent:
		return "parent"
	case ScopeMilestone:
		return "milestone"
	}
	return fmt.Sprintf("scope(%d)", uint8(s))
}

// scopeOps is one scope's set of switch points: how to read and write its
// ordering key, how a nib resolves to its group, how a group enumerates its
// raw members, what the default placement is, and the nouns its membership
// errors speak in. Everything else — grammar, backfill, boundary skipping,
// error tiers — is shared code in the methods below.
type scopeOps struct {
	key    func(*nib.Nib) string
	setKey func(*nib.Nib, string)
	// group resolves the nib's container in this scope; "" has the per-scope
	// meaning documented on the Scope constants.
	group func(*nib.Nib, NibReader) string
	// rawMembers enumerates the group unsorted and un-backfilled.
	rawMembers func(*Orderer, string) []*nib.Nib
	// emptyGroupIsMemberless: "" is a real group in the parent scope (the
	// roots) and no group at all in the milestone scope.
	emptyGroupIsMemberless bool
	// defaultPlace assigns the scope's default position among siblings
	// (never empty here — the empty set short-circuits to OrderInitial).
	defaultPlace func(*Orderer, *nib.Nib, string, []*nib.Nib)
	// errAnchorNotFound / errAnchorNotMember are the two membership error
	// tiers: the anchor does not exist at all, or exists outside the group.
	errAnchorNotFound  func(id string) error
	errAnchorNotMember func(id string) error
	// errNoGroup is the memberless refusal (milestone scope only).
	errNoGroup func(id string) error
}

// scopeTable holds every scope's ops; Scope.ops is the single dispatch point.
var scopeTable = [numScopes]scopeOps{
	ScopeParent: {
		key:    func(b *nib.Nib) string { return b.Order },
		setKey: func(b *nib.Nib, k string) { b.Order = k },
		group:  resolvedParentID,
		rawMembers: func(o *Orderer, groupID string) []*nib.Nib {
			if groupID == "" {
				// Root-ness comes from resolvedParentID, so the root set here
				// is the same one the query surfaces report.
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
			// Root nibs: append last (no priority-aware positioning — use
			// reorderNib to reposition). Child nibs: insert last among
			// siblings of the same priority.
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
			// A full-store scan: an index can move BEHIND this shape without an
			// API change if a profile ever asks for one.
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

// resolvedMilestoneID is the milestone-queue group of b, answered by THE
// shared definition of "directly assigned" — membership.ResolvedMilestoneID,
// which reads the `milestone:` field — through a reader-backed Lookup.
// Reader.Get is resolvedParent's own rule (normalization included; a dangling
// link is no assignment), so the ordering engine and every membership
// consumer read one definition.
func resolvedMilestoneID(b *nib.Nib, reader NibReader) string {
	return membership.ResolvedMilestoneID(b, func(id string) *nib.Nib {
		n, err := reader.Get(id)
		if err != nil {
			return nil
		}
		return n
	})
}

// Orderer is the two-scope ordering engine, with only read/write dependencies.
type Orderer struct {
	reader NibReader
	writer NibWriter
}

// NewOrderer creates an Orderer with the given reader and writer.
func NewOrderer(reader NibReader, writer NibWriter) *Orderer {
	return &Orderer{reader: reader, writer: writer}
}

// Members returns the scope's group sorted by its ordering key, lazily
// backfilling a key onto any member that lacks one. In the parent scope the
// empty group id names the roots; in the milestone scope it names nothing and
// returns nil.
func (o *Orderer) Members(scope Scope, groupID string) []*nib.Nib {
	ops := scope.ops()
	members := ops.rawMembers(o, groupID)
	o.backfillKeys(scope, members)
	nib.SortByKey(members, ops.key)
	return members
}

// Place computes b's ordering key for ENTERING its group in the scope — at
// creation, or after a reassignment put it there. The group is derived from b
// itself. A default placement is allowed and lands where the scope's policy
// says; an explicit position anchors among the current members. An unassigned
// nib in a memberless scope takes a default Place as "no key" (the key is
// cleared) and refuses an anchored one.
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

// Move repositions b within the group it is already in. There is no default
// arm — a Position always names a destination — and moving a nib that is in
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

// Recalculate assigns b a fresh key at the scope's default position among its
// CURRENT group — the hook a reassignment calls after changing the nib's
// container, so it enters the new group where a created nib would. In a
// memberless scope an unassigned nib's key is cleared instead.
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

// excludeSelf returns members without the nib being positioned, so a nib never
// anchors against itself and default placement ignores its old spot.
func excludeSelf(members []*nib.Nib, id string) []*nib.Nib {
	filtered := make([]*nib.Nib, 0, len(members))
	for _, m := range members {
		if m.ID != id {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// sameGroup reports whether x and y sit in the same group of the scope. Two
// nibs whose container links both resolve to nothing are in the same group
// exactly when "" names a real group there (the parent scope's roots).
func (o *Orderer) sameGroup(scope Scope, x, y *nib.Nib) bool {
	ops := scope.ops()
	return ops.group(x, o.reader) == ops.group(y, o.reader)
}

// backfillKeys assigns ordering keys to members that lack them.
// Unkeyed nibs are appended after the last keyed member.
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

	// Sort for stable baseline (keyed first by key, unkeyed by title)
	nib.SortByKey(members, ops.key)

	// Find the last existing ordering key
	lastKey := ""
	for _, b := range members {
		if k := ops.key(b); k != "" && k > lastKey {
			lastKey = k
		}
	}

	// Assign keys to unkeyed nibs, appending after the last keyed one.
	for i := range members {
		b := members[i]
		if ops.key(b) != "" {
			continue
		}
		newKey := nib.OrderBetween(lastKey, "")

		// Compute ETag BEFORE mutation so it matches the on-disk version.
		etag := b.ETag()
		lastKey = newKey

		// Mutate an OWNED clone from GetForUpdate, never the shared reader pointer
		// (b is c.nibs[id]): a refused write must not leave the shared in-memory
		// sibling showing a phantom key that was never persisted.
		// GetForUpdate fails only not-found: the sibling was deleted between the
		// snapshot above and here (a concurrent external/`serve` delete). It's gone,
		// so there is nothing to backfill — quietly skip it (not a write failure, and
		// the nib no longer exists, so no warning is warranted).
		clone, err := o.reader.GetForUpdate(b.ID)
		if err != nil {
			continue
		}
		ops.setKey(clone, newKey)

		// Best-effort persist: ordering falls back to title sort if this fails.
		// backfillKeys runs on the hot Children/root READ path (once per
		// parent per tree render/poll), and a persistently unwritable sibling
		// keeps an empty key so needsBackfill never clears — meaning this Update is
		// re-attempted on EVERY read. Classify the error so a steady-state
		// failure does not flood stderr under a long-running `nibs serve`:
		//   - *ETagMismatchError: a stable on-disk etag divergence (e.g. a
		//     hand-authored nib missing an order key AND both timestamps, whose
		//     synthesized-from-mtime in-memory etag permanently differs from the
		//     stored one). This is the already-accepted best-effort fallback — the
		//     failed clone's computed key is DISCARDED (members[i] keeps its
		//     pre-write, unkeyed pointer), so the sibling falls back to title sort;
		//     the write simply cannot land. Stay quiet.
		//   - *config.AreaError: the nib carries an `area:` the store's vocabulary
		//     no longer declares. Read-tolerant by design, so the value survives
		//     every load and the refusal is as stable as an etag divergence —
		//     but unlike one it is not even a divergence to reconcile, and
		//     nothing this loop can do clears it. Without this arm the warning
		//     is re-emitted on EVERY read of the parent, forever.
		//   - *OnDiskUnparseableError: the file is corrupt/unreadable. Suppressing
		//     our OWN warning here avoids the orderer emitting a line per read on the
		//     hot Children/root path; the condition is still surfaced where it
		//     matters — at the write/pre-validation boundary (cmd/update.go →
		//     FILE_ERROR, bulk-reorder pre-validation). nibcore.computeStoredETag now
		//     RETURNS this error instead of logging it, so suppressing here means the
		//     read path emits no warning at all (no orderer line, no nibcore
		//     double-log, no flood).
		// Warn only on a genuinely unexpected write failure (disk I/O, etc.) so a
		// real problem stays diagnosable (matches activateParentChain's stderr
		// warning). Propagating is not an option: Members returns no error and has
		// many callers, so this stays best-effort.
		if err := o.writer.Update(clone, &etag); err != nil {
			var etagMismatch *nibcore.ETagMismatchError
			var unparseable *nibcore.OnDiskUnparseableError
			var undeclaredArea *config.AreaError
			if !errors.As(err, &etagMismatch) && !errors.As(err, &unparseable) && !errors.As(err, &undeclaredArea) {
				fmt.Fprintf(os.Stderr, "warning: could not backfill order key for %s: %v — this sibling stays unordered (falls back to title sort) until the next successful write\n", b.ID, err)
			}
			continue
		}
		// The write installed the clone as the new c.nibs[id]; reflect the
		// persisted key in the returned slice without touching the pre-write
		// pointer.
		members[i] = clone
	}
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
			// Defensive: every production caller passes a member slice already
			// filtered by group (Members). This guard fires only for direct
			// unit tests that hand-build a mixed list.
			if !o.sameGroup(scope, s, b) {
				return ops.errAnchorNotMember(targetID)
			}
			// Find the next member with a different ordering key to get a real
			// boundary. Duplicate keys (from legacy data) would cause
			// OrderBetween to produce a key that collides with the nib's
			// current one.
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
	// Target was resolved (exists) but not in the member list — that means
	// it belongs to a different group. Surface a clearer error than "not found".
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

	// Find the last sibling with priority >= new nib's priority (rank <= newRank)
	insertAfterIdx := -1
	for i, s := range siblings {
		if cfg.PriorityRank(s.Priority) <= newRank {
			insertAfterIdx = i
		}
	}

	switch {
	case insertAfterIdx == -1:
		// All siblings have lower priority — insert first
		b.Order = nib.OrderFirst(siblings[0].Order)
	case insertAfterIdx == len(siblings)-1:
		// Insert after the last sibling
		b.Order = nib.OrderLast(siblings[insertAfterIdx].Order)
	default:
		// Insert between insertAfterIdx and insertAfterIdx+1
		b.Order = nib.OrderBetween(siblings[insertAfterIdx].Order, siblings[insertAfterIdx+1].Order)
	}
}
