package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
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
	// Migration describes the store's migration state, omitted entirely when
	// the store is current. See storeMigration.
	Migration *migrationStatus `json:"migration,omitempty"`
}

// Migration status kinds. The field carries two materially different outcomes
// and a consumer has to branch on which: one is fixed by running a command, the
// other only by installing a different binary.
const (
	// migrationKindPending means `nibs migrate` resolves this. Steps names them.
	migrationKindPending = "pending"
	// migrationKindBlocked means the migration probe itself refused — a store
	// written by a newer nibs, a directory that cannot be enumerated — and
	// `nibs migrate` refuses the same way. Running it changes nothing.
	migrationKindBlocked = "blocked"
)

// migrationStatus is the structured form of the store's migration state.
//
// It replaced a single free-text sentence that carried three states — healthy,
// pending, probe refused — where "" meant healthy and the other two were only
// separable by matching on prose. This CLI's stated primary audience is coding
// agents, every sibling field in the envelope is structured, and the two
// non-healthy states have opposite remedies: an agent that reads "run
// nibs migrate" off a newer-store refusal loops forever.
type migrationStatus struct {
	// Kind is migrationKindPending or migrationKindBlocked.
	Kind string `json:"kind"`
	// Steps names the pending migration steps, in chain order. Empty for
	// a blocked store: nothing was decided about individual steps.
	Steps []string `json:"steps,omitempty"`
	// PartialLoad reports whether nib files exist that Core.Load does not yet look
	// at, so every other finding in this report speaks for a store that loaded only
	// part of itself.
	//
	// It is a POINTER because the answer has three states and a consumer has to be
	// able to tell them apart: true and false are both emitted, while nil (the
	// field absent) means "not established" — the only honest answer for a blocked
	// store, where the probe refused before anything was decided about the files.
	// An absent-means-false field reported a clean, complete picture for a store
	// that may have loaded nothing.
	//
	// It is derived from the FILES, not from the pending step's kind. Keying it on
	// "a shape step is pending" made a store whose nibs are already under data/,
	// and whose only pending work is its own location, report partial_load: true
	// and "nib files outside data/ are missing" for a store that loaded completely
	// — so an agent branching on the field discarded an accurate report.
	PartialLoad *bool `json:"partial_load,omitempty"`
	// Message is the human sentence the text report prints. Present in both
	// kinds; for a blocked store it is the probe's own refusal.
	Message string `json:"message"`
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate configuration and nib integrity",
	Long: `Checks configuration and nib integrity, including:
- Configuration settings (colors, default type)
- A pending store migration, or a store this build cannot read at all
- Unparseable nib files (skipped at load, so missing from every query)
- Duplicate nib ids on disk (one file silently shadows another)
- Out-of-enum field values (loaded as written, so they sort and filter oddly)
- Broken links (links to non-existent nibs)
- Self-references (nibs linking to themselves)
- Circular dependencies (cycles in blocks/parent relationships)

Plain check is the one command that still runs on a store needing migration —
it is the diagnostic for exactly that state — and it reports the migration as an
issue, so an otherwise clean store exits 1 until ` + "`nibs migrate`" + ` has run.

Use --fix to automatically remove broken links and self-references. --fix WRITES,
so unlike plain check it refuses a store needing migration.
Note: cycles, unparseable files, duplicate ids, out-of-enum values and a pending
migration cannot be auto-fixed and require manual intervention.`,
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
	migration := storeMigration(app)

	// === Nib file checks ===
	// Reported BEFORE the link checks because a load-time problem explains link
	// problems below it: a skipped file makes every link pointing at that nib
	// look broken, and a shadowed duplicate means the links being checked are
	// the surviving file's.
	if !checkJSON {
		ui.Println()
		ui.Println(ui.Bold.Render("Nib Files"))
		// First of all, because it explains everything under it: a store with
		// a pending migration loads only part of itself (or none of itself).
		if migration != nil {
			ui.Printf("  %s %s\n", ui.Danger.Render("✗"), migration.Message)
		}
		renderLoadDiagnostics(linkResult, loadWasPartial(migration))
		renderFieldDiagnostics(app, linkResult)
	}

	// === Nib link checks ===
	if !checkJSON {
		ui.Println()
		ui.Println(ui.Bold.Render("Nib Links"))
	}

	// Report broken documents
	if !checkJSON && !checkFix {
		for _, bd := range linkResult.BrokenDocuments {
			ui.Printf("  %s %s: broken document link %s\n", ui.Danger.Render("✗"), stripControlChars(bd.NibID), stripControlChars(bd.Path))
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
				ui.Printf("  %s %s: removed broken link %s:%s\n", ui.Success.Render("✓"), stripControlChars(bl.NibID), bl.LinkType, stripControlChars(bl.Target))
			}
			for _, bl := range kept {
				ui.Printf("  %s %s: kept %s:%s — its target's file failed to load, so the link is unresolvable for now, not broken\n",
					ui.Warning.Render("!"), stripControlChars(bl.NibID), bl.LinkType, stripControlChars(bl.Target))
			}
			for _, sl := range linkResult.SelfLinks {
				ui.Printf("  %s %s: removed self-reference in %s link\n", ui.Success.Render("✓"), stripControlChars(sl.NibID), sl.LinkType)
			}
			for _, bd := range linkResult.BrokenDocuments {
				ui.Printf("  %s %s: removed broken document link %s\n", ui.Success.Render("✓"), stripControlChars(bd.NibID), stripControlChars(bd.Path))
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
			ui.Printf("  %s %s: broken link %s:%s\n", ui.Danger.Render("✗"), stripControlChars(bl.NibID), bl.LinkType, stripControlChars(bl.Target))
		}
		for _, sl := range linkResult.SelfLinks {
			ui.Printf("  %s %s: self-reference in %s link\n", ui.Danger.Render("✗"), stripControlChars(sl.NibID), sl.LinkType)
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
	// store whose only problems are load-time or field-level still has clean
	// links, and HasIssues() covers all kinds.
	if !checkJSON && linkResult.TotalIssues()-linkResult.LoadIssues()-linkResult.EnumIssues() == 0 && fixed == 0 {
		ui.Printf("  %s No link issues found\n", ui.Success.Render("✓"))
	}

	// === Summary ===
	totalIssues := len(configErrors) + linkResult.TotalIssues()
	if migration != nil {
		totalIssues++
	}

	if checkJSON {
		result := checkResult{
			Success:      totalIssues == 0,
			ConfigErrors: configErrors,
			NibIssues:    linkResult,
			Fixed:        fixed,
			Migration:    migration,
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

// storeMigration describes the store's migration state, or nil when the store is
// current.
//
// Plain `nibs check` is exempt from the pre-run migration gate precisely so it
// can diagnose the store states that gate creates (see cmd/root.go). But
// Core.Load walks data/ and archive/ only, and on a pre-layout store every nib
// file sits at the store ROOT — so every check below runs over a store that
// loaded NOTHING and would report it healthy while every other command refuses
// it. That is the same circle the exemption exists to break, closed from the
// other side, so a pending migration is reported and counted as an issue.
//
// The probe's own refusals (a store written by a newer nibs, a directory that
// cannot be enumerated) are surfaced the same way — they are exactly what a
// diagnostic is for, and swallowing them would restore the silent pass — but as
// a DIFFERENT kind, because `nibs migrate` cannot resolve them.
//
// The probe is skipped when the pre-run gate already answered the same question
// for this store: getting past it is proof nothing is pending. `check --fix` is
// gated, so it asked twice moments apart and the second full store scan could
// only ever confirm the first.
func storeMigration(app *App) *migrationStatus {
	if app.MigrationGatePassed {
		return nil
	}
	env := newMigrateEnv(app.Core.Root())
	pending, err := pendingMigrations(env)
	if err != nil {
		// PartialLoad stays nil: the probe refused before deciding anything about
		// the files, and this kind IS reachable on a store that loaded nothing, so
		// reporting false would be a claim nobody established.
		//
		// The UNBOUNDED rendering, which is what makes check different from every
		// other surface this refusal reaches. Their copies name at most
		// maxEchoedListEntries files and send the reader here for the rest, so
		// bounding it here too would close that loop on nothing.
		return &migrationStatus{
			Kind:    migrationKindBlocked,
			Message: flattenReason(fullRefusal(err)),
		}
	}
	if len(pending) == 0 {
		return nil
	}
	status := &migrationStatus{Kind: migrationKindPending, Steps: make([]string, len(pending))}
	for i, step := range pending {
		status.Steps[i] = step.name
	}
	// The question is whether any nib file sits where Core.Load does not look, so
	// ask the files. A read failure leaves the answer unestablished rather than
	// asserting either value.
	if movable, err := layoutMovableFiles(env); err == nil {
		partial := len(movable) > 0
		status.PartialLoad = &partial
	}
	status.Message = fmt.Sprintf("Store needs migration (pending: %s) — run `nibs migrate`", strings.Join(status.Steps, ", "))
	if status.PartialLoad != nil && *status.PartialLoad {
		status.Message += fmt.Sprintf("; until then only %s/ and %s/ are loaded, so nib files outside them are missing from the checks below",
			store.DataDirName, store.ArchiveDirName)
	}
	return status
}

// fullRefusal renders err with nothing elided where it can, for the one command
// the bounded refusals point at. Any other error is already its own whole text.
func fullRefusal(err error) string {
	var newer *newerStoreError
	if errors.As(err, &newer) {
		return newer.full()
	}
	return err.Error()
}

// renderLoadDiagnostics prints the load-time integrity section in text mode.
//
// Neither condition is auto-fixable: repairing an unparseable file means editing
// YAML the user wrote, and resolving a duplicate means choosing which file to
// lose. So --fix says so per file rather than skipping them silently, which
// would leave "Fixed N issue(s)" implying it had handled everything. The
// phrasing mirrors the cycle branch, the other not-auto-fixable category.
//
// The "all loaded" checkmark is suppressed when the load was, or may have been,
// PARTIAL. On a pre-layout store it is vacuously true — 0 of 0 files failed,
// because Core.Load found nothing where it looks — and printing it directly beneath
// "Store needs migration" reads as a contradiction. It is NOT suppressed merely
// because a migration is pending: a store whose nibs are already under data/ and
// whose only pending work is its own location loaded completely, and there the
// checkmark is the truth. partialLoad carries migrationStatus.PartialLoad's three
// states, and an unestablished answer suppresses the claim like a partial one does.
func renderLoadDiagnostics(result *nibcore.LinkCheckResult, partialLoad *bool) {
	for _, uf := range result.UnparseableFiles {
		// The reason is carried through: the user has to repair the file by
		// hand, so the report has to say what is wrong with it.
		// Every field here comes from the filesystem — the path is a filename,
		// the id is derived from it — so all of them cross the rendering boundary,
		// not only the parse reason. stdout carries lipgloss styling, so the
		// boundary cannot live on this writer (see stripControlChars).
		if checkFix {
			ui.Printf("  %s Cannot auto-fix unparseable nib file %s (repair it by hand): %s\n",
				ui.Warning.Render("!"), stripControlChars(uf.Path), flattenReason(uf.Reason))
		} else {
			ui.Printf("  %s Unparseable nib file %s (skipped at load, so %s is missing from every query): %s\n",
				ui.Danger.Render("✗"), stripControlChars(uf.Path), stripControlChars(describeMissingNib(uf.NibID)), flattenReason(uf.Reason))
		}
	}
	for _, d := range result.DuplicateIDs {
		if checkFix {
			ui.Printf("  %s Cannot auto-fix duplicate id %q: %s shadows %s (choose which file to keep)\n",
				ui.Warning.Render("!"), d.NibID, stripControlChars(d.Loaded), stripControlChars(d.Shadowed))
		} else {
			ui.Printf("  %s Duplicate id %q: %s shadows %s (the shadowed file is unreachable)\n",
				ui.Danger.Render("✗"), d.NibID, stripControlChars(d.Loaded), stripControlChars(d.Shadowed))
		}
	}
	if result.LoadIssues() == 0 && partialLoad != nil && !*partialLoad {
		ui.Printf("  %s All nib files loaded\n", ui.Success.Render("✓"))
	}
}

// loadWasPartial renders the migration status as the answer
// renderLoadDiagnostics needs: it is a PARTIAL-load answer, so no migration
// pending is the definite false, and otherwise the status's own three-state
// answer passes through (nil meaning "cannot tell").
//
// Named for what it returns, not for the question the caller asks. A name
// asserting the opposite polarity reads correctly at a glance and inverts the
// report: `if *thisWasComplete(m)` would print "All nib files loaded" for exactly
// the pre-layout store that loaded nothing, and a three-state pointer makes that
// a silent bug rather than a type error.
func loadWasPartial(migration *migrationStatus) *bool {
	if migration == nil {
		partial := false
		return &partial
	}
	return migration.PartialLoad
}

// renderFieldDiagnostics prints the out-of-enum field findings in text mode,
// under the Nib Files heading (they are per-file integrity, reported next to
// the other conditions --fix cannot repair). Not auto-fixable by design, so
// --fix names them like the cycle branch does instead of skipping them
// silently. The remediation is per finding — see fieldRemediation.
func renderFieldDiagnostics(app *App, result *nibcore.LinkCheckResult) {
	for _, ie := range result.InvalidEnums {
		remedy := fieldRemediation(app, ie)
		// ie.Reason embeds a raw front-matter VALUE, so it crosses the boundary
		// like every other file-sourced field printed to stdout.
		if checkFix {
			ui.Printf("  %s Cannot auto-fix %s: %s (%s)\n",
				ui.Warning.Render("!"), ie.NibID, flattenReason(ie.Reason), remedy)
		} else {
			ui.Printf("  %s %s: %s (loads as written; %s)\n",
				ui.Danger.Render("✗"), ie.NibID, flattenReason(ie.Reason), remedy)
		}
	}
}

// fieldRemediation names the right next step for one out-of-enum finding.
// Plain check is exempt from the pre-run migration gate (see cmd/root.go), so
// its store may hold files no other command would even load — the remediation
// must not send the user somewhere that cannot help:
//
//   - A file with a version above nib.CurrentVersion was written by a newer
//     nibs; its values may be perfectly valid there, and hand-"repairing" them
//     to this build's enums would corrupt a newer store. Upgrading is the fix.
//   - A known legacy value (`priority: deferred`, the one value
//     NormalizeLegacyPriorities rewrites) is migrate-fixable only if the
//     migration HEADER SCAN sees it — detection reads the raw header line, not
//     the parsed store, so a folded/quoted spelling parses to the legacy value
//     while the scan reads something else, migrate reports nothing pending,
//     and a migrate pointer would loop forever. Re-probe the file the way the
//     gate does (readFrontMatterHeader + the step predicate) before pointing
//     at migrate; when the scan cannot see it, say so and name the way out.
//   - Any other value is a hand edit no migration step rewrites — pointing at
//     migrate would be the same no-op loop.
func fieldRemediation(app *App, ie nibcore.InvalidEnum) string {
	b, err := app.Core.Get(ie.NibID)
	if err != nil {
		// The finding came from a loaded nib, so this cannot ordinarily
		// happen; the hand-repair wording is the safe generic answer.
		return "repair the file by hand"
	}
	if b.Version > nib.CurrentVersion {
		return fmt.Sprintf("file format version %d — written by a newer nibs; upgrade nibs", b.Version)
	}
	if b.Priority == "deferred" && strings.HasPrefix(ie.Reason, "invalid priority") {
		if h, scanErr := readFrontMatterHeader(app.Core.FullPath(b)); scanErr == nil && hasDeferredPriorityHeader(h) {
			return "`nibs migrate` rewrites this legacy value"
		}
		return "legacy value in a spelling the migration scan cannot see — re-save the file with a plain scalar, or fix it by hand"
	}
	return "repair the file by hand"
}

// flattenReason collapses a parse error onto a single line so it stays inside
// the report's bullet list. yaml.v3 always wraps its message across two lines
// ("unmarshal errors:" then an indented "line N: ..."), which otherwise breaks
// the alignment of the whole section.
//
// The --json envelope carries the reason VERBATIM, which is safe for the same
// audience the boundary below exists to protect: encoding/json escapes C0 as \uXXXX,
// so a live escape sequence cannot survive into the envelope, and a machine consumer
// reading structured JSON is not rendering it to a terminal. It is the TEXT report
// that reaches an agent's transcript unescaped, which is why the boundary is applied
// here rather than to the envelope.
//
// A parse error quotes the file it came from, so it is file-sourced text and goes
// through the control-character boundary first (see stripControlChars) — a `\e`
// scalar inside a nib's front matter would otherwise reach the terminal from the
// report. Truncation is deliberately not applied: this also renders nibs' own
// multi-file refusals, where the length is the information.
func flattenReason(reason string) string {
	return strings.Join(strings.Fields(stripControlChars(reason)), " ")
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
// The skipped set is nibcore.SkippedIDSet — the SAME builder Core.FixBrokenLinks
// gates its writes on — because the two answer two halves of one question: the
// core decides what to write, this decides what to say, and a report claiming a
// removal that did not happen is its own defect
// (TestCheckFixKeepsLinksToSkippedNibs pins the agreement end to end).
func partitionLinksToSkipped(result *nibcore.LinkCheckResult, configPrefix string) (kept, removed []nibcore.BrokenLink) {
	skipped := nibcore.SkippedIDSet(result.UnparseableFiles, configPrefix)

	kept = []nibcore.BrokenLink{}
	for _, bl := range result.BrokenLinks {
		if skipped[bl.Target] {
			kept = append(kept, bl)
			continue
		}
		removed = append(removed, bl)
	}
	return kept, removed
}
