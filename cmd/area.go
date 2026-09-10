package cmd

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	areaListJSON       bool
	areaAddJSON        bool
	areaAddDescription string
	areaAddColor       string
	areaRenameJSON     bool
	areaRmJSON         bool
	areaRmMoveTo       string
	areaRmUnassign     bool
)

var areaCmd = &cobra.Command{
	Use:   "area",
	Short: "Read and edit the project's declared areas vocabulary",
	Long: `Areas are the one vocabulary a project authors itself — statuses, types,
priorities and estimates are fixed. They are declared as a nested ` + "`areas:`" + ` block
in the store's areas.yml, and a nib is placed in one with ` + "`nibs new --area`" + ` or
` + "`nibs set --area`" + `.

These verbs read that vocabulary and edit it in place. The two that touch an area
already declared also rewrite the nibs assigned to it, because a nib's area is a
PATH: renaming a node moves every path below it, and retiring one leaves its
members pointing at a path that no longer exists. Declaring a new one rewrites
nothing — an area that does not exist yet has no members.`,
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

var areaAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Declare a new area, optionally nested under a declared one",
	Long: `Declares a new area in the store's areas.yml.

<path> is the FULL path of the new node, so ` + "`add web/dashboard`" + ` declares
` + "`dashboard`" + ` under the already-declared ` + "`web`" + `. A parent the store does not
declare is refused rather than created on the way, because the vocabulary is what
authorizes an ` + "`--area`" + ` value and one typo would otherwise mint two areas.

--description is what tells an agent which area new work belongs in, and is worth
giving. A store that declares no areas yet gets its vocabulary from the first
add, whether its areas.yml is missing or declares nothing.

Unlike rename and rm, this verb rewrites no nibs. A nib left carrying the path by
an earlier retire is repaired by the declaration itself, since the value it is
already holding becomes a declared one.`,
	Args: codedExactArgs(&areaAddJSON, 1),
	RunE: runAreaAdd,
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
store's areas.yml.

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
	areaAddCmd.Flags().BoolVar(&areaAddJSON, "json", false, "Output as JSON")
	areaAddCmd.Flags().StringVar(&areaAddDescription, "description", "",
		"What belongs in this area, for the agent choosing one")
	areaAddCmd.Flags().StringVar(&areaAddColor, "color", "",
		"Color the surfaces that display areas render this one in")
	areaRenameCmd.Flags().BoolVar(&areaRenameJSON, "json", false, "Output as JSON")
	areaRmCmd.Flags().BoolVar(&areaRmJSON, "json", false, "Output as JSON")
	areaRmCmd.Flags().StringVar(&areaRmMoveTo, "move-to", "",
		"Reassign every nib at or below the retiring area to this declared area")
	areaRmCmd.Flags().BoolVar(&areaRmUnassign, "unassign", false,
		"Drop the area assignment of every nib at or below the retiring area")
	areaRmCmd.MarkFlagsMutuallyExclusive("move-to", "unassign")

	areaCmd.AddCommand(areaListCmd, areaAddCmd, areaRenameCmd, areaRmCmd)
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
	app := getApp(cmd)
	areas := app.Areas()

	if areaListJSON {
		return output.JSONRaw(struct {
			Areas []areaListNode `json:"areas"`
		}{Areas: areaListNodes(areas.Roots(), "")})
	}

	if !areas.Declared() {
		ui.Printf("This store declares no areas. Declare an `areas:` block in %s to place work by area.\n",
			sanitizeFilePath(store.NewLayout(app.Core.Root()).AreasPath()))
		return nil
	}

	rows := areaListRows(areas.Roots(), "", 0, nil)
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

// --- add -------------------------------------------------------------------

func runAreaAdd(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	path := args[0]

	// Answered BEFORE the call, and printed straight away. The store's write
	// lock is a blocking flock with no timeout and prints nothing while it
	// waits, so a question the arguments alone answer would otherwise make a
	// typo sit silent for as long as any other cooperating writer holds the
	// store. Unlike the rename's, none of these refusals speaks about a node the
	// store declares — they judge the shape of the path and the color — so there
	// is no vocabulary that could make one of them the wrong thing to say.
	if err := validateAreaAddArgument(areaAddJSON, path, areaAddColor); err != nil {
		return err
	}

	// The verb is nibcore's, whole: it takes both of the store's locks in the
	// one order they may be taken in and holds them across the re-read, the
	// plan and the write. The lock is owed even though nothing cascades —
	// areas.yml is rewritten whole, so two concurrent adds without it lose one
	// of the two declarations — and every decision it makes comes from the
	// vocabulary re-read under it, never from app.Config().
	res, err := app.Core.AddArea(path, areaAddDescription, areaAddColor)
	if err != nil {
		return areaAddRefusal(path, err)
	}
	return reportAreaEdit(areaAddJSON, fmt.Sprintf("Declared area %s", quotedArea(path)), res)
}

// areaAddRefusal words what `nibs area add` refuses. What it does not word falls
// through to the refusals every area verb shares.
func areaAddRefusal(path string, err error) error {
	var declared *nibcore.AreaAlreadyDeclaredError
	if errors.As(err, &declared) {
		return cmdError(areaAddJSON, output.ErrValidation,
			"cannot declare area %s: this store already declares it, and two siblings with one name make one path mean two nodes",
			quotedArea(declared.Path))
	}
	// areaEditRefusal's undeclared-path wording is not reused: its no-vocabulary
	// branch sends the reader to edit areas.yml by hand, where this verb IS the
	// remedy, and its other branch speaks about a node to act ON rather than one
	// to nest under. Both directions here have the same answer, so they are one
	// message.
	var parent *nibcore.AreaParentUndeclaredError
	if errors.As(err, &parent) {
		return cmdError(areaAddJSON, output.ErrValidation,
			"cannot declare area %s: this store declares no area %s to nest it under, and a parent is never created on the way — declare it with `nibs area add %s` first, then rerun",
			quotedArea(parent.Path), quotedArea(parent.Parent), config.RenderAreaPath(parent.Parent))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) && ioErr.Phase == nibcore.AreaEditPhaseWrite {
		return cmdError(areaAddJSON, output.ErrFileError,
			"area %s could not be declared: %s could not be updated: %v — nothing else was written, so the store is as it was; rerun `nibs area add %s` once that is fixed",
			quotedArea(path), sanitizeFilePath(ioErr.File), ioErr.Cause, config.RenderAreaPath(path))
	}
	return areaEditRefusal(areaAddJSON, err, "declare")
}

// validateAreaAddArgument refuses every add the arguments alone rule out: a path
// no declared node could answer to, and a color the vocabulary could not hold.
//
// Both rules live in config and are called rather than copied, so there is one
// definition of each to keep in step. Neither call is what keeps a broken
// vocabulary off disk — config.PlanCreateStoredArea asks ValidateNewAreaPath
// itself, and the reread of the edited document rejects the color — so what this
// buys is WHEN the refusal is printed: see runAreaAdd.
func validateAreaAddArgument(jsonMode bool, path, color string) error {
	if err := config.ValidateNewAreaPath(path); err != nil {
		return cmdError(jsonMode, output.ErrValidation, "%v", err)
	}
	if err := config.ValidateAreaColor(color); err != nil {
		return cmdError(jsonMode, output.ErrValidation,
			"cannot declare area %s: %v", quotedArea(path), err)
	}
	return nil
}

// --- rename ----------------------------------------------------------------

func runAreaRename(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	path, newName := args[0], args[1]
	parent, oldName := splitAreaPath(path)

	// Answered BEFORE the call, because the two arguments alone answer it and no
	// vocabulary can change that answer. The store's write lock is a blocking
	// flock with no timeout and prints nothing while it waits, so asking a pure
	// argument question afterwards makes a typo sit silent for as long as any
	// other cooperating writer holds the store — a whole `nibs migrate` run.
	//
	// PRINTING it early is the separate question, and the startup snapshot
	// settles it. Two of these refusals speak about the node at `path` — one
	// asserts it "is already named" the name given, the other prescribes a
	// runnable `nibs area rename` for it — so over a path the store does not
	// declare they answer about a node that is not there, and the prescription
	// is a command the tool then refuses. Held rather than printed, the refusal
	// waits behind the store's own answer and the caller reads the typo first.
	//
	// The snapshot decides only WHICH of two refusals is printed, never whether
	// the store is written, so a stale answer costs a sentence — the same terms
	// nibcore.AreaRetiredWhileWaitingError is carried on. A path it declares
	// takes the fast exit; one it does not is the case where the extra
	// information is worth the wait, since the caller has to fix the path before
	// the name matters.
	argErr := validateAreaRenameArgument(areaRenameJSON, path, parent, oldName, newName)
	if argErr != nil && areaDeclaredAtStartup(app, path) {
		return argErr
	}

	res, err := app.Core.RenameArea(path, newName)
	if err != nil {
		return areaRenameRefusal(argErr, err)
	}

	msg := fmt.Sprintf("Renamed area %s to %s", quotedArea(path), quotedArea(res.NewPath))
	if len(res.Written) > 0 {
		msg += fmt.Sprintf(" and rewrote %s: %s%s",
			areaNibCount(len(res.Written)), strings.Join(namedIDs(res.Written), ", "), moreThanNamed(len(res.Written)))
	}
	return reportAreaEdit(areaRenameJSON, msg, res)
}

// areaRenameRefusal words what `nibs area rename` refuses, and decides when the
// held argument refusal is the one to print.
func areaRenameRefusal(argErr, err error) error {
	// The store answered about the NAME rather than about the path, which means
	// the node at `path` is there and the held refusal now speaks about a node
	// that exists. It comes first for the reason it is held at all: it can name
	// the runnable spelling, or the fact that nothing would change, where the
	// vocabulary's own backstop can only report that the file would be unusable.
	if argErr != nil && areaRenameNameRefusal(err) {
		return argErr
	}
	var unchanged *nibcore.AreaNameUnchangedError
	if errors.As(err, &unchanged) {
		return areaNameUnchangedRefusal(areaRenameJSON, unchanged.Path, unchanged.Name)
	}
	var taken *nibcore.AreaNameTakenError
	if errors.As(err, &taken) {
		return cmdError(areaRenameJSON, output.ErrValidation,
			"cannot rename area %s to %s: this store already declares %s, and two siblings with one name make one path mean two nodes",
			quotedArea(taken.Path), quotedArea(taken.NewName), quotedArea(taken.Sibling))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return cmdError(areaRenameJSON, output.ErrFileError,
				"rewrote %d of the %s assigned at or below area %s, then %v — the vocabulary in %s still declares %s and those writes are persisted; rerun the same command to finish it, since a nib already rewritten is no longer a member and the rerun starts where this stopped",
				len(ioErr.Written), areaNibCount(len(ioErr.Members)),
				quotedArea(ioErr.Path), ioErr.Cause, sanitizeFilePath(ioErr.File), quotedArea(ioErr.Path))
		case nibcore.AreaEditPhaseWrite:
			return cmdError(areaRenameJSON, output.ErrFileError,
				"rewrote %s from area %s to %s, then %s could not be updated: %v — the vocabulary still declares %s and those writes are persisted; rerun the same command to finish it, since the rewritten nibs are no longer members and the rerun only renames the declaration",
				areaNibCount(len(ioErr.Written)), quotedArea(ioErr.Path), quotedArea(ioErr.NewPath),
				sanitizeFilePath(ioErr.File), ioErr.Cause, quotedArea(ioErr.Path))
		}
	}
	return areaEditRefusal(areaRenameJSON, err, "rename")
}

// areaRenameNameRefusal reports that the store refused the NEW NAME rather than
// the path — the refusals a rename makes only after establishing that the node
// it was told to rename is declared.
func areaRenameNameRefusal(err error) bool {
	var unchanged *nibcore.AreaNameUnchangedError
	var taken *nibcore.AreaNameTakenError
	var refusal *config.AreaEditRefusal
	return errors.As(err, &unchanged) || errors.As(err, &taken) || errors.As(err, &refusal)
}

// validateAreaRenameArgument refuses every new name the two arguments rule out
// on their own, in the order a caller can act on: the shape of the name first,
// then whether it changes anything.
//
// All five read `path` and `newName` and nothing else — no vocabulary, no store — so
// no vocabulary can change their answer and runAreaRename asks them before the
// store's write lock. The vocabulary question a rename also has — does a sibling
// already answer to the new name? — is nibcore's, under that lock.
//
// Two of the five nonetheless SPEAK about the node at `path`, so the refusal
// they return is only correct over a path the store declares; when to print it
// is runAreaRename's call and not this function's.
//
// The LENGTH clause is config.ValidateAreaName's and is CALLED rather than
// copied, which is the whole reason that function exists — the wire surface
// calls it too. Its empty and padded clauses are reached only when this
// function's own two are removed: those come first because they can name the
// node being renamed, where a bound shared with a create cannot.
//
// config.PlanRenameStoredArea re-checks the RESULT before it hands back an edit
// to write, so none of this is what keeps a broken vocabulary off disk. What it
// buys is the message: that backstop can only say the edit would leave the file
// unusable, where these can name the runnable spelling, or the fact that nothing
// would change.
func validateAreaRenameArgument(jsonMode bool, path, parent, oldName, newName string) error {
	if newName == "" {
		return cmdError(jsonMode, output.ErrValidation,
			"area %s needs a name to be renamed to, and the new name is empty", quotedArea(path))
	}
	if strings.TrimSpace(newName) != newName {
		return cmdError(jsonMode, output.ErrValidation,
			"the new name %s has leading or trailing whitespace; an `area:` value would have to carry the same spaces to match it",
			quotedArea(newName))
	}
	if err := config.ValidateAreaName(newName); err != nil {
		return cmdError(jsonMode, output.ErrValidation, "%s", err)
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
		return areaNameUnchangedRefusal(jsonMode, path, newName)
	}
	return nil
}

// areaNameUnchangedRefusal refuses renaming a node to the name it already has.
// It has two callers — the pre-lock argument check and the store's own refusal —
// because the two ask the same question at different moments and must not answer
// it differently.
func areaNameUnchangedRefusal(jsonMode bool, path, newName string) error {
	return cmdError(jsonMode, output.ErrValidation,
		"area %s is already named %s, so the rename would change nothing",
		quotedArea(path), quotedArea(newName))
}

// --- rm --------------------------------------------------------------------

func runAreaRm(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	path := args[0]

	// Cobra refuses the two together (MarkFlagsMutuallyExclusive above), so at
	// most one of these is set here. It reads flags alone, so it needs no
	// vocabulary and asks for no lock.
	disposition := nibcore.AreaDisposition{}
	switch {
	case cmd.Flags().Changed("move-to"):
		disposition = nibcore.MoveAreaMembersTo(areaRmMoveTo)
	case areaRmUnassign:
		disposition = nibcore.UnassignAreaMembers()
	}

	res, err := app.Core.RemoveArea(path, disposition)
	if err != nil {
		return areaRetireRefusal(path, err)
	}

	msg := fmt.Sprintf("Retired area %s", quotedArea(path))
	if res.DeclaredBelow > 0 {
		msg += fmt.Sprintf(" and the %s declared beneath it", areaDeclaredCount(res.DeclaredBelow))
	}
	if len(res.Written) > 0 {
		msg += fmt.Sprintf("; %s %s: %s%s", areaDispositionVerb(disposition), areaNibCount(len(res.Written)),
			strings.Join(namedIDs(res.Written), ", "), moreThanNamed(len(res.Written)))
	}
	return reportAreaEdit(areaRmJSON, msg, res)
}

// areaRetireRefusal words what `nibs area rm` refuses.
func areaRetireRefusal(path string, err error) error {
	var members *nibcore.AreaMembersPresentError
	if errors.As(err, &members) {
		return cmdError(areaRmJSON, output.ErrValidation,
			"cannot retire area %s: %s assigned at or below it (%s%s) — reassign them with `nibs area rm %s --move-to <area>`, drop their assignment with `nibs area rm %s --unassign`, or leave the declaration in place",
			quotedArea(members.Path), areaNibsAre(len(members.Members)),
			strings.Join(namedIDs(members.Members), ", "), moreThanNamed(len(members.Members)),
			config.RenderAreaPath(members.Path), config.RenderAreaPath(members.Path))
	}
	// Reachable two ways: by naming a disposition for an area nothing is
	// assigned to, and by rerunning after a disposition completed and the config
	// write did not. Dropping the flag retires the area from either state, which
	// is why that is what the message prescribes.
	var empty *nibcore.AreaDispositionEmptyError
	if errors.As(err, &empty) {
		return cmdError(areaRmJSON, output.ErrValidation,
			"nothing to %s: no nib is assigned at or below area %s — drop %s and run `nibs area rm %s` to retire it",
			areaDispositionAction(empty.Disposition), quotedArea(empty.Path),
			areaDispositionFlag(empty.Disposition), config.RenderAreaPath(empty.Path))
	}
	var within *nibcore.AreaMoveTargetWithinError
	if errors.As(err, &within) {
		return cmdError(areaRmJSON, output.ErrValidation,
			"cannot move members to %s: it is declared at or below %s, which this command is retiring — name an area outside it, or drop their assignment with `nibs area rm %s --unassign`",
			quotedArea(within.Target), quotedArea(within.Path), config.RenderAreaPath(within.Path))
	}
	// A --move-to target retired under the lock has its own remedy: the members
	// are not going anywhere, so the caller needs a target that still exists or
	// no target at all. The generic wording sends them to `nibs area list`, which
	// answers a different question.
	var retired *nibcore.AreaRetiredWhileWaitingError
	if errors.As(err, &retired) && retired.Role == nibcore.AreaPathMoveTarget {
		return cmdError(areaRmJSON, output.ErrFileError,
			"nothing was written: this store declared area %s when this command started and does not declare it now — another nibs process retired or renamed it while this one waited for the store's write lock, and moving members there would leave every one of them carrying a path the vocabulary no longer declares; name a target `nibs area list` shows, or drop their assignment with `nibs area rm %s --unassign`",
			quotedArea(retired.Path), config.RenderAreaPath(path))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return cmdError(areaRmJSON, output.ErrFileError,
				"%s %d of the %s assigned at or below area %s, then %v — %s is still declared and those writes are persisted; rerun the same command to finish it, since a nib already disposed of is no longer a member and the rerun starts where this stopped",
				areaDispositionVerb(ioErr.Disposition), len(ioErr.Written), areaNibCount(len(ioErr.Members)),
				quotedArea(ioErr.Path), ioErr.Cause, quotedArea(ioErr.Path))
		case nibcore.AreaEditPhaseWrite:
			return areaRetireWriteFailure(ioErr)
		}
	}
	return areaEditRefusal(areaRmJSON, err, "retire")
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
func areaRetireWriteFailure(e *nibcore.AreaEditIOError) error {
	if e.Disposition.Kind == nibcore.AreaDispositionNone {
		return cmdError(areaRmJSON, output.ErrFileError,
			"area %s could not be retired: %s could not be updated: %v — nothing is assigned at or below it, so nothing was rewritten and the store is as it was; rerun `nibs area rm %s` once that is fixed",
			quotedArea(e.Path), sanitizeFilePath(e.File), e.Cause, config.RenderAreaPath(e.Path))
	}
	return cmdError(areaRmJSON, output.ErrFileError,
		"%s %s from area %s, then %s could not be updated: %v — %s is still declared and those writes are persisted; rerun WITHOUT %s to retire it, which is what finishes the job now that nothing is assigned below it",
		areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)), quotedArea(e.Path),
		sanitizeFilePath(e.File), e.Cause, quotedArea(e.Path), areaDispositionFlag(e.Disposition))
}

// --- shared ----------------------------------------------------------------

// areaEditRefusal words the refusals every area verb shares, and is the last
// word on an error none of the per-verb formatters claimed.
//
// verb names what the caller asked for, for the one refusal that has to say what
// there is none of. The two directions of an undeclared path are separate
// messages for the reason config.AreaError separates them: "must be one of"
// followed by nothing reads as a bug in nibs, where the real answer is that this
// project has never declared a vocabulary — which is a config edit and not a
// different argument. Neither branch prescribes a command: the declared set IS
// the repair for the first, and the second names a file to edit.
//
// The exit classes follow one line: what the caller sent is theirs to fix
// (validation), and a store the filesystem moved out from under the command is
// not (file error). config.AreaEditRefusal is about the file's CONTENT and lands
// on the first side; anything left over is the filesystem's.
func areaEditRefusal(jsonMode bool, err error, verb string) error {
	var undeclared *nibcore.AreaUndeclaredError
	if errors.As(err, &undeclared) {
		if !undeclared.Areas.Declared() {
			return cmdError(jsonMode, output.ErrValidation,
				"this store declares no areas, so there is none to %s — declare an `areas:` block in %s first",
				areaPathVerb(undeclared.Role, verb), sanitizeFilePath(undeclared.Areas.Path()))
		}
		return cmdError(jsonMode, output.ErrValidation,
			"this store declares no area %s: the declared areas are %s",
			quotedArea(undeclared.Path), undeclared.Areas.List())
	}
	var retired *nibcore.AreaRetiredWhileWaitingError
	if errors.As(err, &retired) {
		// config.PlanRenameStoredArea and PlanRemoveStoredArea refuse this too —
		// the plan is resolved against the file — but as "declares no area",
		// which reads as a typo the caller did not make, and as a VALIDATION
		// error, which says the argument was bad. Neither is true: the argument
		// was declared when it was typed, so this is classified with every other
		// refusal over a store the filesystem moved out from under a command.
		//
		// It prescribes the listing rather than a rerun. A rerun repairs a
		// partial write; nothing here was written, and the node the caller named
		// is not coming back — what they need is the vocabulary as it now stands.
		return cmdError(jsonMode, output.ErrFileError,
			"nothing was written: this store declared area %s when this command started and does not declare it now — another nibs process retired or renamed it while this one waited for the store's write lock; `nibs area list` prints the vocabulary as it now stands",
			quotedArea(retired.Path))
	}
	var vanished *nibcore.AreaVocabularyVanishedError
	if errors.As(err, &vanished) {
		return cmdError(jsonMode, output.ErrFileError,
			"nothing was written: this store's areas vocabulary could not be read under its write lock — %s does not exist; restore it before editing the areas it declares",
			sanitizeFilePath(vanished.File))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseLock:
			return cmdError(jsonMode, output.ErrFileError,
				"this store's write lock could not be taken, and an areas edit rewrites both the nibs and the vocabulary so it must hold one: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadVocabulary:
			return cmdError(jsonMode, output.ErrFileError,
				"nothing was written: re-reading this store's areas vocabulary under its write lock failed: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadNibs:
			return cmdError(jsonMode, output.ErrFileError,
				"nothing was written: re-reading this store's nibs under its write lock failed: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseReload:
			return cmdError(jsonMode, output.ErrFileError,
				"both halves of this edit landed on disk, and re-reading the vocabulary it just wrote then failed: %v — there is nothing to rerun; until that file can be read again this store answers from the vocabulary as it was before the edit",
				ioErr.Cause)
		}
	}
	var refusal *config.AreaEditRefusal
	if errors.As(err, &refusal) {
		// The refusal renders path-free by default, because the same sentence
		// reaches an HTTP client through the area mutations. This reader owns the
		// directory the store sits in, so it is named here.
		return cmdError(jsonMode, output.ErrValidation, "%s", refusal.Naming(sanitizeFilePath(refusal.File)))
	}
	return cmdError(jsonMode, output.ErrFileError, "%v", err)
}

// areaPathVerb names what the caller asked to do with the path a refusal is
// about. A --move-to target is the one role whose verb is not the command's own.
func areaPathVerb(role nibcore.AreaPathRole, verb string) string {
	if role == nibcore.AreaPathMoveTarget {
		return "move work to"
	}
	return verb
}

// areaDeclaredAtStartup reports that path was in the vocabulary this process
// loaded. It is the startup half of the one question a verb asks BEFORE calling
// the store — whether a held argument refusal may be printed straight away — and
// never on its own grounds for a write: a snapshot cannot say what the store
// declares NOW. The store makes the same comparison under its own lock, from the
// vocabulary it held before the edit re-read it, which for a one-shot command is
// this same snapshot.
//
// A Core holding no vocabulary answers false, leaving the ordinary refusal to
// speak: with nothing loaded there is no earlier state to have diverged from.
func areaDeclaredAtStartup(app *App, path string) bool {
	return path != "" && app.StartupAreas().IsValid(path)
}

// reportAreaEdit prints what an area edit did, adding the stale-symlink note
// Areas.Save and SetStoredPrefix both owe: the atomic write replaced a link, so
// whatever manages the target still holds the old vocabulary and will restore it.
//
// It names no live `nibs serve`, unlike `nibs config set-prefix` beside it. A
// server watches the store's areas.yml and reloads it, so this edit reaches one
// on its own — the restart that used to be owed here is not owed any more.
func reportAreaEdit(jsonMode bool, msg string, res nibcore.AreaEditResult) error {
	if res.StaleLinkTarget != "" {
		file := sanitizeFilePath(res.Areas.Path())
		target := sanitizeFilePath(res.StaleLinkTarget)
		msg += fmt.Sprintf("\nNote: %s was a symlink to %s and is now a regular file; %s still declares the old vocabulary, so update or remove it",
			file, target, target)
	}
	if jsonMode {
		return output.SuccessMessage(msg)
	}
	ui.Println(msg)
	return nil
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

// areaDispositionFlag is the flag that asked for a disposition, for a message
// telling the caller to drop it. It is only ever reached for one that was named.
func areaDispositionFlag(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "--move-to"
	}
	return "--unassign"
}

// areaDispositionVerb is the past tense a completed disposition reports in;
// areaDispositionAction is the bare verb a refusal says there is nothing to do.
func areaDispositionVerb(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "reassigned"
	}
	return "unassigned"
}

func areaDispositionAction(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "reassign"
	}
	return "unassign"
}
