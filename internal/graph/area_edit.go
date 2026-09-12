package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/safetext"
)

// The area vocabulary mutations: message formatters over nibcore.Core's area
// verbs. The verb itself lives in nibcore whole — there is one lock order, and a
// surface that stepped through Core could not obey it (see nibcore.editArea).
// What stays here is argument-shape questions and the wording of every refusal.
//
// Name no filesystem path in a message worded here — these messages travel to an
// HTTP client, where an absolute path discloses the operating-system username
// and the project layout. A path can still reach a client underneath the
// wording: an IO failure's Cause, or a read failure the planner passed through
// raw. `nibs serve` scrubs the rendered message at its own boundary
// (servedErrorPresenter in cmd/serve_pathscrub.go); `nibs query` keeps it for an
// operator repairing a broken store.

// renameAreaImpl renames the declared node at input.Path, cascading to every nib
// assigned at or below it.
func (r *mutationResolver) renameAreaImpl(ctx context.Context, input model.RenameAreaInput) (*model.Config, error) {
	// Answered before the call: the arguments alone decide it, and waiting for
	// the store's write lock has no deadline but this request's own end. The
	// store refuses these names again under the lock, in wording about the file.
	if err := validateAreaRenameArgument(input.NewName); err != nil {
		return nil, err
	}

	res, err := r.AreaWriter.RenameArea(ctx, input.Path, input.NewName)
	if err != nil {
		return nil, wordAreaRenameFailure(err)
	}
	r.reportStaleAreaLink(res)
	return configResultWithAreas(r.Reader, res.Areas), nil
}

// removeAreaImpl retires the declared node at input.Path together with the
// subtree it heads, disposing of every nib assigned at or below it as the input
// says.
func (r *mutationResolver) removeAreaImpl(ctx context.Context, input model.RemoveAreaInput) (*model.Config, error) {
	disposition, err := areaDisposition(input)
	if err != nil {
		return nil, err
	}

	res, err := r.AreaWriter.RemoveArea(ctx, input.Path, disposition)
	if err != nil {
		return nil, wordAreaRetireFailure(err)
	}
	r.reportStaleAreaLink(res)
	return configResultWithAreas(r.Reader, res.Areas), nil
}

// validateAreaRenameArgument refuses a new name the argument alone rules out.
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

// areaDisposition reads which disposition a retire was given. `unassign: false`
// is no disposition rather than a contradiction with moveTo: a client sends it
// for an unchecked box.
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
// replaced was a SYMLINK: the atomic write leaves a regular file in its place, so
// restoring the old target brings back the pre-edit vocabulary while the nibs
// this edit rewrote stay as it left them — after a rename, on a path that
// vocabulary does not declare.
//
// It goes to the store's warning sink; the answer is a Config, so carrying a
// warning to the client would be a schema change.
func (r *mutationResolver) reportStaleAreaLink(res nibcore.AreaEditResult) {
	if res.StaleLinkTarget == "" {
		return
	}
	r.AreaWriter.Warn("this store's areas.yml was a symlink to %s and is now a regular file; %s still declares the old vocabulary, so update or remove it",
		res.StaleLinkTarget, res.StaleLinkTarget)
}

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
		case nibcore.AreaEditPhaseConfirm:
			// A retire that named a disposition is past its cascade here and
			// reports that; one that named none rewrote nothing, and the shared
			// arm below words it.
			if ioErr.Disposition.Kind != nibcore.AreaDispositionNone {
				return areaRetireConfirmFailure(ioErr)
			}
		case nibcore.AreaEditPhaseWrite:
			return areaRetireWriteFailure(ioErr)
		}
	}
	return wordAreaEditFailure(err, "retire")
}

// wordAreaEditFailure words the failures both verbs share. verb names what the
// caller asked for; one refusal needs it to say what there is none of.
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
	var arrived *nibcore.AreaMembersArrivedError
	if errors.As(err, &arrived) {
		return wordAreaRefusal(err,
			"the vocabulary was left as it was: this edit did not see %s assigned at or below area %q (%s) when it read the store — a writer that does not take the store's lock landed it, a `git pull` in the store being the usual one; %srerun the same mutation, which decides from the store as it now stands%s",
			areaNibCount(len(arrived.Members)), config.RenderAreaPath(arrived.Path),
			namedMembers(arrived.Members), areaCascadePersisted(len(arrived.Written)),
			areaCascadeStranded(arrived.NewPath, len(arrived.Written)))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseLock:
			var waitEnded *nibcore.StoreLockWaitEndedError
			if errors.As(ioErr.Cause, &waitEnded) {
				return wordAreaRefusal(ioErr,
					"nothing was written: this request ended after %s, while this edit was still waiting for the store's write lock — rerun the same mutation, which decides from the store as it then stands",
					waitEnded.Waited.Round(time.Millisecond))
			}
			return wordAreaRefusal(ioErr,
				"this store's write lock could not be taken, and an areas edit rewrites both the nibs and the vocabulary so it must hold one: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadVocabulary:
			return wordAreaRefusal(ioErr,
				"nothing was written: re-reading this store's areas vocabulary under its write lock failed: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadNibs:
			return wordAreaRefusal(ioErr,
				"nothing was written: re-reading this store's nibs under its write lock failed: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseConfirm:
			return wordAreaRefusal(ioErr,
				"the vocabulary was left as it was: re-reading this store's nibs to confirm that nothing is assigned at or below area %q failed: %v — %srerun the same mutation once that is fixed%s",
				config.RenderAreaPath(ioErr.Path), ioErr.Cause, areaCascadePersisted(len(ioErr.Written)),
				areaCascadeStranded(ioErr.NewPath, len(ioErr.Written)))
		case nibcore.AreaEditPhaseReload:
			// Both writes landed, so there is nothing to rerun — but answering
			// with the stale vocabulary and calling the mutation a success
			// renders in a client as the edit not having happened.
			return wordAreaRefusal(ioErr,
				"both halves of this edit landed on disk, and re-reading the vocabulary it just wrote then failed: %v — there is nothing to rerun; until that file can be read again this store answers from the vocabulary as it was before the edit",
				ioErr.Cause)
		}
	}
	// What is left is a config.AreaEditRefusal, already worded for a reader who
	// may not be told where the store is, or a read failure the planner passed
	// through raw.
	return err
}

func areaRetiredWhileWaiting(err error, e *nibcore.AreaRetiredWhileWaitingError) error {
	return wordAreaRefusal(err, "nothing was written: this store declared area %q when this edit began and does not declare it now — another nibs process retired or renamed it while this one waited for the store's write lock; read the store's config for the vocabulary as it now stands",
		config.RenderAreaPath(e.Path))
}

// areaRetireWriteFailure reports a retire whose members are disposed of and
// whose config write then failed. It branches on whether a disposition was
// NAMED, not on which one: with none there is nothing to report as done and no
// argument to drop.
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

// areaRetireConfirmFailure reports a retire whose disposition completed and
// whose confirming re-read then failed. It prescribes the same rerun as
// areaRetireWriteFailure but promises no outcome — a nib that arrived in that
// window refuses it.
func areaRetireConfirmFailure(e *nibcore.AreaEditIOError) error {
	return wordAreaRefusal(e,
		"%s %s from area %q, then re-reading this store's nibs to confirm that nothing is assigned at or below it failed: %v — %q is still declared and those writes are persisted; rerun WITHOUT %s once that is fixed, which decides from the store as it then stands",
		areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)), config.RenderAreaPath(e.Path), e.Cause,
		config.RenderAreaPath(e.Path), areaDispositionField(e.Disposition))
}

// areaRefusal is one of the store's typed refusals worded for this surface.
//
// Wrap the refusal, never replace it: cmd/set.go's mutationErrCode classifies
// `nibs query`'s exit on the concrete type the store raised, and an error that
// dropped it arrives as a bare validation fallback.
type areaRefusal struct {
	msg   string
	cause error
}

func (e *areaRefusal) Error() string { return e.msg }

func (e *areaRefusal) Unwrap() error { return e.cause }

func wordAreaRefusal(cause error, format string, a ...any) error {
	return &areaRefusal{msg: fmt.Sprintf(format, a...), cause: cause}
}

// areaPathVerb names what the caller asked to do with the path a refusal is
// about.
func areaPathVerb(role nibcore.AreaPathRole, verb string) string {
	if role == nibcore.AreaPathMoveTarget {
		return "move work to"
	}
	return verb
}

// areaDispositionField is the input field that asked for a disposition, for a
// message telling the caller to drop it. Only ever reached for a named one.
func areaDispositionField(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "moveTo"
	}
	return "unassign"
}

// areaDispositionVerb is the past tense a completed disposition reports in.
func areaDispositionVerb(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "reassigned"
	}
	return "unassigned"
}

// areaDispositionAction is the bare verb a refusal says there is nothing to do.
func areaDispositionAction(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "reassign"
	}
	return "unassign"
}

// areaMemberNameLimit is QueueNameLimit applied to an area's members.
const areaMemberNameLimit = QueueNameLimit

// namedMembers renders the member ids a refusal quotes, capped, with the number
// it elided stated. The ids are filename-derived, so they go through
// safetext.Strip.
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

func areaNibsAre(n int) string {
	if n == 1 {
		return "1 nib is"
	}
	return fmt.Sprintf("%d nibs are", n)
}

// areaCascadePersisted names what an edit had already written, empty when the
// cascade wrote nothing. It carries its own trailing separator, so no format
// string holding it has to.
func areaCascadePersisted(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "the nib it had already rewritten is persisted, so "
	}
	return fmt.Sprintf("the %d nibs it had already rewritten are persisted, so ", n)
}

// areaCascadeStranded says what the persisted clause otherwise reads as
// reassurance about: a rename's cascaded members sit on the new path while the
// vocabulary still declares the old one. A retire's land on a declared path or
// on none, so it renders nothing.
func areaCascadeStranded(newPath string, written int) string {
	if newPath == "" || written == 0 {
		return ""
	}
	return " — until that rerun those nibs carry an undeclared path, so every write to them is refused"
}

func areaNibCount(n int) string {
	if n == 1 {
		return "1 nib"
	}
	return fmt.Sprintf("%d nibs", n)
}
