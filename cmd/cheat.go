package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alphaleonis/nibs/internal/config"
)

// cheatCmd prints the entire agent-facing grammar on one screen (<=40 lines).
// It is the fast path an agent runs once to learn the verb surface; deeper
// detail lives behind `nibs catalog <topic>` and `nibs <cmd> --help`. The enum
// lines (types/statuses/priorities/estimates) are generated from config so the
// cheat sheet can never drift from what the CLI actually accepts — the same
// anti-drift discipline `nibs catalog` follows.
var cheatCmd = &cobra.Command{
	Use:               "cheat",
	Short:             "Print the whole nibs agent grammar on one screen",
	Args:              codedNoArgs(nil),
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Default()
		fmt.Print(cheatSheet(cfg))
		return nil
	},
}

// cheatSheet renders the one-screen grammar, interpolating the live enum sets.
// The close line's default reason is interpolated from closeDefaultStatus — the
// same const `nibs close --as` defaults to and `nibs prime` renders — so the
// three agent-facing surfaces cannot disagree about what omitting --as records.
// The rel entry names relDefaultKind the same way, and brackets --rel because
// omitting it is legal: a grammar that reads as required is how the bare form
// gets run unawares, and its default answers plausibly rather than erroring.
// The next entry takes its description from the command's own Short via
// commandShort, the same source `nibs catalog recipes` reads.
func cheatSheet(cfg *config.Config) string {
	var b strings.Builder
	// The STATUS line lists the statuses by group rather than in one flat run:
	// the groups partition the vocabulary, so this names every status AND says
	// which group it filters under, in the same space. The blocker note is
	// derived too — and disappears if every closed status starts releasing its
	// dependents, rather than lingering as a rule with no members.
	blockerNote := ""
	if holding := cfg.HoldingStatusNames(); len(holding) > 0 {
		blockerNote = "; closed but still blocks: " + strings.Join(holding, ", ")
	}
	fmt.Fprintf(&b, `nibs — agentic issue tracker. One-screen grammar; detail: nibs catalog <topic> · nibs <cmd> --help

READ   get <id…>          nib document (default); -f/--view id|ref|card|full; --json → {nib}
       list [filters]     TSV "# <n> nibs"; --json → {nibs,count,truncated}; -c count; -q ids only; --ready unblocked+todo
       rel <id> [--rel R] related nibs, same envelope. R (default %s): parent,children,siblings,blocking,
                          blocked-by,mentions-out/in,ancestors,descendants,*-transitive,neighbours[-active]
       context [id]       project context summary
       plan <id>          plan view: a parent nib and its children
       roadmap            Markdown roadmap from milestones and epics
       next               %s
                          (having nothing to do is an answer: exit 0 either way — branch on --json's null action)
WRITE  new "<title>" -t T create; also -s -p -e --parent --blocked-by --tag --after/--before/--first (siblings)
       set <id>           metadata/links; --clear priority|estimate|parent|milestone; --remove-tag/-blocked-by/…
                          --milestone <ms> assigns to that milestone's queue, appended last (new cannot assign)
       body <id>          --set | --append | --section "## H" --set [--create] | --replace-old T --replace-new U
       mv <id[…]>         --after|--before|--first <anchor> | --parent <id> | --children-of <p> <id…>
                          --queue --after|--before|--first <anchor> repositions within the milestone queue
       rm <id…>           --archive (default) | --delete (irreversible); agents pass -f/--force
       close <id>         --summary - (required); --as <closed status> picks the close reason (default
                          %s). Closing an existing nib goes through close — set -s <closed> errors.
                          A milestone with open work assigned refuses a releasing reason: --move-open-to <ms>,
                          --unassign-open, or a holding reason (which keeps the queue).
META   cheat · catalog <fields|filters|recipes|examples|hierarchy|schema> · prime[ --full] · query (GraphQL)

VIEWS  id < ref < card < full (leanest→fullest). -f adds exact fields, e.g. -f "id,blocked-by(id,status)".
       get → full document · list/rel → ref TSV · --json → card. body & etag are opt-in (-f body/etag).
INPUT  prose/multi-line is ALWAYS '-' (stdin) or '@FILE', never inline — body, new -d, close --summary.
EXIT   0 ok · 1 uncategorized · 2 validation · 3 not-found · 4 conflict · 5 io.
       --json error → {error:{code,message}}
TYPE   %s   (hierarchy: nibs catalog hierarchy)
STATUS %s=%s · %s=%s   (-s/--no-status take either group%s)
PRIO   %s (default normal)   EST  %s (s=1 m=3 l=5 xl=8; default m)
FILTER list/rel show OPEN only by default (closed statuses hidden; header notes "N hidden — --all to include").
       -s overrides (-s closed = only closed); --all = every status. Open work under X: 'rel <id> --rel
       descendants -t bug' is already open — no post-filter. -c/-q honor the open default (--all for totals).
       list only: --milestone <ms> = that queue, in queue order; --backlog = no assignment, own or inherited.
RULE   On any nibs error: STOP, find the root cause, never silently retry. A queue "warning:" printed by a
       command that exited 0 is a lint on a successful write, not an error — read it, don't stop for it.
`,
		relDefaultKind,
		commandShort("next"),
		closeDefaultStatus(),
		strings.Join(cfg.TypeNames(), ", "),
		statusGroupOpen, strings.Join(cfg.OpenStatusNames(), "/"),
		statusGroupClosed, strings.Join(cfg.ClosedStatusNames(), "/"),
		blockerNote,
		strings.Join(cfg.PriorityNames(), ", "),
		strings.Join(cfg.EstimateNames(), ", "),
	)
	return b.String()
}

func init() {
	rootCmd.AddCommand(cheatCmd)
}
