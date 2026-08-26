package nibcore

import (
	"fmt"
	"sort"

	"github.com/alphaleonis/nibs/internal/fsutil"
)

// RewriteAreaAssignments rewrites the `area:` of every nib for which rewrite
// returns a replacement, and returns the ids it wrote, in id order.
//
// It is the cascade half of `nibs area rename` and `nibs area rm`: renaming a
// declared node moves the paths of every nib assigned to it or to anything below
// it, and retiring one has to dispose of its members before the declaration can
// go. Both are bulk writes, so they go through one fsutil.DirSyncBatch — one
// directory flush per DIRECTORY, and deferred, because an aborted cascade has
// already committed every rename before the failure and still owes those entries
// a flush.
//
// The write is the NON-CREATING one (updateOnDiskDeferDirSync). Every target is
// a nib already on disk, so a path this cascade cannot find is a path that went
// stale — and a creating write answers that by writing the nib back under its
// pre-rename name, leaving the store one file heavier under a prefix its config
// no longer declares. The caller re-derives its paths under the lock so this
// does not arise; the refusal is what makes a caller that forgot fail loudly
// instead of duplicating the store.
//
// CONCURRENCY: lock is PROOF-OF-LOCK — the *StoreLock the caller received from
// AcquireStoreLock, held across the whole verb (see MigrateV0ToV1, which takes
// it for the same reason). This method cannot self-acquire, because the flock is
// per-descriptor and re-acquiring it in-process deadlocks.
//
// It is a parameter rather than a per-operation acquisition because the cascade
// is only HALF of one edit: the config write that follows it is a
// read-modify-write of the whole `areas:` block, and the two have to be inside
// one critical section or a concurrent edit of another node is lost — last
// writer wins, both cascades persist, both callers report success. That is the
// state the members are then permanently write-refused in, and nothing prints a
// reason to rerun. Holding the lock per-operation here would serialize the
// cascades and leave exactly that window open between them.
//
// IT DOES NOT GO THROUGH Update, and cannot. Update re-checks the `area:` the
// nib will carry against the declared vocabulary (ValidateArea), and a rename
// has no vocabulary that declares both ends: write the config first and every
// member's stored value is undeclared, write the members first and their new one
// is. The refusal is right for an ordinary write and wrong for the one command
// whose job is to move the vocabulary, so this path validates nothing about
// areas and the CALLER owes the check — it holds the declared tree on both sides
// of the edit, which is the only place that judgment can be made.
//
// ORDER IS BY ID, and it is a contract rather than a tidiness: the caller's
// partial-failure message names the nib that refused, and a loop that stopped
// wherever a Go map happened to iterate would name a different one on every run.
//
// The rewrite function is called UNDER c.mu with one nib's stored area, and must
// return (new value, true) to claim it — the empty string being the legal value
// that clears an assignment. It must not call back into Core, which would
// deadlock, and it is asked only about nibs that carry an area at all.
//
// Copy-on-write, per the canonical live-pointer invariant (see
// NibReader.GetSnapshot in internal/graph/interfaces.go): Area is a non-Path
// field, so a changed nib is cloned, written and reinstalled under its key
// rather than edited in place, leaving any off-lock reader still holding the old
// pointer a stable value.
func (c *Core) RewriteAreaAssignments(lock *StoreLock, rewrite func(area string) (string, bool)) ([]string, error) {
	if err := c.requireStoreLock("RewriteAreaAssignments", lock); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	type target struct {
		id   string
		area string
	}
	var targets []target
	for id, b := range c.nibs {
		if b.Area == "" {
			continue
		}
		next, claimed := rewrite(b.Area)
		if !claimed || next == b.Area {
			continue
		}
		targets = append(targets, target{id: id, area: next})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })

	// One directory fsync per directory the cascade touched, not one per nib.
	// Deferred so an aborted cascade still flushes what it did write: the first
	// error returns with the earlier files already renamed into place.
	var pending fsutil.DirSyncBatch
	defer pending.Flush()

	written := make([]string, 0, len(targets))
	for _, t := range targets {
		clone := c.nibs[t.id].Clone()
		clone.Area = t.area

		dir, err := c.updateOnDiskDeferDirSync(clone)
		pending.Add(dir)
		if err != nil {
			return written, fmt.Errorf("%s: %w", t.id, err)
		}
		c.nibs[t.id] = clone
		written = append(written, t.id)
	}
	return written, nil
}
