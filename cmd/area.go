package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	areaListJSON       bool
	areaAddJSON        bool
	areaAddDescription string
	areaAddColor       string
	areaSetJSON        bool
	areaSetName        string
	areaSetDescription string
	areaSetColor       string
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
	Args: codedUnknownSubcommand(nil),
	// The bare `nibs area` prints this group's help and exits 0. Cobra would do
	// that on its own, but only for a command it considers non-runnable — and a
	// non-runnable command never reaches its Args validator, so an unrecognized
	// verb would keep exiting 0. Printing the help here is what lets the
	// validator run.
	RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
}

// areaVerbs lists the `nibs area` subcommands, each with its operand, in the
// order `nibs catalog areas` and `nibs cheat` present them.
var areaVerbs = []struct{ name, operand string }{
	{"list", ""},
	{"add", "<path>"},
	{"set", "<path>"},
	{"rm", "<path>"},
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

Unlike ` + "`set --name`" + ` and rm, this verb rewrites no nibs. A nib left carrying the path by
an earlier retire is repaired by the declaration itself, since the value it is
already holding becomes a declared one.`,
	Args: codedExactArgs(&areaAddJSON, 1),
	RunE: runAreaAdd,
}

var areaSetCmd = &cobra.Command{
	Use:   "set <path>",
	Short: "Change a declared area's name, description or color",
	Long: `Edits the node at <path>: its name, its description, its color, or any
combination of the three in one write. At least one of --name, --description and
--color is required; setting none is refused rather than reported as a success
over an edit that never happened.

--name is a NAME, not a path: it changes what a node is called and never moves it
between parents. A name a sibling already holds is refused rather than merged,
because two siblings with one name would make one path mean two nodes.

RENAMING is the only part that touches nibs. It rewrites the ` + "`area:`" + ` of every nib
assigned to the node or to any declared area beneath it, since renaming a parent
moves the whole subtree's paths. The nibs are rewritten before the declaration
is, so rerunning the same command finishes a run that failed part way. Setting a
description or a color rewrites nothing — nothing stops being declared.

AN EMPTY VALUE CLEARS. ` + "`--description \"\"`" + ` removes the description; leaving the
flag off keeps whatever the store declares. The same holds for --color.`,
	Args: codedExactArgs(&areaSetJSON, 1),
	RunE: runAreaSet,
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
finished and whose areas.yml write did not is finished by rerunning WITHOUT the
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
	areaSetCmd.Flags().BoolVar(&areaSetJSON, "json", false, "Output as JSON")
	areaSetCmd.Flags().StringVar(&areaSetName, "name", "",
		"New name for the area — its own segment, never a path")
	areaSetCmd.Flags().StringVar(&areaSetDescription, "description", "",
		"What belongs in this area, for the agent choosing one; empty clears it")
	areaSetCmd.Flags().StringVar(&areaSetColor, "color", "",
		"Color the surfaces that display areas render this one in; empty clears it")
	areaRmCmd.Flags().BoolVar(&areaRmJSON, "json", false, "Output as JSON")
	areaRmCmd.Flags().StringVar(&areaRmMoveTo, "move-to", "",
		"Reassign every nib at or below the retiring area to this declared area")
	areaRmCmd.Flags().BoolVar(&areaRmUnassign, "unassign", false,
		"Drop the area assignment of every nib at or below the retiring area")
	areaRmCmd.MarkFlagsMutuallyExclusive("move-to", "unassign")

	areaCmd.AddCommand(areaListCmd, areaAddCmd, areaSetCmd, areaRmCmd)
	rootCmd.AddCommand(areaCmd)
}

// --- list ------------------------------------------------------------------

// areaListNode is one node of the tree `area list --json` emits. Path is what
// --area takes; Name is the single segment the file declares, which is what a
// later `nibs area set` takes.
type areaListNode struct {
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Color       string         `json:"color,omitempty"`
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

	if areas.IsEmpty() {
		ui.Printf("This store declares no areas. Declare an `areas:` block in %s to place work by area.\n",
			sanitizeFilePath(app.AreasPath()))
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

func areaListRows(areas []area.Node, parent string, depth int, rows []areaListRow) []areaListRow {
	for _, node := range areas {
		path := area.JoinPath(parent, node.Name)
		rows = append(rows, areaListRow{
			label:       strings.Repeat("  ", depth) + area.RenderPath(path),
			description: sanitizeFileText(node.Description),
		})
		rows = areaListRows(node.Children, path, depth+1, rows)
	}
	return rows
}

// areaListNodes rebuilds each node's path from the tree it was walked in rather
// than reading Vocabulary.Paths, because each entry has to carry the node's own
// fields alongside its path, which a flat list of strings cannot give back.
func areaListNodes(areas []area.Node, parent string) []areaListNode {
	nodes := make([]areaListNode, 0, len(areas))
	for _, node := range areas {
		path := area.JoinPath(parent, node.Name)
		nodes = append(nodes, areaListNode{
			Path:        path,
			Name:        node.Name,
			Description: node.Description,
			Color:       node.Color,
			Children:    areaListNodes(node.Children, path),
		})
	}
	return nodes
}

// --- add -------------------------------------------------------------------

func runAreaAdd(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	path := args[0]

	// Answered BEFORE the call, and printed straight away. Waiting for the
	// store's write lock has no deadline and prints nothing while it waits, so a
	// question the arguments alone answer would otherwise make a typo sit silent
	// for as long as any other cooperating writer holds the store. Unlike the
	// rename's, none of these refusals speaks about a node the store declares —
	// they judge the shape of the path and the color — so there is no vocabulary
	// that could make one of them the wrong thing to say.
	if err := validateAreaAddArgument(areaAddJSON, path, areaAddColor); err != nil {
		return err
	}

	// The verb is nibcore's, whole: it takes both of the store's locks in the
	// one order they may be taken in and holds them across the re-read, the
	// plan and the write. The lock is owed even though nothing cascades —
	// areas.yml is rewritten whole, so two concurrent adds without it lose one
	// of the two declarations — and every decision it makes comes from the
	// vocabulary re-read under it, never from app.Config().
	res, err := app.Core.AddArea(cmd.Context(), path, areaAddDescription, areaAddColor)
	if err != nil {
		return areaAddRefusal(path, app.AreasPath(), err)
	}
	return reportAreaEdit(areaAddJSON, app.AreasPath(), fmt.Sprintf("Declared area %s", quotedArea(path)), res)
}

// areaAddRefusal words what `nibs area add` refuses. What it does not word falls
// through to the refusals every area verb shares.
func areaAddRefusal(path, areasFile string, err error) error {
	var declared *nibcore.AreaAlreadyDeclaredError
	if errors.As(err, &declared) {
		return cmdError(areaAddJSON, output.ErrValidation,
			"cannot create area %s, because it already exists. Choose a different name",
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
			"cannot create area %s, because area %s does not exist. Create it first with `nibs area add %s`, then try again",
			quotedArea(parent.Path), quotedArea(parent.Parent), area.RenderPath(parent.Parent))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) && ioErr.Phase == nibcore.AreaEditPhaseWrite {
		return cmdError(areaAddJSON, output.ErrFileError,
			"area %s was not created, because %s could not be written: %v. Nothing was changed. Fix the problem, then run `nibs area add %s` again",
			quotedArea(path), sanitizeFilePath(ioErr.File), ioErr.Cause, area.RenderPath(path))
	}
	return areaEditRefusal(areaAddJSON, areasFile, err, "create")
}

// validateAreaAddArgument refuses every add the arguments alone rule out: a path
// no declared node could answer to, and a color the vocabulary could not hold.
//
// Both rules live in internal/area and are called rather than copied, so there is one
// definition of each to keep in step. Neither call is what keeps a broken
// vocabulary off disk — area.PlanCreate asks ValidateNewPath
// itself, and the reread of the edited document rejects the color — so what this
// buys is WHEN the refusal is printed: see runAreaAdd.
func validateAreaAddArgument(jsonMode bool, path, color string) error {
	if err := area.ValidateNewPath(path); err != nil {
		return cmdError(jsonMode, output.ErrValidation, "%v", err)
	}
	if err := area.ValidateColor(color); err != nil {
		return cmdError(jsonMode, output.ErrValidation,
			"cannot create area %s: %v", quotedArea(path), err)
	}
	return nil
}

// --- set -------------------------------------------------------------------

func runAreaSet(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	path := args[0]
	parent, oldName := area.SplitPath(path)

	// A flag left off leaves that key as the store declares it; one given EMPTY
	// clears it. Only cobra's Changed can tell those apart, because the VALUE
	// cannot: "" is both "not given" and "clear this".
	var update area.NodeUpdate
	if cmd.Flags().Changed("name") {
		update.NewName = &areaSetName
	}
	if cmd.Flags().Changed("description") {
		update.Description = &areaSetDescription
	}
	if cmd.Flags().Changed("color") {
		update.Color = &areaSetColor
	}

	// The color is judged by its own value, so it is answered unconditionally and
	// before the lock, exactly as runAreaAdd answers its arguments. The NAME
	// refusals below cannot be: they speak about the node at `path`.
	if update.Color != nil {
		if err := area.ValidateColor(areaSetColor); err != nil {
			return cmdError(areaSetJSON, output.ErrValidation,
				"cannot set the color of area %s: %v", quotedArea(path), err)
		}
	}

	// An update that sets nothing is NOT refused here: Core.UpdateArea refuses it
	// before taking the lock too, and duplicating that check would make its typed
	// refusal unreachable from this surface — dead wording no test could drive.

	// Answered BEFORE the call, because the two arguments alone answer it and no
	// vocabulary can change that answer. Waiting for the store's write lock has
	// no deadline and prints nothing while it waits, so asking a pure argument
	// question afterwards makes a typo sit silent for as long as any other
	// cooperating writer holds the store — a whole `nibs migrate` run.
	//
	// PRINTING it early is the separate question, and the startup snapshot
	// settles it. Two of these refusals speak about the node at `path` — one
	// asserts it "is already named" the name given, the other prescribes a
	// runnable `nibs area set` for it — so over a path the store does not
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
	var argErr error
	if update.NewName != nil {
		argErr = validateAreaSetArgument(areaSetJSON, path, parent, oldName, areaSetName)
		if argErr != nil && areaDeclaredAtStartup(app, path) {
			return argErr
		}
	}

	res, err := app.Core.UpdateArea(cmd.Context(), path, update)
	if err != nil {
		return areaSetRefusal(app.AreasPath(), argErr, err)
	}
	return reportAreaEdit(areaSetJSON, app.AreasPath(), areaSetSummary(path, update, res), res)
}

// areaSetSummary says which of the three fields the edit actually changed. One
// verb now covers all three, so "Updated area web" alone would leave the caller
// unable to tell a rename from a cleared color.
func areaSetSummary(path string, u area.NodeUpdate, res nibcore.AreaEditResult) string {
	var msg string
	if u.NewName != nil {
		msg = fmt.Sprintf("Renamed area %s to %s", quotedArea(path), quotedArea(res.NewPath))
		if len(res.Written) > 0 {
			msg += fmt.Sprintf(" and rewrote %s: %s%s",
				areaNibCount(len(res.Written)), strings.Join(namedIDs(res.Written), ", "),
				moreThanNamed(len(res.Written)))
		}
	} else {
		msg = fmt.Sprintf("Updated area %s", quotedArea(path))
	}

	var also []string
	if u.Description != nil {
		also = append(also, areaSetFieldWord("description", *u.Description))
	}
	if u.Color != nil {
		also = append(also, areaSetFieldWord("color", *u.Color))
	}
	if len(also) > 0 {
		msg += " (" + strings.Join(also, ", ") + ")"
	}
	return msg
}

// areaSetFieldWord tells a field that was SET from one that was CLEARED, which
// is the whole distinction an empty value carries.
func areaSetFieldWord(field, value string) string {
	if value == "" {
		return "cleared the " + field
	}
	return "set the " + field
}

// areaSetRefusal words what `nibs area set` refuses, and decides when the
// held argument refusal is the one to print.
func areaSetRefusal(areasFile string, argErr, err error) error {
	// The store answered about the NAME rather than about the path, which means
	// the node at `path` is there and the held refusal now speaks about a node
	// that exists. It comes first for the reason it is held at all: it can name
	// the runnable spelling, or the fact that nothing would change, where the
	// vocabulary's own backstop can only report that the file would be unusable.
	if argErr != nil && areaSetNameRefusal(err) {
		return argErr
	}
	var nothing *nibcore.AreaUpdateEmptyError
	if errors.As(err, &nothing) {
		return cmdError(areaSetJSON, output.ErrValidation,
			"nothing to change on area %s: give --name, --description or --color",
			quotedArea(nothing.Path))
	}
	var unchanged *nibcore.AreaNameUnchangedError
	if errors.As(err, &unchanged) {
		return areaNameUnchangedRefusal(areaSetJSON, unchanged.Path, unchanged.Name)
	}
	var taken *nibcore.AreaNameTakenError
	if errors.As(err, &taken) {
		return cmdError(areaSetJSON, output.ErrValidation,
			"cannot rename area %s to %s, because area %s already exists. Choose a different name",
			quotedArea(taken.Path), quotedArea(taken.NewName), quotedArea(taken.Sibling))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return cmdError(areaSetJSON, output.ErrFileError,
				"renaming area %s to %s stopped part way: it moved %d of the %s assigned to it or to an area beneath it, then failed: %v. %s still names the area %s, and the moved nibs are saved.%s Run the same command again to finish; it starts where this one stopped",
				quotedArea(ioErr.Path), quotedArea(ioErr.NewPath), len(ioErr.Written), areaNibCount(len(ioErr.Members)),
				ioErr.Cause, sanitizeFilePath(ioErr.File), quotedArea(ioErr.Path),
				areaCascadeStranded(ioErr.NewPath, len(ioErr.Written)))
		case nibcore.AreaEditPhaseWrite:
			return areaSetWriteFailure(ioErr)
		}
	}
	// "change", not "rename": this verb also sets a description or a color, and
	// the word lands in "there is none to %s" over a path the store does not
	// declare — where naming the rename would describe an edit the caller may not
	// have asked for.
	return areaEditRefusal(areaSetJSON, areasFile, err, "change")
}

// areaSetWriteFailure reports a `set` whose areas.yml write failed. Only a
// rename can have moved nibs before that write; a `set` of the description or
// color moved none, so nothing was changed.
func areaSetWriteFailure(e *nibcore.AreaEditIOError) error {
	if e.NewPath == "" || len(e.Written) == 0 {
		return cmdError(areaSetJSON, output.ErrFileError,
			"area %s was not %s, because %s could not be written: %v. Nothing was changed. Fix the problem, then run the same command again",
			quotedArea(e.Path), areaSetVerb(e.NewPath), sanitizeFilePath(e.File), e.Cause)
	}
	return cmdError(areaSetJSON, output.ErrFileError,
		"renaming area %s to %s stopped part way: it moved %s, then %s could not be written: %v. The file still names the area %s, and the moved nibs are saved.%s Fix the problem, then run the same command again to finish",
		quotedArea(e.Path), quotedArea(e.NewPath), areaNibCount(len(e.Written)), sanitizeFilePath(e.File), e.Cause,
		quotedArea(e.Path), areaCascadeStranded(e.NewPath, len(e.Written)))
}

// areaSetVerb is the past participle for a `set` that changed nothing.
func areaSetVerb(newPath string) string {
	if newPath == "" {
		return "updated"
	}
	return "renamed"
}

// areaSetNameRefusal reports that the store refused the NEW NAME rather than
// the path — the refusals a rename makes only after establishing that the node
// it was told to rename is declared.
func areaSetNameRefusal(err error) bool {
	var unchanged *nibcore.AreaNameUnchangedError
	var taken *nibcore.AreaNameTakenError
	var refusal *area.EditRefusal
	return errors.As(err, &unchanged) || errors.As(err, &taken) || errors.As(err, &refusal)
}

// validateAreaSetArgument refuses every new name the two arguments rule out
// on their own, in the order a caller can act on: the shape of the name first,
// then whether it changes anything.
//
// All five read `path` and `newName` and nothing else — no vocabulary, no store — so
// no vocabulary can change their answer and runAreaSet asks them before the
// store's write lock. The vocabulary question a rename also has — does a sibling
// already answer to the new name? — is nibcore's, under that lock.
//
// Two of the five nonetheless SPEAK about the node at `path`, so the refusal
// they return is only correct over a path the store declares; when to print it
// is runAreaSet's call and not this function's.
//
// The LENGTH clause is area.ValidateName's and is CALLED rather than
// copied, which is the whole reason that function exists — the wire surface
// calls it too. It is asked LAST of the shape clauses, because it is the only
// one that cannot name a remedy: a path given where a name belongs is refused
// above with the `nibs area set` that would work, and a bound shared with a
// create can only report a count. Asked earlier, a long path-as-name reported
// the length of the whole path and the runnable spelling was never printed.
// Its own empty and padded clauses are unreachable from here — this function's
// two answer first, in wording that names the node being renamed.
//
// area.PlanUpdate re-checks the RESULT before it hands back an edit
// to write, so none of this is what keeps a broken vocabulary off disk. What it
// buys is the message: that backstop can only say the edit would leave the file
// unusable, where these can name the runnable spelling, or the fact that nothing
// would change.
func validateAreaSetArgument(jsonMode bool, path, parent, oldName, newName string) error {
	if newName == "" {
		return cmdError(jsonMode, output.ErrValidation,
			"cannot rename area %s: --name is empty. Give the new name",
			quotedArea(path))
	}
	if strings.TrimSpace(newName) != newName {
		return cmdError(jsonMode, output.ErrValidation,
			"the new name %s starts or ends with whitespace. Remove it from the name",
			quotedArea(newName))
	}
	if strings.Contains(newName, area.PathSeparator) {
		newParent, tail := area.SplitPath(newName)
		if newParent == parent && tail != "" {
			return cmdError(jsonMode, output.ErrValidation,
				"%s contains \"/\", which separates the areas in a path. A rename keeps an area under its parent, so give only the new name: `nibs area set %s --name %s`",
				quotedArea(newName), area.RenderPath(path), area.RenderPath(tail))
		}
		return cmdError(jsonMode, output.ErrValidation,
			"%s contains \"/\", which separates the areas in a path. A rename keeps an area under its parent and cannot move it to another, so give only the new name. `nibs area list` prints the areas",
			quotedArea(newName))
	}
	if err := area.ValidateName(newName); err != nil {
		return cmdError(jsonMode, output.ErrValidation, "%s", err)
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

	res, err := app.Core.RemoveArea(cmd.Context(), path, disposition)
	if err != nil {
		return areaRetireRefusal(path, app.AreasPath(), err)
	}

	msg := fmt.Sprintf("Retired area %s", quotedArea(path))
	if res.DeclaredBelow > 0 {
		msg += fmt.Sprintf(" and the %s declared beneath it", areaDeclaredCount(res.DeclaredBelow))
	}
	if len(res.Written) > 0 {
		msg += fmt.Sprintf("; %s %s: %s%s", areaDispositionVerb(disposition), areaNibCount(len(res.Written)),
			strings.Join(namedIDs(res.Written), ", "), moreThanNamed(len(res.Written)))
	}
	return reportAreaEdit(areaRmJSON, app.AreasPath(), msg, res)
}

// areaRetireRefusal words what `nibs area rm` refuses.
func areaRetireRefusal(path, areasFile string, err error) error {
	var members *nibcore.AreaMembersPresentError
	if errors.As(err, &members) {
		return cmdError(areaRmJSON, output.ErrValidation,
			"cannot retire area %s: %s assigned to it or to an area beneath it (%s%s). Move them with `nibs area rm %s --move-to <area>`, clear their area with `nibs area rm %s --unassign`, or keep the area",
			quotedArea(members.Path), areaNibsAre(len(members.Members)),
			strings.Join(namedIDs(members.Members), ", "), moreThanNamed(len(members.Members)),
			area.RenderPath(members.Path), area.RenderPath(members.Path))
	}
	// Reachable two ways: by naming a disposition for an area nothing is
	// assigned to, and by rerunning after a disposition completed and the config
	// write did not. Dropping the flag retires the area from either state, which
	// is why that is what the message prescribes.
	var empty *nibcore.AreaDispositionEmptyError
	if errors.As(err, &empty) {
		return cmdError(areaRmJSON, output.ErrValidation,
			"no nib is assigned to area %s or to an area beneath it, so there are none to %s. Retire it without %s: `nibs area rm %s`",
			quotedArea(empty.Path), areaDispositionAction(empty.Disposition),
			areaDispositionFlag(empty.Disposition), area.RenderPath(empty.Path))
	}
	var within *nibcore.AreaMoveTargetWithinError
	if errors.As(err, &within) {
		return cmdError(areaRmJSON, output.ErrValidation,
			"cannot move the nibs to %s, because retiring %s retires it too. Choose an area outside %s, or clear their area with `nibs area rm %s --unassign`",
			quotedArea(within.Target), quotedArea(within.Path), quotedArea(within.Path), area.RenderPath(within.Path))
	}
	// A --move-to target retired under the lock has its own remedy: the members
	// are not going anywhere, so the caller needs a target that still exists or
	// no target at all. The generic wording sends them to `nibs area list`, which
	// answers a different question.
	var retired *nibcore.AreaRetiredWhileWaitingError
	if errors.As(err, &retired) && retired.Role == nibcore.AreaPathMoveTarget {
		return cmdError(areaRmJSON, output.ErrFileError,
			"nothing was changed: area %s was retired or renamed by another edit, so the nibs cannot move there. Choose an area `nibs area list` shows, or clear their area with `nibs area rm %s --unassign`",
			quotedArea(retired.Path), area.RenderPath(path))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return cmdError(areaRmJSON, output.ErrFileError,
				"retiring area %s stopped part way: it %s %d of the %s assigned to it or to an area beneath it, then failed: %v. Area %s still exists, and those changes are saved. Run the same command again to finish; it starts where this one stopped",
				quotedArea(ioErr.Path), areaDispositionVerb(ioErr.Disposition), len(ioErr.Written),
				areaNibCount(len(ioErr.Members)), ioErr.Cause, quotedArea(ioErr.Path))
		case nibcore.AreaEditPhaseConfirm:
			// A retire that named a disposition is past its cascade here, which
			// its own sentence has to report. A retire that named none rewrote
			// nothing, and the shared arm words that case.
			if ioErr.Disposition.Kind != nibcore.AreaDispositionNone {
				return areaRetireConfirmFailure(ioErr)
			}
		case nibcore.AreaEditPhaseWrite:
			return areaRetireWriteFailure(ioErr)
		}
	}
	return areaEditRefusal(areaRmJSON, areasFile, err, "retire")
}

// areaRetireWriteFailure reports a retire whose members are disposed of and
// whose areas.yml write failed, and it branches on whether a disposition was
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
			"area %s was not retired, because %s could not be written: %v. No nib is assigned to it, so nothing was changed. Fix the problem, then run `nibs area rm %s` again",
			quotedArea(e.Path), sanitizeFilePath(e.File), e.Cause, area.RenderPath(e.Path))
	}
	return cmdError(areaRmJSON, output.ErrFileError,
		"retiring area %s stopped part way: it %s %s, then %s could not be written: %v. Area %s still exists, and those changes are saved. Fix the problem, then run it again WITHOUT %s to finish: `nibs area rm %s`",
		quotedArea(e.Path), areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)),
		sanitizeFilePath(e.File), e.Cause, quotedArea(e.Path), areaDispositionFlag(e.Disposition), area.RenderPath(e.Path))
}

// areaRetireConfirmFailure reports a retire whose disposition completed and
// whose confirming re-read then failed. Every member this edit saw is disposed
// of, so it prescribes the rerun areaRetireWriteFailure does — but promises no
// outcome, because the re-read that would have established the area is empty is
// the one that failed: a nib that arrived in that window refuses that rerun.
func areaRetireConfirmFailure(e *nibcore.AreaEditIOError) error {
	return cmdError(areaRmJSON, output.ErrFileError,
		"retiring area %s stopped part way: it %s %s, then the check that no nib is still assigned to it failed: %v. Area %s still exists, and those changes are saved. Fix the problem, then run it again WITHOUT %s: `nibs area rm %s`",
		quotedArea(e.Path), areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)),
		e.Cause, quotedArea(e.Path), areaDispositionFlag(e.Disposition), area.RenderPath(e.Path))
}

// --- shared ----------------------------------------------------------------

// areaEditRefusal words the refusals every area verb shares, and is the last
// word on an error none of the per-verb formatters claimed.
//
// verb names what the caller asked for, for the one refusal that has to say what
// there is none of. The two directions of an undeclared path are separate
// messages for the reason area.Error separates them: an empty list of areas
// reads as a bug in nibs, where the real answer is that this store has none yet.
// Neither branch prescribes a command: the listed areas are the repair for one,
// and creating an area is the repair for the other.
//
// The exit classes follow one line: what the caller sent is theirs to fix
// (validation), and a store the filesystem moved out from under the command is
// not (file error). area.EditRefusal is about the file's CONTENT and lands
// on the first side; anything left over is the filesystem's.
func areaEditRefusal(jsonMode bool, areasFile string, err error, verb string) error {
	var undeclared *nibcore.AreaUndeclaredError
	if errors.As(err, &undeclared) {
		if undeclared.Areas.IsEmpty() {
			return cmdError(jsonMode, output.ErrValidation,
				"this store has no areas in %s, so there is none to %s. Create an area first",
				sanitizeFilePath(areasFile), areaPathVerb(undeclared.Role, verb))
		}
		return cmdError(jsonMode, output.ErrValidation,
			"this store has no area %s%s. Its areas are %s",
			quotedArea(undeclared.Path), areaPathPurpose(undeclared.Role), undeclared.Areas.List())
	}
	var retired *nibcore.AreaRetiredWhileWaitingError
	if errors.As(err, &retired) {
		// area.PlanUpdate and PlanRemove refuse this too —
		// the plan is resolved against the file — but as "has no area",
		// which reads as a typo the caller did not make, and as a VALIDATION
		// error, which says the argument was bad. Neither is true: the argument
		// was declared when it was typed, so this is classified with every other
		// refusal over a store the filesystem moved out from under a command.
		//
		// It prescribes the listing rather than a rerun. A rerun repairs a
		// partial write; nothing here was written, and the node the caller named
		// is not coming back — what they need is the vocabulary as it now stands.
		return cmdError(jsonMode, output.ErrFileError,
			"nothing was changed: area %s was retired or renamed by another edit. `nibs area list` prints the areas as they are now",
			quotedArea(retired.Path))
	}
	var vanished *nibcore.AreaVocabularyVanishedError
	if errors.As(err, &vanished) {
		return cmdError(jsonMode, output.ErrFileError,
			"nothing was changed: %s was deleted. Restore it before you change its areas",
			sanitizeFilePath(vanished.File))
	}
	var arrived *nibcore.AreaMembersArrivedError
	if errors.As(err, &arrived) {
		return cmdError(jsonMode, output.ErrFileError,
			"area %s was left as it was: %s assigned to it or to an area beneath it (%s%s) appeared after this command read the store, usually from a `git pull` in the store.%s Run the same command again; it uses the store as it is now.%s",
			quotedArea(arrived.Path), areaNibCount(len(arrived.Members)),
			strings.Join(namedIDs(arrived.Members), ", "), moreThanNamed(len(arrived.Members)),
			areaCascadePersisted(len(arrived.Written)), areaCascadeStranded(arrived.NewPath, len(arrived.Written)))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseLock:
			var waitEnded *nibcore.StoreLockWaitEndedError
			if errors.As(ioErr.Cause, &waitEnded) {
				return cmdError(jsonMode, output.ErrFileError,
					"nothing was changed: this command was stopped after %s, while it waited for another edit to the store to finish. Run it again",
					waitEnded.Waited.Round(time.Millisecond))
			}
			return cmdError(jsonMode, output.ErrFileError,
				"nothing was changed: the store could not be locked for this edit: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadVocabulary:
			return cmdError(jsonMode, output.ErrFileError,
				"nothing was changed: the store's areas.yml could not be read: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadNibs:
			return cmdError(jsonMode, output.ErrFileError,
				"nothing was changed: the store's nibs could not be read: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseConfirm:
			// The cascade is durable and the vocabulary is not written, which is
			// the state the arrival refusal above describes — so it carries the
			// same stranded-member clause rather than the persisted clause alone.
			return cmdError(jsonMode, output.ErrFileError,
				"area %s was left as it was: the check that no nib is still assigned to it or to an area beneath it failed: %v.%s Fix the problem, then run the same command again.%s",
				quotedArea(ioErr.Path), ioErr.Cause, areaCascadePersisted(len(ioErr.Written)),
				areaCascadeStranded(ioErr.NewPath, len(ioErr.Written)))
		case nibcore.AreaEditPhaseReload:
			return cmdError(jsonMode, output.ErrFileError,
				"the edit was saved, but the store's areas.yml could not be read back afterwards: %v. Do not run the command again. Until the file can be read again, the areas show as they were before the edit",
				ioErr.Cause)
		}
	}
	var refusal *area.EditRefusal
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
		return "move nibs to"
	}
	return verb
}

// areaPathPurpose names what a --move-to target was for, and nothing for the
// path the verb acts on.
func areaPathPurpose(role nibcore.AreaPathRole) string {
	if role == nibcore.AreaPathMoveTarget {
		return " to move the nibs to"
	}
	return ""
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
	return path != "" && app.StartupAreas().Exists(path)
}

// reportAreaEdit prints what an area edit did, adding a note when
// the edit's write replaced a symlink at areas.yml: the link's target
// still holds the old vocabulary, and whatever manages it may restore that.
//
// It names no live `nibs serve`, unlike `nibs config set-prefix` beside it. A
// server watches the store's areas.yml and reloads it, so this edit reaches one
// on its own and owes no restart.
func reportAreaEdit(jsonMode bool, areasFile, msg string, res nibcore.AreaEditResult) error {
	if res.StaleLinkTarget != "" {
		file := sanitizeFilePath(areasFile)
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

// quotedArea renders an area path as a quoted value inside a message. The path
// goes through area.RenderPath because it is file-sourced whenever it is
// the declared set and caller-supplied text of unbounded length otherwise.
func quotedArea(path string) string {
	return fmt.Sprintf("%q", area.RenderPath(path))
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

// areaCascadePersisted names what an edit had already written, for the refusals
// raised after the cascade. Its rationale is in internal/graph/area_edit.go,
// whose copy this is.
func areaCascadePersisted(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return " The nib this command had already updated stays updated."
	}
	return fmt.Sprintf(" The %d nibs this command had already updated stay updated.", n)
}

// areaCascadeStranded says what that clause otherwise reads as reassurance
// about. Its rationale is in internal/graph/area_edit.go, whose copy this is.
func areaCascadeStranded(newPath string, written int) string {
	if newPath == "" || written == 0 {
		return ""
	}
	return " Until the rename finishes, those nibs carry an area that does not exist yet, so every change to them is refused."
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
		return "moved"
	}
	return "unassigned"
}

func areaDispositionAction(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "move"
	}
	return "unassign"
}
