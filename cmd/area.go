package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	areaListJSON   bool
	areaRenameJSON bool
	areaRmJSON     bool
	areaRmMoveTo   string
	areaRmUnassign bool
)

var areaCmd = &cobra.Command{
	Use:   "area",
	Short: "Read and edit the project's declared areas vocabulary",
	Long: `Areas are the one vocabulary a project authors itself — statuses, types,
priorities and estimates are fixed. They are declared as a nested ` + "`areas:`" + ` block
in the store's config.yml, and a nib is placed in one with ` + "`nibs new --area`" + ` or
` + "`nibs set --area`" + `.

These verbs read that vocabulary and edit it in place. Both mutations rewrite the
nibs assigned to what they touch, because a nib's area is a PATH: renaming a node
moves every path below it, and retiring one leaves its members pointing at a path
that no longer exists.`,
	// Cobra auto-shows help when no subcommand is given — no RunE needed.
}

var areaListCmd = &cobra.Command{
	Use:   "list",
	Short: "Print the declared areas tree with each node's description",
	Long: `Prints every declared area, nested under its parent, as the FULL path that
--area takes, followed by the description that says what belongs there.

A store that declares no areas is a normal store, not a broken one: the listing
says so and exits 0.`,
	Args: codedNoArgs(&areaListJSON),
	RunE: runAreaList,
}

var areaRenameCmd = &cobra.Command{
	Use:   "rename <path> <new-name>",
	Short: "Rename a declared area and rewrite every nib assigned below it",
	Long: `Renames the node at <path> and rewrites the ` + "`area:`" + ` of every nib assigned to
it or to any declared area beneath it — renaming a parent moves the whole
subtree's paths, so its children's members move with it.

<new-name> is a NAME, not a path: a rename changes what a node is called and
never moves it between parents. A name a sibling already holds is refused rather
than merged, because two siblings with one name would make one path mean two
nodes.

The nibs are rewritten before the declaration is, so rerunning the same command
finishes a run that failed part way.`,
	Args: codedExactArgs(&areaRenameJSON, 2),
	RunE: runAreaRename,
}

var areaRmCmd = &cobra.Command{
	Use:   "rm <path>",
	Short: "Retire a declared area, together with everything declared beneath it",
	Long: `Removes the node at <path> — and every area declared beneath it — from the
store's config.yml.

It is REFUSED while nibs are assigned at or below that node, because retiring it
would leave them carrying a path the vocabulary no longer declares, which every
later write to those nibs is refused for. Name a disposition to retire it anyway:
--move-to <area> reassigns the members to another declared area, --unassign drops
their assignment. Retiring an area nothing is assigned to needs neither.

The members are rewritten before the declaration is removed, so rerunning the
same command finishes a run whose CASCADE failed part way. A run whose cascade
finished and whose config write did not is finished by rerunning WITHOUT the
disposition flag, since nothing is assigned below the area to dispose of any
more — which is what the error says at the time.`,
	Args: codedExactArgs(&areaRmJSON, 1),
	RunE: runAreaRm,
}

func init() {
	areaListCmd.Flags().BoolVar(&areaListJSON, "json", false, "Output as JSON")
	areaRenameCmd.Flags().BoolVar(&areaRenameJSON, "json", false, "Output as JSON")
	areaRmCmd.Flags().BoolVar(&areaRmJSON, "json", false, "Output as JSON")
	areaRmCmd.Flags().StringVar(&areaRmMoveTo, "move-to", "",
		"Reassign every nib at or below the retiring area to this declared area")
	areaRmCmd.Flags().BoolVar(&areaRmUnassign, "unassign", false,
		"Drop the area assignment of every nib at or below the retiring area")
	areaRmCmd.MarkFlagsMutuallyExclusive("move-to", "unassign")

	areaCmd.AddCommand(areaListCmd, areaRenameCmd, areaRmCmd)
	rootCmd.AddCommand(areaCmd)
}

// --- list ------------------------------------------------------------------

// areaListNode is one node of the tree `area list --json` emits. Path is what
// --area takes; Name is the single segment the file declares, which is what a
// later `nibs area rename` takes.
type areaListNode struct {
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Color       string         `json:"color,omitempty"`
	Order       string         `json:"order,omitempty"`
	Children    []areaListNode `json:"children,omitempty"`
}

func runAreaList(cmd *cobra.Command, _ []string) error {
	cfg := getApp(cmd).Config()

	if areaListJSON {
		return output.JSONRaw(struct {
			Areas []areaListNode `json:"areas"`
		}{Areas: areaListNodes(cfg.Areas, "")})
	}

	if !cfg.AreasDeclared() {
		ui.Printf("This store declares no areas. Declare an `areas:` block in %s to place work by area.\n",
			sanitizeFilePath(cfg.Layout().ConfigPath()))
		return nil
	}

	rows := areaListRows(cfg.Areas, "", 0, nil)
	// Padded by RUNE count rather than by bytes: a declared name is
	// project-authored text, so fmt's width verb — which counts bytes — would
	// misalign the descriptions of every non-ASCII vocabulary.
	width := 0
	for _, row := range rows {
		if n := utf8.RuneCountInString(row.label); n > width {
			width = n
		}
	}
	for _, row := range rows {
		if row.description == "" {
			ui.Printf("%s\n", row.label)
			continue
		}
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(row.label))
		ui.Printf("%s%s  %s\n", row.label, pad, row.description)
	}
	return nil
}

// areaListRow is one printed line: the indented path, and the description that
// says what belongs in it. Both are file-sourced and already rendered.
type areaListRow struct {
	label       string
	description string
}

func areaListRows(areas []config.AreaConfig, parent string, depth int, rows []areaListRow) []areaListRow {
	for _, area := range areas {
		path := joinAreaPathForDisplay(parent, area.Name)
		rows = append(rows, areaListRow{
			label:       strings.Repeat("  ", depth) + config.RenderAreaPath(path),
			description: sanitizeFileText(area.Description),
		})
		rows = areaListRows(area.Children, path, depth+1, rows)
	}
	return rows
}

func areaListNodes(areas []config.AreaConfig, parent string) []areaListNode {
	nodes := make([]areaListNode, 0, len(areas))
	for _, area := range areas {
		path := joinAreaPathForDisplay(parent, area.Name)
		nodes = append(nodes, areaListNode{
			Path:        path,
			Name:        area.Name,
			Description: area.Description,
			Color:       area.Color,
			Order:       area.Order,
			Children:    areaListNodes(area.Children, path),
		})
	}
	return nodes
}

// joinAreaPathForDisplay rebuilds a node's path from the tree it was walked in.
// config.AreaPaths already enumerates the same paths, but the walk here has to
// carry each node's own fields alongside its path, which a flat list of strings
// cannot give back.
func joinAreaPathForDisplay(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + config.AreaPathSeparator + name
}

// --- rename ----------------------------------------------------------------

func runAreaRename(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	cfg := app.Config()
	path, newName := args[0], args[1]

	if err := requireDeclaredArea(cfg, areaRenameJSON, path, "rename"); err != nil {
		return err
	}
	parent, oldName := splitAreaPath(path)
	if err := validateNewAreaName(cfg, areaRenameJSON, path, parent, oldName, newName); err != nil {
		return err
	}
	newPath := joinAreaPathForDisplay(parent, newName)

	lock, err := beginAreaEdit(app, areaRenameJSON)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	// The config edit is resolved BEFORE the first nib is touched: see
	// planAreaEdit for why a refusal has to land while the store is still
	// untouched.
	edit, err := planAreaEdit(areaRenameJSON, func() (*config.StoredAreaEdit, error) {
		return config.PlanRenameStoredArea(cfg.StoreDir(), path, newName)
	})
	if err != nil {
		return err
	}

	// Read BEFORE the cascade: after a partial failure the set has already
	// shrunk, so a count taken then would understate what the run set out to do.
	members := areaMembers(app, cfg, path)

	// The cascade preserves whatever a member carried BELOW the renamed node,
	// because renaming a parent moves its children's paths without changing
	// their names.
	written, err := app.Core.RewriteAreaAssignments(lock, func(area string) (string, bool) {
		if !cfg.IsAreaWithin(area, path) {
			return "", false
		}
		return newPath + strings.TrimPrefix(area, path), true
	})
	if err != nil {
		return cmdError(areaRenameJSON, output.ErrFileError,
			"rewrote %d of the %s assigned at or below area %s, then %v — the vocabulary in %s still declares %s and those writes are persisted; rerun the same command to finish it, since a nib already rewritten is no longer a member and the rerun starts where this stopped",
			len(written), areaNibCount(len(members)),
			quotedArea(path), err, sanitizeFilePath(cfg.Layout().ConfigPath()), quotedArea(path))
	}

	staleLink, err := edit.Write()
	if err != nil {
		return cmdError(areaRenameJSON, output.ErrFileError,
			"rewrote %s from area %s to %s, then %s could not be updated: %v — the vocabulary still declares %s and those writes are persisted; rerun the same command to finish it, since the rewritten nibs are no longer members and the rerun only renames the declaration",
			areaNibCount(len(written)), quotedArea(path), quotedArea(newPath),
			sanitizeFilePath(cfg.Layout().ConfigPath()), err, quotedArea(path))
	}

	msg := fmt.Sprintf("Renamed area %s to %s", quotedArea(path), quotedArea(newPath))
	if len(written) > 0 {
		msg += fmt.Sprintf(" and rewrote %s: %s%s",
			areaNibCount(len(written)), strings.Join(namedIDs(written), ", "), moreThanNamed(len(written)))
	}
	return reportAreaEdit(app, areaRenameJSON, msg, staleLink, cfg)
}

// validateNewAreaName refuses every new name the vocabulary could not hold, in
// the order a caller can act on: the shape of the name first, then whether it
// changes anything, then whether a sibling already answers to it.
//
// config.PlanRenameStoredArea re-checks the RESULT before it hands back an edit
// to write, so none of this is what keeps a broken vocabulary off disk. What it
// buys is the message: that backstop can only say the edit would leave the file
// unusable, where these can name the sibling, the runnable spelling, or the fact
// that nothing would change.
func validateNewAreaName(cfg *config.Config, jsonMode bool, path, parent, oldName, newName string) error {
	if newName == "" {
		return cmdError(jsonMode, output.ErrValidation,
			"area %s needs a name to be renamed to, and the new name is empty", quotedArea(path))
	}
	if strings.TrimSpace(newName) != newName {
		return cmdError(jsonMode, output.ErrValidation,
			"the new name %s has leading or trailing whitespace; an `area:` value would have to carry the same spaces to match it",
			quotedArea(newName))
	}
	if strings.Contains(newName, config.AreaPathSeparator) {
		newParent, tail := splitAreaPath(newName)
		if newParent == parent && tail != "" {
			return cmdError(jsonMode, output.ErrValidation,
				"%s is not a name: a rename changes a node's name and never moves it between parents, so give the name alone — run `nibs area rename %s %s`",
				quotedArea(newName), config.RenderAreaPath(path), config.RenderAreaPath(tail))
		}
		return cmdError(jsonMode, output.ErrValidation,
			"%s is not a name: a rename changes a node's name and never moves it between parents, so give the name alone — `nibs area list` prints the declared tree",
			quotedArea(newName))
	}
	if newName == oldName {
		return cmdError(jsonMode, output.ErrValidation,
			"area %s is already named %s, so the rename would change nothing",
			quotedArea(path), quotedArea(newName))
	}
	if sibling := joinAreaPathForDisplay(parent, newName); cfg.IsValidArea(sibling) {
		return cmdError(jsonMode, output.ErrValidation,
			"cannot rename area %s to %s: this store already declares %s, and two siblings with one name make one path mean two nodes",
			quotedArea(path), quotedArea(newName), quotedArea(sibling))
	}
	return nil
}

// --- rm --------------------------------------------------------------------

func runAreaRm(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	cfg := app.Config()
	path := args[0]

	if err := requireDeclaredArea(cfg, areaRmJSON, path, "retire"); err != nil {
		return err
	}

	// Cobra refuses the two together (MarkFlagsMutuallyExclusive above), so at
	// most one of these is set here.
	disposition := areaDispositionNone
	switch {
	case cmd.Flags().Changed("move-to"):
		disposition = areaDispositionMove
	case areaRmUnassign:
		disposition = areaDispositionUnassign
	}

	lock, err := beginAreaEdit(app, areaRmJSON)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	members := areaMembers(app, cfg, path)

	target := ""
	if disposition == areaDispositionMove {
		if target, err = resolveAreaMoveTarget(cfg, path); err != nil {
			return err
		}
	}
	switch {
	case disposition != areaDispositionNone && len(members) == 0:
		return areaEmptyDispositionError(path, disposition)
	case disposition == areaDispositionNone && len(members) > 0:
		return cmdError(areaRmJSON, output.ErrValidation,
			"cannot retire area %s: %s assigned at or below it (%s%s) — reassign them with `nibs area rm %s --move-to <area>`, drop their assignment with `nibs area rm %s --unassign`, or leave the declaration in place",
			quotedArea(path), areaNibsAre(len(members)),
			strings.Join(namedIDs(members), ", "), moreThanNamed(len(members)),
			config.RenderAreaPath(path), config.RenderAreaPath(path))
	}

	// The config edit is resolved BEFORE the first nib is touched: see
	// planAreaEdit.
	edit, err := planAreaEdit(areaRmJSON, func() (*config.StoredAreaEdit, error) {
		return config.PlanRemoveStoredArea(cfg.StoreDir(), path)
	})
	if err != nil {
		return err
	}

	written, err := app.Core.RewriteAreaAssignments(lock, func(area string) (string, bool) {
		// Every member lands ON the target rather than keeping the remainder it
		// carried below the retiring node: the target declares no such child, so
		// preserving it would move each member to another undeclared path.
		return target, cfg.IsAreaWithin(area, path)
	})
	if err != nil {
		return cmdError(areaRmJSON, output.ErrFileError,
			"%s %d of the %s assigned at or below area %s, then %v — %s is still declared and those writes are persisted; rerun the same command to finish it, since a nib already disposed of is no longer a member and the rerun starts where this stopped",
			areaDispositionVerb(disposition), len(written), areaNibCount(len(members)),
			quotedArea(path), err, quotedArea(path))
	}

	staleLink, err := edit.Write()
	if err != nil {
		return areaRetireWriteFailure(cfg, path, disposition, len(written), err)
	}

	msg := fmt.Sprintf("Retired area %s", quotedArea(path))
	if below := countDeclaredBelow(cfg, path); below > 0 {
		msg += fmt.Sprintf(" and the %s declared beneath it", areaDeclaredCount(below))
	}
	if len(written) > 0 {
		msg += fmt.Sprintf("; %s %s: %s%s", areaDispositionVerb(disposition), areaNibCount(len(written)),
			strings.Join(namedIDs(written), ", "), moreThanNamed(len(written)))
	}
	return reportAreaEdit(app, areaRmJSON, msg, staleLink, cfg)
}

// areaRetireWriteFailure reports a retire whose members are disposed of and
// whose config write failed, and it branches on whether a disposition was
// actually NAMED rather than on which one it was.
//
// With no flag there is nothing to report as done and no flag to drop: that
// branch is reachable only for an area nothing was assigned to, where the
// refusal above has already established that the member set is empty and the
// cascade therefore wrote nothing. Selecting the wording on the move/unassign
// pair instead made this case claim an unassignment had run and prescribe
// dropping a flag the caller never typed.
func areaRetireWriteFailure(cfg *config.Config, path string, disposition areaDisposition, written int, cause error) error {
	if disposition == areaDispositionNone {
		return cmdError(areaRmJSON, output.ErrFileError,
			"area %s could not be retired: %s could not be updated: %v — nothing is assigned at or below it, so nothing was rewritten and the store is as it was; rerun `nibs area rm %s` once that is fixed",
			quotedArea(path), sanitizeFilePath(cfg.Layout().ConfigPath()), cause, config.RenderAreaPath(path))
	}
	return cmdError(areaRmJSON, output.ErrFileError,
		"%s %s from area %s, then %s could not be updated: %v — %s is still declared and those writes are persisted; rerun WITHOUT %s to retire it, which is what finishes the job now that nothing is assigned below it",
		areaDispositionVerb(disposition), areaNibCount(written), quotedArea(path),
		sanitizeFilePath(cfg.Layout().ConfigPath()), cause, quotedArea(path), areaDispositionFlag(disposition))
}

// resolveAreaMoveTarget validates --move-to's area: it has to be declared, and
// it has to survive this command — an area inside the subtree being retired is
// about to stop existing, so moving work into it would leave that work carrying
// a path the vocabulary no longer declares, which is the state the whole refusal
// exists to prevent.
func resolveAreaMoveTarget(cfg *config.Config, path string) (string, error) {
	target := areaRmMoveTo
	if err := requireDeclaredArea(cfg, areaRmJSON, target, "move work to"); err != nil {
		return "", err
	}
	if cfg.IsAreaWithin(target, path) {
		return "", cmdError(areaRmJSON, output.ErrValidation,
			"cannot move members to %s: it is declared at or below %s, which this command is retiring — name an area outside it, or drop their assignment with `nibs area rm %s --unassign`",
			quotedArea(target), quotedArea(path), config.RenderAreaPath(path))
	}
	return target, nil
}

// areaEmptyDispositionError refuses a disposition that has nothing to act on.
// Letting it succeed would report members disposed of when there were none, and
// a silent no-op is the one answer a caller cannot tell apart from a real one.
//
// The caller reaches it two ways: by naming a disposition for an area nothing is
// assigned to, and by rerunning after a disposition completed and the config
// write did not. Dropping the flag retires the area from either state, which is
// why that is what the message prescribes.
func areaEmptyDispositionError(path string, disposition areaDisposition) error {
	return cmdError(areaRmJSON, output.ErrValidation,
		"nothing to %s: no nib is assigned at or below area %s — drop %s and run `nibs area rm %s` to retire it",
		areaDispositionAction(disposition), quotedArea(path), areaDispositionFlag(disposition), config.RenderAreaPath(path))
}

// beginAreaEdit opens an areas edit's critical section: it takes the store's
// cross-process write lock for the WHOLE verb — so the member cascade and the
// `areas:` rewrite that follows it are one critical section — and then re-reads
// the store from disk under it.
//
// Both halves of the edit are read-modify-writes of shared state, and the config
// half is a rewrite of the entire file. Taken separately — a lock per cascade and
// none at all for the config — two concurrent area edits interleave: each reads
// the pre-edit config, each writes the whole file back, and the loser's
// declaration is gone while its cascade is on disk. Both processes exit 0, so
// nothing ever says to rerun, and the members it moved are write-refused from
// then on.
//
// It blocks rather than refusing, matching every other store mutation: the other
// holder is another nibs process finishing one operation.
//
// THE RELOAD IS WHY THE BLOCKING IS SAFE. Waiting here means another process was
// mid-write, and this one has been holding the snapshot it loaded at startup the
// whole time. A `nibs config set-prefix` renames every file in the store, so the
// path each member carried is then a name nothing is at, and a cascade over
// those paths writes each member back under its pre-rename name — leaving the
// store with more files than it had, under a prefix the config no longer
// declares. Reading the store again HERE, rather than refusing over a store that
// moved, is what makes a concurrent rename an ordinary event: the cascade
// proceeds against the paths the store holds now.
//
// It is the NIBS that are re-read, and only those — Core.Load never opens
// config.yml — so the areas vocabulary the rest of the verb decides from is
// still the one this process loaded at startup.
func beginAreaEdit(app *App, jsonMode bool) (*nibcore.StoreLock, error) {
	lock, err := nibcore.AcquireStoreLock(app.Core.Root())
	if err != nil {
		return nil, cmdError(jsonMode, output.ErrFileError,
			"this store's write lock could not be taken, and an areas edit rewrites both the nibs and the vocabulary so it must hold one: %v", err)
	}
	if err := app.Core.Load(); err != nil {
		_ = lock.Release()
		return nil, cmdError(jsonMode, output.ErrFileError,
			"nothing was written: re-reading the store under its write lock failed: %v", err)
	}
	return lock, nil
}

// planAreaEdit resolves the config edit before its caller writes a single nib.
//
// The order is the fix for a partial failure a rerun could never repair. The
// members are rewritten first so that a rerun finds fewer of them and finishes
// the job — but that only works if the config edit's REMAINING failure modes are
// transient. Two of them are not: a vocabulary declared through a YAML alias or
// merge key resolves for the loaded model and is invisible to the file editor,
// and a config holding a second YAML document cannot be rewritten from the first
// one alone. Discovered after the cascade, either leaves the members carrying an
// undeclared path — write-refused, with a printed remedy that fails identically
// forever. Discovered here, they are a refusal over an untouched store.
//
// The split in exit codes follows the same line: config.AreaEditRefusal is about
// the file's CONTENT, which is the caller's to fix, and anything else is the
// filesystem's.
func planAreaEdit(jsonMode bool, plan func() (*config.StoredAreaEdit, error)) (*config.StoredAreaEdit, error) {
	edit, err := plan()
	if err == nil {
		return edit, nil
	}
	var refusal *config.AreaEditRefusal
	if errors.As(err, &refusal) {
		return nil, cmdError(jsonMode, output.ErrValidation, "%v", err)
	}
	return nil, cmdError(jsonMode, output.ErrFileError, "%v", err)
}

// --- shared ----------------------------------------------------------------

// requireDeclaredArea refuses a path the store's vocabulary does not declare.
//
// The two directions are separate messages for the reason config.AreaError
// separates them: "must be one of" followed by nothing reads as a bug in nibs,
// where the real answer is that this project has never declared a vocabulary —
// which is a config edit and not a different argument. Neither branch prescribes
// a command: the declared set IS the repair for the first, and the second names
// a file to edit.
func requireDeclaredArea(cfg *config.Config, jsonMode bool, path, verb string) error {
	if path != "" && cfg.IsValidArea(path) {
		return nil
	}
	if !cfg.AreasDeclared() {
		return cmdError(jsonMode, output.ErrValidation,
			"this store declares no areas, so there is none to %s — declare an `areas:` block in %s first",
			verb, sanitizeFilePath(cfg.Layout().ConfigPath()))
	}
	return cmdError(jsonMode, output.ErrValidation,
		"this store declares no area %s: the declared areas are %s",
		quotedArea(path), cfg.AreaList())
}

// areaMembers returns the ids of every nib assigned at or below path, in id
// order — the set a retire refuses over and a rename cascades through, read the
// same way Core.RewriteAreaAssignments reads it.
//
// The stored pointers All() hands back are read into plain strings immediately,
// per internal/graph's live-pointer discipline: nothing here holds one across
// the writes below.
func areaMembers(app *App, cfg *config.Config, path string) []string {
	var ids []string
	for _, b := range app.Core.All() {
		if cfg.IsAreaWithin(b.Area, path) {
			ids = append(ids, b.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// splitAreaPath separates a path into the path of its parent (empty at the top
// level) and its own name.
func splitAreaPath(path string) (parent, name string) {
	i := strings.LastIndex(path, config.AreaPathSeparator)
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+len(config.AreaPathSeparator):]
}

// countDeclaredBelow counts the areas declared beneath path, which a retire
// takes with it.
func countDeclaredBelow(cfg *config.Config, path string) int {
	n := 0
	for _, declared := range cfg.AreaPaths() {
		if declared != path && cfg.IsAreaWithin(declared, path) {
			n++
		}
	}
	return n
}

// quotedArea renders an area path as a quoted value inside a message. The path
// goes through config.RenderAreaPath because it is file-sourced whenever it is
// the declared set and caller-supplied text of unbounded length otherwise.
func quotedArea(path string) string {
	return fmt.Sprintf("%q", config.RenderAreaPath(path))
}

// areaNibCount renders the tally these messages carry ("1 nib" / "3 nibs").
// cmd/close_queue.go's countNibsOf is its neighbour and takes an adjective
// because a milestone's queue counts two different sets; an area counts one, so
// there is nothing to qualify.
func areaNibCount(n int) string {
	if n == 1 {
		return "1 nib"
	}
	return fmt.Sprintf("%d nibs", n)
}

// areaNibsAre is the same tally as the subject of a refusal sentence, so the
// message never carries a parenthesized plural.
func areaNibsAre(n int) string {
	if n == 1 {
		return "1 nib is"
	}
	return fmt.Sprintf("%d nibs are", n)
}

// areaDeclaredCount renders the tally of DECLARATIONS a retire took with the
// node it named, as distinct from the nibs assigned to them.
func areaDeclaredCount(n int) string {
	if n == 1 {
		return "1 area"
	}
	return fmt.Sprintf("%d areas", n)
}

// areaDisposition is what `nibs area rm` was told to do with the nibs assigned
// at or below the area it is retiring.
//
// None is a legal answer rather than a missing one — an area nothing is assigned
// to needs no disposition — and it is a THIRD case, not the absence of --move-to.
// Deriving the wording from a `move bool` made the no-flag branch describe an
// unassignment that never ran and prescribe dropping a flag never passed.
type areaDisposition int

const (
	areaDispositionNone areaDisposition = iota
	areaDispositionMove
	areaDispositionUnassign
)

// areaDispositionFlag is the flag that asked for it, for a message telling the
// caller to drop it. It is only ever reached for a disposition that was named.
func areaDispositionFlag(d areaDisposition) string {
	if d == areaDispositionMove {
		return "--move-to"
	}
	return "--unassign"
}

// areaDispositionVerb is the past tense a completed disposition reports in;
// areaDispositionAction is the bare verb a refusal says there is nothing to do.
func areaDispositionVerb(d areaDisposition) string {
	if d == areaDispositionMove {
		return "reassigned"
	}
	return "unassigned"
}

func areaDispositionAction(d areaDisposition) string {
	if d == areaDispositionMove {
		return "reassign"
	}
	return "unassign"
}

// reportAreaEdit prints what an area edit did, adding the stale-symlink note
// config.Save and SetStoredPrefix both owe: the atomic write replaced a link, so
// whatever manages the target still holds the old vocabulary and will restore it.
//
// It also names a live `nibs serve`, because for that one reader the edit is not
// finished when this returns — see areaLiveServeNote.
func reportAreaEdit(app *App, jsonMode bool, msg, staleLink string, cfg *config.Config) error {
	if staleLink != "" {
		msg += fmt.Sprintf("\nNote: %s was a symlink to %s and is now a regular file; %s still declares the old vocabulary, so update or remove it",
			sanitizeFilePath(cfg.Layout().ConfigPath()), sanitizeFilePath(staleLink), sanitizeFilePath(staleLink))
	}
	msg += areaLiveServeNote(app)
	if jsonMode {
		return output.SuccessMessage(msg)
	}
	ui.Println(msg)
	return nil
}

// areaLiveServeNote warns that a running `nibs serve` has not seen this edit,
// and returns "" when no other nibs process is holding the store.
//
// The verbs deliberately write the vocabulary to the file and never into the
// loaded config, which is what keeps Core.ValidateArea's off-lock read sound.
// For a CLI verb that prints and exits, being stale afterwards costs nothing.
// A serve is where it costs something: its watcher picks the rewritten NIB files
// up, so the UI shows the new paths, while its vocabulary is still the one it
// read at startup — and Core.Update validates every write against that, so every
// mutation of a cascaded nib comes back "invalid area" until the server restarts.
// Executed, not reasoned: a live serve refuses `updateNib` on a renamed nib with
// `invalid area "frontend": must be one of … web …`.
func areaLiveServeNote(app *App) string {
	return liveServeNote(app, "\nNote: another nibs process is holding this store. A running `nibs serve` reads the areas vocabulary once at startup, so restart it — until then every write it makes to a rewritten nib is refused against the old vocabulary.")
}
