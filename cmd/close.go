package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
)

// closeDefaultStatus returns the close reason a bare `nibs close` produces:
// the first done-role status. The two functions here share one derivation and
// still ask different questions — "which reason does a bare close record" and
// "which reason is an accomplishment" — and the done role answers both:
// recording an accomplishment is what a bare close means, and no other role
// says anything was accomplished (dropped work releases its dependents too,
// but nothing got done).
func closeDefaultStatus() string {
	return firstDoneStatus()
}

// closeCompletionStatus returns the one close reason that counts as progress,
// and so the only one that rewrites the parent's Current Focus (see the parent
// propagation below).
func closeCompletionStatus() string {
	return firstDoneStatus()
}

// firstDoneStatus is the shared derivation: the first done-role status in
// DefaultStatuses order. TestStatusRoleGroupsAreNonEmpty forbids an empty done
// group in the declared vocabulary, so only a test-mutated vocabulary reaches
// the "" fallback.
func firstDoneStatus() string {
	if names := config.Default().DoneStatusNames(); len(names) > 0 {
		return names[0]
	}
	return ""
}

// closeSummaryHeading is the section `close` writes its record into. Matched
// case-insensitively at any level (see mdsection.Find).
const closeSummaryHeading = "Summary"

// closeEntryDateFormat is the date layout stamped on a ## Summary entry — the
// day only. The entry says which day a reason was recorded; the file's
// updated_at already carries the precise instant. The day is taken in UTC, the
// zone updated_at is written in, so the two agree on which day a close happened
// even when the machine running it sits on the other side of midnight.
const closeEntryDateFormat = "2006-01-02"

// closeNow reads the clock for that date stamp. It is a variable so tests can
// pin the date and assert the rendered entry; nothing in the command reassigns
// it. What zone it returns does not matter — closeSummaryEntry converts to UTC
// before formatting.
var closeNow = time.Now

var (
	closeSummary string
	closeAs      string
	closeForce   bool
	closeIfMatch string
	closeJSON    bool
)

var closeCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a nib with a summary (completed, or --as another closed status)",
	Long: `Closes a nib with a summary, recording why it left the board. --as picks the
close reason from the closed statuses (` + closeDefaultStatus() + ` when omitted); they are ordinary
status names, not a separate close-reason vocabulary. An open status is a validation error.

close is the verb that closes an existing nib: ` + "`nibs set -s <closed status>`" + ` is refused,
because close requires a summary and set does not. Going the other way is not refused —
there is no reopen command, so ` + "`nibs set -s todo`" + ` on a closed nib still works.

The summary lands in a ## Summary section as a dated, reason-stamped entry, and closing
an already-closed nib is allowed: it appends another entry rather than replacing the
record, so a reason can be revised without losing why it was closed the first time.

If the nib has a parent, closing it as ` + closeCompletionStatus() + ` rewrites the parent's Current Focus;
every close reason merges Key Decisions into the parent, including the ones that set work
aside rather than finish it. Revising a reason does not retract an earlier close's parent
write: the "Completed <id>: <summary>" line an earlier ` + closeCompletionStatus() + ` close put in the
parent's Current Focus stays there when the reason is revised to another one, so the
parent still reads as though the work landed — correct it by hand when that matters.
The --if-match flag protects the target nib only; the parent update uses its own etag internally.`,
	Args: codedExactArgs(&closeJSON, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		ctx := context.Background()
		resolver := app.newResolver()

		// --as names a close reason out of the status vocabulary itself: the word
		// it takes is the word that lands in the nib's `status:`, so there is no
		// second set of reason names to keep in sync. Checking it against
		// IsClosedStatus rather than a list kept here means a newly declared
		// closed status is accepted without touching this command, and an open
		// status is rejected without naming one.
		if !app.Config().IsClosedStatus(closeAs) {
			return cmdError(closeJSON, output.ErrValidation,
				"invalid --as status: %s (must be a closed status: %s)",
				closeAs, strings.Join(app.Config().ClosedStatusNames(), ", "))
		}

		// Find the nib
		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil || b == nil {
			return cmdError(closeJSON, output.ErrNotFound, "nib not found: %s", args[0])
		}

		// An already-closed nib is NOT rejected: closing it again is how a close
		// reason gets revised, and the ## Summary write below accrues, so the
		// earlier rationale survives the second close.

		// Resolve the summary from the input channel ("-" for stdin, "@FILE" for a
		// file) — prose never rides on argv, mirroring `nibs new --body` and
		// `nibs body --set`. A trailing newline is trimmed so the appended
		// ## Summary section does not accrue a blank line.
		summary, err := resolveAppendFlag(closeSummary)
		if err != nil {
			return inputError(closeJSON, err)
		}
		if summary == "" {
			return cmdError(closeJSON, output.ErrValidation, "--summary is required (use '-' for stdin or '@FILE')")
		}

		// Check children status
		children := resolver.Orderer.Members(graph.ScopeParent, b.ID)
		if len(children) > 0 && !closeForce {
			var incomplete []string
			for _, child := range children {
				if !app.Config().IsClosedStatus(child.Status) {
					incomplete = append(incomplete, child.ID)
				}
			}
			if len(incomplete) > 0 {
				return cmdError(closeJSON, output.ErrValidation,
					"incomplete children: %s (use --force to close anyway)", strings.Join(incomplete, ", "))
			}
		}

		// Save original body before mutation (needed for Key Decisions extraction)
		originalBody := b.Body

		// Record the close by ADDING an entry to ## Summary, never by replacing the
		// section: a nib can be closed again under a different reason, and a
		// replacing write would destroy the earlier rationale — the exact record
		// this ritual exists to capture. Same shape as the Key Decisions merge
		// below: find, extend, replace.
		entry := closeSummaryEntry(closeAs, closeNow(), summary)
		newBody := b.Body
		if existing, found := mdsection.Find(newBody, closeSummaryHeading, mdsection.AnyLevel); found {
			// A blank line between entries keeps each one its own Markdown
			// paragraph. Summaries run to several lines, so entries separated by a
			// single newline would render as one run-on paragraph. A section that
			// is only a stub heading has no earlier entry to separate from, so it
			// takes the entry on its own — the same content the append branch below
			// writes.
			content := "\n" + entry + "\n"
			if kept := strings.TrimRight(existing, "\n"); strings.TrimSpace(kept) != "" {
				content = kept + "\n\n" + entry + "\n"
			}
			newBody = mdsection.Replace(newBody, closeSummaryHeading, content, mdsection.AnyLevel)
		} else {
			// Set is the wildcard-match variant (matches a "Summary" heading at any
			// level); appendLevel 2 creates it at "## " when absent.
			newBody, _ = mdsection.Set(newBody, 2, closeSummaryHeading, "\n"+entry+"\n")
		}

		// Build update input
		status := closeAs
		input := model.UpdateNibInput{
			Status: &status,
			Body:   &newBody,
		}
		if closeIfMatch != "" {
			input.IfMatch = &closeIfMatch
		}

		b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
		if err != nil {
			// A reconcilable ETag conflict carries the server's current etag so an
			// agent can retry with it (the "409 → retry with the server etag"
			// reconcile), mirroring `nibs set`.
			return setMutationError(closeJSON, err)
		}

		// Update parent milestone if present
		if b.Parent != "" {
			parent, parentErr := resolver.Query().Nib(ctx, b.Parent)
			if parentErr == nil && parent != nil {
				parentBody := parent.Body

				// Current Focus answers "what is the latest progress here", so only a
				// completion rewrites it — and it rewrites rather than appends, because
				// the parent should show the latest progress, not accumulate history.
				// Setting work aside or abandoning it is not progress: rewriting the
				// parent's focus for those would erase the record of the last real
				// progress and leave the parent reading as though nothing had happened.
				// Gating the write here is also what keeps the "Completed" verb off the
				// reasons it would misdescribe.
				if closeAs == closeCompletionStatus() {
					focusContent := fmt.Sprintf("\nCompleted %s: %s\n", b.ID, summary)
					parentBody, _ = mdsection.Set(parentBody, 2, "Current Focus", focusContent)
				}

				// Merge Key Decisions from the closed nib into the parent, for EVERY
				// close reason: why work was set aside or abandoned is exactly what a
				// later reader looks for in the parent. All matches are wildcard
				// (AnyLevel) to preserve the historic level-agnostic behavior.
				if childDecisions, found := mdsection.Find(originalBody, "Key Decisions", mdsection.AnyLevel); found {
					existingDecisions, hasExisting := mdsection.Find(parentBody, "Key Decisions", mdsection.AnyLevel)
					if hasExisting {
						// Merge only what the parent does not already carry. Closing runs
						// this merge every time, and a nib can be closed more than once to
						// revise the reason — re-appending the whole child section would
						// leave the parent holding one copy of the child's decisions per
						// close.
						if added := unmergedDecisions(existingDecisions, childDecisions); added != "" {
							merged := strings.TrimRight(existingDecisions, "\n") + "\n\n" + added
							parentBody = mdsection.Replace(parentBody, "Key Decisions", merged, mdsection.AnyLevel)
						}
					} else {
						parentBody, _ = mdsection.Set(parentBody, 2, "Key Decisions", childDecisions)
					}
				}

				if parentBody != parent.Body {
					parentETag := parent.ETag()
					parentInput := model.UpdateNibInput{Body: &parentBody, IfMatch: &parentETag}
					if _, updateErr := resolver.Mutation().UpdateNib(ctx, parent.ID, parentInput); updateErr != nil {
						fmt.Fprintf(os.Stderr, "warning: closed %s but failed to update parent %s: %v\n", b.ID, parent.ID, updateErr)
					}
				}
			}
		}

		// Lean card echo — the same projection path `nibs get` uses (no body/etag
		// unless explicitly asked).
		card, _ := projection.ViewFields(string(projection.ViewCard))
		return echoCard(closeJSON, b, resolver.ProjectionResolver(ctx), card)
	},
}

// closeSummaryEntry renders one ## Summary entry: which reason closed the nib,
// the day that was recorded, and the summary text.
//
//	**Deferred 2026-07-27** — waiting on the upstream provider release
//
// The reason word is the status the close writes, capitalized — not a second
// vocabulary looked up beside it. A newly declared closed status therefore
// stamps its own name here with no edit, exactly as --as accepts it without one.
//
// The date is the UTC day of when, whatever zone when carries: updated_at is
// written in UTC, and a local-time stamp would date the same close a day off
// from it for anyone whose offset crosses midnight.
func closeSummaryEntry(status string, when time.Time, summary string) string {
	return fmt.Sprintf("**%s %s** — %s", capitalizeFirst(status), when.UTC().Format(closeEntryDateFormat), summary)
}

// unmergedDecisions returns the part of a child's Key Decisions the parent's
// section does not already carry, ready to be appended to it — empty when the
// parent carries all of it already.
//
// The comparison is per line, on the line's trimmed text, which is what makes
// the merge idempotent without freezing it: a second close of the same child
// copies nothing up a second time, while a child that GAINS a decision between
// two closes still sends the new line up. A whole-section comparison would get
// the first half right and the second wrong.
//
// A blank line is kept only between two lines that are both being merged, so a
// section written as several paragraphs arrives as several paragraphs rather
// than collapsing into one, and the appended block starts and ends on content.
func unmergedDecisions(existing, incoming string) string {
	seen := make(map[string]bool)
	for _, line := range strings.Split(existing, "\n") {
		if text := strings.TrimSpace(line); text != "" {
			seen[text] = true
		}
	}

	var merged []string
	blankPending := false
	for _, line := range strings.Split(incoming, "\n") {
		text := strings.TrimSpace(line)
		if text == "" {
			blankPending = len(merged) > 0
			continue
		}
		if seen[text] {
			continue
		}
		seen[text] = true
		if blankPending {
			merged = append(merged, "")
			blankPending = false
		}
		merged = append(merged, line)
	}
	if len(merged) == 0 {
		return ""
	}
	return strings.Join(merged, "\n") + "\n"
}

// capitalizeFirst upper-cases the first rune of s and leaves the rest exactly as
// it was. Only the first rune, so a hyphenated status keeps its shape —
// `wont-fix` stamps as "Wont-fix", still recognizable as the status the nib
// carries rather than a re-spelled variant of it.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func init() {
	closeCmd.Flags().StringVar(&closeSummary, "summary", "", "Summary input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	// The usage string is interpolated at package load, so it lists the closed
	// statuses declared then; RunE re-derives the accepted set per invocation,
	// and that check is what actually admits or rejects a value.
	closeCmd.Flags().StringVar(&closeAs, "as", closeDefaultStatus(),
		"Close reason: which closed status to set ("+strings.Join(config.Default().ClosedStatusNames(), ", ")+")")
	closeCmd.Flags().BoolVar(&closeForce, "force", false, "Close even if children are incomplete")
	closeCmd.Flags().StringVar(&closeIfMatch, "if-match", "", "Only close if etag matches (optimistic locking)")
	closeCmd.Flags().BoolVar(&closeJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(closeCmd)
}
