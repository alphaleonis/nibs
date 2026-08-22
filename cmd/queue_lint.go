package cmd

import (
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
)

// inversionKey identifies one queue inversion by the ids it is made of, so
// the set a mutation found can be compared with the set it left without
// holding the live store pointers graph.QueueInversion carries.
type inversionKey struct {
	milestone, ahead, blocker string
}

// queueInversionKeys is the BEFORE half of the lint decision 2.3 asks for — a
// warning once, at the mutation that creates the inversion. It snapshots the
// inversions the subject takes part in, as keys, so queueInversionWarning can
// report only the pairs the mutation added. A subject in no queue (or one no
// nib answers to) snapshots as the empty set; a mutation that then refuses
// never reaches the warning, so the snapshot costs one read and nothing else.
func queueInversionKeys(reader graph.NibReader, id string) map[inversionKey]bool {
	inversions := graph.QueueInversionsInvolving(reader, id)
	keys := make(map[inversionKey]bool, len(inversions))
	for _, inv := range inversions {
		keys[inversionKey{inv.Milestone, inv.Ahead.ID, inv.Blocker.ID}] = true
	}
	return keys
}

// queueInversionWarning is the AFTER half: the lint the mutations that can
// put a new pair into a queue run once their write has landed — `nibs set
// --milestone`, `--blocked-by` and `--blocking`, and `nibs mv --queue`. It
// renders graph.QueueInversionsInvolving (the one definition of an inversion;
// see its doc for the rule) as a single warning line naming the pairs the
// subject takes part in now and did not before the write, or "" when the
// write created none. before is the snapshot queueInversionKeys took ahead of
// the write; an inversion that was already there — a queue move that does not
// cross the blocker, a reassignment to the same milestone — is not reported
// again, since it was reported by the mutation that created it.
//
// It is a lint, not a refusal: an inversion is legal — plans state importance,
// dependencies state feasibility — so the write has already landed by the time
// this runs, and the warning only names the pairs so the author can decide
// whether the order was intended. Every pair the write created goes on the
// one line, so a move that inverts against several blockers is reported once,
// not once per blocker. Ids come from filenames and front-matter links, so they
// cross the rendering boundary like every other id on stderr.
func queueInversionWarning(reader graph.NibReader, id string, before map[inversionKey]bool) string {
	var created []graph.QueueInversion
	for _, inv := range graph.QueueInversionsInvolving(reader, id) {
		if !before[inversionKey{inv.Milestone, inv.Ahead.ID, inv.Blocker.ID}] {
			created = append(created, inv)
		}
	}
	if len(created) == 0 {
		return ""
	}
	pairs := make([]string, len(created))
	for i, inv := range created {
		pairs[i] = fmt.Sprintf("%s is ahead of %s, which still blocks it",
			stripControlChars(inv.Ahead.ID), stripControlChars(inv.Blocker.ID))
	}
	return fmt.Sprintf("warning: queue order and dependencies disagree in milestone %s: %s (inversions are legal — plans state importance, dependencies state feasibility; reorder with `nibs mv <id> --queue --after|--before <anchor>` if the order was unintended)",
		stripControlChars(created[0].Milestone), strings.Join(pairs, "; "))
}
