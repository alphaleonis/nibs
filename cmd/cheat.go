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
func cheatSheet(cfg *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, `nibs — agentic issue tracker. One-screen grammar; detail: nibs catalog <topic> · nibs <cmd> --help

READ   get <id…>          nib document (default); -f/--view id|ref|card|full; --json → {nib}
       list [filters]     TSV "# <n> nibs"; --json → {nibs,count,truncated}; -c count; -q ids only
       rel <id> --rel R   related nibs, same envelope. R: parent,children,siblings,blocking,blocked-by,
                          mentions-out/in,ancestors,descendants,*-transitive,neighbours[-active]
       recipes            context [id] · plan <id> · roadmap · list --ready
WRITE  new "<title>" -t T create; also -s -p -e --parent --blocked-by --tag --after/--before/--first
       set <id>           metadata/links; --clear priority|estimate|parent; --remove-tag/-blocked-by/…
       body <id>          --set | --append | --section "## H" --set [--create] | --replace-old T --replace-new U
       mv <id[…]>         --after|--before|--first <anchor> | --parent <id> | --children-of <p> <id…>
       rm <id…>           --archive (default) | --delete (irreversible); agents pass -f/--force
       close <id> --summary -   mark completed, append summary, propagate to parent
META   cheat · catalog <fields|filters|recipes|examples|hierarchy|schema> · prime[ --full] · query (GraphQL)

VIEWS  id < ref < card < full (leanest→fullest). -f adds exact fields, e.g. -f "id,blocked-by(id,status)".
       get → full document · list/rel → ref TSV · --json → card. body & etag are opt-in (-f body/etag).
INPUT  prose/multi-line is ALWAYS '-' (stdin) or '@FILE', never inline — body, new -d, close --summary.
EXIT   0 ok · 2 validation · 3 not-found · 4 conflict · 5 io.  --json error → {error:{code,message}}
TYPE   %s   (hierarchy: nibs catalog hierarchy)
STATUS %s   (-s/--no-status groups: open|closed|parked; deferred=parked, excl. --ready)
PRIO   %s (default normal)   EST  %s (s=1 m=3 l=5 xl=8; default m)
FILTER list/rel show OPEN only by default (closed statuses hidden; header notes "N hidden — --all to include").
       -s overrides (-s closed = only closed); --all = every status. Open work under X: 'rel <id> --rel
       descendants -t bug' is already open — no post-filter. -c/-q honor the open default (--all for totals).
RULE   On any nibs error: STOP, find the root cause, never silently retry.
`,
		strings.Join(cfg.TypeNames(), ", "),
		strings.Join(cfg.StatusNames(), ", "),
		strings.Join(cfg.PriorityNames(), ", "),
		strings.Join(cfg.EstimateNames(), ", "),
	)
	return b.String()
}

func init() {
	rootCmd.AddCommand(cheatCmd)
}
