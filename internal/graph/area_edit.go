package graph

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/safetext"
)

// The area vocabulary mutations. Both are message formatters over
// nibcore.Core's area verbs: the verb itself — the locks, the re-read under
// them, the plan, the member cascade, the areas.yml write and the reload — lives
// in nibcore, whole, because there is one lock order and a surface that reached
// into Core step by step could not obey it (see nibcore.editArea).
//
// What stays here is what only this surface knows: the argument-shape questions
// the wire can ask before any lock is taken, and the wording of every refusal.
// nibcore's refusals carry FIELDS, so `nibs area rm --unassign` and
// `unassign: true` are the same refusal spoken to two different readers and
// neither sentence has to live where the other one's reader would see it.
//
// NO FILESYSTEM PATH IS NAMED by anything returned from here, and that is a
// mechanism rather than a claim: config.AreaEditRefusal renders path-free from
// Error and names its file only through Naming, nibcore's refusals keep theirs
// in a field nothing below reads, and the sentences below interpolate nothing
// but area paths and nib ids. These messages travel to an HTTP client that has
// no business knowing where the store sits on disk — an absolute path there
// discloses the operating-system username and the project layout.
//
// The exception is an IO failure's Cause: an operating-system error embeds the
// path it failed on, and that is reachable only from a store that is already
// broken. Redacting it is a boundary of its own and is not this one.

// renameAreaImpl renames the declared node at input.Path, cascading to every nib
// assigned at or below it.
func (r *mutationResolver) renameAreaImpl(input model.RenameAreaInput) (*model.Config, error) {
	// Answered BEFORE the call, because the two arguments alone answer it and no
	// vocabulary can change that answer. The store's write lock is a blocking
	// flock with no timeout, so a pure argument question asked underneath it
	// makes a malformed request sit silent for as long as any other cooperating
	// writer holds the store — a whole `nibs migrate` run. None of this is what
	// keeps a bad name off disk: the store refuses every one of them under the
	// lock, in wording about the FILE rather than about the argument.
	if err := validateAreaRenameArgument(input.NewName); err != nil {
		return nil, err
	}

	res, err := r.AreaWriter.RenameArea(input.Path, input.NewName)
	if err != nil {
		return nil, wordAreaRenameFailure(err)
	}
	r.reportStaleAreaLink(res)
	return configResultWithAreas(r.Reader, res.Areas), nil
}

// removeAreaImpl retires the declared node at input.Path together with the
// subtree it heads, disposing of every nib assigned at or below it as the input
// says.
func (r *mutationResolver) removeAreaImpl(input model.RemoveAreaInput) (*model.Config, error) {
	disposition, err := areaDisposition(input)
	if err != nil {
		return nil, err
	}

	res, err := r.AreaWriter.RemoveArea(input.Path, disposition)
	if err != nil {
		return nil, wordAreaRetireFailure(err)
	}
	r.reportStaleAreaLink(res)
	return configResultWithAreas(r.Reader, res.Areas), nil
}

// validateAreaRenameArgument refuses a new name the argument alone rules out.
//
// config.ValidateAreaName is CALLED rather than copied, so the empty, padded and
// over-long clauses have one definition. The separator clause is this surface's,
// because it is about what a rename MEANS rather than about what the file may
// hold: a rename changes a node's name and never moves it between parents, so a
// value carrying the separator is not a name at all. The vocabulary's own
// revalidation refuses it too, but only as a file that would not load.
func validateAreaRenameArgument(newName string) error {
	if err := config.ValidateAreaName(newName); err != nil {
		return err
	}
	if strings.Contains(newName, config.AreaPathSeparator) {
		return fmt.Errorf("%q is not a name: a rename changes a node's name and never moves it between parents, so send the name alone",
			config.RenderAreaPath(newName))
	}
	return nil
}

// areaDisposition reads which disposition a retire was given and resolves its
// target.
//
// The two are mutually exclusive, and there is no wire equivalent of Cobra's
// MarkFlagsMutuallyExclusive to refuse them for us — so the contradiction is
// refused HERE, before any lock, where a malformed request costs nothing.
// `unassign: false` is read as NO disposition rather than as a contradiction: it
// is what a client sends when a checkbox is off, and taking it as a conflicting
// answer would refuse a perfectly ordinary `{path, moveTo, unassign: false}`.
func areaDisposition(input model.RemoveAreaInput) (nibcore.AreaDisposition, error) {
	switch {
	case input.MoveTo != nil && input.Unassign != nil && *input.Unassign:
		return nibcore.AreaDisposition{}, fmt.Errorf(
			"moveTo and unassign: true are two different dispositions for the same nibs — send one")
	case input.MoveTo != nil:
		return nibcore.MoveAreaMembersTo(*input.MoveTo), nil
	case input.Unassign != nil && *input.Unassign:
		return nibcore.UnassignAreaMembers(), nil
	default:
		return nibcore.AreaDisposition{}, nil
	}
}

// reportStaleAreaLink passes on the note an edit owes when the areas.yml it
// replaced was a SYMLINK: the atomic write leaves a regular file in its place and
// the old target still declares the pre-edit vocabulary, so when whatever manages
// that target restores it the vocabulary declares the old paths again while every
// nib the cascade rewrote carries the new one — undeclared, and write-refused
// from then on.
//
// It goes to the store's warning sink rather than into the answer because the
// answer is a Config: carrying a warning to the client is a schema change. The
// sink is where a `nibs serve` operator reads, which is the reader who can act on
// it, and `nibs area rename` prints the same note on its own account.
func (r *mutationResolver) reportStaleAreaLink(res nibcore.AreaEditResult) {
	if res.StaleLinkTarget == "" {
		return
	}
	r.AreaWriter.Warn("this store's areas.yml was a symlink to %s and is now a regular file; %s still declares the old vocabulary, so update or remove it",
		res.StaleLinkTarget, res.StaleLinkTarget)
}

// wordAreaRenameFailure words every way a rename can fail for this surface.
func wordAreaRenameFailure(err error) error {
	var unchanged *nibcore.AreaNameUnchangedError
	if errors.As(err, &unchanged) {
		return wordAreaRefusal(err, "area %q is already named %q, so the rename would change nothing",
			config.RenderAreaPath(unchanged.Path), config.RenderAreaPath(unchanged.Name))
	}
	var taken *nibcore.AreaNameTakenError
	if errors.As(err, &taken) {
		return wordAreaRefusal(err, "cannot rename area %q to %q: this store already declares %q, and two siblings with one name make one path mean two nodes",
			config.RenderAreaPath(taken.Path), config.RenderAreaPath(taken.NewName), config.RenderAreaPath(taken.Sibling))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return wordAreaRefusal(ioErr,
				"rewrote %d of the %s assigned at or below area %q, then %v — the vocabulary still declares %q and those writes are persisted; rerun the same mutation to finish it, since a nib already rewritten is no longer a member and the rerun starts where this stopped",
				len(ioErr.Written), areaNibCount(len(ioErr.Members)), config.RenderAreaPath(ioErr.Path),
				ioErr.Cause, config.RenderAreaPath(ioErr.Path))
		case nibcore.AreaEditPhaseWrite:
			return wordAreaRefusal(ioErr,
				"rewrote %s from area %q to %q, then the store's areas.yml could not be updated: %v — the vocabulary still declares %q and those writes are persisted; rerun the same mutation to finish it, since the rewritten nibs are no longer members and the rerun only renames the declaration",
				areaNibCount(len(ioErr.Written)), config.RenderAreaPath(ioErr.Path), config.RenderAreaPath(ioErr.NewPath),
				ioErr.Cause, config.RenderAreaPath(ioErr.Path))
		}
	}
	return wordAreaEditFailure(err, "rename")
}

// wordAreaRetireFailure words every way a retire can fail for this surface.
func wordAreaRetireFailure(err error) error {
	var members *nibcore.AreaMembersPresentError
	if errors.As(err, &members) {
		return wordAreaRefusal(err,
			"cannot retire area %q: %s assigned at or below it (%s) — reassign them with moveTo, drop their assignment with unassign: true, or leave the declaration in place",
			config.RenderAreaPath(members.Path), areaNibsAre(len(members.Members)), namedMembers(members.Members))
	}
	var empty *nibcore.AreaDispositionEmptyError
	if errors.As(err, &empty) {
		return wordAreaRefusal(err, "nothing to %s: no nib is assigned at or below area %q — drop %s to retire it",
			areaDispositionAction(empty.Disposition), config.RenderAreaPath(empty.Path),
			areaDispositionField(empty.Disposition))
	}
	var within *nibcore.AreaMoveTargetWithinError
	if errors.As(err, &within) {
		return wordAreaRefusal(err,
			"cannot move members to %q: it is declared at or below %q, which this mutation is retiring — name an area outside it, or send unassign: true to drop their assignment",
			config.RenderAreaPath(within.Target), config.RenderAreaPath(within.Path))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return wordAreaRefusal(ioErr,
				"%s %d of the %s assigned at or below area %q, then %v — %q is still declared and those writes are persisted; rerun the same mutation to finish it, since a nib already disposed of is no longer a member and the rerun starts where this stopped",
				areaDispositionVerb(ioErr.Disposition), len(ioErr.Written), areaNibCount(len(ioErr.Members)),
				config.RenderAreaPath(ioErr.Path), ioErr.Cause, config.RenderAreaPath(ioErr.Path))
		case nibcore.AreaEditPhaseWrite:
			return areaRetireWriteFailure(ioErr)
		}
	}
	return wordAreaEditFailure(err, "retire")
}

// wordAreaEditFailure words the failures both verbs share. verb names what the
// caller asked for, for the one refusal that has to say what there is none of.
func wordAreaEditFailure(err error, verb string) error {
	var undeclared *nibcore.AreaUndeclaredError
	if errors.As(err, &undeclared) {
		if !undeclared.Areas.Declared() {
			return wordAreaRefusal(err, "this store declares no areas, so there is none to %s — declare an `areas:` block in the store's areas.yml first",
				areaPathVerb(undeclared.Role, verb))
		}
		return wordAreaRefusal(err, "this store declares no area %q: the declared areas are %s",
			config.RenderAreaPath(undeclared.Path), undeclared.Areas.List())
	}
	var retired *nibcore.AreaRetiredWhileWaitingError
	if errors.As(err, &retired) {
		return areaRetiredWhileWaiting(err, retired)
	}
	var vanished *nibcore.AreaVocabularyVanishedError
	if errors.As(err, &vanished) {
		return wordAreaRefusal(err, "nothing was written: this store's areas vocabulary could not be read under its write lock — the file does not exist; restore it before editing the areas it declares")
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseLock:
			return wordAreaRefusal(ioErr,
				"this store's write lock could not be taken, and an areas edit rewrites both the nibs and the vocabulary so it must hold one: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadVocabulary:
			return wordAreaRefusal(ioErr,
				"nothing was written: re-reading this store's areas vocabulary under its write lock failed: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadNibs:
			return wordAreaRefusal(ioErr,
				"nothing was written: re-reading this store's nibs under its write lock failed: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseReload:
			// The one IO failure with nothing to rerun: both writes landed, so
			// disk holds the finished edit. What is wrong is in THIS process,
			// which keeps the vocabulary it could still read — the one the edit
			// replaced. Reporting it keeps the alternative off the wire: answering
			// with that vocabulary and calling the mutation a success, which
			// renders in a client as the edit not having happened.
			return wordAreaRefusal(ioErr,
				"both halves of this edit landed on disk, and re-reading the vocabulary it just wrote then failed: %v — there is nothing to rerun; until that file can be read again this store answers from the vocabulary as it was before the edit",
				ioErr.Cause)
		}
	}
	// What is left is a config.AreaEditRefusal, whose Error is already worded for
	// a reader who may not be told where the store is, or a read failure the
	// planner passed through raw. Neither is re-worded here: the first would only
	// be restated, and the second has nothing this surface knows to add.
	return err
}

// areaRetiredWhileWaiting words the race the CLI reports through the same
// classification: the area named was declared when this store was last read and
// is not declared now, because another nibs process retired or renamed it while
// this edit waited for the store's write lock.
//
// A SERVER'S "last read" is not a process's startup — it reloads the vocabulary
// on every areas.yml event — so this says what it can honestly say and no more.
// The remedy is the vocabulary as it now stands, not a rerun: nothing was
// written, and the node the caller named is not coming back.
func areaRetiredWhileWaiting(err error, e *nibcore.AreaRetiredWhileWaitingError) error {
	return wordAreaRefusal(err, "nothing was written: this store declared area %q when this edit began and does not declare it now — another nibs process retired or renamed it while this one waited for the store's write lock; read the store's config for the vocabulary as it now stands",
		config.RenderAreaPath(e.Path))
}

// areaRetireWriteFailure reports a retire whose members are disposed of and
// whose config write then failed, branching on whether a disposition was
// actually NAMED rather than on which one it was.
//
// With no disposition there is nothing to report as done and no argument to
// drop: that branch is reachable only for an area nothing was assigned to, where
// the refusal above has already established the member set is empty and the
// cascade therefore wrote nothing. Selecting the wording on the move/unassign
// pair instead made the same case in cmd/area.go claim an unassignment had run
// and prescribe dropping an argument the caller never sent.
func areaRetireWriteFailure(e *nibcore.AreaEditIOError) error {
	if e.Disposition.Kind == nibcore.AreaDispositionNone {
		return wordAreaRefusal(e,
			"area %q could not be retired: the store's areas.yml could not be updated: %v — nothing is assigned at or below it, so nothing was rewritten and the store is as it was; rerun the same mutation once that is fixed",
			config.RenderAreaPath(e.Path), e.Cause)
	}
	return wordAreaRefusal(e,
		"%s %s from area %q, then the store's areas.yml could not be updated: %v — %q is still declared and those writes are persisted; rerun WITHOUT %s to retire it, which is what finishes the job now that nothing is assigned below it",
		areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)), config.RenderAreaPath(e.Path), e.Cause,
		config.RenderAreaPath(e.Path), areaDispositionField(e.Disposition))
}

// areaRefusal is one of the store's typed refusals worded for this surface.
//
// The wording has to travel WITH the refusal rather than replace it: cmd/set.go's
// mutationErrCode classifies `nibs query` on the concrete type the store raised,
// so an error that dropped it would arrive as a bare validation fallback — an
// area another process retired while this edit waited would be reported as bad
// input rather than as a store that moved, which is the divergence between the
// two surfaces this closes.
//
// Unwrap reaches the refusal and stops there: nibcore.AreaEditIOError carries no
// Unwrap of its own, so a classifier keyed on an OS sentinel still cannot claim
// what an area edit's Cause happens to be.
type areaRefusal struct {
	msg   string
	cause error
}

func (e *areaRefusal) Error() string { return e.msg }

func (e *areaRefusal) Unwrap() error { return e.cause }

// wordAreaRefusal words one of the store's refusals for this surface.
func wordAreaRefusal(cause error, format string, a ...any) error {
	return &areaRefusal{msg: fmt.Sprintf(format, a...), cause: cause}
}

// areaPathVerb names what the caller asked to do with the path a refusal is
// about. A move target is the one role whose verb is not the mutation's own.
func areaPathVerb(role nibcore.AreaPathRole, verb string) string {
	if role == nibcore.AreaPathMoveTarget {
		return "move work to"
	}
	return verb
}

// areaDispositionField is the input field that asked for a disposition, for a
// message telling the caller to drop it. It is only ever reached for one that
// was named.
func areaDispositionField(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "moveTo"
	}
	return "unassign"
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

// areaMemberNameLimit is QueueNameLimit applied to the other project-sized set
// this package refuses over. See that constant for why an enumeration is capped
// at all; an area's members are the same shape of set as a milestone's queue.
const areaMemberNameLimit = QueueNameLimit

// namedMembers renders the member ids a refusal quotes, capped, with the number
// it elided stated so a shortened list cannot be read as a complete one. The ids
// are filename-derived, so they go through safetext.Strip like every other
// file-sourced scalar reaching a message.
func namedMembers(ids []string) string {
	named := ids
	if len(named) > areaMemberNameLimit {
		named = named[:areaMemberNameLimit]
	}
	rendered := make([]string, len(named))
	for i, id := range named {
		rendered[i] = safetext.Strip(id)
	}
	out := strings.Join(rendered, ", ")
	if len(ids) > len(named) {
		out += fmt.Sprintf(", and %d more", len(ids)-len(named))
	}
	return out
}

// areaNibsAre is areaNibCount as the subject of a refusal sentence ("1 nib is" /
// "3 nibs are").
func areaNibsAre(n int) string {
	if n == 1 {
		return "1 nib is"
	}
	return fmt.Sprintf("%d nibs are", n)
}

// areaNibCount renders the tally these messages carry ("1 nib" / "3 nibs"), so
// none of them has to print a parenthesized plural.
func areaNibCount(n int) string {
	if n == 1 {
		return "1 nib"
	}
	return fmt.Sprintf("%d nibs", n)
}
