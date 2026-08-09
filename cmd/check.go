package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	checkJSON bool
	checkFix  bool
)

type checkResult struct {
	Success      bool                     `json:"success"`
	ConfigErrors []string                 `json:"config_errors"`
	NibIssues    *nibcore.LinkCheckResult `json:"nib_issues,omitempty"`
	Fixed        int                      `json:"fixed,omitempty"`
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate configuration and nib integrity",
	Long: `Checks configuration and nib integrity, including:
- Configuration settings (colors, default type)
- Unparseable nib files (skipped at load, so missing from every query)
- Duplicate nib ids on disk (one file silently shadows another)
- Broken links (links to non-existent nibs)
- Self-references (nibs linking to themselves)
- Circular dependencies (cycles in blocks/parent relationships)

Use --fix to automatically remove broken links and self-references.
Note: cycles, unparseable files and duplicate ids cannot be auto-fixed and
require manual intervention.`,
	Args: codedNoArgs(&checkJSON), // operates on the whole store; takes no positional args
	RunE: func(cmd *cobra.Command, args []string) error {
		totalIssues, err := runCheck(getApp(cmd))
		if err != nil {
			return err
		}
		// Exit with error code if validation failed
		if totalIssues > 0 {
			os.Exit(1)
		}
		return nil
	},
}

// runCheck runs every check and renders the report (text or --json), returning
// the number of issues left outstanding.
//
// Split out of checkCmd.RunE so tests can drive the whole report: RunE exits the
// process on a non-zero count, which would take a test binary down with it.
func runCheck(app *App) (int, error) {
	var configErrors []string
	var fixed int

	// === Configuration checks ===
	if !checkJSON {
		ui.Println(ui.Bold.Render("Configuration"))
	}

	// 1. Check statuses are defined (always true since hardcoded)
	if !checkJSON {
		ui.Printf("  %s Statuses defined (%d hardcoded)\n", ui.Success.Render("✓"), len(config.DefaultStatuses))
	}

	// 2. Check default_status exists in statuses (always true since hardcoded)
	if !checkJSON {
		ui.Printf("  %s Default status '%s' exists\n", ui.Success.Render("✓"), app.Config().GetDefaultStatus())
	}

	// 2b. Check default_type is a valid hardcoded type
	if app.Config().GetDefaultType() != "" && !app.Config().IsValidType(app.Config().GetDefaultType()) {
		configErrors = append(configErrors, fmt.Sprintf("default_type '%s' is not a valid type", app.Config().GetDefaultType()))
	} else if app.Config().GetDefaultType() != "" {
		if !checkJSON {
			ui.Printf("  %s Default type '%s' is valid\n", ui.Success.Render("✓"), app.Config().GetDefaultType())
		}
	}

	// 3. Check all status colors are valid (hardcoded statuses)
	for _, s := range config.DefaultStatuses {
		if !ui.IsValidColor(s.Color) {
			configErrors = append(configErrors, fmt.Sprintf("invalid color '%s' for status '%s'", s.Color, s.Name))
		}
	}
	if !checkJSON {
		colorErrors := 0
		for _, e := range configErrors {
			if len(e) > 13 && e[:13] == "invalid color" {
				colorErrors++
			}
		}
		if colorErrors == 0 {
			ui.Printf("  %s All status colors valid\n", ui.Success.Render("✓"))
		}
	}

	// 4. Check all type colors are valid (hardcoded types)
	for _, t := range config.DefaultTypes {
		if !ui.IsValidColor(t.Color) {
			configErrors = append(configErrors, fmt.Sprintf("invalid color '%s' for type '%s'", t.Color, t.Name))
		}
	}
	if !checkJSON {
		typeColorErrors := 0
		for _, e := range configErrors {
			if len(e) > 13 && e[:13] == "invalid color" {
				typeColorErrors++
			}
		}
		if typeColorErrors == 0 {
			ui.Printf("  %s All type colors valid\n", ui.Success.Render("✓"))
		}
	}

	// Print config errors in human-readable mode
	if !checkJSON {
		for _, e := range configErrors {
			ui.Printf("  %s %s\n", ui.Danger.Render("✗"), e)
		}
	}

	linkResult := app.Core.CheckAllLinks()

	// === Nib file checks ===
	// Reported BEFORE the link checks because a load-time problem explains link
	// problems below it: a skipped file makes every link pointing at that nib
	// look broken, and a shadowed duplicate means the links being checked are
	// the surviving file's.
	if !checkJSON {
		ui.Println()
		ui.Println(ui.Bold.Render("Nib Files"))
		renderLoadDiagnostics(linkResult)
	}

	// === Nib link checks ===
	if !checkJSON {
		ui.Println()
		ui.Println(ui.Bold.Render("Nib Links"))
	}

	// Report broken documents
	if !checkJSON && !checkFix {
		for _, bd := range linkResult.BrokenDocuments {
			ui.Printf("  %s %s: broken document link %s\n", ui.Danger.Render("✗"), bd.NibID, bd.Path)
		}
	}

	// Handle --fix mode
	if checkFix && (len(linkResult.BrokenLinks) > 0 || len(linkResult.SelfLinks) > 0 || len(linkResult.BrokenDocuments) > 0) {
		fixedCount, err := app.Core.FixBrokenLinks()
		if err != nil {
			return 0, fmt.Errorf("fixing broken links: %w", err)
		}
		fixed = fixedCount

		// FixBrokenLinks deliberately KEEPS a link whose target is only skipped
		// this load — the target's file is on disk and needs a YAML repair, so
		// deleting the edge would destroy something repairing the file would
		// have restored. Partition the same way here, or the report claims a
		// removal that did not happen.
		kept, removed := partitionLinksToSkipped(linkResult, app.Config().Nibs.Prefix)

		if !checkJSON {
			for _, bl := range removed {
				ui.Printf("  %s %s: removed broken link %s:%s\n", ui.Success.Render("✓"), bl.NibID, bl.LinkType, bl.Target)
			}
			for _, bl := range kept {
				ui.Printf("  %s %s: kept %s:%s — its target's file failed to load, so the link is unresolvable for now, not broken\n",
					ui.Warning.Render("!"), bl.NibID, bl.LinkType, bl.Target)
			}
			for _, sl := range linkResult.SelfLinks {
				ui.Printf("  %s %s: removed self-reference in %s link\n", ui.Success.Render("✓"), sl.NibID, sl.LinkType)
			}
			for _, bd := range linkResult.BrokenDocuments {
				ui.Printf("  %s %s: removed broken document link %s\n", ui.Success.Render("✓"), bd.NibID, bd.Path)
			}
		}

		// Clear only what was actually fixed. The kept links stay in the result
		// so the issue count and the --json envelope keep reporting them.
		linkResult.BrokenLinks = kept
		linkResult.SelfLinks = []nibcore.SelfLink{}
		linkResult.BrokenDocuments = []nibcore.BrokenDocument{}
	} else if !checkJSON {
		// Report issues without fixing
		for _, bl := range linkResult.BrokenLinks {
			ui.Printf("  %s %s: broken link %s:%s\n", ui.Danger.Render("✗"), bl.NibID, bl.LinkType, bl.Target)
		}
		for _, sl := range linkResult.SelfLinks {
			ui.Printf("  %s %s: self-reference in %s link\n", ui.Danger.Render("✗"), sl.NibID, sl.LinkType)
		}
	}

	// Cycles cannot be auto-fixed
	if !checkJSON {
		for _, c := range linkResult.Cycles {
			if checkFix {
				ui.Printf("  %s Cannot auto-fix cycle: %s (via %s)\n", ui.Warning.Render("!"), formatCycle(c.Path), c.LinkType)
			} else {
				ui.Printf("  %s Circular dependency: %s (via %s)\n", ui.Danger.Render("✗"), formatCycle(c.Path), c.LinkType)
			}
		}
	}

	// Show success if no issues. This speaks only for the LINK categories: a
	// store whose only problems are load-time still has clean links, and
	// HasIssues() covers both kinds.
	if !checkJSON && linkResult.TotalIssues()-linkResult.LoadIssues() == 0 && fixed == 0 {
		ui.Printf("  %s No link issues found\n", ui.Success.Render("✓"))
	}

	// === Summary ===
	totalIssues := len(configErrors) + linkResult.TotalIssues()

	if checkJSON {
		result := checkResult{
			Success:      totalIssues == 0,
			ConfigErrors: configErrors,
			NibIssues:    linkResult,
			Fixed:        fixed,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		ui.Println(string(data))
	} else {
		ui.Println()
		if totalIssues == 0 && fixed == 0 {
			ui.Println(ui.Success.Render("All checks passed"))
		} else if totalIssues == 0 && fixed > 0 {
			ui.Println(ui.Success.Render(fmt.Sprintf("Fixed %d issue(s)", fixed)))
		} else if fixed > 0 {
			// Some issues fixed, some remain (cycles, unparseable files, duplicate ids)
			ui.Println(ui.Warning.Render(fmt.Sprintf("Fixed %d issue(s), %d require manual intervention", fixed, totalIssues)))
		} else if totalIssues == 1 {
			ui.Println(ui.Danger.Render("1 issue found"))
		} else {
			ui.Println(ui.Danger.Render(fmt.Sprintf("%d issues found", totalIssues)))
		}
	}

	return totalIssues, nil
}

// renderLoadDiagnostics prints the load-time integrity section in text mode.
//
// Neither condition is auto-fixable: repairing an unparseable file means editing
// YAML the user wrote, and resolving a duplicate means choosing which file to
// lose. So --fix says so per file rather than skipping them silently, which
// would leave "Fixed N issue(s)" implying it had handled everything. The
// phrasing mirrors the cycle branch, the other not-auto-fixable category.
func renderLoadDiagnostics(result *nibcore.LinkCheckResult) {
	for _, uf := range result.UnparseableFiles {
		// The reason is carried through: the user has to repair the file by
		// hand, so the report has to say what is wrong with it.
		if checkFix {
			ui.Printf("  %s Cannot auto-fix unparseable nib file %s (repair it by hand): %s\n",
				ui.Warning.Render("!"), uf.Path, flattenReason(uf.Reason))
		} else {
			ui.Printf("  %s Unparseable nib file %s (skipped at load, so %s is missing from every query): %s\n",
				ui.Danger.Render("✗"), uf.Path, describeMissingNib(uf.NibID), flattenReason(uf.Reason))
		}
	}
	for _, d := range result.DuplicateIDs {
		if checkFix {
			ui.Printf("  %s Cannot auto-fix duplicate id %q: %s shadows %s (choose which file to keep)\n",
				ui.Warning.Render("!"), d.NibID, d.Loaded, d.Shadowed)
		} else {
			ui.Printf("  %s Duplicate id %q: %s shadows %s (the shadowed file is unreachable)\n",
				ui.Danger.Render("✗"), d.NibID, d.Loaded, d.Shadowed)
		}
	}
	if result.LoadIssues() == 0 {
		ui.Printf("  %s All nib files loaded\n", ui.Success.Render("✓"))
	}
}

// flattenReason collapses a parse error onto a single line so it stays inside
// the report's bullet list. yaml.v3 always wraps its message across two lines
// ("unmarshal errors:" then an indented "line N: ..."), which otherwise breaks
// the alignment of the whole section. The --json envelope carries the reason
// verbatim, so nothing is lost where the consumer is a machine.
func flattenReason(reason string) string {
	return strings.Join(strings.Fields(reason), " ")
}

// describeMissingNib names the nib a skipped file would have provided. The id
// comes from the filename, which parses whatever the contents are — but a file
// whose name yields no id has no nib to name.
func describeMissingNib(id string) string {
	if id == "" {
		return "its nib"
	}
	return id
}

func init() {
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "Output as JSON")
	checkCmd.Flags().BoolVar(&checkFix, "fix", false, "Automatically fix broken links and self-references")
	rootCmd.AddCommand(checkCmd)
}

// partitionLinksToSkipped splits reported broken links into those whose target
// is merely SKIPPED this load (kept — the file is on disk and needs repair) and
// those whose target genuinely does not exist (removed).
//
// It mirrors Core.FixBrokenLinks' rule rather than sharing it, because the two
// answer different questions: the core decides what to write, this decides what
// to say. They must agree, which is what TestCheckFixKeepsLinksToSkippedNibs
// pins — a report claiming a removal that did not happen is its own defect.
//
// Both spellings are tested (as stored, and prefixed) because a link may name
// its target short-form, exactly as normalizeIDInMap resolves it.
func partitionLinksToSkipped(result *nibcore.LinkCheckResult, configPrefix string) (kept, removed []nibcore.BrokenLink) {
	skipped := make(map[string]bool, len(result.UnparseableFiles))
	for _, uf := range result.UnparseableFiles {
		if uf.NibID != "" {
			skipped[uf.NibID] = true
		}
	}

	kept = []nibcore.BrokenLink{}
	for _, bl := range result.BrokenLinks {
		isSkipped := skipped[bl.Target] || (configPrefix != "" && skipped[configPrefix+bl.Target])
		if isSkipped {
			kept = append(kept, bl)
			continue
		}
		removed = append(removed, bl)
	}
	return kept, removed
}
